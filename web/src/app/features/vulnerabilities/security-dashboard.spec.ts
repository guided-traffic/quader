import { TestBed } from '@angular/core/testing';
import { RedirectCommand, Router, UrlTree, provideRouter } from '@angular/router';
import { vi } from 'vitest';
import { ProjectsApi } from '../../api/projects-api';
import { RepositoriesApi } from '../../api/repositories-api';
import { SessionApi } from '../../api/session-api';
import { FakeSessionApi, provideFakeApis } from '../../api/testing/fakes';
import { FakeRepositoriesApi } from '../../api/testing/fakes-catalog';
import { NPM_REPOSITORIES, NUGET_REPOSITORIES } from '../../api/testing/fakes-packages';
import { FakeProjectsApi, sampleProject } from '../../api/testing/fakes-projects';
import { FINDING_VULN, FakeVulnerabilitiesApi, sampleFinding } from '../../api/testing/fakes-vulnerabilities';
import { VulnerabilitiesApi } from '../../api/vulnerabilities-api';
import { capabilityGuard } from '../../core/auth-guard';
import { helpFor } from '../../core/help-topics';
import { Navigation, SECURITY_DASHBOARD_CAPABILITY } from '../../core/navigation';
import {
  applyFilters,
  buttonByText,
  chipTexts,
  expectAccessible,
  inProject,
  openFilters,
  renderScreen,
  searchList,
  settle,
  text,
} from '../../testing/screen';
import { SECURITY_ROUTES } from './security.routes';
import { SECURITY_CONTEXT, VULN_CONTEXT, advisoryIdOf } from './vuln-context';
import { VulnerabilityReport } from './vulnerability-report';

const SHOP_FINDING = '0193a000-0000-7000-8000-0000000000a1';
/** The capabilities of the role auditor that the dashboard looks at (internal/authz/roles.go). */
const AUDITOR = ['content:metadata-read', 'vulnerabilities:triage', 'settings:read', 'projects:read', 'audit:read'];

async function openDashboard(inputs: Record<string, unknown> = {}, capabilities: string[] = AUDITOR) {
  const r = await renderScreen(VulnerabilityReport, {
    inputs,
    capabilities,
    providers: [{ provide: VULN_CONTEXT, useValue: SECURITY_CONTEXT }],
    setup: (get) => {
      (get(RepositoriesApi) as FakeRepositoriesApi).repositories = [...NPM_REPOSITORIES, ...NUGET_REPOSITORIES];
      (get(ProjectsApi) as FakeProjectsApi).projects = [sampleProject(), sampleProject({ key: 'shop', displayName: 'Online shop' })];
      const fake = get(VulnerabilitiesApi) as FakeVulnerabilitiesApi;
      fake.rows.push(
        sampleFinding({
          id: SHOP_FINDING,
          project: 'shop',
          repository: 'shop/npm-local',
          package: 'minimist',
          version: '1.2.0',
          advisory: 'GHSA-xvch-5gv4-984h',
          aliases: ['CVE-2021-44906'],
          summary: 'Prototype pollution in minimist',
          severity: 'critical',
          cvss: null,
        }),
      );
    },
  });
  return { ...r, fake: r.get(VulnerabilitiesApi) as FakeVulnerabilitiesApi };
}

const rows = (r: { root: HTMLElement }) => [...r.root.querySelectorAll('quader-data-table tbody tr')];
/** The names of the severity bar's links (R-UI-092, Q-UI-376). */
const bar = (r: { root: HTMLElement }) => [...r.root.querySelectorAll('quader-severity-bar a')].map((a) => a.getAttribute('aria-label') ?? '');
const navigate = () => vi.spyOn(TestBed.inject(Router), 'navigate').mockResolvedValue(true);
const searchField = (root: HTMLElement) => root.querySelector<HTMLInputElement>('.list-search input')!;

