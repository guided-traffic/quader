import { HttpErrorResponse, HttpHeaders, HttpRequest } from '@angular/common/http';
import { Injectable } from '@angular/core';
import { ApiError, BODY_LIMIT, FailedAnswer, FailedCall, isProblem, Problem } from '../../api/problem';

/**
 * What a failed call tells the person (R-UI-216, Q-UI-1180): the interceptor's `ApiError` carries the call, the time, the body and
 * the headers of an answer that is no problem document, and the failures of the call in a row. Nothing that carries a credential is
 * kept (Q-UI-1186): no request header, no request body, no cookie, and a query parameter or a header's address that carries a token
 * or a signature is named without its value.
 */

/**
 * A query parameter whose value is a credential: a token, a signature, a secret, a password, a key of a signed address. No call of
 * the UI carries one today (verified 2026-11-25: the sign-in posts its password in a body, a signed download link is a navigation,
 * not a call); the rule holds for the one that will.
 */
const CREDENTIAL_PARAM = /token|signature|secret|password|passwd|credential|^sig$|^x-amz-|^x-goog-|^api_?key$/i;

/** What stands in place of a credential's value. */
export const REDACTED = '[redacted]';

/** The headers of an answer that are never shown or copied: they set or carry a credential (a script cannot read most of them). */
const CREDENTIAL_HEADERS = new Set(['set-cookie', 'set-cookie2', 'authorization', 'proxy-authorization', 'cookie']);

/** The headers whose value is an address, whose query is read by `redactUrl`. */
const ADDRESS_HEADERS = new Set(['location', 'content-location', 'link', 'refresh']);

/**
 * An address with each query parameter that carries a credential named without its value: `…?token=[redacted]&page=2`. A relative
 * path stays relative; an address that cannot be read is left as it is.
 */
export function redactUrl(url: string): string {
  const q = url.indexOf('?');
  if (q < 0) return url;
  const hash = url.indexOf('#', q);
  const query = url.slice(q + 1, hash < 0 ? undefined : hash);
  const parts = query.split('&').map((part) => {
    const eq = part.indexOf('=');
    const name = eq < 0 ? part : part.slice(0, eq);
    let decoded = name;
    try {
      decoded = decodeURIComponent(name.replace(/\+/g, ' '));
    } catch {
      // A name that does not decode is judged as it is written.
    }
    return eq >= 0 && CREDENTIAL_PARAM.test(decoded) ? `${name}=${REDACTED}` : part;
  });
  return `${url.slice(0, q)}?${parts.join('&')}${hash < 0 ? '' : url.slice(hash)}`;
}

/** The call of a request as a notice names it: its method and its path with the query, a credential's value left out. */
export function callOf(req: Pick<HttpRequest<unknown>, 'method' | 'urlWithParams'>): FailedCall {
  return { method: req.method, path: redactUrl(req.urlWithParams) };
}

/** The headers of an answer a notice shows: every one a script may read, but those that carry a credential (Q-UI-1186). */
export function shownHeaders(headers: HttpHeaders | null | undefined): readonly (readonly [string, string])[] {
  if (!headers) return [];
  const out: (readonly [string, string])[] = [];
  for (const name of headers.keys()) {
    const lower = name.toLowerCase();
    if (CREDENTIAL_HEADERS.has(lower)) continue;
    const value = headers.getAll(name)?.join(', ') ?? '';
    out.push([lower, ADDRESS_HEADERS.has(lower) ? redactUrl(value) : value]);
  }
  return out;
}

/** `Retry-After` in whole seconds, or null without one (a date is not read). */
export function retryAfterSeconds(headers: HttpHeaders | null | undefined): number | null {
  const raw = headers?.get('Retry-After');
  if (!raw) return null;
  const n = Number.parseInt(raw, 10);
  return Number.isFinite(n) && n >= 0 ? n : null;
}

/** The body of a failed answer as text, or null where there is none (a network failure's progress event). */
async function bodyText(body: unknown): Promise<string | null> {
  if (body === null || body === undefined) return null;
  if (typeof body === 'string') return body;
  if (typeof Blob !== 'undefined' && body instanceof Blob) return body.text();
  if (body instanceof ArrayBuffer) return new TextDecoder().decode(body);
  if (typeof ProgressEvent !== 'undefined' && body instanceof ProgressEvent) return null;
  if (typeof body === 'object') {
    try {
      return JSON.stringify(body, null, 2);
    } catch {
      return null;
    }
  }
  return String(body);
}

