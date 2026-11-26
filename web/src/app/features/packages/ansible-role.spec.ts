import { PackagesApi } from '../../api/packages-api';
import { FakePackagesApi, roleFixture } from '../../api/testing/fakes-packages';
import { expectAccessible, inProject, renderScreen, settle, text } from '../../testing/screen';
import { PackageVersionPage } from './package-version';

async function openRole(readme: Parameters<typeof roleFixture>[1] = 'available', over: Parameters<typeof roleFixture>[2] = {}, tab?: string) {
  const r = await renderScreen(
    PackageVersionPage,
    inProject({
      inputs: { format: 'ansible', name: 'role/16', version: '2.0.0', ...(tab ? { tab } : {}) },
      setup: (get) => roleFixture(get(PackagesApi) as FakePackagesApi, readme, over),
    }),
  );
  await settle(r.fixture);
  return r;
}

const tabLabels = (root: HTMLElement) => [...root.querySelectorAll('[role="tab"]')].map((t) => text(t));
type Opened = Awaited<ReturnType<typeof openRole>>;
/** Opens the head's panel of the clients' commands (Q-UI-1241) and reads the command of the chosen client. */
async function command(r: Opened): Promise<string | undefined> {
  const button = r.root.querySelector<HTMLButtonElement>('quader-page-header quader-client-command-menu button');
  if (!button) return undefined;
  button.click();
  await settle(r.fixture);
  const panel = document.getElementById(button.getAttribute('aria-controls')!)!;
  const shown = panel.querySelector('[role="tabpanel"]:not([inert]) quader-client-command, .command-body quader-client-command')!;
  const value = shown.querySelector<HTMLInputElement>('quader-address-field input')?.value ?? shown.querySelector('pre')?.textContent ?? undefined;
  button.click();
  await settle(r.fixture);
  return value;
}

describe('The page of a role a remote read (R-ANS-056)', () => {
  it('R-ANS-056 is titled by the owner and the name, opens on the README, and installs the role with its own command', async () => {
    const r = await openRole();
    expect(text(r.root.querySelector('h1')!)).toBe('geerlingguy.pip 2.0.0');
    expect(tabLabels(r.root)).toEqual(['README', 'Files', 'Details']);
    expect(text(r.root.querySelector('quader-package-readme')!)).toContain('Ansible Role: Pip');
    expect(await command(r)).toBe('ansible-galaxy role install geerlingguy.pip,2.0.0');
    // Q-ANS-110: a role is published inside a collection, and the page says where to read how.
    const note = r.root.querySelector('[data-testid="role-publish-note"]')!;
    expect(text(note)).toContain('put it into the roles directory of a collection');
    expect(note.querySelector('a')?.getAttribute('href')).toContain('/help/user/ansible-collections#publish-a-role-put-it-into-a-collection');
    await expectAccessible(r.root);
  });

  it('R-ANS-056 the switch in the head lists the role\'s versions under its key, the address a lookup finds', async () => {
    const r = await openRole();
    r.root.querySelector<HTMLButtonElement>('quader-version-switch button')!.click();
    await settle(r.fixture);
    const hrefs = [...document.querySelectorAll<HTMLAnchorElement>('.version-menu a.menu-item')].map((a) => a.getAttribute('href'));
    expect(hrefs.length).toBeGreaterThan(1);
    expect(hrefs.every((h) => h?.startsWith('/projects/web/packages/ansible/role/16'))).toBe(true);
    expect(hrefs.at(-1)).toBe('/projects/web/packages/ansible/role/16/-/versions');
  });

  it('R-ANS-056 a role whose README is not there yet says why, in its README tab', async () => {
    for (const [state, words] of [
      ['queued', 'is fetching the README'],
      ['absent', 'The upstream has no README for this role.'],
      ['unavailable', 'larger than Quader keeps'],
      ['none', 'Quader has no README of this role yet'],
    ] as const) {
      const r = await openRole(state);
      expect(tabLabels(r.root)[0]).toBe('README');
      expect(r.root.querySelector('quader-package-readme')).toBeNull();
      expect(text(r.root.querySelector('[data-testid="role-readme-state"]')!)).toContain(words);
    }
  });

  it('R-ANS-056 a role the remote holds without its facts (cached before) is named by its package and says when its facts come', async () => {
    const r = await renderScreen(
      PackageVersionPage,
      inProject({
        inputs: { format: 'ansible', name: 'role/16', version: '2.0.0' },
        setup: (get) => {
          const fake = get(PackagesApi) as FakePackagesApi;
          roleFixture(fake, 'none', { name: 'role/16', metadata: {}, hasReadme: false });
          fake.packages = fake.packages.map((p) => (p.key === 'role/16' ? { ...p, name: 'role/16', description: null } : p));
        },
      }),
    );
    await settle(r.fixture);
    expect(text(r.root.querySelector('h1')!)).toBe('role/16 2.0.0');
    expect(tabLabels(r.root)[0]).toBe('README');
    expect(text(r.root.querySelector('[data-testid="role-readme-state"]')!)).toContain("come with the role's next lookup");
  });

  it('R-ANS-056 Details names the owner, the downloads at the upstream apart from Quader\'s, and the name the rules see with the way to the tab Upstream', async () => {
    const r = await openRole('available', {}, 'details');
    const facts = r.root.querySelector('[data-testid="role-facts"]')!;
    const pairs = [...facts.querySelectorAll('dt')].map((dt) => `${text(dt)}: ${text(dt.nextElementSibling!)}`);
    expect(pairs[0]).toBe('Owner: geerlingguy');
    expect(pairs).toContain('Downloads at the upstream: 30,832,123');
    expect(pairs).toContain('Dependencies: geerlingguy.repo-epel');
    const rules = pairs.find((p) => p.startsWith('Name in rules and permissions'))!;
    expect(rules).toContain('role/16');
    const link = facts.querySelector('a')!;
    expect(link.getAttribute('href')).toBe('/projects/web/repositories/galaxy/upstream?name=role%2F16');
    // The source repository is a link of the row under the facts (Q-UI-1313).
    expect(r.root.querySelector('quader-version-links a[href="https://github.com/geerlingguy/ansible-role-pip"]')).not.toBeNull();
    await expectAccessible(r.root);
  });

  it('R-CUR-014 R-ANS-056 a role the remote\'s rules do not deliver says so at the top, with the reason, and offers no install command', async () => {
    const r = await openRole('available', {
      repositories: [{ key: 'web/galaxy', class: 'remote', publishedAt: '2026-11-24T00:00:00.000Z', available: false, unavailableReason: 'blocked' }],
    });
    const note = r.root.querySelector('[data-testid="delivery-note"]')!;
    expect(text(note)).toContain('Not delivered by web/galaxy: a rule of the repository blocks it.');
    expect(note.querySelector('a')?.getAttribute('href')).toBe('/projects/web/repositories/galaxy/upstream?name=role%2F16');
    expect(await command(r)).toBeUndefined();
    expect(text(r.root.querySelector('[data-testid="install-undelivered"]')!)).toContain('No repository delivers this version');
    expect(r.root.querySelector('[data-testid="install-disabled"]')).toBeNull();
    await expectAccessible(r.root);
  });
});
