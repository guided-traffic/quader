import { Component, computed, effect, inject, input, untracked } from '@angular/core';
import { RouterLink } from '@angular/router';
import type { DocumentationHit } from '../../app/api/search-api';
import { packageCommands } from '../../app/features/packages/package-links';
import { PagedResource } from '../../app/shared/paged-resource';
import { DataTable, TableColumn } from '../../app/shared/ui/data-table';
import { DeprecationMark } from '../../app/shared/ui/deprecation-mark';
import { PackageName } from '../../app/views/packages/package-name';
import { BastionData } from '../data/http';
import { BastionStateStore } from '../data/state';

/** Rows per page, with **Load more** (R-API-010). */
const PAGE = 25;

/** A text the documentation's search takes: at least two characters (R-ANS-058). */
const MIN_TEXT = 2;

/**
 * The documentation a project's search finds at the Bastion (R-UI-232, bastion.md R-BAS-125): the modules, plugins and roles of the
 * newest version of every package of the project whose documentation the Bastion shows, below the packages the same text found, in the
 * columns of the main installation's tab **Documentation** (R-UI-098). An entry opens its page, its package's address with `?doc=`. The
 * section stands only while the switch docs is on and the text found an entry: a project without documentation shows no empty
 * section under every search (Q-UI-1465). The text is the list's own search field; it has no field of its own.
 */
@Component({
  selector: 'quader-bastion-documentation-hits',
  imports: [DataTable, DeprecationMark, PackageName, RouterLink],
  template: `
    @if (shown()) {
      <section class="documentation-hits" aria-labelledby="documentation-hits-heading" data-testid="documentation-hits">
        <h2 id="documentation-hits-heading" i18n="@@search.group.documentation">Documentation</h2>
        <quader-data-table
          [caption]="caption"
          [columns]="columns"
          [loaded]="hits.loaded()"
          [loading]="hits.loading()"
          loadingText="Searching."
          i18n-loadingText="@@search.results.loading"
          [error]="hits.error()"
          [empty]="hits.items().length === 0"
          [hasMore]="hits.hasMore()"
          (more)="hits.more()"
          (retry)="hits.reload()"
        >
          @for (d of hits.items(); track d.repository + '/' + d.package + '/' + d.type + '/' + d.name) {
            <tr>
              <th scope="row" class="wrap-name">
                <a [routerLink]="link(d)" [queryParams]="{ doc: d.type + '/' + d.name }" translate="no">{{ d.name }}</a>
                <!-- The entry's type, then its short description: one muted line. -->
                <span class="secondary" translate="no"
                  >{{ d.type }}@if (d.shortDescription) {&ngsp;·&ngsp;{{ d.shortDescription }}}</span
                >
              </th>
              <td class="wrap-name">
                <span class="name-line">
                  <quader-package-name [format]="d.format" [name]="d.package" [link]="link(d)" [namespaceLinks]="false" />
                  <quader-deprecation-mark [deprecation]="d.deprecation" />
                </span>
                <span class="secondary" translate="no">{{ d.version }}</span>
              </td>
              <td translate="no">{{ d.repository }}</td>
            </tr>
          }
        </quader-data-table>
      </section>
    }
  `,
  styles: `
    .documentation-hits {
      margin-block-start: var(--quader-space-8);
    }

    h2 {
      margin: 0 0 var(--quader-space-3);
      font: var(--quader-type-title-medium);
    }
  `,
})
export class DocumentationHits {
  private readonly data = inject(BastionData);
  private readonly state = inject(BastionStateStore);

  /** The project whose packages the page lists, and the list's search text. */
  readonly project = input.required<string>();
  readonly text = input<string | undefined>();

  protected readonly hits = new PagedResource<DocumentationHit>(PAGE);
  private readonly term = computed(() => this.text()?.trim() ?? '');
  private readonly docs = computed(() => this.state.state()?.docs === true);
  /** The section stands once the text found an entry, or its read failed and says so (R-UI-216). */
  protected readonly shown = computed(
    () => this.docs() && this.term().length >= MIN_TEXT && (this.hits.items().length > 0 || this.hits.error() !== null),
  );

  protected readonly caption = $localize`:@@search.group.documentation:Documentation`;
  /** What a reader chooses an entry by (R-ANS-058): the entry, the package it documents and the repository it is read from. */
  protected readonly columns: readonly TableColumn[] = [
    { id: 'entry', label: $localize`:@@search.col.entry:Entry`, width: '14rem', prefer: true },
    { id: 'collection', label: $localize`:@@search.col.collection:Collection`, width: '12rem' },
    { id: 'repository', label: $localize`:@@repo.col.repository:Repository`, width: '8rem' },
  ];

  constructor() {
    effect(() => {
      const q = this.term();
      const project = this.project();
      if (!this.docs() || q.length < MIN_TEXT) return;
      untracked(() => void this.hits.start(this.data.page<DocumentationHit>('search/documentation', { q, project })));
    });
  }

  /** The address of the package an entry documents: its newest version's page, which `?doc=` opens on the entry (R-ANS-049). */
  protected link(d: DocumentationHit): string[] {
    return packageCommands(d.project, d.format, d.package);
  }
}
