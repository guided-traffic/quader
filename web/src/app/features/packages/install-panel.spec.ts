import { ClientSetupApi, PackageVersion } from '../../api/packages-api';
import { ApiError } from '../../api/problem';
import { FakeClientSetupApi, FakePackagesApi } from '../../api/testing/fakes-packages';
import { Notifier } from '../../core/notifier';
import { button, expectAccessible, renderScreen, settle, text } from '../../testing/screen';
import { InstallPanel } from './install-panel';

async function version(v = '1.4.0'): Promise<PackageVersion> {
  return new FakePackagesApi().version('pkg_01JBQ6V7S3K9', v);
}

async function open(over: Partial<PackageVersion> = {}, name = '@team-a/ui') {
  const base = { ...(await version()), ...over };
  const r = await renderScreen(InstallPanel, { inputs: { format: 'npm', name, version: base } });
  return { ...r, fake: r.get(ClientSetupApi) as FakeClientSetupApi };
}

describe('Install command panel', () => {
  it('R-UI-009 shows the install command of the server for the package and version, and passes the accessibility check', async () => {
    const r = await open();
    expect((r.root.querySelector<HTMLInputElement>('quader-address-field input')?.value ?? r.root.querySelector('pre')?.textContent)).toBe('npm install @team-a/ui@1.4.0');
    expect(r.fake.calls).toHaveLength(1);
    expect(r.fake.calls[0]?.query).toMatchObject({ client: 'npm', package: '@team-a/ui', version: '1.4.0' });
    await expectAccessible(r.root);
  });

  it('R-UI-009 the install command carries no credentials: the request asks for none and the text holds no token placeholder', async () => {
    const r = await open();
    expect(r.fake.calls.every((c) => c.query.credentials === 'none')).toBe(true);
    expect(text(r.root)).not.toContain('{{token}}');
    expect(text(r.root)).not.toContain('QUADER_TOKEN');
    expect(text(r.root)).not.toContain('qdr_');
  });

  it('R-UI-009 the client picker offers npm, pnpm, Yarn Berry and Yarn Classic and switches the command', async () => {
    const r = await open();
    const select = r.root.querySelector<HTMLSelectElement>('select')!;
    expect([...select.options].map((o) => o.value)).toEqual(['npm', 'pnpm', 'yarn', 'yarn-classic']);
    for (const [client, command] of [
      ['pnpm', 'pnpm add @team-a/ui@1.4.0'],
      ['yarn', 'yarn add @team-a/ui@1.4.0'],
      ['yarn-classic', 'yarn add @team-a/ui@1.4.0'],
    ] as const) {
      select.value = client;
      select.dispatchEvent(new Event('change', { bubbles: true }));
      await settle(r.fixture);
      expect((r.root.querySelector<HTMLInputElement>('quader-address-field input')?.value ?? r.root.querySelector('pre')?.textContent), client).toBe(command);
    }
  });

  it('R-UI-009 a version in more than one repository offers a picker that starts at a virtual repository containing it', async () => {
    const r = await open();
    const pickers = r.root.querySelectorAll('select');
    expect(pickers).toHaveLength(2);
    expect(pickers[1]?.value).toBe('web/npm-all');
    expect(r.fake.calls[0]?.repository).toBe('web/npm-all');
    pickers[1]!.value = 'web/npm-local';
    pickers[1]!.dispatchEvent(new Event('change', { bubbles: true }));
    await settle(r.fixture);
    expect(r.fake.calls.at(-1)?.repository).toBe('web/npm-local');
  });

  it('R-UI-009 without a virtual repository the first local one is the default; with one repository there is no picker', async () => {
    const local = await open({
      repositories: [
        { key: 'web/npm-remote', class: 'remote', publishedAt: '2026-10-20T09:12:44.512Z', available: true },
        { key: 'web/npm-local', class: 'local', publishedAt: '2026-10-20T09:12:44.512Z', available: true },
      ],
    });
    expect(local.fake.calls[0]?.repository).toBe('web/npm-local');
    const single = await open({ repositories: [{ key: 'web/npm-local', class: 'local', publishedAt: '2026-10-20T09:12:44.512Z', available: true }] });
    expect(single.root.querySelectorAll('select')).toHaveLength(1);
  });

  it('R-UI-009 R-API-030 a name the shell would interpret arrives quoted from the server and is shown as it is', async () => {
    const r = await open({}, 'a(b)');
    expect((r.root.querySelector<HTMLInputElement>('quader-address-field input')?.value ?? r.root.querySelector('pre')?.textContent)).toBe(`npm install 'a(b)@1.4.0'`);
  });

  it('R-UI-009 a command of more than three lines is collapsed behind an expander', async () => {
    const lines = ['yarn add a', 'yarn add b', 'yarn add c', 'yarn add d', 'yarn add e'].join('\n');
    const r = await renderScreen(InstallPanel, {
      inputs: { format: 'npm', name: '@team-a/ui', version: await version() },
      setup: (get) => {
        const fake = get(ClientSetupApi) as FakeClientSetupApi;
        const real = fake.get.bind(fake);
        fake.get = async (repository, query) => {
          const answer = await real(repository, query);
          return { ...answer, snippets: answer.snippets.map((s) => (s.tab === 'install' ? { ...s, content: lines } : s)) };
        };
      },
    });
    expect(r.root.querySelector('pre')?.style.getPropertyValue('--lines')).toBe('3');
    const expander = button(r.root, 'Show the whole command');
    expect(expander.getAttribute('aria-expanded')).toBe('false');
    expander.click();
    await settle(r.fixture);
    expect(r.root.querySelector('pre')?.style.getPropertyValue('--lines')).toBe('5');
    expect((r.root.querySelector<HTMLInputElement>('quader-address-field input')?.value ?? r.root.querySelector('pre')?.textContent)).toBe(lines);
    expect(button(r.root, 'Show less').getAttribute('aria-expanded')).toBe('true');
    await expectAccessible(r.root);
  });

  it('R-UI-009 the copy button copies the whole command and a live region announces it', async () => {
    const written: string[] = [];
    Object.defineProperty(navigator, 'clipboard', {
      configurable: true,
      value: { writeText: async (t: string) => void written.push(t) },
    });
    const r = await open();
    r.root.querySelector<HTMLButtonElement>('quader-copy-button button')!.click();
    await settle(r.fixture);
    expect(written).toEqual(['npm install @team-a/ui@1.4.0']);
    expect(r.get(Notifier).notices().map((n) => n.text)).toContain('Copied to the clipboard.');
  });

  it('R-UI-009 R-UI-126 links to Set up client of the chosen repository with the chosen client', async () => {
    const r = await open();
    const link = [...r.root.querySelectorAll('a')].find((a) => a.textContent?.trim() === 'Set up client');
    expect(link?.getAttribute('href')).toBe('/projects/web/repositories/npm-all/setup?client=npm');
  });

  it('R-UI-009 a failing setup answer is shown with a retry, not as an empty panel', async () => {
    const r = await renderScreen(InstallPanel, {
      inputs: { format: 'npm', name: '@team-a/ui', version: await version() },
      setup: (get) => ((get(ClientSetupApi) as FakeClientSetupApi).failure = new ApiError(503, { code: 'unavailable' }, null)),
    });
    expect(text(r.root)).toContain('temporarily unavailable');
    button(r.root, 'Retry').click();
    await settle(r.fixture);
    expect((r.root.querySelector<HTMLInputElement>('quader-address-field input')?.value ?? r.root.querySelector('pre')?.textContent)).toBe('npm install @team-a/ui@1.4.0');
  });

  it('R-UI-009 a format without client instructions says so', async () => {
    const r = await renderScreen(InstallPanel, {
      inputs: { format: 'maven', name: 'org.example:lib', version: await version() },
    });
    expect(text(r.root)).toContain('no install instructions for this package type yet');
  });

  it('R-UI-193 Q-UI-898 Q-UI-899 a system repository is never offered: a version only it holds says why and asks for no command, one beside it is left out of the picker', async () => {
    const at = '2026-11-21T06:30:12.000Z';
    const only = await renderScreen(InstallPanel, {
      inputs: {
        format: 'oci',
        name: 'aquasec/trivy-db',
        version: { ...(await version()), version: '2', repositories: [{ key: 'upstream/scanner-trivy', class: 'remote', publishedAt: at, available: true }] },
      },
    });
    expect(text(only.root.querySelector('[data-testid="install-system"]')!)).toBe("Only Quader's image scanning reads this version; clients cannot install it.");
    expect(only.root.querySelector('quader-install-command, select, a')).toBeNull();
    expect((only.get(ClientSetupApi) as FakeClientSetupApi).calls).toEqual([]);
    await expectAccessible(only.root);

    const beside = await open({
      repositories: [
        { key: 'upstream/scanner-trivy', class: 'remote', publishedAt: at, available: true },
        { key: 'web/npm-local', class: 'local', publishedAt: at, available: true },
      ],
    });
    // One repository a client can use: no picker, and the command and Set up client are that one's.
    expect(beside.root.querySelectorAll('select')).toHaveLength(1);
    expect(beside.fake.calls.map((c) => c.repository)).toEqual(['web/npm-local']);
    expect(beside.root.querySelector('.setup-link a')?.getAttribute('href')).toContain('/projects/web/repositories/npm-local/setup');
  });

  it('Q-UI-965 a version held only by disabled repositories names them in one sentence and asks for no command', async () => {
    const at = '2026-11-21T06:30:12.000Z';
    const many = await open({
      repositories: [
        { key: 'web/npm-local', class: 'local', publishedAt: at, available: false },
        { key: 'web/npm-remote', class: 'remote', publishedAt: at, available: false },
        { key: 'web/npm-all', class: 'virtual', publishedAt: at, available: false },
      ],
    });
    expect(text(many.root.querySelector('[data-testid="install-disabled"]')!)).toBe(
      'The repositories web/npm-local, web/npm-remote are disabled: nobody can install this version until one of them is enabled again.',
    );
    expect(many.root.querySelector('quader-install-command, select, a')).toBeNull();
    expect(many.fake.calls).toEqual([]);
    await expectAccessible(many.root);

    // A virtual repository that serves the version through another member stays; the disabled one is left out of the picker.
    const through = await open({
      repositories: [
        { key: 'web/npm-local', class: 'local', publishedAt: at, available: false },
        { key: 'web/npm-remote', class: 'remote', publishedAt: at, available: true },
        { key: 'web/npm-all', class: 'virtual', publishedAt: at, available: true },
      ],
    });
    const pickers = through.root.querySelectorAll('select');
    expect([...pickers[1]!.options].map((o) => o.value)).toEqual(['web/npm-remote', 'web/npm-all']);
    expect(through.fake.calls[0]?.repository).toBe('web/npm-all');
  });
});
