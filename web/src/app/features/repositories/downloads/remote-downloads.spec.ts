import { TestBed } from '@angular/core/testing';
import { Router } from '@angular/router';
import { vi } from 'vitest';
import { ApiError } from '../../../api/problem';
import { RemoteDownloadsApi } from '../../../api/remote-downloads-api';
import { RepositoriesApi } from '../../../api/repositories-api';
import { SessionApi } from '../../../api/session-api';
import { FakeRepositoriesApi, PACKAGE_FORMAT } from '../../../api/testing/fakes-catalog';
import { FakeSessionApi } from '../../../api/testing/fakes';
import { FakeRemoteDownloadsApi, downloadBulk, downloadFailure, downloadLimit, downloadRow, downloadSync } from '../../../api/testing/fakes-remote-downloads';
import { LIVE_SLOW_MS } from '../../../core/live-refresh';
import { Notifier } from '../../../core/notifier';
import { expectAccessible, renderScreen, settle, text } from '../../../testing/screen';
import { RepositoryContent } from '../content/repository-content';
import { RemoteDownloads, DOWNLOADS_REFRESH_MS } from './remote-downloads';

const SELF = '00000000-0000-0000-0000-000000000001';

function setVisibility(state: 'visible' | 'hidden'): void {
  Object.defineProperty(document, 'visibilityState', { configurable: true, get: () => state });
  document.dispatchEvent(new Event('visibilitychange'));
}
const REMOTE = { key: 'npmjs', project: 'web', class: 'remote', format: 'npm', description: '', remote: { url: 'https://registry.npmjs.org', cacheFillAction: 'read' } };
const LOCAL = { key: 'npm-local', project: 'web', class: 'local', format: 'npm', description: '' };

/** The queue of the cases: one row in each state and of each origin the tab words differently. */
function queue(fake: FakeRemoteDownloadsApi): void {
  fake.rows = [
    downloadRow({ id: 'r-run', name: '@acme/a', version: '1.2.0', origin: 'bulk', bulk: 'b-1', state: 'running', attempts: 1, files: 1, stored: 1, bytes: 2_202_009 }),
    downloadRow({
      id: 'r-wait',
      name: 'lodash',
      version: '4.17.21',
      origin: 'person',
      startedBy: { id: SELF, name: 'Alice' },
      state: 'waiting',
      attempts: 2,
      nextAttemptAt: '2099-01-01T00:00:00Z',
      failure: downloadFailure({ class: 'upstream_rate_limited', status: 429, attempt: 2, retryAfter: 120 }),
    }),
    downloadRow({ id: 'r-fail', name: 'left-pad', version: '1.3.0', origin: 'client', occurrences: 14, state: 'failed', attempts: 5, failure: downloadFailure() }),
    downloadRow({
      id: 'r-tls',
      name: 'internal-tool',
      version: '2.0.0',
      origin: 'person',
      startedBy: { id: null, name: null },
      state: 'failed',
      attempts: 1,
      failure: downloadFailure({
        class: 'tls',
        step: 'tls',
        code: 'tls_untrusted',
        status: null,
        excerpt: null,
        address: 'https://registry.example.com:8443/internal-tool/-/internal-tool-2.0.0.tgz',
        detail: 'The TLS certificate of registry.example.com:8443 is not trusted by this installation; add its CA to outbound.tls.ca_file.',
        attempt: 1,
      }),
    }),
    downloadRow({ id: 'r-done', name: 'react', version: '19.0.0', origin: 'probe', state: 'done', outcome: 'completed', files: 1, stored: 1, bytes: 1024 }),
  ];
  fake.bulks = [downloadBulk({ id: 'b-1', subject: '@acme/a' })];
}

async function open(
  options: {
    manage?: boolean;
    url?: string;
    rows?: (fake: FakeRemoteDownloadsApi) => void;
    upstreamName?: string;
    repository?: string;
    capabilities?: readonly string[];
  } = {},
) {
  const r = await renderScreen(RemoteDownloads, {
    inputs: { repository: options.repository ?? 'web/npmjs', ...(options.upstreamName ? { upstreamName: options.upstreamName } : {}) },
    setup: async (get) => {
      const session = get(SessionApi) as FakeSessionApi;
      session.meResult = { capabilities: [...(options.capabilities ?? [])] as never, roles: [] };
      session.allow = (c) => c.action !== 'manage' || options.manage === true;
      (options.rows ?? queue)(get(RemoteDownloadsApi) as FakeRemoteDownloadsApi);
      await get(Router).navigateByUrl(options.url ?? '/projects/web/repositories/npmjs/downloads');
    },
  });
  await settle(r.fixture);
  return { ...r, fake: r.get(RemoteDownloadsApi) as FakeRemoteDownloadsApi };
}

function rowOf(root: HTMLElement, name: string): HTMLTableRowElement {
  return [...root.querySelectorAll<HTMLTableRowElement>('[data-testid="download-rows"] tbody tr:not(.expansion)')].find((tr) =>
    tr.querySelector('th')?.textContent?.includes(name),
  )!;
}