describe('The security dashboard (Q-UI-130, R-VULN-022, R-UI-158, R-UI-159)', () => {
  it('Q-UI-130 Q-UI-380 shows every project as the link of each qualified repository name, the counts and the trend over all of them, and passes axe', async () => {
    const r = await openDashboard();
    expect(text(r.root.querySelector('h1')!)).toBe('Security dashboard');
    const headers = [...r.root.querySelectorAll('thead th')].map((th) => text(th));
    expect(headers).not.toContain('Project');
    // Q-UI-380: the project is the first part of the qualified name, a link that narrows the list to it.
    const cell = rows(r)
      .map((tr) => tr.querySelector('td.qualified-name'))
      .find((td) => text(td!).startsWith('shop/'))!;
    expect(cell.querySelector('a')?.getAttribute('href')).toContain('project=shop');
    const all = rows(r).map((tr) => text(tr));
    expect(all.some((t) => t.includes('shop') && t.includes('minimist'))).toBe(true);
    expect(all.some((t) => t.includes('web') && t.includes('quader-test-vuln'))).toBe(true);
    // the API was asked without a project: the counts and the trend cover every project the caller may read
    expect(r.fake.listed.at(-1)?.project).toEqual([]);
    expect(r.fake.calls).toContain('summary');
    expect(r.fake.calls.some((c) => c.startsWith('trend'))).toBe(true);
    expect(bar(r)).toContain('2 Critical');
    expect(r.root.querySelector('quader-vuln-trend')).toBeTruthy();
    await expectAccessible(r.root);
  });

  it('R-UI-159 the page starts with its title, the counts and the trend: no form above them, no example texts, one search at the head of the list', async () => {
    const r = await openDashboard();
    expect(r.root.querySelector('form[role="search"]')).toBeNull();
    expect(r.root.querySelectorAll('[role="search"]')).toHaveLength(1);
    expect(r.root.querySelector('quader-list-head [role="search"]')).toBeTruthy();
    expect(r.root.querySelector('input[placeholder="CVE-2021-23337"], input[placeholder="lodash"], input[placeholder="shop"]')).toBeNull();
    expect(searchField(r.root).getAttribute('aria-label')).toBe('Search by advisory, alias, package or summary');
    // The title, the line of the scope, the counts and the trend come before the list, in this order.
    const order = ['h1', 'p[role="note"]', 'quader-vuln-summary', 'quader-vuln-trend', 'quader-list-head'].map((s) => r.root.querySelector(s)!);
    for (let i = 1; i < order.length; i++) {
      expect(order[i - 1]!.compareDocumentPosition(order[i]!) & Node.DOCUMENT_POSITION_FOLLOWING, `${i}`).toBeTruthy();
    }
  });

  it('R-UI-159 Q-UI-625 a line says what the counts and the trend cover, with and without a project, repository or format filter', async () => {
    const all = await openDashboard();
    expect(text(all.root)).toContain('The counts and the trend cover every project.');
    const scoped = await openDashboard({ project: 'shop', severity: 'critical' });
    expect(text(scoped.root)).toContain('The counts and the trend cover the projects, repositories and formats of the filters.');
    // the severity narrows the list only: the summary is asked for the project alone
    expect(scoped.fake.listed.at(-1)?.severity).toEqual(['critical']);
    const searched = await openDashboard({ q: 'minimist' });
    expect(text(searched.root)).toContain('The counts and the trend cover every project.');
  });

  it('Q-UI-130 a project filter of the URL reaches the list, the counts and the trend, shows as a chip with the project\'s name, and keeps in the count links', async () => {
    const r = await openDashboard({ project: 'shop' });
    expect(r.fake.listed.at(-1)?.project).toEqual(['shop']);
    expect(rows(r)).toHaveLength(1);
    expect(text(rows(r)[0]!)).toContain('minimist');
    expect(bar(r)).toContain('1 Critical');
    expect(bar(r)).toContain('0 High');
    const critical = [...r.root.querySelectorAll<HTMLAnchorElement>('quader-severity-bar a')].find((a) => a.getAttribute('aria-label') === '1 Critical')!;
    expect(critical.getAttribute('href')).toContain('project=shop');
    expect(chipTexts(r.root)).toContain('Project: Online shop');
    const chip = [...r.root.querySelectorAll('.chip')].find((c) => text(c).includes('Project: Online shop'))!;
    const nav = navigate();
    chip.querySelector('button')!.click();
    expect(nav).toHaveBeenCalledWith([], { queryParams: { project: null }, queryParamsHandling: 'merge' });
  });

  it('R-UI-159 Q-UI-624 the project is chosen in the drawer from the projects by mark and name, never typed as a key', async () => {
    const r = await openDashboard();
    const drawer = await openFilters(r.fixture, r.root);
    const category = [...drawer.querySelectorAll('quader-filter-category')].find((c) => text(c).startsWith('Project'))!;
    expect(category.querySelector('input[type="text"]')).toBeNull();
    const rowsOfProjects = [...category.querySelectorAll('label.check-row')];
    expect(rowsOfProjects.map((l) => text(l))).toEqual(['Online shop', 'Web platform']);
    expect(rowsOfProjects[0]!.querySelector('quader-project-mark')).toBeTruthy();
    const nav = navigate();
    rowsOfProjects[0]!.querySelector<HTMLInputElement>('input[type="checkbox"]')!.click();
    await applyFilters(r.fixture);
    expect(nav).toHaveBeenCalledWith([], expect.objectContaining({ queryParams: expect.objectContaining({ project: ['shop'] }) }));
  });

  it('R-UI-159 a project\'s report keeps its project in the route: no project category in its drawer and no project chip', async () => {
    const r = await renderScreen(VulnerabilityReport, inProject({ capabilities: ['vulnerabilities:triage'], inputs: { project: 'web' } }, '/projects/web/vulnerabilities'));
    const drawer = await openFilters(r.fixture, r.root);
    expect([...drawer.querySelectorAll('quader-filter-category')].map((c) => text(c))).not.toContainEqual(expect.stringMatching(/^Project/));
    expect(chipTexts(r.root).some((c) => c.startsWith('Project'))).toBe(false);
  });

  it('Q-UI-130 a malformed project key in the URL never reaches the API', async () => {
    const r = await openDashboard({ project: ['Not A Key', 'shop'] });
    expect(r.fake.listed.at(-1)?.project).toEqual(['shop']);
  });

  it('R-UI-159 Q-UI-623 a whole advisory identifier in the search is the advisory filter in its canonical spelling, any other text the text search', async () => {
    const r = await openDashboard();
    const nav = navigate();
    await searchList(r.fixture, r.root, ' cve-2021-44906 ');
    expect(nav).toHaveBeenLastCalledWith([], { queryParams: { advisory: 'CVE-2021-44906', q: null }, queryParamsHandling: 'merge' });
    await searchList(r.fixture, r.root, 'ghsa-XVCH-5gv4-984H');
    expect(nav).toHaveBeenLastCalledWith([], { queryParams: { advisory: 'GHSA-xvch-5gv4-984h', q: null }, queryParamsHandling: 'merge' });
    await searchList(r.fixture, r.root, 'minimist');
    expect(nav).toHaveBeenLastCalledWith([], { queryParams: { advisory: null, q: 'minimist' }, queryParamsHandling: 'merge' });
    await searchList(r.fixture, r.root, 'CVE-2021');
    expect(nav).toHaveBeenLastCalledWith([], { queryParams: { advisory: null, q: 'CVE-2021' }, queryParamsHandling: 'merge' });
    // An emptied field drops both.
    const searched = await openDashboard({ advisory: 'CVE-2021-44906' });
    const again = navigate();
    await searchList(searched.fixture, searched.root, '');
    expect(again).toHaveBeenLastCalledWith([], { queryParams: { advisory: null, q: null }, queryParamsHandling: 'merge' });
  });

  it('R-UI-159 Q-UI-623 the search by an alias finds the finding through the API, the field shows the identifier and no chip says it twice', async () => {
    const r = await openDashboard({ advisory: 'CVE-2021-44906' });
    expect(r.fake.listed.at(-1)?.advisory).toBe('CVE-2021-44906');
    expect(rows(r)).toHaveLength(1);
    expect(text(rows(r)[0]!)).toContain('GHSA-xvch-5gv4-984h');
    expect(searchField(r.root).value).toBe('CVE-2021-44906');
    expect(chipTexts(r.root)).toEqual([]);
  });

  it('R-UI-159 a text search matches an alias too, as the API does', async () => {
    const r = await openDashboard({ q: '44906' });
    expect(r.fake.listed.at(-1)?.q).toBe('44906');
    expect(rows(r)).toHaveLength(1);
    expect(text(rows(r)[0]!)).toContain('minimist');
  });

  it('R-UI-159 Clear all of the drawer keeps the search, Clear all filters under an empty list takes it too', async () => {
    const r = await openDashboard({ advisory: 'CVE-1999-0001', project: 'shop' });
    expect(rows(r)).toHaveLength(0);
    const nav = navigate();
    buttonByText(r.root, 'Clear all filters').click();
    expect(nav).toHaveBeenLastCalledWith([], expect.objectContaining({ queryParams: expect.objectContaining({ advisory: null, q: null, project: null }) }));
  });

  it('Q-UI-130 a project cell narrows the list to that project', async () => {
    const r = await openDashboard();
    const link = [...rows(r)[0]!.querySelectorAll<HTMLAnchorElement>('a')].find((a) => a.getAttribute('href')?.includes('project='))!;
    expect(link.getAttribute('href')).toMatch(/project=(shop|web)/);
  });

  it('R-UI-158 the findings open below /security, never in the admin area; the advisory sources only for whoever may open them', async () => {
    const r = await openDashboard();
    expect(r.root.querySelector(`a[href="/security/findings/${SHOP_FINDING}"]`)).toBeTruthy();
    expect(r.root.querySelector(`a[href="/security/findings/${FINDING_VULN}"]`)).toBeTruthy();
    expect(r.root.querySelector('a[href^="/admin/security"]')).toBeNull();
    expect(r.root.querySelector('a[href="/admin/advisory-sources"]')).toBeTruthy();
    // A package opens in the project of its finding (Q-UI-163), never at a content address without a project.
    expect(r.root.querySelector('a[href^="/projects/shop/packages/npm/minimist"]')).toBeTruthy();
    // A reader without settings:read sees no link into the admin area at all.
    const reader = await openDashboard({}, ['content:metadata-read']);
    expect(reader.root.querySelector('a[href^="/admin"]')).toBeNull();
  });

  it('Q-UI-130 a holder of vulnerabilities:triage selects findings and changes their status; a reader does not', async () => {
    const triager = await openDashboard({}, AUDITOR);
    expect(triager.root.querySelector('tbody input[type="checkbox"]')).toBeTruthy();
    expect(buttonByText(triager.root, 'Select all loaded findings')).toBeTruthy();
    expect(text(triager.root)).not.toContain('Changing their status needs');

    const reader = await openDashboard({}, ['content:metadata-read']);
    expect(rows(reader).length).toBeGreaterThan(0);
    expect(reader.root.querySelector('tbody input[type="checkbox"]')).toBeNull();
    expect([...reader.root.querySelectorAll('button')].some((b) => text(b).includes('Change status'))).toBe(false);
    expect([...reader.root.querySelectorAll('button')].some((b) => text(b).includes('Select all loaded findings'))).toBe(false);
    expect(text(reader.root)).toContain('Changing their status needs the permission to triage vulnerabilities');
    await expectAccessible(reader.root);
  });

  it('Q-UI-130 a project\'s report has no project link in its rows and no scope line, and its links stay in the project', async () => {
    const r = await renderScreen(VulnerabilityReport, inProject({ capabilities: ['vulnerabilities:triage'] }, '/projects/web/vulnerabilities'));
    expect([...r.root.querySelectorAll('th')].map((th) => text(th))).not.toContain('Project');
    expect(text(r.root)).not.toContain('The counts and the trend cover');
    expect(text(r.root.querySelector('h1')!)).toBe('Vulnerabilities');
    expect(r.root.querySelector('a[href^="/projects/web/vulnerabilities/findings/"]')).toBeTruthy();
  });

  it('Q-UI-130 the dashboard is usable after the list changes: a filter change restarts the list', async () => {
    const r = await openDashboard();
    await r.fixture.componentRef.setInput('project', 'web');
    await settle(r.fixture);
    expect(rows(r).every((tr) => text(tr).includes('web'))).toBe(true);
    expect(r.fake.listed.at(-1)?.project).toEqual(['web']);
  });
});

