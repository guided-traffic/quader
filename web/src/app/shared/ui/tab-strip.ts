import { DestroyRef, Directive, ElementRef, afterNextRender, inject } from '@angular/core';

/** How far a fade reaches into the strip, in px: GitLab's 2rem. A tab is scrolled clear of it. */
export const TAB_FADE = 32;

/** The current tab of a strip: a link's `aria-current`, a tab's `aria-selected`. */
const CURRENT = ':scope > [aria-current="page"], :scope > [aria-selected="true"]';

/**
 * Every tab strip of Quader where its tabs are wider than the window (Q-UI-1074): the links of `<nav quaderTabStrip class="tab-nav">`
 * and the tablist of `quader-tab-group`; the web lint refuses an element of the class `tab-nav` without it (tab-strip-rule.mjs). The strip scrolls
 * sideways without a scrollbar (`.tab-nav` of _controls.scss); at an edge beyond which tabs lie stands a fade with a chevron,
 * as GitLab's scrolling tabs have it, and a click on it scrolls the strip by its width less the two fades. The current tab is
 * scrolled clear of the fades when the strip appears, when its width changes and when the current tab changes; a
 * focused tab is scrolled clear of them too, so Tab and the arrow keys always show the tab they reach.
 *
 * The fades are for the pointer: out of the tab order and hidden from assistive technology, which reach every tab without them.
 * They stand inside the strip, sticky at its edges and taking no room, so a strip needs no wrapper and a tablist stays one.
 */
@Directive({ selector: '[quaderTabStrip]' })
export class TabStrip {
  private readonly strip = inject<ElementRef<HTMLElement>>(ElementRef).nativeElement;
  private readonly start = fade('start');
  private readonly end = fade('end');

  constructor() {
    const destroyRef = inject(DestroyRef);
    afterNextRender(() => {
      // After the first render, so the strip's own children are in place: Angular inserts later tabs before their anchors, which
      // stand between the two fades.
      this.strip.prepend(this.start);
      this.strip.append(this.end);
      this.start.addEventListener('click', () => this.page(-1));
      this.end.addEventListener('click', () => this.page(1));
      const settle = (): void => {
        this.update();
        this.reveal(this.strip.querySelector<HTMLElement>(CURRENT));
      };
      const onScroll = (): void => this.update();
      const onFocus = (e: FocusEvent): void => {
        if (e.target instanceof HTMLElement && e.target.parentElement === this.strip) this.reveal(e.target);
      };
      this.strip.addEventListener('scroll', onScroll, { passive: true });
      this.strip.addEventListener('focusin', onFocus);
      // A changed label (a count that arrives) moves the fades only; a new current tab is scrolled to, and a strip the reader
      // scrolled stays where it is until then.
      const mutations = new MutationObserver((records) => (records.some((r) => r.type === 'attributes') ? settle() : this.update()));
      mutations.observe(this.strip, { subtree: true, childList: true, characterData: true, attributeFilter: ['aria-current', 'aria-selected'] });
      const resize = typeof ResizeObserver === 'undefined' ? null : new ResizeObserver(settle);
      resize?.observe(this.strip);
      // The labels' font arrives after the first layout and widens them.
      void document.fonts?.ready.then(settle);
      settle();
      destroyRef.onDestroy(() => {
        mutations.disconnect();
        resize?.disconnect();
        this.strip.removeEventListener('scroll', onScroll);
        this.strip.removeEventListener('focusin', onFocus);
      });
    });
  }

  /** Shows a fade at each edge beyond which tabs lie. */
  private update(): void {
    const { scrollLeft, clientWidth, scrollWidth } = this.strip;
    this.start.hidden = scrollLeft <= 1;
    this.end.hidden = scrollLeft + clientWidth >= scrollWidth - 1;
  }

  /** Scrolls the strip so that a tab of it stands clear of the fades; nothing when it does. */
  private reveal(tab: HTMLElement | null): void {
    if (!tab || this.strip.scrollWidth <= this.strip.clientWidth) return;
    const left = tab.getBoundingClientRect().left - this.strip.getBoundingClientRect().left + this.strip.scrollLeft;
    const right = left + tab.offsetWidth;
    const { scrollLeft, clientWidth } = this.strip;
    let to = scrollLeft;
    if (left - TAB_FADE < scrollLeft) to = left - TAB_FADE;
    else if (right + TAB_FADE > scrollLeft + clientWidth) to = right + TAB_FADE - clientWidth;
    if (to !== scrollLeft) this.scroll(to, false);
  }

  /** One page towards an edge: the strip's width less the two fades, so a tab cut by a fade stands whole afterwards. */
  private page(direction: -1 | 1): void {
    this.scroll(this.strip.scrollLeft + direction * Math.max(TAB_FADE, this.strip.clientWidth - 2 * TAB_FADE), true);
  }

  private scroll(to: number, smooth: boolean): void {
    const max = this.strip.scrollWidth - this.strip.clientWidth;
    const left = Math.max(0, Math.min(max, Math.round(to)));
    const reduced = typeof matchMedia === 'function' && matchMedia('(prefers-reduced-motion: reduce)').matches;
    if (typeof this.strip.scrollTo === 'function') this.strip.scrollTo({ left, behavior: smooth && !reduced ? 'smooth' : 'instant' });
    else this.strip.scrollLeft = left;
    this.update();
  }
}

/** A fade of an edge: a button for the pointer, its chevron drawn by the stylesheet, so it adds no text to the strip. */
function fade(edge: 'start' | 'end'): HTMLButtonElement {
  const button = document.createElement('button');
  button.type = 'button';
  button.className = `tab-strip-fade ${edge}`;
  button.tabIndex = -1;
  button.hidden = true;
  button.setAttribute('aria-hidden', 'true');
  // A click keeps the focus where it is: the fade is not a place for it.
  button.addEventListener('mousedown', (e) => e.preventDefault());
  return button;
}
