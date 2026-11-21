/*
 * The ClamAV definitions on the package screens (R-UI-194, R-CLAMAV-003, R-CLAMAV-007, the development agent's Q-UI-904): a package is
 * one of ClamAV's databases (`main`, `daily`, `bytecode`), a version one version of its container. The screens are every format's;
 * these are the words and the reading of the metadata the format's view adapter sends (`internal/formats/clamav/view.go` `Metadata`).
 * A member of the wrong type is skipped, never an error, and every text of a header is shown as text.
 */

/** The format of the definitions (R-CLAMAV-001). */
export const CLAMAV_FORMAT = 'clamav';

/** The databases in the order the list shows them: by name, as the repository lists its packages. */
export const CLAMAV_DATABASES = ['bytecode', 'daily', 'main'] as const;

/**
 * The age past which `daily` is outdated: ClamAV's engine warns "The virus database is older than 7 days!" when it loads a `daily`
 * container built more than 604,800 seconds ago, and for no other database (`cli_cvdload` in `libclamav/cvd.c` of ClamAV 1.5.5, the
 * development agent's Q-UI-912). `main` and `bytecode` are rebuilt rarely, so their age is no warning.
 */
export const OUTDATED_SECONDS = 604_800;

/** The one database whose age ClamAV warns about (Q-UI-912). */
export const AGED_DATABASE = 'daily';

const DAY_MS = 86_400_000;

/** The state of a version as the format's rows say it (R-CLAMAV-003). */
export type ClamavState = 'in_use' | 'previous' | 'superseded' | 'files_removed';

/** How the database task checked a container before it stored it (R-MAL-033). */
export type ClamavVerification = 'signature_verified' | 'not_verified_repository' | 'unsigned';

/** One member file of a container's archive, as its index lists it, with its lines that are neither empty nor a comment. */
export interface ClamavMember {
  readonly name: string;
  readonly size: number | null;
  readonly sha256: string;
  readonly lines: number | null;
}

/** What a version of a database shows (R-CLAMAV-003): every field of its header, its verification, its signature file, its members. */
export interface ClamavFacts {
  readonly database: string;
  readonly state: ClamavState | null;
  /** The build time (the header's field 9), ISO 8601; null for a version stored without it. */
  readonly builtAt: string | null;
  /** The header's field 2 as written. */
  readonly buildTimeText: string;
  readonly signatures: number | null;
  readonly functionalityLevel: number | null;
  /** "ClamAV 0.103 or later", "Newer than ClamAV 1.5": the format's table of functionality levels. */
  readonly functionalityLevelText: string;
  readonly builder: string;
  readonly verification: ClamavVerification | null;
  /** The repository a copied container came from (`ops/clamav-db`), for a container that was not verified by its signature. */
  readonly source: string;
  readonly headerMd5: string;
  readonly computedMd5: string;
  /** Whether the member list was read; false when the bounds cut it short (R-MAL-033). */
  readonly membersRead: boolean;
  readonly members: readonly ClamavMember[];
  readonly detachedSignature: { readonly present: boolean; readonly name: string; readonly size: number | null; readonly signatures: number | null };
  readonly filesRemovedAt: string | null;
}

const str = (v: unknown): string => (typeof v === 'string' ? v : '');
const num = (v: unknown): number | null => (typeof v === 'number' && Number.isFinite(v) ? v : null);
const obj = (v: unknown): Record<string, unknown> => (typeof v === 'object' && v !== null && !Array.isArray(v) ? (v as Record<string, unknown>) : {});

const STATES: readonly string[] = ['in_use', 'previous', 'superseded', 'files_removed'];
const VERIFICATIONS: readonly string[] = ['signature_verified', 'not_verified_repository', 'unsigned'];

/** A state the screens know, or null. */
export function clamavState(v: unknown): ClamavState | null {
  return typeof v === 'string' && STATES.includes(v) ? (v as ClamavState) : null;
}

