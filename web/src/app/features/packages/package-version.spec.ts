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
import { ApiError } from '../../api/problem';
import { spokenText } from '../../testing/axe';
import { expectAccessible, inProject, renderScreen, settle, text } from '../../testing/screen';
import { PackageMemory } from './package-memory';
import { PackageVersionPage } from './package-version';
import { RepositoriesApi } from '../../api/repositories-api';
import { SessionApi } from '../../api/session-api';
import { FakeSessionApi } from '../../api/testing/fakes';
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
/** The button of the head that opens the clients' commands (Q-UI-1241); null where the head offers none. */
const commandButton = (r: Opened) => r.root.querySelector<HTMLButtonElement>('quader-page-header quader-client-command-menu button');
/** The panel the button opened, hung in the overlay. */
const commandPanel = (r: Opened) => {
  const id = commandButton(r)?.getAttribute('aria-controls');
  return id ? document.getElementById(id) : null;
};
/** Opens the head's panel and reads the command of the chosen client; undefined where the head has no button. */
async function command(r: Opened): Promise<string | undefined> {
  const button = commandButton(r);
  if (!button) return undefined;
  if (button.getAttribute('aria-expanded') !== 'true') {
    button.click();
    await settle(r.fixture);
  }
  const shown = commandPanel(r)!.querySelector('[role="tabpanel"]:not([inert]) quader-client-command, .command-body quader-client-command')!;
  return shown.querySelector<HTMLInputElement>('quader-address-field input')?.value ?? shown.querySelector('pre')?.textContent ?? undefined;
}
async function closeCommand(r: Opened): Promise<void> {
  const button = commandButton(r);
  if (button?.getAttribute('aria-expanded') === 'true') {
    button.click();
    await settle(r.fixture);
  }
}
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

  it('R-UI-009 R-UI-111 the command of this version is behind the head\'s Install, a tab per client with Set up client at the row\'s end, and no box stands before the tabs', async () => {
    const r = await open();
    // Q-UI-1241: the box is gone; the verb's button stands in the page's head.
    expect(r.root.querySelector('quader-install-panel, #install-heading')).toBeNull();
    expect(spokenText(commandButton(r)!)).toBe('1.4.0: Install');
    expect(await command(r)).toBe('npm install @team-a/ui@1.4.0');
    const panel = commandPanel(r)!;
    expect([...panel.querySelectorAll('[role="tab"]')].map((t) => text(t))).toEqual(['npm', 'pnpm', 'Yarn Berry', 'Yarn Classic']);
    // The repository the panel starts at is a virtual one that holds the version; the setup is its, with the chosen client.
    const setup = panel.querySelector<HTMLAnchorElement>('a.setup-link')!;
    expect(setup.getAttribute('href')).toBe('/projects/web/repositories/npm-all/setup?client=npm');
    // Q-UI-455: the field is as wide as its command, which the style reads from this custom property.
    expect(panel.querySelector<HTMLElement>('quader-address-field.command-field')?.style.getPropertyValue('--command-chars')).toBe(
      String('npm install @team-a/ui@1.4.0'.length),
    );
    await closeCommand(r);
    await set(r, { version: '1.3.0' });
    expect(await command(r)).toBe('npm install @team-a/ui@1.3.0');
    await closeCommand(r);
  });

  it('R-UI-009 R-UI-111 a version in more than one repository offers them in the panel, the virtual one first, and the command and the setup follow the one picked', async () => {
    const r = await open();
    await command(r);
    const panel = commandPanel(r)!;
    const select = panel.querySelector<HTMLSelectElement>('.command-repository select')!;
    expect([...select.options].map((o) => o.value)).toEqual(['web/npm-all', 'web/npm-local']);
    select.value = 'web/npm-local';
    select.dispatchEvent(new Event('change', { bubbles: true }));
    await settle(r.fixture);
    expect(panel.querySelector('a.setup-link')!.getAttribute('href')).toBe('/projects/web/repositories/npm-local/setup?client=npm');
    await closeCommand(r);
  });

  it('R-UI-111 R-UI-008 without a grade the tabs are README, Files and Details, the README first', async () => {
    const r = await open();
    expect(tabLabels(r)).toEqual(['README', 'Files', 'Details']);
    expect(r.root.querySelector('quader-package-readme .readme h1')?.textContent).toBe('@team-a/ui');
    expect(r.fake.readmeCalls).toEqual(['pkg_01JBQ6V7S3K9@1.4.0']);
  });

  it('R-UI-111 R-UI-218 a graded npm version opens on its README with Vulnerabilities beside it; one that is not scanned keeps the tab after Details', async () => {
    const report = (grade: ReturnType<typeof sampleGrade>) => ({
      packageId: 'pkg_01JBQ6V7S3K9',
      version: '1.4.0',
      repositories: [{ repository: 'web/npm-local', evaluation: grade.evaluation, blocked: false, blockReason: null, findings: [], grade }],
    });
    const graded = await open({}, (_, v) => v.versionReports.set('pkg_01JBQ6V7S3K9/1.4.0', report(sampleGrade('B', { medium: 2 }, { fixable: 1 }))));
    // npm leads with its documentation (R-UI-218): the README comes first and is the tab the page opens on, the grade after it.
    expect(tabLabels(graded)).toEqual(['README', 'Vulnerabilities', 'Files', 'Details']);
    expect(text(graded.root.querySelector('[role="tab"][aria-selected="true"]')!)).toBe('README');
    expect(text(graded.root.querySelector('.facts-grade .grade-text')!)).toBe('Grade B · 2 findings · 1 fixable');
    const unscanned = await open({}, (_, v) => v.versionReports.set('pkg_01JBQ6V7S3K9/1.4.0', report(sampleGrade('N/A'))));
    expect(tabLabels(unscanned)).toEqual(['README', 'Files', 'Details', 'Vulnerabilities']);
  });

  it('R-UI-216 a scanning report that failed in a way that may pass is shown as such, a refusal leaves the page without it', async () => {
    const failing = (status: number) => (_: FakePackagesApi, v: FakeVulnerabilitiesApi) => {
      v.versionReport = async () => {
        throw new ApiError(status, null, null, null, { call: { method: 'GET', path: '/api/v1/vulnerabilities/versions' } });
      };
    };
    const gateway = await open({}, failing(502));
    expect(gateway.root.querySelector('[data-testid="error-notice"]')).not.toBeNull();
    const refused = await open({}, failing(403));
    expect(refused.root.querySelector('[data-testid="error-notice"]')).toBeNull();
    expect(tabLabels(refused)).toEqual(['README', 'Files', 'Details']);
  });

  it('R-UI-218 an npm version without a README keeps the tab, which says so, asks for none and opens on its first tab with content', async () => {
    const r = await open({ version: '1.5.0-beta.1' });
    expect(tabLabels(r)).toEqual(['README', 'Files', 'Details']);
    expect(text(r.root.querySelector('[role="tab"][aria-selected="true"]')!)).toBe('Files');
    expect(r.fake.readmeCalls).toEqual([]);
    await set(r, { tab: 'readme' });
    expect(text(r.root.querySelector('[data-testid="no-readme"]')!)).toBe('This version has no README to show.');
  });

  it('R-UI-218 on the package\'s own address (no version) the page shows its newest version, and the head switches between the versions', async () => {
    const r = await open({ version: undefined });
    expect(text(r.root.querySelector('h1')!)).toBe('@team-a/ui 1.4.0');
    const trigger = r.root.querySelector<HTMLButtonElement>('quader-version-switch button')!;
    expect(trigger.getAttribute('aria-label')).toBe('Version 1.4.0, switch the version');
    trigger.click();
    await settle(r.fixture);
    const items = [...document.querySelectorAll<HTMLAnchorElement>('.version-menu a.menu-item')];
    // The newest by the format's order, the one shown current and the newest marked Latest; then the link to the whole table.
    const row = (a: Element) => [a.querySelector('.item-version'), a.querySelector('.badge')].map((e) => (e ? text(e) : '')).join(' ').trim();
    expect(items.map((a) => (a.querySelector('.item-version') ? row(a) : text(a)))).toEqual(['1.5.0-beta.1 Pre-release', '1.4.0 Latest', '1.3.0', 'All 3 versions']);
    expect(items[1]!.getAttribute('aria-current')).toBe('page');
    expect(items[2]!.getAttribute('href')).toBe('/projects/web/packages/npm/@team-a/ui@1.3.0');
    expect(items[3]!.getAttribute('href')).toBe('/projects/web/packages/npm/@team-a/ui/-/versions');
    await expectAccessible(r.root);
  });

  it('R-UI-218 the switch keeps the tab of the address for the version switched to', async () => {
    const r = await open({ tab: 'details' });
    r.root.querySelector<HTMLButtonElement>('quader-version-switch button')!.click();
    await settle(r.fixture);
    const older = [...document.querySelectorAll<HTMLAnchorElement>('.version-menu a.menu-item')].find((a) => a.querySelector('.item-version')?.textContent === '1.3.0')!;
    expect(older.getAttribute('href')).toBe('/projects/web/packages/npm/@team-a/ui@1.3.0?tab=details');
  });

  it('R-UI-111 R-UI-221 the Details tab lists the keywords, the dependency kinds, the Package details, the authors and the repositories', async () => {
    const r = await open({ tab: 'details' });
    // The keywords are chips to the packages of the format that share one (Q-UI-1312).
    const chips = [...r.root.querySelectorAll<HTMLAnchorElement>('quader-package-tags a')];
    expect(chips.map((a) => text(a))).toEqual(['ui', 'web-components']);
    expect(chips[0]?.getAttribute('href')).toBe('/projects/web/packages?format=npm&keyword=ui');
    // The dependencies: a group per field of the manifest, an optional peer marked (Q-UI-1311).
    expect([...r.root.querySelectorAll('quader-dependency-list h3')].map((h) => text(h))).toEqual([
      'dependencies',
      'peerDependencies',
      'devDependencies',
    ]);
    expect([...r.root.querySelectorAll('quader-dependency-list li')].map((li) => text(li))).toEqual([
      'lit ^3.1.0',
      'react >=18 optional',
      'vitest ^3.0.0',
    ]);
    expect(pairs(r)).toEqual([
      'Required runtimes node >=22',
      'Unpacked size 200.0 KiB',
      'Files 12',
      'Install script Runs a script of the package when it is installed',
      'Publisher ada',
    ]);
    const t = text(r.root.querySelector('.version-tabs')!);
    expect(t).toContain('Team A <team-a@example.com>');
    // The links are one row under the facts, on every tab (Q-UI-1313); the install script is said there too (Q-UI-1315).
    const home = [...r.root.querySelectorAll<HTMLAnchorElement>('quader-version-links a')].find((a) => a.href === 'https://example.com/team-a/ui')!;
    expect(text(home)).toContain('Homepage');
    expect(home.getAttribute('rel')).toBe('nofollow noopener noreferrer');
    expect(text(r.root.querySelector('[data-testid="install-script"]')!)).toContain('Runs a script of the package when it is installed');
    const repos = [...r.root.querySelectorAll('.version-tabs quader-data-table tbody tr')];
    expect(repos.map((tr) => text(tr))).toEqual([expect.stringContaining('web/npm-localLocal'), expect.stringContaining('web/npm-allVirtual')]);
    expect(repos[0]?.querySelector('a')?.getAttribute('href')).toBe('/projects/web/repositories/npm-local');
    await expectAccessible(r.root);
  });

  it('R-API-086 Used by lists the dependents beside the dependencies, each with its kind and range, and says how many more there are', async () => {
    const r = await open({ tab: 'details' }, (fake) => {
      fake.dependentsFixtures.set('pkg_01JBQ6V7S3K9', {
        items: [
          {
            packageId: 'pkg_app',
            format: 'npm',
            name: '@team-a/app',
            key: '@team-a/app',
            version: '2.1.0',
            relations: [
              { kind: 'dependencies', range: '^1.4.0', via: '@team-a/ui' },
              { kind: 'peerDependencies', range: '>=1', via: '@team-a/ui' },
            ],
          },
          { packageId: 'pkg_kit', format: 'npm', name: 'kit', key: 'kit', version: '0.3.0', relations: [{ kind: 'devDependencies', range: '*', via: '@team-a/ui' }] },
        ],
        total: 52,
        whole: false,
      });
    });
    const headings = [...r.root.querySelectorAll('.version-tabs h2')].map((h) => text(h));
    expect(headings.indexOf('Used by')).toBe(headings.indexOf('Dependencies') + 1);
    const rows = [...r.root.querySelectorAll('quader-used-by li')];
    expect(rows.map((li) => text(li))).toEqual(['@team-a/app 2.1.0 dependencies ^1.4.0 peerDependencies >=1', 'kit 0.3.0 devDependencies *']);
    expect(rows[0]?.querySelector('a')?.getAttribute('href')).toBe('/projects/web/packages/npm/@team-a/app@2.1.0');
    const more = r.root.querySelector('[data-testid="used-by-more"]')!;
    expect(text(more)).toBe('and 50 more. Packages of this project that use it');
    expect(more.querySelector('a')?.getAttribute('href')).toBe('/projects/web/packages?format=npm&dependsOn=@team-a%2Fui');
    expect(r.fake.dependentsCalls).toEqual(['pkg_01JBQ6V7S3K9@']);
    await expectAccessible(r.root);
  });

  it('R-API-086 Used by claims no more than the reader may know when nothing depends on the package', async () => {
    const r = await open({ tab: 'details' }, (fake) => fake.dependentsFixtures.set('pkg_01JBQ6V7S3K9', { items: [], total: 0, whole: false }));
    expect(text(r.root.querySelector('[data-testid="used-by-none"]')!)).toBe('No package you may read depends on it.');
    const all = await open({ tab: 'details' });
    expect(text(all.root.querySelector('[data-testid="used-by-none"]')!)).toBe('No package the installation holds depends on it.');
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
    expect(await command(r)).toBeUndefined();
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
    expect(await command(r)).toBeUndefined();
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
    expect(await command(r)).toBe('npm install @team-a/ui@1.4.0');
    expect(commandPanel(r)!.querySelector('.command-repository')).toBeNull();
    await closeCommand(r);
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

  it('R-API-085 a deprecated version is flagged with its message, as text', async () => {
    const r = await open({}, (fake) => {
      for (const v of fake.versionList) v.deprecation = { scope: 'version', message: 'Use <b>@team-a/ui2</b>' };
    });
    const banner = r.root.querySelector('[data-testid="deprecation-banner"]')!;
    expect(text(banner)).toContain('Deprecated. Use <b>@team-a/ui2</b>');
    expect(banner.querySelector('b')).toBeNull();
    await expectAccessible(r.root);
  });

  describe('Deprecating (R-API-085)', () => {
    const acts = (r: Opened) => r.root.querySelector<HTMLButtonElement>('[data-testid="version-acts"]');
    const menuItems = () => [...document.querySelectorAll<HTMLButtonElement>('.cdk-overlay-container .menu-item')];
    const dialog = () => document.querySelector<HTMLElement>('.quader-dialog');
    async function choose(r: Opened, label: string): Promise<void> {
      acts(r)!.click();
      await settle(r.fixture);
      menuItems().find((b) => text(b) === label)!.click();
      await settle(r.fixture);
    }

    it('R-API-085 a reader who may deploy deprecates the version from the menu with a message; the banner shows it without a reload', async () => {
      const r = await open();
      await choose(r, 'Deprecate version');
      const d = dialog()!;
      expect(text(d.querySelector('h2')!)).toBe('Deprecate @team-a/ui 1.4.0');
      expect(text(d)).toContain('npm install prints the message as a warning');
      const confirm = [...d.querySelectorAll<HTMLButtonElement>('.dialog-actions button')].find((b) => text(b) === 'Deprecate')!;
      expect(confirm.disabled).toBe(true); // npm needs a message
      const area = d.querySelector<HTMLTextAreaElement>('textarea')!;
      area.value = 'Use 1.5.0';
      area.dispatchEvent(new Event('input'));
      await settle(r.fixture);
      confirm.click();
      await settle(r.fixture);
      expect(r.fake.deprecationCalls).toEqual(['set web/npm-local @team-a/ui@1.4.0']);
      expect(dialog()).toBeNull();
      expect(text(r.root.querySelector('[data-testid="deprecation-banner"]')!)).toContain('Deprecated. Use 1.5.0');
    });

    it('R-API-085 a refusal of the server is shown in the dialog, whole, and the dialog stays open', async () => {
      const r = await open({}, (fake) => {
        fake.deprecationFailure = new ApiError(422, { code: 'validation_failed', detail: 'npm carries a deprecation message only.' }, 'req-1');
      });
      await choose(r, 'Deprecate version');
      const area = dialog()!.querySelector<HTMLTextAreaElement>('textarea')!;
      area.value = 'x';
      area.dispatchEvent(new Event('input'));
      await settle(r.fixture);
      [...dialog()!.querySelectorAll<HTMLButtonElement>('.dialog-actions button')].find((b) => text(b) === 'Deprecate')!.click();
      await settle(r.fixture);
      expect(text(dialog()!)).toContain('npm carries a deprecation message only.');
      expect(r.root.querySelector('[data-testid="deprecation-banner"]')).toBeNull();
    });

    it('R-API-085 a deprecated version offers Change and Undo; Undo clears it at once', async () => {
      const r = await open({}, (fake) => {
        for (const v of fake.versionList) if (v.version === '1.4.0') v.deprecation = { scope: 'version', message: 'old' };
      });
      acts(r)!.click();
      await settle(r.fixture);
      expect(menuItems().map((b) => text(b))).toEqual(['Change deprecation', 'Undo deprecation']);
      menuItems()[1]!.click();
      await settle(r.fixture);
      expect(r.fake.deprecationCalls).toEqual(['clear web/npm-local @team-a/ui@1.4.0']);
      expect(r.root.querySelector('[data-testid="deprecation-banner"]')).toBeNull();
    });

    it('R-API-085 a reader who may not deploy is offered no act', async () => {
      const r = await renderScreen(
        PackageVersionPage,
        inProject({
          inputs: { format: 'npm', name: '@team-a/ui', version: '1.4.0' },
          setup: (get) => {
            (get(SessionApi) as FakeSessionApi).allow = (c) => c.action !== 'deploy';
          },
        }),
      );
      expect(r.root.querySelector('[data-testid="version-acts"]')).toBeNull();
    });
  });

  describe('NuGet', () => {
    const nuget = (inputs: Record<string, unknown> = {}, setup?: (fake: FakePackagesApi) => void) =>
      open({ format: 'nuget', version: '2.3.1', ...inputs }, setup, 'Contoso.Logging');

    it('R-NUGET-036 a deprecated version says its reasons and links the package to use instead', async () => {
      const r = await nuget({}, (fake) => {
        for (const v of fake.versionList) {
          if (v.version === '2.3.1') {
            v.deprecation = {
              scope: 'version', reasons: ['Legacy', 'CriticalBugs'], message: 'Moved.',
              alternatePackage: { name: '@team-a/ui', range: '[2.0.0, )', packageId: 'pkg_01JBQ6V7S3K9' },
            };
          }
        }
      });
      const banner = text(r.root.querySelector('[data-testid="deprecation-banner"]')!);
      expect(banner).toContain('Deprecated. Moved. Reason: Legacy, no longer maintained; Critical bugs. Use instead: @team-a/ui [2.0.0, )');
      expect(r.root.querySelector('[data-testid="deprecation-banner"] a')?.getAttribute('href')).toBe('/projects/web/packages/nuget/@team-a/ui');
    });

    it('R-NUGET-036 the dialog of NuGet asks for a reason, a message and the package to use instead', async () => {
      const r = await nuget();
      r.root.querySelector<HTMLButtonElement>('[data-testid="version-acts"]')!.click();
      await settle(r.fixture);
      [...document.querySelectorAll<HTMLButtonElement>('.cdk-overlay-container .menu-item')].find((b) => text(b) === 'Deprecate version')!.click();
      await settle(r.fixture);
      const d = document.querySelector<HTMLElement>('.quader-dialog')!;
      expect([...d.querySelectorAll('.reasons .check-row')].map((l) => text(l))).toEqual(['Legacy, no longer maintained', 'Critical bugs', 'Other']);
      expect(text(d)).toContain('Use instead (optional)');
      [...d.querySelectorAll<HTMLButtonElement>('.dialog-actions button')].find((b) => text(b) === 'Deprecate')!.click();
      await settle(r.fixture);
      expect(r.fake.deprecationCalls).toEqual(['set web/nuget-local Contoso.Logging@2.3.1']);
    });

    it('R-NUGET-006 an unlisted version is flagged as hidden from search', async () => {
      const r = await nuget({ version: '2.2.0' });
      expect(text(r.root)).toContain('Unlisted.');
      expect(text(r.root)).toContain('hidden from search');
      await expectAccessible(r.root);
    });

    it('R-NUGET-007 Details lists the dependencies by target framework, verbatim, the plain members, and says so for a framework without any', async () => {
      const r = await nuget({ tab: 'details' });
      expect([...r.root.querySelectorAll('quader-dependency-list h3')].map((h) => text(h))).toEqual(['net8.0', '.NETStandard2.0', 'net462']);
      expect([...r.root.querySelectorAll('quader-dependency-list li')].map((li) => text(li))).toEqual([
        'Microsoft.Extensions.Logging.Abstractions [8.0.0, )',
        'System.Text.Json [8.0.4, )',
        'System.Text.Json [6.0.0, )',
      ]);
      expect(pairs(r)).toEqual(
        expect.arrayContaining(['Title Contoso Logging', 'Minimum client version 4.3.0', 'Package types Dependency']),
      );
      // The tags are the keywords; the licence's address is the licence's link on the facts line (R-UI-221).
      expect([...r.root.querySelectorAll('quader-package-tags a')].map((a) => text(a))).toEqual(['logging', 'diagnostics']);
      expect(r.root.querySelector('.facts-line a[href="https://licenses.nuget.org/MIT"]')?.textContent).toContain('MIT');
      expect(pairs(r).join('|')).not.toContain('Copyright');
      expect(text(r.root)).toContain('No dependencies.');
      expect(text(r.root)).toContain('Ada Lovelace');
      await expectAccessible(r.root);
    });

    it('R-UI-009 the install command is the .NET CLI\'s for this version', async () => {
      const r = await nuget();
      expect((await command(r))?.trim()).toBe('dotnet add package Contoso.Logging --version 2.3.1');
      expect([...commandPanel(r)!.querySelectorAll('[role="tab"]')].map((t) => text(t))).toEqual(['.NET CLI', 'NuGet CLI', 'Visual Studio']);
      await closeCommand(r);
    });

    /** Gives version 2.3.1 of the fake the members the server sends for what it read of the package. */
    const withFacts = (extra: Record<string, unknown>, files: string[] = []) => (fake: FakePackagesApi) => {
      for (const v of fake.versionList) {
        if (v.version !== '2.3.1') continue;
        v.metadata = { ...v.metadata, ...extra };
        for (const path of files) v.files = [...v.files, { ...v.files[0]!, path }];
      }
    };

    it('R-NUGET-037 a .NET tool offers the .NET CLI alone, its forms in the select Form, the global install first', async () => {
      const r = await nuget({}, withFacts({ kind: 'dotnet-tool', packageTypes: ['DotnetTool'] }));
      expect((await command(r))?.trim()).toBe('dotnet tool install --global Contoso.Logging --version 2.3.1');
      const panel = commandPanel(r)!;
      expect(panel.querySelectorAll('[role="tab"]')).toHaveLength(0);
      const select = panel.querySelector<HTMLSelectElement>('.command-form select')!;
      expect([...select.options].map((o) => o.text)).toEqual(['Global tool', 'Local tool', 'Cake', 'NUKE']);
      select.value = 'dotnet.install.tool-local';
      select.dispatchEvent(new Event('change'));
      await settle(r.fixture);
      expect((await command(r))?.trim()).toBe('dotnet tool install --local Contoso.Logging --version 2.3.1');
      await expectAccessible(panel);
      await closeCommand(r);
    });

    it('R-NUGET-037 a library has every form of nuget.org, by the client that runs it', async () => {
      const r = await nuget();
      await command(r);
      const options = (panel: HTMLElement) =>
        [...panel.querySelectorAll<HTMLSelectElement>('[role="tabpanel"]:not([inert]) .command-form select')].flatMap((s) => [...s.options].map((o) => o.text));
      expect(options(commandPanel(r)!)).toEqual(['dotnet add package', 'Paket', 'Script and interactive', 'File-based app', 'Cake add-in', 'Cake tool']);
      await closeCommand(r);
    });

    it('R-NUGET-038 Details names the frameworks, the symbols held and the release notes as text, and the links the branch and the commit', async () => {
      const r = await nuget(
        { tab: 'details' },
        withFacts(
          {
            frameworks: ['net462', 'net8.0', 'netstandard2.0'],
            dependencyFrameworks: ['net9.0'],
            repositoryUrl: 'https://github.com/contoso/logging.git',
            repositoryBranch: 'refs/heads/main',
            repositoryCommit: '0123456789abcdef0123456789abcdef01234567',
            releaseNotes: 'First line\nSecond line <b>not markup</b>',
          },
          ['contoso.logging/2.3.1/contoso.logging.2.3.1.snupkg'],
        ),
      );
      expect([...r.root.querySelectorAll('[data-testid="frameworks"] .badge')].map((li) => text(li))).toEqual(['net462', 'net8.0', 'netstandard2.0']);
      expect([...r.root.querySelectorAll('[data-testid="dependency-frameworks"] .badge')].map((li) => text(li))).toEqual(['net9.0']);
      // The branch and the commit stand in the row of links after the source (Q-UI-1362).
      const links = [...r.root.querySelectorAll<HTMLAnchorElement>('quader-version-links a')].map((a) => [text(a), a.getAttribute('href')]);
      expect(links).toEqual(
        expect.arrayContaining([
          [expect.stringContaining('Branch main'), 'https://github.com/contoso/logging/tree/main'],
          [expect.stringContaining('Commit 0123456789ab'), 'https://github.com/contoso/logging/commit/0123456789abcdef0123456789abcdef01234567'],
        ]),
      );
      expect(text(r.root.querySelector('[data-testid="symbols"]')!)).toContain('contoso.logging.2.3.1.snupkg');
      const notes = r.root.querySelector<HTMLElement>('[data-testid="release-notes"]')!;
      expect(notes.textContent).toBe('First line\nSecond line <b>not markup</b>');
      expect(notes.querySelector('b')).toBeNull();
      expect(pairs(r).join('|')).not.toContain('Release notes');
      await expectAccessible(r.root);
    });

    it('R-NUGET-038 a licence file is the licence\'s link, to its text on this installation, and the icon is read from it too', async () => {
      const r = await nuget({}, withFacts({ licenseFile: 'LICENSE.txt', licenseUrl: undefined, hasIcon: true }, []));
      const link = r.root.querySelector<HTMLAnchorElement>('[data-testid="license-file"]')!;
      expect(link.getAttribute('href')).toBe('/api/v1/packages/pkg_01JBQ7ZZZZZZ/versions/2.3.1/license');
      const icon = r.root.querySelector<HTMLImageElement>('[data-testid="package-icon"]')!;
      expect(icon.getAttribute('src')).toBe('/api/v1/packages/pkg_01JBQ7ZZZZZZ/versions/2.3.1/icon');
      expect(icon.getAttribute('alt')).toBe('');
      icon.dispatchEvent(new Event('error'));
      await settle(r.fixture);
      expect(r.root.querySelector('[data-testid="package-icon"]')).toBeNull();
    });

    it('R-NUGET-039 the page says what runs when a project uses the package, under the links, with the files', async () => {
      const r = await nuget(
        {},
        withFacts({
          buildFiles: ['build/Contoso.Logging.targets', 'buildTransitive/Contoso.Logging.props'],
          analyzers: ['analyzers/dotnet/cs/Contoso.Logging.Generators.dll'],
          scripts: ['tools/install.ps1'],
        }),
      );
      expect(text(r.root.querySelector('[data-testid="runs-build"]')!)).toContain(
        'Adds build logic that runs in every build of a project that references it: build/Contoso.Logging.targets, buildTransitive/Contoso.Logging.props',
      );
      expect(text(r.root.querySelector('[data-testid="runs-analyzers"]')!)).toContain('analyzers/dotnet/cs/Contoso.Logging.Generators.dll');
      expect(text(r.root.querySelector('[data-testid="runs-scripts"]')!)).toContain('PowerShell scripts');
      const plain = await nuget();
      expect(plain.root.querySelector('[data-testid^="runs-"]')).toBeNull();
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

    it('R-OCI-041 the Files tab names the tag item the manifest, whose checksum is the manifest digest', async () => {
      const r = await image({ version: '1.1.0', tab: 'files' });
      const rows = cells(r);
      expect(rows).toHaveLength(1);
      expect([...r.root.querySelectorAll('.version-tabs table tbody th .file-path')].map((e) => text(e))).toEqual(['Manifest']);
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
        'Installed size': '280.0 KiB',
        'Multi-Arch': 'foreign',
        'Source package': 'hello (2.10-3)',
      });
      // The source package lists its binaries: the package list filtered by it (Q-DEB-142).
      const source = r.root.querySelector<HTMLAnchorElement>('[data-testid="deb-facts"] dd a');
      expect(source?.getAttribute('href')).toContain('keyword=hello');
      expect([...r.root.querySelectorAll<HTMLAnchorElement>('quader-version-links a[target="_blank"]')].map((a) => a.href)).toContain(
        'https://www.gnu.org/software/hello/',
      );
      await expectAccessible(r.root);
    });

    it('R-DEB-030 fields a version lacks are left out', async () => {
      const bare = await deb({ tab: 'details', version: '2.9-1' });
      expect(Object.keys(facts(bare))).toEqual(['Architectures', 'Suites and components']);
    });

    it('R-DEB-038 Details shows the long description, and the relations as lists whose held names link to their package', async () => {
      const r = await deb({ tab: 'details' }, (fake) => {
        fake.packages.push({ ...fake.packages.find((p) => p.format === 'deb')!, id: 'pkg_libc6', name: 'libc6' });
      });
      const details = r.root.querySelector('.version-tabs')!;
      expect(text(details.querySelector('.deb-description')!)).toContain('friendly greeting');
      expect(details.querySelector('.deb-verbatim')?.textContent).toBe(' * a greeting\n * a farewell');
      const lines = [...details.querySelectorAll('quader-dependency-list li')].map((li) => text(li));
      expect(lines).toEqual(['libc6 >= 2.34', 'python3:any or python3-minimal', 'hello-doc', 'greeter = 2.10']);
      expect([...details.querySelectorAll<HTMLAnchorElement>('quader-dependency-list a')].map((a) => text(a))).toEqual(['libc6']);
      // A field the server could not parse is its text.
      expect(text(details.querySelector('.dependency-text')!)).toBe('hello-old [amd64');
      await expectAccessible(r.root);
    });

    it('R-DEB-039 the changelog is a tab of text, the copyright a closed fold, the files a filtered list under the package files', async () => {
      const r = await deb({ tab: 'changelog' });
      expect(tabLabels(r)).toContain('Changelog');
      expect(r.root.querySelector('[data-testid="deb-changelog"]')?.textContent).toContain('* Rebuild.');
      expect(text(r.root.querySelector('quader-deb-changelog')!)).toContain('newest entries');
      // No markup of the text becomes an element.
      expect(r.root.querySelector('[data-testid="deb-changelog"] a')).toBeNull();

      await set(r, { tab: 'files' });
      const rows = () => [...r.root.querySelectorAll('quader-deb-files tbody tr')].map((tr) => text(tr));
      expect(rows()[1]).toContain('/usr/bin/hi');
      expect(rows()[1]).toContain('link to hello');
      const filter = r.root.querySelector<HTMLInputElement>('quader-deb-files input[type="search"]')!;
      vi.useFakeTimers();
      filter.value = 'changelog';
      filter.dispatchEvent(new Event('input'));
      vi.advanceTimersByTime(300);
      vi.useRealTimers();
      await settle(r.fixture);
      expect(rows()).toHaveLength(1);
      expect(text(r.root.querySelector('.deb-files-count')!)).toBe('1 of 3 files match');

      await set(r, { tab: 'details' });
      const fold = r.root.querySelector('.deb-copyright-fold button')!;
      expect(fold.getAttribute('aria-expanded')).toBe('false');
      expect(r.root.querySelector('[data-testid="deb-copyright"]')?.textContent).toContain('copyright-format/1.0/');
      await expectAccessible(r.root);
    });

    it('R-DEB-039 a version Quader has not read yet says so, and one it could not read says why in its own words', async () => {
      const pending = await deb({ tab: 'changelog', version: '2.9-1' });
      expect(text(pending.root.querySelector('quader-deb-changelog')!)).toContain('Quader is reading the package file');
      const refused = await deb({ tab: 'changelog' }, (fake) => {
        fake.contentsFixtures.set('pkg_01JBQ9DEBDEB@2.10-3', {
          state: 'unavailable',
          reason: 'too_large',
          detail: 'The data archive crosses a bound of the reading: the content is larger than 2147483648 bytes',
          repository: 'web/deb-local',
          architecture: 'amd64',
          architectures: ['amd64'],
        });
      });
      expect(text(refused.root.querySelector('quader-deb-changelog .deb-note')!)).toContain('larger than Quader reads');
      expect(refused.root.querySelector('quader-deb-changelog quader-technical-details')).not.toBeNull();
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

    it('R-ANS-060 a deprecated collection says that every version is, and the menu deprecates the collection', async () => {
      const r = await collection({}, (fake) => {
        for (const v of fake.versionList) if (v.name === 'acme.tools') v.deprecation = { scope: 'package', message: 'Use acme.kit.' };
      });
      expect(text(r.root.querySelector('[data-testid="deprecation-banner"] .deprecation-text')!)).toBe(
        'Deprecated. Every version of this collection is deprecated. Use acme.kit.',
      );
      const plain = await collection();
      plain.root.querySelector<HTMLButtonElement>('[data-testid="version-acts"]')!.click();
      await settle(plain.fixture);
      expect([...document.querySelectorAll('.cdk-overlay-container .menu-item')].map((b) => text(b))).toContain('Deprecate collection');
    });

    it('R-ANS-025 the head\'s Install shows the collection install command of the version under the client\'s name, no strip of one tab, and no error notice', async () => {
      const r = await collection();
      expect((await command(r))?.trim()).toBe('ansible-galaxy collection install acme.tools:1.2.0');
      const panel = commandPanel(r)!;
      expect(panel.querySelector('[role="tablist"]')).toBeNull();
      expect(text(panel.querySelector('.command-client')!)).toBe('ansible-galaxy');
      expect(panel.querySelector('a.setup-link')!.getAttribute('href')).toContain('/setup?client=ansible-galaxy');
      await closeCommand(r);
      expect(r.root.querySelector('quader-error-notice')).toBeNull();
    });

    it('R-ANS-026 Details lists the Ansible versions it needs and the tags as keywords; the licences are the facts of the header', async () => {
      const r = await collection({ tab: 'details' });
      expect(facts(r)).toEqual({ 'Requires Ansible': '>=2.15.0' });
      expect([...r.root.querySelectorAll('quader-package-tags a')].map((a) => text(a))).toEqual(['tools', 'linux']);
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

    it('R-UI-194 while ClamAV definition updates is on, a stored container downloads and the head leads to the client setup, with no command per version', async () => {
      const { root } = await definition('files', true);
      expect(root.querySelector('a[download]')!.getAttribute('href')).toBe('/api/v1/projects/upstream/repositories/clamav/content/daily/28190/daily.cvd');
      expect(text(root.querySelector('[data-testid="install-clamav"]')!)).toContain('A ClamAV installation always takes the version in use');
      expect(root.querySelector('quader-page-header quader-client-command-menu')).toBeNull();
      const setup = [...root.querySelectorAll('quader-page-header a')].find((a) => text(a) === 'Set up client')!;
      expect(setup.getAttribute('href')).toBe('/projects/upstream/repositories/clamav/setup?client=freshclam');
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
            ? { ...v, files: v.files.map((f) => ({ ...f, malware: malwareStatus({ state, scannedAt: state === 'clean' ? '2026-11-24T10:00:00Z' : null, waiting: state === 'pending' ? 'queued' : null }) })) }
            : v,
        );
      };
      const r = await open({ tab: 'files' }, (fake) => pending(fake, 'pending'));
      const reads = vi.spyOn(r.fake, 'version');
      const malwareCell = () => text(r.root.querySelector('quader-malware-status')!);
      expect(malwareCell()).toContain('Queued');
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
      expect(tabLabels(r)).toEqual(['README', 'Vulnerabilities', 'Files', 'Details']);
      expect(selected()).toBe('README');
    } finally {
      vi.useRealTimers();
    }
  });
});
