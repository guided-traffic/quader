import { Page, Route, expect } from '@playwright/test';

/**
 * The top bar's button **Downloads** and its panel (api-and-ui.md R-UI-210 to R-UI-212), answered in the browser: the render profile
 * fetches nothing from the internet, so the downloads from upstreams that the panel lists are the walk's. The walk finds every control
 * by its place, so the German pass walks it too. Nothing is changed on the server.
 */

type Json = Record<string, unknown>;

/** The time `seconds` before now, RFC 3339: the rows' times stay relative to the run. */
const ago = (seconds: number): string => new Date(Date.now() - seconds * 1000).toISOString();

const MiB = 1_048_576;
const DIGEST = 'sha256:7a1b2c3d4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f60718293a4b5c6d7e8f9';
/** A scoped npm name of 120 characters: the longest a row must wrap inside itself. */
const LONG_NAME = '@acme-platform-engineering/shared-frontend-infrastructure-design-system-tokens-and-theming-utilities-for-web-apps-nextv2';

function running(over: Json): Json {
  return {
    id: '',
    repository: { project: 'web-frontend', key: 'npmjs', format: 'npm' },
    name: '',
    version: '',
    path: '',
    bytes: 0,
    total: null,
    bytesPerSecond: null,
    startedAt: ago(10),
    elapsedSeconds: 10,
    origin: 'client',
    queueOrigin: null,
    queueRow: null,
    by: null,
    replica: 'quader-packages-7d9f6c5b8-x2v4q',
    ...over,
  };
}

function finished(over: Json): Json {
  return {
    id: '',
    repository: { project: 'web-frontend', key: 'npmjs', format: 'npm' },
    name: '',
    version: '',
    path: '',
    bytes: 0,
    total: null,
    bytesPerSecond: null,
    startedAt: ago(120),
    finishedAt: ago(60),
    durationSeconds: 1,
    outcome: 'completed',
    failure: null,
    origin: 'client',
    queueOrigin: null,
    queueRow: null,
    by: null,
    replica: 'quader-packages-7d9f6c5b8-x2v4q',
    ...over,
  };
}

/**
 * Five running downloads: an npm tarball with its total, halfway; a Debian package of a mirror's pass without a total; a version-less
 * OCI blob a person queued; a 120-character scoped npm name of a bulk; the vulnerability database in its first second. 140 more run
 * than the replicas listed.
 */
function fiveRunning(): Json[] {
  return [
    running({
      id: '0193d1a0-0000-7000-8000-000000000001',
      name: 'typescript',
      version: '5.9.3',
      path: 'typescript/-/typescript-5.9.3.tgz',
      bytes: 11_534_336,
      total: 23_068_672,
      bytesPerSecond: 12_897_484,
      elapsedSeconds: 42.3,
      by: { id: '0192f0c4-7a7e-7b51-9b1e-2f0d6c9e1a11', name: 'sam' },
    }),
    running({
      id: '0193d1a0-0000-7000-8000-000000000002',
      repository: { project: 'platform', key: 'debian', format: 'deb' },
      name: 'linux-image-6.12.9-amd64',
      version: '6.12.9-1',
      path: 'pool/main/l/linux-signed-amd64/linux-image-6.12.9-amd64_6.12.9-1_amd64.deb',
      bytes: 41_943_040,
      bytesPerSecond: 3_250_585,
      elapsedSeconds: 13,
      origin: 'mirror',
      replica: 'quader-worker-6f9c7d4b5-2xk4p',
    }),
    running({
      id: '0193d1a0-0000-7000-8000-000000000003',
      repository: { project: 'upstream', key: 'docker-hub', format: 'oci' },
      name: 'library/ubuntu',
      path: `v2/library/ubuntu/blobs/${DIGEST}`,
      bytes: 22_020_096,
      total: 30_408_704,
      bytesPerSecond: 5_452_595,
      elapsedSeconds: 4.1,
      origin: 'queue',
      queueOrigin: 'person',
      queueRow: '0193d19f-0000-7000-8000-000000000001',
      by: { id: '0192f0c4-7a7e-7b51-9b1e-2f0d6c9e1a10', name: 'priya' },
      replica: 'quader-worker-6f9c7d4b5-2xk4p',
    }),
    running({
      id: '0193d1a0-0000-7000-8000-000000000004',
      name: LONG_NAME,
      version: '14.2.0-rc.3',
      path: `${LONG_NAME}/-/${LONG_NAME.split('/')[1]}-14.2.0-rc.3.tgz`,
      bytes: 734_003,
      total: 2_936_012,
      bytesPerSecond: 489_335,
      elapsedSeconds: 1.5,
      origin: 'queue',
      queueOrigin: 'bulk',
      queueRow: '0193d19f-0000-7000-8000-000000000002',
      replica: 'quader-worker-6f9c7d4b5-2xk4p',
    }),
    running({
      id: '0193d1a0-0000-7000-8000-000000000005',
      repository: { project: 'upstream', key: 'scanner-trivy', format: 'oci' },
      name: 'aquasecurity/trivy-db',
      version: '2',
      path: `v2/aquasecurity/trivy-db/blobs/${DIGEST.replace('7a1b', '9c8d')}`,
      bytes: 262_144,
      total: 83_886_080,
      bytesPerSecond: null,
      elapsedSeconds: 0.6,
      origin: 'scanner_database',
      replica: 'quader-worker-6f9c7d4b5-2xk4p',
    }),
  ];
}

