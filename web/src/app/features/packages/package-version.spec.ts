import { TestBed } from '@angular/core/testing';
import { Router } from '@angular/router';
import { PackagesApi } from '../../api/packages-api';
import { FakePackagesApi, ansibleFixture, debFixture, ociFixture } from '../../api/testing/fakes-packages';
import { clamavFixture } from '../../api/testing/fakes-clamav';
import { CLAMAV_ENDPOINT, FakeUpstreamApi } from '../../api/testing/fakes-upstream';
import { UpstreamApi } from '../../api/upstream-api';
import { FakeVulnerabilitiesApi, sampleGrade } from '../../api/testing/fakes-vulnerabilities';
import { malwareStatus } from '../../api/testing/fakes-malware';
import { vi } from 'vitest';
import { VulnerabilitiesApi } from '../../api/vulnerabilities-api';
import { spokenText } from '../../testing/axe';
import { expectAccessible, inProject, renderScreen, settle, text } from '../../testing/screen';
import { PackageMemory } from './package-memory';
import { PackageVersionPage } from './package-version';
import { RepositoriesApi } from '../../api/repositories-api';
import { FakeRepositoriesApi, GENERIC_FORMAT, OCI_FORMAT } from '../../api/testing/fakes-catalog';

type Opened = Awaited<ReturnType<typeof open>>;

async function open(
  inputs: Record<string, unknown> = {},
  setup?: (fake: FakePackagesApi, vulns: FakeVulnerabilitiesApi) => void,
  name = '@team-a/ui',
) {
  const r = await renderScreen(
    PackageVersionPage,
    inProject({
      inputs: { format: 'npm', name, version: '1.4.0', ...inputs },
      setup: (get) => setup?.(get(PackagesApi) as FakePackagesApi, get(VulnerabilitiesApi) as FakeVulnerabilitiesApi),
    }),
  );
  return { ...r, fake: r.get(PackagesApi) as FakePackagesApi };
}

async function set(r: Opened, inputs: Record<string, unknown>) {
  for (const [k, v] of Object.entries(inputs)) r.fixture.componentRef.setInput(k, v);
  await settle(r.fixture);
}

const tabLabels = (r: Opened) => [...r.root.querySelectorAll('[role="tab"]')].map((t) => text(t));
const command = (r: Opened) =>
  r.root.querySelector<HTMLInputElement>('quader-install-panel quader-address-field input')?.value ??
  r.root.querySelector('quader-install-panel pre')?.textContent;
const pairs = (r: Opened) =>
  [...r.root.querySelectorAll('.version-tabs dl.facts')].flatMap((dl) =>
    [...dl.querySelectorAll('dt')].map((dt) => `${text(dt)} ${text(dt.nextElementSibling!)}`),
  );

