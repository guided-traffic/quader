import { Page, expect } from '@playwright/test';
import { holdCalls } from './routes';
import { answerNpm, open, packageUrl, previewUrl, searchUrl, tab } from './upstream';

/**
 * The loads of R-UI-217, held open so that each picture shows the spinner of the mark (Q-UI-1229) where it stands: a list's load in the
 * table's place (a search whose answer is held), the busy **Search** button with it, an opened row's line ("Reading the package from
 * the upstream."), a dialog's line (the preview of a download ahead) and a page's load (the repository's first read). The seed's npm
 * remote is answered in the browser as the tab Upstream's walk answers it (upstream.ts); each held call is released before the next
 * state, and its answer goes on to those routes. The controls are found by their place, never by their words: the German pass walks it.
 */
export async function* loadingWalk(p: Page): AsyncGenerator<string> {
  await answerNpm(p);

  // A list that loads a new question: the table's place holds the spinner and the sentence of the search (Q-UI-1234).
  let release = await holdCalls(p, searchUrl);
  await open(p, '?q=gitlab');
  await expect(p.locator(`${tab} quader-data-table quader-loading`)).toBeVisible();
  yield 'list';
  await release();
  await expect(p.locator(`${tab} tbody tr:not(.expansion)`)).toHaveCount(6);

  // A busy act: the next search sent with the Search button, which is held and spins while the table loads.
  release = await holdCalls(p, searchUrl);
  await p.locator(`${tab} form.search input`).first().fill('gitbeaker');
  await p.locator(`${tab} form.search button[type="submit"]`).first().click();
  await expect(p.locator(`${tab} form.search button[aria-busy="true"] quader-spinner`)).toBeVisible();
  yield 'search-busy';
  await release();
  await expect(p.locator(`${tab} tbody tr:not(.expansion)`)).toHaveCount(6);

  // An opened row whose package is read from the upstream: the line under the row.
  release = await holdCalls(p, packageUrl);
  await p.locator(`${tab} button[aria-controls="up-gitlab-ai-provider"]`).click();
  await expect(p.locator(`${tab} quader-upstream-package quader-loading`)).toBeVisible();
  yield 'package';
  await release();
  await expect(p.locator(`${tab} quader-upstream-package tbody tr`).first()).toBeVisible();

  // A dialog that reads before it shows: the preview of a download ahead.
  const every = p.locator(`${tab} [data-testid="upstream-download-every"]`).first();
  if ((await every.count()) > 0) {
    release = await holdCalls(p, previewUrl, 'POST');
    await every.click();
    await expect(p.locator('quader-download-preview-dialog quader-loading')).toBeVisible();
    yield 'preview';
    await release();
    await p.keyboard.press('Escape');
    await expect(p.locator('quader-download-preview-dialog')).toBeHidden();
  }

  // A page's first load: the repository's own read held, the page's spinner of 48 px above its sentence.
  release = await holdCalls(p, (url) => /^\/api\/v1\/projects\/[^/]+\/repositories\/npmjs$/.test(url.pathname));
  await open(p, '');
  await expect(p.locator('main quader-loading.quader-loading-page').first()).toBeVisible();
  yield 'page';
  await release();
  await p.unrouteAll({ behavior: 'ignoreErrors' });
}

const isSession = (url: URL): boolean => url.pathname === '/api/v1/auth/whoami';
const isAccount = (url: URL): boolean => url.pathname === '/api/v1/users/me';
const isPreferences = (url: URL): boolean => url.pathname === '/api/v1/users/me/preferences';

/**
 * Where a load stands (R-UI-252), on the Profile page: the first screen while the session is read (the spinner of the first paint in the
 * middle of the window, no scrollbar), the page's first load (the spinner and its sentence in the middle of the content below the
 * page's title and tabs), and the page after a 503 of its account's read with its preferences still held: the error above, the page's
 * load below it. The render's check `loading-place` measures each; the calls are held or answered in the browser, nothing is written.
 */
export async function* pageLoadWalk(p: Page): AsyncGenerator<string> {
  let release = await holdCalls(p, isSession);
  await p.reload();
  await expect(p.locator('.quader-boot').first()).toBeVisible();
  yield 'boot';
  await release();

  let releasePreferences = await holdCalls(p, isPreferences);
  release = await holdCalls(p, isAccount);
  await p.reload();
  await expect(p.locator('main quader-loading.quader-loading-page')).toBeVisible();
  yield 'page';
  await release();
  await releasePreferences();

  releasePreferences = await holdCalls(p, isPreferences);
  await p.route(isAccount, (route) =>
    route.request().method() === 'GET'
      ? route.fulfill({
          status: 503,
          contentType: 'application/problem+json',
          body: JSON.stringify({ type: 'about:blank', title: 'Service Unavailable', status: 503, code: 'unavailable', detail: 'The service is unavailable.' }),
        })
      : route.fallback(),
  );
  await p.reload();
  await expect(p.locator('main [data-testid="error-notice"]')).toHaveCount(1);
  await expect(p.locator('main quader-loading.quader-loading-page')).toBeVisible();
  yield 'failed';
  await releasePreferences();
  await p.unrouteAll({ behavior: 'ignoreErrors' });
}

/** The answers the walk gives in the browser. */
export const pageLoadExpected = [{ match: /^GET \/api\/v1\/users\/me answered 503$/, why: 'the walk answers the account with a 503 to picture the load below the error' }];
/** The walk's notice is the state it renders. */
export const pageLoadNotices = [{ match: /^/, why: "the 503 of the account's read, which the walk renders above the page's load" }];
