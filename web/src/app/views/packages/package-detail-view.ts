import { DecimalPipe } from '@angular/common';
import { NamespaceOf } from './namespace-of';
import { LIVE_IDLE_MS, live, refreshResource } from '../../core/live-refresh';
import { Component, computed, effect, inject, input, linkedSignal, output, resource, untracked } from '@angular/core';
import { Router, RouterLink } from '@angular/router';
import { PackageSummary, VersionFilters } from '../../api/packages-api';
import { PackageReads } from '../../api/package-reads';
import { collectAll } from '../../api/pagination';
import { VulnerabilityGrade, VulnerabilityPackageReport } from '../../api/vulnerabilities-api';
import { VulnerabilityReads } from '../../api/vulnerability-reads';
import { ErrorNotice } from '../../shared/error-notice';
import { FormatIconView } from '../../shared/format-icon';
import { formatName } from '../../core/format-names';
import { failureOf, valueOf } from '../../shared/paged-resource';
import { noneOnRefusal } from '../../shared/none-on-refusal';
import { DataTable, TableColumn, TableSort } from '../../shared/ui/data-table';
import { Disclosure } from '../../shared/ui/disclosure';
import { PageHeader } from '../../shared/ui/page-header';
import { TimeAgo } from '../../shared/ui/time-ago';
import { versionCommands } from '../../features/packages/package-links';
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
import { GradeLabel } from '../../shared/vulnerability/grade';
import { VersionSize } from './version-size';
import { DistTags } from './dist-tags';
import { SignatureBadge } from '../signatures/signature-badge';
import { CLAMAV_FORMAT, clamavState, stateRank, stateText } from './clamav-definitions';
import { arrivedLabel } from '../../features/repositories/content/content-shape';
import { PackageTags } from './package-tags';
import { ImageTags, listsByContent } from './image-tags';
import { DeprecationMark } from '../../shared/ui/deprecation-mark';
import { Loading } from '../../shared/ui/loading';
import { NoValue } from '../../shared/ui/no-value';
import { MalwareMark, malwareColumn, malwareShown } from '../../shared/malware/malware-mark';
import { MalwareSignaturesStore } from '../../core/malware-signatures';

/**
 * A package's page is its versions (R-UI-007, R-UI-110, the owner's walk of 2026-11-18): the name, the format and the
 * description in the header, then every version the reader may read as one table, newest first, with its size (or that it is not
 * known, R-API-080), its vulnerabilities (the grade, the total of the open findings and how many have a fix), when it was published
 * and when a client last downloaded it (R-API-079). A
 * field above the table narrows it to the versions that hold the typed text; the columns sort; 50 rows show at first. A version's
 * row links to the version's own page (R-UI-111). The README of the latest version follows the table, the facts of the package
 * are its Details. Search and sort are the `q` and `sort` parameters of the address. An image's rows are its images, each with all
 * its tags, found by any of them, while the header counts its tags (the owner's word of 2026-11-25, Q-UI-1130).
 *
 * A view (R-UI-223): it reads through `PackageReads` and `VulnerabilityReads` and acts nowhere; it tells the page that composes it the
 * package it opened (`packageOpened`).
 */
/** Two answers name the same version. */
function sameVersion(a: { id: string; version: string } | undefined, b: { id: string; version: string } | undefined): boolean {
  return a === b || (!!a && !!b && a.id === b.id && a.version === b.version);
}

@Component({
  selector: 'quader-package-detail-view',
  imports: [Loading, DeprecationMark,
    MalwareMark,
    NoValue,
    NamespaceOf,
    SignatureBadge,
    DecimalPipe,
    RouterLink,
    TimeAgo,
    PageHeader,
    Disclosure,
    DataTable,
    ErrorNotice,
    FormatIconView,
    PackageReadmeView,
    GradeLabel,
    VersionSize,
    ImageTags,
    DistTags,
    PackageTags,
  ],
  templateUrl: './package-detail-view.html',
  styleUrl: './package-detail-view.scss',
})
export class PackageDetailView {
  private readonly api = inject(PackageReads);
  private readonly vulnerabilityApi = inject(VulnerabilityReads);
  private readonly router = inject(Router);

