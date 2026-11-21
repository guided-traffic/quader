import { Locator, Page, expect } from '@playwright/test';
import {
  Kind,
  kinds,
  ansiblePackage,
  debPackage,
  deepFolder,
  emptyProject,
  exposureRule,
  group,
  longDebKey,
  longNamePackage,
  longNpmKey,
  mainProject,
  npmPackage,
  nugetPackage,
  ociPackage,
  otherProject,
  projectServiceAccount,
  probedCache,
  remoteCache,
  remoteImage,
  repositoryServiceAccount,
  serviceAccount,
  blockedAddress,
  ciIssuer,
  ciProject,
} from './seed';
import { gradedPackage, manyVersionsPackage } from './advisories';
import { malwarePackage, quarantine } from './malware';
import { routeTooMany } from './suggestions';

/**
 * Every screen of the app (web/src/app/app.routes.ts and the route files of the feature areas), with the parameters of the
 * seed and the kinds of user that reach it from the navigation. A screen's tabs (`role=tab` inside `main`) are found and
 * rendered one by one; a tab strip of links leads to other routes, which are screens of their own.
 */
export interface Screen {
  /** The file name's screen part and the contact sheet's group. */
  readonly id: string;
  readonly path: string;
  readonly kinds: readonly Kind[];
  /** Answers of the app's own API calls that are the screen's normal state, not a failure, each with its reason. */
  readonly expected?: readonly { readonly match: RegExp; readonly why: string }[];
  /**
   * A flow through the screen: each value it yields names a state that is rendered, after the actions before it ran. Without a
   * walk the screen is rendered as it opens and with each of its tabs.
   */
  readonly walk?: (page: Page) => AsyncGenerator<string>;
  /**
   * Widths beyond the run's three, for this screen only: where a list changes its form between them (R-UI-173, R-UI-174). The
   * start page's list was wider than the page at 1,100 px, which neither 1,024 nor 1,440 showed.
   */
  readonly widths?: readonly number[];
}

/**
 * A screen with a field that names a principal (R-UI-150): as it opens, then with the matches of a search open under the field
 * (the avatar, the name and the username of each match).
 */
function principalSearch(label: string, text: string): Screen['walk'] {
  return async function* (page) {
    yield '';
    await page.getByRole('combobox', { name: label, exact: true }).first().fill(text);
    await expect(page.locator('quader-principal-field [role="option"]').first()).toBeVisible();
    yield 'search';
  };
}

/**
 * A list as it opens, then with its filter drawer open over the page (Q-UI-420): the list behind it keeps its width and columns.
 * With `fill`, the drawer's filters are then set through its fields and applied: the head shows them as chips beside **Filters**
 * (Q-UI-422), and at 390 px more chips than fit fold into **+N more**.
 */
function filtersOpen(fill?: (drawer: Locator) => Promise<void>): Screen['walk'] {
  return async function* (page) {
    yield '';
    await page.locator('main .filter-toggle').first().click();
    const drawer = page.getByRole('dialog', { name: 'Filters' });
    await expect(drawer.locator('form.filter-form')).toBeVisible();
    yield 'filters';
    if (!fill) return;
    await fill(drawer);
    await drawer.getByRole('button', { name: 'Apply filters' }).click();
    await expect(drawer).toBeHidden();
    await expect(page.locator('main .list-filters .chip-row')).toBeVisible();
    yield 'chips';
  };
}

/** A list without a drawer: as it opens, then with a text in the search field of its head (Q-UI-421). */
function searched(text: string): Screen['walk'] {
  return async function* (page) {
    yield '';
    const field = page.locator('main .list-search input');
    await field.fill(text);
    await field.press('Enter');
    await expect(field).toHaveValue(text);
    yield 'search';
  };
}

/**
 * A repository's items as they open, then with the first item opened (R-UI-176): the opened row's versions table, its copy
 * buttons, badges and grades stand on their lines as every other row's controls do. The row's button is the one the owner's
 * picture of 2026-11-19 showed off its line.
 */
async function* rowOpened(page: Page): AsyncGenerator<string> {
  yield '';
  await page.locator('main .name-cell button[aria-expanded="false"]').first().click();
  await expect(page.locator('main .collapse-region.open quader-repository-item-versions table.data tbody tr').first()).toBeVisible();
  yield 'opened';
}

/**
 * The page Repositories of the project upstream (R-UI-192): as it opens, with the section of its system repositories closed; with
 * the section opened by its toggle; and, closed again, with a filter text that matches one of them, which opens the section and
 * counts the match while the list above says that none of its rows matches.
 */
async function* systemRepositories(page: Page): AsyncGenerator<string> {
  const section = page.locator('main quader-disclosure.system-repositories');
  const toggle = section.locator('button.toggle');
  await expect(section.locator('.heading-count')).toHaveText('2');
  yield '';
  await toggle.click();
  await expect(toggle).toHaveAttribute('aria-expanded', 'true');
  yield 'opened';
  await toggle.click();
  await expect(toggle).toHaveAttribute('aria-expanded', 'false');
  const field = page.locator('main .list-search input');
  await field.fill('trivy');
  await field.press('Enter');
  await expect(toggle).toHaveAttribute('aria-expanded', 'true');
  await expect(section.locator('.heading-count')).toHaveText('1');
  yield 'search';
}

/** The repository list of the main project with two filters set: a format and a class. */
const repositoryChips = async (drawer: Locator): Promise<void> => {
  await drawer.getByRole('combobox', { name: 'Format' }).selectOption('npm');
  await drawer.getByRole('combobox', { name: 'Class' }).selectOption('local');
};

/** The packages of the main project with three filters set: a type, a repository and a period. */
const packageChips = async (drawer: Locator): Promise<void> => {
  await drawer.getByRole('checkbox', { name: 'npm', exact: true }).check();
  await drawer.getByRole('checkbox', { name: `${mainProject}/npm-vendor`, exact: true }).check();
  await drawer.getByRole('combobox', { name: 'Created' }).selectOption('30');
};

/** The findings of the main project with five filters set: more chips than fit beside **Filters** at 390 px. */
const findingChips = async (drawer: Locator): Promise<void> => {
  await drawer.getByRole('checkbox', { name: 'Critical', exact: true }).check();
  await drawer.getByRole('checkbox', { name: 'High', exact: true }).check();
  await drawer.getByRole('checkbox', { name: `${mainProject}/npm-vendor`, exact: true }).check();
  await drawer.getByLabel('Fix available').selectOption('true');
  await drawer.getByRole('combobox', { name: 'Kind' }).selectOption('vulnerability');
};

/** The security dashboard's drawer with two projects (one with a long name) and a severity set (R-UI-159, Q-UI-624). */
const securityChips = async (drawer: Locator): Promise<void> => {
  await drawer.getByRole('checkbox', { name: 'Web frontend', exact: true }).check();
  await drawer.getByRole('checkbox', { name: 'Customer portal release engineering and delivery', exact: true }).check();
  await drawer.getByRole('checkbox', { name: 'Critical', exact: true }).check();
};

/** The users with two filters set. */
const userChips = async (drawer: Locator): Promise<void> => {
  await drawer.getByLabel('Status').selectOption('active');
  await drawer.getByLabel('Origin').selectOption('jit');
};

/**
 * The operations of a folder as its Owner uses them (R-UI-011, R-UI-012): the folder, the upload dialog with a file chosen and
 * then uploaded (the upload is answered by the walk, so the seed's folder stays as it is), New folder with a name typed, the
 * folder's menu, and the confirmation of Delete folder from it, cancelled.
 */
async function* folderOperations(page: Page): AsyncGenerator<string> {
  yield '';
  if ((await page.getByRole('button', { name: 'Upload files' }).count()) === 0) return;
  await page.getByRole('button', { name: 'Upload files' }).click();
  const dialog = page.getByRole('dialog');
  await expect(dialog.getByRole('heading', { name: 'Upload files' })).toBeVisible();
  await dialog.locator('input[type=file]').setInputFiles({
    name: 'design-system-2.5.0-release-notes-for-the-customer-portal-frontend.md',
    mimeType: 'text/markdown',
    buffer: Buffer.from('# Design system 2.5.0\n'),
  });
  yield 'upload';
  await page.route('**/api/v1/projects/*/repositories/*/content/**', (route) =>
    route.request().method() === 'PUT' ? route.fulfill({ status: 201, json: {} }) : route.fallback(),
  );
  await dialog.getByRole('button', { name: 'Upload', exact: true }).click();
  await expect(dialog.getByText('uploaded', { exact: true })).toBeVisible();
  yield 'uploaded';
  await page.unroute('**/api/v1/projects/*/repositories/*/content/**');
  await dialog.getByRole('button', { name: 'Close' }).click();
  await page.getByRole('button', { name: 'New folder' }).click();
  await page.getByLabel('Folder name').fill('2026/q4-hotfixes');
  yield 'new-folder';
  await page.getByRole('dialog').getByRole('button', { name: 'Cancel' }).click();
  await page.getByRole('button', { name: 'More actions for this folder' }).click();
  await expect(page.getByRole('menuitem', { name: 'Delete folder' })).toBeVisible();
  yield 'folder-menu';
  await page.getByRole('menuitem', { name: 'Delete folder' }).click();
  await expect(page.getByRole('dialog')).toContainText('Delete releases/design-system/2.5.0?');
  yield 'delete-folder';
  await page.getByRole('dialog').getByRole('button', { name: 'Cancel' }).click();
}

/** A deep path as it opens, then, where it is narrow, with the folders in between opened from their menu (Q-UI-521). */
async function* deepPath(page: Page): AsyncGenerator<string> {
  yield '';
  const more = page.getByRole('button', { name: 'Show the folders in between' });
  if (!(await more.isVisible())) return;
  await more.click();
  await expect(page.getByRole('menu')).toBeVisible();
  yield 'folders';
}

/** A file's page, then its Details opened, then, for whoever may delete it, the confirmation of Delete, cancelled. */
async function* fileView(page: Page): AsyncGenerator<string> {
  yield '';
  await page.getByRole('button', { name: /^Details/ }).click();
  await expect(page.getByText('Counts reach the database within a minute of a download.')).toBeVisible();
  yield 'details';
  const remove = page.locator('.file-actions').getByRole('button', { name: 'Delete' });
  if ((await remove.count()) === 0) return;
  await remove.click();
  await expect(page.getByRole('dialog')).toContainText('The file is deleted for good.');
  yield 'delete';
  await page.getByRole('dialog').getByRole('button', { name: 'Cancel' }).click();
}

/** The file of `upstream/scanner-clamav` the file page of a system repository shows: a signature database as the task stores it. */
const systemFile = 'main-63.daily-28189.bytecode-339/main.cvd';

/**
 * The page of a file of a system repository (R-UI-193, Q-UI-898, Q-UI-899): no Download, no download address and no download with
 * curl, one sentence in the address's place; as it opens and with Details open. The profile runs no malware scanning, so
 * `scanner-clamav` holds nothing: the item and its statistics are the answers the server gives for a stored version, routed in the
 * browser; the repository, the session's checks and everything else are the server's.
 */
