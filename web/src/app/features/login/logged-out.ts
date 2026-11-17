import { Component } from '@angular/core';
import { RouterLink } from '@angular/router';
import { QuaderMark } from '../../shared/brand/quader-mark';

/**
 * Landing page of the post-logout redirect `<baseURL>/ui/logged-out` (R-UI-004). It needs no
 * session, never asks the server who the caller is and never sends the browser to an identity
 * provider on its own: with auto-redirect that would sign the user straight back in. The route
 * name is registered at the identity provider and therefore a contract. Like the sign-in screen it
 * stands alone, without the shell (Q-UI-125).
 */
@Component({
  selector: 'quader-logged-out',
  imports: [RouterLink, QuaderMark],
  template: `
    <div class="bare-panel">
      <quader-mark class="bare-logo" [wordmark]="true" />
      <h1 tabindex="-1" i18n="@@loggedout.heading">Signed out</h1>
      <p class="text" i18n="@@loggedout.text">You have been signed out.</p>
      <a class="btn btn-confirm btn-block" routerLink="/login" i18n="@@loggedout.again"
        >Sign in again</a
      >
    </div>
  `,
  styles: `
    .text {
      text-align: center;
    }
  `,
})
export class LoggedOut {}
