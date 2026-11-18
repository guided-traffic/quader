import { ActivatedRouteSnapshot, Routes, UrlMatcher, UrlSegment } from '@angular/router';
import { unsavedChangesGuard } from '../../shared/unsaved-guard';
import { newRepositoryTitle } from './repository-create';

/**
 * `…/repositories/{key}/files/{path…}` (R-UI-010, Q-UI-510): the folder or the file at `path` inside the repository, its slashes
 * kept. The segment `files` alone is the root of the repository's files. The path reaches the page as the input `filePath`.
 */
export const filesMatcher: UrlMatcher = (segments) => {
  const [first, ...rest] = segments;
  if (first?.path !== 'files') return null;
  return { consumed: segments, posParams: { filePath: new UrlSegment(rest.map((s) => s.path).join('/'), {}) } };
};

/** The page of a folder or a file is named after it, the root of the files after the word. */
export function filesTitle(route: ActivatedRouteSnapshot): string {
  return route.url.length > 1 ? (route.url.at(-1)?.path ?? '') : $localize`:@@route.repositories.files.title:Files`;
}

const loadContent = (): Promise<typeof import('./content/repository-content').RepositoryContent> =>
  import('./content/repository-content').then((m) => m.RepositoryContent);

/**
 * The format chooser and the creation form of a format below `new` (R-UI-080): the trail reads "New repository" and then the
 * form's own title, "New OCI (Docker) repository".
 */
const NEW_REPOSITORY_ROUTES: Routes = [
  {
    path: '',
    loadComponent: () => import('./repository-format-chooser').then((m) => m.RepositoryFormatChooser),
  },
  {
    path: ':format',
    title: newRepositoryTitle,
    loadComponent: () => import('./repository-create').then((m) => m.RepositoryCreate),
    canDeactivate: [unsavedChangesGuard],
  },
];

/**
 * R-UI-020 (list), R-UI-080 (a new repository). The admin area's list of every repository, each row naming its project, the format
 * chooser and the creation form of a format, with a project to choose. A repository's own screen lives in its project
 * (`PROJECT_REPOSITORIES_ROUTES`, R-REPO-002, Q-UI-129).
 */
export const REPOSITORIES_ROUTES: Routes = [
  {
    path: '',
    title: $localize`:@@route.repositories.title:Repositories`,
    loadComponent: () => import('./repository-list').then((m) => m.RepositoryList),
  },
  {
    path: 'new',
    title: $localize`:@@route.repositories.new.title:New repository`,
    children: NEW_REPOSITORY_ROUTES,
  },
];

/**
 * The repositories of one project, mounted at `/projects/:project/repositories` (Q-UI-129): the list, the format chooser and the
 * creation form with the project given, a repository's content (R-UI-052), its folders and files (R-UI-010, ADR-203) and its
 * settings (R-UI-087). `new` precedes `:key`.
 */
export const PROJECT_REPOSITORIES_ROUTES: Routes = [
  {
    path: '',
    title: $localize`:@@route.repositories.title:Repositories`,
    loadComponent: () => import('./repository-list').then((m) => m.RepositoryList),
  },
  {
    path: 'new',
    title: $localize`:@@route.repositories.new.title:New repository`,
    children: NEW_REPOSITORY_ROUTES,
  },
  {
    path: ':key',
    title: $localize`:@@route.repositories.detail.title:Repository`,
    children: [
      {
        path: '',
        pathMatch: 'full',
        // The page is named after the repository, as its heading is; the breadcrumb step comes from the parent route.
        title: (route) => route.paramMap.get('key') ?? '',
        loadComponent: loadContent,
      },
      {
        // A folder or a file: the same page. The breadcrumb ends at the repository; the page's path shows the folders (Q-UI-510).
        matcher: filesMatcher,
        title: filesTitle,
        data: { belowTrail: true },
        loadComponent: loadContent,
      },
      {
        // Set up client (R-UI-120): the snippets and the guide of this repository; the client is a query parameter.
        path: 'setup',
        title: $localize`:@@route.setup.title:Set up client`,
        loadComponent: () => import('../setup/client-setup').then((m) => m.ClientSetupPage),
      },
      {
        path: 'settings',
        title: $localize`:@@route.repositories.settings.title:Settings`,
        loadComponent: () => import('./repository-detail').then((m) => m.RepositoryDetail),
        canDeactivate: [unsavedChangesGuard],
      },
    ],
  },
];
