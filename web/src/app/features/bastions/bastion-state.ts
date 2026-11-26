import { formatNumber } from '@angular/common';
import { Bastion } from '../../api/bastions-api';
import { formatCount } from '../limits/figures';

/** How long a replica's report counts as its state now (R-UI-240): an older one is no report. */
export const REPORT_FRESH_MS = 5 * 60_000;

export type ReplicaStatus = Bastion['status'][number];

/** One active block a replica reported, with that replica (R-UI-241's **Blocks**). */
export interface ReportedBlock {
  readonly address: string;
  readonly until: string;
  readonly reason: string;
  readonly replica: string;
}

/** The state of a Bastion now, from the replicas that reported within the last five minutes (R-UI-240, R-LIM-045). */
export interface BastionState {
  readonly replicas: number;
  readonly cacheBytes: number;
  readonly capacityBytes: number;
  /** Hits against hits and misses; null before the first request. */
  readonly hitRatio: number | null;
  readonly protectedBytes: number;
  /** What the replicas read from the main installation and the object store now, every class together. */
  readonly bytesPerSecond: number;
  /** A replica's cap holds a read back now (R-BAS-097). */
  readonly throughputLimited: boolean;
  readonly limited: number;
  /** The addresses blocked now: each address once, however many replicas name it. */
  readonly blocked: number;
}

/** The reports of the last five minutes, the newest first. */
export function freshReports(bastion: Pick<Bastion, 'status'>, now: number): ReplicaStatus[] {
  return bastion.status.filter((s) => now - Date.parse(s.reportedAt) <= REPORT_FRESH_MS);
}

/**
 * The cache's figure f over the reports (R-UI-240, Q-BAS-480): a replica on the volume backend reports a cache of its own, and those
 * are added up; the replicas on the bucket backend (`shared`) report the whole Bastion's bucket alike, which counts once, as the
 * largest of their values.
 */
function cacheFigure(reports: readonly ReplicaStatus[], f: (s: ReplicaStatus) => number): number {
  const shared = reports.filter((s) => s.cache.shared).map(f);
  const own = reports.filter((s) => !s.cache.shared).reduce((t, s) => t + f(s), 0);
  return own + (shared.length > 0 ? Math.max(...shared) : 0);
}

/**
 * The card's state (R-UI-240, Q-UI-1489): the cache's fill, capacity and protected bytes are the sum of the replicas' own caches on
 * the volume backend and the bucket's figure once on the bucket backend, as each report says (Q-BAS-480); the hits and misses, the
 * reads and the callers limited are each replica's own and are added up. An address blocked at several replicas counts once; where
 * the replicas name fewer blocks than they count (a replica names at most 100), the larger count stands. Null where no replica
 * reported in the last five minutes.
 */
export function bastionState(bastion: Pick<Bastion, 'status'>, now: number): BastionState | null {
  const reports = freshReports(bastion, now);
  if (reports.length === 0) return null;
  const sum = (f: (s: ReplicaStatus) => number): number => reports.reduce((t, s) => t + f(s), 0);
  const hits = sum((s) => s.cache.hits);
  const misses = sum((s) => s.cache.misses);
  const addresses = new Set(reports.flatMap((s) => s.blocks.map((b) => b.address)));
  return {
    replicas: reports.length,
    cacheBytes: cacheFigure(reports, (s) => s.cache.bytes),
    capacityBytes: cacheFigure(reports, (s) => s.cache.capacityBytes),
    hitRatio: hits + misses > 0 ? hits / (hits + misses) : null,
    protectedBytes: cacheFigure(reports, (s) => s.cache.protectedBytes),
    bytesPerSecond: sum((s) => s.throughput.bytesPerSecondByClass.anonymous + s.throughput.bytesPerSecondByClass.signedIn + s.throughput.bytesPerSecondByClass.catalogue),
    throughputLimited: reports.some((s) => s.throughput.limited),
    limited: sum((s) => s.limited),
    blocked: Math.max(addresses.size, ...reports.map((s) => s.blocked)),
  };
}

/** Every active block the replicas reported within the last hour, the one ending last first; an address per replica (R-UI-241). */
export function reportedBlocks(bastion: Pick<Bastion, 'status'>, now: number): ReportedBlock[] {
  return bastion.status
    .flatMap((s) => s.blocks.map((b) => ({ ...b, replica: s.replica })))
    .filter((b) => Date.parse(b.until) > now)
    .sort((a, b) => b.until.localeCompare(a.until) || a.address.localeCompare(b.address));
}

/**
 * Why a Bastion blocked an address, in words (R-UI-240, R-UI-241): the closed list a Bastion reports (E9b, R-LIM-045). A code outside
 * it, from a Bastion of a later release, stands as it came.
 */
