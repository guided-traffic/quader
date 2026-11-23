import { StorageConnection } from '../../../api/storage-api';

/**
 * Pure derivations of the tab Storage and the dashboard's lines (api-and-ui.md R-UI-196, R-UI-197; storage.md R-STOR-051,
 * R-STOR-061). Every figure comes from the API; nothing here asks a store.
 */

/** The age past which the oldest gap of a connection that is not filling is behind (the alert QuaderStorageReplicationBacklog). */
export const BACKLOG_BEHIND_SECONDS = 3_600;

/** The state of a connection in words, with the one figure its badge carries (R-UI-196). */
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

/** Whether the connection is unreachable on every live replica that configures it: what the alerts of R-STOR-060 call unreachable. */
export function unreachableEverywhere(c: StorageConnection): boolean {
  return c.replicas.configured > 0 && c.replicas.circuitOpen >= c.replicas.configured;
}

/** The live replicas that configure a connection, while only some do (a rolling update); null otherwise. */
export function partlyConfigured(c: StorageConnection): { readonly configured: number; readonly live: number } | null {
  if (c.state === 'detached' || c.state === 'retired' || c.replicas.configured === 0) return null;
  return c.replicas.configured < c.replicas.live ? { configured: c.replicas.configured, live: c.replicas.live } : null;
}

/** The fill of a connection: its present copies of all blobs as a whole percent, and their stored bytes. */
export function fill(c: StorageConnection): { readonly percent: number; readonly bytes: number } | null {
  const s = c.summary;
  return s ? { percent: percentOf(s.fillRatio), bytes: s.presentBytes } : null;
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

/** The damaged copies of a connection from the latest summary; null while there is none. */
export function damagedCopies(c: StorageConnection): { readonly missing: number; readonly corrupt: number } | null {
  const s = c.summary;
  if (!s || s.missingCopies + s.corruptCopies === 0) return null;
  return { missing: s.missingCopies, corrupt: s.corruptCopies };
}

/** How much of a connection the scrub read whole within its period, in days; null without a summary or a present copy. */
export function scrub(c: StorageConnection): { readonly percent: number; readonly days: number | null; readonly behind: boolean } | null {
  const s = c.summary;
  if (!s || s.presentCopies === 0) return null;
  const days = c.scrubPeriodSeconds === null ? null : Math.round(c.scrubPeriodSeconds / 86_400);
  return { percent: percentOf(s.verifiedWithinPeriod / s.presentCopies), days, behind: s.scrubOverdue > 0 };
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

/** The connections of the table: every connection that is not retired. Retired ones are a line under it, by id. */
export function shownConnections(list: readonly StorageConnection[]): readonly StorageConnection[] {
  return list.filter((c) => c.state !== 'retired');
}

export function retiredConnections(list: readonly StorageConnection[]): readonly StorageConnection[] {
  return list.filter((c) => c.state === 'retired');
}

/** The time of the latest storage summary the list carries; null before the first. */
export function summaryAt(list: readonly StorageConnection[]): string | null {
  return list.find((c) => c.summary)?.summary?.computedAt ?? null;
}

/** What needs an act, for the line over the table (R-UI-196), worst first. */
export type StorageCondition =
  | { readonly kind: 'unreachable'; readonly level: 'error'; readonly ids: readonly string[] }
  | { readonly kind: 'detached'; readonly level: 'warning'; readonly ids: readonly string[] }
  | { readonly kind: 'damaged-blobs'; readonly level: 'warning'; readonly count: number; readonly more: boolean }
  | { readonly kind: 'backlog-behind'; readonly level: 'warning'; readonly ids: readonly string[] }
  | { readonly kind: 'scrub-behind'; readonly level: 'warning'; readonly ids: readonly string[] }
  | { readonly kind: 'drained'; readonly level: 'info'; readonly ids: readonly string[] };

export function storageConditions(
  list: readonly StorageConnection[],
  damaged: { readonly count: number; readonly more: boolean } | null,
  now: number,
): StorageCondition[] {
  const shown = shownConnections(list);
  const ids = (f: (c: StorageConnection) => boolean): string[] => shown.filter(f).map((c) => c.id);
  const out: StorageCondition[] = [];
  const down = ids((c) => unreachable(c) !== null);
  if (down.length > 0) out.push({ kind: 'unreachable', level: 'error', ids: down });
  const detached = ids((c) => c.state === 'detached');
  if (detached.length > 0) out.push({ kind: 'detached', level: 'warning', ids: detached });
  if (damaged && damaged.count > 0) out.push({ kind: 'damaged-blobs', level: 'warning', count: damaged.count, more: damaged.more });
  const behind = ids((c) => backlog(c, now)?.behind ?? false);
  if (behind.length > 0) out.push({ kind: 'backlog-behind', level: 'warning', ids: behind });
  const scrubBehind = ids((c) => scrub(c)?.behind ?? false);
  if (scrubBehind.length > 0) out.push({ kind: 'scrub-behind', level: 'warning', ids: scrubBehind });
  const drained = ids((c) => c.state === 'drained');
  if (drained.length > 0) out.push({ kind: 'drained', level: 'info', ids: drained });
  return out;
}

/**
 * The dashboard's word on the object store (R-UI-197): an error while every connection that a replica configures is unreachable on
 * every replica, a warning while one of several is, else nothing.
 */
export function dashboardStorageAlarm(list: readonly StorageConnection[]): 'error' | 'warning' | null {
  const configured = list.filter((c) => c.replicas.configured > 0);
  const down = configured.filter(unreachableEverywhere);
  if (down.length === 0) return null;
  return down.length === configured.length ? 'error' : 'warning';
}
