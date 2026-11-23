import { Injectable, Provider } from '@angular/core';
import { ApiError } from '../problem';
import {
  RemoteDownload,
  RemoteDownloadBulk,
  RemoteDownloadCounts,
  RemoteDownloadFailure,
  RemoteDownloadFailureClass,
  RemoteDownloadFilters,
  RemoteDownloadPage,
  RemoteDownloadsApi,
} from '../remote-downloads-api';

/** A row of the queue: a waiting download of one version that a person started; `over` sets what a case needs. */
export function downloadRow(over: Partial<RemoteDownload> = {}): RemoteDownload {
  return {
    id: '01900000-0000-7000-8000-000000000001',
    name: 'left-pad',
    version: '1.3.0',
    origin: 'person',
    state: 'waiting',
    outcome: null,
    attempts: 0,
    maxAttempts: 5,
    nextAttemptAt: null,
    startedBy: null,
    bulk: null,
    request: null,
    files: 0,
    stored: 0,
    present: 0,
    refused: 0,
    bytes: 0,
    occurrences: 1,
    failure: null,
    createdAt: '2026-11-24T08:00:00Z',
    startedAt: null,
    finishedAt: null,
    delivery: 'delivered',
    ...over,
  };
}

/** A failure of an attempt: the upstream answered 503 with an HTML page. */
export function downloadFailure(over: Partial<RemoteDownloadFailure> = {}): RemoteDownloadFailure {
  return {
    class: 'upstream_error',
    step: 'status',
    code: 'upstream_error',
    file: 'left-pad/-/left-pad-1.3.0.tgz',
    address: 'https://registry.npmjs.org/left-pad/-/left-pad-1.3.0.tgz',
    status: 503,
    excerpt: '<html><body>Service Unavailable</body></html>',
    retryAfter: null,
    detail: 'The upstream registry.npmjs.org answered 503 Service Unavailable for left-pad-1.3.0.tgz.',
    attempt: 5,
    at: '2026-11-24T08:05:00Z',
    ...over,
  };
}

/** A bulk of every version of a package that expands. */
export function downloadBulk(over: Partial<RemoteDownloadBulk> = {}): RemoteDownloadBulk {
  return {
    id: '01900000-0000-7000-8000-0000000000b1',
    level: 'package',
    subject: 'lodash',
    state: 'queued',
    bounds: { maxVersions: 100, maxBytes: 107374182400, includePrereleases: true },
    versionsFound: 3,
    versionsQueued: 3,
    failure: null,
    startedBy: null,
    createdAt: '2026-11-24T07:59:00Z',
    finishedAt: null,
    rows: { running: 1, waiting: 2 },
    ...over,
  };
}

/** The queue in memory: the rows as given, filtered and paged as the API does, and every call recorded. */
@Injectable()
export class FakeRemoteDownloadsApi extends RemoteDownloadsApi {
  rows: RemoteDownload[] = [];
  bulks: RemoteDownloadBulk[] = [];
  /** The counts the list answers; computed from the rows when null. */
  counts: RemoteDownloadCounts | null = null;
  /** Answers every list with this error, where set. */
  listError: ApiError | null = null;
  /** Answers every retry or cancel with this error, where set. */
  actError: ApiError | null = null;
  readonly calls: string[] = [];
  readonly listCalls: { repository: string; filters: RemoteDownloadFilters; cursor: string | null; limit: number }[] = [];

  override async list(repository: string, filters: RemoteDownloadFilters, cursor: string | null, limit: number): Promise<RemoteDownloadPage> {
    this.listCalls.push({ repository, filters, cursor, limit });
    if (this.listError) throw this.listError;
    const matching = this.rows.filter(
      (r) =>
        (!filters.state || r.state === filters.state) &&
        (!filters.class || r.failure?.class === filters.class) &&
        (!filters.origin || r.origin === filters.origin) &&
        (!filters.bulk || r.bulk === filters.bulk) &&
        (!filters.name || r.name === filters.name),
    );
    const start = cursor === null ? 0 : Number(cursor);
    const items = matching.slice(start, start + limit);
    const next = start + limit < matching.length ? String(start + limit) : null;
    const count = (s: RemoteDownload['state']): number => this.rows.filter((r) => r.state === s).length;
    return {
      items,
      nextCursor: next,
      counts: this.counts ?? { waiting: count('waiting'), running: count('running'), failed: count('failed'), done: count('done') },
    };
  }

  override async retry(repository: string, id: string): Promise<RemoteDownload> {
    this.calls.push(`retry ${repository} ${id}`);
    if (this.actError) throw this.actError;
    return this.change(id, { state: 'waiting', attempts: 0, failure: null });
  }

  override async retryFailed(repository: string, filter: { readonly class?: RemoteDownloadFailureClass; readonly bulk?: string }): Promise<number> {
    this.calls.push(`retry-failed ${repository} ${filter.class ?? '-'} ${filter.bulk ?? '-'}`);
    const failed = this.rows.filter((r) => r.state === 'failed' && (!filter.class || r.failure?.class === filter.class));
    for (const r of failed) this.change(r.id, { state: 'waiting', attempts: 0 });
    return failed.length;
  }

  override async cancel(repository: string, id: string): Promise<RemoteDownload> {
    this.calls.push(`cancel ${repository} ${id}`);
    if (this.actError) throw this.actError;
    return this.change(id, { state: 'cancelled' });
  }

  override async bulk(repository: string, id: string): Promise<RemoteDownloadBulk> {
    this.calls.push(`bulk ${repository} ${id}`);
    const b = this.bulks.find((x) => x.id === id);
    if (!b) throw new ApiError(404, { code: 'not_found', detail: 'not found' }, 'rid-404');
    return b;
  }

  override async cancelBulk(repository: string, id: string): Promise<RemoteDownloadBulk> {
    this.calls.push(`cancel-bulk ${repository} ${id}`);
    const b = await this.bulk(repository, id);
    const next = { ...b, state: 'cancelled' as const };
    this.bulks = this.bulks.map((x) => (x.id === id ? next : x));
    return next;
  }

  private change(id: string, over: Partial<RemoteDownload>): RemoteDownload {
    const r = this.rows.find((x) => x.id === id);
    if (!r) throw new ApiError(404, { code: 'not_found', detail: 'not found' }, 'rid-404');
    const next = { ...r, ...over };
    this.rows = this.rows.map((x) => (x.id === id ? next : x));
    return next;
  }
}

export function provideRemoteDownloadsFakes(): Provider[] {
  return [{ provide: RemoteDownloadsApi, useClass: FakeRemoteDownloadsApi }];
}
