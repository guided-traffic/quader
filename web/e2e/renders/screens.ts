import { Locator, Page, Route, expect } from '@playwright/test';
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
  longProject,
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
  certificateSubject,
  ciIssuer,
  ciProject,
} from './seed';
import { gradedPackage, manyVersionsPackage } from './advisories';
import { malwarePackage, quarantine } from './malware';
import { malwareWaiting, scanFailedCard, scanStatesList, scanStatesPackage } from './scan-states';
import { trivyDatabases } from './scanners';
import { signedFile, signedImage, signedVersions } from './signatures';
import { issuedFile, issuedImage, issuedVersion } from './receipts';
import { signingAnswer } from './signing';
import { withVerify } from './verify';
import { routeTooMany } from './suggestions';
import { clamavDatabases, clamavRegistry, clamavRow, definitionsExpected, definitionsWith, switchState } from './clamav';
import { mirrorFilled } from './mirror';
import { damagedBlobs, noDamagedBlob, oneConnection, secondUnreachable, severalConnections, twoConnectionFacts } from './storage';
import { auditDataKeys, bastionsHandover, bastionsRead, forbiddenLink, seededBastion } from './bastions';
import { downloadsWalk } from './downloads';
import { accountOfferWalk, registryAccountWalk } from './upstream-accounts';
import {
  advisorySourceFailures,
  claimTesterFailure,
  issuerKeysFailure,
  issuerListFailure,
  jobsFailures,
  malwareDatabaseFailure,
  mirrorFailure,
  registryTestFailure,
  registryTests,
  remoteSettingsFailures,
  signatureNote,
  signatureRootFailure,
  valkeyDegraded,
} from './server-texts';
import { downloadsButtonWalk, downloadsExpected, downloadsPanelWalk } from './upstream-downloads';
import { failureExpected, failureNotices, failureStates } from './failures';
import {
  adminLists,
  debianKeys,
  editorPaths,
  everySection,
  findingAndAdvisory,
  issuerStates,
  loginNotice,
  mappingStates,
  mirrorRunMenu,
  openedAndSections,
  ownerMenus,
  policyStates,
  ruleRefused,
  securityFinding,
  shellMenus,
  shellNotices,
  upstreamRequestMenu,
  upstreamVersionMenu,
  withStates,
} from './states';
import {
  upstreamDebWalk,
  upstreamExpected,
  upstreamLookupWalk,
  upstreamOwnWalk,
  upstreamTabWalk,
  upstreamUnknownLevelWalk,
  upstreamVersions,
} from './upstream';

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
  /** Error notices that are the state the walk renders (it answered the failure in the browser), each with its reason. */
  readonly notices?: readonly { readonly match: RegExp; readonly why: string }[];
  /**
   * The lists of the screen that keep the form of a table from 1,024 px on, whatever they hold (Q-UI-969): the host element of each
   * as a selector inside `main`. One that stands as blocks there is a finding `stacked`.
   */
  readonly tables?: string;
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
  /**
   * Where a state of the walk is taken, by the name the walk yields: at the widths of `widths` only, and its dark picture at those of
   * `dark` only (the light one is taken wherever the state is, since the checks measure it). A state that an overlay alone changes (a
   * menu, a drawer, a dialog) shows at 1,024 px nothing that 1,440 and 390 do not. Without an entry: every width, both schemes.
   */
  readonly states?: Readonly<Record<string, { readonly widths?: readonly number[]; readonly dark?: readonly number[] }>>;
  /**
   * The run's widths this screen is loaded at, where none of its states differs at the others (Q-UI-1231): a screen whose every state
   * is an overlay (a menu, a drawer) is not loaded and walked at 1,024 px for no picture. Without it: every width of the run.
   */
  readonly only?: readonly number[];
  /**
   * Rendered in the German pass too (UI_RENDERS_LOCALE=de, Q-UI-1048), into `.out/ui-renders-de`: a screen whose German words were
   * not looked at otherwise. Its walk finds its controls by their place (a selector, a test id, an ID), never by an English word.
   * A list of kinds renders it as those kinds only in the German pass, where one kind shows what the pass is for (Q-UI-1090).
   */
  readonly de?: boolean | readonly Kind[];
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

/**
 * A list with its first row's menu open (G5-20, R-UI-163): the menu of **Actions for <name>** over the list, which no other state shows,
 * so that the layout checks see its entries and the row it stands beside. One state per list that has a row menu; nothing is chosen.
 */
async function* rowMenuOpen(page: Page): AsyncGenerator<string> {
  const trigger = page.locator('main td.actions button[aria-haspopup="menu"]').first();
  await trigger.click();
  await expect(page.locator('.cdk-overlay-container [role="menu"]')).toBeVisible();
  yield 'menu';
}

/**
 * A tooltip open (Q-UI-1065): the first row's **Rotate**, focused as the keyboard focuses it, so that the bubble, its arrow over the
 * button and its colours are in the picture of each scheme; at 390 px the window's edge pushes the bubble off the button's middle and
 * the arrow keeps pointing at it. Found by its place, so the German pass takes it too. Nothing is chosen.
 */
async function* tooltipOpen(page: Page): AsyncGenerator<string> {
  await page.locator('main td.actions button[aria-describedby]').first().focus();
  await expect(page.locator('.cdk-overlay-container quader-tooltip')).toBeVisible();
  yield 'tooltip';
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
 * A repository of images with the image `name` opened in its row (R-UI-052, Q-UI-1130): its newest images, a row each with its leading
 * tag, its other tags and its signature badge. `closed` renders the list before it is opened as well.
 */
function imageOpened(name: string, closed: boolean): (page: Page) => AsyncGenerator<string> {
  return async function* (page: Page): AsyncGenerator<string> {
    if (closed) yield '';
    await page.locator('main tbody tr', { has: page.locator('.name-cell a', { hasText: new RegExp(`^${name}$`) }) }).locator('.name-cell button[aria-expanded="false"]').click();
    await expect(page.locator('main .collapse-region.open quader-repository-item-versions quader-image-tags').first()).toBeVisible();
    yield 'opened';
  };
}

/** An image's page (R-UI-110, Q-UI-1130): its images with every tag, then narrowed to those with a tag that holds "1.1". */
async function* imageTagsFiltered(page: Page): AsyncGenerator<string> {
  yield '';
  // Found by its place, not its English words: the German pass walks it too.
  await page.locator('main .versions-filter input').fill('1.1');
  await expect(page.locator('main .pkg-versions tbody tr')).toHaveCount(3);
  yield 'filtered';
}

/**
 * The menu of a version's commands (Q-UI-1066): the first item opened, then the button of its first version clicked, so that the panel
 * hangs from it with a section and a command per client of the format.
 */
async function* commandMenuOpen(page: Page): AsyncGenerator<string> {
  await page.locator('main .name-cell button[aria-expanded="false"]').first().click();
  const versions = page.locator('main .collapse-region.open quader-repository-item-versions');
  await expect(versions.locator('table.data tbody tr').first()).toBeVisible();
  await versions.locator('tbody quader-client-command-menu button').first().click();
  const panel = page.locator('.client-command-panel');
  await expect(panel.locator('quader-address-field input, quader-code-window').first()).toBeVisible();
  yield 'menu';
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

/** The repositories of every project with the project chosen in the drawer (Q-UI-1042). */
const projectFilter = async (drawer: Locator): Promise<void> => {
  await drawer.getByRole('combobox', { name: 'Project' }).selectOption(mainProject);
};

/**
 * The start page as it opens, then with **Show Owners and storage** ticked (Q-UI-1041), which the browser keeps for the viewer; seen
 * by the administrator and the auditor, who are members of no project.
 */
async function* projectDetails(page: Page): AsyncGenerator<string> {
  const box = page.getByRole('checkbox', { name: 'Show Owners and storage' });
  if (await box.isChecked()) await box.uncheck();
  await expect(page.locator('main thead th', { hasText: 'Owners' })).toHaveCount(0);
  // The administrator and the auditor are members of no project: **Your role** is the dash in every row (Q-UI-561), the state whose
  // empty cells the layout check `empty-cell` names (2026-11-23).
  const roleColumn = (await page.locator('main thead th').allTextContents()).findIndex((t) => t.includes('Your role')) + 1;
  // Attached, not visible: where the rows stack (1,024 and 390 px) a field without a value leaves its line out.
  await expect(page.locator(`main tbody tr > :nth-child(${roleColumn}) quader-no-value`).first()).toBeAttached();
  yield '';
  await box.check();
  await expect(page.locator('main thead th', { hasText: 'Owners' })).toHaveCount(1);
  yield 'details';
  await box.uncheck();
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

/** The audit log filtered by a resource, its kind and its identifier (Q-UI-998): the events of the advisory sync's task. */
const auditResource = async (drawer: Locator): Promise<void> => {
  await drawer.getByLabel('Resource kind', { exact: true }).fill('task');
  await drawer.getByLabel('Resource ID', { exact: true }).fill('vuln-sync-osv');
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

/** A version's details of the ClamAV definitions with the fold of the archive's checksums open (R-UI-194). */
async function openChecksums(page: Page): Promise<void> {
  await page.getByRole('button', { name: /^Checksums of the archive/ }).click();
  await expect(page.getByText('MD5 computed by Quader')).toBeVisible();
}

/** The file of `upstream/clamav` the file page of a system repository shows: a signature database as the task stores it. */
const systemFile = 'main/63/main.cvd';

/**
 * The page of a file of a system repository (R-UI-193, Q-UI-898, Q-UI-899): no Download, no download address and no download with
 * curl, one sentence in the address's place; as it opens and with Details open. The profile runs no malware scanning, so
 * `clamav` holds nothing: the item and its statistics are the answers the server gives for a stored version, routed in the
 * browser; the repository, the session's checks and everything else are the server's.
 */
async function* systemFileView(page: Page): AsyncGenerator<string> {
  const base = '**/api/v1/projects/upstream/repositories/clamav';
  // Relative to the render's clock: the page says how long ago, and a fixed date grew from "3 days ago" to months with the calendar.
  const at = new Date(Date.now() - 3 * 86_400_000).toISOString().replace(/\.\d{3}Z$/, 'Z');
  const item = {
    repository: 'upstream/clamav',
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
    downloadUrl: `https://localhost:4200/files/upstream/clamav/${systemFile}`,
    contentUrl: `/api/v1/projects/upstream/repositories/clamav/content/${systemFile}`,
    itemUrl: `https://localhost:4200/api/v1/projects/upstream/repositories/clamav/items?path=${encodeURIComponent(systemFile)}`,
  };
  await page.route(`${base}/items?**`, (route) => route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(item) }));
  await page.route(`${base}/stats?**`, (route) =>
    route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ downloads: 0, lastDownloadAt: null, lastDownloader: null }) }),
  );
  // The seed leaves ClamAV definition updates on; this page is the one of a repository that answers no client (Q-UI-908).
  await switchState(page, 'off');
  await expect(page.locator('[data-testid="file-system"]')).toBeVisible();
  yield '';
  await page.getByRole('button', { name: /^Details/ }).click();
  await expect(page.getByText('Counts reach the database within a minute of a download.')).toBeVisible();
  yield 'details';
  await page.unroute(`${base}/items?**`);
  await page.unroute(`${base}/stats?**`);
}

/**
 * The repositories of the project upstream on the list of every project (R-UI-193, Q-UI-896, Q-UI-902, Q-UI-1042): every registry
 * with Set up client and its menu, the two system repositories marked **System** (scanner-trivy's badge, clamav's class) and the menu
 * alone; then the menu of
 * `scanner-trivy`.
 */
async function* systemRows(page: Page): AsyncGenerator<string> {
  // scanner-trivy by the badge after its key, clamav by its class cell, a system-only format's one mark (Q-UI-1203).
  await expect(page.locator('main tbody th[scope="row"] .badge', { hasText: /^System$/ })).toHaveCount(1);
  await expect(page.locator('main tbody td.class-cell', { hasText: /^\s*settings\s*System\s*$/ })).toHaveCount(1);
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
 * The advisory sources as they open; with the fold of the image scanners' replicas open (R-OPS-048, Q-UI-897), as seeded (no pod holds
 * a database yet), with the pods in each state of the database they hold and with every pod on the database in use (Q-UI-915); where the
 * image database comes from a local repository, with the fold of the commands that fill it open (R-VULN-045, Q-UI-890), which the
 * database's default repository, a remote, does not have; and without a replica, the profile's own answer: its role all runs no image
 * scanner.
 */
async function* databaseCommandsOpen(page: Page): AsyncGenerator<string> {
  yield '';
  const scanners = page.locator('main [data-testid="image-scanners"] quader-disclosure button.inline-toggle');
  const openScanners = async (): Promise<void> => {
    await expect(scanners).toHaveAttribute('aria-expanded', 'false');
    await scanners.click();
    await expect(scanners).toHaveAttribute('aria-expanded', 'true');
  };
  await openScanners();
  yield 'scanners';
  const url = '**/api/v1/system/scanners';
  await answerWith(page, url, trivyDatabases('mixed'));
  await openScanners();
  await expect(page.locator('main [data-testid="image-scanners"] td.database-cell')).toHaveCount(4);
  yield 'scanners-databases';
  await answerWith(page, url, trivyDatabases('current'));
  await expect(page.locator('main [data-testid="image-scanners-summary"]')).toContainText('all on the database in use');
  await openScanners();
  yield 'scanners-current';
  await page.unroute(url);
  await page.reload();
  await page.waitForLoadState('networkidle');
  const closed = page.locator('main quader-vulnerability-sources > quader-disclosure button[aria-expanded="false"]');
  if ((await closed.count()) > 0) {
    await closed.first().click();
    await expect(page.locator('main quader-vulnerability-sources > quader-disclosure button[aria-expanded="true"]')).toHaveCount(1);
    yield 'commands';
  }
  await answerWith(page, url, (b) => ({ ...b, items: [] }));
  await expect(page.locator('main [data-testid="scanners-none-trivy"]')).toBeVisible();
  yield 'scanners-none';
  await page.unroute(url);
}

/**
 * The System page's scanner replicas (R-OPS-048, Q-UI-897): as seeded (both kinds, image scanners that hold no database yet, a malware
 * scanner that waits for its first database with the longest pod name); the image scanners in each state of the database they hold, and
 * every one on the database in use (Q-UI-915); no replica of either kind reported though both report, which the header warns of; and the
 * profile's own answer without rows: its role all runs no scanner pod and says so.
 */
async function* systemScanners(page: Page): AsyncGenerator<string> {
  // The image scanners, the malware scanners and the malware scanning's updater (Q-UI-913).
  await expect(page.locator('main [data-testid="scanner-replicas"] table')).toHaveCount(3);
  yield '';
  const url = '**/api/v1/system/scanners';
  await answerWith(page, url, trivyDatabases('mixed'));
  await expect(page.locator('main [data-testid="scanner-database-lag"]')).toBeVisible();
  yield 'scanners-databases';
  await answerWith(page, url, trivyDatabases('current'));
  await expect(page.locator('main [data-testid="scanner-replicas"] .database-state')).toHaveCount(2);
  await expect(page.locator('main [data-testid="scanner-database-lag"]')).toHaveCount(0);
  yield 'scanners-current';
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
  // Two replicas lost without draining (G4-08): the header's statement with their names and the moment of the last heartbeat, which
  // the profile's one replica never shows.
  const replicas = '**/api/v1/system/replicas';
  const lostAt = new Date(Date.now() - 47 * 60_000).toISOString();
  await answerWith(page, replicas, (b) => {
    const items = (b['items'] as Record<string, unknown>[]) ?? [];
    const first = items[0] ?? {};
    const lost = (name: string): Record<string, unknown> => ({ ...first, name, lost: true, state: 'ready', heartbeatAt: lostAt });
    return { ...b, items: [...items, lost('quader-worker-7f9c6d5b8-x2v4q'), lost('quader-api-6d8b9c7f5-k3m8n')] };
  });
  await expect(page.locator('main .condition').filter({ hasText: 'quader-worker-7f9c6d5b8-x2v4q' })).toBeVisible();
  yield 'replicas-lost';
  await page.unroute(replicas);
}

/**
 * The tab **Storage** of System status (R-UI-196): as the render profile answers it (one connection, perhaps before the first storage
 * summary), then answered in the browser (storage.ts): one connection with its summary and its fold open; several connections in every
 * state with blobs no connection holds whole; the same with two rows' folds and both sections of details open.
 */
async function* systemStorage(page: Page): AsyncGenerator<string> {
  const connections = '**/api/v1/system/storage-connections';
  const damaged = '**/api/v1/storage/damaged-blobs**';
  const tab = page.locator('main quader-storage-page');
  await expect(tab.locator('table').first()).toBeVisible();
  yield '';
  await routeWith(page, damaged, noDamagedBlob());
  await answerWith(page, connections, oneConnection());
  await expect(tab.locator('quader-data-table').first().locator('tbody tr:not(.expansion)')).toHaveCount(1);
  await tab.locator('th button[aria-expanded="false"]').first().click();
  await expect(tab.locator('th button[aria-expanded="true"]')).toHaveCount(1);
  yield 'one';
  await routeWith(page, damaged, damagedBlobs());
  await answerWith(page, connections, severalConnections());
  await expect(tab.locator('quader-data-table').first().locator('tbody tr:not(.expansion)')).toHaveCount(6);
  yield 'several';
  for (const i of [1, 2]) await tab.locator('quader-data-table').first().locator('th button').nth(i).click();
  // The page's two sections, Damaged blobs and the tasks' last runs, by what a click does not change: a list of the closed toggles
  // shrank under the loop, and the second click waited 15 s for a match that the first click's answer had taken away.
  const folds = tab.locator('quader-disclosure > .disclosure > .heading > button.toggle');
  await expect(folds).toHaveCount(2);
  for (const fold of await folds.all()) {
    await expect(fold).toHaveAttribute('aria-expanded', 'false');
    await fold.click();
    await expect(fold).toHaveAttribute('aria-expanded', 'true');
    // The section that opens moves the toggle under it while it grows; the next click waits until it stands still.
    await page.evaluate(() => Promise.all(document.getAnimations().map((a) => a.finished.catch(() => undefined))));
  }
  yield 'several-details';
  await page.unroute(connections);
  await page.unroute(damaged);
}

