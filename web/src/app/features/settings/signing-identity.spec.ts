import { ApiError } from '../../api/problem';
import { SigningApi } from '../../api/signing-api';
import { FakeSigningApi, daysFromNow, pendingIdentity, signingIdentity, signingOverview } from '../../api/testing/fakes-signing';
import { button, buttonByText, expectAccessible, renderScreen, settle, text, unavailableReason } from '../../testing/screen';
import { SigningIdentitySection } from './signing-identity';

const overlay = (): HTMLElement => document.querySelector('.cdk-overlay-container') as HTMLElement;
const WRITER = ['settings:read', 'security-settings:write'];
const READER = ['settings:read'];

async function render(capabilities: readonly string[], setup?: (api: FakeSigningApi) => void) {
  const r = await renderScreen(SigningIdentitySection, {
    capabilities,
    setup: (get) => setup?.(get(SigningApi) as FakeSigningApi),
  });
  return { ...r, api: r.get(SigningApi) as FakeSigningApi };
}

/** The section's controls by their words: the copy buttons of a fact and the request's own bar left out, icon glyphs dropped. */
const labels = (root: ParentNode): string[] =>
  [...root.querySelectorAll<HTMLElement>('button, a.btn')]
    .filter((b) => !b.closest('quader-copy-button, quader-code-window'))
    .map((b) => {
      const copy = b.cloneNode(true) as HTMLElement;
      copy.querySelectorAll('.material-icons').forEach((i) => i.remove());
      return text(copy);
    });

/** An upload, current, that ends in 12 days, with two previous identities (R-SIG-056's render state). */
const uploaded = (api: FakeSigningApi): void => {
  api.current = signingOverview({
    state: 'upload',
    current: signingIdentity({ notAfter: daysFromNow(12) }),
    previous: [
      signingIdentity({ id: 2, state: 'previous', fingerprint: '5431e7e6654eeec4166d7a61038359f0fea612be8864634ee4c68314e29e0e86', expiresAt: daysFromNow(-30) }),
      signingIdentity({ id: 1, state: 'previous', source: 'configuration', createdBy: null, fingerprint: '966071f45defd9df92eb1a14bfecf7fb97012979d3ec02bffcc3eff27a2ab731', notAfter: daysFromNow(-2), expiresAt: daysFromNow(-200) }),
    ],
  });
};

