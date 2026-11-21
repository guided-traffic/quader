import { Injectable, Provider } from '@angular/core';
import { ApiError } from '../problem';
import { Page, PageFetcher } from '../pagination';
import {
  LoadedSignatureSettings,
  NewSignatureAnchor,
  NewSignatureBlock,
  ProjectSignatureAnchorList,
  SignatureAnchor,
  SignatureAnchorPatch,
  SignatureAnchorScope,
  SignatureBlock,
  SignatureEntry,
  SignatureFileReport,
  SignatureFinding,
  SignatureFindingFilters,
  SignatureGatePreview,
  SignatureImpact,
  SignatureImpactRequest,
  SignatureRefusal,
  SignatureRefusalFilters,
  SignatureRelease,
  SignatureRepositoryReport,
  SignatureSettings,
  SignatureVersionReport,
  SignaturesApi,
  versionTag,
} from '../signatures-api';

/** A digest of 64 hex digits whose first characters say what it is in a test. */
const hex = (head: string): string => head.padEnd(64, '0');

/** The settings of an installation with the signatures on: the switch off, nothing waiting, the release's public root. */
export function signatureSettings(over: Partial<SignatureSettings> = {}): SignatureSettings {
  return {
    enabled: true,
    projectAnchors: false,
    anchorEpoch: 3,
    version: 1,
    updatedAt: '2026-11-21T08:00:00Z',
    updatedBy: null,
    trustRoot: { source: 'release', fetchedAt: '2026-11-14T00:00:00Z', tufVersion: 12, lastError: null, lastErrorAt: null },
    backlog: { pending: 0, stale: 0 },
    ...over,
  };
}

/** A publisher's keyless Sigstore signature of the company's Sigstore for a GitLab job, trusted internally. */
export function sigstoreEntry(over: Partial<SignatureEntry> = {}): SignatureEntry {
  return {
    id: 11,
    role: 'publisher',
    scheme: 'sigstore-bundle',
    state: 'valid',
    reason: null,
    trust: {
      state: 'internal',
      anchors: [{ id: 7, title: 'Company Sigstore', scope: 'installation' }],
      block: null,
      computedAt: '2026-11-21T09:00:00Z',
      stale: false,
    },
    identity: 'https://gitlab.example.com/platform/portal//.gitlab-ci.yml@refs/heads/main',
    oidcIssuer: 'https://gitlab.example.com',
    ci: {
      sourceRepositoryUri: 'https://gitlab.example.com/platform/portal',
      sourceRepositoryRef: 'refs/heads/main',
      runInvocationUri: 'https://gitlab.example.com/platform/portal/-/jobs/88231',
    },
    keyIds: [hex('5a1e')],
    provenAt: '2026-11-21T08:41:07Z',
    timeProof: 'log',
    receivedAt: '2026-11-21T08:42:00Z',
    certificateNotBefore: '2026-11-21T08:41:00Z',
    certificateNotAfter: '2026-11-21T08:51:00Z',
    log: [{ index: 4182, integratedTime: '2026-11-21T08:41:07Z', proof: 'proof' }],
    timestamps: [],
    type: 'application/vnd.dev.sigstore.bundle.v0.3+json',
    subject: hex('ab12'),
    repository: 'web/docker-local',
    source: 'referrer',
    location: 'portal/web/_manifests/sha256:5a1e',
    attachments: [{ repository: 'web/docker-local', source: 'referrer', location: 'portal/web/_manifests/sha256:5a1e' }],
    mediaType: 'application/vnd.dev.sigstore.bundle.v0.3+json',
    size: 4182,
    original: `/api/v1/signatures/objects/${hex('5a1e')}`,
    ...over,
  };
}

/**
 * npm's registry signature this installation made of a local version with its signing identity (signatures.md R-SIG-050, R-SIG-055):
 * `source` issued, `issuedBy` set, no trust row.
 */
export function issuedNpmEntry(over: Partial<SignatureEntry> = {}): SignatureEntry {
  return {
    id: 21,
    role: 'registry',
    scheme: 'npm-registry',
    state: 'valid',
    reason: null,
    trust: null,
    keyIds: ['SHA256:Zq2bQuaderSigningKey0000000000000000000000'],
    keyId: 'SHA256:Zq2bQuaderSigningKey0000000000000000000000',
    receivedAt: '2026-11-21T08:42:01Z',
    log: [],
    timestamps: [],
    type: 'application/json',
    subject: hex('cd34'),
    subjectName: '@acme/ui@1.2.0',
    repository: 'web/npm-local',
    source: 'issued',
    location: '@acme/ui/-/ui-1.2.0.tgz',
    attachments: [{ repository: 'web/npm-local', source: 'issued', location: '@acme/ui/-/ui-1.2.0.tgz', name: '@acme/ui' }],
    mediaType: 'application/json',
    size: 120,
    original: `/api/v1/signatures/objects/${hex('cd34')}`,
    issuedBy: { identity: 3, subject: 'CN=Quader signing,O=Example GmbH', fingerprint: hex('ef56'), state: 'current' },
    ...over,
  };
}

