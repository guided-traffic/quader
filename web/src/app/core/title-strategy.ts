import { Injectable, InjectionToken, inject } from '@angular/core';
import { Title } from '@angular/platform-browser';
import { RouterStateSnapshot, TitleStrategy } from '@angular/router';

/**
 * Suffix of every page title (R-UI-003, WCAG 2.4.2): the product's name, not translated. "Quader" unless an application of its own
 * binds its name: the Bastion's application binds "Quader Bastion" (Q-UI-1617), as its server's head writes it
 * (internal/bastion/gui/page/texts.go `TitleSuffix`).
 */
export const TITLE_SUFFIX = new InjectionToken<string>('TITLE_SUFFIX', { providedIn: 'root', factory: () => 'Quader' });

/** "Packages – Quader": the route's `title` plus the product name. */
@Injectable({ providedIn: 'root' })
export class QuaderTitleStrategy extends TitleStrategy {
  private readonly title = inject(Title);
  private readonly suffix = inject(TITLE_SUFFIX);

  override updateTitle(snapshot: RouterStateSnapshot): void {
    const own = this.buildTitle(snapshot);
    this.title.setTitle(own ? `${own} – ${this.suffix}` : this.suffix);
  }
}
