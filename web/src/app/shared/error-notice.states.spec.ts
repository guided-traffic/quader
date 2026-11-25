import { Component, input } from '@angular/core';
import { describe, it, inject as provided } from 'vitest';
import { ApiError, FailedAnswer } from '../api/problem';
import { REQUEST_ID, expectState, openInPage, stable } from '../testing/states';
import { ErrorNotice } from './error-notice';

/**
 * The states of the shared error notice in a browser (R-UI-216, R-UI-173, R-UI-175, R-UI-215): each failure a screen meets, with the
 * long values a real one has (a deep path with its query, a gateway's error page, a sentence of the server), measured by the render
 * set's checks at 390 and 1440 px. The German words and the screens around the notice are the render set's walk `failureStates`
 * (web/e2e/renders/failures.ts); `UI_STATES_PICTURES=1` adds a picture of each state in the dark scheme.
 */
@Component({
  selector: 'quader-notice-host',
  imports: [ErrorNotice],
  template: `
    @for (e of errors(); track $index) {
      <quader-error-notice [error]="e" (retry)="retried()" />
    }
  `,
})
class NoticeHost {
  readonly errors = input.required<readonly ApiError[]>();
  /** A screen's read of its view again: nothing to read here. */
  protected retried(): void {
    // The state is measured as it opens; a read again by itself would only change the retry note.
  }
}

const PATH = '/api/v1/projects/platform-engineering-release-candidates/repositories/kubernetes-infrastructure-npm-remote';
const AT = new Date('2026-11-25T10:42:07Z');
const read = (path = PATH, extra: Partial<FailedAnswer> = {}): Partial<FailedAnswer> => ({ call: { method: 'GET', path }, at: AT, ...extra });

/** A proxy's error page, as nginx sends it: markup, shown as text. */
const PROXY_PAGE =
  '<html>\r\n<head><title>502 Bad Gateway</title></head>\r\n<body>\r\n<center><h1>502 Bad Gateway</h1></center>\r\n<hr><center>nginx/1.27.3</center>\r\n</body>\r\n</html>\r\n' +
  '<!-- a padding to disable MSIE and Chrome friendly error page -->\r\n'.repeat(6);

const states: Readonly<Record<string, () => readonly ApiError[]>> = {
  // The owner's case: the shell's session read and the page's read failed the same way while the backend restarted.
  'notice-502-bare': () => [
    new ApiError(502, null, null, null, read('/api/v1/auth/whoami')),
    new ApiError(502, null, null, null, read()),
  ],
  'notice-502-page': () => [
    new ApiError(502, null, null, null, read(`${PATH}/packages?q=%40platform-engineering%2Fkubernetes-infrastructure&sort=-updatedAt&limit=50`, {
      contentType: 'text/html',
      body: PROXY_PAGE,
      headers: [['content-type', 'text/html'], ['content-length', String(PROXY_PAGE.length)], ['server', 'nginx/1.27.3'], ['date', 'Wed, 25 Nov 2026 10:42:07 GMT']],
    })),
  ],
  'notice-offline': () => {
    const e = new ApiError(0, null, null, null, read(PATH, { inRow: 3 }));
    e.afterRetry = true;
    return [e];
  },
  'notice-503-retry-after': () => [
    new ApiError(
      503,
      { type: 'about:blank', title: 'Service Unavailable', status: 503, code: 'unavailable', detail: 'The replica drains for a restart; another replica takes the request.', requestId: REQUEST_ID },
      REQUEST_ID,
      120,
      read(PATH, { contentType: 'application/problem+json', headers: [['content-type', 'application/problem+json'], ['retry-after', '120'], ['x-request-id', REQUEST_ID]] }),
    ),
  ],
  'notice-unknown-code': () => [
    new ApiError(
      500,
      {
        code: 'upstream_index_inconsistent',
        status: 500,
        detail: 'The repository index of platform-engineering-release-candidates/kubernetes-infrastructure-npm-remote lists a version whose metadata the store does not hold; the next reconciliation pass repairs it.',
        requestId: REQUEST_ID,
        instance: '/api/v1/requests/7f3c9a2e5b8d4f1a6c0e9b3d7a5f2c8e',
      },
      REQUEST_ID,
      null,
      read(),
    ),
  ],
  'notice-validation': () => [
    new ApiError(
      422,
      {
        code: 'validation_failed',
        status: 422,
        detail: 'The request has invalid fields.',
        requestId: REQUEST_ID,
        errors: [
          { pointer: '/remote/url', code: 'invalid_url', detail: 'must be an absolute https URL without user information' },
          { pointer: '/includePatterns/3', code: 'invalid_pattern', detail: 'the pattern @platform-engineering/kubernetes-infrastructure-** has an unclosed class' },
        ],
      },
      REQUEST_ID,
      null,
      { call: { method: 'PATCH', path: PATH }, at: AT },
    ),
  ],
  'notice-429': () => [
    new ApiError(429, { code: 'rate_limited', status: 429, detail: 'The read class of this session is exhausted.', requestId: REQUEST_ID }, REQUEST_ID, 7, read()),
  ],
};

/** Opens a state in the page, with its Technical details open where it has any. */
async function open(errors: readonly ApiError[], dark: boolean): Promise<void> {
  if (dark) document.documentElement.setAttribute('data-theme', 'dark');
  else document.documentElement.removeAttribute('data-theme');
  await openInPage(NoticeHost, { errors });
  for (const toggle of Array.from(document.querySelectorAll<HTMLButtonElement>('quader-technical-details button.inline-toggle'))) toggle.click();
  await stable();
}

describe('the error notice in a browser (R-UI-216, R-UI-173, R-UI-175, R-UI-215)', () => {
  for (const [name, errors] of Object.entries(states)) {
    it(name, async () => {
      await expectState(name, () => open(errors(), false));
      // A picture in the dark scheme too, when pictures are asked for: the layout is the light one's, measured above.
      if (provided('picturesDir')) await expectState(`${name}-dark`, () => open(errors(), true));
      document.documentElement.removeAttribute('data-theme');
    });
  }
});
