import { Page, Route, expect } from '@playwright/test';
import { mainProject } from './seed';

/**
 * The failures of a repository's page, each as the shared error notice shows it (R-UI-216): the sentence in the page's language,
 * the facts of the answer, and the folded part opened. The seed's installation fails at none of them, so each state is answered in
 * the browser and the page is loaded again; nothing is written. The controls are found by their place, never by their words: the
 * screen is in the German pass. "No connection" is not among them: a call the browser aborts writes a console error, which the render
 * run counts as a failure; its notice is a state of `make ui-states` (error-notice.states.spec.ts).
 */

const repository = `/api/v1/projects/${mainProject}/repositories/npm-internal`;
const isRepository = (url: URL): boolean => url.pathname === repository;
const isWhoami = (url: URL): boolean => url.pathname === '/api/v1/auth/whoami';
const REQUEST_ID = '7f3c9a1e5b2d48c6a0e4f1b9d8c27a65';

/** A proxy's error page, as nginx sends it while the backend behind it restarts. */
const PROXY_PAGE =
  '<html>\r\n<head><title>502 Bad Gateway</title></head>\r\n<body>\r\n<center><h1>502 Bad Gateway</h1></center>\r\n<hr><center>nginx/1.27.3</center>\r\n</body>\r\n</html>\r\n';

interface Answer {
  readonly status: number;
  readonly contentType?: string;
  readonly headers?: Readonly<Record<string, string>>;
  readonly body?: string;
}

/** A problem document as the server writes one. */
function problem(status: number, title: string, code: string, detail: string, extra: Record<string, unknown> = {}): Answer {
  return {
    status,
    contentType: 'application/problem+json',
    headers: { 'X-Request-Id': REQUEST_ID },
    body: JSON.stringify({ type: 'about:blank', title, status, code, detail, requestId: REQUEST_ID, ...extra }),
  };
}

/** Answers the GETs of `match` with `answer` from now on. */
async function answerWith(page: Page, match: (url: URL) => boolean, answer: Answer): Promise<void> {
  await page.unroute(match);
  await page.route(match, (route: Route) =>
    route.request().method() === 'GET'
      ? route.fulfill({ status: answer.status, contentType: answer.contentType, headers: answer.headers, body: answer.body ?? '' })
      : route.fallback(),
  );
}

/** Loads the page again and waits for its notice; opens its Technical details where it has any. */
async function shown(page: Page, open: boolean): Promise<void> {
  await page.reload();
  const notice = page.locator('main [data-testid="error-notice"]');
  await expect(notice).toHaveCount(1);
  if (!open) return;
  const toggle = notice.locator('quader-technical-details button.inline-toggle');
  await toggle.click();
  await expect(toggle).toHaveAttribute('aria-expanded', 'true');
  await page.evaluate(() => Promise.all(document.getAnimations().map((a) => a.finished.catch(() => undefined))));
}

/**
 * The owner's case of 2026-11-25 first: the backend restarts behind the gateway, the session's read and the repository's read both
 * answer 502 without a body, and the page shows one notice that names both calls. Then a gateway's error page, a server that is not
 * ready with its Retry-After, an unknown code with the server's sentence, a refusal with field problems and a rate limit.
 */
export async function* failureStates(page: Page): AsyncGenerator<string> {
  await answerWith(page, isWhoami, { status: 502 });
  await answerWith(page, isRepository, { status: 502 });
  await shown(page, false);
  await expect(page.locator('main [data-testid="error-notice"] .call')).toHaveCount(2);
  yield '';
  await page.unroute(isWhoami);
  await answerWith(page, isRepository, {
    status: 502,
    contentType: 'text/html',
    headers: { Server: 'nginx/1.27.3' },
    body: PROXY_PAGE,
  });
  await shown(page, true);
  yield 'gateway-page';
  await answerWith(page, isRepository, {
    ...problem(503, 'Service Unavailable', 'unavailable', 'The replica drains for a restart; another replica takes the request.'),
    headers: { 'X-Request-Id': REQUEST_ID, 'Retry-After': '120' },
  });
  await shown(page, false);
  yield 'not-ready';
  await answerWith(
    page,
    isRepository,
    problem(
      500,
      'Internal Server Error',
      'upstream_index_inconsistent',
      'The repository index of web-frontend/npm-internal lists a version whose metadata the store does not hold; the next reconciliation pass repairs it.',
    ),
  );
  await shown(page, false);
  yield 'unknown-code';
  await answerWith(
    page,
    isRepository,
    problem(400, 'Bad Request', 'validation_failed', 'The request has invalid fields.', {
      errors: [{ pointer: '/fields/2', code: 'unknown_field', detail: 'the field upstreamCredentials is not readable by this caller' }],
    }),
  );
  await shown(page, true);
  yield 'validation';
  // The page sends the GET once more after Retry-After by itself (R-UI-042), so the notice stands a second later.
  await answerWith(page, isRepository, {
    ...problem(429, 'Too Many Requests', 'rate_limited', 'The read class of this session is exhausted.'),
    headers: { 'X-Request-Id': REQUEST_ID, 'Retry-After': '1' },
  });
  await shown(page, false);
  yield 'rate-limited';
  await page.unroute(isRepository);
}

/** The answers of the walk: the failures are what it renders. */
export const failureExpected = [
  {
    match: new RegExp(`^GET (/api/v1/auth/whoami|${repository}) answered (400|429|500|502|503)$`),
    why: 'the walk answers the session and the repository with each failure to render the notice',
  },
];

/**
 * Every notice of the walk is a state it renders; the empty one is the page's notice that the shell's notice of the same cause stands
 * for (Q-UI-1185), which shows nothing.
 */
export const failureNotices = [{ match: /^/, why: 'the failure states of the repository page, which the walk renders' }];
