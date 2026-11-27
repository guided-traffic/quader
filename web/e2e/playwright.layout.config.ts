import { defineConfig, devices } from '@playwright/test';
import { CHROMIUM_TEXT } from './renders/paths';

/**
 * `make ui-layout-test` (docs/developer/web-ui.md#renders-of-every-screen): the layout checks of the renders (renders/layout.ts)
 * and the request tracking of their `settle` (renders/requests.ts) on fixture pages of their own, in Chromium, Firefox and WebKit (Q-UI-1045), with the app's fonts and its global stylesheet. No
 * stack, no seed: it runs in seconds, and `make ui-renders` runs it before the screens, so a check is proven before it judges them.
 * The screens' renders stay in Chromium (the pipeline's longest job); the fixtures hold in the other two engines that the checks
 * measure what they name, by what an engine lays out and not by its name: no fixture is marked for one engine.
 */
export default defineConfig({
  testDir: './renders',
  // The checks' fixtures, and the calls the render run's `settle` waits for (requests.spec.ts).
  testMatch: ['layout-checks.spec.ts', 'requests.spec.ts'],
  outputDir: '../../.out/ui-layout-test',
  workers: 2,
  fullyParallel: true,
  retries: 0,
  forbidOnly: true,
  timeout: 60_000,
  reporter: [['list']],
  use: { trace: 'off', screenshot: 'off' },
  projects: [
    { name: 'chromium', use: { ...devices['Desktop Chrome'], launchOptions: { args: [...CHROMIUM_TEXT] } } },
    { name: 'firefox', use: { ...devices['Desktop Firefox'] } },
    { name: 'webkit', use: { ...devices['Desktop Safari'] } },
  ],
});
