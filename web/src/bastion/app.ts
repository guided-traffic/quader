import { CdkMenu, CdkMenuItemRadio, CdkMenuTrigger } from '@angular/cdk/menu';
import { DOCUMENT } from '@angular/common';
import { TimePipe } from '../app/shared/time-pipe';
import { Component, LOCALE_ID, computed, inject, signal } from '@angular/core';
import { toSignal } from '@angular/core/rxjs-interop';
import { NavigationEnd, Router, RouterLink, RouterLinkActive, RouterOutlet } from '@angular/router';
import { filter, map } from 'rxjs';
import { LegalStore } from '../app/core/legal-store';
import { LIVE_IDLE_MS, LIVE_SLOW_MS, live } from '../app/core/live-refresh';
import { BrowserStorage } from '../app/core/storage';
import { BASTION_MARK } from '../app/shared/brand/mark-geometry';
import { QuaderMark } from '../app/shared/brand/quader-mark';
import { ErrorNotice } from '../app/shared/error-notice';
import { Notices } from '../app/shared/notices';
import { Loading } from '../app/shared/ui/loading';
import { TimeAgo } from '../app/shared/ui/time-ago';
import { BastionStateStore } from './data/state';

type Theme = 'light' | 'dark';

/** The key of the visitor's theme in the browser (W-35's B.7: no cookie, a choice kept in the browser alone). */
const THEME_KEY = 'quader.theme';

/**
 * The shell of the Bastion's application (R-UI-227): the Bastion's mark and name (Q-UI-1617), the theme and the language in the top
 * bar; the band of an old state above the page; the page or, while the Bastion has no catalogue to show, its sentence; and the footer
 * with the imprint, the privacy notice and "As of". It names nothing of the main installation (W-35's B.13). It reads the state at the
 * idle cadence of a live view (R-UI-213), every 30 s while the Bastion is not ready, and never asks for a reload.
 */
@Component({
  selector: 'quader-root',
  imports: [
    CdkMenu,
    CdkMenuItemRadio,
    CdkMenuTrigger,
    TimePipe,
    ErrorNotice,
    Loading,
    Notices,
    QuaderMark,
    RouterLink,
    RouterLinkActive,
    RouterOutlet,
    TimeAgo,
  ],
  template: `
    <a class="skip-link" href="#main" (click)="skipToMain($event)" i18n="@@shell.skip">Skip to main content</a>
    <header class="topbar">
      <div class="topbar-start">
        <a class="product" routerLink="/">
          <quader-mark class="product-mark" [mark]="bastionMark" />
          <span class="product-name" translate="no">Quader <span class="product-qualifier">Bastion</span></span>
        </a>
      </div>
      <span></span>
      <div class="topbar-actions">
        <!-- The other language: the same address in the other build (Q-UI-5), a link a crawler follows too. -->
        <a class="btn btn-tertiary language-link" [href]="otherLanguage()" [attr.hreflang]="otherLocale" [attr.lang]="otherLocale">
          <span class="material-icons" aria-hidden="true">translate</span>
          <span class="topbar-label">{{ otherLocaleName }}</span>
        </a>
        <button
          type="button"
          class="btn btn-icon topbar-icon"
          [cdkMenuTriggerFor]="themeMenu"
          i18n-aria-label="@@shell.theme"
          aria-label="Theme"
          i18n-title="@@shell.theme"
          title="Theme"
        >
          <span class="material-icons" aria-hidden="true">contrast</span>
        </button>
      </div>
    </header>

    <ng-template #themeMenu>
      <div class="menu theme-menu" cdkMenu>
        @for (t of themes; track t.value) {
          <button
            type="button"
            class="menu-item"
            cdkMenuItemRadio
            [cdkMenuItemChecked]="theme() === t.value"
            (cdkMenuItemTriggered)="setTheme(t.value)"
          >
            <span class="material-icons" aria-hidden="true">{{ theme() === t.value ? 'check' : t.icon }}</span>
            <span>{{ t.label }}</span>
          </button>
        }
      </div>
    </ng-template>

    <div class="frame bare-frame">
      <div class="panel">
        @if (store.state()?.stale) {
          <!-- W-35's B.2b: older than stale_after, the band says how old; it names no party. -->
          <section class="stale-band" role="status" data-nosnippet>
            <span class="material-icons icon" aria-hidden="true">schedule</span>
            <p i18n="@@bastion.stale">This information is from {{ confirmedAt() | time }}. Newer information is not available right now.</p>
          </section>
        }
        <main id="main" tabindex="-1">
          <div class="content">
            @if (store.notReady()) {
              <section class="not-ready" aria-labelledby="not-ready-heading">
                <h1 id="not-ready-heading" i18n="@@bastion.not-ready.heading">Not available right now</h1>
                <p i18n="@@bastion.not-ready.text">This page is not available right now. Try again in a few minutes.</p>
                <p class="muted" i18n="@@bastion.not-ready.again">The page tries again by itself.</p>
              </section>
            } @else if (store.off() && store.failure(); as failure) {
              <quader-error-notice [error]="failure" />
            } @else if (!store.settled()) {
              <quader-loading size="page" i18n="@@bastion.loading">Loading.</quader-loading>
            } @else {
              <router-outlet />
            }
            <footer class="bastion-footer">
              <ul>
                @if (legal.imprint()) {
                  <li>
                    <a routerLink="/legal/imprint" routerLinkActive="active" ariaCurrentWhenActive="page" i18n="@@legal.imprint">Imprint</a>
                  </li>
                }
                @if (legal.privacy()) {
                  <li>
                    <a routerLink="/legal/privacy" routerLinkActive="active" ariaCurrentWhenActive="page" i18n="@@legal.privacy"
                      >Privacy notice</a
                    >
                  </li>
                }
                @if (confirmedAt(); as at) {
                  <li class="as-of" data-nosnippet>
                    <ng-container i18n="@@bastion.as-of">As of</ng-container>&ngsp;<quader-time-ago [at]="at" absolute="title" />
                  </li>
                }
              </ul>
            </footer>
          </div>
        </main>
      </div>
    </div>
    <quader-notices />
  `,
  styles: `
    :host {
      display: flex;
      flex-direction: column;
      block-size: 100dvh;
    }

    /* The lockup's words: "Quader" in the bar's Bold, "Bastion" after it in Regular (Q-UI-1617). */
    .product-qualifier {
      font-weight: 400;
    }

    /* The banner's form and colours of a warning (shared/banner.ts): the icon a line high on the first line of a text that wraps. */
    .stale-band {
      display: flex;
      align-items: flex-start;
      gap: var(--quader-space-2);
      padding: var(--quader-space-2) var(--quader-space-4);
      background: var(--quader-badge-warn-bg);
      color: var(--quader-badge-warn-fg);

      p {
        margin: 0;
      }

      .icon {
        flex: none;
        font-size: 1.25em;
        line-height: var(--quader-line);
      }
    }

    /* The footer stands at the end of a short page (R-UI-226's rule): the column is at least the panel high. */
    .content {
      box-sizing: border-box;
      min-block-size: 100%;
    }

    .not-ready h1 {
      margin: 0 0 var(--quader-space-4);
      font: var(--quader-type-title-large);
    }

    .muted {
      color: var(--quader-text-muted);
    }

    .bastion-footer {
      margin-block-start: auto;
      padding-block-start: var(--quader-space-8);
    }

    .bastion-footer ul {
      display: flex;
      flex-wrap: wrap;
      justify-content: center;
      gap: var(--quader-space-1) var(--quader-space-5);
      margin: 0;
      padding: 0;
      list-style: none;
      font-size: var(--quader-text-muted-size);
      line-height: 1rem;
      color: var(--quader-text-muted);
    }

    .bastion-footer a:not(.btn) {
      color: var(--quader-text-muted);
      text-decoration: none;

      &:hover {
        color: var(--quader-link);
        text-decoration: underline;
      }

      &.active {
        color: var(--quader-on-surface);
      }
    }
  `,
})
export class BastionApp {
  protected readonly bastionMark = BASTION_MARK;
  protected readonly store = inject(BastionStateStore);
  protected readonly legal = inject(LegalStore);
  private readonly storage = inject(BrowserStorage);
  private readonly document = inject(DOCUMENT);
  private readonly router = inject(Router);
  private readonly locale: 'en' | 'de' = inject(LOCALE_ID).startsWith('de') ? 'de' : 'en';

