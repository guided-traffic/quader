import { Page, expect } from '@playwright/test';

/**
 * The tab Downloads of a remote repository (remote-curation.md R-CUR-018) with a queue of every state. The seed's server has a queue
 * only where a probe or a client fetched (no upstream of the internet is reachable for certain), so the walk answers the list in the
 * browser with the rows a cache that is being built shows: a running and a waiting row, a bulk's rows, a client's failure repeated
 * fourteen times, a failure with a long sentence and the 512 bytes of an upstream's error page, a row stopped at a bound and a
 * cancelled one; above them the repository's bulks (Q-CUR-168): one that finds versions, one whose rows run, and one whose expansion
 * failed before it queued a row, with the engine's long sentence; and the queue an upstream's rate limit holds (R-CUR-011, Q-UI-1100):
 * the sentence above the list, the line of its waiting rows and of its bulk, and the upstream's answer folded under the sentence.
 * Nothing is changed on the server.
 */

const BULK = '0193a1b2-0000-7000-8000-0000000000b1';
const BULK_FAILED = '0193a1b2-0000-7000-8000-0000000000b2';
const BULK_EXPANDING = '0193a1b2-0000-7000-8000-0000000000b3';

/** The time `minutes` from now, RFC 3339: the rows' times stay relative to the run. */
function at(minutes: number): string {
  return new Date(Date.now() + minutes * 60_000).toISOString();
}

const errorPage =
  '<!DOCTYPE html><html><head><title>503 Service Unavailable</title></head><body><h1>Service Unavailable</h1><p>The server is ' +
  'temporarily unable to service your request due to maintenance downtime or capacity problems. Please try again later.</p><hr>' +
  '<address>nginx/1.27.3 at registry-cache-eu-west-1b.internal.example.com Port 443, request id 7f3c9a2e-41d8-4b61-9c0e-' +
  'a5d2f1e8b7c4, upstream mirror-pool-3 (10.42.17.9:8080) answered after 30.001 s</address></body></html>';

function row(over: Record<string, unknown>): Record<string, unknown> {
  return {
    id: '',
    name: '',
    version: '',
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
    createdAt: at(-12),
    startedAt: null,
    finishedAt: null,
    delivery: 'delivered',
    ...over,
  };
}

function failure(over: Record<string, unknown>): Record<string, unknown> {
  return {
    class: 'upstream_error',
    step: 'status',
    code: 'upstream_error',
    file: null,
    address: null,
    target: null,
    status: null,
    excerpt: null,
    retryAfter: null,
    detail: '',
    attempt: 1,
    at: at(-3),
    ...over,
  };
}

