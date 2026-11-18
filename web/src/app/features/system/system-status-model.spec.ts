import { Replica, SystemInfo } from '../../api/system-api';
import { sampleInfo, sampleReplica } from '../../api/testing/fakes';
import {
  alarms,
  degradedChecks,
  failingChecks,
  keyStatus,
  LOST_REPLICA_LEVEL,
  mixedVersions,
  roleGroups,
  roleOf,
  sentinelMajority,
  serviceStatus,
  valkeySummary,
} from './system-status-model';

const web = (name: string, node: string | null, over = {}) =>
  sampleReplica({ name, instanceUid: name, node, ...over });
const worker = (name: string, node: string | null, over = {}) =>
  sampleReplica({ name, instanceUid: name, node, roles: ['worker'], ...over });
const packages = (name: string, node: string | null, over = {}) =>
  sampleReplica({ name, instanceUid: name, node, roles: ['packages'], ...over });

/** The summary of /system/info as the server folds the list (Summarise): lost replicas left out. */
function summary(replicas: readonly Replica[]): SystemInfo['replicas'] {
  const live = replicas.filter((r) => !r.lost);
  const roles: SystemInfo['replicas']['roles'] = {};
  for (const r of live) {
    const role = roleOf(r);
    const s = roles[role] ?? { live: 0, ready: 0, degraded: 0, nodes: 0 };
    roles[role] = { ...s, live: s.live + 1, ready: s.ready + (r.state === 'ready' ? 1 : 0) };
  }
  const ready = live.filter((r) => r.state === 'ready').length;
  return { live: live.length, ready, versions: { '0.1.1': live.length }, roles };
}

