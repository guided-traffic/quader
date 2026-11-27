import { Injectable, Provider } from '@angular/core';
import { ApiError } from '../problem';
import { Page, PageFetcher } from '../pagination';
import {
  DamagedBlob,
  DamagedBlobRecheck,
  DamagedPage,
  FindingFilter,
  ProblemFilter,
  StorageApi,
  StorageConnection,
  StorageFinding,
  StorageHistory,
  StorageHistoryBucket,
  StorageOverview,
  StoragePeriod,
  StorageProblem,
} from '../storage-api';

/** The time the samples' summary was computed. */
export const SAMPLE_SUMMARY_AT = '2026-11-22T10:17:00Z';

/**
 * One connection as an installation with one store shows it (R-STOR-061): `default`, active, every blob on it, read whole within its
 * period. `over` replaces members; `figures` replaces members of its summary.
 */
export function sampleConnection(
  over: Partial<StorageConnection> = {},
  figures: Partial<NonNullable<StorageConnection['summary']>> = {},
): StorageConnection {
  const base: StorageConnection = {
    id: 'default',
    position: 1,
    state: 'active',
    endpoint: 'https://object.storage.eu01.example.net',
    bucket: 'quader-blobs',
    prefix: 'quader',
    credentialSource: 'static',
    encrypted: false,
    encryptedSince: null,
    keyedNames: false,
    drain: false,
    capacityBytes: null,
    replicas: { configured: 3, live: 3, circuitOpen: 0 },
    queue: { blobs: 0, oldestQueuedAt: null },
    summary: {
      id: over.id ?? 'default',
      computedAt: SAMPLE_SUMMARY_AT,
      presentCopies: 412_093,
      presentBytes: 1_893_341_184_512,
      missingCopies: 0,
      corruptCopies: 0,
      verifiedWithinPeriod: 412_093,
      scrubOverdue: 0,
      backlogBlobs: 0,
      backlogBytes: 0,
      fillRatio: 1,
      soleCopies: 0,
      soleBytes: 0,
      keyedCopies: 0,
      ...figures,
    },
    scrubPeriodSeconds: 2_592_000,
    firstSeenAt: '2026-11-20T07:12:40Z',
    filledAt: '2026-11-20T08:02:11Z',
    drainedAt: null,
    retiredAt: null,
    passStartedAt: null,
    lastFullPassAt: '2026-11-22T09:58:40Z',
    capabilities: {
      payloadChecksumEnforced: true,
      batchDelete: true,
      metadata: true,
      copyReplacesMetadata: true,
      serverSideCopyFrom: {},
      sseApplied: { algorithm: '', kmsKeyId: '' },
      checkedAt: '2026-11-22T06:00:12Z',
    },
    settings: {
      region: 'eu01',
      forcePathStyle: false,
      payloadChecksum: 'sha256',
      requestChecksumCalculation: 'when_supported',
      responseChecksumValidation: 'when_supported',
      batchDelete: true,
      storageClass: '',
      sseMode: 'none',
      sseKmsKeyId: '',
      sseBucketKey: true,
      scrubBytesPerSecond: 52_428_800,
    },
    storageKeys: [],
  };
  return { ...base, ...over };
}

/**
 * The overview of an installation with one store (R-STOR-107), as the server derives it for `sampleConnection()`: healthy, one store,
 * plain, verified. Each member of `over` replaces that member whole.
 */
export function sampleOverview(over: Partial<StorageOverview> = {}): StorageOverview {
  return {
    at: '2026-11-22T10:39:22Z',
    figuresAt: SAMPLE_SUMMARY_AT,
    connections: { state: 'healthy', configured: 1, reachable: 1, unreachable: [], detached: [] },
    stored: { blobs: 412_093, bytes: 1_893_341_184_512 },
    availability: {
      state: 'one_store',
      target: 1,
      minCopies: 1,
      perBlob: [{ copies: 1, blobs: 412_093, bytes: 1_893_341_184_512 }],
      belowTarget: { blobs: 0, bytes: 0 },
      loss: [{ connection: 'default', blobs: 412_093, bytes: 1_893_341_184_512 }],
    },
    encryption: { state: 'off', encrypted: [], plain: ['default'], providerConfigured: [], providerApplied: [], encryptedShare: 0 },
    integrity: {
      state: 'verified',
      downloadVerify: true,
      authenticatedReads: [],
      scrub: { coverage: 1, overdue: 0, lastRunAt: '2026-11-22T10:07:03Z', nextRunAt: '2026-11-22T11:07:00Z', running: false },
      damagedNow: { copies: 0, blobs: 0 },
    },
    problems: { period: '30d', state: 'none', count: 0, open: 0, acknowledged: 0, worst: null },
    ...over,
  };
}

