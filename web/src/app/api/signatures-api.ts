import { Injectable, inject } from '@angular/core';
import { ApiClient } from './client';
import { packageUrl } from './packages-api';
import { PageFetcher } from './pagination';
import { seg } from './query';
import { repositoryUrl } from './repositories-api';
import { SignatureReads } from './signature-reads';
import { Schema } from './types';

/** The badge's state of an artifact (R-SIG-009): four with a badge, three without. */
export type SignatureBadgeState = Schema<'SignatureBadgeState'>;
/** The member `signatures` of a stored file, a version's file, a version and a row of the version list (R-SIG-034). */
export type SignatureStatus = Schema<'SignatureStatus'>;
export type SignatureEntry = Schema<'SignatureEntry'>;
/** What Quader's receipt attestation says, decoded from its statement (R-SIG-051, R-SIG-055). */
export type SignatureReceipt = Schema<'SignatureReceipt'>;
export type SignatureRole = Schema<'SignatureRole'>;
export type SignatureScheme = Schema<'SignatureScheme'>;
export type SignatureIntegrityState = Schema<'SignatureIntegrityState'>;
export type SignatureTrust = Schema<'SignatureTrust'>;
export type SignatureCovering = Schema<'SignatureCovering'>;
export type SignatureOrigin = Schema<'SignatureOrigin'>;
export type SignatureRepositoryReport = Schema<'SignatureRepositoryReport'>;
export type SignatureVersionReport = Schema<'SignatureVersionReport'>;
export type SignatureFileReport = Schema<'SignatureFileReport'>;
export type SignatureSettings = Schema<'SignatureSettings'>;
export type SignatureAnchor = Schema<'SignatureAnchor'>;
export type SignatureAnchorKind = Schema<'SignatureAnchorKind'>;
export type SignatureAnchorScope = Schema<'SignatureAnchorScope'>;
export type NewSignatureAnchor = Schema<'NewSignatureAnchor'>;
export type SignatureAnchorPatch = Schema<'SignatureAnchorPatch'>;
export type ProjectSignatureAnchorList = Schema<'ProjectSignatureAnchorList'>;
export type SignatureBlock = Schema<'SignatureBlock'>;
export type SignatureBlockKind = Schema<'SignatureBlockKind'>;
export type NewSignatureBlock = Schema<'NewSignatureBlock'>;
export type SignatureFinding = Schema<'SignatureFinding'>;
export type SignatureFindingKind = Schema<'SignatureFindingKind'>;
/** The member `signatures` of a repository document (R-SIG-040, R-SIG-061): `fetch` of a remote repository, and the gate. */
export type RepositorySignatureSettings = Schema<'RepositorySignatureSettings'>;
/** Why a repository's gate refuses an artifact (R-SIG-062, R-SIG-068). */
export type SignatureGateReason = Schema<'SignatureGateReason'>;
/** A reason of the gate, or `pending` while a check is due (R-SIG-073). */
export type SignatureGateRefusedReason = Schema<'SignatureGateRefusedReason'>;
/** The gate of the repository of a signatures answer, while it is on (R-SIG-073). */
export type SignatureGateView = Schema<'SignatureGateView'>;
export type SignatureGateAnchor = Schema<'SignatureGateAnchor'>;
/** What a repository's gate would decide of its stored items (R-SIG-072). */
export type SignatureGatePreview = Schema<'SignatureGatePreview'>;
export type SignatureGateExample = Schema<'SignatureGateExample'>;
/** A release of one artifact for one repository's gate (R-SIG-070). */
export type SignatureRelease = Schema<'SignatureRelease'>;
/** A download a repository's gate refused (R-SIG-073). */
export type SignatureRefusal = Schema<'SignatureRefusal'>;
/** One change whose impact is asked before it is saved (R-SIG-071). */
export type SignatureImpactRequest = Schema<'SignatureImpactRequest'>;
export type SignatureImpact = Schema<'SignatureImpact'>;

/** The reasons of a refusal in the order the screens list them (R-SIG-062: the order of the first reason). */
export const SIGNATURE_GATE_REASONS: readonly SignatureGateReason[] = ['missing', 'untrusted', 'not_chosen', 'invalid', 'blocked', 'not_checked'];

/** The filters of `GET /api/v1/signatures/refusals` (R-SIG-073); an absent member is no filter. */
export interface SignatureRefusalFilters {
  /** `<project>/<key>`. */
  readonly repository?: string;
  readonly reason?: SignatureGateReason;
}

/** The kinds of a finding in the order the screens list them (R-SIG-038). */
export const SIGNATURE_FINDING_KINDS: readonly SignatureFindingKind[] = ['invalid', 'blocked'];
/** The kinds of anchor in the order the form offers them (R-SIG-015). */
export const SIGNATURE_ANCHOR_KINDS: readonly SignatureAnchorKind[] = ['ca', 'openpgp', 'public-key', 'sigstore'];
/** The kinds of block in the order the form offers them (R-SIG-020). */
export const SIGNATURE_BLOCK_KINDS: readonly SignatureBlockKind[] = ['fingerprint', 'serial', 'keyid', 'identity'];

