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
} from '../../api/testing/fakes';
import { EncryptionApi, SystemApi } from '../../api/system-api';
import { SessionStore } from '../../core/session-store';
import { SystemStatus } from './system-status';

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
    const state = root.querySelector('section.role tbody tr td:nth-child(4) > :is(.ok, .bad, .badge)')!;
    expect(state.firstElementChild!.classList).toContain('material-icons');
    expect(state.textContent!.replace(state.firstElementChild!.textContent!, '').trim()).toMatch(/^[A-Z]/);
  });

  it('R-UI-038 shows the summary, the installation facts and the replicas per role', async () => {
    const { root } = await render();
    const t = text(root);
    expect(t).toContain('All ready');
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
    ]);
    const worker = root.querySelectorAll<HTMLElement>('section.role')[2]!;
    const pools = [...worker.querySelectorAll('section.pool > h4')].map(spoken);
    expect(pools).toEqual(['Main live 2, ready 2, degraded 0', 'Pool vuln live 1, ready 1, degraded 0']);
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
    expect(headings).toEqual(['api', 'packages', 'worker', 'web']);
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

  it('R-UI-038 two replicas on one node make the role not drain-safe', async () => {
    const { root } = await render((system) => {
      system.replicasResult = [sampleReplica({ node: 'n1' }), sampleReplica({ name: 'b', instanceUid: 'b', node: 'n1' })];
    });
    expect(text(root)).toContain('All ready replicas on one node');
    expect(text(root)).toContain('Not drain-safe');
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
    expect(text(root)).toContain('All ready');
    expect(text(root)).toContain('rid-9');
  });

  it('R-UI-038 the pause control stops the automatic refresh (WCAG 2.2.2)', async () => {
    vi.useFakeTimers({ toFake: ['setInterval', 'clearInterval'] });
    const { root, fixture } = await render();
    const system = TestBed.inject(SystemApi) as FakeSystemApi;
    const spy = vi.spyOn(system, 'info');
    vi.advanceTimersByTime(15_000);
    await fixture.whenStable();
    expect(spy).toHaveBeenCalledTimes(1);

    const pause = root.querySelector<HTMLButtonElement>('header button[aria-pressed]');
    pause?.click();
    await fixture.whenStable();
    expect(pause?.getAttribute('aria-pressed')).toBe('true');
    vi.advanceTimersByTime(60_000);
    await fixture.whenStable();
    expect(spy).toHaveBeenCalledTimes(1);
    vi.useRealTimers();
  });

  it('R-UI-038 Q-UI-144 the installation card links the notices of the bundled software next to the version', async () => {
    const { root } = await render();
    const link = root.querySelector<HTMLAnchorElement>('section[aria-labelledby="card-install"] a[href="3rdpartylicenses.txt"]');
    expect(link?.textContent?.trim()).toBe('3rdpartylicenses.txt');
  });
});
