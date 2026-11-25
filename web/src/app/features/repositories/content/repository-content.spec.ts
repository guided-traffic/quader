import { ClientSetupApi, PackagesApi } from '../../../api/packages-api';
import { RepositoriesApi } from '../../../api/repositories-api';
import { ApiError } from '../../../api/problem';
import { VulnerabilitiesApi } from '../../../api/vulnerabilities-api';
import { FakeClientSetupApi, FakePackagesApi, OCI_PACKAGE, ociFixture, ociImagesFixture } from '../../../api/testing/fakes-packages';
import { FakeRepositoriesApi, GENERIC_FORMAT, OCI_FORMAT, PACKAGE_FORMAT } from '../../../api/testing/fakes-catalog';
import { FakeFilesApi } from '../../../api/testing/fakes-catalog';
import { FakeVulnerabilitiesApi, sampleGrade } from '../../../api/testing/fakes-vulnerabilities';
import { FilesApi } from '../../../api/files-api';
import { UpstreamApi } from '../../../api/upstream-api';
import { FakeUpstreamApi } from '../../../api/testing/fakes-upstream';
import { SessionApi } from '../../../api/session-api';
import { FakeSessionApi } from '../../../api/testing/fakes';
import { FrequentPlaces } from '../../../core/frequent-places';
import { spokenText } from '../../../testing/axe';
import { browserStorage, expectAccessible, renderScreen, settle, text } from '../../../testing/screen';
import { RepositoryContent } from './repository-content';
import { RepositoryItems } from './repository-items';
import { contentShapeOf } from './content-shape';
import { Component, signal } from '@angular/core';
import { ComponentFixture } from '@angular/core/testing';
import { Router } from '@angular/router';
import { vi } from 'vitest';

