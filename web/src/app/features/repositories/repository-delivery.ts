import { Component, ElementRef, computed, effect, inject, input, output, signal, untracked } from '@angular/core';
import { toSignal } from '@angular/core/rxjs-interop';
import { ActivatedRoute, RouterLink } from '@angular/router';
import { ApiError } from '../../api/problem';
import { Schema } from '../../api/types';
import { ErrorNotice } from '../../shared/error-notice';
import { Checkbox } from '../../shared/ui/checkbox';
import { QuaderDialog } from '../../shared/ui/dialog';
import { Disclosure } from '../../shared/ui/disclosure';
import { firstValueFrom } from 'rxjs';
import { DeliveryModeChoice, DeliveryModeData, DeliveryModeDialog } from './delivery-mode-dialog';
import { DeliveryMode, modeName } from './upstream/upstream-words';
import { Busy } from '../../shared/ui/busy';

/** `remote.delivery` of a remote repository (R-CUR-001, R-CUR-020). */
export type DeliverySettings = Schema<'DeliverySettings'>;

/** What the section saves: `remote.delivery`, and whether the stored packages are allowed first (R-CUR-025). */
export interface DeliverySave {
  readonly settings: DeliverySettings;
  readonly allowStored: boolean;
}

/** The values a remote without the member holds (R-CUR-020): every member's default. */
export const DELIVERY_DEFAULTS: Required<DeliverySettings> = { mode: 'allExceptBlocked', examineOnRequest: true, examinedRetentionDays: 30 };

/** The stored member with its defaults filled in. */
export function deliveryOf(stored: Partial<DeliverySettings> | undefined | null): Required<DeliverySettings> {
  return { ...DELIVERY_DEFAULTS, ...(stored ?? {}) };
}

/**
 * The closed section **Delivery** of a remote repository's settings (remote-curation.md R-CUR-018, R-CUR-020): details on demand
 * (R-UI-067). The mode as two choices with their sentences, **Fetch for examination when someone asks** and the days after which what
 * was examined and not allowed is removed, saved on their own with the page's `ETag`; the rules themselves are set on the tab
 * **Upstream**, which a link opens. The address's fragment `#delivery` (the tab's **Change**) opens the section. A caller who may not
 * change the configuration reads the values. A change of the mode first opens the dialog of R-CUR-025, which says what follows and,
 * towards **Only what is allowed**, offers to allow the stored packages the mode decides.
 */
