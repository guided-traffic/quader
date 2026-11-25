import { ApplicationConfig, provideBrowserGlobalErrorListeners } from '@angular/core';
import {
  provideHttpClient,
  withFetch,
  withInterceptors,
  withXsrfConfiguration,
} from '@angular/common/http';
import { provideRouter, TitleStrategy, withComponentInputBinding } from '@angular/router';
import { routes } from './app.routes';
import { provideApiServices } from './api/providers';
import { CSRF_COOKIE, CSRF_HEADER } from './api/csrf';
import { busyActInterceptor } from './shared/ui/busy';
import {
  conventionsInterceptor,
  problemInterceptor,
  resilienceInterceptor,
} from './core/http/interceptors';
import { QuaderTitleStrategy } from './core/title-strategy';

// >>> M1 ui-a
// The constants live in api/csrf.ts so that the upload client of api/files-api.ts can read them.
export { CSRF_COOKIE, CSRF_HEADER };
// <<< M1 ui-a

/**
 * Root providers. Zoneless change detection is the Angular 22 default, so no provider names it.
 * Interceptor order, outermost first: problem mapping and session consequences, the one
 * automatic retry, then the request conventions (epoch, idempotency key). CSRF comes from
 * Angular's XSRF support, which adds the header to unsafe same-origin requests.
 */
export const appConfig: ApplicationConfig = {
  providers: [
    provideBrowserGlobalErrorListeners(),
    ...provideApiServices(),
    provideRouter(routes, withComponentInputBinding()),
    { provide: TitleStrategy, useExisting: QuaderTitleStrategy },
    provideHttpClient(
      withFetch(),
      withXsrfConfiguration({ cookieName: CSRF_COOKIE, headerName: CSRF_HEADER }),
      withInterceptors([busyActInterceptor, problemInterceptor, resilienceInterceptor, conventionsInterceptor]),
    ),
  ],
};
