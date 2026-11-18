import { DecimalPipe } from '@angular/common';
import { Component, computed, effect, inject, input, linkedSignal, resource, untracked } from '@angular/core';
import { Router, RouterLink } from '@angular/router';
import { PackagesApi } from '../../api/packages-api';
import { collectAll } from '../../api/pagination';
import { VulnerabilitiesApi, VulnerabilityGrade, VulnerabilityPackageReport } from '../../api/vulnerabilities-api';
import { ErrorNotice } from '../../shared/error-notice';
import { FormatIconView } from '../../shared/format-icon';
import { asApiError, valueOf } from '../../shared/paged-resource';
import { DataTable, TableColumn, TableSort } from '../../shared/ui/data-table';
import { Disclosure } from '../../shared/ui/disclosure';
import { PageHeader } from '../../shared/ui/page-header';
import { formatSize } from './format-size';
import { TimeAgo } from '../../shared/ui/time-ago';
import { versionCommands } from './package-links';
import { PackageMemory } from './package-memory';
import { PackageReadmeView } from './package-readme';
import {
  VERSION_PAGE,
  distTagsOf,
  filterVersions,
  parseVersionSort,
  repositoriesDiffer,
  repositoryLabel,
  sortVersions,
  versionRows,
  versionSortParam,
} from './package-versions-model';
import { VersionGrade } from './version-grade';

/**
 * A package's page is its versions (R-UI-007, R-UI-110, the owner's walk of 2026-11-18): the name, the format and the
 * description in the header, then every version the reader may read as one table, newest first, with its size, its vulnerabilities
 * (the grade, the total of the open findings and how many have a fix), when it was published and when it was last fetched. A
 * field above the table narrows it to the versions that hold the typed text; the columns sort; 50 rows show at first. A version's
 * row links to the version's own page (R-UI-111). The README of the latest version follows the table, the facts of the package
 * are its Details. Search and sort are the `q` and `sort` parameters of the address.
 */
@Component({
  selector: 'quader-package-detail',
  imports: [DecimalPipe, RouterLink, TimeAgo, PageHeader, Disclosure, DataTable, ErrorNotice, FormatIconView, PackageReadmeView, VersionGrade],
  templateUrl: './package-detail.html',
  styleUrl: './package-detail.scss',
})
export class PackageDetail {
  private readonly api = inject(PackagesApi);
  private readonly vulnerabilityApi = inject(VulnerabilitiesApi);
  private readonly router = inject(Router);
  private readonly memory = inject(PackageMemory);

  /** Route parameters: the project of `/projects/:project/packages`, then the matcher's, bound by component input binding. */
  readonly project = input.required<string>();
  readonly format = input.required<string>();
  readonly name = input.required<string>();
  /** Query parameters: the text the versions are narrowed to and their sort. */
  readonly q = input<string | undefined>();
  readonly sort = input<string | undefined>();

  protected readonly summary = resource({
    params: () => ({ format: this.format(), name: this.name() }),
    loader: ({ params }) => this.api.lookup(params.format, params.name),
  });
  protected readonly pkg = computed(() => valueOf(this.summary));
  /** Every version in the format's order, the highest first: the table sorts, searches and pages it in the browser (Q-UI-452). */
  protected readonly versions = resource({
    params: () => this.pkg()?.id,
    loader: ({ params }) => collectAll(this.api.versions(params, { sort: '-version' })),
  });
  /** The latest version's details: its distribution tags, which are the package's, and whether it has a README. */
  protected readonly latest = resource({
    params: () => {
      const p = this.pkg();
      return p ? { id: p.id, version: p.latestVersion } : undefined;
    },
    loader: ({ params }) => this.api.version(params.id, params.version),
  });
  protected readonly readme = resource({
    params: () => {
      const d = valueOf(this.latest);
      // The README of an Ansible collection is the first page of its documentation, on the version's page (R-UI-060).
      return d?.hasReadme && d.format !== 'ansible' ? { id: d.packageId, version: d.version } : undefined;
    },
    loader: ({ params }) => this.api.readme(params.id, params.version),
  });
  /**
   * The grade of every version (R-VULN-039, R-VULN-040). The page works without it: an answer that fails (no scanning, a reader who
   * may not ask) leaves the column out (Q-UI-377).
   */
  protected readonly report = resource({
    params: () => this.pkg()?.id,
    loader: async ({ params }): Promise<VulnerabilityPackageReport | null> => {
      try {
        return await this.vulnerabilityApi.packageReport(params);
      } catch {
        return null;
      }
    },
  });
  private readonly grades = computed(
    () => new Map<string, VulnerabilityGrade>((valueOf(this.report)?.versions ?? []).map((v) => [v.version, v.grade])),
  );
  /**
   * The column Vulnerabilities is there when a version has a grade other than N/A (Q-UI-377): a format that is not scanned
   * (Debian, Ansible) and a package of internal names answer N/A for every version, a column that would tell no row from another.
   */
  protected readonly graded = computed(() => [...this.grades().values()].some((g) => g.grade !== 'N/A'));

