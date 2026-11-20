import { Injectable, inject } from '@angular/core';
import { ApiClient } from './client';
import { UpstreamTestResult } from './repositories-api';
import { Schema } from './types';
import { UPSTREAM_PROJECT } from '../core/upstream-project';

/** A well-known repository of the project `upstream` as the admin page shows it (R-REPO-073). */
export type UpstreamRepository = Schema<'UpstreamRepository'>;
/** `active`, `switched-off`, `not-switched-on` or `retired` (R-REPO-068); `null` for a system repository. */
export type UpstreamRepositoryState = Schema<'UpstreamRepositoryState'>;
export type UpstreamRepositoryBulkResult = Schema<'UpstreamRepositoryBulkResult'>;
/** An administrator's address: each member optional, an omitted one the product's (R-REPO-069). */
export type UpstreamRepositoryAddress = Schema<'UpstreamRepositoryAddress'>;
/** An account for the upstream (R-REPO-033). */
export type UpstreamRepositoryAccount = Schema<'UpstreamRepositoryAccount'>;
export type UpstreamVisibilityLevel = Schema<'VisibilityLevel'>;
/** What a writer of a virtual repository learns of a well-known repository of a format: its state and its level (Q-API-364). */
export type UpstreamRepositorySummary = Schema<'UpstreamRepositorySummary'>;

/** A well-known repository with the `ETag` it was read with: the address and the account take it in `If-Match`. */
export interface LoadedUpstreamRepository {
  readonly value: UpstreamRepository;
  readonly etag: string;
}

/**
 * The operations of the well-known repositories (R-API-083, R-REPO-073, tag `upstream`): read with `repositories:read-config`,
 * written with `repositories:write-config`; the read of a format's presets also admits the Owner of any project (Q-API-364). The state and the level set one value and take no `If-Match`; the address and the
 * account replace what an administrator typed and take the loaded `ETag`. The two bulk operations are one transaction each and
 * answer what they changed.
 */
export abstract class UpstreamApi {
  /** `GET /api/v1/upstream-repositories`: every well-known repository, the presets in catalog order, then the system ones. */
  abstract list(): Promise<readonly UpstreamRepository[]>;
  /** `GET /api/v1/upstream-repositories/{key}` with its `ETag`; it names the listing virtual repositories (`listedByNames`). */
  abstract get(key: string): Promise<LoadedUpstreamRepository>;
  /** `GET /api/v1/formats/{format}/upstream-repositories`: the format's presets that are not retired, with state and level. */
  abstract listForFormat(format: string): Promise<readonly UpstreamRepositorySummary[]>;
  /** `PUT …/{key}/state`: switch on or off. */
  abstract setState(key: string, state: 'on' | 'off'): Promise<LoadedUpstreamRepository>;
  /** `POST …/switch-on-all`: every preset repository that is off, switched on and made internal. */
  abstract switchOnAll(idempotencyKey: string): Promise<UpstreamRepositoryBulkResult>;
  /** `PUT …/{key}/visibility`: both levels of the repository. */
  abstract setVisibility(key: string, level: UpstreamVisibilityLevel): Promise<LoadedUpstreamRepository>;
  /** `POST …/set-visibility`: the level of every preset repository that is not retired. */
  abstract setAllVisibility(level: UpstreamVisibilityLevel, idempotencyKey: string): Promise<UpstreamRepositoryBulkResult>;
  /** `PUT …/{key}/address` with `If-Match`. */
  abstract setAddress(key: string, address: UpstreamRepositoryAddress, etag: string): Promise<LoadedUpstreamRepository>;
  /** `DELETE …/{key}/address` with `If-Match`; `removeAccount` where the product's hosts differ from the override's. */
  abstract resetAddress(key: string, etag: string, removeAccount?: boolean): Promise<LoadedUpstreamRepository>;
  /** `PUT …/{key}/account` with `If-Match`. */
  abstract setAccount(key: string, account: UpstreamRepositoryAccount, etag: string): Promise<LoadedUpstreamRepository>;
  /** `DELETE …/{key}/account` with `If-Match`. */
  abstract removeAccount(key: string, etag: string): Promise<LoadedUpstreamRepository>;
  /** The saved upstream of the repository, through the client and the stored account real fetches use (R-REPO-024). */
  abstract testSaved(key: string): Promise<UpstreamTestResult>;
  /** An address and an account that are not saved yet (R-REPO-024); a typed password is used once and stored nowhere. */
  abstract testUnsaved(format: string, remote: Readonly<Record<string, unknown>>): Promise<UpstreamTestResult>;
}

