import { setupCommands } from './project-links';
import {
  isSystemRepository,
  isUpstreamName,
  isUpstreamProject,
  projectDescription,
  systemRepositoryNames,
  upstreamFirst,
  upstreamKeyOf,
  upstreamName,
} from './upstream-project';

describe('The built-in project upstream (R-UI-190)', () => {
  it('R-UI-190 is recognised by its key alone, and its repositories by the qualified name upstream/<key>', () => {
    expect(isUpstreamProject('upstream')).toBe(true);
    expect(isUpstreamProject('upstream-mirror')).toBe(false);
    expect(isUpstreamProject(null)).toBe(false);
    expect(isUpstreamName('upstream/npmjs')).toBe(true);
    expect(isUpstreamName('web/npm-local')).toBe(false);
    expect(upstreamKeyOf('upstream/docker-hub')).toBe('docker-hub');
    expect(upstreamKeyOf('npm-local')).toBeNull();
    expect(upstreamName('npmjs')).toBe('upstream/npmjs');
  });

  it('R-UI-190 a list of projects keeps upstream first and every other entry in its own order, whatever that order is', () => {
    const key = (p: { key: string }) => p.key;
    const sorted = [{ key: 'alpha' }, { key: 'upstream' }, { key: 'web' }];
    expect(upstreamFirst(sorted, key).map(key)).toEqual(['upstream', 'alpha', 'web']);
    const reversed = [{ key: 'web' }, { key: 'upstream' }, { key: 'alpha' }];
    expect(upstreamFirst(reversed, key).map(key)).toEqual(['upstream', 'web', 'alpha']);
    // A filter that took it out leaves it out.
    expect(upstreamFirst([{ key: 'web' }, { key: 'alpha' }], key).map(key)).toEqual(['web', 'alpha']);
  });

  it('R-UI-192 the system repositories are known by key in the project upstream only, and asked for by their qualified names', () => {
    expect(isSystemRepository('upstream', 'scanner-trivy')).toBe(true);
    expect(isSystemRepository('upstream', 'scanner-clamav')).toBe(true);
    expect(isSystemRepository('upstream', 'docker-hub')).toBe(false);
    // A team's repository of the same key is a repository like any other.
    expect(isSystemRepository('web', 'scanner-trivy')).toBe(false);
    expect(systemRepositoryNames()).toEqual(['upstream/scanner-trivy', 'upstream/scanner-clamav']);
  });

  it('Q-UI-896 no Set up client leads to a system repository: its commands are null, every other repository\'s its page …/setup', () => {
    expect(setupCommands('upstream/scanner-trivy')).toBeNull();
    expect(setupCommands('upstream/scanner-clamav')).toBeNull();
    expect(setupCommands('upstream/npmjs')).toEqual(['/projects', 'upstream', 'repositories', 'npmjs', 'setup']);
    expect(setupCommands('web/scanner-trivy')).toEqual(['/projects', 'web', 'repositories', 'scanner-trivy', 'setup']);
  });

  it('Q-UI-895 the built-in project\'s description is the UI\'s localized sentence, whatever is stored; every other project\'s is the stored one', () => {
    expect(projectDescription('upstream', 'Stored by the product')).toBe('The public registries this installation offers to every project.');
    expect(projectDescription('web', "The web team's repositories")).toBe("The web team's repositories");
    expect(projectDescription('web', undefined)).toBe('');
  });
});