/** The dashboard (R-UI-197): as it opens, then two connections of which the second is unreachable on every replica. */
async function* dashboardStorage(page: Page): AsyncGenerator<string> {
  yield '';
  const info = '**/api/v1/system/info';
  const connections = '**/api/v1/system/storage-connections';
  await routeWith(page, info, twoConnectionFacts());
  await answerWith(page, connections, secondUnreachable());
  await expect(page.locator('main section[aria-labelledby="dash-s3"] .connection-row')).toHaveCount(2);
  yield 'storage';
  await page.unroute(info);
  await page.unroute(connections);
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

/** Set up client of `upstream/clamav` (R-CLAMAV-006): as it opens, then with every section of the guide open. */
async function* clamavSetupWalk(page: Page): AsyncGenerator<string> {
  yield '';
  if (await openExplanation(page)) yield 'guide';
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
  // By its place, not its words: the German pass walks the page too (UI_RENDERS_LOCALE=de).
  await page.locator('main quader-disclosure.more-options button.inline-toggle').click();
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

/** The admin area with its side bar shown, pinned or in the drawer: no entry Projects and none Repositories (Q-UI-1040). */
async function* adminSidebarShown(page: Page): AsyncGenerator<string> {
  await openSidebar(page);
  const nav = page.getByRole('navigation', { name: 'Main navigation' });
  await expect(nav.getByRole('link', { name: 'Dashboard', exact: true })).toBeVisible();
  await expect(nav.getByRole('link', { name: 'Repositories', exact: true })).toHaveCount(0);
  yield '';
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
 * A remote repository's section **Mirror** switched to the mode mirror (G5-65, R-UI-088): the format's own mirror settings (an OCI
 * remote's tags and platforms, a Debian remote's distributions and architectures) appear under the bounds, which no stored repository of
 * the seed shows; nothing is saved.
 */
async function* mirrorMode(page: Page): AsyncGenerator<string> {
  // The section is found by its radio of the mode, not by its heading: a change puts "Unsaved changes" into the heading's name.
  const section = page.locator('main quader-disclosure').filter({ has: page.locator('quader-mirror-settings') });
  await section.getByRole('button', { name: 'Mirror', exact: true }).click();
  await section.getByRole('radio', { name: /^Mirror/ }).check();
  await expect(section.locator('quader-format-settings')).toBeVisible();
  await page.evaluate(() => Promise.all(document.getAnimations().map((a) => a.finished.catch(() => undefined))));
  yield 'mirror';
  // The packages to mirror (remote.mirror.include, R-CUR-017): several patterns, one of them long; the bound's notice goes.
  await section
    .locator('#f-remote-mirror-include')
    .fill('library/nginx\nbitnami/redis\ncustomer-portal-release-delivery/frontend-assets-*\nacme/*');
  await expect(section.locator('#mirror-unbounded')).toHaveCount(0);
  yield 'mirror-scope';
}

/** A mirror status of the state unbounded, as the server answers it for a stored mirror of a well-known registry without a bound. */
const unboundedStatus = {
  mode: 'mirror',
  state: 'unbounded',
  pass: null,
  lastCompletePass: null,
  nextPassAt: '2026-11-23T09:00:00Z',
  lastFullPassAt: null,
  errors: { consecutiveFailures: 0, lastAt: null, lastCode: null, lastMessage: null },
  rateLimit: { waits: 0, waitSeconds: 0, lastAt: null, lastStatus: null, until: null },
  bound: { reached: null, at: null, maxBytes: null, usedBytes: 0 },
};

/**
 * A stored mirror of a well-known registry without a bound (Q-CUR-43): the status says **No bound** and plans no pass, and **Run a pass
 * now** is disabled with its reason beside it (Q-UI-1061); the sentence tells the administrator to set a bound and the Owner, who keeps
 * the mirror's settings as stored, who can. The seed stores no such mirror (a save refuses it): the repository's answer is changed in
 * the browser to the mode mirror and the status is the server's answer for one; nothing is saved.
 */
async function* mirrorUnbounded(page: Page): AsyncGenerator<string> {
  const repo = `**/api/v1/projects/${mainProject}/repositories/npmjs`;
  await page.route(`${repo}/mirror`, (route) =>
    route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(unboundedStatus) }),
  );
  await answerWith(page, repo, (body) => ({ ...body, remote: { ...(body['remote'] as Record<string, unknown>), mode: 'mirror' } }));
  const section = page.locator('main quader-disclosure').filter({ has: page.locator('quader-mirror-settings') });
  await section.getByRole('button', { name: 'Mirror', exact: true }).click();
  await expect(section.locator('[data-testid="mirror-state"][data-state="unbounded"]')).toBeVisible();
  await expect(section.getByRole('button', { name: 'Run a pass now' })).toBeDisabled();
  await page.evaluate(() => Promise.all(document.getAnimations().map((a) => a.finished.catch(() => undefined))));
  yield 'unbounded';
}

/** A mirror status of a bounded mirror between two passes, after a complete one, as the server answers it. */
const scheduledStatus = {
  mode: 'mirror',
  state: 'scheduled',
  pass: null,
  lastCompletePass: {
    outcome: 'complete',
    kind: 'incremental',
    method: 'listing',
    startedAt: '2026-11-23T08:00:00Z',
    finishedAt: '2026-11-23T08:14:37Z',
    packages: 42,
    packagesFailed: 0,
    itemsStored: 1318,
    bytesStored: 3_435_973_837,
    discoveryComplete: true,
    discoveryNote: null,
  },
  nextPassAt: '2026-11-23T09:00:00Z',
  lastFullPassAt: '2026-11-22T02:00:00Z',
  errors: { consecutiveFailures: 0, lastAt: null, lastCode: null, lastMessage: null },
  rateLimit: { waits: 3, waitSeconds: 95, lastAt: '2026-11-23T08:06:12Z', lastStatus: 429, until: null },
  bound: { reached: null, at: null, maxBytes: 10_737_418_240, usedBytes: 3_435_973_837 },
};

/** The format's own mirror members of a stored mirror (R-OCI-026, R-DEB-024): what the seed's remotes never store. */
const formatMirror: Record<string, Record<string, unknown>> = {
  oci: { tags: ['1.*', 'stable', 'latest'], excludeTags: ['*-rc*', '*-debug'] },
  deb: { suites: ['bookworm', 'bookworm-updates', 'bookworm-security'], components: ['main', 'contrib'], architectures: ['amd64', 'arm64'], translations: ['en', 'de'], sources: false },
};

/**
 * A remote of a format with mirror settings of its own (an OCI and a Debian remote), stored in the mode mirror (section 12, part C; W-24):
 * first a bounded mirror (packages to mirror, a storage limit and the format's own members) between two passes, then a mirror of the
 * well-known registry stored without its bound, as **No bound** with **Run a pass now** disabled. The seed stores neither (no mirror runs
 * on the render stack): the repository's answer is changed in the browser and the status is the server's answer for one; nothing is
 * saved.
 */
async function* storedMirror(page: Page): AsyncGenerator<string> {
  const repo = `**/api/v1/projects/${otherProject}/repositories/${new URL(page.url()).pathname.split('/').at(-2)}`;
  const section = page.locator('main quader-disclosure').filter({ has: page.locator('quader-mirror-settings') });
  const stored = (bounded: boolean) => (body: Record<string, unknown>): Record<string, unknown> => {
    const format = body['format'] as string;
    const remote = body['remote'] as Record<string, unknown>;
    const mirror = (remote['mirror'] as Record<string, unknown> | undefined) ?? {};
    return {
      ...body,
      remote: {
        ...remote,
        mode: 'mirror',
        mirror: bounded
          ? { ...mirror, include: format === 'oci' ? ['library/nginx', 'library/postgres', 'bitnami/*'] : ['nginx*', 'postgresql-16*'], maxBytes: 10_737_418_240 }
          : { ...mirror, include: [], maxBytes: null },
      },
      [format]: { ...(body[format] as Record<string, unknown>), mirror: formatMirror[format] },
    };
  };
  const open = async (state: string): Promise<void> => {
    await section.getByRole('button', { name: 'Mirror', exact: true }).click();
    await expect(section.locator(`[data-testid="mirror-state"][data-state="${state}"]`)).toBeVisible();
    await expect(section.locator('quader-format-settings')).toBeVisible();
    await page.evaluate(() => Promise.all(document.getAnimations().map((a) => a.finished.catch(() => undefined))));
  };
  await page.route(`${repo}/mirror`, (route) => route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(scheduledStatus) }));
  await answerWith(page, repo, stored(true));
  await open('scheduled');
  yield 'stored';
  await page.unroute(`${repo}/mirror`);
  await page.route(`${repo}/mirror`, (route) => route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(unboundedStatus) }));
  await answerWith(page, repo, stored(false));
  await open('unbounded');
  await expect(section.getByRole('button', { name: 'Run a pass now' })).toBeDisabled();
  yield 'stored-unbounded';
}

/**
 * A service account's page (R-UI-181) as it opens, with its technical tokens (an active one, one with a long name, an expired and a
 * revoked one in the seed), a token's description being edited and its removal being confirmed, then the create form of a technical
 * token with its name first, where the caller may create one.
 */
async function* technicalTokens(page: Page): AsyncGenerator<string> {
  await expect(page.locator('main quader-technical-token-table tbody tr').first()).toBeVisible();
  yield '';
  // A token's own changes from its row's menu (G5-51), where the caller may make them: the dialog of its description, then the
  // confirmation of its removal; each is cancelled, nothing is saved.
  const menu = page.locator('main quader-technical-token-table').first().getByRole('button', { name: /^Actions for / });
  if ((await menu.count()) > 0) {
    await menu.first().click();
    await page.getByRole('menuitem', { name: 'Edit description' }).click();
    const dialog = page.getByRole('dialog');
    await expect(dialog.getByRole('textbox')).toBeVisible();
    yield 'description';
    await dialog.getByRole('button', { name: 'Cancel' }).click();
    await expect(dialog).toHaveCount(0);
    await menu.first().click();
    await page.getByRole('menuitem', { name: 'Delete' }).click();
    await expect(dialog.getByRole('button', { name: 'Delete' })).toBeVisible();
    yield 'delete';
    await dialog.getByRole('button', { name: 'Cancel' }).click();
    await expect(dialog).toHaveCount(0);
  }
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

/** Changes the real answers of a URL from now on, without loading the page again: for an answer the page reads later (a section that opens). */
async function routeWith(page: Page, url: string, change: (body: Record<string, unknown>) => Record<string, unknown>): Promise<void> {
  await page.unroute(url);
  await page.route(url, async (route) => {
    const res = await route.fetch();
    await route.fulfill({ response: res, body: JSON.stringify(change((await res.json()) as Record<string, unknown>)) });
  });
}

/**
 * An answer of the app's API changed in the browser from the real one, and the page loaded again: the states of the malware scan
 * that the shared render database cannot hold (R-MAL-023, R-MAL-024): a stale database with a failure, none yet, the scanning off,
 * the installation's switch on. Whether the caller may read it stays the server's.
 */
async function answerWith(page: Page, url: string, change: (body: Record<string, unknown>) => Record<string, unknown>): Promise<void> {
  await routeWith(page, url, change);
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
  // The replicas of malware-scanner-clamav under the summary, and those of the updater malware-updater-freshclam, the folds opened
  // (R-OPS-048, Q-UI-897, Q-UI-913).
  const folds = page.locator('main quader-scanner-replicas quader-disclosure button.inline-toggle');
  for (const fold of await folds.all()) {
    await fold.click();
    await expect(fold).toHaveAttribute('aria-expanded', 'true');
  }
  yield 'scanners';
  await page.getByRole('checkbox', { name: 'Only checked artifacts can be downloaded' }).check();
  await page.getByRole('button', { name: /^Save changes/ }).click();
  await expect(page.getByRole('dialog', { name: 'Let only checked artifacts be downloaded?' })).toBeVisible();
  yield 'confirm';
  await page.getByRole('dialog').getByRole('button', { name: 'Cancel' }).click();
  await answerWith(page, settings, (b) => ({
    ...b,
    backlog: { unchecked: 48213, notCheckable: 1207 },
    database: {
      version: 'main-62.daily-27832.bytecode-339',
      builtAt: new Date(Date.now() - 4 * 86_400_000).toISOString(),
      importedAt: new Date(Date.now() - 4 * 86_400_000 + 3_600_000).toISOString(),
      stale: true,
      repository: 'upstream/clamav',
      source: 'repository',
      sourceRepository: 'ops/clamav-db',
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
    database: { ...(b['database'] as object), version: null, builtAt: null, importedAt: null, stale: false, lastError: null },
  }));
  await expect(page.locator('main [data-testid="malware-db-none"]')).toBeVisible();
  yield 'none';
  // No malware scanner replica: the profile's own answer, in which no scanner is configured (R-OPS-048).
  const scanners = '**/api/v1/system/scanners';
  await answerWith(page, scanners, (b) => ({ ...b, items: [] }));
  await expect(page.locator('main [data-testid="scanners-none-clamav"]')).toBeVisible();
  // Before the first set and with no updater configured, the database part says why none arrives (Q-UI-1003).
  await expect(page.locator('main [data-testid="malware-db-no-updater"]')).toBeVisible();
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

/**
 * A version whose tarball holds a Go program past clamd's scan-size limit (Q-MAL-105): the banner and the cell as they open, then the
 * cell's Technical details unfolded. By place, not by words: the German pass walks it too.
 */
async function* malwareLimitVersion(page: Page): AsyncGenerator<string> {
  await expect(page.locator('main [data-testid="malware-banner"]')).toBeVisible();
  yield '';
  await page.locator('main quader-malware-status quader-technical-details button').first().click();
  await expect(page.locator('main quader-malware-status [data-testid="technical-details"]').first()).toBeVisible();
  yield 'details';
}

/**
 * The find of that Go program in the malware view (Q-UI-1206): the row, the row opened with the scanner's text unfolded, and the
 * release dialog with the false alarm and its Technical details unfolded. By place, not by words: the German pass walks it too.
 */
async function* malwareLimitFind(page: Page): AsyncGenerator<string> {
  await expect(page.locator('main tbody tr').first()).toBeVisible();
  yield '';
  await page.locator('main .name-cell button[aria-expanded="false"]').first().click();
  await page.locator('main .find-detail quader-technical-details button').first().click();
  await expect(page.locator('main .find-detail [data-testid="technical-details"]')).toBeVisible();
  yield 'opened';
  await page.locator('main .name-cell button[aria-expanded="true"]').first().click();
  await page.locator('main td.actions button').first().click();
  const dialog = page.locator('.cdk-overlay-container [role="dialog"]');
  await expect(dialog.locator('[data-testid="release-false-alarm"]')).toBeVisible();
  await dialog.locator('quader-technical-details button').click();
  await expect(dialog.locator('[data-testid="technical-details"]')).toBeVisible();
  yield 'release';
  await page.keyboard.press('Escape');
  await expect(dialog).toBeHidden();
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

// >>> download-links
/** Opens the closed section headed `heading` and waits for its fold to end. */
async function openHeaded(page: Page, heading: string): Promise<void> {
  const { toggle } = sectionHeaded(page, heading);
  await toggle.click();
  await expect(toggle).toHaveAttribute('aria-expanded', 'true');
  await page.evaluate(() => Promise.all(document.getAnimations().map((a) => a.finished.catch(() => undefined))));
}

/**
 * **Settings › Storage** (R-UI-200): as the stack answers (the switch off, the profile's one connection); Details open; a test link of
 * the connection's endpoint from the server; the switch on with that connection; four connections of which one failed its link check and
 * two are encrypted, one a Bastion reads and one under a host of Quader it cannot (Q-UI-990), Details open; the encrypted one's test link
 * (Q-UI-958); no connection that can serve a link (an address under
 * a host of Quader and an encrypted one);
 * a link life and an address the page refuses at their fields before it sends. Nothing is saved.
 */
async function* storageSettings(page: Page): AsyncGenerator<string> {
  const settings = '**/api/v1/download-links/settings';
  await expect(page.getByRole('heading', { name: 'Download links' })).toBeVisible();
  yield '';
  await openHeaded(page, 'Details');
  yield 'details';
  await page.getByRole('button', { name: /^Test link/ }).first().click();
  await expect(page.locator('main [data-testid^="links-test-"] quader-address-field').first()).toBeVisible();
  yield 'test';
  await answerWith(page, settings, (b) => ({ ...b, enabled: true }));
  await expect(page.locator('main [data-testid="links-status"] .ok')).toBeVisible();
  yield 'on';
  const plain = (name: string, endpoint: string, over: Record<string, unknown> = {}): Record<string, unknown> => ({
    name, encrypted: false, endpoint, publicAddress: null, usable: true, reasons: [], handoverUsable: true, handoverReasons: [], ...over,
  });
  await answerWith(page, settings, (b) => ({
    ...b,
    enabled: true,
    minSize: 16 * 1048576,
    connections: [
      plain('default', 'https://s3.eu-central-1.example.com', { publicAddress: 'https://objects.example.com' }),
      plain('archive', 'https://objects.archive.internal.example.com', {
        encrypted: true, usable: false, reasons: [{ code: 'encrypted', detail: null }],
      }),
      plain('eu-backup-frankfurt', 'https://s3.backup.eu-central-2.example.net', {
        usable: false, handoverUsable: false,
        reasons: [{ code: 'link_check_failed', detail: "the link answered 403 with 0 bytes, want 200 with the probe's 2048", check: forbiddenLink }],
        handoverReasons: [{ code: 'link_check_failed', detail: "the link answered 403 with 0 bytes, want 200 with the probe's 2048", check: forbiddenLink }],
      }),
      // An encrypted connection a Bastion cannot read: its sentence and why a Bastion cannot stand under its field (Q-UI-990).
      plain('vault-eu-west', 'https://vault.s3.quader.example.com', {
        encrypted: true, usable: false, handoverUsable: false,
        reasons: [{ code: 'encrypted', detail: null }, { code: 'quader_host', detail: 'quader.example.com' }],
        handoverReasons: [{ code: 'quader_host', detail: 'quader.example.com' }],
      }),
    ],
  }));
  await openHeaded(page, 'Details');
  yield 'several';
  // The encrypted connection's test link, for a Bastion's network (R-STOR-096, Q-UI-958); the profile has no such connection, so
  // the test is answered in the browser as the API answers it.
  const test = '**/api/v1/download-links/test';
  await page.route(test, (route) => {
    const url = 'https://objects.archive.internal.example.com/quader-blobs/quader/_probe/link-test-4f1c?X-Amz-Expires=60&X-Amz-Signature=5e3d';
    return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ url, expiresAt: new Date(Date.now() + 60_000).toISOString(), command: `curl -fsS '${url}'` }) });
  });
  await page.getByRole('button', { name: 'Test link (archive)' }).click();
  await expect(page.locator('main [data-testid="links-test-archive"] quader-address-field')).toBeVisible();
  yield 'encrypted-test';
  await page.unroute(test);
  await answerWith(page, settings, (b) => ({
    ...b,
    enabled: true,
    connections: [
      plain('default', 'https://s3.localhost:8443', { usable: false, reasons: [{ code: 'quader_host', detail: 'localhost' }] }),
      plain('archive', 'https://objects.archive.internal.example.com', { encrypted: true, usable: false, reasons: [{ code: 'encrypted', detail: null }] }),
    ],
  }));
  await expect(page.locator('main [data-testid="links-none"]')).toBeVisible();
  yield 'none';
  await page.unroute(settings);
  await page.reload();
  await page.waitForLoadState('networkidle');
  await openHeaded(page, 'Details');
  await page.locator('main #links-life').fill('5');
  await page.locator('main input[id^="links-address-"]').first().fill('http://objects.example.com/blobs');
  await page.getByRole('button', { name: /^Save changes/ }).click();
  await expect(page.getByText('Enter a number of seconds from 10 to 3,600.')).toBeVisible();
  yield 'invalid';
}

