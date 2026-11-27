import { StorageConnection } from '../../../api/storage-api';

/**
 * Pure helpers of the page Settings › Storage and the dashboard's card (api-and-ui.md R-UI-262 to R-UI-266; storage.md R-STOR-051,
 * R-STOR-061, R-STOR-107). Every state comes from the API: the overview's states are the server's (Q-STOR-505); what stands here only
 * picks the words, the tone and the figure that a state of the API is shown with, and never asks a store.
 */

/** The age past which the oldest gap of a connection that is not filling is behind (the alert QuaderStorageReplicationBacklog). */
export const BACKLOG_BEHIND_SECONDS = 3_600;

/** The share of a stated capacity from which a connection is nearly full, and full (Q-UI-1586). */
export const NEARLY_FULL = 0.8;
export const FULL = 0.95;

/** The tone of a state: green only for a positive one (B.14), every tone with a word and an icon. */
export type Tone = 'ok' | 'neutral' | 'warn' | 'bad';

/** The icon of a tone. */
export function toneIcon(tone: Tone): string {
  switch (tone) {
    case 'ok':
      return 'check_circle';
    case 'warn':
      return 'warning';
    case 'bad':
      return 'error';
  }
  return 'info';
}

/** The state of a connection in words, with the one figure its badge carries. */
export type ConnectionState =
  | { readonly kind: 'active' }
  | { readonly kind: 'filling'; readonly percent: number | null }
  | { readonly kind: 'draining'; readonly copiesLeft: number | null }
  | { readonly kind: 'drained' }
  | { readonly kind: 'detached'; readonly soleBlobs: number | null }
  | { readonly kind: 'retired' };

export function connectionState(c: StorageConnection): ConnectionState {
  const s = c.summary;
  switch (c.state) {
    case 'filling':
      return { kind: 'filling', percent: s ? percentOf(s.fillRatio) : null };
    case 'draining':
      return { kind: 'draining', copiesLeft: s ? s.presentCopies + s.missingCopies + s.corruptCopies : null };
    case 'drained':
      return { kind: 'drained' };
    case 'detached':
      return { kind: 'detached', soleBlobs: s ? s.soleCopies : null };
    case 'retired':
      return { kind: 'retired' };
  }
  return { kind: 'active' };
}

/**
 * A share as a whole percent, rounded down, so that 99.6 % of the blobs is never "100 %" while a blob is missing; a share that is
 * above 0 is at least 1 %.
 */
export function percentOf(ratio: number): number {
  if (ratio >= 1) return 100;
  if (ratio <= 0) return 0;
  return Math.max(1, Math.floor(ratio * 100));
}

/** The replicas on which the connection's circuit is open, of those that configure it; null while it is open on none. */
export function unreachable(c: StorageConnection): { readonly open: number; readonly of: number } | null {
  return c.replicas.circuitOpen > 0 ? { open: c.replicas.circuitOpen, of: c.replicas.configured } : null;
}

/** The live replicas that configure a connection, while only some do (a rolling update); null otherwise. */
export function partlyConfigured(c: StorageConnection): { readonly configured: number; readonly live: number } | null {
  if (c.state === 'detached' || c.state === 'retired' || c.replicas.configured === 0) return null;
  return c.replicas.configured < c.replicas.live ? { configured: c.replicas.configured, live: c.replicas.live } : null;
}

/** How much of every blob a connection holds: its present copies of all blobs as a whole percent; null before the first summary. */
export function holds(c: StorageConnection): number | null {
  return c.summary ? percentOf(c.summary.fillRatio) : null;
}

/**
 * What a connection holds against the capacity its element states (R-STOR-106, Q-STOR-503, Q-UI-1586): the stored bytes of its
 * present copies, and with a capacity the share and its level; `null` before the first summary.
 */
export interface CapacityUse {
  readonly used: number;
  readonly capacity: number | null;
  readonly ratio: number | null;
  readonly level: 'ok' | 'nearly' | 'full' | null;
}

export function capacityUse(c: StorageConnection): CapacityUse | null {
  if (!c.summary) return null;
  const used = c.summary.presentBytes;
  const capacity = c.capacityBytes;
  if (capacity === null || capacity <= 0) return { used, capacity: null, ratio: null, level: null };
  const ratio = used / capacity;
  return { used, capacity, ratio, level: ratio >= FULL ? 'full' : ratio >= NEARLY_FULL ? 'nearly' : 'ok' };
}

/** An age in its largest whole unit, for a sentence such as "oldest 4 minutes". */
export interface Age {
  readonly unit: 'second' | 'minute' | 'hour' | 'day';
  readonly n: number;
}

export function ageOf(seconds: number): Age {
  const s = Math.max(0, Math.floor(seconds));
  if (s >= 86_400) return { unit: 'day', n: Math.floor(s / 86_400) };
  if (s >= 3_600) return { unit: 'hour', n: Math.floor(s / 3_600) };
  if (s >= 60) return { unit: 'minute', n: Math.floor(s / 60) };
  return { unit: 'second', n: s };
}

