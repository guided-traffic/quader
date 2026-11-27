import { Page, expect } from '@playwright/test';

/**
 * The details of a version's grade (R-UI-091, R-VULN-051, R-VULN-052): when, by which scanner and against which database it was checked,
 * opened from the grade by a tap or the keyboard (R-UI-256). The render database scans no image (its images wait for a database for
 * images), so the walks answer the grades of images in the browser from the real answers, as the scan states do
 * (scan-states.ts): an image scanned under today's database and one under a database six days older; today's database is answered
 * too. A package's grade is the seed's own (advisories.ts). The details are found by their test id, never by an English word, so the
 * German pass walks them too.
 */

type Grade = Record<string, unknown>;

const details = (page: Page) => page.locator('.cdk-overlay-container [data-testid="grade-details"]');
const hoursAgo = (h: number) => new Date(Date.now() - h * 3_600_000).toISOString();
const zero = { critical: 0, high: 0, medium: 0, low: 0, none: 0, unknown: 0 };

/** Today's database of the walks: built five hours ago, fetched four hours ago. */
const today = () => ({
  scanner: 'vuln-scanner-trivy',
  digest: 'sha256:7f3a9c51e2d84b6a0c1f5e9d2b7a4c8e6f0d3b5a9c7e1f2d4b6a8c0e2f4a6b8c',
  builtAt: hoursAgo(5),
  syncedAt: hoursAgo(4),
  stale: false,
});
/** A database six days older than today's. */
const older = () => ({ digest: 'sha256:1b2c3d4e5f60718293a4b5c6d7e8f90123456789abcdef0123456789abcdef01', builtAt: hoursAgo(5 + 6 * 24) });

/** A grade of an image scanned against a database, `hours` ago. */
function scannedImage(g: Grade, letter: string, open: Partial<typeof zero>, database: { digest: string; builtAt: string }, hours: number): Grade {
  return {
    ...g,
    grade: letter,
    open: { ...zero, ...open },
    fixable: Math.min(1, Object.values(open).reduce((a, b) => a + (b ?? 0), 0)),
    eosl: false,
    os: 'alpine 3.24.1',
    checkedAt: hoursAgo(hours),
    evaluation: 'evaluated',
    state: 'checked',
    stateSince: null,
    stateCode: null,
    scanner: 'vuln-scanner-trivy',
    scannerVersion: '0.76.2',
    database,
  };
}

/** Today's database, answered in the browser (the render database holds none). */
async function routeToday(page: Page): Promise<void> {
  await page.route('**/api/v1/vulnerabilities/database', (route) => route.fulfill({ status: 200, contentType: 'application/json', json: today() }));
}

/** A package's report: the tag 1.4.0 (the first row) under today's database, 1.3.1 (the second) under an older one, the others today's. */
async function routePackageReport(page: Page): Promise<void> {
  await page.route('**/api/v1/packages/*/vulnerabilities**', async (route) => {
    const res = await route.fetch();
    const body = (await res.json()) as { versions?: { version: string; grade: Grade }[] };
    for (const v of body.versions ?? []) {
      v.grade =
        v.version === '1.4.0'
          ? scannedImage(v.grade, 'D', { critical: 2, high: 1 }, today(), 3)
          : v.version === '1.3.1'
            ? scannedImage(v.grade, 'B', { medium: 4 }, older(), 6 * 24 + 2)
            : scannedImage(v.grade, 'S', {}, today(), 3);
    }
    await route.fulfill({ response: res, body: JSON.stringify(body) });
  });
}

/** A version's report of an image: scanned under a database six days older. */
async function routeVersionReport(page: Page): Promise<void> {
  await page.route('**/api/v1/packages/*/versions/*/vulnerabilities**', async (route) => {
    const res = await route.fetch();
    const body = (await res.json()) as { repositories?: { grade: Grade; evaluation: string }[] };
    for (const r of body.repositories ?? []) {
      r.grade = scannedImage(r.grade, 'C', { high: 2, low: 1 }, older(), 6 * 24 + 2);
      r.evaluation = 'evaluated';
    }
    await route.fulfill({ response: res, body: JSON.stringify(body) });
  });
}

/** Closes open details by Escape, so that the next state starts from the page. */
async function close(page: Page): Promise<void> {
  await page.keyboard.press('Escape');
  await expect(details(page)).toBeHidden();
}

/**
 * The versions of an image (the package page's table): as it opens, then the details of the first tag's grade, scanned under today's
 * database, opened by a tap, and of the second's, under a database six days older.
 */
export async function* gradeDetailsVersions(page: Page): AsyncGenerator<string> {
  await routeToday(page);
  await routePackageReport(page);
  await page.reload();
  const grades = page.locator('main tbody button.grade');
  await expect(grades.first()).toBeVisible();
  yield '';
  await grades.nth(0).click();
  await expect(details(page)).toBeVisible();
  yield 'today';
  await close(page);
  await grades.nth(1).click();
  await expect(details(page)).toBeVisible();
  yield 'older';
  await close(page);
}

/** A version's page: its header with the grade, then the grade's details opened by the keyboard, ending with the link to the findings. */
export function gradeDetailsVersion(image: boolean): (page: Page) => AsyncGenerator<string> {
  return async function* (page: Page): AsyncGenerator<string> {
    // A package's grade is the seed's own (advisories.ts grades image-convert-tools E): only an image's answers are made here.
    if (image) {
      await routeToday(page);
      await routeVersionReport(page);
      await page.reload();
    }
    const grade = page.locator('main .facts-grade button.grade');
    await expect(grade).toBeVisible();
    // A checked version opens on its tab Vulnerabilities after its report arrived: the details open once the page stands, so that the
    // tab's own load does not close them before the picture (measured: the first width's cold load did).
    await expect(page.locator('main quader-grade-card').first()).toBeVisible();
    await page.waitForLoadState('networkidle');
    yield '';
    await grade.focus();
    await expect(details(page)).toBeVisible();
    yield 'details';
    await close(page);
  };
}
