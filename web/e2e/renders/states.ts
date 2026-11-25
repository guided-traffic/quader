import { Locator, Page, Route, expect } from '@playwright/test';
import { ciIssuer, exposureRule, instancePolicy, mainProject, projectMapping, projectPolicy, systemMapping } from './seed';

/**
 * The states of the pages and the shell that need the real page behind them (WP3 of the render guard's plan, R-UI-173, R-UI-175):
 * the menus of the shell and of the pages, the filter drawers no other walk opens, a person search inside a drawer, the selections of
 * the lists that revoke in bulk, the closed sections and folds opened, the notices above the content, the pages of a finding, an
 * advisory, a CI issuer, a mapping and a policy, and a save the server refuses in the editors of a policy, a mapping and a rule. An
 * overlay is taken at 1,440 and 390 px and in dark at 1,440 px only (`states` and `only` of screens.ts, Q-UI-1172, Q-UI-1231); the
 * overlays without a page behind them are `make ui-states`'.
 */

const P = `/ui/projects/${mainProject}`;
const REQUEST_ID = '4b7e2c9d1a3f46e8b5c0d7a2e9f81c63';

/** Waits until every finite animation of the page has run: a menu, a drawer or a section that opens animates. */
async function still(page: Page): Promise<void> {
  await page.evaluate(() => Promise.all(document.getAnimations().map((a) => a.finished.catch(() => undefined))));
}

/** The one menu of the overlay container, open. */
function openMenu(page: Page): Locator {
  return page.locator('.cdk-overlay-container [role="menu"]');
}

/** Clicks a menu's trigger and waits until the menu stands. */
async function menuFrom(page: Page, trigger: Locator): Promise<void> {
  await trigger.click();
  await expect(openMenu(page).first()).toBeVisible();
  await still(page);
}

/** Closes the open menus with Escape, as a keyboard does. */
async function escape(page: Page): Promise<void> {
  await page.keyboard.press('Escape');
  await expect(openMenu(page)).toHaveCount(0);
}

/** Cancels the open question (a confirm dialog) by its Cancel, the button before its confirmation. */
async function cancel(page: Page): Promise<void> {
  const dialog = page.getByRole('dialog');
  await dialog.locator('.dialog-actions button, [mat-dialog-actions] button, footer button').first().click();
  await expect(dialog).toHaveCount(0);
}

/**
 * A walk with more states after some of its own (`after`: the inner walk's state, then the states of `extra`). The extra states run
 * while the inner walk waits at its state, so they start from the page that state shows and leave it as they found it (a menu opened
 * and closed). So a menu of a page that another walk sets up is taken without copying that walk.
 */
export function withStates(
  walk: (page: Page) => AsyncGenerator<string>,
  after: Readonly<Record<string, (page: Page) => AsyncGenerator<string>>>,
): (page: Page) => AsyncGenerator<string> {
  return async function* (page: Page): AsyncGenerator<string> {
    for await (const state of walk(page)) {
      yield state;
      const extra = after[state];
      if (extra) yield* extra(page);
    }
  };
}

/** A problem document of a refused save, with its field problems (R-API-008), as the server writes one. */
function refused(route: Route, errors: readonly { pointer: string; code: string; detail: string }[]): Promise<void> {
  return route.fulfill({
    status: 422,
    contentType: 'application/problem+json',
    headers: { 'X-Request-Id': REQUEST_ID },
    body: JSON.stringify({
      type: '/api/v1/problems/validation_failed',
      title: 'Validation failed',
      status: 422,
      code: 'validation_failed',
      detail: `the request has ${errors.length} problems`,
      requestId: REQUEST_ID,
      errors,
    }),
  });
}

// ---- The shell ----

/**
 * The shell's menus (R-UI-002, R-API-021): the theme's three choices, the user menu and its language submenu, the same for every kind
 * of user. Found by their place, so the German pass takes them too.
 */