const NAMES: readonly (readonly [format: string, project: string, key: string, name: string, version: string, file: string])[] = [
  ['npm', 'web-frontend', 'npmjs', 'react', '19.2.0', 'react/-/react-19.2.0.tgz'],
  ['npm', 'web-frontend', 'npmjs', '@angular/core', '22.0.1', '@angular/core/-/core-22.0.1.tgz'],
  ['nuget', 'dotnet', 'nuget-org', 'Newtonsoft.Json', '13.0.3', 'v3-flatcontainer/newtonsoft.json/13.0.3/newtonsoft.json.13.0.3.nupkg'],
  ['oci', 'upstream', 'docker-hub', 'library/postgres', '18.1', `v2/library/postgres/manifests/18.1`],
  ['deb', 'platform', 'debian', 'openssl', '3.5.4-1', 'pool/main/o/openssl/openssl_3.5.4-1_amd64.deb'],
  ['ansible', 'automation', 'galaxy', 'community.general', '11.4.0', 'download/community-general-11.4.0.tar.gz'],
];

/**
 * The last 100 finished downloads: each outcome (completed, failed with the class network and with integrity, cancelled,
 * interrupted), durations of 72 s, 0.4 s and 1 h 5 min, then completed downloads of every format, older and older.
 */
function hundredFinished(): Json[] {
  const first: Json[] = [
    finished({
      id: '0193d1b0-0000-7000-8000-000000000001',
      name: 'lodash',
      version: '4.17.21',
      path: 'lodash/-/lodash-4.17.21.tgz',
      bytes: 300_847_104,
      total: 300_847_104,
      durationSeconds: 72,
      bytesPerSecond: 4_178_432,
      finishedAt: ago(30),
      by: { id: '0192f0c4-7a7e-7b51-9b1e-2f0d6c9e1a11', name: 'sam' },
    }),
    finished({
      id: '0193d1b0-0000-7000-8000-000000000002',
      repository: { project: 'upstream', key: 'docker-hub', format: 'oci' },
      name: 'library/nginx',
      path: `v2/library/nginx/blobs/${DIGEST.replace('7a1b', '3e4f')}`,
      bytes: 9_437_184,
      total: 31_457_280,
      durationSeconds: 18.2,
      bytesPerSecond: 518_532,
      finishedAt: ago(95),
      outcome: 'failed',
      failure: { class: 'network', code: 'connection_reset' },
      origin: 'queue',
      queueOrigin: 'person',
      queueRow: '0193d19f-0000-7000-8000-000000000003',
      by: { id: '0192f0c4-7a7e-7b51-9b1e-2f0d6c9e1a10', name: 'priya' },
    }),
    finished({
      id: '0193d1b0-0000-7000-8000-000000000003',
      repository: { project: 'dotnet', key: 'nuget-org', format: 'nuget' },
      name: 'Microsoft.Extensions.DependencyInjection',
      version: '10.0.0',
      path: 'v3-flatcontainer/microsoft.extensions.dependencyinjection/10.0.0/microsoft.extensions.dependencyinjection.10.0.0.nupkg',
      bytes: 104_857,
      total: 104_857,
      durationSeconds: 0.4,
      bytesPerSecond: 262_142,
      finishedAt: ago(140),
      outcome: 'failed',
      failure: { class: 'integrity', code: 'digest_mismatch' },
    }),
    finished({
      id: '0193d1b0-0000-7000-8000-000000000004',
      name: 'esbuild',
      version: '0.27.0',
      path: 'esbuild/-/esbuild-0.27.0.tgz',
      bytes: 1_048_576,
      total: 4_194_304,
      durationSeconds: 2.3,
      bytesPerSecond: 455_902,
      finishedAt: ago(200),
      outcome: 'cancelled',
      origin: 'queue',
      queueOrigin: 'probe',
      queueRow: '0193d19f-0000-7000-8000-000000000004',
    }),
    finished({
      id: '0193d1b0-0000-7000-8000-000000000005',
      repository: { project: 'platform', key: 'debian', format: 'deb' },
      name: 'firmware-nonfree',
      version: '20250808-1',
      path: 'pool/non-free-firmware/f/firmware-nonfree/firmware-misc-nonfree_20250808-1_all.deb',
      bytes: 1_717_986_918,
      total: null,
      durationSeconds: 3900,
      bytesPerSecond: 440_509,
      finishedAt: ago(320),
      outcome: 'interrupted',
      origin: 'mirror',
    }),
  ];
  const rest = Array.from({ length: 95 }, (_, i): Json => {
    const [format, project, key, name, version, path] = NAMES[i % NAMES.length]!;
    const bytes = 40_960 + ((i * 7_919_113) % 48_000_000);
    const duration = 0.6 + (i % 9) * 3.7;
    return finished({
      id: `0193d1b0-0000-7000-8000-${String(100 + i).padStart(12, '0')}`,
      repository: { project, key, format },
      name,
      version,
      path,
      bytes,
      total: bytes,
      durationSeconds: duration,
      bytesPerSecond: Math.round(bytes / duration),
      finishedAt: ago(400 + i * 97),
      origin: i % 4 === 0 ? 'mirror' : 'client',
      by: i % 4 === 0 ? null : { id: '0192f0c4-7a7e-7b51-9b1e-2f0d6c9e1a11', name: 'sam' },
    });
  });
  return [...first, ...rest];
}

