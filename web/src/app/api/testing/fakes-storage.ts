import { Injectable, Provider } from '@angular/core';
import { ApiError } from '../problem';
import { DamagedBlob, DamagedBlobRecheck, DamagedPage, StorageApi, StorageConnection, StorageOverview } from '../storage-api';

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
    encryption: { state: 'off', encrypted: [], plain: ['default'], providerConfigured: [], encryptedShare: 0 },
    integrity: {
      state: 'verified',
      downloadVerify: true,
      authenticatedReads: [],
      scrub: { coverage: 1, overdue: 0, lastRunAt: '2026-11-22T10:07:03Z', nextRunAt: '2026-11-22T11:07:00Z', running: false },
      damagedNow: { copies: 0, blobs: 0 },
    },
    ...over,
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

  override async overview(): Promise<StorageOverview> {
    this.overviewReads++;
    if (this.overviewResult instanceof ApiError) throw this.overviewResult;
    return structuredClone(this.overviewResult);
  }

  override async connections(): Promise<readonly StorageConnection[]> {
    this.connectionReads++;
    if (this.connectionsResult instanceof ApiError) throw this.connectionsResult;
    return structuredClone(this.connectionsResult);
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
