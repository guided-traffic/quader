import { defineConfig, devices } from '@playwright/test';

/**
 * `make ui-layout-test` (docs/developer/web-ui.md#renders-of-every-screen): the layout checks of the renders (renders/layout.ts)
 * on fixture pages of their own, in Chromium, with the app's fonts and its global stylesheet. No stack, no seed: it runs in
 * seconds, and `make ui-renders` runs it before the screens, so a check is proven before it judges them.
 */
export default defineConfig({
  testDir: './renders',
  testMatch: 'layout-checks.spec.ts',
  outputDir: '../../.out/ui-layout-test',
  workers: 2,
  fullyParallel: true,
  retries: 0,
  forbidOnly: true,
  timeout: 60_000,
  reporter: [['list']],
  use: { ...devices['Desktop Chrome'], trace: 'off', screenshot: 'off' },
});
