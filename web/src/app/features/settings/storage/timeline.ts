import { Component, computed, input, signal } from '@angular/core';
import { StorageHistoryBucket } from '../../../api/storage-api';
import { Tooltip } from '../../../shared/ui/tooltip';
import { cellSummary, stripSummary } from './storage-history';

/**
 * A connection's strip of the period (api-and-ui.md R-UI-265, Q-UI-1600, Q-UI-1601): one cell per bucket of the history, oldest first,
 * counted back from now (Q-STOR-527). A cell's tone is the server's: an error when clients were refused, a warning when Quader absorbed
 * it, none when nothing happened (Q-STOR-528). Every cell is a button with its text summary as its name and its tooltip; a click names it
 * under the strip, so the tone is never the only carrier. The strip has its own summary for assistive technology. The strip never scrolls
 * sideways: the cells share its width (52 cells at 358 px are 6 px each). The compact form (the Connections table) is tone only and one
 * image named by its summary.
 */
@Component({
  selector: 'quader-storage-timeline',
  imports: [Tooltip],
  template: `
    @if (compact()) {
      <span class="strip compact" role="img" [attr.aria-label]="label() + ': ' + summary()">
        @for (b of buckets(); track b.start) {
          <span class="cell" [attr.data-tone]="b.tone"></span>
        }
      </span>
    } @else {
      <div class="strip" role="group" [attr.aria-label]="label()">
        @for (b of buckets(); track b.start; let i = $index) {
          <button
            type="button"
            class="cell"
            [attr.data-tone]="b.tone"
            [attr.aria-pressed]="selected() === i"
            [attr.aria-label]="text(b)"
            [quaderTooltip]="text(b)"
            (click)="select(i)"
          ></button>
        }
      </div>
      <p class="visually-hidden">{{ summary() }}</p>
      @if (picked(); as b) {
        <p class="picked" role="status" [attr.data-tone]="b.tone">{{ text(b) }}</p>
      }
    }
  `,
  styles: `
    :host {
      display: block;
      min-inline-size: 0;
    }
    .strip {
      display: flex;
      gap: 2px;
      inline-size: 100%;
      block-size: 1.5rem;
      align-items: stretch;
    }
    .strip.compact {
      block-size: 0.75rem;
      gap: 1px;
      margin-block: calc((var(--quader-line) - 0.75rem) / 2);
    }
    .cell {
      flex: 1 1 0;
      min-inline-size: 2px;
      padding: 0;
      border: 0;
      border-radius: 2px;
      background: var(--quader-card-bg);
      cursor: pointer;
      box-shadow: inset 0 0 0 1px var(--quader-border);
    }
    .compact .cell {
      cursor: default;
      box-shadow: none;
    }
    .cell[data-tone='warning'] {
      background: var(--quader-status-warn);
      box-shadow: none;
    }
    .cell[data-tone='error'] {
      background: var(--quader-status-bad);
      box-shadow: none;
    }
    button.cell:focus-visible {
      outline: var(--quader-focus-ring);
      outline-offset: 1px;
    }
    button.cell[aria-pressed='true'] {
      outline: 2px solid var(--quader-on-surface);
      outline-offset: 1px;
    }
    .picked {
      margin: var(--quader-space-1) 0 0;
      color: var(--quader-text-muted);
      overflow-wrap: anywhere;
    }
  `,
})
export class Timeline {
  /** The cells of the period, oldest first. */
  readonly buckets = input.required<readonly StorageHistoryBucket[]>();
  readonly bucketSeconds = input.required<number>();
  /** The strip's name: the connection it belongs to. */
  readonly label = input.required<string>();
  /** Tone only, one image (the Connections table). */
  readonly compact = input(false);

  protected readonly selected = signal<number | null>(null);
  protected readonly summary = computed(() => stripSummary(this.buckets()));
  protected readonly picked = computed(() => {
    const i = this.selected();
    return i === null ? null : (this.buckets()[i] ?? null);
  });

  protected text(b: StorageHistoryBucket): string {
    return cellSummary(b, this.bucketSeconds());
  }

  protected select(i: number): void {
    this.selected.set(this.selected() === i ? null : i);
  }
}