/** The cells of each period (R-STOR-111): their number and length. */
export const PERIOD_CELLS: Readonly<Record<StoragePeriod, { readonly cells: number; readonly seconds: number }>> = {
  '24h': { cells: 24, seconds: 3_600 },
  '7d': { cells: 28, seconds: 21_600 },
  '30d': { cells: 30, seconds: 86_400 },
  '90d': { cells: 30, seconds: 259_200 },
  '1y': { cells: 52, seconds: 604_800 },
};

/** A quiet cell; `over` replaces members. */
export function sampleBucket(start: string, over: Partial<StorageHistoryBucket> = {}): StorageHistoryBucket {
  return {
    start,
    tone: 'none',
    unreachableSeconds: 0,
    replicas: 0,
    startChecksFailed: 0,
    damagedCopies: 0,
    damagedBytes: 0,
    unavailableBlobs: 0,
    readFallovers: 0,
    readsUnavailable: 0,
    writeFallovers: 0,
    uploadsRefused: 0,
    copiesFailed: 0,
    readsRefused: 0,
    unknownKeyReads: 0,
    ...over,
  };
}

/**
 * The history of a quiet period (R-STOR-111) up to `to`, one strip per connection id; `cells` replaces cells by their index, per
 * connection, and `findings` the totals of a connection.
 */
export function sampleHistory(
  period: StoragePeriod = '30d',
  ids: readonly string[] = ['default'],
  cells: Readonly<Record<string, Readonly<Record<number, Partial<StorageHistoryBucket>>>>> = {},
  findings: Readonly<Record<string, Partial<StorageHistory['connections'][number]['findings']>>> = {},
  to = '2026-11-22T10:39:22Z',
): StorageHistory {
  const { cells: n, seconds } = PERIOD_CELLS[period];
  const end = Date.parse(to);
  const from = end - n * seconds * 1000;
  return {
    period,
    from: new Date(from).toISOString(),
    to,
    bucketSeconds: seconds,
    connections: ids.map((id) => ({
      id,
      buckets: Array.from({ length: n }, (_, i) => sampleBucket(new Date(from + i * seconds * 1000).toISOString(), cells[id]?.[i] ?? {})),
      findings: { count: 0, bytes: 0, unlisted: 0, byOutcome: {}, bySource: {}, ...findings[id] },
    })),
  };
}

/** An outage of a connection that ended (R-STOR-111). */
export function sampleProblem(over: Partial<StorageProblem> = {}): StorageProblem {
  return {
    kind: 'unreachable',
    connection: 'default',
    startedAt: '2026-11-19T01:10:00Z',
    endedAt: '2026-11-19T02:22:00Z',
    seconds: 4_320,
    tone: 'warning',
    replicas: 3,
    occurrences: 4,
    count: 0,
    bytes: 0,
    resolved: 0,
    open: 0,
    acknowledged: 0,
    unavailable: 0,
    codes: ['timeout'],
    sources: [],
    steps: [],
    ...over,
  };
}

/** A copy the scrub found damaged and repaired (R-STOR-108). */
export function sampleFinding(over: Partial<StorageFinding> = {}): StorageFinding {
  return {
    id: 4182,
    detectedAt: '2026-11-21T03:12:09Z',
    connection: 'default',
    sha256: '5be0c1d2a3f4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f60718293a4b5c6d7e',
    status: 'corrupt',
    source: 'scrub',
    reason: 'digest',
    storedSize: 1_153_433,
    unavailable: false,
    resolvedAt: '2026-11-21T03:12:10Z',
    resolution: 'rewritten',
    acknowledged: null,
    items: [{ project: 'web', repository: 'npm-local', path: 'lodash/-/lodash-4.17.21.tgz', trashed: false }],
    itemCount: 3,
    ...over,
  };
}

/** A page fetcher over a list in memory, the index as the cursor; each read notes the filter it was asked with. */
function pages<T>(all: () => readonly T[], asked: unknown[], filter: unknown, match: (item: T) => boolean): PageFetcher<T> {
  return async (cursor, limit): Promise<Page<T>> => {
    asked.push(filter);
    const list = all().filter(match);
    const start = cursor === null ? 0 : Number(cursor);
    const items = structuredClone(list.slice(start, start + limit));
    return { items, nextCursor: start + limit < list.length ? String(start + limit) : null };
  };
}

/** A blob no connection holds whole (R-STOR-027): one copy missing, one item that references it. */
export function sampleDamagedBlob(over: Partial<DamagedBlob> = {}): DamagedBlob {
  return {
    sha256: '3a7bd3e2360a3d29eea436fcfb7e44c735d117c42d1c1835420b6b9942dd4f1b',
    size: 18_432,
    contentStatus: 'missing',
    copies: [{ connection: 'default', state: 'missing', verifiedAt: '2026-11-02T03:07:41Z', checkedAt: '2026-11-22T10:07:12Z', stateChangedAt: '2026-11-22T10:07:12Z' }],
    items: [{ project: 'platform', repository: 'npm-local', path: 'left-pad/-/left-pad-1.3.0.tgz', trashed: false }],
    itemCount: 1,
    ...over,
  };
}