async function* systemFileView(page: Page): AsyncGenerator<string> {
  const base = '**/api/v1/projects/upstream/repositories/scanner-clamav';
  const at = '2026-11-21T06:30:12Z';
  const item = {
    repository: 'upstream/scanner-clamav',
    path: systemFile,
    kind: 'file',
    size: 170_479_789,
    mediaType: 'application/octet-stream',
    created: { at, by: 'system:malware-database' },
    modified: { at, by: 'system:malware-database' },
    updatedAt: at,
    digests: {
      sha256: '6f1c0d7d9a9b3c1e5a0f7e2d4b8c9a1e3f5d7b9c1a3e5f7d9b1c3e5a7f9d1b3c',
      sha512: '9e1b'.repeat(32),
      sha1: '3a5c7e9b1d3f5a7c9e1b3d5f7a9c1e3b5d7f9a1c',
      md5: '7c9e1b3d5f7a9c1e3b5d7f9a1c3e5b7d',
    },
    declaredMismatch: false,
    contentStatus: 'ok',
    package: null,
    upstreamUrl: null,
    downloadUrl: `https://localhost:4200/files/upstream/scanner-clamav/${systemFile}`,
    contentUrl: `/api/v1/projects/upstream/repositories/scanner-clamav/content/${systemFile}`,
    itemUrl: `https://localhost:4200/api/v1/projects/upstream/repositories/scanner-clamav/items?path=${encodeURIComponent(systemFile)}`,
  };
  await page.route(`${base}/items?**`, (route) => route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(item) }));
  await page.route(`${base}/stats?**`, (route) =>
    route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ downloads: 0, lastDownloadAt: null, lastDownloader: null }) }),
  );
  await page.reload();
  await expect(page.locator('[data-testid="file-system"]')).toBeVisible();
  yield '';
  await page.getByRole('button', { name: /^Details/ }).click();
  await expect(page.getByText('Counts reach the database within a minute of a download.')).toBeVisible();
  yield 'details';
  await page.unroute(`${base}/items?**`);
  await page.unroute(`${base}/stats?**`);
}

/**
 * The admin area's repositories of the project upstream (R-UI-193, Q-UI-898, Q-UI-902): every registry with its copy button and its
 * menu, the two system repositories with the badge **System** and the menu alone; then the menu of `scanner-trivy`, without **Copy
 * client address**.
 */
async function* systemRows(page: Page): AsyncGenerator<string> {
  await expect(page.locator('main tbody th[scope="row"] .badge', { hasText: /^System$/ })).toHaveCount(2);
  yield '';
  await page.getByRole('button', { name: 'Actions for scanner-trivy' }).click();
  await expect(page.locator('.menu').getByRole('menuitem', { name: 'Files' })).toBeVisible();
  yield 'menu';
  await page.keyboard.press('Escape');
}

/** The top bar's suggestions for a text that finds the two system repositories, each with the badge **System** (Q-UI-902). */
async function* systemSuggestions(page: Page): AsyncGenerator<string> {
  yield '';
  const narrow = (page.viewportSize()?.width ?? 1440) < 576;
  if (narrow) await page.getByRole('button', { name: 'Search', exact: true }).click();
  else await page.locator('#global-search').click();
  await page.locator('#global-search').fill('scanner');
  await expect(page.locator('#search-suggestions [role=option]', { hasText: 'scanner-trivy' })).toContainText('System');
  yield 'search-system';
}

/** A folder with more entries than a page holds, then filtered while a page is still to load, then with every page loaded. */
async function* manyEntries(page: Page): AsyncGenerator<string> {
  yield '';
  const filter = page.getByRole('searchbox', { name: 'Filter files' });
  await filter.fill('00');
  await expect(page.getByText('The filter searches the 200 entries loaded so far.')).toBeVisible();
  yield 'filtered';
  await filter.fill('');
  await page.getByRole('button', { name: 'Load more' }).click();
  await expect(page.locator('main tbody tr')).toHaveCount(230);
  yield 'more';
}

/** The list as it opens, then sorted by its Detected button: the click fails where the button cannot be reached. */
async function* sortByDetection(page: Page): AsyncGenerator<string> {
  yield '';
  await page.locator('th').getByRole('button', { name: 'Detected' }).click();
  await expect(page).toHaveURL(/sort=detectedAt/);
  yield 'sorted';
}

/**
 * Set up client (R-UI-120, R-UI-125): the page as it opens, then with every folded section of the explanation opened, which is
 * where long prose and its code blocks are judged.
 */
async function* explained(page: Page): AsyncGenerator<string> {
  yield '';
  if (await openExplanation(page)) yield 'explained';
}

/**
 * The advisory sources as they open; with the fold of the image scanners' replicas open (R-OPS-048, Q-UI-897); where the image
 * database comes from a local repository, with the fold of the commands that fill it open (R-VULN-045, Q-UI-890), which the
 * database's default repository, a remote, does not have; and without a replica, the profile's own answer: its role all analyses
 * images in its own process.
 */
async function* databaseCommandsOpen(page: Page): AsyncGenerator<string> {
  yield '';
  const scanners = page.locator('main [data-testid="image-scanners"] quader-disclosure button.inline-toggle');
  await expect(scanners).toHaveAttribute('aria-expanded', 'false');
  await scanners.click();
  await expect(scanners).toHaveAttribute('aria-expanded', 'true');
  yield 'scanners';
  const closed = page.locator('main quader-vulnerability-sources > quader-disclosure button[aria-expanded="false"]');
  if ((await closed.count()) > 0) {
    await closed.first().click();
    await expect(page.locator('main quader-vulnerability-sources > quader-disclosure button[aria-expanded="true"]')).toHaveCount(1);
    yield 'commands';
  }
  const url = '**/api/v1/system/scanners';
  await answerWith(page, url, (b) => ({ ...b, items: [] }));
  await expect(page.locator('main [data-testid="scanners-none-trivy"]')).toBeVisible();
  yield 'scanners-none';
  await page.unroute(url);
}

/**
 * The System page's scanner replicas (R-OPS-048, Q-UI-897): as seeded (both kinds, a malware scanner that waits for its first database
 * with the longest pod name); no replica of either kind reported though both report, which the header warns of; and the profile's own
 * answer without rows: its role all runs no scanner pod and says so.
 */
async function* systemScanners(page: Page): AsyncGenerator<string> {
  await expect(page.locator('main [data-testid="scanner-replicas"] table')).toHaveCount(2);
  yield '';
  const url = '**/api/v1/system/scanners';
  await answerWith(page, url, (b) => ({
    ...b,
    items: [],
    kinds: (b['kinds'] as Record<string, unknown>[]).map((k) => ({ ...k, replicas: 'reporting' })),
  }));
  await expect(page.locator('main [data-testid="scanners-none-clamav"]')).toBeVisible();
  yield 'scanners-none';
  await answerWith(page, url, (b) => ({ ...b, items: [] }));
  await expect(page.locator('main [data-testid="scanners-none-trivy"]')).toBeVisible();
  yield 'scanners-all';
  await page.unroute(url);
}

/** Opens every folded section of the explanation; false where there is none. */
async function openExplanation(page: Page): Promise<boolean> {
  const selector = 'main quader-guide-view quader-disclosure button[aria-expanded="false"]';
  if ((await page.locator(selector).count()) === 0) return false;
  // One click each, in the page: a click by position could hit a heading that the previous section's opening moved.
  await page.$$eval(selector, (buttons) => buttons.forEach((b) => (b as HTMLButtonElement).click()));
  await expect(page.locator(selector)).toHaveCount(0);
  return true;
}

/**
 * Set up client of the npm virtual repository: as it opens; with another client chosen in the one control (Q-UI-482); with More
 * options open and a token to be written into the snippets (R-UI-122); with the explanation opened. No token is created: the render
 * database is shared.
 */
async function* setupWalk(page: Page): AsyncGenerator<string> {
  yield '';
  const select = page.locator('main select#setup-client');
  if ((await select.count()) === 0) return;
  await select.selectOption('yarn');
  await expect(page).toHaveURL(/client=yarn/);
  await expect(page.locator('main quader-setup-snippet pre').first()).toContainText('npmRegistryServer');
  yield 'yarn';
  await page.locator('main quader-disclosure.more-options').getByRole('button', { name: 'More options' }).click();
  await page.locator('main input[name="credentials"][value="paste"]').check();
  await expect(page.locator('main #setup-token')).toBeVisible();
  yield 'more';
  if (await openExplanation(page)) yield 'explained';
}

/** The suggestions' answer of a 429 and its seconds: the notice of R-UI-097 is a state of the field. */
const tooMany = [
  { match: /^GET \/api\/v1\/search\/suggestions\?\S* answered 429$/, why: 'the 429 notice is rendered on purpose' },
  { match: /^GET \/api\/v1\/auth\/whoami answered 401$/, why: 'an anonymous visitor has no session' },
];

/**
 * The search field of the top bar and its list (R-UI-095 to R-UI-097): the bar as it opens; the list opened empty; typing `web`
 * (every group of the main area); the start of the seed's longest key; a text with no match; `us` where the caller is an instance
 * administrator (Admin pages, Users); the filter menu and the chip **Packages**; the 429 notice. Below 36 rem the button opens the
 * sheet, and the sheet with suggestions is the state. The suggestions are the server's (R-API-060); only the 429 is routed.
 */
async function* searchField(page: Page): AsyncGenerator<string> {
  // The 429 of the previous width's last state stays on the page: the server answers again.
  await page.unroute('**/api/v1/search/suggestions?**');
  yield '';
  const narrow = (page.viewportSize()?.width ?? 1440) < 576;
  const field = page.locator('#global-search');
  const list = page.locator('#search-suggestions');
  const heading = (name: string | RegExp) => list.getByRole('group', { name, exact: typeof name === 'string' });
  if (narrow) await page.getByRole('button', { name: 'Search', exact: true }).click();
  else await field.click();
  await expect(list).toBeVisible();
  const admin = (await heading('Admin area').count()) > 0;
  yield 'search-empty';
  // An anonymous visitor reads the public repository collections of the platform project only.
  const anonymous = (await page.getByRole('link', { name: 'Sign in' }).count()) > 0;
  await field.fill(anonymous ? 'coll' : 'web');
  await expect(heading('Repositories')).toBeVisible();
  yield 'search-typing';
  // The other states are a signed-in user's; an anonymous visitor's list differs only in what it may read.
  if (narrow || anonymous) return;
  await field.fill(longNpmKey.slice(0, 24));
  await expect(list.locator('[role=option]').first()).toContainText('release-candidates');
  yield 'search-long';
  await field.fill('zzqx');
  await expect(page.locator('.search-status')).toContainText('Nothing you may read matches');
  yield 'search-none';
  if (admin) {
    await field.fill('us');
    await expect(heading(/^Admin pages/)).toBeVisible();
    yield 'search-admin';
  }
  await page.locator('.search-filter').click();
  await expect(page.locator('.search-filter-menu')).toBeVisible();
  yield 'search-filter-menu';
  await page.getByRole('menuitemradio', { name: 'Packages' }).click();
  await field.fill('acme');
  await expect(heading('Packages')).toBeVisible();
  yield 'search-chip';
  await routeTooMany(page, 30);
  await field.fill('acme-ui');
  await expect(page.locator('.search-status')).toContainText('Too many searches');
  yield 'search-limited';
}

/** Below 1024 px the side bar is a drawer behind the top bar's menu button (R-UI-002). */
async function openSidebar(page: Page): Promise<void> {
  if ((page.viewportSize()?.width ?? 1440) >= 1024) return;
  await page.getByRole('button', { name: 'Open navigation' }).click();
  await expect(page.locator('aside.sidebar')).toBeVisible();
}

/** The page with its side bar shown: as it is where the side bar is pinned, the drawer opened below 1024 px. */
async function* sidebarShown(page: Page): AsyncGenerator<string> {
  await openSidebar(page);
  await expect(page.getByRole('navigation', { name: 'Main navigation' }).getByRole('group', { name: 'Security audit', exact: true })).toBeVisible();
  yield '';
}

