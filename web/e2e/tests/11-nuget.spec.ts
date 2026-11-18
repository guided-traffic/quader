import { e2eProject, expect, expectAccessible, fillKey, qualified, repositories, repositoryApi, signInLocal, signOut, test, unique } from '../support';

// R-UI-047 journey "nuget" (WP3.7; R-UI-007, R-UI-018, R-UI-019, R-UI-021): the creation form creates a local NuGet
// repository and writes the one setting that differs from the default; the edit form reads it back; the repository's page
// Set up client offers the .NET CLI, nuget.exe and Visual Studio 2022 and shows, for each, the snippets of R-NUGET-026 with the
// index.json URL of the repository, and the NuGet guide below them (R-UI-120). Every screen passes axe in the
// light and in the dark colour scheme. The journey creates its own repositories and publishes nothing: the package
// list and detail of a pushed package are covered by the component tier (package-detail.spec.ts) and by the NuGet
// client matrix of the end-to-end suite.

const localKey = unique('ui-nuget');
const virtualKey = unique('ui-nuget-all');
const wizardKey = unique('ui-nuget-wizard');

test.beforeAll(async ({ admin }) => {
  const local = await admin.post(repositories(), {
    data: { key: localKey, class: 'local', format: 'nuget' },
  });
  expect(local.status(), await local.text()).toBe(201);
  const virtual = await admin.post(repositories(), {
    data: {
      key: virtualKey,
      class: 'virtual',
      format: 'nuget',
      virtual: { members: [localKey], deployTarget: localKey },
    },
  });
  expect(virtual.status(), await virtual.text()).toBe(201);
});

test.afterAll(async ({ admin }) => {
  await admin.delete(repositoryApi(virtualKey));
  await admin.delete(repositoryApi(localKey));
  await admin.delete(repositoryApi(wizardKey));
});

test('R-UI-018 R-UI-019 the NuGet clients, their snippets with the index.json URL, and the guide, in both colour schemes', async ({
  page,
  replicas,
  baseURL,
}) => {
  await signInLocal(page);
  const port = new URL(baseURL ?? '').port;
  const feed = (key: string) => `https://localhost:${port}/api/nuget/${e2eProject}/${key}/v3/index.json`;

  const setupPath = `/ui/en/projects/${e2eProject}/repositories/${virtualKey}/setup`;
  const configure = page.locator('section[aria-labelledby="step-configure"] quader-setup-snippet pre');
  const client = page.getByRole('combobox', { name: 'Client', exact: true });

  await test.step('Set up client of a NuGet repository offers its three clients in one control', async () => {
    await page.goto(setupPath);
    await expect(page.getByRole('heading', { name: 'Set up client', level: 1 })).toBeVisible();
    // Opened from the repository, the page asks neither its format nor the repository (R-UI-120): the header names them.
    await expect(page.locator('.context')).toContainText(qualified(virtualKey));
    for (const label of ['.NET CLI (dotnet)', 'nuget.exe', 'Visual Studio 2022']) {
      await expect(client.getByRole('option', { name: label, exact: true })).toBeAttached();
    }
  });

  await test.step('the .NET CLI: the source command with the clear-text flag and a nuget.config that clears other sources', async () => {
    await client.selectOption({ label: '.NET CLI (dotnet)' });
    // The server renders the shell form, the PowerShell form and the nuget.config, in that order; the source is
    // always the index.json URL, and the push names the repository the caller chose (a virtual one forwards it).
    await expect(configure.first()).toContainText(`dotnet nuget add source ${feed(virtualKey)} --name quader`);
    await expect(configure.nth(1)).toContainText(`dotnet nuget add source ${feed(virtualKey)} --name quader`);
    await expect(configure.nth(2)).toContainText('<clear />');
    await expect(configure.nth(2)).toContainText(`value="${feed(virtualKey)}" protocolVersion="3"`);
    await expect(page.locator('section[aria-labelledby="step-publish"] quader-setup-snippet pre').first()).toContainText(
      `--source ${feed(virtualKey)}`,
    );
  });

  await test.step('nuget.exe and Visual Studio', async () => {
    await client.selectOption({ label: 'nuget.exe' });
    await expect(configure.filter({ hasText: 'nuget sources add' }).first()).toContainText(feed(virtualKey));
    await client.selectOption({ label: 'Visual Studio 2022' });
    await expect(configure.first()).toContainText(`<add key="quader" value="${feed(virtualKey)}" protocolVersion="3" />`);
  });

  for (const scheme of ['light', 'dark'] as const) {
    await test.step(`axe on Set up client with its guide in the ${scheme} scheme`, async () => {
      await page.emulateMedia({ colorScheme: scheme });
      await page.goto(`${setupPath}?client=dotnet`);
      await expect(page.getByRole('heading', { name: 'Set up client', level: 1 })).toBeVisible();
      await expect(page.locator('quader-guide-view')).toBeVisible();
      await expectAccessible(page, `set up client, NuGet (${scheme})`);
    });
  }

  await signOut(page);
  expect(replicas.size, `replicas that answered: ${[...replicas]}`).toBeGreaterThanOrEqual(2);
});

