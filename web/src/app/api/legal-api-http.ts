import { Injectable, inject } from '@angular/core';
import { ApiClient } from './client';
import { LegalApi, LegalPages } from './legal-api';

@Injectable()
export class HttpLegalApi extends LegalApi {
  private readonly client = inject(ApiClient);

  // The answer carries an ETag and Cache-Control no-cache: the browser keeps it and asks again with the tag, so a read of unchanged
  // texts is a 304 on the wire and the stored answer here.
  override async pages(locale: 'en' | 'de'): Promise<LegalPages> {
    return (await this.client.get('/api/v1/legal', { params: { locale } })).body;
  }
}
