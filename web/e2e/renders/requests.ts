import { Page, Request } from '@playwright/test';

/**
 * The calls of the app's API a page has open, which the render run's `settle` waits for (render.spec.ts), and the GETs a screen's
 * load sends (UI_RENDERS_COUNT). Tested on a page of its own in `make ui-layout-test` (requests.spec.ts): no stack.
 */

/** The calls of the app's API that are open, per page. */
const open = new WeakMap<Page, Set<Request>>();
/** The GETs of the app's API since the page's last document. */
const gets = new WeakMap<Page, number>();

/** The calls of the app's API the page has open; empty for a page that is not tracked. */
export function openCalls(page: Page): ReadonlySet<Request> {
  return open.get(page) ?? new Set();
}

/** The GETs of the app's API the page's document sent (UI_RENDERS_COUNT). */
export function getsOf(page: Page): number {
  return gets.get(page) ?? 0;
}

/**
 * Tracks the calls of the app's API (and a Bastion's data path below its pages, R-UI-230) the page has open. A new document drops
 * the calls of the one before it when it commits, not when its navigation starts: the old document goes on sending until then (a
 * reload right after another sent the first document's sign-in call after the second navigation had begun), and Chromium cancels
 * what it left open without a "failed" when the server still works on the call, which kept `settle` waiting its 15 s on every
 * later state of the walk. A navigation inside the document (the router's) drops nothing: its view's calls are the screen's.
 */
export function trackRequests(page: Page): void {
  const pending = new Set<Request>();
  open.set(page, pending);
  const api = (url: string): boolean => /^\/(api|auth|ui\/data)\//.test(new URL(url).pathname);
  // The navigation of the main frame to a new document that has begun and not yet committed.
  let navigation: Request | null = null;
  page.on('request', (r) => {
    if (r.isNavigationRequest() && r.frame() === page.mainFrame()) navigation = r;
    if (!api(r.url())) return;
    pending.add(r);
    if (r.method() === 'GET') gets.set(page, getsOf(page) + 1);
  });
  page.on('framenavigated', (frame) => {
    if (frame !== page.mainFrame() || navigation === null) return;
    // The new document has committed: no call it sends has begun yet, every open one is the old document's.
    navigation = null;
    pending.clear();
    gets.set(page, 0);
  });
  page.on('requestfinished', (r) => pending.delete(r));
  page.on('requestfailed', (r) => {
    pending.delete(r);
    // A navigation that ends without a document (cancelled by the next one, a download) commits nothing.
    if (r === navigation) navigation = null;
  });
  // A redirected request gets its answer and no "finished" of its own.
  page.on('response', (r) => {
    if (r.status() >= 300 && r.status() < 400) pending.delete(r.request());
  });
}
