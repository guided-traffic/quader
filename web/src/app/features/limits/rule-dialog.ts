import { ChangeDetectionStrategy, Component, LOCALE_ID, computed, inject, signal } from '@angular/core';
import { newIdempotencyKey } from '../../api/client';
import {
  CallerKind,
  Figure,
  FigureOverrides,
  LimitPlace,
  LimitProfile,
  LimitRule,
  LimitRuleWrite,
  LimitWho,
  LimitsApi,
} from '../../api/limits-api';
import { ApiError } from '../../api/problem';
import { Versioned } from '../../api/repositories-api';
import { SelectField, SelectOption, TextField } from '../../shared/access/fields';
import { ConflictResolver } from '../../shared/conflict-dialog';
import { ErrorNotice } from '../../shared/error-notice';
import { fieldErrorAt } from '../../shared/field-errors';
import { asApiError } from '../../shared/paged-resource';
import { DIALOG_DATA, DialogClose, DialogRef, DialogTitle } from '../../shared/ui/dialog';
import { Disclosure } from '../../shared/ui/disclosure';
import { CI_PROJECT_PREFIX, PrincipalChoice, PrincipalField, PrincipalKind, TECHNICAL_TOKEN_PREFIX } from '../../shared/ui/principal-field';
import { CallerCell } from './caller-cell';
import { everyLabel, whoView } from './callers';
import { FigureFields, FigureTexts, readTexts, textsOf } from './figure-fields';
import { FIGURES, compactFigures, limitText } from './figures';

/** What "Applies to" offers (R-UI-179). */
export type WhoChoice = 'user' | 'group' | 'technicalToken' | 'ciProject' | 'address' | 'every';

export interface RuleDialogData {
  readonly place: LimitPlace;
  /** The rule to change; absent for **Add rule**. */
  readonly rule?: LimitRule;
  /** The "who", fixed: **Set limit** of a technical token (R-UI-181). */
  readonly who?: LimitWho;
  /** The caller writes without `limits:write`: its rule is capped (R-LIM-016). */
  readonly capped: boolean;
  /** A technical token's own rule (R-UI-181, Q-UI-759): the dialog is **Change limit** and offers **Remove limit**, which it answers
   *  with `'remove'` for the caller to confirm and do. */
  readonly tokenRule?: boolean;
  /** The project's maximum's profile name, for the line that names the cap. */
  readonly maximum?: string | null;
}

/** The project whose lookup the principal field searches: the place's project, none at the instance. */
export function placeProject(place: LimitPlace): string | null {
  if (place.kind === 'project') return place.project;
  if (place.kind === 'repository') return place.repository.split('/')[0] ?? null;
  return null;
}

/** The overrides a rule writes: the changed figures of the fields, each against the profile's value. */
export function overridesOf(
  figures: Readonly<Record<Figure, number | null>>,
  changed: ReadonlySet<Figure>,
  base: Readonly<Record<Figure, number | null>>,
): FigureOverrides {
  const out: Partial<Record<Figure, number | null>> = {};
  for (const f of FIGURES) if (changed.has(f) && figures[f] !== base[f]) out[f] = figures[f];
  return out;
}

/** An IPv4 address or prefix, or an IPv6 address or prefix; the server makes it canonical (R-LIM-032). */
export const ADDRESS_PATTERN = /^(?:(?:\d{1,3}\.){3}\d{1,3}(?:\/\d{1,2})?|[0-9a-fA-F:]*:[0-9a-fA-F:.]*(?:\/\d{1,3})?)$/;

/**
 * The rule dialog of R-UI-179 (**Add rule**, **Change**): who it applies to (a person, a group, a technical token or a CI project
 * in the shared principal field, an address or range, or every caller of a kind), the profile with its figures under it, the single
 * figures behind a closed fold, and an optional note. At a project or a repository a caller without `limits:write` sees the cap,
 * and a `422 limit_above_maximum` marks each field with its cap. A change sends `PUT` with the rule's `ETag`; a 412 opens the
 * conflict dialog.
 */
