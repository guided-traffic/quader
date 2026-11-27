import { defineConfig, devices } from '@playwright/test';
import { CHROMIUM_TEXT, authFile, edge, engine, locale, shard } from './renders/paths';

/**
 * `make ui-renders` (docs/developer/web-ui.md#renders-of-every-screen): every screen of the UI as pictures to judge, against
 * the UI development profile with the seed of renders/seed.ts. Locally, and in the job ui-layout of Test and Release (Q-ARCH-510). `deploy/ui-dev/renders.sh` starts
 * what is not running (the services, Quader with the profile's sign-in provider, the dev server) and runs this
 * configuration; by hand: `npx playwright test -c e2e/playwright.renders.config.ts [--grep <screen>]` in web/.
 *
 * UI_RENDERS_EDGE      the dev server, default https://localhost:4200
 * UI_RENDERS_STATE     the profile's state directory, default .dev/ui-dev (a stack of its own names its own)
 * UI_RENDERS_WORKERS   browsers at a time, default 3 (the host is shared)
 * UI_RENDERS_LOCALE    en (default), or de: the screens marked `de`, against the dev server's German build (Q-UI-1048)
 * UI_RENDERS_ENGINE    chromium (default), firefox or webkit (Q-UI-1049); a run other than the default writes into a folder of its own
 * UI_RENDERS_SHARD     i/n: the i-th of n parts of the screens (renders/paths.ts, Q-ARCH-1071); unset, the whole set
 */
const device = { chromium: 'Desktop Chrome', firefox: 'Desktop Firefox', webkit: 'Desktop Safari' }[engine];
export default defineConfig({
  testDir: './renders',
  // The checks' own fixtures run in `make ui-layout-test` (playwright.layout.config.ts), without the stack and the seed.
  testIgnore: 'layout-checks.spec.ts',
  // A part of the screens (UI_RENDERS_SHARD) holds the screens alone: the measurements (live-cost, slow-loads) run with the whole set and
  // their GREP, and a spec file a part leaves out shows as missing in `make ui-renders-parts-check`, never as rendered in every leg.
  ...(shard.total > 1 ? { testMatch: 'render.spec.ts' } : {}),
  outputDir: '../../.out/ui-renders/.results',
  workers: Number(process.env['UI_RENDERS_WORKERS'] ?? 3),
  fullyParallel: true,
  retries: 0,
  forbidOnly: true,
  // Three widths, two schemes and every tab of a screen, each settled before its picture.
  timeout: 300_000,
  expect: { timeout: 15_000 },
  globalSetup: './renders/setup.ts',
  // The contact sheet, then the instance's vulnerability database back (teardown.ts, R-VULN-053).
  globalTeardown: './renders/teardown.ts',
  reporter: [['list']],
  use: {
    baseURL: edge,
    // The dev server's certificate is signed by the profile's CA, which no browser store holds.
    ignoreHTTPSErrors: true,
    locale: locale === 'de' ? 'de-DE' : 'en-US',
    timezoneId: 'UTC',
    actionTimeout: 15_000,
    navigationTimeout: 60_000,
    trace: 'off',
    screenshot: 'off',
    // The scrollbars are shown, as on a desktop with a mouse: one beside a table's rows takes the table's width (15 px in Chromium on
    // macOS and Linux alike once the flag is gone). Text is set without hinting (CHROMIUM_TEXT, Q-UI-1045), so the walk measures the
    // same widths on the development machine and on the runner. Firefox and WebKit take neither flag and show their own scrollbars.
    ...(engine === 'chromium' ? { launchOptions: { ignoreDefaultArgs: ['--hide-scrollbars'], args: [...CHROMIUM_TEXT] } } : {}),
  },
  projects: (['anonymous', 'plain', 'power', 'admin', 'auditor'] as const).map((kind) => ({
    name: kind,
    grep: new RegExp(`@${kind}\\b`),
    // One CSS pixel to one pixel of the picture in every engine: Desktop Safari is a screen of scale 2, which doubled WebKit's pictures.
    use: { ...devices[device], deviceScaleFactor: 1, ...(kind === 'anonymous' ? {} : { storageState: authFile(kind) }) },
  })),
});
