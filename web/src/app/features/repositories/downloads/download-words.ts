import {
  RemoteDownload,
  RemoteDownloadFailure,
  RemoteDownloadFailureClass,
  RemoteDownloadFailureStep,
  RemoteDownloadState,
} from '../../../api/remote-downloads-api';

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