/**
 * The search's store of frequently visited places (R-UI-096): four repositories (one of them with the seed's long key) opened
 * first, then the side bar's **Recently opened** opened on the start page (R-UI-167; at 390 px in the drawer), the field
 * opened with its frequently visited groups, and the project switcher's recent list.
 */
async function* frequentPlaces(page: Page): AsyncGenerator<string> {
  for (const path of [
    `/ui/projects/${mainProject}/repositories/npm-vendor`,
    `/ui/projects/${otherProject}/repositories/collections`,
    `/ui/projects/${mainProject}/repositories/${longNpmKey}`,
    `/ui/projects/${mainProject}/repositories/npm-internal`,
  ]) {
    await page.goto(path);
    await expect(page.locator('main h1')).toBeVisible();
  }
  await page.goto('/ui/projects');
  await openSidebar(page);
  const group = page.getByRole('navigation', { name: 'Main navigation' }).getByRole('button', { name: 'Recently opened' });
  if ((await group.getAttribute('aria-expanded')) !== 'true') await group.click();
  await expect(page.locator('#nav-group-recent a').first()).toBeVisible();
  yield '';
  await page.keyboard.press('Escape');
  const narrow = (page.viewportSize()?.width ?? 1440) < 576;
  if (narrow) await page.getByRole('button', { name: 'Search', exact: true }).click();
  else await page.locator('#global-search').click();
  await expect(page.locator('#search-suggestions').getByRole('group', { name: 'Frequently visited repositories' })).toBeVisible();
  yield 'search-frequent';
  await page.goto(`/ui/projects/${mainProject}/repositories`);
  if (narrow) await page.getByRole('button', { name: 'Open navigation' }).click();
  await page.locator('.switcher-button').click();
  await expect(page.locator('#project-switcher-panel')).toBeVisible();
  yield 'switcher';
}

/**
 * The versions table of a package of many versions (R-UI-110): as it opens, narrowed by a typed version, sorted by its
 * vulnerabilities (worst first), and with the second page of rows shown.
 */
async function* versionsTable(page: Page): AsyncGenerator<string> {
  yield '';
  const table = page.locator('main .pkg-versions');
  await page.getByRole('searchbox', { name: 'Filter versions' }).fill('3.');
  await expect(table.locator('tbody tr')).toHaveCount(12);
  yield 'filtered';
  await page.getByRole('searchbox', { name: 'Filter versions' }).fill('');
  await table.locator('th').getByRole('button', { name: 'Vulnerabilities' }).click();
  await table.locator('th').getByRole('button', { name: 'Vulnerabilities' }).click();
  await expect(page).toHaveURL(/sort=-vulnerabilities/);
  yield 'by-grade';
}

/** The closed Advanced section at the end of a page, opened: where a page keeps its destructive action (the style guide). */
async function* advancedOpen(page: Page): AsyncGenerator<string> {
  yield '';
  await page.locator('main quader-disclosure').last().getByRole('button', { name: 'Advanced' }).click();
  await expect(page.locator('main quader-disclosure').last().getByRole('button', { name: /^(Delete|Revoke all)/ }).first()).toBeVisible();
  yield 'advanced';
}

/**
 * A repository's settings page (R-UI-087) as it opens, with the standard sections open and the details closed, then with every
 * closed section opened, Advanced last; a caller who sees the reduced view has none to open.
 */
async function* allSections(page: Page): AsyncGenerator<string> {
  yield '';
  const closed = page.locator('main quader-disclosure .toggle[aria-expanded="false"]');
  if ((await closed.count()) === 0) return;
  while ((await closed.count()) > 0) {
    // The toggle is named by the region it controls, and the walk waits until that toggle says it is open. The page writes
    // `aria-expanded` a moment after the click, not with it: counting the closed toggles in that moment found the one just
    // clicked, and the click meant for it then waited 15 s for a closed toggle that no longer existed (thirteen screens of the
    // run on a runner, 2026-11-19; one in twenty runs on a fast machine).
    const region = await closed.first().getAttribute('aria-controls');
    const toggle = page.locator(`main quader-disclosure .toggle[aria-controls="${region}"]`);
    await toggle.click();
    await expect(toggle).toHaveAttribute('aria-expanded', 'true');
    // A section that opens moves the toggles under it while it grows: the next click waits until it stands still, or it hits
    // the toggle that moved into its place and closes a section again.
    await page.evaluate(() => Promise.all(document.getAnimations().map((a) => a.finished.catch(() => undefined))));
  }
  // The sections; a fold inside one (`inline`, the pattern syntax) stays as the page has it.
  await expect(page.locator('main quader-disclosure:not(.inline) > .disclosure > .collapse-region:not(.open)')).toHaveCount(0);
  // But the fold of the wiki's own level (R-UI-055): closed while the wiki has the repository's level, which the first picture
  // shows, and opened here, so that its options are rendered for a repository whose wiki does not differ too.
  const wiki = page.locator('main quader-repository-visibility').getByRole('button', { name: /^Wiki visibility/ });
  if ((await wiki.count()) > 0 && (await wiki.getAttribute('aria-expanded')) !== 'true') {
    await wiki.click();
    await expect(wiki).toHaveAttribute('aria-expanded', 'true');
    await page.evaluate(() => Promise.all(document.getAnimations().map((a) => a.finished.catch(() => undefined))));
  }
  yield 'sections';
}

/**
 * A remote repository's settings page with every section open and the upstream's details too (R-UI-082, R-UI-083): the switch of the
 * background download (R-REPO-065, Q-UI-766) and, for OCI, the platforms in the format's settings (Q-UI-767). A caller who sees the
 * reduced view has nothing to open.
 */
async function* remoteSections(page: Page): AsyncGenerator<string> {
  for await (const step of allSections(page)) {
    if (step === 'sections') {
      const details = page.locator('main quader-upstream-settings').getByRole('button', { name: 'Upstream details' });
      await details.click();
      await expect(page.locator('main #f-remote-downloadOnProbe')).toBeVisible();
      await page.evaluate(() => Promise.all(document.getAnimations().map((a) => a.finished.catch(() => undefined))));
    }
    yield step;
  }
}

/**
 * A service account's page (R-UI-181) as it opens, with its technical tokens (an active one, one with a long name, an expired and a
 * revoked one in the seed), then the create form of a technical token with its name first, where the caller may create one.
 */
async function* technicalTokens(page: Page): AsyncGenerator<string> {
  await expect(page.locator('main quader-technical-token-table tbody tr').first()).toBeVisible();
  yield '';
  // The rule dialog of a token's limit, at the account's place (R-UI-181): its limit opens Set limit for a token without a rule.
  const setLimit = page.locator('main quader-technical-token-table').first().getByRole('button', { name: /Set limit/ });
  if ((await setLimit.count()) > 0) {
    await setLimit.first().click();
    const dialog = page.getByRole('dialog');
    await expect(dialog.getByRole('combobox', { name: 'Profile' })).toBeVisible();
    yield 'set-limit';
    await dialog.getByRole('button', { name: 'Cancel' }).click();
    await expect(dialog).toHaveCount(0);
  }
  const create = page.locator('main').getByRole('button', { name: 'New technical token' });
  if ((await create.count()) === 0) return;
  await create.click();
  await expect(page.locator('main quader-token-form input[name="name"]')).toBeVisible();
  yield 'new-token';
}

/**
 * A section of a settings page by its own head, not by its text: a filter on the text takes every fold that holds the words, a
 * closed one too, and the level Internal's sentence in the fold Wiki visibility names service accounts. The head is the label of
 * the section's toggle; a note after it ("2 rules") does not change it.
 */
function sectionHeaded(page: Page, heading: string): { readonly section: Locator; readonly toggle: Locator } {
  const toggle = page.locator('main quader-disclosure .toggle').filter({ has: page.getByText(heading, { exact: true }) });
  return { section: toggle.locator('xpath=ancestor::quader-disclosure[1]'), toggle };
}

/** A repository's settings with its section Service accounts open, then its create dialog with the rights (R-UI-181). */
async function* repositoryAccounts(page: Page): AsyncGenerator<string> {
  const { section, toggle } = sectionHeaded(page, 'Service accounts');
  await toggle.click();
  await expect(toggle).toHaveAttribute('aria-expanded', 'true');
  await expect(section.locator('quader-repository-service-accounts tbody tr').first()).toBeVisible();
  await page.evaluate(() => Promise.all(document.getAnimations().map((a) => a.finished.catch(() => undefined))));
  yield '';
  await section.getByRole('button', { name: 'New service account' }).click();
  await expect(page.locator('quader-service-account-create-dialog .rights')).toBeVisible();
  yield 'new-account';
}

/** The closed Visibility section of a project's settings page, opened: where the level of a project is chosen (R-UI-054). */
async function* visibilityOpen(page: Page): AsyncGenerator<string> {
  yield '';
  await sectionHeaded(page, 'Visibility').toggle.click();
  await expect(page.locator('main quader-visibility-level').first()).toBeVisible();
  yield 'visibility';
}

/**
 * The visibility settings of the installation (R-UI-140): the section is open as the page opens, with the default project
 * visibility and the restricted levels; then Public is ticked as restricted, which turns the default's option Public off with its
 * reason. Nothing is saved.
 */
async function* restrictedLevels(page: Page): AsyncGenerator<string> {
  await expect(page.locator('main quader-restricted-levels')).toBeVisible();
  yield '';
  await page.locator('main quader-restricted-levels').getByRole('checkbox', { name: /Public/ }).check();
  await expect(page.locator('main quader-visibility-level').getByRole('radio', { name: /Public/ })).toBeDisabled();
  yield 'restricted';
}

/**
 * The caller's visibility choices with the level Internal restricted, answered in the browser from the real answer, so that whether
 * the caller is an administrator stays the server's (R-UI-141). The page is loaded again, because the forms read the choices once.
 */
async function restrictInternal(page: Page): Promise<void> {
  await page.route('**/api/v1/visibility-choices', async (route) => {
    const res = await route.fetch();
    const body = { ...((await res.json()) as Record<string, unknown>), restrictedLevels: ['internal'] };
    await route.fulfill({ response: res, body: JSON.stringify(body) });
  });
  // The reloaded form is used once its calls are answered: a project chosen while the page was still loading stood on "Choose a
  // project" again afterwards, and the walk waited for a key in vain.
  const answered = page.waitForResponse('**/api/v1/visibility-choices');
  await page.reload();
  await answered;
  await page.waitForLoadState('networkidle');
}

/**
 * An answer of the app's API changed in the browser from the real one, and the page loaded again: the states of the malware scan
 * that the shared render database cannot hold (R-MAL-023, R-MAL-024): a stale database with a failure, none yet, the scanning off,
 * the installation's switch on. Whether the caller may read it stays the server's.
 */
async function answerWith(page: Page, url: string, change: (body: Record<string, unknown>) => Record<string, unknown>): Promise<void> {
  await page.unroute(url);
  await page.route(url, async (route) => {
    const res = await route.fetch();
    await route.fulfill({ response: res, body: JSON.stringify(change((await res.json()) as Record<string, unknown>)) });
  });
  const answered = page.waitForResponse(url);
  await page.reload();
  await answered;
  await page.waitForLoadState('networkidle');
}

/**
 * **Settings › Malware scanning** (R-MAL-023): as seeded (the switch off, the backlog of the seed's files without a verdict, a current
 * database, two scanners); the question before the switch goes on with a backlog; a database four days old whose updates fail, a
 * backlog of tens of thousands, no scanner ready and an installation without internet with its commands; no database yet and the
 * switch on; the scanning off in the configuration.
 */
