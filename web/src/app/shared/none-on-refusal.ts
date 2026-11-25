import { asFailure } from '../core/http/failure';
import { passes } from './error-notice';

/**
 * Whether the failure of a read the page works without may pass, so that the page shows it and reads it again (R-UI-216): no answer,
 * a gateway's, a server's. A refusal (a reader who may not ask) does not, and neither does `503 unavailable`, which a replica answers
 * for a service it has not wired (the scanning on this replica, Q-VULN-25): that is the answer "none here", not a failure.
 */
export function failureMayPass(e: unknown): boolean {
  const failure = asFailure(e);
  return passes(failure) && failure.code !== 'unavailable';
}

/**
 * A read the page works without (a version's or a package's grades): a refusal leaves the page as it is without it. A failure that
 * may pass is thrown, so the page shows it as what it is and reads it again (R-UI-216): swallowed, the page hid the tab Vulnerabilities,
 * the grades and the banner "Blocked in" until a reload, as if the version were not scanned.
 */
export async function noneOnRefusal<T>(read: Promise<T>): Promise<T | null> {
  try {
    return await read;
  } catch (e) {
    if (failureMayPass(e)) throw e;
    return null;
  }
}
