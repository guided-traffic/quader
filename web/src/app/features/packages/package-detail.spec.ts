import { TestBed } from '@angular/core/testing';
import { Router } from '@angular/router';
import { PackagesApi } from '../../api/packages-api';
import { FakePackagesApi, ansibleDocsFixture, ansibleFixture, debFixture, ociFixture } from '../../api/testing/fakes-packages';
import { FakeVulnerabilitiesApi, sampleGrade } from '../../api/testing/fakes-vulnerabilities';
import { VulnerabilitiesApi } from '../../api/vulnerabilities-api';
import { buttonByText, expectAccessible, inProject, renderScreen, settle, text } from '../../testing/screen';
import { PackageDetail } from './package-detail';
import { PackageMemory } from './package-memory';
import { VERSION_PAGE } from './package-versions-model';

type Opened = Awaited<ReturnType<typeof open>>;

async function open(
  inputs: Record<string, unknown> = {},
  setup?: (fake: FakePackagesApi, vulns: FakeVulnerabilitiesApi) => void,
  name = '@team-a/ui',
) {
  const r = await renderScreen(
    PackageDetail,
    inProject({
      inputs: { format: 'npm', name, ...inputs },
      setup: (get) => setup?.(get(PackagesApi) as FakePackagesApi, get(VulnerabilitiesApi) as FakeVulnerabilitiesApi),
    }),
  );
  return { ...r, fake: r.get(PackagesApi) as FakePackagesApi };
}

async function set(r: Opened, inputs: Record<string, unknown>) {
  for (const [k, v] of Object.entries(inputs)) r.fixture.componentRef.setInput(k, v);
  await settle(r.fixture);
}

const rows = (r: Opened) => [...r.root.querySelectorAll('.pkg-versions tbody tr')];
const versions = (r: Opened) => rows(r).map((tr) => text(tr.querySelector('th a')!));
const headers = (r: Opened) =>
  [...r.root.querySelectorAll('.pkg-versions thead th')].map((th) =>
    text(th)
      .replace(/help_outline$/, '')
      .replace(/(arrow_downward|arrow_upward|unfold_more)$/, ''),
  );
const filter = (r: Opened) => r.root.querySelector<HTMLInputElement>('.versions-filter input')!;

/** Thirty-five versions of `@team-a/ui`, 1.0.0 to 1.34.0, published a day apart. */
function many(fake: FakePackagesApi): void {
  const template = fake.versionList.find((v) => v.version === '1.4.0')!;
  fake.versionList = fake.versionList.filter((v) => v.packageId !== template.packageId);
  for (let i = 0; i < 35; i++) {
    fake.versionList.push({
      ...template,
      version: `1.${i}.0`,
      publishedAt: new Date(Date.UTC(2026, 9, 1 + i)).toISOString(),
      sizeBytes: 1000 + i,
      lastDownloadedAt: i % 2 === 0 ? new Date(Date.UTC(2026, 10, 1 + i % 17)).toISOString() : null,
    });
  }
}

