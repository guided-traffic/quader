import { Component, input, signal } from '@angular/core';
import { ClientSetupReads } from '../../api/package-reads';
import { PackageVersion, PackagesApi } from '../../api/packages-api';
import { ApiError } from '../../api/problem';
import { clamavFixture, clamavPackageId } from '../../api/testing/fakes-clamav';
import { FakeClientSetupReads, FakePackagesApi } from '../../api/testing/fakes-packages';
import { CLAMAV_ENDPOINT, FakeUpstreamApi } from '../../api/testing/fakes-upstream';
import { UpstreamApi } from '../../api/upstream-api';
import { REPOSITORIES_SHOWN } from '../../core/repositories-shown';
import { spokenText } from '../../testing/axe';
import { expectAccessible, renderScreen, settle, text, waitUntil } from '../../testing/screen';
import { ClientCommandMenu } from './client-command-menu';
import { versionCommandReads } from './version-command';

@Component({
  imports: [ClientCommandMenu],
  template: `
    <button type="button" class="elsewhere">Elsewhere</button>
    <quader-client-command-menu format="npm" name="@team-a/ui" version="1.4.0" repository="web/npm-local" verb="Install" />
  `,
})
class Host {}

@Component({
  imports: [ClientCommandMenu],
  template: `<quader-client-command-menu format="deb" name="hello" version="2.10-3" repository="web/apt-local" verb="Install" />`,
})
class OneClient {}

const DIGEST = 'sha256:' + 'ab'.repeat(32);

@Component({
  imports: [ClientCommandMenu],
  template: `
    <quader-client-command-menu format="oci" name="team/app" version="1.2.0" repository="web/oci-local" verb="Pull" kind="image" [digest]="digest" />
  `,
})
class AnImage {
  protected readonly digest = DIGEST;
}

@Component({
  imports: [ClientCommandMenu],
  template: `<quader-client-command-menu format="oci" name="charts/web" version="0.3.0" repository="web/oci-local" verb="Pull" kind="helm-chart" />`,
})
class AChart {}

async function render() {
  const r = await renderScreen(Host);
  document.body.appendChild(r.root);
  const trigger = r.root.querySelector<HTMLButtonElement>('quader-client-command-menu button')!;
  const panel = () => (trigger.getAttribute('aria-controls') ? document.getElementById(trigger.getAttribute('aria-controls')!) : null);
  const open = async () => {
    trigger.focus();
    trigger.click();
    await settle(r.fixture);
    return panel()!;
  };
  return { ...r, trigger, panel, open };
}

