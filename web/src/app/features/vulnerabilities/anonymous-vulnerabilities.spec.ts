import { TestBed } from '@angular/core/testing';
import { ApiError } from '../../api/problem';
import { SessionApi } from '../../api/session-api';
import { FakeSessionApi } from '../../api/testing/fakes';
import { FakeVulnerabilitiesApi, sampleFinding, sampleGrade } from '../../api/testing/fakes-vulnerabilities';
import { VulnerabilitiesApi, VulnerabilityVersionReport } from '../../api/vulnerabilities-api';
import { projectEntries } from '../../core/navigation';
import { SessionStore } from '../../core/session-store';
import { PackageVersionPage } from '../packages/package-version';
import { buttonByText, expectAccessible, inProject, renderScreen, settle, text } from '../../testing/screen';
import { AdvisoryPage } from './vulnerability-advisory';

/*
 * What a visitor without a session sees of the vulnerabilities of a public repository (R-UI-224, the owner's decision of 2026-11-26): the
 * tab Vulnerabilities from the version report's own findings, the open ones only, each linking to its advisory's page, without a status;
 * nothing that links into a screen that needs a session. The API decides what the visitor reads (Q-VULN-280); these specs check the
 * screens. Unit, milliseconds.
 */

const PACKAGE_ID = 'pkg_01JBQ6V7S3K9';
const ADVISORY_LINK = '/projects/web/vulnerabilities/advisories/npm/GHSA-aaaa-aaaa-0001';

/** Makes the session's whoami answer 401: the visitor has no session. */
function anonymous(get: <T>(token: abstract new (...args: never[]) => T) => T): void {
  (get(SessionApi) as FakeSessionApi).whoamiResult = new ApiError(401, { code: 'unauthenticated', detail: 'no session' }, 'rid-401');
}

describe('The tab Vulnerabilities of a visitor without a session (R-UI-224)', () => {
  const finding = (over = {}) =>
    sampleFinding({ package: '@team-a/ui', version: '1.4.0', repository: 'web/npm-public', aliases: ['CVE-2026-1111'], ...over });
  const report = (over: Partial<VulnerabilityVersionReport['repositories'][number]> = {}): VulnerabilityVersionReport => ({
    packageId: PACKAGE_ID,
    version: '1.4.0',
    repositories: [
      {
        repository: 'web/npm-public',
        evaluation: 'evaluated',
        syncedAt: new Date().toISOString(),
        blocked: false,
        blockReason: null,
        findings: [finding()],
        grade: sampleGrade('C', { high: 1 }, { repository: 'web/npm-public', fixable: 1 }),
        ...over,
      },
    ],
  });

  async function open(rep: VulnerabilityVersionReport, extra: Record<string, unknown> = {}) {
    const r = await renderScreen(PackageVersionPage, inProject({
      inputs: { format: 'npm', name: '@team-a/ui', version: '1.4.0', tab: 'vulnerabilities', ...extra },
      setup: (get) => {
        anonymous(get);
        (get(VulnerabilitiesApi) as FakeVulnerabilitiesApi).versionReports.set(`${PACKAGE_ID}/1.4.0`, rep);
      },
    }));
    return { ...r, fake: r.get(VulnerabilitiesApi) as FakeVulnerabilitiesApi };
  }

  it('R-UI-224 lists the report\'s open findings with their aliases, each linking to its advisory, without a status, and asks no findings API', async () => {
    const r = await open(report());
    expect(TestBed.inject(SessionStore).state()).toBe('anonymous');
    const panel = r.root.querySelector('quader-package-vulnerabilities')!;
    const row = panel.querySelector('tbody tr')!;
    expect(text(row)).toContain('GHSA-aaaa-aaaa-0001');
    expect(text(row)).toContain('CVE-2026-1111');
    expect(text(row)).toContain('1.0.1');
    expect(text(row)).not.toContain('Needs triage');
    expect(row.querySelector(`a[href="${ADVISORY_LINK}"]`)).toBeTruthy();
    expect(panel.querySelector('a[href*="/findings/"]')).toBeNull();
    // The project's report needs a session: the card offers no link to it.
    expect(text(panel.querySelector('quader-grade-card')!)).not.toContain('Findings in');
    expect(r.fake.listed).toHaveLength(0);
    expect(text(panel.querySelector('.vuln-findings-total')!)).toBe('1 open findings, the worst first.');
    await expectAccessible(r.root);
  });

  it('R-UI-224 an image\'s finding names the components it affects', async () => {
    const r = await open(
      report({
        findings: [
          finding({
            format: 'oci',
            components: [{ type: 'apk', class: 'os-pkgs', name: 'xz', version: '5.6.0-r0', fixedVersion: '5.6.1-r2' }],
          }),
        ],
      }),
    );
    expect(text(r.root.querySelector('quader-version-findings tbody tr')!)).toContain('In xz 5.6.0-r0 → 5.6.1-r2');
  });

  it('R-UI-224 a repository with more open findings than the report lists says how many of how many are shown, and pages them', async () => {
    const many = Array.from({ length: 30 }, (_, i) => finding({ id: `f${i}`, advisory: `DEMO-${i}`, severity: 'high' }));
    const r = await open(report({ findings: many, grade: sampleGrade('C', { high: 1500 }, { repository: 'web/npm-public' }) }));
    const panel = r.root.querySelector('quader-version-findings')!;
    expect(text(panel.querySelector('.vuln-findings-total')!)).toBe('30 of 1,500 open findings shown, the worst first.');
    expect(panel.querySelectorAll('tbody tr')).toHaveLength(25);
    buttonByText(panel, 'Load more').click();
    await settle(r.fixture);
    expect(panel.querySelectorAll('tbody tr')).toHaveLength(30);
  });

  it('R-UI-224 the bar\'s severity narrows the report\'s findings in the browser', async () => {
    const r = await open(
      report({
        findings: [finding({ id: 'a', severity: 'high' }), finding({ id: 'b', severity: 'low' }), finding({ id: 'c', severity: 'none' })],
        grade: sampleGrade('C', { high: 1, low: 1, none: 1 }, { repository: 'web/npm-public' }),
      }),
      { severity: 'low' },
    );
    expect(r.root.querySelectorAll('quader-version-findings tbody tr')).toHaveLength(2);
  });

  it('R-UI-224 a blocked version names the finding that blocks it by a link to its advisory', async () => {
    const r = await open(report({ blocked: true, blockReason: 'triage', findings: [finding({ status: 'blocked', blocked: true, blockReason: 'triage' })] }));
    const banner = r.root.querySelector('quader-package-vulnerabilities .banner-warning')!;
    expect(banner.querySelector(`a[href="${ADVISORY_LINK}"]`)).toBeTruthy();
  });

  it('R-UI-224 a version that is not checked yet says so in public words', async () => {
    const r = await open(report({ evaluation: 'pending_data', findings: [], grade: sampleGrade('N/A', {}, { state: 'pending', repository: 'web/npm-public' }) }));
    expect(text(r.root.querySelector('quader-grade-card')!)).toContain('This version has not been checked for vulnerabilities yet.');
  });
});

