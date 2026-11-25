import { vi } from 'vitest';
import { UpstreamDownloadsApi } from '../../api/upstream-downloads-api';
import { FakeUpstreamDownloadsApi, finishedDownload, runningDownload, waitingQueue } from '../../api/testing/fakes-upstream-downloads';
import { BrowserStorage } from '../../core/storage';
import { expectAccessible, renderScreen, settle, text } from '../../testing/screen';
import { DOWNLOADS_CAPABILITY, UpstreamDownloadsButton } from './upstream-downloads-button';
import { HIDDEN_BEFORE_KEY } from './upstream-downloads-panel';

const DIGEST = 'sha256:1a2b3c4d5e6f7a8b9c0d1e2f3a4b5c6d7e8f9a0b1c2d3e4f5a6b7c8d9e0f1a2b';

/** Five running downloads, one of each kind a row words differently. */
function running(fake: FakeUpstreamDownloadsApi): void {
  fake.running = [
    runningDownload({ id: 'r-npm', bytes: 150_000_000, total: 300_000_000, bytesPerSecond: 12_897_484, elapsedSeconds: 42.3 }),
    runningDownload({
      id: 'r-deb',
      repository: { project: 'ops', key: 'debian', format: 'deb' },
      name: 'linux-image-amd64',
      version: '6.12.9-1',
      path: 'pool/main/l/linux/linux-image-amd64_6.12.9-1_amd64.deb',
      total: null,
      bytes: 4_194_304,
      bytesPerSecond: null,
      origin: 'mirror',
      by: null,
    }),
    runningDownload({
      id: 'r-blob',
      repository: { project: 'upstream', key: 'docker-hub', format: 'oci' },
      name: 'library/ubuntu',
      version: '',
      path: `v2/library/ubuntu/blobs/${DIGEST}`,
      origin: 'queue',
      queueOrigin: 'person',
      queueRow: '01937d4d-0000-7000-8000-000000000001',
      by: { id: '0192f0c4-7a7e-7b51-9b1e-2f0d6c9e1a10', name: 'priya' },
    }),
    runningDownload({ id: 'r-bulk', name: '@acme/build-helpers', origin: 'queue', queueOrigin: 'bulk', by: null }),
    runningDownload({
      id: 'r-db',
      repository: { project: 'upstream', key: 'scanner-trivy', format: 'oci' },
      name: 'aquasecurity/trivy-db',
      version: '2',
      origin: 'scanner_database',
      by: null,
    }),
  ];
}

async function open(options: { setup?: (fake: FakeUpstreamDownloadsApi) => void; storage?: BrowserStorage } = {}) {
  const r = await renderScreen(UpstreamDownloadsButton, {
    capabilities: [DOWNLOADS_CAPABILITY],
    providers: options.storage ? [{ provide: BrowserStorage, useValue: options.storage }] : [],
    setup: (get) => {
      get(BrowserStorage).remove('local', HIDDEN_BEFORE_KEY);
      (options.setup ?? running)(get(UpstreamDownloadsApi) as FakeUpstreamDownloadsApi);
    },
  });
  r.root.querySelector<HTMLButtonElement>('button.downloads-button')!.click();
  await settle(r.fixture);
  return { ...r, fake: r.get(UpstreamDownloadsApi) as FakeUpstreamDownloadsApi };
}

const drawer = (): HTMLElement | null => document.querySelector<HTMLElement>('.quader-drawer');
const section = (which: 'running' | 'finished'): HTMLElement => drawer()!.querySelector<HTMLElement>(`[data-testid="upstream-downloads-${which}"]`)!;
const rows = (which: 'running' | 'finished'): HTMLElement[] => [...section(which).querySelectorAll<HTMLElement>('li.dl-row')];
/** The first five rows of a section, for a case that names each. */
const five = (which: 'running' | 'finished'): [HTMLElement, HTMLElement, HTMLElement, HTMLElement, HTMLElement] => {
  const r = rows(which);
  expect(r.length).toBeGreaterThanOrEqual(5);
  return r.slice(0, 5) as [HTMLElement, HTMLElement, HTMLElement, HTMLElement, HTMLElement];
};
const heading = (which: 'running' | 'finished'): string => text(section(which).querySelector('h3')!);
/** The parts of a row's first line of figures, as the dots between them separate them on the screen. */
const figures = (row: HTMLElement): string[] => [...row.querySelector('.dl-facts')!.children].map((c) => text(c));
const buttonIn = (root: ParentNode, label: string): HTMLButtonElement | undefined =>
  [...root.querySelectorAll<HTMLButtonElement>('button')].find((b) => text(b) === label);

