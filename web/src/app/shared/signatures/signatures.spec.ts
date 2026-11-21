import { Component, signal } from '@angular/core';
import { SignatureBadgeState, SignatureEntry } from '../../api/signatures-api';
import { RepositoriesApi } from '../../api/repositories-api';
import { FakeRepositoriesApi, OCI_FORMAT } from '../../api/testing/fakes-catalog';
import { issuedNpmEntry, repositoryReport, sigstoreEntry } from '../../api/testing/fakes-signatures';
import { expectAccessible, renderScreen, text } from '../../testing/screen';
import { SignatureBadge } from './signature-badge';
import { SignatureReport, SignatureReportView } from './signature-report';
import { certificateWords, entryBadgeState, knownKindsSentence, originWords, schemeWords, signatureBadge } from './signature-words';

@Component({
  selector: 'quader-badge-host',
  imports: [SignatureBadge],
  template: `<quader-signature-badge [state]="state()" [stale]="stale()" />`,
})
class BadgeHost {
  readonly state = signal<SignatureBadgeState | null>(null);
  readonly stale = signal(false);
}

@Component({
  selector: 'quader-report-host',
  imports: [SignatureReport],
  template: `<quader-signature-report [report]="report()" [format]="format()" />`,
})
class ReportHost {
  readonly report = signal<SignatureReportView>(repositoryReport());
  readonly format = signal('oci');
}

async function badge(state: SignatureBadgeState | null, stale = false) {
  const r = await renderScreen(BadgeHost);
  r.fixture.componentInstance.state.set(state);
  r.fixture.componentInstance.stale.set(stale);
  r.fixture.detectChanges();
  return r.root.querySelector<HTMLElement>('[data-testid="signature-badge"]');
}

async function report(view: SignatureReportView, format = 'oci') {
  const r = await renderScreen(ReportHost, {
    setup: (get) => {
      (get(RepositoriesApi) as FakeRepositoriesApi).formatsResult = [OCI_FORMAT];
    },
  });
  r.fixture.componentInstance.report.set(view);
  r.fixture.componentInstance.format.set(format);
  r.fixture.detectChanges();
  await r.fixture.whenStable();
  r.fixture.detectChanges();
  return r.root;
}

describe('The badge of the signatures (R-SIG-009, R-SIG-035)', () => {
  it('R-SIG-035 each of the four states renders its words and its shield; green only for the verified ones, red for an invalid one', async () => {
    const cases: [SignatureBadgeState, string, string, string][] = [
      ['verified_public', 'Verified · public', 'gpp_good', 'badge ok'],
      ['verified_internal', 'Verified · internal', 'gpp_good', 'badge ok'],
      ['untrusted', 'Signed · signer not trusted', 'shield', 'badge'],
      ['invalid', 'Invalid signature', 'gpp_bad', 'badge bad'],
    ];
    for (const [state, word, icon, cls] of cases) {
      const b = await badge(state);
      expect(text(b!)).toBe(`${icon}${word}`);
      expect([...b!.classList].sort().join(' ')).toBe(cls.split(' ').sort().join(' '));
    }
  });

  it('R-SIG-009 the three states without a badge draw nothing', async () => {
    for (const state of ['not_checked', 'unsupported', 'none'] as const) {
      expect(await badge(state)).toBeNull();
    }
    expect(await badge(null)).toBeNull();
  });

  it('R-SIG-012 a stale badge says that the trust is being recomputed', async () => {
    const b = await badge('verified_internal', true);
    expect(b!.getAttribute('title')).toBe('Trust is being recomputed after a change of trust anchors.');
  });
});