/** A version's report in one repository: verified internally by `entries`, unless `over` says otherwise. */
export function repositoryReport(over: Partial<SignatureRepositoryReport> = {}): SignatureRepositoryReport {
  return {
    repository: 'web/docker-local',
    class: 'local',
    state: 'verified_internal',
    stale: false,
    members: [],
    signatures: [sigstoreEntry()],
    attestations: [],
    covering: [],
    origin: { kind: 'uploaded', principalKind: 'user', principalName: 'priya', at: '2026-11-21T08:42:00Z' },
    ...over,
  };
}

/** A stored file's report: one signature, verified internally. */
export function fileReport(over: Partial<SignatureFileReport> = {}): SignatureFileReport {
  return {
    repository: 'web/files-internal',
    path: 'tools/tool.tar.gz',
    sha256: hex('f11e'),
    state: 'verified_internal',
    stale: false,
    signatures: [sigstoreEntry({ repository: 'web/files-internal', source: 'sibling', path: 'tools/tool.tar.gz' })],
    attestations: [],
    covering: [],
    origin: { kind: 'uploaded', principalKind: 'user', principalName: 'priya', at: '2026-11-21T08:42:00Z' },
    ...over,
  };
}

/** An anchor of `scope`; its kind's member is a CA certificate unless `over` names another kind. */
export function signatureAnchor(over: Partial<SignatureAnchor> = {}): SignatureAnchor {
  return {
    id: 7,
    scope: 'installation',
    project: null,
    key: 'company-ca',
    title: 'Company code signing CA',
    kind: 'ca',
    ca: { certificates: '-----BEGIN CERTIFICATE-----\n…\n-----END CERTIFICATE-----\n', use: 'codeSigning' },
    enabled: true,
    applied: true,
    note: null,
    version: 1,
    createdAt: '2026-11-21T08:00:00Z',
    updatedAt: '2026-11-21T08:00:00Z',
    createdBy: 'user:ada',
    fingerprints: [{ kind: 'sha256', value: hex('ca01'), subject: 'CN=Example Code Signing CA, O=Example GmbH', notAfter: '2031-01-01T00:00:00Z' }],
    signatures: 12,
    ...over,
  };
}

/** A block of a certificate's fingerprint. */
export function signatureBlock(over: Partial<SignatureBlock> = {}): SignatureBlock {
  return {
    id: 3,
    kind: 'fingerprint',
    value: hex('bad1'),
    issuer: null,
    reason: 'The key of the old build server was copied.',
    createdAt: '2026-11-21T09:30:00Z',
    createdBy: 'user:ada',
    signatures: 2,
    ...over,
  };
}

/** A finding of an invalid signature held by `repositories`. */
export function signatureFinding(repositories: readonly string[], over: Partial<SignatureFinding> = {}): SignatureFinding {
  return {
    id: 41,
    kind: 'invalid',
    scheme: 'notation-jws',
    role: 'publisher',
    reason: 'digest_mismatch',
    signer: 'CN=Release signing, O=Example GmbH',
    subject: hex('d1ff'),
    foundAt: '2026-11-21T09:00:00Z',
    block: null,
    repositoryCount: repositories.length,
    firstRepository: repositories[0] ?? '',
    repositories: [...repositories],
    items: repositories.map((r) => ({ repository: r, format: 'oci', path: 'portal/web/_manifests/sha256:d1ff', package: 'portal/web', version: '1.4.0' })),
    itemsTruncated: false,
    ...over,
  };
}

/** A preview of a repository whose 1,000 artifacts pass but for `refused`; nothing waits, every item read. */
export function gatePreview(over: Partial<SignatureGatePreview> = {}): SignatureGatePreview {
  return {
    examined: 1240,
    complete: true,
    artifacts: 1000,
    pass: 1000,
    pending: 0,
    refused: { missing: 0, untrusted: 0, notChosen: 0, invalid: 0, blocked: 0, notChecked: 0 },
    examples: [],
    ...over,
  };
}

/** A refused download of an unsigned image in `web/docker-local`, refused 14 times. */
export function signatureRefusal(over: Partial<SignatureRefusal> = {}): SignatureRefusal {
  return {
    repository: 'web/docker-local',
    format: 'oci',
    path: 'portal/api/_tags/1.4.0',
    package: 'portal/api',
    version: '1.4.0',
    sha256: hex('a11e'),
    reason: 'missing',
    firstAt: '2026-11-21T07:00:00Z',
    lastAt: '2026-11-21T09:40:00Z',
    count: 14,
    release: null,
    ...over,
  };
}

