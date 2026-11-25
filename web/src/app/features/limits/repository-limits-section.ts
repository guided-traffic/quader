import { ChangeDetectionStrategy, Component, computed, effect, inject, input, signal, untracked, viewChild } from '@angular/core';
import { RouterLink } from '@angular/router';
import { CeilingFigures, LimitCeilings, LimitPlace, LimitsApi } from '../../api/limits-api';
import { ApiError } from '../../api/problem';
import { Versioned } from '../../api/repositories-api';
import { Notifier } from '../../core/notifier';
import { SessionStore } from '../../core/session-store';
import { ConflictResolver } from '../../shared/conflict-dialog';
import { ErrorNotice } from '../../shared/error-notice';
import { asApiError } from '../../shared/paged-resource';
import { Disclosure } from '../../shared/ui/disclosure';
import { fromField, toField } from './figures';
import { LimitRules } from './limit-rules';
import { rulesNote } from './project-limits-section';

type CeilingClass = 'anonymous' | 'signedIn';

interface CeilingTexts {
  readonly gb: string;
  readonly requests: string;
}

function textsOf(c: CeilingFigures | undefined): CeilingTexts {
  return { gb: toField('uncachedBytesPerHour', c?.uncachedBytesPerHour ?? null), requests: toField('upstreamRequestsPerHour', c?.upstreamRequestsPerHour ?? null) };
}

/**
 * The repository settings' section **Download limits** (R-UI-180, Q-UI-731): closed, its title line the number of rules. Open,
 * the rule table of the repository with **Add rule** and, for a remote repository, the group **Ceilings**: all anonymous and all
 * signed-in callers together, each with the upstream's gigabytes and requests per hour and a save of its own (R-LIM-015).
 */
