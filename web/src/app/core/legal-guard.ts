import { inject } from '@angular/core';
import { CanActivateFn, RedirectCommand, ResolveFn, Router } from '@angular/router';
import { LegalPageName } from '../api/legal-api';
import { LoginApi } from '../api/login-api';
import { LegalStore } from './legal-store';
import { SessionStore } from './session-store';

/**
 * A legal page exists only while some language has a text for it (R-UI-225): otherwise its address is **Page not found**, as an
 * address the application does not offer, at the address that was asked for. Needs no session: the routes sit outside `authGuard`.
 */
export function legalPageGuard(name: LegalPageName): CanActivateFn {
  return async () => {
    const legal = inject(LegalStore);
    const router = inject(Router);
    await legal.ready();
    if (legal.page(name) !== null) return true;
    return new RedirectCommand(router.parseUrl('/not-found'), { skipLocationChange: true });
  };
}

/**
 * Whether a legal page stands alone (`data.bare`, Q-UI-1421): inside the shell for a caller who runs the application (signed in, or
 * a visitor while there is public content, R-UI-057), alone like the sign-in screen for everyone else, who has no shell to return to.
 */
export const legalBare: ResolveFn<boolean> = async () => {
  const session = inject(SessionStore);
  const login = inject(LoginApi);
  await session.load();
  switch (session.state()) {
    case 'authenticated':
      return false;
    case 'anonymous':
      try {
        return !(await login.options()).publicContent;
      } catch {
        return true;
      }
    default:
      return true;
  }
};
