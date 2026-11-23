import { TestBed } from '@angular/core/testing';
import { Router, UrlTree, provideRouter } from '@angular/router';
import { FakeSessionApi, provideFakeApis } from '../api/testing/fakes';
import { SessionApi } from '../api/session-api';
import { adminHomeGuard } from './auth-guard';

async function startOf(capabilities: string[]): Promise<string> {
  TestBed.resetTestingModule();
  TestBed.configureTestingModule({ providers: [provideRouter([]), ...provideFakeApis()] });
  (TestBed.inject(SessionApi) as FakeSessionApi).meResult = { capabilities };
  const tree = (await TestBed.runInInjectionContext(() => adminHomeGuard({} as never, {} as never))) as UrlTree;
  return TestBed.inject(Router).serializeUrl(tree);
}

describe('The start of the admin area (R-UI-002, Q-OPS-175, R-UI-158)', () => {
  it('Q-OPS-175 starts at the dashboard for a holder of settings:read, the auditor among them', async () => {
    expect(await startOf(['settings:read', 'content:metadata-read'])).toBe('/admin/dashboard');
  });

  it('R-UI-158 Q-UI-1043 never starts at the security dashboard, which is no page of the admin area: without an entry of it, the start page', async () => {
    expect(await startOf(['content:metadata-read'])).toBe('/');
    expect(await startOf(['vulnerabilities:triage'])).toBe('/');
  });

  it('Q-UI-1040 Q-UI-1043 a holder of projects:read alone has no admin area: the projects are the main area\'s', async () => {
    expect(await startOf(['projects:read', 'content:metadata-read'])).toBe('/');
  });

  it('Q-UI-1043 everyone else starts at the first entry of the admin side bar they have, in its order', async () => {
    expect(await startOf(['repositories:read-config', 'content:metadata-read'])).toBe('/admin/upstream');
    expect(await startOf(['endpoints:read', 'identities:read'])).toBe('/admin/endpoints');
    expect(await startOf(['identities:read'])).toBe('/admin/users');
    expect(await startOf(['audit:read'])).toBe('/admin/audit');
  });
});
