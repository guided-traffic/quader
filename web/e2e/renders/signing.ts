/**
 * The answers of Quader's signing identity for the renders of the section **Signing identity** (R-SIG-056). The profile holds no
 * company CA and no identity, so the real answer of `GET /api/v1/signing/identity` is the state without one; every other state is
 * the real answer changed in the browser by the walk (`answerWith` of screens.ts), as the scanner replicas' databases are. Nothing
 * is written to the database: an uploaded identity needs a key sealed by the profile's key ring and a certificate for it, which only
 * the API makes. The material is public data of a company's CA as an installation would show it: the subjects, the fingerprints of
 * certificates and the npm key IDs of P-256 keys made once with openssl for these renders.
 */

const DAY = 86_400_000;
const at = (days: number): string => new Date(Date.now() + days * DAY).toISOString();

const subject = 'CN=Quader signing,O=Nordwind Logistik GmbH,C=DE';
const issuer = 'CN=Nordwind Code Signing CA 2026,OU=Corporate PKI,O=Nordwind Logistik GmbH,C=DE';
const uri = 'https://quader.nordwind-logistik.de';

/** A certificate request of a P-256 key for the company's subject, as the API answers it for a pending key. */
const csr = `-----BEGIN CERTIFICATE REQUEST-----
MIIBhzCCAS4CAQAwZjEXMBUGA1UEAwwOUXVhZGVyIHNpZ25pbmcxHzAdBgNVBAoM
Fk5vcmR3aW5kIExvZ2lzdGlrIEdtYkgxHTAbBgNVBAsMFFBsYXRmb3JtIEVuZ2lu
ZWVyaW5nMQswCQYDVQQGEwJERTBZMBMGByqGSM49AgEGCCqGSM49AwEHA0IABJXU
grWD3jgTR9j7FdRx9rQxjwgfD29fao5KlfQvaCZIYaXca39Jow+Sb4S/XcFDhl4k
/PSuauQMRGdF7ThWkzSgZjBkBgkqhkiG9w0BCQ4xVzBVMC4GA1UdEQQnMCWGI2h0
dHBzOi8vcXVhZGVyLm5vcmR3aW5kLWxvZ2lzdGlrLmRlMA4GA1UdDwEB/wQEAwIH
gDATBgNVHSUEDDAKBggrBgEFBQcDAzAKBggqhkjOPQQDAgNHADBEAiBSF/C8p7kv
lXtX6YpKJYdTGC0D+2SRMXiPZIlRYfoCyQIgWRtAoNSJeeUCv+8tVtfJGVTSTkPf
wrEbyRdAYBN1QNo=
-----END CERTIFICATE REQUEST-----
`;

type Identity = Record<string, unknown>;

function identity(over: Identity): Identity {
  return {
    id: 3,
    source: 'upload',
    state: 'current',
    subject,
    issuer,
    names: [uri],
    notBefore: at(-353),
    notAfter: at(12),
    fingerprint: '8ab2bd4682527e32b5c568365fabc54476a3d52b3f6393468135139cad32636a',
    npmKeyId: 'SHA256:ppEfO0+zms4YTC+AuPEyWvx63580lKut8w80mgHwf0A',
    keySha256: 'a3f19c0d5b7e2846c1d09e7f3a5b8c2d4e6f8091a2b3c4d5e6f708192a3b4c5d',
    csr: null,
    createdAt: at(-354),
    createdBy: 'user:ada',
    announcedAt: at(-353),
    currentAt: at(-353),
    switchAt: null,
    expiresAt: null,
    retiredAt: null,
    ...over,
  };
}

/** The two previous identities of an installation that rotated twice: the Helm values' first pair, then an upload. */
const previous = (): Identity[] => [
  identity({
    id: 2,
    state: 'previous',
    notBefore: at(-718),
    notAfter: at(-353),
    fingerprint: '5431e7e6654eeec4166d7a61038359f0fea612be8864634ee4c68314e29e0e86',
    npmKeyId: 'SHA256:ZlbdKqOUs4F9nMSXHX7198axScB1KHUUZX3rkeRI3Jo',
    createdAt: at(-719),
    createdBy: 'user:priya.raman',
    currentAt: at(-718),
    expiresAt: at(-353),
  }),
  identity({
    id: 1,
    source: 'configuration',
    state: 'previous',
    issuer: 'CN=Nordwind Code Signing CA 2024,OU=Corporate PKI,O=Nordwind Logistik GmbH,C=DE',
    names: [uri, 'pki@nordwind-logistik.de'],
    notBefore: at(-810),
    notAfter: at(-700),
    fingerprint: '966071f45defd9df92eb1a14bfecf7fb97012979d3ec02bffcc3eff27a2ab731',
    npmKeyId: 'SHA256:qn+DNPj6oaSc6M1FPVvjGz/hKt8art0pr1TQxbpfZEo',
    createdAt: at(-810),
    createdBy: null,
    currentAt: at(-810),
    expiresAt: at(-718),
  }),
];

const backlog = [
  { kind: 'npm', due: 0, failed: 0, oldestDueAt: null },
  { kind: 'attestation', due: 0, failed: 0, oldestDueAt: null },
];

/** The states the walk renders, each as a change of the real answer. */
export type SigningState = 'pending' | 'upload' | 'helm' | 'helm-refused';

export function signingAnswer(state: SigningState): (body: Record<string, unknown>) => Record<string, unknown> {
  return (body) => {
    const base = { ...body, state: 'none', managed: false, current: null, announced: null, pending: null, previous: [], refused: null, backlog };
    switch (state) {
      case 'pending':
        return {
          ...base,
          pending: identity({
            id: 4,
            state: 'pending',
            subject: 'CN=Quader signing,OU=Platform Engineering,O=Nordwind Logistik GmbH,C=DE',
            issuer: '',
            notBefore: null,
            notAfter: null,
            fingerprint: '',
            npmKeyId: 'SHA256:7/H0KjW795YpM9mdW5QTPUsCyMRBbOwhHqPCM7ioij4',
            csr,
            createdAt: at(-0.003),
            announcedAt: null,
            currentAt: null,
          }),
        };
      case 'upload':
        return { ...base, state: 'upload', current: identity({}), previous: previous() };
      case 'helm':
      case 'helm-refused':
        return {
          ...base,
          state: 'configuration',
          managed: true,
          current: identity({
            id: 5,
            source: 'configuration',
            notBefore: at(-60),
            notAfter: at(30),
            issuer: 'CN=Nordwind Issuing CA,OU=cert-manager,O=Nordwind Logistik GmbH,C=DE',
            fingerprint: '820dbe1220a158d6ef787c75050c2173ac88ac169ae2e1535a06781fa1c6e6f9',
            npmKeyId: 'SHA256:ISu+iDKOfY9JJFMD5zGAISVJmkBu3lvfj/QmNqbl8p4',
            createdAt: at(-60),
            createdBy: null,
            currentAt: at(-60),
          }),
          previous: previous(),
          refused:
            state === 'helm-refused'
              ? {
                  reason: "the leaf's extended key usage lacks codeSigning, which every verifier of an attestation requires (code_signing)",
                  fingerprint: '3b9f0c5e1d7a2486c0e19f7b3d5a8c2e4f6a8091b2c3d4e5f6a7b8c9d0e1f2a3',
                  at: at(-0.08),
                }
              : null,
        };
    }
  };
}
