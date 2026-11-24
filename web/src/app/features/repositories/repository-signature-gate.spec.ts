import { ApiError } from '../../api/problem';
import { PackagesApi } from '../../api/packages-api';
import { RepositorySignatureSettings, SignaturesApi } from '../../api/signatures-api';
import { FakePackagesApi } from '../../api/testing/fakes-packages';
import { FakeSignaturesApi, gatePreview, signatureAnchor, signatureRefusal } from '../../api/testing/fakes-signatures';
import { button, buttonByText, expectAccessible, inProject, openSection, renderScreen, settle, text } from '../../testing/screen';
import { PackageVersionPage } from '../packages/package-version';
import { SignatureRefusals } from '../vulnerabilities/signature-refusals';
import { RepositorySignaturesSection } from './repository-signatures';
import { gateOrder } from './signature-gate-order';

const overlay = (): HTMLElement => document.querySelector('.cdk-overlay-container') as HTMLElement;
/** The words of an element without the glyph names of its icons. */
const words = (el: Element): string => {
  const copy = el.cloneNode(true) as Element;
  copy.querySelectorAll('.material-icons').forEach((i) => i.remove());
  return text(copy);
};
/** A row's cells, each by its words. */
const cells = (tr: Element): string[] => [...tr.children].map((c) => words(c));
const ADMIN = ['repositories:read-config', 'repositories:write-config', 'settings:read', 'content:metadata-read', 'vulnerabilities:triage'];
/** A project Owner holds no capability: the Owner role of `authz/me` alone, which the section receives as canChange without canWeaken. */
const OWNER: readonly string[] = [];

/** The anchors of the project `web`'s scope: the company CA and the release key of the installation, the team's own key. */
function scope(api: FakeSignaturesApi): void {
  api.current = { ...api.current, projectAnchors: true };
  api.anchorList = [
    signatureAnchor(),
    signatureAnchor({ id: 8, key: 'release-key', title: 'Release key', kind: 'openpgp', fingerprints: [{ kind: 'openpgp', value: 'C0FFEE'.padEnd(40, '0') }] }),
  ];
  api.projectList = { web: [signatureAnchor({ id: 9, scope: 'project', project: 'web', key: 'team-key', title: 'Team web signing key', kind: 'public-key' })] };
}

async function section(
  stored: Partial<RepositorySignatureSettings>,
  over: { format?: string; repoClass?: string; canWeaken?: boolean; capabilities?: readonly string[]; setup?: (api: FakeSignaturesApi) => void } = {},
) {
  const r = await renderScreen(RepositorySignaturesSection, {
    capabilities: over.capabilities ?? ADMIN,
    inputs: {
      repository: 'web/files-internal',
      project: 'web',
      format: over.format ?? 'generic',
      repoClass: over.repoClass ?? 'local',
      stored: { fetch: true, onlyVerified: false, ...stored },
      canChange: true,
      canWeaken: over.canWeaken ?? true,
    },
    setup: (get) => {
      const api = get(SignaturesApi) as FakeSignaturesApi;
      scope(api);
      over.setup?.(api);
    },
  });
  await openSection(r.fixture, r.root, 'Signatures');
  await settle(r.fixture);
  return { ...r, api: r.get(SignaturesApi) as FakeSignaturesApi };
}

const box = (root: HTMLElement): HTMLInputElement =>
  [...root.querySelectorAll<HTMLInputElement>('input[type="checkbox"]')].find((i) => text(i.closest('label')!).startsWith('Only artifacts'))!;

