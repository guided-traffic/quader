import { Component, signal } from '@angular/core';
import { TestBed } from '@angular/core/testing';
import { ApiError, FailedAnswer } from '../api/problem';
import { PageVisibility } from '../core/page-visibility';
import { settle } from '../testing/screen';
import { seriousViolations } from '../testing/axe';
import { ErrorNotice, retryWaitMs } from './error-notice';

const read = (path: string): Partial<FailedAnswer> => ({ call: { method: 'GET', path } });
const AT = new Date('2026-11-25T10:42:07Z');

/** A 502 of a gateway without a body, as the dev server's proxy answers while the backend restarts. */
function gateway(path = '/api/v1/projects/platform/repositories/npm-local', extra: Partial<FailedAnswer> = {}): ApiError {
  return new ApiError(502, null, null, null, { ...read(path), at: AT, ...extra });
}

@Component({
  imports: [ErrorNotice],
  template: `
    <main>
      @for (e of errors(); track $index) {
        <quader-error-notice [error]="e" (retry)="retried.push($index)" />
      }
      @if (unbound(); as e) {
        <quader-error-notice class="unbound" [error]="e" />
      }
    </main>
  `,
})
class Host {
  readonly errors = signal<readonly ApiError[]>([]);
  readonly unbound = signal<ApiError | null>(null);
  readonly retried: number[] = [];
}

const seen = signal(true);

async function show(errors: readonly ApiError[], unbound: ApiError | null = null) {
  TestBed.resetTestingModule();
  TestBed.configureTestingModule({ imports: [Host], providers: [{ provide: PageVisibility, useValue: { visible: seen.asReadonly() } }] });
  const fixture = TestBed.createComponent(Host);
  fixture.componentInstance.errors.set(errors);
  fixture.componentInstance.unbound.set(unbound);
  document.body.append(fixture.nativeElement as HTMLElement);
  await settle(fixture);
  const root = fixture.nativeElement as HTMLElement;
  const shown = (): HTMLElement[] => [...root.querySelectorAll<HTMLElement>('[data-testid="error-notice"]')];
  const facts = (notice: HTMLElement): Record<string, string[]> => {
    const out: Record<string, string[]> = {};
    let term = '';
    for (const el of notice.querySelectorAll('dl.failure-facts > *')) {
      if (el.tagName === 'DT') out[(term = el.textContent!.trim())] = [];
      else {
        const items = [...el.querySelectorAll('.call, .request')];
        out[term]!.push(...(items.length > 0 ? items.map(words) : [words(el)]));
      }
    }
    return out;
  };
  const buttons = (notice: HTMLElement): string[] => [...notice.querySelectorAll('.actions button')].map(words);
  return { fixture, root, shown, facts, buttons, host: fixture.componentInstance };
}

/** The text a reader sees of an element: without the names of icons and the buttons inside it. */
function words(el: Element): string {
  const copy = el.cloneNode(true) as Element;
  for (const icon of copy.querySelectorAll('.material-icons, quader-copy-button')) icon.remove();
  return copy.textContent!.replace(/\s+/g, ' ').trim();
}

