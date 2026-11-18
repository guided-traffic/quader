import { Injectable, inject } from '@angular/core';
import { ApiClient } from './client';
import { Schema } from './types';

/**
 * `GET`, `PATCH /api/v1/settings/visibility` (R-API-056, R-API-075): the installation's default level, the levels restricted to
 * administrators and what is public now.
 */
export type VisibilitySettings = Schema<'VisibilitySettings'>;
export type VisibilitySettingsPatch = Schema<'VisibilitySettingsPatch'>;
export type PublicObjects = Schema<'PublicObjects'>;
/** `GET /api/v1/visibility-choices` (R-API-076): the levels the caller may choose, for the forms of a project and a repository. */
export type VisibilityChoices = Schema<'VisibilityChoices'>;

export interface LoadedVisibilitySettings {
  readonly settings: VisibilitySettings;
  readonly etag: string;
}

export abstract class VisibilityApi {
  /** The settings; `settings:read`, an administrator's read. */
  abstract load(): Promise<LoadedVisibilitySettings>;
  /** Merge patch with the loaded `ETag` (R-API-012); recent authentication applies; 409 `public_not_allowed`. */
  abstract patch(patch: VisibilitySettingsPatch, etag: string): Promise<LoadedVisibilitySettings>;
  /** The caller's choices; every signed-in caller reads them. */
  abstract choices(): Promise<VisibilityChoices>;
}

@Injectable()
export class HttpVisibilityApi extends VisibilityApi {
  private readonly client = inject(ApiClient);
  private readonly url = '/api/v1/settings/visibility';

  override async load(): Promise<LoadedVisibilitySettings> {
    const r = await this.client.get(this.url);
    return { settings: r.body, etag: r.etag ?? '' };
  }

  override async patch(patch: VisibilitySettingsPatch, etag: string): Promise<LoadedVisibilitySettings> {
    const r = await this.client.patch<VisibilitySettings>(this.url, patch, etag);
    return { settings: r.body, etag: r.etag ?? '' };
  }

  override async choices(): Promise<VisibilityChoices> {
    return (await this.client.get('/api/v1/visibility-choices')).body;
  }
}
