import { CanMatchFn, UrlMatcher, UrlSegment } from '@angular/router';
import { leadsWithDocumentation } from '../../views/repositories/content-shape';
import { VERSIONS_SEGMENTS, splitVersion } from './package-links';

/*
 * The addresses of a package's pages (R-UI-003, R-UI-110, R-UI-111, R-UI-218), shared by the main installation's routes
 * (packages.routes.ts) and the Bastion's application (web/src/bastion, R-UI-227), whose pages have the same addresses (W-35's B.3). A
 * module of matchers alone, so that an application that imports them loads no screen of the other.
 */

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

/** The address names a format whose packages lead with their documentation (R-UI-218): its first segment is the format. */
export const leadsWithDocs: CanMatchFn = (_route, segments) => leadsWithDocumentation(segments[0]?.path);
