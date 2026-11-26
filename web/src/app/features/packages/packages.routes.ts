import { Routes, UrlMatcher, UrlSegment } from '@angular/router';
import { VERSIONS_SEGMENTS } from './package-links';
import { leadsWithDocs, packageMatcher, versionMatcher, versionsMatcher } from './package-matchers';

export { packageMatcher, versionMatcher, versionsMatcher } from './package-matchers';

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
