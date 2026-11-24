import { Injectable, inject } from '@angular/core';
import { ApiClient } from './client';
import { Schema } from './types';
import { repositoryBase } from './upstream-catalogue-api';

/** A request entry of a remote repository: one per subject, with its askers, its client refusals, its examination (R-CUR-015). */
export type DeliveryRequest = Schema<'DeliveryRequest'>;
export type DeliveryRequestPage = Schema<'DeliveryRequestPage'>;
export type DeliveryRequestState = DeliveryRequest['state'];
export type DeliveryRequestAllowed = Schema<'DeliveryRequestAllowed'>;
export type DeliveryRequestDeclined = Schema<'DeliveryRequestDeclined'>;
/** What a discard removed: the versions and the count of their files (R-CUR-016). */
export type ExaminationDiscarded = Schema<'ExaminationDiscarded'>;

/** The filters of the list; absent members filter nothing. */
export interface DeliveryRequestFilters {
  readonly state?: DeliveryRequestState;
  /** Only the entries the caller asks in. */
  readonly mine?: boolean;
}

/** How an entry is allowed: as asked or widened to its package, with or without a download of every version (R-CUR-015). */
export interface DeliveryRequestAllow {
  readonly as: 'asked' | 'package';
  readonly downloadEveryVersion: boolean;
  readonly note?: string;
}

/**
 * The requests for an allow of a remote repository (tag `remote-curation`, R-CUR-015, R-CUR-016): the list (every entry to the holders
 * of `manage` and the administrator, a reader's own entries to that reader), the ask, allow, decline and withdraw, and the discard of
 * what was fetched for examination. Who may do what is the server's (R-CUR-005): a refusal is an `ApiError`.
 */
export abstract class DeliveryRequestsApi {
  /** `GET …/delivery-requests`: open entries first, then by the last ask or refusal. */
  abstract list(repository: string, filters: DeliveryRequestFilters, cursor: string | null, limit: number): Promise<DeliveryRequestPage>;
  /** `POST …/delivery-requests`: the package with its future versions, or one version. */
  abstract ask(repository: string, ask: { readonly name: string; readonly version?: string; readonly note?: string }, idempotencyKey: string): Promise<DeliveryRequest>;
  /** `POST …/delivery-requests/{id}/allow`. */
  abstract allow(repository: string, id: string, allow: DeliveryRequestAllow, idempotencyKey: string): Promise<DeliveryRequestAllowed>;
  /** `POST …/delivery-requests/{id}/decline`. */
  abstract decline(repository: string, id: string, decline: { readonly reason: string; readonly discard: boolean }, idempotencyKey: string): Promise<DeliveryRequestDeclined>;
  /** `POST …/delivery-requests/{id}/withdraw`. */
  abstract withdraw(repository: string, id: string, idempotencyKey: string): Promise<DeliveryRequest>;
  /** `POST …/examinations/discard`: the versions of the subject the rules do not deliver; every such version without one. */
  abstract discard(repository: string, subject: { readonly name: string; readonly version?: string }, idempotencyKey: string): Promise<ExaminationDiscarded>;
}

@Injectable()
export class HttpDeliveryRequestsApi extends DeliveryRequestsApi {
  private readonly client = inject(ApiClient);

  async list(repository: string, filters: DeliveryRequestFilters, cursor: string | null, limit: number): Promise<DeliveryRequestPage> {
    const params: Record<string, string | number | boolean> = { limit };
    if (filters.state) params['state'] = filters.state;
    if (filters.mine) params['mine'] = true;
    if (cursor) params['cursor'] = cursor;
    return (await this.client.getJson<DeliveryRequestPage>(`${repositoryBase(repository)}/delivery-requests`, { params })).body;
  }

  async ask(repository: string, ask: { readonly name: string; readonly version?: string; readonly note?: string }, idempotencyKey: string): Promise<DeliveryRequest> {
    return (await this.client.post<DeliveryRequest>(`${repositoryBase(repository)}/delivery-requests`, ask, { idempotencyKey })).body;
  }

  async allow(repository: string, id: string, allow: DeliveryRequestAllow, idempotencyKey: string): Promise<DeliveryRequestAllowed> {
    const url = `${repositoryBase(repository)}/delivery-requests/${encodeURIComponent(id)}/allow`;
    return (await this.client.post<DeliveryRequestAllowed>(url, allow, { idempotencyKey })).body;
  }

  async decline(repository: string, id: string, decline: { readonly reason: string; readonly discard: boolean }, idempotencyKey: string): Promise<DeliveryRequestDeclined> {
    const url = `${repositoryBase(repository)}/delivery-requests/${encodeURIComponent(id)}/decline`;
    return (await this.client.post<DeliveryRequestDeclined>(url, decline, { idempotencyKey })).body;
  }

  async withdraw(repository: string, id: string, idempotencyKey: string): Promise<DeliveryRequest> {
    const url = `${repositoryBase(repository)}/delivery-requests/${encodeURIComponent(id)}/withdraw`;
    return (await this.client.post<DeliveryRequest>(url, undefined, { idempotencyKey })).body;
  }

  async discard(repository: string, subject: { readonly name: string; readonly version?: string }, idempotencyKey: string): Promise<ExaminationDiscarded> {
    return (await this.client.post<ExaminationDiscarded>(`${repositoryBase(repository)}/examinations/discard`, subject, { idempotencyKey })).body;
  }
}
