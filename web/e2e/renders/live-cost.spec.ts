import { Page, Request, test } from '@playwright/test';
import { downloadsWalk } from './downloads';
import { screens } from './screens';

/**
 * A measurement, not a check (CLAUDE.md, "Tests stay runnable"; api-and-ui.md R-UI-213 and H-1847): what one open tab of a live view
 * asks of the server, seen and hidden. It runs only with UI_RENDERS_LIVE_COST set to the seconds of each observation, against the
 * renders' seed of the UI development profile, and prints its numbers; docs/developer/web-ui.md keeps them:
 *
 *   UI_RENDERS_LIVE_COST=60 make ui-renders GREP='live cost'
 *
 * Each case opens one screen, waits until its first reads are done, counts the calls of the app's API while the tab is seen, then
 * while it is hidden (document.visibilityState, as a browser reports a tab in the background), then for a few seconds after it is
 * seen again. A screen that only polls while something runs is shown with something running: a version whose scan has not answered
 * (the profile scans no image), and the tab Downloads with a running queue (the renders' answers of downloads.ts).
 */
const seconds = Number(process.env['UI_RENDERS_LIVE_COST'] ?? 0);

interface Call {
  readonly at: number;
  readonly method: string;
  readonly path: string;
  readonly bytes: number;
}

/** The screens measured, by the renders' id, and the kind of user each is opened as. */
const cases: readonly { id: string; kind: 'plain' | 'power' | 'admin'; running?: (page: Page) => Promise<void> }[] = [
  { id: 'home', kind: 'admin' },
  { id: 'admin-dashboard', kind: 'admin' },
  { id: 'admin-system', kind: 'admin' },
  { id: 'admin-system-jobs', kind: 'admin' },
  { id: 'admin-system-storage', kind: 'admin' },
  { id: 'admin-audit', kind: 'admin' },
  { id: 'security', kind: 'admin' },
  { id: 'home', kind: 'plain' },
  { id: 'repository-npm-vendor', kind: 'plain' },
  { id: 'repository-npm-remote-upstream', kind: 'plain' },
  { id: 'package-npm', kind: 'plain' },
  { id: 'package-version-oci', kind: 'plain' },
  { id: 'vulnerabilities', kind: 'plain' },
  {
    id: 'repository-npm-remote-downloads',
    kind: 'power',
    running: async (page) => {
      // The walk's second state: the queue of a cache that is being built, two rows running and ten waiting.
      const walk = downloadsWalk(page);
      await walk.next();
      await walk.next();
    },
  },
];

/** The path of a call with its identifiers folded, so that the calls of one kind count together. */
function shape(url: string): string {
  const u = new URL(url);
  return (u.pathname + u.search)
    .replace(/(from|to|since|until)=[^&]*/g, '$1=…')
    .replace(/[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}/g, '{id}')
    .replace(/sha256:[0-9a-f]{64}/g, '{digest}');
}

function summary(calls: readonly Call[], from: number, to: number): string {
  const inside = calls.filter((c) => c.at >= from && c.at < to);
  const minutes = (to - from) / 60_000;
  const by = new Map<string, { n: number; bytes: number }>();
  for (const c of inside) {
    const k = `${c.method} ${c.path}`;
    const v = by.get(k) ?? { n: 0, bytes: 0 };
    by.set(k, { n: v.n + 1, bytes: v.bytes + c.bytes });
  }
  const bytes = inside.reduce((a, c) => a + c.bytes, 0);
  const parts = [...by.entries()].sort((a, b) => b[1].n - a[1].n).map(([k, v]) => `${k} ×${v.n} (${Math.round(v.bytes / v.n)} B)`);
  return `${(inside.length / minutes).toFixed(1)} calls/min, ${Math.round(bytes / minutes / 1024)} KiB/min [${parts.join('; ')}]`;
}

async function setHidden(page: Page, hidden: boolean): Promise<void> {
  await page.evaluate((h) => {
    Object.defineProperty(document, 'visibilityState', { configurable: true, get: () => (h ? 'hidden' : 'visible') });
    Object.defineProperty(document, 'hidden', { configurable: true, get: () => h });
    document.dispatchEvent(new Event('visibilitychange'));
  }, hidden);
}

test.describe('live cost', () => {
  test.skip(!(seconds > 0), 'a measurement: set UI_RENDERS_LIVE_COST to the seconds of each observation');
  for (const c of cases) {
    const screen = screens.find((s) => s.id === c.id);
    test(`live cost of ${c.id} as ${c.kind} @${c.kind}`, async ({ page }) => {
      test.setTimeout((seconds * 2 + 90) * 1000);
      if (!screen) throw new Error(`no screen ${c.id}`);
      const calls: Call[] = [];
      const documents: number[] = [];
      page.on('request', (r: Request) => {
        if (r.isNavigationRequest() && r.frame() === page.mainFrame()) documents.push(Date.now());
      });
      page.on('requestfinished', (r: Request) => {
        const path = new URL(r.url()).pathname;
        if (!/^\/(api|auth)\//.test(path)) return;
        // The time is the call's end, taken at once; its size comes later.
        const call = { at: Date.now(), method: r.method(), path: shape(r.url()), bytes: 0 };
        calls.push(call);
        void r.sizes().then((s) => (call.bytes = s.responseBodySize));
      });
      // The app's fetch backend ends a request when its answer is read (Angular's FetchBackend aborts on unsubscribe), which the
      // browser reports as failed (net::ERR_ABORTED) for an answer it has not marked finished yet: the audit log's page of 100 events
      // and the findings counted as no call, and H-1871 read as a view that did not read (Q-UI-1216). A call with an answer counts.
      page.on('requestfailed', (r: Request) => {
        const path = new URL(r.url()).pathname;
        if (!/^\/(api|auth)\//.test(path)) return;
        const at = Date.now();
        void r.response().then((res) => {
          if (res) calls.push({ at, method: r.method(), path: shape(r.url()), bytes: 0 });
        });
      });
      await page.goto(screen.path);
      if (c.running) await c.running(page);
      await page.waitForTimeout(10_000);
      const t0 = Date.now();
      await page.waitForTimeout(seconds * 1000);
      const t1 = Date.now();
      await setHidden(page, true);
      await page.waitForTimeout(seconds * 1000);
      const t2 = Date.now();
      await setHidden(page, false);
      await page.waitForTimeout(3_000);
      const t3 = Date.now();
      // A document loaded after the first one (the dev server's reload after a change of the source) spoils the observation.
      if (documents.some((d) => d >= t0)) throw new Error(`${c.id}: the page loaded a new document during the observation`);
      console.log(`LIVE-COST ${c.id} as ${c.kind} (${screen.path}), ${seconds} s each`);
      console.log(`LIVE-COST   seen:   ${summary(calls, t0, t1)}`);
      console.log(`LIVE-COST   hidden: ${summary(calls, t1, t2)}`);
      const back = calls.filter((x) => x.at >= t2 && x.at < t3);
      console.log(`LIVE-COST   seen again, first 3 s: ${back.length} calls [${back.map((x) => x.path).join('; ')}]`);
      // UI_RENDERS_LIVE_COST_TRACE=1: every call with its second since the observation began (seen from 0, hidden from the seconds).
      if (process.env['UI_RENDERS_LIVE_COST_TRACE']) {
        for (const x of calls.filter((y) => y.at >= t0)) console.log(`LIVE-COST     +${((x.at - t0) / 1000).toFixed(1)} s ${x.path}`);
      }
    });
  }
});