/** A release of an artifact of `web/files-internal` by the auditor. */
export function signatureRelease(over: Partial<SignatureRelease> = {}): SignatureRelease {
  return {
    repository: 'web/files-internal',
    sha256: hex('f11e'),
    reason: 'The vendor signs from the next release on; this one is needed for the hotfix.',
    refusedReason: 'missing',
    releasedBy: 'user:noor',
    releasedAt: '2026-11-21T09:50:00Z',
    ...over,
  };
}

/**
 * The signatures in memory (R-SIG-034): the settings need the loaded `ETag` (`412` otherwise), anchors and blocks are created and
 * removed, a project's write answers `403 project_anchors_disabled` while the switch is off. Every write is recorded; `failure` makes
 * the next write fail.
 */
@Injectable()
export class FakeSignaturesApi extends SignaturesApi {
  current: SignatureSettings = signatureSettings();
  versionReport: SignatureVersionReport = { packageId: 'p1', version: '1.0.0', repositories: [repositoryReport()] };
  fileAnswer: SignatureFileReport = fileReport();
  anchorList: SignatureAnchor[] = [];
  projectList: Record<string, SignatureAnchor[]> = {};
  blockList: SignatureBlock[] = [];
  items: SignatureFinding[] = [];
  failure: ApiError | null = null;
  /** The preview's answer, or a refusal of it (`409 signatures_not_enabled`, `422 not_applicable`). */
  previewAnswer: SignatureGatePreview | ApiError = gatePreview();
  impactAnswer: SignatureImpact = { artifacts: 0, repositories: 0, complete: true, bound: 'exact', byRepository: [] };
  refusalList: SignatureRefusal[] = [];
  releaseList: Record<string, SignatureRelease[]> = {};
  readonly calls: { op: string; body?: unknown }[] = [];
  readonly findQueries: SignatureFindingFilters[] = [];
  readonly refusalQueries: SignatureRefusalFilters[] = [];
  private nextId = 100;

  private fail(): void {
    const f = this.failure;
    if (f) {
      this.failure = null;
      throw f;
    }
  }

  override async version(packageId: string, version: string): Promise<SignatureVersionReport> {
    this.calls.push({ op: 'version', body: { packageId, version } });
    return structuredClone(this.versionReport);
  }

  override async file(repository: string, path: string): Promise<SignatureFileReport> {
    this.calls.push({ op: 'file', body: { repository, path } });
    return structuredClone(this.fileAnswer);
  }

  override async settings(): Promise<LoadedSignatureSettings> {
    return { settings: structuredClone(this.current), etag: versionTag(this.current.version) };
  }

  override async updateSettings(projectAnchors: boolean, etag: string): Promise<LoadedSignatureSettings> {
    this.calls.push({ op: 'settings', body: { projectAnchors, etag } });
    this.fail();
    if (etag !== versionTag(this.current.version)) throw new ApiError(412, { code: 'precondition_failed', requestId: 'rid-sig-412' }, 'rid-sig-412');
    this.current = { ...this.current, projectAnchors, version: this.current.version + 1, updatedBy: 'user:ada', updatedAt: '2026-11-21T10:00:00Z' };
    return this.settings();
  }

  override async anchors(scope?: SignatureAnchorScope): Promise<readonly SignatureAnchor[]> {
    if (scope === 'project') return structuredClone(Object.values(this.projectList).flat());
    return structuredClone(this.anchorList.filter((a) => !scope || a.scope === scope));
  }

  private made(anchor: NewSignatureAnchor, scope: SignatureAnchorScope, project: string | null): SignatureAnchor {
    const { enabled, note, ...rest } = anchor;
    return signatureAnchor({ ...rest, id: this.nextId++, scope, project, enabled, note: note ?? null, signatures: 0, fingerprints: [] });
  }

  override async createAnchor(anchor: NewSignatureAnchor): Promise<SignatureAnchor> {
    this.calls.push({ op: 'createAnchor', body: anchor });
    this.fail();
    const made = this.made(anchor, 'installation', null);
    this.anchorList = [...this.anchorList, made];
    return made;
  }

  override async updateAnchor(anchor: SignatureAnchor, patch: SignatureAnchorPatch): Promise<SignatureAnchor> {
    this.calls.push({ op: 'updateAnchor', body: { id: anchor.id, patch, version: anchor.version } });
    this.fail();
    const next = { ...anchor, ...patch, version: anchor.version + 1, applied: patch.enabled ?? anchor.applied } as SignatureAnchor;
    this.anchorList = this.anchorList.map((a) => (a.id === anchor.id ? next : a));
    return next;
  }

