import { PROFILE_FIGURES, sampleInstanceRules } from '../../api/testing/fakes-limits';
import { everyLabel, subjectReference, subjectTitle, whoView } from './callers';
import { readTexts, textsOf } from './figure-fields';
import { compactFigures, figureGroups, figureName, formatBytes, fromField, limitText, overridesSummary, toField } from './figures';
import { orderRules } from './limit-rules';
import { keyFromName } from './profile-editor';
import { overridesOf } from './rule-dialog';

describe('Download limits: figures and callers (R-UI-178, R-UI-179)', () => {
  it('R-UI-178 shows bytes as decimal MB and GB and reads a field back to bytes, rounded down', () => {
    expect(toField('cachedBytesPerHour', 300_000_000_000)).toBe('300');
    expect(toField('cachedBytesPerSecond', 200_000_000)).toBe('200');
    expect(toField('requestsPerHour', 50000)).toBe('50000');
    expect(toField('bytesPerDay', null)).toBe('');
    expect(fromField('cachedBytesPerHour', '250')).toEqual({ ok: true, value: 250_000_000_000 });
    expect(fromField('cachedBytesPerHour', '0,5')).toEqual({ ok: true, value: 500_000_000 });
    expect(fromField('cachedBytesPerSecond', '0.0000001')).toMatchObject({ ok: false });
    expect(fromField('requestsPerHour', '')).toEqual({ ok: true, value: null });
    expect(fromField('requestsPerHour', '1.5')).toMatchObject({ ok: false });
    expect(fromField('requestsPerHour', '0')).toMatchObject({ ok: false });
  });

  it('R-UI-178 says a profile in one compact line and leaves an unlimited figure out', () => {
    expect(compactFigures(PROFILE_FIGURES['standard']!, 'en-US').replaceAll(' ', ' ')).toBe(
      '200/s · 4,000 burst · 100,000/h · cache 250 MB/s, 300 GB/h · upstream 125 MB/s, 100 GB/h, 20,000 req/h · 250 parallel transfers · 1,000 GB/day · 1,000 refused/h',
    );
    expect(compactFigures(PROFILE_FIGURES['unlimited']!, 'en-US')).toBe('Unlimited');
    expect(compactFigures({ ...PROFILE_FIGURES['standard']!, requestsBurst: null, refusedPerHour: null }, 'en-US')).not.toContain('burst');
  });

  it('R-LIM-041 names the parallel figure transfers and says what one pull or install holds where it is set', () => {
    expect(figureName('parallelTransfers')).toBe('Parallel transfers');
    expect(limitText('parallelTransfers', 250, 'en-US')).toBe('250 transfers in parallel');
    const group = figureGroups().find((g) => g.figures.includes('parallelTransfers'));
    expect(group?.heading).toBe('Transfers');
    expect(group?.help).toContain('Each layer of an image and each package of an install is one transfer');
    expect(group?.help).toContain('a pull runs 3 to 6 at once');
  });

  it('R-LIM-004 a burst needs the rate and is at least the rate', () => {
    const texts = { ...textsOf(PROFILE_FIGURES['restricted']!), requestsBurst: '10' };
    expect(readTexts(texts).errors.requestsBurst).toBe('The burst is at least the requests per second.');
    expect(readTexts({ ...texts, requestsPerSecond: '' }).errors.requestsBurst).toBe('A burst needs requests per second.');
    expect(readTexts(textsOf(PROFILE_FIGURES['restricted']!)).errors).toEqual({});
  });

  it('R-UI-179 names the overrides after the profile, two at most, then how many more', () => {
    expect(overridesSummary({ uncachedBytesPerHour: 20_000_000_000 }, 'en-US')).toEqual(['+ uncached 20 GB/h']);
    expect(overridesSummary({ requestsPerHour: 10, parallelTransfers: 100, bytesPerDay: null, refusedPerHour: 5 }, 'en-US')).toEqual([
      '+ requests 10/h',
      '+ parallel transfers 100',
      '+ 2 more',
    ]);
  });

  it('R-UI-179 writes only the figures the person changed against the profile', () => {
    const base = PROFILE_FIGURES['restricted']!;
    const figures = { ...base, uncachedBytesPerHour: 40_000_000_000, parallelTransfers: 64 };
    expect(overridesOf(figures, new Set(['uncachedBytesPerHour', 'parallelTransfers']), base)).toEqual({ uncachedBytesPerHour: 40_000_000_000 });
    expect(overridesOf({ ...base, bytesPerDay: null }, new Set(['bytesPerDay']), base)).toEqual({ bytesPerDay: null });
  });

  it('R-LIM-013 R-UI-179 orders the rules as resolution tries them: single callers, groups, kinds', () => {
    const order = orderRules(sampleInstanceRules()).map((r) => r.who.kind);
    expect(order.slice(0, 2).sort()).toEqual(['address', 'user']);
    expect(order[2]).toBe('group');
    expect(order.slice(3).every((k) => k === 'every')).toBe(true);
  });

  it('R-UI-179 R-UI-184 names every kind of caller', () => {
    expect(everyLabel('user')).toBe('Every signed-in user');
    expect(whoView({ kind: 'address', prefix: '2001:db8:1:2::/64' })).toMatchObject({ mono: true, primary: '2001:db8:1:2::/64' });
    expect(whoView({ kind: 'group', group: 'gone', deleted: true }).secondary).toBe('(deleted)');
    // Q-UI-765: the title names the thing, the reference stands under it.
    expect(subjectTitle({ kind: 'user', id: 'u', name: 'priya', displayName: 'Priya Raman', deleted: false })).toBe('Priya Raman');
    expect(subjectReference({ kind: 'user', id: 'u', name: 'priya', displayName: 'Priya Raman', deleted: false })).toBe('@priya');
    expect(subjectTitle({ kind: 'technicalToken', id: 't', name: 'deploy-prod', account: 'sa:web/partner', level: 'project', deleted: false })).toBe('deploy-prod');
    expect(subjectReference({ kind: 'technicalToken', id: 't', name: 'deploy-prod', account: 'sa:web/partner', level: 'project', deleted: false })).toBeNull();
    expect(subjectTitle({ kind: 'ci', id: 'g:4711', issuer: 'g', projectId: '4711', projectPath: 'acme/web' })).toBe('acme/web');
    expect(subjectReference({ kind: 'ci', id: 'g:4711', issuer: 'g', projectId: '4711', projectPath: 'acme/web' })).toBe('g');
    expect(subjectTitle({ kind: 'anonymous', id: '198.51.100.23', address: '198.51.100.23' })).toBe('198.51.100.23');
  });

  it('R-UI-178 suggests a key from the name and formats amounts', () => {
    expect(keyFromName('Partners with a contract')).toBe('partners-with-a-contract');
    expect(keyFromName('2nd tier')).toBe('nd-tier');
    expect(formatBytes(1_203_000_000, 'en-US')).toBe('1.2 GB');
    expect(formatBytes(512, 'en-US')).toBe('512 B');
    expect(limitText('cachedBytesPerHour', 300_000_000_000, 'en-US')).toBe('300 GB per hour');
  });
});