async function* malwareSettings(page: Page): AsyncGenerator<string> {
  const settings = '**/api/v1/malware/settings';
  await expect(page.getByRole('heading', { name: 'Signature database' })).toBeVisible();
  yield '';
  // The replicas of malware-scanner-clamav under the summary, the fold opened (R-OPS-048, Q-UI-897).
  const fold = page.locator('main quader-scanner-replicas quader-disclosure button.inline-toggle');
  await fold.click();
  await expect(fold).toHaveAttribute('aria-expanded', 'true');
  yield 'scanners';
  await page.getByRole('checkbox', { name: 'Only checked artifacts can be downloaded' }).check();
  await page.getByRole('button', { name: /^Save changes/ }).click();
  await expect(page.getByRole('dialog', { name: 'Let only checked artifacts be downloaded?' })).toBeVisible();
  yield 'confirm';
  await page.getByRole('dialog').getByRole('button', { name: 'Cancel' }).click();
  await answerWith(page, settings, (b) => ({
    ...b,
    backlog: { unchecked: 48213, notCheckable: 1207 },
    scanners: { ready: 0, busy: 0, observedAt: new Date(Date.now() - 9 * 60_000).toISOString() },
    database: {
      version: 'main-62.daily-27832.bytecode-339',
      builtAt: new Date(Date.now() - 4 * 86_400_000).toISOString(),
      importedAt: new Date(Date.now() - 4 * 86_400_000 + 3_600_000).toISOString(),
      stale: true,
      repository: 'ops/clamav-db',
      freshclam: false,
      lastError: {
        code: 'database_missing',
        detail: 'The repository ops/clamav-db holds no main.cvd, daily.cvd or bytecode.cvd at its root.',
        at: new Date(Date.now() - 25 * 60_000).toISOString(),
        failures: 12,
      },
    },
  }));
  await expect(page.getByRole('heading', { name: 'Fill the repository' })).toBeVisible();
  yield 'stale';
  await answerWith(page, settings, (b) => ({
    ...b,
    onlyChecked: true,
    backlog: { unchecked: 0, notCheckable: 0 },
    scanners: { ready: 0, busy: 0, observedAt: null },
    database: { ...(b['database'] as object), version: null, builtAt: null, importedAt: null, stale: false, lastError: null },
  }));
  await expect(page.locator('main [data-testid="malware-db-none"]')).toBeVisible();
  yield 'none';
  // No malware scanner replica: the profile's own answer, in which no scanner is configured (R-OPS-048).
  const scanners = '**/api/v1/system/scanners';
  await answerWith(page, scanners, (b) => ({ ...b, items: [] }));
  await expect(page.locator('main [data-testid="scanners-none-clamav"]')).toBeVisible();
  yield 'scanners-none';
  await page.unroute(scanners);
  await answerWith(page, settings, (b) => ({ ...b, enabled: false }));
  await expect(page.locator('main [data-testid="malware-off"]')).toBeVisible();
  yield 'off';
  await page.unroute(settings);
}

/**
 * The malware view of the security dashboard (R-MAL-025) as the instance auditor: the finds as they open (one find in three
 * repositories, a long signature in a long path, the unscannable, failed and released ones); the first find opened to its files; the
 * drawer; the release dialog of the find in three repositories, sent without a reason; the empty list.
 */
async function* malwareFinds(page: Page): AsyncGenerator<string> {
  await expect(page.locator('main tbody tr').first()).toBeVisible();
  yield '';
  await page.locator('main .name-cell button[aria-expanded="false"]').first().click();
  await expect(page.locator('main .collapse-region.open .find-items li').first()).toBeVisible();
  yield 'opened';
  await page.locator('main .name-cell button[aria-expanded="true"]').first().click();
  await page.locator('main .filter-toggle').first().click();
  const drawer = page.getByRole('dialog', { name: 'Filters' });
  await expect(drawer.locator('form.filter-form')).toBeVisible();
  yield 'filters';
  await page.keyboard.press('Escape');
  await expect(drawer).toBeHidden();
  await page.getByRole('button', { name: 'Release eicar.com.txt' }).click();
  const dialog = page.getByRole('dialog', { name: 'Release eicar.com.txt?' });
  await dialog.getByRole('button', { name: 'Release', exact: true }).click();
  await expect(dialog.getByText('A release needs a reason.')).toBeVisible();
  yield 'release';
  await dialog.getByRole('button', { name: 'Cancel' }).click();
  await answerWith(page, '**/api/v1/malware/finds?**', () => ({ items: [], nextCursor: null }));
  await expect(page.getByText(/^No malware found\./)).toBeVisible();
  yield 'empty';
  await page.unroute('**/api/v1/malware/finds?**');
}

/** A version's states at its page (R-MAL-024): the infected tarball, the unscannable one its strict repository refuses, a clean one, one that waits. */
async function* malwareVersions(page: Page): AsyncGenerator<string> {
  await expect(page.locator('main [data-testid="malware-banner"]')).toBeVisible();
  yield '';
  for (const [version, state] of [
    ['2.4.0-rc.1', 'unscannable'],
    ['2.2.0', 'clean'],
    ['2.5.0-next.1', 'pending'],
  ] as const) {
    await page.goto(`${ver(P, malwarePackage, version)}?tab=files`);
    await expect(page.locator('main quader-malware-status').first()).toBeVisible();
    yield state;
  }
}

/** A file's fact at its page (R-MAL-024): a long signature in an archive's member, a release, a file that waits. */
async function* malwareFiles(page: Page): AsyncGenerator<string> {
  await expect(page.locator('main [data-testid="malware-fact"]')).toBeVisible();
  yield '';
  for (const [file, state] of [
    ['nightly-build-cache.tar.zst', 'released'],
    ['README.md', 'pending'],
  ] as const) {
    await page.goto(`${P}/repositories/assets/files/${quarantine}/${file}`);
    await expect(page.locator('main [data-testid="malware-fact"]')).toBeVisible();
    yield state;
  }
}

/**
 * The repository's section **Malware scan** (R-MAL-020): opened on the strict npm repository (its Owner cannot switch it off, an
 * administrator can), then with the installation's switch on, which makes it checked and read-only.
 */
async function* malwareSection(page: Page): AsyncGenerator<string> {
  const open = async (): Promise<void> => {
    const { toggle } = sectionHeaded(page, 'Malware scan');
    await toggle.click();
    await expect(toggle).toHaveAttribute('aria-expanded', 'true');
    await page.evaluate(() => Promise.all(document.getAnimations().map((a) => a.finished.catch(() => undefined))));
  };
  await open();
  yield '';
  await answerWith(page, '**/api/v1/malware/switch', (b) => ({ ...b, onlyChecked: true }));
  await open();
  await expect(page.locator('main [data-testid="repo-malware-installation"]')).toBeVisible();
  yield 'installation-on';
  await page.unroute('**/api/v1/malware/switch');
}

/** A repository's settings with Internal restricted: the Owner keeps the internal levels with a note, an administrator sees notes. */
async function* restrictedSettings(page: Page): AsyncGenerator<string> {
  await restrictInternal(page);
  await expect(page.locator('main quader-repository-visibility .note, main quader-repository-visibility .reason').first()).toBeVisible();
  yield '';
  await page.unroute('**/api/v1/visibility-choices');
}

/**
 * A change of a repository's visibility that the server refuses (R-UI-055): the level is lowered, which takes the wiki with it
 * behind its closed fold, and the save is answered in the browser with a field problem at each level. Each stands under its
 * group, the wiki's in its fold, which the answer opened. Nothing is saved.
 */
async function* visibilityRefused(page: Page): AsyncGenerator<string> {
  const block = page.locator('main quader-repository-visibility');
  await block.getByRole('group', { name: 'Visibility level' }).getByRole('radio', { name: /^Private/ }).check();
  const stored = `**/api/v1/projects/${mainProject}/repositories/npm-internal`;
  await page.route(stored, (route) =>
    route.request().method() === 'PATCH'
      ? route.fulfill({
          status: 422,
          contentType: 'application/problem+json',
          body: JSON.stringify({
            type: 'about:blank',
            title: 'Unprocessable Entity',
            status: 422,
            code: 'validation_failed',
            detail: 'The repository document is not valid.',
            errors: [
              { pointer: '/visibility/artifacts', code: 'visibility_above_project', detail: "The artifacts level is at most the project's level, private." },
              { pointer: '/visibility/wiki', code: 'visibility_wiki_above_artifacts', detail: 'The wiki level is at most the artifacts level, private.' },
            ],
          }),
        })
      : route.fallback(),
  );
  await block.getByRole('button', { name: /^Save changes/ }).click();
  await expect(block.getByRole('button', { name: /^Wiki visibility/ })).toHaveAttribute('aria-expanded', 'true');
  await expect(block.locator('.field-error')).toHaveCount(2);
  await page.evaluate(() => Promise.all(document.getAnimations().map((a) => a.finished.catch(() => undefined))));
  yield '';
  await page.unroute(stored);
}

/** The creation form with Internal restricted: the Owner sees it off with the reason, an administrator with a note (R-UI-141). */
async function* restrictedCreate(page: Page): AsyncGenerator<string> {
  await restrictInternal(page);
  await chooseProject(page);
  await expect(page.locator('#f-key')).not.toHaveValue('');
  await expect(page.locator('main quader-visibility-level .note, main quader-visibility-level .reason').first()).toBeVisible();
  yield '';
  await page.unroute('**/api/v1/visibility-choices');
}

/**
 * In the admin area the form starts without a project: choose the seed's, once the select offers it (R-UI-080). The select is held
 * while the projects load, so a choice is made only after the option is there.
 */
async function chooseProject(page: Page): Promise<void> {
  const project = page.locator('#f-project');
  if ((await project.count()) === 0) return;
  await expect(project.locator(`option[value="${mainProject}"]`)).toHaveCount(1);
  await project.selectOption(mainProject);
  await expect(project).toHaveValue(mainProject);
}

/**
 * The creation form of one format and class (R-UI-080 to R-UI-088): as it opens with the class chosen, a virtual repository with
 * two members, Advanced settings open, for a local one Show as API request open, and the key's error after a wrong key was typed
 * and left. Nothing is created.
 */
function createForm(cls: 'Local' | 'Remote' | 'Virtual'): Screen['walk'] {
  return async function* (page) {
    await expect(page.locator('main h1')).toContainText(/^New .+ repository$/);
    await chooseProject(page);
    if (cls !== 'Local') await page.getByRole('radio', { name: new RegExp(`^${cls}`) }).check();
    await expect(page.locator('#f-key')).not.toHaveValue('');
    yield 'opened';
    if (cls === 'Virtual') {
      const available = page.locator('[data-testid="vm-available"] [role="option"]');
      const members = page.locator('[data-testid="vm-members"] [role="option"]');
      for (let i = 0; i < 2 && (await available.count()) > 0; i++) {
        await available.first().click();
        await page.getByRole('button', { name: 'Add', exact: true }).click();
        // The added repository leaves the list of the available ones before the next is picked.
        await expect(members).toHaveCount(i + 1);
      }
      yield 'members';
    }
    await page.getByRole('button', { name: 'Advanced settings' }).click();
    await expect(page.locator('.advanced-settings .collapse-region.open')).toHaveCount(1);
    yield 'advanced';
    if (cls === 'Local') {
      // The request as the API takes it: the form's second closed section, with the chevron of every other one (Q-UI-552).
      await page.getByRole('button', { name: 'Advanced settings' }).click();
      await page.getByRole('button', { name: 'Show as API request' }).click();
      await expect(page.locator('.api-request .collapse-region.open')).toHaveCount(1);
      yield 'api-request';
    }
    await page.locator('#f-key').fill('A');
    await page.locator('#f-key').blur();
    await expect(page.locator('#e-key')).toBeVisible();
    yield 'key-error';
  };
}

