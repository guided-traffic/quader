import { Component, ElementRef, computed, effect, inject, input, output, signal, untracked } from '@angular/core';
import { toSignal } from '@angular/core/rxjs-interop';
import { ActivatedRoute } from '@angular/router';
import { ApiError } from '../../api/problem';
import { Schema } from '../../api/types';
import { ErrorNotice } from '../../shared/error-notice';
import { Checkbox } from '../../shared/ui/checkbox';
import { Disclosure } from '../../shared/ui/disclosure';
import { syncIntervalSentence } from './downloads/download-words';

/** `remote.syncs` of a remote repository (R-CUR-020, R-CUR-027). */
export type SyncSettings = Schema<'SyncSettings'>;

/** The values a remote without the member holds (R-CUR-020): every member's default. */
export const SYNC_DEFAULTS: Required<SyncSettings> = { intervalSeconds: 3600, paused: false, maxBytes: null };

/** The stored member with its defaults filled in. */
export function syncsOf(stored: Partial<SyncSettings> | undefined | null): Required<SyncSettings> {
  return { ...SYNC_DEFAULTS, ...(stored ?? {}) };
}

const MIN_MINUTES = 5;
const MAX_MINUTES = 43_200;
const GIB = 1024 ** 3;

/**
 * The closed section **Kept in sync** of a remote repository's settings (remote-curation.md R-CUR-018, R-CUR-020, R-CUR-027): details
 * on demand (R-UI-067). The interval of every sync of the repository in minutes, **Pause every sync** and the stored size at which
 * they start no run, saved on their own with the page's `ETag`; the address's fragment `#syncs` opens the section. The syncs themselves are set up and listed on the tab **Downloads**. A
 * caller who may not change the configuration reads the values.
 */
@Component({
  selector: 'quader-repository-syncs',
  imports: [Checkbox, Disclosure, ErrorNotice],
  template: `
    <quader-disclosure
      id="syncs"
      i18n-heading="@@syncs.settings.heading"
      heading="Kept in sync"
      i18n-hint="@@syncs.settings.hint"
      hint="How often the downloads kept in sync run, and when they stop."
      [note]="dirty() ? unsavedNote : ''"
      [(expanded)]="open"
    >
      <form (submit)="submit($event)" novalidate>
        @if (failure(); as e) {
          <quader-error-notice [error]="e" />
        }
        <fieldset class="plain syncs-fields" [disabled]="!canChange() || saving()">
          <div class="field">
            <label class="field-label" for="syncs-interval" i18n="@@syncs.settings.interval">Run every this many minutes</label>
            <input
              id="syncs-interval"
              class="text-input field-w-duration"
              type="number"
              inputmode="numeric"
              [min]="minMinutes"
              [max]="maxMinutes"
              [value]="minutes()"
              [attr.aria-invalid]="minutesError() ? 'true' : null"
              aria-describedby="syncs-interval-hint syncs-interval-error"
              (input)="minutes.set($any($event.target).value)"
            />
            <p class="hint" id="syncs-interval-hint">{{ intervalText() }}</p>
            @if (minutesError(); as e) {
              <p class="field-error" id="syncs-interval-error">{{ e }}</p>
            }
          </div>
          <quader-checkbox [checked]="paused()" [disabled]="!canChange() || saving()" (checkedChange)="paused.set($event)" describedBy="syncs-paused-hint">
            <ng-container i18n="@@syncs.settings.paused">Pause every sync of this repository</ng-container>
          </quader-checkbox>
          <p class="hint" id="syncs-paused-hint" i18n="@@syncs.settings.paused-hint">No sync starts a run; a run in progress goes on.</p>
          <div class="field">
            <label class="field-label" for="syncs-max" i18n="@@syncs.settings.max-bytes">Stop at this many GiB stored</label>
            <input
              id="syncs-max"
              class="text-input field-w-number"
              type="number"
              inputmode="decimal"
              min="0.001"
              step="any"
              placeholder="None"
              i18n-placeholder="@@syncs.settings.max-bytes-none"
              [value]="gib()"
              [attr.aria-invalid]="gibError() ? 'true' : null"
              aria-describedby="syncs-max-hint syncs-max-error"
              (input)="gib.set($any($event.target).value)"
            />
            <p class="hint" id="syncs-max-hint" i18n="@@syncs.settings.max-bytes-hint">Once the repository stores this much, its syncs start no run. Empty is no bound of their own.</p>
            @if (gibError(); as e) {
              <p class="field-error" id="syncs-max-error">{{ e }}</p>
            }
          </div>
        </fieldset>
        @if (canChange()) {
          <div class="actions">
            <button class="btn btn-confirm" type="submit" [disabled]="!dirty() || saving() || !!minutesError() || !!gibError()">
              <ng-container i18n="@@repo.settings.save">Save changes</ng-container>
              <span class="visually-hidden" i18n="@@syncs.settings.save-for"> (kept in sync)</span>
            </button>
            @if (dirty()) {
              <button class="btn btn-tertiary" type="button" (click)="reset()" i18n="@@repo.edit.reset">Discard changes</button>
            }
          </div>
        }
      </form>
    </quader-disclosure>
  `,
  styles: `
    .syncs-fields {
      display: flex;
      flex-direction: column;
      gap: var(--quader-space-1);
      margin: 0;
      padding: 0;
      border: 0;
    }
    .field {
      display: flex;
      flex-direction: column;
      gap: var(--quader-space-1);
      margin-block-end: var(--quader-space-3);
    }
    .hint {
      margin: 0;
    }
    #syncs-paused-hint {
      margin-block-end: var(--quader-space-3);
    }
    .field-error {
      margin: 0;
      color: var(--quader-status-bad);
      font: var(--quader-type-body-small);
    }
    .actions {
      display: flex;
      flex-wrap: wrap;
      gap: var(--quader-space-2);
      margin-block-start: var(--quader-space-4);
    }
  `,
})
export class RepositorySyncsSection {
  private readonly route = inject(ActivatedRoute);
  private readonly host = inject(ElementRef<HTMLElement>);
  private readonly fragment = toSignal(this.route.fragment, { initialValue: null });
  /** `remote.syncs` as stored; absent members hold their defaults. */
  readonly stored = input<Partial<SyncSettings> | undefined | null>(undefined);
  /** The caller may set the configuration: `repositories:write-config` or the project's Owner (R-CUR-005). */
  readonly canChange = input(false);
  readonly saving = input(false);
  readonly failure = input<ApiError | null>(null);
  readonly save = output<SyncSettings>();

