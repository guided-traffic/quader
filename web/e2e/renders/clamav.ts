import { Page, expect } from '@playwright/test';

/*
 * The ClamAV definitions in the renders (R-UI-187, R-UI-188, R-UI-194, R-CLAMAV-006): the profile runs no updater and no malware
 * scanning, and only the database task writes `upstream/clamav` (R-REPO-075), so neither the seed nor the API can fill it. As the file
 * page of a system repository does (`systemFileView` of screens.ts), the walks answer the definitions' calls in the browser: the
 * three databases with the metadata the format's view adapter sends (`internal/formats/clamav/view.go` `Metadata`), and the switch's
 * states changed from the server's own item, so that whether the caller may read it stays the server's. Every build time counts
 * from the render's clock, so the ages and the badge **Outdated** read the same on every run.
 */

const HOUR = 3_600_000;
const DAY = 24 * HOUR;
const iso = (msAgo: number): string => new Date(Date.now() - msAgo).toISOString().replace(/\.\d{3}Z$/, 'Z');

/** One version of a database: its age, its state, its signatures, and whether its files are stored. */
interface Version {
  readonly version: string;
  readonly builtAgo: number;
  readonly state: 'in_use' | 'previous' | 'superseded' | 'files_removed';
  readonly signatures: number;
  readonly level: number;
  readonly levelText: string;
  readonly size: number;
  readonly members: readonly (readonly [string, number, number])[];
}

/**
 * The fixture: `daily` in use since 5 hours with the one before it and a history row of 3 days, `main` built 9 days ago with a history
 * row, `bytecode` built 8 days ago with a superseded version; neither of the two is outdated by its age (Q-UI-912).
 */
const DATABASES: Readonly<Record<string, readonly Version[]>> = {
  bytecode: [
    { version: '339', builtAgo: 8 * DAY + 2 * HOUR, state: 'in_use', signatures: 80, level: 90, levelText: 'ClamAV 0.103 or later', size: 291_965, members: [['bytecode.info', 4_200, 0], ['1001.cbc', 4_806, 1]] },
    { version: '338', builtAgo: 19 * DAY, state: 'superseded', signatures: 79, level: 90, levelText: 'ClamAV 0.103 or later', size: 289_440, members: [['bytecode.info', 4_150, 0], ['1001.cbc', 4_806, 1]] },
  ],
  daily: [
    {
      version: '28190',
      builtAgo: 5 * HOUR,
      state: 'in_use',
      signatures: 2_076_903,
      level: 210,
      levelText: 'ClamAV 1.5 or later',
      size: 63_018_446,
      members: [
        ['daily.cdb', 3_520, 31],
        ['daily.hdb', 2_811_904, 41_286],
        ['daily.hsb', 101_284_109, 1_204_331],
        ['daily.info', 1_066, 0],
        ['daily.ldb', 16_910_022, 59_802],
        ['daily.ndb', 8_712_330, 70_115],
      ],
    },
    { version: '28189', builtAgo: 29 * HOUR, state: 'previous', signatures: 2_076_512, level: 210, levelText: 'ClamAV 1.5 or later', size: 62_998_101, members: [['daily.hdb', 2_810_002, 41_270], ['daily.info', 1_066, 0]] },
    { version: '28188', builtAgo: 3 * DAY, state: 'files_removed', signatures: 2_075_998, level: 210, levelText: 'ClamAV 1.5 or later', size: 62_950_771, members: [] },
  ],
  main: [
    { version: '63', builtAgo: 9 * DAY, state: 'in_use', signatures: 3_287_027, level: 90, levelText: 'ClamAV 0.103 or later', size: 170_479_789, members: [['main.hdb', 31_211_880, 498_313], ['main.info', 902, 0], ['main.ndb', 9_122_014, 101_230]] },
    { version: '62', builtAgo: 160 * DAY, state: 'files_removed', signatures: 3_101_442, level: 90, levelText: 'ClamAV 0.103 or later', size: 162_576_485, members: [] },
  ],
};

