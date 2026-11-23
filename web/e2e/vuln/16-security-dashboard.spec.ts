import { APIRequestContext } from '@playwright/test';
import { localNpmRepository, npmPublishBody } from '../npm-publish';
import {
  applyFilters,
  bobPassword,
  e2eProject,
  expect,
  expectAccessible,
  openFilters,
  qualified,
  repositories,
  repositoryApi,
  signInLocal,
  signInWithKeycloak,
  test,
  unique,
} from '../support';

// R-UI-047 journey "security dashboard" (Q-UI-130, R-VULN-020, R-VULN-021, R-VULN-022, R-UI-158, R-UI-159): the auditor's
// instance-wide view at /security, under Security audit in the side bar of Your work, over two projects, against the test
// advisory source (deploy/dev/osvstub.yaml). It creates its own second
// project and local npm repositories and publishes the fixture packages through the API (INV-17); the findings come from the
// commit hook of the publish. Run with `npx playwright test -c playwright.vuln.config.ts` and QUADER_E2E_VULN=1 in the
// `scatter` profile (R-HA-011).

test.beforeAll(() => {
  if (!process.env['QUADER_E2E_VULN']) {
    throw new Error('QUADER_E2E_VULN is not set: the journeys need the scanning against the test advisory source (deploy/dev/osvstub.yaml)');
  }
});

interface Finding {
  readonly id: string;
  readonly project: string;
  readonly repository: string;
  readonly package: string;
  readonly advisory: string;
  readonly aliases: readonly string[];
  readonly status: string;
}

/** A local npm repository in `project`, with the packages published into it. */
async function localNpmRepositoryIn(admin: APIRequestContext, project: string, key: string, packages: readonly (readonly [string, string])[]): Promise<void> {
  const created = await admin.post(repositories(project), { data: { key, class: 'local', format: 'npm', vulnerabilities: { matchPublicAdvisories: 'always' } } });
  expect(created.status(), await created.text()).toBe(201);
  for (const [name, version] of packages) {
    const published = await admin.put(`/api/npm/${project}/${key}/${name}`, { data: npmPublishBody(key, name, version) });
    expect(published.status(), `publish ${name}@${version}: ${await published.text()}`).toBe(201);
  }
}

/** Waits until the findings API shows `count` findings of the project, which the commit hook of the publish wrote. */
async function findingsOfProject(admin: APIRequestContext, project: string, count: number): Promise<Finding[]> {
  let items: Finding[] = [];
  await expect
    .poll(
      async () => {
        const res = await admin.get(`/api/v1/vulnerabilities/findings?project=${project}`);
        items = ((await res.json()) as { items: Finding[] }).items;
        return items.length;
      },
      { message: `findings of project ${project}`, timeout: 60_000 },
    )
    .toBe(count);
  return items;
}

