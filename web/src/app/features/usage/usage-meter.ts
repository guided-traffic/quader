import { formatPercent } from '@angular/common';
import { ChangeDetectionStrategy, Component, LOCALE_ID, computed, inject, input } from '@angular/core';
import { LimitMode, UsageState } from '../../api/limits-api';
import { Tooltip } from '../../shared/ui/tooltip';
import { stateTone, stateWord } from './usage-words';

/**
 * The meter of a limit (R-UI-183's **Now**, R-UI-184's windowed figures, Q-UI-732): a bar of used against the limit with its
 * figure in words beside it, and the state under it as a status badge with its icon and word (**Close to the limit** from 70 %, **Limit reached**,
 * in Audit **Would have been limited**). The bar's colour is the data colour, the warning or the danger token, never green, and
 * never the only carrier of the state. An unlimited figure reads "Unlimited" without a bar.
 */
@Component({
  selector: 'quader-usage-meter',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [Tooltip],
  template: `
    @if (ratio() === null) {
      <span class="unlimited" i18n="@@usage.state.unlimited">Unlimited</span>
    } @else {
      <span class="meter-line" [quaderTooltip]="tooltip()" [attr.tabindex]="tooltip() ? 0 : null">
        <span
          class="bar"
          role="meter"
          aria-valuemin="0"
          aria-valuemax="100"
          [attr.aria-valuenow]="valueNow()"
          [attr.aria-valuetext]="valueText()"
          [attr.aria-label]="label() || nowLabel"
        >
          <span class="fill" [class.warn]="tone() === 'warn'" [class.bad]="tone() === 'bad'" [style.inline-size.%]="fillPercent()"></span>
        </span>
        <span class="figure">{{ text() || percent() }}</span>
      </span>
      @if (word(); as w) {
        <span class="state badge" [class.warn]="tone() === 'warn'" [class.bad]="tone() === 'bad'"
          ><span class="material-icons" aria-hidden="true">{{ tone() === 'bad' ? 'error' : 'warning' }}</span>{{ w }}</span
        >
      }
    }
  `,
  styles: `
    :host {
      display: flex;
      flex-direction: column;
      gap: 2px;
      min-inline-size: 0;
    }
    .meter-line {
      display: flex;
      align-items: flex-start;
      gap: var(--quader-space-2);
    }
    /* A bar of 6 px on the middle of the figure's line (R-UI-175). */
    .bar {
      flex: 1 1 4rem;
      min-inline-size: 3rem;
      max-inline-size: 8rem;
      block-size: 6px;
      margin-block: calc((var(--quader-line) - 6px) / 2);
      overflow: hidden;
      border-radius: var(--quader-radius-full);
      background: var(--quader-card-bg);
    }
    .fill {
      display: block;
      block-size: 100%;
      background: var(--quader-chart-low);
    }
    .fill.warn {
      background: var(--quader-status-warn);
    }
    .fill.bad {
      background: var(--quader-status-bad);
    }
    .figure {
      white-space: nowrap;
      font-variant-numeric: tabular-nums;
    }
    /* The state is the house's status badge: its icon and its word, as wide as they are (never colour alone). */
    .state {
      align-self: flex-start;
    }
    .unlimited {
      color: var(--quader-text-muted);
    }
  `,
})
export class UsageMeter {
  private readonly locale = inject(LOCALE_ID);

  /** Used against the limit (1 is the limit); null for an unlimited figure. */
  readonly ratio = input<number | null>(null);
  readonly state = input<UsageState>('normal');
  readonly mode = input<LimitMode>('audit');
  /** The accessible name: the figure and its place. */
  readonly label = input('');
  /** The figure in words beside the bar ("246 of 300 GB"); the percentage where there is none. */
  readonly text = input('');
  readonly tooltip = input('');

  protected readonly nowLabel = $localize`:@@usage.col.now:Now`;
  protected readonly percent = computed(() => formatPercent(this.ratio() ?? 0, this.locale, '1.0-0'));
  protected readonly valueNow = computed(() => Math.round((this.ratio() ?? 0) * 100));
  protected readonly fillPercent = computed(() => Math.max(0, Math.min(100, (this.ratio() ?? 0) * 100)));
  protected readonly tone = computed(() => stateTone(this.state()));
  protected readonly word = computed(() => stateWord(this.state(), this.mode()));
  protected readonly valueText = computed(() => {
    const parts = [this.text() || this.percent(), this.word()].filter(Boolean);
    return parts.join(', ');
  });
}