/** The bulk the owner started on 2026-11-24: its expansion failed before it queued a row (the catalogue host was refused). */
function refusedBulk() {
  return downloadBulk({
    id: 'b-hub',
    subject: 'guidedtraffic/valkey-operator',
    state: 'failed',
    versionsFound: 0,
    versionsQueued: 0,
    rows: {},
    startedBy: { id: SELF, name: 'Alice' },
    finishedAt: '2026-11-24T10:05:03Z',
    failure: downloadFailure({
      class: 'upstream_unexpected',
      step: 'status',
      code: 'foreign_host_denied',
      file: null,
      address: null,
      target: 'https://hub.docker.com/v2/namespaces/guidedtraffic/repositories/valkey-operator/tags',
      status: null,
      excerpt: null,
      attempt: 0,
      detail:
        'The download failed at the step status. The upstream document points to the host hub.docker.com, which is neither the upstream of the repository upstream/docker-hub nor one of its extraHosts.',
    }),
  });
}

describe('The tab Downloads of a remote repository', () => {
  it('R-CUR-018 R-CUR-011 lists what waits, runs, failed and finished with its origin, its attempt, its progress and its failure in one line', async () => {
    const { root } = await open();
    const states = text(root.querySelector('.state-filter')!);
    expect(states).toContain('Waiting 1');
    expect(states).toContain('Running 1');
    expect(states).toContain('Failed 2');
    expect(states).toContain('Done 1');
    const running = rowOf(root, '@acme/a');
    expect(text(running)).toContain('Running');
    expect(text(running)).toContain('Files: 1 of 1 · 2.1 MiB');
    // A bulk's row leads to its bulk.
    expect(running.querySelector('a')?.textContent?.trim()).toBe('Bulk');
    const waiting = rowOf(root, 'lodash');
    expect(text(waiting)).toContain('You');
    expect(text(waiting)).toContain('Attempt 2 of 5');
    expect(text(waiting)).toContain('next');
    expect(text(waiting)).toContain('Rate limited by the upstream · HTTP 429 · registry.npmjs.org');
    const client = rowOf(root, 'left-pad');
    expect(text(client)).toContain('Client');
    expect(text(client)).toContain('14 times');
    expect(text(client)).toContain('Upstream error · HTTP 503 · registry.npmjs.org');
    // Without a status the engine's code stands in the line, and the host keeps its port.
    const tls = rowOf(root, 'internal-tool');
    expect(text(tls)).toContain('TLS · tls_untrusted · registry.example.com:8443');
    // A person a reader may not see is named as a person, not by a name.
    expect(text(tls)).toContain('A person');
    expect(text(rowOf(root, 'react'))).toContain('Probe');
    await expectAccessible(root);
  });

  it('R-CUR-018 R-CUR-011 a failed row opens to its whole failure: step, code, address without its query, status, the excerpt and the sentence', async () => {
    const { root, fixture } = await open();
    const row = rowOf(root, 'left-pad');
    const opener = row.querySelector<HTMLButtonElement>('button[aria-expanded]')!;
    expect(opener.getAttribute('aria-label')).toBe('Details of left-pad 1.3.0');
    opener.click();
    await settle(fixture);
    const detail = root.querySelector<HTMLElement>('#dl-r-fail')!;
    expect(detail.classList).toContain('open');
    const words = text(detail);
    expect(words).toContain('Failure of attempt 5');
    expect(words).toContain("The answer's status status");
    expect(words).toContain('upstream_error');
    // Q-CUR-134: the class's and the step's value each stand in a muted line of code below the words, not beside them.
    const values = [...detail.querySelectorAll('dd > code.secondary')].map((c) => c.textContent);
    expect(values).toEqual(['upstream_error', 'status']);
    expect(words).toContain('https://registry.npmjs.org/left-pad/-/left-pad-1.3.0.tgz');
    expect(words).toContain('(shown without its query, which can carry a credential)');
    expect(words).toContain('HTTP 503');
    expect(detail.querySelector('pre.excerpt')?.textContent).toBe('<html><body>Service Unavailable</body></html>');
    // The screen's sentence from the code; the server's English one folded under it (Q-CUR-169).
    expect(detail.querySelector('.failure-detail')?.textContent).toBe('The upstream answered with an error of its own. A later attempt may succeed.');
    const engine = detail.querySelector<HTMLElement>('quader-technical-details [data-testid="technical-details"]')!;
    expect(engine.lang).toBe('en');
    expect(engine.textContent).toContain('answered 503 Service Unavailable');
    await expectAccessible(root);
  });

  it('R-CUR-005 R-CUR-012 a holder of manage retries a failed row, cancels a waiting one and retries every failed row of the class the address names', async () => {
    const { root, fixture, fake } = await open({ manage: true, url: '/projects/web/repositories/npmjs/downloads?failure=tls' });
    expect(fake.listCalls.at(-1)?.filters).toMatchObject({ class: 'tls' });
    const notifier = TestBed.inject(Notifier);
    const status = vi.spyOn(notifier, 'status');
    // The class filter leaves the TLS row; the cases below need every row.
    fake.listCalls.length = 0;
    await TestBed.inject(Router).navigateByUrl('/projects/web/repositories/npmjs/downloads');
    await settle(fixture);
    const failed = rowOf(root, 'left-pad');
    expect(failed.querySelector('td.actions button')?.textContent).toContain('Retry');
    failed.querySelector<HTMLButtonElement>('td.actions button')!.click();
    await settle(fixture);
    expect(fake.calls).toContain('retry web/npmjs r-fail');
    expect(status).toHaveBeenCalledWith('left-pad 1.3.0 waits for its download again.');
    rowOf(root, 'lodash').querySelector<HTMLButtonElement>('td.actions button')!.click();
    await settle(fixture);
    expect(fake.calls).toContain('cancel web/npmjs r-wait');
    await TestBed.inject(Router).navigateByUrl('/projects/web/repositories/npmjs/downloads?failure=tls');
    await settle(fixture);
    const all = [...root.querySelectorAll<HTMLButtonElement>('.head-end button')].find((b) => b.textContent?.includes('Retry all failed'))!;
    all.click();
    await settle(fixture);
    expect(fake.calls).toContain('retry-failed web/npmjs tls -');
    expect(status).toHaveBeenCalledWith('Failed downloads that wait again: 1.');
  });

  it('R-CUR-005 a reader retries and cancels only the rows they started and the rows of clients and probes they may fill, and never every failed row', async () => {
    const { root } = await open({ manage: false });
    expect(text(root.querySelector('.head-end')!)).not.toContain('Retry all failed');
    expect(rowOf(root, 'lodash').querySelector('td.actions button')?.textContent).toContain('Cancel');
    expect(rowOf(root, 'left-pad').querySelector('td.actions button')?.textContent).toContain('Retry');
    // Another person's row and a bulk's row offer nothing to a reader.
    expect(rowOf(root, 'internal-tool').querySelector('td.actions button')).toBeNull();
    expect(rowOf(root, '@acme/a').querySelector('td.actions button')).toBeNull();
  });

  it('R-CUR-012 the bulk of the address stands above its rows with its counters, its bounds and Cancel bulk for a holder of manage', async () => {
    const { root, fixture, fake } = await open({ manage: true, url: '/projects/web/repositories/npmjs/downloads?bulk=b-1' });
    expect(fake.listCalls.at(-1)?.filters).toMatchObject({ bulk: 'b-1' });
    const bulk = root.querySelector<HTMLElement>('[data-testid="download-bulk"]')!;
    expect(text(bulk)).toContain('Every version of @acme/a');
    expect(text(bulk)).toContain('3 of 3 versions found are queued');
    expect(text(bulk)).toContain('At most 100 versions and 100.0 GiB, pre-releases included');
    [...bulk.querySelectorAll<HTMLButtonElement>('button')].find((b) => b.textContent?.includes('Cancel bulk'))!.click();
    await settle(fixture);
    expect(fake.calls).toContain('cancel-bulk web/npmjs b-1');
    await expectAccessible(root);
  });

  it('R-CUR-018 R-CUR-012 a bulk that failed before its first row stands over the rows with its cause in words, and a holder of manage starts it again', async () => {
    const { root, fixture, fake } = await open({
      manage: true,
      upstreamName: 'Docker Hub',
      rows: (f) => {
        f.rows = [];
        f.bulks = [downloadBulk({ id: 'b-old', subject: '@acme/a', state: 'done', rows: { done: 3 } }), refusedBulk()];
      },
    });
    const bulks = root.querySelector<HTMLElement>('[data-testid="download-bulks"]')!;
    expect(text(bulks)).toContain('Bulk downloads');
    const [failed, done] = [...bulks.querySelectorAll<HTMLTableRowElement>('tbody tr')] as [HTMLTableRowElement, HTMLTableRowElement];
    expect(text(failed)).toContain('Every version of guidedtraffic/valkey-operator');
    expect(text(failed)).toContain('Started by you');
    expect(text(failed)).not.toContain('Started by You');
    expect(text(failed)).toContain('Failed');
    expect(text(failed)).not.toContain('0 of 0');
    expect(text(failed)).toContain("Docker Hub's version list could not be read");
    expect(text(failed)).toContain('Unexpected answer of the upstream: The upstream points to hub.docker.com, which is neither the upstream nor');
    expect(text(failed)).toContain('hub.docker.com');
    // The rows below say that there is none, and the bulk's title leads to the bulk.
    expect(text(root.querySelector('[data-testid="download-rows"]')!)).toContain('There is no download yet.');
    expect(failed.querySelector('a')?.getAttribute('href')).toContain('bulk=b-hub');
    // A done bulk offers nothing; the failed one starts again with its level, its subject and its bounds.
    expect(done.querySelector('button')).toBeNull();
    expect(text(done)).toContain('3 of 3 queued');
    const status = vi.spyOn(TestBed.inject(Notifier), 'status');
    failed.querySelector<HTMLButtonElement>('button')!.click();
    await settle(fixture);
    expect(fake.starts.at(-1)).toEqual({
      level: 'package',
      name: 'guidedtraffic/valkey-operator',
      maxVersions: 100,
      maxBytes: 107374182400,
      includePrereleases: true,
    });
    expect(status).toHaveBeenCalledWith(
      'The bulk download of guidedtraffic/valkey-operator starts again: Quader reads its versions from the upstream.',
    );
    await expectAccessible(root);
  });

  it('R-CUR-018 R-CUR-012 a reader sees a failed bulk with the upstream named plainly and cannot start it again; no bulk, no list', async () => {
    const { root } = await open({ rows: (f) => ((f.rows = []), (f.bulks = [refusedBulk()])) });
    const bulks = root.querySelector<HTMLElement>('[data-testid="download-bulks"]')!;
    expect(text(bulks)).toContain("The upstream's version list could not be read");
    expect(bulks.querySelector('button')).toBeNull();
    const none = await open({ rows: (f) => ((f.rows = []), (f.bulks = [])) });
    expect(none.root.querySelector('[data-testid="download-bulks"]')).toBeNull();
  });

  it('R-CUR-018 R-CUR-012 the bulk the address names says what failed and starts again; the list of bulks gives way to it', async () => {
    const { root, fixture, fake } = await open({
      manage: true,
      url: '/projects/web/repositories/npmjs/downloads?bulk=b-hub',
      rows: (f) => ((f.rows = []), (f.bulks = [refusedBulk()])),
    });
    expect(root.querySelector('[data-testid="download-bulks"]')).toBeNull();
    const bulk = root.querySelector<HTMLElement>('[data-testid="download-bulk"]')!;
    expect(text(bulk)).toContain("The upstream's version list could not be read");
    expect(text(bulk)).toContain(
      "Unexpected answer of the upstream: The upstream points to hub.docker.com, which is neither the upstream nor one of the repository's further hosts",
    );
    // Nothing found yet is the list's dash, never "0 of 0".
    expect(text(bulk)).not.toContain('0 of 0');
    expect(text(bulk)).toContain('Versions found');
    expect(bulk.querySelector('.bulk-facts quader-no-value')).not.toBeNull();
    // The server's English sentence and the code fold under the screen's sentence, marked as English.
    const technical = bulk.querySelector<HTMLElement>('quader-technical-details')!;
    expect(technical.querySelector('button[aria-expanded="false"]')?.textContent).toContain('Technical details');
    const engine = technical.querySelector<HTMLElement>('[data-testid="technical-details"]')!;
    expect(engine.lang).toBe('en');
    expect(engine.textContent).toContain('The upstream document points to the host hub.docker.com');
    expect(engine.textContent).toContain('foreign_host_denied');
    await expectAccessible(root);
    [...bulk.querySelectorAll<HTMLButtonElement>('button')].find((b) => b.textContent?.includes('Start again'))!.click();
    await settle(fixture);
    await settle(fixture);
    expect(fake.starts.at(-1)).toMatchObject({ level: 'package', name: 'guidedtraffic/valkey-operator' });
    // The new bulk is listed; the address no longer names the failed one.
    expect(TestBed.inject(Router).url).toBe('/projects/web/repositories/npmjs/downloads');
  });

  it('R-CUR-018 R-CUR-012 the tab reads the bulks again while one expands, and shows its failure without a reload', async () => {
    vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] });
    try {
      const expanding = downloadBulk({ id: 'b-hub', subject: 'guidedtraffic/valkey-operator', state: 'expanding', versionsFound: 0, versionsQueued: 0, rows: {} });
      const { root, fixture, fake } = await open({ rows: (f) => ((f.rows = []), (f.bulks = [expanding])) });
      expect(text(root.querySelector('[data-testid="download-bulks"]')!)).toContain('Finding versions');
      fake.bulks = [refusedBulk()];
      vi.advanceTimersByTime(DOWNLOADS_REFRESH_MS);
      await settle(fixture);
      expect(text(root.querySelector('[data-testid="download-bulks"]')!)).toContain("The upstream's version list could not be read");
      // A failed bulk is no reason to read again.
      const reads = fake.calls.filter((c) => c.startsWith('bulks ')).length;
      vi.advanceTimersByTime(DOWNLOADS_REFRESH_MS * 3);
      await settle(fixture);
      expect(fake.calls.filter((c) => c.startsWith('bulks ')).length).toBe(reads);
    } finally {
      vi.useRealTimers();
    }
  });

  it('R-CUR-018 R-UI-211 the name of the address shows the rows of that package, and Show every download clears it', async () => {
    const { root, fixture, fake } = await open({ url: '/projects/web/repositories/npmjs/downloads?name=left-pad' });
    expect(fake.listCalls.at(-1)?.filters).toMatchObject({ name: 'left-pad' });
    const line = root.querySelector<HTMLElement>('[data-testid="download-name-filter"]')!;
    expect(text(line)).toContain('Only the downloads of left-pad.');
    line.querySelector<HTMLButtonElement>('button')!.click();
    await settle(fixture);
    await settle(fixture);
    expect(TestBed.inject(Router).url).toBe('/projects/web/repositories/npmjs/downloads');
    expect(fake.listCalls.at(-1)?.filters.name).toBeUndefined();
    expect(root.querySelector('[data-testid="download-name-filter"]')).toBeNull();
  });

  it('R-CUR-018 the empty queue says so, and a filter that matches nothing says that nothing matches', async () => {
    const { root, fixture } = await open({ rows: (f) => (f.rows = []) });
    expect(text(root)).toContain('There is no download yet.');
    await TestBed.inject(Router).navigateByUrl('/projects/web/repositories/npmjs/downloads?state=failed');
    await settle(fixture);
    expect(text(root)).toContain('No download matches.');
  });

  it('R-CUR-018 the tab reads the queue every five seconds while a row waits or runs, and Pause automatic refresh stops it (WCAG 2.2.2)', async () => {
    vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] });
    try {
      const { root, fixture, fake } = await open();
      const calls = fake.listCalls.length;
      vi.advanceTimersByTime(DOWNLOADS_REFRESH_MS);
      await settle(fixture);
      expect(fake.listCalls.length).toBe(calls + 1);
      const pause = root.querySelector<HTMLButtonElement>('.head-end button[aria-pressed]')!;
      expect(pause.textContent).toContain('Pause automatic refresh');
      pause.click();
      await settle(fixture);
      expect(pause.getAttribute('aria-pressed')).toBe('true');
      vi.advanceTimersByTime(DOWNLOADS_REFRESH_MS * 6);
      await settle(fixture);
      expect(fake.listCalls.length).toBe(calls + 1);
    } finally {
      vi.useRealTimers();
    }
  });

  it('R-UI-213 a queue where nothing waits or runs is read every 30 s, so that a bulk another person starts shows without a reload', async () => {
    vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] });
    try {
      const { root, fixture, fake } = await open({ rows: (f) => ((f.rows = [downloadRow({ state: 'done', outcome: 'completed' })]), (f.bulks = [])) });
      const calls = fake.listCalls.length;
      vi.advanceTimersByTime(DOWNLOADS_REFRESH_MS * 3);
      await settle(fixture);
      expect(fake.listCalls.length).toBe(calls);
      fake.bulks = [downloadBulk({ id: 'b-new', subject: 'guidedtraffic/s3-encryption-proxy' })];
      fake.rows = [...fake.rows, downloadRow({ id: 'r-new', name: 'guidedtraffic/s3-encryption-proxy', version: '3.8.50', origin: 'bulk', bulk: 'b-new' })];
      vi.advanceTimersByTime(LIVE_SLOW_MS);
      await settle(fixture);
      expect(fake.listCalls.length).toBe(calls + 1);
      expect(text(root.querySelector('[data-testid="download-bulks"]')!)).toContain('guidedtraffic/s3-encryption-proxy');
      expect(text(rowOf(root, 'guidedtraffic/s3-encryption-proxy'))).toContain('Waiting');
    } finally {
      vi.useRealTimers();
    }
  });

  it('R-UI-213 a hidden window is not read, and is read at once when it is seen again; a failed read keeps the rows', async () => {
    vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] });
    try {
      const { root, fixture, fake } = await open();
      const calls = fake.listCalls.length;
      setVisibility('hidden');
      vi.advanceTimersByTime(DOWNLOADS_REFRESH_MS * 4);
      await settle(fixture);
      expect(fake.listCalls.length).toBe(calls);
      fake.rows = fake.rows.map((r) => (r.id === 'r-run' ? { ...r, state: 'done' as const, outcome: 'completed' as const } : r));
      setVisibility('visible');
      await settle(fixture);
      expect(fake.listCalls.length).toBe(calls + 1);
      expect(text(rowOf(root, '@acme/a'))).toContain('Done');
      fake.listError = new ApiError(503, { code: 'unavailable', detail: 'down' }, 'rid-503');
      vi.advanceTimersByTime(DOWNLOADS_REFRESH_MS);
      await settle(fixture);
      expect(rowOf(root, 'lodash')).toBeTruthy();
      expect(root.querySelector('[data-testid="download-rows"] quader-error-notice')).toBeNull();
    } finally {
      setVisibility('visible');
      vi.useRealTimers();
    }
  });

  /** The owner's queue of 2026-11-25: Docker Hub's limit holds 206 rows of a bulk, 105 are done, nothing runs. */
  function heldQueue(f: FakeRemoteDownloadsApi): void {
    f.rows = [
      downloadRow({ id: 'r-h1', name: 'guidedtraffic/s3-encryption-proxy', version: '3.8.50', origin: 'bulk', bulk: 'b-hub' }),
      downloadRow({ id: 'r-h2', name: 'guidedtraffic/s3-encryption-proxy', version: '3.8.49', origin: 'bulk', bulk: 'b-hub', state: 'done', outcome: 'completed' }),
    ];
    f.bulks = [downloadBulk({ id: 'b-hub', subject: 'guidedtraffic/s3-encryption-proxy', versionsFound: 315, versionsQueued: 315, rows: { waiting: 206, done: 105 } })];
    f.counts = { waiting: 206, running: 0, failed: 0, done: 105 };
    f.limit = downloadLimit({ until: new Date(Date.now() + 40 * 60_000).toISOString() });
  }

  it("R-CUR-011 R-CUR-018 an upstream's rate limit is said above the list, on its waiting rows and on the bulk, with its answer folded", async () => {
    const { root } = await open({ rows: heldQueue });
    const notice = root.querySelector<HTMLElement>('[data-testid="download-limit"]')!;
    expect(text(notice.querySelector('.limit-sentence span:last-child')!)).toMatch(
      /^Docker Hub limits how often Quader may ask it, since .+, and does not say until when\. Quader asks again at .+; the downloads that wait start once Docker Hub allows it\./,
    );
    const details = notice.querySelector('quader-technical-details')!;
    expect(text(details)).toContain('HTTP 429');
    expect(text(details)).toContain('RateLimit-Remaining: 0;w=3600');
    expect(text(details)).toContain('TOOMANYREQUESTS');
    expect(text(details)).toContain("(Quader's wait: the upstream named no time)");
    expect(text(rowOf(root, '3.8.50'))).toMatch(/Rate limit until \d/);
    expect(text(rowOf(root, '3.8.49'))).not.toContain('Rate limit');
    expect(text(root.querySelector('[data-testid="download-bulks"]')!)).toContain("Waits for Docker Hub's rate limit until");
    await expectAccessible(root);
  });

  it('R-UI-213 a queue a limit holds with nothing running is read every 30 s, and the sentence goes when the limit ends', async () => {
    vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] });
    try {
      const { root, fixture, fake } = await open({ rows: heldQueue });
      const calls = fake.listCalls.length;
      vi.advanceTimersByTime(DOWNLOADS_REFRESH_MS * 3);
      await settle(fixture);
      expect(fake.listCalls.length).toBe(calls);
      fake.limit = null;
      fake.counts = { waiting: 205, running: 1, failed: 0, done: 105 };
      vi.advanceTimersByTime(LIVE_SLOW_MS);
      await settle(fixture);
      expect(fake.listCalls.length).toBe(calls + 1);
      expect(root.querySelector('[data-testid="download-limit"]')).toBeNull();
      expect(text(rowOf(root, '3.8.50'))).not.toContain('Rate limit');
    } finally {
      vi.useRealTimers();
    }
  });

  it('R-REPO-077 a limit an account would lift offers one where this reader adds it: the administrator of a well-known registry gets its page', async () => {
    const offer = async (repository: string, capabilities: readonly string[]) => {
      const { root } = await open({ rows: heldQueue, repository, capabilities });
      return root.querySelector<HTMLElement>('[data-testid="download-limit-account"]');
    };
    const admin = (await offer('upstream/docker-hub', ['repositories:read-config', 'repositories:write-config']))!;
    expect(text(admin)).toBe(
      'Docker Hub allows more requests with an account. This registry has none: add one on its page, or in the Helm values (secrets.upstreamAccounts). Add an account',
    );
    expect(admin.querySelector('a')!.getAttribute('href')).toBe('/admin/upstream/docker-hub');
    expect(text((await offer('upstream/docker-hub', []))!)).toBe('Docker Hub allows more requests with an account; an administrator of Quader can add one.');
    expect(text((await offer('web/hub', ['repositories:write-config']))!)).toBe(
      "Docker Hub allows more requests with an account. This repository has none: add one in the repository's settings under Upstream.",
    );
    const reader = (await offer('web/hub', []))!;
    expect(text(reader)).toBe('Docker Hub allows more requests with an account; an Owner of the project can add one.');
    expect(reader.querySelector('a')).toBeNull();
  });

  it('R-REPO-077 a limit no account lifts, or an upstream that has one, offers nothing', async () => {
    const { root } = await open({
      rows: (f) => (heldQueue(f), (f.limit = { ...f.limit!, accountRaisesLimit: false })),
      repository: 'upstream/docker-hub',
      capabilities: ['repositories:write-config'],
    });
    expect(root.querySelector('[data-testid="download-limit"]')).not.toBeNull();
    expect(root.querySelector('[data-testid="download-limit-account"]')).toBeNull();
  });

  it('R-CUR-011 a limit the upstream named a time for says that time', async () => {
    const until = new Date(Date.now() + 2 * 3600_000).toISOString();
    const { root } = await open({ rows: (f) => ((f.rows = []), (f.limit = downloadLimit({ hinted: true, until, upstream: 'registry.npmjs.org' }))) });
    expect(text(root.querySelector('[data-testid="download-limit"] .limit-sentence span:last-child')!)).toMatch(
      /^registry\.npmjs\.org limits how often Quader may ask it, since .+\. The downloads that wait start at .+, the time registry\.npmjs\.org named\./,
    );
  });
});

