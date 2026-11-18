import { Component, signal } from '@angular/core';
import { TestBed } from '@angular/core/testing';
import { seriousViolations } from '../../testing/axe';
import { FilterCategory, FilterPanel } from './filter-panel';
import { ListChip, ListHead } from './list-head';

@Component({
  imports: [FilterPanel, FilterCategory, ListHead],
  template: `
    <main>
      <quader-list-head
        searchLabel="Filter things"
        [search]="search()"
        (searchChange)="searched.push($event); search.set($event)"
        [chips]="chips()"
        (removeChip)="removed.push($event.key)"
        [(filtersOpen)]="open"
        [controls]="panel.panelId"
      />
      <quader-filter-panel
        #panel
        [(open)]="open"
        [activeCount]="chips().length"
        applyLabel="Search"
        (apply)="applied = applied + 1"
        (clearAll)="cleared = cleared + 1"
        (dismissed)="dismissed = dismissed + 1"
      >
        <quader-filter-category label="Class">
          <input aria-label="Class value" />
        </quader-filter-category>
        <quader-filter-category label="Format">
          <input aria-label="Format value" />
        </quader-filter-category>
      </quader-filter-panel>
      <p class="page-content">The list</p>
    </main>
  `,
})
class Host {
  readonly open = signal(false);
  readonly search = signal('');
  readonly chips = signal<readonly ListChip[]>([]);
  readonly searched: string[] = [];
  readonly removed: string[] = [];
  applied = 0;
  cleared = 0;
  dismissed = 0;
}

async function render() {
  TestBed.resetTestingModule();
  await TestBed.configureTestingModule({ imports: [Host] }).compileComponents();
  const fixture = TestBed.createComponent(Host);
  document.body.appendChild(fixture.nativeElement);
  await fixture.whenStable();
  const root = fixture.nativeElement as HTMLElement;
  const settle = async () => {
    for (let i = 0; i < 4; i++) {
      fixture.detectChanges();
      await fixture.whenStable();
      await new Promise<void>((r) => {
        const channel = new MessageChannel();
        channel.port1.onmessage = () => {
          channel.port1.close();
          r();
        };
        channel.port2.postMessage(0);
      });
    }
  };
  await settle();
  return { fixture, root, host: fixture.componentInstance, settle };
}

const toggle = (root: HTMLElement) => root.querySelector<HTMLButtonElement>('button.filter-toggle')!;
const drawer = () => document.querySelector<HTMLElement>('.quader-drawer [role="dialog"]');