/**
 * The repository's section **Download links** (R-UI-201, Q-UI-956) on an OCI repository: with the installation's switch off as the stack
 * has it (usable, with the sentence that the choice takes effect when an administrator switches links on), with the switch on, and on an
 * npm repository, not every client of which checks a digest (disabled, with its reason). Nothing is saved.
 */
async function* downloadLinksSection(page: Page): AsyncGenerator<string> {
  await openHeaded(page, 'Download links');
  await expect(page.locator('main [data-testid="repo-links-installation-off"]')).toBeVisible();
  yield '';
  const sw = '**/api/v1/download-links/switch';
  await answerWith(page, sw, (b) => ({ ...b, enabled: true }));
  await openHeaded(page, 'Download links');
  await expect(page.locator('main [data-testid="repo-links-installation-on"]')).toBeVisible();
  yield 'on';
  await page.goto(`${P}/repositories/npm-internal/settings`);
  await page.waitForLoadState('networkidle');
  await openHeaded(page, 'Download links');
  await expect(page.locator('main [data-testid="repo-links-not-applicable"]')).toBeVisible();
  yield 'not-applicable';
  await page.unroute(sw);
}
// <<< download-links

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
  { id: 'new-npm-eight-members', path: `/ui/projects/${mainProject}/repositories/new/npm`, kinds: ['power'], walk: eightMembers },
  { id: 'new-npm-restricted', path: `/ui/projects/${mainProject}/repositories/new/npm`, kinds: ['power'], walk: restrictedCreate },
];

/** The creation form of every format with settings of its own, in every class: its Advanced settings show them (R-UI-088). */
const createForms: Screen[] = (['oci', 'npm', 'ansible', 'nuget', 'deb'] as const).flatMap((name) =>
  (['Local', 'Remote', 'Virtual'] as const).flatMap((cls): Screen[] => {
    const id = `new-${name}-${cls.toLowerCase()}`;
    return [{ id, path: `/ui/projects/${mainProject}/repositories/new/${name}`, kinds: ['power'], walk: createForm(cls) }];
  }),
);

/** An answer of the upstream test, as the server gives it (R-REPO-024, R-REPO-064), for a state of the upstream section. */
function testAnswer(over: Record<string, unknown>): Record<string, unknown> {
  return {
    probeUrl: 'https://registry.gitlab.com/v2/',
    credential: 'basic',
    reachable: true,
    httpStatus: 401,
    authenticated: null,
    durationMs: 212,
    tls: { peerSubject: 'CN=registry.example.com', notAfter: '2027-03-01T00:00:00Z' },
    outboundProxy: null,
    sourceAddress: null,
    tokenService: null,
    body: null,
    error: null,
    ...over,
  };
}

