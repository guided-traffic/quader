import { Page, expect } from '@playwright/test';
import { ciIssuer } from './seed';

/**
 * The controls the Helm configuration fixes, offered unavailable with their reason as the tooltip (api-and-ui.md R-UI-270), where the
 * profile declares nothing: a role binding of `config.authz.role_bindings` and a CI issuer of `config.ci.issuers`, each the server's
 * real answer changed in the browser. Nothing is written. The controls are found by their markup, never by a word, so the German pass
 * walks them too.
 */

/** A GET of the app's API answered by the server and changed in the browser, and the page loaded again. */
async function answerWith(page: Page, path: string, change: (body: Record<string, unknown>) => Record<string, unknown>): Promise<void> {
  const url = (u: URL): boolean => u.pathname === path;
  await page.unroute(url);
  await page.route(url, async (route) => {
    if (route.request().method() !== 'GET') return route.fallback();
    const res = await route.fetch();
    await route.fulfill({ response: res, body: JSON.stringify(change((await res.json()) as Record<string, unknown>)) });
  });
  const answered = page.waitForResponse((r) => r.request().method() === 'GET' && new URL(r.url()).pathname === path);
  await page.reload();
  await answered;
  await page.waitForLoadState('networkidle');
}

/** The pinned bubble of an unavailable control, tapped. */
async function tapped(page: Page, control: ReturnType<Page['locator']>): Promise<void> {
  await expect(control).toHaveAttribute('aria-disabled', 'true');
  await control.scrollIntoViewIfNeeded();
  // Playwright holds an aria-disabled control for not enabled and waits; the tap a person makes reaches it (R-UI-270).
  await control.click({ force: true });
  await expect(page.locator('.cdk-overlay-container quader-tooltip')).toBeVisible();
}

/**
 * **System roles** with a binding of the administrator role that the Helm configuration declares beside the seed's: its **Unbind**
 * unavailable, no note under the table; then that **Unbind** tapped, its reason pinned.
 */
export async function* helmRoleBinding(page: Page): AsyncGenerator<string> {
  await answerWith(page, '/api/v1/role-bindings', (body) => ({
    ...body,
    items: [
      ...((body['items'] as unknown[]) ?? []),
      { role: 'admin', principal: 'group:platform-admins', grantedAt: '2026-11-02T08:00:00Z', grantedBy: '', source: 'configuration' },
    ],
  }));
  const unbind = page.locator('main section.card').first().locator('tbody tr', { hasText: 'platform-admins' }).locator('button');
  await expect(unbind).toHaveAttribute('aria-disabled', 'true');
  yield '';
  await tapped(page, unbind);
  yield 'unbind';
  await page.keyboard.press('Escape');
  await page.unrouteAll({ behavior: 'ignoreErrors' });
}

/**
 * The seed's CI issuer as one of the Helm configuration on its tab **Settings**: the mark **Helm configuration** with its help and the
 * read-only fields; then **Advanced** opened and **Delete issuer** tapped, its reason pinned.
 */
export async function* helmIssuer(page: Page): AsyncGenerator<string> {
  await answerWith(page, `/api/v1/ci/issuers/${ciIssuer}`, (body) => ({ ...body, source: 'configuration', removedFromConfiguration: false }));
  await expect(page.locator('main [data-testid="ci-issuer-declared"]')).toBeVisible();
  yield '';
  const advanced = page.locator('main quader-disclosure button[aria-expanded]').last();
  await advanced.click();
  await expect(advanced).toHaveAttribute('aria-expanded', 'true');
  await page.evaluate(() => Promise.all(document.getAnimations().map((a) => a.finished.catch(() => undefined))));
  await tapped(page, page.locator('main button.btn-danger'));
  yield 'delete';
  await page.keyboard.press('Escape');
  await page.unrouteAll({ behavior: 'ignoreErrors' });
}
