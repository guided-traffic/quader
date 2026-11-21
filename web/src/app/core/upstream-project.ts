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

/** The work of Quader a system repository of the project `upstream` serves (R-REPO-070, R-REPO-075). */
export type SystemWork = 'image-scanning' | 'malware-scan';

/**
 * The system repositories of the project `upstream` (R-REPO-066, R-REPO-070, R-REPO-075): the repositories Quader reads for its own
 * work, `scanner-trivy` (the image scanning's database) and `clamav` (ClamAV's definitions, the malware scan's signature databases),
 * with the work each serves. The product declares their keys (`wellknown.ScannerTrivyKey`, `wellknown.ClamavKey`), so the UI knows them
 * by key as it knows the project, also for a caller who cannot read the admin area's item and its `kind`.
 */
const SYSTEM_REPOSITORIES: Readonly<Record<string, SystemWork>> = { 'scanner-trivy': 'image-scanning', clamav: 'malware-scan' };

/**
 * The system repositories an administrator can open to clients (R-REPO-075, the declaration's flag `Clients`): `clamav`, whose switch
 * **ClamAV definition updates** serves the definitions to the company's ClamAV installations. No other declaration has the flag.
 */
const CLIENT_SYSTEM_REPOSITORIES: ReadonlySet<string> = new Set(['clamav']);

/** The key of the repository of ClamAV's definitions in the project `upstream` (R-REPO-075). */
export const CLAMAV_KEY = 'clamav';

/**
 * Whether `upstream/<key>` is a system repository whose clients an administrator can switch on (R-REPO-075, the development agent's
 * Q-UI-908): the one kind of system repository that may answer clients, and then only while its switch is on.
 */
export function mayServeClients(key: string | null | undefined): boolean {
  return !!key && CLIENT_SYSTEM_REPOSITORIES.has(key);
}

/**
 * Whether `<project>/<key>` is a system repository of the project `upstream`, which no client uses (R-UI-190): the one test of every
 * screen that treats one apart (its section of the project's page, R-UI-192; no **Set up client**, `setupCommands`; no address,
 * download or command and its mark among other repositories, R-UI-193). The repository list's items carry no mark of their own: the
 * API's `kind` is the admin area's item only (`UpstreamRepository`).
 */
export function isSystemRepository(project: string | null | undefined, key: string | null | undefined): boolean {
  return isUpstreamProject(project) && !!key && Object.hasOwn(SYSTEM_REPOSITORIES, key);
}

/**
 * Whether a qualified repository name (`upstream/scanner-trivy`) names a system repository (R-UI-193, Q-UI-898): the same test as
 * {@link isSystemRepository} for a screen that holds the name, not the project and the key apart (a file's item, a version's
 * repositories, a filter's options). Such a repository answers every client's request with 404 (R-REPO-070, R-REPO-075), so no
 * screen offers its address, a download through it or a command that uses it.
 */
export function isSystemRepositoryName(name: string | null | undefined): boolean {
  const key = name ? upstreamKeyOf(name) : null;
  return key !== null && isSystemRepository(UPSTREAM_PROJECT, key);
}

/** The work a system repository of the project `upstream` serves, by its key; `null` for any other key. */
export function systemWork(key: string | null | undefined): SystemWork | null {
  return (key && Object.hasOwn(SYSTEM_REPOSITORIES, key) && SYSTEM_REPOSITORIES[key]) || null;
}

/**
 * The mark of a system repository where it is listed among other repositories (R-UI-193, Q-UI-902): the admin area's list, the
 * search's suggestions and its results. One word, as the badge **Built-in** marks the project; the project's own page sets them
 * apart under the heading **System repositories** instead (R-UI-192).
 */
export function systemBadge(): string {
  return $localize`:@@repo.badge.system:System`;
}

/**
 * Why a system repository offers no download or command where one would stand (R-UI-193, Q-UI-899): the work that reads it, and
 * that clients cannot. `what` is the thing the sentence is about: a file, or a version of a package.
 */
export function systemClientSentence(key: string | null | undefined, what: 'file' | 'version'): string {
  if (systemWork(key) === 'malware-scan') {
    return what === 'file'
      ? $localize`:@@repo.system.no-download-clamav:Only Quader's malware scan reads this file while ClamAV definition updates is off; clients cannot download it.`
      : $localize`:@@repo.system.no-install-clamav:Only Quader's malware scan reads this version while ClamAV definition updates is off; clients cannot install it.`;
  }
  return what === 'file'
    ? $localize`:@@repo.system.no-download:Only Quader's image scanning reads this file; clients cannot download it.`
    : $localize`:@@repo.system.no-install:Only Quader's image scanning reads this version; clients cannot install it.`;
}

/**
 * The qualified names of the system repositories, `upstream/scanner-trivy` and `upstream/clamav`: the project's page asks
 * for them by name (`name` of the list, R-API-063), so its section holds them whatever page of the list they would fall on (R-UI-192).
 */
export function systemRepositoryNames(): string[] {
  return Object.keys(SYSTEM_REPOSITORIES).map(upstreamName);
}

/**
 * A project's description as the UI shows it (R-UI-192, Q-UI-895): the built-in project's is the product's fixed sentence
 * (R-REPO-066), stored in English and never changed, so the UI says it in its own localized words, as it draws the project's mark
 * itself (Q-UI-837); every other project's is the stored one.
 */
export function projectDescription(key: string | null | undefined, stored: string | null | undefined): string {
  return isUpstreamProject(key)
    ? $localize`:@@upstream.project.description:The public registries this installation offers to every project.`
    : (stored ?? '');
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
