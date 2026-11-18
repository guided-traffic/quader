import { seriousViolations } from '../../testing/axe';
import { buttonByText, choosePrincipal, openFilters, renderScreen, settle, text, typeInto, chipTexts } from '../../testing/screen';
import { IdentityApi } from '../../api/identity-api';
import { FakeIdentityApi } from '../../api/testing/access-fakes';
import { UserList } from './user-list';

describe('Users list table and filter panel (R-UI-024, R-UI-020)', () => {
  it('R-UI-024 the user name header sorts through the API and flips the order', async () => {
    const { fixture, root } = await renderScreen(UserList);
    const header = root.querySelector('th[aria-sort]')!;
    expect(header.getAttribute('aria-sort')).toBe('ascending');
    expect(text(root.querySelector('tbody tr')!)).toContain('alice');

    header.querySelector<HTMLButtonElement>('button.th-sort')!.click();
    await settle(fixture);
    expect(root.querySelector('th[aria-sort]')!.getAttribute('aria-sort')).toBe('descending');
    expect(text(root.querySelector('tbody tr')!)).toContain('bob');
  });

  it('R-UI-024 only the column the API can sort is a sort control', async () => {
    const { root } = await renderScreen(UserList);
    expect(root.querySelectorAll('th button.th-sort')).toHaveLength(1);
  });

  it('R-UI-024 Q-UI-422 the applied filters are chips, counted on Filters; "Clear all" resets them and the list', async () => {
    const { fixture, root } = await renderScreen(UserList);
    let drawer = await openFilters(fixture, root);
    typeInto(drawer, 'Status', 'disabled');
    buttonByText(drawer, 'Apply filters').click();
    await settle(fixture);
    expect(root.querySelector('button.filter-toggle .filter-count')?.textContent).toBe('1');
    expect(chipTexts(root)).toEqual(['Status: Disabled']);

    drawer = await openFilters(fixture, root);
    buttonByText(drawer, 'Clear all').click();
    await settle(fixture);
    expect(root.querySelector('button.filter-toggle .filter-count')).toBeNull();
    expect(root.querySelectorAll('tbody tr')).toHaveLength(2);
  });

  it('Q-UI-422 removing a chip removes only its filter; the drawer closed without Apply keeps the applied filters', async () => {
    const { fixture, root } = await renderScreen(UserList);
    let drawer = await openFilters(fixture, root);
    typeInto(drawer, 'Status', 'active');
    typeInto(drawer, 'Origin', 'admin');
    buttonByText(drawer, 'Apply filters').click();
    await settle(fixture);
    expect(chipTexts(root)).toEqual(['Status: Active', 'Origin: Created by an administrator']);

    drawer = await openFilters(fixture, root);
    typeInto(drawer, 'Status', 'disabled');
    drawer.querySelector<HTMLButtonElement>('button.drawer-close')!.click();
    await settle(fixture);
    drawer = await openFilters(fixture, root);
    expect(drawer.querySelector<HTMLSelectElement>('select[name="status"]')!.value).toBe('active');
    drawer.querySelector<HTMLButtonElement>('button.drawer-close')!.click();
    await settle(fixture);

    root.querySelector<HTMLButtonElement>('button[aria-label="Remove the filter Status: Active"]')!.click();
    await settle(fixture);
    expect(chipTexts(root)).toEqual(['Origin: Created by an administrator']);
  });

  it('R-UI-044 the open filter drawer passes the accessibility check', async () => {
    const { fixture, root } = await renderScreen(UserList);
    const drawer = await openFilters(fixture, root);
    expect(await seriousViolations(drawer)).toEqual([]);
    expect(await seriousViolations(root)).toEqual([]);
  });

  it('R-UI-150 the group filter is chosen from the search and sent by the group\'s name', async () => {
    const { fixture, root, get } = await renderScreen(UserList);
    const api = get(IdentityApi) as FakeIdentityApi;
    const seen: (string | undefined)[] = [];
    const users = api.users.bind(api);
    api.users = (filters) => {
      seen.push(filters.group);
      return users(filters);
    };
    const drawer = await openFilters(fixture, root);
    await choosePrincipal(fixture, drawer, 'Group', 'leads', 'web-leads');
    buttonByText(drawer, 'Apply filters').click();
    await settle(fixture);
    expect(seen.at(-1)).toBe('web-leads');
    expect(chipTexts(root)).toEqual(['Group: web-leads']);
  });
});
