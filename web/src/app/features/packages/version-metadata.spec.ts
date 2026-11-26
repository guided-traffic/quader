import { ansibleDependencies, debFacts, dependencyNames, licenseLink, npmDependencies, npmFacts, nugetDependencies } from './version-metadata';

/*
 * What each format feeds the blocks of a version's general properties (R-UI-221): the dependency groups in the format's words, the facts
 * of its Package details, a size in the unit a person reads. Pure functions; a member a publisher wrote in another type is skipped.
 */
const lines = (groups: ReturnType<typeof npmDependencies>) =>
  groups.map((g) => [g.label, ...g.entries.map((e) => e.alternatives.map((a) => `${a.written} ${a.qualifiers}`.trim()).join(' | ') + (e.mark ? ` (${e.mark})` : ''))]);

describe('The facts of a version, per format (R-UI-221)', () => {
  it('R-UI-221 npm: a group per field of the manifest in the order of an install, the bundled ones with their range, an optional peer marked', () => {
    const groups = npmDependencies({
      devDependencies: { vitest: '^3' },
      dependencies: { lit: '^3', tslib: 7 },
      peerDependencies: { react: '>=18', 'react-dom': '>=18' },
      optionalPeers: ['react-dom'],
      bundleDependencies: ['lit'],
      optionalDependencies: {},
    });
    expect(lines(groups)).toEqual([
      ['dependencies', 'lit ^3'],
      ['peerDependencies', 'react >=18', 'react-dom >=18 (optional)'],
      ['bundleDependencies', 'lit ^3'],
      ['devDependencies', 'vitest ^3'],
    ]);
    expect(dependencyNames(groups)).toEqual(['lit', 'react', 'react-dom', 'vitest']);
  });

  it('R-UI-221 NuGet: a group per target framework, one without a framework for all of them, an empty group kept', () => {
    const groups = nugetDependencies([
      { targetFramework: 'net8.0', dependencies: [{ id: 'Polly', range: '[8.0.0, )' }, { id: 5 }] },
      { dependencies: [{ id: 'System.Memory' }] },
      { targetFramework: 'net462', dependencies: [] },
      'junk',
    ]);
    expect(lines(groups)).toEqual([['net8.0', 'Polly [8.0.0, )'], ['All frameworks', 'System.Memory *'], ['net462']]);
    expect(nugetDependencies('not a list')).toEqual([]);
  });

  it('R-UI-221 Ansible Galaxy: one list without a heading', () => {
    expect(lines(ansibleDependencies({ dependencies: { 'community.general': '>=8.0.0' } }))).toEqual([[null, 'community.general >=8.0.0']]);
    expect(ansibleDependencies({})).toEqual([]);
  });

  it('R-UI-221 npm\'s Package details: the runtimes, the types, the tarball\'s facts in readable units, the install script and the publisher', () => {
    expect(
      npmFacts({ engines: { node: '>=20' }, types: 'index.d.ts', unpackedSize: 1_572_864, fileCount: 1200, hasInstallScript: true, publisher: 'ada' }),
    ).toEqual([
      ['Required runtimes', 'node >=20'],
      ['Types', 'index.d.ts'],
      ['Unpacked size', '1.5 MiB'],
      ['Files', '1,200'],
      ['Install script', 'Runs a script of the package when it is installed'],
      ['Publisher', 'ada'],
    ]);
    expect(npmFacts({ unpackedSize: 'big', fileCount: -1, hasInstallScript: 'yes' })).toEqual([]);
  });

  it('R-UI-221 Debian\'s Installed size in the unit a person reads, and none for a field that is no number', () => {
    expect(Object.fromEntries(debFacts({ installedSize: '18432' }))['Installed size']).toBe('18.0 MiB');
    expect(Object.fromEntries(debFacts({ installedSize: 'many' }))['Installed size']).toBeUndefined();
  });

  it('R-UI-221 a licence\'s address is a link only when it is http or https', () => {
    expect(licenseLink({ licenseUrl: 'https://licenses.nuget.org/MIT' })).toBe('https://licenses.nuget.org/MIT');
    expect(licenseLink({ licenseUrl: 'javascript:alert(1)' })).toBeNull();
    expect(licenseLink({})).toBeNull();
  });
});