@Component({
  selector: 'quader-repository-limits',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [Disclosure, ErrorNotice, LimitRules, RouterLink],
  template: `
    <quader-disclosure
      i18n-heading="@@limits.section.heading"
      heading="Download limits"
      i18n-hint="@@limits.section.repository-hint"
      hint="How much each caller may download from this repository."
      [note]="note()"
    >
      @if (canWrite()) {
        <div class="head">
          <button class="btn btn-default" type="button" (click)="rules()?.add()" i18n="@@limits.rules.add">Add rule</button>
        </div>
      }
      <quader-limit-rules [place]="place()" [canWrite]="canWrite()" [capped]="capped()" (counted)="count.set($event)" />

      @if (remote()) {
        <section class="ceilings" aria-labelledby="limits-ceilings">
          <h3 id="limits-ceilings" i18n="@@limits.ceilings.heading">Ceilings</h3>
          <p class="hint" i18n="@@limits.ceilings.hint">All callers of one kind together; a request must fit both its caller's limit and the ceiling.</p>
          @if (ceilingsFailure(); as e) {
            <quader-error-notice [error]="e" (retry)="loadCeilings()" />
          }
          @for (c of stored() ? classes : []; track c) {
            <form class="ceiling" (submit)="saveCeiling(c, $event)" novalidate>
              <h4 [id]="'ceiling-' + c">{{ classLabel(c) }}</h4>
              <div class="fields">
                <div class="field">
                  <label class="field-label" [for]="'ceiling-' + c + '-gb'" i18n="@@limits.ceilings.gb">GB per hour from the upstream</label>
                  <input
                    class="text-input field-w-duration"
                    type="text"
                    inputmode="decimal"
                    [id]="'ceiling-' + c + '-gb'"
                    [attr.aria-describedby]="'ceiling-' + c"
                    placeholder="Unlimited"
                    i18n-placeholder="@@limits.unlimited"
                    [value]="texts()[c].gb"
                    [readonly]="!canWriteCeilings()"
                    [attr.aria-invalid]="errorOf(c, 'gb') ? 'true' : null"
                    (input)="set(c, 'gb', $any($event.target).value)"
                  />
                  @if (errorOf(c, 'gb'); as e) {
                    <p class="field-error">{{ e }}</p>
                  }
                </div>
                <div class="field">
                  <label class="field-label" [for]="'ceiling-' + c + '-requests'" i18n="@@limits.ceilings.requests">Upstream requests per hour</label>
                  <input
                    class="text-input field-w-duration"
                    type="text"
                    inputmode="numeric"
                    [id]="'ceiling-' + c + '-requests'"
                    [attr.aria-describedby]="'ceiling-' + c"
                    placeholder="Unlimited"
                    i18n-placeholder="@@limits.unlimited"
                    [value]="texts()[c].requests"
                    [readonly]="!canWriteCeilings()"
                    [attr.aria-invalid]="errorOf(c, 'requests') ? 'true' : null"
                    (input)="set(c, 'requests', $any($event.target).value)"
                  />
                  @if (errorOf(c, 'requests'); as e) {
                    <p class="field-error">{{ e }}</p>
                  }
                </div>
              </div>
              @if (canWriteCeilings()) {
                <button class="btn btn-confirm" type="submit" [disabled]="busy() || !dirty(c)">
                  <ng-container i18n="@@limits.save-changes">Save changes</ng-container>
                  <span class="visually-hidden"> ({{ classLabel(c) }})</span>
                </button>
              }
            </form>
          }
        </section>
      }
      @if (showUsage()) {
        <p class="usage">
          <a [routerLink]="['/projects', project(), 'usage']" [queryParams]="{ repository: repository() }" i18n="@@limits.section.repository-usage"
            >Usage of this repository</a
          >
        </p>
      }
    </quader-disclosure>
  `,
  styles: `
    .head {
      display: flex;
      justify-content: flex-end;
      margin-block-end: var(--quader-space-2);
    }
    .ceilings {
      margin-block-start: var(--quader-space-5);
    }
    h3 {
      margin: 0 0 var(--quader-space-1);
      font-size: var(--quader-text-size);
      font-weight: 600;
    }
    h4 {
      margin: var(--quader-space-3) 0 var(--quader-space-2);
      font-size: var(--quader-text-size);
      font-weight: 600;
    }
    .hint {
      margin: 0 0 var(--quader-space-2);
    }
    .fields {
      display: flex;
      flex-wrap: wrap;
      gap: 0 var(--quader-space-5);
    }
    .field {
      display: flex;
      flex-direction: column;
      gap: var(--quader-space-1);
      margin-block-end: var(--quader-space-3);
    }
    .field-error {
      margin: 0;
      color: var(--quader-status-bad);
      font: var(--quader-type-body-small);
    }
    .usage {
      margin: var(--quader-space-4) 0 0;
    }
  `,
})
export class RepositoryLimitsSection {
  private readonly api = inject(LimitsApi);
  private readonly session = inject(SessionStore);
  private readonly notifier = inject(Notifier);
  private readonly conflicts = inject(ConflictResolver);

  readonly project = input.required<string>();
  /** The repository's key within the project. */
  readonly repository = input.required<string>();
  /** A remote repository has ceilings (R-LIM-015). */
  readonly remote = input(false);

  protected readonly classes: readonly CeilingClass[] = ['anonymous', 'signedIn'];
  protected readonly rules = viewChild(LimitRules);
  protected readonly ref = computed(() => `${this.project()}/${this.repository()}`);
  protected readonly place = computed<LimitPlace>(() => ({ kind: 'repository', repository: this.ref() }));
  protected readonly count = signal<number | null>(null);
  protected readonly note = computed(() => rulesNote(this.count(), null));
  protected readonly canWrite = computed(() => this.session.has('limits:write') || this.session.isOwnerOf(this.project()));
  protected readonly capped = computed(() => !this.session.has('limits:write'));
  protected readonly canWriteCeilings = computed(
    () => this.session.has('limits:write') || this.session.has('repositories:write-config') || this.session.isOwnerOf(this.project()),
  );
  protected readonly showUsage = computed(() => this.session.has('usage:read') || this.session.isOwnerOf(this.project()));