interface RepoDoc {
  readonly key: string;
  readonly project: string;
  readonly class: 'local' | 'remote' | 'virtual';
  readonly format: string;
  readonly description?: string;
  readonly disabled?: boolean;
  readonly virtual?: { members: string[] };
  readonly remote?: { url: string };
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

/** Clicks a version's button and returns the panel it opened (Q-UI-1066): the overlay hangs it outside the page's element. */
async function openMenu(r: { fixture: ComponentFixture<unknown> }, trigger: HTMLButtonElement): Promise<HTMLElement> {
  trigger.click();
  await settle(r.fixture);
  expect(trigger.getAttribute('aria-expanded')).toBe('true');
  return document.getElementById(trigger.getAttribute('aria-controls')!)!;
}

/** The one-line commands of a panel, a section's field each. */
const commands = (panel: Element): string[] => [...panel.querySelectorAll<HTMLInputElement>('quader-address-field input')].map((i) => i.value);

const OCI_REPO: RepoDoc ={ key: 'oci-local', project: 'web', class: 'local', format: 'oci', description: 'Images of the web team' };
const NPM_REPO: RepoDoc = { key: 'npm-local', project: 'web', class: 'local', format: 'npm', description: 'Internal packages' };

describe('Repository content page', () => {
  it('R-UI-216 a page whose reads failed while its backend restarted comes back by itself, with the formats that failed beside it', async () => {
    vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] });
    try {
      const gateway = (path: string): ApiError => new ApiError(502, null, null, null, { call: { method: 'GET', path } });
      const r = await open(NPM_REPO, ({ catalog }) => {
        const get = catalog.get.bind(catalog);
        const formats = catalog.formats.bind(catalog);
        catalog.get = async () => {
          catalog.get = get;
          throw gateway('/api/v1/projects/web/repositories/npm-local');
        };
        catalog.formats = async () => {
          catalog.formats = formats;
          throw gateway('/api/v1/formats');
        };
      });
      const notice = r.root.querySelector('[data-testid="error-notice"]')!;
      expect(notice.textContent).toContain('The server did not answer through the gateway in front of it.');
      expect(notice.textContent).toContain('This page tries again by itself in 3 seconds.');
      vi.advanceTimersByTime(3_000);
      await settle(r.fixture);
      expect(r.root.querySelector('[data-testid="error-notice"]')).toBeNull();
      expect(r.root.querySelector('h1')?.textContent).toContain('npm-local');
      expect(text(r.root)).not.toContain('not scanned for vulnerabilities yet');
    } finally {
      vi.useRealTimers();
    }
  });

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
    // The highest version first, one row per image with the digest of its manifest.
    const rows = [...r.root.querySelectorAll('quader-repository-item-versions tbody tr')];
    expect(rows[0]?.textContent).toContain('1.2.0');
    expect(rows[0]?.textContent).toContain('sha256:');
    // No box of a command stands in the opened row: each tag has its button Pull (Q-UI-1066).
    expect(r.root.querySelector('quader-repository-item-versions quader-address-field')).toBeNull();
    const panel = await openMenu(r, rows[0]!.querySelector<HTMLButtonElement>('quader-client-command-menu button')!);
    expect(commands(panel)[0]).toMatch(/^docker pull \S*web\/oci-local\/team\/app:1\.2\.0$/);
    await expectAccessible(r.root);
    await expectAccessible(panel);
  });

  it('R-UI-215 an opened image lists its ten newest images under a head of its own and links all of its tags, with no Load more', async () => {
    const r = await open(OCI_REPO, ({ packages }) => {
      // Thirteen images of a version and a commit tag each, pushed an hour apart, the highest version the newest (Q-UI-1130); 1.10.0 is
      // above 1.9.0 by the version's order, not by its text.
      ociImagesFixture(packages, Array.from({ length: 13 }, (_, i) => [`1.${12 - i}.0`, `sha-${String(i).padStart(7, 'a')}`]));
    });
    r.root.querySelector<HTMLButtonElement>('.name-cell button[aria-expanded]')!.click();
    await settle(r.fixture);
    const opened = r.root.querySelector('quader-repository-item-versions')!;
    // The newest ten, under the list's own head (its columns are the tags', not the images').
    const leads = [...opened.querySelectorAll(':scope table > tbody > tr')].map((tr) => text(tr.querySelector('th a')!));
    expect(leads).toEqual(['1.12.0', '1.11.0', '1.10.0', '1.9.0', '1.8.0', '1.7.0', '1.6.0', '1.5.0', '1.4.0', '1.3.0']);
    expect([...opened.querySelectorAll(':scope table > thead th')].map((th) => text(th))[0]).toBe('Tag');
    expect(opened.querySelector('.table-more')).toBeNull();
    const all = opened.querySelector<HTMLAnchorElement>('.all-versions a')!;
    // The image's row counts its tags, and so does the link (R-UI-052).
    expect(spokenText(all)).toBe('All 26 tags: team/app');
    expect(all.getAttribute('href')).toBe('/projects/web/packages/oci/team/app');
    await expectAccessible(r.root);
  });

  it('R-UI-052 R-SIG-035 an opened image is one row per image: the leading tag, its other tags muted after it (four, then +N more), its signature badge, asked as the list by content', async () => {
    const r = await open(OCI_REPO, ({ packages }) => {
      ociImagesFixture(packages, [['5.4.7', '5.4', '5', 'latest', 'stable', 'sha-f00cf6c'], ['5.4.6', 'sha-6b24a87'], ['5.4.5', 'sha-ed7fd56']], [0]);
    });
    const packages = r.get(PackagesApi) as FakePackagesApi;
    r.root.querySelector<HTMLButtonElement>('.name-cell button[aria-expanded]')!.click();
    await settle(r.fixture);
    expect(packages.versionFilters.at(-1)).toEqual({ repository: ['web/oci-local'], sort: '-version', group: 'content' });
    expect(packages.versionPages.at(-1)).toEqual({ cursor: null, limit: 10 });
    const opened = r.root.querySelector('quader-repository-item-versions')!;
    const rows = [...opened.querySelectorAll(':scope table > tbody > tr')];
    expect(rows).toHaveLength(3);
    const first = rows[0]!.querySelector('th quader-image-tags')!;
    expect(text(first.querySelector('.lead')!)).toBe('5.4.7');
    // The version tags first, then those that are no version, newest first; the sixth tag is counted, not named.
    expect([...first.querySelectorAll('.other [translate="no"]')].map((t) => text(t))).toEqual(['5.4', '5', 'latest', 'sha-f00cf6c']);
    expect(text(first.querySelector('.more')!)).toBe('+1 more');
    expect(spokenText(first.querySelector('.other')!)).toBe('Other tags of team/app 5.4.7: 5.4');
    expect(first.querySelector('[data-testid="signature-badge"]')?.getAttribute('data-state')).toBe('verified_internal');
    expect(rows[1]!.querySelector('[data-testid="signature-badge"]')).toBeNull();
    expect([...rows[1]!.querySelectorAll('.other [translate="no"]')].map((t) => text(t))).toEqual(['sha-6b24a87']);
    // Every row has its manifest's digest; Pull is the leading tag's.
    expect(rows.every((tr) => tr.querySelector('.digest-cell code')?.textContent?.startsWith('sha256:'))).toBe(true);
    expect(spokenText(rows[0]!.querySelector('quader-client-command-menu button')!)).toBe('5.4.7: Pull');
    // A row leaves a tag out: the link to all of them stands under the three images.
    expect(spokenText(opened.querySelector('.all-versions a')!)).toBe('All 10 tags: team/app');
    await expectAccessible(r.root);
  });

  it('R-UI-215 an opened image whose rows name every tag has no link to all of them, though it has more than ten tags', async () => {
    const r = await open(OCI_REPO, ({ packages }) => {
      ociImagesFixture(packages, [['2.0.0', '2.0', '2', 'sha-0000001'], ['1.9.0', '1.9', 'sha-0000002', 'stable'], ['1.8.0', 'sha-0000003']]);
    });
    r.root.querySelector<HTMLButtonElement>('.name-cell button[aria-expanded]')!.click();
    await settle(r.fixture);
    const opened = r.root.querySelector('quader-repository-item-versions')!;
    expect(opened.querySelectorAll(':scope table > tbody > tr')).toHaveLength(3);
    expect(opened.querySelector('.more')).toBeNull();
    expect(opened.querySelector('.all-versions')).toBeNull();
  });

  it('R-API-010 an opened image reads on after a short and an empty page until it has its ten images', async () => {
    const r = await open(OCI_REPO, ({ packages }) => {
      ociImagesFixture(packages, Array.from({ length: 13 }, (_, i) => [`1.${12 - i}.0`, `sha-${String(i).padStart(7, 'b')}`]));
      // The server's read budget ends twice before the page is whole (Q-API-432).
      packages.versionPageSizes = [3, 0];
    });
    const packages = r.get(PackagesApi) as FakePackagesApi;
    r.root.querySelector<HTMLButtonElement>('.name-cell button[aria-expanded]')!.click();
    await settle(r.fixture);
    expect(packages.versionPages.map((p) => p.limit)).toEqual([10, 7, 7]);
    const opened = r.root.querySelector('quader-repository-item-versions')!;
    expect(opened.querySelectorAll(':scope table > tbody > tr')).toHaveLength(10);
    expect(opened.querySelector('.all-versions')).not.toBeNull();
  });

  it('R-UI-213 an opened image is read again every 3 s while a version it shows waits for its scan, and no longer once none waits', async () => {
    vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] });
    try {
      const report = (state: 'queued' | 'checked') => ({
        packageId: OCI_PACKAGE.id,
        versions: [
          { version: '1.2.0', grade: sampleGrade('A', {}, { state: 'checked' }) },
          { version: '1.1.0', grade: state === 'queued' ? sampleGrade('N/A', {}, { state, stateSince: '2026-11-25T08:00:00Z' }) : sampleGrade('A') },
        ],
      });
      const r = await open(OCI_REPO, ({ packages, vulnerabilities }) => {
        ociFixture(packages);
        packages.packages = packages.packages.map((p) => ({ ...p, vulnerabilityGrade: sampleGrade('A') }));
        vulnerabilities.packageReports.set(OCI_PACKAGE.id, report('queued'));
      });
      const packages = r.get(PackagesApi) as FakePackagesApi;
      const vulnerabilities = r.get(VulnerabilitiesApi) as FakeVulnerabilitiesApi;
      r.root.querySelector<HTMLButtonElement>('.name-cell button[aria-expanded]')!.click();
      await settle(r.fixture);
      const reads = () => packages.versionPages.length;
      const before = reads();
      vi.advanceTimersByTime(3_000);
      await settle(r.fixture);
      expect(reads()).toBe(before + 1);
      // The older image's scan ends: the next read shows it, and the reads stop, with the item's row unchanged.
      vulnerabilities.packageReports.set(OCI_PACKAGE.id, report('checked'));
      vi.advanceTimersByTime(3_000);
      await settle(r.fixture);
      expect(reads()).toBe(before + 2);
      vi.advanceTimersByTime(9_000);
      await settle(r.fixture);
      expect(reads()).toBe(before + 2);
    } finally {
      vi.useRealTimers();
    }
  });

  it('R-UI-215 an opened item with ten versions or fewer has no link to all of them', async () => {
    const r = await open(NPM_REPO, ({ packages }) => {
      packages.packages = packages.packages.map((p) => ({ ...p, repositories: ['web/npm-local'] }));
    });
    r.root.querySelector<HTMLButtonElement>('.name-cell button[aria-expanded]')!.click();
    await settle(r.fixture);
    const opened = r.root.querySelector('quader-repository-item-versions')!;
    expect(opened.querySelectorAll(':scope table > tbody > tr').length).toBeGreaterThan(0);
    expect(opened.querySelector('.all-versions')).toBeNull();
  });

  it('R-UI-052 a tag\'s Pull opens the commands of that tag, a tab per client, and the image row\'s Pull those of its latest tag', async () => {
    const r = await open(OCI_REPO, ({ packages }) => ociFixture(packages));
    const setup = r.get(ClientSetupApi) as FakeClientSetupApi;
    const imageRow = [...r.root.querySelectorAll('tbody tr')].find((tr) => tr.textContent?.includes('team/app'))!;
    const latest = imageRow.querySelector<HTMLButtonElement>('quader-client-command-menu button')!;
    expect(spokenText(latest)).toBe('1.2.0: Pull');
    // Nothing is asked before the panel opens.
    expect(setup.calls).toEqual([]);
    const forLatest = await openMenu(r, latest);
    expect([...forLatest.querySelectorAll('[role="tab"]')].map((h) => text(h))).toEqual(['Docker', 'Podman']);
    expect(commands(forLatest).map((c) => c.split(' ').slice(0, 2).join(' '))).toEqual(['docker pull', 'podman pull']);
    expect(commands(forLatest).every((c) => c.endsWith('team/app:1.2.0'))).toBe(true);
    latest.click();
    await settle(r.fixture);

    imageRow.querySelector<HTMLButtonElement>('.name-cell button[aria-expanded]')!.click();
    await settle(r.fixture);
    const rows = [...r.root.querySelectorAll('quader-repository-item-versions tbody tr')];
    setup.calls = [];
    const older = await openMenu(r, rows.at(-1)!.querySelector<HTMLButtonElement>('quader-client-command-menu button')!);
    expect(setup.calls.map((c) => c.query)).toEqual([
      expect.objectContaining({ client: 'docker', version: '1.1.0', credentials: 'none' }),
      expect.objectContaining({ client: 'podman', version: '1.1.0', credentials: 'none' }),
    ]);
    expect(setup.calls.every((c) => c.repository === 'web/oci-local')).toBe(true);
    expect(commands(older)[1]).toMatch(/^podman pull \S*web\/oci-local\/team\/app:1\.1\.0$/);
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
    const version = [...r.root.querySelectorAll('quader-repository-item-versions tbody tr')].find((tr) => tr.querySelector('th')?.textContent?.trim() === '1.4.0')!.querySelector<HTMLButtonElement>('quader-client-command-menu button')!;
    expect(spokenText(version)).toBe('1.4.0: Install');
    const panel = await openMenu(r, version);
    expect([...panel.querySelectorAll('[role="tab"]')].map((h) => text(h))).toEqual(['npm', 'pnpm', 'Yarn Berry', 'Yarn Classic']);
    expect(commands(panel)[0]).toBe('npm install @team-a/ui@1.4.0');
    await expectAccessible(r.root);
    await expectAccessible(panel);
  });

  it('R-UI-094 the column of grades sorts the list by grade, worst first descending, after the name', async () => {
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

  it('R-UI-094 a list without grades has no column of grades', async () => {
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

  it('R-UI-192 the page of a system repository offers no client action: no Set up client, no upload, no sentence about clients', async () => {
    const trivy = await open(
      { key: 'scanner-trivy', project: 'upstream', class: 'remote', format: 'oci', description: 'Image scanning database' },
      ({ packages }) => {
        packages.packages = [];
      },
    );
    expect([...trivy.root.querySelectorAll('a, button')].some((b) => text(b) === 'Set up client')).toBe(false);
    expect([...trivy.root.querySelectorAll('a')].some((a) => (a.getAttribute('href') ?? '').endsWith('/setup'))).toBe(false);
    expect(text(trivy.root.querySelector('quader-data-table quader-empty-state')!)).toBe('This repository has cached no images yet.');
    expect(text(trivy.root)).not.toContain('clients have pulled');
    expect(text(trivy.root.querySelector('quader-page-header .page-header-actions')!)).toContain('Settings');

    // The local one: the server refuses every deploy to it (R-REPO-075), so neither the toolbar nor the empty state offers one; it
    // lists its databases (R-UI-194), none yet, and while ClamAV definition updates is off it has no Set up client.
    const clamav = await renderScreen(RepositoryContent, {
      inputs: { project: 'upstream', key: 'clamav' },
      setup: (get) => {
        const catalog = get(RepositoriesApi) as FakeRepositoriesApi;
        catalog.repositories = [
          ...catalog.repositories,
          { key: 'clamav', project: 'upstream', class: 'local', format: 'clamav', description: 'ClamAV definitions' } as never,
        ];
        (get(FilesApi) as FakeFilesApi).tree.set('upstream/clamav', []);
      },
    });
    await settle(clamav.fixture);
    // A repository of a system-only format is shown as System, says that the installation fills it, and its badge says it again as
    // the tooltip (Q-UI-1203); the remote system repository keeps its class.
    const badge = clamav.root.querySelector<HTMLElement>('quader-page-header .class-badge')!;
    expect(text(badge)).toBe('settings System');
    expect(badge.getAttribute('tabindex')).toBe('0');
    expect(text(clamav.root.querySelector('.page-header-description')!)).toBe(
      'Filled by this installation itself, on a schedule; nobody publishes to it.',
    );
    expect(text(trivy.root.querySelector('quader-page-header .class-badge')!)).toBe('cloud_download Remote');
    expect(clamav.root.querySelector('.files-actions')?.textContent ?? '').not.toMatch(/Upload files|New folder/);
    expect(text(clamav.root.querySelector('quader-clamav-databases quader-empty-state')!)).toBe(
      "Quader has stored no ClamAV definitions yet. The malware scan's updater fills this repository.",
    );
    expect([...clamav.root.querySelectorAll('a, button')].some((b) => text(b) === 'Set up client')).toBe(false);
  });

  it('R-UI-193 the images of a system repository have no pull command and no button Pull, and no address for downloads by path', async () => {
    const trivy = 'https://quader.example.net/v2/upstream/scanner-trivy/';
    const files = 'https://quader.example.net/files/upstream/scanner-trivy/';
    const r = await open({ key: 'scanner-trivy', project: 'upstream', class: 'remote', format: 'oci', description: 'Image scanning database' }, ({ catalog, packages }) => {
      ociFixture(packages);
      packages.packages = packages.packages.map((p) => ({ ...p, repositories: ['upstream/scanner-trivy'] }));
      // The consumer view lists the protocol address and the address for downloads by path, as for any OCI repository.
      catalog.get = async () => ({
        value: { key: 'scanner-trivy', class: 'remote', format: 'oci', description: 'Image scanning database', layout: 'oci', addresses: [trivy, files] },
        etag: '"1"',
      });
    });
    r.root.querySelector<HTMLButtonElement>('.name-cell button[aria-expanded]')!.click();
    await settle(r.fixture);
    const versions = r.root.querySelector('quader-repository-item-versions')!;
    expect(versions.querySelectorAll('tbody tr').length).toBeGreaterThan(0);
    expect(r.root.querySelector('quader-client-command-menu')).toBeNull();
    expect(text(r.root)).not.toContain('Pull');
    expect([...r.root.querySelectorAll('thead th')].map((th) => text(th))).not.toContain('Actions');
    expect((r.get(ClientSetupApi) as FakeClientSetupApi).calls).toEqual([]);
    expect(r.root.querySelector('quader-address-field')).toBeNull();
    // The consumer view has no upstream and the address for downloads by path is not offered: no tab Details.
    expect([...r.root.querySelectorAll('quader-repository-tabs a.tab-link')].map((a) => text(a))).toEqual(['Images', 'Upstream', 'Downloads']);
    expect(text(r.root)).not.toContain('Address for downloads by path');
    await expectAccessible(r.root);
  });

  it('R-UI-193 a file of a system repository offers no Download, no download address and no download with curl, and says why in one sentence; its folder copies no address', async () => {
    const at = async (filePath: string) => {
      const r = await renderScreen(RepositoryContent, {
        inputs: { project: 'upstream', key: 'clamav', filePath },
        setup: (get) => {
          const catalog = get(RepositoriesApi) as FakeRepositoriesApi;
          catalog.formatsResult = [GENERIC_FORMAT];
          catalog.repositories = [
            ...catalog.repositories,
            { key: 'clamav', project: 'upstream', class: 'local', format: 'generic', description: 'ClamAV definitions' } as never,
          ];
          (get(FilesApi) as FakeFilesApi).tree.set('upstream/clamav', [
            { path: 'main', kind: 'folder' },
            { path: 'main/main.cvd', kind: 'file', size: 4096 },
          ]);
        },
      });
      await settle(r.fixture);
      return r;
    };
    const file = await at('main/main.cvd');
    // No one deletes a definition file by hand (Q-REPO-416), and while the switch is off no client downloads it.
    expect(text(file.root.querySelector('.file-actions')!)).not.toContain('Delete');
    expect(text(file.root.querySelector('.file-actions')!)).not.toContain('Download');
    expect(file.root.querySelector('quader-address-field')).toBeNull();
    expect(text(file.root.querySelector('[data-testid="file-system"]')!)).toBe(
      "Only Quader's malware scan reads this file while ClamAV definition updates is off; clients cannot download it.",
    );
    const details = file.root.querySelector<HTMLElement>('quader-disclosure.details')!;
    details.querySelector<HTMLButtonElement>('button[aria-expanded]')!.click();
    await settle(file.fixture);
    // The item's request stays: the management API answers it; the download by path does not.
    const commands = [...details.querySelectorAll('pre.code-block')].map((e) => e.textContent ?? '').filter((b) => b.startsWith('curl'));
    expect(commands).toHaveLength(1);
    expect(commands[0]).toContain('/items?path=');
    expect(text(details)).not.toContain('Download with curl');
    await expectAccessible(file.root);

    const folder = await at('main');
    expect(folder.root.querySelectorAll('quader-data-table tbody tr')).toHaveLength(1);
    expect(folder.root.querySelector('quader-data-table quader-copy-button')).toBeNull();
    expect([...folder.root.querySelectorAll('quader-data-table thead th')].map((th) => text(th))).toEqual(['Name', 'Size', 'Last change']);
  });

  it('R-UI-144 an empty package repository shows Set up client once, in its header, and its empty state says what fills it and for whom', async () => {
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

  it('R-UI-052 a virtual repository names what it resolves from on its tab Details and a remote one says it lists what clients fetched', async () => {
    const virtual = await open(
      { key: 'npm-all', project: 'web', class: 'virtual', format: 'npm', virtual: { members: ['npm-local', 'npm-remote'] } },
      undefined,
      undefined,
      { tab: 'details' },
    );
    const details = virtual.root.querySelector('.repository-details')!;
    expect(text(details)).toMatch(/Resolves from ?npm-local, npm-remote/);
    expect(details.querySelector<HTMLAnchorElement>('a[href$="/repositories/npm-remote"]')).not.toBeNull();
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

  it('R-UI-052 R-UI-169 the header holds no address; the tab Details names the address for downloads by path, which Set up client does not show', async () => {
    const npm = 'https://quader.example.net/api/npm/web/npm-local/';
    const files = 'https://quader.example.net/files/web/npm-local/';
    const r = await open(
      NPM_REPO,
      ({ catalog }) => {
        // The consumer view of a reader: the protocol address first, the raw file address after it (R-REPO-010).
        catalog.get = async () => ({
          value: { key: 'npm-local', class: 'local', format: 'npm', description: 'Internal packages', layout: 'npm', addresses: [npm, files] },
          etag: '"1"',
        });
      },
      undefined,
      { tab: 'details' },
    );
    expect(r.root.querySelector('quader-page-header quader-address-field')).toBeNull();
    expect(text(r.root)).not.toContain(npm);
    const details = r.root.querySelector('.repository-details')!;
    expect(details.querySelector<HTMLInputElement>('quader-address-field input')?.value).toBe(files);
    expect(text(details)).toContain('Address for downloads by path (curl, wget)');
    expect(details.querySelector('quader-address-field button')?.getAttribute('aria-label')).toBe('Copy address for downloads by path');
    await expectAccessible(r.root);
  });

  it('R-UI-052 Details is a tab of its own, the last, with its address, in place of a section under the list', async () => {
    // The full document of an Owner: the address for downloads by path is built as the reader's view lists it.
    const local = await open(NPM_REPO);
    const links = [...local.root.querySelectorAll<HTMLAnchorElement>('quader-repository-tabs a.tab-link')];
    expect(links.map((a) => text(a))).toEqual(['Packages', 'Details']);
    expect(links[0]!.getAttribute('aria-current')).toBe('page');
    expect(links[1]!.getAttribute('href')).toBe('/projects/web/repositories/npm-local/details');
    expect(local.root.querySelector('quader-disclosure, .repository-details')).toBeNull();
    expect(local.root.querySelector('quader-repository-items')).not.toBeNull();

    const tab = await open(NPM_REPO, undefined, undefined, { tab: 'details' });
    const current = [...tab.root.querySelectorAll<HTMLAnchorElement>('quader-repository-tabs a.tab-link')];
    expect(current[1]!.getAttribute('aria-current')).toBe('page');
    expect(tab.root.querySelector('quader-repository-items')).toBeNull();
    expect(text(tab.root.querySelector('.repository-details')!)).toContain('Address for downloads by path (curl, wget)');
    expect(tab.root.querySelector<HTMLInputElement>('.repository-details quader-address-field input')?.value).toMatch(/\/files\/web\/npm-local\/?$/);
    await expectAccessible(tab.root);

    // A remote repository's strip ends with Details, after Upstream and Downloads; its Details names the upstream.
    const remote = await open({ key: 'npm-cache', project: 'web', class: 'remote', format: 'npm', remote: { url: 'https://registry.npmjs.org' } }, undefined, undefined, { tab: 'details' });
    expect([...remote.root.querySelectorAll('quader-repository-tabs a.tab-link')].map((a) => text(a))).toEqual(['Packages', 'Upstream', 'Downloads', 'Details']);
    expect(text(remote.root.querySelector('.repository-details')!)).toMatch(/Caches ?https:\/\/registry\.npmjs\.org/);
  });

  it('R-UI-052 a repository without details has no strip, and the address of its tab Details leads to its content', async () => {
    const r = await renderScreen(RepositoryContent, {
      inputs: { project: 'web', key: 'generic-local', tab: 'details' },
      setup: (get) => {
        (get(RepositoriesApi) as FakeRepositoriesApi).formatsResult = [GENERIC_FORMAT];
        (get(FilesApi) as FakeFilesApi).children = () => async () => ({ items: [], nextCursor: null });
      },
    });
    await settle(r.fixture);
    expect(r.root.querySelector('quader-repository-tabs')).toBeNull();
    expect(r.get(Router).url).toBe('/projects/web/repositories/generic-local');
  });

  it('R-UI-052 a disabled repository offers no Set up client and says that it is disabled', async () => {
    const r = await open({ ...NPM_REPO, disabled: true });
    expect([...r.root.querySelectorAll('quader-page-header a')].map((a) => text(a))).not.toContain('Set up client');
    expect(text(r.root.querySelector('quader-page-header')!)).toContain('Disabled');
    await expectAccessible(r.root);
  });

  it('R-UI-190 a retired registry of upstream says No longer offered and what that means to who reads the configuration; any other reader sees Disabled', async () => {
    const npmjs: RepoDoc = { key: 'npmjs', project: 'upstream', class: 'remote', format: 'npm', description: 'npm public registry', disabled: true };
    const admin = await renderScreen(RepositoryContent, {
      inputs: { project: 'upstream', key: 'npmjs' },
      capabilities: ['repositories:read-config'],
      setup: (get) => {
        const catalog = get(RepositoriesApi) as FakeRepositoriesApi;
        catalog.repositories = [...catalog.repositories, npmjs as never];
        catalog.formatsResult = [GENERIC_FORMAT, PACKAGE_FORMAT, OCI_FORMAT];
        (get(UpstreamApi) as FakeUpstreamApi).set('npmjs', { state: 'retired' });
      },
    });
    await settle(admin.fixture);
    const header = text(admin.root.querySelector('quader-page-header')!);
    expect(header).toContain('No longer offered');
    expect(header).not.toContain('Disabled');
    expect(text(admin.root.querySelector('.banner-warning > span:not(.material-icons)')!)).toBe(
      'Quader no longer offers this registry. It serves nothing, its cached content is removed, and a virtual repository that lists it skips it.',
    );
    await expectAccessible(admin.root);

    const reader = await open(npmjs);
    expect(text(reader.root.querySelector('quader-page-header')!)).toContain('Disabled');
    expect(reader.root.querySelector('.banner-warning')).toBeNull();
  });

  it('R-UI-052 the Settings action is offered to who can read the configuration, and leads to the settings route', async () => {
    const owner = await open(NPM_REPO);
    const settings = [...owner.root.querySelectorAll('.page-header-actions a')].find((a) => a.textContent?.trim() === 'Settings');
    expect(settings?.getAttribute('href')).toBe('/projects/web/repositories/npm-local/settings');
    const reader = await open(NPM_REPO, undefined, []);
    expect([...reader.root.querySelectorAll('.page-header-actions a')].map((a) => a.textContent?.trim())).not.toContain('Settings');
  });

  it('R-UI-169 R-UI-126 the header has no address field: Set up client, where the address stands, is the first of its actions, in the one style of that action', async () => {
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

  it('R-UI-213 the images are read again every 30 s in place: a pushed image appears, an open row stays open and its tags follow a push', async () => {
    vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] });
    try {
      const r = await open(OCI_REPO, ({ packages }) => ociFixture(packages));
      const packages = r.get(PackagesApi) as FakePackagesApi;
      const imageRow = () => [...r.root.querySelectorAll('tbody tr')].find((tr) => tr.textContent?.includes('team/app'))!;
      const row = imageRow();
      const opener = row.querySelector<HTMLButtonElement>('.name-cell button[aria-expanded]')!;
      opener.click();
      await settle(r.fixture);
      const versionRows = () => [...r.root.querySelectorAll('quader-repository-item-versions tbody tr')];
      const tags = () => versionRows().map((tr) => text(tr.querySelector('th a')!));
      expect(tags()).toEqual(['1.2.0', '1.1.0']);
      const app = packages.packages.find((p) => p.name === 'team/app')!;
      const tag = packages.versionList.find((v) => v.packageId === app.id && v.version === '1.2.0')!;
      // A client pushes the tag 1.3.0 onto the image of 1.2.0, and a second image.
      packages.versionList = [...packages.versionList, { ...tag, version: '1.3.0', publishedAt: '2026-11-24T10:00:00.000Z' }];
      packages.packages = [
        ...packages.packages.map((p) => (p.id === app.id ? { ...p, latestVersion: '1.3.0', versionCount: p.versionCount + 1, updatedAt: '2026-11-24T10:00:00.000Z' } : p)),
        { ...app, id: 'pkg_pushed', name: 'team/worker' },
      ];
      vi.advanceTimersByTime(30_000);
      await settle(r.fixture);
      expect(text(r.root.querySelector('quader-repository-items tbody')!)).toContain('team/worker');
      expect(imageRow()).toBe(row);
      expect(opener.getAttribute('aria-expanded')).toBe('true');
      // The image's row is led by its new tag, with the tag it had beside it (Q-UI-1130).
      expect(tags()).toEqual(['1.3.0', '1.1.0']);
      expect([...versionRows()[0]!.querySelectorAll('.other [translate="no"]')].map((t) => text(t))).toEqual(['1.2.0']);
    } finally {
      vi.useRealTimers();
    }
  });
});
