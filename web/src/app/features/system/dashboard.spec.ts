import { TestBed } from '@angular/core/testing';
import { provideRouter } from '@angular/router';
import { BackupsApi } from '../../api/backups-api';
import { ApiError } from '../../api/problem';
import { FakeBackupsApi, sampleSummary } from '../../api/testing/fakes-backups';
import { FakeSystemApi, provideFakeApis, sampleInfo, sampleReplica, sampleRuntime } from '../../api/testing/fakes';
import { FakeStorageApi, sampleOverview } from '../../api/testing/fakes-storage';
import { StorageApi, StorageOverview } from '../../api/storage-api';
import { Replica, SystemApi } from '../../api/system-api';
import { SessionStore } from '../../core/session-store';
import { seriousViolations } from '../../testing/axe';
import { Dashboard } from './dashboard';

async function render(
  setup?: (system: FakeSystemApi) => void,
  backups?: (api: FakeBackupsApi) => void,
  storage?: (api: FakeStorageApi) => void,
) {
  await TestBed.configureTestingModule({
    imports: [Dashboard],
    providers: [provideRouter([]), ...provideFakeApis()],
  }).compileComponents();
  setup?.(TestBed.inject(SystemApi) as FakeSystemApi);
  backups?.(TestBed.inject(BackupsApi) as FakeBackupsApi);
  storage?.(TestBed.inject(StorageApi) as FakeStorageApi);
  await TestBed.inject(SessionStore).load();
  const fixture = TestBed.createComponent(Dashboard);
  for (let i = 0; i < 4; i++) await fixture.whenStable();
  return { fixture, root: fixture.nativeElement as HTMLElement };
}

const text = (root: HTMLElement): string => root.textContent?.replace(/\s+/g, ' ') ?? '';

/** An installation with a ready replica of each main role, the given ones in front, and the summary the server folds. */
function installation(s: FakeSystemApi, first: Replica[] = []) {
  const one = (role: 'api' | 'packages' | 'worker') =>
    sampleReplica({ name: 'quader-' + role + '-0', instanceUid: role, roles: [role] });
  const replicas = [...first, ...(first.some((r) => r.roles[0] === 'api' && r.state === 'ready' && !r.lost) ? [] : [one('api')]), one('packages'), one('worker')];
  s.replicasResult = replicas;
  const count = (role: string) => {
    const live = replicas.filter((r) => r.roles[0] === role && !r.lost);
    return { live: live.length, ready: live.filter((r) => r.state === 'ready').length, degraded: 0, nodes: 1 };
  };
  s.infoResult = sampleInfo({
    replicas: {
      live: replicas.filter((r) => !r.lost).length,
      ready: replicas.filter((r) => !r.lost && r.state === 'ready').length,
      versions: { '0.1.1': replicas.length },
      roles: { api: count('api'), packages: count('packages'), worker: count('worker') },
    },
  });
}

