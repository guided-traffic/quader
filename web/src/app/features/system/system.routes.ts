import { Routes } from '@angular/router';

/**
 * The page System status and its tabs (R-UI-164): the page's header and tab strip around the tab's route, Status (R-UI-038),
 * Jobs (R-UI-038) and Backups (R-UI-048). The page has no title of its own, so the breadcrumb ends with the tab's.
 */
export const SYSTEM_ROUTES: Routes = [
  {
    path: '',
    loadComponent: () => import('./system-page').then((m) => m.SystemPage),
    children: [
      {
        path: '',
        title: $localize`:@@route.system.title:System status`,
        loadComponent: () => import('./system-status').then((m) => m.SystemStatus),
      },
      {
        path: 'jobs',
        title: $localize`:@@route.system.jobs.title:Jobs`,
        loadComponent: () => import('./jobs-page').then((m) => m.JobsPage),
      },
      {
        path: 'backups',
        title: $localize`:@@route.system.backups.title:Backups`,
        loadComponent: () => import('./backups/backups-page').then((m) => m.BackupsPage),
      },
    ],
  },
];
