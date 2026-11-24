import { createHash } from 'node:crypto';
import { manyVersionsPackage } from './advisories';
import { Api, expectStatus, lit, mainProject, nugetPackage, otherProject, patient, psql, repoId } from './seed';
import { anchorMaterial } from './signature-material';

/**
 * The signatures' data of the renders (R-SIG-035 to R-SIG-038): trust anchors of every kind and scope, the switch for project Owners on,
 * a block, and signatures in each of the four visible states and the three without a badge, on an npm version with a registry signature
 * and a provenance, an image with a Notation and a Sigstore signature, a provenance attestation and an SBOM, a NuGet author signature, a
 * generic file with three signature files beside it uploaded by a pipeline with its claims, and findings of both kinds.
 *
 * The anchors and the block go through the API, which checks their material as an installation's (the material is public and made
 * once, signature-material.ts). No signing key exists in the profile, so the signatures are written into its database (`psql`), keyed
 * by the blobs of the seeded items, with their verdicts and the trust rows under the current anchor epoch, as the worker's jobs would
 * have left them; the jobs then have nothing to do with them (not due, trust not stale). A signature file of the generic file is the
 * file itself (its blob is the object), so the commit hook's own record of it is the same row. Everything is idempotent.
 */

/** The image with the signatures, its attestations and an SBOM, and the one signed with a key no anchor holds. */
export const signedImage = { name: 'portal/web', tag: '1.4.0', untrusted: 'portal/api' } as const;
/** The versions of {@link manyVersionsPackage} in each state (R-SIG-009). */
export const signedVersions = {
  public: '3.9.0',
  internal: '4.0.0-rc.2',
  blocked: '3.8.0',
  invalid: '3.7.0',
  pending: '4.0.0-rc.1',
  none: '3.5.0',
} as const;
/** The generic file with three signatures beside it, in the main project's assets. */
export const signedFile = { repo: 'assets', folder: 'releases/portal-cli/2.0.0', name: 'portal-cli-2.0.0.tar.gz' } as const;
const filePath = `${signedFile.folder}/${signedFile.name}`;
const siblings = {
  asc: { path: `${filePath}.asc`, content: '-----BEGIN PGP SIGNATURE-----\n\nrenders: the release key\'s signature of portal-cli 2.0.0\n-----END PGP SIGNATURE-----\n' },
  sigstore: { path: `${filePath}.sigstore.json`, content: '{"mediaType":"application/vnd.dev.sigstore.bundle.v0.3+json","renders":"portal-cli 2.0.0"}\n' },
  jws: { path: `${filePath}.jws.sig`, content: 'renders: the Notation signature of portal-cli 2.0.0\n' },
} as const;

const tarball = (version: string): string => `${manyVersionsPackage.name}/-/${manyVersionsPackage.name}-${version}.tgz`;
const nupkg = (version: string): string => {
  const id = nugetPackage.name.toLowerCase();
  return `${id}/${version}/${id}.${version}.nupkg`;
};

/** The anchors of the renders: installation, project and one of the Helm values (written by SQL as `quader migrate` would). */
const anchors: readonly { readonly body: Record<string, unknown>; readonly project?: string }[] = [
  {
    body: {
      key: 'example-code-signing',
      title: 'Example Code Signing CA',
      kind: 'ca',
      ca: { certificates: anchorMaterial.caChain, use: 'codeSigning', subject: { O: 'Example GmbH' } },
      note: 'The company CA that issues the code-signing certificates of the release pipelines.',
    },
  },
  {
    body: {
      key: 'company-sigstore',
      title: 'Company Sigstore',
      kind: 'sigstore',
      sigstore: { issuer: 'https://gitlab.example.com', identityPattern: '^https://gitlab\\.example\\.com/.+//\\.gitlab-ci\\.yml@refs/heads/main$', trustRoot: 'public' },
    },
  },
  { body: { key: 'release-key-2026', title: 'Release key 2026', kind: 'openpgp', openpgp: { keys: anchorMaterial.release } } },
  {
    body: {
      key: 'portal-release-signing-key-of-the-platform-engineering-team',
      title: 'Customer portal release signing key of the platform engineering team',
      kind: 'openpgp',
      openpgp: { keys: anchorMaterial.long },
    },
  },
  {
    body: {
      key: 'build-server-key',
      title: 'Build server key',
      kind: 'public-key',
      publicKey: { keys: [{ pem: anchorMaterial.publicKey, keyid: anchorMaterial.keyid, expires: '2031-01-01T00:00:00Z' }] },
    },
  },
  { project: mainProject, body: { key: 'web-team-release-key', title: 'Web team release key', kind: 'openpgp', openpgp: { keys: anchorMaterial.web } } },
];

