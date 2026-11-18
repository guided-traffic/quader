import { e2eProject, expect, expectAccessible, repositories, repositoryApi, signInLocal, signOut, test, unique } from '../support';

// R-UI-047 journey "guides" (E2.13; R-UI-049, R-UI-120 to R-UI-125, R-API-053): the user menu opens Guides with the formats
// as tiles; the npm tile lists the npm repositories, the journey's virtual one among them with its class; its Set up client
// opens the repository's page with the four npm clients in one control, the snippets first and the virtual repository's
// explanation below them; the configure snippet the page shows is, byte for byte, the one the guide's answer carries for the
// variable (R-API-070). The journey creates its own local and virtual repository: the journeys do not own the matrix's npm-all.

const localKey = unique('ui-guide-local');
const virtualKey = unique('ui-guide-all');

test.beforeAll(async ({ admin }) => {
  const local = await admin.post(repositories(), { data: { key: localKey, class: 'local', format: 'npm' } });
  expect(local.status(), await local.text()).toBe(201);
  const virtual = await admin.post(repositories(), {
    data: { key: virtualKey, class: 'virtual', format: 'npm', virtual: { members: [localKey], deployTarget: localKey } },
  });
  expect(virtual.status(), await virtual.text()).toBe(201);
});

test.afterAll(async ({ admin }) => {
  await admin.delete(repositoryApi(virtualKey));
  await admin.delete(repositoryApi(localKey));
});

test('E2.13 R-UI-049 R-UI-124 Guides lead from a tile to the page Set up client of a virtual repository, the snippets first and the guide below', async ({
  page,
  replicas,
}) => {
  await signInLocal(page);

  await test.step('the user menu opens Guides: the formats as tiles, nothing to choose', async () => {
    await page.goto(`/ui/en/projects/${e2eProject}/packages`);
    await page.getByRole('button', { name: /User menu/ }).click();
    await page.getByRole('menuitem', { name: 'Guides', exact: true }).click();
    await expect(page.getByRole('heading', { name: 'Guides', level: 1 })).toBeVisible();
    const tiles = page.getByRole('list', { name: 'Package types' });
    for (const name of ['Generic', 'npm']) await expect(tiles.getByRole('link', { name, exact: true })).toBeVisible();
    await expect(page.locator('main input, main select')).toHaveCount(0);
    await expectAccessible(page, 'guides');
    await tiles.getByRole('link', { name: 'npm', exact: true }).click();
  });

  await test.step('the npm tile lists the npm repositories with their class, and the guide of each class', async () => {
    await expect(page.getByRole('heading', { name: 'npm repositories', level: 1 })).toBeVisible();
    const row = page.getByRole('row').filter({ hasText: `${e2eProject}/${virtualKey}` });
    await expect(row).toContainText('Virtual');
    await expect(page.getByRole('link', { name: /^A local repository/ })).toBeVisible();
    await expectAccessible(page, 'guides of npm');
    await row.getByRole('link', { name: 'Set up client', exact: true }).click();
  });

  await test.step('Set up client of the virtual repository: the client is one control, the snippets come first, the explanation after', async () => {
    await expect(page).toHaveURL(new RegExp(`/projects/${e2eProject}/repositories/${virtualKey}/setup`));
    await expect(page.getByRole('heading', { name: 'Set up client', level: 1 })).toBeVisible();
    const client = page.getByRole('combobox', { name: 'Client' });
    for (const label of ['npm', 'pnpm', 'Yarn Berry (2 and later)', 'Yarn Classic (1.x)']) {
      await expect(client.getByRole('option', { name: label, exact: true })).toBeAttached();
    }
    await expect(page.locator('.credential-line')).toContainText('The snippets read your token from the variable QUADER_TOKEN.');
    const configure = page.locator('section[aria-labelledby="step-configure"] quader-setup-snippet pre').first();
    await expect(configure).toContainText(`/api/npm/${e2eProject}/${virtualKey}/:_authToken=\${QUADER_TOKEN}`);
    await expect(page.getByRole('heading', { name: 'What a virtual repository is', level: 2 })).toBeVisible();
    await page.getByRole('button', { name: 'More about this' }).click();
    await expect(page.getByText(new RegExp(`For (${e2eProject}/)?${virtualKey} that is (${e2eProject}/)?${localKey}\\.`))).toBeVisible();
    await expectAccessible(page, 'set up client');

    // The page's configure snippet is the guide's for the variable (R-API-070): the same rendering, one source.
    const guide = await page.request.get(`/api/v1/projects/${e2eProject}/repositories/${virtualKey}/guide?client=npm`);
    expect(guide.status()).toBe(200);
    const fromGuide = ((await guide.json()) as { snippets: { tab: string; content: string }[] }).snippets.find((s) => s.tab === 'configure');
    const shown = await configure.locator('.line').allTextContents();
    expect(shown.join('\n')).toBe(fromGuide?.content.replace(/\n$/, ''));
  });

  await signOut(page);
  expect(replicas.size, `replicas that answered: ${[...replicas]}`).toBeGreaterThanOrEqual(2);
});
