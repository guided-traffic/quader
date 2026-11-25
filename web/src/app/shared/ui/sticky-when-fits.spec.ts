import { Component } from '@angular/core';
import { TestBed } from '@angular/core/testing';
import { StickyWhenFits } from './sticky-when-fits';

@Component({
  imports: [StickyWhenFits],
  template: `
    <main style="padding: 24px 0">
      <aside quaderStickyWhenFits>Preview</aside>
    </main>
  `,
})
class Host {}

describe('Sticky when it fits', () => {
  afterEach(() => {
    vi.restoreAllMocks();
    vi.unstubAllGlobals();
  });

  it('R-UI-215 a side column sticks while it fits in the view of the page, less its padding, and stops once it is taller', async () => {
    // jsdom has no layout and no ResizeObserver: the view is 600 px with 24 px of padding above and below, and a stand-in observer
    // hands the column's new height over.
    const observers: { callback: ResizeObserverCallback; observed: Element[] }[] = [];
    vi.stubGlobal(
      'ResizeObserver',
      class {
        readonly observed: Element[] = [];
        constructor(readonly callback: ResizeObserverCallback) {
          observers.push(this);
        }
        observe(el: Element): void {
          this.observed.push(el);
        }
        readonly disconnect = vi.fn();
      },
    );
    let height = 552;
    vi.spyOn(HTMLElement.prototype, 'clientHeight', 'get').mockImplementation(function (this: HTMLElement) {
      return this.tagName === 'MAIN' ? 600 : 0;
    });
    vi.spyOn(Element.prototype, 'getBoundingClientRect').mockImplementation(
      () => ({ height, width: 10, top: 0, left: 0, right: 10, bottom: height, x: 0, y: 0, toJSON: () => ({}) }) as DOMRect,
    );
    const fixture = TestBed.createComponent(Host);
    await fixture.whenStable();
    const aside = (fixture.nativeElement as HTMLElement).querySelector('aside')!;
    // 552 px in a view of 600 less 48: it fits, at once after the render that showed it.
    expect(aside.classList.contains('sticks')).toBe(true);
    const observer = observers.at(-1)!;
    expect(observer.observed.map((e) => e.tagName)).toEqual(['ASIDE', 'MAIN']);
    height = 553;
    observer.callback([], observer as unknown as ResizeObserver);
    await new Promise((r) => requestAnimationFrame(() => r(undefined)));
    expect(aside.classList.contains('sticks')).toBe(false);
  });
});