/** The blocked identity: a branch whose pipeline signed with the company's Sigstore before it was retired. */
const blockedIdentity = '^https://gitlab\\.example\\.com/web/chart-render-kit//\\.gitlab-ci\\.yml@refs/heads/legacy-build$';

/**
 * Switches project anchors on, adds the anchors (an existing key answers 409 and is kept) and the block, and uploads the generic file
 * with its three signature files. After the content and before {@link seedSignatures}.
 */
export async function seedSignatureSettings(admin: Api): Promise<void> {
  const current = await admin.get('/api/v1/signatures/settings');
  await expectStatus(current, [200], 'signature settings');
  if (!((await current.json()) as { projectAnchors: boolean }).projectAnchors) {
    const res = await admin.patch('/api/v1/signatures/settings', { projectAnchors: true }, { 'If-Match': current.headers()['etag'] ?? '*' });
    await expectStatus(res, [200], 'project anchors on');
  }
  for (const a of anchors) {
    const url = a.project ? `/api/v1/projects/${a.project}/signatures/anchors` : '/api/v1/signatures/anchors';
    const res = await patient(() => admin.post(url, a.body));
    await expectStatus(res, [201, 409], `anchor ${String(a.body['key'])}`);
  }
  const block = await patient(() =>
    admin.post('/api/v1/signatures/blocks', {
      kind: 'identity',
      issuer: 'https://gitlab.example.com',
      value: blockedIdentity,
      reason: 'The legacy-build branch was retired on 2026-11-18; its runner image was rebuilt from an untrusted base.',
    }),
  );
  await expectStatus(block, [201, 409], 'block');
  for (const f of [{ path: filePath, content: 'renders: portal-cli 2.0.0\n' }, ...Object.values(siblings)]) {
    const url = `/api/v1/projects/${mainProject}/repositories/${signedFile.repo}/content/${f.path.split('/').map(encodeURIComponent).join('/')}`;
    const res = await patient(() => admin.put(url, Buffer.from(f.content), { 'Content-Type': 'application/octet-stream' }));
    await expectStatus(res, [200, 201], `upload ${f.path}`);
  }
}

type Role = 'publisher' | 'provenance' | 'registry' | 'attestation';
type Trust = { readonly state: 'public' | 'internal' | 'untrusted' | 'blocked'; readonly anchors?: readonly string[]; readonly block?: boolean };

interface Seeded {
  /** A name of the renders' own, from which the object's digest is made, unless `file` names the stored signature file. */
  readonly name: string;
  readonly project: string;
  readonly repo: string;
  /** The covered file's path in the repository; its blob is the subject. */
  readonly subjectPath: string;
  /** npm's registry signature: the subject is the signed message and its name `<name>@<version>`. */
  readonly subjectName?: string;
  readonly file?: string;
  readonly role: Role;
  readonly scheme: string;
  readonly mediaType: string;
  readonly state: 'valid' | 'invalid' | 'no_key' | 'pending' | 'unsupported';
  readonly reason?: string;
  readonly predicateType?: string;
  readonly signer?: Record<string, unknown>;
  readonly keyIds?: readonly string[];
  readonly timeProof?: 'log' | 'timestamp';
  /** Hours ago it was received and, with a proof, signed. */
  readonly hoursAgo: number;
  readonly source: string;
  readonly location: string;
  readonly trust?: Trust;
  /** The trust row in a project's scope beside the installation's. */
  readonly projectTrust?: Trust & { readonly project: string };
}

const at = (hours: number, minutes = 0): Date => new Date(Date.now() - hours * 3_600_000 - minutes * 60_000);
const iso = (d: Date): string => d.toISOString();

