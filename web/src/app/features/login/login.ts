import { DOCUMENT } from '@angular/common';
import { Component, computed, effect, inject, input, resource, signal, untracked } from '@angular/core';
import { toSignal } from '@angular/core/rxjs-interop';
import { ActivatedRoute, Router, RouterLink } from '@angular/router';
import { map } from 'rxjs';
import { LoginApi } from '../../api/login-api';
import { ApiError } from '../../api/problem';
import { BrowserNavigation, safeReturnTo } from '../../core/browser-navigation';
import { asFailure } from '../../core/http/failure';
import { SessionStore } from '../../core/session-store';
import { Banner } from '../../shared/banner';
import { QuaderMark } from '../../shared/brand/quader-mark';
import { ErrorNotice } from '../../shared/error-notice';
import { failureOf } from '../../shared/paged-resource';

/**
 * Sign-in (R-UI-004, Q-UI-125). A screen without the shell, laid out as one column: the wordmark, the
 * heading, the break-glass form when it is offered, then the providers, below "or sign in with" when
 * the form is there. Renders only from `GET /api/v1/auth/login-options`: each provider button leaves
 * the application for the provider's `startUrl` with `return_to` set to the UI route (R-AUTH-009: a
 * path under `/ui/`). `autoRedirect` navigates at once unless the URL carries `?auto_sign_in=false`,
 * which also shows the break-glass form; without it the form shows when `localLogin` is true. There is
 * no username and password form for regular users (INV-7). `?ended=1`, which `authGuard` sets for a session that has ended,
 * shows the fixed notice that it has; while there is public content it also waives `autoRedirect`, so the caller can choose to
 * continue without signing in (Q-UI-710 to Q-UI-712).
 */
@Component({
  selector: 'quader-login',
  imports: [RouterLink, Banner, ErrorNotice, QuaderMark],
  template: `
    <quader-banner />
    <div class="bare-panel">
      <quader-mark class="bare-logo" [wordmark]="true" />
      <h1 tabindex="-1" i18n="@@login.heading">Sign in</h1>

      @if (reauth() === '1') {
        <p role="status" class="reauth" i18n="@@login.reauth">
          The action you tried needs a recent sign-in. Sign in again, then repeat it. Your entries are kept.
        </p>
      } @else if (sessionEnded()) {
        <p role="status" class="ended" i18n="@@login.session-ended">
          Your session has ended. Sign in again to continue where you left off.
        </p>
      }

      @if (optionsFailure(); as failure) {
        <quader-error-notice [error]="failure" (retry)="optionsFailure.retry()" />
      } @else if (options.isLoading()) {
        <p role="status" class="state" i18n="@@login.loading">Loading the sign-in options.</p>
      } @else if (options.value(); as o) {
        @if (redirecting()) {
          <p role="status" class="state" i18n="@@login.redirecting">Redirecting to the identity provider.</p>
        } @else {
          @if (o.providers.length === 0 && !showLocal()) {
            <p class="state" i18n="@@login.no-providers">No sign-in method is configured.</p>
          }

          @if (showLocal()) {
            <form
              class="local"
              i18n-aria-label="@@login.local.heading"
              aria-label="Local administrator"
              (submit)="submitLocal($event)"
            >
              <div class="field">
                <label class="field-label" for="login-username" i18n="@@login.local.username">Username</label>
                <input
                  id="login-username"
                  class="text-input field-w-name"
                  name="username"
                  autocomplete="username"
                  [value]="username()"
                  (input)="username.set($any($event.target).value)"
                />
              </div>
              <div class="field">
                <label class="field-label" for="login-password" i18n="@@login.local.password">Password</label>
                <input
                  id="login-password"
                  class="text-input field-w-name"
                  type="password"
                  name="password"
                  autocomplete="current-password"
                  [value]="password()"
                  (input)="password.set($any($event.target).value)"
                />
              </div>
              @if (localError(); as failure) {
                <quader-error-notice class="local-error" [error]="failure" />
              }
              <button class="btn btn-confirm btn-block" type="submit" [disabled]="busy()" i18n="@@login.local.submit">
                Sign in
              </button>
            </form>
          }

          @if (o.providers.length > 0) {
            @if (showLocal()) {
              <p class="divider"><span i18n="@@login.or">or sign in with</span></p>
            }
            <ul class="providers">
              @for (p of o.providers; track p.id) {
                <li>
                  <!-- Below the form a provider is the second way in; alone it is the primary action. -->
                  <button
                    class="btn btn-block"
                    [class.btn-confirm]="!showLocal()"
                    [class.btn-default]="showLocal()"
                    type="button"
                    [disabled]="!p.available"
                    (click)="start(p.startUrl)"
                  >
                    @if (!showLocal()) {
                      <ng-container i18n="@@login.with">Sign in with</ng-container>
                    }
                    {{ p.displayName }}
                  </button>
                  @if (!p.available) {
                    <span class="unavailable" i18n="@@login.unavailable">Currently unavailable</span>
                  }
                </li>
              }
            </ul>
          }

          @if (o.publicContent && reauth() !== '1') {
            <p class="anonymous">
              <a [routerLink]="continuePath()" i18n="@@login.anonymous">Continue without signing in</a>
            </p>
          }
        }
      }
    </div>
  `,
  styles: `
    /* The banner stands apart from the wordmark under it; it renders nothing (only Angular's comments) while there is none. */
    quader-banner:not(:empty) {
      display: block;
      margin-block-end: var(--quader-space-6);
    }
    .local,
    .providers {
      display: flex;
      flex-direction: column;
      gap: var(--quader-space-4);
    }
    .field {
      display: flex;
      flex-direction: column;
      gap: var(--quader-space-2);
    }
    .providers {
      list-style: none;
      margin: 0;
      padding: 0;
      gap: var(--quader-space-3);
    }
    .providers li {
      display: flex;
      flex-direction: column;
      gap: var(--quader-space-1);
    }
    .unavailable,
    .state {
      color: var(--quader-text-muted);
      font-size: var(--quader-text-muted-size);
      text-align: center;
    }
    .local-error {
      display: block;
    }
    .divider {
      display: flex;
      align-items: center;
      gap: var(--quader-space-3);
      margin: var(--quader-space-6) 0 var(--quader-space-4);
      color: var(--quader-text-muted);
      font-size: var(--quader-text-muted-size);
    }
    .divider::before,
    .divider::after {
      content: '';
      flex: 1 1 auto;
      border-block-start: 1px solid var(--quader-border);
    }
    .anonymous {
      margin-block-start: var(--quader-space-6);
      text-align: center;
    }
  `,
})
export class Login {
  private readonly api = inject(LoginApi);
  private readonly navigation = inject(BrowserNavigation);
  private readonly session = inject(SessionStore);
  private readonly router = inject(Router);
  private readonly document = inject(DOCUMENT);