@Component({
  selector: 'quader-limit-rule-dialog',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [CallerCell, DialogClose, DialogTitle, Disclosure, ErrorNotice, FigureFields, PrincipalField, SelectField, TextField],
  template: `
    <h2 quaderDialogTitle>{{ title }}</h2>
    <form (submit)="submit($event)" novalidate>
      <div class="dialog-body">
        @if (failure(); as e) {
          <quader-error-notice [error]="e" />
        }
        @if (fixedWho(); as who) {
          <div class="field">
            <span class="field-label" i18n="@@limits.rule.applies-to">Applies to</span>
            <quader-caller-cell [view]="whoView(who)" />
          </div>
        } @else {
          <quader-select-field
            i18n-label="@@limits.rule.applies-to"
            label="Applies to"
            name="whoKind"
            [options]="whoOptions"
            [value]="whoKind()"
            (valueChange)="chooseKind($any($event))"
          />
          @switch (whoKind()) {
            @case ('address') {
              <quader-text-field
                width="name"
                i18n-label="@@limits.rule.address"
                label="Address or range"
                name="address"
                [(value)]="address"
                i18n-hint="@@limits.rule.address-hint"
                hint="An address, or a range such as 198.51.100.0/24 or 2001:db8:1::/48"
                [error]="addressError()"
              />
            }
            @case ('every') {
              <quader-select-field
                i18n-label="@@limits.rule.kind"
                label="Kind of caller"
                name="callerKind"
                [options]="kindOptions()"
                [(value)]="callerKind"
              />
            }
            @default {
              <quader-principal-field
                [label]="principalLabel()"
                name="who"
                [kinds]="principalKinds()"
                [project]="project"
                [(value)]="principal"
                (picked)="picked.set($event)"
                [hint]="principalHint()"
                [error]="principalError()"
              />
            }
          }
        }

        <quader-select-field
          i18n-label="@@limits.rule.profile"
          label="Profile"
          name="profile"
          [options]="profileOptions()"
          [loading]="profiles() === null ? loadingProfiles : null"
          [value]="profileKey()"
          (valueChange)="chooseProfile($event)"
          [error]="fieldError('/profile')"
        />
        @if (profile(); as p) {
          <p class="figures muted">{{ compact(p) }}</p>
        }
        @if (data.capped && data.place.kind !== 'instance') {
          <p class="cap hint">
            @if (data.maximum) {
              <ng-container i18n="@@limits.rule.cap-maximum"
                >You can set up to the instance's values for this caller, or up to the project's maximum ({{ data.maximum }}).</ng-container
              >
            } @else {
              <ng-container i18n="@@limits.rule.cap">You can set up to the instance's values for this caller.</ng-container>
            }
          </p>
        }

        <quader-disclosure
          [inline]="true"
          i18n-heading="@@limits.rule.single"
          heading="Change single figures"
          [note]="overriddenNote()"
          [(expanded)]="singleOpen"
        >
          @if (profile(); as p) {
            <quader-figure-fields [(texts)]="texts" [base]="p.figures" [(overridden)]="overridden" [errors]="figureErrors()" />
          }
        </quader-disclosure>

        <quader-text-field
          width="full"
          i18n-label="@@limits.rule.note"
          label="Note (optional)"
          name="note"
          [(value)]="note"
          [error]="fieldError('/note')"
        />
      </div>
      <div class="dialog-actions">
        @if (data.tokenRule && data.rule) {
          <button class="btn btn-tertiary remove" type="button" (click)="remove()" i18n="@@limits.token.remove-confirm">Remove limit</button>
        }
        <button class="btn btn-tertiary" type="button" quaderDialogClose i18n="@@limits.cancel">Cancel</button>
        <button class="btn btn-confirm" type="submit" [disabled]="busy() || !profileKey()">{{ confirmLabel }}</button>
      </div>
    </form>
  `,
  styles: `
    /* Remove limit stands apart from the dialog's answer, at the row's start. */
    .remove {
      margin-inline-end: auto;
    }
    quader-error-notice {
      display: block;
      margin-block-end: var(--quader-space-3);
    }
    .field {
      display: flex;
      flex-direction: column;
      gap: var(--quader-space-1);
      margin-block-end: var(--quader-space-3);
    }
    .figures {
      margin: calc(-1 * var(--quader-space-2)) 0 var(--quader-space-3);
      font: var(--quader-type-body-small);
    }
    .muted {
      color: var(--quader-text-muted);
    }
    .cap {
      margin: 0 0 var(--quader-space-3);
    }
    quader-disclosure {
      margin-block-end: var(--quader-space-3);
    }
  `,
})
export class LimitRuleDialog {
  protected readonly data = inject<RuleDialogData>(DIALOG_DATA);
  private readonly ref = inject<DialogRef<Versioned<LimitRule> | 'deleted' | 'remove'>>(DialogRef);
  private readonly api = inject(LimitsApi);
  private readonly conflicts = inject(ConflictResolver);
  private readonly locale = inject(LOCALE_ID);

