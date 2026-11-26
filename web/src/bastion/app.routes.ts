import { inject } from '@angular/core';
import { CanActivateFn, RedirectCommand, Router, Routes } from '@angular/router';
import { LegalPageName } from '../app/api/legal-api';
import { LegalStore } from '../app/core/legal-store';
import { leadsWithDocs, packageMatcher, versionMatcher, versionsMatcher } from '../app/features/packages/package-matchers';

/**
 * A legal page exists only while some language has a text for it (R-UI-225): otherwise its address is not found, at the address that
 * was asked for, as at the main installation (`legalPageGuard`, which also asks the session; the Bastion has none).
 */
function legalPage(name: LegalPageName): CanActivateFn {
  return async () => {
    const legal = inject(LegalStore);
    const router = inject(Router);
    await legal.ready();
    if (legal.page(name) !== null) return true;
    return new RedirectCommand(router.parseUrl('/not-found'), { skipLocationChange: true });
  };
}

/**
 * The routes of the Bastion's application (R-UI-227, W-35's B.3): the start, a project's packages, a package, a version, the versions'
 * table, the imprint and the privacy notice, and not found; nothing else. The addresses are the main installation's
 * (`/projects/<project>/packages/<format>/<name>`), so a link copied at one fits the other's form.
 */
export const BASTION_ROUTES: Routes = [
  {
    path: '',
    pathMatch: 'full',
    title: $localize`:@@bastion.start.title:Projects`,
    loadComponent: () => import('./pages/start-page').then((m) => m.StartPage),
  },
  { path: 'projects/:project', pathMatch: 'full', redirectTo: 'projects/:project/packages' },
  {
    path: 'projects/:project/packages',
    children: [
      {
        path: '',
        title: $localize`:@@route.packages.title:Packages`,
        loadComponent: () => import('./pages/package-list-page').then((m) => m.PackageListPage),
      },
      {
        matcher: versionsMatcher,
        canMatch: [leadsWithDocs],
        title: $localize`:@@route.packages.versions.title:Versions`,
        loadComponent: () => import('./pages/package-pages').then((m) => m.PackagePage),
      },
      {
        matcher: versionMatcher,
        title: $localize`:@@route.packages.version.title:Package version`,
        loadComponent: () => import('./pages/package-pages').then((m) => m.VersionPage),
      },
      {
        matcher: packageMatcher,
        canMatch: [leadsWithDocs],
        title: $localize`:@@route.packages.detail.title:Package`,
        loadComponent: () => import('./pages/package-pages').then((m) => m.VersionPage),
      },
      {
        matcher: packageMatcher,
        title: $localize`:@@route.packages.detail.title:Package`,
        loadComponent: () => import('./pages/package-pages').then((m) => m.PackagePage),
      },
    ],
  },
  {
    path: 'legal/imprint',
    title: $localize`:@@legal.imprint:Imprint`,
    canActivate: [legalPage('imprint')],
    data: { legal: 'imprint' },
    loadComponent: () => import('../app/features/legal/legal-page').then((m) => m.LegalPage),
  },
  {
    path: 'legal/privacy',
    title: $localize`:@@legal.privacy:Privacy notice`,
    canActivate: [legalPage('privacy')],
    data: { legal: 'privacy' },
    loadComponent: () => import('../app/features/legal/legal-page').then((m) => m.LegalPage),
  },
  {
    path: '**',
    title: $localize`:@@route.notfound.title:Page not found`,
    loadComponent: () => import('../app/features/not-found/not-found').then((m) => m.NotFound),
  },
];
