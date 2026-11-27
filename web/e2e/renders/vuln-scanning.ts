import { Page, expect } from '@playwright/test';
import { trivyDatabases } from './scanners';

/**
 * **Settings › Vulnerability scanning** (R-VULN-053, Q-VULN-298) and the details of an image that waits or failed, which lead an
 * administrator there. The render database holds one state of the image scanning (no database in use, every image waiting, the scanner
 * rows of the seed that hold no database): the walks answer the others in the browser from the real answers, which keep who may read
 * what. Every control is found by its place or its test id, never by an English word, so the German pass walks them too.
 */

type Body = Record<string, unknown>;

const hoursAgo = (h: number) => new Date(Date.now() - h * 3_600_000).toISOString();
const DIGEST = 'sha256:7f3a9c51e2d84b6a0c1f5e9d2b7a4c8e6f0d3b5a9c7e1f2d4b6a8c0e2f4a6b8c';

/** The image scanning at work: many waiting for several reasons, two in a call, a long tail of failures, the last with its long text. */
function atWork(b: Body): Body {
  return {
    ...b,
    enabled: true,
    waiting: 77,
    scanning: 2,
    scanned: 12_480,
    outdated: 312,
    failed: 9,
    reasons: [
      { state: 'failed', code: 'image_too_large', count: 5 },
      { state: 'failed', code: 'timeout', count: 2 },
      { state: 'failed', code: 'layer_format', count: 1 },
      { state: 'failed', code: 'digest_mismatch', count: 1 },
      { state: 'waiting', code: '', count: 55 },
      { state: 'waiting', code: 'scanner_busy', count: 11 },
      { state: 'waiting', code: 'database_loading', count: 8 },
      { state: 'waiting', code: 'blobs_pending', count: 3 },
    ],
    oldestWaitingAt: hoursAgo(2.5),
    lastFailure: {
      ...(b['lastFailure'] as Body | null),
      repository: 'customer-portal-release-delivery/container-images-production',
      image: 'platform/observability/opentelemetry-collector-contrib-distribution',
      digest: 'sha256:5a0c0b8e6c1f4d2a9e7b3c6d8f0a1b2c3d4e5f60718293a4b5c6d7e8f9012345',
      code: 'image_too_large',
      detail:
        'the image is 12884901888 bytes, more than vulnerabilities.images.max_image_size (8589934592); raise the bound or leave the image unscanned',
      attempts: 1,
      at: hoursAgo(0.3),
    },
  };
}

/** Today's database, in use: built five hours ago, fetched four hours ago. */
function inUse(b: Body): Body {
  return { ...b, scanner: 'vuln-scanner-trivy', digest: DIGEST, builtAt: hoursAgo(5), syncedAt: hoursAgo(4), stale: false };
}

/** The database's row among the sources: in use, confirmed four hours ago, imported yesterday; or failing twice with its long text. */
function sources(failing: boolean): (b: Body) => Body {
  return (b) => ({
    ...b,
    items: ((b['items'] as Body[]) ?? []).map((s) =>
      s['source'] !== 'trivy'
        ? s
        : {
            ...s,
            state: failing ? 'empty' : 'complete',
            syncedAt: failing ? null : hoursAgo(4),
            fullSyncedAt: failing ? null : hoursAgo(26),
            attemptedAt: hoursAgo(0.2),
            failures: failing ? 2 : 0,
            lastError: failing
              ? {
                  type: 'about:blank', title: 'Bad Gateway', status: 502, code: 'upstream_failed',
                  detail: 'mirror.gcr.io/aquasec/trivy-db:2: the upstream registry did not answer (dial tcp 142.250.185.112:443: connect: connection refused)',
                }
              : null,
          },
    ),
  });
}

/** Changes the real answer of a URL from now on. */
async function change(page: Page, url: string, edit: (b: Body) => Body): Promise<void> {
  await page.unroute(url);
  await page.route(url, async (route) => {
    const res = await route.fetch();
    const body = res.ok() ? ((await res.json()) as Body) : {};
    await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(edit(body)) });
  });
}

/** Loads the page again with the answers changed, and waits until it stands. */
async function reload(page: Page): Promise<void> {
  await page.reload();
  await page.waitForLoadState('networkidle');
  await expect(page.locator('main [data-testid="vuln-scans"], main [data-testid="vuln-blocker"]').first()).toBeVisible();
}

const blocker = (page: Page) => page.locator('main [data-testid="vuln-blocker"]');

/**
 * The page as the render database has it (no database in use, every image waiting: the sentence at the top with **Import now**); then the
 * scanning at work with a database in use, every pod on it, many waiting for several reasons and a long last failure, the scanners' fold
 * opened; every ready pod still loading the database; no replica ready; the database's imports failing; the image scanning off.
 */
