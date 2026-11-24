import { ComponentFixture } from '@angular/core/testing';
import { SessionApi } from '../../api/session-api';
import { FakeSessionApi } from '../../api/testing/fakes';
import { FakeProjectsApi, sampleProject } from '../../api/testing/fakes-projects';
import { ProjectsApi } from '../../api/projects-api';
import { seriousViolations } from '../../testing/axe';
import { buttonByText, choosePrincipal, expectAccessible, renderScreen, settle, sleep, text, type, typeInto } from '../../testing/screen';
import { ProjectSettings } from './project-settings';
import { ProjectPoliciesPage, ProjectServiceAccountsPage } from './project-list-pages';
import { ProjectsHome } from './projects-home';
import { BrowserStorage } from '../../core/storage';

const overlay = (): HTMLElement => document.querySelector('.cdk-overlay-container') as HTMLElement;

/** A caller without any capability that is Owner of `web` (Q-AUTHZ-181). */
function asProjectAdministrator() {
  return (get: <A>(token: abstract new (...args: never[]) => A) => A): void => {
    (get(SessionApi) as FakeSessionApi).meResult = {
      roles: [],
      capabilities: [],
      principals: ['user:alice'],
      projects: [{ key: 'web', role: 'owner' }],
    };
  };
}

/** Ticks **Show Owners and storage** in the head of the start page (Q-UI-1041). */
async function showDetails(r: { root: HTMLElement; fixture: ComponentFixture<unknown> }): Promise<void> {
  const box = [...r.root.querySelectorAll<HTMLInputElement>('input[type="checkbox"]')].find((i) =>
    text(i.closest('label') ?? i.parentElement!).includes('Show Owners and storage'),
  )!;
  box.click();
  await settle(r.fixture);
}

const headsOf = (root: HTMLElement): string[] => [...root.querySelectorAll('thead th')].map((h) => text(h).replace('help_outline', '').trim());

