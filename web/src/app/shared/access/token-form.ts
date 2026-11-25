import { Component, computed, effect, inject, input, output, signal, untracked } from '@angular/core';
import { Checkbox } from '../ui/checkbox';
import { ApiError } from '../../api/problem';
import { Endpoint, EndpointsApi } from '../../api/endpoints-api';
import { CreateTokenRequest } from '../../api/tokens-api';
import { SessionStore } from '../../core/session-store';
import { SelectField, SelectOption, TextField } from './fields';
import { fieldMessage } from '../field-errors';
import { ErrorNotice } from '../error-notice';
import { asApiError } from '../paged-resource';

export const TOKEN_ACTIONS = ['read', 'annotate', 'deploy', 'overwrite', 'delete', 'manage'] as const;
type TokenAction = (typeof TOKEN_ACTIONS)[number];

/** The three things a token can be (Q-UI-213): everything on the installation, the owner's repository permissions, or a list. */
export type TokenScopeChoice = 'admin' | 'inherit' | 'restricted';

const DAY = 86_400;

/** Lifetime choices; the numbers are seconds, `default` leaves the server's default, `0` never expires. */
const PRESET_SECONDS: Readonly<Record<string, number>> = {
  d7: 7 * DAY,
  d30: 30 * DAY,
  d90: 90 * DAY,
  d365: 365 * DAY,
};

/** The non-secret part of the form, for a draft kept across re-authentication (R-UI-042). */
export interface TokenDraft {
  /** The name of a technical token (R-AUTH-045); empty for every other form. */
  readonly name?: string;
  readonly description: string;
  readonly lifetime: string;
  readonly customDate: string;
  readonly scope: TokenScopeChoice;
  readonly repositories: string;
  readonly packages: string;
  readonly actions: readonly string[];
  readonly endpoints: readonly string[];
}

/**
 * What the form creates (R-UI-181): a personal token (`token`, a required description), a technical token of a service account
 * (`technical`: its name first, required, then an optional description), or a new secret of a technical token (`secret`: neither,
 * the name is the token's and stays).
 */
export type TokenFormMode = 'token' | 'technical' | 'secret';

/** The request the form emits: a create of a token, with the name of a technical token in the mode `technical`. */
export type TokenFormRequest = CreateTokenRequest & { readonly name?: string };

/**
 * The create form of a personal token or of a technical token of a service account (R-UI-029, R-UI-026, R-UI-181): description,
 * lifetime (presets, a custom date, or "never" where the owner may have a non-expiring token) and
 * scope (administrator rights for a caller who is one, the owner's repository permissions, or repositories and
 * actions; grammar of R-AUTH-024, Q-UI-213). It
 * validates what it can before the request and shows server field errors next to the fields. The
 * idempotency key is the parent's, generated when the form opens. Where the caller may read the
 * endpoints (`endpoints:read`), the form offers the binding of the token to endpoints
 * (`scope.endpoints`, R-EP-011, Q-UI-91) that accept its type; none chosen leaves the token unbound.
 */
