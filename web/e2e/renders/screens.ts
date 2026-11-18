import { Page, expect } from '@playwright/test';
import {
  Kind,
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
  serviceAccount,
} from './seed';
import { gradedPackage, manyVersionsPackage } from './advisories';
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
}

/**
 * A list as it opens, then with its filter panel open: beside the panel the list has some 300 px less, and that width is the
 * one its columns and addresses have to fit (the owner's screen of 2026-11-12, a repository list at 1860 px).
 */
async function* filtersOpen(page: Page): AsyncGenerator<string> {
  yield '';
  await page.locator('main .filter-toggle').first().click();
  await expect(page.locator('main quader-filter-panel form')).toBeVisible();
  yield 'filters';
}

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

/**
 * The search's store of frequently visited places (R-UI-096): three repositories and two projects opened first, then the start
 * page with **Recently opened** from the store, the field opened with its frequently visited groups, and the project switcher's
 * recent list.
 */
async function* frequentPlaces(page: Page): AsyncGenerator<string> {
  for (const path of [
    `/ui/projects/${mainProject}/repositories/npm-vendor`,
    `/ui/projects/${otherProject}/repositories/collections`,
    `/ui/projects/${mainProject}/repositories/npm-internal`,
  ]) {
    await page.goto(path);
    await expect(page.locator('main h1')).toBeVisible();
  }
  await page.goto('/ui/projects');
  await expect(page.locator('main .recent')).toBeVisible();
  yield '';
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
  while ((await closed.count()) > 0) await closed.first().click();
  await expect(page.locator('main quader-disclosure .collapse-region:not(.open)')).toHaveCount(0);
  yield 'sections';
}

