import { ApplicationConfig, inject, provideBrowserGlobalErrorListeners } from '@angular/core';
import { provideHttpClient, withFetch, withInterceptors } from '@angular/common/http';
import { TitleStrategy, provideRouter, withComponentInputBinding, withNavigationErrorHandler } from '@angular/router';
import { loadGoneScreen } from '../app/core/gone-script';
import { REPOSITORIES_SHOWN } from '../app/core/repositories-shown';
import { QuaderTitleStrategy, TITLE_SUFFIX } from '../app/core/title-strategy';
import { bastionDataInterceptor } from './data/http';
import { provideBastionReads } from './data/reads';
import { BastionStateStore } from './data/state';
import { BASTION_ROUTES } from './app.routes';

/**
 * The providers of the Bastion's application (R-UI-227, R-UI-228): the views' reads bound to the catalogue below `/ui/data/v1/`, the
 * router with the main installation's addresses, and an HTTP client with one interceptor, which turns a failed read into the notice's
 * error. No session, no CSRF, no write: nothing here sends anything but a GET. A screen whose script the Bastion no longer holds after a
 * rollout is loaded from the server (Q-UI-1370, H-2324). Every title ends in the Bastion's name, "Quader Bastion" (Q-UI-1617). The views
 * name the repositories while the state's switch **Hide repositories** is off, and stop at its next read once it is on (R-UI-274).
 */
export const bastionConfig: ApplicationConfig = {
  providers: [
    provideBrowserGlobalErrorListeners(),
    ...provideBastionReads(),
    { provide: REPOSITORIES_SHOWN, useFactory: () => inject(BastionStateStore).repositoriesShown },
    provideRouter(BASTION_ROUTES, withComponentInputBinding(), withNavigationErrorHandler(loadGoneScreen)),
    { provide: TitleStrategy, useExisting: QuaderTitleStrategy },
    { provide: TITLE_SUFFIX, useValue: 'Quader Bastion' },
    provideHttpClient(withFetch(), withInterceptors([bastionDataInterceptor])),
  ],
};
