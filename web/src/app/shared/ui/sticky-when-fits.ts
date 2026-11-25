import { DestroyRef, Directive, ElementRef, afterNextRender, inject } from '@angular/core';

/**
 * A side column that is no navigation (a policy's preview, an opened audit event): it sticks beside the page while the page scrolls,
 * but only while it fits in the view of the page, and it never scrolls by itself (R-UI-215, Q-UI-1112). Taller than the view, it
 * scrolls with the page, so every line of it can be read without a second scrollbar. The host gets the class `sticks` while it
 * fits; the screen's stylesheet makes `.sticks` sticky where the column stands beside the page.
 *
 * The view is the shell's `main`, the one thing of a page that scrolls, less its padding above and below. The first comparison is
 * made before the screen's own hooks after the render that shows the column (a focus given to it then finds it where it sticks, and
 * the page does not jump to its place at the top); later ones follow both heights, in the next frame: a change of the layout inside
 * the observer's callback would start a second round of every observer of the page.
 */
@Directive({
  selector: '[quaderStickyWhenFits]',
})
export class StickyWhenFits {
  constructor() {
    const host = inject<ElementRef<HTMLElement>>(ElementRef).nativeElement;
    let observer: ResizeObserver | undefined;
    let frame = 0;
    const measure = (main: HTMLElement): void => {
      const style = getComputedStyle(main);
      const room = main.clientHeight - (parseFloat(style.paddingTop) || 0) - (parseFloat(style.paddingBottom) || 0);
      host.classList.toggle('sticks', host.getBoundingClientRect().height <= room);
    };
    afterNextRender({
      earlyRead: () => {
        const main = host.closest('main');
        if (!main) return;
        measure(main);
        if (typeof ResizeObserver === 'undefined') return;
        observer = new ResizeObserver(() => {
          cancelAnimationFrame(frame);
          frame = requestAnimationFrame(() => measure(main));
        });
        observer.observe(host);
        observer.observe(main);
      },
    });
    inject(DestroyRef).onDestroy(() => {
      observer?.disconnect();
      if (frame) cancelAnimationFrame(frame);
    });
  }
}