/** GitLab's token service as the test names it, with the status of its answer to the login. */
function gitlabTokenService(httpStatus: number): Record<string, unknown> {
  return {
    host: 'gitlab.com',
    allowed: true,
    receivesCredential: true,
    realm: 'https://gitlab.com/jwt/auth',
    service: 'container_registry',
    extraHost: 'gitlab.com',
    httpStatus,
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
        ['test-accepted', testAnswer({ verdict: 'accepted', authenticated: true, tokenService: gitlabTokenService(200) })],
        ['test-refused', testAnswer({ verdict: 'refused', authenticated: false, tokenService: gitlabTokenService(401) })],
        [
          'test-unreachable',
          testAnswer({
            verdict: 'unreachable',
            reachable: false,
            httpStatus: null,
            tls: null,
            error: { code: 'dns_error', message: 'The name registry.gitlab.com does not resolve.' },
          }),
        ],
        [
          'test-token-foreign',
          testAnswer({
            verdict: 'token_service_denied',
            tokenService: {
              host: 'gitlab.example-company.com',
              allowed: false,
              receivesCredential: false,
              realm: 'https://gitlab.example-company.com/jwt/auth',
              service: 'container_registry',
              extraHost: null,
              httpStatus: null,
            },
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

/**
 * The toasts of R-UI-042 (Q-UI-1063) on the page where the owner saw them stack (2026-11-24): a registry switched on, its write answered
 * in the browser so the seed's registries stay as they are; a second one switched on, whose toast replaces the first's; and beside it
 * the toasts that keep their place, a notice with an action and an error. Those two are sent to the app's `Notifier` through Angular's
 * debugging API of the development build, with the texts of their real callers (the tab Upstream's bulk download, a copy that failed),
 * because this page has neither. Its controls are found by role and state, so it renders in German too.
 */
async function* toastsWalk(page: Page): AsyncGenerator<string> {
  await page.route('**/api/v1/upstream-repositories/*/state', async (route) => {
    const url = new URL(route.request().url());
    const item = (await (await page.request.get(url.href.replace(/\/state$/, ''))).json()) as Record<string, unknown>;
    await route.fulfill({ status: 200, contentType: 'application/json', headers: { ETag: '"1"' }, body: JSON.stringify({ ...item, state: 'active' }) });
  });
  const off = page.locator('main quader-upstream-registries [role="switch"][aria-checked="false"]');
  const toasts = page.locator('quader-notices .toast');
  await expect(off.first()).toBeVisible();
  await off.first().click();
  await expect(toasts).toHaveCount(1);
  yield 'toast';
  await off.first().click();
  await expect(page.locator('main [role="switch"][aria-busy="true"]')).toHaveCount(0);
  await expect(toasts).toHaveCount(1);
  yield 'toast-replaced';
  await page.evaluate(() => {
    const de = document.documentElement.lang.startsWith('de');
    const ng = (window as unknown as { ng: { getComponent(el: Element): unknown } }).ng;
    const host = ng.getComponent(document.querySelector('quader-notices') as Element) as {
      notifier: { status(text: string, action?: { label: string; run(): void }): void; alert(text: string): void };
    };
    host.notifier.status(
      de
        ? 'Der Sammeldownload hat begonnen; Quader liest zuerst seine Versionen und reiht sie dann ein.'
        : 'The bulk download has started; Quader reads its versions first, then queues them.',
      { label: de ? 'Sammeldownload anzeigen' : 'Show bulk download', run: () => undefined },
    );
    host.notifier.alert(
      de ? 'Die Zwischenablage ist nicht verfügbar. Markieren Sie den Text und kopieren Sie ihn.' : 'The clipboard is not available. Select the text and copy it.',
    );
  });
  await expect(toasts).toHaveCount(3);
  yield 'toasts';
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
  const explain = page.locator('main').getByRole('button', { name: 'Explain for a repository' });
  await explain.click();
  // The panel opens over 200 ms with its content clipped: a field focused meanwhile scrolls the clipped content, and the button under
  // it moves with the opening, so on a loaded runner the press of the click landed on the button and its release beside it, and
  // nothing was submitted (stopped at 390 px, 2026-11-24). The walk acts once the panel stands open.
  await expect(explain).toHaveAttribute('aria-expanded', 'true');
  await page.evaluate(() => Promise.all(document.getAnimations().map((a) => a.finished.catch(() => undefined))));
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

// ---- The signatures (R-SIG-035 to R-SIG-038) ----

/** Opens every fold of the details of the signatures (Technical details) and waits for their animations. */
async function openTechnical(page: Page): Promise<void> {
  for (const fold of await page.locator('main quader-signature-report quader-disclosure button.inline-toggle').all()) {
    await fold.click();
    await expect(fold).toHaveAttribute('aria-expanded', 'true');
  }
  await page.evaluate(() => Promise.all(document.getAnimations().map((a) => a.finished.catch(() => undefined))));
}

/**
 * The tab **Signatures** of the versions of one package in every state (R-SIG-009, R-SIG-035): Verified · public with npm's registry
 * signature and a provenance, its technical details opened, Verified · internal, a blocked signer, an invalid provenance, one that
 * waits and none.
 */
async function* signatureVersions(page: Page): AsyncGenerator<string> {
  await expect(page.locator('main [data-testid="signature-entry"]').first()).toBeVisible();
  yield '';
  await openTechnical(page);
  yield 'technical';
  for (const [state, version] of Object.entries(signedVersions).filter(([k]) => k !== 'public')) {
    await page.goto(`${ver(P, manyVersionsPackage, version)}?tab=signatures`);
    await expect(page.locator('main [data-testid="signature-state"]')).toBeVisible();
    yield state;
  }
  // R-SIG-056: the same local version as an installation with a signing identity answers it, Quader's npm signature beside the
  // publisher's provenance and its receipt under Attestations, the receipt's fold opened (renders/receipts.ts).
  const report = `**/api/v1/packages/*/versions/${signedVersions.public}/signatures`;
  await page.goto(`${ver(P, manyVersionsPackage, signedVersions.public)}?tab=signatures`);
  await answerWith(page, report, issuedVersion);
  await expect(page.locator('main [data-testid="signature-receipt"]')).toBeVisible();
  await page.locator('main [data-testid="signature-receipt"] quader-disclosure button.inline-toggle').click();
  await page.evaluate(() => Promise.all(document.getAnimations().map((a) => a.finished.catch(() => undefined))));
  yield 'issued';
  await page.unroute(report);
}

/**
 * The image's tab **Signatures** (R-SIG-024): its signatures and, apart, its attestations and SBOMs; with Quader's receipt standing as
 * its referrer (R-SIG-056); then the image no anchor knows, whose older cosign signature tag puts its receipt behind the API, as the
 * page says (R-SIG-054). The receipts are the real answers changed in the browser (renders/receipts.ts).
 */
async function* signatureImages(page: Page): AsyncGenerator<string> {
  await expect(page.locator('main .sig-attestations table')).toBeVisible();
  yield '';
  await openTechnical(page);
  yield 'technical';
  const image = `**/api/v1/packages/*/versions/${signedImage.tag}/signatures`;
  await answerWith(page, image, issuedImage('referrer'));
  await expect(page.locator('main [data-testid="signature-receipt-served"]')).toContainText('As a referrer of the image');
  yield 'receipt';
  await routeWith(page, image, issuedImage('hides_signature'));
  await page.goto(`${ver(P, { format: 'oci', name: signedImage.untrusted }, '1.4.0')}?tab=signatures`);
  await expect(page.locator('main [data-testid="signature-entry"]').first()).toBeVisible();
  await expect(page.locator('main [data-testid="signature-receipt-served"]')).toContainText('older cosign signature');
  yield 'untrusted';
  await page.unroute(image);
}

/**
 * A file's section **Signatures** opened (R-SIG-035, R-SIG-010, R-SIG-056): three signature files, the pipeline that uploaded it and
 * Quader's receipt (the real answer with the receipt added in the browser, renders/receipts.ts); then one of the signature files.
 */
async function* signatureFile(page: Page): AsyncGenerator<string> {
  await expect(page.locator('main [data-testid="signature-fact"]')).toBeVisible();
  yield '';
  // The section reads the file's answer when it opens, so the change is in place before the toggle.
  const report = `**/api/v1/projects/${mainProject}/repositories/${signedFile.repo}/signatures?**`;
  await routeWith(page, report, issuedFile);
  const { toggle } = sectionHeaded(page, 'Signatures');
  await toggle.click();
  await expect(page.locator('main [data-testid="signature-origin"]')).toBeVisible();
  await expect(page.locator('main [data-testid="signature-receipt"]')).toBeVisible();
  await page.evaluate(() => Promise.all(document.getAnimations().map((a) => a.finished.catch(() => undefined))));
  yield 'details';
  await page.unroute(report);
  await page.goto(`${P}/repositories/${signedFile.repo}/files/${signedFile.folder}/${signedFile.name}.asc`);
  await expect(page.locator('main dl.file-facts')).toBeVisible();
  yield 'sibling';
}

/** Opens the section **Public Sigstore root** of **Settings › Signatures**, which a reload closes. */
async function openRoot(page: Page): Promise<void> {
  const { section, toggle } = sectionHeaded(page, 'Public Sigstore root');
  await toggle.click();
  await expect(toggle).toHaveAttribute('aria-expanded', 'true');
  await expect(section.locator('[data-testid="signature-root"]')).toBeVisible();
  await page.evaluate(() => Promise.all(document.getAnimations().map((a) => a.finished.catch(() => undefined))));
}

/**
 * **Settings › Signatures** (R-SIG-036): as seeded (anchors of every kind and scope, the switch on, a block), the closed sections opened,
 * the form of each kind of anchor with a CA's More options, the form of a block, the impact of a removal and of a block; then the public
 * root refreshed (the profile's own is the release's snapshot, Q-UI-1047), the switch off with a project's anchor kept, the public root
 * refreshed with a failure and a backlog, and the signatures off in the configuration. Nothing is saved.
 */
async function* signatureSettings(page: Page): AsyncGenerator<string> {
  const settings = '**/api/v1/signatures/settings';
  await expect(page.locator('main quader-anchor-table tbody tr').first()).toBeVisible();
  yield '';
  for (const heading of ['Blocked signers', 'Public Sigstore root']) {
    const { toggle } = sectionHeaded(page, heading);
    await toggle.click();
    await expect(toggle).toHaveAttribute('aria-expanded', 'true');
  }
  await page.evaluate(() => Promise.all(document.getAnimations().map((a) => a.finished.catch(() => undefined))));
  yield 'sections';
  await page.getByRole('button', { name: 'Add trust anchor' }).first().click();
  const dialog = page.getByRole('dialog', { name: 'Add trust anchor' });
  await expect(dialog).toBeVisible();
  await dialog.locator('quader-disclosure button.inline-toggle').click();
  yield 'anchor-ca';
  for (const [kind, name] of [
    ['openpgp', 'anchor-openpgp'],
    ['public-key', 'anchor-public-key'],
    ['sigstore', 'anchor-sigstore'],
  ] as const) {
    await dialog.locator('select[name="kind"]').selectOption(kind);
    yield name;
  }
  await dialog.getByRole('button', { name: 'Cancel' }).click();
  await page.getByRole('button', { name: 'Block signer' }).first().click();
  await expect(page.getByRole('dialog', { name: 'Block signer' })).toBeVisible();
  yield 'block';
  await page.getByRole('dialog').getByRole('button', { name: 'Cancel' }).click();
  // The impact before a removal and a block (R-SIG-071, R-SIG-074), answered in the browser: the profile gates no repository.
  const impact = '**/api/v1/signatures/impact';
  await page.route(impact, (route) =>
    route.fulfill({
      json: {
        artifacts: 1328,
        repositories: 23,
        complete: true,
        bound: 'exact',
        byRepository: [
          { repository: `${mainProject}/images`, artifacts: 1120 },
          { repository: `${mainProject}/npm-vendor`, artifacts: 140 },
          { repository: 'customer-portal-release-delivery/signed-release-artifacts', artifacts: 61 },
          { repository: `${otherProject}/collections`, artifacts: 7 },
        ],
      },
    }),
  );
  await page.locator('main quader-anchor-table tr').filter({ hasText: 'Release key 2026' }).getByRole('button', { name: /^Remove/ }).click();
  await expect(page.getByRole('dialog')).toBeVisible();
  yield 'impact';
  await page.getByRole('dialog').getByRole('button', { name: 'Cancel' }).click();
  await page.getByRole('button', { name: 'Block signer' }).first().click();
  const blockDialog = page.getByRole('dialog', { name: 'Block signer' });
  await blockDialog.getByRole('textbox', { name: 'Value' }).fill('9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08');
  await blockDialog.getByRole('textbox', { name: 'Reason' }).fill('The key of the old build server was copied.');
  await blockDialog.getByRole('button', { name: 'Block signer' }).click();
  await expect(blockDialog.locator('[data-testid="block-impact"]')).toBeVisible();
  yield 'block-impact';
  await blockDialog.getByRole('button', { name: 'Cancel' }).click();
  await page.unroute(impact);
  // The profile does not refresh the public root (Q-UI-1047): 'sections' shows the release's snapshot, the stack's answer, and a
  // refreshed root is answered here, without a failure, and in 'switch-off' with one.
  await answerWith(page, settings, (b) => ({
    ...b,
    trustRoot: { source: 'refreshed', fetchedAt: new Date(Date.now() - 6 * 3_600_000).toISOString(), tufVersion: 171, lastError: null, lastErrorAt: null },
  }));
  await openRoot(page);
  yield 'root-refreshed';
  await answerWith(page, settings, (b) => ({
    ...b,
    projectAnchors: false,
    backlog: { pending: 1240, stale: 38211 },
    trustRoot: {
      source: 'refreshed',
      fetchedAt: new Date(Date.now() - 3 * 86_400_000).toISOString(),
      // A stored root is newer than the release's snapshot (166), or the task does not store it (R-SIG-017).
      tufVersion: 171,
      lastError: 'tuf: fetching timestamp.json from https://tuf-repo-cdn.sigstore.dev: dial tcp: i/o timeout',
      lastErrorAt: new Date(Date.now() - 2 * 3_600_000).toISOString(),
    },
  }));
  await openRoot(page);
  yield 'switch-off';
  await answerWith(page, settings, (b) => ({ ...b, enabled: false }));
  yield 'off';
  await page.unroute(settings);
}

/**
 * The section **Signing identity** of **Settings › Signatures** (R-SIG-056): the page without an identity is the real answer and
 * `admin-settings-signatures`' first picture; here, for the holder of `security-settings:write`, the form of **Create signing key**
 * with its More options; then, for both kinds, a key request shown, an upload that expires in 12 days with two previous identities
 * (the section opened), the Helm values' identity, and the Helm values with a refused pair. The states are answered in the browser
 * (renders/signing.ts); nothing is saved.
 */
async function* signingIdentity(page: Page): AsyncGenerator<string> {
  const url = '**/api/v1/signing/identity';
  const section = page.locator('main quader-signing-identity');
  await expect(section.locator('[data-testid="signing-none"]')).toBeVisible();
  const create = section.getByRole('button', { name: 'Create signing key' });
  if ((await create.count()) > 0) {
    await create.click();
    const dialog = page.getByRole('dialog', { name: 'Create signing key' });
    await expect(dialog).toBeVisible();
    await dialog.locator('quader-disclosure button.inline-toggle').click();
    await page.evaluate(() => Promise.all(document.getAnimations().map((a) => a.finished.catch(() => undefined))));
    yield 'create';
    await dialog.getByRole('button', { name: 'Cancel' }).click();
  }
  await answerWith(page, url, signingAnswer('pending'));
  await expect(section.locator('quader-code-window')).toBeVisible();
  yield 'pending';
  await answerWith(page, url, signingAnswer('upload'));
  const { toggle } = sectionHeaded(page, 'Previous identities');
  await toggle.click();
  await expect(toggle).toHaveAttribute('aria-expanded', 'true');
  await page.evaluate(() => Promise.all(document.getAnimations().map((a) => a.finished.catch(() => undefined))));
  // The job's backlog of both kinds in its quiet line (R-SIG-055 `backlog`); the Helm values' state below has none ("Nothing waits").
  await expect(section.locator('[data-testid="signing-backlog"]')).toContainText('receipt attestations');
  yield 'upload';
  await answerWith(page, url, signingAnswer('helm'));
  await expect(section.locator('[data-testid="signing-facts-current"]')).toBeVisible();
  await expect(section.locator('[data-testid="signing-backlog"]')).toHaveText('Nothing waits to be signed.');
  yield 'helm';
  await answerWith(page, url, signingAnswer('helm-refused'));
  await expect(section.locator('[data-testid="signing-refused"]')).toBeVisible();
  yield 'helm-refused';
  await page.unroute(url);
}

/**
 * The client setup page as it opens, then with the step **Verify** (R-SIG-056): the profile holds no signing identity, so the tab is the real
 * answer with the server's snippet added in the browser (renders/verify.ts).
 */
async function* setupVerify(page: Page): AsyncGenerator<string> {
  yield '';
  const url = '**/api/v1/projects/*/repositories/*/client-setup*';
  await answerWith(page, url, withVerify);
  // npm's step holds npm audit signatures before the attestation's command: the last one is the server's added snippet either way.
  await expect(page.locator('main section[aria-labelledby="step-verify"] quader-setup-snippet pre').last()).toBeVisible();
  yield 'verify';
  await page.unroute(url);
}

/** The project's section **Trust anchors** opened (R-SIG-037), its form for an Owner; then the switch off, with the anchor not applied. */
async function* projectAnchors(page: Page): AsyncGenerator<string> {
  const anchors = `**/api/v1/projects/${mainProject}/signatures/anchors`;
  const { toggle } = sectionHeaded(page, 'Trust anchors');
  await toggle.click();
  await expect(toggle).toHaveAttribute('aria-expanded', 'true');
  await page.evaluate(() => Promise.all(document.getAnimations().map((a) => a.finished.catch(() => undefined))));
  yield '';
  const add = page.getByRole('button', { name: 'Add trust anchor' });
  if ((await add.count()) > 0) {
    await add.click();
    await expect(page.getByRole('dialog', { name: 'Add a trust anchor to this project' })).toBeVisible();
    yield 'add';
    await page.getByRole('dialog').getByRole('button', { name: 'Cancel' }).click();
  }
  await answerWith(page, anchors, (b) => ({
    ...b,
    projectAnchors: false,
    items: ((b['items'] as Record<string, unknown>[] | undefined) ?? []).map((a) => ({ ...a, applied: false })),
  }));
  const off = sectionHeaded(page, 'Trust anchors');
  if ((await off.toggle.count()) > 0) {
    await off.toggle.click();
    await expect(page.locator('main [data-testid="project-anchors-off"]')).toBeVisible();
    yield 'off';
  }
  await page.unroute(anchors);
}

/** A remote repository's closed section **Signatures** opened (R-SIG-040): the fetch of the upstream's signatures. */
async function* signaturesSection(page: Page): AsyncGenerator<string> {
  const { toggle } = sectionHeaded(page, 'Signatures');
  await toggle.click();
  await expect(toggle).toHaveAttribute('aria-expanded', 'true');
  await page.evaluate(() => Promise.all(document.getAnimations().map((a) => a.finished.catch(() => undefined))));
  yield '';
}

/** The dashboard's view **Signatures** (R-SIG-038): the findings, the drawer, a filter of a kind, and the empty list. */
async function* signatureFindings(page: Page): AsyncGenerator<string> {
  await expect(page.locator('main tbody tr').first()).toBeVisible();
  yield '';
  // The findings' own head, after the refused downloads' (R-SIG-074).
  await page.locator('main .filter-toggle').last().click();
  const drawer = page.getByRole('dialog', { name: 'Filters' });
  await expect(drawer.locator('form.filter-form')).toBeVisible();
  yield 'filters';
  await drawer.locator('select[aria-label="Kind"]').selectOption('blocked');
  await drawer.getByRole('button', { name: 'Apply filters' }).click();
  await expect(drawer).toBeHidden();
  await expect(page.locator('main .list-filters .chip-row')).toBeVisible();
  await expect(page.locator('main tbody tr').first()).toBeVisible();
  yield 'blocked';
  await page.route('**/api/v1/signatures/findings?**', (route) => route.fulfill({ json: { items: [], nextCursor: null } }));
  await page.goto('/ui/security/signatures');
  await expect(page.getByText('No invalid signatures.')).toBeVisible();
  yield 'empty';
  await page.unroute('**/api/v1/signatures/findings?**');
}

// ---- The gate per repository (R-SIG-074) ----

/** The generic repository of the main project whose settings show the gate, and the anchors a choice of it names (seeded by signatures.ts). */
const gateRepo = 'assets';
const chosenAnchors = ['installation/example-code-signing', 'installation/release-key-2026', 'project/web-team-release-key', 'installation/old-build-server'];
/** A preview of thousands of artifacts with refusals of several reasons and twenty examples (R-SIG-072), answered in the browser. */
function gatePreviewOf(complete: boolean): Record<string, unknown> {
  const reasons = ['missing', 'blocked', 'missing', 'invalid', 'not_chosen'] as const;
  return {
    examined: 6203,
    complete,
    artifacts: 4812,
    pass: 3410,
    pending: 12,
    refused: { missing: 1180, untrusted: 0, notChosen: 9, invalid: 3, blocked: 198, notChecked: 0 },
    examples: Array.from({ length: 20 }, (_, i) => ({
      repository: `${mainProject}/${gateRepo}`,
      path: `releases/portal-cli/1.${20 - i}.0/portal-cli-1.${20 - i}.0-linux-amd64-with-a-long-build-suffix.tar.gz`,
      package: null,
      version: null,
      reason: reasons[i % reasons.length],
    })),
  };
}

/** Opens the section Signatures of a repository's settings and waits for its state line or its notice. */
async function openGate(page: Page): Promise<void> {
  const { toggle } = sectionHeaded(page, 'Signatures');
  await toggle.click();
  await expect(toggle).toHaveAttribute('aria-expanded', 'true');
  await expect(page.locator('main [data-testid="gate-state"], main [data-testid="gate-checks-off"]').first()).toBeVisible();
  await page.evaluate(() => Promise.all(document.getAnimations().map((a) => a.finished.catch(() => undefined))));
}

/**
 * The section **Signatures** of a repository's settings (R-SIG-074): the gate off with the real preview of the profile (the real
 * answer); the question before switching it on, with a preview of thousands; then the stored member changed in the browser: on with any
 * anchor (the warning of Q-SIG-76), on with three chosen anchors and a removed one (read-only where an Owner may not loosen it), and the
 * checks switched off in the installation. Nothing is saved.
 */
async function* gateSection(page: Page): AsyncGenerator<string> {
  const doc = `**/api/v1/projects/${mainProject}/repositories/${gateRepo}`;
  const preview = `**/api/v1/projects/${mainProject}/repositories/${gateRepo}/signatures/preview**`;
  await openGate(page);
  yield 'off';
  await page.route(preview, (route) => route.fulfill({ json: gatePreviewOf(true) }));
  await page.locator('main [data-testid="repo-signatures"] quader-checkbox input').first().check();
  await page.getByRole('button', { name: 'Save changes (signatures)' }).click();
  const dialog = page.getByRole('dialog');
  await expect(dialog).toBeVisible();
  yield 'confirm';
  await dialog.getByRole('button', { name: 'Cancel' }).click();
  const gateOf = (anchors: readonly string[]) => (b: Record<string, unknown>) => ({
    ...b,
    signatures: { ...(b['signatures'] as Record<string, unknown>), onlyVerified: true, anchors },
  });
  await answerWith(page, doc, gateOf([]));
  await openGate(page);
  yield 'any';
  await page.unroute(preview);
  await page.route(preview, (route) => route.fulfill({ json: gatePreviewOf(false) }));
  await answerWith(page, doc, gateOf(chosenAnchors));
  await openGate(page);
  yield 'chosen';
  await page.unroute(preview);
  await page.route(preview, (route) =>
    route.fulfill({ status: 409, contentType: 'application/problem+json', json: { code: 'signatures_not_enabled', status: 409, title: 'Conflict' } }),
  );
  await answerWith(page, doc, gateOf([]));
  await openGate(page);
  yield 'checks-off';
  await page.unroute(preview);
  await page.unroute(doc);
}

/** The gate's member of a signatures answer (R-SIG-073), as the walks below answer it. */
function gateView(decision: string, over: Record<string, unknown> = {}): Record<string, unknown> {
  return {
    onlyVerified: true,
    anchors: [
      { ref: 'installation/example-code-signing', title: 'Example Code Signing CA', kind: 'ca', scope: 'installation', removed: false },
      { ref: 'project/web-team-release-key', title: 'Web team release key', kind: 'openpgp', scope: 'project', removed: false },
      { ref: 'installation/old-build-server', title: null, kind: null, scope: 'installation', removed: true },
    ],
    decision,
    reason: decision === 'refused' ? 'missing' : null,
    passedBy: null,
    ...over,
  };
}

/** Each repository of a version's signatures answer with the gate's member. */
async function withGate(page: Page, url: string, gate: Record<string, unknown>): Promise<void> {
  await page.unroute(url);
  await page.route(url, async (route) => {
    const res = await route.fetch();
    const body = (await res.json()) as { repositories: Record<string, unknown>[] };
    await route.fulfill({ response: res, json: { ...body, repositories: body.repositories.map((r) => ({ ...r, gate })) } });
  });
}

/**
 * A version's files with the badge of a signer no anchor trusts (R-SIG-035): the state the screen stands for, waited for by the badge's
 * state and not its words, so the German pass renders it too (Q-UI-1076: the badge **Signed** and "signer not trusted" after it).
 */
async function* untrustedFiles(page: Page): AsyncGenerator<string> {
  await expect(page.locator('main quader-data-table.version-files [data-testid="signature-badge"][data-state="untrusted"]').first()).toBeVisible();
  yield '';
}

/**
 * A version a repository's gate refuses (R-SIG-074): the banner and the Files tab's "Refused here" without Download; its tab
 * **Signatures** with the gate's decision; for the holder of the triage right the release's dialog, and the version released, by whom,
 * when and why. The member `refused` and the gate are the profile's real answers changed in the browser.
 */
async function* refusedVersionWalk(page: Page): AsyncGenerator<string> {
  const details = `**/api/v1/packages/*/versions/${signedVersions.none}`;
  const report = `**/api/v1/packages/*/versions/${signedVersions.none}/signatures`;
  const releases = `**/api/v1/projects/${mainProject}/repositories/*/signatures/releases`;
  const refused = { state: 'none', stale: false, refused: true, refusedReason: 'missing' };
  let sha = '';
  await answerWith(page, details, (b) => {
    const files = (b['files'] as Record<string, unknown>[] | undefined) ?? [];
    sha = String(files[0]?.['sha256'] ?? '');
    return { ...b, signatures: refused, files: files.map((f) => ({ ...f, signatures: refused })) };
  });
  await expect(page.locator('main [data-testid="signature-banner"]').first()).toBeVisible();
  yield '';
  await withGate(page, report, gateView('refused'));
  await page.getByRole('tab', { name: 'Signatures' }).click();
  await expect(page.locator('main [data-testid="gate-refused"]')).toBeVisible();
  yield 'signatures';
  const release = page.locator('main [data-testid="signature-banner"] button');
  if ((await release.count()) > 0) {
    await release.first().click();
    const dialog = page.getByRole('dialog');
    await expect(dialog).toBeVisible();
    await dialog.getByRole('textbox').fill('The vendor signs its releases from 3.6.0 on; the hotfix of ticket OPS-4711 needs this one today.');
    yield 'release';
    await dialog.getByRole('button', { name: 'Cancel' }).click();
    await withGate(page, report, gateView('pass', { passedBy: { kind: 'release', signature: null } }));
    await page.route(releases, (route) =>
      route.fulfill({
        json: {
          items: [
            {
              repository: `${mainProject}/npm-vendor`,
              sha256: sha,
              reason: 'The vendor signs its releases from 3.6.0 on; the hotfix of ticket OPS-4711 needs this one today.',
              refusedReason: 'missing',
              releasedBy: 'user:noor',
              releasedAt: new Date(Date.now() - 2 * 3_600_000).toISOString(),
            },
          ],
        },
      }),
    );
    await answerWith(page, details, (b) => b);
    await page.getByRole('tab', { name: 'Signatures' }).click();
    await expect(page.locator('main [data-testid="gate-released"] .release-reason')).toBeVisible();
    yield 'released';
    await page.unroute(releases);
  }
  await page.unroute(report);
  await page.unroute(details);
}

/** An image whose index passes the gate on its own signature (R-SIG-063, R-SIG-074): each platform signed through its index. */
async function* gateImage(page: Page): AsyncGenerator<string> {
  const details = `**/api/v1/packages/*/versions/${signedImage.tag}`;
  const report = `**/api/v1/packages/*/versions/${signedImage.tag}/signatures`;
  await page.route(details, async (route) => {
    const res = await route.fetch();
    const body = (await res.json()) as Record<string, unknown> & { metadata: Record<string, unknown> };
    const given = Array.isArray(body.metadata['platforms']) ? (body.metadata['platforms'] as unknown[]) : [];
    const platforms =
      given.length > 1
        ? given
        : ['linux/amd64', 'linux/arm64', 'linux/arm/v7'].map((platform, i) => ({
            platform,
            digest: `sha256:${String(i + 1).repeat(64)}`,
            mediaType: 'application/vnd.oci.image.manifest.v1+json',
            size: 1024,
          }));
    await route.fulfill({ response: res, json: { ...body, metadata: { ...body.metadata, platforms } } });
  });
  await page.route(report, async (route) => {
    const res = await route.fetch();
    const body = (await res.json()) as { repositories: (Record<string, unknown> & { signatures: { id: number; platform?: string | null }[] })[] };
    await route.fulfill({
      response: res,
      json: {
        ...body,
        repositories: body.repositories.map((r) => {
          const own = r.signatures.find((e) => !e.platform) ?? r.signatures[0];
          return { ...r, gate: gateView('pass', { anchors: [], passedBy: { kind: 'signature', signature: own?.id ?? null } }) };
        }),
      },
    });
  });
  await page.reload();
  await expect(page.locator('main [data-testid="gate-platforms"]')).toBeVisible();
  yield '';
  await page.unroute(report);
  await page.unroute(details);
}

/**
 * A file a repository's gate refuses (R-SIG-074): the fact "Refused here" with its reason and no Download; its section Signatures with
 * the gate's decision and, for the holder of the triage right, **Release for this repository**. The profile's answers changed in the
 * browser: the signed file of the seed, judged by a gate whose anchors do not include the one that knows its signer.
 */
async function* refusedFileWalk(page: Page): AsyncGenerator<string> {
  const item = `**/api/v1/projects/${mainProject}/repositories/${signedFile.repo}/items?**`;
  const report = `**/api/v1/projects/${mainProject}/repositories/${signedFile.repo}/signatures?**`;
  await answerWith(page, item, (b) => ({ ...b, signatures: { ...(b['signatures'] as Record<string, unknown>), refused: true, refusedReason: 'not_chosen' } }));
  await expect(page.locator('main [data-testid="signature-refused"]')).toBeVisible();
  yield '';
  await page.route(report, async (route) => {
    const res = await route.fetch();
    const body = (await res.json()) as Record<string, unknown>;
    await route.fulfill({
      response: res,
      json: {
        ...body,
        gate: gateView('refused', {
          reason: 'not_chosen',
          anchors: [
            { ref: 'installation/build-server-key', title: 'Build server key', kind: 'public-key', scope: 'installation', removed: false },
            { ref: 'installation/old-build-server', title: null, kind: null, scope: 'installation', removed: true },
          ],
        }),
      },
    });
  });
  const { toggle } = sectionHeaded(page, 'Signatures');
  await toggle.click();
  await expect(page.locator('main [data-testid="gate-refused"]')).toBeVisible();
  await page.evaluate(() => Promise.all(document.getAnimations().map((a) => a.finished.catch(() => undefined))));
  yield 'details';
  await page.unroute(report);
  await page.unroute(item);
}

/** A digest of 64 hex digits from a text, for the rows below. */
function digestOf(text: string): string {
  let h = 0;
  for (const c of text) h = (h * 31 + c.charCodeAt(0)) >>> 0;
  return h.toString(16).padStart(8, '0').repeat(8);
}

/** Refused downloads of several reasons, long paths, one released (R-SIG-073), answered in the browser: the profile refuses nothing. */
function refusalRows(): Record<string, unknown>[] {
  const at = (h: number): string => new Date(Date.now() - h * 3_600_000).toISOString();
  const row = (
    repository: string,
    format: string,
    path: string,
    pkg: string | null,
    version: string | null,
    reason: string,
    last: number,
    count: number,
    release: Record<string, unknown> | null = null,
  ): Record<string, unknown> => ({ repository, format, path, package: pkg, version, sha256: digestOf(path), reason, firstAt: at(last + 70), lastAt: at(last), count, release });
  return [
    row(`${mainProject}/images`, 'oci', 'portal/api/_tags/1.4.0', 'portal/api', '1.4.0', 'untrusted', 0.2, 41),
    row(`${mainProject}/npm-vendor`, 'npm', 'chart-render-kit/-/chart-render-kit-3.5.0.tgz', 'chart-render-kit', '3.5.0', 'missing', 1, 1287),
    row(`${otherProject}/collections`, 'ansible', 'acme/platform/1.3.0/acme-platform-1.3.0.tar.gz', 'acme.platform', '1.3.0', 'blocked', 5, 3),
    row(
      'customer-portal-release-delivery/signed-release-artifacts',
      'generic',
      'releases/customer-portal/2026.11/customer-portal-release-2026.11.3-linux-amd64-with-debug-symbols-and-third-party-licences.tar.gz',
      null,
      null,
      'not_chosen',
      9,
      12,
    ),
    row(`${otherProject}/nuget-internal`, 'nuget', 'acme.logging.abstractions/2.1.0/acme.logging.abstractions.2.1.0.nupkg', 'Acme.Logging.Abstractions', '2.1.0', 'not_checked', 26, 6, {
      reason: 'NuGet author signatures cannot be read yet; checked by hand against the vendor certificate.',
      releasedBy: 'user:noor',
      releasedAt: at(20),
    }),
    row(`${mainProject}/assets`, 'generic', 'tools/cli-1.2.0.tar.gz', null, null, 'invalid', 70, 2),
  ];
}

/**
 * The dashboard's refused downloads (R-SIG-074): rows of every kind of artifact and reason with long paths, one released; the closed
 * section **Releases** opened on a repository with its releases; the dialog of **Release for this repository**. Answered in the browser.
 */
async function* refusalsWalk(page: Page): AsyncGenerator<string> {
  const list = '**/api/v1/signatures/refusals?**';
  const releases = `**/api/v1/projects/${otherProject}/repositories/nuget-internal/signatures/releases`;
  await page.route(list, (route) => route.fulfill({ json: { items: refusalRows(), nextCursor: null } }));
  await page.route(releases, (route) =>
    route.fulfill({
      json: {
        items: [
          {
            repository: `${otherProject}/nuget-internal`,
            sha256: digestOf('acme.logging.abstractions/2.1.0/acme.logging.abstractions.2.1.0.nupkg'),
            reason: 'NuGet author signatures cannot be read yet; checked by hand against the vendor certificate.',
            refusedReason: 'not_checked',
            releasedBy: 'user:noor',
            releasedAt: new Date(Date.now() - 20 * 3_600_000).toISOString(),
          },
          {
            repository: `${otherProject}/nuget-internal`,
            sha256: 'e'.repeat(64),
            reason: 'Released before the gate was switched on: the build agent image of the old pipeline.',
            refusedReason: 'missing',
            releasedBy: 'user:ada',
            releasedAt: new Date(Date.now() - 6 * 86_400_000).toISOString(),
          },
        ],
      },
    }),
  );
  await page.reload();
  await expect(page.locator('main [data-testid="signature-refusals"] tbody tr').first()).toBeVisible();
  yield '';
  const { toggle } = sectionHeaded(page, 'Releases');
  await toggle.click();
  await page.locator('#release-repository').selectOption(`${otherProject}/nuget-internal`);
  await expect(page.locator('main [data-testid="signature-releases"] tbody tr').first()).toBeVisible();
  await page.evaluate(() => Promise.all(document.getAnimations().map((a) => a.finished.catch(() => undefined))));
  yield 'releases';
  const release = page.locator('main [data-testid="signature-refusals"] td.actions button').first();
  if ((await release.count()) > 0) {
    await release.click();
    await expect(page.getByRole('dialog')).toBeVisible();
    yield 'release';
    await page.getByRole('dialog').getByRole('button', { name: 'Cancel' }).click();
  }
  await page.unroute(releases);
  await page.unroute(list);
}

/** The client setup of a repository whose gate is on with chosen anchors (R-SIG-074): the line above the snippets. */
async function* gateSetup(page: Page): AsyncGenerator<string> {
  const doc = `**/api/v1/projects/${mainProject}/repositories/${gateRepo}`;
  await answerWith(page, doc, (b) => ({
    ...b,
    signatures: { ...(b['signatures'] as Record<string, unknown>), onlyVerified: true, anchors: chosenAnchors.slice(0, 3) },
  }));
  await expect(page.locator('main [data-testid="setup-gate"]')).toBeVisible();
  yield '';
  await page.unroute(doc);
}

// ---- States the seed does not hold, answered in the browser ----

/**
 * The key of the session's storage that keeps the Details a walk opened for the page's next load; without it every load of a render
 * shows the screen as a first visit (render.spec.ts).
 */
export const KEEP_OPEN = 'quader-renders.keep-open';

/**
 * A file of a disabled repository (R-UI-193, Q-UI-965): the repository's answer, the full document and the reader's view alike, says
 * that it is disabled, so the file offers no **Download**, no download address and no download with curl, and says why once.
 */
async function* disabledFile(page: Page): AsyncGenerator<string> {
  const doc = `**/api/v1/projects/${mainProject}/repositories/assets`;
  await answerWith(page, doc, (b) => ({ ...b, disabled: true }));
  await expect(page.locator('main quader-repository-file')).toBeVisible();
  yield '';
  await page.unroute(doc);
}

/**
 * A version that no repository serves (Q-UI-965): every repository that holds it is disabled, or virtual and reaching it only through
 * a disabled member, as the details' `available` says (R-API-029), answered in the browser. The install panel offers no command and
 * names the disabled repository; the **Files** tab offers no **Download** and has no column for it.
 */
async function* disabledVersion(page: Page): AsyncGenerator<string> {
  const details = '**/api/v1/packages/*/versions/1.1.0';
  await answerWith(page, details, (b) => ({
    ...b,
    repositories: (b['repositories'] as Record<string, unknown>[]).map((r) => ({ ...r, available: false })),
  }));
  await expect(page.locator('main [data-testid="install-disabled"]')).toBeVisible();
  await expect(page.locator('main quader-install-command')).toHaveCount(0);
  yield '';
  await page.getByRole('tab', { name: 'Files' }).click();
  await expect(page.locator('main .version-tabs quader-data-table tbody tr').first()).toBeVisible();
  await expect(page.locator('main .version-tabs a[download]')).toHaveCount(0);
  yield 'files';
  await page.unroute(details);
}

/**
 * The shared error notice in each of its states (R-UI-042, G5-20), on a list whose read the walk answers in the browser: a refusal for a
 * moment (503) with **Retry**, which stands on the line of the notice's text (R-UI-175); a problem whose code the app does not know, with
 * the server's sentence, the request ID, its **Copy** and the hint to give it to an administrator; and a refusal the app words itself
 * (403), without a button. The list's own state is the data table's.
 */
async function* listUnavailable(page: Page): AsyncGenerator<string> {
  const list = (url: URL): boolean => url.pathname === '/api/v1/repositories';
  const answer = async (status: number, code: string, detail: string, title: string): Promise<void> => {
    await page.unroute(list);
    await page.route(list, (route) =>
      route.request().method() === 'GET'
        ? route.fulfill({
            status,
            headers: { 'X-Request-Id': '7f3c9a1e5b2d48c6a0e4f1b9d8c27a65' },
            contentType: 'application/problem+json',
            body: JSON.stringify({ type: 'about:blank', title, status, code, detail }),
          })
        : route.fallback(),
    );
    await page.reload();
  };
  await answer(503, 'database_unavailable', 'The database is not reachable.', 'Service Unavailable');
  await expect(page.locator('main quader-error-notice button', { hasText: 'Retry' })).toBeVisible();
  yield '';
  await answer(
    500,
    'upstream_index_inconsistent',
    'The repository index of web-frontend/npm-internal lists a version whose metadata the store does not hold; the next reconciliation pass repairs it.',
    'Internal Server Error',
  );
  await expect(page.locator('main quader-error-notice .request')).toBeVisible();
  yield 'unknown-code';
  await answer(403, 'forbidden', 'missing repositories:read', 'Forbidden');
  await expect(page.locator('main quader-error-notice', { hasText: 'You do not have the permission for this.' })).toBeVisible();
  yield 'forbidden';
  await page.unroute(list);
}

/** The answers of the walk and its notices: the error states are what the screen renders. */
const unavailable = [
  { match: /^GET \/api\/v1\/repositories\?.* answered (503|500|403)$/, why: 'the walk answers the list 503, 500 and 403 to render the error notice\'s states' },
];
const unavailableNotice = [
  { match: /Retry|Request ID|You do not have the permission for this\./, why: 'the error states of the list, which the walk renders' },
];

/**
 * The *Vulnerabilities* tab of an image its repository refuses while it is not checked (R-VULN-015, R-VULN-016, Q-VULN-272): the notice
 * names what the pull is missing and what happens next, first a platform image not scanned yet, then one whose scan stopped at a bound of
 * the scanning, the longest of the reasons. The render database scans no image, so the report is answered in the browser.
 */
async function* imageNotChecked(page: Page): AsyncGenerator<string> {
  const report = (url: URL): boolean => url.pathname.endsWith('/versions/1.4.0/vulnerabilities');
  const answer = (missingData: string, state: string, errorCode?: string) => (route: Route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        packageId: 'render',
        version: '1.4.0',
        repositories: [
          {
            repository: `${mainProject}/images`,
            evaluation: state === 'failed' ? 'scan_failed' : 'pending_scan',
            blocked: true,
            blockReason: 'missing_data',
            missingData,
            findings: [],
            grade: {
              grade: 'N/A', open: { critical: 0, high: 0, medium: 0, low: 0, none: 0, unknown: 0 }, fixable: 0, eosl: false, checkedAt: null,
              evaluation: state === 'failed' ? 'scan_failed' : 'pending_scan', repository: `${mainProject}/images`,
            },
            images: [
              { digest: `sha256:${'a1'.repeat(32)}`, platform: 'linux/amd64', state, ...(errorCode ? { errorCode } : {}), scannedAt: null },
              { digest: `sha256:${'b2'.repeat(32)}`, platform: 'linux/arm64', state: 'pending', scannedAt: null },
            ],
          },
        ],
      }),
    });
  for (const [missingData, state, errorCode, name] of [
    ['image_not_scanned', 'pending', undefined, 'not-scanned'],
    ['image_over_limits', 'failed', 'image_too_large', 'over-limits'],
  ] as const) {
    await page.unroute(report);
    await page.route(report, answer(missingData, state, errorCode));
    await page.reload();
    await expect(page.locator('quader-package-vulnerabilities .banner-warning')).toBeVisible();
    yield name;
  }
  await page.unroute(report);
}

/** A version's tab Vulnerabilities as it opens, its grade card shown, and no other tab. */
async function* vulnerabilitiesTab(page: Page): AsyncGenerator<string> {
  await expect(page.locator('quader-package-vulnerabilities quader-grade-card').first()).toBeVisible();
  yield '';
}

/**
 * The report of a project whose advisory data is stale for one ecosystem and not imported for another (R-VULN-022, Q-UI-966): an
 * administrator reads the banners with their link to the advisory sources; a reader who cannot act on them finds the same facts in
 * the report's closed section **About these counts**, with the coverage line. The database for images is not imported either, and is
 * named as what it is, never as advisory data, in its banner and in the coverage line's part for the repositories of images waiting
 * for it (Q-VULN-273).
 */
async function* staleAdvisories(page: Page): AsyncGenerator<string> {
  const summary = '**/api/v1/vulnerabilities/summary?**';
  await answerWith(page, summary, (b) => ({
    ...b,
    coverage: { ...(b['coverage'] as Record<string, number>), pendingDatabase: 1 },
    data: [
      { ecosystem: 'npm', state: 'complete', stale: true, syncedAt: new Date(Date.now() - 9 * 86_400_000).toISOString() },
      { ecosystem: 'NuGet', state: 'empty', stale: false, syncedAt: null },
      { ecosystem: 'OCI', state: 'empty', stale: false, syncedAt: null },
    ],
  }));
  yield '';
  // The section stands where the report has a coverage line; every width's load is a first visit, so it starts closed.
  const about = page.locator('main quader-disclosure.vuln-about > .disclosure > .heading > button');
  if ((await about.count()) > 0) {
    await expect(about).toHaveAttribute('aria-expanded', 'false');
    await about.click();
    await expect(about).toHaveAttribute('aria-expanded', 'true');
    yield 'about';
  }
  await page.unroute(summary);
}

/**
 * The audit log with one event opened (R-UI-037): its facts, the time on one line, the copy buttons of its IDs on their lines; then an
 * event of an action without a name of its own and a long key, on a long path, by a user's token, answered in the browser: the key
 * breaks after a dot or an underscore, the actor's kind stands under its name; the event under it has an actor without a name, whose
 * identifier is cut in its column.
 */
async function* auditEvent(page: Page): AsyncGenerator<string> {
  await page.locator('main table.data tbody th[scope="row"] button.link').first().click();
  await expect(page.locator('main section.detail dl.facts')).toBeVisible();
  yield 'event';
  // The list carries the events the panel shows: its first event becomes the long one.
  const list = (url: URL): boolean => url.pathname === '/api/v1/audit/events';
  await page.route(list, async (route) => {
    const res = await route.fetch();
    const body = (await res.json()) as { items: (Record<string, unknown> & { actor: Record<string, unknown> })[] };
    const [first, second, ...rest] = body.items;
    // The import of advisory data as the job ui-layout met it among the seed's events: an actor without a name, only its identifier,
    // and a target that is a kind and an identifier (vulnerability_source, osv:npm); the log fell to blocks at 1,440 and 1,024 px.
    const nameless = second && {
      ...second,
      action: 'vulnerability.source.imported',
      repository: null,
      path: null,
      project: null,
      resource: { type: 'vulnerability_source', id: 'osv:npm' },
      actor: { ...second.actor, name: null, id: '01a208c6-2bda-776d-a125-d83c6b9ca7ac', type: 'service_account', tokenId: null, authMethod: null },
    };
    const long = first && {
      ...first,
      action: 'storage.object_lock.retention_extended',
      repository: `${longProject}/signed-release-artifacts`,
      path: 'releases/2026/11/customer-portal-installer-with-a-long-name-for-the-windows-build-agents.exe',
      actor: { ...first.actor, type: 'user', authMethod: 'bearer' },
    };
    await route.fulfill({ response: res, json: { ...body, items: long && nameless ? [long, nameless, ...rest] : body.items } });
  });
  // The log anew, without the event of the address: the first row opens the long one.
  await page.goto('/ui/admin/audit');
  await page.locator('main table.data tbody th[scope="row"] button.link').first().click();
  await expect(page.locator('main section.detail code.fact-key')).toContainText('storage.object_lock.retention_extended');
  yield 'long-key';
  await page.unroute(list);
}

/**
 * A file's **Details** opened, then the page loaded again: the section is open as the reader left it (R-UI-067, Q-UI-319,
 * Q-UI-967), kept in the browser per user and screen. (The repository page's Details is a tab since Q-UI-1067.)
 */
async function* detailsRemembered(page: Page): AsyncGenerator<string> {
  // The file's own Details, not its section Signatures, which is a closed section of the same look.
  const toggle = page.locator('main quader-disclosure.details > .disclosure').getByRole('button', { name: 'Details', exact: true });
  // Every width's load is a first visit (render.spec.ts): the section starts closed, whatever the width before left open.
  await expect(toggle).toHaveAttribute('aria-expanded', 'false');
  await toggle.click();
  await expect(toggle).toHaveAttribute('aria-expanded', 'true');
  await page.evaluate((keep) => sessionStorage.setItem(keep, '1'), KEEP_OPEN);
  await page.reload();
  await page.waitForLoadState('networkidle');
  await expect(toggle).toHaveAttribute('aria-expanded', 'true');
  yield 'remembered';
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
/** The widths of a screen whose states an overlay alone changes (`only`, Q-UI-1231). */
const narrowAndWide: readonly number[] = [1440, 390];
/** States an overlay alone changes: at 1,440 and 390 px, in dark at 1,440 px (Q-UI-1172). */
function overlays(...names: readonly string[]): NonNullable<Screen['states']> {
  return Object.fromEntries(names.map((n) => [n, { widths: narrowAndWide, dark: [1440] }]));
}
/** A page's sections opened: at every width, in dark at 1,440 px only; a colour of a section is a colour of the page. */
const sections: NonNullable<Screen['states']> = { sections: { dark: [1440] } };
/** An editor's sections opened, and its save refused by the server: the summary above the form at 1,440 and 390 px. */
const refusedStates: NonNullable<Screen['states']> = { ...sections, ...overlays('refused') };

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
  // The administrator's columns Owners and Storage on demand (Q-UI-1041), for the holder of projects:read.
  { id: 'projects-details', path: '/ui/projects', kinds: auditors, walk: projectDetails, widths: [1100, 1280] },
  // The repositories of every project under Your work (R-UI-020, Q-UI-1040, Q-UI-1042): as each kind of user sees them, with the
  // drawer's filters open; the row menu; the rows of upstream with the badge System (R-UI-193); a filter that matches nothing.
  { id: 'all-repositories', path: '/ui/repositories', kinds: all, walk: filtersOpen(projectFilter), widths: [1100, 1280] },
  { id: 'all-repositories-menu', path: '/ui/repositories', kinds: ['admin'], walk: rowMenuOpen },
  // The filter by class System: the repository of a system-only format, its class cell System (Q-UI-1203).
  { id: 'all-repositories-system', path: '/ui/repositories?class=system', kinds: ['admin'], de: true },
  { id: 'all-repositories-upstream', path: '/ui/repositories?project=upstream', kinds: admins, walk: systemRows },
  { id: 'all-repositories-none', path: '/ui/repositories?q=zzqx', kinds: ['plain'] },
  // The search field and its list on the start page; anonymous while the seed makes a project public (R-UI-095).
  { id: 'search-field', path: '/ui/projects', kinds: ['anonymous', ...all], walk: searchField, expected: tooMany },
  // The side bar's Recently opened and the project switcher read the search's store (R-UI-096, R-UI-167); an anonymous visitor's
  // start page (R-UI-095).
  { id: 'sidebar-recent', path: '/ui/projects', kinds: ['plain', 'power'], walk: frequentPlaces },
  // The auditor's home side bar: the two context titles Your work and Security audit (R-UI-168), at 390 px in the drawer.
  { id: 'sidebar-auditor', path: '/ui/projects', kinds: ['auditor'], walk: sidebarShown },
  { id: 'projects-anonymous', path: '/ui/projects', kinds: ['anonymous'], expected: tooMany },
  // The results page of the search (R-UI-098): every tab with matches, a text without a match, a tab opened by `kind`.
  { id: 'search-results', path: '/ui/search?q=web', kinds: all, de: true },
  { id: 'search-results-none', path: '/ui/search?q=zzqx', kinds: ['plain'] },
  { id: 'search-results-kind-package', path: '/ui/search?q=acme&kind=package', kinds: all },
  { id: 'repositories', path: `${P}/repositories`, kinds: all, walk: filtersOpen(repositoryChips), widths: [1100] },
  // A list's error states (G5-20): the shared notice with Retry on its line (R-UI-175), with a request ID and Copy, and a refusal.
  { id: 'repositories-unavailable', path: `${P}/repositories`, kinds: ['plain'], walk: listUnavailable, expected: unavailable, notices: unavailableNotice },
  // The failures of a repository's page as the shared error notice shows them (R-UI-216): the owner's two 502s as one notice, a
  // gateway's error page, a server not ready, an unknown code, field problems and a rate limit.
  { id: 'repository-failures', path: `${P}/repositories/npm-internal`, kinds: ['plain'], walk: failureStates, expected: failureExpected, notices: failureNotices, de: true },
  { id: 'repositories-long-keys', path: `${O}/repositories`, kinds: all, walk: filtersOpen() },
  // The row menus of the lists that have one, open (G5-20): a project's repositories, every project's, a service account's technical
  // tokens, the exposure rules of an endpoint and the registries of upstream.
  { id: 'repositories-menu', path: `${P}/repositories`, kinds: ['power'], walk: rowMenuOpen },
  { id: 'repositories-empty-project', path: `/ui/projects/${emptyProject}/repositories`, kinds: admins },
  // The built-in project with the registries the seed switched on: the administrator sees its two system repositories in their
  // section (R-UI-192); the plain user reads the registries that are on and no system repository, so no section.
  { id: 'repositories-upstream', path: '/ui/projects/upstream/repositories', kinds: ['admin'], walk: systemRepositories },
  { id: 'repositories-upstream-plain', path: '/ui/projects/upstream/repositories', kinds: ['plain'] },
  { id: 'repository-npm-local', path: `${P}/repositories/npm-internal`, kinds: all, de: true },
  // The tab Details, the last of the strip (Q-UI-1067): a local repository's address for downloads by path, a remote's upstream and
  // address, a virtual one's members.
  { id: 'repository-npm-local-details', path: `${P}/repositories/npm-internal/details`, kinds: all, de: true },
  { id: 'repository-oci-remote-details', path: `${O}/repositories/docker-hub-remote/details`, kinds: ['plain', 'admin'], de: true },
  { id: 'repository-npm-virtual-details', path: `${P}/repositories/npm/details`, kinds: ['power'] },
  { id: 'repository-npm-remote', path: `${P}/repositories/npmjs`, kinds: all, de: true },
  // The tab Downloads of a remote (R-CUR-018): its queue as the seed left it, then a queue of every state answered in the browser,
  // a failed row opened to its whole failure, and a bulk's rows (downloads.ts). The OCI remote whose tag a probe downloaded has a
  // row of the server's own.
  { id: 'repository-npm-remote-downloads', path: `${P}/repositories/npmjs/downloads`, kinds: all, walk: downloadsWalk, de: true },
  { id: 'repository-oci-probed-downloads', path: `${O}/repositories/${probedCache.key}/downloads`, kinds: ['power'] },
  // The tab Upstream of a remote (R-CUR-018): as the seed's offline remote opens it, then answered in the browser as an npm remote
  // in mode onlyAllowed with every state (upstream.ts); an OCI remote of ghcr.io that offers only the lookup by name; a Debian remote's
  // suites and its index search; a Docker Hub namespace Docker Hub does not have (Q-CUR-165); and the OCI remote whose upstream is this
  // installation, with the server's own answers. A package's versions keep the form of a table from 1,024 px on (Q-CUR-157).
  // With the menus of an open request and of a version, for who decides (states.ts).
  {
    id: 'repository-npm-remote-upstream',
    path: `${P}/repositories/npmjs/upstream`,
    kinds: all,
    walk: withStates(upstreamTabWalk, { search: upstreamRequestMenu, package: upstreamVersionMenu }),
    states: overlays('request-menu', 'version-menu'),
    expected: upstreamExpected,
    de: true,
    tables: upstreamVersions,
  },
  { id: 'repository-oci-upstream-lookup', path: `${O}/repositories/docker-hub-remote/upstream`, kinds: ['power'], walk: upstreamLookupWalk, expected: upstreamExpected, tables: upstreamVersions },
  { id: 'repository-oci-upstream-unknown-level', path: `${O}/repositories/docker-hub-remote/upstream`, kinds: ['power'], walk: upstreamUnknownLevelWalk, de: true },
  { id: 'repository-deb-upstream', path: `${O}/repositories/debian/upstream`, kinds: ['power'], walk: upstreamDebWalk, tables: upstreamVersions },
  { id: 'repository-oci-upstream-own', path: `${O}/repositories/${remoteCache.key}/upstream`, kinds: ['power', 'admin'], walk: upstreamOwnWalk, tables: upstreamVersions },
  { id: 'repository-npm-virtual', path: `${P}/repositories/npm`, kinds: all, de: true },
  { id: 'repository-long-key', path: `${P}/repositories/${longNpmKey}`, kinds: all },
  { id: 'repository-generic', path: `${P}/repositories/assets`, kinds: all },
  // The image of twelve images and 26 tags opened (Q-UI-1130): ten rows, the highest version first, its commit tag muted beside it, and
  // the link to all its tags; then the image of six with the signed 1.4.0 (its badge on the row), a main without a version and a tag of
  // 95 characters, in German too.
  { id: 'repository-oci', path: `${P}/repositories/images`, kinds: all, walk: imageOpened('portal/worker', true) },
  { id: 'repository-oci-signed', path: `${P}/repositories/images`, kinds: ['plain'], walk: imageOpened('portal/web', false), de: true },
  // Every state of a check by its cause in the list and the opened item (R-VULN-039, Q-UI-1120): a word beside the N/A shield, the
  // sentence as the tooltip; the German words in the narrow cell.
  { id: 'repository-oci-scan-states', path: `${P}/repositories/images`, kinds: ['plain'], walk: scanStatesList, de: true },
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
  // A remote's list after a mirror pass (W-24, Q-UI-1092): the rows a pass stored, answered in the browser (mirror.ts), as a reader.
  { id: 'repository-oci-mirror-filled', path: `${O}/repositories/docker-hub-remote`, kinds: ['plain'], walk: mirrorFilled },
  { id: 'repository-deb-mirror-filled', path: `${O}/repositories/debian`, kinds: ['plain'], walk: mirrorFilled },
  // The menu of a version's commands (Q-UI-1066), hung from its button: an OCI remote's tag (Pull), an npm package's version and a
  // Debian package's version (Install).
  { id: 'repository-oci-cache-menu', path: `${O}/repositories/${remoteCache.key}`, kinds: ['power'], walk: commandMenuOpen, de: true },
  { id: 'repository-npm-vendor-menu', path: `${P}/repositories/npm-vendor`, kinds: ['plain'], walk: commandMenuOpen, de: true },
  { id: 'repository-deb-menu', path: `${O}/repositories/${longDebKey}`, kinds: ['power'], walk: commandMenuOpen, de: true },
  { id: 'repository-nuget-virtual', path: `${O}/repositories/nuget`, kinds: all },
  { id: 'repository-ansible', path: `${O}/repositories/collections`, kinds: all },
  { id: 'packages', path: `${P}/packages`, kinds: all, walk: filtersOpen(packageChips) },
  { id: 'packages-other-project', path: `${O}/packages`, kinds: all, walk: filtersOpen() },
  // A package's page is its versions (R-UI-110), a version's page its findings, install command, files and details (R-UI-111).
  { id: 'package-npm', path: pkg(P, npmPackage), kinds: all },
  { id: 'package-npm-many', path: pkg(P, manyVersionsPackage), kinds: all, walk: versionsTable },
  { id: 'package-npm-graded', path: pkg(P, gradedPackage), kinds: all },
  { id: 'package-npm-long-name', path: pkg(P, longNamePackage), kinds: ['plain'] },
  { id: 'package-oci', path: pkg(P, ociPackage), kinds: all, de: true },
  // An image's page of twelve images: every tag in its row, the header's count of tags, the filter by any tag (Q-UI-1130).
  { id: 'package-oci-many', path: pkg(P, { format: 'oci', name: 'portal/worker' }), kinds: ['plain'], walk: imageTagsFiltered, de: true },
  { id: 'package-nuget', path: pkg(O, nugetPackage), kinds: all },
  { id: 'package-deb', path: pkg(O, debPackage), kinds: all },
  { id: 'package-ansible', path: pkg(O, { format: 'ansible', name: collectionName }), kinds: all },
  { id: 'package-version-npm', path: ver(P, npmPackage, '1.1.0'), kinds: all, de: true },
  { id: 'package-version-npm-graded', path: ver(P, gradedPackage, gradedPackage.version), kinds: all },
  { id: 'package-version-npm-many', path: ver(P, manyVersionsPackage, '3.7.0'), kinds: ['plain'] },
  { id: 'package-version-npm-fixed', path: ver(P, { format: 'npm', name: 'xml-stream-parser' }, '0.9.2'), kinds: ['plain'] },
  // A finding rated None (G5-55): its badge on the version's tab of findings.
  { id: 'package-version-npm-none', path: `${ver(P, { format: 'npm', name: 'tiny-colors' }, '1.2.0')}?tab=vulnerabilities`, kinds: ['plain'] },
  { id: 'package-version-npm-long-name', path: ver(P, longNamePackage, '0.4.0'), kinds: ['plain'] },
  // Its repositories disabled: no command, no Download, one sentence (Q-UI-965), as the file of a disabled repository.
  { id: 'package-version-disabled', path: ver(P, npmPackage, '1.1.0'), kinds: ['plain'], walk: disabledVersion },
  // The tab's findings narrowed by a segment of the card's bar (Q-UI-379).
  { id: 'package-version-npm-graded-critical', path: `${ver(P, gradedPackage, gradedPackage.version)}?severity=critical`, kinds: ['plain'] },
  { id: 'package-version-oci', path: ver(P, ociPackage, '1.4.0'), kinds: all, tables: 'quader-data-table.version-files', de: true },
  // An image refused while it is not checked: the notice names the state of its scan and what happens next (Q-VULN-272).
  { id: 'package-version-oci-not-checked', path: `${ver(P, ociPackage, '1.4.0')}?tab=vulnerabilities`, kinds: ['plain'], walk: imageNotChecked, de: true },
  // An image the render database has no database for images to scan with, in a repository that does not block it: the card says it
  // waits for that database, never for advisory data (Q-VULN-273). The tab alone: the other tabs are package-version-oci's.
  { id: 'package-version-oci-unscanned', path: `${ver(P, ociPackage, '1.4.0')}?tab=vulnerabilities`, kinds: ['plain'], walk: vulnerabilitiesTab, de: true },
  // A failed scan on the card: its class in words, its code under Technical details (Q-UI-1120).
  { id: 'package-version-oci-scan-failed', path: `${ver(P, ociPackage, '1.4.0')}?tab=vulnerabilities`, kinds: ['plain'], walk: scanFailedCard, de: true },
  // The package page's versions, every state in a row, the column kept (Q-UI-1121).
  { id: 'package-oci-scan-states', path: pkg(P, ociPackage), kinds: ['plain'], walk: scanStatesPackage, de: true },
  // The image a remote cached (W-24): its versions, and the tags whose index it holds one platform of and none of, with their platforms.
  { id: 'package-oci-cache', path: pkg(O, { format: 'oci', name: remoteImage.name }), kinds: all },
  { id: 'package-version-oci-cache-partial', path: `${ver(O, { format: 'oci', name: remoteImage.name }, remoteImage.partial)}?tab=details`, kinds: all },
  { id: 'package-version-oci-cache-unknown', path: `${ver(O, { format: 'oci', name: remoteImage.name }, remoteImage.unknown)}?tab=details`, kinds: ['plain'] },
  // A cached tag's file is the manifest document it names, with its malware scan, as a local tag's (R-MAL-024, Q-MAL-111).
  {
    id: 'package-version-oci-cache-files',
    path: `${ver(O, { format: 'oci', name: remoteImage.name }, remoteImage.single)}?tab=files`,
    kinds: ['plain'],
    de: true,
    tables: 'quader-data-table.version-files',
  },
  // The tag a background download stored (W-25): its platforms, two stored and one not fetched.
  { id: 'package-version-oci-probed', path: `${ver(O, { format: 'oci', name: remoteImage.name }, probedCache.tag)}?tab=details`, kinds: all },
  { id: 'package-version-nuget', path: ver(O, nugetPackage, nugetPackage.versions[1]), kinds: all },
  { id: 'package-version-deb', path: ver(O, debPackage, debPackage.versions[1]), kinds: all, de: true },
  // In German as the plain user: the strip of the most tabs with the longest labels of every format's version page, the one strip of
  // in-place tabs they share (Q-UI-1090: 13 px wider than its place at 1,024 px before its tabs stood closer).
  { id: 'package-version-ansible', path: ver(O, { format: 'ansible', name: collectionName }, ansiblePackage.versions[1]), kinds: all, de: ['plain'] },
  // The folders and files of a repository on its page (R-UI-010 to R-UI-012, ADR-203): a folder with the Owner's operations, a
  // folder deep in a path of long names, a folder of more than a page, an empty folder, a file and a package format's raw files.
  { id: 'files-folder', path: `${P}/repositories/assets/files/releases/design-system/2.5.0`, kinds: all, walk: folderOperations },
  { id: 'files-deep', path: `${P}/repositories/assets/files/${deepFolder}`, kinds: all, walk: deepPath },
  { id: 'files-many', path: `${P}/repositories/assets/files/icons/material`, kinds: ['plain'], walk: manyEntries },
  { id: 'files-empty-folder', path: `${P}/repositories/assets/files/drafts`, kinds: all },
  { id: 'files-file', path: `${P}/repositories/assets/files/README.md`, kinds: all, walk: fileView },
  // Details stays open across a visit (Q-UI-319, Q-UI-967).
  { id: 'file-details-remembered', path: `${P}/repositories/assets/files/README.md`, kinds: ['power'], walk: detailsRemembered },
  // A file of a disabled repository: no Download, no address, one sentence (Q-UI-965).
  { id: 'files-file-disabled', path: `${P}/repositories/assets/files/README.md`, kinds: all, walk: disabledFile },
  { id: 'files-file-long', path: `${P}/repositories/assets/files/${deepFolder}/sprite-with-every-icon-of-the-design-system-for-the-customer-portal.svg`, kinds: ['plain'] },
  { id: 'files-npm', path: `${P}/repositories/npm-internal/files/@acme/ui-kit/-`, kinds: all },
  { id: 'files-npm-file', path: `${P}/repositories/npm-internal/files/@acme/ui-kit/-/ui-kit-1.1.0.tgz`, kinds: owners },
  { id: 'files-oci', path: `${P}/repositories/images/files`, kinds: ['plain'] },
  // A file of a system repository (R-UI-193): what only an administrator reaches.
  {
    id: 'files-system',
    path: `/ui/projects/upstream/repositories/clamav/files/${systemFile}`,
    kinds: admins,
    walk: systemFileView,
    expected: [
      {
        match: /^GET \/api\/v1\/projects\/upstream\/repositories\/clamav\/items\?path=\S+ answered 404$/,
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
    de: true,
  },
  { id: 'package-version-malware', path: `${ver(P, malwarePackage, '2.3.0')}?tab=files`, kinds: ['plain'], walk: malwareVersions, tables: 'quader-data-table.version-files' },
  // A version whose Go program passed the scan-size limit (Q-MAL-105, Q-MAL-106): the banner names the limit, the cell its value and,
  // unfolded, the scanner's own name of it and the member; in German too.
  {
    id: 'package-version-malware-limit',
    path: `${ver(P, malwarePackage, '2.4.0-rc.1')}?tab=files`,
    kinds: ['plain'],
    walk: malwareLimitVersion,
    tables: 'quader-data-table.version-files',
    de: true,
  },
  // A file whose malware scan waits, by why (Q-UI-1123): queued with its time, no scanner, no signatures.
  {
    id: 'package-version-malware-waiting',
    path: `${ver(P, malwarePackage, '2.3.0')}?tab=files`,
    kinds: ['plain'],
    walk: malwareWaiting,
    tables: 'quader-data-table.version-files',
    de: true,
  },
  // The signatures at a version, an image and a file (R-SIG-035, R-SIG-024, R-SIG-010): what a plain user meets; the same for every kind.
  { id: 'package-version-signatures', path: `${ver(P, manyVersionsPackage, signedVersions.public)}?tab=signatures`, kinds: ['plain'], walk: signatureVersions },
  { id: 'package-version-signatures-files', path: `${ver(P, manyVersionsPackage, signedVersions.public)}?tab=files`, kinds: ['plain'], tables: 'quader-data-table.version-files' },
  // The widest signature state in the files' table, **Signed** with "signer not trusted" after it (a blocked signer), beside the malware
  // scan's state and the download: the table keeps its form at 1,024 px (Q-UI-969, Q-UI-1002), and in German at 390 px the words
  // stay inside the cell (Q-UI-1076).
  {
    id: 'package-version-signatures-untrusted-files',
    path: `${ver(P, manyVersionsPackage, signedVersions.blocked)}?tab=files`,
    kinds: ['plain'],
    walk: untrustedFiles,
    tables: 'quader-data-table.version-files',
    de: true,
  },
  { id: 'package-version-signatures-oci', path: `${ver(P, ociPackage, signedImage.tag)}?tab=signatures`, kinds: ['plain'], walk: signatureImages },
  // A repository's gate at a version, an image and a file (R-SIG-074): what a plain user meets, and the release for an administrator.
  { id: 'package-version-signature-refused', path: `${ver(P, manyVersionsPackage, signedVersions.none)}?tab=files`, kinds: ['plain', 'admin'], walk: refusedVersionWalk },
  { id: 'package-version-signature-gate-oci', path: `${ver(P, ociPackage, signedImage.tag)}?tab=signatures`, kinds: ['plain'], walk: gateImage },
  { id: 'files-signature-refused', path: `${P}/repositories/${signedFile.repo}/files/${signedFile.folder}/${signedFile.name}`, kinds: ['plain', 'admin'], walk: refusedFileWalk },
  { id: 'files-signatures', path: `${P}/repositories/${signedFile.repo}/files/${signedFile.folder}/${signedFile.name}`, kinds: ['plain'], walk: signatureFile },
  { id: 'vulnerabilities', path: `${P}/vulnerabilities`, kinds: all, walk: filtersOpen(findingChips), tables: 'quader-vulnerability-report > quader-data-table' },
  // Stale and missing advisory data: the administrator's banners, the reader's closed section (Q-UI-966); the database for images by
  // its own name (Q-VULN-273).
  { id: 'vulnerabilities-stale', path: `${P}/vulnerabilities`, kinds: ['plain', 'admin'], walk: staleAdvisories, de: true },
  // A filter is a chip beside Filters; the list sorts by Detected at every width, at 390 px from the sort row above its stacked rows (Q-UI-391).
  { id: 'vulnerabilities-filtered', path: `${P}/vulnerabilities?severity=high`, kinds: admins, walk: sortByDetection, tables: 'quader-vulnerability-report > quader-data-table' },
  { id: 'members', path: `${P}/members`, kinds: all },
  // The security dashboard under Security audit (R-UI-158, R-UI-159): its title, the line of the scope, the counts, the trend, then
  // the list with its one search; the drawer with the projects; the chips; a text in the search; a finding opened from it.
  { id: 'security', path: '/ui/security', kinds: auditors, walk: filtersOpen(securityChips), tables: 'quader-vulnerability-report > quader-data-table' },
  { id: 'security-search', path: '/ui/security', kinds: ['auditor'], walk: searched('DEMO-2026-00001') },
  { id: 'security-project', path: `/ui/security?project=${mainProject}`, kinds: ['auditor'], tables: 'quader-vulnerability-report > quader-data-table' },
  // The page Malware under Security audit (R-MAL-025, Q-UI-1005): what the auditor assesses and releases; an administrator sees the same.
  { id: 'security-malware', path: '/ui/security/malware', kinds: ['auditor'], walk: malwareFinds, tables: 'quader-malware-finds > quader-data-table' },
  // The find of that Go program and its release (Q-UI-1206): the limit, the false alarm with the manual's page, the scanner's text.
  {
    id: 'security-malware-limit',
    path: '/ui/security/malware?q=MaxScanSize',
    kinds: ['auditor'],
    walk: malwareLimitFind,
    tables: 'quader-malware-finds > quader-data-table',
    de: true,
  },
  // The page Signatures under Security audit (R-SIG-038, Q-UI-1005): invalid signatures and blocked signers; an administrator sees the same.
  { id: 'security-signatures', path: '/ui/security/signatures', kinds: ['auditor'], walk: signatureFindings, tables: 'quader-signature-findings > quader-data-table' },
  // The refused downloads and the releases of the gates (R-SIG-074), with the release's dialog for both who triage.
  { id: 'security-signature-refusals', path: '/ui/security/signatures', kinds: auditors, walk: refusalsWalk },
  // Set up client of a repository (R-UI-120): every format and class of the seed, the long keys, and a public repository signed out.
  { id: 'setup-npm-local', path: `${P}/repositories/npm-internal/setup`, kinds: all, walk: setupVerify },
  { id: 'setup-npm-remote', path: `${P}/repositories/npmjs/setup`, kinds: all },
  { id: 'setup-npm-virtual', path: `${P}/repositories/npm/setup`, kinds: all, walk: setupWalk, de: true },
  // The snippets of a Bastion (R-BAS-007, R-UI-121, R-UI-123): no Publish step, the banner, no explanation. The seed's registration is
  // offered under Install from to who may read the registrations; the others get the Bastion the address names.
  { id: 'setup-npm-bastion', path: `${P}/repositories/npm/setup?bastion=${seededBastion.name}`, kinds: all, de: true },
  { id: 'setup-long-key', path: `${P}/repositories/${longNpmKey}/setup`, kinds: all },
  { id: 'setup-generic', path: `${P}/repositories/assets/setup`, kinds: all, walk: setupVerify },
  { id: 'setup-oci-local', path: `${P}/repositories/images/setup`, kinds: all, walk: setupVerify, de: true },
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
  { id: 'profile', path: '/ui/profile', kinds: all, de: true },
  { id: 'profile-tokens', path: '/ui/profile/tokens', kinds: all },
  // The plain user holds a token (setup.ts): its revoke-all, last on the page behind Advanced.
  { id: 'profile-tokens-advanced', path: '/ui/profile/tokens', kinds: ['plain'], walk: advancedOpen },
  // A tooltip open over the token's Rotate (Q-UI-1065): the one screen that shows the bubble.
  { id: 'profile-tokens-tooltip', path: '/ui/profile/tokens', kinds: ['plain'], walk: tooltipOpen, de: true },
  { id: 'profile-sessions', path: '/ui/profile/sessions', kinds: all, de: true },
  // A user's own usage and limits (R-UI-185).
  { id: 'profile-usage', path: '/ui/profile/usage', kinds: all, walk: usageDetailWalk },
  { id: 'not-found', path: '/ui/no-such-page', kinds: all },

  // What an Owner of the project maintains, and the settings a reader is offered in the list.
  // The settings page of each class, standard sections open and every detail opened in a second picture (R-UI-087).
  { id: 'repository-settings-npm-local', path: `${P}/repositories/npm-internal/settings`, kinds: all, walk: allSections },
  { id: 'repository-settings-npm-remote', path: `${P}/repositories/npmjs/settings`, kinds: all, walk: remoteSections },
  {
    id: 'repository-settings-npm-mirror-unbounded',
    path: `${P}/repositories/npmjs/settings`,
    kinds: owners,
    walk: mirrorUnbounded,
  },
  // The section Delivery opened by the tab Upstream's Change (R-CUR-018): the mode, the examination's switch and its days; the
  // remote guidedtraffic delivers only what is allowed.
  { id: 'repository-settings-npm-delivery', path: `${P}/repositories/npmjs/settings#delivery`, kinds: owners },
  { id: 'repository-settings-oci-delivery', path: `${O}/repositories/guidedtraffic/settings#delivery`, kinds: admins },
  // The section Kept in sync of a remote (R-CUR-027): the interval, the pause and the byte bound; editable for who may write the
  // configuration, read-only for the plain user.
  { id: 'repository-settings-npm-syncs', path: `${P}/repositories/npmjs/settings#syncs`, kinds: all, de: true },
  { id: 'repository-settings-npm-virtual', path: `${P}/repositories/npm/settings`, kinds: all, walk: allSections },
  { id: 'repository-settings-generic', path: `${P}/repositories/assets/settings`, kinds: owners, walk: allSections },
  { id: 'repository-settings-generic-advanced', path: `${P}/repositories/assets/settings`, kinds: admins, walk: advancedOpen },
  { id: 'repository-settings-oci', path: `${P}/repositories/images/settings`, kinds: all, walk: allSections },
  { id: 'repository-new', path: `${P}/repositories/new`, kinds: owners, walk: chooser, de: ['power'] },
  // The form's address with a system-only format: no form, the sentence and its repository's link, the chooser (Q-UI-1201).
  { id: 'repository-new-system-only', path: `${P}/repositories/new/clamav`, kinds: ['power', 'admin'], de: ['admin'] },
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
  { id: 'project-settings-limits', path: `${P}/settings`, kinds: owners, walk: limitsSectionOpen, tables: 'quader-limit-rules quader-data-table' },
  // The project's trust anchors (R-SIG-037): the Owner's form, a member's read, and the switch off.
  { id: 'project-settings-anchors', path: `${P}/settings`, kinds: [...owners, 'plain'], walk: projectAnchors, tables: 'quader-anchor-table > quader-data-table' },
  { id: 'repository-settings-limits', path: `${P}/repositories/npmjs/settings`, kinds: owners, walk: limitsSectionOpen, tables: 'quader-limit-rules quader-data-table' },
  { id: 'project-usage', path: `${P}/usage`, kinds: owners, walk: usageWalk },
  { id: 'project-usage-repository', path: `${P}/usage?repository=npmjs`, kinds: owners, walk: limitsModes },
  { id: 'project-usage-caller', path: `${P}/usage/user/${kinds.power}`, kinds: owners, walk: usageDetailWalk },
  { id: 'repository-settings-restricted', path: `${P}/repositories/npm-internal/settings`, kinds: owners, walk: restrictedSettings },
  // The strict repository's section Malware scan (R-MAL-020): its Owner may not switch it off, an administrator may.
  { id: 'repository-settings-malware', path: `${P}/repositories/${malwarePackage.repo}/settings`, kinds: owners, walk: malwareSection },
  // >>> download-links
  // The repository's section Download links (R-UI-201): the installation's switch off and on, and a format not every client of which checks.
  { id: 'repository-settings-links', path: `${P}/repositories/images/settings`, kinds: owners, walk: downloadLinksSection },
  // <<< download-links
  // The fetch of a remote's signatures (R-SIG-040): a project's remote, and Docker Hub of the project upstream, an administrator's.
  { id: 'repository-settings-signatures', path: `${P}/repositories/npmjs/settings`, kinds: owners, walk: signaturesSection },
  // The gate of a repository (R-SIG-074): off, the question before switching on, any anchor, chosen anchors with a removed one (read-only
  // for the Owner where loosening is not hers), the checks switched off. A Debian repository's section is in repository-settings-deb.
  {
    id: 'repository-settings-signature-gate',
    path: `${P}/repositories/${gateRepo}/settings`,
    kinds: owners,
    walk: gateSection,
    expected: [{ match: /^GET \/api\/v1\/projects\/[^/]+\/repositories\/assets\/signatures\/preview answered 409$/, why: 'the walk answers the preview as an installation whose signature checks are switched off' }],
  },
  { id: 'setup-signature-gate', path: `${P}/repositories/${gateRepo}/setup`, kinds: owners, walk: gateSetup },
  { id: 'repository-settings-signatures-docker-hub', path: '/ui/projects/upstream/repositories/docker-hub/settings', kinds: admins, walk: signaturesSection },
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
  // The section Mirror of an OCI and a Debian remote switched to the mode mirror, with the format's own mirror settings (G5-65).
  { id: 'repository-settings-oci-mirror', path: `${O}/repositories/docker-hub-remote/settings`, kinds: ['admin'], walk: mirrorMode },
  { id: 'repository-settings-deb-mirror', path: `${O}/repositories/debian/settings`, kinds: ['admin'], walk: mirrorMode },
  // The same remotes stored in the mode mirror, a bounded one and one without its bound (section 12, part C; Q-UI-1061).
  // With the menu of Run a pass now (states.ts), once: the Debian remote's is the same.
  {
    id: 'repository-settings-oci-mirror-stored',
    path: `${O}/repositories/docker-hub-remote/settings`,
    kinds: ['admin'],
    walk: withStates(storedMirror, { stored: mirrorRunMenu }),
    states: overlays('run-menu'),
  },
  { id: 'repository-settings-deb-mirror-stored', path: `${O}/repositories/debian/settings`, kinds: ['admin'], walk: storedMirror },
  { id: 'repository-settings-ansible', path: `${O}/repositories/collections/settings`, kinds: all, walk: allSections },
  { id: 'repository-settings-ansible-remote', path: `${O}/repositories/galaxy/settings`, kinds: admins, walk: remoteSections },
  { id: 'repository-settings-ansible-virtual', path: `${O}/repositories/ansible/settings`, kinds: admins, walk: allSections },
  { id: 'help-admin-page', path: '/ui/help/admin/repositories', kinds: admins },
  { id: 'admin-dashboard', path: '/ui/admin/dashboard', kinds: admins, walk: dashboardStorage },
  // The top bar's Downloads (R-UI-210 to R-UI-212), answered in the browser (upstream-downloads.ts): the button with nothing running,
  // with 3 and with 140 in the main and in the admin area; the panel with five running and 100 finished, the filter set, everything
  // hidden, a refresh that failed and both sections empty; a plain user and a project's Owner get no button.
  { id: 'topbar-downloads', path: '/ui/projects', kinds: auditors, walk: downloadsButtonWalk },
  { id: 'topbar-downloads-admin-area', path: '/ui/admin/dashboard', kinds: admins, walk: downloadsButtonWalk },
  { id: 'topbar-downloads-panel', path: '/ui/projects', kinds: auditors, walk: downloadsPanelWalk, expected: downloadsExpected, de: true },
  { id: 'topbar-downloads-none', path: '/ui/projects', kinds: ['plain', 'power'] },
  // The admin area's side bar without Projects and Repositories (Q-UI-1040), in the drawer at 390 px.
  { id: 'sidebar-admin', path: '/ui/admin/dashboard', kinds: admins, walk: adminSidebarShown },
  // The public registries Quader offers (R-UI-187, R-UI-188): the list grouped by format, and a Debian archive's page, the one with
  // the most to lay out (its archive keys). The member editor's placeholder is in repository-settings-oci-virtual.
  { id: 'admin-upstream', path: '/ui/admin/upstream', kinds: admins, walk: registryPresetsWalk },
  // The section of the installation's registries without any (R-UI-195): the list answered empty by the route.
  { id: 'admin-upstream-presets-empty', path: '/ui/admin/upstream', kinds: ['admin'], walk: registryPresetsEmpty },
  // The row of clamav in System repositories with its switch ClamAV definition updates (R-UI-187, Q-UI-903): on as the seed left it,
  // the question before switching off, and off.
  { id: 'admin-upstream-clamav-row', path: '/ui/admin/upstream', kinds: admins, walk: clamavRow },
  // The toasts (R-UI-042, Q-UI-1063): one registry switched on, a second whose toast replaces the first's, and the toasts that keep
  // their place beside it, a notice with an action and an error.
  { id: 'admin-upstream-toasts', path: '/ui/admin/upstream', kinds: ['admin'], walk: toastsWalk, de: true },
  { id: 'admin-upstream-registry', path: '/ui/admin/upstream/debian', kinds: admins },
  // Docker Hub's account (R-REPO-077, R-UI-188, Q-UI-1160 to Q-UI-1162): none, with the sentence that an account allows more; one the Helm
  // values declare, read-only with its files readable; and one whose Secret is missing on the server, answered in the browser
  // (upstream-accounts.ts).
  { id: 'admin-upstream-docker-hub', path: '/ui/admin/upstream/docker-hub', kinds: auditors, walk: registryAccountWalk, de: true },
  // The offer of an account after the sentence of Docker Hub's limit (Q-UI-1160), as its three kinds of reader read it: the administrator
  // of the well-known registry and its reader, and the writer and the reader of a project's remote of Docker Hub.
  { id: 'upstream-docker-hub-downloads-account', path: '/ui/projects/upstream/repositories/docker-hub/downloads', kinds: auditors, walk: accountOfferWalk, de: true },
  { id: 'repository-docker-hub-downloads-account', path: `${O}/repositories/docker-hub-remote/downloads`, kinds: all, walk: accountOfferWalk, de: true },
  { id: 'admin-upstream-menu', path: '/ui/admin/upstream', kinds: ['admin'], walk: rowMenuOpen },
  // The system repository of the image scanning's database: in use while the scanning reads through it (R-REPO-070, R-VULN-044).
  { id: 'admin-upstream-scanner-trivy', path: '/ui/admin/upstream/scanner-trivy', kinds: admins },
  // ClamAV definition updates on the page of clamav (R-UI-188, Q-UI-903, Q-UI-905): off, the question, on at Public with its fold and the
  // token's disclosure open, on at Internal with an endpoint of the client certificates (R-CLAMAV-010), and on at Internal where the
  // installation allows no public level.
  { id: 'admin-upstream-clamav', path: '/ui/admin/upstream/clamav', kinds: admins, walk: clamavRegistry },
  // The ClamAV definitions as their reader sees them while the switch is on (R-UI-194, R-CLAMAV-006): the databases with the version in
  // use, a database's versions with their states, a version's files with its members and its details, and the client setup.
  { id: 'clamav-databases', path: '/ui/projects/upstream/repositories/clamav', kinds: ['plain', 'admin'], walk: clamavDatabases, expected: definitionsExpected, de: ['admin'] },
  { id: 'clamav-database', path: '/ui/projects/upstream/packages/clamav/daily', kinds: ['plain', 'admin'], walk: definitionsWith('on'), expected: definitionsExpected },
  { id: 'clamav-version-files', path: '/ui/projects/upstream/packages/clamav/daily@28190?tab=files', kinds: ['plain', 'admin'], walk: definitionsWith('on'), expected: definitionsExpected },
  { id: 'clamav-version-details', path: '/ui/projects/upstream/packages/clamav/daily@28190?tab=details', kinds: ['plain', 'admin'], walk: definitionsWith('on', openChecksums), expected: definitionsExpected },
  // Its client setup as it opens, and with the guide's sections open: the four ways of R-CLAMAV-006 (public, a token, a client
  // certificate on the profile's endpoint that admits one, without freshclam).
  { id: 'clamav-setup', path: '/ui/projects/upstream/repositories/clamav/setup', kinds: ['plain', 'admin'], walk: clamavSetupWalk },
  { id: 'admin-users', path: '/ui/admin/users', kinds: admins, walk: filtersOpen(userChips) },
  { id: 'admin-user', path: '/ui/admin/users/sam', kinds: admins },
  // The break-glass account: no Advanced action, so the page asks the token list whether it offers Revoke all.
  { id: 'admin-user-break-glass', path: '/ui/admin/users/admin', kinds: admins },
  { id: 'admin-user-advanced', path: '/ui/admin/users/sam', kinds: admins, walk: advancedOpen },
  { id: 'admin-groups', path: '/ui/admin/groups', kinds: admins, walk: filtersOpen() },
  { id: 'admin-group', path: `/ui/admin/groups/${group}`, kinds: admins, walk: principalSearch('Add a member', 'ad') },
  { id: 'admin-service-accounts', path: '/ui/admin/service-accounts', kinds: admins, walk: searched('release') },
  { id: 'admin-service-account', path: `/ui/admin/service-accounts/${serviceAccount}`, kinds: admins, walk: technicalTokens },
  { id: 'admin-service-account-menu', path: `/ui/admin/service-accounts/${serviceAccount}`, kinds: ['admin'], walk: rowMenuOpen },
  { id: 'admin-permissions', path: '/ui/admin/permissions', kinds: admins, walk: filtersOpen() },
  { id: 'admin-permission-new', path: '/ui/admin/permissions/new', kinds: admins },
  { id: 'admin-roles', path: '/ui/admin/roles', kinds: admins, walk: principalSearch('Bind to', 'sa') },
  { id: 'admin-tokens', path: '/ui/admin/tokens', kinds: admins, walk: filtersOpen() },
  // The usage and the download limits (R-UI-177 to R-UI-184): the administrator changes, the auditor reads.
  { id: 'admin-usage', path: '/ui/admin/usage', kinds: auditors, walk: usageWalk },
  { id: 'admin-usage-caller', path: `/ui/admin/usage/user/${kinds.power}`, kinds: auditors, walk: usageDetailWalk },
  // A ClamAV installation's certificate subject: its certificates by serial under By credential (R-UI-184, R-LIM-027).
  { id: 'admin-usage-caller-certificate', path: `/ui/admin/usage/certificate/${encodeURIComponent(certificateSubject)}`, kinds: ['admin'] },
  // A blocked anonymous address and a CI project as callers (R-UI-184): the block's notice, a subject without credentials.
  { id: 'admin-usage-caller-blocked', path: `/ui/admin/usage/anonymous/${blockedAddress}`, kinds: admins, walk: limitsModes },
  { id: 'admin-usage-caller-ci', path: `/ui/admin/usage/ci/${ciIssuer}:${ciProject.id}`, kinds: admins },
  { id: 'admin-limits', path: '/ui/admin/limits', kinds: auditors, walk: limitRulesWalk, tables: 'quader-limit-rules quader-data-table' },
  { id: 'admin-limits-profiles', path: '/ui/admin/limits/profiles', kinds: auditors, de: true },
  { id: 'admin-limits-profile', path: '/ui/admin/limits/profiles/standard', kinds: auditors },
  { id: 'admin-limits-profile-unlimited', path: '/ui/admin/limits/profiles/unlimited', kinds: admins },
  { id: 'admin-limits-profile-new', path: '/ui/admin/limits/profiles/new', kinds: admins },
  { id: 'admin-limits-projects', path: '/ui/admin/limits/projects', kinds: auditors },
  { id: 'admin-limits-blocks', path: '/ui/admin/limits/blocks', kinds: auditors },
  { id: 'admin-ci', path: '/ui/admin/ci', kinds: admins },
  { id: 'admin-ci-new', path: '/ui/admin/ci/new', kinds: admins },
  { id: 'admin-ci-issuances', path: '/ui/admin/ci/issuances', kinds: admins, walk: filtersOpen() },
  // The direct reads from the object store on a registration's card (R-BAS-071), answered in the browser (bastions.ts).
  { id: 'admin-bastions', path: '/ui/admin/bastions', kinds: admins, walk: bastionsHandover, tables: 'quader-bastion-card quader-data-table' },
  { id: 'admin-bastions-read', path: '/ui/admin/bastions', kinds: ['auditor'], walk: bastionsRead, tables: 'quader-bastion-card quader-data-table' },
  { id: 'admin-endpoints', path: '/ui/admin/endpoints', kinds: admins },
  // The endpoint every installation has (R-EP-001).
  { id: 'admin-endpoint', path: '/ui/admin/endpoints/default', kinds: admins },
  { id: 'admin-endpoint-menu', path: '/ui/admin/endpoints/default', kinds: ['admin'], walk: rowMenuOpen },
  { id: 'admin-endpoint-rule-new', path: '/ui/admin/endpoints/default/rules/new', kinds: admins },
  // With a change the server refuses: the summary of its problems above the form (states.ts).
  {
    id: 'admin-endpoint-rule',
    path: `/ui/admin/endpoints/default/rules/${exposureRule}`,
    kinds: admins,
    walk: withStates(advancedOpen, { advanced: ruleRefused }),
    states: { refused: { widths: [1440, 390], dark: [1440] } },
    expected: [{ match: /^(PUT|PATCH) \/api\/v1\/endpoints\/default\/exposure-rules\/\S+ answered 422$/, why: 'the save the walk refuses with problems' }],
  },

  // The creation form of every format with settings in every class (the owner's rules of 2026-11-12, CLAUDE.md).
  ...createForms,
  ...createStates,
  // The upstream of a remote where it is entered: the presets, the credentials, Test connection, the details (R-UI-082).
  ...upstreams,
  // At 1,280 px too: the log scrolled sideways there (its Source address column).
  { id: 'admin-audit', path: '/ui/admin/audit', kinds: admins, walk: filtersOpen(auditResource), widths: [1280], tables: 'quader-audit-log quader-data-table' },
  // One event opened: its time on one line at 390 px, the copy buttons of its IDs on their lines (R-UI-037).
  { id: 'admin-audit-event', path: '/ui/admin/audit', kinds: admins, walk: auditEvent, widths: [1280], tables: 'quader-audit-log quader-data-table' },
  // The traces of data keys handed to a Bastion (storage.md R-STOR-096), answered in the browser: the list and one opened.
  { id: 'admin-audit-data-keys', path: '/ui/admin/audit', kinds: admins, walk: auditDataKeys, tables: 'quader-audit-log quader-data-table' },
  { id: 'admin-advisory-sources', path: '/ui/admin/advisory-sources', kinds: admins, walk: databaseCommandsOpen },
  // The auditor reads the sources and the image scanners (settings:read, R-OPS-048).
  { id: 'admin-advisory-sources-read', path: '/ui/admin/advisory-sources', kinds: ['auditor'] },
  { id: 'admin-system', path: '/ui/admin/system', kinds: admins, walk: systemScanners },
  // The auditor reads the replicas and the scanner replicas (settings:read, R-OPS-048).
  { id: 'admin-system-read', path: '/ui/admin/system', kinds: ['auditor'] },
  { id: 'admin-system-jobs', path: '/ui/admin/system/jobs', kinds: admins, de: true },
  { id: 'admin-system-backups', path: '/ui/admin/system/backups', kinds: admins },
  { id: 'admin-system-storage', path: '/ui/admin/system/storage', kinds: admins, walk: systemStorage },
  // In German: the longest word of a heading, "Authentifizierungseinstellungen", which broke inside itself at 390 px (Q-UI-1091).
  { id: 'admin-settings-auth', path: '/ui/admin/settings/auth', kinds: admins, de: true },
  { id: 'admin-settings-general', path: '/ui/admin/settings/general', kinds: admins, walk: restrictedLevels },
  // The malware scanning's page (R-MAL-023): every state of the administrator's, and the auditor's read-only page with the link to the finds.
  { id: 'admin-settings-malware', path: '/ui/admin/settings/malware', kinds: admins, walk: malwareSettings },
  { id: 'admin-settings-malware-read', path: '/ui/admin/settings/malware', kinds: ['auditor'] },
  { id: 'admin-settings-signatures', path: '/ui/admin/settings/signatures', kinds: admins, walk: signatureSettings, tables: 'quader-data-table' },
  // >>> download-links
  { id: 'admin-settings-storage', path: '/ui/admin/settings/storage', kinds: admins, walk: storageSettings },
  { id: 'admin-settings-storage-read', path: '/ui/admin/settings/storage', kinds: ['auditor'] },
  // <<< download-links
  { id: 'admin-settings-signatures-read', path: '/ui/admin/settings/signatures', kinds: ['auditor'] },
  // The section Signing identity in each state of R-SIG-056: with the controls of security-settings:write, and read-only.
  { id: 'admin-settings-signing', path: '/ui/admin/settings/signatures', kinds: [...admins, 'auditor'], walk: signingIdentity },
  // A server's failure in the screen's own words, its English text folded under Technical details (R-UI-214): one state each, opened,
  // as the administrator, in English and German (renders/server-texts.ts). A download's preview note is the tab Upstream's walk.
  { id: 'failure-repository-settings', path: `${P}/repositories/npmjs/settings`, kinds: admins, walk: remoteSettingsFailures, de: true },
  { id: 'failure-repository-mirror', path: `${P}/repositories/npmjs/settings`, kinds: admins, walk: mirrorFailure, de: true },
  { id: 'failure-upstream-registry', path: '/ui/admin/upstream/debian', kinds: admins, walk: registryTestFailure, de: true },
  // Test connection of an upstream registry in each answer a person meets, with what the test learned (Q-UI-1225, Q-UI-1226).
  { id: 'upstream-registry-tests', path: '/ui/admin/upstream/docker-hub', kinds: admins, walk: registryTests, de: true },
  { id: 'failure-ci-claim-tester', path: `/ui/admin/ci/${ciIssuer}/test`, kinds: admins, walk: claimTesterFailure, de: true },
  { id: 'failure-ci-issuer-keys', path: `/ui/admin/ci/${ciIssuer}?tab=keys`, kinds: admins, walk: issuerKeysFailure, de: true },
  { id: 'failure-ci-issuers', path: '/ui/admin/ci', kinds: admins, walk: issuerListFailure, de: true },
  { id: 'failure-system-jobs', path: '/ui/admin/system/jobs', kinds: admins, walk: jobsFailures, de: true },
  { id: 'failure-system-valkey', path: '/ui/admin/system', kinds: admins, walk: valkeyDegraded, de: true },
  { id: 'failure-malware-database', path: '/ui/admin/settings/malware', kinds: admins, walk: malwareDatabaseFailure, de: true },
  { id: 'failure-advisory-sources', path: '/ui/admin/advisory-sources', kinds: admins, walk: advisorySourceFailures, de: true },
  { id: 'failure-signature-root', path: '/ui/admin/settings/signatures', kinds: admins, walk: signatureRootFailure, de: true },
  { id: 'failure-signature-note', path: `${ver(P, manyVersionsPackage, signedVersions.invalid)}?tab=signatures`, kinds: ['plain'], walk: signatureNote, de: true },

  // The page and shell states no walk above opens (states.ts, R-UI-173, R-UI-175, Q-UI-1231 to Q-UI-1235). An overlay is taken at 1,440 and
  // 390 px, in dark at 1,440 px; what a person opens on a page (a section, a page of its own) at every width.
  // The shell's menus and notices, the same for every kind of user: theme, user menu and its language submenu; the maintenance mode and a
  // critical banner above a list; the banner on the sign-in page.
  { id: 'shell-menus', path: '/ui/projects', kinds: ['plain'], walk: shellMenus, only: narrowAndWide, states: overlays('theme', 'user', 'language'), de: true },
  { id: 'shell-notices', path: `${P}/repositories`, kinds: ['plain'], walk: shellNotices, only: narrowAndWide, states: overlays('notices'), de: true },
  { id: 'login-notices', path: '/ui/login', kinds: ['anonymous'], walk: loginNotice, only: narrowAndWide, states: overlays('banner') },
  // The page menus of a project's Owner: a repository's More, the saved views of the packages and a view's menu, a CI mapping's row.
  { id: 'owner-menus', path: `${P}/repositories/npm-vendor`, kinds: ['power'], walk: ownerMenus, only: narrowAndWide, states: overlays('repository-more', 'views', 'view-menu', 'mapping-menu') },
  // The admin area's drawers no list walk opens, the owner's person search inside the tokens' drawer, two tokens selected with their
  // question, and the issued CI tokens of one project that a bulk revoke would take.
  {
    id: 'admin-lists',
    path: '/ui/admin/system/jobs',
    kinds: ['admin'],
    walk: adminLists,
    only: narrowAndWide,
    states: overlays('jobs-filters', 'backups-filters', 'refusals-filters', 'owner-search', 'tokens-selected', 'tokens-revoke', 'issuances-matching'),
  },
  // Every closed section and fold of a page opened (R-UI-087's Details on demand): the pages whose sections no other walk opens.
  { id: 'admin-user-sections', path: '/ui/admin/users/sam', kinds: ['admin'], walk: everySection, states: sections },
  { id: 'admin-group-sections', path: `/ui/admin/groups/${group}`, kinds: ['admin'], walk: everySection, states: sections },
  { id: 'profile-sections', path: '/ui/profile', kinds: ['admin'], walk: everySection, states: sections },
  { id: 'admin-settings-auth-sections', path: '/ui/admin/settings/auth', kinds: ['admin'], walk: everySection, states: sections },
  { id: 'admin-settings-general-sections', path: '/ui/admin/settings/general', kinds: ['admin'], walk: everySection, states: sections },
  { id: 'project-settings-sections', path: `${P}/settings`, kinds: ['power'], walk: everySection, states: sections },
  { id: 'admin-endpoint-sections', path: '/ui/admin/endpoints/default', kinds: ['admin'], walk: everySection, states: sections },
  { id: 'admin-ci-new-sections', path: '/ui/admin/ci/new', kinds: ['admin'], walk: everySection, states: sections },
  { id: 'repository-settings-ansible-sections', path: `${O}/repositories/collections/settings`, kinds: ['admin'], walk: everySection, states: sections },
  { id: 'help-sections', path: '/ui/help', kinds: ['plain'], walk: everySection, states: sections },
  // A Debian repository's signing keys in the states with actions: the next key's certificate, its menu, a promotion that fails.
  {
    id: 'repository-settings-deb-keys',
    path: `${O}/repositories/${longDebKey}/settings`,
    kinds: ['admin'],
    walk: debianKeys,
    states: overlays('key-menu'),
    expected: [{ match: /^POST \/api\/v1\/projects\/[^/]+\/repositories\/[^/]+\/deb\/signing-keys\/promote answered 500$/, why: 'the promotion the walk answers as failed' }],
    notices: [{ match: /./, why: "the promotion's failure, which the walk answers in the browser" }],
  },
  // The pages of their own that no screen showed: a finding and its advisory, a finding under Security audit, a CI issuer, its claim
  // tester and mappings, a project's claim tester and mapping, a policy of a project and of the installation (seedPoliciesAndMappings).
  { id: 'finding', path: `${P}/vulnerabilities`, kinds: ['power'], walk: findingAndAdvisory },
  { id: 'security-finding', path: '/ui/security', kinds: ['auditor'], walk: securityFinding, only: narrowAndWide },
  { id: 'admin-ci-issuer', path: `/ui/admin/ci/${ciIssuer}`, kinds: ['admin'] },
  { id: 'admin-ci-issuer-states', path: `/ui/admin/ci/${ciIssuer}`, kinds: ['admin'], walk: issuerStates, states: { ...sections, ...overlays('mapping-menu') } },
  { id: 'admin-ci-test', path: `/ui/admin/ci/${ciIssuer}/test`, kinds: ['admin'] },
  { id: 'admin-ci-mapping', path: editorPaths.systemMapping, kinds: ['admin'] },
  { id: 'admin-ci-mapping-new', path: `/ui/admin/ci/${ciIssuer}/mappings/new`, kinds: ['admin'] },
  { id: 'project-ci-test', path: `${P}/ci/test`, kinds: ['power'] },
  {
    id: 'project-ci-mapping',
    path: editorPaths.projectMapping,
    kinds: ['power'],
    walk: mappingStates,
    states: refusedStates,
    expected: [{ match: /^(PUT|PATCH) \/api\/v1\/projects\/[^/]+\/ci\/mappings\/\S+ answered 422$/, why: 'the save the walk refuses with problems' }],
  },
  {
    id: 'policy',
    path: editorPaths.policy,
    kinds: ['power'],
    walk: policyStates,
    states: refusedStates,
    expected: [{ match: /^PUT \/api\/v1\/projects\/[^/]+\/policies\/\S+ answered 422$/, why: 'the save the walk refuses with problems' }],
    notices: [{ match: /./, why: 'the refused save, which the walk answers in the browser' }],
  },
  { id: 'admin-permission', path: editorPaths.instancePolicy, kinds: ['admin'], walk: openedAndSections, states: sections },
];
