import { Router } from '@angular/router';
import { vi } from 'vitest';
import { BastionsApi } from '../../api/bastions-api';
import { GuidesApi } from '../../api/guides-api';
import { ClientSetupApi } from '../../api/packages-api';
import { RepositoriesApi } from '../../api/repositories-api';
import { FakeRepositoriesApi, GENERIC_FORMAT, PACKAGE_FORMAT } from '../../api/testing/fakes-catalog';
import { FakeBastionsApi, sampleBastion, sampleBastionContact } from '../../api/testing/fakes-bastions';
import { FakeGuidesApi } from '../../api/testing/fakes-guides';
import { FakeClientSetupApi, NPM_REPOSITORIES } from '../../api/testing/fakes-packages';
import { addressValues, expectAccessible, renderScreen, settle, text } from '../../testing/screen';
import { ClientSetupPage } from './client-setup';

type Get = <A>(token: abstract new (...args: never[]) => A) => A;

/** Registrations as the Bastions screen would list them: one has reported a base URL, one has not. */
function registrations(get: Get): void {
  (get(BastionsApi) as FakeBastionsApi).bastions = [
    sampleBastion(),
    sampleBastion({ id: 'b2', name: 'fresh', contacts: [] }),
    sampleBastion({ id: 'b3', name: 'quiet', contacts: [sampleBastionContact({ baseUrl: '' })] }),
  ];
}

async function openSetup(inputs: Record<string, unknown>, capabilities?: readonly string[]) {
  const r = await renderScreen(ClientSetupPage, {
    inputs: { project: 'web', key: 'npm-all', client: 'npm', ...inputs },
    capabilities,
    setup: (get) => {
      const repos = get(RepositoriesApi) as FakeRepositoriesApi;
      repos.formatsResult = [GENERIC_FORMAT, PACKAGE_FORMAT];
      repos.repositories = [...NPM_REPOSITORIES];
      registrations(get);
    },
  });
  return { ...r, setup: r.get(ClientSetupApi) as FakeClientSetupApi, guides: r.get(GuidesApi) as FakeGuidesApi };
}

const target = (root: HTMLElement, value: string) => root.querySelector<HTMLInputElement>(`input[name="target"][value="${value}"]`);
const snippetText = (root: HTMLElement) => [...root.querySelectorAll('quader-setup-snippet pre')].map((p) => p.textContent ?? '').join('\n');

describe('Set up client for a Bastion (R-BAS-007, R-API-030)', () => {
  it('R-BAS-007 offers the Bastions that have reported a base URL as a place to install from, under More options', async () => {
    const r = await openSetup({});
    const offered = [...r.root.querySelectorAll<HTMLInputElement>('input[name="target"]')].map((i) => i.value);
    expect(offered).toEqual(['', 'partners']);
    expect(target(r.root, '')?.checked).toBe(true);
    expect(r.setup.calls.at(-1)?.query.bastion).toBeUndefined();
    const more = r.root.querySelector('quader-disclosure.more-options')!;
    expect(text(more)).toContain('Install from');
    await expectAccessible(r.root);
  });

  it('R-BAS-007 R-UI-066 a Bastion asks for the setup with bastion=<name>: its address, replace-registry-host, no publish step and no guide of this installation', async () => {
    const r = await openSetup({ bastion: 'partners' });
    expect(r.setup.calls.at(-1)?.query).toMatchObject({ client: 'npm', bastion: 'partners' });
    expect(target(r.root, 'partners')?.checked).toBe(true);
    const all = snippetText(r.root);
    expect(all).toContain('registry=https://packages.example.com/api/npm/web/npm-all/');
    expect(all).toContain('replace-registry-host=always');
    expect(all).not.toContain('quader.example.net');
    expect(text(r.root)).toContain('makes npm fetch every package from this address');
    // R-UI-121: a setting named in a snippet's sentence is code, in the note of a Bastion too.
    expect([...r.root.querySelectorAll('quader-setup-snippet p.why code')].map((c) => c.textContent)).toContain('replace-registry-host=always');
    expect(text(r.root)).toContain('These snippets point at the Bastion');
    expect(addressValues(r.root)).toEqual(['https://packages.example.com/api/npm/web/npm-all/']);
    expect([...r.root.querySelectorAll('section.step > h2')].map((h) => text(h))).not.toContain('3Publish');
    expect(text(r.root)).toContain('A Bastion is read only, so there are no publish snippets.');
    // The guide describes this installation: none is asked for, and the page says how to read it.
    expect(r.guides.calls).toEqual([]);
    expect(r.root.querySelector('quader-guide-view')).toBeNull();
    expect(text(r.root)).toContain('The explanation describes this installation.');
    await expectAccessible(r.root);
  });

  it('R-BAS-007 choosing the Bastion, and then this installation, puts the choice into the query', async () => {
    const r = await openSetup({});
    const navigate = vi.spyOn(r.get(Router), 'navigate');
    const choose = async (value: string) => {
      const input = target(r.root, value)!;
      input.checked = true;
      input.dispatchEvent(new Event('change', { bubbles: true }));
      await settle(r.fixture);
    };
    await choose('partners');
    expect(navigate).toHaveBeenLastCalledWith([], expect.objectContaining({ queryParams: { bastion: 'partners' } }));
    await choose('');
    expect(navigate).toHaveBeenLastCalledWith([], expect.objectContaining({ queryParams: { bastion: null } }));
  });

  it('R-BAS-007 a caller without endpoints:read gets no choice, and a Bastion named in the query still works', async () => {
    const plain = await openSetup({}, ['repositories:read-config']);
    expect(plain.root.querySelector('input[name="target"]')).toBeNull();
    expect(text(plain.root.querySelector('quader-disclosure.more-options')!)).not.toContain('Install from');
    const linked = await openSetup({ bastion: 'partners' }, ['repositories:read-config']);
    expect(target(linked.root, 'partners')?.checked).toBe(true);
    expect(linked.setup.calls.at(-1)?.query.bastion).toBe('partners');
  });

  it('R-BAS-007 an address the installation does not list is explained, not shown as a bare 409', async () => {
    const r = await openSetup({ bastion: 'unlisted' });
    expect(text(r.root)).toContain('server.additional_base_urls');
    expect(r.root.querySelector('quader-setup-snippet')).toBeNull();
  });

  it('R-BAS-007 a name that is no registration is a not-found notice', async () => {
    const r = await openSetup({ bastion: 'nobody' });
    expect(r.root.querySelector('quader-error-notice')).toBeTruthy();
    expect(r.root.querySelector('quader-setup-snippet')).toBeNull();
  });
});
