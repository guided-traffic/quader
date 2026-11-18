/**
 * A link taken from package metadata (npm `homepage`, `repository.url`, `bugs.url`): a package
 * author sets it, so it becomes a link only when its scheme is `http` or `https` (R-UI-007).
 * Anything else, `javascript:` and `data:` included, stays text. Angular's URL sanitizer is a
 * second layer, not the rule. Returns the absolute URL, or null.
 */
export function safeHttpUrl(value: unknown): string | null {
  if (typeof value !== 'string') return null;
  const trimmed = value.trim();
  if (trimmed === '') return null;
  try {
    const url = new URL(trimmed);
    return url.protocol === 'http:' || url.protocol === 'https:' ? url.href : null;
  } catch {
    return null;
  }
}

/** The rel of every link that leaves Quader for an address a package author chose. */
export const EXTERNAL_REL = 'nofollow noopener noreferrer';

/** The router commands of a package page in its project: the name keeps its slash as a separator (`@team-a/ui`). */
export function packageCommands(project: string, format: string, name: string): string[] {
  return ['/projects', project, 'packages', format, ...name.split('/')];
}

/**
 * The router commands of a version's page (R-UI-111, Q-UI-451): the package's address with `@` and the version appended to its last
 * segment, as the install command of npm writes it: `/projects/web/packages/npm/@team-a/ui@1.4.0`, `/…/packages/oci/portal/web@1.4.0`.
 * No format's names hold an `@` after their first character, and none of their versions holds one, so the last `@` of the last
 * segment is the separator ({@link splitVersion}).
 */
export function versionCommands(project: string, format: string, name: string, version: string): string[] {
  const segments = name.split('/');
  segments[segments.length - 1] += `@${version}`;
  return ['/projects', project, 'packages', format, ...segments];
}

/**
 * The name and the version of the last segment of a package address: `ui@1.4.0` is `ui` and `1.4.0`; an `@` at its start is a
 * scope (`@team-a`) and no separator, and a segment without one has no version.
 */
export function splitVersion(segment: string): { readonly name: string; readonly version: string | null } {
  const at = segment.lastIndexOf('@');
  return at > 0 && at < segment.length - 1 ? { name: segment.slice(0, at), version: segment.slice(at + 1) } : { name: segment, version: null };
}