@Injectable()
export class FakeStorageApi extends StorageApi {
  overviewResult: StorageOverview | ApiError = sampleOverview();
  overviewReads = 0;
  connectionsResult: readonly StorageConnection[] | ApiError = [sampleConnection()];
  damagedResult: DamagedPage | ApiError = { items: [], more: false };
  /** The answer of the next recheck; by default the blob read whole on its connection. */
  recheckResult: DamagedBlobRecheck | ApiError | null = null;
  connectionReads = 0;
  damagedReads = 0;
  rechecks: string[] = [];
  /** The answer of history(); by default a quiet period of the asked one with the connections of `connectionsResult`. */
  historyResult: StorageHistory | ApiError | null = null;
  historyReads: StoragePeriod[] = [];
  overviewPeriods: StoragePeriod[] = [];
  problemsResult: readonly StorageProblem[] = [];
  findingsResult: StorageFinding[] = [];
  problemQueries: unknown[] = [];
  findingQueries: unknown[] = [];
  acknowledgements: { id: number; note: string }[] = [];
  acknowledgeResult: ApiError | null = null;

  override async overview(period: StoragePeriod = '30d'): Promise<StorageOverview> {
    this.overviewReads++;
    this.overviewPeriods.push(period);
    if (this.overviewResult instanceof ApiError) throw this.overviewResult;
    return structuredClone(this.overviewResult);
  }

  override async connections(): Promise<readonly StorageConnection[]> {
    this.connectionReads++;
    if (this.connectionsResult instanceof ApiError) throw this.connectionsResult;
    return structuredClone(this.connectionsResult);
  }

  override async history(period: StoragePeriod): Promise<StorageHistory> {
    this.historyReads.push(period);
    if (this.historyResult instanceof ApiError) throw this.historyResult;
    if (this.historyResult) return structuredClone(this.historyResult);
    const ids = this.connectionsResult instanceof ApiError ? [] : this.connectionsResult.filter((c) => c.state !== 'retired').map((c) => c.id);
    return sampleHistory(period, ids);
  }

  override problems(period: StoragePeriod, filter: ProblemFilter = {}): PageFetcher<StorageProblem> {
    return pages(
      () => this.problemsResult,
      this.problemQueries,
      { period, ...filter },
      (p) => (filter.state === 'ongoing' ? p.endedAt === null : filter.state === 'ended' ? p.endedAt !== null : true),
    );
  }

  override findings(period: StoragePeriod, filter: FindingFilter = {}): PageFetcher<StorageFinding> {
    return pages(
      () => this.findingsResult,
      this.findingQueries,
      { period, ...filter },
      (f) => (filter.state === 'open' ? !f.resolution && !f.acknowledged : true),
    );
  }

  override async acknowledge(id: number, note: string): Promise<StorageFinding> {
    this.acknowledgements.push({ id, note });
    if (this.acknowledgeResult) throw this.acknowledgeResult;
    const f = this.findingsResult.find((x) => x.id === id) ?? sampleFinding({ id });
    const done: StorageFinding = {
      ...f,
      acknowledged: { at: '2026-11-22T10:40:00Z', by: { id: '0192f0c4-7a7e-7b51-9b1e-2f0d6c9e1a10', name: 'alice' }, note: note.trim() },
    };
    this.findingsResult = this.findingsResult.map((x) => (x.id === id ? done : x));
    return structuredClone(done);
  }

  override async damagedBlobs(limit: number): Promise<DamagedPage> {
    this.damagedReads++;
    if (this.damagedResult instanceof ApiError) throw this.damagedResult;
    return { items: structuredClone(this.damagedResult.items.slice(0, limit)), more: this.damagedResult.more || this.damagedResult.items.length > limit };
  }

  override async recheck(sha256: string): Promise<DamagedBlobRecheck> {
    this.rechecks.push(sha256);
    if (this.recheckResult instanceof ApiError) throw this.recheckResult;
    return (
      this.recheckResult ?? {
        sha256,
        contentStatus: 'ok',
        copies: [{ connection: 'default', state: 'present', verifiedAt: SAMPLE_SUMMARY_AT, checkedAt: SAMPLE_SUMMARY_AT, stateChangedAt: SAMPLE_SUMMARY_AT }],
      }
    );
  }
}

export function provideStorageFakes(): Provider[] {
  return [{ provide: StorageApi, useClass: FakeStorageApi }];
}