/** The filters of `GET /api/v1/signatures/findings` (R-SIG-034); an absent member is no filter. */
export interface SignatureFindingFilters {
  readonly kind?: SignatureFindingKind;
  /** `<project>/<key>`. */
  readonly repository?: string;
  /** A fragment of a signer, a path or a package. */
  readonly q?: string;
}

/** The settings with the `ETag` a change takes in `If-Match`. */
export interface LoadedSignatureSettings {
  readonly settings: SignatureSettings;
  readonly etag: string;
}

/** The entity tag of a resource whose revision is its `version` (`ETag(revision)` of the server: `"<version>"`). */
export function versionTag(version: number): string {
  return `"${version}"`;
}

/**
 * The download of Quader's receipt attestation of a stored file or an image's manifest (R-SIG-054): `GET …/attestation?path=`, read with
 * the item's permission, from the repository and the location the receipt is attached at.
 */
export function attestationUrl(repository: string, path: string): string {
  return `${repositoryUrl(repository)}/attestation?path=${encodeURIComponent(path)}`;
}

/**
 * The operations of the signatures (R-SIG-034, tag `signatures`): what a reader sees of a version and of a file (`SignatureReads`, the
 * reads of the views, R-UI-223), the settings, the installation's anchors and blocks for `settings:read` and their changes for
 * `security-settings:write`, a project's anchors for its members and their changes for its Owner while the switch is on, and the findings
 * of the security dashboard.
 */
export abstract class SignaturesApi extends SignatureReads {
  /** `GET /api/v1/signatures/settings` with its `ETag`. */
  abstract settings(): Promise<LoadedSignatureSettings>;
  /** `PATCH /api/v1/signatures/settings`: the switch for project Owners, with `If-Match`. */
  abstract updateSettings(projectAnchors: boolean, etag: string): Promise<LoadedSignatureSettings>;
  /** `GET /api/v1/signatures/anchors` of a scope, or of every scope of the installation. */
  abstract anchors(scope?: SignatureAnchorScope): Promise<readonly SignatureAnchor[]>;
  /** `POST /api/v1/signatures/anchors`. */
  abstract createAnchor(anchor: NewSignatureAnchor): Promise<SignatureAnchor>;
  /** `PATCH /api/v1/signatures/anchors/{id}` with the anchor's version as `If-Match`. */
  abstract updateAnchor(anchor: SignatureAnchor, patch: SignatureAnchorPatch): Promise<SignatureAnchor>;
  /** `DELETE /api/v1/signatures/anchors/{id}`: an anchor of the installation or of any project. */
  abstract deleteAnchor(id: number): Promise<void>;
  /** `GET /api/v1/projects/{projectKey}/signatures/anchors`. */
  abstract projectAnchors(project: string): Promise<ProjectSignatureAnchorList>;
  /** `POST /api/v1/projects/{projectKey}/signatures/anchors`. */
  abstract createProjectAnchor(project: string, anchor: NewSignatureAnchor): Promise<SignatureAnchor>;
  /** `DELETE /api/v1/projects/{projectKey}/signatures/anchors/{id}`. */
  abstract deleteProjectAnchor(project: string, id: number): Promise<void>;
  /** `GET /api/v1/signatures/blocks`. */
  abstract blocks(): Promise<readonly SignatureBlock[]>;
  /** `POST /api/v1/signatures/blocks`. */
  abstract createBlock(block: NewSignatureBlock): Promise<SignatureBlock>;
  /** `DELETE /api/v1/signatures/blocks/{id}`: lifts a block. */
  abstract deleteBlock(id: number): Promise<void>;
  /** `GET /api/v1/signatures/findings`, newest first, in pages. */
  abstract findings(filters: SignatureFindingFilters): PageFetcher<SignatureFinding>;
  /**
   * `GET …/repositories/{repoKey}/signatures/preview` (R-SIG-072): the gate switched on over the stored items, with the stored anchors
   * (`anchors` undefined), any anchor (an empty list) or the anchors named.
   */
  abstract preview(repository: string, anchors?: readonly string[]): Promise<SignatureGatePreview>;
  /** `POST /api/v1/signatures/impact` (R-SIG-071): what one change makes the gates refuse. */
  abstract impact(change: SignatureImpactRequest): Promise<SignatureImpact>;
  /** `GET /api/v1/signatures/refusals`, the newest refusal first, in pages (R-SIG-073). */
  abstract refusals(filters: SignatureRefusalFilters): PageFetcher<SignatureRefusal>;
  /** `POST …/repositories/{repoKey}/signatures/releases`: releases one artifact for this repository's gate (R-SIG-070). */
  abstract release(repository: string, sha256: string, reason: string): Promise<SignatureRelease>;
  /** `DELETE …/repositories/{repoKey}/signatures/releases/{sha256}`: withdraws a release. */
  abstract withdraw(repository: string, sha256: string): Promise<void>;
}

const BASE = '/api/v1/signatures';

