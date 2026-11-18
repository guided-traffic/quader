import { DestroyRef, Injectable, signal } from '@angular/core';

/**
 * The one pair of refresh controls of the page **System status** (R-UI-164, Q-UI-651): **Pause automatic refresh** and
 * **Refresh now** stand in the page's header and act on the tab that is shown. The tab registers what **Refresh now**
 * reloads and reads `paused` for its own timer; leaving the tab ends its registration.
 *
 * The page provides its own instance, so a pause lasts while the page is open, across its tabs, and ends with it. The root
 * instance serves a tab that is rendered without the page.
 */
@Injectable({ providedIn: 'root' })
export class SystemRefresh {
  /** Whether the automatic refresh of the shown tab is paused (WCAG 2.2.2). */
  readonly paused = signal(false);
  private readonly reloads = new Set<() => void>();

  togglePause(): void {
    this.paused.update((p) => !p);
  }

  /** Reloads what the shown tab shows. */
  now(): void {
    for (const reload of this.reloads) reload();
  }

  /** Called by a tab when it is created; the registration ends when the tab is destroyed. */
  register(reload: () => void, destroyRef: DestroyRef): void {
    this.reloads.add(reload);
    destroyRef.onDestroy(() => this.reloads.delete(reload));
  }
}
