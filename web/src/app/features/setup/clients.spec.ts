import german from '../../../locale/messages.de.xlf' with { loader: 'text' };
import { clientsOf, clientsOfKind, installFormLabel, snippetText } from './clients';
import { RENDERED_SNIPPET_IDS } from './snippet-ids.fixture';

/** A unit of the translation file: its id, the English source and the German target. */
interface Unit {
  readonly id: string;
  readonly en: string;
  readonly de: string;
}

const xmlText = (s: string): string =>
  s.replace(/&lt;/g, '<').replace(/&gt;/g, '>').replace(/&quot;/g, '"').replace(/&apos;/g, "'").replace(/&amp;/g, '&');

/** The labels of the folds of Set up client in both locales: the second ways (`setup.fold.*`) and **More options** (`setup.more`). */
function foldUnits(): Unit[] {
  const out: Unit[] = [];
  const unit = /<unit id="(setup\.fold\.[^"]+|setup\.more)">[\s\S]*?<source>([\s\S]*?)<\/source>\s*<target>([\s\S]*?)<\/target>/g;
  for (const m of german.matchAll(unit)) out.push({ id: m[1]!, en: xmlText(m[2]!), de: xmlText(m[3]!) });
  return out;
}

describe('Snippet words', () => {
  for (const [client, ids] of Object.entries(RENDERED_SNIPPET_IDS)) {
    it(`R-UI-016 R-API-030 every snippet id the API renders for ${client} is known to the page`, () => {
      expect(ids.filter((id) => snippetText(id) === null)).toEqual([]);
    });
  }

  it('R-NUGET-037 every install form of a client with several has a name for the select Form, short enough for 390 px', () => {
    for (const ids of Object.values(RENDERED_SNIPPET_IDS)) {
      // A format offers its further install snippets as forms by naming them; Ansible's requirements file stays on Set up client.
      const installs = ids.filter((id) => id.split('.')[1] === 'install' && installFormLabel(id) !== null);
      for (const id of installs) expect([...(installFormLabel(id) ?? '')].length, id).toBeLessThanOrEqual(32);
    }
    expect(installFormLabel('visual-studio.install.pmc')).toBe('Package Manager console');
    // Every install snippet of NuGet is a named form.
    const nuget = Object.entries(RENDERED_SNIPPET_IDS).filter(([client]) => client.startsWith('nuget:')).flatMap(([, ids]) => ids);
    expect(nuget.filter((id) => id.split('.')[1] === 'install' && id !== 'nuget.install' && installFormLabel(id) === null)).toEqual([]);
  });

  it('R-NUGET-037 a .NET tool and a template are installed by the .NET CLI alone, an MSBuild SDK not by nuget.exe', () => {
    const ids = (kind: string | null) => clientsOfKind('nuget', kind).map((c) => c.id);
    expect(ids(null)).toEqual(['dotnet', 'nuget', 'visual-studio']);
    expect(ids('dotnet-tool')).toEqual(['dotnet']);
    expect(ids('template')).toEqual(['dotnet']);
    expect(ids('msbuild-sdk')).toEqual(['dotnet', 'visual-studio']);
  });

  it('R-UI-121 a sentence says only what the window does not: none for an install, a short one for a file', () => {
    expect(snippetText('npm.install')?.caption).toBeNull();
    expect(snippetText('docker.install')?.caption).toBeNull();
    expect(snippetText('npm.configure.npmrc')?.caption).toBe('Save as `.npmrc` in your project.');
    for (const ids of Object.values(RENDERED_SNIPPET_IDS)) {
      for (const id of ids) expect(snippetText(id)?.caption?.split(/\s+/).length ?? 0, id).toBeLessThanOrEqual(20);
    }
  });

  it('R-UI-121 a second way of a step folds under its heading: the pull secret, the PowerShell variant, the .list file', () => {
    expect(snippetText('docker.configure.kubernetes')?.fold).toBe('For a Kubernetes cluster');
    expect(snippetText('dotnet.configure.powershell')?.fold).toBe('In PowerShell');
    expect(snippetText('apt.configure.list')?.fold).toBe('As a one-line .list file');
    expect(snippetText('docker.configure.shell')?.fold).toBeNull();
  });

  // A fold's toggle is a button, and a button's label stays on one line (R-UI-171): the render guard measures it at 390 px, but
  // only in English and only for the repositories the seed has. So every fold label is held here, in both locales, to a number of
  // characters (Q-UI-810). At 390 px a step is 350 px wide, the chevron and its gap take 24 px: 326 px for the label (the credential
  // row's More options has 292). Inter at 14 px, weight 600, sets the labels of this page at 5.9 to 7.7 px a character on a Mac,
  // and the runner's Chromium sets them wider: a label of 45 characters, 315 px on a Mac, broke at 326 px on the runner. 32
  // characters at 7.7 px plus a tenth are 271 px; the bound holds as long as a label averages less than 9.1 px a character
  // (292 / 32), which only runs of capitals and of "m" and "w" reach.
  const FOLD_LABEL_MAX = 32;

  it('R-UI-171 every fold label of every format and client fits one line at 390 px, in English and in German', () => {
    const folds = foldUnits();
    expect(folds.map((u) => u.id)).toContain('setup.more');
    expect(folds.length).toBeGreaterThanOrEqual(11);
    const tooLong = folds.flatMap((u) => [u.en, u.de].filter((label) => [...label].length > FOLD_LABEL_MAX).map((label) => `${u.id}: ${label}`));
    expect(tooLong).toEqual([]);
  });

  it('R-UI-171 every fold the page shows for a snippet the API renders is one of those labels', () => {
    const folds = foldUnits().filter((u) => u.id !== 'setup.more');
    const shown = new Set<string>();
    for (const ids of Object.values(RENDERED_SNIPPET_IDS)) {
      for (const id of ids) {
        for (const anonymous of [false, true]) {
          const fold = snippetText(id, { anonymous })?.fold;
          if (fold) shown.add(fold);
        }
      }
    }
    expect(shown.size).toBeGreaterThanOrEqual(9);
    expect([...shown].filter((label) => !folds.some((u) => u.en === label))).toEqual([]);
  });

  it('R-UI-123 an anonymous visitor of a public image is not told to sign in to pull, and gets no pull secret', () => {
    expect(snippetText('docker.configure.shell', { anonymous: true })?.caption).toBe(
      'Not needed to pull: this repository can be read without signing in.',
    );
    expect(snippetText('docker.configure.shell')?.caption).toBe('Sign in once on each machine.');
    expect(snippetText('docker.configure.kubernetes')?.signedIn).toBe(true);
  });

  it('R-UI-123 R-GENERIC-017 an anonymous reader of a file is told that the configuration sends no credential, not that it keeps a token off the command line', () => {
    for (const id of ['curl.configure.function', 'wget.configure.function', 'powershell.configure.headers']) {
      const anonymous = snippetText(id, { anonymous: true })?.caption ?? '';
      expect(anonymous).toContain('this repository can be read without signing in.');
      expect(anonymous).not.toContain('token');
      expect(snippetText(id)?.caption).toContain('The token stays');
    }
  });
});

describe('Ansible Galaxy client setup', () => {
  it('R-ANS-025 R-ANS-026 the format offers ansible-galaxy as its one client', () => {
    expect(clientsOf('ansible')).toEqual([{ id: 'ansible-galaxy', label: 'Ansible Galaxy (ansible-galaxy)', tab: 'ansible-galaxy' }]);
  });

  it('R-ANS-025 the snippets of every shell for the token variable are known, and the API renders exactly the ones the UI knows', () => {
    const ids = RENDERED_SNIPPET_IDS['ansible:ansible-galaxy'];
    expect(ids).toEqual([
      'ansible-galaxy.configure.ansible-cfg',
      'ansible-galaxy.configure.netrc',
      'ansible-galaxy.configure.token-env',
      'ansible-galaxy.configure.token-env-cmd',
      'ansible-galaxy.configure.token-env-powershell',
      'ansible-galaxy.install.collection',
      'ansible-galaxy.install.requirements',
      'ansible-galaxy.publish.collection',
    ]);
    expect(snippetText('ansible-galaxy.configure.netrc')?.fold).toBe('To install roles');
    expect(snippetText('ansible-galaxy.configure.token-env')?.caption).toContain('does not expand variables');
  });
});