export async function* shellMenus(page: Page): AsyncGenerator<string> {
  await menuFrom(page, page.locator('header .topbar-actions button.topbar-icon[aria-haspopup="menu"]').last());
  yield 'theme';
  await escape(page);
  await menuFrom(page, page.locator('header button.user-button'));
  yield 'user';
  const language = openMenu(page).locator('[aria-haspopup="menu"]');
  await language.click();
  await expect(openMenu(page)).toHaveCount(2);
  await still(page);
  yield 'language';
  await page.keyboard.press('Escape');
  await escape(page);
}

/** The notices above the content (R-UI-002, R-UI-033): the read-only maintenance mode and a critical banner with a link, answered in the browser. */
async function answerNotices(page: Page, maintenance: boolean): Promise<void> {
  await page.route('**/api/v1/system/banner', (route) =>
    route.fulfill({
      json: {
        enabled: true,
        severity: 'critical',
        title: 'Storage migration tonight',
        message:
          'From 22:00 to 23:30 CET the object storage moves to the new cluster. Uploads and pushes fail during that time; downloads from the cache keep working. Details and the timeline: [the change record CHG-2026-1184](https://status.example.com/changes/CHG-2026-1184).',
      },
    }),
  );
  if (maintenance) {
    await page.route('**/api/v1/system/maintenance', (route) =>
      route.fulfill({
        json: {
          mode: 'read_only',
          cacheFill: true,
          message: 'The database is being upgraded to PostgreSQL 18. Publishing, deleting and every change of a setting wait until about 21:15 CET.',
          retryAfterSeconds: 1800,
          since: new Date(Date.now() - 25 * 60_000).toISOString(),
          by: 'Ada Example',
        },
      }),
    );
  }
}

/** A project's repositories under the maintenance mode's notice and a critical banner (R-UI-002, R-UI-033); nothing is changed on the server. */
export async function* shellNotices(page: Page): AsyncGenerator<string> {
  await answerNotices(page, true);
  await page.reload();
  await expect(page.locator('quader-banner .banner')).toHaveCount(2);
  yield 'notices';
  await page.unrouteAll({ behavior: 'ignoreErrors' });
}

/** The sign-in page with the banner (R-UI-002: the banner is public, the maintenance mode is read for a session only). */
export async function* loginNotice(page: Page): AsyncGenerator<string> {
  await answerNotices(page, false);
  await page.reload();
  await expect(page.locator('quader-banner .banner')).toHaveCount(1);
  yield 'banner';
  await page.unrouteAll({ behavior: 'ignoreErrors' });
}

// ---- The menus of the pages ----

/**
 * The page menus a project's Owner meets (R-UI-163): a repository's **More**; the package list's saved views, two of them with a long
 * name and the first the default, and a view's menu; the menu of a CI mapping's row on the project's CI page. Each menu is opened and
 * closed; the views live in the browser only.
 */
export async function* ownerMenus(page: Page): AsyncGenerator<string> {
  await menuFrom(page, page.locator('main quader-page-header button.btn-default.btn-icon[aria-haspopup="menu"]'));
  yield 'repository-more';
  await escape(page);

  // The views of the width before are the browser's: forgotten, so each width saves the same two.
  await page.evaluate(() => {
    for (const key of Object.keys(localStorage)) if (key.startsWith('quader.packages.')) localStorage.removeItem(key);
  });
  await page.goto(`${P}/packages`);
  // The third tab, Saved views, by its place: the German pass walks this screen too.
  await page.locator('main nav.view-tabs a.tab-link').nth(2).click();
  const name = page.locator('main form.save-view input');
  for (const view of ['Internal packages of the design system, newest first', 'Fixable findings']) {
    await name.fill(view);
    await page.locator('main form.save-view button[type=submit]').click();
    await expect(name).toHaveValue('');
  }
  const menus = page.locator('main button.view-menu');
  await expect(menus).toHaveCount(2);
  await menus.first().click();
  await openMenu(page).locator('[role=menuitem]').nth(1).click();
  await expect(page.locator('main .plain-list .badge')).toHaveCount(1);
  yield 'views';
  await menuFrom(page, menus.first());
  yield 'view-menu';
  await escape(page);

  await page.goto(`${P}/ci`);
  await menuFrom(page, page.locator('main quader-ci-mapping-list td.actions button[aria-haspopup="menu"]').first());
  yield 'mapping-menu';
  await escape(page);
}