  /** Query parameters, bound by the router's component input binding. */
  readonly returnTo = input<string | undefined>();

  readonly reauth = input<string | undefined>();

  /** `1`: the guard found the session ended (Q-UI-710). Read once, then taken out of the address (Q-UI-711). */
  readonly ended = input<string | undefined>();

  /** The notice that the session has ended; it stays for this visit after the parameter has left the address. */
  protected readonly sessionEnded = signal(false);

  protected readonly options = resource({ loader: () => this.api.options() });
  /** The failure of the sign-in options, kept while its notice reads them again (R-UI-216). */
  protected readonly optionsFailure = failureOf(this.options);
  protected readonly redirecting = signal(false);
  protected readonly username = signal('');
  protected readonly password = signal('');
  protected readonly busy = signal(false);
  /** A failed sign-in, shown as every failure is (R-UI-216): its sentence, the facts of the answer, Copy report. */
  protected readonly localError = signal<ApiError | null>(null);

  /**
   * `?auto_sign_in=false` waives the automatic sign-in at the provider for this visit and shows the
   * break-glass form (Q-UI-125). The parameter keeps the snake_case name operators know from other
   * tools' sign-in pages, so it is read from the query, not bound as an input.
   */
  private readonly autoSignInOff = toSignal(
    inject(ActivatedRoute).queryParamMap.pipe(map((q) => q.get('auto_sign_in') === 'false')),
    { initialValue: false },
  );

  protected readonly showLocal = computed(
    () => this.autoSignInOff() || (this.options.value()?.localLogin ?? false),
  );

  constructor() {
    effect(() => {
      if (this.ended() !== '1') return;
      untracked(() => {
        this.sessionEnded.set(true);
        // A reload of the sign-in screen does not claim the end again; returnTo and the other parameters stay.
        void this.router.navigate([], { queryParams: { ended: null }, queryParamsHandling: 'merge', replaceUrl: true });
      });
    });
    effect(() => {
      const o = this.options.value();
      if (!o?.autoRedirect || this.autoSignInOff()) return;
      // With public content the caller of an ended session chooses: sign in, or continue as a visitor (Q-UI-712).
      if (o.publicContent && this.sessionEnded()) return;
      const provider = o.providers.find((p) => p.id === o.autoRedirect && p.available);
      if (provider) untracked(() => this.start(provider.startUrl));
    });
  }

  /** Where "continue without signing in" leads: the route the caller wanted, or the start page. */
  protected continuePath(): string {
    return safeReturnTo(this.returnTo());
  }

  /**
   * The provider's start URL with `return_to` as the route under the application's own base (R-AUTH-009): the path of the
   * resolved `document.baseURI`, so a relative `<base href>` counts as the browser resolves it; `/ui/en/` in a build per locale,
   * `/ui/de/` in the German one, `/ui/` on the development server, which serves no locale folder (UI-A-49). A base outside
   * `/ui/` (no `<base>` element) gives the application's base `/ui/`.
   */
  protected start(startUrl: string): void {
    const resolved = new URL(this.document.baseURI).pathname;
    const base = (resolved.startsWith('/ui/') ? resolved : '/ui/').replace(/\/+$/, '');
    const target = new URLSearchParams({
      return_to: `${base}${safeReturnTo(this.returnTo())}`,
    });
    if (this.reauth() === '1') target.set('purpose', 'reauth');
    this.redirecting.set(true);
    this.navigation.assign(`${startUrl}?${target.toString()}`);
  }

  protected async submitLocal(event: Event): Promise<void> {
    event.preventDefault();
    this.busy.set(true);
    this.localError.set(null);
    try {
      await this.api.localLogin(this.username(), this.password());
      this.password.set('');
      await this.session.reload();
      await this.router.navigateByUrl(safeReturnTo(this.returnTo()));
    } catch (e) {
      this.localError.set(asFailure(e));
    } finally {
      this.busy.set(false);
    }
  }
}
