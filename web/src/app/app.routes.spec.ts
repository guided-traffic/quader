import { provideHttpClient } from '@angular/common/http';
import { TestBed } from '@angular/core/testing';
import { ActivatedRouteSnapshot, Router, TitleStrategy, provideRouter, withComponentInputBinding } from '@angular/router';
import { App } from './app';
import { routes } from './app.routes';
import { FakeSessionApi, provideFakeApis } from './api/testing/fakes';
import { SessionApi } from './api/session-api';
import { Breadcrumbs } from './core/breadcrumbs';
import { PackagesApi } from './api/packages-api';
import { FakePackagesApi, ansibleDocsFixture, ansibleFixture } from './api/testing/fakes-packages';

/** The collection of the fakes with the documentation of its newest version. */
const withDocs = (f: FakePackagesApi): void => {
  ansibleFixture(f);
  ansibleDocsFixture(f);
};
import { QuaderTitleStrategy } from './core/title-strategy';
import { seriousViolations } from './testing/axe';

/** Every screen of the M1 route map (R-UI-003) with a concrete URL and its expected heading. */
/** The URL, the page title and, where the page names its object (Q-UI-307), the `<h1>` when it is not the title. */
const SCREENS: readonly (readonly [url: string, heading: string, h1?: string])[] = [
  ['/login', 'Sign in'],
  ['/logged-out', 'Signed out'],
  ['/projects', 'Projects'],
  ['/projects/web/packages', 'Packages'],
  ['/projects/web/repositories', 'Repositories'],
  ['/projects/web/repositories/new', 'New repository'],
  ['/projects/web/repositories/generic-local', 'generic-local'],
  // A folder and a file of a repository are its page, named after them (R-UI-010, Q-UI-510).
  ['/projects/web/repositories/generic-local/files/releases', 'releases', 'generic-local'],
  ['/projects/web/repositories/generic-local/files/releases/1.0/app.zip', 'app.zip', 'generic-local'],
  ['/projects/web/repositories/generic-local/files', 'Files', 'generic-local'],
  ['/projects/web/repositories/npm-local/settings', 'Settings'],
  // The heading carries the number of members (Q-UI-550).
  ['/projects/web/members', 'Members', 'Members 1'],
  ['/projects/web/settings', 'Settings'],
  ['/projects/web/policies', 'Permission policies'],
  ['/projects/web/policies/new', 'New permission policy'],
  ['/projects/web/policies/team-a', 'Permission policy', 'team-a'],
  ['/projects/web/service-accounts', 'Service accounts'],
  ['/projects/web/service-accounts/ci-bot', 'Service account', 'ci-bot'],
  ['/projects/web/ci', 'CI identity'],
  // >>> M4b vulnerabilities
  ['/projects/web/vulnerabilities', 'Vulnerabilities'],
  // A finding and an advisory are named by the advisory (Q-UI-968).
  ['/projects/web/vulnerabilities/findings/0193a000-0000-7000-8000-000000000001', 'GHSA-aaaa-aaaa-0001', 'GHSA-aaaa-aaaa-0001'],
  ['/projects/web/vulnerabilities/advisories/npm/GHSA-aaaa-aaaa-0001', 'GHSA-aaaa-aaaa-0001', 'GHSA-aaaa-aaaa-0001'],
  // <<< M4b vulnerabilities
  ['/projects/web/repositories/npm-local/setup', 'Set up client'],
  // The repositories of every project, under Your work (Q-UI-1040).
  ['/repositories', 'Repositories'],
  ['/guides', 'Guides'],
  // The title names the format as people read it (Q-UI-553).
  ['/guides/generic', 'Generic', 'Generic repositories'],
  ['/guides/generic/local', 'Set up client'],
  ['/help', 'Documentation'],
  ['/help/user/getting-started', 'Getting started'],
  ['/profile', 'Profile'],
  ['/profile/tokens', 'Personal tokens'],
  ['/profile/sessions', 'Sessions'],
  ['/admin/users', 'Users'],
  ['/admin/users/alice', 'User', 'alice'],
  ['/admin/groups', 'Groups'],
  ['/admin/groups/platform', 'Group', 'platform'],
  ['/admin/service-accounts', 'Service accounts'],
  ['/admin/service-accounts/ci-bot', 'Service account', 'ci-bot'],
  ['/admin/permissions', 'Permission policies'],
  ['/admin/permissions/new', 'New permission policy'],
  ['/admin/permissions/team-a', 'Permission policy', 'team-a'],
  ['/admin/roles', 'System roles'],
  ['/admin/tokens', 'Tokens'],
  // The heading carries the number of the registries (Q-UI-550).
  ['/admin/upstream', 'Upstream registries', 'Upstream registries 12'],
  ['/admin/upstream/docker-hub', 'Upstream registry', 'Docker Hub'],
  ['/admin/endpoints', 'Endpoints'],
  ['/admin/bastions', 'Bastions'],
  // >>> M4b vulnerabilities
  ['/admin/advisory-sources', 'Advisory sources'],
  // <<< M4b vulnerabilities
  ['/admin/endpoints/main', 'Endpoint', 'main'],
  ['/admin/endpoints/main/rules/new', 'New exposure rule'],
  ['/admin/endpoints/main/rules/r1', 'Exposure rule'],
  ['/admin/audit', 'Audit log'],
  // The tabs of the page System status: titled by the tab, one header (R-UI-164).
  ['/admin/system', 'System status'],
  ['/admin/system/jobs', 'Jobs', 'System status'],
  ['/admin/system/backups', 'Backups', 'System status'],
  ['/admin/settings/auth', 'Authentication settings'],
  ['/admin/settings/general', 'General settings'],
  ['/no/such/page', 'Page not found'],
];

