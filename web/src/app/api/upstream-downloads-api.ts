import { Injectable, forwardRef, inject } from '@angular/core';
import { ApiClient } from './client';
import { Schema } from './types';

/** The running and the last finished downloads from the upstreams of remote repositories (operations-and-observability.md R-OPS-052). */
export type UpstreamDownloadList = Schema<'UpstreamDownloadList'>;
/** A download that runs now (R-OPS-049, R-OPS-050). */
export type UpstreamDownloadRunning = Schema<'UpstreamDownloadRunning'>;
/** A download that ended (R-OPS-049, R-OPS-051). */
export type UpstreamDownloadFinished = Schema<'UpstreamDownloadFinished'>;
/** The number of running downloads and the publication interval, for the top bar's button. */
export type UpstreamDownloadSummary = Schema<'UpstreamDownloadSummary'>;
/** What caused a download: `client`, `queue`, `mirror`, `scanner_database`. */
export type UpstreamDownloadOrigin = Schema<'UpstreamDownloadOrigin'>;
/** The origin of the queue row a download of origin `queue` belongs to. */
export type UpstreamDownloadQueueOrigin = Schema<'UpstreamDownloadQueueOrigin'>;
/** How a download ended: `completed`, `failed`, `cancelled`, `interrupted`. */
export type UpstreamDownloadOutcome = UpstreamDownloadFinished['outcome'];
/** A download queue an upstream's rate limit holds (remote-curation.md R-CUR-011, Q-CUR-176): its rows wait, none runs. */
export type UpstreamDownloadWaiting = Schema<'UpstreamDownloadWaiting'>;
/** A running or a finished download: the facts both carry. */
export type UpstreamDownload = UpstreamDownloadRunning | UpstreamDownloadFinished;

/**
 * The downloads from upstreams (tag `system`, R-OPS-052): the list behind the panel of the top bar's button **Downloads**, and the
 * count behind its badge. Both need `content:metadata-read`; a refusal is an `ApiError`. A poll does not retry: its next tick does.
 */
@Injectable({ providedIn: 'root', useClass: forwardRef(() => HttpUpstreamDownloadsApi) })
export abstract class UpstreamDownloadsApi {
  /** `GET /api/v1/system/upstream-downloads`. */
  abstract list(): Promise<UpstreamDownloadList>;
  /** `GET /api/v1/system/upstream-downloads/summary`. */
  abstract summary(): Promise<UpstreamDownloadSummary>;
}

@Injectable()
export class HttpUpstreamDownloadsApi extends UpstreamDownloadsApi {
  private readonly client = inject(ApiClient);

  async list(): Promise<UpstreamDownloadList> {
    return (await this.client.get('/api/v1/system/upstream-downloads', { noRetry: true })).body;
  }

  async summary(): Promise<UpstreamDownloadSummary> {
    return (await this.client.get('/api/v1/system/upstream-downloads/summary', { noRetry: true })).body;
  }
}