/** The reconciliation's queue of a connection now; null while it is empty. `behind` is the alert's condition. */
export function backlog(c: StorageConnection, now: number): { readonly blobs: number; readonly age: Age; readonly behind: boolean } | null {
  if (c.queue.blobs <= 0) return null;
  const oldest = c.queue.oldestQueuedAt ? Date.parse(c.queue.oldestQueuedAt) : NaN;
  const seconds = Number.isNaN(oldest) ? 0 : (now - oldest) / 1000;
  return { blobs: c.queue.blobs, age: ageOf(seconds), behind: c.state !== 'filling' && seconds > BACKLOG_BEHIND_SECONDS };
}

/** The host of an endpoint, the place a person recognises a provider by; empty for the AWS endpoint of a region. */
export function hostOf(endpoint: string | null): string {
  if (!endpoint) return '';
  try {
    return new URL(endpoint.includes('://') ? endpoint : `https://${endpoint}`).host;
  } catch {
    return endpoint;
  }
}

/** The connections of the tables: every connection that is not retired. Retired ones are a line under the table, by id. */
export function shownConnections(list: readonly StorageConnection[]): readonly StorageConnection[] {
  return list.filter((c) => c.state !== 'retired');
}

export function retiredConnections(list: readonly StorageConnection[]): readonly StorageConnection[] {
  return list.filter((c) => c.state === 'retired');
}

/**
 * What a connection does in the list's order (R-STOR-050): uploads go to the first connection that can take them and reads try the
 * copies in the order of the list, so its place is its rank for both; a draining connection takes no upload; a connection the answering
 * replica does not configure has no place.
 */
export function roleOf(c: StorageConnection): { readonly uploads: number | null; readonly reads: number | null } {
  if (c.position === null || c.state === 'detached' || c.state === 'retired') return { uploads: null, reads: null };
  const draining = c.drain === true || c.state === 'draining' || c.state === 'drained';
  return { uploads: draining ? null : c.position, reads: c.state === 'drained' ? null : c.position };
}

/** The share of an encrypted connection's present copies that name a storage key, written in Quader's format (R-STOR-106). */
export function inFormat(c: StorageConnection): number | null {
  const s = c.summary;
  if (!c.encrypted || !s) return null;
  return s.presentCopies === 0 ? 1 : s.keyedCopies / s.presentCopies;
}

/** A connection's storage keys: the active one and the counts of the retired and the destroyed ones (R-STOR-088). */
export function keyRing(c: StorageConnection): {
  readonly active: StorageConnection['storageKeys'][number] | null;
  readonly retired: number;
  readonly destroyed: number;
} {
  return {
    active: c.storageKeys.find((k) => k.state === 'active') ?? null,
    retired: c.storageKeys.filter((k) => k.state === 'retired').length,
    destroyed: c.storageKeys.filter((k) => k.state === 'destroyed').length,
  };
}

/**
 * The server-side encryption a store applied against the one it is asked for (R-STOR-032, Q-STOR-519, Q-UI-1610): the algorithm the start
 * check's probe came back with (empty: none reported). It **differs** when the configuration asks for a mode the store did not apply
 * (`s3` without `AES256`, `kms` without `aws:kms`); a store that encrypts under `none` applies a **bucket default**, which is named and
 * is no difference. `null` before a start check wrote a probe it could ask about.
 */
export interface AppliedSse {
  readonly algorithm: string;
  readonly kmsKeyId: string;
  readonly differs: boolean;
  readonly bucketDefault: boolean;
}

export function appliedSse(c: StorageConnection): AppliedSse | null {
  const a = c.capabilities?.sseApplied;
  if (!a) return null;
  const mode = c.settings?.sseMode ?? null;
  const kms = a.algorithm === 'aws:kms' || a.algorithm === 'aws:kms:dsse';
  const differs = (mode === 's3' && a.algorithm !== 'AES256') || (mode === 'kms' && !kms);
  return { algorithm: a.algorithm, kmsKeyId: a.kmsKeyId, differs, bucketDefault: mode === 'none' && a.algorithm !== '' };
}

/** The connections that hold content (B.6): every one that is not retired or drained, as the overview counts them. */
export function holdingConnections(list: readonly StorageConnection[]): readonly StorageConnection[] {
  return list.filter((c) => c.state !== 'retired' && c.state !== 'drained');
}

/** A family of what Quader stores, and whether Quader encrypts it (A.4, Q-UI-1588). */
export interface EncryptsRow {
  readonly family:
    | 'artifacts'
    | 'upload-parts'
    | 'upload-spool'
    | 'database-secrets'
    | 'database-other'
    | 'backup-artifacts'
    | 'backup-database'
    | 'journal'
    | 'bastion-caches';
  /** `yes` encrypted, `partly` as something else decides, `no` in the clear, `plain` in the clear on purpose. */
  readonly mark: 'yes' | 'partly' | 'no' | 'plain';
  /** The connections the row stands for: those of the artifacts and the upload parts; empty otherwise. */
  readonly connections: readonly string[];
  /** The open gap of the security pages the row's help names, when there is one (H-2432). */
  readonly gap: string | null;
}