/** The rows of a queue while a cache is built: newest first within running and waiting first, as the API orders them. */
export function queueRows(): Record<string, unknown>[] {
  return [
    row({ id: '0193a1b2-0000-7000-8000-000000000001', name: '@acme/design-tokens', version: '5.4.2', origin: 'bulk', bulk: BULK, state: 'running', attempts: 1, files: 1, stored: 0, startedAt: at(-1) }),
    row({
      id: '0193a1b2-0000-7000-8000-000000000002',
      name: '@customer-portal/release-candidates-for-the-frontend-design-system-components',
      version: '12.0.0-rc.3+build.20261124.1842',
      origin: 'bulk',
      bulk: BULK,
      state: 'running',
      attempts: 2,
      files: 3,
      stored: 2,
      present: 0,
      bytes: 48_211_456,
      startedAt: at(-2),
    }),
    row({
      id: '0193a1b2-0000-7000-8000-000000000003',
      name: 'lodash',
      version: '4.17.21',
      origin: 'request',
      state: 'waiting',
      attempts: 2,
      nextAttemptAt: at(4),
      failure: failure({
        class: 'upstream_rate_limited',
        step: 'status',
        code: 'upstream_rate_limited',
        file: 'lodash/-/lodash-4.17.21.tgz',
        address: 'https://registry.npmjs.org/lodash/-/lodash-4.17.21.tgz',
        status: 429,
        retryAfter: 240,
        detail: 'The upstream registry.npmjs.org limited the rate of this installation (429 Too Many Requests); Quader tries again after its Retry-After of 240 s.',
        attempt: 2,
      }),
    }),
    row({ id: '0193a1b2-0000-7000-8000-000000000004', name: 'react', version: '19.0.0', origin: 'person', startedBy: { id: null, name: null }, state: 'waiting' }),
    row({
      id: '0193a1b2-0000-7000-8000-000000000005',
      name: 'left-pad',
      version: '1.3.0',
      origin: 'client',
      occurrences: 14,
      state: 'failed',
      attempts: 1,
      maxAttempts: 1,
      finishedAt: at(-4),
      failure: failure({
        class: 'upstream_error',
        step: 'status',
        code: 'upstream_error',
        file: 'left-pad/-/left-pad-1.3.0.tgz',
        address: 'https://registry-cache-eu-west-1b.internal.example.com/npm/left-pad/-/left-pad-1.3.0.tgz',
        status: 503,
        excerpt: errorPage.slice(0, 512),
        detail:
          'The upstream registry-cache-eu-west-1b.internal.example.com answered 503 Service Unavailable for left-pad-1.3.0.tgz after 30 s; the next client fetch or a retry asks it again.',
        attempt: 1,
      }),
    }),
    row({
      id: '0193a1b2-0000-7000-8000-000000000006',
      name: '@internal/build-tooling-and-release-automation-scripts',
      version: '2.0.0',
      origin: 'probe',
      state: 'failed',
      attempts: 5,
      finishedAt: at(-6),
      failure: failure({
        class: 'tls',
        step: 'tls',
        code: 'tls_untrusted',
        file: '@internal/build-tooling-and-release-automation-scripts/-/build-tooling-and-release-automation-scripts-2.0.0.tgz',
        address: 'https://registry.example.com:8443/@internal/build-tooling-and-release-automation-scripts/-/build-tooling-and-release-automation-scripts-2.0.0.tgz',
        detail: 'The TLS certificate of registry.example.com:8443 is not trusted by this installation; add its CA to outbound.tls.ca_file.',
        attempt: 5,
      }),
    }),
    row({ id: '0193a1b2-0000-7000-8000-000000000007', name: 'typescript', version: '5.9.3', origin: 'bulk', bulk: BULK, state: 'done', outcome: 'completed', attempts: 1, files: 1, stored: 1, bytes: 23_068_672, finishedAt: at(-8) }),
    row({ id: '0193a1b2-0000-7000-8000-000000000008', name: 'webpack', version: '5.98.0', origin: 'person', startedBy: { id: null, name: null }, state: 'done', outcome: 'bounded', attempts: 1, files: 1000, stored: 1000, bytes: 1_073_741_824, finishedAt: at(-9) }),
    row({ id: '0193a1b2-0000-7000-8000-000000000009', name: 'moment', version: '2.30.1', origin: 'allow', state: 'cancelled', finishedAt: at(-10) }),
  ];
}

function page(items: Record<string, unknown>[], more: boolean): Record<string, unknown> {
  const count = (s: string): number => items.filter((r) => r['state'] === s).length;
  // The repository's whole queue is larger than its first page: ten more waiting rows and the rows done before.
  return { items, nextCursor: more ? 'more' : null, counts: { waiting: count('waiting') + 10, running: count('running'), failed: count('failed'), done: count('done') + 228 } };
}

/** The bounds of a bulk without a range of versions. */
const noRange = { maxVersions: 100, maxBytes: 107374182400, includePrereleases: true, fromVersion: null, toVersion: null, toInclusive: true };

/** A bulk of a package asked for a range of versions (Q-CUR-193): its line says "From 5.0.0 up to 5.4.7". */
const bulk = {
  id: BULK,
  level: 'package',
  subject: '@acme/design-tokens',
  origin: 'person',
  state: 'queued',
  bounds: { ...noRange, fromVersion: '5.0.0', toVersion: '5.4.7' },
  versionsFound: 38,
  versionsQueued: 38,
  failure: null,
  startedBy: { id: null, name: null },
  createdAt: at(-15),
  finishedAt: null,
  rows: { running: 2, waiting: 10, done: 26 },
};

/** The bulk of the owner's report of 2026-11-24: its expansion failed before its first row, at a host the repository may not reach. */
const failedBulk = {
  ...bulk,
  id: BULK_FAILED,
  subject: '@customer-portal/release-candidates-for-the-frontend-design-system-components',
  // The versions below one: its line says "Below 3.0.0".
  bounds: { ...noRange, toVersion: '3.0.0', toInclusive: false },
  state: 'failed',
  versionsFound: 0,
  versionsQueued: 0,
  rows: {},
  createdAt: at(-5),
  finishedAt: at(-5),
  failure: failure({
    class: 'upstream_unexpected',
    step: 'status',
    code: 'foreign_host_denied',
    target: 'https://replicate.registry.example.com/@customer-portal/release-candidates-for-the-frontend-design-system-components',
    attempt: 0,
    at: at(-5),
    detail:
      'The download failed at the step status. The upstream document points to the host replicate.registry.example.com, which is neither the upstream of the repository web/npmjs nor one of its extraHosts.',
  }),
};

