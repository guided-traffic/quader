import { inject } from '@angular/core';
import { ActivatedRouteSnapshot, CanActivateFn, RedirectCommand, ResolveFn, Router, Routes } from '@angular/router';
import { LimitsApi, UsageSubjectKind } from '../../api/limits-api';
import { SessionStore } from '../../core/session-store';
import { subjectTitle } from '../limits/callers';

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

/**
 * The title of a caller's page, which the breadcrumb's last step shows (Q-UI-765): the caller's name as the page's heading names
 * it ("Priya Raman", a token's name, a CI project's path, an address), read from the caller's usage of the last hour; the
 * reference of the address when the read fails, so the page itself shows the error.
 */
export const callerTitle: ResolveFn<string> = async (route: ActivatedRouteSnapshot) => {
  const api = inject(LimitsApi);
  const kind = route.paramMap.get('kind') ?? '';
  const id = route.paramMap.get('id') ?? '';
  const project = route.pathFromRoot.map((r) => r.paramMap.get('project')).find((p) => !!p) ?? null;
  const scope = route.data['scope'] === 'project' ? { project } : {};
  try {
    return subjectTitle((await api.subjectUsage(scope, kind as UsageSubjectKind, id, 'hour')).subject);
  } catch {
    return id;
  }
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
    title: callerTitle,
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
    title: callerTitle,
    data: { scope: 'project' },
    canActivate: [usageGuard],
    loadComponent: () => import('./usage-detail').then((m) => m.UsageDetailPage),
  },
];
