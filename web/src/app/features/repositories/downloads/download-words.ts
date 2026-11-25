import {
  RemoteDownload,
  RemoteDownloadFailure,
  RemoteDownloadOrigin,
  RemoteDownloadFailureClass,
  RemoteDownloadFailureStep,
  RemoteDownloadState,
} from '../../../api/remote-downloads-api';

/** A bulk's range of versions as the API answers it in its bounds (R-CUR-010, Q-CUR-193). */
export interface VersionRange {
  readonly fromVersion: string | null;
  readonly toVersion: string | null;
  readonly toInclusive: boolean;
}

/**
 * What a bulk of a package was asked for by version, in words (Q-CUR-193): "From 5.0.0 up to 5.4.7", "From 5.0.0, below 6.0.0",
 * "Up to 5.4.7", "Below 6.0.0", "From 5.0.0"; null for a bulk without a range.
 */
export function versionRangeText(r: VersionRange): string | null {
  const from = r.fromVersion;
  const to = r.toVersion;
  if (from && to) {
    return r.toInclusive
      ? $localize`:@@downloads.range.from-up-to:From ${from}:from: up to ${to}:to:`
      : $localize`:@@downloads.range.from-below:From ${from}:from:, below ${to}:to:`;
  }
  if (from) return $localize`:@@downloads.range.from:From ${from}:from:`;
  if (to) return r.toInclusive ? $localize`:@@downloads.range.up-to:Up to ${to}:to:` : $localize`:@@downloads.range.below:Below ${to}:to:`;
  return null;
}

/** A row's state as the word of its badge (Q-UI-560, Q-UI-564). */
export function downloadStateText(state: RemoteDownloadState): string {
  switch (state) {
    case 'waiting':
      return $localize`:@@downloads.state.waiting:Waiting`;
    case 'running':
      return $localize`:@@downloads.state.running:Running`;
    case 'done':
      return $localize`:@@downloads.state.done:Done`;
    case 'failed':
      return $localize`:@@downloads.state.failed:Failed`;
    case 'cancelled':
      return $localize`:@@downloads.state.cancelled:Cancelled`;
  }
}

/**
 * Who or what put a row into the queue, in the queue's words (R-CUR-018): a person who is not named reads as **A person**. The
 * downloads panel of the top bar uses the same words for the queue's origins (api-and-ui.md R-UI-211).
 */
export function queueOriginText(origin: RemoteDownloadOrigin): string {
  switch (origin) {
    case 'person':
      return $localize`:@@downloads.origin.person:A person`;
    case 'request':
      return $localize`:@@downloads.origin.request:Request`;
    case 'allow':
      return $localize`:@@downloads.origin.allow:Allow`;
    case 'bulk':
      return $localize`:@@downloads.origin.bulk:Bulk`;
    case 'sync':
      return $localize`:@@downloads.origin.sync:Sync`;
    case 'probe':
      return $localize`:@@downloads.origin.probe:Probe`;
    case 'client':
      return $localize`:@@downloads.origin.client:Client`;
  }
}

/**
 * What a done row's outcome says beside its state (R-CUR-011): nothing for one that completed; why the others stored less than
 * they named.
 */
export function downloadOutcomeText(outcome: string | null): string {
  switch (outcome) {
    case 'bounded':
      return $localize`:@@downloads.outcome.bounded:Stopped at a bound`;
    case 'refused':
      return $localize`:@@downloads.outcome.refused:Every file was refused by a gate`;
    case 'skipped':
      return $localize`:@@downloads.outcome.skipped:Skipped: the repository is gone, disabled, offline or no longer remote`;
    default:
      return '';
  }
}

