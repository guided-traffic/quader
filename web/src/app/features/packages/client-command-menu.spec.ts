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

  it('R-UI-052 opens a panel hung from its button with a section per client, each its command and copy button, asked only on opening', async () => {
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
    expect([...panel.querySelectorAll('.menu-heading')].map((h) => text(h))).toEqual(['npm', 'pnpm', 'Yarn Berry (2 and later)', 'Yarn Classic (1.x)']);
    expect(setup.calls.map((c) => c.query.client)).toEqual(['npm', 'pnpm', 'yarn', 'yarn-classic']);
    expect(setup.calls.every((c) => c.query.credentials === 'none' && c.query.version === '1.4.0' && c.repository === 'web/npm-local')).toBe(true);
    const first = panel.querySelector<HTMLInputElement>('quader-address-field input')!;
    expect(first.value).toBe('npm install @team-a/ui@1.4.0');
    expect(first.getAttribute('aria-label')).toBe('Command for npm');
    await expectAccessible(panel);

    const writeText = vi.fn().mockResolvedValue(undefined);
    Object.defineProperty(navigator, 'clipboard', { value: { writeText }, configurable: true });
    const copy = panel.querySelector<HTMLButtonElement>('quader-copy-button button')!;
    expect(copy.getAttribute('aria-label')).toBe('Copy the command for npm');
    copy.click();
    await settle(r.fixture);
    expect(writeText).toHaveBeenCalledWith('npm install @team-a/ui@1.4.0');
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