  /** Route parameters: the project of `/projects/:project/packages`, then the matcher's, bound by component input binding. */
  readonly project = input.required<string>();
  readonly format = input.required<string>();
  /** Whether the name's namespace links to its page; false where the application has none (the Bastion's, Q-UI-1443). */
  readonly namespaceLinks = input(true);
  /**
   * Whether the application has download figures; false where it has none (the Bastion's catalogue carries no download count and no
   * time of a last download, Q-BAS-358): no column Last download, no count, no "never" (Q-UI-1449).
   */
  readonly downloads = input(true);
  /** The format's name in the header, never its key (R-UI-006): **OCI (Docker)**, not `oci`. */
  protected readonly formatName = formatName(() => this.format());
  readonly name = input.required<string>();
  /** Query parameters: the text the versions are narrowed to and their sort. */
  readonly q = input<string | undefined>();
  readonly sort = input<string | undefined>();
  /** The package the view opened, once it has answered and whenever it answers anew: the page records it (R-UI-007). */
  readonly packageOpened = output<PackageSummary>();

  protected readonly summary = resource({
    params: () => ({ format: this.format(), name: this.name() }),
    loader: ({ params }) => this.api.lookup(params.format, params.name),
  });
  protected readonly pkg = computed(() => valueOf(this.summary));
  /**
   * Every version in the format's order, the highest first, every page while a cursor is set (a page of a list by content may be
   * short or empty, R-API-010): the table sorts, searches and pages it in the browser (Q-UI-452).
   */
  protected readonly versions = resource({
    params: () => this.pkg()?.id,
    loader: ({ params }) => collectAll(this.api.versions(params, this.versionQuery())),
  });
  /** A database of the ClamAV definitions: its versions show their state, the version in use first (R-UI-194, Q-UI-904). */
  protected readonly clamav = computed(() => this.format() === CLAMAV_FORMAT);
  /** The rows are images, each with all its tags (`group=content`, Q-UI-1130): the words say tags and images. */
  protected readonly images = computed(() => listsByContent(this.format()));
  /** The query of the version list: the format's order, a ClamAV database's states, an image's list by content. */
  private versionQuery(): VersionFilters {
    return {
      sort: '-version',
      ...(this.clamav() ? { include: ['state'] } : {}),
      ...(this.images() ? { group: 'content' as const } : {}),
    };
  }
  /** The latest version's details: its distribution tags, which are the package's, and whether it has a README. */
  /** The latest version by value: reading the package again (R-UI-213) is no new latest version unless it changed. */
  private readonly latestKey = computed(
    () => {
      const p = this.pkg();
      return p ? { id: p.id, version: p.latestVersion } : undefined;
    },
    { equal: sameVersion },
  );
  protected readonly latest = resource({
    params: () => this.latestKey(),
    loader: ({ params }) => this.api.version(params.id, params.version),
  });
  private readonly readmeKey = computed(
    () => {
      const d = valueOf(this.latest);
      // The README of an Ansible collection is the first page of its documentation, on the version's page (R-UI-060).
      return d?.hasReadme && d.format !== 'ansible' ? { id: d.packageId, version: d.version } : undefined;
    },
    { equal: sameVersion },
  );
  protected readonly readme = resource({
    params: () => this.readmeKey(),
    loader: ({ params }) => this.api.readme(params.id, params.version),
  });
  /**
   * The grade of every version (R-VULN-039, R-VULN-040). The page works without it: a refusal (no scanning, a reader who may not ask)
   * leaves the column out (Q-UI-377); a failure that may pass is shown above the versions and read again (R-UI-216).
   */
  protected readonly report = resource({
    params: () => this.pkg()?.id,
    loader: ({ params }): Promise<VulnerabilityPackageReport | null> => noneOnRefusal(this.vulnerabilityApi.packageReport(params)),
  });
  private readonly grades = computed(
    () => new Map<string, VulnerabilityGrade>((valueOf(this.report)?.versions ?? []).map((v) => [v.version, v.grade])),
  );
  /**
   * The column Vulnerabilities is there unless every version is of a format that is not scanned (Debian, Ansible Galaxy, generic files,
   * Q-UI-377), which the page says once. A version that is not checked for any other cause says why in its cell (R-VULN-039, Q-UI-1121):
   * an image that waits for its scan, a repository whose scanning is off, an internal name.
   */
  private readonly malwareMarks = inject(MalwareSignaturesStore);
  /** The versions carry their malware state (R-MAL-039): the column **Malware scan** (R-UI-255). */
  protected readonly malwareShown = computed(() => malwareShown(this.malwareMarks, this.rows()));
  protected readonly graded = computed(() => [...this.grades().values()].some((g) => g.state !== 'format_not_scanned'));

