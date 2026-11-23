import { Injectable, inject } from '@angular/core';
import { ApiClient } from './client';
import { seg } from './query';
import { Schema } from './types';

/*
 * The object-store connections and the blobs no connection holds whole, as the admin area's tab Storage reads them (storage.md
 * R-STOR-061, R-STOR-027; api-and-ui.md R-UI-196). The connections are only shown: no operation changes one (the owner's decision 9).
 * The one write is the recheck of a damaged blob, which reads its copies again and writes no object.
 */

export type StorageConnection = Schema<'StorageConnection'>;
export type StorageConnectionFigures = Schema<'StorageConnectionFigures'>;
export type DamagedBlob = Schema<'DamagedBlob'>;
export type DamagedBlobCopy = Schema<'DamagedBlobCopy'>;
export type DamagedBlobRecheck = Schema<'DamagedBlobRecheck'>;

/** One page of the blobs no connection holds whole and whether more follow. */
export interface DamagedPage {
  readonly items: readonly DamagedBlob[];
  readonly more: boolean;
}

export abstract class StorageApi {
  /** `GET /api/v1/system/storage-connections` (R-STOR-061): every connection, the answering replica's in its order. */
  abstract connections(): Promise<readonly StorageConnection[]>;
  /** The first `limit` blobs of `GET /api/v1/storage/damaged-blobs?scope=blobs` (R-STOR-027). */
  abstract damagedBlobs(limit: number): Promise<DamagedPage>;
  /** `POST /api/v1/storage/damaged-blobs/{sha256}/recheck`: reads every copy whole now and answers the copies after the reads. */
  abstract recheck(sha256: string): Promise<DamagedBlobRecheck>;
}

@Injectable()
export class HttpStorageApi extends StorageApi {
  private readonly client = inject(ApiClient);

  async connections(): Promise<readonly StorageConnection[]> {
    return (await this.client.get('/api/v1/system/storage-connections')).body.connections;
  }

  async damagedBlobs(limit: number): Promise<DamagedPage> {
    const page = (await this.client.get('/api/v1/storage/damaged-blobs', { params: { scope: 'blobs', limit } })).body;
    return { items: page.items, more: page.nextCursor !== null };
  }

  async recheck(sha256: string): Promise<DamagedBlobRecheck> {
    return (await this.client.post<DamagedBlobRecheck>(`/api/v1/storage/damaged-blobs/${seg(sha256)}/recheck`)).body;
  }
}