describe('The start page holds what the admin area\'s list of projects held (R-UI-041, the owner\'s T-25)', () => {
  // The choice of the columns is kept in the browser per viewer (Q-UI-1041): each case starts without it.
  beforeEach(() => new BrowserStorage().clear('local'));
  afterEach(() => new BrowserStorage().clear('local'));

  it('R-UI-041 R-UI-146 Q-UI-1041 a holder of projects:read adds Owners and Storage on demand; the role stays, and axe passes', async () => {
    const r = await renderScreen(ProjectsHome, {
      setup: (get) => {
        (get(ProjectsApi) as FakeProjectsApi).projects = [
          sampleProject({ repositories: 2, role: 'owner' }),
          sampleProject({ key: 'data', displayName: 'Data', owners: [], members: 0 }),
        ];
      },
    });
    expect(headsOf(r.root)).not.toContain('Owners');
    expect(headsOf(r.root)).not.toContain('Storage');
    await showDetails(r);
    expect(headsOf(r.root)).toEqual(['Project', 'Description', 'Visibility', 'Your role', 'Owners', 'Repositories', 'Members', 'Storage']);
    const row = [...r.root.querySelectorAll('tbody tr')].find((x) => text(x).includes('Web platform'))!;
    // The Owners are named, not referenced (UI-A-27).
    expect(text(row)).toContain('web-leads');
    expect(text(row)).not.toContain('group:web-leads');
    await expectAccessible(r.root);
  });

  it('R-UI-146 Q-UI-574 Q-UI-561 Q-UI-1041 the counts and the size stand at their column\'s end; no Owners is the dash', async () => {
    const r = await renderScreen(ProjectsHome, {
      setup: (get) => {
        (get(ProjectsApi) as FakeProjectsApi).projects = [sampleProject({ key: 'data', displayName: 'Data', owners: [], members: 0 })];
      },
    });
    await showDetails(r);
    const heads = [...r.root.querySelectorAll('thead th')].map((h) => [text(h).replace('help_outline', '').trim(), h.classList.contains('num')]);
    expect(heads.filter(([, num]) => num).map(([label]) => label)).toEqual(['Repositories', 'Members', 'Storage']);
    const row = r.root.querySelector('tbody tr')!;
    expect(row.querySelectorAll('td.num')).toHaveLength(3);
    // No Owners and, for this caller, no role: each the dash.
    expect(row.querySelectorAll('td quader-no-value')).toHaveLength(2);
  });

  it('R-STOR-049 Q-UI-1041 shows the logical size each project is charged, the exact bytes its title', async () => {
    const r = await renderScreen(ProjectsHome, {
      setup: (get) => {
        (get(ProjectsApi) as FakeProjectsApi).projects = [
          sampleProject({ logicalBytes: 3 * 1024 ** 3 }),
          sampleProject({ key: 'data', displayName: 'Data', logicalBytes: 0 }),
        ];
      },
    });
    await showDetails(r);
    const column = headsOf(r.root).indexOf('Storage');
    expect(column).toBeGreaterThan(-1);
    const cells = (row: Element): Element[] => [...row.querySelectorAll('th, td')];
    const rows = [...r.root.querySelectorAll('tbody tr')];
    const data = rows.find((x) => text(x).includes('Data'))!;
    const web = rows.find((x) => text(x).includes('Web platform'))!;
    expect(text(cells(data)[column]!)).toBe('0 B');
    expect(text(cells(web)[column]!)).toBe('3.0 GiB');
    expect(cells(web)[column]!.getAttribute('title')).toBe(`${3 * 1024 ** 3} B`);
  });

  it('Q-UI-1041 the choice is kept for the viewer, and a plain user is offered no such columns', async () => {
    const first = await renderScreen(ProjectsHome);
    await showDetails(first);
    const again = await renderScreen(ProjectsHome);
    expect(headsOf(again.root)).toContain('Owners');

    const plain = await renderScreen(ProjectsHome, { capabilities: [] });
    expect(text(plain.root)).not.toContain('Show Owners and storage');
    expect(headsOf(plain.root)).not.toContain('Owners');
    expect(headsOf(plain.root)).not.toContain('Storage');
  });

  it('R-UI-041 an Owner without projects:write reads the list and is offered no New project', async () => {
    const { root } = await renderScreen(ProjectsHome, { capabilities: [], setup: asProjectAdministrator() });
    expect([...root.querySelectorAll('button')].some((b) => text(b) === 'New project')).toBe(false);
    expect(text(root)).toContain('Web platform');
  });

  it('R-UI-041 R-AUTHZ-026 R-UI-150 creates a project with a first Owner group chosen from the search, then opens it', async () => {
    const { fixture, root, get } = await renderScreen(ProjectsHome);
    buttonByText(root, 'New project').click();
    await settle(fixture);
    const dialog = overlay().querySelector('quader-project-dialog') as HTMLElement;
    typeInto(dialog, 'Key', 'team-a');
    typeInto(dialog, 'Display name', 'Team A');
    await choosePrincipal(fixture, dialog, 'First Owners', 'leads', 'web-leads');
    buttonByText(dialog, 'Create project').click();
    await sleep(fixture, 100);
    const api = get(ProjectsApi) as FakeProjectsApi;
    expect(api.calls).toEqual(['create team-a']);
    expect(api.keys).toHaveLength(1);
    expect(api.projects.find((x) => x.key === 'team-a')?.owners).toEqual(['group:web-leads']);
  });

  it('R-UI-041 a key that does not match the pattern is refused before any request', async () => {
    const { fixture, root, get } = await renderScreen(ProjectsHome);
    buttonByText(root, 'New project').click();
    await settle(fixture);
    const dialog = overlay().querySelector('quader-project-dialog') as HTMLElement;
    typeInto(dialog, 'Key', 'Team_A');
    buttonByText(dialog, 'Create project').click();
    await settle(fixture);
    expect(text(dialog)).toContain('The key does not match the pattern.');
    expect((get(ProjectsApi) as FakeProjectsApi).calls).toEqual([]);
  });
});

