import { clientsOf, snippetText } from './clients';
import { RENDERED_SNIPPET_IDS } from './snippet-ids.fixture';

describe('Snippet words', () => {
  for (const [client, ids] of Object.entries(RENDERED_SNIPPET_IDS)) {
    it(`R-UI-016 R-API-030 every snippet id the API renders for ${client} is known to the page`, () => {
      expect(ids.filter((id) => snippetText(id) === null)).toEqual([]);
    });
  }

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

  it('R-UI-123 an anonymous visitor of a public image is not told to sign in to pull, and gets no pull secret', () => {
    expect(snippetText('docker.configure.shell', { anonymous: true })?.caption).toBe(
      'Not needed to pull: this repository can be read without signing in.',
    );
    expect(snippetText('docker.configure.shell')?.caption).toBe('Sign in once on each machine.');
    expect(snippetText('docker.configure.kubernetes')?.signedIn).toBe(true);
  });
});

describe('Ansible Galaxy client setup', () => {
  it('R-ANS-025 R-ANS-026 the format offers ansible-galaxy as its one client', () => {
    expect(clientsOf('ansible')).toEqual([{ id: 'ansible-galaxy', label: 'Ansible Galaxy (ansible-galaxy)' }]);
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