describe('The commands menu of a version', () => {
  afterEach(() => document.querySelectorAll('quader-client-command-menu').forEach((e) => e.closest('[ng-version], div')?.remove()));

  it('R-UI-052 opens a panel hung from its button with a tab per client and Set up client at the row\'s end, the chosen client\'s command and copy button, asked only on opening', async () => {
    const r = await render();
    const setup = r.get(ClientSetupReads) as FakeClientSetupReads;
    expect(spokenText(r.trigger)).toBe('1.4.0: Install');
    expect(r.trigger.getAttribute('aria-haspopup')).toBe('dialog');
    expect(r.trigger.getAttribute('aria-expanded')).toBe('false');
    expect(r.panel()).toBeNull();
    expect(setup.calls).toEqual([]);

    const panel = await r.open();
    expect(r.trigger.getAttribute('aria-expanded')).toBe('true');
    expect(panel.getAttribute('role')).toBe('dialog');
    expect(panel.getAttribute('aria-label')).toBe('Install @team-a/ui 1.4.0');
    // The panel takes the focus, so that Tab reaches its first field.
    expect(document.activeElement).toBe(panel);
    const tabs = [...panel.querySelectorAll<HTMLElement>('[role="tab"]')];
    expect(tabs.map((t) => text(t))).toEqual(['npm', 'pnpm', 'Yarn Berry', 'Yarn Classic']);
    expect(tabs.map((t) => t.getAttribute('aria-selected'))).toEqual(['true', 'false', 'false', 'false']);
    // Every client's command is asked once, so the panel is as wide as the longest; only the chosen one is shown, the others inert.
    expect(setup.calls.map((c) => c.query.client)).toEqual(['npm', 'pnpm', 'yarn', 'yarn-classic']);
    expect(setup.calls.every((c) => c.query.credentials === 'none' && c.query.version === '1.4.0' && c.repository === 'web/npm-local')).toBe(true);
    expect(panel.querySelectorAll('[role="tabpanel"]:not([inert])')).toHaveLength(1);
    const setupLink = panel.querySelector<HTMLAnchorElement>('a.setup-link')!;
    expect(spokenText(setupLink)).toBe('Set up client npm');
    expect(setupLink.getAttribute('href')).toBe('/projects/web/repositories/npm-local/setup?client=npm');
    const first = panel.querySelector<HTMLInputElement>('[role="tabpanel"]:not([inert]) quader-address-field input')!;
    expect(first.value).toBe('npm install @team-a/ui@1.4.0');
    expect(first.getAttribute('aria-label')).toBe('Command for npm');
    await expectAccessible(panel);

    const writeText = vi.fn().mockResolvedValue(undefined);
    Object.defineProperty(navigator, 'clipboard', { value: { writeText }, configurable: true });
    const copy = panel.querySelector<HTMLButtonElement>('[role="tabpanel"]:not([inert]) quader-copy-button button')!;
    expect(copy.getAttribute('aria-label')).toBe('Copy the command for npm');
    copy.click();
    await settle(r.fixture);
    expect(writeText).toHaveBeenCalledWith('npm install @team-a/ui@1.4.0');
  });

  it('R-UI-052 a tab shows its client\'s command and the setup follows it; the tab is kept when the panel opens again', async () => {
    const r = await render();
    const panel = await r.open();
    const tabs = () => [...panel.querySelectorAll<HTMLElement>('[role="tab"]')];
    tabs()[2]!.click();
    await settle(r.fixture);
    const shown = () => panel.querySelector<HTMLInputElement>('[role="tabpanel"]:not([inert]) quader-address-field input')!;
    expect(shown().value).toBe('yarn add @team-a/ui@1.4.0');
    expect(shown().getAttribute('aria-label')).toBe('Command for Yarn Berry (2 and later)');
    expect(panel.querySelector('a.setup-link')!.getAttribute('href')).toBe('/projects/web/repositories/npm-local/setup?client=yarn');
    r.trigger.click();
    await settle(r.fixture);
    const again = await r.open();
    expect(text(again.querySelector('[role="tab"][aria-selected="true"]')!)).toBe('Yarn Berry');
  });

  it('R-UI-052 a format of one client shows its name as the row\'s label, no strip of one tab', async () => {
    const r = await renderScreen(OneClient);
    document.body.appendChild(r.root);
    const trigger = r.root.querySelector<HTMLButtonElement>('quader-client-command-menu button')!;
    trigger.click();
    await settle(r.fixture);
    const panel = document.getElementById(trigger.getAttribute('aria-controls')!)!;
    expect(panel.querySelector('[role="tablist"]')).toBeNull();
    expect(text(panel.querySelector('.command-client')!)).toBe('APT');
    expect(panel.querySelector('a.setup-link')!.getAttribute('href')).toBe('/projects/web/repositories/apt-local/setup?client=apt');
    await expectAccessible(panel);
    trigger.click();
    await settle(r.fixture);
  });

  it('R-OCI-041 an image\'s tabs are Docker and Podman, each with the pull by its digest as the second line', async () => {
    const r = await renderScreen(AnImage);
    document.body.appendChild(r.root);
    const trigger = r.root.querySelector<HTMLButtonElement>('quader-client-command-menu button')!;
    trigger.click();
    await settle(r.fixture);
    const panel = document.getElementById(trigger.getAttribute('aria-controls')!)!;
    expect([...panel.querySelectorAll('[role="tab"]')].map((t) => text(t))).toEqual(['Docker', 'Podman']);
    const shown = [...panel.querySelectorAll<HTMLInputElement>('[role="tabpanel"]:not([inert]) quader-address-field input')];
    expect(shown.map((f) => f.getAttribute('aria-label'))).toEqual(['Command for Docker CLI (with Buildx)', 'Command for Docker CLI (with Buildx) by digest']);
    expect(shown[1]!.value).toMatch(new RegExp(`team/app@${DIGEST}$`));
    await expectAccessible(panel);
    trigger.click();
    await settle(r.fixture);
  });

  it('R-OCI-042 a chart offers Helm alone, its setup Helm\'s, and no pull by digest', async () => {
    const r = await renderScreen(AChart);
    document.body.appendChild(r.root);
    const trigger = r.root.querySelector<HTMLButtonElement>('quader-client-command-menu button')!;
    trigger.click();
    await settle(r.fixture);
    const panel = document.getElementById(trigger.getAttribute('aria-controls')!)!;
    expect(panel.querySelector('[role="tablist"]')).toBeNull();
    expect(text(panel.querySelector('.command-client')!)).toBe('Helm');
    expect(panel.querySelector('a.setup-link')!.getAttribute('href')).toBe('/projects/web/repositories/oci-local/setup?client=helm');
    expect(panel.querySelectorAll('quader-client-command')).toHaveLength(1);
    trigger.click();
    await settle(r.fixture);
  });

  it('R-UI-044 Escape, the button and a click outside close the panel, and the focus goes back to the button', async () => {
    const r = await render();
    const panel = await r.open();
    panel.querySelector<HTMLInputElement>('quader-address-field input')!.focus();
    panel.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }));
    await settle(r.fixture);
    expect(r.panel()).toBeNull();
    expect(r.trigger.getAttribute('aria-expanded')).toBe('false');
    expect(document.activeElement).toBe(r.trigger);

    await r.open();
    r.trigger.click();
    await settle(r.fixture);
    expect(r.panel()).toBeNull();
    expect(document.activeElement).toBe(r.trigger);

    await r.open();
    document.body.dispatchEvent(new MouseEvent('pointerdown', { bubbles: true }));
    document.body.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    await settle(r.fixture);
    expect(r.panel()).toBeNull();
    expect(document.activeElement).toBe(r.trigger);

    // A click on another control keeps that control's focus.
    await r.open();
    const elsewhere = r.root.querySelector<HTMLButtonElement>('.elsewhere')!;
    elsewhere.focus();
    elsewhere.dispatchEvent(new MouseEvent('pointerdown', { bubbles: true }));
    elsewhere.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    await settle(r.fixture);
    expect(r.panel()).toBeNull();
    expect(document.activeElement).toBe(elsewhere);
  });
});

