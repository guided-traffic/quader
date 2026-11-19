import { inject } from '@angular/core';
import { ActivatedRouteSnapshot, CanActivateFn, RedirectCommand, Router, Routes } from '@angular/router';
import { SessionStore } from '../../core/session-store';

/**
 * Who reaches an overview (R-UI-183, R-LIM-029): `usage:read` everywhere, an Owner in its project. Everybody else is sent to their
 * own usage on the profile (R-UI-185); the API decides in any case.
 */
export const usageGuard: CanActivateFn = async (route: ActivatedRouteSnapshot) => {
  const session = inject(SessionStore);
  const router = inject(Router);
  await session.load();
  const project = (route.paramMap.get('project') ?? route.parent?.paramMap.get('project')) || null;
  const scoped = route.data['scope'] === 'project';
  if (session.has('usage:read') || (scoped && session.isOwnerOf(project)) || session.state() === 'failed') return true;
  return new RedirectCommand(router.parseUrl('/profile/usage'));
};

/** The usage of the instance, `/admin/usage` (R-UI-183, R-UI-184). */
export const USAGE_ROUTES: Routes = [
  {
    path: '',
    title: $localize`:@@usage.heading:Usage`,
    data: { scope: 'instance' },
    canActivate: [usageGuard],
    loadComponent: () => import('./usage-overview').then((m) => m.UsageOverview),
  },
  {
    path: ':kind/:id',
    title: $localize`:@@usage.detail.title:Usage of a caller`,
    data: { scope: 'instance' },
    canActivate: [usageGuard],
    loadComponent: () => import('./usage-detail').then((m) => m.UsageDetailPage),
  },
];

/** The usage of one project, `/projects/{project}/usage` (R-UI-180, R-UI-183): for its Owners and `usage:read`. */
export const PROJECT_USAGE_ROUTES: Routes = [
  {
    path: '',
    title: $localize`:@@usage.heading:Usage`,
    data: { scope: 'project' },
    canActivate: [usageGuard],
    loadComponent: () => import('./usage-overview').then((m) => m.UsageOverview),
  },
  {
    path: ':kind/:id',
    title: $localize`:@@usage.detail.title:Usage of a caller`,
    data: { scope: 'project' },
    canActivate: [usageGuard],
    loadComponent: () => import('./usage-detail').then((m) => m.UsageDetailPage),
  },
];
