import { Component } from '@angular/core';
import { renderScreen, settle, sleep, text } from '../../testing/screen';
import { DETAIL_POPOVER_DELAY_MS, DetailPopover } from './detail-popover';

/** A button with details, as a mark has them, and a link beside it to move the focus to. */
@Component({
  imports: [DetailPopover],
  template: `
    <button [quaderDetailPopover]="card">Checked</button>
    <a href="#elsewhere">elsewhere</a>
    <ng-template #card><p data-testid="card">Checked 3 days ago</p><button type="button">Technical details</button></ng-template>
  `,
})
class Host {}

const card = (): Element | null => document.querySelector('[data-testid="card"]');
const panel = (): HTMLElement | null => document.querySelector('quader-detail-popover-panel');

async function render() {
  const r = await renderScreen(Host);
  const trigger = r.root.querySelector('button')!;
  return { ...r, trigger };
}

describe('Details on demand (R-UI-256)', () => {
  afterEach(() => document.querySelectorAll('.cdk-overlay-container').forEach((c) => (c.innerHTML = '')));

  it('R-UI-256 opens on hover after a pause, stays while the pointer is on the card, and closes when it leaves both', async () => {
    const { fixture, trigger } = await render();
    expect(trigger.getAttribute('aria-expanded')).toBe('false');
    expect(trigger.getAttribute('type')).toBe('button');
    trigger.dispatchEvent(new MouseEvent('mouseenter'));
    await sleep(fixture, DETAIL_POPOVER_DELAY_MS / 3);
    // A pointer that passes over a list opens nothing.
    expect(card()).toBeNull();
    await sleep(fixture, DETAIL_POPOVER_DELAY_MS);
    expect(text(card()!)).toBe('Checked 3 days ago');
    expect(trigger.getAttribute('aria-expanded')).toBe('true');
    expect(trigger.getAttribute('aria-controls')).toBe(panel()!.id);
    // Hoverable (WCAG 1.4.13): from the button onto the card keeps it.
    trigger.dispatchEvent(new MouseEvent('mouseleave'));
    panel()!.parentElement!.dispatchEvent(new MouseEvent('mouseenter'));
    await sleep(fixture, 150);
    expect(card()).not.toBeNull();
    panel()!.parentElement!.dispatchEvent(new MouseEvent('mouseleave'));
    await sleep(fixture, 150);
    expect(card()).toBeNull();
    expect(trigger.getAttribute('aria-expanded')).toBe('false');
    expect(trigger.hasAttribute('aria-controls')).toBe(false);
  });

  it('R-UI-256 opens on keyboard focus at once and closes on Escape with the focus back on the button', async () => {
    const { fixture, trigger } = await render();
    trigger.focus();
    await settle(fixture);
    expect(card()).not.toBeNull();
    trigger.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }));
    await settle(fixture);
    expect(card()).toBeNull();
    expect(document.activeElement).toBe(trigger);
  });

  it('R-UI-256 a tap opens it and keeps it after the pointer leaves; a second tap or a tap outside closes it', async () => {
    const { fixture, root, trigger } = await render();
    trigger.dispatchEvent(new PointerEvent('pointerdown', { bubbles: true }));
    trigger.focus();
    // A pointer's focus does not open it: the tap does.
    await settle(fixture);
    expect(card()).toBeNull();
    trigger.click();
    await settle(fixture);
    expect(card()).not.toBeNull();
    trigger.dispatchEvent(new MouseEvent('mouseleave'));
    await sleep(fixture, 150);
    expect(card()).not.toBeNull();
    // A press inside the card (its Technical details) keeps it.
    panel()!.querySelector('button')!.dispatchEvent(new PointerEvent('pointerdown', { bubbles: true }));
    await settle(fixture);
    expect(card()).not.toBeNull();
    trigger.click();
    await settle(fixture);
    expect(card()).toBeNull();
    trigger.click();
    await settle(fixture);
    expect(card()).not.toBeNull();
    root.querySelector('a')!.dispatchEvent(new PointerEvent('pointerdown', { bubbles: true }));
    await settle(fixture);
    expect(card()).toBeNull();
  });

  it('R-UI-256 the focus leaving the button for another element closes it', async () => {
    const { fixture, root, trigger } = await render();
    trigger.focus();
    await settle(fixture);
    expect(card()).not.toBeNull();
    root.querySelector('a')!.focus();
    await settle(fixture);
    expect(card()).toBeNull();
  });
});