  protected readonly rows = computed(() =>
    versionRows(valueOf(this.versions) ?? [], this.grades(), distTagsOf(valueOf(this.latest)?.metadata)),
  );
  protected readonly sortState = computed<TableSort>(() => parseVersionSort(this.sort()));
  /** The text as typed: the table follows at once, the address with it (no request is made). */
  protected readonly draft = linkedSignal(() => this.q() ?? '');
  protected readonly matching = computed(() => sortVersions(filterVersions(this.rows(), this.draft()), this.sortState()));
  /** How many rows are shown; a new search or sort starts again at one page. */
  protected readonly shownCount = linkedSignal({
    source: () => [this.draft(), this.sortState()] as const,
    computation: () => VERSION_PAGE,
  });
  protected readonly shown = computed(() => this.matching().slice(0, this.shownCount()));
  protected readonly showRepositories = computed(() => repositoriesDiffer(this.rows()));

  protected readonly loadFailure = computed(() => {
    const e = this.summary.error() ?? this.versions.error();
    return e ? asApiError(e) : null;
  });
  protected readonly readmeFailure = computed(() => {
    const e = this.readme.error();
    return e ? asApiError(e) : null;
  });

  protected readonly columns = computed<readonly TableColumn[]>(() => [
    { id: 'version', label: $localize`:@@pkg.ver.version:Version`, sortable: true },
    { id: 'size', label: $localize`:@@pkg.ver.size:Size`, numeric: true, sortable: true },
    ...(this.graded()
      ? [{ id: 'vulnerabilities', label: $localize`:@@pkg.ver.vulnerabilities:Vulnerabilities`, sortable: true }]
      : []),
    { id: 'published', label: $localize`:@@pkg.ver.published:Published`, sortable: true },
    { id: 'fetched', label: $localize`:@@pkg.ver.fetched:Last fetched`, sortable: true },
    ...(this.showRepositories() ? [{ id: 'repositories', label: $localize`:@@pkg.ver.repositories:Repositories` }] : []),
  ]);
  protected readonly caption = $localize`:@@pkg.ver.caption:Versions`;
  protected readonly filterLabel = $localize`:@@pkg.ver.filter:Filter versions`;
  protected readonly detailsHeading = $localize`:@@pkg.details:Details`;
  protected readonly detailsText = $localize`:@@pkg.details-hint:The repositories that hold the package, its keywords and its downloads.`;
  protected readonly emptyText = computed(() =>
    $localize`:@@pkg.ver.none-matching:No version matches “${this.draft().trim()}:text:”.`,
  );

  protected readonly size = formatSize;
  protected versionLink(version: string): string[] {
    return versionCommands(this.project(), this.format(), this.name(), version);
  }
  protected repositoryText(qualified: string): string {
    return repositoryLabel(qualified, this.project());
  }
  /** A repository's page, inside its own project. */
  protected repositoryLink(qualified: string): string[] {
    const i = qualified.indexOf('/');
    return ['/projects', qualified.slice(0, i), 'repositories', qualified.slice(i + 1)];
  }
  protected readonly ansibleDocsLink = computed(() => {
    const d = valueOf(this.latest);
    return d?.format === 'ansible' && d.metadata.docs && d.metadata.docs.state !== 'none'
      ? versionCommands(this.project(), d.format, d.name, d.version)
      : null;
  });

  constructor() {
    // Opening the page records it in "recently opened" (R-UI-007).
    effect(() => {
      const s = valueOf(this.summary);
      if (s) untracked(() => this.memory.addRecent({ format: s.format, name: s.name }));
    });
  }

  protected onFilter(value: string): void {
    this.draft.set(value);
    void this.router.navigate([], { queryParams: { q: value.trim() === '' ? null : value }, queryParamsHandling: 'merge', replaceUrl: true });
  }

  protected changeSort(next: TableSort): void {
    void this.router.navigate([], { queryParams: { sort: versionSortParam(next) }, queryParamsHandling: 'merge' });
  }

  protected more(): void {
    this.shownCount.update((n) => n + VERSION_PAGE);
  }
}
