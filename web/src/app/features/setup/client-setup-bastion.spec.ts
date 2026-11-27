import { Router } from '@angular/router';
import { vi } from 'vitest';
import { GuidesApi } from '../../api/guides-api';
import { ClientSetupReads } from '../../api/package-reads';
import { ApiError } from '../../api/problem';
import { Repository, RepositoriesApi } from '../../api/repositories-api';
import { SessionApi } from '../../api/session-api';
import { FakeSessionApi } from '../../api/testing/fakes';
import { FakeRepositoriesApi, GENERIC_FORMAT, OCI_FORMAT, PACKAGE_FORMAT } from '../../api/testing/fakes-catalog';
import { FakeGuidesApi } from '../../api/testing/fakes-guides';
import { FakeClientSetupReads, NPM_REPOSITORIES, OCI_REPOSITORIES } from '../../api/testing/fakes-packages';
import { addressValues, expectAccessible, renderScreen, settle, text } from '../../testing/screen';
import { ClientSetupPage } from './client-setup';

/**
 * The Bastions that distribute a repository, as every read of it carries them (bastion.md R-BAS-134, R-BAS-135): `partners` with two
 * deployments, `quiet` before its first contact (no address), `off` disabled now. Only `partners` is a place to install from.
 */
const DISTRIBUTED: Repository['bastions'] = [
  { name: 'partners', addresses: ['https://packages.example.com', 'https://pkgs-eu.example.com'], disabled: false },
  { name: 'quiet', addresses: [], disabled: false },
  { name: 'off', addresses: ['https://off.example.com'], disabled: true },
];

async function openSetup(inputs: Record<string, unknown>, options: { anonymous?: boolean } = {}) {
  const r = await renderScreen(ClientSetupPage, {
    inputs: { project: 'web', key: 'npm-all', client: 'npm', ...inputs },
    // A plain reader: no endpoints:read, so nothing comes from the list of Bastions (R-BAS-004).
    capabilities: ['repositories:read'],
    setup: (get) => {
      const repos = get(RepositoriesApi) as FakeRepositoriesApi;
      repos.formatsResult = [GENERIC_FORMAT, PACKAGE_FORMAT, OCI_FORMAT];
      repos.repositories = [...NPM_REPOSITORIES, ...OCI_REPOSITORIES].map((r) =>
        r.key === 'npm-all' || r.key === 'oci-local' ? { ...r, bastions: DISTRIBUTED } : r,
      );
      if (options.anonymous) {
        (get(SessionApi) as FakeSessionApi).whoamiResult = new ApiError(401, { code: 'unauthenticated' }, null);
        (get(GuidesApi) as FakeGuidesApi).anonymous = true;
      }
    },
  });
  return { ...r, setup: r.get(ClientSetupReads) as FakeClientSetupReads, guides: r.get(GuidesApi) as FakeGuidesApi };
}

const select = (root: HTMLElement) => root.querySelector<HTMLSelectElement>('select#setup-target');
const options = (root: HTMLElement) => [...(select(root)?.options ?? [])].map((o) => o.textContent?.trim());
const chosen = (root: HTMLElement) => select(root)?.selectedOptions[0]?.textContent?.trim();
const snippetText = (root: HTMLElement) => [...root.querySelectorAll('quader-setup-snippet pre')].map((p) => p.textContent ?? '').join('\n');