/** The menu of a version of a package on the tab Upstream, for who may download (R-CUR-018): after the walk's state `package`. */
export async function* upstreamVersionMenu(page: Page): AsyncGenerator<string> {
  const trigger = page.locator('main quader-upstream-package td.actions button[aria-haspopup="menu"]').first();
  if ((await trigger.count()) === 0) return;
  await menuFrom(page, trigger);
  yield 'version-menu';
  await escape(page);
}

/** The menu of an open request on the tab Upstream, for who decides (R-CUR-018): after the walk's state `search`. */
export async function* upstreamRequestMenu(page: Page): AsyncGenerator<string> {
  const trigger = page.locator('main [data-testid="upstream-requests"] button.request-menu').first();
  if ((await trigger.count()) === 0) return;
  await menuFrom(page, trigger);
  yield 'request-menu';
  await escape(page);
}

/** The menu of **Run a pass now** of a stored mirror (R-CUR-017): after the walk's state `stored`. */
export async function* mirrorRunMenu(page: Page): AsyncGenerator<string> {
  await menuFrom(page, page.locator('main quader-mirror-status .split button.more'));
  yield 'run-menu';
  await escape(page);
}

// ---- Drawers, the person search in a drawer, selections ----

/** Opens the drawer of the list whose head is `head` and waits until it stands. */
async function drawerOf(page: Page, head: Locator): Promise<Locator> {
  await head.locator('.filter-toggle').click();
  const drawer = page.locator('.cdk-overlay-container [role="dialog"]').filter({ has: page.locator('form.filter-form') });
  await expect(drawer).toBeVisible();
  await still(page);
  return drawer;
}

/** Closes a drawer by its close button and waits until it is gone. */
async function closeDrawer(page: Page, drawer: Locator): Promise<void> {
  await drawer.locator('button.drawer-close').click();
  await expect(drawer).toHaveCount(0);
}

/**
 * The lists of the admin area in the states no other walk opens (R-UI-163, Q-UI-420, R-UI-150): the filter drawers of the jobs, the
 * backups and the refused downloads; the tokens' drawer with the matches of the owner's person search open inside it (W-12's shape);
 * two tokens selected with **Revoke selected** and its question; the issued CI tokens filtered by an issuer and a project, with
 * **Revoke matching tokens** offered (its question is the confirm dialog's plain shape of `make ui-states`, and the seed's one issuance
 * has expired by the time the walk runs). Nothing is revoked: the question is cancelled.
 */
export async function* adminLists(page: Page): AsyncGenerator<string> {
  let drawer = await drawerOf(page, page.locator('main quader-list-head'));
  yield 'jobs-filters';
  await closeDrawer(page, drawer);

  await page.goto('/ui/admin/system/backups');
  drawer = await drawerOf(page, page.locator('main quader-list-head'));
  yield 'backups-filters';
  await closeDrawer(page, drawer);

  // The refused downloads' head is the first of the page Signatures (R-SIG-074).
  await page.goto('/ui/security/signatures');
  drawer = await drawerOf(page, page.locator('main quader-list-head').first());
  yield 'refusals-filters';
  await closeDrawer(page, drawer);

  await page.goto('/ui/admin/tokens');
  drawer = await drawerOf(page, page.locator('main quader-list-head'));
  await drawer.locator('quader-principal-field input[role="combobox"]').fill('re');
  await expect(page.locator('quader-principal-field [role="option"]').first()).toBeVisible();
  await still(page);
  yield 'owner-search';
  await closeDrawer(page, drawer);

  const boxes = page.locator('main quader-token-table tbody input[type="checkbox"]');
  await boxes.nth(0).check();
  await boxes.nth(1).check();
  await expect(page.locator('main quader-list-head button.btn-confirm')).toBeVisible();
  yield 'tokens-selected';
  await page.locator('main quader-list-head button.btn-confirm').click();
  await expect(page.getByRole('dialog')).toBeVisible();
  await still(page);
  yield 'tokens-revoke';
  await cancel(page);

  await page.goto('/ui/admin/ci/issuances');
  drawer = await drawerOf(page, page.locator('main quader-list-head'));
  await drawer.locator('input[name="issuer"]').fill(ciIssuer);
  await drawer.locator('input[name="projectId"]').fill('4711');
  await drawer.locator('button[type="submit"]').click();
  await expect(drawer).toHaveCount(0);
  const revoke = page.locator('main quader-list-head button.btn-default:not(.filter-toggle)');
  await expect(revoke).toBeEnabled();
  yield 'issuances-matching';
}

