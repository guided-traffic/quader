import { mkdirSync } from 'node:fs';
import { join } from 'node:path';
import { test } from '@playwright/test';
import { root } from './paths';
import { screens } from './screens';

/**
 * A measurement, not a check (CLAUDE.md, "Tests stay runnable"; api-and-ui.md R-UI-217): what every screen shows while its reads are
 * slow. It runs only with UI_RENDERS_SLOW_LOADS set to the milliseconds every read of the app's API is held back, against the renders'
 * seed of the UI development profile, and prints a line per screen; docs/developer/web-ui.md keeps the result:
 *
 *   UI_RENDERS_SLOW_LOADS=2000 make ui-renders GREP='slow loads'
 *
 * Each screen is opened as the first kind of user that reaches it, at 1,440 px in the light scheme, with every GET of `/api/` and `/auth/`
 * answered `UI_RENDERS_SLOW_LOADS` ms late, and looked at half of those after the app's first navigation ended (the spinner of the first
 * paint is gone): the screen's own reads are open then. A screen whose content panel then shows no visible `quader-loading`
 * and less than a line of text says nothing while it loads: the line says `EMPTY`. The pictures go to `.out/ui-slow-loads/` to be looked
 * at (a section that stays empty under a header is seen there, not counted).
 */
const delay = Number(process.env['UI_RENDERS_SLOW_LOADS'] ?? 0);
const out = join(root, '.out', 'ui-slow-loads');

test.describe('slow loads', () => {
  test.skip(!(delay > 0), 'a measurement: set UI_RENDERS_SLOW_LOADS to the milliseconds every read is held back');
  for (const screen of screens) {
    const kind = screen.kinds[0];
    if (!kind || screen.walk) continue;
    test(`slow loads of ${screen.id} as ${kind} @${kind}`, async ({ page }) => {
      await page.setViewportSize({ width: 1440, height: 900 });
      await page.route(
        (url) => /^\/(api|auth)\//.test(url.pathname),
        async (route) => {
          if (route.request().method() === 'GET') await new Promise((resolve) => setTimeout(resolve, delay));
          await route.fallback().catch(() => undefined);
        },
      );
      await page.goto(screen.path, { waitUntil: 'commit' });
      // The first screen stands once the app's first navigation has ended (its spinner of the first paint is gone); its own reads run.
      await page.waitForFunction(() => document.querySelector('quader-root') !== null && !document.querySelector('.quader-boot'), null, { timeout: 60_000 });
      await page.waitForTimeout(delay / 2);
      const seen = await page.evaluate(() => {
        const main = document.querySelector('main') ?? document.body;
        const loading = [...document.querySelectorAll<HTMLElement>('quader-loading, .quader-boot, button[aria-busy="true"]')].filter(
          (e) => getComputedStyle(e).visibility === 'visible' && e.getBoundingClientRect().height > 0,
        );
        return { loading: loading.map((e) => (e.textContent ?? '').trim()).join(' | '), text: (main.textContent ?? '').replace(/\s+/g, ' ').trim().length };
      });
      mkdirSync(join(out, kind), { recursive: true });
      await page.screenshot({ path: join(out, kind, `${screen.id}.png`) });
      const empty = seen.loading === '' && seen.text < 40;
      console.log(`slow-loads ${empty ? 'EMPTY' : 'ok   '} ${screen.id} as ${kind}: ${seen.loading || '(no load shown)'}; ${seen.text} characters`);
      await page.unrouteAll({ behavior: 'ignoreErrors' });
    });
  }
});
