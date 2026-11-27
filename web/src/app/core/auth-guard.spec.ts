import { provideHttpClient } from '@angular/common/http';
import { signal } from '@angular/core';
import { TestBed } from '@angular/core/testing';
import { Router, provideRouter, withComponentInputBinding } from '@angular/router';
import { App } from '../app';
import { routes } from '../app.routes';
import { LoginApi } from '../api/login-api';
import { ApiError } from '../api/problem';
import { SessionApi } from '../api/session-api';
import { FakeLoginApi, FakeSessionApi, provideFakeApis } from '../api/testing/fakes';
import { BrowserNavigation } from './browser-navigation';
import { Layout } from './layout';
import { SessionStore } from './session-store';
import { BrowserStorage } from './storage';
import { settle, waitUntil } from '../testing/screen';

/**
 * The browser of these specs holds the cookie of a session that has ended. The server answers it with `401 session_expired`,
 * and `whoami` clears it with that answer (Q-AUTH-410): the first `whoami` is `session_expired`, every later one carries no
 * cookie and is `unauthenticated`.
 */
function endedCookie(api: FakeSessionApi): void {
  let cleared = false;
  vi.spyOn(api, 'whoami').mockImplementation(async () => {
    const code = cleared ? 'unauthenticated' : 'session_expired';
    cleared = true;
    throw new ApiError(401, { code }, null);
  });
}

async function render(url: string, publicContent: boolean, session: (api: FakeSessionApi) => void) {
  new BrowserStorage().clear('local');
  await TestBed.configureTestingModule({
    imports: [App],
    providers: [
      provideRouter(routes, withComponentInputBinding()),
      provideHttpClient(),
      ...provideFakeApis(),
      { provide: Layout, useValue: { pinned: signal(true) } },
    ],
  }).compileComponents();
  const login = TestBed.inject(LoginApi) as FakeLoginApi;
  login.optionsResult = { ...login.optionsResult, publicContent };
  session(TestBed.inject(SessionApi) as FakeSessionApi);
  vi.spyOn(TestBed.inject(BrowserNavigation), 'assign').mockImplementation(() => undefined);
  const fixture = TestBed.createComponent(App);
  await TestBed.inject(Router).navigateByUrl(url);
  await settle(fixture);
  return { fixture, root: fixture.nativeElement as HTMLElement, router: TestBed.inject(Router) };
}

describe('authGuard and an ended session (R-UI-004)', () => {
  it('R-UI-004 an ended session lands on the sign-in screen with the notice, also while there is public content', async () => {
    const { root, router } = await render('/projects/web/repositories', true, endedCookie);
    expect(router.url).toBe('/login?returnTo=%2Fprojects%2Fweb%2Frepositories');
    expect(root.querySelector('main h1')?.textContent?.trim()).toBe('Sign in');
    expect(root.querySelector('main p.ended')?.textContent).toContain('Your session has ended.');
    expect(root.querySelector('header.topbar')).toBeNull();
    expect(TestBed.inject(SessionStore).state()).toBe('ended');
  });

  it('R-UI-057 "Continue without signing in" then leads into a working visitor session', async () => {
    const { fixture, root, router } = await render('/projects/web/repositories', true, endedCookie);
    const link = [...root.querySelectorAll<HTMLAnchorElement>('main a')].find((a) => a.textContent?.includes('Continue without signing in'));
    expect(link).toBeTruthy();
    link?.click();
    await settle(fixture);
    expect(router.url).toBe('/projects/web/repositories');
    expect(TestBed.inject(SessionStore).state()).toBe('anonymous');
    expect(root.querySelector('header.topbar')?.textContent).toContain('Sign in');
    expect(root.textContent).not.toContain('Your session has ended');
  });

  it('R-UI-004 without public content an ended session lands on the sign-in screen with the notice too', async () => {
    const { root, router } = await render('/admin/users', false, endedCookie);
    expect(router.url).toBe('/login?returnTo=%2Fadmin%2Fusers');
    expect(root.querySelector('main p.ended')).toBeTruthy();
    expect(root.textContent).not.toContain('Continue without signing in');
  });

  it('R-UI-057 a browser without a session cookie still runs as a visitor while there is public content, without the notice', async () => {
    const { root, router } = await render('/projects/web/repositories', true, (api) => {
      api.whoamiResult = new ApiError(401, { code: 'unauthenticated' }, null);
    });
    expect(router.url).toBe('/projects/web/repositories');
    expect(TestBed.inject(SessionStore).state()).toBe('anonymous');
    expect(root.textContent).not.toContain('Your session has ended');
  });

  it('R-UI-004 after the dialog of a session that ended during use, "Continue without signing in" is a visitor at once, not the sign-in screen again', async () => {
    const { fixture, root, router } = await render('/projects/web/repositories', true, () => undefined);
    const store = TestBed.inject(SessionStore);
    expect(store.state()).toBe('authenticated');
    endedCookie(TestBed.inject(SessionApi) as FakeSessionApi);
    store.markEnded();
    // The dialog loads when a session first needs it (a dynamic import, not the first page's chunk).
    const signInAgain = (): HTMLButtonElement | undefined =>
      [...document.querySelectorAll<HTMLButtonElement>('button')].find((b) => b.textContent?.trim() === 'Sign in again');
    await waitUntil(fixture, () => signInAgain() !== undefined, 'the dialog of the ended session');
    // The dialog's "Sign in again" opens the sign-in screen without the notice: the dialog has said it.
    const signIn = signInAgain();
    expect(signIn).toBeTruthy();
    signIn?.click();
    await settle(fixture);
    expect(router.url).toBe('/login?returnTo=%2Fprojects%2Fweb%2Frepositories');
    expect(root.querySelector('main p.ended')).toBeNull();
    const link = [...root.querySelectorAll<HTMLAnchorElement>('main a')].find((a) => a.textContent?.includes('Continue without signing in'));
    link?.click();
    await settle(fixture);
    expect(router.url).toBe('/projects/web/repositories');
    expect(store.state()).toBe('anonymous');
  });
});
