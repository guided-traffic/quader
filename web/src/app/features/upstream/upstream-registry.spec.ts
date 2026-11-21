import { UpstreamApi } from '../../api/upstream-api';
import { FakeUpstreamApi } from '../../api/testing/fakes-upstream';
import { button, buttonByText, expectAccessible, renderScreen, settle, text, type } from '../../testing/screen';
import { UpstreamRegistry } from './upstream-registry';

const overlay = (): HTMLElement => document.querySelector('.cdk-overlay-container') as HTMLElement;

async function render(key: string, options: { capabilities?: readonly string[]; setup?: (api: FakeUpstreamApi) => void } = {}) {
  const r = await renderScreen(UpstreamRegistry, {
    inputs: { key },
    ...(options.capabilities ? { capabilities: options.capabilities } : {}),
    setup: (get) => options.setup?.(get(UpstreamApi) as FakeUpstreamApi),
  });
  return { ...r, api: r.get(UpstreamApi) as FakeUpstreamApi };
}

const urlField = (root: HTMLElement): HTMLInputElement =>
  [...root.querySelectorAll<HTMLInputElement>('input')].find((i) => i.name === 'wkUrl')!;

describe("A well-known repository's page (R-UI-188)", () => {
  it('R-UI-188 names the registry in its header with its kind and its name, with Open repository, and the three open sections', async () => {
    const { root } = await render('docker-hub');
    expect(text(root.querySelector('h1')!)).toBe('Docker Hub');
    expect(text(root.querySelector('.subject')!)).toContain('upstream/docker-hub');
    expect(text(root.querySelector('.subject')!)).toContain('Upstream registry');
    expect(buttonByText(root, 'Open repository').getAttribute('href')).toBe('/projects/upstream/repositories/docker-hub');
    expect([...root.querySelectorAll('h2')].map((h) => text(h).replace('chevron_right', ''))).toEqual(['State and visibility', 'Address', 'Account', 'Details']);
    expect(text(root)).toContain("This is Quader's address.");
    expect(root.querySelector('[role="switch"]')!.getAttribute('aria-label')).toBe('Switch on Docker Hub');
    await expectAccessible(root);
  });

  it("R-UI-188 an overridden address of quay shows Differs from Quader's and a reset that sends DELETE with If-Match", async () => {
    const { fixture, root, api } = await render('quay', {
      setup: (a) =>
        a.set('quay', {
          address: {
            url: 'https://quay-mirror.example.com/',
            extraHosts: [],
            upstreamKeys: null,
            overridden: true,
            product: { url: 'https://quay.io/', extraHosts: [], upstreamKeys: null },
          },
          configVersion: 4,
        }),
    });
    expect(text(root)).toContain("Differs from Quader's");
    expect(text(root)).toContain("Quader's address is https://quay.io/.");
    button(root, "Reset to Quader's address").click();
    await settle(fixture);
    expect(api.calls).toContainEqual({ op: 'reset-address', key: 'quay', etag: '"4"', removeAccount: false });
    expect(urlField(root).value).toBe('https://quay.io/');
    expect(text(root)).toContain("This is Quader's address.");
  });

  it('R-UI-188 saving a new URL of docker-hub with an account stored asks for the account and sends it with the address in one PUT', async () => {
    const { fixture, root, api } = await render('docker-hub', {
      setup: (a) =>
        a.set('docker-hub', { account: { authMode: 'basic', username: 'ci-pull', password: { set: true }, removedByUpdateAt: null } }),
    });
    type(urlField(root), 'https://mirror.example.com/');
    await settle(fixture);
    button(root, 'Save address').click();
    await settle(fixture);
    const asked = overlay().querySelector('quader-account-again-dialog') as HTMLElement;
    expect(text(asked)).toContain('The account goes to auth.docker.io and registry-1.docker.io only.');
    expect(api.calls.some((c) => c.op === 'address')).toBe(false);
    type(asked.querySelector<HTMLInputElement>('input[type="password"]')!, 's3cret');
    await settle(fixture);
    buttonByText(asked, 'Save with this account').click();
    await settle(fixture);
    const sent = api.calls.filter((c) => c.op === 'address');
    expect(sent).toHaveLength(1);
    expect(sent[0]).toEqual({
      op: 'address',
      key: 'docker-hub',
      etag: '"1"',
      body: {
        url: 'https://mirror.example.com/',
        extraHosts: [{ pattern: 'auth.docker.io', sendCredentials: true }],
        account: { authMode: 'basic', username: 'ci-pull', password: { value: 's3cret' } },
      },
    });
    expect(text(root)).toContain("Differs from Quader's");
  });

  it('R-UI-188 scanner-trivy says its use and its fixed level in words, with no switch and no level to choose', async () => {
    const { root } = await render('scanner-trivy');
    expect(text(root)).toContain('In use by the image scanning. Its visibility is always Private.');
    expect(root.querySelector('[role="switch"]')).toBeNull();
    expect(root.querySelector('select#wk-level')).toBeNull();
    expect(text(root)).toContain('The registry must serve the image aquasec/trivy-db.');
  });

  it('R-UI-188 R-REPO-075 scanner-clamav says the malware scan uses it, and has no address and no account to set', async () => {
    const { root } = await render('scanner-clamav');
    expect(text(root)).toContain('In use by the malware scan. Its visibility is always Private.');
    expect(root.querySelector('[role="switch"]')).toBeNull();
    expect(root.querySelector('#wk-address, #wk-account')).toBeNull();
    expect(text(root)).not.toContain('aquasec/trivy-db');
    // A local repository listed by nobody and downloaded by no client: its badge and its Details say so (Q-UI-883).
    expect(text(root.querySelector('.subject')!)).toContain('Local repository');
    expect(text(root.querySelector('.subject')!)).not.toContain('Upstream registry');
    expect(text(root)).toContain('Its settings page and its dates.');
    expect(text(root)).not.toContain('Who lists the registry');
    expect(text(root)).not.toContain('Listed by');
    const unused = await render('scanner-clamav', { setup: (a) => a.set('scanner-clamav', { inUse: false }) });
    expect(text(unused.root)).toContain('Not in use by the malware scan. Its visibility is always Private.');
  });

  it('R-UI-188 a retired registry says so and shows only who lists it; a reader changes nothing', async () => {
    const retired = await render('galaxy', {
      setup: (a) => {
        a.set('galaxy', { state: 'retired', listedBy: 2, listedByProjects: 1 });
        a.listedByNames = { galaxy: ['web/roles', 'web/roles-all'] };
      },
    });
    expect(text(retired.root)).toContain('Quader no longer offers this registry. Its cached content is being removed.');
    expect(text(retired.root)).toContain('2 virtual repositories in 1 project list it.');
    expect([...retired.root.querySelectorAll('.listed-names a')].map((a) => text(a))).toEqual(['web/roles', 'web/roles-all']);
    expect(retired.root.querySelector('form')).toBeNull();

    const reader = await render('npmjs', { capabilities: ['repositories:read-config'] });
    expect(reader.root.querySelector('[role="switch"]')).toBeNull();
    expect([...reader.root.querySelectorAll('button')].some((b) => /Save|Reset|Remove/.test(text(b)))).toBe(false);
    expect(urlField(reader.root).readOnly).toBe(true);
  });

  it('R-UI-188 Details counts the listing virtual repositories in their projects, names them as links and counts the others', async () => {
    const { fixture, root } = await render('npmjs', {
      setup: (a) => {
        a.set('npmjs', { state: 'active', listedBy: 3, listedByProjects: 2 });
        a.listedByNames = { npmjs: ['payments/npm-all', 'web/npm'] };
      },
    });
    button(root, /Details/).click();
    await settle(fixture);
    const details = root.querySelector('dl.facts')!;
    expect(text(details)).toContain('3 virtual repositories in 2 projects list it.');
    const links = [...details.querySelectorAll<HTMLAnchorElement>('.listed-names a')];
    expect(links.map((a) => [text(a), a.getAttribute('href')])).toEqual([
      ['payments/npm-all', '/projects/payments/repositories/npm-all/settings'],
      ['web/npm', '/projects/web/repositories/npm/settings'],
    ]);
    expect(text(details)).toContain('And 1 more virtual repository.');
    await expectAccessible(root);
  });
});
