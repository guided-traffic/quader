import { createServer, IncomingMessage, Server, ServerResponse } from 'node:http';
import { AddressInfo, Socket } from 'node:net';
import { expect, test } from '@playwright/test';
import { openCalls, trackRequests } from './requests';

/**
 * The calls `settle` of the render run waits for (requests.ts), on pages of a server of this file: no stack, no seed, a second.
 * The server never answers a call of `/api/hold`, as a server still at work on a call does when its document goes.
 */

let server: Server;
let base = '';
const sockets = new Set<Socket>();

/**
 * A page whose document goes on sending calls that never end, one every 80 ms, four at most: a browser keeps six connections to a
 * server, and a fifth and sixth open call would hold the next document's navigation in its queue.
 */
const sending = `<!doctype html><title>one</title><script>
  let n = 0;
  const id = setInterval(() => {
    fetch('/api/hold?' + n).catch(() => undefined);
    if (++n === 4) clearInterval(id);
  }, 80);
</script>`;

/** A page that sends one call that never ends and then moves inside the document, as the router does. */
const routed = `<!doctype html><title>routed</title><script>
  fetch('/api/hold?routed').catch(() => undefined);
  history.pushState({}, '', '/routed/view');
</script>`;

function serve(req: IncomingMessage, res: ServerResponse): void {
  const path = new URL(req.url ?? '/', base).pathname;
  if (path === '/api/hold') return; // never answered
  const html = (body: string): void => {
    res.writeHead(200, { 'content-type': 'text/html' });
    res.end(body);
  };
  if (path === '/one') return html(sending);
  // The next document answers late: the one before it sends meanwhile, after this navigation has begun.
  if (path === '/two') return void setTimeout(() => html('<!doctype html><title>two</title>'), 300);
  if (path === '/routed') return html(routed);
  res.writeHead(404).end();
}

test.beforeAll(async () => {
  server = createServer(serve);
  server.on('connection', (s) => {
    sockets.add(s);
    s.on('close', () => sockets.delete(s));
  });
  await new Promise<void>((done) => server.listen(0, '127.0.0.1', done));
  base = `http://127.0.0.1:${(server.address() as AddressInfo).port}`;
});

test.afterAll(async () => {
  for (const s of sockets) s.destroy();
  await new Promise((done) => server.close(done));
});

test('a new document drops the calls its predecessor left open, also those sent after its navigation began', async ({ page }) => {
  trackRequests(page);
  await page.goto(`${base}/one`);
  await expect.poll(() => openCalls(page).size).toBeGreaterThan(0);
  // The old document sends on while the new one is on its way (Chromium and Firefox do; WebKit stops its timers when the
  // navigation begins): the calls a tracker that dropped them when the navigation began kept open.
  await page.goto(`${base}/two`);
  expect([...openCalls(page)].map((r) => r.url())).toEqual([]);
});

test('a navigation inside the document keeps the calls open: they are its view', async ({ page }) => {
  trackRequests(page);
  await page.goto(`${base}/routed`);
  await expect(page).toHaveURL(`${base}/routed/view`);
  await expect.poll(() => [...openCalls(page)].map((r) => new URL(r.url()).search)).toEqual(['?routed']);
});