describe('The Downloads panel: what runs', () => {
  it('R-UI-211 a running download with a total shows its bar and "of", one without a total its bytes alone, a blob its digest', async () => {
    const { root } = await open();
    expect(root.querySelector('button.downloads-button')!.getAttribute('aria-expanded')).toBe('true');
    expect(drawer()!.getAttribute('role') ?? drawer()!.querySelector('[role="dialog"]')?.getAttribute('role')).toBeTruthy();
    expect(heading('running')).toBe('Running (5)');
    const [npm, deb, blob] = five('running');
    const bar = npm.querySelector('progress')!;
    expect(bar.getAttribute('aria-label')).toBe('Progress of left-pad 1.3.0');
    expect(bar.max).toBe(300_000_000);
    expect(figures(npm)).toEqual(['143.1 MiB of 286.1 MiB', '12.3 MiB/s', '42 s']);
    expect(deb.querySelector('progress')).toBeNull();
    expect(figures(deb)).toEqual(['4.0 MiB', '–', '2 s']);
    expect(text(blob.querySelector('.dl-subject')!)).toBe('library/ubuntu sha256:1a2b3c4d5e6f');
    expect(blob.querySelector('.dl-subject')!.getAttribute('title')).toBe(`v2/library/ubuntu/blobs/${DIGEST}`);
    await expectAccessible(drawer()!);
  });

  it('R-UI-211 each origin reads in words, and a queued row links its queue filtered by its name', async () => {
    await open();
    const [npm, deb, blob, bulk, db] = five('running');
    expect(text(npm)).toContain('Client · sam');
    expect(text(deb)).toContain('Mirror pass');
    expect(text(blob)).toContain('priya');
    expect(text(bulk)).toContain('Bulk');
    expect(text(db)).toContain('Vulnerability database');
    const queue = [...blob.querySelectorAll('a')].find((a) => text(a) === 'Show in queue')!;
    expect(queue.getAttribute('href')).toBe('/projects/upstream/repositories/docker-hub/downloads?name=library%2Fubuntu');
    expect([...blob.querySelectorAll('a')].find((a) => text(a) === 'upstream/docker-hub')?.getAttribute('href')).toBe('/projects/upstream/repositories/docker-hub');
    expect([...npm.querySelectorAll('a')].some((a) => text(a) === 'Show in queue')).toBe(false);
  });

  it('R-UI-211 the filter narrows both sections and their headings say how many of how many', async () => {
    const { fixture } = await open({
      setup: (f) => {
        running(f);
        f.finished = [finishedDownload({ id: 'f-1' }), finishedDownload({ id: 'f-2', name: 'react', version: '19.0.0', path: 'react/-/react-19.0.0.tgz' })];
      },
    });
    const field = drawer()!.querySelector<HTMLInputElement>('input[type="search"]')!;
    field.value = 'UBUNTU';
    field.dispatchEvent(new Event('input'));
    await settle(fixture);
    expect(heading('running')).toBe('Running (1 of 5)');
    expect(rows('running')).toHaveLength(1);
    expect(heading('finished')).toBe('Finished (0 of 2)');
    expect(text(section('finished'))).toContain('No download matches the filter.');
    field.value = 'react';
    field.dispatchEvent(new Event('input'));
    await settle(fixture);
    expect(heading('finished')).toBe('Finished (1 of 2)');
  });

  it('R-UI-211 more running than listed says how many more; nothing running says so', async () => {
    const { fixture, fake } = await open({
      setup: (f) => {
        running(f);
        f.runningCount = 145;
      },
    });
    expect(heading('running')).toBe('Running (145)');
    expect(text(section('running'))).toContain('and 140 more running');
    fake.running = [];
    fake.runningCount = null;
    drawer()!.querySelector<HTMLButtonElement>('.drawer-close')!.click();
    await settle(fixture);
    fixture.nativeElement.querySelector('button.downloads-button').click();
    await settle(fixture);
    expect(text(section('running'))).toContain('No download from an upstream is running.');
  });

  // The owner's panel of 2026-11-25 said "No download from an upstream is running" while 206 rows waited for Docker Hub's limit: a queue
  // an upstream's rate limit holds is said above Running, with the upstream, the time and the way to the queue (R-CUR-011, Q-UI-1101).
  it("R-CUR-011 R-UI-211 a queue an upstream's rate limit holds is said above what runs, and leads to its queue", async () => {
    await open({
      setup: (f) => {
        f.waiting = [
          waitingQueue(),
          waitingQueue({ repository: { project: 'web', key: 'npmjs', format: 'npm' }, waiting: 1, upstream: 'registry.npmjs.org', hinted: true }),
        ];
      },
    });
    const held = drawer()!.querySelector<HTMLElement>('[data-testid="upstream-downloads-waiting"]')!;
    expect(text(held.querySelector('h3')!)).toBe('Waiting for an upstream (2)');
    const [hub, npm] = [...held.querySelectorAll<HTMLElement>('li.dl-row')];
    expect(text(hub!.querySelector('.dl-wait')!)).toMatch(
      /206 downloads of upstream\/docker-hub wait: Docker Hub limits requests; Quader asks again at .+\.$/,
    );
    expect(text(npm!.querySelector('.dl-wait')!)).toMatch(/1 download of web\/npmjs waits: registry\.npmjs\.org limits requests until .+\.$/);
    const queue = [...hub!.querySelectorAll('a')].find((a) => text(a) === 'Show in queue')!;
    expect(queue.getAttribute('href')).toBe('/projects/upstream/repositories/docker-hub/downloads');
    expect(text(section('running'))).toContain('No download from an upstream is running.');
    await expectAccessible(drawer()!);
  });

  it('R-UI-211 the open panel reads the list every pollSeconds and stops when it closes; a failed read keeps the rows and says so', async () => {
    vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] });
    try {
      const { root, fixture, fake } = await open();
      const calls = fake.listCalls;
      vi.advanceTimersByTime(2_000);
      await settle(fixture);
      expect(fake.listCalls).toBe(calls + 1);
      fake.failWith = 503;
      vi.advanceTimersByTime(2_000);
      await settle(fixture);
      expect(drawer()!.querySelector('[data-testid="upstream-downloads-refresh-failed"]')?.textContent).toContain(
        'The list could not be refreshed; Quader tries again.',
      );
      expect(rows('running')).toHaveLength(5);
      root.querySelector<HTMLButtonElement>('button.downloads-button')!.click();
      await settle(fixture);
      const closed = fake.listCalls;
      vi.advanceTimersByTime(10_000);
      await settle(fixture);
      expect(fake.listCalls).toBe(closed);
    } finally {
      vi.useRealTimers();
    }
  });
});

