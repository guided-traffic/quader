import { inject } from '@angular/core';
import { ActivatedRouteSnapshot, CanActivateFn, RedirectCommand, ResolveFn, Router, Routes } from '@angular/router';
import { LegalPageName } from '../app/api/legal-api';
import { LegalStore } from '../app/core/legal-store';
import { leadsWithDocs, packageMatcher, versionMatcher, versionsMatcher } from '../app/features/packages/package-matchers';
import { filesMatcher, filesTitle } from '../app/features/repositories/files-matchers';
import { BastionProjects } from './data/projects';
import { BastionRepositories } from './data/repositories';
import { BastionStateStore } from './data/state';

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

/** "Page not found" at the address that was asked for, as an address the application does not know answers. */
function notFound(router: Router): RedirectCommand {
  return new RedirectCommand(router.parseUrl('/not-found'), { skipLocationChange: true });
}

/**
 * A project's own address opens its default view (R-UI-273, W-40's D1): its repositories, as the main installation's project does, or,
 * while the Bastion hides its repositories, its packages (R-UI-274). The switch is the state's, which the guard waits for once.
 */
const projectDefault: CanActivateFn = async (route) => {
  const state = inject(BastionStateStore);
  const router = inject(Router);
  await state.ready();
  const view = state.repositoriesShown() ? 'repositories' : 'packages';
  return new RedirectCommand(router.createUrlTree(['/projects', route.paramMap.get('project') ?? '', view]), { replaceUrl: true });
};

/** A project's repositories exist while the Bastion shows them (R-UI-274): hidden, the address is not found. */
const repositoriesShown: CanActivateFn = async () => {
  const state = inject(BastionStateStore);
  const router = inject(Router);
  await state.ready();
  return state.repositoriesShown() ? true : notFound(router);
};

/**
 * A repository's page exists while the Bastion shows the repositories; hidden, only a generic repository's files keep their root, whose
 * folder is the repository's key as every download address of them has it (R-UI-274, W-40's D14). Every other repository address is
 * then not found.
 */
const repositoryShown: CanActivateFn = async (route) => {
  const state = inject(BastionStateStore);
  const router = inject(Router);
  const repositories = inject(BastionRepositories);
  await state.ready();
  if (state.repositoriesShown()) return true;
  const repository = await repositories.one(route.paramMap.get('project') ?? '', route.paramMap.get('key') ?? '').catch(() => null);
  return repository?.format === 'generic' ? true : notFound(router);
};

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
const repositoriesTitle: ResolveFn<string> = async (route: ActivatedRouteSnapshot) => {
  const project = await inject(BastionProjects).named(route.paramMap.get('project') ?? '');
  return $localize`:@@bastion.title.repositories:Repositories in ${project.displayName}:project:`;
};
const packageTitle: ResolveFn<string> = (route: ActivatedRouteSnapshot) => route.paramMap.get('name') ?? '';
const versionTitle: ResolveFn<string> = (route: ActivatedRouteSnapshot) =>
  `${route.paramMap.get('name') ?? ''} ${route.paramMap.get('version') ?? ''}`;
const versionsTitle: ResolveFn<string> = (route: ActivatedRouteSnapshot) =>
  $localize`:@@bastion.title.versions:Versions of ${route.paramMap.get('name') ?? ''}:name:`;

/**
 * The routes of the Bastion's application (R-UI-227, R-UI-273): the start, a project's two views (its repositories and its packages), a
 * repository by its format (its images or packages, a generic repository's folders and files, the ClamAV databases), its Set up client,
 * a package, a version, the versions' table, the imprint and the privacy notice, and not found; nothing else. The addresses are the main
 * installation's (`/projects/<project>/repositories/<key>`, `/projects/<project>/packages/<format>/<name>`), so a link copied at one fits
 * the other's form. While the repositories are hidden (R-UI-274) a project opens on its packages and its repositories' addresses are not
 * found, but for a generic repository's files.
 */
export const BASTION_ROUTES: Routes = [
  {
    path: '',
    pathMatch: 'full',
    title: $localize`:@@bastion.start.title:Projects`,
    loadComponent: () => import('./pages/start-page').then((m) => m.StartPage),
  },
  // The guard always redirects; the component is never shown.
  {
    path: 'projects/:project',
    pathMatch: 'full',
    canActivate: [projectDefault],
    loadComponent: () => import('../app/features/not-found/not-found').then((m) => m.NotFound),
  },
  {
    path: 'projects/:project/repositories',
    pathMatch: 'full',
    canActivate: [repositoriesShown],
    title: repositoriesTitle,
    loadComponent: () => import('./pages/project-repositories-page').then((m) => m.ProjectRepositoriesPage),
  },
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
    // A public repository's page by its format (R-UI-273): its images or packages, a generic repository's files (its address is the
    // root of its files, as at the main installation, R-UI-229), the ClamAV databases.
    path: 'projects/:project/repositories/:key',
    children: [
      {
        path: '',
        pathMatch: 'full',
        canActivate: [repositoryShown],
        title: (route) => route.paramMap.get('key') ?? '',
        loadComponent: () => import('./pages/repository-page').then((m) => m.RepositoryPage),
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
