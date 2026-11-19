import { Routes } from '@angular/router';

/** R-UI-029: the profile, its personal tokens, its sessions; R-UI-185: its usage and limits. */
export const PROFILE_ROUTES: Routes = [
  {
    path: '',
    title: $localize`:@@route.profile.title:Profile`,
    loadComponent: () => import('./profile').then((m) => m.Profile),
  },
  {
    path: 'tokens',
    title: $localize`:@@route.profile.tokens.title:Personal tokens`,
    loadComponent: () => import('./profile-tokens').then((m) => m.ProfileTokens),
  },
  {
    path: 'usage',
    title: $localize`:@@profile.usage.heading:Usage`,
    loadComponent: () => import('../usage/profile-usage').then((m) => m.ProfileUsage),
  },
  {
    path: 'sessions',
    title: $localize`:@@route.profile.sessions.title:Sessions`,
    loadComponent: () => import('./profile-sessions').then((m) => m.ProfileSessions),
  },
];
