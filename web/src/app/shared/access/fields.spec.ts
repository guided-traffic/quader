import { Component, signal } from '@angular/core';
import { TestBed } from '@angular/core/testing';
import { describe, expect, it } from 'vitest';
import { text } from '../../testing/screen';
import { FieldWidth, SelectField, SelectOption, TextField } from './fields';

@Component({
  imports: [SelectField],
  template: `<quader-select-field label="Project" name="project" [options]="options" [loading]="loading()" [width]="width()" [(value)]="value" />`,
})
class Host {
  readonly options: readonly SelectOption[] = [
    { value: '', label: 'Choose a project' },
    { value: 'web', label: 'web' },
  ];
  readonly loading = signal<string | null>(null);
  readonly width = signal<'name' | 'full'>('name');
  readonly value = signal('');
}

async function render() {
  TestBed.resetTestingModule();
  await TestBed.configureTestingModule({ imports: [Host] }).compileComponents();
  const fixture = TestBed.createComponent(Host);
  fixture.detectChanges();
  await fixture.whenStable();
  const root = fixture.nativeElement as HTMLElement;
  return { fixture, host: fixture.componentInstance, root, select: () => root.querySelector('select') as HTMLSelectElement };
}

describe('The select field (R-UI-085)', () => {
  it('R-UI-085 a select takes the field width of a choice, half the column, unless its place fills a line', async () => {
    const { fixture, host, root, select } = await render();
    expect([...select().classList]).toEqual(['select-input', 'field-w-name']);
    expect(root.querySelector('quader-select-field')!.classList.contains('full')).toBe(false);
    host.width.set('full');
    fixture.detectChanges();
    expect(select().classList).toContain('field-w-full');
    expect(root.querySelector('quader-select-field')!.classList.contains('full')).toBe(true);
  });

  it('while its options load it is the same select, held and busy, its one option saying so', async () => {
    const { fixture, host, select } = await render();
    host.loading.set('Loading projects…');
    fixture.detectChanges();
    const held = select();
    expect(held.disabled).toBe(true);
    expect(held.getAttribute('aria-busy')).toBe('true');
    expect([...held.options].map((o) => text(o))).toEqual(['Loading projects…']);
    host.loading.set(null);
    fixture.detectChanges();
    expect(select()).toBe(held);
    expect(held.disabled).toBe(false);
    expect(held.getAttribute('aria-busy')).toBeNull();
    expect([...held.options].map((o) => text(o))).toEqual(['Choose a project', 'web']);
  });
});

@Component({
  imports: [TextField],
  template: `
    <quader-text-field label="Priority" name="priority" type="number" [width]="width()" [(value)]="value" />
    <quader-text-field width="full" label="Description" name="description" type="textarea" [(value)]="value" />
  `,
})
class TextHost {
  readonly width = signal<FieldWidth>('number');
  readonly value = signal('');
}

describe('The text field (R-UI-085)', () => {
  it('R-UI-085 a text field takes the width its place names, a number field the width of its number, an area the column', async () => {
    TestBed.resetTestingModule();
    await TestBed.configureTestingModule({ imports: [TextHost] }).compileComponents();
    const fixture = TestBed.createComponent(TextHost);
    fixture.detectChanges();
    await fixture.whenStable();
    const root = fixture.nativeElement as HTMLElement;
    const input = root.querySelector('input') as HTMLInputElement;
    expect([...input.classList]).toEqual(['text-input', 'field-w-number']);
    fixture.componentInstance.width.set('duration');
    fixture.detectChanges();
    expect([...input.classList]).toEqual(['text-input', 'field-w-duration']);
    expect([...root.querySelector('textarea')!.classList]).toEqual(['area-input', 'field-w-full']);
  });

  it('R-UI-267 the hint keeps the format under the field; the help is the sign after the label, outside it', async () => {
    TestBed.resetTestingModule();
    await TestBed.configureTestingModule({ imports: [HelpHost] }).compileComponents();
    const fixture = TestBed.createComponent(HelpHost);
    fixture.detectChanges();
    await fixture.whenStable();
    const root = fixture.nativeElement as HTMLElement;
    const input = root.querySelector('input') as HTMLInputElement;
    const label = root.querySelector('label') as HTMLLabelElement;
    expect(label.control).toBe(input);
    expect(label.querySelector('button')).toBeNull();
    const sign = root.querySelector<HTMLButtonElement>('.field-label-line > quader-help button.help-sign')!;
    expect(sign.getAttribute('aria-label')).toBe('About Exempt addresses');
    expect(document.getElementById(sign.getAttribute('aria-describedby')!)?.textContent).toBe('No flood figure, timeout or block applies to them.');
    // The format is the input's description; the meaning is the sign's, not the field's.
    expect(document.getElementById(input.getAttribute('aria-describedby')!)?.textContent).toBe('Addresses or networks, separated by commas');
    expect(root.querySelectorAll('.hint')).toHaveLength(1);
  });
});

@Component({
  imports: [TextField],
  template: `<quader-text-field
    width="full"
    label="Exempt addresses"
    hint="Addresses or networks, separated by commas"
    help="No flood figure, timeout or block applies to them."
  />`,
})
class HelpHost {}
