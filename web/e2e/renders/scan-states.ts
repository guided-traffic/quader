import { Page, expect } from '@playwright/test';

/**
 * The state of a version's check by its cause in every list (R-VULN-039, Q-VULN-274, Q-UI-1120) and of a file's malware scan
 * (R-MAL-024, Q-UI-1123): the render database holds one state at a time (its images wait for a database for images, its malware scan
 * has no scanner), so the walks answer the grades and the files in the browser with every state the server sends, from the real
 * answers, which keep who may read what. Every state is in a picture, in both languages, with the longest German words ("Prüfung
 * fehlgeschlagen", "Keine Advisory-Daten") in the narrow cell at 390 px.
 */

/** Every state but checked, in the order the walks hand them out. */
export const CHECK_STATES = [
  'queued',
  'scanning',
  'no_scanner',
  'no_database',
  'scan_failed',
  'over_limits',
  'unreadable',
  'off_installation',
  'off_repository',
  'not_matched',
  'unknown_upstream',
  'internal',
  'format_not_scanned',
  'not_an_image',
  'no_advisory_data',
] as const;

type Grade = Record<string, unknown>;

/** A grade of the answer turned N/A with the state; a time where the state has one, a code where a failed or put-off scan has one. */
function withState(g: Grade, state: (typeof CHECK_STATES)[number], minutes: number): Grade {
  const ago = (m: number) => new Date(Date.now() - m * 60_000).toISOString();
  const code: Partial<Record<string, string>> = { scan_failed: 'timeout', over_limits: 'image_too_large', unreadable: 'layer_format', no_scanner: 'scanner_unavailable' };
  return {
    ...g,
    grade: 'N/A',
    open: { critical: 0, high: 0, medium: 0, low: 0, none: 0, unknown: 0 },
    fixable: 0,
    checkedAt: null,
    state,
    stateSince: state === 'queued' ? ago(minutes) : state === 'scanning' ? ago(1) : null,
    stateCode: code[state] ?? null,
  };
}

/** The list of a repository's items: the first row keeps its grade, the others take the states from `from` on. */
async function routeList(page: Page, from: number): Promise<void> {
  const url = '**/api/v1/packages?**';
  await page.unroute(url);
  await page.route(url, async (route) => {
    const res = await route.fetch();
    const body = (await res.json()) as { items?: { vulnerabilityGrade?: Grade }[] };
    (body.items ?? []).forEach((p, i) => {
      if (i > 0 && p.vulnerabilityGrade) p.vulnerabilityGrade = withState(p.vulnerabilityGrade, CHECK_STATES[(from + i - 1) % CHECK_STATES.length]!, 13);
    });
    await route.fulfill({ response: res, body: JSON.stringify(body) });
  });
}

/** A package's report (the opened item's versions, the package page's): every version takes a state, from `from` on. */
async function routeReport(page: Page, from: number): Promise<void> {
  const url = '**/api/v1/packages/*/vulnerabilities**';
  await page.unroute(url);
  await page.route(url, async (route) => {
    const res = await route.fetch();
    const body = (await res.json()) as { versions?: { grade: Grade }[] };
    (body.versions ?? []).forEach((v, i) => {
      v.grade = withState(v.grade, CHECK_STATES[(from + i) % CHECK_STATES.length]!, 13 + i);
    });
    await route.fulfill({ response: res, body: JSON.stringify(body) });
  });
}

/**
 * A repository of images (R-UI-091, R-VULN-039): the second item's row says its state beside the first's grade; opened, the first item's
 * tags say a state each, three times until every state was in a picture. The sentence of a cell, its tooltip, is the unit tests' (the
 * picture is taken without the hover).
 */
