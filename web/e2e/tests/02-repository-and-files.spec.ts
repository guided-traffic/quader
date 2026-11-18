import { readFile } from 'node:fs/promises';
import { e2eProject, expect, expectAccessible, repositories, repositoryApi, signInLocal, signOut, test, unique } from '../support';

// R-UI-047 journey "a new repository" with its files (R-UI-010, R-UI-011, R-UI-012, R-UI-020, R-UI-080, ADR-203, E1.11): the
// administrator creates a repository with the format chooser and its form, uploads a file on the repository's page, opens the
// file's own page and downloads it, with the requests spread over three replicas.

const repoKey = unique('ui-files');
const content = `quader browser journey ${repoKey}\n`;

test.afterAll(async ({ admin }) => {
  await admin.delete(repositoryApi(repoKey));
});

test('R-UI-010 R-UI-011 R-UI-012 R-UI-080 create, upload on the repository page, open the file and download it', async ({ page, replicas }) => {
  await signInLocal(page);

  await test.step('the format chooser and the form create a local generic repository', async () => {
    await page.goto(`/ui/en/projects/${e2eProject}/repositories`);
    await expect(page.getByRole('heading', { name: 'Repositories', level: 1 })).toBeVisible();
    await expect(page.getByRole('navigation', { name: 'Breadcrumb' })).toContainText(e2eProject);
    await expectAccessible(page, 'repository list of the project');
    await page.getByRole('link', { name: 'Create repository' }).click();
    // The format first, as a page of tiles under a search field, each a link to its form (R-UI-080).
    await expect(page.getByRole('heading', { name: 'New repository', level: 1 })).toBeVisible();
    await expect(page.getByRole('button', { name: 'Next' })).toHaveCount(0);
    await expectAccessible(page, 'format chooser');
    await page.getByRole('searchbox', { name: 'Search by package type' }).fill('gener');
    await expect(page.locator('main a.tile')).toHaveCount(1);
    await page.getByRole('searchbox', { name: 'Search by package type' }).press('Enter');

    // One form named after the format; generic has only the class local, the others are shown unavailable.
    await expect(page.getByRole('heading', { name: 'New Generic repository', level: 1 })).toBeVisible();
    await expect(page.getByRole('radio', { name: /^Local/ })).toBeChecked();
    await expect(page.getByRole('radio', { name: /^Remote/ })).toBeDisabled();
    await expect(page.getByText('Generic does not implement the class Virtual.')).toBeVisible();
    // Inside a project the project is given, not chosen (Q-UI-129); the key starts with a free suggestion and shows no error.
    await expect(page.getByLabel('Project', { exact: true })).toHaveCount(0);
    await expect(page.locator('#f-key')).toHaveValue(/^generic-local/);
    await expect(page.locator('main .field-error')).toHaveCount(0);
    await expectAccessible(page, 'creation form of a generic repository');
    await page.locator('#f-key').fill(repoKey);
    await page.getByRole('button', { name: 'Create repository' }).click();
    await expect(page).toHaveURL(new RegExp(`/projects/${e2eProject}/repositories/${repoKey}$`));
    await expectAccessible(page, 'repository detail');
  });

  await test.step('the page of the new repository offers the upload; a file is uploaded in the browser', async () => {
    // Created and empty: the toolbar offers the upload, the empty state says so (Q-UI-512).
    await expect(page.getByText('This repository holds no files yet.')).toBeVisible();
    await page.getByRole('button', { name: 'Upload files' }).click();
    await expect(page.getByRole('heading', { name: 'Upload files' })).toBeVisible();
    await page.locator('input[type=file]').setInputFiles({
      name: 'hello.txt',
      mimeType: 'text/plain',
      buffer: Buffer.from(content),
    });
    await expectAccessible(page, 'upload dialog');
    await page.getByRole('button', { name: 'Upload', exact: true }).click();
    await expect(page.getByText('uploaded', { exact: true })).toBeVisible();
    await page.getByRole('button', { name: 'Close' }).click();
  });

  await test.step('the list of the repository shows the file, and its name opens the page of the file', async () => {
    await page.goto(`/ui/en/projects/${e2eProject}/repositories/${repoKey}`);
    await expect(page.getByRole('table')).toContainText('hello.txt');
    await expectAccessible(page, 'files of the repository');
    await page.getByRole('link', { name: 'hello.txt' }).click();
    await expect(page).toHaveURL(new RegExp(`/projects/${e2eProject}/repositories/${repoKey}/files/hello\\.txt$`));
  });

  await test.step('the page of the file downloads the original bytes', async () => {
    await page.goto(`/ui/en/projects/${e2eProject}/repositories/${repoKey}/files/hello.txt`);
    await expect(page.getByRole('navigation', { name: 'Folder path' }).locator('[aria-current="page"]')).toContainText('hello.txt');
    await expect(page.locator('.file-facts').getByText('SHA-256')).toBeVisible();
    await expectAccessible(page, 'page of a file');
    const download = page.waitForEvent('download');
    await page.getByRole('link', { name: 'Download' }).click();
    const file = await download;
    expect(file.suggestedFilename()).toBe('hello.txt');
    expect(await readFile(await file.path(), 'utf8')).toBe(content);
  });

  await signOut(page);
  expect(replicas.size, `replicas that answered: ${[...replicas]}`).toBeGreaterThanOrEqual(2);
});

test('R-UI-066 Q-UI-426 the repository list of a project offers Set up client for a row and no address, the one of the admin area a copy button that puts the address on the clipboard', async ({ page, context, admin }) => {
  await context.grantPermissions(['clipboard-read', 'clipboard-write']);
  const key = unique('ui-address');
  const created = await admin.post(repositories(), {
    data: { key, class: 'local', format: 'generic', description: 'client address field' },
  });
  expect(created.ok(), await created.text()).toBe(true);
  try {
    const listed = await admin.get(`${repositories()}?q=${key}`);
    const row = ((await listed.json()) as { items: { key: string; addresses: string[] }[] }).items.find((r) => r.key === key);
    expect(row?.addresses[0], 'the API gives the repository a client address').toBeTruthy();
    const address = row!.addresses[0]!;

    await signInLocal(page);
    await page.goto(`/ui/en/projects/${e2eProject}/repositories?q=${key}`);
    // The address means nothing without its client: the row offers the client's setup, where the address stands (Q-UI-426).
    await expect(page.getByRole('textbox', { name: `Client address of ${key}` })).toHaveCount(0);
    await expect(page.getByRole('searchbox', { name: 'Filter repositories' })).toHaveValue(key);
    await expectAccessible(page, 'repository list of a project');
    await page.getByRole('link', { name: `Set up client ${key}` }).click();
    await expect(page).toHaveURL(new RegExp(`/projects/${e2eProject}/repositories/${key}/setup$`));

    // The admin area's list has no field: a quiet copy button beside the row's menu carries the address (Q-UI-330).
    await page.evaluate(() => navigator.clipboard.writeText(''));
    await page.goto(`/ui/en/admin/repositories?q=${key}`);
    await expect(page.getByRole('textbox', { name: `Client address of ${key}` })).toHaveCount(0);
    await page.getByRole('button', { name: `Copy client address ${address}` }).click();
    expect(await page.evaluate(() => navigator.clipboard.readText())).toBe(address);
    await signOut(page);
  } finally {
    await admin.delete(repositoryApi(key));
  }
});
