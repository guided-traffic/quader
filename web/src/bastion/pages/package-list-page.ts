import { Component, computed, effect, inject, input, resource, untracked } from '@angular/core';
import { Router } from '@angular/router';
import { PackageReads } from '../../app/api/package-reads';
import { PackageSort, PackageSummary } from '../../app/api/packages-api';
import { FormatCatalog } from '../../app/core/format-names';
import { LIVE_IDLE_MS, live } from '../../app/core/live-refresh';
import { PagedResource, valueOf } from '../../app/shared/paged-resource';
import { TableSort } from '../../app/shared/ui/data-table';
import { ListHead } from '../../app/shared/ui/list-head';
import { PageHeader } from '../../app/shared/ui/page-header';
import { PackageTable } from '../../app/views/packages/package-table';

const KNOWN_SORTS: readonly string[] = ['name', '-name', 'createdAt', '-createdAt', 'updatedAt', '-updatedAt'];

/**
 * A project's packages at the Bastion (R-UI-229, W-35's B.8): the search by name, description and keyword over the catalogue, the
 * sorts of the column headers, "Load more" in pages of 50; the rows are the main installation's view (views/packages/package-table.ts).
 * The search and the sort live in the address, so a link to a result is a link to the same list.
 */
@Component({
  selector: 'quader-bastion-package-list',
  imports: [ListHead, PackageTable, PageHeader],
  template: `
    <quader-page-header heading="Packages" i18n-heading="@@pkg.list.heading" />
    <quader-list-head
      [searchLabel]="searchLabel"
      [search]="q() ?? ''"
      (searchChange)="navigate({ q: $event || null })"
      [filters]="false"
    />
    <quader-package-table
      [list]="list"
      [project]="project()"
      [sort]="tableSort()"
      [emptyText]="q() ? emptyFilteredText : emptyText"
      [formatLabel]="formatLabelOf"
      (sortChange)="sortBy($event)"
      (retry)="retry()"
    />
  `,
})
export class PackageListPage {
  private readonly reads = inject(PackageReads);
  private readonly router = inject(Router);
  private readonly catalog = inject(FormatCatalog);

  /** The project of the address, then the query's search and sort, bound by component input binding. */
  readonly project = input.required<string>();
  readonly q = input<string | undefined>();
  readonly sort = input<string | undefined>();

  protected readonly searchLabel = $localize`:@@bastion.list.search:Search packages`;
  protected readonly emptyText = $localize`:@@bastion.list.empty:No package is published in this project.`;
  protected readonly emptyFilteredText = $localize`:@@bastion.list.empty-search:No package matches the search.`;
  protected readonly list = new PagedResource<PackageSummary>();
  private readonly formats = resource({ loader: () => this.catalog.formats() });

  /** The sort: the column's when one was chosen, else the best match first while a text is searched, else the name. */
  private readonly apiSort = computed<PackageSort>(() => {
    const s = this.sort();
    if (s !== undefined && KNOWN_SORTS.includes(s)) return s as PackageSort;
    return this.q()?.trim() ? 'relevance' : 'name';
  });
  protected readonly tableSort = computed<TableSort | null>(() => {
    const s = this.apiSort();
    return s === 'relevance' ? null : { column: s.replace('-', ''), direction: s.startsWith('-') ? 'desc' : 'asc' };
  });

  protected readonly formatLabelOf = (format: string): string =>
    (valueOf(this.formats) ?? []).find((f) => f.name === format)?.displayName ?? format;

  constructor() {
    effect(() => {
      const filters = { project: this.project(), q: this.q()?.trim() || undefined, sort: this.apiSort() };
      untracked(() => void this.list.start(this.reads.list(filters)));
    });
    live({ reload: () => this.list.refresh(), every: () => LIVE_IDLE_MS });
  }

  protected navigate(params: Record<string, string | null>): void {
    void this.router.navigate([], { queryParams: params, queryParamsHandling: 'merge' });
  }

  protected sortBy(s: TableSort): void {
    this.navigate({ sort: `${s.direction === 'desc' ? '-' : ''}${s.column}` });
  }

  protected retry(): void {
    void this.list.refresh();
  }
}
