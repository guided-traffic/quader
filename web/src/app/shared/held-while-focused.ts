import { Signal, linkedSignal } from '@angular/core';

/**
 * A suggested default as a text field takes it (R-UI-084, Q-UI-702): it follows `source` while the person is not in the field and
 * keeps what it was while they are. A default that arrives while the field has the focus is taken when the field is left, so a
 * field never changes under the hands of the person typing into it, and a typed character never joins a value that arrived
 * between the focus and the key. Whether a field the person edited takes a default at all stays the form's rule.
 */
export function heldWhileFocused<T>(source: () => T, focused: () => boolean): Signal<T> {
  return linkedSignal<{ readonly value: T; readonly focused: boolean }, T>({
    source: () => ({ value: source(), focused: focused() }),
    computation: (now, previous) => (now.focused && previous !== undefined ? previous.value : now.value),
  }).asReadonly();
}
