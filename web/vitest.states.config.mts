import { resolve } from 'node:path';
import { playwright } from '@vitest/browser-playwright';
import { defineConfig } from 'vitest/config';
import { CHROMIUM_TEXT } from './e2e/renders/paths.ts';

/**
 * The runner's settings of `make ui-states` (the target `states` of angular.json, docs/developer/web-ui.md#dialog-states): the
 * `*.states.spec.ts` in headless Chromium, the one of the locked Playwright version (no second browser), with text set without
 * hinting as the renders set it (CHROMIUM_TEXT, Q-UI-1045), in the light scheme, English and UTC. The builder's own `browsers`
 * option builds its provider without launch arguments, so the browser is configured here.
 */
export default defineConfig({
  server: {
    // The pictures go to .out/ui-states at the repository's root, outside the workspace's folder.
    fs: { allow: [resolve(import.meta.dirname, '..', '.out')] },
  },
  test: {
    browser: {
      enabled: true,
      headless: true,
      provider: playwright({
        launchOptions: { args: [...CHROMIUM_TEXT] },
        contextOptions: { colorScheme: 'light', deviceScaleFactor: 1, locale: 'en-US', timezoneId: 'UTC' },
      }),
      instances: [{ browser: 'chromium' }],
      viewport: { width: 1440, height: 900 },
      screenshotFailures: false,
    },
    provide: {
      // UI_STATES_PICTURES=1 writes a picture of every state at every width (never in the pipeline).
      picturesDir: process.env['UI_STATES_PICTURES'] === '1' ? resolve(import.meta.dirname, '..', '.out', 'ui-states') : '',
    },
  },
});