  protected readonly minMinutes = MIN_MINUTES;
  protected readonly maxMinutes = MAX_MINUTES;
  protected readonly minutes = signal<string | number>(60);
  protected readonly paused = signal(false);
  /** The byte bound in GiB as typed; '' is no bound. */
  protected readonly gib = signal<string | number>('');
  protected readonly open = signal(false);
  protected readonly unsavedNote = $localize`:@@repo.settings.unsaved:Unsaved changes`;

  private readonly current = computed(() => syncsOf(this.stored()));
  /** The stored byte bound as the field shows it, in GiB with three places; '' for none. */
  private readonly storedGib = computed(() => {
    const b = this.current().maxBytes;
    return b === null ? '' : String(Math.round((b / GIB) * 1000) / 1000);
  });
  /** The byte bound was typed anew: the field's text differs from the stored value's. */
  private readonly gibChanged = computed(() => String(this.gib()).trim() !== this.storedGib());

  protected readonly minutesValue = computed(() => Number(this.minutes()));
  protected readonly minutesError = computed(() => {
    const n = this.minutesValue();
    return Number.isInteger(n) && n >= MIN_MINUTES && n <= MAX_MINUTES
      ? null
      : $localize`:@@syncs.settings.interval-error:Enter a whole number of minutes from 5 to 43,200 (30 days).`;
  });
  /** The interval the field says, as the dialog of a sync says it: "every hour", "every 6 hours". */
  protected readonly intervalText = computed(() =>
    this.minutesError() ? syncIntervalSentence(this.current().intervalSeconds) : syncIntervalSentence(this.minutesValue() * 60),
  );
  /** The byte bound in bytes: the stored one while the field shows it, else the one typed; null for none, NaN for what is no size. */
  protected readonly bytesValue = computed<number | null>(() => {
    if (!this.gibChanged()) return this.current().maxBytes;
    const raw = String(this.gib()).trim();
    if (raw === '') return null;
    return Math.round(Number(raw) * GIB);
  });
  protected readonly gibError = computed(() => {
    const b = this.bytesValue();
    return b === null || (Number.isFinite(b) && b >= 1024 * 1024)
      ? null
      : $localize`:@@syncs.settings.max-bytes-error:Enter a size of at least 0.001 GiB (1 MiB), or leave the field empty.`;
  });
  protected readonly dirty = computed(() => {
    const c = this.current();
    return this.minutesValue() * 60 !== c.intervalSeconds || this.paused() !== c.paused || this.gibChanged();
  });

  constructor() {
    // The fields follow what is stored, after the load and after a save.
    effect(() => {
      this.current();
      untracked(() => this.reset());
    });
    // An address with `#syncs` opens the section and brings it into view, as `#delivery` does its own.
    effect(() => {
      if (this.fragment() !== 'syncs') return;
      untracked(() => {
        this.open.set(true);
        queueMicrotask(() => (this.host.nativeElement as HTMLElement).scrollIntoView?.({ block: 'start' }));
      });
    });
  }

  protected reset(): void {
    const c = this.current();
    this.minutes.set(Math.round(c.intervalSeconds / 60));
    this.paused.set(c.paused);
    this.gib.set(this.storedGib());
  }

  protected submit(event: Event): void {
    event.preventDefault();
    if (!this.dirty() || this.saving() || this.minutesError() || this.gibError()) return;
    this.save.emit({ intervalSeconds: this.minutesValue() * 60, paused: this.paused(), maxBytes: this.bytesValue() });
  }
}
