import { Routes } from '@angular/router';

/** R-UI-187, R-UI-188: the admin area's well-known repositories of the project `upstream`. */
export const UPSTREAM_ROUTES: Routes = [
  {
    path: '',
    title: $localize`:@@wellknown.heading:Upstream registries`,
    loadComponent: () => import('./upstream-registries').then((m) => m.UpstreamRegistries),
  },
  {
    path: ':key',
    title: $localize`:@@wellknown.route.detail:Upstream registry`,
    loadComponent: () => import('./upstream-registry').then((m) => m.UpstreamRegistry),
  },
];
