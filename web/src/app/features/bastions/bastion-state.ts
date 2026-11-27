import { formatNumber } from '@angular/common';
import { Bastion, BastionTokenSummary } from '../../api/bastions-api';
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
 * are added up; the replicas on the bucket backend (`shared`) report their deployment's bucket alike, which counts once per deployment,
 * as the largest of their values, and the deployments' buckets add up (Q-UI-1643: a pair of deployments has two buckets). A report's
 * deployment is the base URL of its replica's contact.
 */
function cacheFigure(reports: readonly ReplicaStatus[], deploymentOf: (replica: string) => string, f: (s: ReplicaStatus) => number): number {
  const shared = new Map<string, number>();
  let own = 0;
  for (const s of reports) {
    if (!s.cache.shared) {
      own += f(s);
      continue;
    }
    const d = deploymentOf(s.replica);
    shared.set(d, Math.max(shared.get(d) ?? 0, f(s)));
  }
  return own + [...shared.values()].reduce((t, v) => t + v, 0);
}

/**
 * The card's state (R-UI-240, Q-UI-1489): the cache's fill, capacity and protected bytes are the sum of the replicas' own caches on
 * the volume backend and each deployment's bucket once on the bucket backend, as each report says (Q-BAS-480, Q-UI-1643); the hits and misses, the
 * reads and the callers limited are each replica's own and are added up. An address blocked at several replicas counts once; where
 * the replicas name fewer blocks than they count (a replica names at most 100), the larger count stands. Null where no replica
 * reported in the last five minutes.
 */