describe('Admin dashboard (R-UI-038)', () => {
  it('R-UI-038 shows the instance, each role with its load, the dependencies with their checks, and the counts', async () => {
    const { root } = await render((s) =>
      installation(s, [
        sampleReplica({
          runtime: sampleRuntime({ requests: 200, windowSeconds: 10, latencyMs: { p50: 4, p95: 33, p99: 50 } }),
          checks: { database: { status: 'pass', durationMs: 2.4 }, valkey: { status: 'pass', durationMs: 0.8 }, objectStore: { status: 'pass', durationMs: 7 } },
        }),
      ]),
    );
    const t = text(root);
    expect(root.querySelector('h1')?.textContent).toContain('Dashboard');
    expect(t).toContain('All roles ready');
    expect(t).toContain('20 per second');
    expect(t).toContain('33 ms');
    expect(t).toContain('PostgreSQL');
    expect(t).toContain('Connected');
    expect(t).toContain('2.4 ms');
    expect(t).toContain('1.7 TiB');
    expect(t).toContain('Repositories');
    // The classes as the lists show them: the system repository is System, not one of Local (Q-UI-1203, Q-UI-1217).
    const classes = [...root.querySelectorAll<HTMLElement>('.card:has(#dash-repos) .kv > div')].map((d) => text(d));
    expect(classes).toEqual(['Local3', 'Remote3', 'Virtual1', 'System1']);
    // The repositories of every project are the main area's, and a new one is created in its project (Q-UI-1040).
    expect(root.querySelector('a[href="/repositories"]')).toBeTruthy();
    expect(root.querySelector('a[href^="/admin/repositories"], a[href^="/admin/projects"]')).toBeNull();
    expect(await seriousViolations(root)).toEqual([]);
  });

  it('R-UI-145 every row of a component\'s card is built one way: CPU and Memory are rows of its list with their bar inside', async () => {
    const { root } = await render((s) => {
      s.replicasResult = [sampleReplica({ runtime: sampleRuntime({ requests: 200, windowSeconds: 10 }) })];
    });
    const card = root.querySelector('section.card[aria-labelledby^="dash-role-"]')!;
    const rows = [...card.querySelectorAll(':scope > dl.kv > div')];
    expect(rows.map((r) => r.querySelector('dt')!.textContent!.trim())).toEqual([
      'Nodes',
      'Requests',
      'Latency p95',
      'Server errors',
      'In flight',
      'CPU',
      'Memory',
    ]);
    expect(card.querySelector(':scope > .meter')).toBeNull();
    for (const row of rows.slice(5)) {
      expect(row.classList).toContain('meter-row');
      expect(row.querySelector('dd .bar')?.getAttribute('aria-hidden') ?? 'true').toBe('true');
    }
    expect(await seriousViolations(root)).toEqual([]);
  });

  it('R-UI-038 a failing overview leaves the rest of the screen readable and offers a retry', async () => {
    const { root } = await render((s) => {
      installation(s);
      s.overviewResult = new ApiError(503, { code: 'database_unavailable' }, null);
    });
    const t = text(root);
    expect(t).toContain('All roles ready');
    expect(t).toContain('PostgreSQL');
    expect(root.querySelectorAll('quader-error-notice').length).toBe(1);
    expect(t).not.toContain('Active tokens');
  });

  it('R-UI-038 a role without a replica says so instead of showing zeros, and the instance is red', async () => {
    const { root } = await render((s) => (s.replicasResult = [sampleReplica()]));
    expect(text(root)).toContain('No replica of this role is running.');
    expect(text(root)).toContain('No ready replica of the role packages, worker');
    expect(root.querySelector('.instance-bar .state.bad')).toBeTruthy();
  });

  it('R-UI-038 a replica that drains during an update leaves the instance ready and says that replicas are replaced', async () => {
    const { root } = await render((s) =>
      installation(s, [sampleReplica({ name: 'quader-api-old', instanceUid: 'old', state: 'draining', version: '0.1.0' })]),
    );
    const bar = root.querySelector<HTMLElement>('.instance-bar')!;
    expect(text(bar)).toContain('All roles ready');
    expect(text(bar)).toContain('Replicas are being replaced');
    expect(bar.querySelector('.state.bad, .state.warn')).toBeNull();
  });

  it('R-UI-038 a replica lost without draining while its role is ready is a warning that says when', async () => {
    const { root } = await render((s) =>
      installation(s, [sampleReplica({ name: 'quader-api-crashed', instanceUid: 'crashed', lost: true, heartbeatAt: '2026-11-18T09:12:00Z' })]),
    );
    const bar = root.querySelector<HTMLElement>('.instance-bar')!;
    expect(text(bar)).toContain('Replica lost, last heartbeat');
    expect(bar.querySelector('.state.warn')).toBeTruthy();
    expect(bar.querySelector('.state.bad')).toBeNull();
  });

  it('R-UI-038 next to a live replica of the development role all, the roles it covers are not shown as missing', async () => {
    const { root } = await render((s) => {
      s.replicasResult = [sampleReplica({ roles: ['all'] })];
      s.infoResult = { ...s.infoResult, replicas: { live: 1, ready: 1, versions: { dev: 1 }, roles: { all: { live: 1, ready: 1, degraded: 0, nodes: 0 } } } };
    });
    expect(text(root)).not.toContain('No replica of this role is running.');
    expect([...root.querySelectorAll('.card-head h3')].map((h) => h.textContent?.trim())).toContain('all');
  });

  it('R-UI-038 a role whose replicas name no node shows no Nodes row instead of 0', async () => {
    const { root } = await render((s) => {
      s.replicasResult = [sampleReplica({ roles: ['all'], node: null })];
      s.infoResult = { ...s.infoResult, replicas: { live: 1, ready: 1, versions: { dev: 1 }, roles: { all: { live: 1, ready: 1, degraded: 0, nodes: 0 } } } };
    });
    const card = root.querySelector<HTMLElement>('section[aria-labelledby="dash-role-all"]')!;
    expect(text(card)).toContain('1 of 1 ready');
    expect([...card.querySelectorAll('dt')].map((d) => d.textContent?.trim())).not.toContain('Nodes');
    expect(text(root.querySelector<HTMLElement>('.instance-bar')!)).toContain('All roles ready');
  });

  describe('the state of the backup (R-UI-165)', () => {
    /** The row Last backup of the card Operations, or null. */
    const backupRow = (root: HTMLElement) =>
      [...root.querySelectorAll<HTMLElement>('section[aria-labelledby="dash-ops"] dl.kv > div')].find((d) => d.querySelector('dt')?.textContent?.trim() === 'Last backup') ?? null;
    const viewBackups = (root: HTMLElement) => root.querySelector<HTMLAnchorElement>('section[aria-labelledby="dash-ops"] a[href="/admin/system/backups"]');

    it('R-UI-165 the card Operations says when the last backup succeeded, relative with its date as the title, and links the Backups tab', async () => {
      const at = new Date(Date.now() - 3 * 3600_000).toISOString();
      const { root } = await render(installation, (b) => (b.summaryResult = sampleSummary({ lastSuccessAt: at, overdue: false })));
      const row = backupRow(root)!;
      expect(text(row)).toContain('3 hours ago');
      expect(row.querySelector('time')?.getAttribute('datetime')).toBe(at);
      expect(row.querySelector('time')?.getAttribute('title')).toBeTruthy();
      expect(row.querySelector('.state.warn')).toBeNull();
      expect(viewBackups(root)?.textContent?.trim()).toBe('View backups');
      expect(await seriousViolations(root)).toEqual([]);
    });

    it('R-UI-165 an overdue backup is a warning beside its time, and no backup yet is a warning of its own', async () => {
      const overdue = await render(installation, (b) => (b.summaryResult = sampleSummary({ overdue: true })));
      const row = backupRow(overdue.root)!;
      expect(row.querySelector('time')).toBeTruthy();
      expect(text(row.querySelector('.state.warn')!)).toContain('Overdue');
      TestBed.resetTestingModule();

      const none = await render(installation, (b) => (b.summaryResult = sampleSummary({ lastSuccessAt: null, overdue: null })));
      const empty = backupRow(none.root)!;
      expect(empty.querySelector('time')).toBeNull();
      expect(text(empty.querySelector('.state.warn')!)).toContain('No backup yet');
      expect(viewBackups(none.root)).toBeTruthy();
      expect(await seriousViolations(none.root)).toEqual([]);
    });

    it('R-UI-165 a summary the caller may not read leaves the row and the link out, and no error', async () => {
      const { root } = await render(installation, (b) => (b.failure = new ApiError(403, { code: 'forbidden' }, null)));
      expect(backupRow(root)).toBeNull();
      expect(viewBackups(root)).toBeNull();
      expect(root.querySelectorAll('quader-error-notice').length).toBe(0);
      expect(text(root)).toContain('Jobs running');
    });
  });

  describe('The card Storage, the summary of Settings › Storage (R-UI-266)', () => {
    const tiles = (root: HTMLElement) => [...root.querySelectorAll<HTMLElement>('section[aria-labelledby="dash-s3"] quader-storage-summary .tile')];

    it('R-UI-266 the card keeps the readiness check and shows the six tiles in their compact form from the one overview, and links the page', async () => {
      const { root } = await render(installation);
      const card = root.querySelector<HTMLElement>('section[aria-labelledby="dash-s3"]')!;
      expect(text(card.querySelector('h3')!)).toBe('Storage');
      expect(tiles(root).map((t) => t.getAttribute('data-tile'))).toEqual(['connections', 'availability', 'encryption', 'integrity', 'problems', 'stored']);
      expect(text(tiles(root)[0]!)).toContain('Healthy');
      expect(text(tiles(root)[0]!)).toContain('1 of 1 reachable');
      // The compact form: the state and one figure, the page's second lines left out.
      expect(text(tiles(root)[5]!)).not.toContain('blobs');
      expect(card.querySelector('a[href="/admin/settings/storage"]')?.textContent?.trim()).toBe('Storage');
      expect(card.querySelector('a[href="/admin/settings/storage#connections"]')).toBeTruthy();
      expect(text(root.querySelector('.instance-bar')!)).toContain('All roles ready');
      expect(await seriousViolations(root)).toEqual([]);
    });

    it('R-UI-266 the overall state reads the overview: degraded with an unreachable connection warns, unreachable is the error, detached only says degraded', async () => {
      const connections = (c: Partial<StorageOverview['connections']>) => (st: FakeStorageApi) =>
        (st.overviewResult = sampleOverview({ connections: { ...sampleOverview().connections, ...c } }));
      const one = await render(installation, undefined, connections({ state: 'degraded', reachable: 1, configured: 2, unreachable: [{ id: 'secondary', circuitOpen: 1, configured: 3 }] }));
      const bar = text(one.root.querySelector('.instance-bar')!);
      expect(bar).toContain('A storage connection is unreachable');
      expect(bar).not.toContain('All roles ready');
      expect(one.root.querySelector('.instance-bar .state.warn')).toBeTruthy();
      TestBed.resetTestingModule();

      const none = await render(installation, undefined, connections({ state: 'unreachable', reachable: 0 }));
      expect(text(none.root.querySelector('.instance-bar')!)).toContain('No storage connection is reachable');
      expect(none.root.querySelector('.instance-bar .state.bad')).toBeTruthy();
      TestBed.resetTestingModule();

      const detached = await render(installation, undefined, connections({ state: 'degraded', detached: ['archive'] }));
      expect(text(detached.root.querySelector('.instance-bar')!)).toContain('Storage degraded');
    });
  });

  it('R-UI-038 WCAG 2.2.2: the automatic refresh can be paused', async () => {
    const { fixture, root } = await render();
    const pause = [...root.querySelectorAll<HTMLButtonElement>('button')].find((b) => b.textContent?.includes('Pause'))!;
    expect(pause.getAttribute('aria-pressed')).toBe('false');
    pause.click();
    await fixture.whenStable();
    expect(pause.getAttribute('aria-pressed')).toBe('true');
    expect(pause.textContent).toContain('Resume automatic refresh');
  });
});