describe('Project settings (R-UI-041, Q-UI-129)', () => {
  it('R-UI-041 shows the facts of the project and passes axe', async () => {
    const { root } = await renderScreen(ProjectSettings, { inputs: { project: 'web' } });
    expect(root.querySelector<HTMLInputElement>('input[name="displayName"]')!.value).toBe('Web platform');
    expect(text(root)).toContain('group:web-leads');
    expect([...root.querySelectorAll('h2')].some((h) => text(h).endsWith('Details'))).toBe(true);
    // The members, repositories, policies and service accounts have screens of their own (Q-UI-129).
    for (const heading of ['Members', 'Repositories', 'Permission policies', 'Service accounts']) {
      expect([...root.querySelectorAll('h2')].some((h) => text(h) === heading)).toBe(false);
    }
    expect(await seriousViolations(root)).toEqual([]);
  });

  it('R-STOR-049 shows the logical size of the project with the exact bytes and what they count', async () => {
    const { root } = await renderScreen(ProjectSettings, {
      inputs: { project: 'web' },
      setup: (g) => {
        (g(ProjectsApi) as FakeProjectsApi).projects = [sampleProject({ logicalBytes: 1_610_612_736 })];
      },
    });
    const terms = [...root.querySelectorAll('dt')];
    const storage = terms.find((t) => text(t) === 'Storage');
    expect(storage).toBeTruthy();
    const value = storage!.nextElementSibling!;
    expect(text(value)).toBe('1.5 GiB');
    expect(value.getAttribute('title')).toContain('1,610,612,736 bytes');
    expect(value.getAttribute('title')).toContain('also where another project holds the same content');
  });

  it('R-UI-041 Q-UI-215 the settings are edited in place: the key is read-only and Save changes waits for a change', async () => {
    const { fixture, root, get } = await renderScreen(ProjectSettings, { inputs: { project: 'web' } });
    expect(root.querySelector<HTMLInputElement>('input[name="key"]')!.readOnly).toBe(true);
    const save = buttonByText(root, 'Save changes') as HTMLButtonElement;
    expect(save.disabled).toBe(true);
    typeInto(root, 'Display name', 'The web team');
    await settle(fixture);
    expect(save.disabled).toBe(false);
    save.click();
    await sleep(fixture, 100);
    const api = get(ProjectsApi) as FakeProjectsApi;
    expect(api.calls.some((c) => c.startsWith('replace web'))).toBe(true);
    expect(save.disabled).toBe(true);
    expect(await seriousViolations(root)).toEqual([]);
  });

  it('R-UI-041 R-UI-042 a save sends If-Match; a 412 opens the conflict dialog', async () => {
    const { fixture, root, get } = await renderScreen(ProjectSettings, { inputs: { project: 'web' } });
    const api = get(ProjectsApi) as FakeProjectsApi;
    // Someone else changes the project while the page is open.
    api.bump('web');
    typeInto(root, 'Display name', 'Web crew');
    await settle(fixture);
    buttonByText(root, 'Save changes').click();
    await sleep(fixture, 100);
    expect(api.ifMatch).toEqual(['"1"']);
    expect(overlay().querySelector('quader-conflict-dialog')).toBeTruthy();
  });

  it('R-UI-041 Q-UI-215 a settings section folds and says whether it is open', async () => {
    const { fixture, root } = await renderScreen(ProjectSettings, { inputs: { project: 'web' } });
    const toggle = buttonByText(root, 'Advanced');
    const region = root.querySelector<HTMLElement>(`#${toggle.getAttribute('aria-controls')}`)!;
    expect(toggle.getAttribute('aria-expanded')).toBe('false');
    expect(region.classList.contains('open')).toBe(false);
    toggle.click();
    await settle(fixture);
    expect(toggle.getAttribute('aria-expanded')).toBe('true');
    expect(region.classList.contains('open')).toBe(true);
  });

  it('R-UI-041 delete is offered only for an empty project, with the reason otherwise', async () => {
    const { root } = await renderScreen(ProjectSettings, {
      inputs: { project: 'web' },
      setup: (g) => {
        (g(ProjectsApi) as FakeProjectsApi).projects = [sampleProject({ serviceAccounts: 1 })];
      },
    });
    expect((buttonByText(root, 'Delete project') as HTMLButtonElement).disabled).toBe(true);
    expect(text(root)).toContain('no repository and no service account belongs to it');
  });

  it('R-UI-041 deletes an empty project after the key is typed', async () => {
    const { fixture, root, get } = await renderScreen(ProjectSettings, { inputs: { project: 'web' } });
    buttonByText(root, 'Delete project').click();
    await settle(fixture);
    const confirm = overlay().querySelector('quader-confirm-dialog') as HTMLElement;
    type(confirm.querySelector<HTMLInputElement>('#confirm-name')!, 'web');
    await settle(fixture);
    buttonByText(confirm, 'Delete project').click();
    await sleep(fixture, 100);
    expect((get(ProjectsApi) as FakeProjectsApi).calls).toEqual(['delete web']);
  });

  it('R-UI-041 explains 409 project_not_empty when something arrived since the page was read', async () => {
    const { fixture, root, get } = await renderScreen(ProjectSettings, { inputs: { project: 'web' } });
    const api = get(ProjectsApi) as FakeProjectsApi;
    api.failure = new (await import('../../api/problem')).ApiError(409, { code: 'project_not_empty' }, 'req-1');
    buttonByText(root, 'Delete project').click();
    await settle(fixture);
    const confirm = overlay().querySelector('quader-confirm-dialog') as HTMLElement;
    type(confirm.querySelector<HTMLInputElement>('#confirm-name')!, 'web');
    await settle(fixture);
    buttonByText(confirm, 'Delete project').click();
    await sleep(fixture, 100);
    expect(text(root)).toContain('still has repositories or service accounts');
  });

  it('R-UI-041 R-AUTHZ-026 an Owner edits the project but cannot delete it', async () => {
    const { root } = await renderScreen(ProjectSettings, {
      inputs: { project: 'web' },
      capabilities: [],
      setup: asProjectAdministrator(),
    });
    expect(root.textContent).toContain('Save changes');
    expect(root.textContent).not.toContain('Delete project');
    expect(await seriousViolations(root)).toEqual([]);
  });

  it('R-UI-041 the auditor reads the project and the edit action says whom to ask', async () => {
    const { root } = await renderScreen(ProjectSettings, {
      inputs: { project: 'web' },
      capabilities: ['projects:read'],
      setup: (g) => {
        (g(SessionApi) as FakeSessionApi).meResult = { roles: ['auditor'], capabilities: ['projects:read'] };
      },
    });
    // A project's screen names its Owners, not the capability (Q-UI-318).
    expect(text(root)).toMatch(/Read only\. (Ask .+, an Owner of web, to change this|An Owner of web can change this)\./);
    expect(text(root)).not.toContain('projects:write');
    expect(root.textContent).not.toContain('Save changes');
    expect(root.querySelector<HTMLInputElement>('input[name="displayName"]')!.readOnly).toBe(true);
  });

  it('R-UI-041 a project that is not visible answers 404 and says so', async () => {
    const { root } = await renderScreen(ProjectSettings, { inputs: { project: 'nope' } });
    expect(text(root)).toContain('does not exist, or you may not see it');
  });
});

