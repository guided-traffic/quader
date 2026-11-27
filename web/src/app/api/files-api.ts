import { Injectable, forwardRef, inject } from '@angular/core';
import { ApiClient } from './client';
import { Child, FileReads, Item } from './file-reads';
import { PageFetcher } from './pagination';
import { DeleteOutcome, JobReference } from './repositories-api';
import { contentPath, repositoryUrl } from './refs';
import { Schema } from './types';
import { TransferOptions, UploadTransfer } from './upload-transfer';

export type { Child, Item } from './file-reads';
export type ItemStats = Schema<'ItemStats'>;

/** A file larger than this is not hashed in the browser; the server's own digest is used (R-UI-012). */
export const HASH_LIMIT_BYTES = 256 * 1024 * 1024;

/**
 * `ui.deploy.max_upload_bytes` (R-UI-012, default 100 MiB). The UI cannot read the configuration
 * (no API member carries it), so the default is a constant; the server's own limit still decides, and answers
 * 413 on the content route before it spools a larger body (R-API-025, Q-UI-106).
 */
export const UPLOAD_LIMIT_BYTES = 100 * 1024 * 1024;

/** One upload: the target, the file and the way to follow and stop it. */
export interface UploadRequest {
  readonly repository: string;
  readonly path: string;
  readonly file: Blob;
  /** `sha-256=:<base64>:` when the file was hashed in the browser. */
  readonly reprDigest?: string;
  /** False sends `If-None-Match: *`, so an existing path is refused instead of replaced. */
  readonly overwrite: boolean;
  readonly onProgress?: (loaded: number, total: number) => void;
  readonly signal?: AbortSignal;
}

export { contentPath, encodePath, isFilesAddress, repositoryOfContent } from './refs';

/** SHA-256 of the file as the `Repr-Digest` value of RFC 9530, or undefined above the limit. */
export async function reprDigestOf(file: Blob): Promise<string | undefined> {
  if (file.size > HASH_LIMIT_BYTES || !crypto.subtle) return undefined;
  try {
    const digest = new Uint8Array(await crypto.subtle.digest('SHA-256', await bytesOf(file)));
    let binary = '';
    for (const byte of digest) binary += String.fromCharCode(byte);
    return `sha-256=:${btoa(binary)}:`;
  } catch {
    // A file that cannot be read for hashing is still sent; the server computes its own digest.
    return undefined;
  }
}

function bytesOf(blob: Blob): Promise<ArrayBuffer> {
  if (typeof blob.arrayBuffer === 'function') return blob.arrayBuffer();
  return new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.onload = () => resolve(reader.result as ArrayBuffer);
    reader.onerror = () => reject(reader.error);
    reader.readAsArrayBuffer(blob);
  });
}

/** The hex SHA-256 behind a `Repr-Digest` value, for the comparison after a retried upload. */
export function hexOfReprDigest(value: string): string | undefined {
  const m = /^sha-256=:([A-Za-z0-9+/=]+):/.exec(value);
  if (!m?.[1]) return undefined;
  return [...atob(m[1])].map((c) => c.charCodeAt(0).toString(16).padStart(2, '0')).join('');
}

function itemsUrl(repository: string, suffix: 'items' | 'children' | 'stats'): string {
  return `${repositoryUrl(repository)}/${suffix}`;
}

/**
 * Files and folders of a repository: the browser (R-UI-010, R-UI-011) and the deploy dialog (R-UI-012). The reads the views take are
 * {@link FileReads} (R-UI-223); this class adds the main installation's writes and its statistics.
 */
@Injectable({ providedIn: 'root', useClass: forwardRef(() => HttpFilesApi) })
export abstract class FilesApi extends FileReads {
  abstract stats(repository: string, path: string): Promise<ItemStats>;
  /**
   * Counts the items below a folder, stopping once the count exceeds `cap`; the second member is
   * true when the walk stopped early (the real number is larger).
   */
  abstract countBelow(
    repository: string,
    path: string,
    cap: number,
  ): Promise<{ count: number; capped: boolean }>;
  abstract deleteItem(
    repository: string,
    path: string,
    idempotencyKey: string,
  ): Promise<DeleteOutcome>;
  abstract upload(request: UploadRequest): Promise<Item>;
  /** `PUT …/folder/` with an empty body: an explicit folder and its missing parents. */
  abstract createFolder(repository: string, path: string): Promise<Item>;
}

@Injectable()
export class HttpFilesApi extends FilesApi {
  private readonly client = inject(ApiClient);
  private readonly transfer = inject(UploadTransfer);

  override async item(repository: string, path: string): Promise<Item> {
    return (await this.client.getJson<Item>(itemsUrl(repository, 'items'), { params: { path } }))
      .body;
  }

  override children(repository: string, path: string, limit: number): PageFetcher<Child> {
    const fetcher = this.client.page<Child>(itemsUrl(repository, 'children'), { path });
    return (cursor) => fetcher(cursor, limit);
  }

  async stats(repository: string, path: string): Promise<ItemStats> {
    return (
      await this.client.getJson<ItemStats>(itemsUrl(repository, 'stats'), { params: { path } })
    ).body;
  }

  async countBelow(
    repository: string,
    path: string,
    cap: number,
  ): Promise<{ count: number; capped: boolean }> {
    const fetcher = this.client.page<Child>(itemsUrl(repository, 'children'), {
      path,
      recursive: true,
    });
    let count = 0;
    let cursor: string | null = null;
    do {
      const page: { items: readonly Child[]; nextCursor: string | null } = await fetcher(
        cursor,
        cap + 1,
      );
      count += page.items.length;
      cursor = page.nextCursor;
      if (count > cap) return { count, capped: true };
    } while (cursor !== null);
    return { count, capped: false };
  }

  async deleteItem(
    repository: string,
    path: string,
    idempotencyKey: string,
  ): Promise<DeleteOutcome> {
    const r = await this.client.delete<{ job: JobReference } | null>(
      itemsUrl(repository, 'items'),
      {
        params: { path },
        idempotencyKey,
      },
    );
    return r.status === 202 && r.body ? { done: false, job: r.body.job } : { done: true };
  }

  upload(request: UploadRequest): Promise<Item> {
    // A file the browser knows no type of is sent without one, so the server gives it the text type its name says, if any
    // (R-GENERIC-017), as for a client's upload; otherwise it is served as application/octet-stream.
    const headers: Record<string, string> = request.file.type ? { 'Content-Type': request.file.type } : {};
    if (request.reprDigest) headers['Repr-Digest'] = request.reprDigest;
    if (!request.overwrite) headers['If-None-Match'] = '*';
    return this.put(request, contentPath(request.repository, request.path), request.file, headers);
  }

  createFolder(repository: string, path: string): Promise<Item> {
    return this.put({}, `${contentPath(repository, path)}/`, new Blob([]), {});
  }

  /** The transfer with its progress, through the XHR-based client the collection's upload shares (R-UI-012). */
  private put(options: TransferOptions, url: string, body: Blob, headers: Record<string, string>): Promise<Item> {
    return this.transfer.send<Item>('PUT', url, body, headers, options);
  }
}
