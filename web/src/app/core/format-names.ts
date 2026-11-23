import { Injectable, Signal, computed, inject, resource } from '@angular/core';
import { FormatDescriptor, RepositoriesApi } from '../api/repositories-api';
import { valueOf } from '../shared/paged-resource';

/**
 * The formats of the installation as `GET /api/v1/formats` names them, read once per page load: wherever a screen shows a format, it
 * shows its name (**OCI (Docker)**, **NuGet**), never its key (`oci`), which stays in addresses and filters (R-UI-006). A failed read
 * is not remembered, so the next caller asks again.
 */
@Injectable({ providedIn: 'root' })
export class FormatCatalog {
  private readonly api = inject(RepositoriesApi);
  private pending: Promise<readonly FormatDescriptor[]> | null = null;

  /** The formats; one request for every caller of a page load. */
  formats(): Promise<readonly FormatDescriptor[]> {
    this.pending ??= this.api.formats().catch((e: unknown) => {
      this.pending = null;
      throw e;
    });
    return this.pending;
  }
}

/**
 * The name of the format `key()` (its `displayName`), the key itself until the formats are read or for a format the installation
 * does not know. Called in an injection context.
 */
export function formatName(key: () => string | null | undefined): Signal<string> {
  const catalog = inject(FormatCatalog);
  const formats = resource({ loader: () => catalog.formats() });
  return computed(() => {
    const k = key() ?? '';
    return (valueOf(formats) ?? []).find((f) => f.name === k)?.displayName ?? k;
  });
}