/** A row of a list that knows its version but not its repositories: the panel reads the version when it opens (R-UI-269). */
@Component({
  imports: [ClientCommandMenu],
  template: `<quader-client-command-menu [format]="format()" [name]="name()" [version]="version()" verb="Install" [resolve]="read(id(), version())" />`,
})
class ARow {
  readonly format = input('npm');
  readonly name = input('@team-a/ui');
  readonly version = input('1.4.0');
  readonly id = input('pkg_01JBQ6V7S3K9');
  protected readonly read = versionCommandReads();
}

/** Holds the reads of a version until `release`, counts them, and fails the next one where asked. */
function holdVersions(fake: FakePackagesApi): { calls: () => number; release: () => void; failNext: () => void } {
  const real = fake.version.bind(fake);
  let calls = 0;
  let fail = false;
  let waiting: (() => void)[] = [];
  let held = true;
  fake.version = (id: string, v: string): Promise<PackageVersion> => {
    calls++;
    const failing = fail;
    fail = false;
    const failure = new ApiError(500, { code: 'internal' }, 'req-1', null, { call: { method: 'GET', path: `/api/v1/packages/${id}/versions/${v}` } });
    const answer = () => (failing ? Promise.reject(failure) : real(id, v));
    if (!held) return answer();
    return new Promise<void>((done) => waiting.push(done)).then(answer);
  };
  return {
    calls: () => calls,
    release: () => {
      held = false;
      waiting.forEach((w) => w());
      waiting = [];
    },
    failNext: () => (fail = true),
  };
}

type Get = <A>(token: abstract new (...args: never[]) => A) => A;

async function renderRow(options: { shown?: boolean; setup?: (fake: FakePackagesApi, get: Get) => void; row?: Record<string, string> } = {}) {
  let hold!: ReturnType<typeof holdVersions>;
  const r = await renderScreen(ARow, {
    providers: options.shown === false ? [{ provide: REPOSITORIES_SHOWN, useValue: signal(false) }] : [],
    inputs: options.row ?? {},
    setup: (get) => {
      const fake = get(PackagesApi) as FakePackagesApi;
      options.setup?.(fake, get);
      hold = holdVersions(fake);
    },
  });
  document.body.appendChild(r.root);
  const trigger = r.root.querySelector<HTMLButtonElement>('quader-client-command-menu button')!;
  const panel = () => (trigger.getAttribute('aria-controls') ? document.getElementById(trigger.getAttribute('aria-controls')!) : null);
  const click = async () => {
    trigger.click();
    await settle(r.fixture);
  };
  return { ...r, hold, trigger, panel, click };
}