  protected readonly project = placeProject(this.data.place);
  protected readonly title = this.data.rule
    ? this.data.tokenRule
      ? $localize`:@@limits.rule.change-limit-title:Change limit`
      : $localize`:@@limits.rule.change-title:Change rule`
    : this.data.who
      ? $localize`:@@limits.rule.set-title:Set limit`
      : $localize`:@@limits.rule.add-title:Add rule`;
  protected readonly confirmLabel = this.data.rule
    ? $localize`:@@limits.rule.save:Save rule`
    : this.data.who
      ? $localize`:@@limits.rule.set:Set limit`
      : $localize`:@@limits.rule.add:Add rule`;
  protected readonly loadingProfiles = $localize`:@@limits.rule.loading-profiles:Loading profiles…`;
  protected readonly whoView = whoView;

  protected readonly whoOptions: readonly SelectOption[] = [
    { value: 'user', label: $localize`:@@limits.rule.who-user:A user` },
    { value: 'group', label: $localize`:@@limits.rule.who-group:A group` },
    { value: 'technicalToken', label: $localize`:@@limits.rule.who-token:A technical token` },
    { value: 'ciProject', label: $localize`:@@limits.rule.who-ci:A CI project` },
    { value: 'address', label: $localize`:@@limits.rule.who-address:An address or range` },
    { value: 'every', label: $localize`:@@limits.rule.who-every:Every caller of a kind` },
  ];

  protected readonly fixedWho = signal<LimitWho | null>(this.data.rule?.who ?? this.data.who ?? null);
  protected readonly whoKind = signal<WhoChoice>('user');
  protected readonly principal = signal<readonly string[]>([]);
  protected readonly picked = signal<PrincipalChoice | null>(null);
  protected readonly address = signal('');
  protected readonly callerKind = signal<string>('user');
  protected readonly note = signal(this.data.rule?.note ?? '');

  protected readonly profiles = signal<readonly LimitProfile[] | null>(null);
  /** A default is a value in the field: a technical token's first limit starts from the profile its kind gets (R-LIM-038). */
  protected readonly profileKey = signal(this.data.rule?.profile ?? (this.data.who?.kind === 'technicalToken' ? 'technical' : ''));
  protected readonly profile = computed(() => this.profiles()?.find((p) => p.key === this.profileKey()) ?? null);
  protected readonly texts = signal<FigureTexts>(textsOf(null));
  protected readonly overridden = signal<ReadonlySet<Figure>>(new Set());
  protected readonly singleOpen = signal(Object.keys(this.data.rule?.overrides ?? {}).length > 0);

  protected readonly busy = signal(false);
  protected readonly attempted = signal(false);
  protected readonly failure = signal<ApiError | null>(null);
  /** The caps a `422 limit_above_maximum` named, per figure. */
  private readonly caps = signal<Partial<Record<Figure, string>>>({});

  protected readonly profileOptions = computed<readonly SelectOption[]>(() => [
    ...(this.profileKey() ? [] : [{ value: '', label: $localize`:@@limits.rule.choose-profile:Choose a profile` }]),
    ...(this.profiles() ?? []).map((p) => ({ value: p.key, label: p.name })),
  ]);

