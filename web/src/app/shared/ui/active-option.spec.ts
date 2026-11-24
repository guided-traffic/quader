import { signal } from '@angular/core';
import { describe, expect, it } from 'vitest';
import { ActiveOption, heldEnter, optionDomId } from './active-option';

/** A `mousemove` at a position of the screen. */
const at = (x: number, y: number): MouseEvent => new MouseEvent('mousemove', { screenX: x, screenY: y });

describe('The active entry of a combobox (R-UI-097, R-UI-150)', () => {
  it('R-UI-097 R-UI-150 the arrows walk the list without wrapping; Up from the first entry returns to the text', () => {
    const list = signal(['a', 'b', 'c']);
    const active = new ActiveOption(list);
    expect(active.index()).toBe(-1);
    active.next();
    active.next();
    expect(active.current()).toBe('b');
    active.next();
    active.next();
    expect(active.current()).toBe('c');
    active.previous();
    active.previous();
    active.previous();
    expect(active.current()).toBeNull();
    expect(active.index()).toBe(-1);
  });

  it('R-UI-097 R-UI-150 a new list keeps the entry the person walked to active where it now stands', () => {
    const list = signal(['a', 'b', 'c']);
    const active = new ActiveOption(list);
    active.next();
    active.next();
    list.set(['x', 'y', 'a', 'b', 'c']);
    expect(active.current()).toBe('b');
    expect(active.index()).toBe(3);
    // Down goes on from where the entry now stands, not from its old position.
    active.next();
    expect(active.current()).toBe('c');
  });

  it('R-UI-097 R-UI-150 a new list without the entry leaves none active, and the entry does not come back with a later list', () => {
    const list = signal(['a', 'b', 'c']);
    const active = new ActiveOption(list);
    active.next();
    active.next();
    list.set(['a', 'c']);
    expect(active.current()).toBeNull();
    expect(active.index()).toBe(-1);
    list.set(['a', 'b', 'c']);
    expect(active.current()).toBeNull();
    active.next();
    expect(active.current()).toBe('a');
  });

  it('R-UI-097 R-UI-150 a list that becomes empty leaves none active, and Down does nothing in it', () => {
    const list = signal<readonly string[]>(['a']);
    const active = new ActiveOption(list);
    active.next();
    list.set([]);
    expect(active.current()).toBeNull();
    active.next();
    expect(active.current()).toBeNull();
  });

  it('R-UI-097 R-UI-150 the pointer takes an entry when it moves, never when the list moves under a resting pointer', () => {
    const list = signal(['a', 'b', 'c']);
    const active = new ActiveOption(list);
    active.pointer(0, at(10, 10));
    expect(active.current()).toBe('a');
    active.next();
    expect(active.current()).toBe('b');
    // The list changes under the pointer; the browser reports a move at the same position over another entry.
    list.set(['x', 'a', 'b', 'c']);
    active.pointer(0, at(10, 10));
    expect(active.current()).toBe('b');
    active.pointer(3, at(10, 70));
    expect(active.current()).toBe('c');
  });

  it('R-UI-097 R-UI-150 an option is named by its identity, so the name stays when the entry moves', () => {
    expect(optionDomId('search-option', 'place-/projects')).toBe('search-option-place-%2Fprojects');
    expect(optionDomId('search-option', 'more-repository-a b')).toBe('search-option-more-repository-a%20b');
  });

  it('R-UI-097 R-UI-150 only a repeated keydown of Enter is a held Enter', () => {
    expect(heldEnter(new KeyboardEvent('keydown', { key: 'Enter', repeat: true }))).toBe(true);
    expect(heldEnter(new KeyboardEvent('keydown', { key: 'Enter' }))).toBe(false);
    expect(heldEnter(new KeyboardEvent('keydown', { key: 'ArrowDown', repeat: true }))).toBe(false);
  });
});
