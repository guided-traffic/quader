import { HttpClient, HttpErrorResponse, HttpEvent, HttpInterceptorFn, HttpParams, HttpResponse } from '@angular/common/http';
import { Injectable, inject } from '@angular/core';
import { Observable, catchError, from, switchMap, tap, throwError, timer } from 'rxjs';
import { firstValueFrom } from 'rxjs';
import { Page, PageFetcher } from '../../app/api/pagination';
import { CallFailures, apiErrorOf, callOf, retryAfterSeconds } from '../../app/core/http/failure';

/**
 * Where the Bastion's application reads (bastion.md R-BAS-118): a relative path of the Bastion's own host, the main installation's JSON
 * shapes below it. The bundle holds no other address and no environment file (W-35's B.13).
 */
export const DATA_ROOT = '/ui/data/v1/';

/** Query parameter values; arrays repeat the parameter, undefined is left out. */
export type DataParams = Readonly<Record<string, string | number | boolean | readonly string[] | undefined>>;

function toParams(params: DataParams | undefined): HttpParams {
  let out = new HttpParams();
  for (const [name, value] of Object.entries(params ?? {})) {
    if (value === undefined) continue;
    if (typeof value === 'object') {
      for (const v of value) out = out.append(name, v);
    } else {
      out = out.append(name, String(value));
    }
  }
  return out;
}

/**
 * One segment of a document's path, escaped as the catalogue's grammar spells it (R-BAS-103): every byte but the unreserved characters
 * as `%XX`. The Bastion writes a path in that spelling itself (R-BAS-118); escaping it here keeps the address a person copies equal to
 * the one the server reads.
 */
export function seg(value: string): string {
  return encodeURIComponent(value).replace(/[!'()*]/g, (c) => `%${c.charCodeAt(0).toString(16).toUpperCase()}`);
}

/**
 * The read client of the Bastion's application: GET only, JSON only, below {@link DATA_ROOT}. It has no method that writes, no
 * session and no credential: the Bastion's data are the public catalogue (R-UI-228).
 */
@Injectable({ providedIn: 'root' })
export class BastionData {
  private readonly http = inject(HttpClient);

  /** The answer of a path below the data root. */
  async get<T>(path: string, params?: DataParams): Promise<T> {
    const res = await firstValueFrom(
      this.http.get<T>(DATA_ROOT + path, { params: toParams(params), observe: 'response', headers: { Accept: 'application/json' } }),
    );
    return res.body as T;
  }

  /**
   * A page fetcher of a list in the envelope of R-API-010. A page asks at most {@link BASTION_PAGE_LIMIT}: a view that walks every page
   * with the main installation's 500 (the versions of a package) gets the same rows in pages of the Bastion's size (Q-UI-1445).
   */
  page<T>(path: string, params: DataParams = {}): PageFetcher<T> {
    return async (cursor, limit) =>
      this.get<Page<T>>(path, { ...params, limit: Math.min(limit, BASTION_PAGE_LIMIT), ...(cursor !== null ? { cursor } : {}) });
  }
}

/** The largest page the Bastion's data path answers (`catalogue.MaxLimit`, W-35's B.8: pages of 50, at most 100). */
export const BASTION_PAGE_LIMIT = 100;

/** The longest Retry-After the application waits for its one automatic retry of a read. */
const MAX_RETRY_WAIT_S = 30;

/**
 * The failures of the data (R-UI-216 at the Bastion): a failed answer becomes the `ApiError` every view's notice shows whole, with the
 * call, the time and the answer; a read that met no answer, a `429` or a `503` with Retry-After is read once more after the wait, as
 * the main installation's interceptor does (R-UI-042), except a `503 not_ready`: the shell says at once that the Bastion has nothing to
 * show yet and reads its state again by itself (R-UI-227), where a retry after its Retry-After of 30 s kept the spinner that long
 * (Q-UI-1445). No session consequence: the Bastion has none.
 */
export const bastionDataInterceptor: HttpInterceptorFn = (req, next) => {
  if (!req.url.startsWith(DATA_ROOT)) return next(req);
  const failures = inject(CallFailures);
  const call = callOf(req);
  let retried = false;
  const run = (): Observable<HttpEvent<unknown>> =>
    next(req).pipe(
      catchError((err: unknown) => {
        if (!(err instanceof HttpErrorResponse) || retried) return throwError(() => err);
        const wait = retryAfterSeconds(err.headers);
        const notReady = (err.error as { code?: unknown } | null)?.code === 'not_ready';
        const again =
          err.status === 0 || err.status === 429 || (err.status === 503 && !notReady && wait !== null && wait <= MAX_RETRY_WAIT_S);
        if (!again) return throwError(() => err);
        retried = true;
        return timer(Math.min(wait ?? 1, MAX_RETRY_WAIT_S) * 1000).pipe(switchMap(() => run()));
      }),
    );
  return run().pipe(
    tap((event) => {
      if (event instanceof HttpResponse) failures.answered(call);
    }),
    catchError((err: unknown) => {
      if (!(err instanceof HttpErrorResponse)) return throwError(() => err);
      const inRow = failures.failed(call);
      return from(apiErrorOf(req, err, { retried, inRow })).pipe(switchMap((apiError) => throwError(() => apiError)));
    }),
  );
};
