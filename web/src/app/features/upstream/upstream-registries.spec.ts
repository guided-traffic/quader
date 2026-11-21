import { ApiError } from '../../api/problem';
import { UpstreamApi } from '../../api/upstream-api';
import { FakeUpstreamApi } from '../../api/testing/fakes-upstream';
import { RepositoriesApi } from '../../api/repositories-api';
import { ANSIBLE_FORMAT, DEB_FORMAT, FakeRepositoriesApi, NUGET_FORMAT, OCI_FORMAT, PACKAGE_FORMAT } from '../../api/testing/fakes-catalog';
import { Location } from '@angular/common';
import { button, buttonByText, expectAccessible, renderScreen, searchList, settle, text, type } from '../../testing/screen';
import { UpstreamRegistries } from './upstream-registries';

const overlay = (): HTMLElement => document.querySelector('.cdk-overlay-container') as HTMLElement;
const dialog = (): HTMLElement | null => overlay()?.querySelector('quader-confirm-dialog') ?? null;

async function render(options: { capabilities?: readonly string[]; setup?: (api: FakeUpstreamApi) => void } = {}) {
  const r = await renderScreen(UpstreamRegistries, {
    ...(options.capabilities ? { capabilities: options.capabilities } : {}),
    setup: (get) => {
      (get(RepositoriesApi) as FakeRepositoriesApi).formatsResult = [ANSIBLE_FORMAT, DEB_FORMAT, PACKAGE_FORMAT, NUGET_FORMAT, OCI_FORMAT];
      options.setup?.(get(UpstreamApi) as FakeUpstreamApi);
    },
  });
  return { ...r, api: r.get(UpstreamApi) as FakeUpstreamApi };
}

/** The row of a registry, by its display name. */
function row(root: HTMLElement, name: string): HTMLTableRowElement {
  const found = [...root.querySelectorAll<HTMLTableRowElement>('tbody tr')].find((tr) => text(tr.querySelector('th a') ?? tr) === name);
  if (!found) throw new Error(`no row ${name}`);
  return found;
}

