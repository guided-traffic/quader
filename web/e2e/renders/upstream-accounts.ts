import { Page, expect } from '@playwright/test';

/**
 * The accounts of the upstream registries (repositories-and-proxy.md R-REPO-077, api-and-ui.md R-UI-188, Q-UI-1160 to Q-UI-1162): the
 * page of Docker Hub in the admin area without an account (the sentence that an account allows more), with an account the Helm values
 * declare whose files are readable, and with one whose Secret is missing; and the tab Downloads of a queue Docker Hub's limit holds, with
 * the sentence that offers an account, as each kind of reader reads it. The profile declares no account and no upstream limits it, so
 * both are answered in the browser from the server's own item and the shapes of the API. Nothing is changed on the server; no control
 * is found by an English word, so the German pass walks them too.
 */

const itemUrl = '**/api/v1/upstream-repositories/docker-hub';

/** The time `minutes` from now, RFC 3339. */
function at(minutes: number): string {
  return new Date(Date.now() + minutes * 60_000).toISOString();
}

/** The account the Helm values declare, its files in the state `files` on the answering server. */
function declared(files: 'readable' | 'missing'): Record<string, unknown> {
  return {
    authMode: 'basic',
    username: null,
    password: null,
    removedByUpdateAt: null,
    source: 'configuration',
    configuration: { secretName: 'quader-upstream-docker-hub', since: at(-95), files },
    lastDownloadAt: files === 'readable' ? at(-3) : at(-50),
    lastFailure: files === 'readable' ? null : { at: at(-2), code: 'upstream_secret_unavailable' },
  };
}

async function answerAccount(page: Page, files: 'readable' | 'missing'): Promise<void> {
  await page.unroute(itemUrl);
  await page.route(itemUrl, async (route) => {
    if (route.request().method() !== 'GET') return route.fallback();
    const res = await route.fetch();
    const item = (await res.json()) as Record<string, unknown>;
    await route.fulfill({ response: res, body: JSON.stringify({ ...item, account: declared(files), accountRaisesLimit: false }) });
  });
  await page.reload();
  await expect(page.locator('main [aria-labelledby="wk-account"] .account-facts')).toBeVisible();
}

/** The page of Docker Hub: as served (no account), then an account of the Helm values with readable files, then one whose Secret is missing. */
export async function* registryAccountWalk(page: Page): AsyncGenerator<string> {
  await expect(page.locator('main [aria-labelledby="wk-account"]')).toBeVisible();
  yield '';
  await answerAccount(page, 'readable');
  yield 'configured';
  await answerAccount(page, 'missing');
  await expect(page.locator('main [aria-labelledby="wk-account"] .banner-warning')).toBeVisible();
  yield 'configured-missing';
  await page.unroute(itemUrl);
}

const listUrl = (url: URL): boolean => /^\/api\/v1\/projects\/[^/]+\/repositories\/[^/]+\/downloads$/.test(url.pathname);
const bulksUrl = (url: URL): boolean => /^\/api\/v1\/projects\/[^/]+\/repositories\/[^/]+\/download-bulks$/.test(url.pathname);

/** Docker Hub's limit without an account, as it held the owner's queue on 2026-11-25: a 429 without a time of its own. */
const hubLimit = {
  since: at(-42),
  until: at(14),
  holds: true,
  hinted: false,
  waits: 3,
  upstream: 'Docker Hub',
  accountRaisesLimit: true,
  failure: {
    class: 'upstream_rate_limited',
    step: 'status',
    code: 'upstream_rate_limited',
    file: null,
    address: 'https://registry-1.docker.io/v2/library/postgres/manifests/18.1',
    target: null,
    status: 429,
    excerpt: '{"errors":[{"code":"TOOMANYREQUESTS","message":"You have reached your unauthenticated pull rate limit."}]}',
    retryAfter: null,
    headers: { 'ratelimit-limit': '100;w=21600', 'ratelimit-remaining': '0;w=21600' },
    detail: 'Docker Hub is rate limiting Quader (status 429); Quader asks again after the wait, and the wait is no failed attempt of the file.',
    attempt: 1,
    at: at(-3),
  },
};

function waitingRow(i: number): Record<string, unknown> {
  return {
    id: `0193a1b2-0000-7000-8000-0000000001${String(i).padStart(2, '0')}`,
    name: 'library/postgres',
    version: `18.${i}`,
    origin: 'person',
    state: 'waiting',
    outcome: null,
    attempts: 0,
    maxAttempts: 5,
    nextAttemptAt: null,
    startedBy: null,
    bulk: null,
    request: null,
    files: 0,
    stored: 0,
    present: 0,
    refused: 0,
    bytes: 0,
    occurrences: 1,
    failure: null,
    createdAt: at(-40 + i),
    startedAt: null,
    finishedAt: null,
    delivery: 'delivered',
  };
}

/** The tab Downloads while Docker Hub's limit holds the queue: the limit's sentence and, after it, the offer of an account for this reader. */
export async function* accountOfferWalk(page: Page): AsyncGenerator<string> {
  await page.route(listUrl, (route) => {
    if (route.request().method() !== 'GET') return route.fallback();
    const counts = { waiting: 3, running: 0, failed: 0, done: 0 };
    return route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ items: [1, 2, 3].map(waitingRow), nextCursor: null, counts, limit: hubLimit }),
    });
  });
  await page.route(bulksUrl, (route) =>
    route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ items: [], nextCursor: null }) }),
  );
  await page.reload();
  await expect(page.locator('main [data-testid="download-limit-account"]')).toBeVisible();
  yield 'limited-account';
  await page.unroute(listUrl);
  await page.unroute(bulksUrl);
}
