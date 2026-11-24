import { Injectable, inject } from '@angular/core';
import { ApiClient } from './client';
import { Schema } from './types';

/** A row of a remote repository's download queue (remote-curation.md R-CUR-011, R-CUR-012). */
export type RemoteDownload = Schema<'RemoteDownload'>;
/** A row's state: `waiting`, `running`, `done`, `failed`, `cancelled`. */
export type RemoteDownloadState = RemoteDownload['state'];
/** Who or what put a row into the queue: `person`, `request`, `allow`, `bulk`, `probe`, `client`. */
export type RemoteDownloadOrigin = RemoteDownload['origin'];
/** The failure of a row's attempt in its own words (Q-CUR-20). */
export type RemoteDownloadFailure = Schema<'RemoteDownloadFailure'>;
/** The closed list of a failure's classes (R-CUR-011). */
export type RemoteDownloadFailureClass = Schema<'RemoteDownloadFailureClass'>;
/** The closed list of a failure's steps (R-CUR-011). */
export type RemoteDownloadFailureStep = RemoteDownloadFailure['step'];
/** One page of the queue with the counts of its rows by state. */
export type RemoteDownloadPage = Schema<'RemoteDownloadPage'>;
/** The rows of the queue by state: waiting, running, failed, done. */
export type RemoteDownloadCounts = Schema<'RemoteDownloadCounts'>;
/** A bulk download with its counters and its rows by state (R-CUR-010, R-CUR-012). */
export type RemoteDownloadBulk = Schema<'RemoteDownloadBulk'>;
/** One page of a repository's bulk downloads, newest first (Q-CUR-168). */
export type RemoteDownloadBulkPage = Schema<'RemoteDownloadBulkPage'>;
/** A download ahead of a client: one version, every version of a package, or a sub-section, with a bulk's bounds (R-CUR-010). */
export type RemoteDownloadRequest = Omit<Schema<'RemoteDownloadRequest'>, 'includePrereleases'> & { readonly includePrereleases?: boolean };
/** What a download ahead would fetch, before it is started (R-CUR-010). */
export type RemoteDownloadPreview = Schema<'RemoteDownloadPreview'>;
/** The answer of a start: the row of one version, or the bulk (R-CUR-010). */
export type RemoteDownloadStarted = Schema<'RemoteDownloadStarted'>;

/** The states of a row in the order the tab lists them (R-CUR-018). */
export const DOWNLOAD_STATES: readonly RemoteDownloadState[] = ['waiting', 'running', 'failed', 'done', 'cancelled'];

/** The failure classes in the order of R-CUR-011, the options of the tab's filter **Failure**. */
export const DOWNLOAD_FAILURE_CLASSES: readonly RemoteDownloadFailureClass[] = [
  'upstream_not_found',
  'upstream_auth',
  'upstream_rate_limited',
  'upstream_error',
  'upstream_unexpected',
  'network',
  'tls',
  'egress_denied',
  'integrity',
  'content_refused',
  'gate_refused',
  'circuit_open',
  'bound',
  'storage',
  'internal',
];

/** A row that waits or runs: the tab refreshes itself while one exists (R-CUR-018). */
export function downloadActive(state: RemoteDownloadState): boolean {
  return state === 'waiting' || state === 'running';
}

/** A row that a retry makes wait again: `failed` or `cancelled` (R-CUR-012). */
export function downloadRetryable(state: RemoteDownloadState): boolean {
  return state === 'failed' || state === 'cancelled';
}

/** The filters of the queue's list (R-CUR-012); absent members filter nothing. */
export interface RemoteDownloadFilters {
  readonly state?: RemoteDownloadState;
  readonly class?: RemoteDownloadFailureClass;
  readonly origin?: RemoteDownloadOrigin;
  readonly bulk?: string;
  readonly name?: string;
}

/**
 * The download queue of a remote repository (tag `remote-curation`, R-CUR-012): its rows with their counts, one row, the retry of a
 * row and of every failed row that matches, the cancel of a row, and a bulk with its cancel. `repository` is the qualified name
 * `<project>/<key>`. Who may do what is the server's (R-CUR-005): a refusal is an `ApiError`.
 */
