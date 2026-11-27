import { Component } from '@angular/core';
import { vi } from 'vitest';
import { ClientSetupReads } from '../../api/package-reads';
import { Repository, RepositoriesApi } from '../../api/repositories-api';
import { FakeRepositoriesApi } from '../../api/testing/fakes-catalog';
import { FakeClientSetupReads, GENERIC_REPOSITORY, OCI_REPOSITORIES } from '../../api/testing/fakes-packages';
import { expectAccessible, renderScreen, settle, text } from '../../testing/screen';
import { ClientCommandMenu } from './client-command-menu';
import { DESTINATIONS } from './destinations';

/** `partners` distributes the repositories with two deployments; `off` is disabled now and offers none (bastion.md R-BAS-134). */
const DISTRIBUTED: Repository['bastions'] = [
  { name: 'partners', addresses: ['https://packages.example.com', 'https://pkgs-eu.example.com'], disabled: false },
  { name: 'off', addresses: ['https://off.example.com'], disabled: true },
];

const DIGEST = 'sha256:' + 'ab'.repeat(32);

/** Two rows' panels of one image's repository, as a repository's list has them, and a row of a repository distributed nowhere. */
@Component({
  imports: [ClientCommandMenu],
  template: `
    <quader-client-command-menu class="one" format="oci" name="team/app" version="1.2.0" repository="web/oci-local" verb="Pull" kind="image" [digest]="digest" />
    <quader-client-command-menu class="two" format="oci" name="team/app" version="1.1.0" repository="web/oci-local" verb="Pull" kind="image" />
    <quader-client-command-menu class="nowhere" format="npm" name="@team-a/ui" version="1.4.0" repository="web/npm-local" verb="Install" />
  `,
})
class Rows {
  protected readonly digest = DIGEST;
}

/** A generic repository's file, with its download address on this installation. */
@Component({
  imports: [ClientCommandMenu],
  template: `
    <quader-client-command-menu format="generic" name="tool.tgz" version="" repository="web/generic-local" path="dist/tool.tgz"
      address="https://quader.example.net/files/web/generic-local/dist/tool.tgz" verb="Download" />
  `,
})
class AFile {}

async function render<T>(host: new () => T, providers: unknown[] = []) {
  const r = await renderScreen(host, {
    providers: providers as never[],
    setup: (get) => {
      const repos = get(RepositoriesApi) as FakeRepositoriesApi;
      repos.repositories = [...OCI_REPOSITORIES, GENERIC_REPOSITORY].map((x) => ({ ...x, bastions: DISTRIBUTED }));
    },
  });
  document.body.appendChild(r.root);
  const open = async (which: string) => {
    const trigger = r.root.querySelector<HTMLButtonElement>(`quader-client-command-menu${which} button`)!;
    trigger.click();
    await settle(r.fixture);
    // The destinations are read once the panel opened; the commands follow.
    await settle(r.fixture);
    return { trigger, panel: document.getElementById(trigger.getAttribute('aria-controls')!)! };
  };
  return { ...r, open, setup: r.get(ClientSetupReads) as FakeClientSetupReads, repos: r.get(RepositoriesApi) as FakeRepositoriesApi };
}

const from = (panel: HTMLElement) => panel.querySelector<HTMLSelectElement>('.command-from select');
const pick = async (panel: HTMLElement, value: string, settleOn: { fixture: Parameters<typeof settle>[0] }) => {
  const s = from(panel)!;
  s.value = value;
  s.dispatchEvent(new Event('change', { bubbles: true }));
  await settle(settleOn.fixture);
};
const shownCommand = (panel: HTMLElement) =>
  [...panel.querySelectorAll<HTMLInputElement>('[role="tabpanel"]:not([inert]) quader-address-field input')].map((i) => i.value);

