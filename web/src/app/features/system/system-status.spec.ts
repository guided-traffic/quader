import { TestBed } from '@angular/core/testing';
import { provideRouter } from '@angular/router';
import { ApiError } from '../../api/problem';
import { SessionApi } from '../../api/session-api';
import {
  FakeEncryptionApi,
  FakeSessionApi,
  FakeSystemApi,
  provideFakeApis,
  sampleInfo,
  sampleReplica,
  sampleScannerReplica,
  sampleScanners,
} from '../../api/testing/fakes';
import { EncryptionApi, Replica, SystemApi } from '../../api/system-api';
import { SessionStore } from '../../core/session-store';
import { SystemStatus } from './system-status';
import { SystemRefresh } from './system-refresh';

/** A badge's word without its icon's glyph name. */
const word = (badge: HTMLElement): string => text(badge).replace(/^(check_circle|hourglass_empty)/, '');

async function render(setup?: (system: FakeSystemApi, enc: FakeEncryptionApi, session: FakeSessionApi) => void) {
  await TestBed.configureTestingModule({
    imports: [SystemStatus],
    providers: [provideRouter([]), ...provideFakeApis()],
  }).compileComponents();
  setup?.(
    TestBed.inject(SystemApi) as FakeSystemApi,
    TestBed.inject(EncryptionApi) as FakeEncryptionApi,
    TestBed.inject(SessionApi) as FakeSessionApi,
  );
  await TestBed.inject(SessionStore).load();
  const fixture = TestBed.createComponent(SystemStatus);
  for (let i = 0; i < 4; i++) await fixture.whenStable();
  return { fixture, root: fixture.nativeElement as HTMLElement };
}

const text = (root: HTMLElement): string => root.textContent?.replace(/\s+/g, ' ') ?? '';