const digest = (seed: string): string => {
  let h = 0;
  for (const c of seed) h = (h * 31 + c.charCodeAt(0)) >>> 0;
  return Array.from({ length: 8 }, (_, i) => ((h + i * 2654435761) >>> 0).toString(16).padStart(8, '0')).join('');
};
const md5 = (seed: string): string => digest(seed).slice(0, 32);
/** The header's field 2 as ClamAV writes it: `21 Nov 2026 07-00 +0000`. */
function buildText(msAgo: number): string {
  const d = new Date(Date.now() - msAgo);
  const p = (n: number): string => String(n).padStart(2, '0');
  const month = d.toLocaleString('en-US', { month: 'short', timeZone: 'UTC' });
  return `${p(d.getUTCDate())} ${month} ${d.getUTCFullYear()} ${p(d.getUTCHours())}-${p(d.getUTCMinutes())} +0000`;
}
const idOf = (db: string): string => `pkg_clamav${db}`;
const REPO = 'upstream/clamav';
const CONTENT = '/api/v1/projects/upstream/repositories/clamav/content';

function metadata(db: string, v: Version): Record<string, unknown> {
  const removed = v.state === 'files_removed';
  return {
    database: db,
    version: Number(v.version),
    state: v.state,
    storedAt: iso(v.builtAgo - 40 * 60_000),
    file: `${db}.cvd`,
    ...(removed ? { filesRemovedAt: iso(v.builtAgo - DAY) } : {}),
    builtAt: iso(v.builtAgo),
    buildTimeText: buildText(v.builtAgo),
    signatures: v.signatures,
    functionalityLevel: v.level,
    functionalityLevelText: v.levelText,
    builder: 'raynman',
    verification: { outcome: 'signature_verified', headerMd5: md5(`${db}${v.version}`), computedMd5: md5(`${db}${v.version}`), source: '' },
    membersRead: !removed,
    members: v.members.map(([name, size, lines]) => ({ name, size, sha256: digest(`${db}${v.version}${name}`), lines })),
    detachedSignature: removed
      ? { present: false }
      : { present: true, name: `${db}-${v.version}.cvd.sign`, size: 1_284, signatures: 2 },
  };
}

/**
 * The age of the `daily` in use the answers give, where a walk sets one (the state of the databases' list with a `daily` past 7 days,
 * Q-UI-912); null for the fixture's 5 hours. A walk sets it and takes it back in the same page.
 */
let dailyInUseAgo: number | null = null;

/** The versions of a database as the answers give them. */
function versionsOf(db: string): readonly Version[] | undefined {
  const vs = DATABASES[db];
  if (!vs || db !== 'daily' || dailyInUseAgo === null) return vs;
  const ago = dailyInUseAgo;
  return vs.map((v) => (v.state === 'in_use' ? { ...v, builtAgo: ago } : v));
}

function summary(db: string): Record<string, unknown> {
  const vs = versionsOf(db)!;
  const inUse = vs.find((v) => v.state === 'in_use')!;
  return {
    id: idOf(db),
    format: 'clamav',
    name: db,
    description: `ClamAV ${db} signatures, built ${iso(inUse.builtAgo).slice(0, 16).replace('T', ' ')} UTC`,
    latestVersion: vs[0]!.version,
    keywords: [],
    repositories: [REPO],
    createdAt: iso(vs.at(-1)!.builtAgo),
    updatedAt: iso(inUse.builtAgo - 40 * 60_000),
    versionCount: vs.length,
    downloadCount: db === 'daily' ? 412 : 37,
  };
}

function versionSummary(v: Version): Record<string, unknown> {
  return {
    version: v.version,
    prerelease: false,
    publishedAt: iso(v.builtAgo - 40 * 60_000),
    sizeBytes: v.state === 'files_removed' ? 0 : v.size,
    sizePartial: false,
    repositories: [REPO],
    downloadCount: v.state === 'in_use' ? 312 : 4,
    lastDownloadedAt: v.state === 'in_use' ? iso(20 * 60_000) : null,
    state: v.state,
  };
}

function versionDetail(db: string, v: Version): Record<string, unknown> {
  const files =
    v.state === 'files_removed'
      ? []
      : [
          { path: `${db}/${v.version}/${db}.cvd`, sizeBytes: v.size },
          { path: `${db}/${v.version}/${db}-${v.version}.cvd.sign`, sizeBytes: 1_284 },
        ].map((f) => ({
          ...f,
          sha256: digest(f.path),
          downloadUrl: `https://localhost:4200/files/${REPO}/${f.path}`,
          contentUrl: `${CONTENT}/${f.path}`,
        }));
  return {
    ...versionSummary(v),
    packageId: idOf(db),
    format: 'clamav',
    name: db,
    licenses: [],
    hasReadme: false,
    metadata: metadata(db, v),
    repositories: [{ key: REPO, class: 'local', publishedAt: iso(v.builtAgo - 40 * 60_000) }],
    files,
  };
}