export async function* scanStatesList(page: Page): AsyncGenerator<string> {
  await routeList(page, 0);
  await routeReport(page, 0);
  await page.reload();
  await expect(page.locator('main tbody quader-grade').first()).toBeVisible();
  yield '';
  // The image with six tags (portal/web of the seed), so that three passes show every state.
  const open = page.locator('main tbody tr', { hasText: 'portal/web' }).locator('.name-cell button[aria-expanded]').first();
  const versions = page.locator('main .collapse-region.open quader-repository-item-versions');
  // A picture's name keeps letters and dashes only, so each pass has a word of its own.
  const names = ['opened', 'opened-more', 'opened-last', 'opened-rest'];
  let n = 0;
  for (let from = 0, pass = 0; from < CHECK_STATES.length && pass < names.length; from += n, pass++) {
    if (from > 0) {
      await routeReport(page, from);
      await page.reload();
      await expect(page.locator('main tbody quader-grade').first()).toBeVisible();
    }
    if ((await open.getAttribute('aria-expanded')) === 'false') await open.click();
    await expect(versions.locator('tbody quader-grade').first()).toBeVisible();
    n = await versions.locator('tbody quader-grade').count();
    yield names[pass]!;
  }
  await page.unroute('**/api/v1/packages?**');
  await page.unroute('**/api/v1/packages/*/vulnerabilities**');
}

/** A package's page (Q-UI-1121): its versions say a state each, the column kept although none has a grade. */
export async function* scanStatesPackage(page: Page): AsyncGenerator<string> {
  for (const [from, name] of [[0, ''], [6, 'more'], [12, 'last']] as const) {
    await routeReport(page, from);
    await page.reload();
    await expect(page.locator('main tbody quader-grade').first()).toBeVisible();
    yield name;
  }
  await page.unroute('**/api/v1/packages/*/vulnerabilities**');
}

/** A version's card of a failed scan (Q-UI-1120): the class in words, the code folded under Technical details, then opened. */
export async function* scanFailedCard(page: Page): AsyncGenerator<string> {
  const url = '**/api/v1/packages/*/versions/*/vulnerabilities**';
  await page.route(url, async (route) => {
    const res = await route.fetch();
    const body = (await res.json()) as { repositories?: { grade: Grade; evaluation: string }[] };
    for (const r of body.repositories ?? []) {
      r.grade = withState(r.grade, 'over_limits', 0);
      r.evaluation = 'scan_failed';
    }
    await route.fulfill({ response: res, body: JSON.stringify(body) });
  });
  await page.reload();
  const card = page.locator('main quader-grade-card').first();
  await expect(card.locator('quader-technical-details')).toBeVisible();
  yield '';
  await card.locator('quader-technical-details button').first().click();
  await expect(card.locator('[data-testid="technical-details"]')).toBeVisible();
  yield 'details';
  await page.unroute(url);
}

/** A version's files whose malware scan waits (Q-UI-1123): queued with its time, no scanner, no signatures, each in a picture. */
export async function* malwareWaiting(page: Page): AsyncGenerator<string> {
  const waits = [
    { name: '', waiting: 'queued', queuedSince: new Date(Date.now() - 13 * 60_000).toISOString() },
    { name: 'no-scanner', waiting: 'no_scanner', queuedSince: null },
    { name: 'no-database', waiting: 'no_database', queuedSince: null },
  ];
  const url = /\/api\/v1\/packages\/[^/]+\/versions\/[^/?]+(\?.*)?$/;
  for (const { name, ...wait } of waits) {
    await page.unroute(url);
    await page.route(url, async (route) => {
      const res = await route.fetch();
      const body = (await res.json()) as { files?: { malware?: Record<string, unknown> }[] };
      for (const f of body.files ?? []) {
        if (!f.malware) continue;
        f.malware = { ...f.malware, state: 'pending', signature: null, part: null, reason: null, released: false, releasedBy: null, releasedAt: null, refused: false, scannedAt: null, ...wait };
      }
      await route.fulfill({ response: res, body: JSON.stringify(body) });
    });
    await page.reload();
    await expect(page.locator('main quader-malware-status').first()).toBeVisible();
    yield name;
  }
  await page.unroute(url);
}