describe('The gate of a repository in its settings (R-SIG-074, R-SIG-061)', () => {
  it('R-SIG-074 off by default: the box unchecked with its sentence, no anchors, and the preview of switching it on; passes axe', async () => {
    const { root } = await section({}, { setup: (api) => (api.previewAnswer = gatePreview({ pass: 940, refused: { ...gatePreview().refused, missing: 60 } })) });
    expect(box(root).checked).toBe(false);
    expect(text(root)).toContain('A signature says who signed an artifact, not that it is safe.');
    expect(root.querySelector('[data-testid="gate-anchors"]')).toBeNull();
    expect(text(root.querySelector('[data-testid="gate-state"]')!)).toBe('Switched on with the stored anchors: 940 of 1,000 artifacts pass. 60 are refused.');
    await expectAccessible(root);
  });

  it('R-SIG-074 on with any anchor: the warning that the public Sigstore counts, and the stored state with its dashboard link', async () => {
    const { root } = await section({ onlyVerified: true, anchors: [] });
    expect(box(root).checked).toBe(true);
    expect(words(root.querySelector('[data-testid="gate-any-warning"]')!)).toBe(
      'This includes the public Sigstore, where anyone can sign. Choose anchors to accept only your own signers.',
    );
    expect(text(root.querySelector('[data-testid="gate-state"]')!)).toContain('1,000 of 1,000 artifacts pass. 0 are refused. Refused downloads');
  });

  it('R-SIG-074 chosen anchors list the scope with kind, source and fingerprint, and a removed reference in red', async () => {
    const { root } = await section({ onlyVerified: true, anchors: ['installation/company-ca', 'installation/gone-key'] });
    const rows = [...root.querySelectorAll('[data-testid="gate-anchor-list"] li')].map((li) =>
      [...li.querySelectorAll('.choice-text > span')].map((x) => text(x)).join(' '),
    );
    expect(rows).toEqual([
      'Company code signing CA CA certificate · Added by an administrator · CN=Example Code Signing CA, O=Example GmbH',
      'Release key OpenPGP key · Added by an administrator · C0FFEE00…00000000',
      "Team web signing key Public key · Added by the project's Owner · CN=Example Code Signing CA, O=Example GmbH",
      'Removed trust anchor installation/gone-key',
    ]);
    const ticked = [...root.querySelectorAll<HTMLInputElement>('[data-testid="gate-anchor-list"] input')].map((i) => i.checked);
    expect(ticked).toEqual([true, false, false, true]);
    expect(root.querySelector('[data-testid="gate-any-warning"]')).toBeNull();
  });

  it('R-SIG-061 an Owner sees a gate that is on read-only, may narrow it by a reference and may not add one', async () => {
    const { fixture, root } = await section(
      { onlyVerified: true, anchors: ['installation/company-ca', 'installation/release-key'] },
      { canWeaken: false, capabilities: OWNER },
    );
    expect(box(root).disabled).toBe(true);
    expect(text(root.querySelector('[data-testid="gate-locked"]')!)).toBe('Only an administrator or someone who triages security findings can loosen this.');
    const any = root.querySelector<HTMLInputElement>('input[type="radio"]')!;
    expect(any.disabled).toBe(true);
    const ticks = [...root.querySelectorAll<HTMLInputElement>('[data-testid="gate-anchor-list"] input')];
    expect(ticks.map((i) => i.disabled)).toEqual([false, false, true]);
    ticks[0]!.click();
    await settle(fixture);
    // One reference stays: the last one cannot be taken away, which would mean any anchor.
    expect(ticks[1]!.disabled).toBe(true);
  });

  it('R-SIG-074 R-SIG-063 a Debian repository says why it offers nothing; checks switched off say that the setting does not apply', async () => {
    const deb = await section({}, { format: 'deb' });
    expect(text(deb.root.querySelector('[data-testid="gate-deb"]')!)).toBe(
      "apt checks the signature of this repository's index, not of a package. This setting is not available for Debian repositories.",
    );
    expect(deb.root.querySelector('input')).toBeNull();
    const off = await section({ onlyVerified: true }, { setup: (api) => (api.previewAnswer = new ApiError(409, { code: 'signatures_not_enabled', requestId: 'r' }, 'r')) });
    expect(words(off.root.querySelector('[data-testid="gate-checks-off"]')!)).toBe('Signature checks are switched off in this installation. This setting does not apply.');
  });

  it('R-SIG-074 switching on asks first with the preview\'s numbers per reason and its examples, then saves the members', async () => {
    const { fixture, root, api } = await section({}, {
      setup: (fake) =>
        (fake.previewAnswer = gatePreview({
          artifacts: 4812,
          pass: 3410,
          pending: 12,
          refused: { missing: 1180, untrusted: 0, notChosen: 0, invalid: 3, blocked: 207, notChecked: 0 },
          examples: [{ repository: 'web/files-internal', path: 'tools/cli-1.2.0.tar.gz', package: null, version: null, reason: 'missing' }],
        })),
    });
    const saved = vi.spyOn(fixture.componentInstance.save, 'emit');
    box(root).click();
    await settle(fixture);
    button(root, /^\s*Save changes/).click();
    await settle(fixture);
    const dialog = text(overlay());
    expect(dialog).toContain(
      '1,390 of 4,812 artifacts in this repository have no verified signature that this setting accepts and will be refused. 12 are still being checked.',
    );
    expect(dialog).toContain("1,180 · No publisher's signature");
    expect(dialog).toContain('207 · Signer blocked');
    expect(dialog).toContain("tools/cli-1.2.0.tar.gz · No publisher's signature");
    expect(api.calls.at(-1)).toEqual({ op: 'preview', body: { repository: 'web/files-internal', anchors: [] } });
    buttonByText(overlay(), 'Save and refuse them').click();
    await settle(fixture);
    expect(saved).toHaveBeenCalledWith({ onlyVerified: true, anchors: [] });
  });

  it('R-SIG-061 the order: on narrows, off and a swap weaken, a reference taken from a list narrows', () => {
    expect(gateOrder({ on: false, anchors: [] }, { on: true, anchors: [] })).toBe('narrowing');
    expect(gateOrder({ on: true, anchors: ['a'] }, { on: false, anchors: ['a'] })).toBe('weakening');
    expect(gateOrder({ on: true, anchors: ['a', 'b'] }, { on: true, anchors: ['a'] })).toBe('narrowing');
    expect(gateOrder({ on: true, anchors: ['a'] }, { on: true, anchors: ['b'] })).toBe('weakening');
    expect(gateOrder({ on: true, anchors: [] }, { on: true, anchors: ['a'] })).toBe('narrowing');
    expect(gateOrder({ on: false, anchors: ['a'] }, { on: false, anchors: [] })).toBe('none');
  });
});

