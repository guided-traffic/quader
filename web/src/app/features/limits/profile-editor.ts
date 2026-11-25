import { ChangeDetectionStrategy, Component, LOCALE_ID, computed, effect, inject, input, signal, untracked } from '@angular/core';
import { Router } from '@angular/router';
import { newIdempotencyKey } from '../../api/client';
import { LimitProfile, LimitProfileWrite, LimitsApi } from '../../api/limits-api';
import { ApiError } from '../../api/problem';
import { Versioned } from '../../api/repositories-api';
import { Notifier } from '../../core/notifier';
import { SessionStore } from '../../core/session-store';
import { TextField } from '../../shared/access/fields';
import { limitConflictFields } from './figures';
import { ConflictResolver } from '../../shared/conflict-dialog';
import { ErrorNotice } from '../../shared/error-notice';
import { fieldErrorAt } from '../../shared/field-errors';
import { asApiError } from '../../shared/paged-resource';
import { PageHeader } from '../../shared/ui/page-header';
import { FigureFields, FigureTexts, readTexts, textsOf } from './figure-fields';

/** A profile's key (R-LIM-010). */
export const PROFILE_KEY_PATTERN = /^[a-z][a-z0-9-]{1,31}$/;

/** The key a name suggests: lower case, a hyphen for every run of other characters, starting with a letter, at most 32. */
export function keyFromName(name: string): string {
  const slug = name
    .normalize('NFKD')
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^[^a-z]+/, '')
    .replace(/-+$/, '');
  return slug.slice(0, 32).replace(/-+$/, '');
}

/**
 * The profile editor (R-UI-178): a page of one column for a new profile and for one profile. The name, the key (on create only,
 * filled from the name until it is typed), the description, and the eleven figures in their groups with the value in each field;
 * an empty field is unlimited. Unlimited is shown read-only. **Save** sends `POST`, or `PUT` with `If-Match`; a 412 opens the
 * conflict dialog. A holder of `limits:read` alone reads the page.
 */
@Component({
  selector: 'quader-limit-profile-editor',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [ErrorNotice, FigureFields, PageHeader, TextField],
  template: `
    @if (isNew()) {
      <quader-page-header heading="New profile" i18n-heading="@@limits.profile.new-title" />
    } @else {
      <quader-page-header [heading]="stored()?.value?.name ?? key() ?? ''" [name]="true">
        <span meta class="badge" i18n="@@limits.profile.badge">Download limit profile</span>
      </quader-page-header>
    }

    @if (loadFailure(); as e) {
      <quader-error-notice [error]="e" (retry)="load()" />
    } @else if (isNew() || stored()) {
      <form class="settings" (submit)="save($event)" novalidate>
        @if (failure(); as e) {
          <quader-error-notice [error]="e" />
        }
        <quader-text-field
          width="name"
          i18n-label="@@limits.profile.name"
          label="Name"
          name="name"
          [value]="name()"
          (valueChange)="setName($event)"
          [required]="true"
          [readonly]="readonly()"
          [error]="nameError()"
        />
        @if (isNew()) {
          <quader-text-field
            width="name"
            i18n-label="@@limits.profile.key"
            label="Key"
            name="key"
            [value]="keyText()"
            (valueChange)="setKey($event)"
            i18n-hint="@@limits.profile.key-hint"
            hint="Lower case letters, digits and hyphens. It cannot be changed."
            [error]="keyError()"
          />
        } @else {
          <quader-text-field width="name" i18n-label="@@limits.profile.key" label="Key" name="key" [value]="key() ?? ''" [readonly]="true" />
        }
        <quader-text-field
          width="name"
          type="textarea"
          i18n-label="@@limits.profile.description"
          label="Description (optional)"
          name="description"
          [(value)]="description"
          [readonly]="readonly()"
          [error]="fieldError('/description')"
        />

        @if (unlimited()) {
          <p class="hint" i18n="@@limits.profile.unlimited">Unlimited is always unlimited.</p>
        }
        <quader-figure-fields [(texts)]="texts" [readonly]="readonly() || unlimited()" [errors]="figureErrors()" [cacheNote]="true" />

        @if (!readonly() && !unlimited()) {
          <button class="btn btn-confirm" type="submit" [disabled]="busy() || (!isNew() && !dirty())">
            @if (isNew()) {
              <ng-container i18n="@@limits.profile.create">Create profile</ng-container>
            } @else {
              <ng-container i18n="@@limits.save-changes">Save changes</ng-container>
            }
          </button>
        }
      </form>
    } @else {
      <p role="status" i18n="@@limits.loading">Loading.</p>
    }
  `,
  styles: `
    .settings {
      max-inline-size: var(--quader-settings-measure);
      padding-block-end: var(--quader-space-6);
    }
    .hint {
      margin: 0 0 var(--quader-space-3);
    }
  `,
})
export class LimitProfileEditor {
  private readonly api = inject(LimitsApi);
  private readonly session = inject(SessionStore);
  private readonly router = inject(Router);
  private readonly notifier = inject(Notifier);
  private readonly conflicts = inject(ConflictResolver);
  private readonly locale = inject(LOCALE_ID);