const json = (body: unknown) => ({ status: 200, contentType: 'application/json', body: JSON.stringify(body) });

/**
 * Answers the package calls of the definitions in the browser: the list of `upstream/clamav`'s packages, a database's lookup, its
 * versions (with their state) and a version's details. The vulnerability reports answer 404, which the screens take as "no grade"
 * (Q-UI-377).
 */
export async function routeDefinitions(page: Page): Promise<void> {
  await page.route('**/api/v1/packages?**', async (route) => {
    const url = new URL(route.request().url());
    if (!url.searchParams.getAll('format').includes('clamav')) return route.fallback();
    return route.fulfill(json({ items: ['bytecode', 'daily', 'main'].map(summary), nextCursor: null }));
  });
  await page.route('**/api/v1/packages/lookup?**', async (route) => {
    const url = new URL(route.request().url());
    const name = url.searchParams.get('name') ?? '';
    if (url.searchParams.get('format') !== 'clamav' || !DATABASES[name]) return route.fallback();
    return route.fulfill(json(summary(name)));
  });
  await page.route('**/api/v1/packages/pkg_clamav*/**', async (route) => {
    const path = new URL(route.request().url()).pathname;
    const m = /\/packages\/pkg_clamav(\w+)\/versions(?:\/([^/]+))?(\/vulnerabilities)?$/.exec(path);
    const db = m?.[1] ?? '';
    const vs = versionsOf(db);
    if (!m || !vs || m[3] || path.endsWith('/vulnerabilities')) return route.fulfill({ status: 404, contentType: 'application/problem+json', body: JSON.stringify({ code: 'not_found', detail: 'No report.', requestId: 'render' }) });
    if (!m[2]) return route.fulfill(json({ items: vs.map(versionSummary), nextCursor: null }));
    const v = vs.find((x) => x.version === decodeURIComponent(m[2]!));
    return v ? route.fulfill(json(versionDetail(db, v))) : route.fallback();
  });
}

/**
 * The answers of a definitions' screen that are its normal state: the first load reads the database before the walk routes it, and
 * the walks answer the versions' reports 404, which the screens take as "no grade".
 */
export const definitionsExpected = [
  {
    match: /^GET \/api\/v1\/packages\/lookup\?format=clamav&name=\w+ answered 404$/,
    why: 'the profile stores no definitions: the first load reads the database before the walk routes its answer',
  },
  {
    match: /^GET \/api\/v1\/packages\/pkg_clamav\w+\/(versions\/[^/]+\/)?vulnerabilities\S* answered 404$/,
    why: 'the definitions are not matched against advisories: the walk answers their reports 404, which the page takes as no grade',
  },
] as const;

/** The switch's states the walks render: off as a fresh installation has it, on at Public, on at Internal where Public is not allowed. */
export type SwitchState = 'off' | 'on' | 'lowered';

function patched(item: Record<string, unknown>, state: SwitchState): Record<string, unknown> {
  if (item['key'] !== 'clamav') return item;
  const clients = { ...(item['clients'] as Record<string, unknown>), enabled: state !== 'off' };
  return {
    ...item,
    clients,
    visibility: state === 'off' ? 'private' : state === 'on' ? 'public' : 'internal',
    fixed: state === 'off' ? ['state', 'visibility', 'address', 'account'] : ['state', 'address', 'account'],
  };
}

/**
 * The switch of `clamav` in a state, answered in the browser from the server's own list and item; `lowered` also answers the visibility
 * choices without Public, as an installation whose default endpoint admits no request without a credential (R-REPO-075). The page is
 * loaded again, because it reads both once.
 */
