import { Signal, WritableSignal, computed, linkedSignal } from '@angular/core';

/**
 * The active entry of a combobox's listbox, shared by the search field (R-UI-097) and the principal field (R-UI-150).
 *
 * The entry is held by its identity, not by its position (Q-UI-700):
 * when a newer answer replaces the list, the entry the person walked to stays active wherever it now stands; when the new list
 * does not hold it, or holds nothing, no entry is active and the text is, so Enter does what it does without an active entry.
 * An entry is never activated by a change of the list alone.
 *
 * The pointer activates an entry only when it moves (Q-UI-701):
 * a browser sends a `mousemove` with the old position when the content under a resting pointer changes or scrolls, and that
 * must not take the keyboard's place away.
 */
export class ActiveOption {
  private readonly key: WritableSignal<string | null>;
  /** The position of the active entry in the list, or -1 while the text is active. */
  readonly index: Signal<number>;
  private pointerAt: { readonly x: number; readonly y: number } | null = null;

  /** `keys` are the identities of the entries in their order, unique within the list. */
  constructor(private readonly keys: () => readonly string[]) {
    this.key = linkedSignal<readonly string[], string | null>({
      source: keys,
      computation: (list, previous) => {
        const held = previous?.value ?? null;
        return held !== null && list.includes(held) ? held : null;
      },
    });
    this.index = computed(() => {
      const key = this.key();
      return key === null ? -1 : this.keys().indexOf(key);
    });
  }

  /** The identity of the active entry, or null while the text is active. */
  current(): string | null {
    return this.key();
  }

  /** The text becomes active. */
  clear(): void {
    this.key.set(null);
  }

  /** Down: from the text the first entry, then the next; the last entry stays active (nothing wraps). */
  next(): void {
    const list = this.keys();
    if (list.length === 0) return;
    this.key.set(list[Math.min(this.index() + 1, list.length - 1)]!);
  }

  /** Up: the entry before; from the first entry back to the text. */
  previous(): void {
    const i = this.index();
    this.key.set(i <= 0 ? null : this.keys()[i - 1]!);
  }

  /** The pointer moved over the entry at `index`; a `mousemove` at the position of the one before is the content moving, not the pointer. */
  pointer(index: number, event: MouseEvent): void {
    const at = { x: event.screenX, y: event.screenY };
    if (this.pointerAt !== null && this.pointerAt.x === at.x && this.pointerAt.y === at.y) return;
    this.pointerAt = at;
    const key = this.keys()[index];
    if (key !== undefined) this.key.set(key);
  }
}

/**
 * The DOM id of an option, derived from the entry's identity rather than its position: `aria-activedescendant` keeps naming the same
 * entry when it moves within a new list, and a screen reader does not announce a move as a new entry.
 */
export function optionDomId(prefix: string, key: string): string {
  return `${prefix}-${encodeURIComponent(key)}`;
}

/** A repeated `keydown` of a held key: a held Enter acts once (Q-UI-701). */
export function heldEnter(event: KeyboardEvent): boolean {
  return event.key === 'Enter' && event.repeat;
}
