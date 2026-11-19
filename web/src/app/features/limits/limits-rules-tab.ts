import { ChangeDetectionStrategy, Component, LOCALE_ID, computed, inject, signal, viewChild } from '@angular/core';
import { LimitPlace, LimitsApi } from '../../api/limits-api';
import { ApiError } from '../../api/problem';
import { Notifier } from '../../core/notifier';
import { SessionStore } from '../../core/session-store';
import { ErrorNotice } from '../../shared/error-notice';
import { asApiError } from '../../shared/paged-resource';
import { formatCount } from './figures';
import { LimitRules } from './limit-rules';
import { LimitsSettingsState } from './limits-state';

/** The block time as it reads: "15 minutes", "1 minute". */
export function minutesText(seconds: number, locale: string): string {
  const minutes = Math.round(seconds / 60);
  return minutes === 1
    ? $localize`:@@limits.minute:1 minute`
    : $localize`:@@limits.minutes:${formatCount(minutes, locale)}:count: minutes`;
}

/**
 * The tab **Rules** of **Download limits** (R-UI-177): the rule table of the instance (R-UI-179) with **Add rule** for a holder
 * of `limits:write`, and under it the block time after too many refused requests, changed in place in minutes (1 to 1,440).
 */
@Component({
  selector: 'quader-limits-rules-tab',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [ErrorNotice, LimitRules],
  template: `
    @if (canWrite()) {
      <div class="tab-head">
        <button class="btn btn-confirm" type="button" (click)="rules()?.add()" i18n="@@limits.rules.add">Add rule</button>
      </div>
    }
    <quader-limit-rules [place]="place" [canWrite]="canWrite()" />

    @if (state.settings(); as s) {
      <div class="block-time">
        @if (editing()) {
          <form class="field-action-row" (submit)="saveBlockTime($event)" novalidate>
            <div class="field">
              <label class="field-label" for="block-minutes" i18n="@@limits.block-time.label">Block time in minutes (1 to 1,440)</label>
              <input
                id="block-minutes"
                class="text-input field-w-number"
                type="text"
                inputmode="numeric"
                name="blockMinutes"
                [value]="minutes()"
                [attr.aria-invalid]="minutesError() ? 'true' : null"
                [attr.aria-describedby]="minutesError() ? 'block-minutes-error' : null"
                (input)="minutes.set($any($event.target).value)"
              />
            </div>
            <button class="btn btn-confirm" type="submit" [disabled]="busy()" i18n="@@limits.save-changes">Save changes</button>
            <button class="btn btn-tertiary" type="button" (click)="editing.set(false)" i18n="@@limits.cancel">Cancel</button>
          </form>
          @if (minutesError(); as e) {
            <p id="block-minutes-error" class="field-error">{{ e }}</p>
          }
        } @else {
          <p class="line">
            <span i18n="@@limits.block-time.text">Block time after too many refused requests: {{ blockText(s.value.blockSeconds) }}</span>
            @if (canWrite()) {
              <button class="btn btn-tertiary on-line" type="button" (click)="edit(s.value.blockSeconds)">
                <ng-container i18n="@@limits.block-time.change">Change</ng-container>
                <span class="visually-hidden" i18n="@@limits.block-time.change-hidden"> the block time</span>
              </button>
            }
          </p>
        }
        @if (failure(); as e) {
          <quader-error-notice [error]="e" />
        }
      </div>
    }
  `,
  styles: `
    .tab-head {
      display: flex;
      justify-content: flex-end;
      margin-block-end: var(--quader-space-3);
    }
    .block-time {
      margin-block-start: var(--quader-space-4);
    }
    .line {
      display: flex;
      flex-wrap: wrap;
      align-items: flex-start;
      gap: var(--quader-space-2);
      margin: 0;
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
  `,
})
export class LimitsRulesTab {
  private readonly session = inject(SessionStore);
  private readonly api = inject(LimitsApi);
  private readonly notifier = inject(Notifier);
  private readonly locale = inject(LOCALE_ID);
  protected readonly state = inject(LimitsSettingsState);

  protected readonly place: LimitPlace = { kind: 'instance' };
  protected readonly rules = viewChild(LimitRules);
  protected readonly canWrite = computed(() => this.session.has('limits:write'));
  protected readonly editing = signal(false);
  protected readonly minutes = signal('');
  protected readonly attempted = signal(false);
  protected readonly busy = signal(false);
  protected readonly failure = signal<ApiError | null>(null);

  protected readonly minutesError = computed(() => {
    if (!this.attempted()) return null;
    const t = this.minutes().trim();
    const n = Number(t);
    return /^\d+$/.test(t) && n >= 1 && n <= 1440
      ? null
      : $localize`:@@limits.block-time.invalid:A whole number of minutes from 1 to 1,440.`;
  });

  protected blockText(seconds: number): string {
    return minutesText(seconds, this.locale);
  }

  protected edit(seconds: number): void {
    this.minutes.set(String(Math.round(seconds / 60)));
    this.attempted.set(false);
    this.failure.set(null);
    this.editing.set(true);
  }

  protected async saveBlockTime(event: Event): Promise<void> {
    event.preventDefault();
    this.attempted.set(true);
    const settings = this.state.settings();
    if (!settings || this.minutesError()) return;
    this.busy.set(true);
    this.failure.set(null);
    try {
      const saved = await this.api.patchSettings({ blockSeconds: Number(this.minutes().trim()) * 60 }, settings.etag);
      this.state.store(saved);
      this.editing.set(false);
      this.notifier.status($localize`:@@limits.block-time.saved:Block time saved.`);
    } catch (e) {
      this.failure.set(asApiError(e));
    } finally {
      this.busy.set(false);
    }
  }
}
