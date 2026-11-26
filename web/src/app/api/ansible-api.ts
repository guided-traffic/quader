import { Injectable, inject } from '@angular/core';
import { ApiClient } from './client';
import { PageFetcher } from './pagination';
import { ApiError } from './problem';
import { cleanQuery, seg } from './query';
import { repositoryUrl } from './repositories-api';
import { Schema } from './types';
import { TransferOptions, UploadTransfer } from './upload-transfer';

/* The management operations of the Ansible Galaxy format: the screens' publish (R-ANS-061), import tasks (R-ANS-009) and tombstones (R-ANS-012). */

export type AnsibleImportTask = Schema<'AnsibleImportTask'>;
export type AnsibleTombstone = Schema<'AnsibleTombstone'>;
export type AnsibleUploadToken = Schema<'AnsibleUploadToken'>;

/** The optional filters of the import tasks (Q-API-262): one collection, `<namespace>.<name>`, and one version of it. */
export interface ImportTaskFilter {
  readonly package?: string;
  readonly version?: string;
}

export abstract class AnsibleApi {
  /**
   * The screens' publish of a collection archive (R-ANS-061, Q-ANS-140): `POST …/ansible/upload-tokens` issues the caller's token of
   * five minutes for this repository; the archive goes with it to the client's own publish route (`publishPath`, served by the role
   * packages with every check of `ansible-galaxy collection publish`); `GET …/ansible/imports/{id}` reads the task it answered,
   * `completed` or `failed`; the token is revoked once the publish answered. It lives in this call only: it is never shown, logged,
   * stored or put into an address. A refusal before the archive was read rejects with an `ApiError` whose code is the management API's
   * (a Galaxy error's code gets the prefix `ansible_` where the server's code has it). The file is sent as it is: the server reads its
   * name and version from its `MANIFEST.json`.
   */
  abstract publish(ref: string, file: Blob, options?: TransferOptions): Promise<AnsibleImportTask>;
  /** `GET …/ansible/imports`: the import tasks of a local repository (of the deployment target for a virtual one), newest first. */
  abstract importTasks(ref: string, filter?: ImportTaskFilter): PageFetcher<AnsibleImportTask>;
  /** `GET …/ansible/tombstones`: the deleted versions of a local repository whose numbers are not accepted again. */
  abstract tombstones(ref: string): PageFetcher<AnsibleTombstone>;
  /** `DELETE …/ansible/tombstones/{collection}/{version}`: clears a tombstone, so that the number can be published again. */
  abstract clearTombstone(ref: string, collection: string, version: string): Promise<void>;
}

@Injectable()
export class HttpAnsibleApi extends AnsibleApi {
  private readonly client = inject(ApiClient);
  private readonly transfer = inject(UploadTransfer);

  override async publish(ref: string, file: Blob, options: TransferOptions = {}): Promise<AnsibleImportTask> {
    const issued = (await this.client.post<AnsibleUploadToken>(`${repositoryUrl(ref)}/ansible/upload-tokens`)).body;
    let answer: { readonly task?: string };
    try {
      answer = await this.transfer.send<{ readonly task?: string }>(
        'POST',
        issued.publishPath,
        file,
        { 'Content-Type': 'application/gzip', Authorization: `Bearer ${issued.token}` },
        options,
      );
    } catch (e) {
      throw galaxyFailure(e);
    } finally {
      // The token served one publish: it is revoked once the publish answered, failed or was cancelled, so that five minutes is the
      // longest it lives (Q-ANS-140); a revocation that fails leaves it to expire.
      void this.client.delete(`/api/v1/tokens/${seg(issued.id)}`).catch(() => undefined);
    }
    const id = (answer.task ?? '').replace(/\/+$/, '').split('/').pop() ?? '';
    return (await this.client.getJson<AnsibleImportTask>(`${repositoryUrl(ref)}/ansible/imports/${seg(id)}`)).body;
  }

  override importTasks(ref: string, filter: ImportTaskFilter = {}): PageFetcher<AnsibleImportTask> {
    return this.client.page<AnsibleImportTask>(`${repositoryUrl(ref)}/ansible/imports`, cleanQuery(filter));
  }

  override tombstones(ref: string): PageFetcher<AnsibleTombstone> {
    return this.client.page<AnsibleTombstone>(`${repositoryUrl(ref)}/ansible/tombstones`);
  }

  override async clearTombstone(ref: string, collection: string, version: string): Promise<void> {
    await this.client.delete(`${repositoryUrl(ref)}/ansible/tombstones/${seg(collection)}/${seg(version)}`);
  }
}

/** The Galaxy codes whose management code carries the prefix `ansible_` (Q-ANS-29: the Galaxy shape writes them without it). */
const PREFIXED = new Set([
  'invalid_request',
  'sha256_mismatch',
  'invalid_name',
  'invalid_version',
  'invalid_archive',
  'path_mismatch',
  'version_exists',
  'version_tombstoned',
]);

/**
 * The refusal of the client's publish route as the screens read one (R-UI-216): the route answers in the Galaxy error shape
 * (`{"errors": [{"status", "code", "title", "detail"}]}`), which is no problem document; its first error becomes the problem of the
 * `ApiError`, with the code the management API gives it, and every other fact of the answer stays. Anything else passes unchanged.
 */
export function galaxyFailure(e: unknown): unknown {
  if (!(e instanceof ApiError) || e.problem || !e.answer.body) return e;
  let first: { readonly code?: unknown; readonly title?: unknown; readonly detail?: unknown } | undefined;
  try {
    first = (JSON.parse(e.answer.body) as { errors?: readonly (typeof first)[] }).errors?.[0];
  } catch {
    return e;
  }
  if (!first || typeof first.code !== 'string') return e;
  const code = PREFIXED.has(first.code) ? `ansible_${first.code}` : first.code;
  const problem = {
    code,
    status: e.status,
    ...(typeof first.title === 'string' ? { title: first.title } : {}),
    ...(typeof first.detail === 'string' ? { detail: first.detail } : {}),
  };
  const out = new ApiError(e.status, problem, e.requestId, e.retryAfterSeconds, { ...e.answer, body: null, bodyLength: null });
  out.afterRetry = e.afterRetry;
  return out;
}
