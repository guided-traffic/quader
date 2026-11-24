import { Page, expect } from '@playwright/test';

/**
 * The tab Downloads of a remote repository (remote-curation.md R-CUR-018) with a queue of every state. The seed's server has a queue
 * only where a probe or a client fetched (no upstream of the internet is reachable for certain), so the walk answers the list in the
 * browser with the rows a cache that is being built shows: a running and a waiting row, a bulk's rows, a client's failure repeated
 * fourteen times, a failure with a long sentence and the 512 bytes of an upstream's error page, a row stopped at a bound and a
 * cancelled one. Nothing is changed on the server.
 */

const BULK = '0193a1b2-0000-7000-8000-0000000000b1';

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
    row({ id: '0193a1b2-0000-7000-8000-000000000001', name: '@acme/design-tokens', version: '4.2.0', origin: 'bulk', bulk: BULK, state: 'running', attempts: 1, files: 1, stored: 0, startedAt: at(-1) }),
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

const bulk = {
  id: BULK,
  level: 'package',
  subject: '@acme/design-tokens',
  state: 'queued',
  bounds: { maxVersions: 100, maxBytes: 107374182400, includePrereleases: true },
  versionsFound: 38,
  versionsQueued: 38,
  failure: null,
  startedBy: { id: null, name: null },
  createdAt: at(-15),
  finishedAt: null,
  rows: { running: 2, waiting: 10, done: 26 },
};

const listUrl = (url: URL): boolean => /^\/api\/v1\/projects\/[^/]+\/repositories\/[^/]+\/downloads$/.test(url.pathname);
const bulkUrl = (url: URL): boolean => /^\/api\/v1\/projects\/[^/]+\/repositories\/[^/]+\/download-bulks\/[^/]+$/.test(url.pathname);

/**
 * The tab as it opens (the seed's own queue: in a fresh database the empty state), then with the queue of a cache that is being
 * built, a failed row opened to its whole failure, and a bulk's rows under its line.
 */
export async function* downloadsWalk(p: Page): AsyncGenerator<string> {
  await expect(p.locator('main quader-remote-downloads quader-data-table')).toBeVisible();
  yield '';
  await p.route(listUrl, (route) => {
    if (route.request().method() !== 'GET') return route.fallback();
    const bulkOnly = new URL(route.request().url()).searchParams.get('bulk');
    const items = queueRows().filter((r) => !bulkOnly || r['bulk'] === bulkOnly);
    return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(page(items, !bulkOnly)) });
  });
  await p.route(bulkUrl, (route) => route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(bulk) }));
  await p.reload();
  await expect(p.locator('main quader-remote-downloads tbody tr:not(.expansion)')).toHaveCount(9);
  yield 'queue';
  // The controls by their place, not their words: the German pass walks the tab too (UI_RENDERS_LOCALE=de).
  await p.locator('main quader-remote-downloads button[aria-controls="dl-0193a1b2-0000-7000-8000-000000000005"]').click();
  await expect(p.locator('main quader-remote-downloads .collapse-region.open pre.excerpt')).toBeVisible();
  yield 'failure';
  await p.locator('main quader-remote-downloads td a[href*="bulk="]').first().click();
  await expect(p.locator('main [data-testid="download-bulk"] h2')).toBeVisible();
  await expect(p.locator('main quader-remote-downloads tbody tr:not(.expansion)')).toHaveCount(3);
  yield 'bulk';
  await p.unroute(listUrl);
  await p.unroute(bulkUrl);
}