describe('The Downloads panel: what finished, and Hide all', () => {
  const finished = (f: FakeUpstreamDownloadsApi): void => {
    f.finished = [
      finishedDownload({ id: 'f-ok', finishedAt: '2026-11-24T08:38:05Z', durationSeconds: 72, bytes: 300_000_000, bytesPerSecond: 4_194_304 }),
      finishedDownload({ id: 'f-net', finishedAt: '2026-11-24T08:38:04Z', outcome: 'failed', failure: { class: 'network', code: 'connection_reset' } }),
      finishedDownload({ id: 'f-int', finishedAt: '2026-11-24T08:38:03Z', outcome: 'failed', failure: { class: 'integrity', code: 'digest_mismatch' } }),
      finishedDownload({ id: 'f-can', finishedAt: '2026-11-24T08:38:02Z', outcome: 'cancelled' }),
      finishedDownload({ id: 'f-irr', finishedAt: '2026-11-24T08:38:01Z', outcome: 'interrupted', durationSeconds: 0.4, bytesPerSecond: 7_864_320 }),
    ];
  };

  it('R-UI-212 each outcome has its badge and words, and a duration below one second reads "< 1 s" without an average', async () => {
    await open({ setup: finished });
    expect(heading('finished')).toBe('Finished (5)');
    const [ok, net, int, can, irr] = five('finished');
    expect(ok.querySelector('.badge')).toBeNull();
    expect(figures(ok)).toEqual(['286.1 MiB', '1 min 12 s', '4.0 MiB/s']);
    expect(text(net.querySelector('.badge.bad')!)).toBe('errorFailed: Network');
    expect(net.querySelector('.badge')!.getAttribute('title')).toBe('connection_reset');
    expect(text(int.querySelector('.badge.bad')!)).toContain('Failed: Integrity check failed');
    expect(text(can.querySelector('.badge')!)).toBe('Cancelled');
    const interrupted = irr.querySelector('.badge.warn')!;
    expect(text(interrupted)).toContain('Interrupted');
    expect(interrupted.getAttribute('title')).toBe('The replica stopped while this download ran.');
    expect(figures(irr)).toEqual(['3.0 MiB', '< 1 s']);
    expect(irr.querySelector('time')).not.toBeNull();
    await expectAccessible(drawer()!);
  });

  it('R-UI-212 Hide all keeps the newest end on this device and hides those rows, a later download shows, Show again brings them back', async () => {
    const { fixture, fake, get } = await open({ setup: finished });
    buttonIn(section('finished'), 'Hide all')!.click();
    await settle(fixture);
    expect(get(BrowserStorage).get('local', HIDDEN_BEFORE_KEY)).toBe('2026-11-24T08:38:05Z');
    expect(rows('finished')).toHaveLength(0);
    expect(text(section('finished'))).toContain('Hidden on this device: 5.');
    expect(text(section('finished'))).not.toContain('No download has finished yet.');
    expect(buttonIn(section('finished'), 'Hide all')).toBeUndefined();

    fake.finished = [finishedDownload({ id: 'f-new', name: 'react', finishedAt: '2026-11-24T08:41:00Z' }), ...fake.finished];
    drawer()!.querySelector<HTMLButtonElement>('.drawer-close')!.click();
    await settle(fixture);
    fixture.nativeElement.querySelector('button.downloads-button').click();
    await settle(fixture);
    expect(rows('finished').map((r) => text(r.querySelector('.dl-name')!))).toEqual(['react']);

    buttonIn(section('finished'), 'Show again')!.click();
    await settle(fixture);
    expect(get(BrowserStorage).get('local', HIDDEN_BEFORE_KEY)).toBeNull();
    expect(rows('finished')).toHaveLength(6);
  });

  it('R-UI-212 without local storage there is no Hide all', async () => {
    const blocked = { available: () => false, get: () => null, set: () => false, remove: () => undefined } as unknown as BrowserStorage;
    await open({ setup: finished, storage: blocked });
    expect(rows('finished')).toHaveLength(5);
    expect(buttonIn(section('finished'), 'Hide all')).toBeUndefined();
  });

  it('R-UI-212 empty sections say that nothing runs and nothing has finished, and the foot links the manual', async () => {
    await open({ setup: () => undefined });
    expect(text(section('running'))).toContain('No download from an upstream is running.');
    expect(text(section('finished'))).toContain('No download has finished yet.');
    const about = [...drawer()!.querySelectorAll('a')].find((a) => text(a) === 'About this list')!;
    expect(about.getAttribute('href')).toBe('/help/admin/operations#downloads-from-upstreams');
  });
});
