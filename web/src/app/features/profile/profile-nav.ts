import { Component } from '@angular/core';
import { RouterLink, RouterLinkActive } from '@angular/router';

/** The four pages of the profile as the tab strip of links (UI-A-20, R-UI-185); the current page is marked with `aria-current`. */
@Component({
  selector: 'quader-profile-nav',
  imports: [RouterLink, RouterLinkActive],
  template: `
    <nav class="tab-nav profile-nav" i18n-aria-label="@@profile.nav" aria-label="Profile pages">
      <a class="tab-link" routerLink="/profile" routerLinkActive [routerLinkActiveOptions]="{ exact: true }" ariaCurrentWhenActive="page" i18n="@@profile.nav.profile">Profile</a>
      <a class="tab-link" routerLink="/profile/tokens" routerLinkActive ariaCurrentWhenActive="page" i18n="@@profile.nav.tokens">Personal tokens</a>
      <a class="tab-link" routerLink="/profile/sessions" routerLinkActive ariaCurrentWhenActive="page" i18n="@@profile.nav.sessions">Sessions</a>
      <a class="tab-link" routerLink="/profile/usage" routerLinkActive ariaCurrentWhenActive="page" i18n="@@profile.nav.usage">Usage</a>
    </nav>
  `,
  styles: `
    .profile-nav {
      margin-block-end: var(--quader-space-4);
    }
  `,
})
export class ProfileNav {}
