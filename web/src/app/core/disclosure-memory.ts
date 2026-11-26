import { Injectable, InjectionToken, computed, effect, inject, signal, untracked } from '@angular/core';
import { BrowserStorage } from './storage';

/**
 * Whose memory it is: the name of the signed-in principal, read in a reactive context, or null for a reader without a session, whose
 * choices last for the tab. The main installation binds it to its session (`provideViewReads`); without a binding, as in the Bastion's
 * application, every reader is anonymous (R-UI-223, R-UI-228).
 */
export const MEMORY_OWNER = new InjectionToken<() => string | null>('MEMORY_OWNER', {
  providedIn: 'root',
  factory: () => () => null,
});

/**
 * Which **Details** sections the viewer left open (R-UI-067, Q-UI-319, Q-UI-967): one entry per screen and section (`file.details`,
 * `package.details`), in the browser's local storage, keyed by the signed-in principal like the side bar's state (NavState), so
 * that two people on one browser keep their own. Only an open section is kept: closed is every section's default. A convenience of
 * one viewer: blocked or cleared storage leaves every section closed at the next visit, and the page works as before; an anonymous
 * visitor's choice lasts for the tab.
 */
@Injectable({ providedIn: 'root' })
export class DisclosureMemory {
  private readonly storage = inject(BrowserStorage);
  private readonly owner = inject(MEMORY_OWNER);
  private readonly opened = signal<ReadonlySet<string>>(new Set());

  private readonly key = computed(() => {
    const name = this.owner();
    return name ? `quader.open.${name}` : null;
  });

  constructor() {
    effect(() => {
      const key = this.key();
      untracked(() => this.opened.set(key ? this.read(key) : new Set()));
    });
  }

  /** Whether the viewer left the section `id` open. */
  isOpen(id: string): boolean {
    return this.opened().has(id);
  }

  /** The viewer opened or closed the section `id`. */
  set(id: string, open: boolean): void {
    const next = new Set(this.opened());
    if (open) next.add(id);
    else next.delete(id);
    this.opened.set(next);
    const key = this.key();
    if (!key) return;
    if (next.size === 0) this.storage.remove('local', key);
    else this.storage.set('local', key, JSON.stringify([...next].sort()));
  }

  private read(key: string): ReadonlySet<string> {
    try {
      const parsed: unknown = JSON.parse(this.storage.get('local', key) ?? '[]');
      return new Set(Array.isArray(parsed) ? parsed.filter((v): v is string => typeof v === 'string') : []);
    } catch {
      return new Set();
    }
  }
}