  /** The route's `:key`; absent on `/admin/limits/profiles/new`. */
  readonly key = input<string | undefined>(undefined);

  protected readonly isNew = computed(() => !this.key());
  protected readonly stored = signal<Versioned<LimitProfile> | null>(null);
  protected readonly name = signal('');
  protected readonly keyText = signal('');
  private readonly keyTyped = signal(false);
  protected readonly description = signal('');
  protected readonly texts = signal<FigureTexts>(textsOf(null));
  protected readonly attempted = signal(false);
  protected readonly busy = signal(false);
  protected readonly failure = signal<ApiError | null>(null);
  protected readonly loadFailure = signal<ApiError | null>(null);
  private readonly idempotencyKey = newIdempotencyKey();

  protected readonly readonly = computed(() => !this.session.has('limits:write'));
  /** Unlimited's figures stay empty (R-LIM-010). */
  protected readonly unlimited = computed(() => this.stored()?.value.key === 'unlimited');

  protected readonly figureErrors = computed(() => (this.attempted() || this.dirtyFigures() ? readTexts(this.texts()).errors : {}));
  private readonly dirtyFigures = computed(() => {
    const s = this.stored();
    return JSON.stringify(this.texts()) !== JSON.stringify(textsOf(s?.value.figures ?? null));
  });
  protected readonly dirty = computed(() => {
    const s = this.stored()?.value;
    if (!s) return true;
    return this.name().trim() !== s.name || this.description().trim() !== s.description || this.dirtyFigures();
  });

  constructor() {
    effect(() => {
      // A new profile has nothing to read and no failure of one.
      if (!this.key()) untracked(() => this.loadFailure.set(null));
      untracked(() => void this.load());
    });
  }

  protected async load(): Promise<void> {
    const key = this.key();
    if (!key) return;
    try {
      const p = await this.api.profile(key);
      // The failure stays until the answer, so the notice stays in place while it reads again (R-UI-216).
      this.loadFailure.set(null);
      this.show(p);
    } catch (e) {
      this.loadFailure.set(asApiError(e));
    }
  }

  private show(p: Versioned<LimitProfile>): void {
    this.stored.set(p);
    this.name.set(p.value.name);
    this.description.set(p.value.description);
    this.texts.set(textsOf(p.value.figures));
  }

  protected setName(value: string): void {
    this.name.set(value);
    if (this.isNew() && !this.keyTyped()) this.keyText.set(keyFromName(value));
  }

  protected setKey(value: string): void {
    this.keyTyped.set(true);
    this.keyText.set(value);
  }

  protected fieldError(pointer: string): string | null {
    return fieldErrorAt(this.failure(), pointer);
  }

  protected nameError(): string | null {
    const server = this.fieldError('/name');
    if (server) return server;
    const n = this.name().trim().length;
    return this.attempted() && (n < 1 || n > 64) ? $localize`:@@limits.profile.name-invalid:A name of 1 to 64 characters.` : null;
  }

  protected keyError(): string | null {
    const server = this.fieldError('/key');
    if (server) return server;
    return this.attempted() && !PROFILE_KEY_PATTERN.test(this.keyText())
      ? $localize`:@@limits.profile.key-invalid:2 to 32 lower case letters, digits and hyphens, starting with a letter.`
      : null;
  }

  protected async save(event: Event): Promise<void> {
    event.preventDefault();
    this.attempted.set(true);
    const read = readTexts(this.texts());
    if (this.nameError() || (this.isNew() && this.keyError()) || Object.keys(read.errors).length > 0) return;
    const body: LimitProfileWrite = { name: this.name().trim(), description: this.description().trim(), figures: read.figures };
    this.busy.set(true);
    this.failure.set(null);
    try {
      if (this.isNew()) {
        await this.api.createProfile({ key: this.keyText(), ...body }, this.idempotencyKey);
        this.notifier.status($localize`:@@limits.profile.created:Profile created.`);
        await this.router.navigateByUrl('/admin/limits/profiles');
        return;
      }
      const stored = this.stored()!;
      const result = await this.conflicts.save<LimitProfile>({
        etag: stored.etag,
        patch: { name: body.name, description: body.description, figures: body.figures },
        fields: limitConflictFields(this.locale),
        send: (etag) => this.api.replaceProfile(stored.value.key, body, etag),
        reload: () => this.api.profile(stored.value.key),
      });
      if (result.outcome === 'dismissed') return;
      this.show(result.current);
      this.attempted.set(false);
      if (result.outcome === 'saved') this.notifier.status($localize`:@@limits.profile.saved:Profile saved.`);
    } catch (e) {
      this.failure.set(asApiError(e));
    } finally {
      this.busy.set(false);
    }
  }
}
