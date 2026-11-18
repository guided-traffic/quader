import { inject } from '@angular/core';
import { ResolveFn, Routes } from '@angular/router';
import { RepositoriesApi } from '../../api/repositories-api';

/**
 * A format's name as people read it (`OCI (Docker)`), from the format catalog, for the trail and the title of a format's guides
 * (Q-UI-553); the key when the catalog does not know it or cannot be read.
 */
export const formatDisplayName: ResolveFn<string> = async (route) => {
  const key = route.paramMap.get('format') ?? '';
  try {
    return (await inject(RepositoriesApi).formats()).find((f) => f.name === key)?.displayName ?? key;
  } catch {
    return key;
  }
};

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
    // The trail names the format as every other place does (Guides › OCI (Docker)), not by its key (Q-UI-553); its step leads to
    // the format's repositories. `crumbLabel` is the resolved name the step shows instead of the parameter.
    data: { crumbParam: 'format' },
    resolve: { crumbLabel: formatDisplayName },
    children: [
      {
        path: '',
        pathMatch: 'full',
        // The same name, resolved once by the parent: the trail's step of this page and the parent's are one.
        title: (route) => (route.parent?.data['crumbLabel'] as string | undefined) ?? route.paramMap.get('format') ?? '',
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