/**
 * The errors of the creation form (R-UI-086, R-UI-083): a key that a repository of the project already holds, named once the server
 * answered; then a create the server refuses with an error at an include pattern, routed in the browser, which opens the closed
 * Advanced settings and lists the field in the summary. Nothing is created.
 */
async function* createErrors(page: Page): AsyncGenerator<string> {
  await chooseProject(page);
  // The project's suggested key arrives first; a key typed before it would be typed into it.
  await expect(page.locator('#f-key')).not.toHaveValue('');
  await page.locator('#f-key').fill('npm-internal');
  await expect(page.locator('#e-key')).toHaveText('A repository with this key already exists in the project.');
  yield 'key-taken';
  await page.locator('#f-key').fill('renders-npm-refused');
  await page.getByRole('button', { name: 'Advanced settings' }).click();
  await page.locator('#f-includePatterns').fill('@acme/[');
  await page.getByRole('button', { name: 'Advanced settings' }).click();
  await page.route('**/api/v1/projects/*/repositories', (route) =>
    route.request().method() === 'POST'
      ? route.fulfill({
          status: 422,
          contentType: 'application/problem+json',
          body: JSON.stringify({
            type: 'about:blank',
            title: 'Unprocessable Entity',
            status: 422,
            code: 'validation_failed',
            detail: 'The repository document is not valid.',
            errors: [{ pointer: '/includePatterns/0', code: 'invalid_pattern', detail: "the pattern '@acme/[' has an unclosed bracket" }],
          }),
        })
      : route.fallback(),
  );
  await page.getByRole('button', { name: 'Create repository' }).click();
  await expect(page.locator('main [role="alert"]')).toBeVisible();
  await expect(page.locator('.advanced-settings .collapse-region.open')).toHaveCount(1);
  yield 'server-error';
  await page.unroute('**/api/v1/projects/*/repositories');
}

/** Eight repositories with long keys a virtual repository can take as members, answered in the browser for the picker (R-UI-081). */
function eightCandidates(): Record<string, unknown> {
  const keys = [
    'release-candidates-for-the-customer-portal-frontend',
    'shared-design-system-components-and-icons-internal',
    'payment-service-client-libraries-for-the-checkout',
    'registry-npmjs-org-cached-for-the-web-frontend-team',
    'analytics-dashboards-widgets-and-chart-primitives',
    'internationalisation-message-catalogues-and-tools',
    'feature-flags-sdk-wrappers-for-the-browser-and-node',
    'legacy-angularjs-packages-kept-for-the-old-portal',
  ];
  return {
    items: keys.map((key, i) => ({
      key,
      project: mainProject,
      class: i === 3 ? 'remote' : 'local',
      format: 'npm',
      description: '',
      visibility: { artifacts: 'private', wiki: 'private' },
      addresses: [],
      disabled: false,
      lifecycle: 'active',
      lastActivityAt: null,
    })),
    nextCursor: null,
  };
}

/** A virtual npm repository with eight members of long keys, Advanced settings closed (R-UI-081, R-UI-085). */
async function* eightMembers(page: Page): AsyncGenerator<string> {
  await chooseProject(page);
  const list = (url: URL): boolean => url.pathname === '/api/v1/repositories';
  await page.route(list, (route) =>
    route.request().method() === 'GET'
      ? route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(eightCandidates()) })
      : route.fallback(),
  );
  await page.getByRole('radio', { name: /^Virtual/ }).check();
  const available = page.locator('[data-testid="vm-available"] [role="option"]');
  const members = page.locator('[data-testid="vm-members"] [role="option"]');
  await expect(available).toHaveCount(8);
  for (let i = 0; i < 8; i++) {
    await available.first().click();
    await page.getByRole('button', { name: 'Add', exact: true }).click();
    await expect(members).toHaveCount(i + 1);
  }
  yield 'eight-members';
  await page.unroute(list);
}

/** The format chooser (R-UI-080): the tiles as they open, then a search that leaves none. */
async function* chooser(page: Page): AsyncGenerator<string> {
  yield '';
  await page.locator('#f-format-search').fill('zzz');
  await expect(page.locator('a.tile')).toHaveCount(0);
  yield 'no-match';
}

/** The answer of a create the walk refuses in the browser, and the key check of a free key. */
const refused = [
  { match: /^POST \/api\/v1\/projects\/[^/]+\/repositories answered 422$/, why: 'the create the walk refuses with a field error' },
  { match: /^GET \/api\/v1\/projects\/[^/]+\/repositories\/renders-[a-z-]+ answered 404$/, why: 'the key check: 404 is a free key' },
];

/** The states of the creation form the class walks do not reach (R-UI-086, R-UI-081). */
const createStates: Screen[] = [
  { id: 'new-npm-errors', path: `/ui/projects/${mainProject}/repositories/new/npm`, kinds: ['power'], walk: createErrors, expected: refused },
  { id: 'admin-new-npm-errors', path: '/ui/admin/repositories/new/npm', kinds: ['admin'], walk: createErrors, expected: refused },
  { id: 'new-npm-eight-members', path: `/ui/projects/${mainProject}/repositories/new/npm`, kinds: ['power'], walk: eightMembers },
  { id: 'new-npm-restricted', path: `/ui/projects/${mainProject}/repositories/new/npm`, kinds: ['power'], walk: restrictedCreate },
  { id: 'admin-new-npm-restricted', path: '/ui/admin/repositories/new/npm', kinds: ['admin'], walk: restrictedCreate },
];

/** The creation form of every format with settings of its own, in every class: its Advanced settings show them (R-UI-088). */
const createForms: Screen[] = (['oci', 'npm', 'ansible', 'nuget', 'deb'] as const).flatMap((name) =>
  (['Local', 'Remote', 'Virtual'] as const).flatMap((cls): Screen[] => {
    const id = `new-${name}-${cls.toLowerCase()}`;
    return [
      { id, path: `/ui/projects/${mainProject}/repositories/new/${name}`, kinds: ['power'], walk: createForm(cls) },
      { id: `admin-${id}`, path: `/ui/admin/repositories/new/${name}`, kinds: ['admin'], walk: createForm(cls) },
    ];
  }),
);

/** An answer of the upstream test, as the server gives it (R-REPO-024, R-REPO-064), for a state of the upstream section. */
function testAnswer(over: Record<string, unknown>): Record<string, unknown> {
  return {
    reachable: true,
    httpStatus: 401,
    authenticated: null,
    durationMs: 212,
    tls: { peerSubject: 'CN=registry.example.com', notAfter: '2027-03-01T00:00:00Z' },
    outboundProxy: null,
    sourceAddress: null,
    tokenService: null,
    error: null,
    ...over,
  };
}

/**
 * The upstream section of a new remote repository (R-UI-082) for one format: the presets as it opens, then, where `full`, a preset
 * with credentials and the line that names their hosts, a typed URL, the four kinds of test result (answered by the route, so
 * no registry is contacted), and Upstream details open with two further hosts, one of 64 characters. Nothing is created.
 */
function upstreamWalk(preset: string | null, full: boolean): Screen['walk'] {
  return async function* (page) {
    await chooseProject(page);
    await page.getByRole('radio', { name: /^Remote/ }).check();
    const upstream = page.locator('quader-upstream-settings');
    await expect(upstream).toBeVisible();
    yield 'presets';
    if (full && preset) {
      await upstream.getByRole('button', { name: preset, exact: true }).click();
      await page.locator('#f-remote-authMode').selectOption('basic');
      await page.locator('#f-remote-username').fill('ci-robot');
      await page.locator('#f-remote-password').fill('a-password-of-the-renders');
      yield 'preset-credentials';
      const answers: [string, Record<string, unknown>][] = [
        ['test-accepted', testAnswer({ authenticated: true, tokenService: { host: 'gitlab.com', allowed: true, receivesCredential: true } })],
        ['test-refused', testAnswer({ authenticated: false, tokenService: { host: 'gitlab.com', allowed: true, receivesCredential: true } })],
        [
          'test-unreachable',
          testAnswer({ reachable: false, httpStatus: null, tls: null, error: { code: 'dns_error', message: 'The name registry.gitlab.com does not resolve.' } }),
        ],
        [
          'test-token-foreign',
          testAnswer({
            tokenService: { host: 'gitlab.example-company.com', allowed: false, receivesCredential: false },
            error: {
              code: 'foreign_host_denied',
              message: "The registry's token service is on gitlab.example-company.com, which is neither the upstream nor one of the repository's further hosts; Quader did not ask it.",
            },
          }),
        ],
      ];
      for (const [state, body] of answers) {
        await page.route('**/upstream-tests', (route) => route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(body) }));
        await upstream.getByRole('button', { name: 'Test connection' }).click();
        await expect(upstream.locator('[data-testid="upstream-test-result"]')).toBeVisible();
        yield state;
        await page.unroute('**/upstream-tests');
      }
      await page.locator('#f-remote-url').fill('https://registry.example-company.com/team/');
      yield 'typed-url';
    }
    await upstream.getByRole('button', { name: 'Upstream details' }).click();
    const rows = upstream.locator('.host-row input[type="text"]');
    const n = (await rows.count()) + 2;
    await upstream.getByRole('button', { name: 'Add host' }).click();
    await upstream.getByRole('button', { name: 'Add host' }).click();
    await expect(rows).toHaveCount(n);
    await rows.nth(n - 2).fill('cdn.registry.example-company.com');
    await rows.nth(n - 1).fill('a-very-long-host-name-of-a-token-service.eu-central.example-co.com');
    yield 'details';
  };
}

/**
 * The page Upstream registries with the section of the installation's registries (R-UI-195): as it opens, scrolled to the section,
 * and, for an administrator, the dialog of **Add registry** with a further host, the row's menu and the dialog of **Edit** of the
 * long-named one.
 * Nothing is saved.
 */
async function* registryPresetsWalk(page: Page): AsyncGenerator<string> {
  const section = page.locator('quader-registry-presets');
  await expect(section.locator('tbody tr').first()).toBeVisible();
  yield 'page';
  await section.scrollIntoViewIfNeeded();
  yield 'presets';
  const add = section.getByRole('button', { name: 'Add registry' });
  if ((await add.count()) === 0) return;
  await add.click();
  const dialog = page.locator('quader-registry-preset-dialog');
  await expect(dialog).toBeVisible();
  await dialog.getByLabel('Name').fill('Artifacts of the platform team');
  await dialog.getByLabel('Upstream URL').fill('https://registry.platform.example-company.com/');
  await dialog.getByRole('button', { name: 'Add host' }).click();
  await dialog.locator('.host-row input[type="text"]').first().fill('login.platform.example-company.com');
  yield 'add-dialog';
  await dialog.getByRole('button', { name: 'Cancel' }).click();
  await section.getByRole('button', { name: 'Actions for Registry of the build farm in Frankfurt' }).click();
  yield 'row-menu';
  await page.getByRole('menuitem', { name: 'Edit' }).click();
  await expect(dialog).toBeVisible();
  yield 'edit-dialog';
  await dialog.getByRole('button', { name: 'Cancel' }).click();
}

/** The section without a registry of the installation: the list answered empty (R-UI-195). */
async function* registryPresetsEmpty(page: Page): AsyncGenerator<string> {
  await page.route('**/api/v1/settings/registry-presets', (route) =>
    route.fulfill({ status: 200, contentType: 'application/json', headers: { ETag: '"1"' }, body: JSON.stringify({ presets: [] }) }),
  );
  await page.reload();
  const section = page.locator('quader-registry-presets');
  await expect(section).toContainText('No registry of this installation yet.');
  await section.scrollIntoViewIfNeeded();
  yield 'empty';
  await page.unroute('**/api/v1/settings/registry-presets');
}

