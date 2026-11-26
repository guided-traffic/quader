import { ActivatedRouteSnapshot, UrlMatcher, UrlSegment } from '@angular/router';

/*
 * The addresses of a repository's folders and files (R-UI-010, Q-UI-510), shared by the main installation's routes
 * (repositories.routes.ts) and the Bastion's application (web/src/bastion, R-UI-227), whose pages of a public generic repository have
 * the same addresses (Q-UI-1455). A module of matchers alone, so that an application that imports them loads no screen of the other.
 */

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
