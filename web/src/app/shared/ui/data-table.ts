import { Component, DestroyRef, ElementRef, afterRenderEffect, computed, inject, input, output, untracked } from '@angular/core';
import { Tooltip } from './tooltip';
import { ApiError } from '../../api/problem';
import { ErrorNotice } from '../error-notice';
import { EmptyState } from './empty-state';

/** One column of a `<quader-data-table>`. The label is the translated header text. */
export interface TableColumn {
  readonly id: string;
  readonly label: string;
  /** Explains the column; shown as a "?" button whose tooltip is also its accessible description. */
  readonly help?: string;
  /** Only where the API (or a complete client-side list) can really sort by this column. */
  readonly sortable?: boolean;
  /** Right-aligned figures. */
  readonly numeric?: boolean;
  /** The header text stays for assistive technology only (checkbox and action columns). */
  readonly hideLabel?: boolean;
  /** The column's least width (a CSS length), from the list's budget (Q-UI-308): the header cell carries it. It must hold the header's label. */
  readonly width?: string;
  /**
   * The column takes everything the others leave, and every other column of the list keeps its `width` (Q-UI-330): the name of a
   * repository list or of the search results, the scope of a token list. Its own `width` is its least.
   */
  readonly prefer?: boolean;
  /** When the rows stack (R-UI-174), the cell shows its column's name before its value; false where the value says it. */
  readonly stackLabel?: boolean;
  /**
   * The column gives its room to the others below 64 rem of the list's width (Q-UI-308, UI-A-30): its header and the list's cells
   * marked `narrow-drop` are hidden there.
   */
  readonly narrow?: boolean;
}

export type SortDirection = 'asc' | 'desc';

/** A cell without a value: empty, a bare "–", or only `<quader-no-value>` (Q-UI-561). */
function emptyCell(cell: HTMLTableCellElement, text: string): boolean {
  if (cell.children.length === 0) return text === '' || text === '–';
  return cell.children.length === 1 && cell.firstElementChild?.tagName === 'QUADER-NO-VALUE';
}

/** The active sort: one column and a direction. */
export interface TableSort {
  readonly column: string;
  readonly direction: SortDirection;
}

let nextTableId = 1;

/**
 * The data table of every list screen (R-UI-020, R-UI-024 to R-UI-027, R-UI-030, R-UI-037, R-UI-006).
 * It owns the frame: a bordered, inner-scrolling region with a sticky header, sortable headers
 * (`aria-sort`, a real button per sortable column), a "?" help tooltip per column that needs one,
 * and the four states: loading, error (with retry), empty and filled, plus the "Load more" button of
 * cursor lists (R-UI-006). The rows are the screen's own `<tr>` elements, projected into the body, so
 * links, badges and actions stay in the screen's template.
 *
 * Sorting is a request, not a reordering: the table emits `sortChange` and the screen restarts its
 * list with the sort the API understands. A column is `sortable` only where that is true.
 */
