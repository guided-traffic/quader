import { ageDays, ageOldText, ageText, clamavFacts, isOutdated, setName, stateRank, verificationText } from './clamav-definitions';

const DAY = 86_400_000;
const now = Date.parse('2026-11-21T12:00:00Z');

describe('The ClamAV definitions on the package screens (R-UI-194, R-CLAMAV-003)', () => {
  it('R-UI-194 daily is Outdated past 7 days, as ClamAV warns, and main and bytecode never are', () => {
    const at = (ms: number): string => new Date(now - ms).toISOString();
    expect(isOutdated('daily', at(7 * DAY + 1000), now)).toBe(true);
    expect(isOutdated('daily', at(7 * DAY), now)).toBe(false);
    expect(isOutdated('daily', at(6 * DAY), now)).toBe(false);
    expect(isOutdated('daily', null, now)).toBe(false);
    expect(isOutdated('main', at(160 * DAY), now)).toBe(false);
    expect(isOutdated('bytecode', at(8 * DAY), now)).toBe(false);
    expect(ageDays(at(8 * DAY), now)).toBe(8);
    // The column's value reads beside its label Age; the version's page says it after the build time.
    expect([0, 1, 8].map(ageText)).toEqual(['under a day', '1 day', '8 days']);
    expect([0, 1, 8].map(ageOldText)).toEqual(['under a day old', '1 day old', '8 days old']);
  });

  it('R-CLAMAV-003 reads every field of the metadata, the members by name, and skips what has the wrong type', () => {
    const f = clamavFacts({
      database: 'daily',
      state: 'in_use',
      builtAt: '2026-11-21T07:00:00Z',
      buildTimeText: '21 Nov 2026 07-00 +0000',
      signatures: 2076903,
      functionalityLevel: 210,
      functionalityLevelText: 'ClamAV 1.5 or later',
      builder: '<b>raynman</b>',
      verification: { outcome: 'signature_verified', headerMd5: 'a', computedMd5: 'a', source: '' },
      membersRead: true,
      members: [{ name: 'daily.ndb', size: 3, sha256: 'x', lines: 2 }, { name: 'daily.cdb', size: 'big', sha256: 'y', lines: 1 }, { size: 1 }],
      detachedSignature: { present: true, name: 'daily-28190.cvd.sign', size: 1284, signatures: 2 },
    })!;
    expect(f.members.map((m) => m.name)).toEqual(['daily.cdb', 'daily.ndb']);
    expect(f.members[0]!.size).toBeNull();
    expect(f.builder).toBe('<b>raynman</b>');
    expect(f.detachedSignature).toEqual({ present: true, name: 'daily-28190.cvd.sign', size: 1284, signatures: 2 });
    expect(verificationText(f)).toBe("ClamAV's signature verified");
    expect(verificationText({ verification: 'not_verified_repository', source: 'ops/clamav-db' })).toBe('Not verified: from the repository ops/clamav-db');
    expect(verificationText({ verification: 'unsigned', source: 'ops/clamav-db' })).toBe('Unsigned container');
    expect(clamavFacts({ name: 'left-pad' })).toBeNull();
  });

  it('R-UI-194 names a set main, daily, bytecode, and orders the version in use first', () => {
    expect(setName([{ database: 'bytecode', version: '339' }, { database: 'daily', version: '28190' }, { database: 'main', version: '63' }])).toBe(
      'main-63.daily-28190.bytecode-339',
    );
    expect(['superseded', 'previous', 'in_use', 'files_removed'].sort((a, b) => stateRank(a as never) - stateRank(b as never))).toEqual([
      'in_use',
      'previous',
      'superseded',
      'files_removed',
    ]);
  });
});