/** The syncs of the cases (R-CUR-027): a run in progress, nothing new, a run that waits for its previous one, a limit, a paused one. */
function syncs(fake: FakeRemoteDownloadsApi): void {
  queue(fake);
  fake.syncs = [
    downloadSync({
      id: 's-run', subject: 'lodash', createdBy: { id: SELF, name: 'Alice' },
      lastBulk: downloadBulk({ id: 'b-s1', origin: 'sync', sync: 's-run', state: 'queued', versionsFound: 2, versionsQueued: 1, rows: { running: 1 } }),
    }),
    downloadSync({
      id: 's-none', subject: 'react', createdBy: { id: 'other', name: 'Bob' },
      bounds: { maxVersions: 5, maxBytes: 107374182400, includePrereleases: true, fromVersion: '19.0.0', toVersion: null, toInclusive: true },
      lastBulk: downloadBulk({ id: 'b-s2', origin: 'sync', sync: 's-none', state: 'done', versionsFound: 5, versionsQueued: 0, versionsRefused: 1, rows: {} }),
    }),
    downloadSync({ id: 's-over', subject: '@acme/build-helpers', lastOutcome: 'overlapped' }),
    downloadSync({ id: 's-lim', level: 'section', subject: '@acme', lastOutcome: 'limited' }),
    downloadSync({ id: 's-pause', subject: 'left-pad', state: 'paused', nextRunAt: null }),
  ];
}