  protected readonly otherLocale = this.locale === 'de' ? 'en' : 'de';
  /** The other language in its own words, as a language switch names it. */
  protected readonly otherLocaleName = this.locale === 'de' ? 'English' : 'Deutsch';
  protected readonly themes: readonly { value: Theme | null; label: string; icon: string }[] = [
    { value: null, label: $localize`:@@shell.theme.system:Follow the system`, icon: 'brightness_auto' },
    { value: 'light', label: $localize`:@@shell.theme.light:Light`, icon: 'light_mode' },
    { value: 'dark', label: $localize`:@@shell.theme.dark:Dark`, icon: 'dark_mode' },
  ];
  protected readonly theme = signal<Theme | null>(null);
  protected readonly confirmedAt = computed(() => this.store.state()?.confirmedAt ?? null);

  private readonly url = toSignal(
    this.router.events.pipe(
      filter((e): e is NavigationEnd => e instanceof NavigationEnd),
      map((e) => e.urlAfterRedirects),
    ),
    { initialValue: this.router.url },
  );
  /** The same address in the other language's build: `/ui/<locale>/<route>`. */
  protected readonly otherLanguage = computed(() => `/ui/${this.otherLocale}${this.url() === '/' ? '/' : this.url()}`);

  constructor() {
    const stored = this.storage.get('local', THEME_KEY);
    this.applyTheme(stored === 'light' || stored === 'dark' ? stored : null);
    void this.store.refresh();
    live({ reload: () => this.store.refresh(), every: () => (this.store.notReady() ? LIVE_SLOW_MS : LIVE_IDLE_MS) });
  }

  protected setTheme(theme: Theme | null): void {
    this.applyTheme(theme);
    if (theme === null) this.storage.remove('local', THEME_KEY);
    else this.storage.set('local', THEME_KEY, theme);
  }

  private applyTheme(theme: Theme | null): void {
    this.theme.set(theme);
    const root = this.document.documentElement;
    if (theme === null) root.removeAttribute('data-theme');
    else root.setAttribute('data-theme', theme);
  }

  protected skipToMain(event: Event): void {
    event.preventDefault();
    this.document.getElementById('main')?.focus();
  }
}