  /** The ceilings as stored; the forms show once they are read. */
  protected readonly stored = signal<Versioned<LimitCeilings> | null>(null);
  protected readonly texts = signal<Record<CeilingClass, CeilingTexts>>({ anonymous: textsOf(undefined), signedIn: textsOf(undefined) });
  private readonly attempted = signal<ReadonlySet<CeilingClass>>(new Set());
  protected readonly busy = signal(false);
  protected readonly ceilingsFailure = signal<ApiError | null>(null);

  constructor() {
    effect(() => {
      if (!this.remote()) return;
      this.ref();
      untracked(() => void this.loadCeilings());
    });
  }

  protected classLabel(c: CeilingClass): string {
    return c === 'anonymous'
      ? $localize`:@@limits.ceilings.anonymous:All anonymous callers`
      : $localize`:@@limits.ceilings.signed-in:All signed-in callers`;
  }

  async loadCeilings(): Promise<void> {
    try {
      this.show(await this.api.ceilings(this.ref()));
      this.ceilingsFailure.set(null);
    } catch (e) {
      this.ceilingsFailure.set(asApiError(e));
    }
  }

  private show(c: Versioned<LimitCeilings>): void {
    this.stored.set(c);
    this.texts.set({ anonymous: textsOf(c.value.anonymous), signedIn: textsOf(c.value.signedIn) });
  }

  protected set(c: CeilingClass, field: keyof CeilingTexts, value: string): void {
    this.texts.update((t) => ({ ...t, [c]: { ...t[c], [field]: value } }));
  }

  protected dirty(c: CeilingClass): boolean {
    const stored = textsOf(this.stored()?.value[c]);
    const now = this.texts()[c];
    return stored.gb !== now.gb || stored.requests !== now.requests;
  }

  protected errorOf(c: CeilingClass, field: keyof CeilingTexts): string | null {
    if (!this.attempted().has(c) && !this.dirty(c)) return null;
    const read = fromField(field === 'gb' ? 'uncachedBytesPerHour' : 'upstreamRequestsPerHour', this.texts()[c][field]);
    return read.ok ? null : read.error;
  }

  private figuresOf(c: CeilingClass): CeilingFigures | null {
    const gb = fromField('uncachedBytesPerHour', this.texts()[c].gb);
    const req = fromField('upstreamRequestsPerHour', this.texts()[c].requests);
    return gb.ok && req.ok ? { uncachedBytesPerHour: gb.value, upstreamRequestsPerHour: req.value } : null;
  }

  /** Saves one class; the other goes with it as it is stored, since one `PUT` replaces both (R-LIM-032). */
  protected async saveCeiling(c: CeilingClass, event: Event): Promise<void> {
    event.preventDefault();
    this.attempted.update((s) => new Set([...s, c]));
    const stored = this.stored();
    const figures = this.figuresOf(c);
    if (!stored || !figures) return;
    const body = {
      anonymous: c === 'anonymous' ? figures : stored.value.anonymous,
      signedIn: c === 'signedIn' ? figures : stored.value.signedIn,
    };
    this.busy.set(true);
    this.ceilingsFailure.set(null);
    try {
      const result = await this.conflicts.save<LimitCeilings>({
        etag: stored.etag,
        patch: { [c]: figures },
        send: (etag, current) =>
          this.api.replaceCeilings(this.ref(), current ? { anonymous: current.anonymous, signedIn: current.signedIn, [c]: figures } : body, etag),
        reload: () => this.api.ceilings(this.ref()),
      });
      if (result.outcome === 'dismissed') return;
      const keep = this.texts();
      this.show(result.current);
      // The other class keeps what the person typed and has not saved.
      const other: CeilingClass = c === 'anonymous' ? 'signedIn' : 'anonymous';
      this.texts.update((t) => ({ ...t, [other]: keep[other] }));
      if (result.outcome === 'saved') this.notifier.status($localize`:@@limits.ceilings.saved:Ceilings saved.`);
    } catch (e) {
      this.ceilingsFailure.set(asApiError(e));
    } finally {
      this.busy.set(false);
    }
  }
}