function syncRow(root: HTMLElement, subject: string): HTMLTableRowElement {
  return [...root.querySelectorAll<HTMLTableRowElement>('[data-testid="download-syncs"] tbody tr')].find((tr) =>
    tr.querySelector('th')?.textContent?.includes(subject),
  )!;
}

describe('The downloads a remote repository keeps in sync', () => {
  it('R-CUR-027 R-CUR-018 the syncs stand above the bulks: what each keeps, its last run in words, its next run or Paused', async () => {
    const { root } = await open({ manage: true, rows: syncs, upstreamName: 'Docker Hub' });
    const section = root.querySelector<HTMLElement>('[data-testid="download-syncs"]')!;
    expect(section.compareDocumentPosition(root.querySelector('[data-testid="download-bulks"]')!) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
    expect(text(section)).toContain('Quader downloads what is new every hour, the interval of this repository. A sync stores only what the rules deliver and removes nothing.');
    expect(text(syncRow(root, 'lodash'))).toContain('Every version of lodash');
    expect(text(syncRow(root, 'lodash'))).toContain('The newest 2, without pre-releases');
    expect(text(syncRow(root, 'lodash'))).toContain('Set up by you');
    expect(text(syncRow(root, 'lodash'))).toContain('Downloading 1 new');
    expect(text(syncRow(root, 'react'))).toContain('From 19.0.0');
    expect(text(syncRow(root, 'react'))).toContain('Set up by Bob');
    expect(text(syncRow(root, 'react'))).toContain('Nothing new');
    expect(text(syncRow(root, 'react'))).toContain('1 not delivered');
    expect(syncRow(root, 'react').querySelector('a')?.getAttribute('href')).toContain('bulk=b-s2');
    expect(text(syncRow(root, '@acme/build-helpers'))).toContain('Its previous run has not ended yet.');
    expect(text(syncRow(root, 'The sub-section @acme'))).toContain('Docker Hub limits how often Quader may ask it.');
    expect(text(syncRow(root, 'left-pad'))).toContain('Paused');
    await expectAccessible(root);
  });

  it('R-CUR-027 R-UI-163 a holder of manage runs, pauses, resumes and removes a sync from its menu; a reader has no menu', async () => {
    const { root, fixture, fake } = await open({ manage: true, rows: syncs });
    const menu = (subject: string): HTMLButtonElement => syncRow(root, subject).querySelector<HTMLButtonElement>('td.actions button[aria-haspopup]')!;
    expect(menu('lodash').getAttribute('aria-label')).toBe('Actions for lodash');
    menu('lodash').click();
    await settle(fixture);
    expect([...document.querySelectorAll('.menu .menu-item')].map((e) => text(e))).toEqual(['Run now', 'Pause', 'Remove…']);
    [...document.querySelectorAll<HTMLButtonElement>('.menu .menu-item')][0]!.click();
    await settle(fixture);
    expect(fake.calls).toContain('run-sync web/npmjs s-run');
    menu('left-pad').click();
    await settle(fixture);
    expect([...document.querySelectorAll('.menu .menu-item')].map((e) => text(e))).toEqual(['Run now', 'Resume', 'Remove…']);
    [...document.querySelectorAll<HTMLButtonElement>('.menu .menu-item')][1]!.click();
    await settle(fixture);
    expect(fake.calls).toContain('resume-sync web/npmjs s-pause');
    menu('react').click();
    await settle(fixture);
    [...document.querySelectorAll<HTMLButtonElement>('.menu .menu-item')][2]!.click();
    await settle(fixture);
    const confirm = [...document.querySelectorAll<HTMLButtonElement>('.dialog-actions button')].find((b) => text(b) === 'Remove')!;
    expect(text(document.querySelector('.quader-dialog')!)).toContain('Stop keeping react in sync?');
    confirm.click();
    await settle(fixture);
    expect(fake.calls).toContain('remove-sync web/npmjs s-none');
    expect(syncRow(root, 'react')).toBeUndefined();

    const reader = await open({ rows: syncs });
    expect(reader.root.querySelector('[data-testid="download-syncs"] td.actions')).toBeNull();
  });

  it('R-CUR-027 R-UI-213 the syncs are read every five seconds while a run is in progress, every 30 s otherwise; a repository without one shows none', async () => {
    vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] });
    try {
      const { fixture, fake } = await open({ rows: syncs });
      const reads = () => fake.calls.filter((c) => c.startsWith('syncs ')).length;
      const before = reads();
      vi.advanceTimersByTime(DOWNLOADS_REFRESH_MS);
      await settle(fixture);
      expect(reads()).toBe(before + 1);
      fake.syncs = fake.syncs.map((s) => ({ ...s, lastBulk: s.lastBulk && { ...s.lastBulk, state: 'done' as const } }));
      vi.advanceTimersByTime(DOWNLOADS_REFRESH_MS);
      await settle(fixture);
      const settled = reads();
      vi.advanceTimersByTime(DOWNLOADS_REFRESH_MS * 3);
      await settle(fixture);
      expect(reads()).toBe(settled);
      vi.advanceTimersByTime(LIVE_SLOW_MS);
      await settle(fixture);
      expect(reads()).toBe(settled + 1);
    } finally {
      vi.useRealTimers();
    }
    const none = await open();
    expect(none.root.querySelector('[data-testid="download-syncs"]')).toBeNull();
  });

  it("R-CUR-027 R-UI-216 a failed read of the syncs says every fact of the answer and reads again on Try again", async () => {
    const { root } = await open({
      rows: (f) => {
        syncs(f);
        f.listSyncs = async () => {
          throw new ApiError(503, { code: 'unavailable', detail: 'The download queue is not available on this replica.' }, 'rid-503');
        };
      },
    });
    const section = root.querySelector<HTMLElement>('[data-testid="download-syncs"]')!;
    expect(text(section)).toContain('rid-503');
    expect(text(section)).toContain('503');
  });
});