  protected readonly kindOptions = computed<readonly SelectOption[]>(() => {
    const kinds: CallerKind[] = ['user', 'technicalToken', 'ci', 'anonymous'];
    // Administrators are decided at the instance alone (R-LIM-012): a project or a repository cannot name them.
    if (this.data.place.kind === 'instance') kinds.push('administrator');
    return kinds.map((k) => ({ value: k, label: everyLabel(k) }));
  });

  protected readonly overriddenNote = computed(() => {
    const n = this.overridden().size;
    return n === 0 ? '' : n === 1 ? $localize`:@@limits.rule.one-changed:1 changed` : $localize`:@@limits.rule.n-changed:${n}:count: changed`;
  });

  protected readonly figureErrors = computed<Partial<Record<Figure, string>>>(() => {
    const read = readTexts(this.texts());
    const shown: Partial<Record<Figure, string>> = {};
    for (const f of FIGURES) {
      const own = this.overridden().has(f) ? read.errors[f] : undefined;
      const cap = this.caps()[f];
      if (own) shown[f] = own;
      else if (cap) shown[f] = cap;
    }
    return shown;
  });

  constructor() {
    void this.loadProfiles();
  }

  private async loadProfiles(): Promise<void> {
    try {
      const list = await this.api.profiles();
      this.profiles.set(list);
      const rule = this.data.rule;
      const start = list.find((p) => p.key === this.profileKey());
      if (start) {
        // A rule's fields are the profile's with its overrides; the overridden figures are the changed ones.
        this.texts.set(textsOf({ ...start.figures, ...(rule?.overrides ?? {}) }));
        this.overridden.set(new Set(FIGURES.filter((f) => rule && f in rule.overrides)));
      }
    } catch (e) {
      // The select stops saying that it loads; the notice says why it is empty.
      this.profiles.set([]);
      this.failure.set(asApiError(e));
    }
  }

  protected chooseKind(kind: WhoChoice): void {
    this.whoKind.set(kind);
    this.principal.set([]);
    this.picked.set(null);
  }

  /** A new profile keeps the figures the person changed and takes the profile's values for the rest. */
  protected chooseProfile(key: string): void {
    this.profileKey.set(key);
    const p = this.profiles()?.find((x) => x.key === key);
    if (!p) return;
    const texts = { ...textsOf(p.figures) } as Record<Figure, string>;
    for (const f of this.overridden()) texts[f] = this.texts()[f];
    this.texts.set(texts as FigureTexts);
    this.caps.set({});
  }

  protected compact(p: LimitProfile): string {
    return compactFigures(p.figures, this.locale);
  }

  /** **Remove limit** of a token's rule: the caller confirms and deletes (TechnicalTokenLimits.remove). */
  protected remove(): void {
    this.ref.close('remove');
  }

  protected principalKinds(): readonly PrincipalKind[] {
    switch (this.whoKind()) {
      case 'group':
        return ['group'];
      case 'technicalToken':
        return ['technical_token'];
      case 'ciProject':
        return ['ci_project'];
      default:
        return ['user'];
    }
  }

  protected principalLabel(): string {
    switch (this.whoKind()) {
      case 'group':
        return $localize`:@@limits.rule.group:Group`;
      case 'technicalToken':
        return $localize`:@@limits.rule.token:Technical token`;
      case 'ciProject':
        return $localize`:@@limits.rule.ci:CI project`;
      default:
        return $localize`:@@limits.rule.user:User`;
    }
  }

  protected principalHint(): string {
    switch (this.whoKind()) {
      case 'technicalToken':
        return $localize`:@@limits.rule.token-hint:Type the token's name or its account.`;
      case 'ciProject':
        return $localize`:@@limits.rule.ci-hint:Type the project's path.`;
      case 'group':
        return $localize`:@@limits.rule.group-hint:Type the group's name.`;
      default:
        return $localize`:@@limits.rule.user-hint:Type a name or a username.`;
    }
  }

  protected principalError(): string | null {
    const server = this.fieldError('/who');
    if (server) return server;
    return this.attempted() && this.principal().length === 0
      ? $localize`:@@limits.rule.who-required:Choose who the rule applies to.`
      : null;
  }

