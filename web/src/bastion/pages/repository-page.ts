import { Component, computed, inject, input, resource } from '@angular/core';
import { Router } from '@angular/router';
import { FormatCatalog } from '../../app/core/format-names';
import { LIVE_IDLE_MS, live } from '../../app/core/live-refresh';
import { NotFound } from '../../app/features/not-found/not-found';
import { ErrorNotice } from '../../app/shared/error-notice';
import { failureOf, valueOf } from '../../app/shared/paged-resource';
import { Loading } from '../../app/shared/ui/loading';
import { CLAMAV_FORMAT } from '../../app/views/packages/clamav-definitions';
import { ClamavDatabases } from '../../app/views/repositories/clamav-databases';
import { contentShapeOf } from '../../app/views/repositories/content-shape';
import { ItemSort, RepositoryItems } from '../../app/views/repositories/repository-items';
import { BastionRepositories } from '../data/repositories';
import { BastionStateStore } from '../data/state';
import { FilesPage } from './files-page';
import { BastionRepositoryHead } from './repository-head';

/** The sorts of a repository's list, as the main installation's page takes them from its address. */
const SORTS: readonly ItemSort[] = ['name', '-name', 'updatedAt', '-updatedAt', 'grade', '-grade'];

/**
 * A public repository's page at the Bastion (R-UI-273, W-40's D5), by its format as at the main installation: a generic repository's
 * address is the root of its files (the files page, R-UI-229), the ClamAV definitions show their databases with the version in use
 * (R-UI-194), and every other format its images or packages, the main installation's list (views/repositories/, R-UI-223): the filter
 * and the sort in the address, a row that opens its newest versions, a **Pull ▾** or **Install ▾** per item and per version, the
 * repository's own key in each command. No column Downloads (the catalogue keeps no count, Q-UI-1449), nothing that fills the list, no
 * remote's note about what clients fetched (it explains a cache's working), the sentence of a format that is not scanned. The head is the
 * main installation's for a visitor (`BastionRepositoryHead`). While the repositories are hidden (R-UI-274) only a generic repository's
 * files stay; a page open when the switch turns on says "Page not found".
 */
@Component({
  selector: 'quader-bastion-repository',
  imports: [BastionRepositoryHead, ClamavDatabases, ErrorNotice, FilesPage, Loading, NotFound, RepositoryItems],
  template: `
    @if (failure(); as f) {
      <quader-error-notice [error]="f" (retry)="failure.retry()" />
    } @else if (!answered()) {
      <quader-loading size="page" i18n="@@repo.loading.repository">Loading the repository.</quader-loading>
    } @else if (repository(); as r) {
      @if (r.format === 'generic') {
        <quader-bastion-files [project]="project()" [key]="key()" />
      } @else if (!shown()) {
        <quader-not-found />
      } @else {
        <quader-bastion-repository-head [project]="project()" [key]="key()" [repository]="r" />
        @if (r.format === clamav) {
          <quader-clamav-databases [repository]="ref()" />
        } @else {
          @if (!scanned()) {
            <p class="content-note">{{ shape().notScannedText }}</p>
          }
          <quader-repository-items
            [repository]="ref()"
            [format]="r.format"
            [shape]="shape()"
            [scanned]="scanned()"
            [q]="q() ?? ''"
            [sortParam]="order()"
            fill="none"
            [downloads]="false"
            (filterChange)="navigate({ q: $event || null })"
            (sortParamChange)="navigate({ sort: $event === 'name' ? null : $event })"
          />
        }
      }
    } @else {
      <quader-not-found />
    }
  `,
})
export class RepositoryPage {
  private readonly repositories = inject(BastionRepositories);
  private readonly catalog = inject(FormatCatalog);
  private readonly router = inject(Router);
  private readonly state = inject(BastionStateStore);

  /** Route parameters and the query's filter and sort, bound by the router's component input binding. */
  readonly project = input.required<string>();
  readonly key = input.required<string>();
  readonly q = input<string | undefined>();
  readonly sort = input<string | undefined>();

  protected readonly clamav = CLAMAV_FORMAT;
  protected readonly ref = computed(() => `${this.project()}/${this.key()}`);
  protected readonly shown = this.state.repositoriesShown;
  private readonly read = resource({
    params: () => ({ project: this.project(), key: this.key() }),
    loader: ({ params }) => this.repositories.one(params.project, params.key),
  });
  private readonly formats = resource({ loader: () => this.catalog.formats() });
  protected readonly failure = failureOf(this.read, { also: [this.formats] });
  protected readonly answered = computed(() => this.read.hasValue());
  protected readonly repository = computed(() => valueOf(this.read) ?? null);
  /** The format is matched against advisories: the list's column of findings, else the sentence that says why not (Q-UI-1244). */
  protected readonly scanned = computed(
    () => (valueOf(this.formats) ?? []).find((f) => f.name === this.repository()?.format)?.vulnerabilityScanning ?? false,
  );
  protected readonly shape = computed(() => contentShapeOf(this.repository()?.format ?? '', this.repository()?.class ?? ''));
  protected readonly order = computed<ItemSort>(() => {
    const s = this.sort();
    return (SORTS as readonly string[]).includes(s ?? '') ? (s as ItemSort) : 'name';
  });

  constructor() {
    // The repository's facts are read again with the page's other reads (R-UI-213); the list reads its rows itself.
    live({ reload: async () => this.read.reload(), every: () => LIVE_IDLE_MS });
  }

  protected navigate(change: Readonly<Record<string, string | null>>): void {
    void this.router.navigate([], { queryParams: change, queryParamsHandling: 'merge', replaceUrl: true });
  }
}