@Component({
  selector: 'quader-data-table',
  imports: [Tooltip, ErrorNotice, EmptyState],
  template: `
    @if (error(); as e) {
      <quader-error-notice [error]="e" (retry)="retry.emit()" />
    }
    @if (!loaded()) {
      @if (loading()) {
        <p class="table-status" role="status" i18n="@@table.loading">Loading.</p>
      }
    } @else if (empty()) {
      <quader-empty-state [text]="emptyText()"><ng-content select="[emptyAction]" /></quader-empty-state>
    } @else {
      <div class="table-width" aria-hidden="true"></div>
      <div class="table-frame">
        <div class="scroll table-scroll" tabindex="0" role="region" [attr.aria-labelledby]="captionId">
          <table class="data">
            <caption [id]="captionId" class="visually-hidden">
              {{ caption() }}
            </caption>
            <thead>
              <tr>
                @for (c of columns(); track c.id) {
                  <th scope="col" [class.num]="c.numeric" [class.narrow-drop]="c.narrow" [attr.aria-sort]="ariaSort(c)" [style.min-inline-size]="c.width" [style.inline-size]="kept(c)">
                    <span class="th-inner">
                      @if (c.sortable) {
                        <button type="button" class="th-sort" (click)="toggle(c)">
                          <span [class.visually-hidden]="c.hideLabel">{{ c.label }}</span>
                          <span class="material-icons th-arrow" aria-hidden="true" [class.sorted]="isSorted(c)">{{ arrow(c) }}</span>
                        </button>
                      } @else {
                        <span [class.visually-hidden]="c.hideLabel">{{ c.label }}</span>
                      }
                      @if (c.help) {
                        <button
                          type="button"
                          class="th-help"
                          [quaderTooltip]="c.help"
                          [attr.aria-label]="helpLabel(c)"
                        >
                          <span class="material-icons" aria-hidden="true">help_outline</span>
                        </button>
                      }
                    </span>
                  </th>
                }
              </tr>
            </thead>
            <tbody>
              <ng-content />
            </tbody>
          </table>
        </div>
      </div>
      @if (hasMore()) {
        <p class="table-more">
          <button class="btn btn-default"
            type="button"
            [disabled]="loading()"
            (click)="more.emit()"
            i18n="@@list.load-more"
          >
            Load more
          </button>
        </p>
      }
    }
  `,
  host: {
    class: 'quader-data-table',
    '[class.narrow-early]': 'narrowBelow() === "80rem"',
    '[class.stack]': 'stack()',
  },
})
export class DataTable {
  /** Accessible name of the table (rendered as a visually hidden caption). */
  readonly caption = input.required<string>();
  readonly columns = input.required<readonly TableColumn[]>();
  readonly sort = input<TableSort | null>(null);

  /** True once the first answer has arrived. */
  readonly loaded = input(true);
  /** True while a request is running; before `loaded` it shows the loading status. */
  readonly loading = input(false);
  readonly error = input<ApiError | null>(null);
  readonly empty = input(false);
  readonly emptyText = input($localize`:@@table.empty:Nothing to show.`);
  readonly hasMore = input(false);

  readonly sortChange = output<TableSort>();
  readonly more = output<void>();
  readonly retry = output<void>();

  /**
   * Where its columns do not fit, the list shows each row as a block (R-UI-174, Q-UI-680); a list whose rows cannot be read so
   * turns it off.
   */
  readonly stack = input(true);
  /**
   * The list's width below which the columns marked `narrow` give way: 64 rem (Q-UI-308), or 80 rem for a list whose every column
   * is a figure with a sortable header, which holds seven columns only from there (the usage overview, Q-UI-760).
   */
  readonly narrowBelow = input<'64rem' | '80rem'>('64rem');

  protected readonly captionId = `quader-table-${nextTableId++}`;

  /**
   * In a list with a column that prefers the room, every other column keeps its width (the header cell gets it as its size, not
   * only as its least) and that column asks for the whole table, so the room left over goes to it alone (Q-UI-330).
   */
  protected kept(c: TableColumn): string | null {
    if (!this.columns().some((x) => x.prefer)) return null;
    return c.prefer ? '100%' : (c.width ?? null);
  }

