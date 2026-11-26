import { Page, Route, expect } from '@playwright/test';

/**
 * What a visitor without a session reads of the vulnerabilities of a public repository (R-UI-224, the owner's decision of 2026-11-26):
 * the version's tab from the report's own findings, each linking to its advisory's page, which the walk then opens; an image at the end
 * of life of its OS, which the render profile cannot scan, answered in the browser from the real report with the C the end of life makes.
 */

/** The tab's grade card and findings, then the advisory page the first finding links to. */
export async function* anonymousFindings(page: Page): AsyncGenerator<string> {
  await expect(page.locator('main quader-package-vulnerabilities quader-grade-card').first()).toBeVisible();
  yield '';
  const first = page.locator('main quader-version-findings tbody a[href*="/advisories/"]').first();
  if ((await first.count()) === 0) return;
  await first.click();
  await expect(page).toHaveURL(/\/advisories\//);
  await expect(page.locator('main quader-data-table tbody tr').first()).toBeVisible();
  yield 'advisory';
}

/** The tab's grade card alone: a version without findings, or one that is not checked. */
export async function* anonymousCard(page: Page): AsyncGenerator<string> {
  await expect(page.locator('main quader-package-vulnerabilities quader-grade-card').first()).toBeVisible();
  yield '';
}

/**
 * An image whose OS is at its end of life and that has no finding: the grade C, the OS and "end of life" in the facts, and the rule that
 * says why (R-VULN-038). The real report keeps the repository, the manifests and who may read it; the walk sets what a scan would.
 */
export async function* anonymousEndOfLife(page: Page): AsyncGenerator<string> {
  const scanned = new Date(Date.now() - 3 * 3_600_000).toISOString();
  await page.route('**/api/v1/packages/*/versions/*/vulnerabilities', async (route: Route) => {
    const res = await route.fetch();
    const body = (await res.json()) as { repositories?: Record<string, unknown>[] };
    for (const r of body.repositories ?? []) {
      r['evaluation'] = 'evaluated';
      r['findings'] = [];
      r['blocked'] = false;
      r['blockReason'] = null;
      delete r['missingData'];
      r['images'] = ((r['images'] as Record<string, unknown>[] | undefined) ?? []).map((m) => ({
        ...m, state: 'scanned', scannedAt: scanned, osFamily: 'alpine', osName: '3.12.0', eosl: true, packages: 14,
      }));
      r['grade'] = {
        ...(r['grade'] as Record<string, unknown>),
        grade: 'C', open: { critical: 0, high: 0, medium: 0, low: 0, none: 0, unknown: 0 }, fixable: 0, eosl: true, os: 'alpine 3.12.0',
        checkedAt: scanned, evaluation: 'evaluated', state: 'checked', stateSince: null, stateCode: null,
      };
    }
    await route.fulfill({ response: res, body: JSON.stringify(body) });
  });
  await page.reload();
  await expect(page.locator('main quader-package-vulnerabilities quader-grade-card .eosl').first()).toBeVisible();
  yield '';
}
