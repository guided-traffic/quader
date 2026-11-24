import { DOCUMENT } from '@angular/common';
import { DestroyRef, Injectable, inject, signal } from '@angular/core';

/**
 * Whether the window is seen (`document.visibilityState`), as a signal: a poll that only serves the eye stops in a hidden tab and
 * reads again at once when it is seen (the top bar's Downloads, api-and-ui.md R-UI-210 and R-UI-211).
 */
@Injectable({ providedIn: 'root' })
export class PageVisibility {
  private readonly document = inject(DOCUMENT);
  private readonly _visible = signal(this.document.visibilityState !== 'hidden');
  readonly visible = this._visible.asReadonly();

  constructor() {
    const update = (): void => this._visible.set(this.document.visibilityState !== 'hidden');
    this.document.addEventListener('visibilitychange', update);
    inject(DestroyRef).onDestroy(() => this.document.removeEventListener('visibilitychange', update));
  }
}