  protected readonly rows = computed(() =>
    versionRows(valueOf(this.versions) ?? [], this.grades(), distTagsOf(valueOf(this.latest)?.metadata)),
  );
  protected readonly sortState = computed<TableSort>(() => parseVersionSort(this.sort()));
  /** The text as typed: the table follows at once, the address with it (no request is made). */
  protected readonly draft = linkedSignal(() => this.q() ?? '');
  protected readonly matching = computed(() => {
    const sorted = sortVersions(filterVersions(this.rows(), this.draft()), this.sortState());
    // The version in use first, then the one before it, in the table's default order (R-UI-194): the history follows.
    const byDefault = this.sort() === undefined || this.sort() === null;
    return this.clamav() && byDefault
      ? [...sorted].sort((a, b) => stateRank(clamavState(a.state)) - stateRank(clamavState(b.state)))
      : sorted;
  });
  /** The badge of a version's state; null where the version has none. */
  protected stateBadge(state: string | null): { readonly text: string; readonly cls: string } | null {
    const s = clamavState(state);
    if (!s) return null;
    return { text: stateText(s), cls: s === 'in_use' ? 'ok' : s === 'files_removed' ? 'badge muted' : 'badge' };
  }
  /** How many rows are shown; a new search or sort starts again at one page. */
  protected readonly shownCount = linkedSignal({
    source: () => [this.draft(), this.sortState()] as const,
    computation: () => VERSION_PAGE,
  });
  protected readonly shown = computed(() => this.matching().slice(0, this.shownCount()));
  protected readonly showRepositories = computed(() => repositoriesDiffer(this.rows()));

  /**
   * The failure of the package's read, kept while its notice reads it again (R-UI-216); the reads beside the versions are read again
   * with it when they failed too, so the page that comes back is the page of a fresh load: the latest version (its distribution tags,
   * its README, its documentation's link), the README, the grades.
   */
  protected readonly loadFailure = failureOf([this.summary, this.versions], { also: [this.latest, this.readme, this.report] });
  /** The README's failure, and the latest version's that decides whether there is one: shown where the README stands. */
  protected readonly readmeFailure = failureOf([this.latest, this.readme]);
  /** The grades' failure that may pass: without it the column was left out as if the package were not scanned. */
  protected readonly reportFailure = failureOf(this.report);