describe('system status model', () => {
  it('R-UI-038 a role with two ready replicas on two nodes is drain-safe', () => {
    const groups = roleGroups(sampleInfo(), [web('a', 'n1'), web('b', 'n2'), worker('w1', 'n1'), worker('w2', 'n2')]);
    expect(groups.find((g) => g.role === 'api')?.drainSafe).toBe(true);
    expect(groups.find((g) => g.role === 'api')?.warnings).toEqual([]);
    expect(groups.find((g) => g.role === 'api')?.pools[0]?.warnings).toEqual([]);
  });

  it('R-UI-038 fewer than two ready replicas is flagged and is not drain-safe', () => {
    const [apiGroup] = roleGroups(sampleInfo(), [web('a', 'n1'), web('b', 'n2', { state: 'unready' })]);
    expect(apiGroup?.pools[0]?.warnings).toContain('fewer-than-two-ready');
    expect(apiGroup?.drainSafe).toBe(false);
  });

  it('R-UI-038 all ready replicas on one node is flagged only when every replica reports its node', () => {
    const one = roleGroups(sampleInfo(), [web('a', 'n1'), web('b', 'n1')])[0];
    expect(one?.pools[0]?.warnings).toEqual(['one-node']);
    const unknown = roleGroups(sampleInfo(), [web('a', null), web('b', 'n1')])[0];
    expect(unknown?.pools[0]?.warnings).toEqual([]);
  });

  it('R-UI-038 a lost replica does not count as ready', () => {
    const g = roleGroups(sampleInfo(), [web('a', 'n1'), web('b', 'n2', { lost: true })])[0];
    expect(g?.pools[0]?.warnings).toContain('fewer-than-two-ready');
  });

  it('R-UI-038 Q-UI-107 the groups are api, packages and worker in that order, whatever the replicas are', () => {
    expect(roleGroups(sampleInfo(), []).map((g) => g.role)).toEqual(['api', 'packages', 'worker']);
    expect(roleGroups(sampleInfo(), [web('a', 'n1')]).map((g) => g.role)).toEqual(['api', 'packages', 'worker']);
  });

  it('R-UI-038 a live replica of the role all serves every role: no empty group of api, packages or worker, no warning', () => {
    const all = sampleReplica({ name: 'dev', instanceUid: 'dev', node: 'n1', roles: ['all'] });
    const groups = roleGroups(sampleInfo(), [all]);
    expect(groups.map((g) => g.role)).toEqual(['all']);
    expect(groups.flatMap((g) => g.warnings)).toEqual([]);
    // A lost one serves nothing: the main roles are back with their warnings.
    const lost = roleGroups(sampleInfo(), [{ ...all, lost: true }]);
    expect(lost.map((g) => g.role)).toEqual(['api', 'packages', 'worker', 'all']);
  });

  it('R-UI-038 Q-UI-107 a replica lands in the group of its own role', () => {
    const groups = roleGroups(sampleInfo(), [web('a', 'n1'), packages('p', 'n2'), worker('w', 'n3')]);
    expect(groups.map((g) => [g.role, g.replicas.map((r) => r.name)])).toEqual([
      ['api', ['a']],
      ['packages', ['p']],
      ['worker', ['w']],
    ]);
  });

  it('R-UI-038 no live worker replica is reported for the worker role, no live packages replica for packages', () => {
    const groups = roleGroups(sampleInfo(), [web('a', 'n1'), web('b', 'n2')]);
    expect(groups.find((g) => g.role === 'worker')?.warnings).toContain('no-live-worker');
    expect(groups.find((g) => g.role === 'packages')?.warnings).toContain('no-live-packages');
    expect(groups.find((g) => g.role === 'api')?.warnings).toEqual([]);
  });

  it('R-UI-038 a worker pool is a sub-group with its own counts and drain safety, and the role counts come from the info', () => {
    const info = sampleInfo({
      replicas: {
        live: 3,
        ready: 3,
        versions: { '0.1.1': 3 },
        roles: { worker: { live: 3, ready: 3, degraded: 0, nodes: 2 } },
      },
    });
    const [, , w] = roleGroups(info, [
      worker('w1', 'n1'),
      worker('w2', 'n2'),
      worker('v1', 'n1', { pool: 'vuln' }),
    ]);
    expect(w?.live).toBe(3);
    expect(w?.pools.map((p) => p.pool)).toEqual([null, 'vuln']);
    expect(w?.pools[0]).toMatchObject({ live: 2, ready: 2, drainSafe: true });
    expect(w?.pools[1]).toMatchObject({ live: 1, ready: 1, drainSafe: false });
    expect(w?.pools[1]?.warnings).toEqual(['fewer-than-two-ready']);
    expect(w?.drainSafe).toBe(false);
  });

  it('R-UI-038 a pool whose replicas are all lost is named in its warning', () => {
    const [, , w] = roleGroups(sampleInfo(), [
      worker('w1', 'n1'),
      worker('w2', 'n2'),
      worker('v1', 'n1', { pool: 'vuln', lost: true }),
    ]);
    expect(w?.pools[1]?.warnings).toContain('no-live-pool');
  });

  it('R-UI-038 Q-UI-107 the bridge role web and the development role all have a group only while a replica reports them', () => {
    expect(roleGroups(sampleInfo(), [web('a', 'n1')]).map((g) => g.role)).not.toContain('web');
    expect(roleGroups(sampleInfo(), [web('a', 'n1', { roles: ['web'] })]).map((g) => g.role)).toEqual([
      'api',
      'packages',
      'worker',
      'web',
    ]);
    expect(roleGroups(sampleInfo(), [web('a', 'n1', { roles: ['all'] })]).map((g) => g.role)).toContain('all');
  });

  it('R-UI-038 a role this screen does not know is shown after the known ones', () => {
    expect(roleGroups(sampleInfo(), [web('a', 'n1', { roles: ['fetch'] })]).map((g) => g.role).at(-1)).toBe('fetch');
  });

  it('R-UI-038 failing and degraded checks stay separate kinds', () => {
    const r = sampleReplica({
      state: 'unready',
      checks: { database: { status: 'fail' }, valkey: { status: 'degraded' } },
      degraded: ['objectStore'],
    });
    expect(failingChecks(r)).toEqual(['database']);
    expect(degradedChecks(r).sort()).toEqual(['objectStore', 'valkey']);
  });

  it('R-UI-038 the synthetic draining check is not listed among the failing checks: the state says it', () => {
    const r = sampleReplica({ state: 'draining', checks: { database: { status: 'pass' }, draining: { status: 'fail' } } });
    expect(failingChecks(r)).toEqual([]);
  });

  describe('the header (R-UI-038, Q-UI-600 to Q-UI-603)', () => {
    /** A healthy split installation: two ready replicas of each main role on two nodes. */
    const healthy = () => [
      web('a1', 'n1'),
      web('a2', 'n2'),
      packages('p1', 'n1'),
      packages('p2', 'n2'),
      worker('w1', 'n1'),
      worker('w2', 'n2'),
    ];
    const status = (replicas: Replica[]) =>
      serviceStatus(roleGroups(sampleInfo({ replicas: summary(replicas) }), replicas), replicas);
    const kinds = (replicas: Replica[]) => status(replicas).conditions.map((c) => c.kind);

    it('R-UI-038 every role ready on two nodes: no condition, the level is ok', () => {
      expect(status(healthy())).toEqual({ level: 'ok', conditions: [] });
    });

    it('R-UI-038 Q-UI-600 a replica that drains during an update informs and is no error', () => {
      const replicas = [...healthy(), web('a0', 'n1', { state: 'draining', version: '0.1.0' }), web('a3', 'n2', { state: 'starting' })];
      const st = status(replicas);
      expect(st.level).toBe('info');
      expect(st.conditions).toEqual([{ kind: 'replacing', level: 'info', draining: 1, starting: 1 }]);
      expect(alarms(st)).toEqual([]);
    });

    it('R-UI-038 Q-UI-602 a replica lost without draining while its role has ready replicas is a warning with its last heartbeat', () => {
      const lost = web('a0', 'n3', { lost: true, heartbeatAt: '2026-11-18T09:12:00Z' });
      const st = status([...healthy(), lost, worker('w0', null, { lost: true, heartbeatAt: '2026-11-18T08:00:00Z' })]);
      expect(st.level).toBe('warning');
      expect(st.conditions).toEqual([{ kind: 'lost', level: 'warning', replicas: ['a0', 'w0'], lastSeenAt: '2026-11-18T09:12:00Z' }]);
      expect(LOST_REPLICA_LEVEL).toBe('warning');
    });

    it('R-UI-038 Q-UI-602 a role without a ready replica is red, also when its only replica was lost', () => {
      const replicas = healthy().filter((r) => roleOf(r) !== 'worker');
      replicas.push(worker('w1', 'n1', { lost: true }));
      const st = status(replicas);
      expect(st.level).toBe('error');
      expect(st.conditions[0]).toEqual({ kind: 'no-ready-replica', level: 'error', roles: ['worker'] });
      expect(kinds(replicas)).toEqual(['no-ready-replica', 'lost', 'not-drain-safe']);
    });

    it('R-UI-038 Q-UI-602 a role whose replicas only drain or start has no ready replica and is red', () => {
      const replicas = [...healthy().filter((r) => roleOf(r) !== 'api'), web('a1', 'n1', { state: 'draining' })];
      expect(status(replicas).conditions[0]).toEqual({ kind: 'no-ready-replica', level: 'error', roles: ['api'] });
    });

    it('R-UI-038 a live replica that fails a gating check after it was ready stays red', () => {
      const replicas = [...healthy(), web('a3', 'n3', { state: 'unready', checks: { database: { status: 'fail' } } })];
      const st = status(replicas);
      expect(st.level).toBe('error');
      expect(st.conditions[0]).toEqual({ kind: 'unready', level: 'error', replicas: ['a3'] });
    });

    it('R-UI-038 a degraded check on a ready replica is a warning', () => {
      const replicas = healthy().map((r, i) => (i === 0 ? { ...r, degraded: ['valkey'] } : r));
      expect(status(replicas)).toEqual({ level: 'warning', conditions: [{ kind: 'degraded', level: 'warning' }] });
    });

    it('R-UI-038 Q-UI-603 one healthy replica of the role all is not a fault: it is not drain-safe, which only informs', () => {
      const all = [sampleReplica({ name: 'dev', instanceUid: 'dev', node: null, roles: ['all'] })];
      const st = status(all);
      expect(st.level).toBe('info');
      expect(st.conditions).toEqual([{ kind: 'not-drain-safe', level: 'info' }]);
      expect(alarms(st)).toEqual([]);
    });

    it('R-UI-038 the bridge role web with only a lost row left is no role the installation needs: a warning, not red', () => {
      const replicas = [...healthy(), web('w-old', 'n1', { roles: ['web'], lost: true })];
      const st = status(replicas);
      expect(st.level).toBe('warning');
      expect(kinds(replicas)).toEqual(['lost']);
    });

    it('R-UI-038 conditions are ordered worst first', () => {
      const replicas = [
        ...healthy().filter((r) => roleOf(r) !== 'packages'),
        web('a0', 'n1', { lost: true }),
        web('a3', 'n1', { state: 'draining' }),
      ];
      expect(kinds(replicas)).toEqual(['no-ready-replica', 'lost', 'replacing']);
    });
  });

  it('R-UI-038 the Valkey summary counts degraded replicas, flags primary disagreement and missing Sentinel majority', () => {
    const view = (primary: string, reachable = 3) => ({
      state: 'available' as const,
      reason: null,
      circuit: 'closed',
      primary,
      sentinelsReachable: reachable,
      sentinelsConfigured: 3,
      serverVersion: '9.0.0',
      lastPrimaryChangeAt: null,
    });
    const agree = valkeySummary(sampleInfo(), [web('a', 'n1', { valkey: view('p1') }), web('b', 'n2', { valkey: view('p1') })]);
    expect(agree.primariesDisagree).toBe(false);
    expect(agree.sentinelsUnreachable).toBe(false);

    const split = valkeySummary(sampleInfo(), [web('a', 'n1', { valkey: view('p1') }), web('b', 'n2', { valkey: view('p2', 1) })]);
    expect(split.primariesDisagree).toBe(true);
    expect(split.sentinelsUnreachable).toBe(true);

    const info = sampleInfo({ valkey: { ...sampleInfo().valkey, degradedReplicas: 2 } });
    expect(valkeySummary(info, [web('a', 'n1')]).degradedReplicas).toBe(2);
  });

  it('R-UI-038 a Sentinel majority is more than half', () => {
    expect([1, 2, 3, 5].map(sentinelMajority)).toEqual([1, 2, 2, 3]);
  });

  it('R-UI-038 two live versions produce the information notice, one does not', () => {
    const one = sampleInfo();
    expect(mixedVersions(one)).toEqual([]);
    const two = sampleInfo({ replicas: { ...one.replicas, versions: { '0.1.1': 1, '0.1.0': 1 } } });
    expect(mixedVersions(two)).toEqual(['0.1.0', '0.1.1']);
  });

  it('R-UI-038 a key that is not active but still seals values is re-encryption pending', () => {
    expect(keyStatus({ active: true, sealedValues: 4 })).toBe('active');
    expect(keyStatus({ active: false, sealedValues: 3 })).toBe('re-encryption-pending');
    expect(keyStatus({ active: false, sealedValues: 0 })).toBe('unused');
  });
});
