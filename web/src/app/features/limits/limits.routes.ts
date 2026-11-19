import { Routes } from '@angular/router';
/**
 * **Download limits** of the admin area, `/admin/limits` (R-UI-177): the frame with the mode and the tabs Rules, Profiles,
 * Projects and Blocks, and the profile editor as a page of its own (R-UI-178). The admin route guards the area with
 * `limits:read`.
 */
export const LIMITS_ROUTES: Routes = [
  {
    path: 'profiles/new',
    title: $localize`:@@limits.profile.new-title:New profile`,
    loadComponent: () => import('./profile-editor').then((m) => m.LimitProfileEditor),
  },
  {
    path: 'profiles/:key',
    title: $localize`:@@limits.profile.title:Download limit profile`,
    loadComponent: () => import('./profile-editor').then((m) => m.LimitProfileEditor),
  },
  {
    path: '',
    loadComponent: () => import('./limits-frame').then((m) => m.LimitsFrame),
    children: [
      {
        path: '',
        pathMatch: 'full',
        title: $localize`:@@limits.heading:Download limits`,
        loadComponent: () => import('./limits-rules-tab').then((m) => m.LimitsRulesTab),
      },
      {
        path: 'profiles',
        title: $localize`:@@limits.title.profiles:Profiles of the download limits`,
        loadComponent: () => import('./limits-profiles-tab').then((m) => m.LimitsProfilesTab),
      },
      {
        path: 'projects',
        title: $localize`:@@limits.title.projects:Projects of the download limits`,
        loadComponent: () => import('./limits-projects-tab').then((m) => m.LimitsProjectsTab),
      },
      {
        path: 'blocks',
        title: $localize`:@@limits.title.blocks:Blocks of the download limits`,
        loadComponent: () => import('./limits-blocks-tab').then((m) => m.LimitsBlocksTab),
      },
    ],
  },
];