/** The class of a failure in words (R-CUR-011): the first part of its one line, and the options of the filter **Failure**. */
export function failureClassText(c: RemoteDownloadFailureClass): string {
  switch (c) {
    case 'upstream_not_found':
      return $localize`:@@downloads.class.upstream-not-found:Not found upstream`;
    case 'upstream_auth':
      return $localize`:@@downloads.class.upstream-auth:Refused by the upstream's authentication`;
    case 'upstream_rate_limited':
      return $localize`:@@downloads.class.upstream-rate-limited:Rate limited by the upstream`;
    case 'upstream_error':
      return $localize`:@@downloads.class.upstream-error:Upstream error`;
    case 'upstream_unexpected':
      return $localize`:@@downloads.class.upstream-unexpected:Unexpected answer of the upstream`;
    case 'network':
      return $localize`:@@downloads.class.network:Network`;
    case 'tls':
      return $localize`:@@downloads.class.tls:TLS`;
    case 'egress_denied':
      return $localize`:@@downloads.class.egress-denied:Address not allowed by this installation`;
    case 'integrity':
      return $localize`:@@downloads.class.integrity:Integrity check failed`;
    case 'content_refused':
      return $localize`:@@downloads.class.content-refused:Content refused`;
    case 'gate_refused':
      return $localize`:@@downloads.class.gate-refused:Refused by a gate`;
    case 'circuit_open':
      return $localize`:@@downloads.class.circuit-open:Upstream assumed offline`;
    case 'bound':
      return $localize`:@@downloads.class.bound:Bound reached`;
    case 'storage':
      return $localize`:@@downloads.class.storage:Storage`;
    case 'internal':
      return $localize`:@@downloads.class.internal:Internal error`;
  }
}

/** The step a failure happened in, in words (R-CUR-011); the value itself stands beside it for a script's filter. */
export function failureStepText(step: RemoteDownloadFailureStep): string {
  switch (step) {
    case 'plan':
      return $localize`:@@downloads.step.plan:Reading the documents that name the files`;
    case 'resolve':
      return $localize`:@@downloads.step.resolve:Resolving the host name`;
    case 'connect':
      return $localize`:@@downloads.step.connect:Connecting`;
    case 'tls':
      return $localize`:@@downloads.step.tls:TLS handshake or certificate check`;
    case 'request':
      return $localize`:@@downloads.step.request:Sending the request`;
    case 'status':
      return $localize`:@@downloads.step.status:The answer's status`;
    case 'body':
      return $localize`:@@downloads.step.body:Reading the body`;
    case 'verify':
      return $localize`:@@downloads.step.verify:Checking digest, size and media type`;
    case 'gate':
      return $localize`:@@downloads.step.gate:Vulnerability check before the download`;
    case 'store':
      return $localize`:@@downloads.step.store:Storing`;
  }
}

/**
 * The host and port of a failure's address, the part a person recognises in a line; the address as it is where it is no URL.
 */
export function failureHost(address: string | null): string | null {
  if (!address) return null;
  try {
    const u = new URL(address);
    return u.port ? `${u.hostname}:${u.port}` : u.hostname;
  } catch {
    return address;
  }
}

/**
 * A failure in one line (R-CUR-018): its class, then the upstream's status where an answer came, else the engine's code, then the
 * host it asked: "Upstream error · HTTP 503 · registry.npmjs.org", "TLS · tls_untrusted · registry.example.com:443". The parts
 * are separate so the template sets the code apart as code.
 */
export interface FailureLine {
  readonly label: string;
  /** "HTTP 503", or null where the failure has no status and the code stands in its place. */
  readonly status: string | null;
  readonly code: string;
  readonly host: string | null;
}

export function failureLine(f: RemoteDownloadFailure): FailureLine {
  return {
    label: failureClassText(f.class),
    status: f.status ? `HTTP ${f.status}` : null,
    code: f.code,
    host: failureHost(f.address ?? null),
  };
}

/** The subject of a row: the name and the version, or the name alone for a file of no version (an OCI blob). */
export function downloadSubject(row: Pick<RemoteDownload, 'name' | 'version'>): string {
  return row.version ? `${row.name} ${row.version}` : row.name;
}