describe('Signing identity (R-SIG-056)', () => {
  it('R-SIG-056 without an identity: the sentence, Create signing key and Import behind More options for the writer; passes axe', async () => {
    const { root } = await render(WRITER);
    expect(text(root.querySelector('[data-testid="signing-none"]')!)).toBe(
      'No signing identity: Quader signs nothing. Packages of local npm repositories carry no registry signature, and no artifact gets a receipt attestation.',
    );
    expect(labels(root)).toEqual(['Create signing key', 'More options', 'Import key and certificate']);
    await expectAccessible(root);
  });

  it('R-SIG-056 read-only without the right: the sentence, the request of a pending key, and no control', async () => {
    const none = await render(READER);
    expect(none.root.querySelector('[data-testid="signing-none"]')).not.toBeNull();
    expect(labels(none.root)).toEqual([]);
    const pending = await render(READER, (api) => (api.current = signingOverview({ pending: pendingIdentity() })));
    expect(text(pending.root.querySelector('[data-testid="signing-pending"]')!)).toContain('CN=Quader signing,OU=Platform Engineering');
    expect(pending.root.querySelector('quader-code-window pre')!.textContent).toContain('BEGIN CERTIFICATE REQUEST');
    expect(labels(pending.root)).toEqual([]);
  });

  it('R-SIG-047 a pending key: its request with Copy and Download, the sentence about the CA, Upload certificate and Discard', async () => {
    const { fixture, root, api } = await render(WRITER, (fake) => (fake.current = signingOverview({ pending: pendingIdentity() })));
    const window = root.querySelector('quader-code-window')!;
    expect(window.querySelector('[aria-label="Download quader-signing.csr"]')).not.toBeNull();
    expect(window.querySelector('quader-copy-button')).not.toBeNull();
    expect(text(root)).toContain("Have your company's CA issue a code-signing certificate for this request, then upload it with its chain.");
    expect(labels(root)).toContain('Upload certificate');
    buttonByText(root, 'Discard').click();
    await settle(fixture);
    buttonByText(overlay(), 'Discard').click();
    await settle(fixture);
    expect(api.calls).toEqual([{ op: 'discardPending' }]);
    expect(root.querySelector('[data-testid="signing-none"]')).not.toBeNull();
  });

  it('R-SIG-056 uploaded: the facts with the validity amber 12 days ahead, Replace and Remove; Previous identities with Retire and the spec\'s question', async () => {
    const { fixture, root, api } = await render(WRITER, uploaded);
    const facts = text(root.querySelector('[data-testid="signing-facts-current"]')!);
    expect(facts).toContain('CN=Quader signing,O=Nordwind Logistik GmbH,C=DE');
    expect(facts).toContain('CN=Nordwind Code Signing CA 2026');
    expect(facts).toContain('https://quader.nordwind-logistik.de');
    expect(facts).toContain('8ab2 bd46 8252 7e32');
    expect(facts).toContain('SHA256:ppEfO0+zms4YTC+AuPEyWvx63580lKut8w80mgHwf0A');
    expect(facts).toContain('Current: signs');
    expect(text(root.querySelector('[data-testid="signing-expiring"]')!)).toBe('scheduleexpires in 12 days');
    expect(root.querySelector('[data-testid="signing-expired"]')).toBeNull();
    expect(text(root.querySelector('[data-testid="signing-source"]')!)).toContain('Uploaded in the admin area by ada');
    expect(labels(root).slice(0, 4)).toEqual(['Download chain', 'Download trusted root', 'Replace', 'Remove']);
    expect(root.querySelector<HTMLAnchorElement>('a[download="quader-signing-chain.pem"]')!.getAttribute('href')).toBe('/api/v1/signing/chain.pem');
    const rows = root.querySelectorAll('quader-disclosure quader-data-table tbody tr');
    expect(rows.length).toBe(2);
    expect(text(rows[1]!)).toContain('expired');
    buttonByText(rows[0] as HTMLElement, 'Retire').click();
    await settle(fixture);
    expect(text(overlay())).toContain(
      "Retire this identity?Its key leaves npm's keys at once; Quader signs its packages and attestations again with the current identity.",
    );
    buttonByText(overlay(), 'Retire').click();
    await settle(fixture);
    expect(api.calls).toEqual([{ op: 'retire', body: { id: 2 } }]);
  });

  it('R-SIG-056 the job\'s backlog of both kinds is one quiet line beside a current identity, and no line without one and without work', async () => {
    const busy = await render(READER, (api) => {
      uploaded(api);
      api.current = {
        ...api.current,
        backlog: [
          { kind: 'npm', due: 3, failed: 0, oldestDueAt: daysFromNow(-0.003) },
          { kind: 'attestation', due: 1204, failed: 2, oldestDueAt: daysFromNow(-0.01) },
        ],
      };
    });
    expect(text(busy.root.querySelector('[data-testid="signing-backlog"]')!)).toMatch(
      /^Waiting to be signed: 3 npm versions and 1,204 receipt attestations, the oldest since .+\. 2 reached their last attempt; the job tries again once a day\.$/,
    );
    const idle = await render(READER, uploaded);
    expect(text(idle.root.querySelector('[data-testid="signing-backlog"]')!)).toBe('Nothing waits to be signed.');
    const none = await render(READER);
    expect(none.root.querySelector('[data-testid="signing-backlog"]')).toBeNull();
  });

  it('R-SIG-056 an expired certificate reads "expired" in red; an auditor sees the facts and the previous identities without a control', async () => {
    const { root } = await render(READER, (api) => {
      uploaded(api);
      api.current = { ...api.current, current: signingIdentity({ notAfter: daysFromNow(-1) }) };
    });
    expect(text(root.querySelector('[data-testid="signing-expired"]')!)).toBe('errorexpired');
    expect(root.querySelector('[data-testid="signing-expiring"]')).toBeNull();
    expect(labels(root)).toEqual(['Download chain', 'Download trusted root', 'Previous identities 2']);
  });

  it('R-SIG-046 R-UI-270 from the Helm configuration: the facts, Replace and Remove unavailable with the key; a refused pair as a red line', async () => {
    const { fixture, root, api } = await render(WRITER, (api) => {
      api.current = signingOverview({
        state: 'configuration',
        managed: true,
        current: signingIdentity({ source: 'configuration', createdBy: null }),
        refused: {
          reason: "the leaf's extended key usage lacks codeSigning, which every verifier of an attestation requires (code_signing)",
          fingerprint: null,
          at: daysFromNow(-0.1),
        },
      });
    });
    // The source in its words, no paragraph of where it is changed: the acts' tooltip says it.
    expect(text(root.querySelector('[data-testid="signing-source"]')!)).toBe('From the Helm configuration');
    expect(labels(root)).toEqual(['Download chain', 'Download trusted root', 'Replace', 'Remove']);
    const fixed = 'Set in signing.identity of the Helm configuration: change or remove it there.';
    expect(unavailableReason(buttonByText(root, 'Replace'))).toBe(fixed);
    expect(unavailableReason(buttonByText(root, 'Remove'))).toBe(fixed);
    buttonByText(root, 'Remove').click();
    await settle(fixture);
    expect(overlay().querySelector('quader-confirm-dialog')).toBeNull();
    expect(api.calls).toEqual([]);
    // An auditor reads the same without a control.
    const auditor = await render(READER, (a) => (a.current = signingOverview({ state: 'configuration', managed: true, current: signingIdentity({ source: 'configuration', createdBy: null }) })));
    expect(labels(auditor.root)).toEqual(['Download chain', 'Download trusted root']);
    const refused = text(root.querySelector('[data-testid="signing-refused"]')!);
    expect(refused).toContain('The certificate is not for code signing: ask the CA for the extended key usage codeSigning.');
    expect(refused).toContain('(code_signing).');
    expect(refused).toContain('The identity in use keeps signing.');
  });

  it('R-SIG-046 R-UI-270 named by the Helm configuration before a pair passes: Create signing key unavailable, nothing behind More options', async () => {
    const { root } = await render(WRITER, (api) => (api.current = signingOverview({ state: 'configuration', managed: true })));
    expect(text(root.querySelector('[data-testid="signing-managed"]')!)).toBe(
      'From the Helm configuration: Quader signs nothing until a pair of its files passes the checks.',
    );
    expect(labels(root)).toEqual(['Create signing key']);
    expect(unavailableReason(buttonByText(root, 'Create signing key'))).toBe(
      'Set in signing.identity of the Helm configuration: change or remove it there.',
    );
    await expectAccessible(root);
  });

  it('R-SIG-047 Create signing key: the fields hold the defaults, More options holds the rest, and a refused chain names its check under the field', async () => {
    const { fixture, root, api } = await render(WRITER);
    buttonByText(root, 'Create signing key').click();
    await settle(fixture);
    const dialog = overlay().querySelector('quader-signing-key-dialog')!;
    expect(dialog.querySelector<HTMLInputElement>('input[name="cn"]')!.value).toBe('Quader signing');
    expect(dialog.querySelector<HTMLInputElement>('input[name="uri"]')!.value).toBe('https://quader.example.net');
    expect(dialog.querySelector('quader-disclosure input[name="ou"]')).not.toBeNull();
    expect(dialog.querySelector('quader-disclosure input[name="o"]')).toBeNull();
    const o = dialog.querySelector<HTMLInputElement>('input[name="o"]')!;
    o.value = 'Nordwind Logistik GmbH';
    o.dispatchEvent(new Event('input'));
    await settle(fixture);
    button(dialog as HTMLElement, 'Create signing key').click();
    await settle(fixture);
    expect(api.calls[0]).toEqual({
      op: 'createKey',
      body: { subject: { CN: 'Quader signing', O: 'Nordwind Logistik GmbH' }, uris: ['https://quader.example.net'], emails: [] },
    });
    // The request is shown at once; its certificate is refused by a check of R-SIG-045.
    api.failure = new ApiError(
      422,
      { code: 'validation_failed', requestId: 'rid-1', errors: [{ pointer: '/chain', code: 'invalid_value', detail: 'the chain does not end in a self-signed root: add the root certificate last, or in the CA field (chain_root)' }] },
      'rid-1',
    );
    buttonByText(root, 'Upload certificate').click();
    await settle(fixture);
    const upload = overlay().querySelector('quader-signing-certificate-dialog')!;
    const chain = upload.querySelector<HTMLTextAreaElement>('textarea[name="chain"]')!;
    chain.value = '-----BEGIN CERTIFICATE-----\nMIIB\n-----END CERTIFICATE-----\n';
    chain.dispatchEvent(new Event('input'));
    await settle(fixture);
    button(upload as HTMLElement, 'Upload certificate').click();
    await settle(fixture);
    expect(text(upload.querySelector('.field-error')!)).toContain('The chain does not end in a self-signed root. Add the root certificate last');
    expect(text(upload.querySelector('.field-error')!)).toContain('(chain_root).');
  });
});