/** Gives `@team-a/ui 1.4.0`'s tarball the member `signatures` of a gate's decision. */
function gated(pkgs: FakePackagesApi, refused: boolean, reason: 'missing' | 'pending'): void {
  pkgs.versionList = pkgs.versionList.map((v) =>
    v.version === '1.4.0'
      ? {
          ...v,
          signatures: { state: 'none', stale: false, refused, refusedReason: reason },
          files: v.files.map((f) => ({ ...f, signatures: { state: 'none', stale: false, refused, refusedReason: reason } })),
        }
      : v,
  );
}

async function version(capabilities: readonly string[], refused: boolean, reason: 'missing' | 'pending') {
  return renderScreen(
    PackageVersionPage,
    inProject({ capabilities, inputs: { format: 'npm', name: '@team-a/ui', version: '1.4.0', tab: 'files' }, setup: (get) => gated(get(PackagesApi) as FakePackagesApi, refused, reason) }),
  );
}

describe('A refused artifact at its version and in the dashboard (R-SIG-074)', () => {
  it('R-SIG-074 a refused file has a banner in the words of R-SIG-068, "Refused here" with its reason and no Download', async () => {
    const { root } = await version(['content:metadata-read'], true, 'missing');
    expect(text(root.querySelector('[data-testid="signature-banner"] p')!)).toBe(
      'Downloads are refused in web/npm-local: it serves only artifacts with a verified signature, and @team-a/ui 1.4.0 has no signature of its publisher or its build.',
    );
    expect(words(root.querySelector('[data-testid="signature-refused"]')!.closest('td')!)).toBe("Refused here No publisher's signature");
    expect(root.querySelector('.version-tabs a[download]')).toBeNull();
    // Release for this repository is the holder of the triage right's.
    expect([...root.querySelectorAll('button')].map((b) => text(b))).not.toContain('Release for this repository web/npm-local');
  });

  it('R-SIG-074 a pending file shows nothing more than its state: no banner, and Download stays', async () => {
    const { root } = await version(ADMIN, false, 'pending');
    expect(root.querySelector('[data-testid="signature-banner"]')).toBeNull();
    expect(root.querySelector('.version-tabs a[download]')).not.toBeNull();
  });

  it('R-SIG-070 the holder of the triage right releases from the banner: the dialog names the repository and asks for a reason', async () => {
    const { fixture, root, get } = await version(ADMIN, true, 'missing');
    button(root, 'Release for this repository web/npm-local').click();
    await settle(fixture);
    expect(text(overlay().querySelector('[data-testid="release-refusal"]')!)).toBe(
      'web/npm-local serves only artifacts with a verified signature and refuses @team-a/ui 1.4.0. It has no signature of its publisher or its build.',
    );
    const api = get(SignaturesApi) as FakeSignaturesApi;
    buttonByText(overlay(), 'Release for this repository').click();
    await settle(fixture);
    expect(text(overlay())).toContain('A release needs a reason.');
    expect(api.calls.filter((c) => c.op === 'release')).toEqual([]);
  });

  it('R-SIG-074 the dashboard lists refused downloads with Release for this repository for its holders only, and says when there are none', async () => {
    const auditor = await renderScreen(SignatureRefusals, {
      capabilities: ['content:metadata-read', 'vulnerabilities:triage'],
      setup: (get) => ((get(SignaturesApi) as FakeSignaturesApi).refusalList = [signatureRefusal(), signatureRefusal({ sha256: 'b'.repeat(64), release: { reason: 'Hotfix', releasedBy: 'user:noor', releasedAt: '2026-11-21T09:50:00Z' } })]),
    });
    const rows = [...auditor.root.querySelectorAll('[data-testid="signature-refusals"] tbody tr')].map((tr) => cells(tr));
    expect(rows[0]?.slice(0, 3)).toEqual(['portal/api 1.4.0portal/api/_tags/1.4.0', 'web/docker-local', "No publisher's signature"]);
    expect(rows[0]?.at(-2)).toBe('14');
    expect(rows[0]?.at(-1)).toContain('Release for this repository');
    expect(rows[1]?.[2]).toContain('Released by noor');
    expect(rows[1]?.at(-1)).toContain('Withdraw release');
    const reader = await renderScreen(SignatureRefusals, { capabilities: ['content:metadata-read'] });
    expect(text(reader.root.querySelector('[data-testid="signature-refusals"]')!)).toContain('No refused downloads.');
    expect([...reader.root.querySelectorAll('button')].map((b) => text(b))).not.toContain('Release for this repository');
  });
});