/** What a failure's sentence reads of it: its class and code, the hosts it names and the wait the upstream asked for. */
export type FailureFacts = Pick<RemoteDownloadFailure, 'class' | 'code' | 'address' | 'target' | 'retryAfter'>;

/**
 * A failure in the screen's language (R-CUR-011, R-CUR-018, Q-CUR-169): what failed and what helps, written here from the failure's
 * code and its members, never from the server's `detail`, which is an English sentence for a script and an administrator. The host
 * is the address the upstream named and Quader refused (`target`), else the address asked. A code this list does not know reads as
 * its class.
 */
export function failureSentence(f: FailureFacts): string {
  const host = failureHost(f.target ?? f.address ?? null);
  return (
    answerSentence(f.code, host) ??
    reachSentence(f.code) ??
    contentSentence(f.code) ??
    limitSentence(f.code) ??
    serviceSentence(f.code) ??
    bulkSentence(f.code) ??
    classSentence(f.class, host, f.retryAfter ?? null)
  );
}

/** The upstream answered, but not with the file: a redirect, a host it named, a document Quader cannot use. */
export function answerSentence(code: string, host: string | null): string | null {
  switch (code) {
    case 'upstream_unexpected_status':
      return $localize`:@@downloads.why.unexpected-status:The upstream answered with a status Quader does not expect here. Check the upstream's address in the repository's settings; the start of its answer may say more.`;
    case 'too_many_redirects':
      return $localize`:@@downloads.why.too-many-redirects:The upstream redirected the request more often than this installation allows. Check the upstream's address in the repository's settings.`;
    case 'insecure_redirect':
      return host
        ? $localize`:@@downloads.why.insecure-redirect-host:The upstream sent the request from https to plain http on ${host}:host:, and Quader never leaves https. Ask the upstream's operator for an https address.`
        : $localize`:@@downloads.why.insecure-redirect:The upstream sent the request from https to plain http, and Quader never leaves https. Ask the upstream's operator for an https address.`;
    case 'foreign_host_denied':
      return host
        ? $localize`:@@downloads.why.foreign-host-host:The upstream points to ${host}:host:, which is neither the upstream nor one of the repository's further hosts, so Quader sent no request there. If you trust this host, add it to the repository's further hosts.`
        : $localize`:@@downloads.why.foreign-host:The upstream points to a host that is neither the upstream nor one of the repository's further hosts, so Quader sent no request there. If you trust this host, add it to the repository's further hosts.`;
    case 'upstream_package_mismatch':
      return $localize`:@@downloads.why.package-mismatch:The upstream answered with another package than the one asked for; nothing was stored.`;
    case 'npm_invalid_upstream_document':
    case 'nuget_invalid_upstream_document':
      return $localize`:@@downloads.why.invalid-document:The upstream's document of the package is not valid; nothing was stored. A later attempt succeeds once the upstream corrects it.`;
    default:
      return null;
  }
}

/** The way to the upstream: the network a replica sends from, the credential, the time an answer took. */
export function reachSentence(code: string): string | null {
  switch (code) {
    case 'upstream_timeout':
      return $localize`:@@downloads.why.timeout:The upstream did not answer in time. A later attempt may succeed.`;
    case 'source_network_unavailable':
      return $localize`:@@downloads.why.source-network:This replica has no address in the network the repository sends its requests from; another replica may have one. An administrator checks the repository's source network.`;
    case 'upstream_secret_unavailable':
      return $localize`:@@downloads.why.secret:The repository's credential for the upstream cannot be read on this replica. A Maintainer or Owner enters it again in the repository's settings.`;
    default:
      return null;
  }
}

