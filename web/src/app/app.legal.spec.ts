import { provideHttpClient } from '@angular/common/http';
import { signal } from '@angular/core';
import { TestBed } from '@angular/core/testing';
import { Router, provideRouter, withComponentInputBinding } from '@angular/router';
import { App } from './app';
import { routes } from './app.routes';
import { FakeLegalApi, FakeLoginApi, FakeSessionApi, provideFakeApis } from './api/testing/fakes';
import { LegalApi, LegalPages } from './api/legal-api';
import { LoginApi } from './api/login-api';
import { ApiError } from './api/problem';
import { SessionApi } from './api/session-api';
import { Layout } from './core/layout';
import { BrowserStorage } from './core/storage';
import { seriousViolations } from './testing/axe';

const both: LegalPages = {
  imprint: { html: '<p>Example Operations GmbH<br>Musterstraße 1</p><table><tbody><tr><td>HRB 12345</td></tr></tbody></table>', lang: 'en' },
  privacy: { html: '<p>Wir speichern die Adresse jeder Anfrage 30 Tage.</p>', lang: 'de' },
};

interface Setup {
  readonly legal?: LegalPages;
  readonly signedOut?: boolean;
  readonly publicContent?: boolean;
}

async function render(url: string, s: Setup = {}) {
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
  if (s.legal) (TestBed.inject(LegalApi) as FakeLegalApi).pagesResult = s.legal;
  if (s.signedOut) {
    (TestBed.inject(SessionApi) as FakeSessionApi).whoamiResult = new ApiError(401, { code: 'unauthenticated' }, null);
  }
  if (s.publicContent) (TestBed.inject(LoginApi) as FakeLoginApi).optionsResult.publicContent = true;
  const fixture = TestBed.createComponent(App);
  await TestBed.inject(Router).navigateByUrl(url);
  await fixture.whenStable();
  fixture.detectChanges();
  await fixture.whenStable();
  return { fixture, root: fixture.nativeElement as HTMLElement };
}

const footerLinks = (root: HTMLElement): string[] =>
  [...root.querySelectorAll('main quader-legal-footer a')].map((a) => `${a.textContent?.trim()} ${a.getAttribute('href')}`);

describe('Imprint and privacy notice', () => {
  it('R-UI-226 without any text there is no footer line, in the shell and on the sign-in screen', async () => {
    const shell = await render('/projects');
    expect(shell.root.querySelector('header.topbar')).toBeTruthy();
    expect(shell.root.querySelector('quader-legal-footer nav')).toBeNull();
    TestBed.resetTestingModule();
    const login = await render('/login', { signedOut: true });
    expect(login.root.querySelector('main h1')?.textContent).toContain('Sign in');
    expect(login.root.querySelector('quader-legal-footer nav')).toBeNull();
  });

  it('R-UI-226 the footer links both pages at the end of the content panel, after the screen', async () => {
    const { root } = await render('/projects', { legal: both });
    expect(footerLinks(root)).toEqual(['Imprint /legal/imprint', 'Privacy notice /legal/privacy']);
    const main = root.querySelector('main')!;
    expect(main.lastElementChild?.tagName).toBe('QUADER-LEGAL-FOOTER');
    expect(root.querySelector('quader-legal-footer nav')?.getAttribute('aria-label')).toBe('Legal information');
    expect(await seriousViolations(root)).toEqual([]);
  });

  it('R-UI-226 a page without a text in any language has no link', async () => {
    const { root } = await render('/projects', { legal: { imprint: null, privacy: both.privacy } });
    expect(footerLinks(root)).toEqual(['Privacy notice /legal/privacy']);
  });

  it('R-UI-226 the bare screens end with the footer: sign-in and signed out', async () => {
    for (const url of ['/login', '/logged-out']) {
      const { root } = await render(url, { legal: both, signedOut: true });
      expect(root.querySelector('header.topbar')).toBeNull();
      expect(footerLinks(root)).toEqual(['Imprint /legal/imprint', 'Privacy notice /legal/privacy']);
      expect(await seriousViolations(root)).toEqual([]);
      TestBed.resetTestingModule();
    }
  });

  it('R-UI-225 signed in, a legal page stands in the shell with its heading and the operator text', async () => {
    const { root } = await render('/legal/imprint', { legal: both });
    expect(root.querySelector('header.topbar')).toBeTruthy();
    expect(root.querySelector('main h1')?.textContent?.trim()).toBe('Imprint');
    const text = root.querySelector('quader-legal-page .readme')!;
    expect(text.innerHTML).toContain('Musterstraße 1');
    expect(text.querySelector('table')).toBeTruthy();
    expect(root.querySelector('.legal-language')).toBeNull();
    expect(root.querySelector('quader-legal-footer a[aria-current="page"]')?.textContent?.trim()).toBe('Imprint');
    expect(await seriousViolations(root)).toEqual([]);
  });

  it('R-UI-225 signed out without public content, a legal page stands alone; a text of the other language says so and carries its lang', async () => {
    const { root } = await render('/legal/privacy', { legal: both, signedOut: true });
    expect(root.querySelector('header.topbar')).toBeNull();
    expect(root.querySelector('main h1')?.textContent?.trim()).toBe('Privacy notice');
    expect(root.querySelector('.legal-language')?.textContent).toContain('available in German only');
    expect(root.querySelector('quader-package-readme.legal-text')?.getAttribute('lang')).toBe('de');
    expect(root.querySelector('a.bare-logo-link')?.getAttribute('href')).toBe('/');
    expect(await seriousViolations(root)).toEqual([]);
  });

  it('R-UI-225 a visitor of the public content reads a legal page in the shell', async () => {
    const { root } = await render('/legal/imprint', { legal: both, signedOut: true, publicContent: true });
    expect(root.querySelector('header.topbar')).toBeTruthy();
    expect(root.querySelector('main h1')?.textContent?.trim()).toBe('Imprint');
  });

  it('R-UI-225 a page without a text is Page not found at its address', async () => {
    const { root } = await render('/legal/imprint', { legal: { imprint: null, privacy: both.privacy } });
    // The guard answers with the not-found screen without changing the address (skipLocationChange), as capabilityGuard does.
    expect(root.querySelector('main h1')?.textContent).toContain('Page not found');
    expect(root.querySelector('quader-legal-page')).toBeNull();
  });
});
