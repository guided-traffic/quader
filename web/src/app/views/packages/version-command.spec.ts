import { PackageVersion } from '../../api/packages-api';
import { FakePackagesApi } from '../../api/testing/fakes-packages';
import { clientRepositories, versionCommand } from './version-command';

const at = '2026-11-21T06:30:12.000Z';

async function version(repositories?: PackageVersion['repositories']): Promise<PackageVersion> {
  const v = await new FakePackagesApi().version('pkg_01JBQ6V7S3K9', '1.4.0');
  return repositories ? { ...v, repositories } : v;
}

describe('What the head of a version page offers a client', () => {
  it('R-UI-009 the repositories a client can use start at a virtual one that holds the version, else the first local one', async () => {
    expect(clientRepositories(await version(), false)).toEqual(['web/npm-all', 'web/npm-local']);
    const noVirtual = await version([
      { key: 'web/npm-remote', class: 'remote', publishedAt: at, available: true },
      { key: 'web/npm-local', class: 'local', publishedAt: at, available: true },
    ]);
    expect(clientRepositories(noVirtual, false)).toEqual(['web/npm-local', 'web/npm-remote']);
    expect(versionCommand('npm', noVirtual, false)).toEqual({ kind: 'menu', repository: 'web/npm-local', repositories: ['web/npm-local', 'web/npm-remote'] });
  });

  it('R-UI-009 a version held only by disabled repositories names them in one sentence and has no button', async () => {
    const one = await version([
      { key: 'web/npm-local', class: 'local', publishedAt: at, available: false },
      { key: 'web/npm-all', class: 'virtual', publishedAt: at, available: false },
    ]);
    expect(versionCommand('npm', one, false)).toEqual({
      kind: 'note',
      cause: 'disabled',
      note: 'The repository web/npm-local is disabled: nobody can install this version until it is enabled again.',
    });
    const many = await version([
      { key: 'web/npm-local', class: 'local', publishedAt: at, available: false },
      { key: 'web/npm-remote', class: 'remote', publishedAt: at, available: false },
    ]);
    expect(versionCommand('npm', many, false)).toMatchObject({
      kind: 'note',
      note: 'The repositories web/npm-local, web/npm-remote are disabled: nobody can install this version until one of them is enabled again.',
    });
    // A disabled repository beside one that serves the version is left out.
    const beside = await version([
      { key: 'web/npm-local', class: 'local', publishedAt: at, available: false },
      { key: 'web/npm-remote', class: 'remote', publishedAt: at, available: true },
    ]);
    expect(versionCommand('npm', beside, false)).toEqual({ kind: 'menu', repository: 'web/npm-remote', repositories: ['web/npm-remote'] });
  });

  it('R-UI-193 a system repository is never offered: a version only it holds has the sentence why, one beside it is left out', async () => {
    const only = await version([{ key: 'upstream/scanner-trivy', class: 'remote', publishedAt: at, available: true }]);
    expect(versionCommand('oci', only, false)).toEqual({
      kind: 'note',
      cause: 'system',
      note: "Only Quader's image scanning reads this version; clients cannot install it.",
    });
    const beside = await version([
      { key: 'upstream/scanner-trivy', class: 'remote', publishedAt: at, available: true },
      { key: 'web/npm-local', class: 'local', publishedAt: at, available: true },
    ]);
    expect(clientRepositories(beside, false)).toEqual(['web/npm-local']);
  });

  it('R-UI-194 a version of the ClamAV definitions while the switch is on leads to the setup with freshclam; off, it says why', async () => {
    const held = await version([{ key: 'upstream/clamav', class: 'remote', publishedAt: at, available: true }]);
    expect(versionCommand('clamav', held, false)).toEqual({ kind: 'clamav', setup: ['/projects', 'upstream', 'repositories', 'clamav', 'setup'] });
    expect(versionCommand('clamav', held, true)).toMatchObject({ kind: 'note', cause: 'system' });
  });

  it('a format without clients offers nothing', async () => {
    expect(versionCommand('maven', await version(), false)).toEqual({ kind: 'none' });
  });
});