@Component({
  selector: 'quader-token-form',
  imports: [Checkbox, ErrorNotice, SelectField, TextField],
  template: `
    <form class="form" (submit)="submit($event)" novalidate>
      @if (mode() === 'technical') {
        <quader-text-field
          width="name"
          i18n-label="@@token-form.name"
          label="Name"
          name="name"
          [(value)]="name"
          [required]="true"
          [error]="fieldError('/name', 'name')"
          i18n-hint="@@token-form.name-hint"
          hint="The system that uses this token, for example nightly-mirror. It cannot be changed."
        />
        <quader-text-field
          width="name"
          i18n-label="@@token-form.description-optional"
          label="Description (optional)"
          name="description"
          [(value)]="description"
          [error]="fieldError('/description', 'description')"
        />
      } @else if (mode() === 'token') {
        <quader-text-field
          width="name"
          i18n-label="@@token-form.description"
          label="Description"
          name="description"
          [(value)]="description"
          [required]="true"
          [error]="fieldError('/description', 'description')"
          i18n-hint="@@token-form.description-hint"
          hint="What the token is for, for example the machine or the job."
        />
      }
      <quader-select-field
        i18n-label="@@token-form.lifetime"
        label="Lifetime"
        name="lifetime"
        [options]="lifetimeOptions()"
        [(value)]="lifetime"
        [error]="fieldError('/ttlSeconds', 'lifetime')"
      />
      @if (lifetimeCapped()) {
        <p class="hint" i18n="@@token-form.life.capped">
          The installation caps the lifetime of service-account tokens. A longer lifetime is refused.
        </p>
      }
      @if (lifetime() === 'custom') {
        <quader-text-field
          width="bytes"
          i18n-label="@@token-form.expires"
          label="Expires on (UTC)"
          name="expires"
          type="date"
          [(value)]="customDate"
          [required]="true"
          [error]="fieldError('/expiresAt', 'customDate')"
        />
      }

      <fieldset class="scope">
        <legend i18n="@@token-form.scope">Scope</legend>
        @if (offerAdmin()) {
          <label class="radio">
            <input type="radio" name="scope" [checked]="scopeChoice() === 'admin'" (change)="scopeChoice.set('admin')" />
            <span i18n="@@token-form.scope-admin">Administrator: everything on this installation</span>
          </label>
        }
        <label class="radio">
          <input type="radio" name="scope" [checked]="scopeChoice() === 'inherit'" (change)="scopeChoice.set('inherit')" />
          <span i18n="@@token-form.scope-all">All repository permissions of the owner, without administration</span>
        </label>
        <label class="radio">
          <input type="radio" name="scope" [checked]="restricted()" (change)="scopeChoice.set('restricted')" />
          <span i18n="@@token-form.scope-restricted">Only the repositories and actions below</span>
        </label>
        @if (restricted()) {
          <quader-text-field
            width="full"
            i18n-label="@@token-form.repositories"
            label="Repositories"
            name="repositories"
            type="textarea"
            [(value)]="repositories"
            [required]="true"
            [error]="fieldError('/scope/repositories', 'repositories')"
            i18n-hint="@@token-form.repositories-hint"
            hint="One project/key or pattern per line, for example web/npm-internal or web/npm-team-*; or a class such as @any-local."
          />
          <quader-text-field
            width="full"
            i18n-label="@@token-form.packages"
            label="Package patterns (optional)"
            name="packages"
            type="textarea"
            [(value)]="packages"
            i18n-hint="@@token-form.packages-hint"
            hint="One pattern per line, for example @team-a/*. Empty means every package."
          />
          <fieldset class="actions">
            <legend i18n="@@token-form.actions">Actions</legend>
            @for (a of actionList; track a) {
              <quader-checkbox [checked]="actions().includes(a)" (checkedChange)="toggle(a, $event)">{{ a }}</quader-checkbox>
            }
            @if (fieldError('/scope/actions', 'actions'); as text) {
              <p class="field-error">{{ text }}</p>
            }
          </fieldset>
        }
      </fieldset>

      @if (endpointsFailure(); as e) {
        <!-- Without the endpoints the token could not be bound to one: the failure says so and reads them again (R-UI-216). -->
        <quader-error-notice [error]="e" (retry)="readEndpoints()" />
      }
      @if (endpointNames().length > 0) {
        <fieldset class="endpoints">
          <legend i18n="@@token-form.endpoints">Endpoints</legend>
          <p class="hint" i18n="@@token-form.endpoints-hint">
            Valid only on the endpoints you tick. Tick none and the token is accepted wherever its type is
            accepted. A further endpoint accepts only service-account tokens bound to it.
          </p>
          @for (e of endpointNames(); track e) {
            <quader-checkbox [checked]="endpoints().includes(e)" (checkedChange)="toggleEndpoint(e, $event)">{{ e }}</quader-checkbox>
          }
        </fieldset>
      }

      <div class="row">
        <button class="btn btn-confirm" type="submit" [disabled]="busy()">{{ submitLabel() }}</button>
        <button class="btn btn-tertiary" type="button" (click)="cancelled.emit()" i18n="@@token-form.cancel">
          Cancel
        </button>
      </div>
    </form>
  `,
  styles: `
    .form {
      display: flex;
      flex-direction: column;
      align-items: flex-start;
      max-inline-size: 36rem;
    }
    fieldset {
      margin: 0 0 var(--quader-space-3);
      padding: var(--quader-space-2) var(--quader-space-3);
      border: 1px solid var(--quader-border);
      border-radius: var(--quader-radius-m);
      inline-size: 100%;
      box-sizing: border-box;
    }
    .hint {
      flex-basis: 100%;
      margin: 0;
      color: var(--quader-text-muted);
      font: var(--quader-type-body-small);
    }
    .actions,
    .endpoints {
      display: flex;
      flex-wrap: wrap;
      gap: var(--quader-space-1) var(--quader-space-3);
    }
    /* The radio stands on the middle of its label's first line, also when a narrow screen wraps the label (R-UI-175). */
    .radio {
      display: flex;
      align-items: flex-start;
      gap: var(--quader-space-2);
      min-block-size: 24px;
      margin-block-end: var(--quader-space-1);
    }
    .row {
      display: flex;
      gap: var(--quader-space-2);
    }
    .field-error {
      flex-basis: 100%;
      margin: 0;
      color: var(--quader-status-bad);
      font: var(--quader-type-body-small);
    }
  `,
})
export class TokenForm {
  /** Offers "Never expires" (service-account tokens may be non-expiring, R-AUTH-026). */
  readonly allowNonExpiring = input(false);
  /**
   * The lifetime is capped by the installation's maximum for service-account tokens, as for the tokens a
   * project Owner issues (R-AUTHZ-026): the form says so and the server refuses a longer one.
   */
  readonly lifetimeCapped = input(false);
  /** The type of the token this form creates; it decides which endpoints can be offered. */
  readonly tokenType = input<'pat' | 'sat'>('pat');
  /** What the form creates: a personal token, a technical token with its name, or a new secret of one (R-UI-181). */
  readonly mode = input<TokenFormMode>('token');
  /** Offers administrator rights: only for the personal token of a caller who holds the administrator role (Q-UI-213). */
  readonly offerAdmin = input(false);
  readonly submitLabel = input.required<string>();
  readonly busy = input(false);
  /** The last failed submit; its field errors appear next to the fields. */
  readonly failure = input<ApiError | null>(null);
  /** A draft restored after re-authentication. */
  readonly restore = input<TokenDraft | null>(null);

