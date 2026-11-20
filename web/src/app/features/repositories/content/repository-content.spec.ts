import { ClientSetupApi, PackagesApi } from '../../../api/packages-api';
import { RepositoriesApi } from '../../../api/repositories-api';
import { VulnerabilitiesApi } from '../../../api/vulnerabilities-api';
import { FakeClientSetupApi, FakePackagesApi, ociFixture } from '../../../api/testing/fakes-packages';
import { FakeRepositoriesApi, GENERIC_FORMAT, OCI_FORMAT, PACKAGE_FORMAT } from '../../../api/testing/fakes-catalog';
import { FakeFilesApi } from '../../../api/testing/fakes-catalog';
import { FakeVulnerabilitiesApi, sampleGrade } from '../../../api/testing/fakes-vulnerabilities';
import { FilesApi } from '../../../api/files-api';
import { SessionApi } from '../../../api/session-api';
import { FakeSessionApi } from '../../../api/testing/fakes';
import { FrequentPlaces } from '../../../core/frequent-places';
import { browserStorage, expectAccessible, renderScreen, settle, text } from '../../../testing/screen';
import { RepositoryContent } from './repository-content';
import { RepositoryItems } from './repository-items';
import { contentShapeOf } from './content-shape';
import { Component, signal } from '@angular/core';

interface RepoDoc {
  readonly key: string;
  readonly project: string;
  readonly class: 'local' | 'remote' | 'virtual';
  readonly format: string;
  readonly description?: string;
  readonly disabled?: boolean;
  readonly virtual?: { members: string[] };
}

async function open(repo: RepoDoc, setup?: (fakes: {
  catalog: FakeRepositoriesApi;
  packages: FakePackagesApi;
  vulnerabilities: FakeVulnerabilitiesApi;
  session: FakeSessionApi;
}) => void, capabilities?: readonly string[], inputs: Record<string, unknown> = {}) {
  const r = await renderScreen(RepositoryContent, {
    inputs: { project: repo.project, key: repo.key, ...inputs },
    capabilities,
    setup: (get) => {
      const catalog = get(RepositoriesApi) as FakeRepositoriesApi;
      catalog.repositories = [...catalog.repositories, repo as never];
      catalog.formatsResult = [GENERIC_FORMAT, PACKAGE_FORMAT, OCI_FORMAT];
      setup?.({ catalog, packages: get(PackagesApi) as FakePackagesApi, vulnerabilities: get(VulnerabilitiesApi) as FakeVulnerabilitiesApi, session: get(SessionApi) as FakeSessionApi });
    },
  });
  await settle(r.fixture);
  return r;
}

const OCI_REPO: RepoDoc = { key: 'oci-local', project: 'web', class: 'local', format: 'oci', description: 'Images of the web team' };
const NPM_REPO: RepoDoc = { key: 'npm-local', project: 'web', class: 'local', format: 'npm', description: 'Internal packages' };

