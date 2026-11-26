import { Component, DestroyRef, inject } from '@angular/core';
import { RouterLink, RouterLinkActive } from '@angular/router';
import { BannerStore } from '../core/banner-store';
import { LegalStore } from '../core/legal-store';

/**
 * The line of the imprint and the privacy notice (R-UI-226): at the end of the content panel of every screen and at the end of every
 * screen that stands alone (sign-in, signed out), part of the page and never fixed, so the page keeps its one scrollbar (R-UI-215).
 * A link only for a page that has a text in some language, no line at all while neither has one, and nothing until the first answer,
 * so no line blinks in and out. It reads with the banner store, at its cadence: watching the banner is what starts the reads.
 */
@Component({
  selector: 'quader-legal-footer',
  imports: [RouterLink, RouterLinkActive],
  template: `
    @if (!legal.empty()) {
      <nav class="legal-footer" i18n-aria-label="@@legal.footer" aria-label="Legal information">
        <ul>
          @if (legal.imprint()) {
            <li>
              <a routerLink="/legal/imprint" routerLinkActive="active" ariaCurrentWhenActive="page" i18n="@@legal.imprint">Imprint</a>
            </li>
          }
          @if (legal.privacy()) {
            <li>
              <a routerLink="/legal/privacy" routerLinkActive="active" ariaCurrentWhenActive="page" i18n="@@legal.privacy">Privacy notice</a>
            </li>
          }
        </ul>
      </nav>
    }
  `,
  styles: `
    :host {
      display: block;
    }

    /* A quiet line below the screen's content: muted small text, the links centred. The space above it and its place at the end of
       the column are the shell's (_shell.scss). */
    .legal-footer ul {
      display: flex;
      flex-wrap: wrap;
      justify-content: center;
      gap: var(--quader-space-1) var(--quader-space-5);
      margin: 0;
      padding: 0;
      list-style: none;
      font-size: var(--quader-text-muted-size);
      line-height: 1rem;
    }

    /* Above the page's link rule of the content panel and of a bare screen (_shell.scss), which would draw them as links in running text. */
    :host .legal-footer a:not(.btn):not(.tab-link) {
      color: var(--quader-text-muted);
      text-decoration: none;

      &:hover {
        color: var(--quader-link);
        text-decoration: underline;
      }

      &:focus-visible {
        outline: var(--quader-focus-ring);
        outline-offset: 2px;
      }

      &.active {
        color: var(--quader-on-surface);
      }
    }
  `,
})
export class LegalFooter {
  protected readonly legal = inject(LegalStore);

  constructor() {
    inject(BannerStore).watch(inject(DestroyRef));
  }
}