  protected addressError(): string | null {
    const server = this.fieldError('/who/prefix') ?? this.fieldError('/who');
    if (server) return server;
    if (!this.attempted()) return null;
    const a = this.address().trim();
    if (a === '') return $localize`:@@limits.rule.address-required:Type an address or a range.`;
    return ADDRESS_PATTERN.test(a) ? null : $localize`:@@limits.rule.address-invalid:An IPv4 or IPv6 address, optionally with a prefix length such as /24.`;
  }

  protected fieldError(pointer: string): string | null {
    return fieldErrorAt(this.failure(), pointer);
  }

  /** The "who" of a new rule from the controls, or null where it is not complete. */
  private who(): LimitWho | null {
    const fixed = this.fixedWho();
    if (fixed) return fixed;
    switch (this.whoKind()) {
      case 'address': {
        const prefix = this.address().trim();
        return prefix && ADDRESS_PATTERN.test(prefix) ? { kind: 'address', prefix } : null;
      }
      case 'every':
        return { kind: 'every', callerKind: this.callerKind() as CallerKind };
      default: {
        const ref = this.principal()[0];
        if (!ref) return null;
        const at = ref.indexOf(':');
        const prefix = ref.slice(0, at);
        const name = ref.slice(at + 1);
        if (prefix === 'user') return { kind: 'user', user: name };
        if (prefix === 'group') return { kind: 'group', group: name };
        if (prefix === TECHNICAL_TOKEN_PREFIX) return { kind: 'technicalToken', technicalToken: name };
        if (prefix === CI_PROJECT_PREFIX) {
          const cut = name.lastIndexOf(':');
          return { kind: 'ciProject', issuer: name.slice(0, cut), projectId: name.slice(cut + 1) };
        }
        return null;
      }
    }
  }

  protected async submit(event: Event): Promise<void> {
    event.preventDefault();
    this.attempted.set(true);
    const profile = this.profile();
    const who = this.who();
    const read = readTexts(this.texts());
    const ownErrors = FIGURES.some((f) => this.overridden().has(f) && read.errors[f]);
    if (!profile || !who || ownErrors) {
      if (ownErrors) this.singleOpen.set(true);
      return;
    }
    const body: LimitRuleWrite = {
      profile: profile.key,
      overrides: overridesOf(read.figures, this.overridden(), profile.figures),
      note: this.note().trim(),
    };
    this.busy.set(true);
    this.failure.set(null);
    this.caps.set({});
    try {
      const rule = this.data.rule;
      if (!rule) {
        this.ref.close(await this.api.createRule(this.data.place, { ...body, who }, newIdempotencyKey()));
        return;
      }
      const stored = await this.api.rule(this.data.place, rule.id);
      const result = await this.conflicts.save<LimitRule>({
        etag: stored.etag,
        patch: { ...body } as Record<string, unknown>,
        send: (etag) => this.api.replaceRule(this.data.place, rule.id, body, etag),
        reload: () => this.api.rule(this.data.place, rule.id),
      });
      if (result.outcome !== 'dismissed') this.ref.close(result.current);
    } catch (e) {
      const failure = asApiError(e);
      this.failure.set(failure);
      if (failure.code === 'limit_above_maximum') this.showCaps(failure);
    } finally {
      this.busy.set(false);
    }
  }

  /** "At most 30 GB per hour here" beside each figure the server named (R-UI-179). */
  private showCaps(failure: ApiError): void {
    const caps: Partial<Record<Figure, string>> = {};
    for (const problem of failure.problem?.errors ?? []) {
      const figure = problem.pointer.split('/').pop() as Figure;
      if (!FIGURES.includes(figure)) continue;
      const cap = (problem as unknown as { cap?: number | null }).cap;
      caps[figure] =
        cap === undefined
          ? (problem.detail ?? $localize`:@@limits.rule.above-cap:Above what you may set here.`)
          : $localize`:@@limits.rule.at-most:At most ${limitText(figure, cap, this.locale)}:limit: here.`;
    }
    this.caps.set(caps);
    // The fields that carry a cap are the profile's too: they are marked where the person can change them.
    const next = new Set(this.overridden());
    for (const f of Object.keys(caps) as Figure[]) next.add(f);
    this.overridden.set(next);
    this.singleOpen.set(true);
  }
}
