import { Injectable, inject } from '@angular/core';
import { ApiClient } from './client';
import { UpstreamTestResult } from './repositories-api';
import { Schema } from './types';
import { UPSTREAM_PROJECT, wellKnownName } from '../core/upstream-project';

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

/** One of the installation's own registries, which the form of a new remote repository offers as a button (R-REPO-076). */
export type RegistryPreset = Schema<'RegistryPreset'>;

/** The installation's registry presets with the `ETag` they were read with, which their replacement takes in `If-Match`. */
export interface LoadedRegistryPresets {
  readonly presets: readonly RegistryPreset[];
  readonly etag: string;
}

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
  /**
   * `PUT …/{key}/clients`: the switch **ClamAV definition updates** of `clamav` (R-REPO-075, Q-API-382). On opens it at `level`
   * (`public` when omitted; the highest level the installation allows, with the warning `public_not_allowed_lowered`); off closes it
   * with both levels back at `private`.
   */
  abstract setClients(key: string, enabled: boolean, level?: UpstreamVisibilityLevel): Promise<LoadedUpstreamRepository>;
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
  /** `GET /api/v1/settings/registry-presets`: the installation's own registries with their `ETag` (R-API-084). */
  abstract registryPresets(): Promise<LoadedRegistryPresets>;
  /** `PUT /api/v1/settings/registry-presets` with `If-Match`: the whole list replaced; the answer is the list as stored. */
  abstract setRegistryPresets(presets: readonly RegistryPreset[], etag: string): Promise<LoadedRegistryPresets>;
}

const BASE = '/api/v1/upstream-repositories';
const PRESETS = '/api/v1/settings/registry-presets';
const item = (key: string): string => `${BASE}/${encodeURIComponent(key)}`;

/**
 * A well-known repository with its name in the UI's words (R-UI-272): the server sends the preset's English name, the UI names a
 * repository the product declares by its key, so every screen of the admin area, every dialog and every notice that names one shows
 * the reader's words.
 */
function worded<T extends { readonly key: string; readonly displayName: string }>(r: T): T {
  return { ...r, displayName: wellKnownName(r.key, r.displayName) };
}

const loaded = (r: { readonly body: UpstreamRepository; readonly etag?: string | null }): LoadedUpstreamRepository => ({
  value: worded(r.body),
  etag: r.etag ?? '',
});

const bulk = (b: UpstreamRepositoryBulkResult): UpstreamRepositoryBulkResult => ({ ...b, items: b.items.map(worded) });

@Injectable()
export class HttpUpstreamApi extends UpstreamApi {
  private readonly client = inject(ApiClient);

  override async list(): Promise<readonly UpstreamRepository[]> {
    return (await this.client.get(BASE)).body.items.map(worded);
  }

  override async get(key: string): Promise<LoadedUpstreamRepository> {
    const r = await this.client.getJson<UpstreamRepository>(item(key));
    return loaded(r);
  }

  override async listForFormat(format: string): Promise<readonly UpstreamRepositorySummary[]> {
    const url = `/api/v1/formats/${encodeURIComponent(format)}/upstream-repositories`;
    return (await this.client.getJson<Schema<'UpstreamRepositorySummaryList'>>(url)).body.items.map(worded);
  }

  override async setState(key: string, state: 'on' | 'off'): Promise<LoadedUpstreamRepository> {
    const r = await this.client.putJson<UpstreamRepository>(`${item(key)}/state`, { state });
    return loaded(r);
  }

  override async switchOnAll(idempotencyKey: string): Promise<UpstreamRepositoryBulkResult> {
    return bulk((await this.client.post<UpstreamRepositoryBulkResult>(`${BASE}/switch-on-all`, undefined, { idempotencyKey })).body);
  }

  override async setClients(key: string, enabled: boolean, level?: UpstreamVisibilityLevel): Promise<LoadedUpstreamRepository> {
    const r = await this.client.putJson<UpstreamRepository>(`${item(key)}/clients`, enabled && level ? { enabled, level } : { enabled });
    return loaded(r);
  }

  override async setVisibility(key: string, level: UpstreamVisibilityLevel): Promise<LoadedUpstreamRepository> {
    const r = await this.client.putJson<UpstreamRepository>(`${item(key)}/visibility`, { level });
    return loaded(r);
  }

  override async setAllVisibility(level: UpstreamVisibilityLevel, idempotencyKey: string): Promise<UpstreamRepositoryBulkResult> {
    return bulk((await this.client.post<UpstreamRepositoryBulkResult>(`${BASE}/set-visibility`, { level }, { idempotencyKey })).body);
  }

  override async setAddress(key: string, address: UpstreamRepositoryAddress, etag: string): Promise<LoadedUpstreamRepository> {
    const r = await this.client.put<UpstreamRepository>(`${item(key)}/address`, address, etag);
    return loaded(r);
  }

  override async resetAddress(key: string, etag: string, removeAccount = false): Promise<LoadedUpstreamRepository> {
    const r = await this.client.delete<UpstreamRepository>(`${item(key)}/address`, {
      etag,
      params: removeAccount ? { removeAccount: true } : undefined,
    });
    return loaded(r);
  }

  override async setAccount(key: string, account: UpstreamRepositoryAccount, etag: string): Promise<LoadedUpstreamRepository> {
    const r = await this.client.put<UpstreamRepository>(`${item(key)}/account`, account, etag);
    return loaded(r);
  }

  override async removeAccount(key: string, etag: string): Promise<LoadedUpstreamRepository> {
    const r = await this.client.delete<UpstreamRepository>(`${item(key)}/account`, { etag });
    return loaded(r);
  }

  override async testSaved(key: string): Promise<UpstreamTestResult> {
    const url = `/api/v1/projects/${UPSTREAM_PROJECT}/repositories/${encodeURIComponent(key)}/upstream-test`;
    return (await this.client.post<UpstreamTestResult>(url)).body;
  }

  override async testUnsaved(format: string, remote: Readonly<Record<string, unknown>>): Promise<UpstreamTestResult> {
    return (await this.client.post<UpstreamTestResult>(`/api/v1/projects/${UPSTREAM_PROJECT}/upstream-tests`, { format, remote })).body;
  }

  override async registryPresets(): Promise<LoadedRegistryPresets> {
    const r = await this.client.getJson<Schema<'RegistryPresets'>>(PRESETS);
    return { presets: r.body.presets, etag: r.etag ?? '' };
  }

  override async setRegistryPresets(presets: readonly RegistryPreset[], etag: string): Promise<LoadedRegistryPresets> {
    const r = await this.client.put<Schema<'RegistryPresets'>>(PRESETS, { presets }, etag);
    return { presets: r.body.presets, etag: r.etag ?? '' };
  }
}
