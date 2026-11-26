import { Injectable, inject } from '@angular/core';
import { ApiClient } from './client';
import { PageFetcher } from './pagination';
import { cleanQuery, seg } from './query';
import { repositoryUrl } from './repositories-api';
import { Schema } from './types';
import { TransferOptions, UploadTransfer } from './upload-transfer';

/* The management operations of the Ansible Galaxy format: the screens' publish (R-ANS-061), import tasks (R-ANS-009) and tombstones (R-ANS-012). */

export type AnsibleImportTask = Schema<'AnsibleImportTask'>;
export type AnsibleTombstone = Schema<'AnsibleTombstone'>;

/** The optional filters of the import tasks (Q-API-262): one collection, `<namespace>.<name>`, and one version of it. */
export interface ImportTaskFilter {
  readonly package?: string;
  readonly version?: string;
}

export abstract class AnsibleApi {
  /**
   * `POST …/ansible/collections`: publishes a collection archive as the client's publish does (R-ANS-061) and answers its import task,
   * `completed` or `failed`; a refusal before the archive was read rejects with the `ApiError` of the answer. The file is sent as it is:
   * the server reads its name and version from its `MANIFEST.json`.
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

  override publish(ref: string, file: Blob, options: TransferOptions = {}): Promise<AnsibleImportTask> {
    return this.transfer.send<AnsibleImportTask>('POST', `${repositoryUrl(ref)}/ansible/collections`, file, { 'Content-Type': 'application/gzip' }, options);
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