@Injectable()
export class HttpSignaturesApi extends SignaturesApi {
  private readonly client = inject(ApiClient);

  override attestationUrl(repository: string, path: string): string {
    return attestationUrl(repository, path);
  }

  override async version(packageId: string, version: string): Promise<SignatureVersionReport> {
    return (await this.client.getJson<SignatureVersionReport>(`${packageUrl(packageId)}/versions/${seg(version)}/signatures`)).body;
  }

  override async file(repository: string, path: string): Promise<SignatureFileReport> {
    return (await this.client.getJson<SignatureFileReport>(`${repositoryUrl(repository)}/signatures`, { params: { path } })).body;
  }

  override async settings(): Promise<LoadedSignatureSettings> {
    const r = await this.client.getJson<SignatureSettings>(`${BASE}/settings`);
    return { settings: r.body, etag: r.etag ?? '' };
  }

  override async updateSettings(projectAnchors: boolean, etag: string): Promise<LoadedSignatureSettings> {
    const r = await this.client.patch<SignatureSettings>(`${BASE}/settings`, { projectAnchors }, etag);
    return { settings: r.body, etag: r.etag ?? '' };
  }

  override async anchors(scope?: SignatureAnchorScope): Promise<readonly SignatureAnchor[]> {
    const r = await this.client.getJson<{ items: SignatureAnchor[] }>(`${BASE}/anchors`, { params: scope ? { scope } : {} });
    return r.body.items;
  }

  override async createAnchor(anchor: NewSignatureAnchor): Promise<SignatureAnchor> {
    return (await this.client.post<SignatureAnchor>(`${BASE}/anchors`, anchor)).body;
  }

  override async updateAnchor(anchor: SignatureAnchor, patch: SignatureAnchorPatch): Promise<SignatureAnchor> {
    return (await this.client.patch<SignatureAnchor>(`${BASE}/anchors/${anchor.id}`, patch, versionTag(anchor.version))).body;
  }

  override async deleteAnchor(id: number): Promise<void> {
    await this.client.delete(`${BASE}/anchors/${id}`);
  }

  override async projectAnchors(project: string): Promise<ProjectSignatureAnchorList> {
    return (await this.client.getJson<ProjectSignatureAnchorList>(`/api/v1/projects/${encodeURIComponent(project)}/signatures/anchors`)).body;
  }

  override async createProjectAnchor(project: string, anchor: NewSignatureAnchor): Promise<SignatureAnchor> {
    return (await this.client.post<SignatureAnchor>(`/api/v1/projects/${encodeURIComponent(project)}/signatures/anchors`, anchor)).body;
  }

  override async deleteProjectAnchor(project: string, id: number): Promise<void> {
    await this.client.delete(`/api/v1/projects/${encodeURIComponent(project)}/signatures/anchors/${id}`);
  }

  override async blocks(): Promise<readonly SignatureBlock[]> {
    return (await this.client.getJson<{ items: SignatureBlock[] }>(`${BASE}/blocks`)).body.items;
  }

  override async createBlock(block: NewSignatureBlock): Promise<SignatureBlock> {
    return (await this.client.post<SignatureBlock>(`${BASE}/blocks`, block)).body;
  }

  override async deleteBlock(id: number): Promise<void> {
    await this.client.delete(`${BASE}/blocks/${id}`);
  }

  override findings(filters: SignatureFindingFilters): PageFetcher<SignatureFinding> {
    return this.client.page<SignatureFinding>(`${BASE}/findings`, {
      kind: filters.kind,
      repository: filters.repository || undefined,
      q: filters.q || undefined,
    });
  }

  override async preview(repository: string, anchors?: readonly string[]): Promise<SignatureGatePreview> {
    const params = anchors === undefined ? {} : { anchors: anchors.join(',') };
    return (await this.client.getJson<SignatureGatePreview>(`${repositoryUrl(repository)}/signatures/preview`, { params })).body;
  }

  override async impact(change: SignatureImpactRequest): Promise<SignatureImpact> {
    return (await this.client.post<SignatureImpact>(`${BASE}/impact`, change)).body;
  }

  override refusals(filters: SignatureRefusalFilters): PageFetcher<SignatureRefusal> {
    return this.client.page<SignatureRefusal>(`${BASE}/refusals`, {
      repository: filters.repository || undefined,
      reason: filters.reason,
    });
  }

  override async releases(repository: string): Promise<readonly SignatureRelease[]> {
    return (await this.client.getJson<{ items: SignatureRelease[] }>(`${repositoryUrl(repository)}/signatures/releases`)).body.items;
  }

  override async release(repository: string, sha256: string, reason: string): Promise<SignatureRelease> {
    return (await this.client.post<SignatureRelease>(`${repositoryUrl(repository)}/signatures/releases`, { sha256, reason })).body;
  }

  override async withdraw(repository: string, sha256: string): Promise<void> {
    await this.client.delete(`${repositoryUrl(repository)}/signatures/releases/${encodeURIComponent(sha256)}`);
  }
}
