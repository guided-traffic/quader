import { RepositoriesApi } from '../../api/repositories-api';
import { FakeRepositoriesApi, ANSIBLE_FORMAT, DEB_FORMAT, NUGET_FORMAT, OCI_FORMAT, PACKAGE_FORMAT } from '../../api/testing/fakes-catalog';
import { FakeUpstreamApi } from '../../api/testing/fakes-upstream';
import { RegistryPreset, UpstreamApi } from '../../api/upstream-api';
import { buttonByText, expectAccessible, renderScreen, settle, text, typeInto } from '../../testing/screen';
import { UpstreamRegistries } from './upstream-registries';

const HARBOR: RegistryPreset = {
  format: 'oci',
  displayName: 'Harbor',
  url: 'https://harbor.example.com/',
  extraHosts: [{ pattern: 'harbor.example.com', sendCredentials: true }],
};
const VERDACCIO: RegistryPreset = { format: 'npm', displayName: 'Verdaccio', url: 'https://npm.example.com/', extraHosts: [] };

const overlay = (): HTMLElement => document.querySelector('.cdk-overlay-container') as HTMLElement;
const presetDialog = (): HTMLElement => overlay().querySelector('quader-registry-preset-dialog') as HTMLElement;
const confirmDialog = (): HTMLElement => overlay().querySelector('quader-confirm-dialog') as HTMLElement;

async function render(options: { capabilities?: readonly string[]; presets?: RegistryPreset[] } = {}) {
  const r = await renderScreen(UpstreamRegistries, {
    ...(options.capabilities ? { capabilities: options.capabilities } : {}),
    setup: (get) => {
      (get(RepositoriesApi) as FakeRepositoriesApi).formatsResult = [ANSIBLE_FORMAT, DEB_FORMAT, PACKAGE_FORMAT, NUGET_FORMAT, OCI_FORMAT];
      (get(UpstreamApi) as FakeUpstreamApi).presets = structuredClone(options.presets ?? []);
    },
  });
  const section = r.root.querySelector('quader-registry-presets') as HTMLElement;
  return { ...r, section, api: r.get(UpstreamApi) as FakeUpstreamApi };
}

describe('Registries of this installation (R-UI-195)', () => {
  it('R-UI-195 groups the presets by format in catalog order, with the address and the hosts, between the two tables', async () => {
    const { root, section } = await render({ presets: [HARBOR, VERDACCIO] });
    expect(text(section.querySelector('h2')!)).toBe('Registries of this installation');
    expect([...section.querySelectorAll('tr.group-row .format-name > span')].map((s) => text(s))).toEqual(['npm', 'OCI (Docker)']);
    const harbor = [...section.querySelectorAll('tbody tr')].find((tr) => text(tr).includes('Harbor'))!;
    expect(text(harbor)).toContain('https://harbor.example.com/');
    expect(text(harbor)).toContain('harbor.example.com receives the credential');
    // The section stands after the public registries and before the system repositories (Q-UI-906).
    const order = [...root.querySelectorAll('h2')].map((h) => text(h));
    expect(order.indexOf('Registries of this installation')).toBeLessThan(order.indexOf('System repositories'));
    await expectAccessible(section);
  });

  it('R-UI-195 an auditor sees the table without Add registry and without the menu of Edit and Remove; an empty list says so', async () => {
    const { section } = await render({ capabilities: ['repositories:read-config', 'settings:read', 'audit:read'], presets: [HARBOR] });
    expect([...section.querySelectorAll('button')].map((b) => text(b))).toEqual([]);
    const empty = await render({ capabilities: ['repositories:read-config'] });
    expect(text(empty.section)).toContain('No registry of this installation yet.');
    expect(text(empty.section)).not.toContain('Add one');
  });

  it('R-UI-195 adding a registry sends the whole list with If-Match, and a refusal of its URL stands under the field', async () => {
    const { fixture, section, api } = await render({ presets: [HARBOR, VERDACCIO] });
    buttonByText(section, 'Add registry').click();
    await settle(fixture);
    const d = presetDialog();
    expect(text(d.querySelector('h2')!)).toBe('Add a registry');
    expect(d.querySelector('input[type="password"]')).toBeNull();
    typeInto(d, 'Format', 'oci');
    typeInto(d, 'Name', 'Again');
    typeInto(d, 'Upstream URL', 'https://registry-1.docker.io/');
    buttonByText(d, 'Save').click();
    await settle(fixture);
    expect(api.calls.at(-1)).toEqual({
      op: 'registry-presets',
      etag: '"1"',
      body: [HARBOR, VERDACCIO, { format: 'oci', displayName: 'Again', url: 'https://registry-1.docker.io/', extraHosts: [] }],
    });
    expect(presetDialog()).not.toBeNull();
    expect(text(presetDialog())).toContain('is the address of a well-known registry');

    typeInto(presetDialog(), 'Upstream URL', 'https://registry.build.example.com/');
    buttonByText(presetDialog(), 'Add host').click();
    await settle(fixture);
    typeInto(presetDialog(), 'Host pattern', 'auth.build.example.com');
    buttonByText(presetDialog(), 'Save').click();
    await settle(fixture);
    expect(presetDialog()).toBeNull();
    expect(api.presets.at(-1)).toEqual({
      format: 'oci',
      displayName: 'Again',
      url: 'https://registry.build.example.com/',
      extraHosts: [{ pattern: 'auth.build.example.com', sendCredentials: true }],
    });
    expect(text(section)).toContain('Again');
  });

  it('R-UI-195 Remove asks first and sends the list without the registry', async () => {
    const { fixture, section, api } = await render({ presets: [HARBOR, VERDACCIO] });
    const harbor = [...section.querySelectorAll('tbody tr')].find((tr) => text(tr).includes('Harbor'))!;
    harbor.querySelector<HTMLButtonElement>('button[aria-label="Actions for Harbor"]')!.click();
    await settle(fixture);
    buttonByText(overlay(), 'Remove').click();
    await settle(fixture);
    expect(text(confirmDialog())).toContain('Remote repositories created from it keep their address and hosts.');
    expect(api.calls.some((c) => c.op === 'registry-presets')).toBe(false);
    buttonByText(confirmDialog(), 'Remove').click();
    await settle(fixture);
    expect(api.calls.at(-1)).toEqual({ op: 'registry-presets', etag: '"1"', body: [VERDACCIO] });
    expect(text(section)).not.toContain('Harbor');
  });
});