describe('The advisory page of a visitor without a session (R-UI-224)', () => {
  it('R-UI-224 lists the versions it affects without a status and offers no link into the report or a finding', async () => {
    const r = await renderScreen(AdvisoryPage, inProject({
      inputs: { ecosystem: 'npm', id: 'GHSA-aaaa-aaaa-0001' },
      setup: (get) => {
        anonymous(get);
        const f = get(VulnerabilitiesApi) as FakeVulnerabilitiesApi;
        const orig = f.advisory.bind(f);
        f.advisory = async (e, id) => ({ ...(await orig(e, id)), truncated: true });
      },
    }, '/projects/web/vulnerabilities'));
    const rows = [...r.root.querySelectorAll('quader-data-table tbody tr')];
    expect(rows).toHaveLength(1);
    expect(text(rows[0]!)).toContain('quader-test-vuln');
    expect(text(rows[0]!)).not.toContain('Needs triage');
    expect(r.root.querySelector('a[href*="/findings/"]')).toBeNull();
    expect(r.root.querySelector('a[href*="advisory="]')).toBeNull();
    expect(text(r.root)).toContain('first 1,000');
    await expectAccessible(r.root);
  });
});

describe('The project of a visitor without a session (R-UI-224)', () => {
  it('R-UI-224 the side bar offers no project report, which needs a session', async () => {
    TestBed.resetTestingModule();
    await renderScreen(AdvisoryPage, inProject({ inputs: { ecosystem: 'npm', id: 'GHSA-aaaa-aaaa-0001' }, setup: anonymous }, '/projects/web/vulnerabilities'));
    const labels = (entries: ReturnType<typeof projectEntries>) => entries.map((e) => e.label);
    expect(labels(projectEntries('web', TestBed.inject(SessionStore)))).not.toContain('Vulnerabilities');
  });
});