// ---- Closed sections and folds ----

/**
 * Every closed section of the page opened, then every fold inside them (`inline`: a certificate, the messages of a row, the roles of a
 * principal), until none is closed; a section that loads its content when it opens is waited for by the run's `settle`. One picture of
 * the page with everything open, beside the screen's own pictures as it opens: the details a person opens one by one stand together.
 */
export async function* everySection(page: Page): AsyncGenerator<string> {
  await openEverySection(page, page.locator('main'));
  yield 'sections';
}

/** A page of its own as it opens, then with every closed section and fold opened (`everySection`). */
export async function* openedAndSections(page: Page): AsyncGenerator<string> {
  yield '';
  yield* everySection(page);
}

async function openEverySection(page: Page, within: Locator): Promise<void> {
  const closed = within.locator('quader-disclosure button[aria-expanded="false"]:is(.toggle, .inline-toggle)');
  await expect(closed.first()).toBeVisible();
  for (let round = 0; round < 60 && (await closed.count()) > 0; round++) {
    // The toggle is named by the region it controls; the page writes `aria-expanded` a moment after the click (allSections).
    const region = await closed.first().getAttribute('aria-controls');
    const toggle = within.locator(`quader-disclosure button[aria-controls="${region}"]`);
    await toggle.click();
    await expect(toggle).toHaveAttribute('aria-expanded', 'true');
    await still(page);
  }
  await expect(closed).toHaveCount(0);
}

/**
 * A Debian repository's signing keys (R-DEB-018) with a key in each state that has actions, answered in the browser: the section
 * opened with the next key's public certificate unfolded; the menu of the next key; and a promotion the server refuses with a code the
 * page has no sentence for, so the shared error notice stands inside the section (R-UI-216). Nothing is changed on the server.
 */
export async function* debianKeys(page: Page): AsyncGenerator<string> {
  const keys = '**/api/v1/projects/*/repositories/*/deb/signing-keys';
  await page.route(keys, async (route) => {
    if (route.request().method() !== 'GET') return route.fallback();
    const real = await route.fetch();
    const body = (await real.json()) as { keys: Record<string, unknown>[] };
    const primary = body.keys[0] ?? {};
    const key = (state: string, fingerprint: string, extra: Record<string, unknown> = {}): Record<string, unknown> => ({
      ...primary,
      fingerprint,
      state,
      origin: 'generated',
      promotedAt: null,
      keyCreatedAt: new Date(Date.now() - 3 * 86_400_000).toISOString(),
      createdAt: new Date(Date.now() - 3 * 86_400_000).toISOString(),
      ...extra,
    });
    return route.fulfill({
      response: real,
      json: {
        ...body,
        keys: [
          ...body.keys,
          key('next', '7D2A4C9E1B3F5A6D8C0E2F4A6B8D0C2E4F6A8B0C'),
          key('withheld', '0C3E5A7B9D1F2A4C6E8B0D2F4A6C8E0B2D4F6A8C', { origin: 'imported', promotedAt: null, withheldAt: new Date(Date.now() - 86_400_000).toISOString() }),
        ],
      },
    });
  });
  await page.reload();
  // The section renders its keys once it is opened: found by its heading.
  const toggle = page.locator('main quader-disclosure button.toggle').filter({ hasText: 'Signing keys' });
  await toggle.click();
  const section = toggle.locator('xpath=ancestor::quader-disclosure[1]');
  const table = section.locator('[data-testid="deb-key-table"]');
  await expect(table.locator('tbody tr')).toHaveCount(3);
  await table.locator('tr[data-state="next"] button.inline-toggle').click();
  await expect(table.locator('tr[data-state="next"] button.inline-toggle')).toHaveAttribute('aria-expanded', 'true');
  await still(page);
  yield 'keys';
  await menuFrom(page, table.locator('tr[data-state="next"] td.actions button'));
  yield 'key-menu';
  await page.route('**/api/v1/projects/*/repositories/*/deb/signing-keys/promote', (route) =>
    route.fulfill({
      status: 500,
      contentType: 'application/problem+json',
      headers: { 'X-Request-Id': REQUEST_ID },
      body: JSON.stringify({
        type: '/api/v1/problems/deb_key_store_unavailable',
        title: 'Key store unavailable',
        status: 500,
        code: 'deb_key_store_unavailable',
        detail: 'the key ring of debian-internal-packages-for-bookworm-and-trixie could not be written: the key store answered 503',
        requestId: REQUEST_ID,
      }),
    }),
  );
  await openMenu(page).locator('[role=menuitem]').first().click();
  // A promotion asks first (the old primary key's private key is erased): confirmed here.
  const dialog = page.getByRole('dialog');
  await expect(dialog).toBeVisible();
  await dialog.locator('.dialog-actions button.btn-confirm').click();
  await expect(section.locator('quader-error-notice')).toBeVisible();
  yield 'key-failed';
  await page.unrouteAll({ behavior: 'ignoreErrors' });
}