export async function* vulnScanningSettings(page: Page): AsyncGenerator<string> {
  await expect(page.locator('main h2#vuln-scanning')).toBeVisible();
  yield '';
  await change(page, '**/api/v1/vulnerabilities/image-scans', atWork);
  await change(page, '**/api/v1/vulnerabilities/database', inUse);
  await change(page, '**/api/v1/vulnerabilities/sources', sources(false));
  await change(page, '**/api/v1/system/scanners', trivyDatabases('current'));
  await reload(page);
  await expect(page.locator('main [data-testid="vuln-last-failure"]')).toBeVisible();
  await expect(blocker(page)).toHaveCount(0);
  yield 'at-work';
  const fold = page.locator('main quader-scanner-replicas quader-disclosure button.inline-toggle').first();
  await fold.click();
  await expect(fold).toHaveAttribute('aria-expanded', 'true');
  yield 'scanners';
  await change(page, '**/api/v1/system/scanners', (b) => {
    const mixed = trivyDatabases('mixed')(b);
    return { ...mixed, items: (mixed['items'] as Body[]).map((r) => (r['kind'] === 'trivy' ? { ...r, database: null } : r)) };
  });
  await reload(page);
  await expect(blocker(page)).toHaveAttribute('data-blocker', 'loading');
  yield 'loading';
  // The kind reports itself (as in a cluster; the render backend's role all names it as configured nowhere) and no pod of it has.
  await change(page, '**/api/v1/system/scanners', (b) => ({
    ...b,
    kinds: ((b['kinds'] as Body[]) ?? []).map((k) => (k['kind'] === 'trivy' ? { ...k, replicas: 'reporting' } : k)),
    items: ((b['items'] as Body[]) ?? []).filter((r) => r['kind'] !== 'trivy'),
  }));
  await reload(page);
  await expect(blocker(page)).toHaveAttribute('data-blocker', 'no_scanner');
  yield 'no-scanner';
  await page.unroute('**/api/v1/system/scanners');
  await page.unroute('**/api/v1/vulnerabilities/database');
  await change(page, '**/api/v1/vulnerabilities/sources', sources(true));
  await reload(page);
  await expect(page.locator('main [data-testid="vuln-db-error"]')).toBeVisible();
  yield 'import-failing';
  await change(page, '**/api/v1/vulnerabilities/image-scans', (b) => ({ ...b, enabled: false, waiting: 0, scanning: 0, scanned: 0, failed: 0, reasons: [], lastFailure: null }));
  await reload(page);
  await expect(blocker(page)).toHaveAttribute('data-blocker', 'off');
  yield 'off';
  for (const url of ['**/api/v1/vulnerabilities/image-scans', '**/api/v1/vulnerabilities/sources']) await page.unroute(url);
}

/** A version's report of an image whose grade is N/A in the state, with its code. */
async function routeState(page: Page, state: string, code: string | null): Promise<void> {
  const url = '**/api/v1/packages/*/versions/*/vulnerabilities**';
  await page.unroute(url);
  await page.route(url, async (route) => {
    const res = await route.fetch();
    const body = (await res.json()) as { repositories?: { grade: Body; evaluation: string }[] };
    for (const r of body.repositories ?? []) {
      r.grade = {
        ...r.grade, grade: 'N/A', open: { critical: 0, high: 0, medium: 0, low: 0, none: 0, unknown: 0 }, fixable: 0, checkedAt: null,
        state, stateSince: state === 'queued' ? hoursAgo(0.4) : null, stateCode: code,
      };
      r.evaluation = 'pending_data';
    }
    await route.fulfill({ response: res, body: JSON.stringify(body) });
  });
}

/**
 * An image's version page as an administrator: the grade's details of an image that waits because every scanner was busy, of one without a
 * database in use, and of one over the scan's limits; each says why and leads to **Settings › Vulnerability scanning**.
 */
export async function* gradeDetailsWaiting(page: Page): AsyncGenerator<string> {
  const details = page.locator('.cdk-overlay-container [data-testid="grade-details"]');
  for (const [state, code, name] of [
    ['queued', 'scanner_busy', 'busy'],
    ['no_database', null, 'no-database'],
    ['over_limits', 'image_too_large', 'over-limits'],
  ] as const) {
    await routeState(page, state, code);
    await page.reload();
    const grade = page.locator('main .facts-grade button.grade');
    await expect(grade).toHaveAttribute('data-state', state);
    await page.waitForLoadState('networkidle');
    await grade.focus();
    await expect(details.locator('[data-testid="grade-scanning-link"]')).toBeVisible();
    yield name;
    await page.keyboard.press('Escape');
    await expect(details).toBeHidden();
  }
  await page.unroute('**/api/v1/packages/*/versions/*/vulnerabilities**');
}
