import { signal } from '@angular/core';
import { describe, expect, it } from 'vitest';
import { heldWhileFocused } from './held-while-focused';

describe('A suggested default and the focus of its field (Q-UI-702)', () => {
  it('R-UI-084 Q-UI-702 follows the default while the field has no focus', () => {
    const source = signal<string | undefined>(undefined);
    const focused = signal(false);
    const shown = heldWhileFocused(source, focused);
    expect(shown()).toBeUndefined();
    source.set('npm-local');
    expect(shown()).toBe('npm-local');
  });

  it('R-UI-084 Q-UI-702 keeps what the field showed while it has the focus, and takes the newest default when it is left', () => {
    const source = signal<string | undefined>(undefined);
    const focused = signal(false);
    const shown = heldWhileFocused(source, focused);
    expect(shown()).toBeUndefined();
    focused.set(true);
    source.set('npm-local');
    expect(shown()).toBeUndefined();
    source.set('npm-remote');
    expect(shown()).toBeUndefined();
    focused.set(false);
    expect(shown()).toBe('npm-remote');
  });

  it('R-UI-084 Q-UI-702 a default the field showed before the focus stays while the focus lasts', () => {
    const source = signal('npm-local');
    const focused = signal(false);
    const shown = heldWhileFocused(source, focused);
    expect(shown()).toBe('npm-local');
    focused.set(true);
    source.set('npm-remote');
    expect(shown()).toBe('npm-local');
    focused.set(false);
    expect(shown()).toBe('npm-remote');
  });
});
