/**
 * Quader's own signatures in the details of a version, an image and a file, for the renders of R-SIG-056's second half: npm's registry
 * signature of a local version as "Origin confirmed by registry · this installation" and the receipt attestation of every artifact. The
 * profile holds no signing identity (renders/signing.ts), so nothing in it is issued by Quader: each state is the real signatures answer
 * of the seed (renders/signatures.ts) changed in the browser by the walk (`answerWith` of screens.ts), as the section Signing identity is
 * (Q-SIG-175, Q-SIG-237). Nothing is written to the database: an issued signature names a row of `signing_identities`, which needs a key
 * sealed by the profile's key ring. The identity is the one of renders/signing.ts; its npm key ID and fingerprint are the upload's there.
 */

type Body = Record<string, unknown>;
type Entry = Record<string, unknown>;

const issuedBy = {
  identity: 3,
  subject: 'CN=Quader signing,O=Nordwind Logistik GmbH,C=DE',
  fingerprint: '8ab2bd4682527e32b5c568365fabc54476a3d52b3f6393468135139cad32636a',
  state: 'current',
};
const npmKeyId = 'SHA256:ppEfO0+zms4YTC+AuPEyWvx63580lKut8w80mgHwf0A';
const predicateType = 'https://github.com/guided-traffic/quader/blob/main/docs/spec/signatures.md#receipt-attestation-v1';
const later = (iso: unknown, seconds: number): string => new Date(Date.parse(String(iso ?? new Date().toISOString())) + seconds * 1000).toISOString();

/** A receipt of this installation at the item it is attached to, decoded as the API answers it (R-SIG-055). */
function receipt(id: number, repository: string, location: string, origin: unknown, over: Entry = {}): Entry {
  const received = (origin as { at?: string } | null)?.at ?? new Date(Date.now() - 86_400_000).toISOString();
  return {
    id,
    role: 'attestation',
    scheme: 'sigstore-bundle',
    state: 'valid',
    reason: null,
    trust: null,
    signer: issuedBy.subject,
    issuer: 'CN=Nordwind Code Signing CA 2026,OU=Corporate PKI,O=Nordwind Logistik GmbH,C=DE',
    identity: 'https://quader.nordwind-logistik.de',
    algorithm: 'ECDSA P-256',
    hashAlgorithm: 'SHA-256',
    keyIds: [],
    claimedAt: later(received, 6),
    receivedAt: later(received, 6),
    log: [],
    timestamps: [],
    predicateType,
    type: predicateType,
    subject: 'ab'.repeat(32),
    repository,
    source: 'issued',
    location,
    attachments: [{ repository, source: 'issued', location }],
    mediaType: 'application/vnd.dev.sigstore.bundle.v0.3+json',
    size: 2611,
    original: `/api/v1/signatures/objects/${'7e'.repeat(32)}`,
    issuedBy,
    attestation: {
      receivedAt: received,
      issuedAt: later(received, 6),
      origin: origin ?? { kind: 'uploaded', principalKind: 'user', principalName: 'priya' },
      signatures: [],
      signaturesOmitted: 0,
      malware: { state: 'clean', database: 'main-62.daily-27450.bytecode-339', databaseBuiltAt: later(received, -14_400), scannedAt: later(received, 4), released: false },
      vulnerabilities: { state: 'evaluated', grade: 'B', counts: { critical: 0, high: 0, medium: 4, low: 2, unknown: 0 }, dataAt: later(received, -21_600) },
    },
    servedAs: 'api',
    ...over,
  };
}

/** The integrity states a receipt names: those of the signatures the answer lists, without their trust (R-SIG-051). */
function named(signatures: readonly Entry[]): Entry[] {
  return signatures
    .filter((s) => s['role'] === 'publisher' || s['role'] === 'provenance')
    .map((s) => ({ role: s['role'], scheme: s['scheme'], integrity: s['state'], ...(s['identity'] ? { identity: s['identity'] } : {}) }));
}

function withReceipt(r: Entry, made: Entry): Entry {
  const att = made['attestation'] as Entry;
  att['signatures'] = named((r['signatures'] as Entry[]) ?? []);
  return { ...r, attestations: [made, ...((r['attestations'] as Entry[]) ?? [])] };
}

/**
 * A local npm version as an installation with an identity answers it: npmjs's registry signature replaced by Quader's own (the version
 * was published here, so the registry is this installation), the provenance as it is, and the tarball's receipt served through the API.
 */
export function issuedVersion(body: Body): Body {
  const repos = (body['repositories'] as Entry[]).map((r) => {
    const published = (r['origin'] as { at?: string } | null)?.at;
    const sigs = ((r['signatures'] as Entry[]) ?? []).map((s) =>
      s['role'] === 'registry'
        ? {
            ...s,
            // Quader checks no signature it made (R-SIG-054): no trust row; signed by the job right after the publish.
            trust: null,
            receivedAt: later(published, 1),
            signer: undefined,
            issuer: undefined,
            keyIds: [npmKeyId],
            keyId: npmKeyId,
            source: 'issued',
            attachments: ((s['attachments'] as Entry[]) ?? []).map((a) => ({ ...a, source: 'issued' })),
            issuedBy,
          }
        : s,
    );
    const tarball = String((sigs.find((s) => s['path']) ?? {})['path'] ?? '');
    return withReceipt({ ...r, signatures: sigs }, receipt(9001, String(r['repository']), tarball, r['origin']));
  });
  return { ...body, repositories: repos };
}

/** The image's receipt: a referrer beside the publisher's signatures (R-SIG-054), or through the API with the reason it gives. */
export function issuedImage(served: 'referrer' | 'hides_signature'): (body: Body) => Body {
  return (body) => ({
    ...body,
    repositories: (body['repositories'] as Entry[]).map((r) => {
      const own = ((r['signatures'] as Entry[]) ?? []).find((s) => !s['platform']);
      const digest = String(own?.['subject'] ?? 'ab'.repeat(32));
      const image = String((own?.['attachments'] as Entry[] | undefined)?.[0]?.['location'] ?? '').split('/_')[0] || 'portal/web';
      const over: Entry = served === 'referrer' ? { servedAs: 'referrer' } : { servedAs: 'api', servedReason: 'hides_signature' };
      return withReceipt(r, receipt(9002, String(r['repository']), `${image}/_manifests/sha256:${digest}`, r['origin'], { subject: digest, ...over }));
    }),
  });
}

/**
 * A generic file's answer with its receipt, which names the three signature files beside it (R-SIG-051). The profile runs no malware
 * scanner, so the file's own fact reads "Checking": its receipt is the one made at the bound of `wait_for_checks` (an hour after the
 * upload) with the malware state `pending` (R-SIG-053), "No verdict yet when it was issued".
 */
export function issuedFile(body: Body): Body {
  const made = receipt(9003, String(body['repository']), String(body['path']), body['origin'], { subject: body['sha256'] });
  const att = made['attestation'] as Entry;
  const issued = later(att['receivedAt'], 3600);
  att['issuedAt'] = issued;
  att['malware'] = { state: 'pending', released: false };
  return withReceipt(body, { ...made, claimedAt: issued, receivedAt: issued });
}
