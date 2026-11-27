import { Component } from '@angular/core';
import { TestBed } from '@angular/core/testing';
import { settle, sleep } from '../../testing/screen';
import { Tooltip } from './tooltip';

@Component({
  imports: [Tooltip],
  template: `<button type="button" aria-label="About the column" quaderTooltip="Counts the files">?</button>`,
})
class Host {}

@Component({
  imports: [Tooltip],
  template: `
    <button type="button" class="plain" aria-label="Plain" quaderTooltip="Not pinned">?</button>
    <button type="button" class="pin" aria-label="Pinned" quaderTooltip="Pinned by a tap" [quaderTooltipPin]="true">i</button>
  `,
})
class PinHost {}

describe('Quader tooltip', () => {
  async function render() {
    await TestBed.configureTestingModule({ imports: [Host] }).compileComponents();
    const fixture = TestBed.createComponent(Host);
    document.body.appendChild(fixture.nativeElement);
    await settle(fixture);
    const button = fixture.nativeElement.querySelector('button') as HTMLButtonElement;
    return { fixture, button };
  }
  const bubble = () => document.querySelector('quader-tooltip');

  it('R-UI-044 describes its element always, shows on focus and pointer, and hides on Escape and blur', async () => {
    const { fixture, button } = await render();
    const describedBy = button.getAttribute('aria-describedby');
    expect(describedBy).toBeTruthy();
    expect(document.getElementById(describedBy!)?.textContent).toBe('Counts the files');
    expect(bubble()).toBeNull();

    button.dispatchEvent(new Event('focus'));
    await settle(fixture);
    expect(bubble()?.textContent).toBe('Counts the files');
    // The reference's tooltip (Q-UI-1065): above the element first, its text in the box beside an arrow that names nothing.
    expect(bubble()?.getAttribute('data-placement')).toBe('top');
    expect(bubble()?.querySelector('.inner')?.textContent).toBe('Counts the files');
    expect(bubble()?.querySelector('.arrow')?.textContent).toBe('');

    button.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }));
    await settle(fixture);
    expect(bubble()).toBeNull();

    button.dispatchEvent(new Event('mouseenter'));
    await settle(fixture);
    expect(bubble()).not.toBeNull();
    button.dispatchEvent(new Event('blur'));
    await settle(fixture);
    expect(bubble()).toBeNull();
  });

  it('R-UI-267 a pinning tooltip stays after a click when the pointer leaves, until the next click; without the pin a click changes nothing', async () => {
    TestBed.resetTestingModule();
    await TestBed.configureTestingModule({ imports: [PinHost] }).compileComponents();
    const fixture = TestBed.createComponent(PinHost);
    document.body.appendChild(fixture.nativeElement);
    await settle(fixture);
    const root = fixture.nativeElement as HTMLElement;
    const pin = root.querySelector<HTMLButtonElement>('.pin')!;
    pin.click();
    await settle(fixture);
    pin.dispatchEvent(new Event('mouseleave'));
    await sleep(fixture, 150);
    expect(bubble()?.textContent).toBe('Pinned by a tap');
    pin.click();
    await settle(fixture);
    expect(bubble()).toBeNull();

    const plain = root.querySelector<HTMLButtonElement>('.plain')!;
    plain.dispatchEvent(new Event('mouseenter'));
    plain.click();
    await settle(fixture);
    plain.dispatchEvent(new Event('mouseleave'));
    await sleep(fixture, 150);
    expect(bubble()).toBeNull();
  });
});
