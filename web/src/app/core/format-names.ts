import { DestroyRef, Signal, computed, effect, inject, resource, untracked } from '@angular/core';
import type { FormatDescriptor } from '../api/repositories-api';
import { valueOf } from '../shared/paged-resource';
import { ReadAgain } from './read-again';

/**
 * The formats wherever a screen shows a format: it shows its name (**OCI (Docker)**, **NuGet**), never its key (`oci`), which stays in
 * addresses and filters (R-UI-006). A view reads it as one of its reads (R-UI-223): the main installation binds the read of
 * `GET /api/v1/formats` (`HttpFormatCatalog` of format-catalog-http.ts, `provideViewReads`), the Bastion's application its own table (R-UI-228).
 */
export abstract class FormatCatalog {
  /** The formats; one request for every caller of a page load. */
  abstract formats(): Promise<readonly FormatDescriptor[]>;
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