  protected readonly columns = computed<readonly TableColumn[]>(() => [
    {
      id: 'version',
      label: this.images() ? $localize`:@@pkg.ver.tag:Tag` : $localize`:@@pkg.ver.version:Version`,
      sortable: true,
    },
    { id: 'size', label: $localize`:@@pkg.ver.size:Size`, numeric: true, sortable: true },
    ...(this.graded()
      ? [{ id: 'vulnerabilities', label: $localize`:@@pkg.ver.vulnerabilities:Vulnerabilities`, sortable: true }]
      : []),
    ...(this.malwareShown() ? [malwareColumn('version')] : []),
    { id: 'published', label: this.arrived(), sortable: true },
    ...(this.downloads()
      ? [
          {
            id: 'fetched',
            label: $localize`:@@pkg.ver.last-download:Last download`,
            help: $localize`:@@pkg.help.last-download:When a client last downloaded this version through Quader. Never for a version nobody downloaded yet, such as one a mirror pass fetched.`,
            sortable: true,
          },
        ]
      : []),
    ...(this.showRepositories() ? [{ id: 'repositories', label: $localize`:@@pkg.ver.repositories:Repositories` }] : []),
  ]);
  /**
   * The word of the date the versions arrived (Q-UI-1314): an image's Pushed, or Fetched where only remote repositories hold its newest
   * version; every other format's Published.
   */
  protected readonly arrived = computed(() =>
    arrivedLabel(this.format(), (valueOf(this.latest)?.repositories ?? []).map((r) => r.class)),
  );
  /** A digest's first twelve hex digits, the form a list shows a hash in (the style guide); its title and its copy have the whole. */
  protected shortDigest(digest: string): string {
    return digest.slice(digest.indexOf(':') + 1, digest.indexOf(':') + 13);
  }
  protected readonly imagesText = $localize`:@@pkg.ver.images:Images`;
  protected readonly caption = computed(() => (this.images() ? this.imagesText : $localize`:@@pkg.ver.caption:Versions`));
  protected readonly filterLabel = computed(() =>
    this.images() ? $localize`:@@pkg.ver.filter-tags:Filter tags` : $localize`:@@pkg.ver.filter:Filter versions`,
  );
  protected readonly detailsHeading = $localize`:@@pkg.details:Details`;
  protected readonly detailsText = computed(() =>
    this.downloads()
      ? $localize`:@@pkg.details-hint:The repositories that hold the package, its keywords and its downloads.`
      : $localize`:@@pkg.details-hint-no-downloads:The repositories that hold the package and its keywords.`,
  );
  protected readonly emptyText = computed(() =>
    this.images()
      ? $localize`:@@pkg.ver.none-matching-tags:No image has a tag that matches “${this.draft().trim()}:text:”.`
      : $localize`:@@pkg.ver.none-matching:No version matches “${this.draft().trim()}:text:”.`,
  );

  protected readonly neverTitle = $localize`:@@pkg.ver.never-title:No client has downloaded this version through Quader yet.`;
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

  /** What the package's summary says changed: a version, the latest, a download. The list of versions is read again only then. */
  private readonly stamp = computed(() => {
    const p = this.pkg();
    return p ? `${p.versionCount} ${p.latestVersion} ${p.updatedAt} ${p.downloadCount}` : '';
  });

  constructor() {
    // The package is read again every minute while the page is seen (R-UI-213): its summary and the grades, one request each, and
    // its versions (a request per 500) only when the summary says a version was added, removed or downloaded.
    live({
      reload: async () => {
        const id = this.pkg()?.id;
        if (!id) return;
        const before = this.stamp();
        await refreshResource(this.summary, () => this.api.lookup(this.format(), this.name()));
        const reads: Promise<void>[] = [];
        if (this.report.hasValue()) {
          reads.push(refreshResource(this.report, () => noneOnRefusal(this.vulnerabilityApi.packageReport(id))));
        }
        if (this.stamp() !== before || this.clamav()) {
          reads.push(refreshResource(this.versions, () => collectAll(this.api.versions(id, this.versionQuery()))));
        }
        await Promise.all(reads);
      },
      every: () => LIVE_IDLE_MS,
    });
    // The page records the package it opened in "recently opened" (R-UI-007) and names it in the trail.
    effect(() => {
      const s = valueOf(this.summary);
      if (s) untracked(() => this.packageOpened.emit(s));
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