/** A problem document in the text of a body, or null. */
function problemIn(text: string | null): Problem | null {
  if (!text) return null;
  try {
    const parsed: unknown = JSON.parse(text);
    return isProblem(parsed) ? parsed : null;
  } catch {
    return null;
  }
}

/**
 * The problem document of a failed answer, read synchronously: a JSON body, or a text body that holds one (a request that reads
 * text, a CI snippet). A body of another kind (a Blob) is read by `apiErrorOf`.
 */
export function problemOf(err: HttpErrorResponse): Problem | null {
  const body: unknown = err.error;
  if (isProblem(body)) return body;
  return typeof body === 'string' ? problemIn(body) : null;
}

/** A successful status whose body could not be read: Angular reports it with the parser's error and the text beside it. */
function unreadableOf(err: HttpErrorResponse): { readonly error: unknown } | null {
  if (err.status < 200 || err.status >= 300) return null;
  const e = err.error as { error?: unknown } | null;
  return e && typeof e === 'object' && 'error' in e ? (e as { error: unknown }) : { error: null };
}

/** "TypeError: Cannot read properties of undefined": the name and the message of an error a page's code threw. */
export function faultText(e: unknown): string {
  if (e instanceof Error) return e.message ? `${e.name}: ${e.message}` : e.name;
  try {
    return typeof e === 'string' ? e : JSON.stringify(e) ?? String(e);
  } catch {
    return String(e);
  }
}

export interface FailureContext {
  /** The request was sent twice (the one automatic retry of R-UI-042). */
  readonly retried: boolean;
  /** The failures of the call in a row, this one included. */
  readonly inRow: number;
}

/**
 * The `ApiError` of a failed answer (R-UI-042, R-UI-216): its status, its problem document, the request ID (the document's, else the
 * answer's `X-Request-Id`), `Retry-After`, and what else the answer told: the call, the time, the content type, the headers, and the
 * body of an answer that is no problem document, cut at `BODY_LIMIT` characters. A body the browser holds as a Blob or a buffer is
 * read here, so a problem document of a download is found too.
 */
export async function apiErrorOf(
  req: Pick<HttpRequest<unknown>, 'method' | 'urlWithParams'>,
  err: HttpErrorResponse,
  context: FailureContext,
): Promise<ApiError> {
  const unreadable = unreadableOf(err);
  let problem = unreadable ? null : problemOf(err);
  let body: string | null = null;
  if (!unreadable && !problem) {
    body = await bodyText(err.error).catch(() => null);
    problem = problemIn(body);
    if (problem) body = null;
  }
  const whole = body?.length ?? 0;
  const answer: Partial<FailedAnswer> = {
    call: callOf(req),
    contentType: err.headers?.get('Content-Type') ?? null,
    body: body ? body.slice(0, BODY_LIMIT) : null,
    bodyLength: whole > BODY_LIMIT ? whole : null,
    headers: shownHeaders(err.headers),
    unreadable: unreadable !== null,
    fault: unreadable?.error ? faultText(unreadable.error) : null,
    inRow: context.inRow,
  };
  const error = new ApiError(
    err.status,
    problem,
    problem?.requestId ?? err.headers?.get('X-Request-Id') ?? null,
    retryAfterSeconds(err.headers),
    answer,
  );
  error.afterRetry = context.retried;
  return error;
}

/**
 * A failure as an `ApiError`: itself, or the failure of the page's own code (a loader that threw), which has no answer and no call
 * and says so (R-UI-216). It used to become `ApiError(0)`, which said that the server could not be reached.
 */
export function asFailure(e: unknown): ApiError {
  if (e instanceof ApiError) return e;
  return new ApiError(0, null, null, null, { fault: faultText(e) });
}

/** The longest the failures of calls are remembered: a bound for a page that fails at many addresses. */
const REMEMBERED_CALLS = 200;

/**
 * The failures of each call in a row, for the notice's back-off (R-UI-216): a call that fails again waits longer before the notice
 * reads it again by itself. An answer of the call ends its row.
 */
@Injectable({ providedIn: 'root' })
export class CallFailures {
  private readonly rows = new Map<string, number>();

  /** Counts a failure of the call and answers its failures in a row. */
  failed(call: FailedCall): number {
    const key = `${call.method} ${call.path}`;
    const n = (this.rows.get(key) ?? 0) + 1;
    this.rows.delete(key);
    this.rows.set(key, n);
    if (this.rows.size > REMEMBERED_CALLS) this.rows.delete(this.rows.keys().next().value as string);
    return n;
  }

  /** The call answered: its row ends. */
  answered(call: FailedCall): void {
    if (this.rows.size > 0) this.rows.delete(`${call.method} ${call.path}`);
  }
}