export async function switchState(page: Page, state: SwitchState): Promise<void> {
  await page.unroute('**/api/v1/upstream-repositories');
  await page.unroute('**/api/v1/upstream-repositories/clamav');
  await page.unroute('**/api/v1/visibility-choices');
  await page.route('**/api/v1/upstream-repositories', async (route) => {
    const res = await route.fetch();
    const body = (await res.json()) as { items: Record<string, unknown>[] };
    await route.fulfill({ response: res, body: JSON.stringify({ ...body, items: body.items.map((i) => patched(i, state)) }) });
  });
  await page.route('**/api/v1/upstream-repositories/clamav', async (route) => {
    const res = await route.fetch();
    await route.fulfill({ response: res, body: JSON.stringify(patched((await res.json()) as Record<string, unknown>, state)) });
  });
  if (state === 'lowered') {
    await page.route('**/api/v1/visibility-choices', async (route) => {
      const res = await route.fetch();
      const body = { ...((await res.json()) as Record<string, unknown>), publicAvailable: false };
      await route.fulfill({ response: res, body: JSON.stringify(body) });
    });
  }
  await page.reload();
  await page.waitForLoadState('networkidle');
}

/**
 * The admin area's page of the upstream registries with the row of `clamav` (R-UI-187, Q-UI-903): as it opens, its switch on as the
 * seed left it; the question before switching off; the switch off.
 */
export async function* clamavRow(page: Page): AsyncGenerator<string> {
  const toggle = page.getByRole('switch', { name: 'ClamAV definition updates' });
  await expect(toggle).toBeVisible();
  yield '';
  if ((await toggle.getAttribute('aria-checked')) === 'true') {
    await toggle.click();
    await expect(page.getByRole('dialog')).toContainText('Stop serving the ClamAV definitions?');
    yield 'off-question';
    await page.getByRole('dialog').getByRole('button', { name: 'Cancel' }).click();
  }
  await switchState(page, 'off');
  await expect(toggle).toHaveAttribute('aria-checked', 'false');
  yield 'off';
}

/**
 * The page of `clamav` among the upstream registries (R-UI-188, Q-UI-903): off; on at Public with the fold **Visibility** and the
 * token's disclosure open; on at Internal where Public is not allowed, with the warning.
 */
export async function* clamavRegistry(page: Page): AsyncGenerator<string> {
  await switchState(page, 'off');
  await expect(page.locator('main .clients-section')).toBeVisible();
  yield 'off';
  await page.getByRole('switch', { name: 'ClamAV definition updates' }).click();
  await expect(page.getByRole('dialog')).toContainText('Offer the ClamAV definitions to your ClamAV installations?');
  yield 'on-question';
  await page.getByRole('dialog').getByRole('button', { name: 'Cancel' }).click();
  await switchState(page, 'on');
  await expect(page.locator('main quader-address-field')).toBeVisible();
  yield 'on';
  await page.locator('main .level-fold').getByRole('button', { name: /^Visibility/ }).click();
  await page.locator('main .clamav-setup quader-disclosure').getByRole('button', { name: /^Authenticate/ }).click();
  yield 'on-details';
  await switchState(page, 'lowered');
  await expect(page.locator('main .clients-section .banner-warning')).toBeVisible();
  yield 'lowered';
}

/** A screen of the definitions: their calls answered in the browser, the switch on (the reader's view of R-UI-194). */
/**
 * The databases of `upstream/clamav` (R-UI-194): as they open, a `daily` of 5 hours; then with the `daily` in use built 8 days ago, the
 * one database whose age ClamAV warns about, with **Outdated** and its sentence (Q-UI-912).
 */
export async function* clamavDatabases(page: Page): AsyncGenerator<string> {
  await routeDefinitions(page);
  await switchState(page, 'on');
  await expect(page.locator('main quader-clamav-databases tbody tr')).toHaveCount(3);
  yield '';
  dailyInUseAgo = 8 * DAY + 3 * HOUR;
  try {
    await page.reload();
    await page.waitForLoadState('networkidle');
    await expect(page.locator('main quader-clamav-databases .badge.warn')).toHaveCount(1);
    yield 'daily-outdated';
  } finally {
    dailyInUseAgo = null;
  }
}

export function definitionsWith(state: SwitchState, then?: (page: Page) => Promise<void>): (page: Page) => AsyncGenerator<string> {
  return async function* (page) {
    await routeDefinitions(page);
    await switchState(page, state);
    if (then) await then(page);
    yield '';
  };
}
