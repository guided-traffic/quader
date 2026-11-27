import { Injectable, inject } from '@angular/core';
import { ApiClient } from './client';
import { PageFetcher } from './pagination';
import { seg } from './query';
import { Schema } from './types';

/*
 * The stored content as the admin area's page Settings › Storage and the dashboard's card read it (storage.md R-STOR-061, R-STOR-107,
 * R-STOR-111, R-STOR-027; api-and-ui.md R-UI-262 to R-UI-266): the overview of a period, the object-store connections, the history of the
 * period (its cells, its problems and its findings) and the blobs no connection holds whole. The connections are only shown: no
 * operation changes one (the owner's decision 9). The writes are the recheck of a blob, which reads its copies again and writes no
 * object, and the acknowledgement of a finding, which resolves nothing.
 */

export type StorageOverview = Schema<'StorageOverview'>;
export type StorageConnection = Schema<'StorageConnection'>;
export type StorageConnectionFigures = Schema<'StorageConnectionFigures'>;
export type DamagedBlob = Schema<'DamagedBlob'>;
export type DamagedBlobCopy = Schema<'DamagedBlobCopy'>;
export type DamagedBlobRecheck = Schema<'DamagedBlobRecheck'>;
export type StorageHistory = Schema<'StorageHistory'>;
export type StorageHistoryConnection = Schema<'StorageHistoryConnection'>;
export type StorageHistoryBucket = Schema<'StorageHistoryBucket'>;
export type StorageProblem = Schema<'StorageProblem'>;
export type StorageFinding = Schema<'StorageFinding'>;

/** The periods of the page, shortest first (R-STOR-111); `30d` is the default. */
export type StoragePeriod = StorageHistory['period'];
export const STORAGE_PERIODS: readonly StoragePeriod[] = ['24h', '7d', '30d', '90d', '1y'];
export const DEFAULT_STORAGE_PERIOD: StoragePeriod = '30d';

/** One page of the blobs no connection holds whole and whether more follow. */
export interface DamagedPage {
  readonly items: readonly DamagedBlob[];
  readonly more: boolean;
}

/** What narrows the findings' list (R-STOR-111). */
export interface FindingFilter {
  readonly state?: 'open' | 'acknowledged' | 'resolved' | 'all';
  readonly connection?: string;
}

/** What narrows the problems' list (R-STOR-111). */
export interface ProblemFilter {
  readonly state?: 'ongoing' | 'ended' | 'all';
  readonly connection?: string;
}

export abstract class StorageApi {
  /** `GET /api/v1/storage/overview` (R-STOR-107): the states of now and the problems of the period, each derived by the server (Q-STOR-505). */
  abstract overview(period?: StoragePeriod): Promise<StorageOverview>;
  /** `GET /api/v1/system/storage-connections` (R-STOR-061): every connection, the answering replica's in its order. */
  abstract connections(): Promise<readonly StorageConnection[]>;
  /** `GET /api/v1/storage/history` (R-STOR-111): the cells of the period per connection. */
  abstract history(period: StoragePeriod): Promise<StorageHistory>;
  /** `GET /api/v1/storage/problems` (R-STOR-111), page by page, newest first. */
  abstract problems(period: StoragePeriod, filter?: ProblemFilter): PageFetcher<StorageProblem>;
  /** `GET /api/v1/storage/findings` (R-STOR-111), page by page, newest first. */
  abstract findings(period: StoragePeriod, filter?: FindingFilter): PageFetcher<StorageFinding>;
  /** `POST /api/v1/storage/findings/{id}/acknowledge`: records that the caller knows of an unresolved finding; it resolves nothing. */
  abstract acknowledge(id: number, note: string): Promise<StorageFinding>;
  /** The first `limit` blobs of `GET /api/v1/storage/damaged-blobs?scope=blobs` (R-STOR-027). */
  abstract damagedBlobs(limit: number): Promise<DamagedPage>;
  /** `POST /api/v1/storage/damaged-blobs/{sha256}/recheck`: reads every copy whole now and answers the copies after the reads. */
  abstract recheck(sha256: string): Promise<DamagedBlobRecheck>;
}

@Injectable()
export class HttpStorageApi extends StorageApi {
  private readonly client = inject(ApiClient);

  async overview(period: StoragePeriod = DEFAULT_STORAGE_PERIOD): Promise<StorageOverview> {
    return (await this.client.get('/api/v1/storage/overview', { params: { period } })).body;
  }

  async connections(): Promise<readonly StorageConnection[]> {
    return (await this.client.get('/api/v1/system/storage-connections')).body.connections;
  }

  async history(period: StoragePeriod): Promise<StorageHistory> {
    return (await this.client.get('/api/v1/storage/history', { params: { period } })).body;
  }

  problems(period: StoragePeriod, filter: ProblemFilter = {}): PageFetcher<StorageProblem> {
    return this.client.page<StorageProblem>('/api/v1/storage/problems', { period, ...filter });
  }

  findings(period: StoragePeriod, filter: FindingFilter = {}): PageFetcher<StorageFinding> {
    return this.client.page<StorageFinding>('/api/v1/storage/findings', { period, ...filter });
  }

  async acknowledge(id: number, note: string): Promise<StorageFinding> {
    const body = note.trim() === '' ? {} : { note: note.trim() };
    return (await this.client.post<StorageFinding>(`/api/v1/storage/findings/${seg(String(id))}/acknowledge`, body)).body;
  }

  async damagedBlobs(limit: number): Promise<DamagedPage> {
    const page = (await this.client.get('/api/v1/storage/damaged-blobs', { params: { scope: 'blobs', limit } })).body;
    return { items: page.items, more: page.nextCursor !== null };
  }

  async recheck(sha256: string): Promise<DamagedBlobRecheck> {
    return (await this.client.post<DamagedBlobRecheck>(`/api/v1/storage/damaged-blobs/${seg(sha256)}/recheck`)).body;
  }
}
