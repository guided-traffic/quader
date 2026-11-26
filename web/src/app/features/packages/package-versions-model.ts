import { PackageDeprecation, PackageVersionSummary } from '../../api/packages-api';
import { SignatureStatus } from '../../api/signatures-api';
import { VulnerabilityGrade } from '../../api/vulnerabilities-api';
import { SortDirection, TableSort } from '../../shared/ui/data-table';
import { gradeRank } from '../../shared/vulnerability/grade-words';

/*
 * The versions table of a package page (R-UI-110): one row per version (for an image one row per image, its tags as the leading
 * version and its aliases, Q-UI-1130), searched, sorted and paged in the browser over the complete list the page reads once (Q-UI-452).
 * Pure functions, so the rules are tested without a screen.
 */

/** One row of the table: the version, the facts the owner named and what the row's badges need. */
export interface VersionRow {
  readonly version: string;
  readonly prerelease: boolean;
  readonly publishedAt: string;
  /** Its size; null when it is not known (R-API-080). */
  readonly sizeBytes: number | null;
  /** The size counts only the parts the repositories hold: the platforms of an image index fetched so far (Q-OCI-221). */
  readonly sizePartial: boolean;
  /** When a client last fetched it (R-API-065, R-API-079); null when none did. */
  readonly lastDownloadedAt: string | null;
  /** The qualified names of the repositories that hold it and that the reader may read. */
  readonly repositories: readonly string[];
  /** The distribution tags that point at it (npm `latest`, `next`), in the order of the package's tags (Q-UI-453). */
  readonly tags: readonly string[];
  /** Its grade, the worst of its repositories (R-VULN-038); null when the reader gets no grades (Q-VULN-158). */
  readonly grade: VulnerabilityGrade | null;
  /** Its place in the format's version order, 0 the highest: the sort by version without the format's rules in the browser. */
  readonly order: number;
  /** Its state from the format's metadata (`include=state`: the ClamAV definitions, R-UI-194); null for a format without one. */
  readonly state: string | null;
  /** The badge of its signatures (R-SIG-035), the member of the row; null while the signatures are off. */
  readonly signatures: SignatureStatus | null;
  /**
   * For a row of a list by content (an image, Q-API-432): the other versions that name its content (the image's other tags), in the
   * list's order; null for a list of versions.
   */
  readonly aliases: readonly string[] | null;
  /** The answer names 100 aliases and the content has more. */
  readonly aliasesMore: boolean;
  /** Its deprecation, its own or its package's (R-API-085); null for none. */
  readonly deprecation: PackageDeprecation | null;
  /** The format hides it from its search and serves it by its number (NuGet, R-NUGET-006). */
  readonly unlisted: boolean;
}

/** The sortable columns, by the name the address carries (`?sort=-published`). */
export const VERSION_SORT_COLUMNS = ['version', 'size', 'vulnerabilities', 'published', 'fetched'] as const;
export type VersionSortColumn = (typeof VERSION_SORT_COLUMNS)[number];

/** The table opens on the highest version first (Q-UI-452). */
export const DEFAULT_VERSION_SORT: TableSort = { column: 'version', direction: 'desc' };

/** How many rows the table shows at first and adds with each **Show more** (Q-UI-452). */
export const VERSION_PAGE = 50;

/**
 * The rows of the versions in the server's version order (`sort=-version`, newest first), with the grades of the package report and
 * the distribution tags of the package. A version without a grade in the report has none.
 */
export function versionRows(
  versions: readonly PackageVersionSummary[],
  grades: ReadonlyMap<string, VulnerabilityGrade>,
  distTags: readonly (readonly [string, string])[],
): VersionRow[] {
  return versions.map((v, order) => ({
    version: v.version,
    prerelease: v.prerelease,
    publishedAt: v.publishedAt,
    sizeBytes: v.sizeBytes ?? null,
    sizePartial: v.sizePartial ?? false,
    lastDownloadedAt: v.lastDownloadedAt ?? null,
    repositories: v.repositories,
    tags: distTags.filter(([, target]) => target === v.version).map(([tag]) => tag),
    grade: grades.get(v.version) ?? null,
    order,
    state: v.state ?? null,
    signatures: v.signatures ?? null,
    aliases: v.aliases ?? null,
    aliasesMore: v.aliasesMore ?? false,
    deprecation: v.deprecation ?? null,
    unlisted: v.unlisted ?? false,
  }));
}

