import { Page, expect } from '@playwright/test';

/*
 * A generic file's commands behind Download (R-GENERIC-017, Q-UI-1330 to Q-UI-1335): the file's page with the panel of its chevron
 * open, one picture per client's tab; a file's row in the list with its panel; a folder's Upload files with the panel of its upload.
 * The controls are found by their place, never by their words: the German pass walks it.
 */

/** Waits for the chosen tab's command in the open panel. */
async function commandShown(page: Page): Promise<void> {
  await expect(page.locator('.client-command-panel [role="tabpanel"]:not([inert]) quader-address-field input').first()).toBeVisible();
  await page.waitForLoadState('networkidle');
}

/** The state of each tab, by the client's id: a picture's name drops trailing digits. */
const TAB_NAMES = ['curl', 'wget', 'powershell'];

/** Each client's tab of the open panel, one picture each: curl, wget, PowerShell. */
async function* everyTab(page: Page, name: string): AsyncGenerator<string> {
  await commandShown(page);
  const tabs = page.locator('.client-command-panel [role="tab"]');
  const count = await tabs.count();
  for (let i = 0; i < count; i++) {
    if (i > 0) {
      await tabs.nth(i).click();
      await commandShown(page);
    }
    yield `${name}-${TAB_NAMES[i] ?? `tab-${String.fromCharCode(97 + i)}`}`;
  }
}

/** The file's page as it opens, then the panel of its chevron with each client's tab. */
export async function* fileCommands(page: Page): AsyncGenerator<string> {
  await expect(page.locator('main .file-facts')).toBeVisible();
  yield '';
  await page.locator('main .file-actions quader-client-command-menu button[aria-haspopup="dialog"]').click();
  yield* everyTab(page, 'download');
}

/** A file's row in its folder's list: the panel of its Download. */
export async function* rowFileCommands(page: Page): AsyncGenerator<string> {
  await expect(page.locator('main quader-data-table tbody tr').first()).toBeVisible();
  await page.locator('main quader-data-table tbody quader-client-command-menu button').first().click();
  await commandShown(page);
  yield 'row';
}

/** A folder's Upload files and the panel of its chevron, the upload into the folder in each client. */
export async function* folderUpload(page: Page): AsyncGenerator<string> {
  const chevron = page.locator('main .files-actions quader-client-command-menu button[aria-haspopup="dialog"]');
  await expect(chevron).toBeVisible();
  await chevron.click();
  yield* everyTab(page, 'upload');
}
