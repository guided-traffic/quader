import {
  Component,
  ElementRef,
  computed,
  effect,
  inject,
  signal,
  untracked,
  viewChild,
} from '@angular/core';
import { toSignal } from '@angular/core/rxjs-interop';
import { CdkTrapFocus } from '@angular/cdk/a11y';
import { OpenSubmenuOnClick } from './shared/ui/open-submenu-on-click';
import { CdkMenu, CdkMenuItem, CdkMenuItemRadio, CdkMenuTrigger } from '@angular/cdk/menu';
import { QuaderDialog } from './shared/ui/dialog';
import {
  ActivatedRouteSnapshot,
  NavigationEnd,
  NavigationError,
  Router,
  RouterLink,
  RouterOutlet,
} from '@angular/router';
import { filter, map } from 'rxjs';
import { ApiError } from './api/problem';
import { Breadcrumbs } from './core/breadcrumbs';
import { FocusOnNavigation } from './core/focus-on-navigation';
import { helpFor } from './core/help-topics';
import { Layout } from './core/layout';
import { NavGroup, NavSection, Navigation, YOUR_WORK_SECTION } from './core/navigation';
import { NavState } from './core/nav-state';
import { RecentRepositories } from './core/recent-repositories';
import { ProjectMark } from './shared/ui/project-mark';
import { PreferencesStore, UiTheme } from './core/preferences-store';
import { SessionStore } from './core/session-store';
import { Banner } from './shared/banner';
import { ErrorNotice } from './shared/error-notice';
import { Notices } from './shared/notices';
import { SessionDialog } from './shared/session-dialog';
import { QuaderMark } from './shared/brand/quader-mark';
import { QuaderAvatar } from './shared/ui/avatar';
import { AvatarStore } from './core/avatar-store';
import { ProjectSwitcher } from './features/projects/project-switcher';
import { SearchField } from './features/search/search-field';
import { UpstreamDownloadsButton } from './shared/upstream-downloads/upstream-downloads-button';
import { Loading } from './shared/ui/loading';

/** Whether a key typed at `target` is text: a text field, a text area, a select or an editable element. */
function isTextTarget(target: EventTarget | null): boolean {
  if (!(target instanceof HTMLElement)) return false;
  if (target.isContentEditable || target instanceof HTMLTextAreaElement || target instanceof HTMLSelectElement) return true;
  if (!(target instanceof HTMLInputElement)) return false;
  return !['checkbox', 'radio', 'button', 'submit', 'reset', 'range', 'color', 'file', 'image'].includes(target.type);
}

/** Whether a route on the path to the active screen carries `data: { bare: true }`. */
function isBare(route: ActivatedRouteSnapshot | null): boolean {
  for (let r = route; r; r = r.firstChild) {
    if (r.data['bare'] === true) return true;
  }
  return false;
}

/**
 * Root component (R-UI-002, Q-UI-68, Q-UI-127, Q-UI-129): skip link, the top bar (mark, the search of R-UI-095,
 * Admin button, help, the downloads of R-UI-210, theme, user menu), the sidebar of the current area (the start, a project with its
 * switcher, or the admin area behind the Admin button) with folding groups, collapsible where it is pinned and a drawer
 * below, and the content panel with the breadcrumb trail, banner area and router outlet. A screen whose route carries `data: { bare: true }`
 * (sign-in and signed out, Q-UI-125) stands alone: none of the shell renders around it.
 */
@Component({
  selector: 'quader-root',
  imports: [Loading, 
    RouterOutlet,
    RouterLink,
    CdkMenu,
    CdkMenuItem,
    CdkMenuItemRadio,
    CdkMenuTrigger,
    OpenSubmenuOnClick,
    CdkTrapFocus,
    QuaderMark,
    ProjectMark,
    QuaderAvatar,
    ProjectSwitcher,
    SearchField,
    UpstreamDownloadsButton,
    Banner,
    ErrorNotice,
    Notices,
  ],
  // Escape closes the drawer (the key bubbles up from the focus trapped in it); the pinned sidebar stays. `/`, Ctrl+K and ⌘K
  // focus the search (R-UI-095).
  host: { '(keydown.escape)': 'closeDrawer()', '(document:keydown)': 'shortcut($event)' },
  templateUrl: './app.html',
  styleUrl: './app.scss',
})
export class App {
  protected readonly layout = inject(Layout);
  protected readonly session = inject(SessionStore);
  protected readonly avatars = inject(AvatarStore);
  protected readonly navigation = inject(Navigation);
  protected readonly preferences = inject(PreferencesStore);
  protected readonly navState = inject(NavState);
  protected readonly breadcrumbs = inject(Breadcrumbs);
  protected readonly recent = inject(RecentRepositories);
  /** The block of the home side bar that ends with **Recently opened**, and the group's id in the stored layout (R-UI-167). */
  protected readonly yourWork = YOUR_WORK_SECTION;
  protected readonly recentGroup = 'recent';
  private readonly router = inject(Router);
  private readonly dialog = inject(QuaderDialog);
  private readonly main = viewChild.required<ElementRef<HTMLElement>>('main');
  private readonly search = viewChild(SearchField);