const upstreams: Screen[] = [
  { id: 'upstream-oci', path: `/ui/projects/${mainProject}/repositories/new/oci`, kinds: ['power'], walk: upstreamWalk('GitLab', true) },
  { id: 'upstream-npm', path: `/ui/projects/${mainProject}/repositories/new/npm`, kinds: ['power'], walk: upstreamWalk(null, false) },
  { id: 'upstream-deb', path: `/ui/projects/${mainProject}/repositories/new/deb`, kinds: ['power'], walk: upstreamWalk(null, false) },
  { id: 'admin-upstream-oci', path: '/ui/admin/repositories/new/oci', kinds: ['admin'], walk: upstreamWalk('GitLab', true) },
];

/**
 * The download limits and the usage in Enforce (R-UI-177, R-UI-183): the settings and the usage answers are read through the
 * page's routes with the mode Enforce. The render database is shared and the setup puts it in Audit (an instance starts in
 * Enforce), so the seed's own heavy requests and another builder's run never meet a refusal of these limits, and the main
 * pictures show what Audit records: "Would have been limited". A refused and a blocked caller are pictured in Enforce.
 */
const enforceAnswers = /\/api\/v1\/(limits\/settings|usage|users\/me\/usage|projects\/[^/]+\/usage)(\/|\?|$)/;

async function inEnforce(page: Page): Promise<void> {
  await page.route(enforceAnswers, async (route) => {
    const res = await route.fetch();
    const body = (await res.text()).replaceAll('"mode":"audit"', '"mode":"enforce"');
    await route.fulfill({ response: res, body });
  });
  await page.reload();
}

/** The answers as they are again: the next width of a walk opens its page in Audit, the database's mode. */
async function backToAudit(page: Page): Promise<void> {
  await page.unroute(enforceAnswers);
}

/** A page of the limits or the usage as it opens, then the same page in Enforce. */
async function* limitsModes(page: Page): AsyncGenerator<string> {
  yield '';
  await inEnforce(page);
  await expect(page.locator('main h1')).toBeVisible();
  yield 'enforce';
  await backToAudit(page);
}

/**
 * The rules of the instance (R-UI-177, R-UI-179): as they open; for an administrator the rule dialog with the single figures
 * opened, then the switch's dialog with the preview of the last 24 hours (R-UI-182), each cancelled; then in Enforce.
 */
async function* limitRulesWalk(page: Page): AsyncGenerator<string> {
  yield '';
  const add = page.locator('main').getByRole('button', { name: 'Add rule' });
  if ((await add.count()) > 0) {
    await add.click();
    const dialog = page.getByRole('dialog', { name: 'Add rule' });
    await expect(dialog).toBeVisible();
    await dialog.getByRole('combobox', { name: 'Profile' }).selectOption('restricted');
    await dialog.getByRole('button', { name: 'Change single figures' }).click();
    await expect(dialog.locator('input[name="cachedBytesPerHour"]')).toBeVisible();
    yield 'add-rule';
    await dialog.getByRole('button', { name: 'Cancel' }).click();
    await page.locator('main').getByRole('button', { name: 'Switch to Enforce…' }).click();
    await expect(page.getByRole('dialog', { name: 'Switch to Enforce' })).toBeVisible();
    yield 'switch';
    await page.getByRole('dialog').getByRole('button', { name: 'Cancel' }).click();
  }
  await inEnforce(page);
  await expect(page.locator('main quader-limit-mode')).toContainText('Enforce');
  yield 'enforce';
  // The switch back removes the protection: its dialog says so (Q-LIM-4).
  const toAudit = page.locator('main').getByRole('button', { name: 'Switch to Audit…' });
  if ((await toAudit.count()) > 0) {
    await toAudit.click();
    await expect(page.getByRole('dialog', { name: 'Switch to Audit' })).toBeVisible();
    yield 'to-audit';
    await page.getByRole('dialog').getByRole('button', { name: 'Cancel' }).click();
  }
  await backToAudit(page);
}

/** A list of the usage: as it opens, with its filter drawer, then in Enforce (R-UI-183). */
async function* usageWalk(page: Page): AsyncGenerator<string> {
  yield '';
  await page.locator('main .filter-toggle').first().click();
  await expect(page.getByRole('dialog', { name: 'Filters' })).toBeVisible();
  yield 'filters';
  await page.keyboard.press('Escape');
  await inEnforce(page);
  await expect(page.locator('main h1')).toBeVisible();
  yield 'enforce';
  await backToAudit(page);
}

/** A caller's usage (R-UI-184, R-UI-185): as it opens, with the explanation for a repository, the history as a table, in Enforce. */
async function* usageDetailWalk(page: Page): AsyncGenerator<string> {
  yield '';
  await page.locator('main').getByRole('button', { name: 'Explain for a repository' }).click();
  await page.locator('#explain-repository').fill(`${mainProject}/npm-internal`);
  await page.locator('main quader-limit-explain').getByRole('button', { name: 'Explain', exact: true }).click();
  await expect(page.locator('main ol.steps li').first()).toBeVisible();
  yield 'explain';
  await page.locator('main quader-usage-history').getByRole('button', { name: 'Show as table' }).click();
  yield 'table';
  await inEnforce(page);
  await expect(page.locator('main h1')).toBeVisible();
  yield 'enforce';
  await backToAudit(page);
}

/** The section **Download limits** of a settings page opened (R-UI-180), then the rule dialog of an Owner. */
async function* limitsSectionOpen(page: Page): AsyncGenerator<string> {
  yield '';
  const { section, toggle } = sectionHeaded(page, 'Download limits');
  await toggle.click();
  await expect(toggle).toHaveAttribute('aria-expanded', 'true');
  yield 'limits';
  const add = section.getByRole('button', { name: 'Add rule' });
  if ((await add.count()) === 0) return;
  await add.click();
  await expect(page.getByRole('dialog', { name: 'Add rule' })).toBeVisible();
  yield 'add-rule';
  await page.getByRole('dialog').getByRole('button', { name: 'Cancel' }).click();
}

const all: readonly Kind[] = ['plain', 'power', 'admin'];
/** A signed-out visitor's page asks who is signed in and is told nobody. */
const noSession = [{ match: /^GET \/api\/v1\/auth\/whoami answered 401$/, why: 'an anonymous visitor has no session' }];
const owners: readonly Kind[] = ['power', 'admin'];
const admins: readonly Kind[] = ['admin'];
/** Who opens the security dashboard: the instance's administrator and its auditor (R-UI-158). */
const auditors: readonly Kind[] = ['admin', 'auditor'];
const P = `/ui/projects/${mainProject}`;
const O = `/ui/projects/${otherProject}`;

/** The address of a package page (R-UI-007) and of a version page (R-UI-111, `…/<name>@<version>`). */
const pkg = (base: string, p: { readonly format: string; readonly name: string }): string => `${base}/packages/${p.format}/${p.name}`;
const ver = (base: string, p: { readonly format: string; readonly name: string }, version: string): string => `${pkg(base, p)}@${version}`;
const collectionName = `${ansiblePackage.namespace}.${ansiblePackage.name}`;

