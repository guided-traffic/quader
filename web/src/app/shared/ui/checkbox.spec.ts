import { Component, signal } from '@angular/core';
import { TestBed } from '@angular/core/testing';
import { seriousViolations } from '../../testing/axe';
import { settle } from '../../testing/screen';
import { Checkbox } from './checkbox';

@Component({
  imports: [Checkbox],
  template: `
    <quader-checkbox [checked]="on()" (checkedChange)="on.set($event)" describedBy="note">Allow it</quader-checkbox>
    <p id="note">Only for admins.</p>
    <quader-checkbox [checked]="false" [disabled]="true">Locked</quader-checkbox>
    <quader-checkbox [checked]="false" ariaLabel="Select row 1" />
    <quader-checkbox
      class="helped"
      [checked]="helped()"
      (checkedChange)="helped.set($event)"
      describedBy="why"
      help="Artifacts that are not checked yet wait for their scan."
      about="Only checked artifacts"
      >Only checked artifacts</quader-checkbox
    >
    <p id="why">The installation lets only checked artifacts be downloaded.</p>
  `,
})
class Host {
  readonly on = signal(false);
  readonly helped = signal(false);
}

describe('Quader checkbox', () => {
  async function render() {
    await TestBed.configureTestingModule({ imports: [Host] }).compileComponents();
    const fixture = TestBed.createComponent(Host);
    document.body.appendChild(fixture.nativeElement);
    await settle(fixture);
    const root = fixture.nativeElement as HTMLElement;
    return { fixture, root, boxes: [...root.querySelectorAll<HTMLInputElement>('input[type="checkbox"]')] };
  }

  it('R-UI-044 is a native checkbox named by its label or its aria-label, linked to its note, and passes axe', async () => {
    const { root, boxes } = await render();
    expect(boxes[0]!.labels?.[0]?.textContent?.trim()).toBe('Allow it');
    expect(boxes[0]!.getAttribute('aria-describedby')).toBe('note');
    expect(boxes[1]!.disabled).toBe(true);
    expect(boxes[2]!.getAttribute('aria-label')).toBe('Select row 1');
    expect(await seriousViolations(root)).toEqual([]);
  });

  it('R-UI-145 the box is a direct child of its .check-row, which keeps it at its size beside a long label', async () => {
    const { boxes } = await render();
    for (const box of boxes) expect(box.parentElement!.matches('.check-row')).toBe(true);
  });

  it('R-UI-267 a help sign follows the label outside it: the label still names the box, the sign pins and never toggles', async () => {
    const { fixture, root, boxes } = await render();
    const box = boxes[3]!;
    expect(box.labels?.[0]?.textContent?.trim()).toBe('Only checked artifacts');
    expect(box.getAttribute('aria-describedby')).toBe('why');
    const sign = root.querySelector<HTMLButtonElement>('.helped button.help-sign')!;
    expect(sign.closest('label')).toBeNull();
    expect(sign.getAttribute('aria-label')).toBe('About Only checked artifacts');
    sign.click();
    await settle(fixture);
    expect(box.checked).toBe(false);
    expect(fixture.componentInstance.helped()).toBe(false);
    box.labels![0]!.click();
    await settle(fixture);
    expect(fixture.componentInstance.helped()).toBe(true);
    expect(await seriousViolations(root)).toEqual([]);
  });

  it('a click toggles it and reports the new state', async () => {
    const { fixture, boxes } = await render();
    boxes[0]!.click();
    await settle(fixture);
    expect(fixture.componentInstance.on()).toBe(true);
    expect(boxes[0]!.checked).toBe(true);
  });
});
