import { Router } from '@angular/router';
import { addressValues, applyFilters, button, chipTexts, expectAccessible, filterDrawer, openFilters, renderScreen, searchList, settle, type } from '../../testing/screen';
import { RepositoryList } from './repository-list';
import { BrowserStorage } from '../../core/storage';

async function open(inputs: Record<string, unknown> = {}) {
  const rendered = await renderScreen(RepositoryList, {
    inputs,
    setup: (get) => void vi.spyOn(get(Router), 'navigate').mockResolvedValue(true),
  });
  return { ...rendered, router: rendered.get(Router) };
}

describe('Repository list head and filter drawer (R-UI-020)', () => {
  it('R-UI-020 Q-UI-420 the drawer is closed until Filters opens it; it holds project, format, class and state, and passes axe', async () => {
    const { fixture, root } = await open();
    expect(filterDrawer()).toBeNull();
    const drawer = await openFilters(fixture, root);
    const legends = [...drawer.querySelectorAll('fieldset > legend')].map((l) => l.textContent?.trim());
    expect(legends).toEqual(['Project', 'Format', 'Class', 'State']);
    await expectAccessible(drawer);
    await expectAccessible(root);
  });

  it('R-UI-002 Q-UI-420 the head is one row above the list (search, Filters), and no "Recently opened" stands above it', async () => {
    const { root } = await renderScreen(RepositoryList, {
      setup: (get) => {
        vi.spyOn(get(Router), 'navigate').mockResolvedValue(true);
        new BrowserStorage().set(
          'local',
          'quader.frequent.00000000-0000-0000-0000-000000000001',
          JSON.stringify({ repositories: [{ id: 'web/generic-local', visits: 1, last: Date.now() }], projects: [] }),
        );
      },
    });
    const head = root.querySelector('quader-list-head')!;
    expect(head.querySelector('.list-search input')?.getAttribute('aria-label')).toBe('Filter repositories');
    expect(head.querySelector('button.filter-toggle')).toBeTruthy();
    // The head stands before the table in the page.
    expect(head.compareDocumentPosition(root.querySelector('quader-data-table')!) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
    expect(root.textContent).not.toContain('Recently opened');
  });

  it('Q-UI-421 the search field puts the key or description into the query', async () => {
    const { fixture, root, router } = await open();
    await searchList(fixture, root, ' npm ');
    const [, extras] = (router.navigate as ReturnType<typeof vi.fn>).mock.calls.at(-1)!;
    expect(extras.queryParams).toEqual({ q: 'npm' });
    expect(extras.queryParamsHandling).toBe('merge');
  });

  it('R-UI-020 Apply in the drawer puts every staged value into the query parameters at once and closes it', async () => {
    const { fixture, root, router } = await open();
    const drawer = await openFilters(fixture, root);
    type(drawer.querySelector<HTMLSelectElement>('select[aria-label="Class"]')!, 'remote');
    drawer.querySelector<HTMLInputElement>('input[type="checkbox"]')!.click();
    await settle(fixture);
    await applyFilters(fixture);
    const [, extras] = (router.navigate as ReturnType<typeof vi.fn>).mock.calls.at(-1)!;
    expect(extras.queryParams).toEqual({ class: 'remote', format: null, state: 'inactive', project: null });
    expect(extras.queryParamsHandling).toBe('merge');
    expect(filterDrawer()).toBeNull();
  });

  it('Q-UI-422 an applied filter is a chip beside Filters, counted on the button, and the drawer stays closed', async () => {
    const { root } = await open({ class: 'local', format: 'generic' });
    expect(filterDrawer()).toBeNull();
    expect(root.querySelector('button.filter-toggle .filter-count')?.textContent).toBe('2');
    expect(chipTexts(root)).toEqual(['Format: Generic', 'Class: Local']);
  });

  it('Q-UI-422 removing a chip removes only its filter', async () => {
    const { fixture, root, router } = await open({ class: 'local', format: 'generic' });
    root.querySelector<HTMLButtonElement>('button[aria-label="Remove the filter Class: Local"]')!.click();
    await settle(fixture);
    const [, extras] = (router.navigate as ReturnType<typeof vi.fn>).mock.calls.at(-1)!;
    expect(extras.queryParams).toEqual({ class: null });
  });

  it('R-UI-020 "Clear all" removes every filter of the drawer from the query and keeps the search text', async () => {
    const { fixture, root, router } = await open({ class: 'local', q: 'npm' });
    const drawer = await openFilters(fixture, root);
    button(drawer, 'Clear all').click();
    await settle(fixture);
    const [, extras] = (router.navigate as ReturnType<typeof vi.fn>).mock.calls.at(-1)!;
    expect(extras.queryParams).toEqual({ class: null, format: null, state: null, project: null });
  });

  it('R-UI-020 the repository table has no sort control because the API takes no sort parameter, and explains the class', async () => {
    const { root } = await open();
    expect(root.querySelectorAll('th button.th-sort')).toHaveLength(0);
    // The admin area's list has no address column (Q-UI-330): the class is the column to explain.
    expect([...root.querySelectorAll('th button.th-help')].map((b) => b.getAttribute('aria-label'))).toEqual([
      'About the column Class',
    ]);
  });

  it('R-REPO-002 R-UI-020 R-UI-066 an absolute client address is copied unchanged, without the origin of the page in front', async () => {
    const { root } = await open();
    expect(addressValues(root)).toEqual([]);
    const write = vi.fn().mockResolvedValue(undefined);
    Object.defineProperty(navigator, 'clipboard', { value: { writeText: write }, configurable: true });
    const copy = root.querySelector<HTMLButtonElement>('td.actions quader-copy-button button')!;
    expect(copy.getAttribute('aria-label')).toBe('Copy client address https://quader.example.net/files/web/generic-local/');
    copy.click();
    await Promise.resolve();
    expect(write).toHaveBeenCalledWith('https://quader.example.net/files/web/generic-local/');
  });
});
