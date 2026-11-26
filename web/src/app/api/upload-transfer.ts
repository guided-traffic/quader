import { HttpClient, HttpErrorResponse, HttpEventType, provideHttpClient, withXhr, withXsrfConfiguration } from '@angular/common/http';
import { EnvironmentInjector, Injectable, createEnvironmentInjector, inject } from '@angular/core';
import { Observable, catchError, filter, from, fromEvent, lastValueFrom, map, switchMap, takeUntil, tap, throwError } from 'rxjs';
import { CallFailures, apiErrorOf, callOf } from '../core/http/failure';
import { SessionStore } from '../core/session-store';
import { CSRF_COOKIE, CSRF_HEADER } from './csrf';

/** How a transfer is followed and stopped. */
export interface TransferOptions {
  readonly onProgress?: (loaded: number, total: number) => void;
  readonly signal?: AbortSignal;
}

/**
 * The one client that sends a file to the server with its progress (R-UI-012, R-ANS-061): the deploy dialog's upload of a file and the
 * upload of an Ansible collection. The fetch backend reports no upload progress, so the transfer goes through an XHR-based client. It
 * runs no Quader interceptor of its own: it keeps the XSRF protection (a session route, so the write carries the CSRF token), maps a
 * failure to `ApiError` as the interceptors do (R-UI-216) and ends the session on a 401. An abort through the signal rejects with a
 * `DOMException` named `AbortError`.
 */
@Injectable({ providedIn: 'root' })
export class UploadTransfer {
  private readonly session = inject(SessionStore);
  private readonly failures = inject(CallFailures);
  private readonly injector = inject(EnvironmentInjector);
  private xhr: HttpClient | null = null;

  send<T>(method: 'PUT' | 'POST', url: string, body: Blob, headers: Record<string, string>, options: TransferOptions = {}): Promise<T> {
    this.xhr ??= createEnvironmentInjector(
      [provideHttpClient(withXhr(), withXsrfConfiguration({ cookieName: CSRF_COOKIE, headerName: CSRF_HEADER }))],
      this.injector,
    ).get(HttpClient);
    const abort$: Observable<unknown> = options.signal ? fromEvent(options.signal, 'abort') : new Observable<never>();
    const events$ = this.xhr
      .request<T>(method, url, { body, headers, observe: 'events', reportProgress: true, responseType: 'json' })
      .pipe(
        tap((event) => {
          if (event.type === HttpEventType.UploadProgress && event.total) options.onProgress?.(event.loaded, event.total);
        }),
        filter((event) => event.type === HttpEventType.Response),
        map((event) => (event as { body: T | null }).body as T),
        takeUntil(abort$),
        catchError((err: unknown) => from(this.toApiError(method, url, err)).pipe(switchMap((e) => throwError(() => e)))),
      );
    return lastValueFrom(events$, { defaultValue: null }).then((answer) => {
      if (answer === null) throw new DOMException('The upload was cancelled.', 'AbortError');
      return answer;
    });
  }

  /** The transfer's failure as the interceptors make one (R-UI-216): this client runs none of them. */
  private async toApiError(method: string, url: string, err: unknown): Promise<unknown> {
    if (!(err instanceof HttpErrorResponse)) return err;
    const req = { method, urlWithParams: url };
    const error = await apiErrorOf(req, err, { retried: false, inRow: this.failures.failed(callOf(req)) });
    if (err.status === 401 && error.code !== 'reauthentication_required') this.session.markEnded();
    return error;
  }
}