describe('System status screen', () => {
  it('R-UI-145 Q-UI-570 Q-UI-560 the version names the revision by its short form with the full hash as its title, and the schema version and its badge stand apart', async () => {
    const revision = '3897aa47708f8a0c1b2d3e4f5a6b7c8d9e0f1a2b';
    const { root } = await render((system) => (system.infoResult = sampleInfo({ version: { version: 'dev', revision, goVersion: 'go1.27.4' } })));
    const code = root.querySelector('section[aria-labelledby="card-install"] code.revision')!;
    expect(code.textContent!.trim()).toBe('3897aa47708f');
    expect(code.getAttribute('title')).toBe(revision);
    const schema = [...root.querySelectorAll('section[aria-labelledby="card-db"] dd')][1]!;
    const badge = schema.querySelector('span.ok')!;
    expect(badge.firstElementChild!.classList).toContain('material-icons');
    expect(badge.textContent!.replace(badge.firstElementChild!.textContent!, '').trim()).toBe('Supported');
    // The number and the badge are separated by a space, not set against each other.
    expect(schema.textContent!.replace(/\s+/g, ' ')).toMatch(/^ ?12 check_circle/);
  });

  it('R-UI-145 Q-UI-560 a replica\'s state is a badge, its icon and its word with a capital in one element', async () => {
    const { root } = await render();
    const state = root.querySelector('section.role tbody tr td:nth-child(4) > .state > :is(.ok, .bad, .badge)')!;
    expect(state.firstElementChild!.classList).toContain('material-icons');
    expect(state.textContent!.replace(state.firstElementChild!.textContent!, '').trim()).toMatch(/^[A-Z]/);
  });

  it('R-UI-038 shows the summary, the installation facts and the replicas per role', async () => {
    const { root } = await render();
    const t = text(root);
    // The sample runs api only: packages and worker have no ready replica, which is red (Q-UI-602).
    expect(t).toContain('No ready replica of the role packages, worker');
    expect(root.querySelector('.summary .material-icons.bad')).toBeTruthy();
    expect(t).toContain('0.1.1');
    expect(t).toContain('https://quader.example.net');
    expect(t).toContain('quader-api-0');
    expect(t).toContain('No live worker replica');
    expect(root.querySelectorAll('table th[scope="row"]').length).toBeGreaterThan(1);
  });

  it('R-UI-038 Q-UI-107 groups the replicas by role in the order api, packages, worker, and by pool within a role', async () => {
    const { root } = await render((system) => {
      const sample = (name: string, roleName: string, node: string, pool: string | null = null) =>
        sampleReplica({ name, instanceUid: name, node, pool, roles: [roleName as 'api' | 'packages' | 'worker'] });
      system.replicasResult = [
        sample('quader-api-0', 'api', 'n1'),
        sample('quader-api-1', 'api', 'n2'),
        sample('quader-packages-0', 'packages', 'n1'),
        sample('quader-packages-1', 'packages', 'n2'),
        sample('quader-worker-0', 'worker', 'n1'),
        sample('quader-worker-1', 'worker', 'n2'),
        sample('quader-worker-vuln-0', 'worker', 'n1', 'vuln'),
      ];
      system.infoResult = sampleInfo({
        replicas: {
          live: 7,
          ready: 7,
          versions: { '0.1.1': 7 },
          roles: {
            api: { live: 2, ready: 2, degraded: 0, nodes: 2 },
            packages: { live: 2, ready: 2, degraded: 0, nodes: 2 },
            worker: { live: 3, ready: 3, degraded: 0, nodes: 2 },
          },
        },
      });
    });
    const spoken = (h: Element) => [...h.children].map((c) => c.textContent?.trim()).join(' ');
    const headings = [...root.querySelectorAll('section.role > h3')].map(spoken);
    expect(headings).toEqual([
      'api live 2, ready 2, degraded 0',
      'packages live 2, ready 2, degraded 0',
      'worker live 3, ready 3, degraded 0',
      // The scanner replicas, one more kind of row after the roles (R-OPS-048).
      'scanner live 2, ready 2',
    ]);
    const worker = root.querySelectorAll<HTMLElement>('section.role')[2]!;
    const pools = [...worker.querySelectorAll('section.pool > h4')].map(spoken);
    expect(pools).toEqual(['Main live 2, ready 2, degraded 0', 'Pool vuln live 1, ready 1, degraded 0']);
    // As it reads: the word and the pool's name with a space between them, not "Poolvuln".
    const named = worker.querySelectorAll('section.pool > h4')[1]!;
    expect(named.textContent!.replace(named.querySelector('.counts')!.textContent!, '').trim()).toBe('Pool vuln');
    expect(text(worker)).toContain('quader-worker-vuln-0');
    // The pool has one ready replica, so it is not drain-safe, and the warning is on the pool, not on the role.
    const warnings = [...worker.querySelectorAll('p.warning')].map((w) => w.textContent?.trim());
    expect(warnings).toHaveLength(1);
    expect(warnings[0]).toContain('Fewer than two ready replicas');
    expect(root.querySelector('section.role#role-web')).toBeNull();
  });

  it('R-UI-038 Q-UI-107 a group web appears during the upgrade across the cut and says so with its replicas', async () => {
    const { root } = await render((system) => {
      system.replicasResult = [
        sampleReplica({ name: 'quader-api-0', instanceUid: 'a' }),
        sampleReplica({ name: 'quader-web-0', instanceUid: 'b', roles: ['web'], node: 'node-b' }),
      ];
    });
    const headings = [...root.querySelectorAll('section.role > h3 > span:first-child')].map((h) => h.textContent?.trim());
    expect(headings).toEqual(['api', 'packages', 'worker', 'web', 'scanner']);
    expect(text(root)).toContain('quader-web-0');
  });

  it('R-UI-038 Q-UI-107 no live packages replica is reported by the group', async () => {
    const { root } = await render();
    expect(text(root)).toContain('No live packages replica');
  });

  it('R-UI-038 shows no maintenance mode row while the mode is normal, and a warning row for any other mode', async () => {
    const normal = await render();
    expect(text(normal.root)).not.toContain('Maintenance mode');
    TestBed.resetTestingModule();

    const other = await render((system) => {
      system.infoResult = { ...sampleInfo(), maintenanceMode: 'read-only' };
    });
    expect(text(other.root)).toContain('Maintenance mode');
    expect(text(other.root)).toContain('read-only');
  });

  it('R-UI-038 a degraded Valkey check is a warning that names what it means, not an error', async () => {
    const { root } = await render((system) => {
      system.replicasResult = [
        sampleReplica({ degraded: ['valkey'], valkey: { ...sampleReplica().valkey!, state: 'degraded', reason: 'timeout' } }),
        sampleReplica({ name: 'b', instanceUid: 'b', node: 'node-b', degraded: ['valkey'], valkey: { ...sampleReplica().valkey!, state: 'degraded', reason: 'timeout' } }),
      ];
      system.infoResult = sampleInfo({ valkey: { ...sampleInfo().valkey, degradedReplicas: 2 } });
    });
    const t = text(root);
    expect(t).toContain('Degraded');
    expect(t).not.toContain('Replicas unready');
    expect(t).toContain('Degraded on 2 of 2 replicas');
    expect(t).toContain('change-log poll');
  });

  it('R-UI-038 R-UI-172 an unready replica names its failing checks beside its state, a space after the colon', async () => {
    const { root } = await render((system) => {
      system.replicasResult = [
        sampleReplica({ state: 'unready', checks: { database: { status: 'fail' }, schema: { status: 'fail' } }, degraded: ['valkey'] }),
      ];
    });
    const state = root.querySelector('section.role tbody td.state-cell > .state')!;
    // The checks are the second column of the state's grid, so their first line is the row's (the layout check measures it).
    expect(state.querySelector(':scope > .bad + .checks')).toBeTruthy();
    const checks = [...state.querySelectorAll('.checks > .check')].map((c) => text(c as HTMLElement).trim());
    expect(checks[0]).toBe('Failing checks: database, schema');
    expect(checks[1]).toMatch(/^warning ?Valkey:/);
  });

  it('R-UI-038 two replicas on one node make the role not drain-safe', async () => {
    const { root } = await render((system) => {
      system.replicasResult = [sampleReplica({ node: 'n1' }), sampleReplica({ name: 'b', instanceUid: 'b', node: 'n1' })];
    });
    expect(text(root)).toContain('All ready replicas on one node');
    expect(text(root)).toContain('Not drain-safe');
  });

  describe('an update, a lost replica and a single replica (R-UI-038, Q-UI-600 to Q-UI-603)', () => {
    /** Two ready replicas of each main role on two nodes, and the summary the server folds from them. */
    function installation(system: FakeSystemApi, extra: Replica[] = []) {
      const one = (name: string, role: 'api' | 'packages' | 'worker', node: string, over: Partial<Replica> = {}) =>
        sampleReplica({ name, instanceUid: name, roles: [role], node, ...over });
      const replicas = [
        one('quader-api-0', 'api', 'n1'),
        one('quader-api-1', 'api', 'n2'),
        one('quader-packages-0', 'packages', 'n1'),
        one('quader-packages-1', 'packages', 'n2'),
        one('quader-worker-0', 'worker', 'n1'),
        one('quader-worker-1', 'worker', 'n2'),
        ...extra,
      ];
      system.replicasResult = replicas;
      const count = (role: string) => {
        const live = replicas.filter((r) => r.roles[0] === role && !r.lost);
        return { live: live.length, ready: live.filter((r) => r.state === 'ready').length, degraded: 0, nodes: 2 };
      };
      system.infoResult = sampleInfo({
        replicas: {
          live: replicas.filter((r) => !r.lost).length,
          ready: replicas.filter((r) => !r.lost && r.state === 'ready').length,
          versions: { '0.1.1': 6 },
          roles: { api: count('api'), packages: count('packages'), worker: count('worker') },
        },
      });
    }
    const summary = (root: HTMLElement) => root.querySelector<HTMLElement>('p.summary')!;

    it('R-UI-038 Q-UI-600 a replica that drains during an update is shown as draining and leaves the header ready', async () => {
      const { root } = await render((system) =>
        installation(system, [
          sampleReplica({
            name: 'quader-api-old',
            instanceUid: 'old',
            node: 'n1',
            version: '0.1.0',
            state: 'draining',
            checks: { database: { status: 'pass' }, draining: { status: 'fail' } },
          }),
        ]),
      );
      const head = text(summary(root));
      expect(head).toContain('All roles ready');
      expect(head).toContain('Replicas are being replaced (1 draining, 0 starting)');
      expect(head).not.toContain('Replicas unready');
      expect(summary(root).querySelector('.material-icons.bad, .material-icons.warn')).toBeNull();
      const row = [...root.querySelectorAll('tr')].find((tr) => tr.textContent?.includes('quader-api-old'))!;
      expect(text(row)).toContain('Draining');
      expect(text(row)).not.toContain('Failing checks');
    });

    it('R-UI-038 Q-UI-602 a replica lost without draining is a warning with its last heartbeat, and its row stays as lost', async () => {
      const { root } = await render((system) =>
        installation(system, [
          sampleReplica({ name: 'quader-api-crashed', instanceUid: 'crashed', node: 'n3', lost: true, heartbeatAt: '2026-11-18T09:12:00Z' }),
        ]),
      );
      const head = text(summary(root));
      expect(head).toContain('Lost without draining: quader-api-crashed, last heartbeat');
      expect(summary(root).querySelector('time')?.getAttribute('datetime')).toBe('2026-11-18T09:12:00Z');
      expect(summary(root).querySelector('.material-icons.bad')).toBeNull();
      expect(summary(root).querySelector('.material-icons.warn')).toBeTruthy();
      expect(head).not.toContain('All roles ready');
      const row = [...root.querySelectorAll('tr')].find((tr) => tr.textContent?.includes('quader-api-crashed'))!;
      expect(text(row)).toContain('Lost');
      expect(row.querySelector('.warn .material-icons')).toBeTruthy();
    });

    it('R-UI-038 Q-UI-603 one healthy replica of the role all reads all roles ready; not drain-safe only informs', async () => {
      const { root } = await render((system) => {
        system.replicasResult = [sampleReplica({ name: 'quader-dev', instanceUid: 'dev', roles: ['all'], node: null })];
        system.infoResult = sampleInfo({
          replicas: { live: 1, ready: 1, versions: { dev: 1 }, roles: { all: { live: 1, ready: 1, degraded: 0, nodes: 0 } } },
        });
      });
      const head = text(summary(root));
      expect(head).toContain('All roles ready');
      expect(head).toContain('Not drain-safe');
      expect(summary(root).querySelector('.material-icons.bad, .material-icons.warn')).toBeNull();
      const note = [...root.querySelectorAll('p.warning')].find((p) => p.textContent?.includes('Fewer than two ready replicas'))!;
      expect(note.querySelector('.material-icons')?.textContent).toBe('info');
    });
  });

  it('R-UI-038 the key card marks a key with sealed values as re-encryption pending and offers Re-encrypt now', async () => {
    const { root, fixture } = await render((_s, _e, session) => {
      session.meResult = { capabilities: ['content:bypass', 'settings:read'] };
    });
    expect(text(root)).toContain('Re-encryption pending');
    const button = [...root.querySelectorAll('button')].find((b) => b.textContent?.includes('Re-encrypt now'));
    expect(button).toBeTruthy();
    button?.click();
    await fixture.whenStable();
    expect((TestBed.inject(EncryptionApi) as FakeEncryptionApi).reencryptKeys.length).toBe(1);
  });

  it('R-UI-038 Re-encrypt now is disabled with its reason while the rings are incompatible', async () => {
    const { root } = await render((_s, enc) => {
      enc.statusResult = { ...(enc.statusResult as object), ringsCompatible: false } as never;
    });
    const button = [...root.querySelectorAll('button')].find((b) => b.textContent?.includes('Re-encrypt now'));
    expect(button?.disabled).toBe(true);
    expect(text(root)).toContain('Disabled while the key rings are not compatible');
  });

  it('R-UI-038 a user without the admin role does not get Re-encrypt now', async () => {
    const { root } = await render((_s, _e, session) => {
      session.meResult = { capabilities: ['settings:read'] };
    });
    expect([...root.querySelectorAll('button')].some((b) => b.textContent?.includes('Re-encrypt now'))).toBe(false);
  });

  it('R-UI-038 a failing key-ring endpoint shows its own error and leaves the rest of the screen alone', async () => {
    const { root } = await render((_s, enc) => {
      enc.statusResult = new ApiError(404, { code: 'key_ring_unreadable', detail: 'no such endpoint', requestId: 'rid-9' }, 'rid-9');
    });
    expect(root.querySelector('p.summary[role="status"]')).toBeTruthy();
    expect(text(root)).toContain('rid-9');
  });

  it('R-UI-038 R-UI-164 the page\'s pause stops the automatic refresh of the tab (WCAG 2.2.2)', async () => {
    vi.useFakeTimers({ toFake: ['setInterval', 'clearInterval'] });
    const { fixture } = await render();
    const system = TestBed.inject(SystemApi) as FakeSystemApi;
    const spy = vi.spyOn(system, 'info');
    vi.advanceTimersByTime(15_000);
    await fixture.whenStable();
    expect(spy).toHaveBeenCalledTimes(1);

    // The control is the page's header's (system-page.spec.ts); the tab reads the page's pause.
    TestBed.inject(SystemRefresh).togglePause();
    await fixture.whenStable();
    vi.advanceTimersByTime(60_000);
    await fixture.whenStable();
    expect(spy).toHaveBeenCalledTimes(1);
    vi.useRealTimers();
  });

  it('R-OPS-048 Q-UI-897 the scanner replicas are one more group of rows after the roles, a sub-group per kind with its tool, and a kind with no ready replica is a warning of the header', async () => {
    const { root } = await render((system) => {
      system.scannersResult = sampleScanners({
        items: [
          sampleScannerReplica(),
          sampleScannerReplica({ instanceUid: 't2', name: 'quader-vuln-scanner-trivy-7d9f6c5b8-fghij', node: 'node-b' }),
          sampleScannerReplica({ kind: 'clamav', instanceUid: 'c1', name: 'quader-malware-scanner-clamav-6d9c7b5f4-x2v4q', state: 'starting', reason: 'database', toolVersion: '1.5.5', slots: { inUse: 0, total: 2 } }),
        ],
      });
    });
    const group = root.querySelector<HTMLElement>('[data-testid="scanner-replicas"]')!;
    const spoken = (h: Element) => [...h.children].map((c) => c.textContent?.trim()).join(' ');
    expect(spoken(group.querySelector('h3')!)).toBe('scanner live 3, ready 2');
    expect([...group.querySelectorAll('section.pool > h4')].map(spoken)).toEqual([
      'vuln-scanner-trivy live 2, ready 2',
      'malware-scanner-clamav live 1, ready 0',
    ]);
    expect(text(group)).toContain('Trivy 0.67.2');
    expect(text(group)).toContain('No database loaded yet');
    const summary = text(root.querySelector('p.summary')!);
    expect(summary).toContain('No ready replica of malware-scanner-clamav');
    expect(summary).not.toContain('No ready replica of vuln-scanner-trivy');
  });

  it('Q-UI-915 the image scanners say per pod which database it holds, and the group informs while ready pods do not hold the database in use', async () => {
    const current = { version: 'a1b2c3d4e5f6'.padEnd(64, '0'), builtAt: '2026-11-21T07:02:00Z' };
    const behind = await render((system) => {
      system.scannersResult = sampleScanners({
        items: [
          sampleScannerReplica({ database: current }),
          sampleScannerReplica({ instanceUid: 't2', name: 'quader-vuln-scanner-trivy-7d9f6c5b8-fghij', database: { version: 'b'.repeat(64), builtAt: null } }),
          sampleScannerReplica({ instanceUid: 't3', name: 'quader-vuln-scanner-trivy-7d9f6c5b8-klmno', state: 'starting', reason: 'starting', database: null }),
        ],
      });
    });
    const group = behind.root.querySelector<HTMLElement>('[data-testid="scanner-replicas"]')!;
    const note = group.querySelector<HTMLElement>('[data-testid="scanner-database-lag"]')!;
    expect(text(note)).toContain('1 of 2 ready replicas does not hold the database in use yet');
    // Informing, not a warning: every new database leaves the pods on the one before for the seconds of the load.
    expect(note.querySelector('.material-icons')!.textContent).toBe('info');
    expect([...group.querySelectorAll('.database-state')].map((c) => word(c.querySelector<HTMLElement>('.ok, .badge')!))).toEqual([
      'Database in use',
      'Older database',
      'No database yet',
    ]);
    // The header warns of no image scanner here: the pods are ready, and the note and the rows say the rest.
    expect(text(behind.root.querySelector('p.summary')!)).not.toContain('vuln-scanner-trivy');
  });

  it('Q-UI-915 while every ready image scanner holds the database in use, the group has no note', async () => {
    const current = { version: 'a1b2c3d4e5f6'.padEnd(64, '0'), builtAt: '2026-11-21T07:02:00Z' };
    const current2 = await render((system) => {
      system.scannersResult = sampleScanners({
        items: [sampleScannerReplica({ database: current }), sampleScannerReplica({ instanceUid: 't2', database: current })],
      });
    });
    expect(current2.root.querySelector('[data-testid="scanner-database-lag"]')).toBeNull();
  });

  it('R-OPS-048 Q-UI-897 the role all runs no scanner pod, and the page says so instead of an empty table', async () => {
    const { root } = await render((system) => {
      system.replicasResult = [sampleReplica({ name: 'quader-all-0', roles: ['all'] })];
      system.scannersResult = sampleScanners({
        kinds: [
          { kind: 'trivy', deployment: 'vuln-scanner-trivy', purpose: 'vuln', tool: 'Trivy', replicas: 'none' },
          { kind: 'clamav', deployment: 'malware-scanner-clamav', purpose: 'malware', tool: 'ClamAV', replicas: 'none' },
        ],
        items: [],
      });
    });
    const group = root.querySelector<HTMLElement>('[data-testid="scanner-replicas"]')!;
    expect(group.querySelector('table')).toBeNull();
    expect(text(group.querySelector('[data-testid="scanners-none-trivy"]') as HTMLElement)).toContain('vulnerabilities.images.scanner.url is empty');
    expect(text(group.querySelector('[data-testid="scanners-none-clamav"]') as HTMLElement)).toContain('malware.scanner.url is empty');
    expect(text(root.querySelector('p.summary')!)).not.toContain('No ready replica of');
  });

  it('R-OPS-048 a failure of the scanners is shown in their place and leaves the roles', async () => {
    const { root } = await render((system) => {
      system.scannersResult = new ApiError(503, { code: 'database_unavailable', requestId: 'rid-sc' }, 'rid-sc');
    });
    expect(root.querySelector('[data-testid="scanner-replicas"]')).toBeNull();
    expect(text(root)).toContain('rid-sc');
    expect(root.querySelectorAll('section.role').length).toBe(3);
  });

  it('R-UI-038 Q-UI-144 the installation card links the notices of the bundled software next to the version', async () => {
    const { root } = await render();
    const link = root.querySelector<HTMLAnchorElement>('section[aria-labelledby="card-install"] a[href="3rdpartylicenses.txt"]');
    expect(link?.textContent?.trim()).toBe('3rdpartylicenses.txt');
  });
});