/** Opens `url` in the whole app; `capabilities` replace the administrator's of the fake session. */
async function open(url: string, capabilities?: readonly string[], fixture?: (packages: FakePackagesApi) => void) {
  TestBed.resetTestingModule();
  await TestBed.configureTestingModule({
    imports: [App],
    providers: [
      provideRouter(routes, withComponentInputBinding()),
      { provide: TitleStrategy, useExisting: QuaderTitleStrategy },
      provideHttpClient(),
      ...provideFakeApis(),
    ],
  }).compileComponents();
  if (capabilities) (TestBed.inject(SessionApi) as FakeSessionApi).meResult = { capabilities: [...capabilities] };
  fixture?.(TestBed.inject(PackagesApi) as FakePackagesApi);
  const app = TestBed.createComponent(App);
  await TestBed.inject(Router).navigateByUrl(url);
  await app.whenStable();
  return app.nativeElement as HTMLElement;
}

describe('route map', () => {
  for (const [url, heading, h1] of SCREENS) {
    it(`R-UI-003 ${url} shows "${h1 ?? heading}", sets the page title and has no serious accessibility violation (R-UI-044)`, async () => {
      const root = await open(url);
      expect(root.querySelector('main h1')?.textContent?.trim()).toBe(h1 ?? heading);
      expect(document.title).toBe(`${heading} – Quader`);
      expect(await seriousViolations(root)).toEqual([]);
    });
  }

  /** The route of the screen: the deepest primary child of the router state. */
  const leaf = (): ActivatedRouteSnapshot => {
    let route = TestBed.inject(Router).routerState.snapshot.root;
    while (route.firstChild) route = route.firstChild;
    return route;
  };

  it('R-UI-003 the files route of a repository captures the project, the repository key and the slash-containing path', async () => {
    await open('/projects/web/repositories/npm-local/files/@team-a/ui/-/ui-1.0.0.tgz');
    expect(leaf().params['project']).toBe('web');
    expect(leaf().params['key']).toBe('npm-local');
    expect(leaf().params['filePath']).toBe('@team-a/ui/-/ui-1.0.0.tgz');
  });

  it('R-UI-052 /projects/{project} answers with the repositories of the project, so that its breadcrumb step links to a route that exists', async () => {
    const root = await open('/projects/web');
    expect(TestBed.inject(Router).url).toBe('/projects/web/repositories');
    expect(root.querySelector('main h1')?.textContent?.trim()).toBe('Repositories');
  });

  it('R-UI-003 R-UI-126 R-UI-158 the old routes are gone: /packages, /browse, the /browse of a project, /vulnerabilities, /setup, /admin/repositories…, /admin/projects… and /admin/security are not found', async () => {
    for (const url of ['/packages', '/browse', '/projects/web/browse', '/projects/web/browse/generic-local/releases', '/vulnerabilities', '/admin/repositories', '/admin/repositories/new', '/admin/repositories/new/npm', '/admin/repositories/web/npm-local', '/admin/projects', '/admin/projects/web', '/setup', '/admin/security', '/admin/security/findings/0193a000-0000-7000-8000-000000000001']) {
      const root = await open(url);
      expect(root.querySelector('main h1')?.textContent?.trim(), url).toBe('Page not found');
    }
  });

  it('R-UI-003 R-UI-007 R-UI-218 /projects/{project}/packages/{format}/{name…} captures the format and the scoped name with its slash; an npm package opens its newest version, the table of its versions is …/-/versions', async () => {
    const root = await open('/projects/web/packages/npm/@team-a/ui');
    expect(leaf().params['project']).toBe('web');
    expect(leaf().params['format']).toBe('npm');
    expect(leaf().params['name']).toBe('@team-a/ui');
    expect(leaf().params['version']).toBeUndefined();
    // npm leads with its documentation (R-UI-218): the package's address is its newest version's page.
    expect(root.querySelector('main h1')?.textContent?.trim()).toBe('@team-a/ui 1.4.0');
    expect(document.title).toBe('Package – Quader');
    expect(TestBed.inject(Breadcrumbs).trail().map((c) => c.label)).toEqual(['Projects', 'web', 'Packages', '@team-a/ui']);
    expect(await seriousViolations(root)).toEqual([]);

    const table = await open('/projects/web/packages/npm/@team-a/ui/-/versions');
    expect(leaf().params['name']).toBe('@team-a/ui');
    expect(table.querySelector('main h1')?.textContent?.trim()).toBe('@team-a/ui');
    expect(table.querySelector('main quader-data-table')).not.toBeNull();
    expect(document.title).toBe('Versions – Quader');
    const trail = TestBed.inject(Breadcrumbs).trail();
    expect(trail.map((c) => c.label)).toEqual(['Projects', 'web', 'Packages', '@team-a/ui', 'Versions']);
    expect(trail[3]?.commands).toEqual(['/', 'projects', 'web', 'packages', 'npm', '@team-a', 'ui']);
  });

  it('R-UI-218 a collection\'s address with a page of its documentation and an anchor shows that page of the newest version, keeps both, and its switch carries them to another version', async () => {
    const at = '?tab=documentation&doc=module%2Fcheck_host_reachable#parameter-host';
    const root = await open(`/projects/web/packages/ansible/acme.tools${at}`, undefined, withDocs);
    expect(root.querySelector('main h1')?.textContent?.trim()).toBe('acme.tools 1.2.0');
    const router = TestBed.inject(Router);
    expect(router.url).toBe(`/projects/web/packages/ansible/acme.tools${at}`);
    expect(leaf().queryParams['doc']).toBe('module/check_host_reachable');
    expect(leaf().fragment).toBe('parameter-host');
    expect(root.querySelector('main [role="tab"][aria-selected="true"]')?.textContent?.trim()).toBe('Documentation');
    expect(root.querySelector('main .pane-details')?.textContent).toContain('check_host_reachable');
    // The switch keeps the tab, the page and the anchor for the version it switches to.
    root.querySelector<HTMLButtonElement>('main quader-version-switch button')!.click();
    await TestBed.inject(Router).navigateByUrl(router.url);
    const older = [...document.querySelectorAll<HTMLAnchorElement>('.version-menu a.menu-item')].find((a) => a.querySelector('.item-version')?.textContent === '1.0.0');
    expect(older?.getAttribute('href')).toBe(`/projects/web/packages/ansible/acme.tools@1.0.0${at}`);
  });

  it('R-UI-111 a link that names a version opens that version, with its page of the documentation and its anchor', async () => {
    const at = '?tab=documentation&doc=module%2Fcheck_host_reachable#parameter-host';
    const root = await open(`/projects/web/packages/ansible/acme.tools@1.0.0${at}`, undefined, withDocs);
    expect(root.querySelector('main h1')?.textContent?.trim()).toBe('acme.tools 1.0.0');
    expect(leaf().params['version']).toBe('1.0.0');
    expect(leaf().fragment).toBe('parameter-host');
    expect(TestBed.inject(Router).url).toBe(`/projects/web/packages/ansible/acme.tools@1.0.0${at}`);
    // The table of the versions answers at …/-/versions, a query and all.
    await open('/projects/web/packages/ansible/acme.tools/-/versions?q=1.1#top', undefined, withDocs);
    expect(leaf().params['name']).toBe('acme.tools');
    expect(leaf().queryParams['q']).toBe('1.1');
    expect(leaf().fragment).toBe('top');
  });

  it('R-UI-007 R-UI-110 an image keeps its table at its own address, and has no …/-/versions', async () => {
    const root = await open('/projects/web/packages/oci/team/app');
    expect(leaf().params['name']).toBe('team/app');
    expect(root.querySelector('main h1')?.textContent?.trim()).toBe('team/app');
    expect(root.querySelector('main quader-package-detail')).not.toBeNull();
    await open('/projects/web/packages/oci/team/app/-/versions');
    expect(leaf().params['name']).toBe('team/app/-/versions');
  });

  it('R-VULN-022 /projects/{project}/vulnerabilities/advisories/{ecosystem}/{id} binds both parameters and the trail names the advisory once, with a link that exists', async () => {
    const root = await open('/projects/web/vulnerabilities/advisories/npm/GHSA-aaaa-aaaa-0001');
    expect(root.querySelector('main')?.textContent).toContain('Prototype pollution in quader-test-vuln');
    const trail = TestBed.inject(Breadcrumbs).trail();
    expect(trail.map((c) => c.label)).toEqual(['Projects', 'web', 'Vulnerabilities', 'GHSA-aaaa-aaaa-0001']);
    expect(trail[3]?.commands).toEqual(['/', 'projects', 'web', 'vulnerabilities', 'advisories', 'npm', 'GHSA-aaaa-aaaa-0001']);
  });

  it('R-VULN-022 /projects/{project}/vulnerabilities/findings/{id} binds the finding and the trail links the report', async () => {
    const root = await open('/projects/web/vulnerabilities/findings/0193a000-0000-7000-8000-000000000001');
    expect(root.querySelector('main')?.textContent).toContain('quader-test-vuln@1.0.0');
    // The trail's last step is the page's title, the finding's advisory (Q-UI-968).
    expect(TestBed.inject(Breadcrumbs).trail().map((c) => c.label)).toEqual(['Projects', 'web', 'Vulnerabilities', 'GHSA-aaaa-aaaa-0001']);
  });

  it('R-UI-158 /security is the security dashboard of the main area: its trail is Security audit, Security dashboard, never the admin area', async () => {
    const auditor = ['content:metadata-read', 'vulnerabilities:triage', 'settings:read', 'audit:read', 'projects:read'];
    const root = await open('/security', auditor);
    expect(root.querySelector('main h1')?.textContent?.trim()).toBe('Security dashboard');
    expect(document.title).toBe('Security dashboard – Quader');
    const trail = TestBed.inject(Breadcrumbs).trail();
    expect(trail.map((c) => c.label)).toEqual(['Security audit', 'Security dashboard']);
    expect(trail[0]?.commands).toBeNull();
    expect(trail.some((c) => c.label === 'Admin area')).toBe(false);
    expect(await seriousViolations(root)).toEqual([]);

    const finding = await open('/security/findings/0193a000-0000-7000-8000-000000000001', auditor);
    expect(finding.querySelector('main h1')?.textContent?.trim()).toBe('GHSA-aaaa-aaaa-0001');
    expect(TestBed.inject(Breadcrumbs).trail().map((c) => c.label)).toEqual(['Security audit', 'Security dashboard', 'GHSA-aaaa-aaaa-0001']);

    // Malware and Signatures are entries of their own (Q-UI-1005): their trail does not pass the dashboard.
    for (const [url, title] of [['/security/malware', 'Malware'], ['/security/signatures', 'Signatures']] as const) {
      const page = await open(url, auditor);
      expect(page.querySelector('main h1')?.textContent?.trim()).toBe(title);
      expect(document.title).toBe(`${title} – Quader`);
      expect(TestBed.inject(Breadcrumbs).trail().map((c) => c.label)).toEqual(['Security audit', title]);
    }
  });

  it('R-UI-158 /security is Page not found for a caller without content:metadata-read, a security administrator included', async () => {
    for (const capabilities of [[], ['vulnerabilities:triage', 'settings:read', 'audit:read']]) {
      const root = await open('/security', capabilities);
      expect(root.querySelector('main h1')?.textContent?.trim(), capabilities.join()).toBe('Page not found');
    }
  });

  it('R-UI-003 /admin/settings opens the general settings, the first page of the group Settings', async () => {
    await open('/admin/settings');
    expect(TestBed.inject(Router).url).toBe('/admin/settings/general');
  });

  it('R-UI-044 the accessibility check fails on a real violation (the tooling is live)', async () => {
    const host = document.createElement('div');
    host.innerHTML = '<img src="x.png"><button></button>';
    document.body.appendChild(host);
    const found = await seriousViolations(host);
    host.remove();
    expect(found.join('\n')).toContain('image-alt');
    expect(found.join('\n')).toContain('button-name');
  });
});