describe('The page of a version (R-UI-111)', () => {
  it('R-UI-111 names the package and the version, its tags, and its facts on one line: published, size, last fetched, downloads, licence', async () => {
    const r = await open();
    expect(text(r.root.querySelector('h1')!)).toBe('@team-a/ui 1.4.0');
    expect([...r.root.querySelectorAll('.pkg-meta .badge')].map((b) => text(b))).toEqual(['latest']);
    const facts = r.root.querySelector('.facts-line')!;
    expect(text(facts)).toContain('47.1 KiB');
    expect(text(facts)).toContain('410 downloads');
    expect(text(facts)).toContain('MIT');
    expect([...facts.querySelectorAll('time')].map((t) => t.getAttribute('datetime'))).toEqual([
      '2026-10-20T09:12:44.512Z',
      '2026-11-17T16:40:02.118Z',
    ]);
    await expectAccessible(r.root);
  });

  it('R-API-065 R-API-079 a version no client downloaded says so', async () => {
    const r = await open({ version: '1.3.0' });
    expect(text(r.root.querySelector('.facts-line')!)).toContain('Last download never');
  });

  it('R-UI-111 a pre-release carries its mark; a version nobody may read is the not-found error', async () => {
    const r = await open({ version: '1.5.0-beta.1' });
    expect([...r.root.querySelectorAll('.pkg-meta .badge')].map((b) => text(b))).toEqual(['next', 'Pre-release']);
    const gone = await open({ version: '9.9.9' });
    expect(text(gone.root)).toContain('does not exist');
  });

  it('R-UI-009 R-UI-111 the install command of this version stands before the tabs, at the page\'s width, and fits its command', async () => {
    const r = await open();
    expect(command(r)).toBe('npm install @team-a/ui@1.4.0');
    const panel = r.root.querySelector('quader-install-panel')!;
    const tabs = r.root.querySelector('quader-tab-group')!;
    expect(panel.compareDocumentPosition(tabs) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
    // Q-UI-455: the field is as wide as its command, which the style reads from this custom property.
    expect(r.root.querySelector<HTMLElement>('quader-address-field.command-field')?.style.getPropertyValue('--command-chars')).toBe(
      String('npm install @team-a/ui@1.4.0'.length),
    );
    await set(r, { version: '1.3.0' });
    expect(command(r)).toBe('npm install @team-a/ui@1.3.0');
  });

  it('R-UI-111 R-UI-008 without a grade the tabs are README, Files and Details, the README first', async () => {
    const r = await open();
    expect(tabLabels(r)).toEqual(['README', 'Files', 'Details']);
    expect(r.root.querySelector('quader-package-readme .readme h1')?.textContent).toBe('@team-a/ui');
    expect(r.fake.readmeCalls).toEqual(['pkg_01JBQ6V7S3K9@1.4.0']);
  });

  it('R-UI-111 a graded version opens on Vulnerabilities; one that is not scanned keeps the tab after Details', async () => {
    const report = (grade: ReturnType<typeof sampleGrade>) => ({
      packageId: 'pkg_01JBQ6V7S3K9',
      version: '1.4.0',
      repositories: [{ repository: 'web/npm-local', evaluation: grade.evaluation, blocked: false, blockReason: null, findings: [], grade }],
    });
    const graded = await open({}, (_, v) => v.versionReports.set('pkg_01JBQ6V7S3K9/1.4.0', report(sampleGrade('B', { medium: 2 }, { fixable: 1 }))));
    expect(tabLabels(graded)).toEqual(['Vulnerabilities', 'README', 'Files', 'Details']);
    expect(text(graded.root.querySelector('.facts-grade .grade-text')!)).toBe('Grade B · 2 findings · 1 fixable');
    const unscanned = await open({}, (_, v) => v.versionReports.set('pkg_01JBQ6V7S3K9/1.4.0', report(sampleGrade('N/A'))));
    expect(tabLabels(unscanned)).toEqual(['README', 'Files', 'Details', 'Vulnerabilities']);
  });

  it('R-UI-057 a version without a README, as for a caller who may not read the wiki, has no README tab and asks for none', async () => {
    const r = await open({ version: '1.5.0-beta.1' });
    expect(tabLabels(r)).toEqual(['Files', 'Details']);
    expect(r.fake.readmeCalls).toEqual([]);
  });

  it('R-UI-111 the Details tab lists the dependency kinds, the authors, the links and the repositories that hold the version', async () => {
    const r = await open({ tab: 'details' });
    expect(pairs(r)).toEqual(expect.arrayContaining(['lit ^3.1.0', 'vitest ^3.0.0', 'node >=22']));
    const t = text(r.root.querySelector('.version-tabs')!);
    expect(t).toContain('Development dependencies');
    expect(t).toContain('Team A <team-a@example.com>');
    const home = [...r.root.querySelectorAll('a')].find((a) => a.textContent === 'https://example.com/team-a/ui')!;
    expect(home.getAttribute('rel')).toBe('nofollow noopener noreferrer');
    const repos = [...r.root.querySelectorAll('.version-tabs quader-data-table tbody tr')];
    expect(repos.map((tr) => text(tr))).toEqual([expect.stringContaining('web/npm-localLocal'), expect.stringContaining('web/npm-allVirtual')]);
    expect(repos[0]?.querySelector('a')?.getAttribute('href')).toBe('/projects/web/repositories/npm-local');
    await expectAccessible(r.root);
  });

  it('R-UI-007 a homepage of javascript:alert(1) is shown as text and no element carries it as a link', async () => {
    const r = await open({ tab: 'details' }, (fake) => {
      for (const v of fake.versionList) {
        v.metadata = { ...v.metadata, homepage: 'javascript:alert(1)', bugsUrl: 'data:text/html,x', repositoryUrl: 'git+ssh://example.com/x.git' };
      }
    });
    expect(text(r.root)).toContain('javascript:alert(1)');
    expect(text(r.root)).toContain('git+ssh://example.com/x.git');
    for (const a of r.root.querySelectorAll('a')) {
      expect(a.getAttribute('href') ?? '').not.toMatch(/javascript:|data:|git\+ssh:/i);
    }
  });

  it('R-UI-007 R-API-029 R-UI-111 the Files tab shows the SHA-256 with a copy button, and the path is the download of the contentUrl', async () => {
    const r = await open({ tab: 'files' });
    // Q-UI-1002: the first twelve characters under the path, the whole hash as their title and on the copy button.
    const hash = r.root.querySelector('.file-hash code')!;
    expect(text(hash)).toBe('0'.repeat(12));
    expect(hash.getAttribute('title')).toBe('0'.repeat(56) + '5be0c1d2');
    expect(r.root.querySelector('.version-tabs quader-copy-button button')?.getAttribute('aria-label')).toBe('Copy SHA-256');
    const download = r.root.querySelector('.version-files th[scope="row"] a.file-path')!;
    expect(text(download)).toBe('@team-a/ui/-/ui-1.4.0.tgz');
    expect(download.getAttribute('href')).toBe('/api/v1/projects/web/repositories/npm-local/content/@team-a/ui/-/ui-1.4.0.tgz');
    expect(download.hasAttribute('download')).toBe(true);
    expect(download.getAttribute('title')).toBe('Download');
    // No column of its own for the download: the path is it (Q-UI-1051).
    expect([...r.root.querySelectorAll('.version-files thead th')].map((th) => text(th))).toEqual(['File']);
    await expectAccessible(r.root);
  });

  it('R-UI-193 a version a system repository alone holds offers no Download and no command, and says why', async () => {
    const r = await open({ tab: 'files' }, (fake) => {
      fake.versionList = fake.versionList.map((v) =>
        v.version !== '1.4.0'
          ? v
          : {
              ...v,
              repositories: [{ key: 'upstream/scanner-trivy', class: 'remote', publishedAt: v.publishedAt, available: true }],
              files: v.files.map((f) => ({ ...f, contentUrl: f.contentUrl.replace('/projects/web/repositories/npm-local/', '/projects/upstream/repositories/scanner-trivy/') })),
            },
      );
    });
    expect(r.root.querySelector('.version-files a[download]')).toBeNull();
    expect([...r.root.querySelectorAll('.version-tabs thead th')].map((th) => text(th))).not.toContain('Download');
    expect(text(r.root.querySelector('[data-testid="install-system"]')!)).toBe("Only Quader's image scanning reads this version; clients cannot install it.");
    expect(command(r)).toBeUndefined();
    await expectAccessible(r.root);
  });

  it('R-UI-111 a version its disabled repository alone serves offers no Download and no command, and says why in one sentence', async () => {
    // web/npm-local is disabled, and web/npm-all reaches the version only through it: neither serves it (R-REPO-019).
    const r = await open({ tab: 'files' }, (fake) => {
      fake.versionList = fake.versionList.map((v) =>
        v.version !== '1.4.0' ? v : { ...v, repositories: v.repositories.map((repo) => ({ ...repo, available: false })) },
      );
    });
    expect(r.root.querySelector('.version-files a[download]')).toBeNull();
    expect([...r.root.querySelectorAll('.version-tabs thead th')].map((th) => text(th))).not.toContain('Download');
    expect(text(r.root.querySelector('[data-testid="install-disabled"]')!)).toBe(
      'The repository web/npm-local is disabled: nobody can install this version until it is enabled again.',
    );
    expect(command(r)).toBeUndefined();
    expect(r.root.querySelector('[data-testid="install-system"]')).toBeNull();
    await expectAccessible(r.root);
  });

  it('R-UI-111 a disabled repository beside one that serves the version is left out of the picker, and its file offers no Download', async () => {
    const r = await open({ tab: 'files' }, (fake) => {
      fake.versionList = fake.versionList.map((v) =>
        v.version !== '1.4.0'
          ? v
          : {
              ...v,
              repositories: [
                { key: 'web/npm-local', class: 'local', publishedAt: v.publishedAt, available: false },
                { key: 'web/npm-remote', class: 'remote', publishedAt: v.publishedAt, available: true },
              ],
            },
      );
    });
    // One repository serves the version: its command, no picker of repositories, no sentence.
    expect(command(r)).toBe('npm install @team-a/ui@1.4.0');
    expect(r.root.querySelectorAll('quader-install-panel select')).toHaveLength(1);
    expect(r.root.querySelector('[data-testid="install-disabled"]')).toBeNull();
    // The file is web/npm-local's copy: its path is no download.
    expect(r.root.querySelector('.version-files a[download]')).toBeNull();
  });

  it('R-UI-111 the open tab is a query parameter: selecting a tab navigates with ?tab=', async () => {
    const r = await open();
    const navigate = vi.spyOn(TestBed.inject(Router), 'navigate').mockResolvedValue(true);
    [...r.root.querySelectorAll<HTMLElement>('[role="tab"]')].find((t) => text(t) === 'Files')!.click();
    await settle(r.fixture);
    expect(navigate).toHaveBeenCalledWith([], { queryParams: { tab: 'files' }, queryParamsHandling: 'merge' });
  });

  it('R-UI-007 opening a version records its package in "recently viewed"', async () => {
    const r = await open();
    expect(r.get(PackageMemory).recent()[0]).toEqual({ format: 'npm', name: '@team-a/ui' });
  });

  it('R-UI-007 a deprecated version is flagged with its reason', async () => {
    const r = await open({}, (fake) => {
      for (const v of fake.versionList) v.metadata = { ...v.metadata, deprecated: 'Use @team-a/ui2' };
    });
    expect(text(r.root.querySelector('.version-facts')!)).toContain('Deprecated. Use @team-a/ui2');
  });

  describe('NuGet', () => {
    const nuget = (inputs: Record<string, unknown> = {}, setup?: (fake: FakePackagesApi) => void) =>
      open({ format: 'nuget', version: '2.3.1', ...inputs }, setup, 'Contoso.Logging');

    it('R-NUGET-006 an unlisted version is flagged as hidden from search', async () => {
      const r = await nuget({ version: '2.2.0' });
      expect(text(r.root)).toContain('Unlisted.');
      expect(text(r.root)).toContain('hidden from search');
      await expectAccessible(r.root);
    });

    it('R-NUGET-007 Details lists the dependencies by target framework, verbatim, the plain members, and says so for a framework without any', async () => {
      const r = await nuget({ tab: 'details' });
      expect([...r.root.querySelectorAll('.version-tabs h3')].map((h) => text(h))).toEqual(['net8.0', '.NETStandard2.0', 'net462']);
      expect(pairs(r)).toEqual(
        expect.arrayContaining([
          'Microsoft.Extensions.Logging.Abstractions [8.0.0, )',
          'System.Text.Json [6.0.0, )',
          'Minimum client version 4.3.0',
          'Package types Dependency',
        ]),
      );
      expect(pairs(r).join('|')).not.toContain('Copyright');
      expect(text(r.root)).toContain('No dependencies.');
      expect(text(r.root)).toContain('Ada Lovelace');
      await expectAccessible(r.root);
    });

    it('R-UI-009 the install command is the .NET CLI\'s for this version', async () => {
      const r = await nuget();
      expect(command(r)?.trim()).toBe('dotnet add package Contoso.Logging --version 2.3.1');
    });
  });

  describe('OCI', () => {
    const image = async (inputs: Record<string, unknown> = {}, setup?: (fake: FakePackagesApi) => void) => {
      const r = await renderScreen(
        PackageVersionPage,
        inProject({
          inputs: { format: 'oci', name: 'team/app', version: '1.2.0', ...inputs },
          setup: (get) => {
            (get(RepositoriesApi) as FakeRepositoriesApi).formatsResult = [GENERIC_FORMAT, OCI_FORMAT];
            ociFixture(get(PackagesApi) as FakePackagesApi);
            setup?.(get(PackagesApi) as FakePackagesApi);
          },
        }),
      );
      return { ...r, fake: r.get(PackagesApi) as FakePackagesApi };
    };
    const cells = (r: Opened) => [...r.root.querySelectorAll('.version-tabs table tbody tr')].map((tr) => [...tr.children].map((c) => text(c)));
    const copyButtons = (r: Opened) => [...r.root.querySelectorAll<HTMLButtonElement>('.version-tabs quader-copy-button button')];
    const nameOf = (b: HTMLElement) => b.getAttribute('aria-label') ?? text(b);

    it('R-UI-111 R-OCI-031 R-UI-006 the tag leads the title, the digest is a detail; the header shows the OCI logo beside the format by its name', async () => {
      const r = await image();
      expect(text(r.root.querySelector('h1')!)).toBe('team/app 1.2.0');
      expect(text(r.root.querySelector('.version-facts')!)).not.toContain('sha256:');
      const badge = r.root.querySelector('.type-badge')!;
      expect([...badge.querySelectorAll('quader-format-icon img')].map((i) => i.getAttribute('src'))).toEqual([
        'formats/oci/oci-icon-color.svg',
        'formats/oci/oci-icon-white.svg',
      ]);
      // The format's name, not its key (the renders of 2026-11-21 showed `oci` on every version page).
      expect(spokenText(badge)).toBe('OCI (Docker)');
      await expectAccessible(r.root);
    });

    it('R-OCI-031 the Files tab lists the tag item, whose checksum is the manifest digest', async () => {
      const r = await image({ version: '1.1.0', tab: 'files' });
      const rows = cells(r);
      expect(rows).toHaveLength(1);
      expect([...r.root.querySelectorAll('.version-tabs table tbody th .file-path')].map((e) => text(e))).toEqual(['team/app/_tags/1.1.0']);
      expect(r.root.querySelector('.file-hash code')?.getAttribute('title')).toContain('a4d64'.padEnd(64, 'f'));
    });

    it('R-OCI-031 Details shows the digest, media type, artifact type and size of the tag', async () => {
      const r = await image({ version: '1.1.0', tab: 'details' });
      const p = pairs(r);
      expect(p.some((x) => x.startsWith('Digest sha256:') && x.includes('a4d64'))).toBe(true);
      expect(p).toEqual(
        expect.arrayContaining([
          'Media type application/vnd.oci.image.manifest.v1+json',
          'Artifact type application/vnd.oci.image.config.v1+json',
          'Size 30.0 MiB',
        ]),
      );
      expect([...r.root.querySelectorAll('.version-tabs h2')].map((h) => text(h))).toEqual(['Image', 'Repositories']);
      await expectAccessible(r.root);
    });

    it('R-OCI-031 an index lists its platforms, tells repeated or empty platforms apart by a short digest, and each copy button copies its row', async () => {
      const written: string[] = [];
      Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { writeText: async (t: string) => void written.push(t) } });
      const r = await image({ tab: 'details' });
      expect(pairs(r).join('|')).not.toContain('Artifact type');
      expect([...r.root.querySelectorAll('.version-tabs h2')].map((h) => text(h))).toEqual(['Image', 'Platforms', 'Repositories']);
      const platforms = [...r.root.querySelectorAll('.version-tabs table')].find((t) => text(t.querySelector('caption')!) === 'Platforms of this image index')!;
      const headers = [...platforms.querySelectorAll('tbody th')].map((th) => text(th));
      expect(headers).toHaveLength(5);
      expect(headers[0]).toBe('linux/amd64');
      expect(headers[2]).toMatch(/^unknown\/unknown \(sha256:[0-9a-f]{12}\)$/);
      expect(headers[4]).toMatch(/^No platform \(sha256:[0-9a-f]{12}\)$/);
      const names = copyButtons(r).map(nameOf);
      expect(names[0]).toBe('Copy digest');
      expect(names[1]).toBe('Copy digest of linux/amd64');
      copyButtons(r).find((b) => nameOf(b) === 'Copy digest of linux/arm64')!.click();
      await settle(r.fixture);
      expect(written).toEqual(['sha256:' + 'a4c64'.padEnd(64, 'f')]);
    });

    it('R-OCI-031 members of the wrong type are skipped: a child without a digest is not listed, a size that is no number is a dash', async () => {
      const odd = await image({ tab: 'details' }, (fake) => {
        for (const v of fake.versionList) {
          if (v.format === 'oci') {
            v.metadata = {
              digest: 'sha256:' + 'a'.repeat(64),
              size: 'big',
              platforms: ['x', { platform: 'linux/riscv64', size: 1 }, { platform: 'linux/amd64', digest: 'sha256:' + 'b'.repeat(64), size: 'n' }],
            };
          }
        }
      });
      // A size that is no number is one that is not known (R-API-080): the shared no-value, never a number.
      expect(pairs(odd).find((p) => p.startsWith('Size'))).toBe('Size –None');
      const platforms = [...odd.root.querySelectorAll('.version-tabs table')].find((t) => text(t.querySelector('caption')!) === 'Platforms of this image index')!;
      expect([...platforms.querySelectorAll('tbody tr')].map((tr) => text(tr.children[0]!))).toEqual(['linux/amd64']);
      expect(text(platforms.querySelector('tbody tr')!.children[3]!)).toBe('–None');
    });

    it('R-API-080 an index of a remote counts the platforms fetched so far, and the others say they are not fetched', async () => {
      const r = await image({ tab: 'details' }, (fake) => {
        for (const v of fake.versionList) {
          if (v.format === 'oci') {
            v.sizeBytes = 31_457_280;
            v.sizePartial = true;
            v.metadata = {
              digest: 'sha256:' + 'a'.repeat(64),
              mediaType: 'application/vnd.oci.image.index.v1+json',
              size: 31_457_280,
              sizePartial: true,
              platforms: [
                { platform: 'linux/amd64', digest: 'sha256:' + 'b'.repeat(64), stored: true, mediaType: 'application/vnd.oci.image.manifest.v1+json', size: 31_457_280 },
                { platform: 'linux/arm64', digest: 'sha256:' + 'c'.repeat(64), stored: false },
              ],
            };
          }
        }
      });
      expect(text(r.root.querySelector('.facts-line')!)).toContain('≥ at least 30.0 MiB');
      expect(text(r.root.querySelector('.facts-line')!)).not.toContain('Size not known');
      expect(pairs(r)).toContain('Size ≥ at least 30.0 MiB');
      const platforms = [...r.root.querySelectorAll('.version-tabs table')].find((t) => text(t.querySelector('caption')!) === 'Platforms of this image index')!;
      const cells = [...platforms.querySelectorAll('tbody tr')].map((tr) => [...tr.children].map((c) => text(c)));
      expect(cells[0]?.[3]).toBe('30.0 MiB');
      expect(cells[1]?.[2]).toBe('–None');
      expect(cells[1]?.[3]).toBe('Not fetched');
      await expectAccessible(r.root);
    });
  });

  describe('Debian', () => {
    const deb = (inputs: Record<string, unknown> = {}, setup?: (fake: FakePackagesApi) => void) =>
      open({ format: 'deb', version: '2.10-3', ...inputs }, (fake) => {
        debFixture(fake);
        setup?.(fake);
      }, 'hello');
    const facts = (r: Opened) =>
      Object.fromEntries([...r.root.querySelectorAll('[data-testid="deb-facts"] dt')].map((dt) => [text(dt), text(dt.nextElementSibling!)]));

    it('R-DEB-030 Details lists the architectures, the suites and components and the control fields of the version', async () => {
      const r = await deb({ tab: 'details' });
      expect(facts(r)).toEqual({
        Architectures: 'amd64, arm64',
        'Suites and components': 'stable/main',
        Maintainer: 'Jane Maintainer <jane@example.com>',
        Section: 'devel',
        Priority: 'optional',
        'Installed size': '280 KiB',
        Depends: 'libc6 (>= 2.34)',
        'Source package': 'hello (2.10-3)',
      });
      expect([...r.root.querySelectorAll<HTMLAnchorElement>('.version-tabs a[target="_blank"]')].map((a) => a.href)).toContain(
        'https://www.gnu.org/software/hello/',
      );
      await expectAccessible(r.root);
    });

    it('R-DEB-030 fields a version lacks are left out', async () => {
      const bare = await deb({ tab: 'details', version: '2.9-1' });
      expect(Object.keys(facts(bare))).toEqual(['Architectures', 'Suites and components']);
    });
  });

  describe('Ansible Galaxy', () => {
    const collection = (inputs: Record<string, unknown> = {}, setup?: (fake: FakePackagesApi) => void) =>
      open({ format: 'ansible', version: '1.2.0', ...inputs }, (fake) => {
        ansibleFixture(fake);
        setup?.(fake);
      }, 'acme.tools');
    const facts = (r: Opened) =>
      Object.fromEntries([...r.root.querySelectorAll('[data-testid="ansible-facts"] dt')].map((dt) => [text(dt), text(dt.nextElementSibling!)]));

    it('R-ANS-025 the install panel shows the collection install command of the version, and no error notice', async () => {
      const r = await collection();
      expect(command(r)?.trim()).toBe('ansible-galaxy collection install acme.tools:1.2.0');
      expect(r.root.querySelector('quader-error-notice')).toBeNull();
    });

    it('R-ANS-026 Details lists the Ansible versions it needs and the tags; the licences are the facts of the header', async () => {
      const r = await collection({ tab: 'details' });
      expect(facts(r)).toEqual({ 'Requires Ansible': '>=2.15.0', Tags: 'tools, linux' });
      expect(text(r.root.querySelector('.facts-line')!)).toContain('Apache-2.0, MIT');
      expect(text(r.root)).toContain('community.general');
      expect(r.root.querySelector<HTMLAnchorElement>('a[href="https://example.com/acme/tools/issues"]')).not.toBeNull();
      await expectAccessible(r.root);
    });
  });

  describe('a version of the ClamAV definitions (R-UI-194)', () => {
    async function definition(tab: string, on = false) {
      const r = await renderScreen(
        PackageVersionPage,
        inProject(
          {
            inputs: { format: 'clamav', name: 'daily', version: '28190', tab },
            setup: (get) => {
              clamavFixture(get(PackagesApi) as FakePackagesApi);
              if (on) (get(UpstreamApi) as FakeUpstreamApi).set('clamav', { clients: { enabled: true, address: CLAMAV_ENDPOINT, certificateAddresses: [] }, visibility: 'public' });
            },
          },
          '/projects/upstream/packages',
        ),
      );
      return r;
    }

    it('R-UI-194 R-CLAMAV-003 Files lists the members by name with the sentence, the detached signature, and no Download or Delete while the switch is off', async () => {
      const { root } = await definition('files');
      expect(text(root.querySelector('.pkg-meta')!)).toContain('In use');
      const members = [...root.querySelectorAll('.clamav-members tbody th')].map((th) => text(th));
      expect(members).toEqual(['daily.info', 'daily.ndb']);
      expect(text(root)).toContain('The signature count of the header is the total; lines of a member are not always signatures.');
      expect(text(root.querySelector('.clamav-files')!)).toContain('Present, 2 signatures. Quader passes it on; ClamAV checks it.');
      expect(root.querySelector('a[download]')).toBeNull();
      expect([...root.querySelectorAll('button')].some((b) => text(b) === 'Delete')).toBe(false);
      expect(text(root.querySelector('[data-testid="install-system"]')!)).toContain('ClamAV definition updates is off');
      await expectAccessible(root);
    });

    it('R-UI-194 while ClamAV definition updates is on, a stored container downloads and the install panel leads to the client setup', async () => {
      const { root } = await definition('files', true);
      expect(root.querySelector('a[download]')!.getAttribute('href')).toBe('/api/v1/projects/upstream/repositories/clamav/content/daily/28190/daily.cvd');
      expect(text(root.querySelector('[data-testid="install-clamav"]')!)).toContain('A ClamAV installation always takes the version in use');
      expect(root.querySelector('.setup-link a')!.getAttribute('href')).toBe('/projects/upstream/repositories/clamav/setup?client=freshclam');
    });

    it('R-UI-194 R-CLAMAV-003 Details shows every field of the header as text and the two MD5 in a closed fold', async () => {
      const { root } = await definition('details');
      const dl = root.querySelector('.clamav-facts')!;
      const facts = Object.fromEntries([...dl.querySelectorAll('dt')].map((dt) => [text(dt), text(dt.nextElementSibling!)]));
      expect(facts['Signatures']).toBe('2,076,903');
      expect(facts['Functionality level']).toBe('210 · ClamAV 1.5 or later');
      expect(facts['Builder']).toBe('raynman');
      expect(facts['Verification']).toBe("ClamAV's signature verified");
      expect(facts['Build time as written']).toBe('21 Nov 2026 07-00 +0000');
      expect(root.querySelector('.clamav-fold button')!.getAttribute('aria-expanded')).toBe('false');
      await expectAccessible(root);
    });
  });

  it('R-UI-213 a file whose malware scan has not answered is read every 3 s until it has, then every minute, and the open tab and the chosen repository stay', async () => {
    vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] });
    try {
      const pending = (fake: FakePackagesApi, state: 'pending' | 'clean') => {
        fake.versionList = fake.versionList.map((v) =>
          v.packageId === 'pkg_01JBQ6V7S3K9' && v.version === '1.4.0'
            ? { ...v, files: v.files.map((f) => ({ ...f, malware: malwareStatus({ state, scannedAt: state === 'clean' ? '2026-11-24T10:00:00Z' : null }) })) }
            : v,
        );
      };
      const r = await open({ tab: 'files' }, (fake) => pending(fake, 'pending'));
      const reads = vi.spyOn(r.fake, 'version');
      const malwareCell = () => text(r.root.querySelector('quader-malware-status')!);
      expect(malwareCell()).toContain('Checking');
      const tab = r.root.querySelector('[role="tab"][aria-selected="true"]');
      vi.advanceTimersByTime(3_000);
      await settle(r.fixture);
      expect(reads).toHaveBeenCalledTimes(1);
      pending(r.fake, 'clean');
      vi.advanceTimersByTime(3_000);
      await settle(r.fixture);
      expect(reads).toHaveBeenCalledTimes(2);
      expect(malwareCell()).toContain('No malware found');
      // In place: the same tab element, still the open one.
      expect(r.root.querySelector('[role="tab"][aria-selected="true"]')).toBe(tab);
      vi.advanceTimersByTime(3_000);
      await settle(r.fixture);
      expect(reads).toHaveBeenCalledTimes(2);
      vi.advanceTimersByTime(57_000);
      await settle(r.fixture);
      expect(reads).toHaveBeenCalledTimes(3);
    } finally {
      vi.useRealTimers();
    }
  });

  it('R-UI-213 a scan that finishes while the page is open adds its tab in its place, and the tab the page opened on stays open', async () => {
    vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] });
    try {
      const report = (grade: ReturnType<typeof sampleGrade>) => ({
        packageId: 'pkg_01JBQ6V7S3K9',
        version: '1.4.0',
        repositories: [{ repository: 'web/npm-local', evaluation: grade.evaluation, blocked: false, blockReason: null, findings: [], grade }],
      });
      let vulns: FakeVulnerabilitiesApi | undefined;
      const r = await open({}, (_, v) => {
        vulns = v;
        v.versionReports.set('pkg_01JBQ6V7S3K9/1.4.0', report(sampleGrade('N/A')));
      });
      const selected = () => text(r.root.querySelector('[role="tab"][aria-selected="true"]')!);
      expect(selected()).toBe('README');
      vulns!.versionReports.set('pkg_01JBQ6V7S3K9/1.4.0', report(sampleGrade('B', { medium: 2 }, { fixable: 1 })));
      vi.advanceTimersByTime(60_000);
      await settle(r.fixture);
      expect(tabLabels(r)).toEqual(['Vulnerabilities', 'README', 'Files', 'Details']);
      expect(selected()).toBe('README');
    } finally {
      vi.useRealTimers();
    }
  });
});
