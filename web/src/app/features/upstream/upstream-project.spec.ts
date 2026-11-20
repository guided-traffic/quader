import { TestBed } from '@angular/core/testing';
import { Router } from '@angular/router';
import { ProjectsApi } from '../../api/projects-api';
import { RepositoriesApi } from '../../api/repositories-api';
import { FakeRepositoriesApi, OCI_FORMAT, PACKAGE_FORMAT } from '../../api/testing/fakes-catalog';
import { FakeProjectsApi, sampleProject } from '../../api/testing/fakes-projects';
import { UpstreamApi } from '../../api/upstream-api';
import { FakeUpstreamApi } from '../../api/testing/fakes-upstream';
import { projectEntries } from '../../core/navigation';
import { SessionStore } from '../../core/session-store';
import { expectAccessible, renderScreen, settle, text } from '../../testing/screen';
import { ProjectList } from '../projects/project-list';
import { ProjectSwitcher } from '../projects/project-switcher';
import { UPSTREAM_PROJECT } from '../../core/upstream-project';
import { ProjectsHome } from '../projects/projects-home';
import { RepositoryCreate } from '../repositories/repository-create';
import { RepositoryDetail } from '../repositories/repository-detail';

/** The three projects of the lists: one whose key and name sort before `upstream`, the built-in project, and one after it. */
function projects(get: <A>(token: abstract new (...args: never[]) => A) => A): void {
  (get(ProjectsApi) as FakeProjectsApi).projects = [
    sampleProject({ key: 'alpha', displayName: 'Alpha team', role: 'owner' }),
    sampleProject({
      key: 'upstream',
      displayName: 'Upstream',
      description: 'The public registries this installation offers to every project.',
      visibility: 'public',
      owners: [],
      members: 0,
      repositories: 12,
    }),
    sampleProject({ key: 'web', displayName: 'Web platform', role: 'reader' }),
  ];
}

const names = (root: HTMLElement): string[] => [...root.querySelectorAll('tbody th[scope="row"] a')].map((a) => text(a));