describe('The lists of a project (R-UI-041, R-AUTHZ-026, Q-UI-129)', () => {
  it('Q-UI-129 an Owner creates policies in the project and service accounts', async () => {
    const { root } = await renderScreen(ProjectPoliciesPage, {
      inputs: { project: 'web' },
      capabilities: [],
      setup: asProjectAdministrator(),
    });
    expect(buttonByText(root, 'Create policy').getAttribute('href')).toBe('/projects/web/policies/new');
    expect(text(root)).toContain('The project has no permission policy yet.');
    expect(await seriousViolations(root)).toEqual([]);
    const accounts = await renderScreen(ProjectServiceAccountsPage, {
      inputs: { project: 'web' },
      capabilities: [],
      setup: asProjectAdministrator(),
    });
    expect((buttonByText(accounts.root, 'Create service account') as HTMLButtonElement).disabled).toBe(false);
    expect(text(accounts.root)).toContain('The project has no service account yet.');
  });

  it('Q-UI-129 the auditor reads and every create action says what is missing', async () => {
    const { root } = await renderScreen(ProjectPoliciesPage, {
      inputs: { project: 'web' },
      capabilities: ['projects:read', 'policies:read'],
      setup: (g) => {
        (g(SessionApi) as FakeSessionApi).meResult = { roles: ['auditor'], capabilities: ['projects:read', 'policies:read'] };
      },
    });
    expect(text(root)).toContain('Read only: changing this needs the permission policies:write');
  });
});
