import { inject } from '@angular/core';
import { CanActivateFn, RedirectCommand, Router, RouterStateSnapshot } from '@angular/router';
import { LoginApi } from '../api/login-api';
import { Navigation, areaItems } from './navigation';
import { ProjectContext } from './project-context';
import { SessionStore } from './session-store';

/**
 * The start page (Q-UI-129): a caller with exactly one project starts in it, everyone else on the list of projects.
 * It runs after `authGuard`, so the session is known.
 */
export const homeGuard: CanActivateFn = async () => {
  const session = inject(SessionStore);
  const router = inject(Router);
  const context = inject(ProjectContext);
  await session.load();
  const projects = await context.ready();
  const only = session.authenticated() && projects.length === 1 ? projects[0] : undefined;
  return router.createUrlTree(only ? ['/projects', only.key] : ['/projects']);
};

/**
 * `/admin` itself starts at the first screen the caller may use (Q-UI-1043): the dashboard for a caller who may read the
 * installation's facts (Q-OPS-175), else the first entry of the admin side bar the caller has, in the side bar's order; a caller
 * with none, who has no admin area, starts on the start page. It runs after `authGuard`, so the session is known; a redirect
 * function of the route would run before it.
 */
export const adminHomeGuard: CanActivateFn = async () => {
  const session = inject(SessionStore);
  const router = inject(Router);
  const navigation = inject(Navigation);
  await session.load();
  if (session.has('settings:read')) return router.createUrlTree(['/admin', 'dashboard']);
  const first = areaItems(navigation.area('admin'))[0];
  return router.parseUrl(first?.route ?? '/');
};

/**
 * A page of the main area that only the holder of `capability` may open (the security dashboard, R-UI-158, Q-UI-622). It runs
 * after `authGuard`, so the session is known. A caller without the capability gets **Page not found** at the address it asked
 * for, as for any address the app does not offer: the page would otherwise claim a scope the API does not give that caller.
 * A session that could not be read passes, and the screen shows the failure. The API decides what the page reads in any case.
 */
export function capabilityGuard(capability: string): CanActivateFn {
  return async () => {
    const session = inject(SessionStore);
    const router = inject(Router);
    await session.load();
    if (session.has(capability) || session.state() === 'failed') return true;
    return new RedirectCommand(router.parseUrl('/not-found'), { skipLocationChange: true });
  };
}

/**
 * Shapes navigation only; the API decides (R-UI-005). A signed-in session passes. A session that has ended (`whoami` answered
 * `session_expired`, which cleared its cookie) goes to `/login?returnTo=<route>&ended=1`, with or without public content: the
 * caller believes they are signed in and is told, instead of running on silently as a visitor (R-UI-004, Q-UI-710). Without a
 * session the guard asks `login-options`: with public content the application runs anonymously (R-UI-057), otherwise it goes
 * to `/login?returnTo=<route>`. If the session could not be determined at all (the server is down) the route is entered and
 * the shell shows the failure.
 */
export const authGuard: CanActivateFn = async (_route, state: RouterStateSnapshot) => {
  const session = inject(SessionStore);
  const router = inject(Router);
  const login = inject(LoginApi);

  await session.load();
  switch (session.state()) {
    case 'authenticated':
    case 'failed':
      return true;
    case 'ended':
      return router.createUrlTree(['/login'], { queryParams: { returnTo: state.url, ended: 1 } });
    default: {
      try {
        if ((await login.options()).publicContent) return true;
      } catch {
        // No login options: send the user to the sign-in screen, which reports the failure.
      }
      return router.createUrlTree(['/login'], { queryParams: { returnTo: state.url } });
    }
  }
};