describe('The project upstream in the main area (R-UI-190, Q-UI-837)', () => {
  it('R-UI-190 the start page lists Upstream first with Built-in, above a project of the caller that sorts before it, and its mark is an arrow', async () => {
    const { root } = await renderScreen(ProjectsHome, { setup: projects });
    expect(names(root)).toEqual(['Upstream', 'Alpha team', 'Web platform']);
    const first = root.querySelector('tbody tr')!;
    expect(text(first.querySelector('th')!)).toContain('Built-in');
    const mark = first.querySelector('quader-project-mark')!;
    expect(mark.classList.contains('upstream')).toBe(true);
    expect(mark.hasAttribute('data-letter')).toBe(false);
    // Every other project keeps its initial.
    expect(root.querySelectorAll('tbody tr')[1]!.querySelector('quader-project-mark')!.getAttribute('data-letter')).toBe('A');
    await expectAccessible(root);
  });

  it('R-UI-190 the admin area keeps Upstream first under either direction of the sort, without Edit, Owners or members', async () => {
    const r = await renderScreen(ProjectList, { setup: projects });
    expect(names(r.root)).toEqual(['Upstream', 'Alpha team', 'Web platform']);
    r.root.querySelector<HTMLButtonElement>('thead button.th-sort')!.click();
    await settle(r.fixture);
    expect(names(r.root)).toEqual(['Upstream', 'Web platform', 'Alpha team']);
    const row = r.root.querySelector('tbody tr')!;
    expect(text(row)).toContain('Built-in');
    expect([...row.querySelectorAll('button')].some((b) => text(b).startsWith('Edit'))).toBe(false);
    expect(row.querySelectorAll('quader-no-value').length).toBe(2);
  });

  it('R-UI-190 a filter that does not match takes it out, and the project switcher lists it first among its matches', async () => {
    const home = await renderScreen(ProjectsHome, { setup: projects });
    const search = home.root.querySelector<HTMLInputElement>('.list-search input')!;
    search.value = 'web';
    search.dispatchEvent(new Event('input', { bubbles: true }));
    search.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true }));
    await settle(home.fixture);
    expect(names(home.root)).toEqual(['Web platform']);

    const switcher = await renderScreen(ProjectSwitcher, { inputs: { label: 'Web platform' }, setup: projects });
    switcher.root.querySelector<HTMLButtonElement>('.switcher-button')!.click();
    await settle(switcher.fixture);
    // The matches, after the recently opened ones, which keep their own order.
    const lists = switcher.root.querySelectorAll('.switcher-list');
    const listed = [...lists[lists.length - 1]!.querySelectorAll('.switcher-key')].map((k) => text(k));
    expect(listed).toEqual(['upstream', 'alpha', 'web']);
  });

  it('R-UI-190 its level is the en dash, heard as each registry having its own, with no level icon in the lists or the switcher', async () => {
    const home = await renderScreen(ProjectsHome, { setup: projects });
    const cell = home.root.querySelector('tbody tr td.level')!;
    expect(cell.querySelector('quader-visibility-icon')).toBeNull();
    expect(text(cell)).toBe('–Each registry has its own visibility.');
    expect(cell.querySelector('.visually-hidden')!.textContent).toBe('Each registry has its own visibility.');
    // Every other project keeps its level.
    expect(home.root.querySelectorAll('tbody tr')[1]!.querySelector('td.level quader-visibility-icon')).not.toBeNull();
    await expectAccessible(home.root);

    const admin = await renderScreen(ProjectList, { setup: projects });
    expect(admin.root.querySelector('tbody tr')!.querySelector('td.level quader-visibility-icon')).toBeNull();

    const switcher = await renderScreen(ProjectSwitcher, {
      inputs: { label: 'Upstream' },
      setup: async (get) => {
        projects(get);
        await get(Router).navigateByUrl(`/projects/${UPSTREAM_PROJECT}/repositories`);
      },
    });
    await settle(switcher.fixture);
    expect(switcher.root.querySelector('.switcher-button quader-visibility-icon')).toBeNull();
  });

  it('R-UI-190 its side bar has the content and no Access group and no Settings', async () => {
    await renderScreen(ProjectsHome, { setup: projects });
    const session = TestBed.inject(SessionStore);
    const labels = projectEntries('upstream', session).map((e) => e.label);
    expect(labels).toEqual(['Repositories', 'Packages', 'Vulnerabilities']);
    expect(projectEntries('web', session).map((e) => e.label)).toContain('Settings');
  });

  it('R-UI-190 the settings of upstream/scanner-trivy state its fixed level in one sentence, without the group of levels', async () => {
    const { root } = await renderScreen(RepositoryDetail, {
      inputs: { project: 'upstream', key: 'scanner-trivy' },
      setup: (get) => {
        const fake = get(RepositoriesApi) as FakeRepositoriesApi;
        fake.formatsResult = [OCI_FORMAT];
        fake.repositories.push({
          key: 'scanner-trivy',
          project: 'upstream',
          class: 'remote',
          format: 'oci',
          description: 'Image scanning database',
          visibility: { artifacts: 'private', wiki: 'private' },
          remote: { url: 'https://mirror.gcr.io/', authMode: 'none' },
        } as never);
      },
    });
    const level = root.querySelector('[aria-labelledby="repo-visibility-fixed"]')!;
    expect(text(level)).toContain("Its visibility is always Private: only Quader's image scanning reads it.");
    expect(level.textContent).not.toContain('Managed in');
    expect(root.querySelector('quader-repository-visibility, quader-visibility-level')).toBeNull();
    expect(text(root.querySelector('[aria-labelledby="repo-upstream-managed"]')!)).toContain('Managed in Admin area › Upstream registries.');
    await expectAccessible(root);
  });

  it('R-UI-190 the settings of upstream/npmjs show the upstream read-only with the link, the include patterns editable, no Wiki visibility and no Delete repository', async () => {
    const { root } = await renderScreen(RepositoryDetail, {
      inputs: { project: 'upstream', key: 'npmjs' },
      setup: (get) => {
        const fake = get(RepositoriesApi) as FakeRepositoriesApi;
        fake.formatsResult = [PACKAGE_FORMAT];
        fake.repositories.push({
          key: 'npmjs',
          project: 'upstream',
          class: 'remote',
          format: 'npm',
          description: 'npm public registry',
          disabled: true,
          visibility: { artifacts: 'internal', wiki: 'internal' },
          remote: { url: 'https://registry.npmjs.org/', authMode: 'none' },
        } as never);
      },
    });
    // One level for its packages and its documentation, set in the admin area: npm has wiki content, and still no fold.
    expect(root.querySelector('quader-repository-visibility')).not.toBeNull();
    expect(root.querySelector('quader-repository-visibility quader-disclosure')).toBeNull();
    const upstream = root.querySelector('[aria-labelledby="repo-upstream-managed"]')!;
    expect(text(upstream)).toContain('Managed in Admin area › Upstream registries.');
    expect(upstream.querySelector('a')!.getAttribute('href')).toBe('/admin/upstream/npmjs');
    expect(upstream.querySelector<HTMLInputElement>('#f-remote-url-managed')!.readOnly).toBe(true);
    expect(root.querySelector('quader-upstream-settings')).toBeNull();
    expect(root.querySelector<HTMLTextAreaElement>('#f-includePatterns')!.readOnly).toBe(false);
    expect(root.querySelector<HTMLTextAreaElement>('#f-description')!.readOnly).toBe(true);
    expect([...root.querySelectorAll('button')].some((b) => text(b) === 'Delete repository')).toBe(false);
    expect(text(root.querySelector('.subject')!)).toContain('Not switched on');
    await expectAccessible(root);
  });
});