export abstract class RemoteDownloadsApi {
  /** `GET …/downloads`: one page, newest first within running and waiting first, with the counts by state. */
  abstract list(repository: string, filters: RemoteDownloadFilters, cursor: string | null, limit: number): Promise<RemoteDownloadPage>;
  /** `POST …/downloads/{id}/retry`: the row, waiting again. */
  abstract retry(repository: string, id: string, idempotencyKey: string): Promise<RemoteDownload>;
  /** `POST …/downloads/retry`: how many failed rows wait again. */
  abstract retryFailed(
    repository: string,
    filter: { readonly class?: RemoteDownloadFailureClass; readonly bulk?: string },
    idempotencyKey: string,
  ): Promise<number>;
  /** `POST …/downloads/{id}/cancel`: the row after the cancel. */
  abstract cancel(repository: string, id: string, idempotencyKey: string): Promise<RemoteDownload>;
  /** `GET …/download-bulks`: one page of the repository's bulks, newest first (Q-CUR-168). */
  abstract listBulks(repository: string, cursor: string | null, limit: number): Promise<RemoteDownloadBulkPage>;
  /** `GET …/download-bulks/{id}`. */
  abstract bulk(repository: string, id: string): Promise<RemoteDownloadBulk>;
  /** `POST …/download-bulks/{id}/cancel`: the bulk after the cancel. */
  abstract cancelBulk(repository: string, id: string, idempotencyKey: string): Promise<RemoteDownloadBulk>;
  /** `POST …/downloads/preview`: what the download would fetch, with the bounds that hold (R-CUR-010). */
  abstract preview(repository: string, request: RemoteDownloadRequest): Promise<RemoteDownloadPreview>;
  /** `POST …/downloads`: the row of one version, or the bulk of a package or a sub-section (R-CUR-010). */
  abstract start(repository: string, request: RemoteDownloadRequest, idempotencyKey: string): Promise<RemoteDownloadStarted>;
}

/** `/api/v1/projects/{project}/repositories/{key}` of a qualified name. */
function base(repository: string): string {
  const slash = repository.indexOf('/');
  const project = repository.slice(0, slash);
  const key = repository.slice(slash + 1);
  return `/api/v1/projects/${encodeURIComponent(project)}/repositories/${encodeURIComponent(key)}`;
}

@Injectable()
export class HttpRemoteDownloadsApi extends RemoteDownloadsApi {
  private readonly client = inject(ApiClient);

  async list(repository: string, filters: RemoteDownloadFilters, cursor: string | null, limit: number): Promise<RemoteDownloadPage> {
    const params: Record<string, string | number> = { limit };
    if (cursor !== null) params['cursor'] = cursor;
    for (const [k, v] of Object.entries(filters) as [string, string | undefined][]) if (v) params[k] = v;
    return (await this.client.getJson<RemoteDownloadPage>(`${base(repository)}/downloads`, { params })).body;
  }

  async retry(repository: string, id: string, idempotencyKey: string): Promise<RemoteDownload> {
    return (await this.client.post<RemoteDownload>(`${base(repository)}/downloads/${encodeURIComponent(id)}/retry`, undefined, { idempotencyKey })).body;
  }

  async retryFailed(
    repository: string,
    filter: { readonly class?: RemoteDownloadFailureClass; readonly bulk?: string },
    idempotencyKey: string,
  ): Promise<number> {
    const body: Record<string, string> = {};
    if (filter.class) body['class'] = filter.class;
    if (filter.bulk) body['bulk'] = filter.bulk;
    return (await this.client.post<{ retried: number }>(`${base(repository)}/downloads/retry`, body, { idempotencyKey })).body.retried;
  }

  async cancel(repository: string, id: string, idempotencyKey: string): Promise<RemoteDownload> {
    return (await this.client.post<RemoteDownload>(`${base(repository)}/downloads/${encodeURIComponent(id)}/cancel`, undefined, { idempotencyKey })).body;
  }

  async listBulks(repository: string, cursor: string | null, limit: number): Promise<RemoteDownloadBulkPage> {
    const params: Record<string, string | number> = { limit };
    if (cursor !== null) params['cursor'] = cursor;
    return (await this.client.getJson<RemoteDownloadBulkPage>(`${base(repository)}/download-bulks`, { params })).body;
  }

  async bulk(repository: string, id: string): Promise<RemoteDownloadBulk> {
    return (await this.client.getJson<RemoteDownloadBulk>(`${base(repository)}/download-bulks/${encodeURIComponent(id)}`)).body;
  }

  async cancelBulk(repository: string, id: string, idempotencyKey: string): Promise<RemoteDownloadBulk> {
    return (await this.client.post<RemoteDownloadBulk>(`${base(repository)}/download-bulks/${encodeURIComponent(id)}/cancel`, undefined, { idempotencyKey })).body;
  }

  async preview(repository: string, request: RemoteDownloadRequest): Promise<RemoteDownloadPreview> {
    return (await this.client.post<RemoteDownloadPreview>(`${base(repository)}/downloads/preview`, request)).body;
  }

  async start(repository: string, request: RemoteDownloadRequest, idempotencyKey: string): Promise<RemoteDownloadStarted> {
    return (await this.client.post<RemoteDownloadStarted>(`${base(repository)}/downloads`, request, { idempotencyKey })).body;
  }
}
