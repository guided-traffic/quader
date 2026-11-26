import { describe, expect, it } from 'vitest';
import { sampleConnection, sampleOverview } from '../../../api/testing/fakes-storage';
import { capacityUse, connectionState, percentOf, problemsNow, roleOf, tileTone, tileWord, whatQuaderEncrypts } from './storage-model';

const TIB = 1024 ** 4;

describe('The words of Settings › Storage (R-UI-262 to R-UI-264)', () => {
  it('R-UI-263 a share is rounded down, so a connection with a blob missing never holds 100 %', () => {
    expect([percentOf(1), percentOf(0.996), percentOf(0.5), percentOf(0.0001), percentOf(0)]).toEqual([100, 99, 50, 1, 0]);
    expect(connectionState(sampleConnection({ state: 'filling' }, { fillRatio: 0.25 }))).toEqual({ kind: 'filling', percent: 25 });
    expect(connectionState(sampleConnection({ state: 'filling', summary: null }))).toEqual({ kind: 'filling', percent: null });
  });

  it('R-UI-263 the use against a stated capacity is nearly full from 80 % and full from 95 %; without one it has no level', () => {
    const at = (share: number) => capacityUse(sampleConnection({ capacityBytes: 2 * TIB }, { presentBytes: Math.round(share * 2 * TIB) }));
    expect(at(0.5)?.level).toBe('ok');
    expect(at(0.85)?.level).toBe('nearly');
    expect(at(0.96)?.level).toBe('full');
    expect(capacityUse(sampleConnection())).toEqual({ used: 1_893_341_184_512, capacity: null, ratio: null, level: null });
    expect(capacityUse(sampleConnection({ summary: null }))).toBeNull();
  });

  it('R-UI-263 the role is the place in the list: a draining connection takes no upload, a detached one has no place', () => {
    expect(roleOf(sampleConnection({ position: 2 }))).toEqual({ uploads: 2, reads: 2 });
    expect(roleOf(sampleConnection({ position: 3, state: 'draining', drain: true }))).toEqual({ uploads: null, reads: 3 });
    expect(roleOf(sampleConnection({ position: null, state: 'detached' }))).toEqual({ uploads: null, reads: null });
  });

  it('R-UI-262 every state of the overview has its word and its tone, green only for a positive state (B.14)', () => {
    const rows: [Parameters<typeof tileWord>[0], string, string, string][] = [
      ['connections', 'healthy', 'Healthy', 'ok'],
      ['connections', 'degraded', 'Degraded', 'warn'],
      ['connections', 'unreachable', 'Unreachable', 'bad'],
      ['availability', 'redundant', 'Redundant', 'ok'],
      ['availability', 'catching_up', 'Catching up', 'neutral'],
      ['availability', 'at_risk', 'At risk', 'warn'],
      ['availability', 'one_store', 'One store', 'neutral'],
      ['encryption', 'encrypted', 'Encrypted', 'ok'],
      ['encryption', 'partly', 'Partly', 'warn'],
      ['encryption', 'provider_only', 'Provider only', 'neutral'],
      ['encryption', 'off', 'Off', 'neutral'],
      ['integrity', 'verified', 'Verified', 'ok'],
      ['integrity', 'checking', 'Checking', 'neutral'],
      ['integrity', 'behind', 'Behind', 'warn'],
      ['integrity', 'repairing', 'Repairing', 'warn'],
      ['integrity', 'damaged', 'Damaged', 'bad'],
      ['problems', 'none', 'None', 'ok'],
      ['problems', 'open', 'Open', 'warn'],
      ['problems', 'ongoing', 'Ongoing', 'bad'],
    ];
    expect(rows.map(([k, s]) => [k, s, tileWord(k, s), tileTone(k, s)])).toEqual(rows);
  });

  it('R-UI-262 the problems of now are the server\'s states: an unreachable connection or an unavailable blob is ongoing, a copy to repair open', () => {
    expect(problemsNow(sampleOverview())).toBe('none');
    const o = sampleOverview();
    expect(problemsNow({ ...o, integrity: { ...o.integrity, state: 'repairing', damagedNow: { copies: 3, blobs: 0 } } })).toBe('open');
    expect(problemsNow({ ...o, integrity: { ...o.integrity, state: 'damaged', damagedNow: { copies: 3, blobs: 1 } } })).toBe('ongoing');
    expect(
      problemsNow({ ...o, connections: { ...o.connections, state: 'degraded', unreachable: [{ id: 'default', circuitOpen: 1, configured: 3 }] } }),
    ).toBe('ongoing');
  });

  it('R-UI-264 what Quader encrypts names every family of the stored content, the artifacts by connection (H-2432)', () => {
    const rows = whatQuaderEncrypts([
      sampleConnection(),
      sampleConnection({ id: 'archive', encrypted: true }),
      sampleConnection({ id: 'gone', state: 'retired', encrypted: true }),
    ]);
    expect(rows.map((r) => [r.family, r.mark, r.connections.join(','), r.gap])).toEqual([
      ['artifacts', 'yes', 'archive', null],
      ['artifacts', 'no', 'default', 'H-1317'],
      ['upload-parts', 'yes', 'archive', null],
      ['upload-parts', 'no', 'default', null],
      ['upload-spool', 'no', '', 'H-2'],
      ['database-secrets', 'yes', '', null],
      ['database-other', 'no', '', null],
      ['backup-artifacts', 'partly', '', null],
      ['backup-database', 'no', '', 'H-48'],
      ['journal', 'plain', '', 'H-1316'],
      ['bastion-caches', 'partly', '', null],
    ]);
    // A plain installation has no encrypted connection to name, and no gap of a plain one beside it.
    expect(whatQuaderEncrypts([sampleConnection()]).filter((r) => r.family === 'artifacts')).toEqual([
      { family: 'artifacts', mark: 'no', connections: ['default'], gap: null },
    ]);
  });
});
