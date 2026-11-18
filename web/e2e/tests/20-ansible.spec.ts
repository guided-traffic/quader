import { e2eProject, expect, expectAccessible, qualified, repositories, repositoryApi, signInLocal, signOut, test, unique } from '../support';

// R-UI-047 journey "ansible" (R-ANS-025, R-ANS-026, R-ANS-012, R-UI-049, R-UI-120): the repository's page Set up client renders
// the ansible.cfg with the address of the repository and no token, and below it the guide with the sections for a local repository
// and the netrc line the roles need; the repository's page leads there with Set up client; the edit form
// has the tabs for the import tasks and the tombstones, which say so when there is nothing yet. Every screen passes axe in the
// light and in the dark colour scheme. The journey creates its own repository and publishes nothing: the screens of a published
// collection are covered by the component tier (package-detail.spec.ts) and by the ansible-galaxy matrix of the end-to-end suite.

const localKey = unique('ui-ansible');

test.beforeAll(async ({ admin }) => {
  const local = await admin.post(repositories(), { data: { key: localKey, class: 'local', format: 'ansible' } });
  expect(local.status(), await local.text()).toBe(201);
});

test.afterAll(async ({ admin }) => {
  await admin.delete(repositoryApi(localKey));
});

test('R-ANS-025 R-UI-049 R-UI-120 Set up client of a local repository renders the server without a token, with the guide below', async ({
  page,
  replicas,
  baseURL,
}) => {
  await signInLocal(page);
  const port = new URL(baseURL ?? '').port;
  const address = `https://localhost:${port}/api/ansible/${e2eProject}/${localKey}/`;

  await test.step('Set up client renders the ansible.cfg with the server and without a token', async () => {
    await page.goto(`/ui/en/projects/${e2eProject}/repositories/${localKey}/setup`);
    await expect(page.getByRole('heading', { name: 'Set up client', level: 1 })).toBeVisible();
    await expect(page.locator('.context')).toContainText(qualified(localKey));
    // ansible-galaxy is the format's one client: the page asks nothing (Q-UI-482).
    await expect(page.getByRole('combobox', { name: 'Client' })).toHaveCount(0);
    const configure = page.locator('section[aria-labelledby="step-configure"]');
    const cfg = configure.locator('quader-setup-snippet pre').first();
    await expect(cfg).toContainText('server_list = quader');
    await expect(cfg).toContainText(`url=${address}`);
    await expect(cfg).not.toContainText('token');
    await expect(configure).toContainText('ANSIBLE_GALAXY_SERVER_QUADER_TOKEN');
    for (const scheme of ['light', 'dark'] as const) {
      await page.emulateMedia({ colorScheme: scheme });
      await expectAccessible(page, `set up client, ansible-galaxy (${scheme})`);
    }
  });

  await test.step('the guide below the snippets: what a local repository is open, the other sections under their headings', async () => {
    await expect(page.getByRole('heading', { name: 'What a local repository is', level: 2 })).toBeVisible();
    // The owner of the journey's repository may publish, so the publishing sections are there (R-API-070).
    for (const name of ['Which credential ansible-galaxy sends', 'Remove a version, import tasks and tombstones']) {
      await expect(page.getByRole('button', { name, exact: true })).toBeVisible();
    }
    await expect(page.locator('body')).toContainText('~/.netrc');
    await page.goto(`/ui/en/projects/${e2eProject}/repositories/${localKey}`);
    await expect(page.locator('quader-page-header').getByRole('link', { name: 'Set up client', exact: true })).toHaveAttribute(
      'href',
      new RegExp(`/repositories/${localKey}/setup$`),
    );
  });

  await signOut(page);
  expect(replicas.size, `replicas that answered: ${[...replicas]}`).toBeGreaterThanOrEqual(2);
});

test('R-ANS-026 R-ANS-012 the settings page has the import tasks and the tombstones, each with its empty line', async ({ page, replicas }) => {
  await signInLocal(page);
  await page.goto(`/ui/en/projects/${e2eProject}/repositories/${localKey}/settings`);

  await test.step('the section Import tasks says that nothing was published', async () => {
    await page.getByRole('button', { name: 'Import tasks', exact: true }).click();
    await expect(page.getByTestId('ansible-imports-table')).toContainText('No collection has been published to this repository yet');
    for (const scheme of ['light', 'dark'] as const) {
      await page.emulateMedia({ colorScheme: scheme });
      await expectAccessible(page, `repository detail, import tasks (${scheme})`);
    }
  });

  await test.step('the section Tombstones says that no number is blocked', async () => {
    await page.getByRole('button', { name: 'Tombstones', exact: true }).click();
    await expect(page.getByTestId('ansible-tombstones-table')).toContainText('no number is blocked');
    for (const scheme of ['light', 'dark'] as const) {
      await page.emulateMedia({ colorScheme: scheme });
      await expectAccessible(page, `repository detail, tombstones (${scheme})`);
    }
  });

  await signOut(page);
  expect(replicas.size, `replicas that answered: ${[...replicas]}`).toBeGreaterThanOrEqual(2);
});
