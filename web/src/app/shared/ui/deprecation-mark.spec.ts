import { Component, signal } from '@angular/core';
import { TestBed } from '@angular/core/testing';
import { DeprecationLike, DeprecationMark } from './deprecation-mark';

@Component({
  imports: [DeprecationMark],
  template: `<p><quader-deprecation-mark [deprecation]="deprecation()" [unlisted]="unlisted()" /></p>`,
})
class Host {
  readonly deprecation = signal<DeprecationLike | boolean | null>(null);
  readonly unlisted = signal(false);
}

describe('The mark of a deprecated or unlisted version (R-API-085, Q-UI-1300)', () => {
  function render(deprecation: DeprecationLike | boolean | null, unlisted = false): HTMLElement {
    const fixture = TestBed.createComponent(Host);
    fixture.componentInstance.deprecation.set(deprecation);
    fixture.componentInstance.unlisted.set(unlisted);
    fixture.detectChanges();
    return fixture.nativeElement as HTMLElement;
  }

  it('R-API-085 says Deprecated as a warning badge with the message as its title, as text', () => {
    const root = render({ scope: 'version', message: 'Use <b>2.0</b> instead' });
    const badge = root.querySelector('[data-testid="deprecated-mark"]')!;
    expect(badge.classList).toContain('warn');
    expect(badge.textContent?.trim()).toContain('Deprecated');
    expect(badge.getAttribute('title')).toBe('Use <b>2.0</b> instead');
    expect(root.querySelector('b')).toBeNull();
  });

  it('R-API-085 a list that knows only that there is one marks it without a title; nothing for none', () => {
    expect(render(true).querySelector('[data-testid="deprecated-mark"]')?.getAttribute('title')).toBeNull();
    expect(render(null).textContent?.trim()).toBe('');
  });

  it('R-NUGET-036 Unlisted is a mark of its own word, never a deprecation, and both stand together', () => {
    const root = render({ scope: 'version' }, true);
    expect(root.querySelector('[data-testid="unlisted-mark"]')?.textContent).toContain('Unlisted');
    expect(root.querySelector('[data-testid="unlisted-mark"]')?.classList).not.toContain('warn');
    expect(render(null, true).querySelector('[data-testid="deprecated-mark"]')).toBeNull();
  });
});
