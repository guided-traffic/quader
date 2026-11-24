import { TestBed } from '@angular/core/testing';
import { Router } from '@angular/router';
import { vi } from 'vitest';
import { ApiError } from '../../../api/problem';
import { RemoteDownloadsApi } from '../../../api/remote-downloads-api';
import { RepositoriesApi } from '../../../api/repositories-api';
import { SessionApi } from '../../../api/session-api';
import { FakeRepositoriesApi, PACKAGE_FORMAT } from '../../../api/testing/fakes-catalog';
import { FakeSessionApi } from '../../../api/testing/fakes';
import { FakeRemoteDownloadsApi, downloadBulk, downloadFailure, downloadRow } from '../../../api/testing/fakes-remote-downloads';
import { Notifier } from '../../../core/notifier';
import { expectAccessible, renderScreen, settle, text } from '../../../testing/screen';
import { RepositoryContent } from '../content/repository-content';
import { RemoteDownloads, DOWNLOADS_REFRESH_MS } from './remote-downloads';

const SELF = '00000000-0000-0000-0000-000000000001';
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
  options: { manage?: boolean; url?: string; rows?: (fake: FakeRemoteDownloadsApi) => void; upstreamName?: string } = {},
) {
  const r = await renderScreen(RemoteDownloads, {
    inputs: { repository: 'web/npmjs', ...(options.upstreamName ? { upstreamName: options.upstreamName } : {}) },
    setup: async (get) => {
      const session = get(SessionApi) as FakeSessionApi;
      session.meResult = { capabilities: [], roles: [] };
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
    const engine = detail.querySelector<HTMLElement>('quader-disclosure.technical .technical-detail')!;
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
    expect(text(failed)).toContain('Started by You');
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
    const technical = bulk.querySelector<HTMLElement>('quader-disclosure.technical')!;
    expect(technical.querySelector('button[aria-expanded="false"]')?.textContent).toContain('Technical details');
    const engine = technical.querySelector<HTMLElement>('.technical-detail')!;
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
    vi.useFakeTimers({ toFake: ['setInterval', 'clearInterval'] });
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
    vi.useFakeTimers({ toFake: ['setInterval', 'clearInterval'] });
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

  it('R-CUR-018 a queue where nothing waits or runs is not read again by itself', async () => {
    vi.useFakeTimers({ toFake: ['setInterval', 'clearInterval'] });
    try {
      const { fixture, fake } = await open({ rows: (f) => (f.rows = [downloadRow({ state: 'done', outcome: 'completed' })]) });
      const calls = fake.listCalls.length;
      vi.advanceTimersByTime(DOWNLOADS_REFRESH_MS * 3);
      await settle(fixture);
      expect(fake.listCalls.length).toBe(calls);
    } finally {
      vi.useRealTimers();
    }
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