/**
 * What Quader encrypts, family by family (the plan's A.4; storage.md R-STOR-075, R-STOR-081, R-ARCH-021, R-BAK-031, R-BAS-061;
 * Q-UI-1588): product knowledge in words, with the connections from the list. The artifacts and the upload parts stand once for the
 * encrypted and once for the plain connections that hold content; the rest is the same for every installation. A change of what
 * Quader encrypts changes this list (H-2432), and its Vitest holds it.
 */
export function whatQuaderEncrypts(list: readonly StorageConnection[]): readonly EncryptsRow[] {
  const held = holdingConnections(list);
  const encrypted = held.filter((c) => c.encrypted).map((c) => c.id);
  const plain = held.filter((c) => !c.encrypted).map((c) => c.id);
  const rows: EncryptsRow[] = [];
  for (const family of ['artifacts', 'upload-parts'] as const) {
    if (encrypted.length > 0) rows.push({ family, mark: 'yes', connections: encrypted, gap: null });
    if (plain.length > 0) rows.push({ family, mark: 'no', connections: plain, gap: family === 'artifacts' && encrypted.length > 0 ? 'H-1317' : null });
  }
  rows.push(
    { family: 'upload-spool', mark: 'no', connections: [], gap: 'H-2' },
    { family: 'database-secrets', mark: 'yes', connections: [], gap: null },
    { family: 'database-other', mark: 'no', connections: [], gap: null },
    { family: 'backup-artifacts', mark: 'partly', connections: [], gap: null },
    { family: 'backup-database', mark: 'no', connections: [], gap: 'H-48' },
    { family: 'journal', mark: 'plain', connections: [], gap: 'H-1316' },
    { family: 'bastion-caches', mark: 'partly', connections: [], gap: null },
  );
  return rows;
}

/** The overview's states of the six tiles (B.14); the tile Problems is the period's (R-STOR-111). */
export type TileKind = 'connections' | 'availability' | 'encryption' | 'integrity' | 'problems' | 'stored';

/** The tone of a tile's state (B.14, Q-UI-1587): green only for a positive state. */
export function tileTone(kind: TileKind, state: string): Tone {
  switch (`${kind}:${state}`) {
    case 'connections:healthy':
    case 'availability:redundant':
    case 'encryption:encrypted':
    case 'integrity:verified':
    case 'problems:none':
      return 'ok';
    case 'connections:degraded':
    case 'availability:at_risk':
    case 'encryption:partly':
    case 'integrity:behind':
    case 'integrity:repairing':
    case 'problems:open':
      return 'warn';
    case 'connections:unreachable':
    case 'integrity:damaged':
    case 'problems:ongoing':
      return 'bad';
  }
  return 'neutral';
}

/** A tile's state word (B.14's table, Q-UI-1587). */
export function tileWord(kind: TileKind, state: string): string {
  switch (`${kind}:${state}`) {
    case 'connections:healthy':
      return $localize`:@@storage.tile.connections.healthy:Healthy`;
    case 'connections:degraded':
      return $localize`:@@storage.tile.connections.degraded:Degraded`;
    case 'connections:unreachable':
      return $localize`:@@storage.tile.connections.unreachable:Unreachable`;
    case 'availability:redundant':
      return $localize`:@@storage.tile.availability.redundant:Redundant`;
    case 'availability:catching_up':
      return $localize`:@@storage.tile.availability.catching-up:Catching up`;
    case 'availability:at_risk':
      return $localize`:@@storage.tile.availability.at-risk:At risk`;
    case 'availability:one_store':
      return $localize`:@@storage.tile.availability.one-store:One store`;
    case 'encryption:encrypted':
      return $localize`:@@storage.tile.encryption.encrypted:Encrypted`;
    case 'encryption:partly':
      return $localize`:@@storage.tile.encryption.partly:Partly`;
    case 'encryption:provider_only':
      return $localize`:@@storage.tile.encryption.provider-only:Provider only`;
    case 'encryption:off':
      return $localize`:@@storage.tile.encryption.off:Off`;
    case 'integrity:verified':
      return $localize`:@@storage.tile.integrity.verified:Verified`;
    case 'integrity:checking':
      return $localize`:@@storage.tile.integrity.checking:Checking`;
    case 'integrity:behind':
      return $localize`:@@storage.tile.integrity.behind:Behind`;
    case 'integrity:repairing':
      return $localize`:@@storage.tile.integrity.repairing:Repairing`;
    case 'integrity:damaged':
      return $localize`:@@storage.tile.integrity.damaged:Damaged`;
    case 'problems:none':
      return $localize`:@@storage.tile.problems.none:None`;
    case 'problems:resolved':
      return $localize`:@@storage.tile.problems.resolved:Resolved`;
    case 'problems:open':
      return $localize`:@@storage.tile.problems.open:Open`;
    case 'problems:ongoing':
      return $localize`:@@storage.tile.problems.ongoing:Ongoing`;
  }
  return state;
}