describe('A new remote repository names the central one (R-UI-191)', () => {
  async function newOciRemote(dockerHub: { disabled: boolean } | null) {
    const r = await renderScreen(RepositoryCreate, {
      inputs: { project: 'web', format: 'oci', class: 'remote' },
      setup: async (get) => {
        await get(Router).navigateByUrl('/projects/web/repositories/new/oci');
        const fake = get(RepositoriesApi) as FakeRepositoriesApi;
        fake.formatsResult = [OCI_FORMAT];
        if (dockerHub) {
          fake.repositories.push({ key: 'docker-hub', project: 'upstream', class: 'remote', format: 'oci', disabled: dockerHub.disabled } as never);
        }
        (get(UpstreamApi) as FakeUpstreamApi).set('docker-hub', { state: dockerHub && !dockerHub.disabled ? 'active' : 'not-switched-on' });
      },
    });
    await settle(r.fixture);
    return r;
  }

  it('R-UI-191 with upstream/docker-hub active and readable, the form of a new OCI remote says so under the presets and links a new virtual repository', async () => {
    const { root } = await newOciRemote({ disabled: false });
    const line = root.querySelector('[data-testid="upstream-central"]')!;
    expect(text(line)).toContain('Docker Hub is offered to every project as upstream/docker-hub.');
    const link = line.querySelector('a')!;
    expect(text(link)).toBe('New virtual repository');
    // The test renders the form outside a project's route: the admin area's form, which carries the project on.
    expect(link.getAttribute('href')).toBe('/admin/repositories/new/oci?class=virtual&project=web');
    // Nothing stops the create.
    expect([...root.querySelectorAll('button')].some((b) => text(b).startsWith('Create') && !b.disabled)).toBe(true);
  });

  it('R-UI-191 with it off the line is not there', async () => {
    const off = await newOciRemote({ disabled: true });
    expect(off.root.querySelector('[data-testid="upstream-central"]')).toBeNull();
    const unread = await newOciRemote(null);
    expect(unread.root.querySelector('[data-testid="upstream-central"]')).toBeNull();
  });
});