/** Reads the metadata of a version of the definitions; null when it names no database (any other format's version). */
export function clamavFacts(m: Record<string, unknown>): ClamavFacts | null {
  const database = str(m['database']);
  if (database === '') return null;
  const v = obj(m['verification']);
  const sig = obj(m['detachedSignature']);
  const outcome = str(v['outcome']);
  const members = (Array.isArray(m['members']) ? m['members'] : []).flatMap((x): ClamavMember[] => {
    const e = obj(x);
    const name = str(e['name']);
    return name === '' ? [] : [{ name, size: num(e['size']), sha256: str(e['sha256']), lines: num(e['lines']) }];
  });
  members.sort((a, b) => (a.name < b.name ? -1 : a.name > b.name ? 1 : 0));
  return {
    database,
    state: clamavState(m['state']),
    builtAt: str(m['builtAt']) || null,
    buildTimeText: str(m['buildTimeText']),
    signatures: num(m['signatures']),
    functionalityLevel: num(m['functionalityLevel']),
    functionalityLevelText: str(m['functionalityLevelText']),
    builder: str(m['builder']),
    verification: VERIFICATIONS.includes(outcome) ? (outcome as ClamavVerification) : null,
    source: str(v['source']),
    headerMd5: str(v['headerMd5']),
    computedMd5: str(v['computedMd5']),
    membersRead: m['membersRead'] === true,
    members,
    detachedSignature: {
      present: sig['present'] === true,
      name: str(sig['name']),
      size: num(sig['size']),
      signatures: num(sig['signatures']),
    },
    filesRemovedAt: str(m['filesRemovedAt']) || null,
  };
}

/** Whole days between a build time and `now`; null without a build time. */
export function ageDays(builtAt: string | null, now: number): number | null {
  if (!builtAt) return null;
  const at = Date.parse(builtAt);
  return Number.isNaN(at) ? null : Math.max(0, Math.floor((now - at) / DAY_MS));
}

/**
 * A version of `daily` built more than 7 days ago is outdated (R-UI-194, Q-UI-912): ClamAV warns about it, as it does about no other
 * database.
 */
export function isOutdated(database: string, builtAt: string | null, now: number): boolean {
  if (database !== AGED_DATABASE || !builtAt) return false;
  const at = Date.parse(builtAt);
  return !Number.isNaN(at) && now - at > OUTDATED_SECONDS * 1000;
}

/** The badge **Outdated** and why, its tooltip and the row's note. */
export function outdatedBadge(): string {
  return $localize`:@@clamav.outdated:Outdated`;
}
export function outdatedNote(): string {
  return $localize`:@@clamav.outdated.note:ClamAV warns about a database older than 7 days.`;
}

/** The age of a database as the value of the column **Age**: "under a day", "1 day", "8 days". */
export function ageText(days: number): string {
  if (days === 0) return $localize`:@@clamav.age.col.under-day:under a day`;
  return days === 1 ? $localize`:@@clamav.age.col.one:1 day` : $localize`:@@clamav.age.col.many:${days}:days: days`;
}

/** The age of a version after its build time, as running text: "under a day old", "1 day old", "8 days old". */
export function ageOldText(days: number): string {
  if (days === 0) return $localize`:@@clamav.age.under-day:under a day old`;
  return days === 1 ? $localize`:@@clamav.age.one:1 day old` : $localize`:@@clamav.age.many:${days}:days: days old`;
}

/** The badge of a version's state (R-UI-194): **In use**, **Previous**, **Superseded**, **Files removed**. */
export function stateText(state: ClamavState): string {
  switch (state) {
    case 'in_use':
      return $localize`:@@clamav.state.in-use:In use`;
    case 'previous':
      return $localize`:@@clamav.state.previous:Previous`;
    case 'superseded':
      return $localize`:@@clamav.state.superseded:Superseded`;
    case 'files_removed':
      return $localize`:@@clamav.state.files-removed:Files removed`;
  }
}

/** The order of the states on a database's page: the version in use first, then the one before it, then the rest (R-UI-194). */
export function stateRank(state: ClamavState | null | undefined): number {
  switch (state) {
    case 'in_use':
      return 0;
    case 'previous':
      return 1;
    default:
      return 2;
  }
}

/** How a container was checked, in words (R-CLAMAV-003). */
export function verificationText(f: Pick<ClamavFacts, 'verification' | 'source'>): string {
  switch (f.verification) {
    case 'signature_verified':
      return $localize`:@@clamav.verification.signature:ClamAV's signature verified`;
    case 'not_verified_repository': {
      const source = f.source || '–';
      return $localize`:@@clamav.verification.repository:Not verified: from the repository ${source}:repository:`;
    }
    case 'unsigned':
      return $localize`:@@clamav.verification.unsigned:Unsigned container`;
    default:
      return $localize`:@@clamav.verification.unknown:Not recorded`;
  }
}

/** One database's version in a set: `main-63`. */
export interface DatabaseVersion {
  readonly database: string;
  readonly version: string;
}

/**
 * The name of a set (Q-MAL-82): `main-63.daily-28190.bytecode-339`, in the order main, daily, bytecode; a database the set does not
 * name is left out.
 */
export function setName(versions: readonly DatabaseVersion[]): string {
  return ['main', 'daily', 'bytecode']
    .flatMap((db) => {
      const v = versions.find((x) => x.database === db);
      return v ? [`${db}-${v.version}`] : [];
    })
    .join('.');
}