  readonly submitted = output<TokenFormRequest>();
  readonly cancelled = output<void>();
  /** The current non-secret form content, for the draft kept across re-authentication. */
  readonly draft = output<TokenDraft>();

  private readonly endpointsApi = inject(EndpointsApi);
  private readonly session = inject(SessionStore);

  protected readonly actionList = TOKEN_ACTIONS;
  protected readonly name = signal('');
  protected readonly description = signal('');
  protected readonly lifetime = signal('default');
  protected readonly customDate = signal('');
  protected readonly scopeChoice = signal<TokenScopeChoice>('inherit');
  protected readonly restricted = computed(() => this.scopeChoice() === 'restricted');
  protected readonly repositories = signal('');
  protected readonly packages = signal('');
  protected readonly actions = signal<readonly TokenAction[]>(['read']);
  private readonly endpointList = signal<readonly Endpoint[]>([]);
  /**
   * The configured endpoints the token can be bound to: those whose accepted token types include
   * the type of this form. A token bound to any other would work nowhere (R-EP-011); the server
   * refuses it with 400 invalid_scope. Empty when the caller cannot read the endpoints.
   */
  protected readonly endpointNames = computed(() =>
    this.endpointList()
      .filter((e) => !e.inert && e.configuration?.auth.tokenTypes.includes(this.tokenType()))
      .map((e) => e.name),
  );
  protected readonly endpoints = signal<readonly string[]>([]);
  /** The failed read of the endpoints, kept until a read answers (R-UI-216). */
  protected readonly endpointsFailure = signal<ApiError | null>(null);

  /** Reads the endpoints a token can be bound to; the server still checks every name it is given. */
  protected readEndpoints(): void {
    this.endpointsApi.list().then(
      (list) => {
        this.endpointsFailure.set(null);
        this.endpointList.set(list);
      },
      (e: unknown) => this.endpointsFailure.set(asApiError(e)),
    );
  }
  private readonly localErrors = signal<Readonly<Record<string, string>>>({});

  protected readonly lifetimeOptions = computed<readonly SelectOption[]>(() => [
    { value: 'default', label: $localize`:@@token-form.life.default:Server default` },
    { value: 'd7', label: $localize`:@@token-form.life.d7:7 days` },
    { value: 'd30', label: $localize`:@@token-form.life.d30:30 days` },
    { value: 'd90', label: $localize`:@@token-form.life.d90:90 days` },
    { value: 'd365', label: $localize`:@@token-form.life.d365:1 year` },
    { value: 'custom', label: $localize`:@@token-form.life.custom:Custom date` },
    ...(this.allowNonExpiring()
      ? [{ value: 'never', label: $localize`:@@token-form.life.never:Never expires` }]
      : []),
  ]);