/**
 * The download queues an upstream's rate limit holds (remote-curation.md R-CUR-011, Q-UI-1101): the owner's Docker Hub queue of
 * 2026-11-25 with 206 rows and no time of the upstream's own, and one row of a long project's npm remote whose upstream named its time.
 */
function heldQueues(): Json[] {
  return [
    {
      repository: { project: 'upstream', key: 'docker-hub', format: 'oci' },
      waiting: 206,
      upstream: 'Docker Hub',
      since: ago(3600),
      until: new Date(Date.now() + 17 * 60_000).toISOString(),
      hinted: false,
      status: 429,
    },
    {
      repository: { project: 'customer-portal-release-delivery', key: 'npm-registry-of-the-release-candidates', format: 'npm' },
      waiting: 1,
      upstream: 'registry.npmjs.org',
      since: ago(300),
      until: new Date(Date.now() + 2 * 3600_000).toISOString(),
      hinted: true,
      status: 429,
    },
  ];
}

/** What the two endpoints answer now; the walk changes it between its states. */
interface Answers {
  running: Json[];
  runningCount: number;
  finished: Json[];
  listStatus: number;
  waiting?: Json[];
}

const listUrl = '**/api/v1/system/upstream-downloads';
const summaryUrl = '**/api/v1/system/upstream-downloads/summary';
const HIDDEN_KEY = 'quader.upstream-downloads.hidden-before';

async function answer(page: Page, a: Answers): Promise<void> {
  await page.unroute(listUrl);
  await page.unroute(summaryUrl);
  await page.route(listUrl, (route: Route) =>
    a.listStatus === 200
      ? route.fulfill({
          status: 200,
          contentType: 'application/json',
          headers: { 'Cache-Control': 'no-store' },
          body: JSON.stringify({
            asOf: new Date().toISOString(),
            pollSeconds: 2,
            runningCount: a.runningCount,
            running: a.running,
            finished: a.finished,
            waiting: a.waiting ?? [],
          }),
        })
      : route.fulfill({ status: a.listStatus, contentType: 'application/problem+json', body: JSON.stringify({ type: 'about:blank', title: 'Service Unavailable', status: a.listStatus }) }),
  );
  await page.route(summaryUrl, (route: Route) =>
    route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ running: a.runningCount, pollSeconds: 2 }) }),
  );
}

const button = (page: Page) => page.locator('header.topbar button.downloads-button');
const drawer = (page: Page) => page.locator('.quader-drawer');

/** Loads the page again with the answers in place, so the button shows the count. */
async function reload(page: Page): Promise<void> {
  const counted = page.waitForResponse((r) => r.url().endsWith('/api/v1/system/upstream-downloads/summary'));
  await page.reload();
  await counted;
}

async function openPanel(page: Page): Promise<void> {
  await button(page).click();
  await expect(drawer(page).locator('[data-testid="upstream-downloads-running"], [data-testid="upstream-downloads-finished"]').first()).toBeVisible();
}

async function closePanel(page: Page): Promise<void> {
  await drawer(page).locator('.drawer-close').click();
  await expect(drawer(page)).toHaveCount(0);
}