test('R-UI-159 the dashboard searches every project by alias and text in its one search field, filters by project in the drawer, and the counts follow the project', async ({
  page,
  admin,
  replicas,
}) => {
  const shop = unique('sec');
  const repoA = unique('sec-a');
  const repoB = unique('sec-b');
  const created = await admin.post('/api/v1/projects', { data: { key: shop, displayName: 'Security journey' } });
  expect(created.status(), await created.text()).toBe(201);
  try {
    // quader-test-alias 2.0.0 carries GHSA-aaaa-bbbb-cccc with the alias CVE-2026-0001 (test/osvstub/fixture.go)
    await localNpmRepository(admin, repoA, [['quader-test-alias', '2.0.0']]);
    await localNpmRepositoryIn(admin, shop, repoB, [
      ['quader-test-alias', '2.0.0'],
      ['quader-test-cvss', '1.0.0'],
    ]);
    const mine = await findingsOfProject(admin, shop, 2);
    const aliasFinding = mine.find((f) => f.package === 'quader-test-alias')!;
    expect(aliasFinding.aliases, 'the aliases of the finding').toContain('CVE-2026-0001');
    await signInLocal(page);

    await test.step('the dashboard is the entry Security dashboard under Security audit and shows the findings of both projects', async () => {
      await page.goto('/ui/en/projects');
      const nav = page.getByRole('navigation', { name: 'Main navigation' });
      await nav.getByRole('link', { name: 'Security dashboard' }).click();
      await expect(page).toHaveURL(/\/ui\/en\/security$/);
      await expect(page.getByRole('heading', { name: 'Security dashboard', level: 1 })).toBeVisible();
      // A second context title of the side bar, not a folding group (R-UI-168): the block is a group named by its title.
      await expect(nav.getByRole('group', { name: 'Security audit', exact: true }).getByRole('link', { name: 'Security dashboard' })).toBeVisible();
      await expect(nav.getByRole('button', { name: 'Security audit' })).toHaveCount(0);
      await expect(page.getByText('The counts and the trend cover every project.')).toBeVisible();
      await page.goto('/ui/en/security?package=quader-test-alias');
      const rows = page.locator('quader-data-table tbody tr').filter({ hasText: 'quader-test-alias' });
      await expect(rows.filter({ hasText: shop })).toHaveCount(1);
      await expect(rows.filter({ hasText: e2eProject })).not.toHaveCount(0);
      // Q-UI-380: the project is the link of each qualified repository name, not a column of its own.
      await expect(page.getByRole('columnheader', { name: 'Project' })).toHaveCount(0);
      await expect(rows.filter({ hasText: shop }).locator('td.qualified-name').getByRole('link', { name: shop })).toBeVisible();
      for (const scheme of ['light', 'dark'] as const) {
        await page.emulateMedia({ colorScheme: scheme });
        await expectAccessible(page, `the security dashboard (${scheme})`);
      }
      await page.emulateMedia({ colorScheme: 'light' });
    });

    await test.step('an alias pasted into the search, in lower case, finds the finding of both projects', async () => {
      await page.goto('/ui/en/security');
      const search = page.getByRole('searchbox', { name: 'Search by advisory, alias, package or summary' });
      await search.fill('cve-2026-0001');
      await search.press('Enter');
      await expect(page).toHaveURL(/advisory=CVE-2026-0001/);
      await expect(search).toHaveValue('CVE-2026-0001');
      const rows = page.locator('quader-data-table tbody tr');
      await expect(rows.filter({ hasText: shop })).toHaveCount(1);
      await expect(rows.filter({ hasText: qualified(repoA) })).toHaveCount(1);
      await expect(rows.filter({ hasText: 'GHSA-aaaa-bbbb-cccc' })).not.toHaveCount(0);
      await expect(rows.filter({ hasText: 'quader-test-cvss' })).toHaveCount(0);
    });

    await test.step('the project chosen in the drawer narrows the list, the counts and the URL to one project', async () => {
      const drawer = await openFilters(page);
      await drawer.getByRole('checkbox', { name: 'Security journey' }).check();
      await applyFilters(page);
      await expect(page).toHaveURL(new RegExp(`project=${shop}`));
      const rows = page.locator('quader-data-table tbody tr');
      await expect(rows).toHaveCount(1);
      await expect(rows.first()).toContainText(qualified(repoB, shop));
      await expect(page.getByRole('list', { name: 'Filters that are set' })).toContainText('Project: Security journey');
      await expect(page.getByText('The counts and the trend cover the projects, repositories and formats of the filters.')).toBeVisible();
    });

    await test.step('a text search with the project lists that project\'s matches, and the counts cover that project', async () => {
      await page.goto(`/ui/en/security?project=${shop}`);
      const rows = page.locator('quader-data-table tbody tr');
      await expect(rows).toHaveCount(2);
      for (const row of await rows.all()) await expect(row).toContainText(shop);
      const search = page.getByRole('searchbox', { name: 'Search by advisory, alias, package or summary' });
      await search.fill('quader-test-cvss');
      await search.press('Enter');
      await expect(page).toHaveURL(/q=quader-test-cvss/);
      await expect(rows).toHaveCount(1);
      await expect(rows.first()).toContainText('quader-test-cvss');
      // the counts are the project's, not the search's: both open findings of the project need triage
      const counts = page.getByRole('list', { name: 'Open findings by state' });
      await expect(counts.getByRole('link', { name: /^2\s*Needs triage/ })).toBeVisible();
    });

    await test.step('a project cell narrows the list to its project, and a finding opens below /security', async () => {
      await page.goto('/ui/en/security?package=quader-test-alias');
      await page.locator('quader-data-table tbody tr').filter({ hasText: shop }).getByRole('link', { name: shop, exact: true }).click();
      await expect(page).toHaveURL(new RegExp(`project=${shop}`));
      await page.getByRole('link', { name: 'GHSA-aaaa-bbbb-cccc' }).first().click();
      await expect(page).toHaveURL(/\/ui\/en\/security\/findings\//);
      // The advisory names the page; the way back is the breadcrumb (Q-UI-968).
      await expect(page.getByRole('heading', { name: 'GHSA-aaaa-bbbb-cccc', level: 1 })).toBeVisible();
      await expect(page.getByRole('navigation', { name: 'Breadcrumb' }).getByRole('link', { name: 'Security dashboard' })).toHaveAttribute('href', /\/security$/);
      await expect(page.getByRole('navigation', { name: 'Breadcrumb' })).not.toContainText('Admin area');
      await expectAccessible(page, 'a finding opened from the dashboard');
    });

    await test.step('the old address in the admin area is gone', async () => {
      await page.goto('/ui/en/admin/security');
      await expect(page.getByRole('heading', { name: 'Page not found', level: 1 })).toBeVisible();
    });
    expect(replicas.size, `replicas that answered: ${[...replicas]}`).toBeGreaterThanOrEqual(2);
  } finally {
    await admin.delete(repositoryApi(repoA));
    await admin.delete(repositoryApi(repoB, shop));
    await admin.delete(`/api/v1/projects/${shop}`);
  }
});

test('R-UI-158 R-VULN-013 an auditor who is a member of no project finds the dashboard under Your work, reads every project and triages one finding', async ({
  browser,
  admin,
}) => {
  const shop = unique('sec');
  const repoB = unique('sec-c');
  const created = await admin.post('/api/v1/projects', { data: { key: shop, displayName: 'Security journey' } });
  expect(created.status(), await created.text()).toBe(201);
  const bobContext = await browser.newContext({ ignoreHTTPSErrors: true, locale: 'en-US' });
  try {
    await localNpmRepositoryIn(admin, shop, repoB, [['quader-test-cvss', '1.0.0']]);
    await findingsOfProject(admin, shop, 1);
    const bob = await bobContext.newPage();
    await signInWithKeycloak(bob, 'bob', bobPassword);
    // bob is created at his first sign-in; the auditor role reads every repository's metadata and triages
    const bound = await admin.put('/api/v1/role-bindings/auditor/user%3Abob');
    expect(bound.status(), await bound.text()).toBeLessThan(300);

    await test.step('Security audit in his side bar of Your work leads to the dashboard, which lists the project he is no member of', async () => {
      await bob.goto('/ui/en/projects');
      await bob.getByRole('navigation', { name: 'Main navigation' }).getByRole('link', { name: 'Security dashboard' }).click();
      await expect(bob.getByRole('heading', { name: 'Security dashboard', level: 1 })).toBeVisible();
      await bob.goto(`/ui/en/security?project=${shop}`);
      const rows = bob.locator('quader-data-table tbody tr');
      await expect(rows).toHaveCount(1);
      await expect(rows.first()).toContainText('quader-test-cvss');
      await expectAccessible(bob, 'the dashboard of an auditor');
    });

    await test.step('he confirms the finding, and the list and the counts follow', async () => {
      await bob.getByRole('button', { name: 'Select all loaded findings' }).click();
      await bob.getByRole('button', { name: 'Change status' }).click();
      const dialog = bob.getByRole('dialog');
      await dialog.getByRole('button', { name: 'Save' }).click();
      await expect(dialog).toContainText('1 changed, 0 failed, 0 unchanged');
      await dialog.getByRole('button', { name: 'Close' }).click();
      await bob.goto(`/ui/en/security?project=${shop}&status=confirmed`);
      await expect(bob.locator('quader-data-table tbody tr')).toHaveCount(1);
      await expect(bob.locator('quader-data-table tbody tr').first()).toContainText('Confirmed');
    });
    await test.step('without the role the section is gone and the address answers Page not found', async () => {
      await admin.delete('/api/v1/role-bindings/auditor/user%3Abob');
      // a fresh page reads authz/me again
      const again = await bobContext.newPage();
      await again.goto('/ui/en/projects');
      await expect(again.getByRole('navigation', { name: 'Main navigation' })).not.toContainText('Security audit');
      await again.goto('/ui/en/security');
      await expect(again.getByRole('heading', { name: 'Page not found', level: 1 })).toBeVisible();
    });
  } finally {
    await bobContext.close();
    await admin.delete('/api/v1/role-bindings/auditor/user%3Abob');
    await admin.delete(repositoryApi(repoB, shop));
    await admin.delete(`/api/v1/projects/${shop}`);
  }
});
