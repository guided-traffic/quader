import { Routes } from '@angular/router';

/**
 * The navigation entry **Guides** (R-UI-124): the formats as tiles, the repositories of one format the reader may use, and the
 * guide of a class of a format without a repository, which is the page **Set up client** without its repository (R-UI-120).
 */
export const GUIDES_ROUTES: Routes = [
  {
    path: '',
    title: $localize`:@@route.guides.title:Guides`,
    loadComponent: () => import('./guides-home').then((m) => m.GuidesHome),
  },
  {
    path: ':format',
    // The trail names the format (Guides › npm); its step leads to the format's repositories.
    data: { crumbParam: 'format' },
    children: [
      {
        path: '',
        pathMatch: 'full',
        title: (route) => route.paramMap.get('format') ?? '',
        loadComponent: () => import('./guide-format').then((m) => m.GuideFormat),
      },
      {
        path: ':class',
        // A title the router resolves names the page in the trail (Guides › npm › Set up client), as under a repository; a plain
        // title would leave the step to the parameter, the class's key. The class is the page's badge.
        title: () => $localize`:@@route.setup.title:Set up client`,
        loadComponent: () => import('../setup/client-setup').then((m) => m.ClientSetupPage),
      },
    ],
  },
];
