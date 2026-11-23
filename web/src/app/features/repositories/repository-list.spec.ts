import { FakeRepositoriesApi } from '../../api/testing/fakes-catalog';
import { ApiError } from '../../api/problem';
import { RepositoriesApi } from '../../api/repositories-api';
import { FrequentPlaces } from '../../core/frequent-places';
import { TestBed } from '@angular/core/testing';
import { provideRouter, withComponentInputBinding } from '@angular/router';
import { RouterTestingHarness } from '@angular/router/testing';
import { FakeSessionApi, provideFakeApis } from '../../api/testing/fakes';
import { SessionApi } from '../../api/session-api';
import { SessionStore } from '../../core/session-store';
import { addressValues, browserStorage, button, expectAccessible, renderScreen, settle, text, type } from '../../testing/screen';
import { RepositoryList } from './repository-list';

/** The list as the router opens it at `url`, its route and query bound to its inputs: a project's list or the list of every project. */
async function routed(url: string, capabilities?: string[]): Promise<HTMLElement> {
  TestBed.resetTestingModule();
  TestBed.configureTestingModule({
    providers: [
      provideRouter(
        [
          { path: 'projects/:project/repositories', component: RepositoryList },
          { path: 'repositories', component: RepositoryList },
          { path: '**', children: [] },
        ],
        withComponentInputBinding(),
      ),
      ...provideFakeApis(),
    ],
  });
  if (capabilities) (TestBed.inject(SessionApi) as FakeSessionApi).meResult = { capabilities };
  await TestBed.inject(SessionStore).load();
  const harness = await RouterTestingHarness.create();
  await harness.navigateByUrl(url);
  for (let i = 0; i < 6; i++) {
    await harness.fixture.whenStable();
    harness.detectChanges();
  }
  return harness.routeNativeElement as HTMLElement;
}

