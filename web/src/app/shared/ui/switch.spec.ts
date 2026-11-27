import { Component, signal } from '@angular/core';
import { expectAccessible, renderScreen, settle, text } from '../../testing/screen';
import { Switch } from './switch';

@Component({
  imports: [Switch],
  template: `<p>
    <quader-switch [checked]="on()" [busy]="busy()" [disabled]="disabled()" [unavailable]="reason()" label="Switch on Docker Hub" (checkedChange)="asked.push($event)" />
  </p>`,
})
class Host {
  readonly on = signal(false);
  readonly busy = signal(false);
  readonly disabled = signal(false);
  readonly reason = signal('');
  readonly asked: boolean[] = [];
}

describe('The switch', () => {
  it('R-UI-187 is a button with the role switch, its name and aria-checked, and the word On or Off after it hidden from assistive technology', async () => {
    const { fixture, root } = await renderScreen(Host);
    const sw = root.querySelector<HTMLButtonElement>('button[role="switch"]')!;
    expect(sw.getAttribute('aria-label')).toBe('Switch on Docker Hub');
    expect(sw.getAttribute('aria-checked')).toBe('false');
    expect(text(root.querySelector('.word')!)).toBe('Off');
    expect(root.querySelector('.word')!.getAttribute('aria-hidden')).toBe('true');
    fixture.componentInstance.on.set(true);
    await settle(fixture);
    expect(sw.getAttribute('aria-checked')).toBe('true');
    expect(text(root.querySelector('.word')!)).toBe('On');
    await expectAccessible(root);
  });

  it('R-UI-187 a click asks for the other state and leaves the state to the screen; busy and disabled ask nothing', async () => {
    const { fixture, root } = await renderScreen(Host);
    const host = fixture.componentInstance;
    const sw = root.querySelector<HTMLButtonElement>('button[role="switch"]')!;
    sw.click();
    expect(host.asked).toEqual([true]);
    // The switch shows what the screen set, not the click.
    await settle(fixture);
    expect(sw.getAttribute('aria-checked')).toBe('false');
    host.busy.set(true);
    await settle(fixture);
    expect(sw.getAttribute('aria-busy')).toBe('true');
    sw.click();
    host.busy.set(false);
    host.disabled.set(true);
    await settle(fixture);
    expect(sw.disabled).toBe(true);
    sw.click();
    expect(host.asked).toEqual([true]);
  });

  it('R-UI-270 a switch the Helm configuration fixes stays, greyed out and aria-disabled, says its reason, and asks nothing', async () => {
    const { fixture, root } = await renderScreen(Host);
    const host = fixture.componentInstance;
    host.on.set(true);
    host.disabled.set(true);
    host.reason.set('Set in signatures.anchors of the Helm configuration: change it there.');
    await settle(fixture);
    const sw = root.querySelector<HTMLButtonElement>('button[role="switch"]')!;
    expect(sw.disabled).toBe(false);
    expect(sw.getAttribute('aria-disabled')).toBe('true');
    expect(sw.getAttribute('aria-checked')).toBe('true');
    expect(document.getElementById(sw.getAttribute('aria-describedby')!)?.textContent).toBe(
      'Set in signatures.anchors of the Helm configuration: change it there.',
    );
    sw.click();
    expect(host.asked).toEqual([]);
    await expectAccessible(root);
  });
});