export function blockReasonText(reason: string): string {
  switch (reason) {
    case 'refusals':
      return $localize`:@@bastion.block-reason.refusals:Too many requests refused by its flood protection`;
    case 'failed_auth':
      return $localize`:@@bastion.block-reason.failed-auth:Too many failed sign-ins`;
    case 'refused_requests':
      return $localize`:@@bastion.block-reason.refused-requests:Too many requests refused by its download limits`;
    default:
      return reason;
  }
}

/**
 * Why the addresses the replicas reported in the last five minutes are blocked (R-UI-240): each reason in words with its number of
 * addresses, an address blocked at several replicas once, in the order of the closed list.
 */
export function blockReasons(bastion: Pick<Bastion, 'status'>, now: number): string[] {
  const byAddress = new Map<string, string>();
  for (const s of freshReports(bastion, now)) for (const b of s.blocks) if (Date.parse(b.until) > now) byAddress.set(b.address, b.reason);
  const counts = new Map<string, number>();
  for (const r of byAddress.values()) counts.set(r, (counts.get(r) ?? 0) + 1);
  const order = ['refusals', 'refused_requests', 'failed_auth'];
  return [...counts.entries()]
    .sort(([a], [b]) => (order.indexOf(a) + 1 || 99) - (order.indexOf(b) + 1 || 99) || a.localeCompare(b))
    .map(([r, n]) => `${blockReasonText(r)} (${n})`);
}

/** "14.0 GB": one decimal, as the state line reads (R-UI-240). */
export function gigabytes(bytes: number, locale: string): string {
  return `${formatNumber(bytes / 1e9, locale, '1.1-1')} GB`;
}

/** "41 MB/s": whole megabytes a second. */
export function megabytesPerSecond(bytes: number, locale: string): string {
  return `${formatNumber(bytes / 1e6, locale, '1.0-0')} MB/s`;
}

/** The parts of the state line, each a phrase of its own (R-UI-240). */
export function stateParts(s: BastionState, locale: string): string[] {
  const used = formatNumber(s.cacheBytes / 1e9, locale, '1.1-1');
  const parts = [$localize`:@@bastion.state.cache:Cache ${used}:used: of ${gigabytes(s.capacityBytes, locale)}:capacity:`];
  if (s.hitRatio !== null) {
    parts.push($localize`:@@bastion.state.hits:${formatNumber(s.hitRatio * 100, locale, '1.0-0')}:percent: % hits`);
  }
  parts.push($localize`:@@bastion.state.protected:${gigabytes(s.protectedBytes, locale)}:bytes: protected`);
  parts.push($localize`:@@bastion.state.read:${megabytesPerSecond(s.bytesPerSecond, locale)}:rate: from the main installation`);
  return parts;
}

/** "4 callers limited now", "1 caller limited now". */
export function limitedText(n: number, locale: string): string {
  return n === 1
    ? $localize`:@@bastion.state.limited-one:1 caller limited now`
    : $localize`:@@bastion.state.limited:${formatCount(n, locale)}:count: callers limited now`;
}

/** "1 address blocked", "3 addresses blocked". */
export function blockedText(n: number, locale: string): string {
  return n === 1
    ? $localize`:@@bastion.state.blocked-one:1 address blocked`
    : $localize`:@@bastion.state.blocked:${formatCount(n, locale)}:count: addresses blocked`;
}

/**
 * The lowest request figure per address an edge in front of the Bastion reported (R-BAS-093): `rps=<n>` of a contact's `edgeLimit`;
 * null where no replica reported one, or every one reported `off` or connections alone.
 */
export function edgeRequestsPerSecond(bastion: Pick<Bastion, 'contacts'>): number | null {
  const figures = bastion.contacts.flatMap((c) => {
    const m = /(?:^|[;\s])rps=(\d+)/.exec(c.edgeLimit ?? '');
    return m ? [Number(m[1])] : [];
  });
  return figures.length > 0 ? Math.min(...figures) : null;
}

/**
 * The edge figure where a configured per-address request figure lies above it (R-UI-240): the ingress refuses before the Bastion's
 * figures apply. Null where none does.
 */
export function edgeBelowFigures(bastion: Pick<Bastion, 'contacts' | 'limits'>): number | null {
  const edge = edgeRequestsPerSecond(bastion);
  if (edge === null) return null;
  const f = bastion.limits.flood;
  return [f.anonymous.requestsPerSecond, f.authenticated.requestsPerSecond, f.gui.requestsPerSecond].some((v) => v > edge) ? edge : null;
}