/** A keyless Sigstore certificate of ten minutes, issued for a CI job, and the log entry that proves its time. */
function keyless(identity: string, issuer: string, ci: Record<string, string>, hours: number, fulcio: string, extra: Record<string, unknown> = {}) {
  const signed = at(hours, 1);
  return {
    identity,
    oidcIssuer: issuer,
    issuer: fulcio,
    certificateNotBefore: iso(signed),
    certificateNotAfter: iso(new Date(signed.getTime() + 600_000)),
    chain: [`CN=${identity}`, fulcio, fulcio.replace('intermediate', 'root').replace('Fulcio', 'Fulcio root')],
    ci: { issuer, ...ci },
    log: [{ logId: 'c0d23d6ad406973f9559f3ba2d1ca01f84147d8ffc5b8445c224f98b9591801d', index: 152340000 + hours, integratedTime: iso(signed), proof: 'proof' }],
    algorithm: 'ECDSA P-256',
    hashAlgorithm: 'SHA-256',
    ...extra,
  };
}

const githubRelease = (version: string) =>
  keyless(
    `https://github.com/acme-oss/chart-render-kit/.github/workflows/release.yml@refs/tags/v${version}`,
    'https://token.actions.githubusercontent.com',
    {
      buildSignerUri: `https://github.com/acme-oss/chart-render-kit/.github/workflows/release.yml@refs/tags/v${version}`,
      sourceRepositoryUri: 'https://github.com/acme-oss/chart-render-kit',
      sourceRepositoryRef: `refs/tags/v${version}`,
      runInvocationUri: 'https://github.com/acme-oss/chart-render-kit/actions/runs/11893274651/attempts/1',
      buildTrigger: 'push',
    },
    30,
    'CN=sigstore-intermediate, O=sigstore.dev',
    { predicateType: 'https://slsa.dev/provenance/v1', statementSubjects: [`pkg:npm/chart-render-kit@${version}`] },
  );

const gitlabJob = (project: string, ref: string, job: string, hours: number, extra: Record<string, unknown> = {}) =>
  keyless(
    `https://gitlab.example.com/${project}//.gitlab-ci.yml@refs/heads/${ref}`,
    'https://gitlab.example.com',
    {
      sourceRepositoryUri: `https://gitlab.example.com/${project}`,
      sourceRepositoryRef: `refs/heads/${ref}`,
      runInvocationUri: `https://gitlab.example.com/${project}/-/jobs/${job}`,
      buildConfigUri: `https://gitlab.example.com/${project}//.gitlab-ci.yml@refs/heads/${ref}`,
      buildTrigger: 'push',
    },
    hours,
    'CN=Example Sigstore Fulcio intermediate, O=Example GmbH',
    extra,
  );

/** A Notation signature of the company CA's leaf, with a timestamp. */
const notationLeaf = (cn: string, hours: number) => ({
  subject: `CN=${cn}, OU=Web team, O=Example GmbH, C=DE`,
  issuer: 'CN=Example Code Signing CA, O=Example GmbH, C=DE',
  serial: '4f2a9c71e03b5d88',
  // Relative to the render's clock, so that the leaf reads "valid until …" on every day, not "expired on …" after a fixed date.
  certificateNotBefore: iso(at(300 * 24)),
  certificateNotAfter: iso(at(-160 * 24)),
  chain: [`CN=${cn}, OU=Web team, O=Example GmbH, C=DE`, 'CN=Example Code Signing CA, O=Example GmbH, C=DE', 'CN=Example Root CA, O=Example GmbH, C=DE'],
  signingScheme: 'notary.x509',
  signingAgent: 'notation-go/1.3.2',
  timestamps: [{ time: iso(at(hours, 2)), authority: 'CN=Example Timestamping Authority, O=Example GmbH, C=DE' }],
  algorithm: 'ECDSA P-256',
  hashAlgorithm: 'SHA-256',
});

const npmVendor = { project: mainProject, repo: manyVersionsPackage.repo } as const;
const images = { project: mainProject, repo: 'images' } as const;
const nuget = { project: otherProject, repo: nugetPackage.repo } as const;
const assets = { project: mainProject, repo: signedFile.repo } as const;
const manifestOf = (image: string, tag: string): string => `${image}/_tags/${tag}`;
const registryKey = 'SHA256:jl3bwswu80PjjokCgh0o2w5c2U4LhQAE57gj9cz1kzA';

