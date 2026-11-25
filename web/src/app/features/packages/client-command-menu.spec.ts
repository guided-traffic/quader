import { Component } from '@angular/core';
import { ClientSetupApi } from '../../api/packages-api';
import { FakeClientSetupApi } from '../../api/testing/fakes-packages';
import { spokenText } from '../../testing/axe';
import { expectAccessible, renderScreen, settle, text } from '../../testing/screen';
import { ClientCommandMenu } from './client-command-menu';

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
    const setup = r.get(ClientSetupApi) as FakeClientSetupApi;
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
