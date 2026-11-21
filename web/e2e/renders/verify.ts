/**
 * The step **Verify** of the client setup page (signatures.md R-SIG-056, Q-SIG-71) for the renders. The profile holds no signing identity,
 * so the real answer of the client setup carries no tab `verify`; the walk adds the snippet the server writes while an identity is current
 * (internal/api/v1/setup_verify.go) to the real answer in the browser, as the section **Signing identity** is rendered (renders/signing.ts).
 * The identity's URI is the one of signing.ts.
 */

const identity = 'https://quader.nordwind-logistik.de';
const predicate = 'https://github.com/guided-traffic/quader/blob/main/docs/spec/signatures.md#receipt-attestation-v1';

type Snippet = { id: string; tab: string; fileName: string | null; language: string; content: string; marks: { text: string; kind: string }[] };

/**
 * The client setup's answer with the snippets of the tab `verify` for its client: npm's `npm audit signatures` for the client npm
 * (Q-SIG-163), then an image's cosign command, or a file's.
 */
export function withVerify(body: Record<string, unknown>): Record<string, unknown> {
  const registry = String(body['registryUrl'] ?? '');
  const base = new URL(registry).origin;
  const [project, key] = String(body['repository'] ?? '').split('/');
  const client = String(body['client'] ?? '');
  const trust = [
    `curl -fsSo quader-trusted-root.json ${base}/api/v1/signing/trusted-root.json`,
  ];
  const flags = [
    '  --trusted-root quader-trusted-root.json \\',
    `  --certificate-identity ${identity} \\`,
    "  --certificate-oidc-issuer-regexp '.*' \\",
    '  --insecure-ignore-tlog --insecure-ignore-sct \\',
    `  --type '${predicate}' \\`,
  ];
  const image = body['format'] === 'oci';
  const prefix = `${new URL(registry).host}/${project}/${key}`;
  const lines = image
    ? [...trust, 'cosign verify-attestation \\', ...flags, `  ${prefix}/myapp@sha256:<digest>`]
    : [
        ...trust,
        `curl -fsS -H "Authorization: Bearer \${QUADER_TOKEN}" -o <file>.quader.sigstore.json '${base}/api/v1/projects/${project}/repositories/${key}/attestation?path=<path>'`,
        'cosign verify-blob-attestation \\',
        '  --bundle <file>.quader.sigstore.json \\',
        ...flags,
        '  <file>',
      ];
  const marks = image
    ? [{ text: 'myapp', kind: 'value' }, { text: '<digest>', kind: 'value' }]
    : [{ text: '${QUADER_TOKEN}', kind: 'token' }, { text: '<file>', kind: 'value' }, { text: '<path>', kind: 'value' }];
  const verify: Snippet = { id: `${client}.verify.attestation`, tab: 'verify', fileName: null, language: 'shell', content: lines.join('\n') + '\n', marks };
  const audit: Snippet[] =
    client === 'npm' ? [{ id: 'npm.verify.signatures', tab: 'verify', fileName: null, language: 'shell', content: 'npm audit signatures\n', marks: [] }] : [];
  return { ...body, snippets: [...((body['snippets'] as Snippet[] | undefined) ?? []), ...audit, verify] };
}