/** The sort of the address: `version`, `-published`, …; anything else is the default. */
export function parseVersionSort(raw: string | null | undefined): TableSort {
  const desc = raw?.startsWith('-') ?? false;
  const column = desc ? raw!.slice(1) : (raw ?? '');
  if (!(VERSION_SORT_COLUMNS as readonly string[]).includes(column)) return DEFAULT_VERSION_SORT;
  return { column, direction: desc ? 'desc' : 'asc' };
}

/** The address's form of a sort; null for the default, which the address leaves out. */
export function versionSortParam(sort: TableSort): string | null {
  if (sort.column === DEFAULT_VERSION_SORT.column && sort.direction === DEFAULT_VERSION_SORT.direction) return null;
  return `${sort.direction === 'desc' ? '-' : ''}${sort.column}`;
}

/**
 * The rows whose version, one of whose distribution tags or one of whose aliases holds the text, case-insensitive: `4.0` finds every
 * 4.0.x, `rc` the release candidates, `latest` the version the tag points at, and an image by any of its tags (`sha-5ccf`, Q-UI-1130).
 */
export function filterVersions(rows: readonly VersionRow[], text: string): readonly VersionRow[] {
  const needle = text.trim().toLowerCase();
  if (needle === '') return rows;
  const holds = (t: string): boolean => t.toLowerCase().includes(needle);
  return rows.filter((r) => holds(r.version) || r.tags.some(holds) || (r.aliases ?? []).some(holds));
}

function key(r: VersionRow, column: string): number {
  switch (column) {
    case 'size':
      // A size that is not known sorts below every known one.
      return r.sizeBytes ?? -1;
    case 'vulnerabilities':
      return gradeRank(r.grade);
    case 'published':
      return Date.parse(r.publishedAt);
    case 'fetched':
      // Never downloaded sorts as the oldest time: last when the newest come first.
      return r.lastDownloadedAt === null ? Number.NEGATIVE_INFINITY : Date.parse(r.lastDownloadedAt);
    default:
      // The version order: a lower place is a higher version.
      return -r.order;
  }
}

/**
 * The rows in the order of the sort. Equal keys keep the version order, the highest version first, whatever the direction:
 * a sort by grade lists the versions of one grade newest first.
 */
export function sortVersions(rows: readonly VersionRow[], sort: TableSort): VersionRow[] {
  const sign = (d: SortDirection): number => (d === 'desc' ? -1 : 1);
  return [...rows].sort((a, b) => {
    const ka = key(a, sort.column);
    const kb = key(b, sort.column);
    if (ka !== kb) return ka < kb ? -sign(sort.direction) : sign(sort.direction);
    return a.order - b.order;
  });
}

/**
 * Whether the rows differ in the repositories that hold them: the column **Repositories** appears only then, since a column with the
 * same value in every row tells no row from another (Q-UI-457).
 */
export function repositoriesDiffer(rows: readonly VersionRow[]): boolean {
  const first = rows[0]?.repositories.join('\n');
  return rows.some((r) => r.repositories.join('\n') !== first);
}

/** The key of a repository in the reader's project, else its qualified name: the cell of the column **Repositories**. */
export function repositoryLabel(qualified: string, project: string): string {
  return qualified.startsWith(`${project}/`) ? qualified.slice(project.length + 1) : qualified;
}

/** The distribution tags of a version's metadata (npm `distTags`, tag to version), in their order; empty for other formats. */
export function distTagsOf(metadata: Record<string, unknown> | undefined): (readonly [string, string])[] {
  const raw = metadata?.['distTags'];
  if (typeof raw !== 'object' || raw === null || Array.isArray(raw)) return [];
  return Object.entries(raw).flatMap(([tag, v]): (readonly [string, string])[] => (typeof v === 'string' ? [[tag, v]] : []));
}