/** A sub-section's bulk that is still finding versions: nothing queued yet. */
const expandingBulk = {
  ...bulk,
  id: BULK_EXPANDING,
  level: 'section',
  subject: '@acme',
  bounds: noRange,
  state: 'expanding',
  versionsFound: 0,
  versionsQueued: 0,
  rows: {},
  createdAt: at(-1),
  // Another person started it: its head says "Started by Priya Raman".
  startedBy: { id: '0193a1b2-0000-7000-8000-0000000000aa', name: 'Priya Raman' },
};

const bulks = [expandingBulk, failedBulk, bulk];

/** The bulk of a queue an upstream's rate limit holds, as the owner's of 2026-11-25: 315 versions found, 105 done, 206 wait. */
const heldBulk = {
  ...bulk,
  id: BULK,
  subject: '@types/node',
  bounds: noRange,
  versionsFound: 315,
  versionsQueued: 315,
  rows: { waiting: 206, done: 105 },
  createdAt: at(-64),
};

/** The upstream's limit of that queue: a 429 without a time of its own, the third wait in a row; Quader asks again in 17 minutes. */
const heldLimit = {
  since: at(-61),
  until: at(17),
  holds: true,
  hinted: false,
  waits: 3,
  upstream: 'registry.npmjs.org',
  // npm's preset documents no higher limit with an account, so no account is offered (Q-UI-1160).
  accountRaisesLimit: false,
  failure: failure({
    class: 'upstream_rate_limited',
    step: 'status',
    code: 'upstream_rate_limited',
    address: 'https://registry.npmjs.org/@types%2fnode',
    status: 429,
    excerpt: '{"error":"Too Many Requests","reason":"You have exceeded the rate limit for anonymous requests; sign in or wait before you retry."}',
    headers: { 'RateLimit-Limit': '100;w=3600', 'RateLimit-Remaining': '0;w=3600', 'X-RateLimit-Reset': String(Math.floor(Date.now() / 1000) + 21_000) },
    detail: 'registry.npmjs.org is rate limiting Quader (status 429); Quader asks again after the wait, and the wait is no failed attempt of the file.',
    attempt: 1,
    at: at(-4),
  }),
};

/** The rows of the held queue: the newest waiting rows of the bulk, one that met the limit, and the done ones under them. */
function heldRows(): Record<string, unknown>[] {
  const waiting = ['24.10.1', '24.10.0', '24.9.2', '24.9.1', '24.9.0'].map((v, i) =>
    row({ id: `0193a1b2-0000-7000-8000-0000000001${i}0`, name: '@types/node', version: v, origin: 'bulk', bulk: BULK, createdAt: at(-64) }),
  );
  waiting[0] = { ...waiting[0], failure: heldLimit.failure };
  const done = ['22.19.0', '22.18.13'].map((v, i) =>
    row({ id: `0193a1b2-0000-7000-8000-0000000002${i}0`, name: '@types/node', version: v, origin: 'bulk', bulk: BULK, state: 'done', outcome: 'completed', attempts: 1, files: 1, stored: 1, bytes: 2_412_544, finishedAt: at(-62) }),
  );
  return [...waiting, ...done];
}

const listUrl = (url: URL): boolean => /^\/api\/v1\/projects\/[^/]+\/repositories\/[^/]+\/downloads$/.test(url.pathname);
const bulkUrl = (url: URL): boolean => /^\/api\/v1\/projects\/[^/]+\/repositories\/[^/]+\/download-bulks\/[^/]+$/.test(url.pathname);
const bulksUrl = (url: URL): boolean => /^\/api\/v1\/projects\/[^/]+\/repositories\/[^/]+\/download-bulks$/.test(url.pathname);
const rowsOfTab = 'main quader-remote-downloads [data-testid="download-rows"] tbody tr:not(.expansion)';

/**
 * The tab as it opens (the seed's own queue: in a fresh database the empty state), then with the bulks and the queue of a cache
 * that is being built, a failed row opened to its whole failure, a bulk's rows under its line, the bulk that failed before its first
 * row, and the rows of one package (`?name=`).
 */
