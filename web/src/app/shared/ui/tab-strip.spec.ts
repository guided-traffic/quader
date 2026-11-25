import { Component, signal } from '@angular/core';
import { TestBed } from '@angular/core/testing';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { seriousViolations } from '../../testing/axe';
import { settle } from '../../testing/screen';
import { TAB_FADE, TAB_TIGHT, TabStrip } from './tab-strip';
import { TabGroup, TabPage } from './tabs';

/**
 * jsdom lays nothing out, so the strip's geometry is a model: every tab is 100 px wide, 92 px in a strip that takes its tabs closer
 * (`tight`: 4 px less padding at each side), side by side from the strip's left edge, and the strip is `width` px wide; `scrollLeft`
 * is kept per element and clamped as a browser clamps it.
 */
const TAB = 100;
const TIGHT = 92;
let width = 250;
const scrolled = new WeakMap<Element, number>();
const isStrip = (el: Element): boolean => el.classList.contains('tab-nav');
const tabsOf = (strip: Element): Element[] => [...strip.children].filter((c) => !c.classList.contains('tab-strip-fade'));
const tabWidth = (strip: Element): number => (strip.classList.contains(TAB_TIGHT) ? TIGHT : TAB);
const stripWidth = (strip: Element): number => tabsOf(strip).length * tabWidth(strip);

beforeEach(() => {
  width = 250;
  vi.spyOn(HTMLElement.prototype, 'clientWidth', 'get').mockImplementation(function (this: HTMLElement) {
    return isStrip(this) ? width : 0;
  });
  vi.spyOn(HTMLElement.prototype, 'scrollWidth', 'get').mockImplementation(function (this: HTMLElement) {
    return isStrip(this) ? Math.max(width, stripWidth(this)) : 0;
  });
  vi.spyOn(HTMLElement.prototype, 'offsetWidth', 'get').mockImplementation(function (this: HTMLElement) {
    return this.parentElement && isStrip(this.parentElement) ? tabWidth(this.parentElement) : 0;
  });
  vi.spyOn(Element.prototype, 'scrollLeft', 'get').mockImplementation(function (this: Element) {
    return scrolled.get(this) ?? 0;
  });
  vi.spyOn(Element.prototype, 'scrollLeft', 'set').mockImplementation(function (this: Element, v: number) {
    scrolled.set(this, Math.max(0, Math.min(v, Math.max(0, stripWidth(this) - width))));
  });
  vi.spyOn(Element.prototype, 'getBoundingClientRect').mockImplementation(function (this: Element) {
    const strip = this.parentElement;
    const tab = strip && isStrip(strip) ? tabWidth(strip) : TAB;
    const left = strip && isStrip(strip) ? tabsOf(strip).indexOf(this) * tab - (scrolled.get(strip) ?? 0) : 0;
    return DOMRect.fromRect({ x: left, y: 0, width: tab, height: 48 });
  });
});

afterEach(() => vi.restoreAllMocks());

@Component({
  imports: [TabStrip],
  template: `
    <nav quaderTabStrip class="tab-nav" aria-label="Views">
      @for (t of tabs; track t; let i = $index) {
        <a class="tab-link" href="#{{ t }}" [attr.aria-current]="i === current() ? 'page' : null">{{ t }}</a>
      }
    </nav>
  `,
})
class Links {
  readonly tabs = ['Files', 'Signatures', 'Details', 'Vulnerabilities', 'Documentation'];
  readonly current = signal(0);
}

async function render(current = 0) {
  await TestBed.configureTestingModule({ imports: [Links] }).compileComponents();
  const fixture = TestBed.createComponent(Links);
  fixture.componentInstance.current.set(current);
  document.body.appendChild(fixture.nativeElement);
  await settle(fixture);
  const root = fixture.nativeElement as HTMLElement;
  const strip = root.querySelector<HTMLElement>('.tab-nav')!;
  const fade = (edge: 'start' | 'end') => strip.querySelector<HTMLButtonElement>(`.tab-strip-fade.${edge}`)!;
  const shown = () => (['start', 'end'] as const).filter((e) => !fade(e).hidden);
  return { fixture, root, strip, fade, shown };
}

