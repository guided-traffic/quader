import { ChangeDetectionStrategy, Component, computed, input } from '@angular/core';
import { Tone } from './storage-model';

/**
 * The bar of a share on the storage page (Q-UI-1592): how much of every blob a connection holds, its use of a stated capacity, the share
 * of its copies in Quader's format. A bar of 6 px on the middle of the text's line, filled with the data colour, the warning or the
 * danger token, never green and never the only carrier of a state: the figure and the word stand beside it. It is not the usage meter
 * of the download limits, whose words and thresholds differ.
 */
@Component({
  selector: 'quader-storage-meter',
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `<span
    class="bar"
    role="meter"
    aria-valuemin="0"
    aria-valuemax="100"
    [attr.aria-valuenow]="percent()"
    [attr.aria-label]="label()"
    ><span class="fill" [attr.data-tone]="tone()" [style.inline-size.%]="percent()"></span
  ></span>`,
  styles: `
    :host {
      display: inline-flex;
      flex: 0 1 6rem;
      min-inline-size: 3rem;
      max-inline-size: 6rem;
      vertical-align: top;
    }
    .bar {
      display: block;
      inline-size: 100%;
      block-size: 6px;
      margin-block: calc((var(--quader-line) - 6px) / 2);
      overflow: hidden;
      border-radius: var(--quader-radius-full);
      background: var(--quader-card-bg);
      box-shadow: inset 0 0 0 1px var(--quader-border);
    }
    .fill {
      display: block;
      block-size: 100%;
      background: var(--quader-chart-usage);
    }
    .fill[data-tone='warn'] {
      background: var(--quader-status-warn);
    }
    .fill[data-tone='bad'] {
      background: var(--quader-status-bad);
    }
  `,
})
export class StorageMeter {
  /** The share, 0 to 1. */
  readonly ratio = input.required<number>();
  readonly tone = input<Tone>('neutral');
  /** What the bar measures, for assistive technology. */
  readonly label = input.required<string>();

  protected readonly percent = computed(() => Math.round(Math.min(1, Math.max(0, this.ratio())) * 100));
}
