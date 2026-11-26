import { Injectable, inject } from '@angular/core';
import { ApiClient } from './client';
import { Schema } from './types';

/** `GET /api/v1/legal` (R-UI-225): the imprint and the privacy notice as one locale shows them; a page without any text is null. */
export type LegalPages = Schema<'LegalPages'>;
export type LegalPage = Schema<'LegalPage'>;
/** The two legal pages, by the segment of their address. */
export type LegalPageName = 'imprint' | 'privacy';

export abstract class LegalApi {
  /** Always open, also without a session (R-AUTH-037). */
  abstract pages(locale: 'en' | 'de'): Promise<LegalPages>;
}

@Injectable()
export class HttpLegalApi extends LegalApi {
  private readonly client = inject(ApiClient);

  // The answer carries an ETag and Cache-Control no-cache: the browser keeps it and asks again with the tag, so a read of unchanged
  // texts is a 304 on the wire and the stored answer here.
  override async pages(locale: 'en' | 'de'): Promise<LegalPages> {
    return (await this.client.get('/api/v1/legal', { params: { locale } })).body;
  }
}