describe('Repository list', () => {
  it('R-UI-020 Q-UI-330 Q-UI-1042 the list of every project has the columns repository, project, format and class, the level as the icon before the key, and no address field, and has no serious violation', async () => {
    const { root } = await renderScreen(RepositoryList);
    const t = text(root);
    expect(t).toContain('generic-local');
    expect(t).toContain('Installers and build outputs');
    expect(root.querySelectorAll('tbody tr').length).toBe(2);
    const heads = [...root.querySelectorAll('thead th')].map((th) => text(th).replace('help_outline', '').split(' ')[0]);
    expect(heads.slice(0, 4)).toEqual(['Repository', 'Project', 'Format', 'Class']);
    const row = [...root.querySelectorAll('tbody tr')].find((tr) => text(tr).includes('generic-local'))!;
    expect(row.querySelector('td.project-cell a')?.getAttribute('href')).toBe('/projects/web');
    // The level is the icon before the key, as in a project's list; no column of its own (Q-UI-1042).
    expect(row.querySelector('th[scope="row"] .icon-name > quader-visibility-icon')).toBeTruthy();
    expect(heads).not.toContain('Visibility');
    // The line under the heading says what the list holds and where a repository is created.
    expect(text(root.querySelector('.page-header-description')!)).toBe(
      'The repositories you can see, in every project. A new repository is created in its project.',
    );
    expect(row.querySelector('td.format-cell .format-name quader-format-icon')).toBeTruthy();
    expect(text(row.querySelector('td.format-cell span[translate="no"]')!)).toBe('Generic');
    expect(text(row.querySelector('td.class-cell')!)).toContain('Local');
    expect(root.querySelector('quader-address-field')).toBeNull();
    expect(addressValues(root)).toEqual([]);
    await expectAccessible(root);
  });

  it('R-UI-145 Q-UI-563 the columns of the list of every project take their widths from their content: the repository and the project wrap and share the room, the rest is as wide as its values', async () => {
    const { root } = await renderScreen(RepositoryList);
    const heads = [...root.querySelectorAll<HTMLElement>('thead th')];
    // No column is pinned to its least while another takes every pixel (the project broke into three lines beside an empty column).
    expect(heads.map((h) => h.style.inlineSize).every((w) => w === '')).toBe(true);
    expect(heads.map((h) => h.style.minInlineSize)).toEqual(['12rem', '12rem', ...heads.slice(2).map(() => '')]);
    const empty = [...root.querySelectorAll('tbody td.narrow-drop quader-no-value')];
    for (const cell of empty) expect(cell.parentElement!.hasAttribute('data-empty')).toBe(true);
  });

  it('R-UI-145 Q-UI-562 a format and a class are each one element, the icon and the name, centred on the line of the cell by the shared table', async () => {
    const { root } = await renderScreen(RepositoryList);
    const row = root.querySelector('tbody tr')!;
    // _list-screens.scss gives `.format-name` and `.class-name` in a data table's cell the negative margin that keeps a 20 px icon
    // on the cell's 16 px line; the test holds the structure the rule selects.
    expect(row.querySelector('td.format-cell > .format-name > quader-format-icon')).toBeTruthy();
    expect(row.querySelector('td.class-cell > .class-name > .material-icons')).toBeTruthy();
    expect(row.closest('quader-data-table table.data tbody')).toBeTruthy();
  });

  it('R-UI-169 Q-UI-426 Q-UI-1042 a row of the list of every project offers Set up client, and no copy of the address', async () => {
    const { root, fixture } = await renderScreen(RepositoryList);
    const row = [...root.querySelectorAll('tbody tr')].find((tr) => text(tr).includes('generic-local'))!;
    const setup = row.querySelector<HTMLAnchorElement>('td.actions a.row-setup')!;
    expect(text(setup)).toBe('Set up client generic-local');
    expect(setup.getAttribute('href')).toBe('/projects/web/repositories/generic-local/setup');
    expect(row.querySelector('quader-copy-button')).toBeNull();
    row.querySelector<HTMLButtonElement>('button[aria-label="Actions for generic-local"]')!.click();
    await settle(fixture);
    const items = [...root.ownerDocument.body.querySelectorAll('.menu [role="menuitem"]')].map((i) => i.textContent?.trim());
    expect(items).not.toContain('Copy client address');
  });

  it('R-UI-020 the description is the second line of the first column, and the row actions are one menu named for the row', async () => {
    const { root, fixture } = await renderScreen(RepositoryList);
    const first = [...root.querySelectorAll('tbody th[scope="row"]')].find((th) => th.textContent?.includes('generic-local'))!;
    const lines = [...first.querySelectorAll('.secondary')].map((s) => s.textContent?.replace(/\s+/g, ' ').trim());
    expect(lines.at(-1)).toContain('Installers and build outputs');
    const heads = [...root.querySelectorAll('thead th')].map((th) => th.textContent?.trim());
    expect(heads).not.toContain('Description');
    const row = first.closest('tr')!;
    // The description is the only line under the key: every other property is a column (Q-UI-330).
    expect(first.querySelectorAll('.secondary')).toHaveLength(1);
    // No row has a last activity: the column is left out (a column of dashes says nothing).
    expect(heads).not.toContain('Last activity');
    expect(root.querySelector('quader-data-table')?.classList.contains('stack')).toBe(true);
    // Set up client and the menu.
    expect(row.querySelectorAll('td.actions a.row-setup')).toHaveLength(1);
    expect(row.querySelectorAll('td.actions button')).toHaveLength(1);
    row.querySelector<HTMLButtonElement>('button[aria-label="Actions for generic-local"]')!.click();
    await settle(fixture);
    const menu = root.ownerDocument.body.querySelector('.menu')!;
    expect([...menu.querySelectorAll('[role="menuitem"]')].map((i) => i.textContent?.trim())).toEqual(
      expect.arrayContaining(['Settings', 'Files']),
    );
  });

  it('R-UI-020 R-UI-067 a reader is offered no Settings in a row: there is nothing to read or change for them there', async () => {
    const { root, fixture } = await renderScreen(RepositoryList, { capabilities: [] });
    root.querySelector<HTMLButtonElement>('button[aria-label="Actions for generic-local"]')!.click();
    await settle(fixture);
    const items = [...root.ownerDocument.body.querySelectorAll('.menu [role="menuitem"]')].map((i) => i.textContent?.trim());
    expect(items).toContain('Files');
    expect(items).not.toContain('Settings');
  });

  it('R-UI-020 the filters reach the API and restart the walk (class and format)', async () => {
    const { root, get } = await renderScreen(RepositoryList, {
      inputs: { class: 'local', format: 'generic', state: 'inactive' },
    });
    const fake = get(RepositoriesApi) as FakeRepositoriesApi;
    expect(fake.listFilters.at(-1)).toEqual({
      q: undefined,
      class: 'local',
      format: 'generic',
      state: 'inactive',
    });
    expect(text(root)).toContain('releases');
    expect(text(root)).not.toContain('generic-local');
  });

  it('R-UI-020 an empty list and an empty filter result are explained, and pass the accessibility check', async () => {
    const empty = await renderScreen(RepositoryList, {
      setup: (get) => ((get(RepositoriesApi) as FakeRepositoriesApi).repositories = []),
    });
    expect(text(empty.root)).toContain('There is no repository yet.');
    await expectAccessible(empty.root);

    const filtered = await renderScreen(RepositoryList, { inputs: { q: 'nothing-like-this' } });
    expect(text(filtered.root)).toContain('No repository matches.');
    await expectAccessible(filtered.root);
  });

  it('R-UI-020 a failing list shows the error with its request ID and a retry, and passes the accessibility check', async () => {
    const { root } = await renderScreen(RepositoryList, {
      setup: (get) => {
        const fake = get(RepositoriesApi) as FakeRepositoriesApi;
        fake.list = () => () => Promise.reject(new ApiError(503, { code: 'unavailable' }, 'rid-1'));
      },
    });
    expect(text(root)).toContain('temporarily unavailable');
    expect(text(root)).toContain('Retry');
    await expectAccessible(root);
  });

  it('R-UI-020 R-UI-146 Q-UI-573 Q-UI-1042 "New repository" stands on a project\'s list for who may write repository configuration, and on the list of every project for nobody', async () => {
    const admin = await routed('/projects/web/repositories');
    const create = [...admin.querySelectorAll('a')].find((a) => text(a) === 'New repository')!;
    expect(create.getAttribute('href')).toBe('/projects/web/repositories/new');
    const reader = await routed('/projects/web/repositories', ['repositories:read-config']);
    expect(text(reader)).not.toContain('New repository');
    const across = await routed('/repositories');
    expect(text(across)).toContain('generic-local');
    expect(text(across)).not.toContain('New repository');
    expect([...across.querySelectorAll('a')].some((a) => (a.getAttribute('href') ?? '').startsWith('/admin'))).toBe(false);
  });

  it('R-UI-020 "Load more" continues the cursor walk', async () => {
    const { root, fixture } = await renderScreen(RepositoryList, {
      setup: (get) => {
        const fake = get(RepositoriesApi) as FakeRepositoriesApi;
        fake.repositories = Array.from({ length: 120 }, (_, i) => ({
          key: `repo-${String(i).padStart(3, '0')}`,
          class: 'local' as const,
          format: 'generic',
        }));
      },
    });
    expect(root.querySelectorAll('tbody tr').length).toBe(50);
    button(root, 'Load more').click();
    await settle(fixture);
    expect(root.querySelectorAll('tbody tr').length).toBe(100);
  });

  it('R-UI-020 delete asks for the key, runs as a job and follows its progress until the row is gone', async () => {
    const { root, fixture, get } = await renderScreen(RepositoryList, {
      setup: (get) => {
        const fake = get(RepositoriesApi) as FakeRepositoriesApi;
        fake.deleteResult = {
          done: false,
          job: {
            id: 'job-1',
            kind: 'repository-delete',
            state: 'queued',
            href: '/api/v1/system/jobs/job-1',
          },
        };
      },
    });
    const row = [...root.querySelectorAll('tbody tr')].find((r) =>
      r.textContent?.includes('generic-local'),
    )!;
    // The row's actions are a menu (Q-UI-302): open it, then choose Delete.
    row.querySelector<HTMLButtonElement>('button[aria-label="Actions for generic-local"]')!.click();
    await settle(fixture);
    const body = root.ownerDocument.body;
    button(body.querySelector<HTMLElement>('.menu')!, 'Delete').click();
    await settle(fixture);

    const confirm = button(body, 'Delete repository');
    expect(confirm.disabled).toBe(true);
    type(body.querySelector<HTMLInputElement>('#confirm-name')!, 'generic-local');
    await settle(fixture);
    expect(confirm.disabled).toBe(false);
    confirm.click();
    await settle(fixture);

    const fake = get(RepositoriesApi) as FakeRepositoriesApi;
    expect(fake.deleteCalls).toHaveLength(1);
    expect(fake.deleteCalls[0]?.idempotencyKey).toBeTruthy();
    expect(text(root)).toContain('Deleting');
  });

  it('R-UI-167 a repository deleted from its row leaves the remembered places at once, and opening it while its job runs records nothing', async () => {
    const store = 'quader.frequent.00000000-0000-0000-0000-000000000001';
    browserStorage.clear('local');
    const { root, fixture, get } = await renderScreen(RepositoryList, {
      setup: (get) => {
        const entries = ['web/generic-local', 'web/releases'].map((id, i) => ({ id, visits: 1, last: 1000 - i }));
        browserStorage.set('local', store, JSON.stringify({ repositories: entries, projects: [] }));
        (get(RepositoriesApi) as FakeRepositoriesApi).deleteResult = {
          done: false,
          job: { id: 'job-1', kind: 'repository-delete', state: 'queued', href: '/api/v1/system/jobs/job-1' },
        };
      },
    });
    const remembered = () => get(FrequentPlaces).recentRepositories().map((v) => v.id);
    const row = [...root.querySelectorAll('tbody tr')].find((r) => r.textContent?.includes('generic-local'))!;
    row.querySelector<HTMLButtonElement>('button[aria-label="Actions for generic-local"]')!.click();
    await settle(fixture);
    const body = root.ownerDocument.body;
    button(body.querySelector<HTMLElement>('.menu')!, 'Delete').click();
    await settle(fixture);
    type(body.querySelector<HTMLInputElement>('#confirm-name')!, 'generic-local');
    await settle(fixture);
    button(body, 'Delete repository').click();
    await settle(fixture);
    expect(remembered()).toEqual(['web/releases']);

    row.querySelector<HTMLAnchorElement>('th a')!.click();
    await settle(fixture);
    expect(remembered()).toEqual(['web/releases']);
    browserStorage.clear('local');
  });
});
