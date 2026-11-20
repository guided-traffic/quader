import { DOCUMENT } from '@angular/common';
import { Injectable, Injector, afterNextRender, effect, inject } from '@angular/core';
import { toSignal } from '@angular/core/rxjs-interop';
import { NavigationEnd, Router } from '@angular/router';
import { filter } from 'rxjs';

/**
 * Moves focus to the new screen's `h1` after a navigation to another screen (WCAG 2.4.3, R-UI-044), or to the main region when
 * the screen has none. The first navigation is left alone so a fresh load does not steal focus from the skip link. A navigation
 * that changes only the query or the fragment stays on the screen and leaves the focus where it is: a list's filter writes `?q=`
 * while the person types, a tab writes `?tab=`.
 */
@Injectable({ providedIn: 'root' })
export class FocusOnNavigation {
  private readonly router = inject(Router);
  private readonly document = inject(DOCUMENT);
  private readonly injector = inject(Injector);
  /** The path of the last navigation, without its query and fragment; `null` before the first. */
  private path: string | null = null;

  private readonly ends = toSignal(
    this.router.events.pipe(filter((e): e is NavigationEnd => e instanceof NavigationEnd)),
  );

  /** Starts watching; call once from the root component. */
  start(): void {
    effect(
      () => {
        const end = this.ends();
        if (!end) return;
        const previous = this.path;
        this.path = this.pathOf(end.urlAfterRedirects);
        if (previous === null || previous === this.path) return;
        afterNextRender(() => this.focusHeading(), { injector: this.injector });
      },
      { injector: this.injector },
    );
  }

  private pathOf(url: string): string {
    const tree = this.router.parseUrl(url);
    tree.queryParams = {};
    tree.fragment = null;
    return this.router.serializeUrl(tree);
  }

  private focusHeading(): void {
    const main = this.document.getElementById('main');
    const target = main?.querySelector<HTMLElement>('h1') ?? main;
    target?.focus();
  }
}