/** The closed Visibility section of a settings page, opened: where the level of the installation or of a project is chosen (R-UI-053, R-UI-054). */
async function* visibilityOpen(page: Page): AsyncGenerator<string> {
  yield '';
  await page.locator('main quader-disclosure').filter({ hasText: 'Visibility' }).first().getByRole('button').first().click();
  await expect(page.locator('main quader-visibility-level').first()).toBeVisible();
  yield 'visibility';
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
 * two members, Advanced settings open, and the key's error after a wrong key was typed and left. Nothing is created.
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

const upstreams: Screen[] = [
  { id: 'upstream-oci', path: `/ui/projects/${mainProject}/repositories/new/oci`, kinds: ['power'], walk: upstreamWalk('GitLab', true) },
  { id: 'upstream-npm', path: `/ui/projects/${mainProject}/repositories/new/npm`, kinds: ['power'], walk: upstreamWalk(null, false) },
  { id: 'upstream-deb', path: `/ui/projects/${mainProject}/repositories/new/deb`, kinds: ['power'], walk: upstreamWalk(null, false) },
  { id: 'admin-upstream-oci', path: '/ui/admin/repositories/new/oci', kinds: ['admin'], walk: upstreamWalk('GitLab', true) },
];

const all: readonly Kind[] = ['plain', 'power', 'admin'];
const owners: readonly Kind[] = ['power', 'admin'];
const admins: readonly Kind[] = ['admin'];
const P = `/ui/projects/${mainProject}`;
const O = `/ui/projects/${otherProject}`;

/** The address of a package page (R-UI-007) and of a version page (R-UI-111, `…/<name>@<version>`). */
const pkg = (base: string, p: { readonly format: string; readonly name: string }): string => `${base}/packages/${p.format}/${p.name}`;
const ver = (base: string, p: { readonly format: string; readonly name: string }, version: string): string => `${pkg(base, p)}@${version}`;
const collectionName = `${ansiblePackage.namespace}.${ansiblePackage.name}`;

export const screens: readonly Screen[] = [
  // Without a session (Q-UI-125).
  { id: 'login', path: '/ui/login', kinds: ['anonymous'] },
  { id: 'logged-out', path: '/ui/logged-out', kinds: ['anonymous'] },

  // The start and the project's content (Q-UI-129): the plain user's main path.
  { id: 'home', path: '/ui/', kinds: all },
  { id: 'projects', path: '/ui/projects', kinds: all },
  // The search field and its list on the start page; anonymous while the seed makes a project public (R-UI-095).
  { id: 'search-field', path: '/ui/projects', kinds: ['anonymous', ...all], walk: searchField, expected: tooMany },
  // The start page and the project switcher read the search's store (R-UI-096); an anonymous visitor's start page (R-UI-095).
  { id: 'start-recent', path: '/ui/projects', kinds: ['plain', 'power'], walk: frequentPlaces },
  { id: 'projects-anonymous', path: '/ui/projects', kinds: ['anonymous'], expected: tooMany },
  // The results page of the search (R-UI-098): every tab with matches, a text without a match, a tab opened by `kind`.
  { id: 'search-results', path: '/ui/search?q=web', kinds: all },
  { id: 'search-results-none', path: '/ui/search?q=zzqx', kinds: ['plain'] },
  { id: 'search-results-kind-package', path: '/ui/search?q=acme&kind=package', kinds: all },
  { id: 'repositories', path: `${P}/repositories`, kinds: all, walk: filtersOpen },
  { id: 'repositories-long-keys', path: `${O}/repositories`, kinds: all, walk: filtersOpen },
  { id: 'repositories-empty-project', path: `/ui/projects/${emptyProject}/repositories`, kinds: admins },
  { id: 'repository-npm-local', path: `${P}/repositories/npm-internal`, kinds: all },
  { id: 'repository-npm-remote', path: `${P}/repositories/npmjs`, kinds: all },
  { id: 'repository-npm-virtual', path: `${P}/repositories/npm`, kinds: all },
  { id: 'repository-long-key', path: `${P}/repositories/${longNpmKey}`, kinds: all },
  { id: 'repository-generic', path: `${P}/repositories/assets`, kinds: all },
  { id: 'repository-oci', path: `${P}/repositories/images`, kinds: all },
  // The grades of R-VULN-038: S to E in the vendored packages, N/A in the internal ones and the images (advisories.ts).
  { id: 'repository-npm-vendor', path: `${P}/repositories/npm-vendor`, kinds: all },
  { id: 'repository-npm-vendor-by-grade', path: `${P}/repositories/npm-vendor?sort=-grade`, kinds: ['plain'] },
  { id: 'repository-empty', path: `${P}/repositories/scratch`, kinds: all },
  { id: 'repository-oci-remote', path: `${O}/repositories/docker-hub-remote`, kinds: all },
  { id: 'repository-deb', path: `${O}/repositories/${longDebKey}`, kinds: all },
  { id: 'repository-nuget-virtual', path: `${O}/repositories/nuget`, kinds: all },
  { id: 'repository-ansible', path: `${O}/repositories/collections`, kinds: all },
  { id: 'packages', path: `${P}/packages`, kinds: all, walk: filtersOpen },
  { id: 'packages-other-project', path: `${O}/packages`, kinds: all, walk: filtersOpen },
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
  { id: 'vulnerabilities', path: `${P}/vulnerabilities`, kinds: all, walk: filtersOpen },
  // A filter opens the panel; the list sorts by Detected at every width, at 390 px from the sort row above its stacked rows (Q-UI-391).
  { id: 'vulnerabilities-filtered', path: `${P}/vulnerabilities?severity=high`, kinds: admins, walk: sortByDetection },
  { id: 'members', path: `${P}/members`, kinds: all },
  { id: 'setup', path: '/ui/setup', kinds: all },
  { id: 'setup-repository', path: `/ui/setup?repository=${mainProject}/npm`, kinds: all },
  // The top bar's search ends on a page of the main area, never in the admin area (Q-UI-390); the field shows the text.
  { id: 'search', path: '/ui/search?q=npm', kinds: all },
  { id: 'search-none', path: '/ui/search?q=no-such-repository', kinds: ['admin'] },
  { id: 'guides', path: '/ui/guides', kinds: all },
  { id: 'guides-repository', path: `/ui/guides?repository=${mainProject}/npm`, kinds: all },
  { id: 'help', path: '/ui/help', kinds: all },
  { id: 'help-user-page', path: '/ui/help/user/getting-started', kinds: all },
  { id: 'profile', path: '/ui/profile', kinds: all },
  { id: 'profile-tokens', path: '/ui/profile/tokens', kinds: all },
  // The plain user holds a token (setup.ts): its revoke-all, last on the page behind Advanced.
  { id: 'profile-tokens-advanced', path: '/ui/profile/tokens', kinds: ['plain'], walk: advancedOpen },
  { id: 'profile-sessions', path: '/ui/profile/sessions', kinds: all },
  { id: 'not-found', path: '/ui/no-such-page', kinds: all },

  // What an Owner of the project maintains, and the settings a reader is offered in the list.
  // The settings page of each class, standard sections open and every detail opened in a second picture (R-UI-087).
  { id: 'repository-settings-npm-local', path: `${P}/repositories/npm-internal/settings`, kinds: all, walk: allSections },
  { id: 'repository-settings-npm-remote', path: `${P}/repositories/npmjs/settings`, kinds: all, walk: allSections },
  { id: 'repository-settings-npm-virtual', path: `${P}/repositories/npm/settings`, kinds: all, walk: allSections },
  { id: 'repository-settings-generic', path: `${P}/repositories/assets/settings`, kinds: owners, walk: allSections },
  { id: 'repository-settings-generic-advanced', path: `${P}/repositories/assets/settings`, kinds: admins, walk: advancedOpen },
  { id: 'repository-settings-oci', path: `${P}/repositories/images/settings`, kinds: all, walk: allSections },
  { id: 'repository-new', path: `${P}/repositories/new`, kinds: owners, walk: chooser },
  { id: 'policies', path: `${P}/policies`, kinds: owners },
  { id: 'policy-new', path: `${P}/policies/new`, kinds: owners },
  { id: 'project-service-accounts', path: `${P}/service-accounts`, kinds: owners },
  { id: 'project-service-account', path: `${P}/service-accounts/${projectServiceAccount}`, kinds: owners },
  { id: 'project-ci', path: `${P}/ci`, kinds: owners },
  // A project's mapping editor without an issuer in the address: the issuer select and the policy picker (Q-UI-334).
  { id: 'project-ci-mapping-new', path: `${P}/ci/mappings/new`, kinds: owners },
  { id: 'project-settings', path: `${P}/settings`, kinds: owners, walk: visibilityOpen },

  // The administrator's area (Q-UI-127).
  { id: 'repository-settings-oci-remote', path: `${O}/repositories/docker-hub-remote/settings`, kinds: all, walk: allSections },
  { id: 'repository-settings-oci-virtual', path: `${O}/repositories/containers/settings`, kinds: admins, walk: allSections },
  { id: 'repository-settings-deb', path: `${O}/repositories/${longDebKey}/settings`, kinds: admins, walk: allSections },
  { id: 'repository-settings-nuget-remote', path: `${O}/repositories/nuget-org/settings`, kinds: admins, walk: allSections },
  { id: 'repository-settings-ansible', path: `${O}/repositories/collections/settings`, kinds: all, walk: allSections },
  { id: 'repository-settings-ansible-remote', path: `${O}/repositories/galaxy/settings`, kinds: admins, walk: allSections },
  { id: 'repository-settings-ansible-virtual', path: `${O}/repositories/ansible/settings`, kinds: admins, walk: allSections },
  { id: 'help-admin-page', path: '/ui/help/admin/repositories', kinds: admins },
  { id: 'admin-dashboard', path: '/ui/admin/dashboard', kinds: admins },
  { id: 'admin-projects', path: '/ui/admin/projects', kinds: admins, walk: filtersOpen },
  { id: 'admin-repositories', path: '/ui/admin/repositories', kinds: admins, walk: filtersOpen },
  { id: 'admin-repository-new', path: '/ui/admin/repositories/new', kinds: admins, walk: chooser },
  { id: 'admin-users', path: '/ui/admin/users', kinds: admins, walk: filtersOpen },
  { id: 'admin-user', path: '/ui/admin/users/sam', kinds: admins },
  // The break-glass account: no Advanced action, so the page asks the token list whether it offers Revoke all.
  { id: 'admin-user-break-glass', path: '/ui/admin/users/admin', kinds: admins },
  { id: 'admin-user-advanced', path: '/ui/admin/users/sam', kinds: admins, walk: advancedOpen },
  { id: 'admin-groups', path: '/ui/admin/groups', kinds: admins, walk: filtersOpen },
  { id: 'admin-group', path: `/ui/admin/groups/${group}`, kinds: admins },
  { id: 'admin-service-accounts', path: '/ui/admin/service-accounts', kinds: admins, walk: filtersOpen },
  { id: 'admin-service-account', path: `/ui/admin/service-accounts/${serviceAccount}`, kinds: admins },
  { id: 'admin-permissions', path: '/ui/admin/permissions', kinds: admins, walk: filtersOpen },
  { id: 'admin-permission-new', path: '/ui/admin/permissions/new', kinds: admins },
  { id: 'admin-roles', path: '/ui/admin/roles', kinds: admins },
  { id: 'admin-tokens', path: '/ui/admin/tokens', kinds: admins, walk: filtersOpen },
  { id: 'admin-ci', path: '/ui/admin/ci', kinds: admins },
  { id: 'admin-ci-new', path: '/ui/admin/ci/new', kinds: admins },
  { id: 'admin-ci-issuances', path: '/ui/admin/ci/issuances', kinds: admins, walk: filtersOpen },
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
  { id: 'admin-security', path: '/ui/admin/security', kinds: admins },
  { id: 'admin-audit', path: '/ui/admin/audit', kinds: admins, walk: filtersOpen },
  { id: 'admin-advisory-sources', path: '/ui/admin/advisory-sources', kinds: admins },
  { id: 'admin-system', path: '/ui/admin/system', kinds: admins },
  { id: 'admin-system-jobs', path: '/ui/admin/system/jobs', kinds: admins },
  { id: 'admin-system-backups', path: '/ui/admin/system/backups', kinds: admins },
  { id: 'admin-settings-auth', path: '/ui/admin/settings/auth', kinds: admins },
  { id: 'admin-settings-general', path: '/ui/admin/settings/general', kinds: admins, walk: visibilityOpen },
];