export async function* downloadsWalk(p: Page): AsyncGenerator<string> {
  await expect(p.locator('main quader-remote-downloads [data-testid="download-rows"]')).toBeVisible();
  yield '';
  // The bulk whose rows run was started by the reader: its head says so in a sentence of its own, "Started by you", in German
  // "Gestartet von Ihnen" (Q-UI-1219). The reader is the session's principal.
  const who = await p.request.get('/api/v1/auth/whoami');
  expect(who.ok()).toBe(true);
  const me = ((await who.json()) as { principal: { id: string; name?: string } }).principal;
  const mine = { ...bulk, startedBy: { id: me.id, name: me.name ?? null } };
  const answered = (b: Record<string, unknown>) => (b['id'] === bulk.id ? mine : b);
  // The queue an upstream's limit holds is answered once the walk reaches it.
  let held = false;
  await p.route(listUrl, (route) => {
    if (route.request().method() !== 'GET') return route.fallback();
    const params = new URL(route.request().url()).searchParams;
    const bulkOnly = params.get('bulk');
    const nameOnly = params.get('name');
    if (held) {
      const counts = { waiting: 206, running: 0, failed: 0, done: 105 };
      return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ items: heldRows(), nextCursor: 'more', counts, limit: heldLimit }) });
    }
    const items = queueRows().filter((r) => (!bulkOnly || r['bulk'] === bulkOnly) && (!nameOnly || r['name'] === nameOnly));
    return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(page(items, !bulkOnly && !nameOnly)) });
  });
  await p.route(bulkUrl, (route) => {
    const id = new URL(route.request().url()).pathname.split('/').at(-1);
    const answer = held ? heldBulk : answered(bulks.find((b) => b.id === id) ?? bulk);
    return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(answer) });
  });
  await p.route(bulksUrl, (route) =>
    route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ items: held ? [heldBulk] : bulks.map(answered), nextCursor: null }) }),
  );
  await p.reload();
  await expect(p.locator(rowsOfTab)).toHaveCount(9);
  await expect(p.locator('main [data-testid="download-bulks"] tbody tr')).toHaveCount(3);
  yield 'queue';
  // The controls by their place, not their words: the German pass walks the tab too (UI_RENDERS_LOCALE=de).
  await p.locator('main quader-remote-downloads button[aria-controls="dl-0193a1b2-0000-7000-8000-000000000005"]').click();
  await expect(p.locator('main quader-remote-downloads .collapse-region.open pre.excerpt')).toBeVisible();
  yield 'failure';
  await p.locator('main quader-remote-downloads [data-testid="download-rows"] td a[href*="bulk="]').first().click();
  await expect(p.locator('main [data-testid="download-bulk"] h2')).toBeVisible();
  await expect(p.locator(rowsOfTab)).toHaveCount(3);
  yield 'bulk';
  // The bulk whose expansion failed before its first row, as the notice of its start leads to it: its cause and Start again.
  await p.goto(`${new URL(p.url()).pathname}?bulk=${BULK_FAILED}`);
  await expect(p.locator('main [data-testid="download-bulk"] .bulk-failure')).toBeVisible();
  yield 'bulk-failed';
  // The server's English sentence and its code, folded under the screen's sentence (Q-CUR-169).
  await p.locator('main [data-testid="download-bulk"] quader-technical-details button').click();
  await expect(p.locator('main [data-testid="download-bulk"] quader-technical-details .collapse-region.open')).toBeVisible();
  yield 'bulk-failed-technical';
  // The rows of one package, as the link Show in queue of the top bar's downloads panel opens the tab (R-UI-211).
  await p.goto(`${new URL(p.url()).pathname}?name=${encodeURIComponent('typescript')}`);
  await expect(p.locator('main [data-testid="download-name-filter"]')).toBeVisible();
  await expect(p.locator(rowsOfTab)).toHaveCount(1);
  yield 'name';
  // The queue an upstream's rate limit holds (R-CUR-011, Q-UI-1100): the sentence above the list, the waiting rows' and the bulk's line,
  // then the upstream's answer folded under the sentence, and the bulk's own line.
  held = true;
  const tab = new URL(p.url()).pathname;
  await p.goto(tab);
  await expect(p.locator('main [data-testid="download-limit"]')).toBeVisible();
  await expect(p.locator(rowsOfTab)).toHaveCount(7);
  yield 'limited';
  await p.locator('main [data-testid="download-limit"] quader-technical-details button').click();
  await expect(p.locator('main [data-testid="download-limit"] quader-technical-details .collapse-region.open')).toBeVisible();
  yield 'limited-technical';
  await p.goto(`${tab}?bulk=${BULK}`);
  await expect(p.locator('main [data-testid="download-bulk"] .limit-line')).toBeVisible();
  yield 'limited-bulk';
  await p.unroute(listUrl);
  await p.unroute(bulkUrl);
  await p.unroute(bulksUrl);
}
