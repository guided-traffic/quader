import german from '../../locale/messages.de.xlf' with { loader: 'text' };
import { clearTranslations, loadTranslations } from '@angular/localize';
import {
  ANSIBLE_PRESETS,
  DEB_PRESETS,
  GENERIC_FORMAT,
  NPM_PRESETS,
  NUGET_PRESETS,
  OCI_FORMAT,
  OCI_PRESETS,
} from '../api/testing/fakes-catalog';
import { clientLabel } from '../features/setup/clients';
import { formatDisplayName, withFormatWords } from './format-names';
import { setupCommands } from './project-links';
import {
  isSystemRepository,
  isSystemRepositoryName,
  isUpstreamName,
  isUpstreamProject,
  mayServeClients,
  projectDescription,
  repositoryDescription,
  systemBadge,
  systemClientSentence,
  systemRepositoryNames,
  upstreamFirst,
  upstreamKeyOf,
  upstreamName,
  wellKnownName,
  wordedByProduct,
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
    expect(isSystemRepository('upstream', 'clamav')).toBe(true);
    expect(isSystemRepository('upstream', 'docker-hub')).toBe(false);
    // A team's repository of the same key is a repository like any other.
    expect(isSystemRepository('web', 'scanner-trivy')).toBe(false);
    expect(systemRepositoryNames()).toEqual(['upstream/scanner-trivy', 'upstream/clamav']);
  });

  it('R-REPO-075 R-UI-193 clamav is the one system repository that may serve clients, and has Set up client only while it does', () => {
    expect(mayServeClients('clamav')).toBe(true);
    expect(mayServeClients('scanner-trivy')).toBe(false);
    expect(mayServeClients(null)).toBe(false);
    expect(setupCommands('upstream/clamav', true)).toEqual(['/projects', 'upstream', 'repositories', 'clamav', 'setup']);
    expect(setupCommands('upstream/scanner-trivy', true)).toBeNull();
  });

  it('R-UI-192 no Set up client leads to a system repository: its commands are null, every other repository\'s its page …/setup', () => {
    expect(setupCommands('upstream/scanner-trivy')).toBeNull();
    expect(setupCommands('upstream/clamav')).toBeNull();
    expect(setupCommands('upstream/npmjs')).toEqual(['/projects', 'upstream', 'repositories', 'npmjs', 'setup']);
    expect(setupCommands('web/scanner-trivy')).toEqual(['/projects', 'web', 'repositories', 'scanner-trivy', 'setup']);
  });

  it('R-UI-193 a qualified name is a system repository\'s in the project upstream only; its mark is one word and its sentence names the work that reads it', () => {
    expect(isSystemRepositoryName('upstream/scanner-trivy')).toBe(true);
    expect(isSystemRepositoryName('upstream/clamav')).toBe(true);
    expect(isSystemRepositoryName('upstream/docker-hub')).toBe(false);
    expect(isSystemRepositoryName('web/scanner-trivy')).toBe(false);
    expect(isSystemRepositoryName('scanner-trivy')).toBe(false);
    expect(isSystemRepositoryName(null)).toBe(false);
    expect(systemBadge()).toBe('System');
    expect(systemClientSentence('clamav', 'file')).toBe("Only Quader's malware scan reads this file while ClamAV definition updates is off; clients cannot download it.");
    expect(systemClientSentence('scanner-trivy', 'version')).toBe("Only Quader's image scanning reads this version; clients cannot install it.");
  });

  it('R-UI-190 the built-in project\'s description is the UI\'s localized sentence, whatever is stored; every other project\'s is the stored one', () => {
    expect(projectDescription('upstream', 'Stored by the product')).toBe('The public registries this installation offers to every project.');
    expect(projectDescription('web', "The web team's repositories")).toBe("The web team's repositories");
    expect(projectDescription('web', undefined)).toBe('');
  });
});

/** The German targets of the translation file by unit id, for the units without a placeholder. */
function germanUnits(): Record<string, string> {
  const unit = /<unit id="([^"]+)">[\s\S]*?<target>([^<]*)<\/target>/g;
  return Object.fromEntries([...german.matchAll(unit)].map((m) => [m[1]!, m[2]!.replace(/&apos;/g, "'").replace(/&amp;/g, '&')]));
}

