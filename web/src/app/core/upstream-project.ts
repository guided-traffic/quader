/**
 * The built-in project `upstream` (R-REPO-066, R-UI-190): the project of the product's well-known repositories, the same key in every
 * installation. The UI recognises it by this key, here and nowhere else; the API carries no flag for it.
 */
export const UPSTREAM_PROJECT = 'upstream';

/** Whether a project key is the built-in project `upstream`. */
export function isUpstreamProject(key: string | null | undefined): boolean {
  return key === UPSTREAM_PROJECT;
}

/** Whether a qualified repository name (`<project>/<key>`) names a repository of the project `upstream`. */
export function isUpstreamName(name: string): boolean {
  return name.startsWith(`${UPSTREAM_PROJECT}/`);
}

/** The key of a well-known repository in its qualified name, `upstream/<key>`, or null for any other name. */
export function upstreamKeyOf(name: string): string | null {
  return isUpstreamName(name) ? name.slice(UPSTREAM_PROJECT.length + 1) : null;
}

/** The qualified name of a well-known repository, `upstream/<key>`. */
export function upstreamName(key: string): string {
  return `${UPSTREAM_PROJECT}/${key}`;
}

/**
 * The system repositories of the project `upstream` (R-REPO-066, R-REPO-070): the repositories Quader reads for its own work, today
 * `scanner-trivy`. The product declares their keys (`wellknown.ScannerTrivyKey`), so the UI knows them by key as it knows the project,
 * also for a caller who cannot read the admin area's item and its `kind`.
 */
const SYSTEM_REPOSITORIES: readonly string[] = ['scanner-trivy'];

/** Whether `<project>/<key>` is a system repository of the project `upstream`, which no client uses (R-UI-190). */
export function isSystemRepository(project: string | null | undefined, key: string | null | undefined): boolean {
  return isUpstreamProject(project) && !!key && SYSTEM_REPOSITORIES.includes(key);
}

/**
 * A list of projects with the project `upstream` first and every other entry in its order (the owner's Q-UI-837): every list of
 * projects the app builds keeps it on top whatever it is sorted by, and a filter that does not match it has already taken it out.
 * Lists ordered by time or by relevance (Recently opened, the switcher's recent projects, the search's results) do not use it.
 */
export function upstreamFirst<T>(items: readonly T[], keyOf: (item: T) => string): T[] {
  const first = items.filter((i) => isUpstreamProject(keyOf(i)));
  return first.length === 0 ? [...items] : [...first, ...items.filter((i) => !isUpstreamProject(keyOf(i)))];
}