describe('Where the security dashboard lives and who sees it (R-UI-158, Q-AUTHZ-295)', () => {
  const homeGroup = () => TestBed.inject(Navigation).area('home')?.entries.find((e) => e.kind === 'group' && e.id === 'security-audit');

  it('R-UI-158 the section Security audit of Your work holds Security dashboard for a holder of content:metadata-read, the auditor and the administrator', async () => {
    await renderScreen(VulnerabilityReport, { capabilities: AUDITOR });
    const group = homeGroup();
    expect(group?.label).toBe('Security audit');
    expect(group?.kind === 'group' ? group.items.map((i) => [i.label, i.route]) : []).toEqual([['Security dashboard', '/security']]);
    expect(SECURITY_DASHBOARD_CAPABILITY).toBe('content:metadata-read');
  });

  it('R-UI-158 Q-AUTHZ-295 a caller without content:metadata-read has no section, also a holder of vulnerabilities:triage alone (security-admin)', async () => {
    await renderScreen(VulnerabilityReport, { capabilities: [] });
    expect(homeGroup()).toBeUndefined();
    await renderScreen(VulnerabilityReport, { capabilities: ['vulnerabilities:triage', 'settings:read', 'audit:read'] });
    expect(homeGroup()).toBeUndefined();
  });

  it('R-UI-158 the admin area has no security entry any more, for anybody', async () => {
    await renderScreen(VulnerabilityReport, { capabilities: [...AUDITOR, 'repositories:read-config'] });
    const admin = TestBed.inject(Navigation).area('admin')!;
    const routes = admin.entries.flatMap((e) => (e.kind === 'item' ? [e.route] : e.items.map((i) => i.route)));
    expect(routes.some((r) => r.startsWith('/admin/security'))).toBe(false);
    expect(routes).toContain('/admin/advisory-sources');
  });

  it('R-UI-158 Q-UI-621 the Admin button follows the admin area: shown to an auditor, not to a caller whose capabilities are the dashboard\'s alone', async () => {
    await renderScreen(VulnerabilityReport, { capabilities: AUDITOR });
    expect(TestBed.inject(Navigation).hasAdminArea()).toBe(true);
    await renderScreen(VulnerabilityReport, { capabilities: ['content:metadata-read', 'vulnerabilities:triage'] });
    expect(TestBed.inject(Navigation).hasAdminArea()).toBe(false);
    expect(TestBed.inject(Navigation).area('home')?.entries.some((e) => e.kind === 'group' && e.id === 'security-audit')).toBe(true);
  });

  it('R-UI-158 the "?" opens the manual page of the dashboard in the user guide, and the routes keep the screens below /security', () => {
    expect(helpFor('/security')).toEqual(['/help', 'user', 'security-dashboard']);
    expect(helpFor('/security/findings/abc')).toEqual(['/help', 'user', 'security-dashboard']);
    const inner = SECURITY_ROUTES[0]!.children!;
    expect(inner.map((c) => c.path ?? 'matcher')).toEqual(['', 'findings/:id', 'matcher']);
    expect(SECURITY_ROUTES[0]!.providers).toEqual([{ provide: VULN_CONTEXT, useValue: SECURITY_CONTEXT }]);
  });
});