describe('the error notice (R-UI-216)', () => {
  afterEach(() => {
    vi.useRealTimers();
    seen.set(true);
  });

  it('R-UI-216 a 502 without a body says what happened and shows the status, the call and the time; no empty row', async () => {
    const { shown, facts, buttons, root } = await show([gateway()]);
    const [notice] = shown();
    expect(notice!.querySelector('[role="alert"]')!.textContent!.trim()).toBe(
      'The server did not answer through the gateway in front of it. It may be restarting or overloaded.',
    );
    expect(notice!.textContent).not.toMatch(/^\s*HTTP 502/);
    expect(facts(notice!)).toEqual({
      Status: ['502 Bad Gateway'],
      Call: ['GET /api/v1/projects/platform/repositories/npm-local'],
      Time: [new Intl.DateTimeFormat('en-US', { dateStyle: 'medium', timeStyle: 'medium' }).format(AT)],
    });
    expect(notice!.querySelector('quader-technical-details')).toBeNull();
    expect(buttons(notice!)).toEqual(['Retry', 'Copy report']);
    expect(notice!.querySelector('.retry-note')!.textContent).toBe('This page tries again by itself in 3 seconds.');
    expect(await seriousViolations(root)).toEqual([]);
  });

  it('R-UI-216 a gateway\'s error page is folded as text, never as markup, with its headers and the note that it was cut', async () => {
    const page = '<html><body><h1>502 Bad Gateway</h1><script>alert(1)</script></body></html>';
    const { shown } = await show([
      gateway(undefined, { contentType: 'text/html', body: page, bodyLength: 9_000, headers: [['content-type', 'text/html'], ['server', 'edge']] }),
    ]);
    const details = shown()[0]!.querySelector('quader-technical-details')!;
    expect(details.querySelector('h1, script')).toBeNull();
    const pre = [...details.querySelectorAll('pre')].map((p) => p.textContent);
    expect(pre).toEqual(['content-type: text/html\nserver: edge', page]);
    expect(details.textContent).toContain(`Showing the first ${page.length} of 9,000 characters.`);
    expect(details.querySelector('pre')!.getAttribute('translate')).toBe('no');
  });

  it('R-UI-216 a 503 with a problem document shows its code, the server\'s sentence as English, Retry-After and the request ID with Copy', async () => {
    const e = new ApiError(
      503,
      { code: 'unavailable', title: 'Service unavailable', detail: 'The replica drains for a restart.', requestId: 'rid-503', instance: '/i/1' },
      'rid-503',
      30,
      { ...read('/api/v1/system/info'), at: AT, inRow: 2 },
    );
    e.afterRetry = true;
    const { shown, facts } = await show([e]);
    const notice = shown()[0]!;
    const f = facts(notice);
    expect(f['Code']).toEqual(['unavailable']);
    expect(f["Server's message"]).toEqual(['The replica drains for a restart.']);
    expect(f['Retry after']).toEqual(['30 seconds']);
    expect(f['Request ID']).toEqual(['rid-503']);
    expect(f['Attempts']).toEqual(['Sent twice: the page sent the call again by itself after the first failure.']);
    expect(f['Failed in a row']).toEqual(['2 times']);
    const message = [...notice.querySelectorAll('dd')].find((d) => d.textContent === 'The replica drains for a restart.')!;
    expect(message.getAttribute('lang')).toBe('en');
    expect(message.getAttribute('translate')).toBe('no');
    expect(notice.querySelector('[aria-label="Copy request ID"]')).not.toBeNull();
    expect(notice.querySelector('.retry-note')!.textContent).toBe('This page tries again by itself in 30 seconds.');
  });

  it('R-UI-216 a validation answer lists its field problems under Technical details', async () => {
    const e = new ApiError(
      422,
      { code: 'validation_failed', errors: [{ pointer: '/key', code: 'pattern', detail: 'must match ^[a-z]+$' }] },
      'rid-422',
      null,
      { call: { method: 'POST', path: '/api/v1/projects/p/repositories' } },
    );
    const { shown, buttons } = await show([e]);
    const details = shown()[0]!.querySelector('quader-technical-details')!;
    expect(details.querySelector('ul.fields li')!.textContent!.replace(/\s+/g, ' ').trim()).toBe('/key pattern must match ^[a-z]+$');
    expect(buttons(shown()[0]!)).toEqual(['Copy report']);
  });

  it('R-UI-216 calls of one view that fail the same way are one notice naming each call; its Retry reads every view', async () => {
    const { shown, facts, host, fixture } = await show([gateway('/api/v1/a'), gateway('/api/v1/b')]);
    expect(shown().length).toBe(1);
    expect(facts(shown()[0]!)['Calls']).toEqual(['GET /api/v1/a', 'GET /api/v1/b']);
    (shown()[0]!.querySelector('.actions button') as HTMLButtonElement).click();
    await settle(fixture);
    expect(host.retried).toEqual([0, 1]);
  });

  it('R-UI-216 failures of different causes stay two notices', async () => {
    const { shown } = await show([gateway('/api/v1/a'), new ApiError(404, { code: 'not_found', detail: 'No b.' }, null, null, read('/api/v1/b'))]);
    expect(shown().length).toBe(2);
  });

  it('R-UI-216 Copy report copies the sentence, every fact and the folded part as text', async () => {
    const writeText = vi.fn().mockResolvedValue(undefined);
    Object.defineProperty(navigator, 'clipboard', { value: { writeText }, configurable: true });
    const { shown, fixture } = await show([gateway(undefined, { body: 'upstream connect error', contentType: 'text/plain', headers: [['server', 'edge']] })]);
    const copy = [...shown()[0]!.querySelectorAll('button')].find((b) => b.textContent!.includes('Copy report'))!;
    copy.click();
    await settle(fixture);
    const report = writeText.mock.calls[0]![0] as string;
    expect(report).toContain('The server did not answer through the gateway in front of it.');
    expect(report).toContain('Status: 502 Bad Gateway');
    expect(report).toContain('Call: GET /api/v1/projects/platform/repositories/npm-local');
    expect(report).toContain('(2026-11-25T10:42:07.000Z)');
    expect(report).toContain('Response headers:\n  server: edge');
    expect(report).toContain('Body of the answer:\nupstream connect error');
  });

  it('R-UI-216 a failed read reads its view again by itself after 3 s, 6 s … while the window is seen; a write never', async () => {
    vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] });
    const write = new ApiError(503, null, null, null, { call: { method: 'POST', path: '/api/v1/x/rescan' } });
    const { host, fixture, shown, buttons } = await show([gateway('/api/v1/a'), write]);
    expect(buttons(shown()[1]!)).toEqual(['Retry', 'Copy report']);
    expect(shown()[1]!.querySelector('.retry-note')).toBeNull();
    vi.advanceTimersByTime(2_999);
    expect(host.retried).toEqual([]);
    vi.advanceTimersByTime(1);
    expect(host.retried).toEqual([0]);
    await settle(fixture);
    expect(shown()[0]!.querySelector('.retry-note')!.textContent).toBe('Trying again.');
    vi.advanceTimersByTime(60_000);
    expect(host.retried).toEqual([0]);
  });

  it('R-UI-216 no read again while the window is hidden; at once when it is seen again', async () => {
    vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] });
    seen.set(false);
    const { host, fixture } = await show([gateway()]);
    vi.advanceTimersByTime(60_000);
    expect(host.retried).toEqual([]);
    seen.set(true);
    await settle(fixture);
    vi.advanceTimersByTime(0);
    expect(host.retried).toEqual([0]);
  });

  it('R-UI-216 the wait doubles with the failures in a row up to five minutes; a refusal is not read again by itself', async () => {
    expect(retryWaitMs(gateway(undefined, { inRow: 1 }))).toBe(3_000);
    expect(retryWaitMs(gateway(undefined, { inRow: 3 }))).toBe(12_000);
    expect(retryWaitMs(gateway(undefined, { inRow: 30 }))).toBe(300_000);
    vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] });
    const refused = new ApiError(403, { code: 'forbidden' }, null, null, read('/api/v1/a'));
    const { host, shown, buttons } = await show([refused]);
    expect(buttons(shown()[0]!)).toEqual(['Retry', 'Copy report']);
    expect(shown()[0]!.querySelector('.retry-note')).toBeNull();
    vi.advanceTimersByTime(600_000);
    expect(host.retried).toEqual([]);
  });

  it('R-UI-216 a notice whose screen bound no retry offers none and reads nothing again', async () => {
    const { root, buttons } = await show([], gateway());
    const notice = root.querySelector<HTMLElement>('.unbound [data-testid="error-notice"]')!;
    expect(buttons(notice)).toEqual(['Copy report']);
    expect(notice.querySelector('.retry-note')).toBeNull();
  });
});