const signatures: readonly Seeded[] = [
  // npm: the newest version, with npm's registry signature and a provenance of the public Sigstore (Verified · public).
  {
    name: 'npm-provenance-public',
    ...npmVendor,
    subjectPath: tarball(signedVersions.public),
    role: 'provenance',
    scheme: 'sigstore-bundle',
    mediaType: 'application/vnd.dev.sigstore.bundle.v0.3+json',
    state: 'valid',
    predicateType: 'https://slsa.dev/provenance/v1',
    signer: githubRelease(signedVersions.public),
    keyIds: ['3c1e9f5aa08d2b67e4f1a0c9d8b7e6f5a4b3c2d1e0f9a8b7c6d5e4f3a2b1c0d9'],
    timeProof: 'log',
    hoursAgo: 30,
    source: 'document',
    location: `${manyVersionsPackage.name}#/versions/${signedVersions.public}/dist/attestations`,
    trust: { state: 'public', anchors: ['sigstore-public'] },
  },
  {
    name: 'npm-registry-signature',
    ...npmVendor,
    subjectPath: tarball(signedVersions.public),
    subjectName: `${manyVersionsPackage.name}@${signedVersions.public}`,
    role: 'registry',
    scheme: 'npm-registry',
    mediaType: 'application/json',
    state: 'valid',
    signer: { subject: 'registry.npmjs.org', keyId: registryKey, algorithm: 'ECDSA P-256', hashAlgorithm: 'SHA-256' },
    keyIds: [registryKey],
    hoursAgo: 30,
    source: 'document',
    location: `${manyVersionsPackage.name}#/versions/${signedVersions.public}/dist/signatures/0`,
    trust: { state: 'public', anchors: ['npmjs'] },
  },
  // npm: a pre-release with a provenance of the company's Sigstore for a GitLab job (Verified · internal).
  {
    name: 'npm-provenance-internal',
    ...npmVendor,
    subjectPath: tarball(signedVersions.internal),
    role: 'provenance',
    scheme: 'sigstore-bundle',
    mediaType: 'application/vnd.dev.sigstore.bundle.v0.3+json',
    state: 'valid',
    predicateType: 'https://slsa.dev/provenance/v1',
    signer: gitlabJob('web/chart-render-kit', 'main', '88231', 6, { predicateType: 'https://slsa.dev/provenance/v1' }),
    keyIds: ['9d4c2b1a0f8e7d6c5b4a39281706f5e4d3c2b1a09f8e7d6c5b4a392817060f5e'],
    timeProof: 'log',
    hoursAgo: 6,
    source: 'upload',
    location: `chart-render-kit/-/chart-render-kit-${signedVersions.internal}.sigstore`,
    trust: { state: 'internal', anchors: ['company-sigstore'] },
  },
  // npm: a provenance of a retired branch whose identity an administrator blocked (Signed · signer not trusted, with the block).
  {
    name: 'npm-provenance-blocked',
    ...npmVendor,
    subjectPath: tarball(signedVersions.blocked),
    role: 'provenance',
    scheme: 'sigstore-bundle',
    mediaType: 'application/vnd.dev.sigstore.bundle.v0.3+json',
    state: 'valid',
    predicateType: 'https://slsa.dev/provenance/v1',
    signer: gitlabJob('web/chart-render-kit', 'legacy-build', '80114', 200),
    keyIds: ['77aa01bb02cc03dd04ee05ff0611227733448855996600aa11bb22cc33dd44ee'],
    timeProof: 'log',
    hoursAgo: 200,
    source: 'upload',
    location: `chart-render-kit/-/chart-render-kit-${signedVersions.blocked}.sigstore`,
    trust: { state: 'blocked', anchors: ['company-sigstore'], block: true },
  },
  // npm: a provenance that names another tarball (Invalid signature).
  {
    name: 'npm-provenance-invalid',
    ...npmVendor,
    subjectPath: tarball(signedVersions.invalid),
    role: 'provenance',
    scheme: 'sigstore-bundle',
    mediaType: 'application/vnd.dev.sigstore.bundle.v0.3+json',
    state: 'invalid',
    reason: 'subject_mismatch',
    predicateType: 'https://slsa.dev/provenance/v1',
    signer: gitlabJob('web/chart-render-kit', 'main', '79903', 260),
    timeProof: 'log',
    hoursAgo: 260,
    source: 'upload',
    location: `chart-render-kit/-/chart-render-kit-${signedVersions.invalid}.sigstore`,
  },
  // npm: a provenance that waits for its check (no badge, Not checked yet).
  {
    name: 'npm-provenance-pending',
    ...npmVendor,
    subjectPath: tarball(signedVersions.pending),
    role: 'provenance',
    scheme: 'sigstore-bundle',
    mediaType: 'application/vnd.dev.sigstore.bundle.v0.3+json',
    state: 'pending',
    hoursAgo: 0,
    source: 'upload',
    location: `chart-render-kit/-/chart-render-kit-${signedVersions.pending}.sigstore`,
  },
  // OCI: the image with a Notation signature of the company CA, a keyless signature, a provenance attestation and an SBOM.
  {
    name: 'oci-notation',
    ...images,
    subjectPath: manifestOf(signedImage.name, signedImage.tag),
    role: 'publisher',
    scheme: 'notation-jws',
    mediaType: 'application/jose+json',
    state: 'valid',
    signer: notationLeaf('Portal release signing', 20),
    keyIds: ['b1f3a6c9e2d5f8a1b4c7e0d3f6a9c2e5b8d1f4a7c0e3b6d9f2a5c8e1b4d7f0a3'],
    timeProof: 'timestamp',
    hoursAgo: 20,
    source: 'referrer',
    location: `${signedImage.name}/_manifests/sha256:4b8c1d2e3f405162738495a6b7c8d9e0f1a2b3c4d5e6f708192a3b4c5d6e7f80`,
    trust: { state: 'internal', anchors: ['example-code-signing'] },
  },
  {
    name: 'oci-sigstore',
    ...images,
    subjectPath: manifestOf(signedImage.name, signedImage.tag),
    role: 'publisher',
    scheme: 'sigstore-bundle',
    mediaType: 'application/vnd.dev.sigstore.bundle.v0.3+json',
    state: 'valid',
    signer: gitlabJob('web/customer-portal-frontend', 'main', '88412', 20),
    keyIds: ['0a1b2c3d4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f60718293a4b5c6d7e8f9'],
    timeProof: 'log',
    hoursAgo: 20,
    source: 'referrer',
    location: `${signedImage.name}/_manifests/sha256:5c9d2e3f405162738495a6b7c8d9e0f1a2b3c4d5e6f708192a3b4c5d6e7f8091`,
    trust: { state: 'internal', anchors: ['company-sigstore'] },
  },
  {
    name: 'oci-provenance',
    ...images,
    subjectPath: manifestOf(signedImage.name, signedImage.tag),
    role: 'provenance',
    scheme: 'sigstore-bundle',
    mediaType: 'application/vnd.dev.sigstore.bundle.v0.3+json',
    state: 'valid',
    predicateType: 'https://slsa.dev/provenance/v1',
    signer: gitlabJob('web/customer-portal-frontend', 'main', '88412', 20, { predicateType: 'https://slsa.dev/provenance/v1' }),
    keyIds: ['1b2c3d4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f60718293a4b5c6d7e8f90a'],
    timeProof: 'log',
    hoursAgo: 20,
    source: 'referrer',
    location: `${signedImage.name}/_manifests/sha256:6dae3f405162738495a6b7c8d9e0f1a2b3c4d5e6f708192a3b4c5d6e7f8091a2`,
    trust: { state: 'internal', anchors: ['company-sigstore'] },
  },
  {
    name: 'oci-sbom',
    ...images,
    subjectPath: manifestOf(signedImage.name, signedImage.tag),
    role: 'attestation',
    scheme: 'unknown',
    mediaType: 'application/spdx+json',
    state: 'unsupported',
    reason: 'scheme_unknown',
    predicateType: 'https://spdx.dev/Document',
    hoursAgo: 20,
    source: 'referrer',
    location: `${signedImage.name}/_manifests/sha256:7ebf405162738495a6b7c8d9e0f1a2b3c4d5e6f708192a3b4c5d6e7f8091a2b3`,
  },
  {
    name: 'oci-vuln-scan',
    ...images,
    subjectPath: manifestOf(signedImage.name, signedImage.tag),
    role: 'attestation',
    scheme: 'sigstore-bundle',
    mediaType: 'application/vnd.dev.sigstore.bundle.v0.3+json',
    state: 'valid',
    predicateType: 'https://cosign.sigstore.dev/attestation/vuln/v1',
    signer: gitlabJob('web/customer-portal-frontend', 'main', '88413', 19),
    timeProof: 'log',
    hoursAgo: 19,
    source: 'referrer',
    location: `${signedImage.name}/_manifests/sha256:8fc05162738495a6b7c8d9e0f1a2b3c4d5e6f708192a3b4c5d6e7f8091a2b3c4`,
    trust: { state: 'internal', anchors: ['company-sigstore'] },
  },
  // OCI: an image signed by cosign with a key no anchor holds (Signed · signer not trusted).
  {
    name: 'oci-cosign-key',
    ...images,
    subjectPath: manifestOf(signedImage.untrusted, '1.4.0'),
    role: 'publisher',
    scheme: 'cosign-legacy',
    mediaType: 'application/vnd.dev.cosign.simplesigning.v1+json',
    state: 'no_key',
    signer: { dockerReference: 'quader.example.com/web-frontend/images/portal/api', algorithm: 'ECDSA P-256', hashAlgorithm: 'SHA-256' },
    keyIds: ['e6f0c2a9d41b7735a0c9b2e1f4d7a3c6b9e2f5a8c1d4e7f0a3b6c9d2e5f8a1b4'],
    hoursAgo: 50,
    source: 'cosign_tag',
    location: `${signedImage.untrusted}/_tags/sha256-0000000000000000000000000000000000000000000000000000000000000000.sig`,
    trust: { state: 'untrusted' },
  },
  // NuGet: an author signature of the company CA (Verified · internal), and a package changed after it was signed (Invalid).
  {
    name: 'nuget-author',
    ...nuget,
    subjectPath: nupkg(nugetPackage.versions[1]),
    role: 'publisher',
    scheme: 'nuget-cms',
    mediaType: 'application/pkcs7-signature',
    state: 'valid',
    signer: { ...notationLeaf('Platform libraries signing', 40), signingScheme: undefined, signingAgent: undefined, commitmentType: 'author' },
    keyIds: ['c2e5a8b1d4f7c0e3a6b9d2f5c8e1a4b7d0f3c6e9a2b5d8f1c4e7a0b3d6f9c2e5'],
    timeProof: 'timestamp',
    hoursAgo: 40,
    source: 'embedded',
    location: `${nupkg(nugetPackage.versions[1])}#.signature.p7s`,
    trust: { state: 'internal', anchors: ['example-code-signing'] },
  },
  {
    name: 'nuget-author-invalid',
    ...nuget,
    subjectPath: nupkg(nugetPackage.versions[2]),
    role: 'publisher',
    scheme: 'nuget-cms',
    mediaType: 'application/pkcs7-signature',
    state: 'invalid',
    reason: 'digest_mismatch',
    signer: { ...notationLeaf('Platform libraries signing', 10), signingScheme: undefined, signingAgent: undefined, commitmentType: 'author' },
    hoursAgo: 10,
    source: 'embedded',
    location: `${nupkg(nugetPackage.versions[2])}#.signature.p7s`,
  },
  // generic: three signature files beside the file, each its own kind, all known by anchors of the installation.
  {
    name: 'generic-asc',
    ...assets,
    subjectPath: filePath,
    file: siblings.asc.path,
    role: 'publisher',
    scheme: 'openpgp',
    mediaType: 'application/pgp-signature',
    state: 'valid',
    signer: { subject: 'Release key 2026 <release@example.com>', fingerprint: anchorMaterial.releaseId, keyId: anchorMaterial.releaseId, algorithm: 'EdDSA', hashAlgorithm: 'SHA-256' },
    keyIds: [anchorMaterial.releaseId],
    hoursAgo: 3,
    source: 'sibling',
    location: siblings.asc.path,
    trust: { state: 'internal', anchors: ['release-key-2026'] },
    projectTrust: { project: mainProject, state: 'internal', anchors: ['release-key-2026', 'web-team-release-key'] },
  },
  {
    name: 'generic-sigstore',
    ...assets,
    subjectPath: filePath,
    file: siblings.sigstore.path,
    role: 'publisher',
    scheme: 'sigstore-bundle',
    mediaType: 'application/vnd.dev.sigstore.bundle.v0.3+json',
    state: 'valid',
    signer: gitlabJob('acme/customer-portal/portal-cli', 'main', '4471029', 3),
    keyIds: ['2c3d4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f60718293a4b5c6d7e8f90a1b'],
    timeProof: 'log',
    hoursAgo: 3,
    source: 'sibling',
    location: siblings.sigstore.path,
    trust: { state: 'internal', anchors: ['company-sigstore'] },
  },
  {
    name: 'generic-jws',
    ...assets,
    subjectPath: filePath,
    file: siblings.jws.path,
    role: 'publisher',
    scheme: 'notation-jws',
    mediaType: 'application/jose+json',
    state: 'valid',
    signer: notationLeaf('Portal CLI release signing', 3),
    keyIds: ['d3f6a9c2e5b8d1f4a7c0e3b6d9f2a5c8e1b4d7f0a3c6e9b2d5f8a1c4e7b0d3f6'],
    timeProof: 'timestamp',
    hoursAgo: 3,
    source: 'sibling',
    location: siblings.jws.path,
    trust: { state: 'internal', anchors: ['example-code-signing'] },
  },
];