describe('From in the panel of a version\'s commands (R-UI-271)', () => {
  afterEach(() => document.querySelectorAll('quader-client-command-menu').forEach((e) => e.closest('[ng-version], div')?.remove()));

  it('R-UI-271 a repository distributed through a Bastion offers From: the commands, the pull by digest and Set up client follow the deployment', async () => {
    const r = await render(Rows);
    const { panel, trigger } = await r.open('.one');
    expect([...from(panel)!.options].map((o) => o.textContent?.trim())).toEqual([
      'This installation (quader.example.net)',
      'Bastion partners · packages.example.com',
      'Bastion partners · pkgs-eu.example.com',
    ]);
    expect(text(panel.querySelector('.command-from')!)).toContain('From');
    expect(shownCommand(panel)).toEqual([
      'docker pull quader.example.net/web/oci-local/team/app:1.2.0',
      `docker pull quader.example.net/web/oci-local/team/app@${DIGEST}`,
    ]);
    await expectAccessible(panel);

    await pick(panel, '1', r);
    expect(r.setup.calls.at(-1)?.query).toMatchObject({ bastion: 'partners', address: 'https://pkgs-eu.example.com', credentials: 'none' });
    expect(shownCommand(panel)).toEqual([
      'docker pull pkgs-eu.example.com/web/oci-local/team/app:1.2.0',
      `docker pull pkgs-eu.example.com/web/oci-local/team/app@${DIGEST}`,
    ]);
    const link = new URL(panel.querySelector<HTMLAnchorElement>('a.setup-link')!.href, 'https://x');
    expect(link.pathname).toBe('/projects/web/repositories/oci-local/setup');
    expect(Object.fromEntries(link.searchParams)).toEqual({ client: 'docker', bastion: 'partners', address: 'https://pkgs-eu.example.com' });

    // Back to this installation: no bastion in the commands' read or the link.
    await pick(panel, '', r);
    expect(r.setup.calls.at(-1)?.query.bastion).toBeUndefined();
    expect(panel.querySelector('a.setup-link')!.getAttribute('href')).toBe('/projects/web/repositories/oci-local/setup?client=docker');
    trigger.click();
    await settle(r.fixture);
  });

  it('R-UI-271 the page keeps the choice: another panel of a repository that offers the same deployment opens on it, one that offers none has no From', async () => {
    const r = await render(Rows);
    const get = vi.spyOn(r.repos, 'get');
    const one = await r.open('.one');
    await pick(one.panel, '1', r);
    one.trigger.click();
    await settle(r.fixture);
    const two = await r.open('.two');
    expect(from(two.panel)!.selectedOptions[0]?.textContent?.trim()).toBe('Bastion partners · pkgs-eu.example.com');
    expect(r.setup.calls.at(-1)?.query).toMatchObject({ bastion: 'partners', address: 'https://pkgs-eu.example.com', version: '1.1.0' });
    two.trigger.click();
    await settle(r.fixture);
    // web/npm-local is distributed nowhere: no From, and its commands are this installation's.
    const nowhere = await r.open('.nowhere');
    expect(from(nowhere.panel)).toBeNull();
    expect(r.setup.calls.filter((c) => c.repository === 'web/npm-local').every((c) => c.query.bastion === undefined)).toBe(true);
    // One read per repository and page, however many panels open.
    expect(get.mock.calls.filter(([ref]) => ref === 'web/oci-local')).toHaveLength(1);
  });

  it('R-UI-271 a file\'s download address moves to the deployment chosen', async () => {
    const r = await render(AFile);
    const { panel } = await r.open('');
    const address = () => panel.querySelector<HTMLInputElement>('.command-address input')!.value;
    expect(address()).toBe('https://quader.example.net/files/web/generic-local/dist/tool.tgz');
    await pick(panel, '0', r);
    expect(address()).toBe('https://packages.example.com/files/web/generic-local/dist/tool.tgz');
  });

  it('R-UI-271 the Bastion\'s application binds no destinations: its panels have no From and read no repository', async () => {
    const r = await render(Rows, [{ provide: DESTINATIONS, useValue: null }]);
    const get = vi.spyOn(r.repos, 'get');
    const { panel } = await r.open('.one');
    expect(from(panel)).toBeNull();
    expect(get).not.toHaveBeenCalled();
    expect(r.setup.calls.every((c) => c.query.bastion === undefined)).toBe(true);
  });
});