describe('The guard of the security dashboard (R-UI-158, Q-UI-622)', () => {
  async function guard(capabilities: string[]): Promise<true | string> {
    TestBed.resetTestingModule();
    TestBed.configureTestingModule({ providers: [provideRouter([]), ...provideFakeApis()] });
    (TestBed.inject(SessionApi) as FakeSessionApi).meResult = { capabilities };
    const result = await TestBed.runInInjectionContext(() => capabilityGuard(SECURITY_DASHBOARD_CAPABILITY)({} as never, {} as never));
    if (result === true) return true;
    expect(result).toBeInstanceOf(RedirectCommand);
    const command = result as RedirectCommand;
    expect(command.navigationBehaviorOptions?.skipLocationChange).toBe(true);
    return TestBed.inject(Router).serializeUrl(command.redirectTo as UrlTree);
  }

  it('R-UI-158 lets the auditor and the administrator in', async () => {
    expect(await guard(AUDITOR)).toBe(true);
    expect(await guard(['content:metadata-read'])).toBe(true);
  });

  it('R-UI-158 Q-UI-622 answers Page not found at the same address to everyone else, a security administrator included', async () => {
    expect(await guard([])).toBe('/not-found');
    expect(await guard(['vulnerabilities:triage', 'settings:read'])).toBe('/not-found');
  });
});