const hex = (s: string): string => createHash('sha256').update(s).digest('hex');
const bytea = (h: string): string => `decode('${h}', 'hex')`;
const jsonb = (v: unknown): string => `${lit(JSON.stringify(v))}::jsonb`;
const textArray = (xs: readonly string[]): string => `ARRAY[${xs.map(lit).join(', ')}]::text[]`;

/** The blob of a live file over a path of a repository, as SQL. */
const blobOf = (project: string, repo: string, path: string): string =>
  `(SELECT i.blob_sha256 FROM items i WHERE i.repo_id = ${repoId(project, repo)} AND i.path = ${lit(path)} AND i.kind = 'file' AND i.trashed_at IS NULL)`;

function trustSql(sig: string, t: Trust, project: string | null): string {
  const projectId = project === null ? 'NULL::uuid' : `(SELECT id FROM project WHERE key = ${lit(project)})`;
  const ids = `ARRAY(SELECT a.id FROM signature_anchors a WHERE a.key = ANY(${textArray(t.anchors ?? [])}) ORDER BY a.id)::bigint[]`;
  const block = t.block ? `(SELECT b.id FROM signature_blocks b WHERE b.value = ${lit(blockedIdentity)})` : 'NULL::bigint';
  return `
INSERT INTO signature_trust (signature_id, project_id, state, anchor_ids, block_id, anchor_epoch, computed_at)
SELECT ${sig}, ${projectId}, ${lit(t.state)}, ${ids}, ${block}, (SELECT anchor_epoch FROM signature_settings WHERE id), now() - interval '10 minutes'
 WHERE ${sig} IS NOT NULL
ON CONFLICT (signature_id, project_id) DO UPDATE SET state = EXCLUDED.state, anchor_ids = EXCLUDED.anchor_ids, block_id = EXCLUDED.block_id,
       anchor_epoch = EXCLUDED.anchor_epoch, computed_at = EXCLUDED.computed_at;`;
}

