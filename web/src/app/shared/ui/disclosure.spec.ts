import { Component } from '@angular/core';
import { TestBed } from '@angular/core/testing';
import { expectAccessible, settle } from '../../testing/screen';
import { Disclosure } from './disclosure';

@Component({
  imports: [Disclosure],
  template: `
    <quader-disclosure heading="Details" hint="Where this repository fetches from.">
      <p class="inside">Caches https://registry.npmjs.org/</p>
    </quader-disclosure>
  `,
})
class Host {}

@Component({
  imports: [Disclosure],
  template: `
    <quader-disclosure heading="Pattern syntax" [inline]="true">
      <p class="inside">* matches any characters within one path segment.</p>
    </quader-disclosure>
  `,
})
class InlineHost {}

describe('Disclosure', () => {
  it('R-UI-067 the details are one quiet disclosure: a heading at text size with a chevron, one hint line, closed until opened', async () => {
    await TestBed.configureTestingModule({ imports: [Host] }).compileComponents();
    const fixture = TestBed.createComponent(Host);
    await settle(fixture);
    const root = fixture.nativeElement as HTMLElement;
    const toggle = root.querySelector<HTMLButtonElement>('h2 button')!;
    expect(toggle.textContent).toContain('Details');
    expect(root.querySelector('.hint')?.textContent).toBe('Where this repository fetches from.');
    const region = root.querySelector('.collapse-region')!;
    expect(toggle.getAttribute('aria-controls')).toBe(region.id);
    // Closed at first (Q-UI-319): the region is not open, so nothing in it can be focused.
    expect(toggle.getAttribute('aria-expanded')).toBe('false');
    expect(region.classList).not.toContain('open');
    toggle.click();
    await settle(fixture);
    expect(toggle.getAttribute('aria-expanded')).toBe('true');
    expect(region.classList).toContain('open');
    expect(region.querySelector('.inside')?.textContent).toContain('registry.npmjs.org');
    await expectAccessible(root);
  });

  it('Q-UI-552 a fold inside a section is the inline form: the same toggle and chevron without a heading element or a rule', async () => {
    TestBed.resetTestingModule();
    await TestBed.configureTestingModule({ imports: [InlineHost] }).compileComponents();
    const fixture = TestBed.createComponent(InlineHost);
    await settle(fixture);
    const root = fixture.nativeElement as HTMLElement;
    expect(root.querySelector('quader-disclosure')!.classList).toContain('inline');
    expect(root.querySelector('h2')).toBeNull();
    const toggle = root.querySelector<HTMLButtonElement>('button.inline-toggle')!;
    // Its own class keeps it out of a page's list of sections, which reads .toggle.
    expect(toggle.classList).not.toContain('toggle');
    expect(toggle.querySelector('.chevron')?.textContent).toBe('chevron_right');
    expect(toggle.textContent).toContain('Pattern syntax');
    expect(toggle.getAttribute('aria-expanded')).toBe('false');
    toggle.click();
    await settle(fixture);
    expect(toggle.getAttribute('aria-expanded')).toBe('true');
    expect(root.querySelector('.collapse-region')!.classList).toContain('open');
    await expectAccessible(root);
  });
});
