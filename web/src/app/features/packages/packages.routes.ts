import { Routes, UrlMatcher, UrlSegment } from '@angular/router';
import { splitVersion } from './package-links';

/**
 * `/packages/{format}/{name…}` (R-UI-003): the first segment is the format, the rest is the package name with its slashes
 * (`@team-a/ui`). The last segment carries the version after an `@` on a version's page (`ui@1.4.0`, R-UI-111): `withVersion`
 * says which of the two pages the matcher matches. Inputs `format`, `name` and `version` reach the screen through component input
 * binding.
 */
function matcherFor(withVersion: boolean): UrlMatcher {
  return (segments) => {
    const [format, ...rest] = segments;
    const last = rest.at(-1);
    if (!format || !last) return null;
    const { name, version } = splitVersion(last.path);
    if ((version !== null) !== withVersion) return null;
    const names = [...rest.slice(0, -1).map((s) => s.path), name];
    return {
      consumed: segments,
      posParams: {
        format,
        name: new UrlSegment(names.join('/'), {}),
        ...(version !== null ? { version: new UrlSegment(version, {}) } : {}),
      },
    };
  };
}

export const packageMatcher: UrlMatcher = matcherFor(false);
export const versionMatcher: UrlMatcher = matcherFor(true);

/** R-UI-006 (list), R-UI-007 (a package: its versions), R-UI-111 (a version). */
export const PACKAGES_ROUTES: Routes = [
  {
    path: '',
    title: $localize`:@@route.packages.title:Packages`,
    loadComponent: () => import('./package-list').then((m) => m.PackageList),
  },
  {
    matcher: versionMatcher,
    title: $localize`:@@route.packages.version.title:Package version`,
    data: { crumbParam: 'name', crumbVersion: 'version' },
    loadComponent: () => import('./package-version').then((m) => m.PackageVersionPage),
  },
  {
    matcher: packageMatcher,
    title: $localize`:@@route.packages.detail.title:Package`,
    data: { crumbParam: 'name' },
    loadComponent: () => import('./package-detail').then((m) => m.PackageDetail),
  },
];