/** What came does not hold: a digest, a size, a media type. */
export function contentSentence(code: string): string | null {
  switch (code) {
    case 'upstream_integrity_mismatch':
      return $localize`:@@downloads.why.integrity:The file does not match the digest the upstream announced, so it was not stored.`;
    case 'upstream_digest_missing':
      return $localize`:@@downloads.why.digest-missing:The upstream announces no digest for the file and the repository requires one, so it was not stored.`;
    case 'oci_digest_invalid':
      return $localize`:@@downloads.why.oci-digest:The content does not match its digest, so it was not stored.`;
    case 'oci_size_invalid':
      return $localize`:@@downloads.why.oci-size:The image exceeds a size limit of this installation, so it was not stored.`;
    case 'upstream_content_type_blocked':
      return $localize`:@@downloads.why.content-type:The upstream answered with a page in place of the file, usually an error page or the sign-in page of a network, so nothing was stored.`;
    default:
      return null;
  }
}

/** A limit of the repository, of the run or of the vulnerability gate. */
export function limitSentence(code: string): string | null {
  switch (code) {
    case 'upstream_item_too_large':
    case 'payload_too_large':
    case 'npm_too_large':
      return $localize`:@@downloads.why.too-large:The file is larger than the repository's limit allows, so it was not stored. A Maintainer or Owner can raise the limit in the repository's settings.`;
    case 'download_timeout':
      return $localize`:@@downloads.why.download-timeout:The download took longer than its time limit. What it stored stays, and the next attempt goes on from there.`;
    case 'vulnerability_blocked':
      return $localize`:@@downloads.why.vulnerability-blocked:A vulnerability finding blocks this version in this repository, so it was not fetched.`;
    case 'vulnerability_data_missing':
      return $localize`:@@downloads.why.vulnerability-data:The vulnerability data of this ecosystem is not complete and the repository blocks what it cannot check, so the version was not fetched.`;
    case 'populate_not_permitted':
      return $localize`:@@downloads.why.populate:Whoever started the download may read the repository but not bring new content into its cache.`;
    default:
      return null;
  }
}

/** Quader's own state: offline, a full spool, a pre-empted run, the database. */
export function serviceSentence(code: string): string | null {
  switch (code) {
    case 'not_cached_offline':
      return $localize`:@@downloads.why.offline:The repository is offline and the file is not in its cache, so Quader sent no request.`;
    case 'spool_busy':
      return $localize`:@@downloads.why.spool-busy:The space for downloads in progress is full; the next attempt waits for room.`;
    case 'preempted':
      return $localize`:@@downloads.why.preempted:A client's fetch took the repository's place for downloads ahead; the download goes on with its next attempt.`;
    case 'database_timeout':
      return $localize`:@@downloads.why.database:The database answered too slowly. A later attempt may succeed.`;
    default:
      return null;
  }
}

/** A bulk's own ends: an upstream without sub-sections, a server without the lists, the bulk's bounds. */
export function bulkSentence(code: string): string | null {
  switch (code) {
    case 'catalogue_not_offered':
      return $localize`:@@downloads.why.no-sections:The upstream of this repository lists no sub-sections, so a sub-section cannot be downloaded. Download its packages one by one.`;
    case 'catalogue_unavailable':
      return $localize`:@@downloads.why.no-catalogue:This server cannot read the upstream's lists for a bulk download. Start it again; if it fails again, an administrator finds the cause in the server's log.`;
    case 'bulk_max_versions':
      return $localize`:@@downloads.why.bulk-versions:The bulk download queued as many versions as its limit allows and adds no more.`;
    case 'bulk_max_bytes':
      return $localize`:@@downloads.why.bulk-bytes:The announced sizes of the queued versions reach the bulk download's size limit; it adds no more.`;
    case 'bulk_stored_bytes':
      return $localize`:@@downloads.why.bulk-stored:The versions of the bulk download stored as much as its size limit allows; it adds no more.`;
    default:
      return null;
  }
}

