import { TestBed } from '@angular/core/testing';
import { Router } from '@angular/router';
import { PackagesApi } from '../../api/packages-api';
import { FakePackagesApi, ansibleDocsFixture, ansibleFixture, debFixture, ociFixture, ociImagesFixture } from '../../api/testing/fakes-packages';
import { clamavFixture } from '../../api/testing/fakes-clamav';
import { FakeVulnerabilitiesApi, sampleGrade } from '../../api/testing/fakes-vulnerabilities';
import { VulnerabilitiesApi, VulnerabilityGrade } from '../../api/vulnerabilities-api';
import { buttonByText, expectAccessible, inProject, renderScreen, settle, text } from '../../testing/screen';
import { ClientSetupReads } from '../../api/package-reads';
import { FakeClientSetupReads } from '../../api/testing/fakes-packages';
import { spokenText } from '../../testing/axe';
import { PackageDetail } from './package-detail';
import { RepositoriesApi } from '../../api/repositories-api';
import { FakeRepositoriesApi, GENERIC_FORMAT, OCI_FORMAT } from '../../api/testing/fakes-catalog';
import { PackageMemory } from './package-memory';
import { VERSION_PAGE } from '../../views/packages/package-versions-model';

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
      .replace(/info$/, '')
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
    expect(headers(r)).toEqual(['Version', 'Size', 'Published', 'Last download', 'Actions']);
    expect(r.root.querySelector('[role="tab"]')).toBeNull();
    expect(r.root.querySelector('select')).toBeNull();
    await expectAccessible(r.root);
  });

  it('R-UI-110 a row carries its distribution tags and the pre-release mark beside the version, and no digest', async () => {
    const r = await open();
    const row = (v: string) => rows(r).find((tr) => text(tr.querySelector('th a')!) === v)!;
    expect([...row('1.4.0').querySelectorAll('th .badge')].map((b) => text(b))).toEqual(['latest']);
    expect([...row('1.5.0-beta.1').querySelectorAll('th .badge')].map((b) => text(b))).toEqual(['next', 'Pre-release']);
    expect(row('1.3.0').querySelectorAll('th .badge')).toHaveLength(0);
    expect(text(r.root.querySelector('.pkg-versions')!)).not.toMatch(/sha256:/);
    // The tag's badge is the tab Upstream's (dist-tags.ts, Q-UI-1190): its tooltip says what it is, also to assistive technology.
    const latest = row('1.4.0').querySelector<HTMLElement>('th .dist-tag')!;
    const described = document.getElementById(latest.getAttribute('aria-describedby') ?? '')?.textContent ?? '';
    expect(described).toMatch(/^npm tag “latest”: npm install \S+@latest installs this version\.$/);
  });

  it('R-UI-110 R-API-065 R-UI-222 a row says how large the version is, when it was published, when a client last downloaded it, or that none did, and how often', async () => {
    const r = await open();
    const cells = (v: string) => [...rows(r).find((tr) => text(tr.querySelector('th a')!) === v)!.querySelectorAll('td')];
    const [size, published, fetched] = cells('1.4.0');
    expect(text(size!)).toBe('47.1 KiB');
    // The downloads of every format's table, under the last download (R-UI-222).
    expect(text(fetched!.querySelector('[data-testid="version-downloads"]')!)).toBe('410 downloads');
    expect(published!.querySelector('time')?.getAttribute('datetime')).toBe('2026-10-20T09:12:44.512Z');
    expect(fetched!.querySelector('time')?.getAttribute('datetime')).toBe('2026-11-17T16:40:02.118Z');
    expect(text(cells('1.3.0')[2]!.querySelector('.muted')!)).toBe('Never');
  });

  it('R-UI-111 a version links to its own page, the version after an @ in the last segment of the address', async () => {
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
    expect(headers(graded)).toEqual(['Version', 'Size', 'Vulnerabilities', 'Published', 'Last download', 'Actions']);
    const cell = (v: string) => rows(graded).find((tr) => text(tr.querySelector('th a')!) === v)!.querySelectorAll('td')[1]!;
    expect(text(cell('1.4.0').querySelector('.grade-text')!)).toBe('Grade E · 1,145 findings · 112 fixable');
    expect(text(cell('1.3.0').querySelector('.grade-text')!)).toBe('Grade N/A · Scanning off');
    await expectAccessible(graded.root);
    const plain = await open();
    expect(headers(plain)).not.toContain('Vulnerabilities');
    // Every version of a format that is not scanned: no column (Q-UI-377); every version queued: the column, with why (Q-UI-1121).
    const na = (state: VulnerabilityGrade['state']) => sampleGrade('N/A', {}, { state });
    const unscanned = await open({}, (_, v) =>
      v.packageReports.set('pkg_01JBQ6V7S3K9', {
        packageId: 'pkg_01JBQ6V7S3K9',
        versions: [{ version: '1.4.0', grade: na('format_not_scanned') }, { version: '1.3.0', grade: na('format_not_scanned') }],
      }),
    );
    expect(headers(unscanned)).not.toContain('Vulnerabilities');
    const queued = await open({}, (_, v) =>
      v.packageReports.set('pkg_01JBQ6V7S3K9', {
        packageId: 'pkg_01JBQ6V7S3K9',
        versions: [{ version: '1.4.0', grade: na('queued') }, { version: '1.3.0', grade: na('no_scanner') }],
      }),
    );
    expect(headers(queued)).toContain('Vulnerabilities');
  });

  it('R-UI-110 the filter narrows the rows as one types, by version and by tag, and the address follows without a new entry', async () => {
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

  it('R-UI-110 the columns sort in the browser and the sort is the address\'s; the default is left out of it', async () => {
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

  it('R-UI-110 shows 50 versions at first and 50 more with each Show more; a new search starts at 50 again', async () => {
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

  it('R-UI-110 the column Repositories appears only where the versions are held by different repositories', async () => {
    const same = await open();
    expect(headers(same)).not.toContain('Repositories');
    const differ = await open({}, (fake) => {
      const v = fake.versionList.find((x) => x.version === '1.3.0')!;
      v.repositories = [{ key: 'web/npm-remote', class: 'remote', publishedAt: v.publishedAt, available: true }];
    });
    expect(headers(differ)).toContain('Repositories');
    const cell = rows(differ).find((tr) => text(tr.querySelector('th a')!) === '1.3.0')!.querySelector('td.wrap')!;
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
    expect([...details.querySelectorAll('dd .plain-list a')].map((a) => a.getAttribute('href'))).toEqual([
      '/projects/web/repositories/npm-local',
      '/projects/web/repositories/npm-all',
    ]);
    // The keywords are the chips of a version's page (R-UI-221).
    expect([...details.querySelectorAll('quader-package-tags a')].map((a) => text(a))).toEqual(['ui', 'components']);
    expect(text(details)).toContain('1,832');
  });

  it('R-UI-007 opening the page records the package in "recently viewed"', async () => {
    const r = await open();
    expect(r.get(PackageMemory).recent()[0]).toEqual({ format: 'npm', name: '@team-a/ui' });
  });

  it('R-UI-006 R-UI-007 the header names the format by its name, OCI (Docker), not by its key', async () => {
    const r = await renderScreen(
      PackageDetail,
      inProject({
        inputs: { format: 'oci', name: 'team/app' },
        setup: (get) => {
          (get(RepositoriesApi) as FakeRepositoriesApi).formatsResult = [GENERIC_FORMAT, OCI_FORMAT];
          ociFixture(get(PackagesApi) as FakePackagesApi);
        },
      }),
    );
    expect(text(r.root.querySelector('.type-badge')!)).toBe('OCI (Docker)');
  });

  it('R-UI-269 every image of an image\'s page has its Pull, whose panel reads its leading tag and pulls by the image\'s digest too', async () => {
    const r = await open({ format: 'oci' }, (fake) => {
      ociImagesFixture(fake, [['5.4.7', 'latest'], ['5.4.6']]);
      for (const v of fake.versionList) if (v.format === 'oci') v.metadata = { ...v.metadata, kind: 'image' };
    }, 'team/app');
    document.body.appendChild(r.root);
    const setup = r.get(ClientSetupReads) as FakeClientSetupReads;
    const buttons = [...r.root.querySelectorAll<HTMLButtonElement>('.pkg-versions tbody quader-client-command-menu button')];
    expect(buttons.map((b) => spokenText(b))).toEqual(['5.4.7: Pull', '5.4.6: Pull']);
    buttons[0]!.click();
    await settle(r.fixture);
    const panel = document.getElementById(buttons[0]!.getAttribute('aria-controls')!)!;
    expect([...panel.querySelectorAll('[role="tab"]')].map((t) => text(t))).toEqual(['Docker', 'Podman']);
    expect(text(panel)).toContain('By digest');
    // The tag's pull and the pull by digest (the package without a version, the digest after its reference), through its repository.
    expect(setup.calls.filter((c) => c.query.client === 'docker').map((c) => `${c.repository} ${c.query.version}`)).toEqual(['web/oci-local 5.4.7', 'web/oci-local undefined']);
    buttons[0]!.click();
    await settle(r.fixture);
    r.root.remove();
  });

  it('R-UI-007 an unknown package shows the not-found error', async () => {
    const r = await open({}, undefined, 'no-such-package');
    expect(text(r.root)).toContain('does not exist');
    await expectAccessible(r.root);
  });

  it('R-UI-007 R-UI-110 an image\'s page counts its tags and lists one row per image with every tag, found by any of them, and the images counted', async () => {
    const r = await open({ format: 'oci' }, (fake) => {
      ociImagesFixture(fake, [['5.4.7', '5.4', '5', 'latest', 'stable', 'sha-f00cf6c'], ['5.4.6', 'sha-6b24a87'], ['5.4.5', 'sha-ed7fd56']], [1]);
    }, 'team/app');
    expect(r.fake.versionFilters.at(-1)).toEqual({ sort: '-version', group: 'content' });
    expect(text(r.root.querySelector('.pkg-meta .dot-separated')!)).toMatch(/^10 tags\s*Latest 5\.4\.7$/);
    // The image's digest as the repository's content lists it, and its date is Pushed (R-UI-222, Q-UI-1314).
    expect(headers(r)).toEqual(['Tag', 'Size', 'Pushed', 'Last download', 'Actions']);
    expect(versions(r)).toEqual(['5.4.7', '5.4.6', '5.4.5']);
    const digest = rows(r)[0]!.querySelector('th .digest-line code')!;
    expect(text(digest)).toMatch(/^[0-9a-f]{12}$/);
    expect(digest.getAttribute('title')).toMatch(/^sha256:[0-9a-f]{64}$/);
    // Every tag of the image, none counted away.
    const others = (tr: Element) => [...tr.querySelectorAll('.other [translate="no"]')].map((t) => text(t));
    expect(others(rows(r)[0]!)).toEqual(['5.4', '5', 'latest', 'sha-f00cf6c', 'stable']);
    expect(rows(r)[1]!.querySelector('[data-testid="signature-badge"]')?.getAttribute('data-state')).toBe('verified_internal');
    expect(text(r.root.querySelector('.pkg-versions')!)).not.toMatch(/sha256:/);
    expect(filter(r).getAttribute('aria-label')).toBe('Filter tags');
    filter(r).value = 'sha-6b2';
    filter(r).dispatchEvent(new Event('input'));
    await settle(r.fixture);
    expect(versions(r)).toEqual(['5.4.6']);
    expect(text(r.root.querySelector('.versions-count')!)).toBe('1 of 3 images');
    filter(r).value = 'nothing-like-it';
    filter(r).dispatchEvent(new Event('input'));
    await settle(r.fixture);
    expect(text(r.root.querySelector('.pkg-versions')!)).toContain('No image has a tag that matches “nothing-like-it”.');
    await expectAccessible(r.root);
  });

  it('R-API-010 R-UI-110 the page reads every page of the list by content while a cursor is set, a short and an empty one too', async () => {
    const r = await open({ format: 'oci' }, (fake) => {
      ociImagesFixture(fake, Array.from({ length: 6 }, (_, i) => [`1.${i}.0`, `sha-${String(i).padStart(7, 'c')}`]));
      fake.versionPageSizes = [2, 0, 1];
    }, 'team/app');
    expect(r.fake.versionPages.map((p) => p.cursor)).toEqual([null, '2.1', '2.2', '3.3']);
    expect(versions(r)).toEqual(['1.5.0', '1.4.0', '1.3.0', '1.2.0', '1.1.0', '1.0.0']);
  });

  it('R-UI-110 every format has the page: an image lists its images, the tag leading; NuGet, Debian and a collection their versions', async () => {
    const image = await open({ format: 'oci' }, (fake) => ociFixture(fake), 'team/app');
    expect(versions(image)).toEqual(['1.2.0', '1.1.0']);
    expect(image.fake.versionFilters.at(-1)).toEqual({ sort: '-version', group: 'content' });
    // The other formats' lists are no lists by content.
    expect((await open()).fake.versionFilters.at(-1)).toEqual({ sort: '-version' });
    // Until the formats are read, and for a format the installation does not know, the header shows the key.
    expect(text(image.root.querySelector('.type-badge')!)).toBe('oci');
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

  it('R-UI-194 a database of the ClamAV definitions shows its versions with their state, the version in use first', async () => {
    const r = await open({ format: 'clamav' }, (fake) => clamavFixture(fake), 'daily');
    expect(versions(r)).toEqual(['28190', '28189', '28188']);
    expect(rows(r).map((tr) => text(tr.querySelector('th .version-cell')!).replace('check_circle', '').trim())).toEqual([
      '28190 In use',
      '28189 Previous',
      '28188 Files removed',
    ]);
    expect(r.fake.versionFilters.at(-1)).toMatchObject({ include: ['state'] });
    await expectAccessible(r.root);
  });
});