const BASE = '/api/v1/upstream-repositories';
const item = (key: string): string => `${BASE}/${encodeURIComponent(key)}`;

@Injectable()
export class HttpUpstreamApi extends UpstreamApi {
  private readonly client = inject(ApiClient);

  override async list(): Promise<readonly UpstreamRepository[]> {
    return (await this.client.get(BASE)).body.items;
  }

  override async get(key: string): Promise<LoadedUpstreamRepository> {
    const r = await this.client.getJson<UpstreamRepository>(item(key));
    return { value: r.body, etag: r.etag ?? '' };
  }

  override async listForFormat(format: string): Promise<readonly UpstreamRepositorySummary[]> {
    const url = `/api/v1/formats/${encodeURIComponent(format)}/upstream-repositories`;
    return (await this.client.getJson<Schema<'UpstreamRepositorySummaryList'>>(url)).body.items;
  }

  override async setState(key: string, state: 'on' | 'off'): Promise<LoadedUpstreamRepository> {
    const r = await this.client.putJson<UpstreamRepository>(`${item(key)}/state`, { state });
    return { value: r.body, etag: r.etag ?? '' };
  }

  override async switchOnAll(idempotencyKey: string): Promise<UpstreamRepositoryBulkResult> {
    return (await this.client.post<UpstreamRepositoryBulkResult>(`${BASE}/switch-on-all`, undefined, { idempotencyKey })).body;
  }

  override async setVisibility(key: string, level: UpstreamVisibilityLevel): Promise<LoadedUpstreamRepository> {
    const r = await this.client.putJson<UpstreamRepository>(`${item(key)}/visibility`, { level });
    return { value: r.body, etag: r.etag ?? '' };
  }

  override async setAllVisibility(level: UpstreamVisibilityLevel, idempotencyKey: string): Promise<UpstreamRepositoryBulkResult> {
    return (await this.client.post<UpstreamRepositoryBulkResult>(`${BASE}/set-visibility`, { level }, { idempotencyKey })).body;
  }

  override async setAddress(key: string, address: UpstreamRepositoryAddress, etag: string): Promise<LoadedUpstreamRepository> {
    const r = await this.client.put<UpstreamRepository>(`${item(key)}/address`, address, etag);
    return { value: r.body, etag: r.etag ?? '' };
  }

  override async resetAddress(key: string, etag: string, removeAccount = false): Promise<LoadedUpstreamRepository> {
    const r = await this.client.delete<UpstreamRepository>(`${item(key)}/address`, {
      etag,
      params: removeAccount ? { removeAccount: true } : undefined,
    });
    return { value: r.body, etag: r.etag ?? '' };
  }

  override async setAccount(key: string, account: UpstreamRepositoryAccount, etag: string): Promise<LoadedUpstreamRepository> {
    const r = await this.client.put<UpstreamRepository>(`${item(key)}/account`, account, etag);
    return { value: r.body, etag: r.etag ?? '' };
  }

  override async removeAccount(key: string, etag: string): Promise<LoadedUpstreamRepository> {
    const r = await this.client.delete<UpstreamRepository>(`${item(key)}/account`, { etag });
    return { value: r.body, etag: r.etag ?? '' };
  }

  override async testSaved(key: string): Promise<UpstreamTestResult> {
    const url = `/api/v1/projects/${UPSTREAM_PROJECT}/repositories/${encodeURIComponent(key)}/upstream-test`;
    return (await this.client.post<UpstreamTestResult>(url)).body;
  }

  override async testUnsaved(format: string, remote: Readonly<Record<string, unknown>>): Promise<UpstreamTestResult> {
    return (await this.client.post<UpstreamTestResult>(`/api/v1/projects/${UPSTREAM_PROJECT}/upstream-tests`, { format, remote })).body;
  }
}
