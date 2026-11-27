import { signal } from '@angular/core';
import { PackagesApi } from '../../api/packages-api';
import { commonFixture } from '../../api/testing/fakes-ansible-docs';
import { FakePackagesApi, ansibleDocsFixture, ansibleFixture } from '../../api/testing/fakes-packages';
import { FakeVulnerabilitiesApi, sampleGrade } from '../../api/testing/fakes-vulnerabilities';
import { VulnerabilitiesApi } from '../../api/vulnerabilities-api';
import { REPOSITORIES_SHOWN } from '../../core/repositories-shown';
import { PackageDetail } from '../../features/packages/package-detail';
import { PackageVersionPage } from '../../features/packages/package-version';
import { inProject, renderScreen, text } from '../../testing/screen';

/*
 * A Bastion whose switch Hide repositories is on binds REPOSITORIES_SHOWN to false (Q-UI-1632): the shared views then name no
 * repository where a visitor reads, while the commands keep the key a client needs (R-UI-269). The main installation's pages, composed
 * here with the token set, stand for the Bastion's, which compose the same views.
 */

const HIDDEN = [{ provide: REPOSITORIES_SHOWN, useValue: signal(false) }];
type Setup = (fake: FakePackagesApi, vulns: FakeVulnerabilitiesApi) => void;

async function packagePage(setup: Setup) {
  return renderScreen(
    PackageDetail,
    inProject({
      providers: HIDDEN,
      inputs: { format: 'npm', name: '@team-a/ui' },
      setup: (get) => setup(get(PackagesApi) as FakePackagesApi, get(VulnerabilitiesApi) as FakeVulnerabilitiesApi),
    }),
  );
}

async function versionPage(inputs: Record<string, unknown>, setup: Setup) {
  return renderScreen(
    PackageVersionPage,
    inProject({
      providers: HIDDEN,
      inputs: { format: 'npm', name: '@team-a/ui', version: '1.4.0', ...inputs },
      setup: (get) => setup(get(PackagesApi) as FakePackagesApi, get(VulnerabilitiesApi) as FakeVulnerabilitiesApi),
    }),
  );
}

describe('The views of a Bastion that hides its repositories', () => {
  it('R-UI-269 the views name no repository while REPOSITORIES_SHOWN is false', async () => {
    // The package's page: no column Repositories where the versions differ in their holders, no fact, a hint without them.
    const pkg = await packagePage((fake) => {
      const v = fake.versionList.find((x) => x.packageId === 'pkg_01JBQ6V7S3K9' && x.version === '1.3.0')!;
      v.repositories = [{ key: 'web/npm-remote', class: 'remote', publishedAt: v.publishedAt, available: true }];
    });
    const heads = [...pkg.root.querySelectorAll('.pkg-versions thead th')].map((th) => text(th));
    expect(heads).not.toContain('Repositories');
    expect(heads).toContain('Actions');
    expect([...pkg.root.querySelectorAll('.pkg-details dt')].map((dt) => text(dt))).not.toContain('Repositories');
    expect(text(pkg.root.querySelector('.pkg-details')!)).toContain("The package's keywords and when it was first published.");
    expect(pkg.root.querySelector('a[href^="/projects/web/repositories/"]')).toBeNull();

    // A version's Details: no table of the repositories that hold it; a remote's refusal without its key and its tab Upstream.
    const details = await versionPage({ tab: 'details' }, (fake) => {
      const v = fake.versionList.find((x) => x.packageId === 'pkg_01JBQ6V7S3K9' && x.version === '1.4.0')!;
      v.repositories = [
        ...v.repositories,
        { key: 'web/npm-remote', class: 'remote', publishedAt: v.publishedAt, available: false, unavailableReason: 'blocked' },
      ];
    });
    expect([...details.root.querySelectorAll('.version-tabs h2')].map((h) => text(h))).not.toContain('Repositories');
    const note = details.root.querySelector('[data-testid="delivery-note"]')!;
    expect(text(note)).toBe('blockNot delivered: a rule of the repository blocks it. A client cannot install this version through it.');
    expect(note.querySelector('a')).toBeNull();

    // The findings: the first repository's card alone, without its key as a heading.
    const grade = sampleGrade('B', { medium: 2 }, { fixable: 1 });
    const card = (repository: string) => ({ repository, evaluation: grade.evaluation, blocked: false, blockReason: null, findings: [], grade });
    const findings = await versionPage({ tab: 'vulnerabilities' }, (_, vulns) =>
      vulns.versionReports.set('pkg_01JBQ6V7S3K9/1.4.0', {
        packageId: 'pkg_01JBQ6V7S3K9',
        version: '1.4.0',
        repositories: [card('web/npm-local'), card('web/npm-all')],
      }),
    );
    expect(findings.root.querySelectorAll('.vuln-repo')).toHaveLength(1);
    expect(findings.root.querySelector('.vuln-repo h2')).toBeNull();
    // The list of its findings names no key either. (The card's link "Findings in …" to the project's report is a signed-in reader's,
    // never a Bastion visitor's.)
    expect(text(findings.root.querySelector('quader-version-findings')!)).not.toContain('web/npm-');

    // An Ansible collection's documentation held by two repositories: where it comes from, not which repository.
    const docs = await versionPage({ format: 'ansible', name: 'acme.tools', version: '1.2.0' }, (fake) => {
      ansibleFixture(fake);
      for (const v of fake.versionList) {
        if (v.format === 'ansible') v.repositories.push({ key: 'web/ansible-all', class: 'virtual', publishedAt: v.publishedAt, available: true });
      }
      ansibleDocsFixture(fake, commonFixture({ source: 'upstream' }));
    });
    const source = docs.root.querySelector('[data-testid="docs-source"]')!;
    expect(text(source)).toBe('Taken from the upstream');
  });
});
