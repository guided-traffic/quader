import { Injectable, computed, effect, inject, signal, untracked } from '@angular/core';
import { SessionStore } from './session-store';
import { BrowserStorage } from './storage';

interface StoredNavState {
  /** True when the viewer collapsed the pinned sidebar (Q-UI-127). */
  readonly collapsed: boolean;
  /** Ids of the groups the viewer folded. */
  readonly closed: readonly string[];
  /** Ids of the groups that start folded and that the viewer opened (Recently opened, R-UI-167). */
  readonly opened: readonly string[];
}

const EMPTY: StoredNavState = { collapsed: false, closed: [], opened: [] };

function strings(v: unknown): string[] {
  return Array.isArray(v) ? v.filter((c): c is string => typeof c === 'string') : [];
}

/**
 * How the viewer left the navigation sidebar (R-UI-002, Q-UI-68, Q-UI-127): shown or collapsed, and
 * which groups are folded. A group is open until the viewer folds it, or, where it is declared folded by default (Recently
 * opened, R-UI-167), folded until the viewer opens it. Non-secret convenience data in browser storage, keyed by the signed-in
 * principal so that two people on one browser keep their own layout (R-UI-043); when storage is blocked the state lasts for the
 * tab only.
 */
@Injectable({ providedIn: 'root' })
export class NavState {
  private readonly storage = inject(BrowserStorage);
  private readonly session = inject(SessionStore);
  private readonly state = signal<StoredNavState>(EMPTY);

  private readonly key = computed(() => {
    const name = this.session.whoami()?.principal?.name;
    return name ? `quader.nav.${name}` : null;
  });

  /** The viewer collapsed the sidebar. Only honoured where the sidebar is pinned; the drawer starts closed. */
  readonly collapsed = computed(() => this.state().collapsed);

  constructor() {
    effect(() => {
      const key = this.key();
      untracked(() => this.state.set(key ? this.read(key) : EMPTY));
    });
  }

  setCollapsed(collapsed: boolean): void {
    this.update({ ...this.state(), collapsed });
  }

  /** Whether the group is open: what the viewer chose, else `openByDefault`. */
  isOpen(groupId: string, openByDefault = true): boolean {
    const s = this.state();
    if (s.closed.includes(groupId)) return false;
    if (s.opened.includes(groupId)) return true;
    return openByDefault;
  }

  toggleGroup(groupId: string, openByDefault = true): void {
    const s = this.state();
    const open = !this.isOpen(groupId, openByDefault);
    const closed = s.closed.filter((id) => id !== groupId);
    const opened = s.opened.filter((id) => id !== groupId);
    // Only a choice that differs from the default is kept.
    if (open && !openByDefault) opened.push(groupId);
    if (!open && openByDefault) closed.push(groupId);
    this.update({ ...s, closed, opened });
  }

  private update(next: StoredNavState): void {
    this.state.set(next);
    const key = this.key();
    if (key) this.storage.set('local', key, JSON.stringify(next));
  }

  private read(key: string): StoredNavState {
    try {
      const parsed: unknown = JSON.parse(this.storage.get('local', key) ?? 'null');
      if (typeof parsed !== 'object' || parsed === null) return EMPTY;
      const { collapsed, closed, opened } = parsed as Record<string, unknown>;
      return { collapsed: collapsed === true, closed: strings(closed), opened: strings(opened) };
    } catch {
      return EMPTY;
    }
  }
}
