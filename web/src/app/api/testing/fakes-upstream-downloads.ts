import { Injectable, Provider } from '@angular/core';
import { ApiError } from '../problem';
import {
  UpstreamDownloadFinished,
  UpstreamDownloadList,
  UpstreamDownloadRunning,
  UpstreamDownloadSummary,
  UpstreamDownloadWaiting,
  UpstreamDownloadsApi,
} from '../upstream-downloads-api';

/** A download queue an upstream's rate limit holds: Docker Hub's, with 206 rows that wait; `over` sets what a case needs. */
export function waitingQueue(over: Partial<UpstreamDownloadWaiting> = {}): UpstreamDownloadWaiting {
  return {
    repository: { project: 'upstream', key: 'docker-hub', format: 'oci' },
    waiting: 206,
    upstream: 'Docker Hub',
    since: '2026-11-25T05:30:51Z',
    until: '2026-11-25T06:12:00Z',
    hinted: false,
    status: 429,
    ...over,
  };
}

/** A running download: an npm tarball a client's miss fetches, halfway, one file; `over` sets what a case needs. */
export function runningDownload(over: Partial<UpstreamDownloadRunning> = {}): UpstreamDownloadRunning {
  return {
    id: '01937d4e-0000-7000-8000-000000000001',
    repository: { project: 'web', key: 'npmjs', format: 'npm' },
    name: 'left-pad',
    version: '1.3.0',
    path: 'left-pad/-/left-pad-1.3.0.tgz',
    parts: 1,
    bytes: 512,
    total: 1024,
    bytesPerSecond: 256,
    startedAt: '2026-11-24T08:39:30Z',
    elapsedSeconds: 2,
    origin: 'client',
    queueOrigin: null,
    queueRow: null,
    by: { id: '0192f0c4-7a7e-7b51-9b1e-2f0d6c9e1a11', name: 'sam' },
    replica: 'quader-packages-7d9f-abc12',
    ...over,
  };
}

/** A finished download: the same tarball, completed in 1.5 s. */
export function finishedDownload(over: Partial<UpstreamDownloadFinished> = {}): UpstreamDownloadFinished {
  return {
    id: '01937d4c-0000-7000-8000-000000000001',
    repository: { project: 'web', key: 'npmjs', format: 'npm' },
    name: 'left-pad',
    version: '1.3.0',
    path: 'left-pad/-/left-pad-1.3.0.tgz',
    parts: 1,
    bytes: 3_145_728,
    total: 3_145_728,
    bytesPerSecond: 2_097_152,
    startedAt: '2026-11-24T08:38:01Z',
    finishedAt: '2026-11-24T08:38:02.5Z',
    durationSeconds: 1.5,
    outcome: 'completed',
    failure: null,
    origin: 'client',
    queueOrigin: null,
    queueRow: null,
    by: null,
    replica: 'quader-packages-7d9f-abc12',
    ...over,
  };
}

/** The list and the count in memory, with every call counted; `failWith` makes the next calls fail with that status. */
@Injectable()
export class FakeUpstreamDownloadsApi extends UpstreamDownloadsApi {
  running: UpstreamDownloadRunning[] = [];
  finished: UpstreamDownloadFinished[] = [];
  /** The queues an upstream's rate limit holds. */
  waiting: UpstreamDownloadWaiting[] = [];
  /** The count the API reports; the length of `running` when null. */
  runningCount: number | null = null;
  pollSeconds = 2;
  failWith: number | null = null;
  listCalls = 0;
  summaryCalls = 0;

  async list(): Promise<UpstreamDownloadList> {
    this.listCalls++;
    this.fail();
    return {
      asOf: '2026-11-24T08:40:12Z',
      pollSeconds: this.pollSeconds,
      runningCount: this.runningCount ?? this.running.length,
      running: [...this.running],
      finished: [...this.finished],
      waiting: [...this.waiting],
    };
  }

  async summary(): Promise<UpstreamDownloadSummary> {
    this.summaryCalls++;
    this.fail();
    return { running: this.runningCount ?? this.running.length, pollSeconds: this.pollSeconds };
  }

  private fail(): void {
    if (this.failWith !== null) throw new ApiError(this.failWith, { code: 'failed', detail: `HTTP ${this.failWith}` }, 'rid');
  }
}

export function provideUpstreamDownloadsFakes(): Provider[] {
  return [{ provide: UpstreamDownloadsApi, useClass: FakeUpstreamDownloadsApi }];
}