  override async deleteAnchor(id: number): Promise<void> {
    this.calls.push({ op: 'deleteAnchor', body: { id } });
    this.fail();
    this.anchorList = this.anchorList.filter((a) => a.id !== id);
    for (const k of Object.keys(this.projectList)) this.projectList[k] = (this.projectList[k] ?? []).filter((a) => a.id !== id);
  }

  override async projectAnchors(project: string): Promise<ProjectSignatureAnchorList> {
    return {
      projectAnchors: this.current.projectAnchors,
      items: structuredClone(this.projectList[project] ?? []),
      installation: structuredClone(this.anchorList.filter((a) => a.applied)),
    };
  }

  override async createProjectAnchor(project: string, anchor: NewSignatureAnchor): Promise<SignatureAnchor> {
    this.calls.push({ op: 'createProjectAnchor', body: { project, anchor } });
    this.fail();
    if (!this.current.projectAnchors) throw new ApiError(403, { code: 'project_anchors_disabled', requestId: 'rid-sig-403' }, 'rid-sig-403');
    const made = this.made(anchor, 'project', project);
    this.projectList[project] = [...(this.projectList[project] ?? []), made];
    return made;
  }

  override async deleteProjectAnchor(project: string, id: number): Promise<void> {
    this.calls.push({ op: 'deleteProjectAnchor', body: { project, id } });
    this.fail();
    this.projectList[project] = (this.projectList[project] ?? []).filter((a) => a.id !== id);
  }

  override async blocks(): Promise<readonly SignatureBlock[]> {
    return structuredClone(this.blockList);
  }

  override async createBlock(block: NewSignatureBlock): Promise<SignatureBlock> {
    this.calls.push({ op: 'createBlock', body: block });
    this.fail();
    const made = signatureBlock({ ...block, issuer: block.issuer ?? null, id: this.nextId++, signatures: 0 });
    this.blockList = [...this.blockList, made];
    return made;
  }

  override async deleteBlock(id: number): Promise<void> {
    this.calls.push({ op: 'deleteBlock', body: { id } });
    this.fail();
    this.blockList = this.blockList.filter((b) => b.id !== id);
  }

  override findings(filters: SignatureFindingFilters): PageFetcher<SignatureFinding> {
    this.findQueries.push(filters);
    return async (cursor, limit): Promise<Page<SignatureFinding>> => {
      const all = this.items.filter(
        (f) =>
          (!filters.kind || f.kind === filters.kind) &&
          (!filters.repository || f.repositories.includes(filters.repository)) &&
          (!filters.q || `${f.signer ?? ''} ${f.identity ?? ''}`.toLowerCase().includes(filters.q.toLowerCase())),
      );
      const start = cursor === null ? 0 : Number(cursor);
      return { items: structuredClone(all.slice(start, start + limit)), nextCursor: start + limit < all.length ? String(start + limit) : null };
    };
  }

  override async preview(repository: string, anchors?: readonly string[]): Promise<SignatureGatePreview> {
    this.calls.push({ op: 'preview', body: { repository, anchors } });
    const a = this.previewAnswer;
    if (a instanceof ApiError) throw a;
    return structuredClone(a);
  }

  override async impact(change: SignatureImpactRequest): Promise<SignatureImpact> {
    this.calls.push({ op: 'impact', body: change });
    this.fail();
    return structuredClone(this.impactAnswer);
  }

  override refusals(filters: SignatureRefusalFilters): PageFetcher<SignatureRefusal> {
    this.refusalQueries.push(filters);
    return async (cursor, limit): Promise<Page<SignatureRefusal>> => {
      const all = this.refusalList.filter(
        (r) => (!filters.repository || r.repository === filters.repository) && (!filters.reason || r.reason === filters.reason),
      );
      const start = cursor === null ? 0 : Number(cursor);
      return { items: structuredClone(all.slice(start, start + limit)), nextCursor: start + limit < all.length ? String(start + limit) : null };
    };
  }

  override async releases(repository: string): Promise<readonly SignatureRelease[]> {
    this.calls.push({ op: 'releases', body: { repository } });
    return structuredClone(this.releaseList[repository] ?? []);
  }

  override async release(repository: string, sha256: string, reason: string): Promise<SignatureRelease> {
    this.calls.push({ op: 'release', body: { repository, sha256, reason } });
    this.fail();
    const made = signatureRelease({ repository, sha256, reason, releasedBy: 'user:noor' });
    this.releaseList[repository] = [made, ...(this.releaseList[repository] ?? [])];
    return made;
  }

  override async withdraw(repository: string, sha256: string): Promise<void> {
    this.calls.push({ op: 'withdraw', body: { repository, sha256 } });
    this.fail();
    this.releaseList[repository] = (this.releaseList[repository] ?? []).filter((r) => r.sha256 !== sha256);
  }
}

export function provideSignatureFakes(): Provider[] {
  return [{ provide: SignaturesApi, useClass: FakeSignaturesApi }];
}