/** The package or image name an attachment is under; none for a generic file. */
function attachmentName(s: Seeded): string {
  if (s.subjectPath.includes('/_tags/')) return lit(s.subjectPath.split('/_tags/')[0]!);
  if (s.repo === manyVersionsPackage.repo) return lit(manyVersionsPackage.name);
  if (s.repo === nugetPackage.repo) return lit(nugetPackage.name);
  return 'NULL';
}

function signatureSql(s: Seeded): string {
  const raw = s.file ? null : JSON.stringify({ renders: s.name, mediaType: s.mediaType });
  const contentOfFile = s.file ? Object.values(siblings).find((x) => x.path === s.file)!.content : null;
  const objectHex = raw !== null ? hex(raw) : hex(contentOfFile!);
  const subject = s.subjectName ? bytea(hex(`${s.subjectName}:sha512-renders`)) : blobOf(s.project, s.repo, s.subjectPath);
  const received = iso(at(s.hoursAgo));
  const proven = s.timeProof ? `${lit(iso(at(s.hoursAgo, 1)))}::timestamptz` : 'NULL';
  const checked = s.state === 'pending' ? 'NULL' : `${lit(iso(at(s.hoursAgo, -1)))}::timestamptz`;
  const nameSql = s.subjectName ? lit(s.subjectName) : 'NULL';
  const sig = `(SELECT id FROM signatures WHERE object_sha256 = ${bytea(objectHex)} AND part = 0 AND subject_sha256 = ${subject} AND subject_name IS NOT DISTINCT FROM ${nameSql})`;
  const object =
    raw !== null
      ? `INSERT INTO signature_objects (sha256, scheme, media_type, raw, size, first_seen_at)
VALUES (${bytea(objectHex)}, ${lit(s.scheme)}, ${lit(s.mediaType)}, convert_to(${lit(raw)}, 'UTF8'), ${Buffer.byteLength(raw)}, ${lit(received)})`
      : `INSERT INTO signature_objects (sha256, scheme, media_type, item_blob, size, first_seen_at)
VALUES (${bytea(objectHex)}, ${lit(s.scheme)}, ${lit(s.mediaType)}, ${bytea(objectHex)}, ${Buffer.byteLength(contentOfFile!)}, ${lit(received)})`;
  return `
${object}
ON CONFLICT (sha256) DO UPDATE SET scheme = EXCLUDED.scheme, media_type = EXCLUDED.media_type;
INSERT INTO signatures (object_sha256, part, subject_sha256, subject_name, role, predicate_type, state, reason, signer, key_ids,
                        proven_at, time_proof, received_at, checked_at, due, priority, attempts)
SELECT ${bytea(objectHex)}, 0, ${subject}, ${nameSql}, ${lit(s.role)}, ${s.predicateType ? lit(s.predicateType) : 'NULL'}, ${lit(s.state)},
       ${s.reason ? lit(s.reason) : 'NULL'}, ${s.signer ? jsonb(s.signer) : 'NULL'}, ${textArray(s.keyIds ?? [])}, ${proven},
       ${s.timeProof ? lit(s.timeProof) : 'NULL'}, ${lit(received)}, ${checked}, ${s.state === 'pending'}, 1, 0
 WHERE ${subject} IS NOT NULL
ON CONFLICT (object_sha256, part, subject_sha256, subject_name) DO UPDATE SET role = EXCLUDED.role, predicate_type = EXCLUDED.predicate_type,
       state = EXCLUDED.state, reason = EXCLUDED.reason, signer = EXCLUDED.signer, key_ids = EXCLUDED.key_ids, proven_at = EXCLUDED.proven_at,
       time_proof = EXCLUDED.time_proof, received_at = EXCLUDED.received_at, checked_at = EXCLUDED.checked_at, due = EXCLUDED.due,
       not_before = now() + interval '30 days';
INSERT INTO signature_attachments (repo_id, signature_id, location, item_id, source, name)
SELECT ${repoId(s.project, s.repo)}, ${sig}, ${lit(s.location)},
       ${s.file ? `(SELECT i.id FROM items i WHERE i.repo_id = ${repoId(s.project, s.repo)} AND i.path = ${lit(s.file)} AND i.trashed_at IS NULL)` : 'NULL'},
       ${lit(s.source)}, ${attachmentName(s)}
 WHERE ${sig} IS NOT NULL
ON CONFLICT (repo_id, signature_id, location) DO NOTHING;
${s.trust ? trustSql(sig, s.trust, null) : ''}
${s.projectTrust ? trustSql(sig, s.projectTrust, s.projectTrust.project) : ''}`;
}