// ---- Pages of their own ----

/**
 * A finding's page, opened from the project's report as a person reaches it (R-VULN-022): its advisory, the version and the repository,
 * the history and, for who may triage, the actions; then the advisory's page its heading links to.
 */
export async function* findingAndAdvisory(page: Page): AsyncGenerator<string> {
  await page.locator('main quader-vulnerability-report tbody a[href*="/findings/"]').first().click();
  await expect(page.locator('main h1')).toBeVisible();
  yield 'finding';
  await page.locator('main a[href*="/advisories/"]').first().click();
  await expect(page).toHaveURL(/\/advisories\//);
  await expect(page.locator('main h1')).toBeVisible();
  yield 'advisory';
}

/** A finding's page under Security audit (R-UI-158), opened from the dashboard's list. */
export async function* securityFinding(page: Page): AsyncGenerator<string> {
  await page.locator('main quader-vulnerability-report tbody a[href*="/findings/"]').first().click();
  await expect(page).toHaveURL(/\/security\/findings\//);
  await expect(page.locator('main h1')).toBeVisible();
  yield 'finding';
}

/**
 * The CI issuer's page (R-UI-035): its settings with every section opened, and the menu of a mapping's row on its tab Mappings. The tabs
 * as the page opens are `admin-ci-issuer`'s pictures.
 */
export async function* issuerStates(page: Page): AsyncGenerator<string> {
  await page.goto(`/ui/admin/ci/${ciIssuer}?tab=settings`);
  await openEverySection(page, page.locator('main'));
  yield 'sections';
  await page.goto(`/ui/admin/ci/${ciIssuer}?tab=mappings`);
  await menuFrom(page, page.locator('main quader-ci-mapping-list td.actions button[aria-haspopup="menu"]').first());
  yield 'mapping-menu';
  await escape(page);
}

/** The addresses of the seed's editors that have a page of their own (seed.ts, `seedPoliciesAndMappings`). */
export const editorPaths = {
  policy: `${P}/policies/${projectPolicy}`,
  instancePolicy: `/ui/admin/permissions/${instancePolicy}`,
  systemMapping: `/ui/admin/ci/${ciIssuer}/mappings/${systemMapping}`,
  projectMapping: `${P}/ci/mappings/${projectMapping}?issuer=${ciIssuer}`,
  rule: `/ui/admin/endpoints/default/rules/${exposureRule}`,
} as const;

/**
 * A stored policy (R-UI-027, R-AUTHZ-004): as it opens; with its closed sections opened; then a change of its description saved and
 * refused by the server with a problem at the description and at an entry, each under its field and the notice above the form. The
 * question of what changes (`PolicyDiffDialog`, `make ui-states`) is confirmed on the way. Nothing is stored.
 */
export async function* policyStates(page: Page): AsyncGenerator<string> {
  yield '';
  await openEverySection(page, page.locator('main'));
  yield 'sections';
  await page.route('**/api/v1/projects/*/policies/*', (route) =>
    route.request().method() === 'PUT'
      ? refused(route, [
          { pointer: '/description', code: 'invalid_description', detail: 'The description has at most 1024 characters.' },
          { pointer: '/entries/2/principal', code: 'unknown_principal', detail: 'No principal sa:web-frontend/release-pipeline exists.' },
        ])
      : route.fallback(),
  );
  await descriptionChanged(page);
  await page.locator('main form button.btn-confirm[type="submit"]').click();
  const dialog = page.getByRole('dialog');
  await expect(dialog).toBeVisible();
  await dialog.locator('button.btn-confirm').click();
  await expect(page.locator('main quader-error-notice')).toBeVisible();
  yield 'refused';
  await page.unrouteAll({ behavior: 'ignoreErrors' });
}

/** Changes a form's first description field, so that a save has something to send. */
async function descriptionChanged(page: Page): Promise<void> {
  const field = page.locator('main textarea[name="description"], main input[name="description"], main #f-description').first();
  await field.fill('Lets the release pipeline and the web team read the internal packages of the customer portal, and nothing else.');
}

/**
 * A mapping of a project's CI (R-UI-036, R-CI-009): as it opens, with its closed sections opened, then a change saved and refused by
 * the server: the summary of the problems above the form links each to its section (R-UI-041).
 */
export async function* mappingStates(page: Page): AsyncGenerator<string> {
  yield '';
  if ((await page.locator('main quader-disclosure button[aria-expanded="false"]').count()) > 0) {
    await openEverySection(page, page.locator('main'));
    yield 'sections';
  }
  await page.route('**/api/v1/projects/*/ci/mappings/**', (route) =>
    route.request().method() === 'PUT' || route.request().method() === 'PATCH'
      ? refused(route, [
          { pointer: '/priority', code: 'out_of_range', detail: 'The priority is between 1 and 1000000.' },
          { pointer: '/match/all/1', code: 'condition_invalid', detail: 'ref_type: equals takes one of branch and tag.' },
          {
            pointer: '/grants/policies/0',
            code: 'policy_outside_project',
            detail: `The project "${mainProject}" has no policy named "${projectPolicy}"; a project's mapping names the project's own policies.`,
          },
        ])
      : route.fallback(),
  );
  await descriptionChanged(page);
  await page.locator('main form button.btn-confirm[type="submit"]').click();
  await expect(page.locator('main quader-error-summary')).toBeVisible();
  yield 'refused';
  await page.unrouteAll({ behavior: 'ignoreErrors' });
}

/**
 * An exposure rule's change saved and refused by the server (R-EP-017, R-UI-041): the question of what changes confirmed, then the
 * summary of the problems above the form. Taken after the walk's `advanced`.
 */
export async function* ruleRefused(page: Page): AsyncGenerator<string> {
  await page.route('**/api/v1/endpoints/*/exposure-rules/*', (route) =>
    route.request().method() === 'PUT' || route.request().method() === 'PATCH'
      ? refused(route, [
          { pointer: '/repository', code: 'not_found', detail: 'repository web-frontend/assets-archive: not found' },
          { pointer: '/paths/include/1', code: 'invalid_pattern', detail: 'releases/**/[: unterminated character class' },
        ])
      : route.fallback(),
  );
  await descriptionChanged(page);
  await page.locator('main form button.btn-confirm[type="submit"]').click();
  const dialog = page.getByRole('dialog');
  await expect(dialog).toBeVisible();
  await dialog.locator('button.btn-confirm').click();
  await expect(page.locator('main quader-error-summary')).toBeVisible();
  yield 'refused';
  await page.unrouteAll({ behavior: 'ignoreErrors' });
}
