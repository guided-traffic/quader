import { Page, Request, Route } from '@playwright/test';

type Url = Parameters<Page['route']>[0];
type Handler = (route: Route, request: Request) => unknown;

/** One `page.route` of a walk: the handler as the walk wrote it, the one Playwright calls, and the calls still running. */
interface Registration {
  readonly url: Url;
  readonly handler: Handler;
  readonly wrapped: Handler;
  stopped: boolean;
  readonly calls: Set<Promise<unknown>>;
}

/** How long `unroute` waits for a call in flight; one that takes longer is left to Playwright as before. */
const IN_FLIGHT_MS = 15_000;

/**
 * Makes the page's `unroute` wait for the walk's answers in flight before it takes their handlers away (R-UI-213).
 *
 * A walk changes an answer in the browser with `page.route` and a handler that fetches the real answer and fulfils a changed one,
 * and ends with `page.unroute`. Every view that shows what the server changes reads itself again (`live()` of
 * `web/src/app/core/live-refresh.ts`), so such a read can arrive while the walk ends. Playwright's `unroute` does not wait for a
 * handler that runs: once the page's last route is gone, the server lets the request go on to the network at once, and the
 * handler's `route.fulfill` throws "Route is already handled!" into the test, which stopped `package-version-signature-gate-oci` at its
 * next width (Test and Release 45388064566, 2026-11-24). Here `unroute` marks the handlers it removes, waits for their calls in
 * flight, then removes them; a request that arrives in between is passed on (`route.fallback()`), as it would be after the removal.
 * `unrouteAll` marks every handler and then does what Playwright does with the walk's `behavior`.
 */
export function awaitAnswersOnUnroute(page: Page): void {
  let registrations: Registration[] = [];
  const route = page.route.bind(page);
  const unroute = page.unroute.bind(page);
  const unrouteAll = page.unrouteAll.bind(page);

  page.route = async (url, handler, options) => {
    const calls = new Set<Promise<unknown>>();
    const r: Registration = {
      url,
      handler,
      stopped: false,
      calls,
      wrapped: (rt, request) => {
        if (r.stopped) return rt.fallback();
        const call = Promise.resolve(handler(rt, request));
        calls.add(call);
        const done = (): void => void calls.delete(call);
        call.then(done, done);
        return call;
      },
    };
    registrations.push(r);
    return route(url, r.wrapped, options);
  };

  page.unroute = async (url, handler) => {
    const removed = registrations.filter((r) => sameUrl(r.url, url) && (!handler || r.handler === handler));
    for (const r of removed) r.stopped = true;
    await settled(removed.flatMap((r) => [...r.calls]));
    registrations = registrations.filter((r) => !removed.includes(r));
    if (!handler) return unroute(url);
    for (const r of removed) await unroute(r.url, r.wrapped);
  };

  page.unrouteAll = async (options) => {
    for (const r of registrations) r.stopped = true;
    registrations = [];
    return unrouteAll(options);
  };
}

/** Playwright's own test of two route patterns (`urlMatchesEqual`): a regular expression by its source and flags, else the same value. */
function sameUrl(a: Url, b: Url): boolean {
  if (a instanceof RegExp && b instanceof RegExp) return a.source === b.source && a.flags === b.flags;
  return a === b;
}

/** Resolves when every call has ended, however it ended, or after `IN_FLIGHT_MS`. */
async function settled(calls: Promise<unknown>[]): Promise<void> {
  if (calls.length === 0) return;
  let timer: ReturnType<typeof setTimeout> | undefined;
  await Promise.race([Promise.allSettled(calls), new Promise<void>((resolve) => (timer = setTimeout(resolve, IN_FLIGHT_MS)))]);
  clearTimeout(timer);
}
