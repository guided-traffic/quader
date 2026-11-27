import { Component, signal } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { describe, expect, it } from 'vitest';
import { expectAccessible, settle, sleep } from '../../testing/screen';
import { Help } from './help';

@Component({
  imports: [Help],
  template: `
    <p class="row"><span class="help-anchor">Web interface</span><quader-help [text]="text()" about="Web interface" /></p>
    <p class="column"><span class="help-anchor">Tier</span><quader-help text="Which tier the rule belongs to." about="Tier" [column]="true" /></p>
    <p class="elsewhere">Elsewhere</p>
  `,
})
class Host {
  readonly text = signal('A read-only web interface of this Bastion that shows anyone the packages of public repositories.');
}

describe('The help sign (R-UI-267)', () => {
  async function render(): Promise<{ fixture: ComponentFixture<Host>; root: HTMLElement; sign: HTMLButtonElement }> {
    TestBed.resetTestingModule();
    await TestBed.configureTestingModule({ imports: [Host] }).compileComponents();
    const fixture = TestBed.createComponent(Host);
    document.body.appendChild(fixture.nativeElement);
    await settle(fixture);
    const root = fixture.nativeElement as HTMLElement;
    return { fixture, root, sign: root.querySelector<HTMLButtonElement>('.row button.help-sign')! };
  }
  const bubble = (): Element | null => document.querySelector('quader-tooltip');

  it('R-UI-267 the sign is a button named after what it explains and described by its sentence, with the info icon', async () => {
    const { root, sign } = await render();
    expect(sign.type).toBe('button');
    expect(sign.getAttribute('aria-label')).toBe('About Web interface');
    expect(document.getElementById(sign.getAttribute('aria-describedby')!)?.textContent).toContain('A read-only web interface');
    expect(sign.querySelector('.material-icons')?.textContent).toBe('info');
    expect(sign.querySelector('.material-icons')?.getAttribute('aria-hidden')).toBe('true');
    // No aria-expanded: nothing is disclosed that the description does not carry.
    expect(sign.hasAttribute('aria-expanded')).toBe(false);
    expect(root.querySelector('.column button.help-sign')?.getAttribute('aria-label')).toBe('About the column Tier');
  });

  it('R-UI-267 hover and focus show the bubble; Escape hides it and keeps the focus, and so does the focus leaving', async () => {
    const { fixture, sign } = await render();
    sign.dispatchEvent(new Event('mouseenter'));
    await settle(fixture);
    expect(bubble()?.textContent).toContain('A read-only web interface');
    sign.dispatchEvent(new Event('mouseleave'));
    await sleep(fixture, 150);
    expect(bubble()).toBeNull();

    sign.focus();
    await settle(fixture);
    expect(bubble()).not.toBeNull();
    sign.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }));
    await settle(fixture);
    expect(bubble()).toBeNull();
    expect(document.activeElement).toBe(sign);

    sign.blur();
    sign.focus();
    await settle(fixture);
    expect(bubble()).not.toBeNull();
    sign.blur();
    await settle(fixture);
    expect(bubble()).toBeNull();
  });

  it('R-UI-267 a tap pins the bubble through leaving; a second tap closes it, and so does a press outside the sign and the bubble', async () => {
    const { fixture, root, sign } = await render();
    sign.click();
    await settle(fixture);
    sign.dispatchEvent(new Event('mouseleave'));
    await sleep(fixture, 150);
    expect(bubble()).not.toBeNull();
    // A press on the bubble itself keeps it.
    bubble()!.dispatchEvent(new Event('pointerdown', { bubbles: true }));
    await settle(fixture);
    expect(bubble()).not.toBeNull();
    sign.click();
    await settle(fixture);
    expect(bubble()).toBeNull();

    sign.click();
    await settle(fixture);
    expect(bubble()).not.toBeNull();
    root.querySelector('.elsewhere')!.dispatchEvent(new Event('pointerdown', { bubbles: true }));
    await settle(fixture);
    expect(bubble()).toBeNull();
  });

  it('R-UI-267 an empty text renders no sign', async () => {
    const { fixture, root } = await render();
    fixture.componentInstance.text.set('');
    await settle(fixture);
    expect(root.querySelector('.row button')).toBeNull();
    expect(root.querySelectorAll('button.help-sign')).toHaveLength(1);
  });

  it('R-UI-267 the signs pass the accessibility rules', async () => {
    const { root } = await render();
    await expectAccessible(root);
  });
});