@Component({
  selector: 'quader-repository-delivery',
  imports: [Busy, Checkbox, Disclosure, ErrorNotice, RouterLink],
  template: `
    <quader-disclosure
      id="delivery"
      i18n-heading="@@delivery.heading"
      heading="Delivery"
      i18n-hint="@@delivery.hint"
      hint="What this repository delivers of its upstream, and what it fetches to examine."
      [note]="dirty() ? unsavedNote : ''"
      [(expanded)]="open"
    >
      <form (submit)="submit($event)" novalidate>
        @if (otherError(); as e) {
          <quader-error-notice [error]="e" />
        }
        <fieldset class="mode" [disabled]="!canChange() || saving()">
          <legend i18n="@@delivery.mode">Delivery mode</legend>
          @for (m of modes; track m) {
            <label class="mode-option">
              <input type="radio" name="delivery-mode" [value]="m" [checked]="mode() === m" (change)="mode.set(m)" />
              <span class="mode-title">{{ name(m) }}</span>
              <span class="hint">{{ sentence(m) }}</span>
            </label>
          }
        </fieldset>
        <p class="rules-link">
          <a [routerLink]="upstreamLink()" i18n="@@delivery.rules-link">The rules are set on the tab Upstream</a>
        </p>
        <fieldset class="plain examination" [disabled]="!canChange() || saving()">
          <quader-checkbox [checked]="examine()" [disabled]="!canChange() || saving()" [help]="examineHelp" [about]="examineName" (checkedChange)="examine.set($event)">
            <ng-container i18n="@@delivery.examine">Fetch for examination when someone asks</ng-container>
          </quader-checkbox>
          <!-- What the box does is its help (R-UI-267): a request downloads at once, so that it is scanned before anyone decides,
               and it is not delivered until allowed. -->
          <div class="days">
            <label class="field-label" for="delivery-days" i18n="@@delivery.days">Remove what was examined and not allowed after this many days</label>
            <input
              id="delivery-days"
              class="text-input field-w-duration"
              type="number"
              inputmode="numeric"
              min="1"
              max="365"
              [value]="days()"
              [attr.aria-invalid]="daysError() ? 'true' : null"
              [attr.aria-describedby]="daysError() ? 'delivery-days-error' : null"
              (input)="days.set($any($event.target).value)"
            />
            @if (daysError(); as e) {
              <p class="field-error" id="delivery-days-error">{{ e }}</p>
            }
          </div>
        </fieldset>
        @if (canChange()) {
          <div class="actions">
            <button class="btn btn-confirm" type="submit" [disabled]="!dirty() || saving() || !!daysError()" [quaderBusy]="saving()">
              <ng-container i18n="@@repo.settings.save">Save changes</ng-container>
              <span class="visually-hidden" i18n="@@delivery.save-for"> (delivery)</span>
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
    .mode {
      display: flex;
      flex-direction: column;
      gap: var(--quader-space-2);
      max-inline-size: 36rem;
      margin: 0 0 var(--quader-space-3);
      padding: 0;
      border: 0;

      legend {
        margin-block-end: var(--quader-space-2);
        font-weight: 600;
      }
    }
    // A choice and its sentence, as the mode of a remote's mirror (mirror-settings.ts).
    .mode-option {
      display: grid;
      grid-template-columns: auto 1fr;
      column-gap: var(--quader-space-2);
      padding: var(--quader-space-3);
      border: 1px solid var(--quader-border);
      border-radius: var(--quader-radius-l);

      input {
        grid-row: 1 / span 2;
      }
      .hint {
        grid-column: 2;
        margin: 0;
      }
    }
    .mode-title {
      font-weight: 600;
    }
    .rules-link {
      margin: 0 0 var(--quader-space-4);
    }
    .examination {
      margin: 0;
      padding: 0;
      border: 0;
    }
    .examination .hint {
      margin: var(--quader-space-1) 0 var(--quader-space-3);
    }
    .days {
      display: flex;
      flex-direction: column;
      gap: var(--quader-space-1);
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
export class RepositoryDeliverySection {
  private readonly route = inject(ActivatedRoute);
  private readonly host = inject(ElementRef<HTMLElement>);
  private readonly dialog = inject(QuaderDialog);

  /** `remote.delivery` as stored; absent members hold their defaults. */
  readonly stored = input<Partial<DeliverySettings> | undefined | null>(undefined);
  readonly project = input.required<string>();
  readonly repositoryKey = input.required<string>();
  /** The caller may set the configuration: `repositories:write-config` or the project's Owner (R-CUR-005). */
  readonly canChange = input(false);
  readonly saving = input(false);
  readonly failure = input<ApiError | null>(null);
  readonly save = output<DeliverySave>();

  protected readonly modes: readonly DeliveryMode[] = ['allExceptBlocked', 'onlyAllowed'];
  protected readonly name = modeName;
  protected readonly mode = signal<DeliveryMode>('allExceptBlocked');
  protected readonly examine = signal(true);
  protected readonly days = signal<string | number>(30);
  protected readonly open = signal(false);
  protected readonly unsavedNote = $localize`:@@repo.settings.unsaved:Unsaved changes`;

  private readonly fragment = toSignal(this.route.fragment, { initialValue: null });
  private readonly current = computed(() => deliveryOf(this.stored()));
  protected readonly upstreamLink = computed(() => ['/projects', this.project(), 'repositories', this.repositoryKey(), 'upstream']);

  protected readonly daysValue = computed(() => Number(this.days()));
  protected readonly daysError = computed(() => {
    const n = this.daysValue();
    return Number.isInteger(n) && n >= 1 && n <= 365 ? null : $localize`:@@delivery.days-error:Enter a whole number of days from 1 to 365.`;
  });
  protected readonly dirty = computed(() => {
    const c = this.current();
    return this.mode() !== c.mode || this.examine() !== c.examineOnRequest || this.daysValue() !== c.examinedRetentionDays;
  });
  protected readonly otherError = computed(() => this.failure());

  constructor() {
    // The fields follow what is stored, after the load and after a save.
    effect(() => {
      this.current();
      untracked(() => this.reset());
    });
    // The tab Upstream's **Change** leads here with `#delivery`: the section opens and comes into view.
    effect(() => {
      if (this.fragment() !== 'delivery') return;
      untracked(() => {
        this.open.set(true);
        queueMicrotask(() => (this.host.nativeElement as HTMLElement).scrollIntoView?.({ block: 'start' }));
      });
    });
  }

  protected sentence(m: DeliveryMode): string {
    return m === 'onlyAllowed'
      ? $localize`:@@delivery.mode.only-allowed.hint:Nothing is delivered unless a rule allows it. Readers ask for what they need, and those who manage the repository decide.`
      : $localize`:@@delivery.mode.all-except-blocked.hint:Every package and version is delivered unless a rule blocks it. This is the default.`;
  }

  protected reset(): void {
    const c = this.current();
    this.mode.set(c.mode);
    this.examine.set(c.examineOnRequest);
    this.days.set(c.examinedRetentionDays);
  }

  protected async submit(event: Event): Promise<void> {
    event.preventDefault();
    if (!this.dirty() || this.saving() || this.daysError()) return;
    const settings: DeliverySettings = { mode: this.mode(), examineOnRequest: this.examine(), examinedRetentionDays: this.daysValue() };
    let allowStored = false;
    if (settings.mode !== this.current().mode) {
      const ref = this.dialog.open<DeliveryModeDialog, DeliveryModeData, DeliveryModeChoice>(DeliveryModeDialog, {
        data: { repository: `${this.project()}/${this.repositoryKey()}`, to: this.mode() },
        width: '36rem',
      });
      const choice = await firstValueFrom(ref.closed);
      if (!choice) return;
      allowStored = choice.allowStored;
    }
    this.save.emit({ settings, allowStored });
  }

  /** The box's name and what it does, behind its help sign (R-UI-267). */
  protected readonly examineName = $localize`:@@delivery.examine:Fetch for examination when someone asks`;
  protected readonly examineHelp = $localize`:@@delivery.examine-hint:A request downloads what it asks for at once, so that it is scanned before anyone decides; it is not delivered until allowed.`;
}
