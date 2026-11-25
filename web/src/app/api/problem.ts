/** One field-level problem; `pointer` is an RFC 6901 pointer into the request body (R-API-009). */
export interface FieldProblem {
  readonly pointer: string;
  readonly code: string;
  readonly detail?: string;
}

/**
 * The `application/problem+json` body of every management API error (RFC 9457, R-API-009).
 * Resource-specific extensions are reachable through the index signature.
 */
export interface Problem {
  readonly type?: string;
  readonly title?: string;
  readonly status?: number;
  readonly detail?: string;
  readonly instance?: string;
  readonly code: string;
  readonly requestId?: string;
  readonly errors?: readonly FieldProblem[];
  readonly currentEtag?: string;
  readonly [extension: string]: unknown;
}

/** True for an object that carries the mandatory `code` member of a Quader problem. */
export function isProblem(value: unknown): value is Problem {
  return (
    typeof value === 'object' &&
    value !== null &&
    typeof (value as { code?: unknown }).code === 'string'
  );
}

/** The call a failure belongs to: its method and its path with the query, a credential's value left out (R-UI-216). */
export interface FailedCall {
  readonly method: string;
  readonly path: string;
}

/**
 * What the browser knows of a failed answer beyond the problem document (R-UI-216, Q-UI-1180): each member is there only where the
 * answer carried it. Built by `apiErrorOf` (core/http/failure.ts) for every call through the interceptors and the upload's own
 * client.
 */
export interface FailedAnswer {
  /** The call; null for a failure of the page itself (`fault`) and for an error a screen made up. */
  readonly call: FailedCall | null;
  /** When the failure was seen. */
  readonly at: Date;
  /** The `Content-Type` of the answer. */
  readonly contentType: string | null;
  /**
   * The body of an answer that is no problem document, as text, at most `BODY_LIMIT` characters (a proxy's error page, a text a
   * gateway wrote). Never the body of a successful answer that could not be read: that may carry a secret (R-UI-043, Q-UI-1186).
   */
  readonly body: string | null;
  /** The body's whole length in characters where `body` was cut; null where it is whole. */
  readonly bodyLength: number | null;
  /** The headers of the answer that a script may read, in the order the browser lists them (Q-UI-1186). */
  readonly headers: readonly (readonly [string, string])[];
  /** A successful status whose body could not be read as the call expected. */
  readonly unreadable: boolean;
  /** A failure of the page's own code, not of an answer: its name and message ("TypeError: …"). */
  readonly fault: string | null;
  /** The failures of this call in a row, this one included: 1 for a first failure (`CallFailures` of core/http/failure.ts). */
  readonly inRow: number;
}

/** The longest body of a failed answer a notice keeps: enough for a proxy's error page, and a bound for a large one (R-UI-216). */
export const BODY_LIMIT = 4_000;

const NO_ANSWER: Omit<FailedAnswer, 'at'> = {
  call: null,
  contentType: null,
  body: null,
  bodyLength: null,
  headers: [],
  unreadable: false,
  fault: null,
  inRow: 1,
};

/**
 * Failure of an API call after the interceptors have run. `problem` is null when the answer was
 * not a problem document (a proxy error page, a network failure with `status` 0). `answer` holds what else is known of it: the
 * call, the time, the body and the headers of an answer that is no problem document (R-UI-216).
 */
export class ApiError extends Error {
  /**
   * The request was sent twice (R-UI-042). A 412 on a retried PUT or PATCH may mean that the
   * first attempt did commit: compare the stored resource with the draft before opening the
   * conflict dialog.
   */
  afterRetry = false;
  readonly answer: FailedAnswer;

  constructor(
    readonly status: number,
    readonly problem: Problem | null,
    readonly requestId: string | null,
    /** `Retry-After` in whole seconds, when the answer carried one. */
    readonly retryAfterSeconds: number | null = null,
    answer: Partial<FailedAnswer> = {},
  ) {
    super(problem?.detail ?? problem?.title ?? statusLine(status));
    this.name = 'ApiError';
    this.answer = { ...NO_ANSWER, at: new Date(), ...answer };
  }

  /** A read: a GET or a HEAD, which may be sent again at any time (R-UI-216). */
  get isRead(): boolean {
    const m = this.answer.call?.method;
    return m === 'GET' || m === 'HEAD';
  }

  /** The stable error code, or null without a problem document. */
  get code(): string | null {
    return this.problem?.code ?? null;
  }

  /** The `ETag` the server holds now, from a 412 (R-API-012). */
  get currentEtag(): string | null {
    return this.problem?.currentEtag ?? null;
  }

  /** Field problems by JSON pointer; the first problem of a pointer wins. */
  get fieldErrors(): ReadonlyMap<string, FieldProblem> {
    const map = new Map<string, FieldProblem>();
    for (const e of this.problem?.errors ?? []) {
      if (!map.has(e.pointer)) map.set(e.pointer, e);
    }
    return map;
  }
}

export function isApiError(value: unknown): value is ApiError {
  return value instanceof ApiError;
}

/** "HTTP 502 Bad Gateway": the status as a line of a log, for `Error.message`. */
function statusLine(status: number): string {
  const reason = reasonPhrase(status);
  return reason ? `HTTP ${status} ${reason}` : `HTTP ${status}`;
}

/**
 * The reason phrase of a status (RFC 9110 section 15, and the statuses a gateway answers): "Bad Gateway" for 502. HTTP/2 carries
 * none, so the browser's `statusText` is empty there, and the notice names the status with this (R-UI-216). Empty for a status it
 * does not know.
 */
export function reasonPhrase(status: number): string {
  return REASONS[status] ?? '';
}

const REASONS: Readonly<Record<number, string>> = {
  200: 'OK',
  201: 'Created',
  202: 'Accepted',
  204: 'No Content',
  301: 'Moved Permanently',
  302: 'Found',
  303: 'See Other',
  304: 'Not Modified',
  307: 'Temporary Redirect',
  308: 'Permanent Redirect',
  400: 'Bad Request',
  401: 'Unauthorized',
  403: 'Forbidden',
  404: 'Not Found',
  405: 'Method Not Allowed',
  406: 'Not Acceptable',
  408: 'Request Timeout',
  409: 'Conflict',
  410: 'Gone',
  411: 'Length Required',
  412: 'Precondition Failed',
  413: 'Content Too Large',
  414: 'URI Too Long',
  415: 'Unsupported Media Type',
  416: 'Range Not Satisfiable',
  421: 'Misdirected Request',
  422: 'Unprocessable Content',
  425: 'Too Early',
  428: 'Precondition Required',
  429: 'Too Many Requests',
  431: 'Request Header Fields Too Large',
  451: 'Unavailable For Legal Reasons',
  500: 'Internal Server Error',
  501: 'Not Implemented',
  502: 'Bad Gateway',
  503: 'Service Unavailable',
  504: 'Gateway Timeout',
  505: 'HTTP Version Not Supported',
  507: 'Insufficient Storage',
};