/**
 * The signatures, their attachments and trust rows, a project anchor's own row for the generic file, the anchor of the Helm values,
 * and the origin of the generic file as an upload of a pipeline with its verified claims (R-SIG-010).
 */
export function seedSignatures(): void {
  psql(
    [
      'BEGIN;',
      `INSERT INTO signature_anchors (scope, key, title, kind, material, enabled)
       SELECT 'configuration', 'gitlab-ci-signing', 'GitLab CI signing CA', 'ca',
              jsonb_build_object('ca', jsonb_build_object('certificates', ${lit(anchorMaterial.gitlabCa)}, 'use', 'codeSigning')), true
       ON CONFLICT (scope, project_id, key) DO NOTHING;`,
      ...signatures.map(signatureSql),
      `INSERT INTO artifact_origins (item_id, principal_kind, principal_name, ci, uploaded_at)
       SELECT i.id, 'ci', 'ci-mapping:portal-cli-release', ${jsonb({
         issuer: 'gitlab-internal',
         project_path: 'acme/customer-portal/portal-cli',
         project_id: '4712',
         namespace_path: 'acme/customer-portal',
         pipeline_id: '1882231',
         job_id: '4471029',
         ref: 'main',
         ref_type: 'branch',
         ref_protected: true,
         pipeline_source: 'push',
         sha: '9c1f2e7a4b3d5c6e8f90a1b2c3d4e5f60718293a',
         user_login: 'priya',
       })}, now() - interval '3 hours'
         FROM items i WHERE i.repo_id = ${repoId(mainProject, signedFile.repo)} AND i.path = ${lit(filePath)} AND i.trashed_at IS NULL
       ON CONFLICT (item_id) DO UPDATE SET principal_kind = EXCLUDED.principal_kind, principal_name = EXCLUDED.principal_name, ci = EXCLUDED.ci,
              uploaded_at = EXCLUDED.uploaded_at;`,
      'COMMIT;',
    ].join('\n'),
  );
}