describe('The commands menu of a list\'s row', () => {
  afterEach(() => document.querySelectorAll('quader-client-command-menu').forEach((e) => e.closest('[ng-version], div')?.remove()));

  it('R-UI-269 a row\'s panel reads the version when it opens and offers its repositories, once for two opens', async () => {
    const r = await renderRow();
    const setup = r.get(ClientSetupReads) as FakeClientSetupReads;
    expect(spokenText(r.trigger)).toBe('1.4.0: Install');
    expect(r.hold.calls()).toBe(0);

    await r.click();
    // The spinner with its sentence while the version is read (R-UI-217); no command is asked yet.
    expect(text(r.panel()!.querySelector('quader-loading')!)).toBe('Loading the commands.');
    expect(r.hold.calls()).toBe(1);
    expect(setup.calls).toEqual([]);
    r.hold.release();
    await settle(r.fixture);

    // The head's rule: a virtual repository that holds it first, then the local one, the select above the clients.
    const panel = r.panel()!;
    expect(panel.querySelector('quader-loading')).toBeNull();
    expect([...panel.querySelectorAll<HTMLOptionElement>('.command-repository option')].map((o) => o.value)).toEqual(['web/npm-all', 'web/npm-local']);
    expect([...panel.querySelectorAll('[role="tab"]')].map((t) => text(t))).toEqual(['npm', 'pnpm', 'Yarn Berry', 'Yarn Classic']);
    expect(setup.calls.every((c) => c.repository === 'web/npm-all' && c.query.version === '1.4.0')).toBe(true);
    await expectAccessible(panel);

    // Closed and opened again: the answer is kept, no second read.
    await r.click();
    await r.click();
    expect(r.panel()!.querySelector('.command-repository')).not.toBeNull();
    expect(r.hold.calls()).toBe(1);
  });

  it('R-UI-269 a row\'s version that only disabled repositories hold says why instead of a command, and ClamAV\'s leads to its Set up client', async () => {
    const disabled = await renderRow({
      setup: (fake) => {
        const v = fake.versionList.find((x) => x.packageId === 'pkg_01JBQ6V7S3K9' && x.version === '1.4.0')!;
        v.repositories = v.repositories.map((h) => ({ ...h, available: false }));
      },
    });
    disabled.hold.release();
    await disabled.click();
    const note = disabled.panel()!.querySelector('[data-testid="install-disabled"]')!;
    expect(text(note)).toBe('The repository web/npm-local is disabled: nobody can install this version until it is enabled again.');
    expect(disabled.panel()!.querySelector('quader-client-command, [role="tab"]')).toBeNull();
    disabled.root.remove();

    const clamav = await renderRow({
      row: { format: 'clamav', name: 'daily', version: '28190', id: clamavPackageId('daily') },
      setup: (fake, get) => {
        clamavFixture(fake);
        (get(UpstreamApi) as FakeUpstreamApi).set('clamav', { clients: { enabled: true, address: CLAMAV_ENDPOINT, certificateAddresses: [] }, visibility: 'public' });
      },
    });
    clamav.hold.release();
    await clamav.click();
    // The switch of ClamAV's definitions is read with the version (Q-UI-908).
    await waitUntil(clamav.fixture, () => clamav.panel()?.querySelector('a.setup-link') != null, 'the panel of a ClamAV database');
    const panel = clamav.panel()!;
    const link = panel.querySelector<HTMLAnchorElement>('a.setup-link')!;
    expect(link.getAttribute('href')).toBe('/projects/upstream/repositories/clamav/setup?client=freshclam');
    expect(text(panel.querySelector('[data-testid="install-clamav"]')!)).toContain('A ClamAV installation always takes the version in use');
    expect(panel.querySelector('quader-client-command')).toBeNull();
  });

  it('R-UI-269 a failed read is the notice with Retry in the panel, and Retry reads again', async () => {
    const r = await renderRow();
    r.hold.release();
    r.hold.failNext();
    await r.click();
    const notice = r.panel()!.querySelector('quader-error-notice')!;
    expect(notice).not.toBeNull();
    const retry = [...notice.querySelectorAll('button')].find((b) => text(b) === 'Retry')!;
    retry.click();
    await settle(r.fixture);
    expect(r.panel()!.querySelector('quader-error-notice')).toBeNull();
    expect(r.panel()!.querySelectorAll('[role="tab"]')).toHaveLength(4);
    expect(r.hold.calls()).toBe(2);
  });

  it('R-UI-269 a row whose version changes while its open panel reads asks the new version at once', async () => {
    const r = await renderRow();
    const setup = r.get(ClientSetupReads) as FakeClientSetupReads;
    await r.click();
    r.fixture.componentRef.setInput('version', '1.3.0');
    await settle(r.fixture);
    r.hold.release();
    await settle(r.fixture);
    expect(r.hold.calls()).toBe(2);
    expect(r.panel()!.querySelectorAll('[role="tab"]')).toHaveLength(4);
    expect(setup.calls.every((c) => c.query.version === '1.3.0')).toBe(true);
  });

  it('R-UI-269 the panel offers no select of repositories while REPOSITORIES_SHOWN is false, and its commands go through the first', async () => {
    const r = await renderRow({ shown: false });
    const setup = r.get(ClientSetupReads) as FakeClientSetupReads;
    r.hold.release();
    await r.click();
    expect(r.panel()!.querySelector('.command-repository')).toBeNull();
    expect(setup.calls.length).toBeGreaterThan(0);
    expect(setup.calls.every((c) => c.repository === 'web/npm-all')).toBe(true);
  });
});