/** The sentence of a class (R-CUR-011): for a code without a sentence of its own, and for one this screen does not know. */
export function classSentence(c: RemoteDownloadFailureClass, host: string | null, retryAfter: number | null): string {
  switch (c) {
    case 'upstream_not_found':
      return $localize`:@@downloads.why.class.not-found:The upstream does not have this file. Check the package's name and version.`;
    case 'upstream_auth':
      return $localize`:@@downloads.why.class.auth:The upstream refused the repository's credentials. A Maintainer or Owner checks them in the repository's settings.`;
    case 'upstream_rate_limited':
      return retryAfter
        ? $localize`:@@downloads.why.class.rate-limited-wait:The upstream limits how often Quader may ask it and asked to wait ${retryAfter}:seconds: s; the next attempt waits that long.`
        : $localize`:@@downloads.why.class.rate-limited:The upstream limits how often Quader may ask it; the next attempt waits.`;
    case 'upstream_error':
      return $localize`:@@downloads.why.class.upstream-error:The upstream answered with an error of its own. A later attempt may succeed.`;
    case 'upstream_unexpected':
      return $localize`:@@downloads.why.class.unexpected:The upstream answered in a way Quader does not expect here, so nothing was stored.`;
    case 'network':
      return $localize`:@@downloads.why.class.network:Quader could not reach the upstream. Check the upstream's address in the repository's settings; an administrator checks the network and the proxy.`;
    case 'tls':
      return host
        ? $localize`:@@downloads.why.class.tls-host:This installation does not trust the certificate of ${host}:host:. An administrator adds the certificate authority that signed it.`
        : $localize`:@@downloads.why.class.tls:This installation does not trust the upstream's certificate. An administrator adds the certificate authority that signed it.`;
    case 'egress_denied':
      return host
        ? $localize`:@@downloads.why.class.egress-host:This installation does not allow requests to ${host}:host:. An administrator decides which addresses Quader may reach.`
        : $localize`:@@downloads.why.class.egress:This installation does not allow requests to the upstream's address. An administrator decides which addresses Quader may reach.`;
    case 'integrity':
      return $localize`:@@downloads.why.class.integrity:The file failed its integrity check, so it was not stored.`;
    case 'content_refused':
      return $localize`:@@downloads.why.class.content-refused:The file has a structure Quader does not read, so it was not stored.`;
    case 'gate_refused':
      return $localize`:@@downloads.why.class.gate:A check of this repository refused the file before it was fetched.`;
    case 'circuit_open':
      return $localize`:@@downloads.why.class.circuit:The upstream failed several times in a row and counts as unavailable for a while; the next attempt waits for it.`;
    case 'bound':
      return $localize`:@@downloads.why.class.bound:The download reached a limit. What it stored stays.`;
    case 'storage':
      return $localize`:@@downloads.why.class.storage:The file could not be stored. A later attempt may succeed.`;
    case 'internal':
      return $localize`:@@downloads.why.class.internal:The download failed inside Quader. A later attempt may succeed; if not, an administrator finds the cause in the server's log.`;
  }
}

/**
 * The sentence of a repository's interval of syncs (R-CUR-027, Q-CUR-242), in words a person reads: "every hour", "every 6 hours",
 * "every day". A sentence of its own per unit, so that no number word is put into another language's sentence (Q-UI-1219).
 */
export function syncIntervalSentence(seconds: number): string {
  const days = seconds / 86_400;
  const hours = seconds / 3_600;
  const minutes = Math.max(1, Math.round(seconds / 60));
  if (Number.isInteger(days)) {
    return days === 1
      ? $localize`:@@sync.interval.day:Quader downloads what is new every day, the interval of this repository.`
      : $localize`:@@sync.interval.days:Quader downloads what is new every ${days}:count: days, the interval of this repository.`;
  }
  if (Number.isInteger(hours)) {
    return hours === 1
      ? $localize`:@@sync.interval.hour:Quader downloads what is new every hour, the interval of this repository.`
      : $localize`:@@sync.interval.hours:Quader downloads what is new every ${hours}:count: hours, the interval of this repository.`;
  }
  return $localize`:@@sync.interval.minutes:Quader downloads what is new every ${minutes}:count: minutes, the interval of this repository.`;
}