export function bastionState(bastion: Pick<Bastion, 'status' | 'contacts'>, now: number): BastionState | null {
  const reports = freshReports(bastion, now);
  if (reports.length === 0) return null;
  const urls = new Map(bastion.contacts.map((c) => [c.replica, c.baseUrl]));
  const deploymentOf = (replica: string): string => urls.get(replica) ?? '';
  const sum = (f: (s: ReplicaStatus) => number): number => reports.reduce((t, s) => t + f(s), 0);
  const hits = sum((s) => s.cache.hits);
  const misses = sum((s) => s.cache.misses);
  const addresses = new Set(reports.flatMap((s) => s.blocks.map((b) => b.address)));
  return {
    replicas: reports.length,
    cacheBytes: cacheFigure(reports, deploymentOf, (s) => s.cache.bytes),
    capacityBytes: cacheFigure(reports, deploymentOf, (s) => s.cache.capacityBytes),
    hitRatio: hits + misses > 0 ? hits / (hits + misses) : null,
    protectedBytes: cacheFigure(reports, deploymentOf, (s) => s.cache.protectedBytes),
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

/** "14.0 GB": one decimal, as the state line reads (R-UI-240). */
export function gigabytes(bytes: number, locale: string): string {
  return `${formatNumber(bytes / 1e9, locale, '1.1-1')} GB`;
}

/** "41 MB/s": whole megabytes a second. */
export function megabytesPerSecond(bytes: number, locale: string): string {
  return `${formatNumber(bytes / 1e6, locale, '1.0-0')} MB/s`;
}

/**
 * The parts of the row **Now**, each a phrase of its own (R-UI-240). The read rate stands only where `withRead` says so: where a maximum
 * throughput is set, the meter under its field says the rate already.
 */
export function stateParts(s: BastionState, locale: string, withRead = true): string[] {
  const used = formatNumber(s.cacheBytes / 1e9, locale, '1.1-1');
  const parts = [$localize`:@@bastion.state.cache:Cache ${used}:used: of ${gigabytes(s.capacityBytes, locale)}:capacity:`];
  if (s.hitRatio !== null) {
    parts.push($localize`:@@bastion.state.hits:${formatNumber(s.hitRatio * 100, locale, '1.0-0')}:percent: % hits`);
  }
  parts.push($localize`:@@bastion.state.protected:${gigabytes(s.protectedBytes, locale)}:bytes: protected`);
  if (withRead) parts.push($localize`:@@bastion.state.read:${megabytesPerSecond(s.bytesPerSecond, locale)}:rate: from the main installation`);
  return parts;
}

/** "4 callers limited", "1 caller limited": the row says **Now**. */
export function limitedText(n: number, locale: string): string {
  return n === 1
    ? $localize`:@@bastion.state.limited-one:1 caller limited`
    : $localize`:@@bastion.state.limited:${formatCount(n, locale)}:count: callers limited`;
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

/** Why the builder of the public catalogue builds nothing for a registration (R-BAS-104), as its `catalogue.reason` says it. */
export type CatalogueStop = NonNullable<NonNullable<Bastion['catalogue']>['reason']>;

/**
 * What the card says of the builder's last run while the web interface is on (R-BAS-104, R-UI-240): `stopped` with its reason, `built`
 * with the time of the run, or `pending` while no run completed since the switch (also before the first run made a state). Null while
 * the web interface is off: the card says nothing of a catalogue then.
 */
export type CatalogueLine =
  | { readonly kind: 'stopped'; readonly reason: CatalogueStop }
  | { readonly kind: 'built'; readonly at: string }
  | { readonly kind: 'pending' };

export function catalogueLine(bastion: Pick<Bastion, 'switches' | 'catalogue'>): CatalogueLine | null {
  if (!bastion.switches.gui) return null;
  const c = bastion.catalogue ?? null;
  if (c?.outcome === 'stopped' && c.reason) return { kind: 'stopped', reason: c.reason };
  if (c?.outcome === 'built' && c.checkedAt) return { kind: 'built', at: c.checkedAt };
  return { kind: 'pending' };
}

/**
 * What the row **Web interface** says of the builder while the web interface is on (R-BAS-104, Q-BAS-497): after the switch's word
 * the time of the last build (`built`), "Not built yet" (`pending`), or, while no replica made contact, that it is built once one has
 * (`waits`); in their place a warning under the row where the builder stopped: the main installation admits no request without a
 * credential (`refused`), the base URL the newest contact reported is not an address of the installation (`unlisted`, with that URL),
 * or the newest contact reported none (`no-url`). Null while the web interface is off.
 */
export type CatalogueNote =
  | { readonly kind: 'built'; readonly at: string }
  | { readonly kind: 'pending' }
  | { readonly kind: 'waits' }
  | { readonly kind: 'refused' }
  | { readonly kind: 'unlisted'; readonly url: string }
  | { readonly kind: 'no-url' };

export function catalogueNote(bastion: Pick<Bastion, 'switches' | 'catalogue' | 'contacts'>): CatalogueNote | null {
  const line = catalogueLine(bastion);
  if (!line || line.kind !== 'stopped') return line;
  if (line.reason === 'anonymous_requests_refused') return { kind: 'refused' };
  const newest = newestContact(bastion.contacts);
  if (!newest) return { kind: 'waits' };
  return newest.baseUrl ? { kind: 'unlisted', url: newest.baseUrl } : { kind: 'no-url' };
}

/** The contact with the latest time; null where no replica made contact. */
export function newestContact(contacts: Bastion['contacts']): Bastion['contacts'][number] | null {
  let newest: Bastion['contacts'][number] | null = null;
  for (const c of contacts) if (!newest || Date.parse(c.contactAt) > Date.parse(newest.contactAt)) newest = c;
  return newest;
}

/** A token that expires within this many days carries the warning mark and turns the section **Tokens** into a warning (R-BAS-007). */
export const BASTION_TOKEN_WARN_DAYS = 30;

const DAY_MS = 86_400_000;

/** The state of a token against `now`: expired, expiring within the warning window, or fine. */
export function tokenExpiry(token: Pick<BastionTokenSummary, 'expiresAt'>, now: number): 'expired' | 'soon' | 'ok' {
  // The declared token does not expire (R-BAS-127).
  if (!token.expiresAt) return 'ok';
  const left = Date.parse(token.expiresAt) - now;
  if (left <= 0) return 'expired';
  return left < BASTION_TOKEN_WARN_DAYS * DAY_MS ? 'soon' : 'ok';
}

/** How long a deployment's address stands in the card's head after its last contact (Q-UI-1641): a deployment taken down drops out a day later. */
export const DEPLOYMENT_HEARD_MS = 24 * 3_600_000;

/** The host of a base URL as the card names it: no scheme, no default port, no path ("pkgs.example.com", "pkgs.example.com:8443"). */
export function hostOf(baseUrl: string): string {
  try {
    return new URL(baseUrl).host || baseUrl;
  } catch {
    return baseUrl;
  }
}

/**
 * One deployment of a registration (Q-UI-1643): the replicas that report one base URL. A pair of deployments under one registration
 * (two clusters, two addresses, one token) is legitimate. `baseUrl` is empty for the replicas that reported none.
 */
export interface Deployment {
  readonly baseUrl: string;
  readonly host: string;
  /** Its replicas' contacts, the most recent first. */
  readonly contacts: readonly BastionContact[];
  /** The time of its most recent contact. */
  readonly last: string;
}

type BastionContact = Bastion['contacts'][number];

/** The registration's contacts grouped by the base URL they report, by host; the replicas that reported none last. */
export function deployments(bastion: Pick<Bastion, 'contacts'>): Deployment[] {
  const groups = new Map<string, BastionContact[]>();
  for (const c of bastion.contacts) groups.set(c.baseUrl, [...(groups.get(c.baseUrl) ?? []), c]);
  return [...groups.entries()]
    .map(([baseUrl, contacts]) => {
      const sorted = [...contacts].sort((a, b) => Date.parse(b.contactAt) - Date.parse(a.contactAt));
      return { baseUrl, host: baseUrl === '' ? '' : hostOf(baseUrl), contacts: sorted, last: sorted[0]!.contactAt };
    })
    .sort((a, b) => (a.host === '') !== (b.host === '') ? (a.host === '' ? 1 : -1) : a.host.localeCompare(b.host));
}

/**
 * The hosts the card's head names (R-UI-240, Q-UI-1641): each deployment that reported a base URL and made contact within the last 24
 * hours, by host; none before the first contact. Neither a registration nor its declaration holds the Bastion's public address: the base
 * URL its replicas report is the one the main installation knows.
 */
export function headHosts(bastion: Pick<Bastion, 'contacts'>, now: number): string[] {
  return deployments(bastion)
    .filter((d) => d.host !== '' && now - Date.parse(d.last) <= DEPLOYMENT_HEARD_MS)
    .map((d) => d.host);
}

/**
 * What the section **Replicas** says (R-UI-240, Q-UI-1622, Q-UI-1643): `none` before a contact; `silent` when no replica made contact in
 * the last five minutes (`REPORT_FRESH_MS`), since the newest contact, a warning only while the registration is not disabled; otherwise
 * `current` with the replicas heard in the last five minutes, the newest contact and its version, the versions those replicas run (newest
 * contact's first, each once), the number of deployments they report, and the deployments heard within 24 hours but not in the last five
 * minutes beside a current one (`silentDeployments`, each a warning). A replica not heard beside current ones of its deployment is no
 * warning: every rollout leaves its old pods' contacts for 30 days. Different base URLs are no warning: they are a pair of deployments.
 */
export type ContactHealth =
  | { readonly kind: 'none' }
  | { readonly kind: 'silent'; readonly since: string; readonly warn: boolean }
  | {
      readonly kind: 'current';
      readonly count: number;
      readonly last: string;
      readonly release: string;
      readonly releases: readonly string[];
      readonly deployments: number;
      readonly silentDeployments: readonly { readonly host: string; readonly since: string }[];
    };

export function contactHealth(bastion: Pick<Bastion, 'contacts' | 'disabled'>, now: number): ContactHealth {
  const newest = newestContact(bastion.contacts);
  if (!newest) return { kind: 'none' };
  const current = bastion.contacts
    .filter((c) => now - Date.parse(c.contactAt) <= REPORT_FRESH_MS)
    .sort((a, b) => Date.parse(b.contactAt) - Date.parse(a.contactAt));
  if (current.length === 0) return { kind: 'silent', since: newest.contactAt, warn: !bastion.disabled };
  const releases = [...new Set(current.map((c) => c.release))];
  const silentDeployments = deployments(bastion)
    .filter((d) => d.host !== '')
    .filter((d) => {
      const age = now - Date.parse(d.last);
      return age > REPORT_FRESH_MS && age <= DEPLOYMENT_HEARD_MS;
    })
    .map((d) => ({ host: d.host, since: d.last }));
  const live = new Set(current.map((c) => c.baseUrl).filter((u) => u !== '')).size;
  return {
    kind: 'current',
    count: current.length,
    last: newest.contactAt,
    release: newest.release,
    releases,
    deployments: live,
    silentDeployments,
  };
}

/**
 * What the section **Tokens** says (R-UI-240, Q-UI-1623): `none` without a token; `expired` with the first token whose expiry lies
 * before now; `soon` with the token that expires first within `BASTION_TOKEN_WARN_DAYS`; otherwise `ok` with the earliest expiry, or
 * null where no token expires.
 */
export type TokenHealth =
  | { readonly kind: 'none' }
  | { readonly kind: 'expired'; readonly token: BastionTokenSummary }
  | { readonly kind: 'soon'; readonly token: BastionTokenSummary }
  | { readonly kind: 'ok'; readonly next: string | null };

export function tokenHealth(bastion: Pick<Bastion, 'tokens'>, now: number): TokenHealth {
  if (bastion.tokens.length === 0) return { kind: 'none' };
  const expired = bastion.tokens.find((t) => tokenExpiry(t, now) === 'expired');
  if (expired) return { kind: 'expired', token: expired };
  const expiring = bastion.tokens
    .filter((t) => t.expiresAt !== null)
    .sort((a, b) => Date.parse(a.expiresAt!) - Date.parse(b.expiresAt!));
  const first = expiring[0];
  if (first && tokenExpiry(first, now) === 'soon') return { kind: 'soon', token: first };
  return { kind: 'ok', next: first?.expiresAt ?? null };
}
