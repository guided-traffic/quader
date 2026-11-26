import { PackagesApi } from '../../api/packages-api';
import { SignaturesApi } from '../../api/signatures-api';
import { FakePackagesApi } from '../../api/testing/fakes-packages';
import { FakeSignaturesApi, repositoryReport, signatureFinding } from '../../api/testing/fakes-signatures';
import { expectAccessible, inProject, renderScreen, settle, text } from '../../testing/screen';
import { SignatureFindings } from '../vulnerabilities/signature-findings';
import { PackageVersionPage } from './package-version';
import { versionRows } from '../../views/packages/package-versions-model';

async function open(setup?: (pkgs: FakePackagesApi, sigs: FakeSignaturesApi) => void, tab?: string) {
  const r = await renderScreen(
    PackageVersionPage,
    inProject({
      inputs: { format: 'npm', name: '@team-a/ui', version: '1.4.0', ...(tab ? { tab } : {}) },
      setup: (get) => setup?.(get(PackagesApi) as FakePackagesApi, get(SignaturesApi) as FakeSignaturesApi),
    }),
  );
  return r;
}

/** Gives the fake version `@team-a/ui 1.4.0` the member `signatures` and its files a badge. */
function signed(pkgs: FakePackagesApi): void {
  pkgs.versionList = pkgs.versionList.map((v) =>
    v.version === '1.4.0'
      ? { ...v, signatures: { state: 'verified_public', stale: false }, files: v.files.map((f) => ({ ...f, signatures: { state: 'verified_public', stale: false } })) }
      : v,
  );
}

describe('The signatures at a version (R-SIG-035)', () => {
  it('R-SIG-035 the header carries the badge and the files a column of it; the tab Signatures shows the details', async () => {
    const r = await open((pkgs) => signed(pkgs), 'files');
    const header = r.root.querySelector('.pkg-meta')!;
    expect(text(header.querySelector('[data-testid="signature-badge"]')!)).toContain('Verified · public');
    expect([...r.root.querySelectorAll('[role="tab"]')].map((t) => text(t))).toContain('Signatures');
    expect(text(r.root.querySelector('.version-tabs table')!)).toContain('Signature');
  });

  it('R-SIG-035 the tab reads the version\'s answer once it is open and lists each repository that holds it', async () => {
    const r = await open(
      (pkgs, sigs) => {
        signed(pkgs);
        sigs.versionReport = { packageId: 'p', version: '1.4.0', repositories: [repositoryReport(), repositoryReport({ repository: 'web/npm-all', class: 'virtual', members: ['web/docker-local'] })] };
      },
      'signatures',
    );
    await settle(r.fixture);
    expect([...r.root.querySelectorAll('h2.sig-repository')].map((h) => text(h))).toEqual(['web/docker-local · Local', 'web/npm-all · Virtual']);
    expect(r.root.querySelectorAll('[data-testid="signature-entry"]')).toHaveLength(2);
    await expectAccessible(r.root);
  });

  it('R-SIG-034 without the member, while the signatures are off, there is no tab and no badge', async () => {
    const r = await open();
    expect([...r.root.querySelectorAll('[role="tab"]')].map((t) => text(t))).not.toContain('Signatures');
    expect(r.root.querySelector('[data-testid="signature-badge"]')).toBeNull();
  });

  it('R-SIG-035 a row of the version list carries the member for its badge', () => {
    const rows = versionRows(
      [{ version: '1.0.0', prerelease: false, publishedAt: '2026-11-21T08:00:00Z', repositories: [], signatures: { state: 'invalid', stale: false } } as never],
      new Map(),
      [],
    );
    expect(rows[0]!.signatures).toEqual({ state: 'invalid', stale: false });
  });
});

describe('The page Signatures under Security audit (R-SIG-038)', () => {
  it('R-SIG-038 an empty list says there are no invalid signatures; the page is named Signatures and has no strip of views', async () => {
    const { root } = await renderScreen(SignatureFindings);
    expect(text(root)).toContain('No invalid signatures.');
    expect(text(root.querySelector('h1')!)).toBe('Signatures');
    expect(root.querySelector('nav.tab-nav')).toBeNull();
  });

  it('R-SIG-038 a row of each kind, with the artifact, the signer, the reason and the repositories; passes axe', async () => {
    const { root } = await renderScreen(SignatureFindings, {
      setup: (get) => {
        (get(SignaturesApi) as FakeSignaturesApi).items = [
          signatureFinding(['web/docker-local', 'platform/images']),
          signatureFinding(['web/files-internal'], {
            id: 42,
            kind: 'blocked',
            scheme: 'openpgp',
            reason: null,
            signer: 'Old build server <build@example.com>',
            block: { id: 3, kind: 'fingerprint', reason: 'The key was copied.', createdAt: '2026-11-21T09:30:00Z' },
            items: [{ repository: 'web/files-internal', format: 'generic', path: 'tools/tool.tar.gz' }],
          }),
        ];
      },
    });
    const rows = [...root.querySelectorAll('tbody tr')].map((tr) => text(tr));
    expect(rows[0]).toContain('portal/web 1.4.0');
    expect(rows[0]).toContain('Invalid signature');
    expect(rows[0]).toContain("the content's digest differs from the signed one");
    expect(rows[0]).toContain('and 1 more');
    expect(rows[1]).toContain('tool.tar.gz');
    expect(rows[1]).toContain('Blocked signer');
    expect(rows[1]).toContain('The key was copied.');
    await expectAccessible(root);
  });
});
