import { DestroyRef, Injectable, Signal, computed, effect, inject, resource, untracked } from '@angular/core';
import { FormatDescriptor, RepositoriesApi } from '../api/repositories-api';
import { valueOf } from '../shared/paged-resource';
import { ReadAgain } from './read-again';

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
 * does not know. A read that failed is read again with the notice's back-off (R-UI-216): the key used to stand for the name until the
 * page was opened again. Called in an injection context.
 */
export function formatName(key: () => string | null | undefined): Signal<string> {
  const catalog = inject(FormatCatalog);
  const formats = resource({ loader: () => catalog.formats() });
  const again = new ReadAgain(() => formats.reload());
  effect(() => {
    const e = formats.error();
    untracked(() => (e ? again.failed(e) : formats.hasValue() && again.answered()));
  });
  inject(DestroyRef).onDestroy(() => again.answered());
  return computed(() => {
    const k = key() ?? '';
    return (valueOf(formats) ?? []).find((f) => f.name === k)?.displayName ?? k;
  });
}