  constructor() {
    if (this.session.has('endpoints:read')) this.readEndpoints();
    effect(() => {
      const d = this.restore();
      if (!d) return;
      untracked(() => {
        this.name.set(d.name ?? '');
        this.description.set(d.description);
        this.lifetime.set(d.lifetime);
        this.customDate.set(d.customDate);
        this.scopeChoice.set(d.scope === 'admin' && !this.offerAdmin() ? 'inherit' : d.scope);
        this.repositories.set(d.repositories);
        this.packages.set(d.packages);
        this.endpoints.set(d.endpoints ?? []);
        this.actions.set(d.actions.filter((a): a is TokenAction => TOKEN_ACTIONS.includes(a as TokenAction)));
      });
    });
  }

  /** The text for a server field error at `pointer` or a local one under `key`. */
  protected fieldError(pointer: string, key: string): string | null {
    const local = this.localErrors()[key];
    if (local) return local;
    const server = this.failure()?.fieldErrors.get(pointer);
    if (!server) return null;
    return fieldMessage(server);
  }

  protected toggle(action: TokenAction, on: boolean): void {
    this.actions.update((list) => (on ? [...new Set([...list, action])] : list.filter((a) => a !== action)));
  }

  protected toggleEndpoint(name: string, on: boolean): void {
    this.endpoints.update((list) => (on ? [...new Set([...list, name])] : list.filter((n) => n !== name)));
  }

  /** Emits the current content as a draft; the parent keeps it when re-authentication interrupts. */
  currentDraft(): TokenDraft {
    return {
      name: this.name(),
      description: this.description(),
      lifetime: this.lifetime(),
      customDate: this.customDate(),
      scope: this.scopeChoice(),
      repositories: this.repositories(),
      packages: this.packages(),
      actions: this.actions(),
      endpoints: this.endpoints(),
    };
  }

  protected submit(event: Event): void {
    event.preventDefault();
    this.draft.emit(this.currentDraft());
    const errors: Record<string, string> = {};
    const description = this.description().trim();
    const name = this.name().trim();
    if (this.mode() === 'token' && !description) {
      errors['description'] = $localize`:@@token-form.err.description:Enter a description.`;
    }
    if (this.mode() === 'technical' && !/^[a-z0-9][a-z0-9-]{0,62}$/.test(name)) {
      errors['name'] = $localize`:@@token-form.err.name:Enter the name of the system: lower-case letters, digits and -, for example nightly-mirror.`;
    }
    const lifetime = this.lifetime();
    if (lifetime === 'custom' && !/^\d{4}-\d{2}-\d{2}$/.test(this.customDate())) {
      errors['customDate'] = $localize`:@@token-form.err.date:Pick the expiry date.`;
    }
    const repositories = lines(this.repositories());
    if (this.restricted()) {
      if (repositories.length === 0) {
        errors['repositories'] = $localize`:@@token-form.err.repositories:Name at least one repository.`;
      }
      if (this.actions().length === 0) {
        errors['actions'] = $localize`:@@token-form.err.actions:Choose at least one action.`;
      }
    }
    this.localErrors.set(errors);
    if (Object.keys(errors).length > 0) return;

    const request: {
      name?: string;
      description: string;
      selfRotatable: boolean;
      ttlSeconds?: number;
      expiresAt?: string;
      scope?: CreateTokenRequest['scope'];
    } = { description, selfRotatable: false };
    if (this.mode() === 'technical') request.name = name;
    if (lifetime === 'never') request.ttlSeconds = 0;
    else if (lifetime === 'custom') request.expiresAt = `${this.customDate()}T23:59:59Z`;
    else if (lifetime in PRESET_SECONDS) request.ttlSeconds = PRESET_SECONDS[lifetime];
    if (this.restricted()) {
      const include = lines(this.packages());
      request.scope = {
        repositories: [
          {
            repositories,
            ...(include.length > 0 ? { packages: { include } } : {}),
            actions: [...this.actions()],
          },
        ],
      };
    } else if (this.scopeChoice() === 'admin' && this.offerAdmin()) {
      // With inherit, so that the token also acts on the owner's own account (R-AUTH-024).
      request.scope = { inherit: true, admin: true };
    } else {
      request.scope = { inherit: true };
    }
    if (this.endpoints().length > 0) request.scope = { ...request.scope, endpoints: [...this.endpoints()] };
    this.submitted.emit(request);
  }
}

function lines(text: string): string[] {
  return text
    .split('\n')
    .map((l) => l.trim())
    .filter((l) => l !== '');
}