  /**
   * Whether the active screen stands alone (route data `bare`), read when a navigation ends or fails;
   * null until the first one has. A cancelled navigation is left out: a guard's redirect cancels one
   * and starts the next, and the screen is only known when that one ends.
   */
  private readonly bare = toSignal(
    this.router.events.pipe(
      filter((e) => e instanceof NavigationEnd || e instanceof NavigationError),
      map(() => isBare(this.router.routerState.snapshot.root)),
    ),
    { initialValue: null },
  );
  /**
   * The shell renders only around a screen that is not bare, and only after the first navigation ended:
   * opening `/login` directly and arriving there from a guarded route look the same, and the top bar
   * never flashes before the sign-in screen. The router outlet itself never moves, so a screen is
   * created once.
   */
  protected readonly shell = computed(() => this.bare() === false);
  /** No navigation has ended yet: the first screen loads (R-UI-217). */
  protected readonly firstLoad = computed(() => this.bare() === null);
  /**
   * The rest of the delay of every load (Q-UI-1230), counted from the page's start: the spinner of `index.html` that the app replaced
   * may already stand, and the one of the app then shows at once instead of blinking for another 300 ms.
   */
  protected readonly bootDelay = Math.max(0, 300 - Math.round(typeof performance === 'undefined' ? 0 : performance.now()));
  /** The side bar shows once the application knows whether the caller is signed in or anonymous. */
  protected readonly showNav = computed(
    () => this.session.state() === 'authenticated' || this.session.state() === 'anonymous',
  );
  /** The drawer below the pinned width is open. */
  private readonly drawerOpen = signal(false);
  /** The sidebar shows: pinned unless the viewer collapsed it, as a drawer while it is open. */
  protected readonly sidebarOpen = computed(() =>
    this.layout.pinned() ? !this.navState.collapsed() : this.drawerOpen(),
  );
  private readonly url = toSignal(
    this.router.events.pipe(
      filter((e): e is NavigationEnd => e instanceof NavigationEnd),
      map(() => this.router.url),
    ),
    { initialValue: this.router.url },
  );
  /** The manual page of the current screen, or the help overview (R-UI-050, Q-UI-90). */
  protected readonly help = computed(() => helpFor(this.url()));
  /** The area of the current route: its sidebar shows, and the Admin button is pressed in the admin area. */
  protected readonly area = computed(() => Navigation.areaOf(this.url()));
  protected readonly sidebarArea = computed(() => this.navigation.area(this.area()));
  /**
   * The entry of the side bar that is current: the one whose route is the longest that the page's path starts with (Q-UI-1005). So
   * **Security dashboard** (`/security`) is not current beside **Malware** (`/security/malware`), and a finding of the dashboard
   * (`/security/findings/…`) keeps it current.
   */
  protected readonly currentRoute = computed(() => currentRouteOf(this.url(), this.sidebarArea()?.sections ?? []));
  protected readonly themes: readonly { value: UiTheme | null; label: string; icon: string }[] = [
    { value: null, label: $localize`:@@shell.theme.system:Follow the system`, icon: 'brightness_auto' },
    { value: 'light', label: $localize`:@@shell.theme.light:Light`, icon: 'light_mode' },
    { value: 'dark', label: $localize`:@@shell.theme.dark:Dark`, icon: 'dark_mode' },
  ];
  /** The session's failed read, kept while it is read again, so its notice stays in place and counts (R-UI-216). */
  protected readonly loadFailure = computed(() => {
    const state = this.session.state();
    if (state === 'failed') return this.session.loadError() ?? new ApiError(0, null, null);
    return state === 'loading' ? this.session.loadError() : null;
  });