describe('Set up client: Install from (R-BAS-007, R-UI-016)', () => {
  it('R-UI-016 a repository distributed nowhere has no Install from, and More options none either', async () => {
    const r = await openSetup({ key: 'npm-local' });
    expect(select(r.root)).toBeNull();
    expect(text(r.root)).not.toContain('Install from');
    expect(r.setup.calls.at(-1)?.query.bastion).toBeUndefined();
  });

  it('R-BAS-007 every reader is offered each deployment of each distributing Bastion beside the client, this installation chosen', async () => {
    const r = await openSetup({});
    expect(options(r.root)).toEqual([
      'This installation (quader.example.net)',
      'Bastion partners · packages.example.com',
      'Bastion partners · pkgs-eu.example.com',
    ]);
    expect(chosen(r.root)).toBe('This installation (quader.example.net)');
    // In the row of Client, with its help behind the sign; nothing of it under More options.
    const row = r.root.querySelector('.client-choice')!;
    expect(row.querySelector('select#setup-client')).toBeTruthy();
    expect(row.querySelector('quader-help button')?.getAttribute('aria-label')).toBe('About Install from');
    expect(text(r.root.querySelector('quader-disclosure.more-options')!)).not.toContain('Install from');
    expect(r.setup.calls.at(-1)?.query.bastion).toBeUndefined();
    await expectAccessible(r.root);
  });

  it('R-BAS-135 a deployment asks for its snippets with bastion and address: its host, no publish step, no guide, no banner', async () => {
    const r = await openSetup({ bastion: 'partners', address: 'https://pkgs-eu.example.com' });
    expect(r.setup.calls.at(-1)?.query).toMatchObject({ client: 'npm', bastion: 'partners', address: 'https://pkgs-eu.example.com' });
    expect(chosen(r.root)).toBe('Bastion partners · pkgs-eu.example.com');
    const all = snippetText(r.root);
    expect(all).toContain('registry=https://pkgs-eu.example.com/api/npm/web/npm-all/');
    expect(all).toContain('replace-registry-host=always');
    expect(text(r.root)).toContain('makes npm fetch every package from this address');
    expect(addressValues(r.root)).toEqual(['https://pkgs-eu.example.com/api/npm/web/npm-all/']);
    expect([...r.root.querySelectorAll('section.step > h2')].map((h) => text(h))).not.toContain('3Publish');
    expect(text(r.root)).not.toContain('These snippets point at the Bastion');
    expect(r.guides.calls).toEqual([]);
    expect(r.root.querySelector('quader-guide-view')).toBeNull();
    expect(text(r.root)).toContain('Choose This installation under Install from to read it.');
    await expectAccessible(r.root);
  });

  it('R-BAS-135 a link that names the Bastion alone shows its first deployment, which the server answers without an address', async () => {
    const r = await openSetup({ bastion: 'partners' });
    expect(chosen(r.root)).toBe('Bastion partners · packages.example.com');
    expect(r.setup.calls.at(-1)?.query.address).toBeUndefined();
  });

  it('R-BAS-007 choosing a deployment, and then this installation, puts bastion and address into the query', async () => {
    const r = await openSetup({});
    const navigate = vi.spyOn(r.get(Router), 'navigate');
    const pick = async (value: string) => {
      const s = select(r.root)!;
      s.value = value;
      s.dispatchEvent(new Event('change', { bubbles: true }));
      await settle(r.fixture);
    };
    await pick('1');
    expect(navigate).toHaveBeenLastCalledWith(
      [],
      expect.objectContaining({ queryParams: { bastion: 'partners', address: 'https://pkgs-eu.example.com' } }),
    );
    await pick('');
    expect(navigate).toHaveBeenLastCalledWith([], expect.objectContaining({ queryParams: { bastion: null, address: null } }));
  });

  it('R-BAS-007 a signed-out visitor of a public repository is offered the Bastion as well, and gets no More options', async () => {
    const r = await openSetup({}, { anonymous: true });
    expect(options(r.root)).toHaveLength(3);
    expect(r.root.querySelector('quader-disclosure.more-options')).toBeNull();
  });

  it('R-BAS-007 one OCI case: the pull names the image on the deployment chosen', async () => {
    const r = await openSetup({ key: 'oci-local', client: 'docker', bastion: 'partners', address: 'https://pkgs-eu.example.com' });
    expect(chosen(r.root)).toBe('Bastion partners · pkgs-eu.example.com');
    expect(snippetText(r.root)).toContain('docker pull pkgs-eu.example.com/web/oci-local/myapp:1.0');
  });

  it('R-BAS-007 a Bastion the link names but the repository does not offer stays chosen, and the answer is worded', async () => {
    const r = await openSetup({ bastion: 'unlisted' });
    expect(chosen(r.root)).toBe('Bastion unlisted');
    expect(text(r.root)).toContain('server.additional_base_urls');
    expect(r.root.querySelector('quader-setup-snippet')).toBeNull();
    const refused = await openSetup({ bastion: 'partners' });
    refused.setup.failure = new ApiError(409, { code: 'repository_not_distributed' }, null);
    refused.fixture.componentRef.setInput('bastion', 'off');
    await settle(refused.fixture);
    expect(text(refused.root)).toContain('This Bastion does not distribute the repository.');
  });
});