export const screens: readonly Screen[] = [
  // Without a session (Q-UI-125).
  { id: 'login', path: '/ui/login', kinds: ['anonymous'] },
  // The sign-in screen as the guard opens it for a session that has ended (R-UI-004, Q-UI-710): the notice above the providers,
  // and Continue without signing in while the seed makes a project public.
  { id: 'login-session-ended', path: '/ui/login?returnTo=%2Fprojects&ended=1', kinds: ['anonymous'] },
  { id: 'logged-out', path: '/ui/logged-out', kinds: ['anonymous'] },

  // The start and the project's content (Q-UI-129): the plain user's main path.
  { id: 'home', path: '/ui/', kinds: all },
  { id: 'projects', path: '/ui/projects', kinds: all, widths: [1100] },
  // The search field and its list on the start page; anonymous while the seed makes a project public (R-UI-095).
  { id: 'search-field', path: '/ui/projects', kinds: ['anonymous', ...all], walk: searchField, expected: tooMany },
  // The side bar's Recently opened and the project switcher read the search's store (R-UI-096, R-UI-167); an anonymous visitor's
  // start page (R-UI-095).
  { id: 'sidebar-recent', path: '/ui/projects', kinds: ['plain', 'power'], walk: frequentPlaces },
  // The auditor's home side bar: the two context titles Your work and Security audit (R-UI-168), at 390 px in the drawer.
  { id: 'sidebar-auditor', path: '/ui/projects', kinds: ['auditor'], walk: sidebarShown },
  { id: 'projects-anonymous', path: '/ui/projects', kinds: ['anonymous'], expected: tooMany },
  // The results page of the search (R-UI-098): every tab with matches, a text without a match, a tab opened by `kind`.
  { id: 'search-results', path: '/ui/search?q=web', kinds: all },
  { id: 'search-results-none', path: '/ui/search?q=zzqx', kinds: ['plain'] },
  { id: 'search-results-kind-package', path: '/ui/search?q=acme&kind=package', kinds: all },
  { id: 'repositories', path: `${P}/repositories`, kinds: all, walk: filtersOpen(repositoryChips), widths: [1100] },
  { id: 'repositories-long-keys', path: `${O}/repositories`, kinds: all, walk: filtersOpen() },
  { id: 'repositories-empty-project', path: `/ui/projects/${emptyProject}/repositories`, kinds: admins },
  // The built-in project with the registries the seed switched on: the administrator sees its two system repositories in their
  // section (R-UI-192); the plain user reads the registries that are on and no system repository, so no section.
  { id: 'repositories-upstream', path: '/ui/projects/upstream/repositories', kinds: ['admin'], walk: systemRepositories },
  { id: 'repositories-upstream-plain', path: '/ui/projects/upstream/repositories', kinds: ['plain'] },
  { id: 'repository-npm-local', path: `${P}/repositories/npm-internal`, kinds: all },
  { id: 'repository-npm-remote', path: `${P}/repositories/npmjs`, kinds: all },
  { id: 'repository-npm-virtual', path: `${P}/repositories/npm`, kinds: all },
  { id: 'repository-long-key', path: `${P}/repositories/${longNpmKey}`, kinds: all },
  { id: 'repository-generic', path: `${P}/repositories/assets`, kinds: all },
  { id: 'repository-oci', path: `${P}/repositories/images`, kinds: all, walk: rowOpened },
  // The grades of R-VULN-038: S to E in the vendored packages, N/A in the internal ones and the images (advisories.ts).
  { id: 'repository-npm-vendor', path: `${P}/repositories/npm-vendor`, kinds: all, walk: rowOpened },
  { id: 'repository-npm-vendor-by-grade', path: `${P}/repositories/npm-vendor?sort=-grade`, kinds: ['plain'] },
  { id: 'repository-empty', path: `${P}/repositories/scratch`, kinds: all },
  { id: 'repository-oci-remote', path: `${O}/repositories/docker-hub-remote`, kinds: all },
  // The owner's screen of W-23 (2026-11-19): the public remote of the guidedtraffic images, seen by a visitor. It lists only what
  // was pulled through it, which the seed cannot do (Docker Hub is not reachable for certain): in a fresh database it is the empty
  // state, and the opened rows of `repository-oci` show the same rows with data.
  { id: 'repository-oci-public-remote', path: `${O}/repositories/guidedtraffic`, kinds: ['anonymous', 'plain'], expected: noSession },
  // A remote that cached tags of an image of two platforms (W-24): a known size, a partial one and one that is not known.
  { id: 'repository-oci-cache', path: `${O}/repositories/${remoteCache.key}`, kinds: all, walk: rowOpened },
  // A remote whose tag arrived by a background download after a HEAD of its manifest (W-25, R-REPO-065): listed, the size of the two
  // platforms of its list, partial, and no client download.
  { id: 'repository-oci-probed', path: `${O}/repositories/${probedCache.key}`, kinds: all, walk: rowOpened },
  { id: 'repository-deb', path: `${O}/repositories/${longDebKey}`, kinds: all },
  { id: 'repository-nuget-virtual', path: `${O}/repositories/nuget`, kinds: all },
  { id: 'repository-ansible', path: `${O}/repositories/collections`, kinds: all },
  { id: 'packages', path: `${P}/packages`, kinds: all, walk: filtersOpen(packageChips) },
  { id: 'packages-other-project', path: `${O}/packages`, kinds: all, walk: filtersOpen() },
  // A package's page is its versions (R-UI-110), a version's page its findings, install command, files and details (R-UI-111).
  { id: 'package-npm', path: pkg(P, npmPackage), kinds: all },
  { id: 'package-npm-many', path: pkg(P, manyVersionsPackage), kinds: all, walk: versionsTable },
  { id: 'package-npm-graded', path: pkg(P, gradedPackage), kinds: all },
  { id: 'package-npm-long-name', path: pkg(P, longNamePackage), kinds: ['plain'] },
  { id: 'package-oci', path: pkg(P, ociPackage), kinds: all },
  { id: 'package-nuget', path: pkg(O, nugetPackage), kinds: all },
  { id: 'package-deb', path: pkg(O, debPackage), kinds: all },
  { id: 'package-ansible', path: pkg(O, { format: 'ansible', name: collectionName }), kinds: all },
  { id: 'package-version-npm', path: ver(P, npmPackage, '1.1.0'), kinds: all },
  { id: 'package-version-npm-graded', path: ver(P, gradedPackage, gradedPackage.version), kinds: all },
  { id: 'package-version-npm-many', path: ver(P, manyVersionsPackage, '3.7.0'), kinds: ['plain'] },
  { id: 'package-version-npm-fixed', path: ver(P, { format: 'npm', name: 'xml-stream-parser' }, '0.9.2'), kinds: ['plain'] },
  { id: 'package-version-npm-long-name', path: ver(P, longNamePackage, '0.4.0'), kinds: ['plain'] },
  // The tab's findings narrowed by a segment of the card's bar (Q-UI-379).
  { id: 'package-version-npm-graded-critical', path: `${ver(P, gradedPackage, gradedPackage.version)}?severity=critical`, kinds: ['plain'] },
  { id: 'package-version-oci', path: ver(P, ociPackage, '1.4.0'), kinds: all },
  // The image a remote cached (W-24): its versions, and the tags whose index it holds one platform of and none of, with their platforms.
  { id: 'package-oci-cache', path: pkg(O, { format: 'oci', name: remoteImage.name }), kinds: all },
  { id: 'package-version-oci-cache-partial', path: `${ver(O, { format: 'oci', name: remoteImage.name }, remoteImage.partial)}?tab=details`, kinds: all },
  { id: 'package-version-oci-cache-unknown', path: `${ver(O, { format: 'oci', name: remoteImage.name }, remoteImage.unknown)}?tab=details`, kinds: ['plain'] },
  // The tag a background download stored (W-25): its platforms, two stored and one not fetched.
  { id: 'package-version-oci-probed', path: `${ver(O, { format: 'oci', name: remoteImage.name }, probedCache.tag)}?tab=details`, kinds: all },
  { id: 'package-version-nuget', path: ver(O, nugetPackage, nugetPackage.versions[1]), kinds: all },
  { id: 'package-version-deb', path: ver(O, debPackage, debPackage.versions[1]), kinds: all },
  { id: 'package-version-ansible', path: ver(O, { format: 'ansible', name: collectionName }, ansiblePackage.versions[1]), kinds: all },
  // The folders and files of a repository on its page (R-UI-010 to R-UI-012, ADR-203): a folder with the Owner's operations, a
  // folder deep in a path of long names, a folder of more than a page, an empty folder, a file and a package format's raw files.
  { id: 'files-folder', path: `${P}/repositories/assets/files/releases/design-system/2.5.0`, kinds: all, walk: folderOperations },
  { id: 'files-deep', path: `${P}/repositories/assets/files/${deepFolder}`, kinds: all, walk: deepPath },
  { id: 'files-many', path: `${P}/repositories/assets/files/icons/material`, kinds: ['plain'], walk: manyEntries },
  { id: 'files-empty-folder', path: `${P}/repositories/assets/files/drafts`, kinds: all },
  { id: 'files-file', path: `${P}/repositories/assets/files/README.md`, kinds: all, walk: fileView },
  { id: 'files-file-long', path: `${P}/repositories/assets/files/${deepFolder}/sprite-with-every-icon-of-the-design-system-for-the-customer-portal.svg`, kinds: ['plain'] },
  { id: 'files-npm', path: `${P}/repositories/npm-internal/files/@acme/ui-kit/-`, kinds: all },
  { id: 'files-npm-file', path: `${P}/repositories/npm-internal/files/@acme/ui-kit/-/ui-kit-1.1.0.tgz`, kinds: owners },
  { id: 'files-oci', path: `${P}/repositories/images/files`, kinds: ['plain'] },
  // A file of a system repository (R-UI-193): what only an administrator reaches.
  {
    id: 'files-system',
    path: `/ui/projects/upstream/repositories/scanner-clamav/files/${systemFile}`,
    kinds: admins,
    walk: systemFileView,
    expected: [
      {
        match: /^GET \/api\/v1\/projects\/upstream\/repositories\/scanner-clamav\/items\?path=\S+ answered 404$/,
        why: 'the profile stores no signature database: the first load reads the file before the walk routes its answer',
      },
    ],
  },
  // The malware scan's fact of a file and a version (R-MAL-024): what a plain user meets; the same for every kind.
  {
    id: 'files-malware',
    path: `${P}/repositories/assets/files/${quarantine}/customer-portal-installer-with-a-long-name-for-the-windows-build-agents.exe`,
    kinds: ['plain'],
    walk: malwareFiles,
  },
  { id: 'package-version-malware', path: `${ver(P, malwarePackage, '2.3.0')}?tab=files`, kinds: ['plain'], walk: malwareVersions },
  { id: 'vulnerabilities', path: `${P}/vulnerabilities`, kinds: all, walk: filtersOpen(findingChips) },
  // A filter is a chip beside Filters; the list sorts by Detected at every width, at 390 px from the sort row above its stacked rows (Q-UI-391).
  { id: 'vulnerabilities-filtered', path: `${P}/vulnerabilities?severity=high`, kinds: admins, walk: sortByDetection },
  { id: 'members', path: `${P}/members`, kinds: all },
  // The security dashboard under Security audit (R-UI-158, R-UI-159): its title, the line of the scope, the counts, the trend, then
  // the list with its one search; the drawer with the projects; the chips; a text in the search; a finding opened from it.
  { id: 'security', path: '/ui/security', kinds: auditors, walk: filtersOpen(securityChips) },
  { id: 'security-search', path: '/ui/security', kinds: ['auditor'], walk: searched('DEMO-2026-00001') },
  { id: 'security-project', path: `/ui/security?project=${mainProject}`, kinds: ['auditor'] },
  // The dashboard's malware view (R-MAL-025): what the auditor assesses and releases; an administrator sees the same.
  { id: 'security-malware', path: '/ui/security/malware', kinds: ['auditor'], walk: malwareFinds },
  // Set up client of a repository (R-UI-120): every format and class of the seed, the long keys, and a public repository signed out.
  { id: 'setup-npm-local', path: `${P}/repositories/npm-internal/setup`, kinds: all },
  { id: 'setup-npm-remote', path: `${P}/repositories/npmjs/setup`, kinds: all },
  { id: 'setup-npm-virtual', path: `${P}/repositories/npm/setup`, kinds: all, walk: setupWalk },
  { id: 'setup-long-key', path: `${P}/repositories/${longNpmKey}/setup`, kinds: all },
  { id: 'setup-generic', path: `${P}/repositories/assets/setup`, kinds: all },
  { id: 'setup-oci-local', path: `${P}/repositories/images/setup`, kinds: all },
  { id: 'setup-oci-remote', path: `${O}/repositories/docker-hub-remote/setup`, kinds: all },
  { id: 'setup-nuget', path: `${O}/repositories/nuget/setup`, kinds: all },
  { id: 'setup-deb', path: `${O}/repositories/${longDebKey}/setup`, kinds: all, walk: explained },
  { id: 'setup-ansible-local', path: `${O}/repositories/collections/setup`, kinds: ['anonymous', ...all], expected: noSession },
  { id: 'setup-ansible-remote', path: `${O}/repositories/galaxy/setup`, kinds: all },
  { id: 'setup-ansible-virtual', path: `${O}/repositories/ansible/setup`, kinds: all },
  // The top bar's search ends on a page of the main area, never in the admin area (Q-UI-390); the field shows the text.
  { id: 'search', path: '/ui/search?q=npm', kinds: all },
  // The registries of upstream and its two system repositories, each of these with the badge System (R-UI-193, Q-UI-902).
  { id: 'search-system', path: '/ui/search?q=upstream', kinds: admins },
  { id: 'search-field-system', path: '/ui/projects', kinds: admins, walk: systemSuggestions },
  { id: 'search-none', path: '/ui/search?q=no-such-repository', kinds: ['admin'] },
  // Guides (R-UI-124): the formats as tiles, the repositories of a format, the guide of a class without a repository.
  { id: 'guides', path: '/ui/guides', kinds: ['anonymous', ...all], expected: noSession },
  { id: 'guides-format', path: '/ui/guides/npm', kinds: all },
  // The trail names the format as people read it: Guides / OCI (Docker), not its key (Q-UI-553).
  { id: 'guides-format-oci', path: '/ui/guides/oci', kinds: ['plain'] },
  { id: 'guides-format-anonymous', path: '/ui/guides/ansible', kinds: ['anonymous'], expected: noSession },
  { id: 'guides-class', path: '/ui/guides/npm/local', kinds: ['plain', 'power'], walk: explained },
  { id: 'help', path: '/ui/help', kinds: all },
  { id: 'help-user-page', path: '/ui/help/user/getting-started', kinds: all },
  { id: 'profile', path: '/ui/profile', kinds: all },
  { id: 'profile-tokens', path: '/ui/profile/tokens', kinds: all },
  // The plain user holds a token (setup.ts): its revoke-all, last on the page behind Advanced.
  { id: 'profile-tokens-advanced', path: '/ui/profile/tokens', kinds: ['plain'], walk: advancedOpen },
  { id: 'profile-sessions', path: '/ui/profile/sessions', kinds: all },
  // A user's own usage and limits (R-UI-185).
  { id: 'profile-usage', path: '/ui/profile/usage', kinds: all, walk: usageDetailWalk },
  { id: 'not-found', path: '/ui/no-such-page', kinds: all },

  // What an Owner of the project maintains, and the settings a reader is offered in the list.
  // The settings page of each class, standard sections open and every detail opened in a second picture (R-UI-087).
  { id: 'repository-settings-npm-local', path: `${P}/repositories/npm-internal/settings`, kinds: all, walk: allSections },
  { id: 'repository-settings-npm-remote', path: `${P}/repositories/npmjs/settings`, kinds: all, walk: remoteSections },
  { id: 'repository-settings-npm-virtual', path: `${P}/repositories/npm/settings`, kinds: all, walk: allSections },
  { id: 'repository-settings-generic', path: `${P}/repositories/assets/settings`, kinds: owners, walk: allSections },
  { id: 'repository-settings-generic-advanced', path: `${P}/repositories/assets/settings`, kinds: admins, walk: advancedOpen },
  { id: 'repository-settings-oci', path: `${P}/repositories/images/settings`, kinds: all, walk: allSections },
  { id: 'repository-new', path: `${P}/repositories/new`, kinds: owners, walk: chooser },
  { id: 'policies', path: `${P}/policies`, kinds: owners },
  { id: 'policy-new', path: `${P}/policies/new`, kinds: owners },
  { id: 'project-service-accounts', path: `${P}/service-accounts`, kinds: owners },
  { id: 'project-service-account', path: `${P}/service-accounts/${projectServiceAccount}`, kinds: owners, walk: technicalTokens },
  // A repository's service account and the section of the repository's settings that lists them (R-UI-181).
  { id: 'repository-service-account', path: `${P}/repositories/npm-internal/service-accounts/${repositoryServiceAccount}`, kinds: owners, walk: technicalTokens },
  { id: 'repository-settings-service-accounts', path: `${P}/repositories/npm-internal/settings`, kinds: owners, walk: repositoryAccounts },
  { id: 'project-ci', path: `${P}/ci`, kinds: owners },
  // A project's mapping editor without an issuer in the address: the issuer select and the policy picker (Q-UI-334).
  { id: 'project-ci-mapping-new', path: `${P}/ci/mappings/new`, kinds: owners },
  { id: 'project-settings', path: `${P}/settings`, kinds: owners, walk: visibilityOpen },
  // The download limits of a project and of a remote repository, and the project's usage (R-UI-180, R-UI-183, R-UI-184).
  // The main project has no maximum: its read answers 200 with `maximum` null (Q-API-356).
  { id: 'project-settings-limits', path: `${P}/settings`, kinds: owners, walk: limitsSectionOpen },
  { id: 'repository-settings-limits', path: `${P}/repositories/npmjs/settings`, kinds: owners, walk: limitsSectionOpen },
  { id: 'project-usage', path: `${P}/usage`, kinds: owners, walk: usageWalk },
  { id: 'project-usage-repository', path: `${P}/usage?repository=npmjs`, kinds: owners, walk: limitsModes },
  { id: 'project-usage-caller', path: `${P}/usage/user/${kinds.power}`, kinds: owners, walk: usageDetailWalk },
  { id: 'repository-settings-restricted', path: `${P}/repositories/npm-internal/settings`, kinds: owners, walk: restrictedSettings },
  // The strict repository's section Malware scan (R-MAL-020): its Owner may not switch it off, an administrator may.
  { id: 'repository-settings-malware', path: `${P}/repositories/${malwarePackage.repo}/settings`, kinds: owners, walk: malwareSection },
  // The one state of the visibility section that no stored repository shows: the server's answer under each level's group.
  {
    id: 'repository-settings-visibility-refused',
    path: `${P}/repositories/npm-internal/settings`,
    kinds: ['power'],
    walk: visibilityRefused,
    expected: [{ match: /^PATCH \/api\/v1\/projects\/[^/]+\/repositories\/npm-internal answered 422$/, why: 'the save the walk refuses with a field problem at each level' }],
  },

  // The administrator's area (Q-UI-127).
  { id: 'repository-settings-oci-remote', path: `${O}/repositories/docker-hub-remote/settings`, kinds: all, walk: remoteSections },
  { id: 'repository-settings-oci-virtual', path: `${O}/repositories/containers/settings`, kinds: admins, walk: allSections },
  { id: 'repository-settings-deb', path: `${O}/repositories/${longDebKey}/settings`, kinds: admins, walk: allSections },
  { id: 'repository-settings-nuget-remote', path: `${O}/repositories/nuget-org/settings`, kinds: admins, walk: remoteSections },
  { id: 'repository-settings-deb-remote', path: `${O}/repositories/debian/settings`, kinds: admins, walk: remoteSections },
  { id: 'repository-settings-ansible', path: `${O}/repositories/collections/settings`, kinds: all, walk: allSections },
  { id: 'repository-settings-ansible-remote', path: `${O}/repositories/galaxy/settings`, kinds: admins, walk: remoteSections },
  { id: 'repository-settings-ansible-virtual', path: `${O}/repositories/ansible/settings`, kinds: admins, walk: allSections },
  { id: 'help-admin-page', path: '/ui/help/admin/repositories', kinds: admins },
  { id: 'admin-dashboard', path: '/ui/admin/dashboard', kinds: admins },
  { id: 'admin-projects', path: '/ui/admin/projects', kinds: admins, walk: searched('web') },
  { id: 'admin-repositories', path: '/ui/admin/repositories', kinds: admins, walk: filtersOpen(), widths: [1100, 1280] },
  // The rows of upstream: the system repositories with their badge and without the copy button (R-UI-193).
  { id: 'admin-repositories-upstream', path: '/ui/admin/repositories?project=upstream', kinds: admins, walk: systemRows },
  // The public registries Quader offers (R-UI-187, R-UI-188): the list grouped by format, and a Debian archive's page, the one with
  // the most to lay out (its archive keys). The member editor's placeholder is in repository-settings-oci-virtual.
  { id: 'admin-upstream', path: '/ui/admin/upstream', kinds: admins, walk: registryPresetsWalk },
  // The section of the installation's registries without any (R-UI-195): the list answered empty by the route.
  { id: 'admin-upstream-presets-empty', path: '/ui/admin/upstream', kinds: ['admin'], walk: registryPresetsEmpty },
  { id: 'admin-upstream-registry', path: '/ui/admin/upstream/debian', kinds: admins },
  // The system repository of the image scanning's database: in use while the scanning reads through it (R-REPO-070, R-VULN-044).
  { id: 'admin-upstream-scanner-trivy', path: '/ui/admin/upstream/scanner-trivy', kinds: admins },
  { id: 'admin-repository-new', path: '/ui/admin/repositories/new', kinds: admins, walk: chooser },
  { id: 'admin-users', path: '/ui/admin/users', kinds: admins, walk: filtersOpen(userChips) },
  { id: 'admin-user', path: '/ui/admin/users/sam', kinds: admins },
  // The break-glass account: no Advanced action, so the page asks the token list whether it offers Revoke all.
  { id: 'admin-user-break-glass', path: '/ui/admin/users/admin', kinds: admins },
  { id: 'admin-user-advanced', path: '/ui/admin/users/sam', kinds: admins, walk: advancedOpen },
  { id: 'admin-groups', path: '/ui/admin/groups', kinds: admins, walk: filtersOpen() },
  { id: 'admin-group', path: `/ui/admin/groups/${group}`, kinds: admins, walk: principalSearch('Add a member', 'ad') },
  { id: 'admin-service-accounts', path: '/ui/admin/service-accounts', kinds: admins, walk: searched('release') },
  { id: 'admin-service-account', path: `/ui/admin/service-accounts/${serviceAccount}`, kinds: admins, walk: technicalTokens },
  { id: 'admin-permissions', path: '/ui/admin/permissions', kinds: admins, walk: filtersOpen() },
  { id: 'admin-permission-new', path: '/ui/admin/permissions/new', kinds: admins },
  { id: 'admin-roles', path: '/ui/admin/roles', kinds: admins, walk: principalSearch('Bind to', 'sa') },
  { id: 'admin-tokens', path: '/ui/admin/tokens', kinds: admins, walk: filtersOpen() },
  // The usage and the download limits (R-UI-177 to R-UI-184): the administrator changes, the auditor reads.
  { id: 'admin-usage', path: '/ui/admin/usage', kinds: auditors, walk: usageWalk },
  { id: 'admin-usage-caller', path: `/ui/admin/usage/user/${kinds.power}`, kinds: auditors, walk: usageDetailWalk },
  // A blocked anonymous address and a CI project as callers (R-UI-184): the block's notice, a subject without credentials.
  { id: 'admin-usage-caller-blocked', path: `/ui/admin/usage/anonymous/${blockedAddress}`, kinds: admins, walk: limitsModes },
  { id: 'admin-usage-caller-ci', path: `/ui/admin/usage/ci/${ciIssuer}:${ciProject.id}`, kinds: admins },
  { id: 'admin-limits', path: '/ui/admin/limits', kinds: auditors, walk: limitRulesWalk },
  { id: 'admin-limits-profiles', path: '/ui/admin/limits/profiles', kinds: auditors },
  { id: 'admin-limits-profile', path: '/ui/admin/limits/profiles/standard', kinds: auditors },
  { id: 'admin-limits-profile-unlimited', path: '/ui/admin/limits/profiles/unlimited', kinds: admins },
  { id: 'admin-limits-profile-new', path: '/ui/admin/limits/profiles/new', kinds: admins },
  { id: 'admin-limits-projects', path: '/ui/admin/limits/projects', kinds: auditors },
  { id: 'admin-limits-blocks', path: '/ui/admin/limits/blocks', kinds: auditors },
  { id: 'admin-ci', path: '/ui/admin/ci', kinds: admins },
  { id: 'admin-ci-new', path: '/ui/admin/ci/new', kinds: admins },
  { id: 'admin-ci-issuances', path: '/ui/admin/ci/issuances', kinds: admins, walk: filtersOpen() },
  { id: 'admin-bastions', path: '/ui/admin/bastions', kinds: admins },
  { id: 'admin-endpoints', path: '/ui/admin/endpoints', kinds: admins },
  // The endpoint every installation has (R-EP-001).
  { id: 'admin-endpoint', path: '/ui/admin/endpoints/default', kinds: admins },
  { id: 'admin-endpoint-rule-new', path: '/ui/admin/endpoints/default/rules/new', kinds: admins },
  { id: 'admin-endpoint-rule', path: `/ui/admin/endpoints/default/rules/${exposureRule}`, kinds: admins, walk: advancedOpen },

  // The creation form of every format with settings in every class (the owner's rules of 2026-11-12, CLAUDE.md).
  ...createForms,
  ...createStates,
  // The upstream of a remote where it is entered: the presets, the credentials, Test connection, the details (R-UI-082).
  ...upstreams,
  { id: 'admin-audit', path: '/ui/admin/audit', kinds: admins, walk: filtersOpen() },
  { id: 'admin-advisory-sources', path: '/ui/admin/advisory-sources', kinds: admins, walk: databaseCommandsOpen },
  // The auditor reads the sources and the image scanners (settings:read, R-OPS-048).
  { id: 'admin-advisory-sources-read', path: '/ui/admin/advisory-sources', kinds: ['auditor'] },
  { id: 'admin-system', path: '/ui/admin/system', kinds: admins, walk: systemScanners },
  // The auditor reads the replicas and the scanner replicas (settings:read, R-OPS-048).
  { id: 'admin-system-read', path: '/ui/admin/system', kinds: ['auditor'] },
  { id: 'admin-system-jobs', path: '/ui/admin/system/jobs', kinds: admins },
  { id: 'admin-system-backups', path: '/ui/admin/system/backups', kinds: admins },
  { id: 'admin-settings-auth', path: '/ui/admin/settings/auth', kinds: admins },
  { id: 'admin-settings-general', path: '/ui/admin/settings/general', kinds: admins, walk: restrictedLevels },
  // The malware scanning's page (R-MAL-023): every state of the administrator's, and the auditor's read-only page with the link to the finds.
  { id: 'admin-settings-malware', path: '/ui/admin/settings/malware', kinds: admins, walk: malwareSettings },
  { id: 'admin-settings-malware-read', path: '/ui/admin/settings/malware', kinds: ['auditor'] },
];