test('R-UI-021 R-ARCH-004 R-UI-083 the NuGet settings in Advanced settings and on the settings page, rendered from the settings schema', async ({
  page,
  replicas,
  admin,
}) => {
  await signInLocal(page);
  const mode = page.getByRole('combobox', { name: 'Delete mode' });
  const nugetMember = async () => {
    const res = await admin.get(repositoryApi(wizardKey));
    expect(res.status()).toBe(200);
    return ((await res.json()) as { nuget?: Record<string, unknown> }).nuget;
  };

  await test.step('the form offers the delete mode of a local repository in Advanced settings and writes the value that differs from the default', async () => {
    await page.goto('/ui/en/admin/repositories/new/nuget');
    await expect(page.getByRole('heading', { name: 'New NuGet repository', level: 1 })).toBeVisible();
    await page.getByLabel('Project', { exact: true }).selectOption(e2eProject);
    await fillKey(page, 'nuget-local', wizardKey);
    await page.getByRole('button', { name: 'Advanced settings' }).click();
    await expect(page.getByRole('heading', { name: 'NuGet settings', level: 3 })).toBeVisible();
    await expect(page.getByRole('checkbox', { name: 'Forward search' })).toHaveCount(0);
    await expect(mode.locator('option:checked')).toHaveText('Unlist'); // the options carry their index as value and the schema's label as text (format-settings.ts)
    await mode.selectOption({ label: 'Delete for good' });
    for (const scheme of ['light', 'dark'] as const) {
      await page.emulateMedia({ colorScheme: scheme });
      await expectAccessible(page, `creation form, NuGet settings (${scheme})`);
    }
    await page.getByRole('button', { name: 'Create repository' }).click();
    await expect(page).toHaveURL(new RegExp(`/projects/${e2eProject}/repositories/${wizardKey}$`));
    expect(await nugetMember()).toEqual({ deleteMode: 'delete' });
  });

  await test.step('the settings page reads it back and setting the default back removes it from the document', async () => {
    await page.goto(`/ui/en/projects/${e2eProject}/repositories/${wizardKey}/settings`);
    await page.getByRole('button', { name: 'NuGet settings', exact: true }).click();
    await expect(mode.locator('option:checked')).toHaveText('Delete for good');
    await expectAccessible(page, 'repository settings, NuGet settings');
    await mode.selectOption({ label: 'Unlist' });
    await page.getByRole('button', { name: 'Save changes (NuGet settings)' }).click();
    await expect(page.getByText('Repository saved.')).toBeVisible();
    await expect.poll(nugetMember).toBeUndefined();
  });

  await signOut(page);
  expect(replicas.size, `replicas that answered: ${[...replicas]}`).toBeGreaterThanOrEqual(2);
});