describe('Filter drawer and the head of a list', () => {
  afterEach(() => document.body.replaceChildren());

  it('R-UI-101 Q-UI-420 R-UI-044 the drawer is closed until Filters opens it: a labelled modal dialog over the page, the list untouched', async () => {
    const { root, settle } = await render();
    expect(drawer()).toBeNull();
    expect(toggle(root).getAttribute('aria-expanded')).toBe('false');
    expect(toggle(root).getAttribute('aria-haspopup')).toBe('dialog');

    toggle(root).click();
    await settle();
    const dialog = drawer()!;
    expect(dialog).toBeTruthy();
    expect(dialog.getAttribute('aria-modal')).toBe('true');
    const title = document.getElementById(dialog.getAttribute('aria-labelledby')!)!;
    expect(title.textContent?.trim()).toBe('Filters');
    expect(toggle(root).getAttribute('aria-expanded')).toBe('true');
    expect(toggle(root).getAttribute('aria-controls')).toBe(dialog.id);
    // The drawer stands in the overlay, outside the page: the list keeps its place and its width.
    expect(root.contains(dialog)).toBe(false);
    expect(root.querySelector('.page-content')).toBeTruthy();
    expect(dialog.querySelector('form')?.getAttribute('role')).toBe('search');
    expect(await seriousViolations(dialog)).toEqual([]);
  });

  it('R-UI-101 R-UI-044 focus moves to the drawer\'s heading, and its close button returns it to Filters', async () => {
    const { root, host, settle } = await render();
    toggle(root).focus();
    toggle(root).click();
    await settle();
    expect(document.activeElement?.classList.contains('drawer-title')).toBe(true);

    drawer()!.querySelector<HTMLButtonElement>('button.drawer-close')!.click();
    await settle();
    expect(drawer()).toBeNull();
    expect(host.open()).toBe(false);
    expect(document.activeElement).toBe(toggle(root));
    expect(host.dismissed).toBe(1);
  });

  it('R-UI-101 R-UI-044 Escape closes the drawer without applying it', async () => {
    const { host, settle } = await render();
    host.open.set(true);
    await settle();
    drawer()!.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }));
    await settle();
    expect(host.open()).toBe(false);
    expect(drawer()).toBeNull();
    expect(host.applied).toBe(0);
    expect(host.dismissed).toBe(1);
  });

  it('R-UI-101 Q-UI-423 the filters stand open, each a group named by its legend, no folding sections', async () => {
    const { host, settle } = await render();
    host.open.set(true);
    await settle();
    const legends = [...drawer()!.querySelectorAll('fieldset > legend')].map((l) => l.textContent?.trim());
    expect(legends).toEqual(['Class', 'Format']);
    expect(drawer()!.querySelector('[aria-expanded]')).toBeNull();
    expect(drawer()!.querySelector<HTMLInputElement>('input[aria-label="Format value"]')).toBeTruthy();
  });

  it('R-UI-020 "Clear all" is disabled without a set filter and asks the screen to clear them', async () => {
    const { root, host, settle } = await render();
    host.open.set(true);
    await settle();
    const clear = drawer()!.querySelector<HTMLButtonElement>('button.clear-all')!;
    expect(clear.disabled).toBe(true);
    host.chips.set([
      { key: 'a', label: 'Class', value: 'Local' },
      { key: 'b', label: 'Format', value: 'npm' },
    ]);
    await settle();
    expect(clear.disabled).toBe(false);
    clear.click();
    expect(host.cleared).toBe(1);
    expect(toggle(root).textContent).toContain('2');
  });

  it('R-UI-101 Q-UI-420 the apply button submits the form once, closes the drawer and reports no dismissal', async () => {
    const { host, settle } = await render();
    host.open.set(true);
    await settle();
    const apply = [...drawer()!.querySelectorAll<HTMLButtonElement>('button[type="submit"]')].find((b) => b.textContent?.trim() === 'Search')!;
    apply.click();
    await settle();
    expect(host.applied).toBe(1);
    expect(host.open()).toBe(false);
    expect(drawer()).toBeNull();
    expect(host.dismissed).toBe(0);
  });

  it('R-UI-100 Q-UI-422 a set filter is a chip beside Filters with its name, its value and a button that removes it', async () => {
    const { root, host, settle } = await render();
    host.chips.set([
      { key: 'format', label: 'Format', value: 'OCI (Docker)' },
      { key: 'class', label: 'Class', value: 'Remote' },
    ]);
    await settle();
    const chips = [...root.querySelectorAll('.list-filters .chip-item:not(.chip-more-item)')];
    expect(chips.map((c) => c.querySelector('.chip-text')?.textContent?.replace(/\s+/g, ' ').trim())).toEqual([
      'Format: OCI (Docker)',
      'Class: Remote',
    ]);
    const remove = chips[1]!.querySelector('button')!;
    expect(remove.getAttribute('aria-label')).toBe('Remove the filter Class: Remote');
    remove.click();
    expect(host.removed).toEqual(['class']);
    // Everything fits in a test's DOM, which measures nothing: no "+N more".
    expect(root.querySelector('.chip-more-item')?.classList.contains('chip-hidden')).toBe(true);
    expect(await seriousViolations(root)).toEqual([]);
  });

  it('R-UI-100 Q-UI-421 the search field filters 250 ms after the last key, at once on Enter, and shows a text the list applies', async () => {
    vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] });
    try {
      const { root, host, settle } = await render();
      const input = root.querySelector<HTMLInputElement>('.list-search input')!;
      expect(input.getAttribute('aria-label')).toBe('Filter things');
      input.value = 'np';
      input.dispatchEvent(new Event('input'));
      vi.advanceTimersByTime(100);
      input.value = 'npm ';
      input.dispatchEvent(new Event('input'));
      vi.advanceTimersByTime(249);
      expect(host.searched).toEqual([]);
      vi.advanceTimersByTime(1);
      expect(host.searched).toEqual(['npm']);

      input.value = 'npm-internal';
      input.dispatchEvent(new Event('input'));
      input.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter' }));
      expect(host.searched).toEqual(['npm', 'npm-internal']);

      // A text the list applies from elsewhere (a link, a cleared filter) replaces what the field shows.
      host.search.set('images');
      await settle();
      expect(input.value).toBe('images');
    } finally {
      vi.useRealTimers();
    }
  });
});
