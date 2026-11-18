import {
  Component,
  DestroyRef,
  ElementRef,
  afterRenderEffect,
  computed,
  effect,
  inject,
  input,
  model,
  output,
  signal,
  untracked,
  viewChild,
} from '@angular/core';
import { FilterToggle } from './filter-panel';

/** One filter that is set, as the list's head shows it: a chip with the filter's name, its value and a way to remove it. */
export interface ListChip {
  /** Unique within the list; the list knows by it which filter to remove. */
  readonly key: string;
  /** The filter's name: **Format**, **Severity**. */
  readonly label: string;
  /** The value as the reader knows it: **OCI (Docker)**, **High**, a key. */
  readonly value: string;
}

/** How long the search field waits after the last key before it filters: the repository page's field waits as long. */
export const SEARCH_DEBOUNCE_MS = 250;

/**
 * The head of every list (Q-UI-420 to Q-UI-422): one row of the search by name, the **Filters** button that opens the list's
 * drawer, and the filters that are set as chips, each with its value and a button that removes it. Chips that do not fit beside
 * the button fold into one **+N more** button that opens the drawer, where every filter stands open. A list's own controls (a
 * selection's action, an export) are projected with `headEnd` and stand at the row's end.
 *
 * The search field filters as the list's name filter did: 250 ms after the last key, or at once on Enter, through
 * `searchChange`, with the text trimmed. The list keeps the applied text and passes it back as `search`; a text the list applies
 * from elsewhere (a link, a cleared filter) replaces what the field shows. A list without a search by name leaves `searchLabel`
 * empty, and a list without a drawer sets `filters` to false.
 */
@Component({
  selector: 'quader-list-head',
  imports: [FilterToggle],
  template: `
    <div class="list-head">
      @if (searchLabel(); as label) {
        <div class="list-search" role="search" [attr.aria-label]="label">
          <input
            class="text-input"
            type="search"
            autocomplete="off"
            spellcheck="false"
            [attr.aria-label]="label"
            [attr.placeholder]="label"
            [value]="draft()"
            (input)="typed($any($event.target).value)"
            (keydown.enter)="$event.preventDefault(); now($any($event.target).value)"
          />
          <span class="material-icons" aria-hidden="true">search</span>
        </div>
      }
      @if (filters()) {
        <div class="list-filters">
          <quader-filter-toggle [(open)]="filtersOpen" [activeCount]="chips().length" [controls]="controls()" />
          @if (chips().length > 0) {
            <ul #chipBox class="chip-row" [attr.aria-label]="chipsLabel">
              @for (c of chips(); track c.key; let i = $index) {
                <li class="chip-item" [class.chip-hidden]="i >= shown()" [attr.aria-hidden]="i >= shown() ? 'true' : null">
                  <span class="chip">
                    <span class="chip-text"
                      ><span class="chip-label">{{ c.label }}:&ngsp;</span><span class="chip-value">{{ c.value }}</span></span
                    >
                    <button
                      type="button"
                      [attr.aria-label]="removeLabel(c)"
                      [attr.tabindex]="i >= shown() ? -1 : null"
                      (click)="removeChip.emit(c)"
                    >
                      <span class="material-icons" aria-hidden="true">close</span>
                    </button>
                  </span>
                </li>
              }
              <li class="chip-item chip-more-item" [class.chip-hidden]="hidden() === 0" [attr.aria-hidden]="hidden() === 0 ? 'true' : null">
                <button
                  type="button"
                  class="btn btn-tertiary chip-more"
                  aria-haspopup="dialog"
                  [attr.aria-label]="moreLabel()"
                  [attr.tabindex]="hidden() === 0 ? -1 : null"
                  (click)="filtersOpen.set(true)"
                >
                  {{ moreText() }}
                </button>
              </li>
            </ul>
          }
        </div>
      }
      <div class="list-head-end"><ng-content select="[headEnd]" /></div>
    </div>
  `,
  host: { class: 'quader-list-head' },
})
export class ListHead {
  /** The accessible name and the placeholder of the search field (**Filter repositories**); no field without one. */
  readonly searchLabel = input<string | null>(null);
  /** The search text the list applies now. */
  readonly search = input('');
  /** A new search text, trimmed: 250 ms after the last key, or at once on Enter. */
  readonly searchChange = output<string>();
  /** The list has a filter drawer: the **Filters** button and the chips. */
  readonly filters = input(true);
  /** The filters that are set, without the search text, which the field shows. */
  readonly chips = input<readonly ListChip[]>([]);
  readonly removeChip = output<ListChip>();
  /** The list's drawer is open; bound to the `<quader-filter-panel>`'s `open`. */
  readonly filtersOpen = model(false);
  /** The id of the drawer (`FilterPanel.panelId`). */
  readonly controls = input<string | null>(null);