describe('The tab strip of a remote repository', () => {
  async function page(repo: object, options: { anonymous?: boolean; tab?: 'content' | 'downloads'; failed?: number } = {}) {
    const r = await renderScreen(RepositoryContent, {
      inputs: { project: 'web', key: (repo as { key: string }).key, ...(options.tab ? { tab: options.tab } : {}) },
      setup: (get) => {
        const catalog = get(RepositoriesApi) as FakeRepositoriesApi;
        catalog.repositories = [...catalog.repositories, repo as never];
        catalog.formatsResult = [PACKAGE_FORMAT];
        const downloads = get(RemoteDownloadsApi) as FakeRemoteDownloadsApi;
        downloads.counts = { waiting: 0, running: 0, failed: options.failed ?? 0, done: 3 };
        if (options.anonymous) (get(SessionApi) as FakeSessionApi).whoamiResult = new ApiError(401, { code: 'unauthenticated', detail: 'no session' }, 'rid-401');
      },
    });
    await settle(r.fixture);
    return r;
  }

  it('R-CUR-018 a remote repository has the strip of its content, Upstream, Downloads and Details with addresses, the failed count on Downloads', async () => {
    const { root } = await page(REMOTE, { failed: 2 });
    const links = [...root.querySelectorAll<HTMLAnchorElement>('quader-repository-tabs a.tab-link')];
    expect(links.map((a) => text(a))).toEqual(['Packages', 'Upstream', 'Downloads · 2 failed', 'Details']);
    expect(links[0]!.getAttribute('aria-current')).toBe('page');
    expect(links[0]!.getAttribute('href')).toBe('/projects/web/repositories/npmjs');
    expect(links[1]!.getAttribute('href')).toBe('/projects/web/repositories/npmjs/upstream');
    expect(links[2]!.getAttribute('href')).toBe('/projects/web/repositories/npmjs/downloads');
    expect(links[3]!.getAttribute('href')).toBe('/projects/web/repositories/npmjs/details');
    await expectAccessible(root);
  });

  it('R-UI-213 the strip reads the queue itself on the content, every 30 s, so that a failed download shows without a reload', async () => {
    vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] });
    try {
      const { root, fixture, get } = await page(REMOTE);
      const label = (): string => text([...root.querySelectorAll<HTMLAnchorElement>('quader-repository-tabs a.tab-link')][2]!);
      expect(label()).toBe('Downloads');
      (get(RemoteDownloadsApi) as FakeRemoteDownloadsApi).counts = { waiting: 0, running: 0, failed: 1, done: 3 };
      vi.advanceTimersByTime(LIVE_SLOW_MS);
      await settle(fixture);
      expect(label()).toBe('Downloads · 1 failed');
    } finally {
      vi.useRealTimers();
    }
  });

  it('R-CUR-018 the tab Downloads shows the queue under the strip, with Downloads current, and no content list', async () => {
    const { root } = await page(REMOTE, { tab: 'downloads' });
    const links = [...root.querySelectorAll<HTMLAnchorElement>('quader-repository-tabs a.tab-link')];
    expect(links[2]!.getAttribute('aria-current')).toBe('page');
    expect(root.querySelector('quader-remote-downloads')).not.toBeNull();
    expect(root.querySelector('quader-repository-items')).toBeNull();
  });

  it('R-CUR-018 a local repository has no tab Downloads, and the address of its tab Downloads leads to its content', async () => {
    const { root, get } = await page(LOCAL, { tab: 'downloads' });
    expect([...root.querySelectorAll('quader-repository-tabs a.tab-link')].map((a) => text(a))).not.toContain('Downloads');
    expect(root.querySelector('quader-remote-downloads')).toBeNull();
    expect(get(Router).url).toBe('/projects/web/repositories/npm-local');
  });

  it('R-CUR-018 a visitor without a session sees a remote repository without Upstream and Downloads: the queue answers signed-in readers only', async () => {
    const { root } = await page(REMOTE, { anonymous: true });
    expect([...root.querySelectorAll('quader-repository-tabs a.tab-link')].map((a) => text(a))).not.toContain('Downloads');
  });
});