describe('Tab strip', () => {
  it('R-UI-173 a strip whose tabs fit shows no fade, and the fades add no text, no tab stop and nothing for a screen reader', async () => {
    width = 600;
    const { root, strip, fade, shown } = await render();
    expect(shown()).toEqual([]);
    expect(strip.firstElementChild).toBe(fade('start'));
    expect(strip.lastElementChild).toBe(fade('end'));
    for (const f of [fade('start'), fade('end')]) {
      expect(f.tabIndex).toBe(-1);
      expect(f.getAttribute('aria-hidden')).toBe('true');
      expect(f.textContent).toBe('');
    }
    expect(strip.textContent).not.toContain('chevron');
    expect(await seriousViolations(root)).toEqual([]);
  });

  it('R-UI-173 a strip whose tabs fit only closer together takes them closer and does not scroll; one that fits keeps their padding', async () => {
    // Five tabs: 500 px at their own padding, 460 px closer together.
    width = 480;
    const first = await render();
    expect(first.strip.classList.contains(TAB_TIGHT)).toBe(true);
    expect(first.strip.scrollWidth).toBe(width);
    expect(first.shown()).toEqual([]);
    TestBed.resetTestingModule();
    width = 500;
    const second = await render();
    expect(second.strip.classList.contains(TAB_TIGHT)).toBe(false);
    expect(second.shown()).toEqual([]);
  });

  it('R-UI-173 a strip wider than its place shows a fade at each edge beyond which tabs lie, and a click on one scrolls it', async () => {
    const { fixture, strip, fade, shown } = await render();
    expect(shown()).toEqual(['end']);
    fade('end').click();
    await settle(fixture);
    expect(strip.scrollLeft).toBe(width - 2 * TAB_FADE);
    expect(shown()).toEqual(['start', 'end']);
    fade('end').click();
    await settle(fixture);
    expect(strip.scrollLeft).toBe(stripWidth(strip) - width);
    expect(shown()).toEqual(['start']);
    fade('start').click();
    fade('start').click();
    await settle(fixture);
    expect(strip.scrollLeft).toBe(0);
    expect(shown()).toEqual(['end']);
  });

  it('R-UI-173 the current tab is scrolled clear of the fade when the strip appears and when the current tab changes', async () => {
    const { fixture, strip } = await render(3);
    // Vulnerabilities spans 276 to 368 px in the strip that takes its tabs closer: its right edge and the fade's width at the strip's
    // right edge.
    expect(strip.classList.contains(TAB_TIGHT)).toBe(true);
    expect(strip.scrollLeft).toBe(4 * TIGHT + TAB_FADE - width);
    fixture.componentInstance.current.set(0);
    await settle(fixture);
    expect(strip.scrollLeft).toBe(0);
  });

  it('R-UI-173 a tab reached by the keyboard is scrolled clear of the fade', async () => {
    const { strip } = await render();
    const tab = strip.querySelectorAll<HTMLElement>('a.tab-link')[2]!;
    tab.focus();
    expect(strip.scrollLeft).toBe(3 * TIGHT + TAB_FADE - width);
  });

  it('R-UI-044 the tabs of a tab group scroll the same way and stay a tablist of tabs alone for assistive technology', async () => {
    @Component({
      imports: [TabGroup, TabPage],
      template: `
        <quader-tab-group [selectedIndex]="3">
          @for (t of tabs; track t) {
            <quader-tab [label]="t"><p>{{ t }} body</p></quader-tab>
          }
        </quader-tab-group>
      `,
    })
    class Group {
      readonly tabs = ['Files', 'Signatures', 'Details', 'Vulnerabilities'];
    }
    await TestBed.configureTestingModule({ imports: [Group] }).compileComponents();
    const fixture = TestBed.createComponent(Group);
    document.body.appendChild(fixture.nativeElement);
    await settle(fixture);
    const root = fixture.nativeElement as HTMLElement;
    const list = root.querySelector<HTMLElement>('[role="tablist"]')!;
    // The last tab: scrolled to the end, where no fade stands.
    expect(list.scrollLeft).toBe(4 * TIGHT - width);
    expect(list.querySelector('.tab-strip-fade.end')?.hasAttribute('hidden')).toBe(true);
    expect(list.querySelector('.tab-strip-fade.start')?.hasAttribute('hidden')).toBe(false);
    expect(await seriousViolations(root)).toEqual([]);
  });
});