  protected readonly draft = signal('');
  /** How many chips stand in the row; the rest fold into **+N more**. */
  protected readonly shown = signal(Number.POSITIVE_INFINITY);
  protected readonly hidden = computed(() => Math.max(0, this.chips().length - this.shown()));
  protected readonly chipsLabel = $localize`:@@list-head.chips:Filters that are set`;

  private readonly chipBox = viewChild<ElementRef<HTMLElement>>('chipBox');
  private debounce: ReturnType<typeof setTimeout> | undefined;
  /** The last text this field sent: an answer that brings it back does not overwrite what was typed since. */
  private sent: string | null = null;

  constructor() {
    effect(() => {
      const applied = this.search();
      untracked(() => {
        if (applied !== this.sent) this.draft.set(applied);
        this.sent = null;
      });
    });
    // The row is measured after every change of the chips and of its width.
    let observer: ResizeObserver | undefined;
    let observed: HTMLElement | undefined;
    afterRenderEffect({
      read: () => {
        this.chips();
        // The row of chips goes with the last chip and comes again with a new one: the new element is observed.
        const box = this.chipBox()?.nativeElement;
        if (box !== observed && typeof ResizeObserver !== 'undefined') {
          observer ??= new ResizeObserver(() => this.fit());
          if (observed) observer.unobserve(observed);
          if (box) observer.observe(box);
          observed = box;
        }
        untracked(() => this.fit());
      },
    });
    inject(DestroyRef).onDestroy(() => {
      observer?.disconnect();
      clearTimeout(this.debounce);
    });
  }

  protected typed(value: string): void {
    this.draft.set(value);
    clearTimeout(this.debounce);
    this.debounce = setTimeout(() => this.send(value), SEARCH_DEBOUNCE_MS);
  }

  protected now(value: string): void {
    clearTimeout(this.debounce);
    this.send(value);
  }

  private send(value: string): void {
    const text = value.trim();
    if (text === this.search()) return;
    this.sent = text;
    this.searchChange.emit(text);
  }

  protected removeLabel(c: ListChip): string {
    return $localize`:@@list-head.remove:Remove the filter ${c.label}:filter:: ${c.value}:value:`;
  }

  protected moreText(): string {
    return $localize`:@@list-head.more:+${this.hidden()}:count: more`;
  }

  protected moreLabel(): string {
    return $localize`:@@list-head.more-label:Show all ${this.chips().length}:count: filters`;
  }

  /**
   * As many chips as fit beside the button stand in the row, in their order; when not all fit, the last place is the **+N more**
   * button. A hidden chip keeps its box out of the flow, so it can be measured when the row grows again.
   */
  private fit(): void {
    const box = this.chipBox()?.nativeElement;
    if (!box) return;
    const items = Array.from(box.querySelectorAll<HTMLElement>(':scope > li.chip-item:not(.chip-more-item)'));
    const more = box.querySelector<HTMLElement>(':scope > li.chip-more-item');
    const room = box.clientWidth;
    const gap = Number.parseFloat(getComputedStyle(box).columnGap) || 0;
    const widths = items.map((i) => i.getBoundingClientRect().width);
    const all = widths.reduce((a, w) => a + w, 0) + gap * Math.max(0, widths.length - 1);
    let count = widths.length;
    if (all > room + 0.5) {
      const moreWidth = more?.getBoundingClientRect().width ?? 0;
      let used = 0;
      count = 0;
      for (const w of widths) {
        const next = used + (count > 0 ? gap : 0) + w;
        if (next + gap + moreWidth > room + 0.5) break;
        used = next;
        count++;
      }
    }
    if (count !== this.shown()) this.shown.set(count);
  }
}
