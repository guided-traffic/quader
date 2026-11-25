import { Component, computed, inject, resource } from '@angular/core';
import { FormatDescriptor, RepositoriesApi } from '../../api/repositories-api';
import { ErrorNotice } from '../../shared/error-notice';
import { failureOf, valueOf } from '../../shared/paged-resource';
import { FormatTiles } from '../../shared/ui/format-tiles';
import { PageHeader } from '../../shared/ui/page-header';

/**
 * The navigation entry **Guides** (R-UI-124, Q-UI-481): the formats that have clients, as the tiles of R-UI-127, and nothing else
 * to choose. A tile leads to the repositories of its format; the guide itself opens from a repository (R-UI-120).
 */
@Component({
  selector: 'quader-guides-home',
  imports: [ErrorNotice, FormatTiles, PageHeader],
  template: `
    <quader-page-header
      heading="Guides"
      i18n-heading="@@guides.heading"
      i18n-description="@@guides.description"
      description="How a package manager is set up for this installation, with the explanation. Choose the package type."
    />
    @if (failure(); as f) {
      <quader-error-notice [error]="f" (retry)="failure.retry()" />
    } @else if (formats.isLoading()) {
      <p role="status" i18n="@@guides.loading">Loading.</p>
    } @else if (offered().length === 0) {
      <p i18n="@@guides.no-formats">No package type has a guide yet.</p>
    } @else {
      <quader-format-tiles [formats]="offered()" [linkOf]="linkOf" [label]="tilesLabel" />
    }
  `,
})
export class GuidesHome {
  private readonly api = inject(RepositoriesApi);

  protected readonly formats = resource({ loader: () => this.api.formats() });
  /** A format with clients has a guide (kit check 8); the others are not offered. */
  protected readonly offered = computed<readonly FormatDescriptor[]>(() =>
    (valueOf(this.formats) ?? []).filter((f) => f.clients.length > 0),
  );
  /** The failure of the formats' read, kept while its notice reads it again (R-UI-216). */
  protected readonly failure = failureOf(this.formats);
  protected readonly linkOf = (f: FormatDescriptor): string => `/guides/${f.name}`;
  protected readonly tilesLabel = $localize`:@@guides.formats:Package types`;
}