describe('The package page: its versions (R-UI-110)', () => {
  it('R-UI-007 R-UI-110 opens on the versions as a table, the highest version first, with the description in the header', async () => {
    const r = await open();
    expect(text(r.root.querySelector('h1')!)).toBe('@team-a/ui');
    expect(text(r.root.querySelector('.page-header-description')!)).toBe('Shared UI components');
    expect(versions(r)).toEqual(['1.5.0-beta.1', '1.4.0', '1.3.0']);
    expect(headers(r)).toEqual(['Version', 'Size', 'Published', 'Last download']);
    expect(r.root.querySelector('[role="tab"]')).toBeNull();
    expect(r.root.querySelector('select')).toBeNull();
    await expectAccessible(r.root);
  });

  it('R-UI-110 Q-UI-453 a row carries its distribution tags and the pre-release mark beside the version, and no digest', async () => {
    const r = await open();
    const row = (v: string) => rows(r).find((tr) => text(tr.querySelector('th a')!) === v)!;
    expect([...row('1.4.0').querySelectorAll('th .badge')].map((b) => text(b))).toEqual(['latest']);
    expect([...row('1.5.0-beta.1').querySelectorAll('th .badge')].map((b) => text(b))).toEqual(['next', 'pre-release']);
    expect(row('1.3.0').querySelectorAll('th .badge')).toHaveLength(0);
    expect(text(r.root.querySelector('.pkg-versions')!)).not.toMatch(/sha256:/);
  });

  it('R-UI-110 R-API-065 a row says how large the version is, when it was published and when a client last downloaded it, or that none did', async () => {
    const r = await open();
    const cells = (v: string) => [...rows(r).find((tr) => text(tr.querySelector('th a')!) === v)!.querySelectorAll('td')];
    const [size, published, fetched] = cells('1.4.0');
    expect(text(size!)).toBe('47.1 KiB');
    expect(published!.querySelector('time')?.getAttribute('datetime')).toBe('2026-10-20T09:12:44.512Z');
    expect(fetched!.querySelector('time')?.getAttribute('datetime')).toBe('2026-11-17T16:40:02.118Z');
    expect(text(cells('1.3.0')[2]!)).toBe('Never');
  });

  it('R-UI-111 Q-UI-451 a version links to its own page, the version after an @ in the last segment of the address', async () => {
    const r = await open();
    const hrefs = rows(r).map((tr) => tr.querySelector('th a')!.getAttribute('href'));
    expect(hrefs).toEqual([
      '/projects/web/packages/npm/@team-a/ui@1.5.0-beta.1',
      '/projects/web/packages/npm/@team-a/ui@1.4.0',
      '/projects/web/packages/npm/@team-a/ui@1.3.0',
    ]);
    expect(r.root.querySelector('.pkg-meta a')?.getAttribute('href')).toBe('/projects/web/packages/npm/@team-a/ui@1.4.0');
  });

  it('R-UI-110 R-UI-112 with grades the column Vulnerabilities shows each version\'s grade, total and fixable findings; without them there is no column', async () => {
    const graded = await open({}, (_, v) =>
      v.packageReports.set('pkg_01JBQ6V7S3K9', {
        packageId: 'pkg_01JBQ6V7S3K9',
        versions: [
          { version: '1.4.0', grade: sampleGrade('E', { critical: 13, high: 160, medium: 113, low: 16, unknown: 843 }, { fixable: 112 }) },
          { version: '1.3.0', grade: sampleGrade('N/A') },
        ],
      }),
    );
    expect(headers(graded)).toEqual(['Version', 'Size', 'Vulnerabilities', 'Published', 'Last download']);
    const cell = (v: string) => rows(graded).find((tr) => text(tr.querySelector('th a')!) === v)!.querySelectorAll('td')[1]!;
    expect(text(cell('1.4.0').querySelector('.grade-text')!)).toBe('Grade E · 1,145 findings · 112 fixable');
    expect(text(cell('1.3.0').querySelector('.grade-text')!)).toBe('Grade N/A · not scanned');
    await expectAccessible(graded.root);
    const plain = await open();
    expect(headers(plain)).not.toContain('Vulnerabilities');
    // Every version N/A, as a format that is not scanned answers: no column.
    const unscanned = await open({}, (_, v) =>
      v.packageReports.set('pkg_01JBQ6V7S3K9', {
        packageId: 'pkg_01JBQ6V7S3K9',
        versions: [{ version: '1.4.0', grade: sampleGrade('N/A') }, { version: '1.3.0', grade: sampleGrade('N/A') }],
      }),
    );
    expect(headers(unscanned)).not.toContain('Vulnerabilities');
  });

  it('R-UI-110 Q-UI-452 the filter narrows the rows as one types, by version and by tag, and the address follows without a new entry', async () => {
    const r = await open();
    const navigate = vi.spyOn(TestBed.inject(Router), 'navigate').mockResolvedValue(true);
    filter(r).value = 'beta';
    filter(r).dispatchEvent(new Event('input', { bubbles: true }));
    await settle(r.fixture);
    expect(versions(r)).toEqual(['1.5.0-beta.1']);
    expect(text(r.root.querySelector('.versions-count')!)).toBe('1 of 3 versions');
    expect(navigate).toHaveBeenCalledWith([], { queryParams: { q: 'beta' }, queryParamsHandling: 'merge', replaceUrl: true });
    await set(r, { q: 'latest' });
    expect(versions(r)).toEqual(['1.4.0']);
    await set(r, { q: 'zzz' });
    expect(rows(r)).toHaveLength(0);
    expect(text(r.root.querySelector('.pkg-versions')!)).toContain('No version matches “zzz”.');
  });

  it('R-UI-110 Q-UI-452 the columns sort in the browser and the sort is the address\'s; the default is left out of it', async () => {
    const r = await open();
    const navigate = vi.spyOn(TestBed.inject(Router), 'navigate').mockResolvedValue(true);
    const sortButton = (label: string) =>
      [...r.root.querySelectorAll<HTMLButtonElement>('.pkg-versions thead button.th-sort')].find((b) => text(b).startsWith(label))!;
    sortButton('Published').click();
    expect(navigate).toHaveBeenCalledWith([], { queryParams: { sort: 'published' }, queryParamsHandling: 'merge' });
    await set(r, { sort: '-fetched' });
    expect(versions(r)).toEqual(['1.4.0', '1.5.0-beta.1', '1.3.0']);
    await set(r, { sort: 'size' });
    expect(versions(r)).toEqual(['1.5.0-beta.1', '1.4.0', '1.3.0']);
    sortButton('Version').click();
    // The second click on the sorted column turns it: the lowest version first.
    expect(navigate).toHaveBeenLastCalledWith([], { queryParams: { sort: 'version' }, queryParamsHandling: 'merge' });
  });

  it('R-UI-110 Q-UI-452 shows 50 versions at first and 50 more with each Show more; a new search starts at 50 again', async () => {
    const r = await open({}, (fake) => {
      many(fake);
      for (let i = 35; i < 120; i++) {
        fake.versionList.push({ ...fake.versionList.at(-1)!, version: `2.${i}.0` });
      }
    });
    expect(rows(r)).toHaveLength(VERSION_PAGE);
    buttonByText(r.root, 'Load more').click();
    await settle(r.fixture);
    expect(rows(r)).toHaveLength(2 * VERSION_PAGE);
    await set(r, { q: '.' });
    expect(rows(r)).toHaveLength(VERSION_PAGE);
  });

  it('R-UI-110 Q-UI-457 the column Repositories appears only where the versions are held by different repositories', async () => {
    const same = await open();
    expect(headers(same)).not.toContain('Repositories');
    const differ = await open({}, (fake) => {
      const v = fake.versionList.find((x) => x.version === '1.3.0')!;
      v.repositories = [{ key: 'web/npm-remote', class: 'remote', publishedAt: v.publishedAt }];
    });
    expect(headers(differ)).toContain('Repositories');
    const cell = rows(differ).find((tr) => text(tr.querySelector('th a')!) === '1.3.0')!.querySelector('td:last-child')!;
    expect(text(cell)).toBe('npm-remote');
    expect(cell.querySelector('a')?.getAttribute('href')).toBe('/projects/web/repositories/npm-remote');
  });

  it('R-UI-110 R-UI-008 the README of the latest version follows the table; a package without one asks for none', async () => {
    const r = await open();
    expect(text(r.root.querySelector('.pkg-readme h2')!)).toBe('README of 1.4.0');
    expect(r.root.querySelector('.pkg-readme quader-package-readme .readme h1')?.textContent).toBe('@team-a/ui');
    expect(r.fake.readmeCalls).toEqual(['pkg_01JBQ6V7S3K9@1.4.0']);
    const none = await open({}, undefined, 'left-pad');
    expect(none.root.querySelector('.pkg-readme')).toBeNull();
    expect(none.fake.readmeCalls).toEqual([]);
  });

  it('R-UI-110 the facts of the package are its Details, closed: the repositories with links, the keywords, the downloads', async () => {
    const r = await open();
    const details = r.root.querySelector('quader-disclosure.pkg-details')!;
    expect(text(details)).toContain('Details');
    expect([...details.querySelectorAll('dd a')].map((a) => a.getAttribute('href'))).toEqual([
      '/projects/web/repositories/npm-local',
      '/projects/web/repositories/npm-all',
    ]);
    expect(text(details)).toContain('ui, components');
    expect(text(details)).toContain('1,832');
  });

  it('R-UI-007 opening the page records the package in "recently viewed"', async () => {
    const r = await open();
    expect(r.get(PackageMemory).recent()[0]).toEqual({ format: 'npm', name: '@team-a/ui' });
  });

  it('R-UI-007 an unknown package shows the not-found error', async () => {
    const r = await open({}, undefined, 'no-such-package');
    expect(text(r.root)).toContain('does not exist');
    await expectAccessible(r.root);
  });

  it('R-UI-110 every format has the page: an image lists its tags, the tag leading; NuGet, Debian and a collection their versions', async () => {
    const image = await open({ format: 'oci' }, (fake) => ociFixture(fake), 'team/app');
    expect(versions(image)).toEqual(['1.2.0', '1.1.0']);
    expect(rows(image)[0]!.querySelector('th a')?.getAttribute('href')).toBe('/projects/web/packages/oci/team/app@1.2.0');
    const nuget = await open({ format: 'nuget' }, undefined, 'Contoso.Logging');
    expect(versions(nuget)).toEqual(['2.3.1', '2.2.0']);
    const deb = await open({ format: 'deb' }, (fake) => debFixture(fake), 'hello');
    expect(versions(deb)).toEqual(['2.10-3', '2.9-1']);
    await expectAccessible(deb.root);
    const collection = await open({ format: 'ansible' }, (fake) => {
      ansibleFixture(fake);
      ansibleDocsFixture(fake);
    }, 'acme.tools');
    expect(versions(collection)).toEqual(['1.2.0', '1.0.0']);
    // The README of a collection is the first page of its documentation, on the version's page (R-UI-060).
    expect(collection.root.querySelector('.pkg-readme')).toBeNull();
    const docs = collection.root.querySelector('.pkg-docs-link a')!;
    expect(text(docs)).toBe('Documentation of 1.2.0');
    expect(docs.getAttribute('href')).toBe('/projects/web/packages/ansible/acme.tools@1.2.0?tab=documentation');
  });
});
