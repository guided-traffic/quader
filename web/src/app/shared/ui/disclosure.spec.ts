import { Component } from '@angular/core';
import { vi } from 'vitest';
import { TestBed } from '@angular/core/testing';
import { expectAccessible, settle } from '../../testing/screen';
import { Disclosure } from './disclosure';
import { DisclosureMemory } from '../../core/disclosure-memory';
import { renderScreen } from '../../testing/screen';
import { BrowserStorage } from '../../core/storage';

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

@Component({
  imports: [Disclosure],
  template: `
    <quader-disclosure heading="Details" remember="file.details" (expandedChange)="opened = $event">
      <p class="inside">Every checksum.</p>
    </quader-disclosure>
  `,
})
class RememberedHost {
  opened = false;
}

/** The store of the open Details of one viewer, in memory (Q-UI-967). */
class FakeMemory {
  readonly open = new Set<string>();
  isOpen(id: string): boolean {
    return this.open.has(id);
  }
  set(id: string, open: boolean): void {
    if (open) this.open.add(id);
    else this.open.delete(id);
  }
}

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

  it('a fold inside a section is the inline form: the same toggle and chevron without a heading element or a rule', async () => {
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

  it('R-UI-067 a Details section opens as the viewer left it, and tells its screen so; another fold is not remembered', async () => {
    const memory = new FakeMemory();
    TestBed.resetTestingModule();
    await TestBed.configureTestingModule({ imports: [RememberedHost], providers: [{ provide: DisclosureMemory, useValue: memory }] }).compileComponents();
    let fixture = TestBed.createComponent(RememberedHost);
    await settle(fixture);
    let toggle = (fixture.nativeElement as HTMLElement).querySelector<HTMLButtonElement>('h2 button')!;
    expect(toggle.getAttribute('aria-expanded')).toBe('false');
    toggle.click();
    await settle(fixture);
    expect([...memory.open]).toEqual(['file.details']);
    // The next visit of the screen: open at once, and the screen hears it (the file page then reads the statistics).
    fixture.destroy();
    fixture = TestBed.createComponent(RememberedHost);
    await settle(fixture);
    toggle = (fixture.nativeElement as HTMLElement).querySelector<HTMLButtonElement>('h2 button')!;
    expect(toggle.getAttribute('aria-expanded')).toBe('true');
    expect(fixture.componentInstance.opened).toBe(true);
    toggle.click();
    await settle(fixture);
    expect(memory.open.size).toBe(0);
  });

  it('R-UI-067 the open Details are kept per signed-in viewer in the browser, and a page works when the storage is blocked', async () => {
    new BrowserStorage().clear('local');
    await renderScreen(RememberedHost, {});
    const memory = TestBed.inject(DisclosureMemory);
    memory.set('file.details', true);
    expect(new BrowserStorage().get('local', 'quader.open.alice')).toBe('["file.details"]');
    memory.set('file.details', false);
    expect(new BrowserStorage().get('local', 'quader.open.alice')).toBeNull();
    // Blocked storage: the choice lasts while the page does, nothing throws.
    const blocked = vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {
      throw new Error('blocked');
    });
    try {
      memory.set('repository.details', true);
      expect(memory.isOpen('repository.details')).toBe(true);
    } finally {
      blocked.mockRestore();
    }
  });
});