describe('Upstream registries (R-UI-187)', () => {
  it('R-UI-187 Q-UI-900 groups the twelve registries by format in catalog order and puts scanner-trivy under System repositories, the name of the project\'s page', async () => {
    const { root } = await render();
    expect(text(root.querySelector('h1')!)).toBe('Upstream registries 12');
    const groups = [...root.querySelectorAll('tr.group-row .format-name > span')].map((name) => text(name));
    expect(groups).toEqual(['Ansible Galaxy', 'Debian (APT)', 'npm', 'NuGet', 'OCI (Docker)']);
    const oci = [...root.querySelectorAll('tbody tr')];
    const at = oci.findIndex((tr) => text(tr).endsWith('OCI (Docker)'));
    expect(oci.slice(at + 1, at + 5).map((tr) => text(tr.querySelector('th a')!))).toEqual(['Docker Hub', 'Quay', 'GitHub (ghcr.io)', 'GitLab']);
    expect(text(row(root, 'Docker Hub').querySelector('th')!)).toContain('upstream/docker-hub');
    const system = root.querySelector('#upstream-system')!;
    expect(text(system)).toBe('System repositories');
    expect(text(row(root, 'Image scanning database'))).toContain('In use by the image scanning');
    expect(row(root, 'Image scanning database').querySelector('[role="switch"], select')).toBeNull();
    // R-REPO-075: clamav stands below it the same way, with the malware scan's words, and the table's one switch (Q-UI-903).
    const systemRows = [...root.querySelectorAll('tbody tr')].map((tr) => text(tr.querySelector('th a') ?? tr));
    expect(systemRows.slice(-2)).toEqual(['Image scanning database', 'ClamAV definitions']);
    expect(text(row(root, 'ClamAV definitions'))).toContain('upstream/clamav');
    expect(text(row(root, 'ClamAV definitions'))).toContain('In use by the malware scan');
    expect(text(row(root, 'ClamAV definitions'))).toContain('Private');
    expect(row(root, 'ClamAV definitions').querySelector('select')).toBeNull();
    expect(row(root, 'ClamAV definitions').querySelector('[role="switch"]')!.getAttribute('aria-label')).toBe('ClamAV definition updates');
    await expectAccessible(root);
  });

  it('R-UI-187 Q-UI-903 switching ClamAV definition updates on asks with the address and Public, sends PUT …/clamav/clients, and shows the level', async () => {
    const { fixture, root, api } = await render();
    const toggle = row(root, 'ClamAV definitions').querySelector<HTMLButtonElement>('[role="switch"]')!;
    expect(toggle.getAttribute('aria-checked')).toBe('false');
    toggle.click();
    await settle(fixture);
    expect(text(dialog()!)).toContain('Offer the ClamAV definitions to your ClamAV installations?');
    expect(text(dialog()!)).toContain('They update from https://quader.example.com/api/clamav/upstream/clamav.');
    buttonByText(dialog()!, 'Switch on').click();
    await settle(fixture);
    expect(api.calls).toContainEqual({ op: 'clients', key: 'clamav', body: { enabled: true, level: 'public' } });
    expect(row(root, 'ClamAV definitions').querySelector('[role="switch"]')!.getAttribute('aria-checked')).toBe('true');
    expect(text(row(root, 'ClamAV definitions'))).toContain('Public');
    // An auditor reads the word, no switch.
    const auditor = await render({ capabilities: ['repositories:read-config'] });
    expect(row(auditor.root, 'ClamAV definitions').querySelector('[role="switch"]')).toBeNull();
    expect(text(row(auditor.root, 'ClamAV definitions'))).toContain('Off');
  });

  it('R-UI-187 an auditor sees the words, no switch, no select and no header actions', async () => {
    const { root } = await render({ capabilities: ['repositories:read-config', 'settings:read', 'audit:read'] });
    expect(root.querySelector('[role="switch"]')).toBeNull();
    expect(root.querySelector('select')).toBeNull();
    expect(root.querySelector('quader-page-header button')).toBeNull();
    expect(text(row(root, 'Docker Hub'))).toContain('Off');
    expect(text(row(root, 'Docker Hub'))).toContain('Internal');
  });

  it('R-UI-187 switching Docker Hub on sends its state at once and says so; switching off npmjs, which a virtual repository lists, asks first', async () => {
    const { fixture, root, api } = await render({ setup: (a) => a.set('npmjs', { state: 'active', listedBy: 3, listedByProjects: 2 }) });
    row(root, 'Docker Hub').querySelector<HTMLButtonElement>('[role="switch"]')!.click();
    await settle(fixture);
    expect(dialog()).toBeNull();
    expect(api.calls).toContainEqual({ op: 'state', key: 'docker-hub', body: { state: 'on' } });
    expect(row(root, 'Docker Hub').querySelector('[role="switch"]')!.getAttribute('aria-checked')).toBe('true');

    row(root, 'npm public registry').querySelector<HTMLButtonElement>('[role="switch"]')!.click();
    await settle(fixture);
    expect(text(dialog()!)).toContain('3 virtual repositories in 2 projects list it.');
    expect(api.calls.some((c) => c.key === 'npmjs')).toBe(false);
    buttonByText(dialog()!, 'Switch off').click();
    await settle(fixture);
    expect(api.calls).toContainEqual({ op: 'state', key: 'npmjs', body: { state: 'off' } });
    expect(text(row(root, 'npm public registry'))).toContain('Switched off');
  });

  it('R-UI-187 Switch all on names the registries that are off and sends switch-on-all; with nothing off it is disabled and says why', async () => {
    const { fixture, root, api } = await render({ setup: (a) => a.set('galaxy', { state: 'active' }) });
    button(root, 'Switch all on').click();
    await settle(fixture);
    const asked = dialog()!;
    expect(text(asked)).toContain('Switch on 11 registries?');
    expect(text(asked)).toContain('Each is Internal once it is on');
    const names = [...asked.querySelectorAll('li')].map((li) => text(li));
    expect(names).toHaveLength(11);
    expect(names).toContain('Docker Hub');
    expect(names).not.toContain('Ansible Galaxy');
    buttonByText(asked, 'Switch all on').click();
    await settle(fixture);
    expect(api.calls).toContainEqual({ op: 'switch-on-all' });
    expect(button(root, 'Switch all on').disabled).toBe(true);
    expect(text(root)).toContain('Every registry is on.');
  });

  it('R-UI-187 choosing Public in a row asks with the owner sentence before it is sent', async () => {
    const { fixture, root, api } = await render();
    type(row(root, 'Quay').querySelector('select')!, 'public');
    await settle(fixture);
    expect(text(dialog()!)).toContain('Public: anyone on the internet can pull through this installation');
    buttonByText(dialog()!, 'Make public').click();
    await settle(fixture);
    expect(api.calls).toContainEqual({ op: 'visibility', key: 'quay', body: { level: 'public' } });
  });

  it('R-UI-187 a refusal of a level stands at the row select, and a retired registry has no switch', async () => {
    const { fixture, root, api } = await render({ setup: (a) => a.set('ubuntu-ports', { state: 'retired' }) });
    expect(row(root, 'Ubuntu ports').querySelector('[role="switch"]')).toBeNull();
    expect(text(row(root, 'Ubuntu ports'))).toContain('No longer offered');
    api.failure = new ApiError(409, { code: 'public_not_allowed', detail: 'The installation admits no request without a credential.' }, 'rid');
    type(row(root, 'Quay').querySelector('select')!, 'public');
    await settle(fixture);
    buttonByText(dialog()!, 'Make public').click();
    await settle(fixture);
    const select = row(root, 'Quay').querySelector('select')!;
    expect(select.value).toBe('internal');
    expect(select.getAttribute('aria-invalid')).toBe('true');
    expect(text(row(root, 'Quay'))).toContain('The installation admits no request without a credential.');
  });

  it('R-UI-187 the filter writes its text into the address as every list does, and filters the rows', async () => {
    const { fixture, root, get } = await render();
    await searchList(fixture, root, 'zzz');
    expect(text(root)).toContain('No registry matches.');
    expect(get(Location).path()).toContain('q=zzz');
  });
});