  constructor() {
    // Each cell carries its column's name for the stacked rows, and an empty value ("–") is marked, so a stacked row leaves its
    // line out. The rows are the screen's, projected: they are labelled after every change of the body.
    const host = inject(ElementRef<HTMLElement>).nativeElement as HTMLElement;
    let observer: MutationObserver | undefined;
    let body: Element | null = null;
    afterRenderEffect(() => {
      this.columns();
      // The body appears with the first answer and goes with an empty list or an error: look for it again then.
      this.loaded();
      this.empty();
      this.error();
      const current = host.querySelector('tbody');
      const changed = (): void => {
        label();
        this.measure(host);
      };
      // The columns are read when the body changes, not when the observer was made: a list whose columns follow its data (a
      // package's versions gain Vulnerabilities with the first grade) named every cell after the column before it.
      const label = (): void => {
        const columns = untracked(() => this.columns());
        for (const row of Array.from(current?.rows ?? [])) {
          Array.from(row.cells).forEach((cell, i) => {
            const c = columns[i];
            const text = cell.textContent?.trim() ?? '';
            const named = c && cell.colSpan === 1 && cell.tagName === 'TD' && !c.hideLabel && c.stackLabel !== false;
            if (named) cell.setAttribute('data-label', c.label);
            else cell.removeAttribute('data-label');
            cell.toggleAttribute('data-empty', cell.tagName === 'TD' && emptyCell(cell, text));
          });
        }
      };
      changed();
      if (current !== body && typeof MutationObserver !== 'undefined') {
        observer?.disconnect();
        body = current;
        if (current) {
          observer = new MutationObserver(changed);
          observer.observe(current, { childList: true, subtree: true, characterData: true });
        }
      }
      // The list's width: an element of no height across the table, and the room inside the scroll region, which a scrollbar
      // beside the rows narrows while the list keeps its width (a window made lower: the admin area's repositories at 1,280 px
      // were 10 px wider than their region). Only a change of a width is a reason to measure; the change of the frame's height
      // that the stacking makes is not. The measurement waits for the next frame: a change of the layout inside the observer's
      // callback would start a second round of every observer of the page ("ResizeObserver loop completed with undelivered
      // notifications").
      const ruler = host.querySelector('.table-width');
      const region = host.querySelector('.table-scroll');
      if (ruler !== measured && typeof ResizeObserver !== 'undefined') {
        resized?.disconnect();
        measured = ruler;
        if (ruler) {
          const last = new Map<Element, number>();
          resized = new ResizeObserver((entries) => {
            let changed = false;
            for (const entry of entries) {
              if (last.get(entry.target) === entry.contentRect.width) continue;
              last.set(entry.target, entry.contentRect.width);
              changed = true;
            }
            if (!changed) return;
            cancelAnimationFrame(frame);
            frame = requestAnimationFrame(() => this.measure(host));
          });
          resized.observe(ruler);
          if (region) resized.observe(region);
        }
      }
    });
    let measured: Element | null = null;
    let resized: ResizeObserver | undefined;
    let frame = 0;
    // A web font that arrives late changes every column's width.
    void document.fonts?.ready.then(() => this.measure(host));
    inject(DestroyRef).onDestroy(() => {
      observer?.disconnect();
      resized?.disconnect();
      if (frame) cancelAnimationFrame(frame);
    });
  }

  /**
   * Stacks the rows where the columns do not fit (R-UI-174, Q-UI-680): the table at its least width, which is each column's
   * declared least width (`width`) or its longest word, and a column that never breaks at its whole value, against the room of
   * its scroll region, which a scrollbar beside the rows narrows. The table is measured as a table, so a stacked one is taken
   * out of its blocks for the measurement and back in the same task; nothing is painted between.
   */
  private measure(host: HTMLElement): void {
    const table = host.querySelector<HTMLElement>('table.data');
    const region = host.querySelector<HTMLElement>('.table-scroll');
    if (!this.stack() || !table || !region) {
      host.classList.remove('stacked');
      return;
    }
    if (region.getBoundingClientRect().width === 0) return;
    host.classList.remove('stacked');
    const width = table.style.inlineSize;
    table.style.inlineSize = '0px';
    const need = table.getBoundingClientRect().width;
    table.style.inlineSize = width;
    host.classList.toggle('stacked', need > region.clientWidth + 0.5);
  }

  protected readonly sortedId = computed(() => this.sort()?.column ?? null);

  protected isSorted(c: TableColumn): boolean {
    return this.sortedId() === c.id;
  }

  protected ariaSort(c: TableColumn): 'ascending' | 'descending' | 'none' | null {
    if (!c.sortable) return null;
    const s = this.sort();
    if (s?.column !== c.id) return 'none';
    return s.direction === 'asc' ? 'ascending' : 'descending';
  }

  protected arrow(c: TableColumn): string {
    const s = this.sort();
    if (s?.column !== c.id) return 'unfold_more';
    return s.direction === 'asc' ? 'arrow_upward' : 'arrow_downward';
  }

  protected helpLabel(c: TableColumn): string {
    return $localize`:@@table.help-label:About the column ${c.label}:column:`;
  }

  protected toggle(c: TableColumn): void {
    const s = this.sort();
    const direction: SortDirection = s?.column === c.id && s.direction === 'asc' ? 'desc' : 'asc';
    this.sortChange.emit({ column: c.id, direction });
  }
}