describe('The details of the signatures (R-SIG-035)', () => {
  it('R-SIG-035 R-SIG-010 lists a keyless signature with its kind, identity, time proof, certificate, anchor, CI job and the origin, and passes axe', async () => {
    const root = await report(repositoryReport());
    expect(text(root.querySelector('[data-testid="signature-state"]')!)).toContain('Verified · internal');
    const entry = text(root.querySelector('[data-testid="signature-entry"]')!);
    expect(entry).toContain('Publisher signature · Sigstore, keyless');
    expect(entry).toContain('The signature matches the content. A trust anchor knows the signer.');
    expect(entry).toContain('https://gitlab.example.com/platform/portal//.gitlab-ci.yml@refs/heads/main');
    expect(entry).toContain('proven by the transparency log');
    expect(entry).toContain('valid for 10 minutes at signing; covered by the log entry');
    expect(entry).toContain('Company Sigstore · Added by an administrator');
    expect(entry).toContain('https://gitlab.example.com/platform/portal · refs/heads/main');
    expect(root.querySelector<HTMLAnchorElement>('a[download]')!.getAttribute('href')).toMatch(/^\/api\/v1\/signatures\/objects\//);
    expect(text(root.querySelector('[data-testid="signature-origin"]')!)).toContain('Uploaded by priya');
    await expectAccessible(root);
  });

  it('R-SIG-035 a signature carries text, never markup', async () => {
    const root = await report(repositoryReport({ signatures: [sigstoreEntry({ signer: '<img src=x onerror=alert(1)>CN=Evil' })] }));
    expect(root.querySelector('img')).toBeNull();
    expect(text(root)).toContain('<img src=x onerror=alert(1)>CN=Evil');
  });

  it('R-SIG-020 a blocked signer shows the block\'s date and reason instead of the sentence', async () => {
    const blocked = sigstoreEntry({
      trust: { state: 'blocked', anchors: [], block: { id: 3, kind: 'fingerprint', reason: 'The key was copied.', createdAt: '2026-11-21T09:30:00Z' }, computedAt: '2026-11-21T09:31:00Z', stale: false },
    });
    const root = await report(repositoryReport({ state: 'untrusted', signatures: [blocked] }));
    expect(text(root.querySelector('[data-testid="signature-block"]')!)).toContain('Blocked by an administrator on Nov 21, 2026: The key was copied.');
    expect(text(root.querySelector('[data-testid="signature-entry"]')!)).toContain('Signed · signer not trusted');
    // The badge's sentence names the block that decides it, not that no anchor knows the signer.
    const state = text(root.querySelector('[data-testid="signature-state"]')!);
    expect(state).toContain('The signature matches the content, but an administrator blocked its signer on Nov 21, 2026: The key was copied.');
    expect(state).not.toContain('No trust anchor knows this signer');
  });

  it('R-SIG-020 an untrusted signer beside a blocked one keeps the sentence that no anchor knows it', async () => {
    const block = { id: 3, kind: 'fingerprint' as const, reason: 'The key was copied.', createdAt: '2026-11-21T09:30:00Z' };
    const blocked = sigstoreEntry({ trust: { state: 'blocked', anchors: [], block, computedAt: '2026-11-21T09:31:00Z', stale: false } });
    const unknown = sigstoreEntry({ id: 12, trust: { state: 'untrusted', anchors: [], block: null, computedAt: '2026-11-21T09:31:00Z', stale: false } });
    const root = await report(repositoryReport({ state: 'untrusted', signatures: [blocked, unknown] }));
    expect(root.querySelector('[data-testid="signature-blocked-sentence"]')).toBeNull();
    expect(text(root.querySelector('[data-testid="signature-state"]')!)).toContain('No trust anchor knows this signer.');
  });

  it('R-SIG-055 Quader\'s own npm signature reads as the registry\'s origin, never as a publisher\'s signature, and carries no badge', async () => {
    const root = await report(repositoryReport({ repository: 'web/npm-local', signatures: [sigstoreEntry(), issuedNpmEntry()] }), 'npm');
    expect(text(root.querySelector('[data-testid="signature-state"]')!)).toContain('Verified · internal');
    const entries = [...root.querySelectorAll<HTMLElement>('[data-testid="signature-entry"]')];
    const own = entries.find((e) => text(e).includes('Origin confirmed by registry'));
    expect(own).toBeDefined();
    expect(text(own!)).not.toContain('Publisher signature');
    expect(own!.querySelector('[data-testid="signature-badge"]')).toBeNull();
    expect(text(own!)).toContain('SHA256:Zq2bQuaderSigningKey0000000000000000000000');
  });

  it('R-SIG-009 no signature names the kinds the format knows', async () => {
    const root = await report(repositoryReport({ state: 'none', signatures: [], origin: null }));
    expect(text(root.querySelector('[data-testid="signature-state"]')!)).toBe(
      'No signature. No signature found. Signatures this format knows: Notation, Sigstore bundle, cosign signature, provenance attestation.',
    );
  });

  it('R-SIG-030 a Debian package says that apt checks the index and lists the releases that cover it', async () => {
    const root = await report(
      repositoryReport({
        state: 'none',
        signatures: [],
        covering: [{ release: 'dists/bookworm/InRelease', suite: 'bookworm', date: '2026-11-20T00:00:00Z', state: 'verified_public', signatures: [] }],
      }),
      'deb',
    );
    expect(text(root.querySelector('[data-testid="signature-state"]')!)).toContain("apt checks the signature of this repository's index, not of the package.");
    expect(text(root.querySelector('[data-testid="signature-covering"]')!)).toContain('bookworm');
    expect(text(root.querySelector('[data-testid="signature-covering"]')!)).toContain('Verified · public');
  });

  it('R-SIG-024 an image lists its attestations and SBOMs apart, in a table', async () => {
    const sbom = sigstoreEntry({ id: 12, role: 'attestation', type: 'https://spdx.dev/Document', platform: 'linux/amd64', size: 18200 });
    const root = await report(repositoryReport({ attestations: [sbom] }));
    const table = root.querySelector('.sig-attestations table')!;
    expect(text(table)).toContain('https://spdx.dev/Document');
    expect(text(table)).toContain('linux/amd64');
    expect(root.querySelectorAll('[data-testid="signature-entry"]')).toHaveLength(1);
  });
});

describe('The words of the signatures (R-SIG-009, R-SIG-010, R-SIG-035)', () => {
  it('R-SIG-009 only the four states carry a badge', () => {
    expect(signatureBadge('verified_public')?.tone).toBe('ok');
    expect(signatureBadge('untrusted')?.tone).toBe('neutral');
    expect(signatureBadge('invalid')?.tone).toBe('bad');
    expect(signatureBadge('none')).toBeNull();
  });

  it('R-SIG-008 a signature\'s own verdict follows its integrity state and its trust', () => {
    const e = (over: Partial<SignatureEntry>): SignatureEntry => sigstoreEntry(over);
    expect(entryBadgeState(e({}))).toBe('verified_internal');
    expect(entryBadgeState(e({ state: 'no_key', trust: null }))).toBe('untrusted');
    expect(entryBadgeState(e({ state: 'invalid', trust: null }))).toBe('invalid');
    expect(entryBadgeState(e({ state: 'pending', trust: null }))).toBeNull();
    expect(entryBadgeState(issuedNpmEntry())).toBeNull();
    expect(schemeWords(e({}))).toBe('Sigstore, keyless');
    expect(schemeWords(e({ identity: undefined, oidcIssuer: undefined }))).toBe('Sigstore, with a key');
  });

  it('R-SIG-007 a certificate that expired since is information', () => {
    const date = (iso: string) => iso.slice(0, 10);
    const now = Date.parse('2026-11-21T12:00:00Z');
    expect(certificateWords({ certificateNotBefore: '2025-01-01T00:00:00Z', certificateNotAfter: '2026-04-30T00:00:00Z' }, now, date)).toBe('expired on 2026-04-30');
    expect(certificateWords({ certificateNotBefore: '2025-01-01T00:00:00Z', certificateNotAfter: '2028-01-01T00:00:00Z' }, now, date)).toBe('valid until 2028-01-01');
  });

  it('R-SIG-010 the origin of a pipeline names its project, ref and pipeline', () => {
    expect(originWords({ kind: 'uploaded', principalKind: 'ci', principalName: 'ci:portal', ci: { projectPath: 'platform/portal', ref: 'main', pipelineId: '88231' } })).toBe(
      'Uploaded by CI job platform/portal · main · pipeline 88231',
    );
    expect(originWords({ kind: 'fetched', upstreamUrl: 'https://registry.npmjs.org/', at: '2026-11-21T08:00:00Z' })).toBe('Fetched from https://registry.npmjs.org/');
    expect(knownKindsSentence([])).toBe('This format has no signatures.');
  });
});
