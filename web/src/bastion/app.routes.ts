import { inject } from '@angular/core';
import { ActivatedRouteSnapshot, CanActivateFn, RedirectCommand, ResolveFn, Router, Routes } from '@angular/router';
import { LegalPageName } from '../app/api/legal-api';
import { LegalStore } from '../app/core/legal-store';
import { leadsWithDocs, packageMatcher, versionMatcher, versionsMatcher } from '../app/features/packages/package-matchers';
import { filesMatcher, filesTitle } from '../app/features/repositories/files-matchers';
import { BastionProjects } from './data/projects';

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

/*
 * The titles of a project's list, a package, a version and the versions name what the page shows, as the head the Bastion writes for
 * a crawler does (R-UI-231, R-BAS-121): a crawler that renders the page reads the title the application sets, so a generic "Package"
 * would be every package's. The server's words (internal/bastion/gui/page/texts.go) are these.
 */
const packagesTitle: ResolveFn<string> = async (route: ActivatedRouteSnapshot) => {
  // The project by its display name, as the server's head names it (R-BAS-128); its key where the Bastion does not offer it.
  const project = await inject(BastionProjects).named(route.paramMap.get('project') ?? '');
  return $localize`:@@bastion.title.packages:Packages in ${project.displayName}:project:`;
};
const packageTitle: ResolveFn<string> = (route: ActivatedRouteSnapshot) => route.paramMap.get('name') ?? '';
const versionTitle: ResolveFn<string> = (route: ActivatedRouteSnapshot) =>
  `${route.paramMap.get('name') ?? ''} ${route.paramMap.get('version') ?? ''}`;
const versionsTitle: ResolveFn<string> = (route: ActivatedRouteSnapshot) =>
  $localize`:@@bastion.title.versions:Versions of ${route.paramMap.get('name') ?? ''}:name:`;

/**
 * The routes of the Bastion's application (R-UI-227, W-35's B.3): the start, a project's packages, a package, a version, the versions'
 * table, a public generic repository's folders and files (Q-UI-1455), the imprint and the privacy notice, and not found; nothing else.
 * The addresses are the main installation's (`/projects/<project>/packages/<format>/<name>`,
 * `/projects/<project>/repositories/<key>/files/<path>`), so a link copied at one fits the other's form.
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
        title: packagesTitle,
        loadComponent: () => import('./pages/package-list-page').then((m) => m.PackageListPage),
      },
      {
        matcher: versionsMatcher,
        canMatch: [leadsWithDocs],
        title: versionsTitle,
        loadComponent: () => import('./pages/package-pages').then((m) => m.PackagePage),
      },
      {
        matcher: versionMatcher,
        title: versionTitle,
        loadComponent: () => import('./pages/package-pages').then((m) => m.VersionPage),
      },
      {
        matcher: packageMatcher,
        canMatch: [leadsWithDocs],
        title: packageTitle,
        loadComponent: () => import('./pages/package-pages').then((m) => m.VersionPage),
      },
      {
        matcher: packageMatcher,
        title: packageTitle,
        loadComponent: () => import('./pages/package-pages').then((m) => m.PackagePage),
      },
    ],
  },
  {
    // A public generic repository's files (R-UI-229): the repository's address is the root of its files, as at the main installation.
    path: 'projects/:project/repositories/:key',
    children: [
      {
        path: '',
        pathMatch: 'full',
        title: (route) => route.paramMap.get('key') ?? '',
        loadComponent: () => import('./pages/files-page').then((m) => m.FilesPage),
      },
      {
        // Set up client of a public repository (bastion.md R-BAS-129), the target of Set up client in a panel of commands.
        path: 'setup',
        title: $localize`:@@route.setup.title:Set up client`,
        loadComponent: () => import('./pages/setup-page').then((m) => m.SetupPage),
      },
      { matcher: filesMatcher, title: filesTitle, loadComponent: () => import('./pages/files-page').then((m) => m.FilesPage) },
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