/**
 * The top bar's button with nothing running, with 3 and with 140 (`99+`): the main area and the admin area show the same bar, so the
 * screen's path decides which.
 */
export async function* downloadsButtonWalk(page: Page): AsyncGenerator<string> {
  await expect(button(page)).toBeVisible();
  const a: Answers = { running: [], runningCount: 0, finished: [], listStatus: 200 };
  await answer(page, a);
  await reload(page);
  await expect(button(page).locator('.downloads-count')).toHaveCount(0);
  yield 'none';
  a.running = fiveRunning().slice(0, 3);
  a.runningCount = 3;
  await reload(page);
  await expect(button(page).locator('.downloads-count')).toHaveText('3');
  yield 'three';
  a.running = fiveRunning();
  a.runningCount = 140;
  await reload(page);
  await expect(button(page).locator('.downloads-count')).toHaveText('99+');
  yield 'many';
  await page.unroute(listUrl);
  await page.unroute(summaryUrl);
}

/** Grows the window to the drawer's whole content, so one picture holds every row; the next width sets the window again. */
async function wholeDrawer(page: Page): Promise<void> {
  const body = drawer(page).locator('.dl-body');
  const hidden = await body.evaluate((el) => el.scrollHeight - el.clientHeight);
  const view = page.viewportSize();
  if (view && hidden > 0) await page.setViewportSize({ width: view.width, height: Math.min(view.height + hidden, 16_000) });
}

/**
 * The panel: five running downloads and 140 more with the last 100 finished, as it opens and then whole; the filter set; everything
 * finished hidden on this device; a refresh that failed over the last rows; and both sections empty.
 */
export async function* downloadsPanelWalk(page: Page): AsyncGenerator<string> {
  await expect(button(page)).toBeVisible();
  await page.evaluate((key) => localStorage.removeItem(key), HIDDEN_KEY);
  const a: Answers = { running: fiveRunning(), runningCount: 145, finished: hundredFinished(), listStatus: 200 };
  await answer(page, a);
  await reload(page);
  await openPanel(page);
  await expect(drawer(page).locator('[data-testid="upstream-downloads-finished"] li.dl-row')).toHaveCount(100);
  yield 'panel';
  const first = page.viewportSize();
  await wholeDrawer(page);
  yield 'panel-whole';
  // The states after it as a window of the width's first height shows them.
  if (first) await page.setViewportSize(first);
  const field = drawer(page).locator('#upstream-downloads-filter');
  await field.fill('nginx');
  await expect(drawer(page).locator('[data-testid="upstream-downloads-finished"] li.dl-row')).toHaveCount(1);
  await field.fill('ubuntu');
  await expect(drawer(page).locator('[data-testid="upstream-downloads-running"] li.dl-row')).toHaveCount(1);
  yield 'filtered';
  await field.fill('');
  await drawer(page).locator('[data-testid="upstream-downloads-finished"] .dl-section-head button').click();
  await expect(drawer(page).locator('.dl-hidden')).toBeVisible();
  // The section Finished in view: the running rows stand above it.
  await drawer(page).locator('[data-testid="upstream-downloads-finished"]').evaluate((el) => el.scrollIntoView({ block: 'start' }));
  yield 'hidden';
  await drawer(page).locator('.dl-hidden button').click();
  await expect(drawer(page).locator('.dl-hidden')).toHaveCount(0);
  await drawer(page).locator('.dl-body').evaluate((el) => el.scrollTo({ top: 0 }));
  a.listStatus = 503;
  await expect(drawer(page).locator('[data-testid="upstream-downloads-refresh-failed"]')).toBeVisible({ timeout: 10_000 });
  yield 'refresh-failed';
  a.listStatus = 200;
  a.running = [];
  a.runningCount = 0;
  a.finished = [];
  await closePanel(page);
  await openPanel(page);
  await expect(drawer(page).locator('.dl-empty')).toHaveCount(2);
  yield 'empty';
  // Nothing runs while two queues wait for their upstream's rate limit (R-CUR-011, Q-UI-1101): the panel says so above Running.
  a.waiting = heldQueues();
  a.finished = hundredFinished().slice(0, 3);
  await closePanel(page);
  await openPanel(page);
  await expect(drawer(page).locator('[data-testid="upstream-downloads-waiting"] li.dl-row')).toHaveCount(2);
  yield 'limited';
  await closePanel(page);
  await page.unroute(listUrl);
  await page.unroute(summaryUrl);
}

/** The refresh of the open panel that the walk fails on purpose. */
export const downloadsExpected = [
  { match: /^GET \/api\/v1\/system\/upstream-downloads answered 503$/, why: 'the walk fails a refresh of the open panel to render its error line' },
];
