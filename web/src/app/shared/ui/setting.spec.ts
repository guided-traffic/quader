import { Component } from '@angular/core';
import { TestBed } from '@angular/core/testing';
import { describe, expect, it } from 'vitest';
import { expectAccessible, settle, text } from '../../testing/screen';
import { Setting } from './setting';

@Component({
  imports: [Setting],
  template: `
    <quader-setting class="throughput" label="Maximum throughput" help="The Bastion's share of the link between the two networks." for="throughput">
      <input id="throughput" class="text-input field-w-number" value="200" />
      <p below class="hint">132 MB/s of 200 MB/s</p>
    </quader-setting>
    <quader-setting #preset class="preset" label="Preset" help="Internet: strict limits, administrators too.">
      <div role="group" [attr.aria-labelledby]="preset.labelId">
        <button type="button" class="btn btn-default" aria-pressed="true">Internet</button>
        <button type="button" class="btn btn-default" aria-pressed="false">Partner network</button>
      </div>
    </quader-setting>
    <quader-setting class="bare" label="Mode">Enforce</quader-setting>
  `,
})
class Host {}

describe('The setting row (R-UI-267)', () => {
  async function render(): Promise<HTMLElement> {
    TestBed.resetTestingModule();
    await TestBed.configureTestingModule({ imports: [Host] }).compileComponents();
    const fixture = TestBed.createComponent(Host);
    await settle(fixture);
    return fixture.nativeElement as HTMLElement;
  }

  it('R-UI-267 with `for` the name is the label of the control; the lines below stand in the control column', async () => {
    const root = await render();
    const row = root.querySelector('quader-setting.throughput')!;
    const label = row.querySelector<HTMLLabelElement>('label.setting-name')!;
    expect(label.htmlFor).toBe('throughput');
    expect(label.control).toBe(root.querySelector('#throughput'));
    // The sign stands after the name, outside the label: its tap never focuses the field.
    expect(label.querySelector('button')).toBeNull();
    expect(row.querySelector('.setting-label button.help-sign')?.getAttribute('aria-label')).toBe('About Maximum throughput');
    expect(text(row.querySelector('.setting-control .setting-below')!)).toBe('132 MB/s of 200 MB/s');
    await expectAccessible(root);
  });

  it('R-UI-267 without `for` the name is a span whose `labelId` names the control; a row without help has no sign', async () => {
    const root = await render();
    const row = root.querySelector('quader-setting.preset')!;
    const name = row.querySelector<HTMLElement>('span.setting-name')!;
    expect(row.querySelector('label')).toBeNull();
    expect(name.id).toMatch(/^quader-setting-\d+$/);
    expect(row.querySelector('[role="group"]')?.getAttribute('aria-labelledby')).toBe(name.id);
    const bare = root.querySelector('quader-setting.bare')!;
    expect(bare.querySelector('button')).toBeNull();
    expect(text(bare.querySelector('.setting-control')!)).toBe('Enforce');
    // Nothing projected below: the empty column of lines takes no room.
    expect(getComputedStyle(bare.querySelector('.setting-below')!).display).toBe('none');
  });
});
