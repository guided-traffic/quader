import { Injectable, inject } from '@angular/core';
import { ApiClient } from './client';
import { Schema } from './types';

/** The installation's switch of the signed links, its details and each connection's state (R-STOR-014): the settings page. */
export type DownloadLinkSettings = Schema<'DownloadLinkSettings'>;
/** One connection and whether it can serve links on the replica that answered (R-STOR-074). */
export type DownloadLinkConnection = Schema<'DownloadLinkConnection'>;
/** Why a connection cannot serve links: `encrypted`, `not_https`, `quader_host`, `link_check_failed`. */
export type DownloadLinkReason = Schema<'DownloadLinkReason'>;
/** A route whose clients check a digest themselves (R-STOR-071). */
export type DownloadLinkRoute = Schema<'DownloadLinkRoute'>;
/** A merge patch of the settings; a connection's `null` returns it to its endpoint. */
export type DownloadLinkSettingsPatch = Schema<'DownloadLinkSettingsPatch'>;
/** A test link with its expiry and the `curl` command that fetches it (Q-UI-952). */
export type DownloadLinkTest = Schema<'DownloadLinkTest'>;
/** Whether the switch is on and the formats it reaches: every signed-in caller reads it (R-UI-201). */
export type DownloadLinkSwitch = Schema<'DownloadLinkSwitch'>;

/** The settings with the `ETag` a change takes in `If-Match`. */
export interface LoadedDownloadLinkSettings {
  readonly settings: DownloadLinkSettings;
  readonly etag: string;
}

/**
 * The operations of the delivery by signed links (tag `download-links`, R-STOR-014, R-STOR-074): the settings for `settings:read`,
 * their change and the test link for `settings:write`, the switch for every signed-in caller.
 */
export abstract class DownloadLinksApi {
  /** `GET /api/v1/download-links/settings` with its `ETag`. */
  abstract settings(): Promise<LoadedDownloadLinkSettings>;
  /** `PATCH /api/v1/download-links/settings`, a merge patch of the changed members with `If-Match`. */
  abstract updateSettings(patch: DownloadLinkSettingsPatch, etag: string): Promise<LoadedDownloadLinkSettings>;
  /** `POST /api/v1/download-links/test`: a link to a probe on the connection, signed against the address as typed. */
  abstract test(connection: string, publicAddress: string): Promise<DownloadLinkTest>;
  /** `GET /api/v1/download-links/switch`. */
  abstract switch(): Promise<DownloadLinkSwitch>;
}

const BASE = '/api/v1/download-links';

@Injectable()
export class HttpDownloadLinksApi extends DownloadLinksApi {
  private readonly client = inject(ApiClient);

  override async settings(): Promise<LoadedDownloadLinkSettings> {
    const r = await this.client.get(`${BASE}/settings`);
    return { settings: r.body, etag: r.etag ?? '' };
  }

  override async updateSettings(patch: DownloadLinkSettingsPatch, etag: string): Promise<LoadedDownloadLinkSettings> {
    const r = await this.client.patch<DownloadLinkSettings>(`${BASE}/settings`, patch, etag);
    return { settings: r.body, etag: r.etag ?? '' };
  }

  override async test(connection: string, publicAddress: string): Promise<DownloadLinkTest> {
    return (await this.client.post<DownloadLinkTest>(`${BASE}/test`, { connection, publicAddress })).body;
  }

  override async switch(): Promise<DownloadLinkSwitch> {
    return (await this.client.get(`${BASE}/switch`)).body;
  }
}