describe('A text the product writes in the reader\'s language (R-UI-272)', () => {
  const declared = [
    ...[NPM_PRESETS, NUGET_PRESETS, OCI_PRESETS, DEB_PRESETS, ANSIBLE_PRESETS].flat().map((p) => ({ key: p.id, stored: p.displayName })),
    { key: 'scanner-trivy', stored: 'Image scanning database' },
    { key: 'clamav', stored: 'ClamAV definitions' },
  ];

  it('R-UI-272 every repository the product declares has its words, the English ones its stored text; an administrator\'s text elsewhere stays as written', () => {
    for (const { key, stored } of declared) {
      expect(wordedByProduct('upstream', key), key).toBe(true);
      // The words by key, not the stored text: a stored text the product changed is not what the screen says.
      expect(repositoryDescription('upstream', key, 'Stored by the product'), key).toBe(stored);
    }
    expect(repositoryDescription('web', 'npmjs', 'Our npm mirror')).toBe('Our npm mirror');
    expect(wordedByProduct('web', 'npmjs')).toBe(false);
    expect(repositoryDescription('upstream', 'retired-preset', 'A retired preset')).toBe('A retired preset');
    expect(repositoryDescription('upstream', 'npmjs', null)).toBe('npm public registry');
    expect(repositoryDescription('web', 'npm-local', null)).toBe('');
    expect(wellKnownName('debian-security', 'Debian security updates')).toBe('Debian security updates');
  });

  it('R-UI-272 in German, the descriptive names and the project\'s description are German, the proper names as they are written', () => {
    loadTranslations(germanUnits());
    try {
      expect(repositoryDescription('upstream', 'debian-security', 'Debian security updates')).toBe('Debian-Sicherheitsupdates');
      expect(repositoryDescription('upstream', 'npmjs', 'npm public registry')).toBe('Öffentliche npm-Registry');
      expect(repositoryDescription('upstream', 'scanner-trivy', 'Image scanning database')).toBe('Datenbank der Image-Analyse');
      expect(repositoryDescription('upstream', 'clamav', 'ClamAV definitions')).toBe('ClamAV-Definitionen');
      expect(repositoryDescription('upstream', 'docker-hub', 'Docker Hub')).toBe('Docker Hub');
      expect(projectDescription('upstream', 'The public registries this installation offers to every project.')).toBe(
        'Die öffentlichen Registrys, die diese Installation jedem Projekt anbietet.',
      );
      expect(formatDisplayName('generic', 'Generic')).toBe('Generisch');
      expect(formatDisplayName('clamav', 'ClamAV definitions')).toBe('ClamAV-Definitionen');
      expect(formatDisplayName('oci', 'OCI (Docker)')).toBe('OCI (Docker)');
    } finally {
      clearTranslations();
    }
  });

  it('R-UI-272 a format is named by the UI for generic and clamav, by the server otherwise, by its key when the server names it not', () => {
    expect(formatDisplayName('generic', 'Generic')).toBe('Generic');
    expect(formatDisplayName('npm', 'npm')).toBe('npm');
    expect(formatDisplayName('cargo', null)).toBe('cargo');
    const [oci] = withFormatWords([{ ...OCI_FORMAT, upstreamPresets: [{ ...OCI_PRESETS[0]!, displayName: 'Stored' }] }]);
    expect(oci!.upstreamPresets[0]!.displayName).toBe('Docker Hub');
    expect(withFormatWords([GENERIC_FORMAT])[0]!.displayName).toBe('Generic');
  });

  it('R-UI-272 a client is named by the UI\'s table, the server\'s name only for a client the table does not know', () => {
    expect(clientLabel('nuget', 'nuget', 'nuget.exe')).toBe('NuGet CLI (nuget.exe)');
    expect(clientLabel('oci', 'docker', 'Docker CLI (with Buildx)')).toBe('Docker CLI (with Buildx)');
    expect(clientLabel('cargo', 'cargo', 'Cargo')).toBe('Cargo');
  });
});
