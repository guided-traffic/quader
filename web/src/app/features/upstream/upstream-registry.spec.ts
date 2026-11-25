import { UpstreamApi } from '../../api/upstream-api';
import { VisibilityApi } from '../../api/visibility-api';
import { SystemApi } from '../../api/system-api';
import { FakeSystemApi, sampleScanners, withUpdater } from '../../api/testing/fakes';
import { MalwareApi } from '../../api/malware-api';
import { FakeMalwareApi, malwareSettings } from '../../api/testing/fakes-malware';
import { CLAMAV_ENDPOINT, FakeUpstreamApi, configuredAccount, noAccount } from '../../api/testing/fakes-upstream';
import { FakeVisibilityApi } from '../../api/testing/fakes-projects';
import { button, buttonByText, expectAccessible, renderScreen, settle, text, type } from '../../testing/screen';
import { UpstreamRegistry } from './upstream-registry';

const overlay = (): HTMLElement => document.querySelector('.cdk-overlay-container') as HTMLElement;

async function render(
  key: string,
  options: { capabilities?: readonly string[]; setup?: (api: FakeUpstreamApi) => void; publicAvailable?: boolean } = {},
) {
  const r = await renderScreen(UpstreamRegistry, {
    inputs: { key },
    ...(options.capabilities ? { capabilities: options.capabilities } : {}),
    setup: (get) => {
      options.setup?.(get(UpstreamApi) as FakeUpstreamApi);
      if (options.publicAvailable === false) {
        const v = get(VisibilityApi) as FakeVisibilityApi;
        v.stored = { ...v.stored, settings: { ...v.stored.settings, publicAvailable: false } };
      }
    },
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
        a.set('docker-hub', { account: { ...noAccount(), authMode: 'basic', username: 'ci-pull', password: { set: true } } }),
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

  it('R-UI-188 R-REPO-075 clamav says the malware scan uses it, and has no address and no account to set', async () => {
    const { root } = await render('clamav');
    expect(text(root)).toContain('In use by the malware scan.');
    expect(root.querySelector('#wk-address, #wk-account')).toBeNull();
    expect(text(root)).not.toContain('aquasec/trivy-db');
    // A repository listed by nobody and downloaded by no client: its badge and its Details say so (Q-UI-883). Its format is system
    // only, so the badge says System repository, and what that means as its tooltip (Q-UI-1203).
    const badge = [...root.querySelectorAll<HTMLElement>('.subject .badge')].find((b) => text(b) === 'System repository')!;
    expect(badge.getAttribute('tabindex')).toBe('0');
    expect(text(root.querySelector('.subject')!)).not.toContain('Local repository');
    expect(text(root.querySelector('.subject')!)).not.toContain('Upstream registry');
    expect(text(root)).toContain('Its settings page and its dates.');
    expect(text(root)).not.toContain('Who lists the registry');
    expect(text(root)).not.toContain('Listed by');
    const unused = await render('clamav', { setup: (a) => a.set('clamav', { inUse: false }) });
    expect(text(unused.root)).toContain('Not in use by the malware scan.');
  });

  it('R-UI-188 R-CLAMAV-006 the section ClamAV definition updates: off shows the switch alone; on shows the address, the level and the one line', async () => {
    const { fixture, root, api } = await render('clamav');
    expect(text(root.querySelector('h2')!)).toBe('ClamAV definition updates');
    const toggle = root.querySelector<HTMLButtonElement>('[role="switch"]')!;
    expect(toggle.getAttribute('aria-label')).toBe('ClamAV definition updates');
    expect(toggle.getAttribute('aria-checked')).toBe('false');
    expect(text(root)).toContain("Serves these definitions to the ClamAV installations of your company. Quader's own malware scan uses them either way.");
    expect(root.querySelector('quader-address-field')).toBeNull();
    expect(text(root)).not.toContain('PrivateMirror');

    toggle.click();
    await settle(fixture);
    expect(text(overlay())).toContain('Offer the ClamAV definitions to your ClamAV installations?');
    expect(text(overlay())).toContain(`They update from ${CLAMAV_ENDPOINT}.`);
    expect(text(overlay())).toContain('Public: anyone who reaches this address can download them, without a credential.');
    buttonByText(overlay(), 'Switch on').click();
    await settle(fixture);
    expect(api.calls).toContainEqual({ op: 'clients', key: 'clamav', body: { enabled: true, level: 'public' } });
    expect(root.querySelector('[role="switch"]')!.getAttribute('aria-checked')).toBe('true');
    expect(root.querySelector<HTMLInputElement>('quader-address-field input')!.value).toBe(CLAMAV_ENDPOINT);
    expect(text(root.querySelector('.level-now')!)).toContain('Public');
    expect(text(root.querySelector('.clamav-setup')!)).toContain(`PrivateMirror ${CLAMAV_ENDPOINT}`);
    expect(text(root)).not.toContain('This installation allows no public level');
    await expectAccessible(root);
  });

  it('R-UI-188 the fold Visibility says what a ClamAV needs at each level and sets the level; below Public the line carries the token', async () => {
    const { fixture, root, api } = await render('clamav', {
      setup: (a) => a.set('clamav', { clients: { enabled: true, address: CLAMAV_ENDPOINT, certificateAddresses: [] }, visibility: 'public', fixed: ['state', 'address', 'account'] }),
    });
    const fold = root.querySelector('.level-fold')!;
    expect(text(fold)).toContain('Every ClamAV that reaches this address updates, without credentials.');
    expect(text(fold)).toContain('A ClamAV authenticates with a token in its address, which freshclam writes into its log, or with a client certificate.');
    expect(text(fold)).toContain('Only tokens of members.');
    root.querySelector<HTMLInputElement>('.level-fold input[type="radio"][value="internal"]')!.click();
    await settle(fixture);
    button(root, 'Set visibility').click();
    await settle(fixture);
    expect(api.calls).toContainEqual({ op: 'visibility', key: 'clamav', body: { level: 'internal' } });
    expect(text(root.querySelector('.clamav-setup')!)).toContain('PrivateMirror https://clamav:${QUADER_TOKEN}@quader.example.com/api/clamav/upstream/clamav');
  });

  it('R-UI-188 R-CLAMAV-010 the certificate lines and the certificate variant show only while an endpoint admits client certificates', async () => {
    const certAddress = 'https://clamav.quader.example.com/api/clamav/upstream/clamav';
    const none = await render('clamav', {
      setup: (a) => a.set('clamav', { clients: { enabled: true, address: CLAMAV_ENDPOINT, certificateAddresses: [] }, visibility: 'internal', fixed: ['state', 'address', 'account'] }),
    });
    const fold = none.root.querySelector('.level-fold')!;
    expect(text(fold)).toContain('or with a client certificate. Client certificates need an endpoint your operator sets up.');
    expect(text(fold)).toContain('Only tokens of members. Client certificates are not admitted.');
    expect(none.root.querySelector('[data-testid="cert-none"] a')!.getAttribute('href')).toBe('/help/admin/endpoints#client-certificates-for-clamav-installations');
    expect(none.root.querySelector('[data-testid="cert-addresses"]')).toBeNull();
    expect(text(none.root.querySelector('.clamav-setup')!)).not.toContain('FRESHCLAM_CLIENT_CERT');
    expect(text(none.root.querySelector('[data-testid="clamav-without"]')!)).toContain('curl -fsS --config -');

    const some = await render('clamav', {
      setup: (a) =>
        a.set('clamav', { clients: { enabled: true, address: CLAMAV_ENDPOINT, certificateAddresses: [certAddress] }, visibility: 'internal', fixed: ['state', 'address', 'account'] }),
    });
    const lines = some.root.querySelector('[data-testid="cert-addresses"]')!;
    expect(text(lines)).toContain('With a client certificate:');
    expect([...lines.querySelectorAll<HTMLInputElement>('quader-address-field input')].map((i) => i.value)).toEqual([certAddress]);
    expect(text(some.root.querySelector('.level-fold')!)).not.toContain('Client certificates need an endpoint');
    const setup = text(some.root.querySelector('.clamav-setup')!);
    expect(setup).toContain(`PrivateMirror ${certAddress}`);
    expect(setup).toContain('Environment=FRESHCLAM_CLIENT_CERT=/etc/clamav/tls/client.crt');
    expect(setup).toContain(`--cacert /etc/pki/quader-ca.pem`);
    await expectAccessible(some.root);
  });

  it('R-UI-188 R-REPO-075 where the installation allows no public level, switching on says Internal, and the section warns', async () => {
    const { fixture, root, api } = await render('clamav', {
      publicAvailable: false,
      setup: (a) => (a.publicAllowed = false),
    });
    root.querySelector<HTMLButtonElement>('[role="switch"]')!.click();
    await settle(fixture);
    expect(text(overlay())).toContain('This installation allows no public level, so the definitions are served at Internal');
    buttonByText(overlay(), 'Switch on').click();
    await settle(fixture);
    expect(api.calls.at(-1)).toEqual({ op: 'clients', key: 'clamav', body: { enabled: true, level: 'public' } });
    expect(text(root.querySelector('.level-now')!)).toContain('Internal');
    expect(text(root.querySelector('.clients-section .banner-warning')!)).toContain('every ClamAV needs a token in its address');
  });

  it('R-UI-188 switching ClamAV definition updates off asks first, and a reader sees the word and no switch', async () => {
    const on = { clients: { enabled: true, address: CLAMAV_ENDPOINT, certificateAddresses: [] }, visibility: 'public' as const, fixed: ['state' as const, 'address' as const, 'account' as const] };
    const { fixture, root, api } = await render('clamav', { setup: (a) => a.set('clamav', on) });
    root.querySelector<HTMLButtonElement>('[role="switch"]')!.click();
    await settle(fixture);
    expect(text(overlay())).toContain("ClamAV installations that update from Quader get 404 until they are on again. Quader's own malware scan is not affected.");
    buttonByText(overlay(), 'Switch off').click();
    await settle(fixture);
    expect(api.calls).toContainEqual({ op: 'clients', key: 'clamav', body: { enabled: false } });
    expect(root.querySelector('quader-address-field')).toBeNull();
    const reader = await render('clamav', { capabilities: ['repositories:read-config'], setup: (a) => a.set('clamav', on) });
    expect(reader.root.querySelector('[role="switch"]')).toBeNull();
    expect(text(reader.root.querySelector('.clients-section .word')!)).toBe('On');
    expect(reader.root.querySelector('.level-fold button.btn-confirm')).toBeNull();
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

  it('R-UI-188 the section Updater lists the updater\'s replicas, or says why there is none, and leads to the versions', async () => {
    const withRow = await renderScreen(UpstreamRegistry, {
      inputs: { key: 'clamav' },
      setup: (get) => ((get(SystemApi) as FakeSystemApi).scannersResult = withUpdater(sampleScanners())),
    });
    const section = withRow.root.querySelector('.updater-section')!;
    expect(text(section.querySelector('h2')!)).toBe('Updater');
    expect(text(section.querySelector('tbody tr')!)).toContain('quader-malware-updater-freshclam-7c9f5d6b8-m4k2t');
    expect(section.querySelector('a')!.getAttribute('href')).toBe('/projects/upstream/repositories/clamav');
    await expectAccessible(withRow.root);

    const note = async (setup: (m: FakeMalwareApi) => void) => {
      const r = await renderScreen(UpstreamRegistry, { inputs: { key: 'clamav' }, setup: (get) => setup(get(MalwareApi) as FakeMalwareApi) });
      return text(r.root.querySelector('[data-testid="updater-none"]')!);
    };
    expect(await note(() => undefined)).toBe('No updater reports: the definitions are not updated.');
    expect(await note((m) => (m.current = malwareSettings({ enabled: false })))).toBe('The malware scan is off, so the definitions are not updated.');
    expect(
      await note((m) => {
        const s = malwareSettings();
        m.current = { ...s, database: { ...s.database, source: 'repository', sourceRepository: 'ops/clamav-db' } };
      }),
    ).toBe('No updater runs: this installation takes its definitions from ops/clamav-db.');
  });
});

describe('An account the configuration declares on a well-known repository (R-UI-188 section 3, R-REPO-077)', () => {
  const section = (root: HTMLElement): HTMLElement => root.querySelector<HTMLElement>('[aria-labelledby="wk-account"]')!;

  it('R-REPO-077 shows where it comes from, its mode and its files, with Test connection and no field, Save or Remove', async () => {
    const { fixture, root, api } = await render('docker-hub', {
      setup: (a) => a.set('docker-hub', { account: configuredAccount('readable'), accountRaisesLimit: false }),
    });
    const s = section(root);
    expect(text(s.querySelector('.source-note > span:last-child')!)).toBe('From the Helm values: Secret quader-upstream-docker-hub. Change or remove it there.');
    expect(text(s.querySelector('.account-facts')!)).toContain('Username and password');
    expect(text(s.querySelector('.account-facts')!)).toContain('Readable on this server');
    expect(text(s)).toContain('The credential is sent to');
    expect(s.querySelectorAll('input, select')).toHaveLength(0);
    for (const label of ['Save account', 'Remove account']) {
      expect([...s.querySelectorAll('button')].some((b) => text(b) === label)).toBe(false);
    }
    expect(text(s)).not.toContain('allows more requests');
    buttonByText(s, 'Test connection').click();
    await settle(fixture);
    expect(api.calls).toContainEqual({ op: 'test-saved', key: 'docker-hub' });
    await expectAccessible(root);
  });

  it('R-REPO-077 missing files warn that every request fails until the Secret exists; a configuration without the chart names no Secret', async () => {
    const { root } = await render('docker-hub', {
      setup: (a) => a.set('docker-hub', { account: configuredAccount('missing'), accountRaisesLimit: false }),
    });
    expect(text(section(root).querySelector('.banner-warning > span:last-child')!)).toBe(
      "The Secret's files are missing on this server, so every request to Docker Hub fails until the Secret exists with its keys; a Secret reaches the pods within two minutes.",
    );
    const plain = await render('docker-hub', {
      setup: (a) =>
        a.set('docker-hub', {
          account: configuredAccount('empty', { configuration: { secretName: '', since: null, files: 'empty' } }),
          accountRaisesLimit: false,
        }),
    });
    expect(text(section(plain.root).querySelector('.source-note > span:last-child')!)).toBe("From the installation's configuration file. Change or remove it there.");
    expect(text(section(plain.root).querySelector('.banner-warning > span:last-child')!)).toContain('A file of the account is empty on this server');
  });

  it('R-REPO-077 an address that would send the declared account to other hosts is refused before it is sent', async () => {
    const { fixture, root, api } = await render('docker-hub', {
      setup: (a) => a.set('docker-hub', { account: configuredAccount('readable'), accountRaisesLimit: false }),
    });
    type(urlField(root), 'https://mirror.example.com/');
    await settle(fixture);
    button(root, 'Save address').click();
    await settle(fixture);
    expect(api.calls.some((c) => c.op === 'address')).toBe(false);
    expect(document.querySelector('quader-account-again-dialog')).toBeNull();
    const held = [...root.querySelectorAll<HTMLElement>('.banner-warning > span:last-child')].map((b) => text(b));
    expect(held).toContain(
      "The account of this registry comes from the installation's configuration, so an address that sends it to other hosts is changed there too.",
    );
  });

  it('R-REPO-077 a registry whose vendor allows an account more says so while it has none; another registry does not', async () => {
    const hub = await render('docker-hub');
    expect(text(section(hub.root))).toContain('Docker Hub allows more requests with an account.');
    const quay = await render('quay');
    expect(text(section(quay.root))).not.toContain('allows more requests');
  });
});