describe('advisoryIdOf (Q-UI-623)', () => {
  it('returns a whole identifier of every family in its canonical spelling, and null for anything else', () => {
    expect(advisoryIdOf(' cve-2021-23337 ')).toBe('CVE-2021-23337');
    expect(advisoryIdOf('mal-2026-1234')).toBe('MAL-2026-1234');
    expect(advisoryIdOf('ghsa-35JH-R3H4-6JHM')).toBe('GHSA-35jh-r3h4-6jhm');
    expect(advisoryIdOf('GHSA-35jh-r3h4-6jhm')).toBe('GHSA-35jh-r3h4-6jhm');
    expect(advisoryIdOf('pysec-2021-1')).toBe('PYSEC-2021-1');
    expect(advisoryIdOf('go-2022-0001')).toBe('GO-2022-0001');
    expect(advisoryIdOf('rustsec-2021-0001')).toBe('RUSTSEC-2021-0001');
    expect(advisoryIdOf('osv-2020-111')).toBe('OSV-2020-111');
    expect(advisoryIdOf('CVE-2021')).toBeNull();
    expect(advisoryIdOf('GHSA-35jh')).toBeNull();
    expect(advisoryIdOf('lodash')).toBeNull();
    expect(advisoryIdOf('prototype pollution CVE-2021-23337')).toBeNull();
    expect(advisoryIdOf('')).toBeNull();
  });
});
