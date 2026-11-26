import { CanMatchFn, Routes, UrlMatcher, UrlSegment } from '@angular/router';
import { leadsWithDocumentation } from '../repositories/content/content-shape';
import { VERSIONS_SEGMENTS, splitVersion } from './package-links';

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

/**
 * `/packages/{format}/{name…}/-/versions`: the table of a package's versions where the package's own address is its newest version's
 * page (R-UI-218, Q-UI-1242). The name is every segment between the format and `-/versions`.
 */
export const versionsMatcher: UrlMatcher = (segments) => {
  const tail = segments.slice(-VERSIONS_SEGMENTS.length).map((s) => s.path);
  const [format, ...rest] = segments.slice(0, -VERSIONS_SEGMENTS.length);
  if (!format || rest.length === 0 || tail.join('/') !== VERSIONS_SEGMENTS.join('/')) return null;
  if (splitVersion(rest.at(-1)!.path).version !== null) return null;
  return {
    consumed: segments,
    posParams: { format, name: new UrlSegment(rest.map((s) => s.path).join('/'), {}) },
  };
};

/**
 * `…/packages/-/namespaces/{format}/{name…}`: the page of a name in the project (R-UI-220, Q-UI-1291). No format is named `-`, so a
 * package's address never starts with it.
 */
export const namespaceMatcher: UrlMatcher = (segments) => {
  const [dash, word, format, ...rest] = segments;
  if (dash?.path !== '-' || word?.path !== 'namespaces' || !format || rest.length === 0) return null;
  return {
    consumed: segments,
    posParams: { format, name: new UrlSegment(rest.map((s) => s.path).join('/'), {}) },
  };
};

/** `/namespaces/{format}/{name…}`: the page of a name across every project (R-UI-220, Q-UI-1291). */
export const allNamespacesMatcher: UrlMatcher = (segments) => {
  const [format, ...rest] = segments;
  if (!format || rest.length === 0) return null;
  return { consumed: segments, posParams: { format, name: new UrlSegment(rest.map((s) => s.path).join('/'), {}) } };
};

/** The routes of `/namespaces`: the page of a name across every project (R-UI-220). */
export const NAMESPACE_ROUTES: Routes = [
  {
    matcher: allNamespacesMatcher,
    title: $localize`:@@route.namespace.title:Namespace`,
    data: { crumbParam: 'name' },
    loadComponent: () => import('./namespace-page').then((m) => m.NamespacePage),
  },
];

/** The address names a format whose packages lead with their documentation (R-UI-218): its first segment is the format. */
const leadsWithDocs: CanMatchFn = (_route, segments) => leadsWithDocumentation(segments[0]?.path);

/**
 * R-UI-006 (list), R-UI-007 and R-UI-110 (a package: its versions), R-UI-111 (a version), R-UI-218 (a package that leads with its
 * documentation: its own address is its newest version's page, its versions' table is `…/-/versions`).
 */
export const PACKAGES_ROUTES: Routes = [
  {
    path: '',
    title: $localize`:@@route.packages.title:Packages`,
    loadComponent: () => import('./package-list').then((m) => m.PackageList),
  },
  {
    matcher: namespaceMatcher,
    title: $localize`:@@route.namespace.title:Namespace`,
    data: { crumbParam: 'name' },
    loadComponent: () => import('./namespace-page').then((m) => m.NamespacePage),
  },
  {
    matcher: versionsMatcher,
    canMatch: [leadsWithDocs],
    title: $localize`:@@route.packages.versions.title:Versions`,
    data: { crumbParam: 'name', crumbTail: VERSIONS_SEGMENTS.length },
    loadComponent: () => import('./package-detail').then((m) => m.PackageDetail),
  },
  {
    matcher: versionMatcher,
    title: $localize`:@@route.packages.version.title:Package version`,
    data: { crumbParam: 'name', crumbVersion: 'version' },
    loadComponent: () => import('./package-version').then((m) => m.PackageVersionPage),
  },
  {
    matcher: packageMatcher,
    canMatch: [leadsWithDocs],
    title: $localize`:@@route.packages.detail.title:Package`,
    data: { crumbParam: 'name' },
    loadComponent: () => import('./package-version').then((m) => m.PackageVersionPage),
  },
  {
    matcher: packageMatcher,
    title: $localize`:@@route.packages.detail.title:Package`,
    data: { crumbParam: 'name' },
    loadComponent: () => import('./package-detail').then((m) => m.PackageDetail),
  },
];
