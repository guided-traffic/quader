import { Injectable, inject } from '@angular/core';
import { FormatDescriptor, RepositoriesApi } from '../api/repositories-api';
import { FormatCatalog } from './format-names';

/**
 * The formats of the installation as `GET /api/v1/formats` names them, read once per page load, which the main installation binds to
 * {@link FormatCatalog} (`provideViewReads`). A failed read is not remembered, so the next caller asks again.
 */
@Injectable()
export class HttpFormatCatalog extends FormatCatalog {
  private readonly api = inject(RepositoriesApi);
  private pending: Promise<readonly FormatDescriptor[]> | null = null;

  override formats(): Promise<readonly FormatDescriptor[]> {
    this.pending ??= this.api.formats().catch((e: unknown) => {
      this.pending = null;
      throw e;
    });
    return this.pending;
  }
}