  constructor() {
    inject(FocusOnNavigation).start();
    // Leaving the drawer's width closes it, so it does not reappear open when the window narrows again.
    effect(() => {
      if (this.layout.pinned()) untracked(() => this.drawerOpen.set(false));
    });
    // Load the preferences (theme) once a session exists.
    effect(() => {
      if (this.session.authenticated()) untracked(() => void this.preferences.load());
    });
    // The uploaded avatar is not part of `whoami` (R-UI-051): read the user record once per signed-in principal.
    effect(() => {
      const principal = this.session.authenticated() ? this.session.whoami()?.principal.id : undefined;
      if (principal) {
        untracked(() => void this.avatars.refresh());
      } else {
        untracked(() => this.avatars.clear());
      }
    });
    // Recently opened shows only what the server confirmed (R-UI-167, Q-UI-662): asked while the home side bar is shown, for the
    // newest stored repositories the session has not asked yet; navigating asks nothing.
    effect(() => {
      const names = this.recent.unconfirmed();
      if (names.length === 0 || !this.shell() || !this.sidebarOpen() || this.area() !== 'home') return;
      untracked(() => void this.recent.confirm(names));
    });
    effect(() => {
      if (this.session.ended()) {
        untracked(() => this.dialog.open(SessionDialog, { data: { kind: 'ended' }, disableClose: true }));
      }
    });
    effect(() => {
      if (this.session.reauthRequired()) {
        untracked(() => {
          const ref = this.dialog.open(SessionDialog, { data: { kind: 'reauth' } });
          // `afterClosed` is an observable; the flag only has to be cleared, so use the promise form.
          void ref.closed.forEach(() => this.session.clearReauthRequired());
        });
      }
    });
  }

  /** Skip link target: moves focus to the main heading, or to the main region without one. */
  protected skipToMain(event: Event): void {
    event.preventDefault();
    const region = this.main().nativeElement;
    const heading = region.querySelector<HTMLElement>('h1');
    (heading ?? region).focus();
  }

  protected openSidebar(): void {
    if (this.layout.pinned()) this.navState.setCollapsed(false);
    else this.drawerOpen.set(true);
  }

  /** "Collapse sidebar": collapses the pinned sidebar (stored), closes the drawer. */
  protected closeSidebar(): void {
    if (this.layout.pinned()) this.navState.setCollapsed(true);
    else this.drawerOpen.set(false);
  }

  /** A link in the drawer, or Escape in it, closes it; the pinned sidebar stays. */
  protected closeDrawer(): void {
    if (!this.layout.pinned()) this.drawerOpen.set(false);
  }

  protected go(url: string): void {
    void this.router.navigateByUrl(url);
  }

  /** A folded group that holds the current page still shows it, by a mark on the group. */
  protected groupActive(group: NavGroup): boolean {
    const url = this.url();
    return group.items.some((i) => url === i.route || url.startsWith(i.route + '/') || url.startsWith(i.route + '?'));
  }

  /**
   * The search's keys (R-UI-095, Q-UI-401): Ctrl+K and ⌘K always; `/` while the field's preference allows it and the focus is not
   * where the key is text (a text field, a text area, a select or an editable element). The key is not typed into the field.
   */
  protected shortcut(event: KeyboardEvent): void {
    const search = this.search();
    if (!search || event.defaultPrevented || event.altKey) return;
    const chord = (event.ctrlKey || event.metaKey) && !event.shiftKey && event.key.toLowerCase() === 'k';
    const slash = event.key === '/' && !event.ctrlKey && !event.metaKey && search.slashOn() && !isTextTarget(event.target);
    if (!chord && !slash) return;
    event.preventDefault();
    search.focus();
  }

  protected setTheme(theme: UiTheme | null): void {
    void this.preferences.setTheme(theme);
  }

  protected setLocale(locale: 'en' | 'de'): void {
    void this.preferences.setLocale(locale);
  }

  protected signOut(): void {
    void this.session.signOut();
  }

  protected retrySession(): void {
    void this.session.reload();
  }
}

/** The route of the side bar's entry that is current for a URL: the longest route the URL's path is or lies below; null for none. */
export function currentRouteOf(url: string, sections: readonly NavSection[]): string | null {
  const path = url.split(/[?#]/)[0] ?? '';
  let best: string | null = null;
  for (const s of sections) {
    for (const e of s.entries) {
      for (const route of e.kind === 'item' ? [e.route] : e.items.map((i) => i.route)) {
        if ((path === route || path.startsWith(route + '/')) && route.length > (best?.length ?? -1)) best = route;
      }
    }
  }
  return best;
}