describe('Repository content page', () => {
  it('R-UI-052 an OCI repository lists its images, opens the tags with digest, size and pushed time, and shows the pull command of this installation', async () => {
    const r = await open(OCI_REPO, ({ packages }) => {
      ociFixture(packages);
      packages.packages = packages.packages.map((p) => ({ ...p, vulnerabilityGrade: sampleGrade('C', {}, { eosl: true, os: 'alpine 3.12.0' }) }));
    });
    expect(r.root.querySelector('h1')?.textContent).toContain('oci-local');
    expect(text(r.root)).toContain('Images of the web team');
    expect(text(r.root)).not.toContain('not scanned for vulnerabilities yet');
    expect(text(r.root)).toContain('Vulnerabilities');
    expect(r.root.querySelector('quader-data-table caption')?.textContent).toContain('Images');
    // An image whose OS is at its end of life says so beside the shield; the letter is the shield's (R-UI-143).
    expect(text(r.root.querySelector('tbody quader-grade .grade-text')!)).toBe('Grade C · OS end of life');
    const opener = r.root.querySelector<HTMLButtonElement>('.name-cell button[aria-expanded]')!;
    expect(opener.getAttribute('aria-expanded')).toBe('false');
    opener.click();
    await settle(r.fixture);
    expect(opener.getAttribute('aria-expanded')).toBe('true');
    // Newest push first, with the digest of the format.
    const rows = [...r.root.querySelectorAll('quader-repository-item-versions tbody tr')];
    expect(rows[0]?.textContent).toContain('1.2.0');
    expect(rows[0]?.textContent).toContain('sha256:');
    expect((r.root.querySelector<HTMLInputElement>('quader-repository-item-versions quader-address-field input')?.value ?? r.root.querySelector('quader-repository-item-versions pre')?.textContent)).toMatch(/^docker pull \S*web\/oci-local\/team\/app:1\.2\.0$/);
    await expectAccessible(r.root);
  });

  it('R-UI-052 the pull command follows the tag chosen in the list and switches between docker and podman', async () => {
    const r = await open(OCI_REPO, ({ packages }) => ociFixture(packages));
    r.root.querySelector<HTMLButtonElement>('.name-cell button[aria-expanded]')!.click();
    await settle(r.fixture);
    const setup = r.get(ClientSetupApi) as FakeClientSetupApi;
    const rows = [...r.root.querySelectorAll('quader-repository-item-versions tbody tr')];
    rows.at(-1)!.querySelector<HTMLButtonElement>('button[aria-pressed]')!.click();
    await settle(r.fixture);
    expect(setup.calls.at(-1)?.query).toMatchObject({ client: 'docker', version: '1.1.0', credentials: 'none' });
    const client = r.root.querySelector<HTMLSelectElement>('quader-repository-item-versions select')!;
    client.value = 'podman';
    client.dispatchEvent(new Event('change', { bubbles: true }));
    await settle(r.fixture);
    expect(setup.calls.at(-1)?.query).toMatchObject({ client: 'podman', version: '1.1.0' });
    expect(setup.calls.at(-1)?.repository).toBe('web/oci-local');
    expect(setup.calls.every((c) => c.query.credentials === 'none')).toBe(true);
  });

  it('R-UI-052 R-UI-094 R-UI-143 an npm repository shows the packages with the grade of their latest version and the install command of the version, and grades each version of an opened row, in the words of the versions table', async () => {
    const r = await open(NPM_REPO, ({ packages, vulnerabilities }) => {
      packages.packages = packages.packages.map((p) =>
        p.name === '@team-a/ui'
          ? { ...p, repositories: ['web/npm-local'], vulnerabilityGrade: sampleGrade('C', { high: 2, low: 3 }, { fixable: 1 }) }
          : { ...p, repositories: ['web/npm-local'], vulnerabilityGrade: sampleGrade('S') },
      );
      vulnerabilities.packageReports.set('pkg_01JBQ6V7S3K9', {
        packageId: 'pkg_01JBQ6V7S3K9',
        versions: [{ version: '1.4.0', grade: sampleGrade('C', { high: 2, low: 3 }, { fixable: 1 }) }],
      });
    });
    const packages = r.get(PackagesApi) as FakePackagesApi;
    expect(packages.listFilters.at(-1)).toMatchObject({ repository: ['web/npm-local'], format: ['npm'], include: ['vulnerabilities'] });
    const row = [...r.root.querySelectorAll('tbody tr')].find((tr) => tr.textContent?.includes('@team-a/ui'))!;
    // The statement of the package's versions table (Q-UI-556): the total and the fixable, not the count that decides the letter.
    expect(text(row.querySelector('quader-grade .grade-text')!)).toBe('Grade C · 5 findings · 1 fixable');
    const clean = [...r.root.querySelectorAll('tbody tr')].find((tr) => tr !== row && tr.querySelector('quader-grade'))!;
    expect(text(clean.querySelector('quader-grade .grade-text')!)).toBe('Grade S · no findings');
    row.querySelector<HTMLButtonElement>('.name-cell button[aria-expanded]')!.click();
    await settle(r.fixture);
    const vulnerabilities = r.get(VulnerabilitiesApi) as FakeVulnerabilitiesApi;
    expect(vulnerabilities.calls).toEqual(expect.arrayContaining(['packageReport pkg_01JBQ6V7S3K9 in web/npm-local']));
    expect(text(r.root.querySelector('quader-repository-item-versions quader-grade .grade-text')!)).toBe('Grade C · 5 findings · 1 fixable');
    expect((r.root.querySelector<HTMLInputElement>('quader-repository-item-versions quader-address-field input')?.value ?? r.root.querySelector('quader-repository-item-versions pre')?.textContent)).toBe('npm install @team-a/ui@1.4.0');
    await expectAccessible(r.root);
  });

  it('R-UI-094 Q-UI-373 the column of grades sorts the list by grade, worst first descending, after the name', async () => {
    const r = await open(NPM_REPO, ({ packages }) => {
      const grades = [sampleGrade('A', { low: 2 }), sampleGrade('E', { critical: 7 }), sampleGrade('N/A')];
      packages.packages = packages.packages.map((p, i) => ({ ...p, repositories: ['web/npm-local'], vulnerabilityGrade: grades[i % grades.length] }));
    }, undefined, { sort: '-grade' });
    const packages = r.get(PackagesApi) as FakePackagesApi;
    // The server sorts by name; the browser by grade (Q-UI-373).
    expect(packages.listFilters.at(-1)).toMatchObject({ sort: 'name' });
    const order = ['N/A', 'S', 'A', 'B', 'C', 'D', 'E'];
    const shown = [...r.root.querySelectorAll('tbody > tr > td quader-grade quader-grade-shield .letter')].map((e) => order.indexOf(e.textContent?.trim() ?? ''));
    expect(shown.length).toBeGreaterThan(1);
    expect(shown[0]).toBe(order.indexOf('E'));
    expect(shown).toEqual([...shown].sort((a, b) => b - a));
    const header = [...r.root.querySelectorAll('thead th')].find((th) => th.textContent?.includes('Vulnerabilities'))!;
    expect(header.getAttribute('aria-sort')).toBe('descending');
  });

  it('R-UI-094 a list asked before the format was known to be scanned is asked again with the grades', async () => {
    @Component({
      imports: [RepositoryItems],
      template: `<quader-repository-items repository="web/npm-local" format="npm" [shape]="shape" [scanned]="scanned()" />`,
    })
    class Host {
      readonly shape = contentShapeOf('npm');
      readonly scanned = signal(false);
    }
    const r = await renderScreen(Host, {});
    const packages = r.get(PackagesApi) as FakePackagesApi;
    expect(packages.listFilters.at(-1)).toMatchObject({ include: [] });
    r.fixture.componentInstance.scanned.set(true);
    await settle(r.fixture);
    expect(packages.listFilters.at(-1)).toMatchObject({ include: ['vulnerabilities'] });
  });

  it('R-UI-094 Q-UI-377 a list without grades has no column of grades', async () => {
    const r = await open(NPM_REPO, ({ packages }) => {
      packages.packages = packages.packages.map((p) => ({ ...p, repositories: ['web/npm-local'] }));
    });
    expect([...r.root.querySelectorAll('thead th')].some((th) => th.textContent?.includes('Vulnerabilities'))).toBe(false);
    expect(r.root.querySelector('tbody quader-grade')).toBeNull();
  });

  it('R-UI-052 R-UI-010 a generic repository lists its files, each folder and file linking to its own address on the page of the repository', async () => {
    const generic: RepoDoc = { key: 'generic-local', project: 'web', class: 'local', format: 'generic', description: 'Files' };
    const r = await renderScreen(RepositoryContent, {
      inputs: { project: 'web', key: 'generic-local' },
      setup: (get) => {
        (get(RepositoriesApi) as FakeRepositoriesApi).formatsResult = [GENERIC_FORMAT];
        const files = get(FilesApi) as FakeFilesApi;
        files.children = () => async () => ({
          items: [
            { path: 'bin', kind: 'folder' as const },
            { path: 'app.tar.gz', kind: 'file' as const, size: 2048 },
          ],
          nextCursor: null,
        });
      },
    });
    await settle(r.fixture);
    expect(r.root.querySelector('quader-data-table caption')?.textContent).toContain('Files');
    expect(text(r.root)).toContain('app.tar.gz');
    expect(r.root.querySelector<HTMLAnchorElement>('tbody a[href="/projects/web/repositories/generic-local/files/app.tar.gz"]')).not.toBeNull();
    expect(r.root.querySelector<HTMLAnchorElement>('tbody a[href="/projects/web/repositories/generic-local/files/bin"]')).not.toBeNull();
    expect(generic.format).toBe('generic');
    await expectAccessible(r.root);
  });

  it('R-UI-052 R-UI-144 an empty repository says how it is filled and offers the upload to who may upload; a disabled one offers nothing', async () => {
    const empty = async (disabled: boolean, deploy = true) => {
      const r = await renderScreen(RepositoryContent, {
        inputs: { project: 'web', key: 'generic-local' },
        setup: (get) => {
          const catalog = get(RepositoriesApi) as FakeRepositoriesApi;
          catalog.formatsResult = [GENERIC_FORMAT];
          catalog.repositories = catalog.repositories.map((x) => (x.key === 'generic-local' ? { ...x, disabled } : x));
          (get(FilesApi) as FakeFilesApi).children = () => async () => ({ items: [], nextCursor: null });
          (get(SessionApi) as FakeSessionApi).allow = (c) => deploy || c.action !== 'deploy';
        },
      });
      await settle(r.fixture);
      return r;
    };
    const r = await empty(false);
    const state = r.root.querySelector('quader-data-table quader-empty-state')!;
    // The caller may upload here (the fake allows every check): the toolbar offers the upload, the empty state says so (Q-UI-512).
    expect(text(state)).toContain('This repository holds no files yet. Upload files here or with your client.');
    expect(state.querySelector(':is(a, button)')).toBeNull();
    expect(text(r.root.querySelector('.files-actions')!)).toContain('Upload files');
    await expectAccessible(r.root);
    const off = await empty(true);
    expect(off.root.querySelector('quader-data-table quader-empty-state :is(a, button)')).toBeNull();
    expect(text(off.root.querySelector('quader-data-table quader-empty-state')!)).toBe('This repository holds no files yet.');
    // A reader is told who fills it; Set up client stands once, in the header (Q-UI-555).
    const reader = await empty(false, false);
    const said = reader.root.querySelector('quader-data-table quader-empty-state')!;
    expect(text(said)).toBe('This repository holds no files yet. Members who may upload to it fill it with their client.');
    expect(said.querySelector(':is(a, button)')).toBeNull();
    expect([...reader.root.querySelectorAll('a, button')].filter((b) => text(b) === 'Set up client')).toHaveLength(1);
  });

  it('R-UI-144 Q-UI-555 an empty package repository shows Set up client once, in its header, and its empty state says what fills it and for whom', async () => {
    const owner = await open(NPM_REPO, ({ packages }) => {
      packages.packages = [];
    });
    const state = owner.root.querySelector('quader-data-table quader-empty-state')!;
    expect(text(state)).toBe('This repository holds no packages yet. Publish a package with your client; Set up client shows how.');
    expect(state.querySelector(':is(a, button)')).toBeNull();
    expect([...owner.root.querySelectorAll('a, button')].filter((b) => text(b) === 'Set up client')).toHaveLength(1);
    expect(owner.root.querySelector('quader-page-header .page-header-actions a.btn')?.textContent?.trim()).toBe('Set up client');
    const reader = await open(NPM_REPO, ({ packages, session }) => {
      packages.packages = [];
      session.allow = (c) => c.action !== 'deploy';
    });
    expect(text(reader.root.querySelector('quader-data-table quader-empty-state')!)).toBe(
      'This repository holds no packages yet. Members who may publish to it fill it with their client.',
    );
  });

  it('R-UI-052 a virtual repository names what it resolves from and a remote one says it lists what clients fetched', async () => {
    const virtual = await open({ key: 'npm-all', project: 'web', class: 'virtual', format: 'npm', virtual: { members: ['npm-local', 'npm-remote'] } });
    expect(text(virtual.root)).toContain('Resolves from');
    expect(virtual.root.querySelector<HTMLAnchorElement>('.resolves a[href$="/repositories/npm-remote"]')).not.toBeNull();
    const remote = await open({ key: 'npm-cache', project: 'web', class: 'remote', format: 'npm' });
    expect(text(remote.root)).toContain('Only what clients have fetched through this repository is listed here, what it downloaded after a client asked about a version, and, in mirror mode, what a pass has fetched.');
  });

  it('R-OCI-036 an OCI remote repository lists the images it has cached like a local one, with the fetched time of a tag, and no longer says that they are not listed', async () => {
    const remote: RepoDoc = { key: 'oci-cache', project: 'web', class: 'remote', format: 'oci' };
    const r = await open(remote, ({ packages }) => {
      ociFixture(packages);
      packages.packages = packages.packages.map((p) => (p.format === 'oci' ? { ...p, repositories: ['web/oci-cache'] } : p));
    });
    const body = text(r.root);
    expect(body).not.toContain('not listed here yet');
    expect(body).toContain('Only the tags clients have pulled through this repository are listed here');
    const packages = r.get(PackagesApi) as FakePackagesApi;
    expect(packages.listFilters.at(-1)).toMatchObject({ repository: ['web/oci-cache'], format: ['oci'] });
    const opener = r.root.querySelector<HTMLButtonElement>('.name-cell button[aria-expanded]')!;
    opener.click();
    await settle(r.fixture);
    const headers = [...r.root.querySelectorAll('quader-repository-item-versions th')].map((th) => th.textContent?.trim());
    expect(headers).toContain('Fetched');
    expect(headers).not.toContain('Pushed');
    const rows = [...r.root.querySelectorAll('quader-repository-item-versions tbody tr')];
    expect(rows[0]?.textContent).toContain('sha256:');
    await expectAccessible(r.root);
  });

  it('R-OCI-036 an OCI remote repository with nothing cached says so and names the way to fill it', async () => {
    const remote: RepoDoc = { key: 'oci-cache', project: 'web', class: 'remote', format: 'oci' };
    const r = await open(remote, ({ packages }) => {
      packages.packages = [];
    });
    expect(text(r.root)).toContain('This repository has cached no images yet. It fills as clients pull images through it; Set up client shows how.');
  });

  it('R-UI-052 the page has no setup tab: its Set up client opens the setup for this repository', async () => {
    const r = await open(NPM_REPO);
    // One way to the setup (Q-UI-321): the header's action, no tab beside the content.
    expect([...r.root.querySelectorAll('[role="tab"]')].map((t) => text(t))).not.toContain('Set up client');
    const setup = r.root.querySelector('quader-page-header .page-header-actions a.btn');
    expect(setup?.getAttribute('href')).toBe('/projects/web/repositories/npm-local/setup');
    await expectAccessible(r.root);
  });

  it('R-UI-052 R-UI-169 the header holds no address; Details names the address for downloads by path, which Set up client does not show', async () => {
    const npm = 'https://quader.example.net/api/npm/web/npm-local/';
    const files = 'https://quader.example.net/files/web/npm-local/';
    const r = await open(NPM_REPO, ({ catalog }) => {
      // The consumer view of a reader: the protocol address first, the raw file address after it (R-REPO-010).
      catalog.get = async () => ({
        value: { key: 'npm-local', class: 'local', format: 'npm', description: 'Internal packages', layout: 'npm', addresses: [npm, files] },
        etag: '"1"',
      });
    });
    expect(r.root.querySelector('quader-page-header quader-address-field')).toBeNull();
    expect(text(r.root)).not.toContain(npm);
    const details = r.root.querySelector('quader-disclosure')!;
    expect(details.querySelector<HTMLInputElement>('quader-address-field input')?.value).toBe(files);
    expect(text(details)).toContain('Address for downloads by path (curl, wget)');
    expect(details.querySelector('quader-address-field button')?.getAttribute('aria-label')).toBe('Copy address for downloads by path');
    await expectAccessible(r.root);
  });

  it('R-UI-052 a disabled repository offers no Set up client and says that it is disabled', async () => {
    const r = await open({ ...NPM_REPO, disabled: true });
    expect([...r.root.querySelectorAll('quader-page-header a')].map((a) => text(a))).not.toContain('Set up client');
    expect(text(r.root.querySelector('quader-page-header')!)).toContain('disabled');
    await expectAccessible(r.root);
  });

  it('R-UI-052 the Settings action is offered to who can read the configuration, and leads to the settings route', async () => {
    const owner = await open(NPM_REPO);
    const settings = [...owner.root.querySelectorAll('.page-header-actions a')].find((a) => a.textContent?.trim() === 'Settings');
    expect(settings?.getAttribute('href')).toBe('/projects/web/repositories/npm-local/settings');
    const reader = await open(NPM_REPO, undefined, []);
    expect([...reader.root.querySelectorAll('.page-header-actions a')].map((a) => a.textContent?.trim())).not.toContain('Settings');
  });

  it('R-UI-169 Q-UI-664 R-UI-126 the header has no address field: Set up client, where the address stands, is the first of its actions, in the one style of that action', async () => {
    const r = await open(NPM_REPO);
    const header = r.root.querySelector('quader-page-header')!;
    expect(header.querySelector('quader-address-field, input')).toBeNull();
    const actions = header.querySelector('.page-header-actions')!;
    const order = [...actions.querySelectorAll('a.btn, button.btn')].map((e) => e.getAttribute('aria-label') ?? text(e));
    expect(order).toEqual(['Set up client', 'Settings', 'More actions']);
    const setup = actions.querySelector('a.btn')!;
    expect(setup.classList.contains('btn-default')).toBe(true);
    expect(setup.getAttribute('href')).toBe('/projects/web/repositories/npm-local/setup');
    await expectAccessible(r.root);
  });

  it('R-UI-052 the filter of the list is sent to the server and lives in the URL', async () => {
    const r = await open(NPM_REPO, undefined, undefined, { q: 'left' });
    const packages = r.get(PackagesApi) as FakePackagesApi;
    expect(packages.listFilters.at(-1)).toMatchObject({ q: 'left' });
    const items = r.fixture.debugElement.query((d) => d.componentInstance instanceof RepositoryItems);
    expect(items).toBeTruthy();
  });

  it('R-UI-096 R-UI-167 the visit counts once the repository was read; a repository its page does not find is forgotten and never recorded', async () => {
    const store = 'quader.frequent.00000000-0000-0000-0000-000000000001';
    browserStorage.clear('local');
    const found = await open(NPM_REPO);
    const remembered = (r: typeof found) => r.get(FrequentPlaces).recentRepositories().map((v) => v.id);
    expect(remembered(found)).toEqual(['web/npm-local']);

    // The page of a repository that was deleted since: the entry that led here goes, and the 404 records no visit.
    const missing = await renderScreen(RepositoryContent, {
      inputs: { project: 'web', key: 'gone' },
      setup: () => {
        const entries = ['web/gone', 'web/npm-local'].map((id, i) => ({ id, visits: 1, last: 1000 - i }));
        browserStorage.set('local', store, JSON.stringify({ repositories: entries, projects: [] }));
      },
    });
    await settle(missing.fixture);
    expect(remembered(missing)).toEqual(['web/npm-local']);
    browserStorage.clear('local');
  });
});
