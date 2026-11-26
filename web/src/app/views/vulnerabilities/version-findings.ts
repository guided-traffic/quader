import { DecimalPipe } from '@angular/common';
import { Component, computed, effect, inject, input, signal, untracked } from '@angular/core';
import { RouterLink } from '@angular/router';
import {
  FindingFilters,
  OPEN_STATUSES,
  VulnerabilityFinding,
  VulnerabilitySeverity,
  VulnerabilitySeverityCounts,
} from '../../api/vulnerabilities-api';
import { VulnerabilityReads } from '../../api/vulnerability-reads';
import { PagedResource } from '../../shared/paged-resource';
import { DataTable, TableColumn } from '../../shared/ui/data-table';
import { fiveCounts } from '../../shared/vulnerability/grade-words';
import { BarSeverity } from '../../shared/vulnerability/severity-bar';
import { findingCommands } from '../../features/vulnerabilities/vuln-context';
import { severityLabel } from '../../features/vulnerabilities/vuln-labels';
import { FindingStatus, SeverityBadge } from '../../features/vulnerabilities/vuln-parts';

/** The findings of a version on the package page come 25 at a time (Q-UI-379). */
export const VERSION_FINDINGS_PAGE = 25;

/** The severities a segment of the bar stands for: Low with None (Q-VULN-153). */
export function severitiesOf(s: BarSeverity): VulnerabilitySeverity[] {
  return s === 'low' ? ['low', 'none'] : [s];
}

/** The components of an image's finding in a line: "xz 5.6.0-r0 → 5.6.1-r2, liblzma5 5.6.0". */
export function componentsText(f: Pick<VulnerabilityFinding, 'components'>): string {
  return (f.components ?? []).map((c) => `${c.name} ${c.version}${c.fixedVersion ? ` → ${c.fixedVersion}` : ''}`).join(', ');
}

/**
 * The open findings of one version in one repository on the package page's Vulnerabilities tab (R-VULN-023, Q-UI-379): asked of
 * the findings API, which pages, orders by severity and narrows to what the reader may read (R-VULN-019, R-VULN-020), 25 at a time,
 * the worst first, narrowed to one severity by the card's bar. How many there are comes from the grade's counts, which the API
 * counts in full; the version report's own list stops at 1,000 per repository. The table is the shared data table, which stacks
 * on a narrow screen.
 *
 * A visitor without a session (R-UI-224) cannot ask the findings API: the page hands over the version report's own findings (`rows`),
 * the open ones only, which are exactly the ones the grade counts (Q-VULN-280). They are shown 25 at a time and narrowed by the bar in
 * the browser; each row links to its advisory's page, names the identifier's aliases and an image's components, and carries no status:
 * where a repository holds more than the report's 1,000, the line above says how many of how many are listed.
 */
@Component({
  selector: 'quader-version-findings',
  preserveWhitespaces: true,
  imports: [DecimalPipe, RouterLink, DataTable, FindingStatus, SeverityBadge],
  template: `
    <p class="vuln-findings-total">
      @if (severity(); as s) {
        <span i18n="@@pkg.vuln.findings-filtered">{{ shown() | number }} of {{ total() | number }} open findings: {{ severityName(s) }}.</span>
        <a [routerLink]="[]" [queryParams]="{ severity: null }" queryParamsHandling="merge" i18n="@@pkg.vuln.findings-all">Show all</a>
      } @else if (bounded()) {
        <span i18n="@@pkg.vuln.findings-bounded">{{ rows()!.length | number }} of {{ total() | number }} open findings shown, the worst first.</span>
      } @else {
        <span i18n="@@pkg.vuln.findings-total">{{ total() | number }} open findings, the worst first.</span>
      }
    </p>
    <quader-data-table
      [caption]="caption()"
      [columns]="columns()"
      [loaded]="given() ? true : list.loaded()"
      [loading]="given() ? false : list.loading()"
      loadingText="Loading the findings."
      i18n-loadingText="@@vuln.findings.loading"
      [error]="given() ? null : list.error()"
      [empty]="items().length === 0"
      [emptyText]="emptyText"
      [hasMore]="given() ? items().length < givenShown().length : list.hasMore()"
      (more)="more()"
      (retry)="list.reload()"
    >
      @for (f of items(); track f.id) {
        <tr>
          <td><quader-severity [severity]="f.severity" [score]="f.cvss?.score ?? null" /></td>
          <th scope="row">
            <a [routerLink]="link(f)" translate="no">{{ f.advisory }}</a>
            @if (given()) {
              @if (f.kind === 'malicious') {
                <span class="vuln-marker"><span class="material-icons" aria-hidden="true">bug_report</span><ng-container i18n="@@vuln.marker.malicious">Malicious</ng-container></span>
              }
              @if (f.knownExploited) {
                <span class="vuln-marker"><span class="material-icons" aria-hidden="true">local_fire_department</span><ng-container i18n="@@vuln.marker.exploited">Known exploited</ng-container></span>
              }
              @if (f.aliases.length > 0) {
                <span class="secondary" translate="no">{{ f.aliases.join(', ') }}</span>
              }
            }
            <span class="secondary" translate="no">{{ f.summary }}</span>
            @if (given() && componentsOf(f); as c) {
              <span class="secondary"><ng-container i18n="@@pkg.vuln.components">In</ng-container>&ngsp;<span translate="no">{{ c }}</span></span>
            }
          </th>
          @if (!given()) {
            <td><quader-finding-status [finding]="f" /></td>
          }
          <td translate="no" class="wrap">{{ f.fixedVersions.length > 0 ? f.fixedVersions.join(', ') : '–' }}</td>
        </tr>
      }
    </quader-data-table>
  `,
})
export class VersionFindings {
  private readonly api = inject(VulnerabilityReads);

  /** The repository's qualified name. */
  readonly repository = input.required<string>();
  readonly packageName = input.required<string>();
  /** The versions the findings name: the version, or an image tag's manifest digests (R-VULN-037). */
  readonly versions = input.required<readonly string[]>();
  /** The open findings by severity, the grade's (R-VULN-039): the total. */
  readonly counts = input.required<VulnerabilitySeverityCounts>();
  /** The severity the card's bar narrowed to, or null for all. */
  readonly severity = input<BarSeverity | null>(null);
  /**
   * The version report's findings of the repository, for a visitor without a session (R-UI-224): shown instead of the findings API's,
   * which needs a session. Null for a reader with one.
   */
  readonly rows = input<readonly VulnerabilityFinding[] | null>(null);

  protected readonly list = new PagedResource<VulnerabilityFinding>(VERSION_FINDINGS_PAGE);
  /** The rows were handed over: a visitor without a session. */
  protected readonly given = computed(() => this.rows() !== null);
  /** The pages of the handed-over rows shown so far. */
  private readonly pages = signal(1);
  protected readonly columns = computed<readonly TableColumn[]>(() => [
    { id: 'severity', label: $localize`:@@vuln.col.severity:Severity`, width: '7rem' },
    { id: 'identifier', label: $localize`:@@vuln.col.identifier:Identifier`, width: '14rem' },
    ...(this.given() ? [] : [{ id: 'status', label: $localize`:@@vuln.col.status:Status` }]),
    { id: 'fixed', label: $localize`:@@pkg.vuln.col-fixed:Fixed in` },
  ]);
  protected readonly emptyText = $localize`:@@pkg.vuln.findings-none:No open finding of this severity.`;
  protected readonly caption = computed(() => $localize`:@@pkg.vuln.caption-in:Open findings of this version in ${this.repository()}:repository:`);

  protected readonly total = computed(() => {
    const c = fiveCounts(this.counts());
    return c.critical + c.high + c.medium + c.low + c.unknown;
  });
  protected readonly shown = computed(() => {
    const s = this.severity();
    return s ? fiveCounts(this.counts())[s] : this.total();
  });
  /** The handed-over rows stop at the report's bound while the grade counts more (R-UI-224: "1,000 of N shown"). */
  protected readonly bounded = computed(() => this.given() && this.rows()!.length < this.total());
  /** The handed-over rows of the bar's severity, the worst first as the report orders them. */
  protected readonly givenShown = computed(() => {
    const all = this.rows() ?? [];
    const s = this.severity();
    return s ? all.filter((f) => severitiesOf(s).includes(f.severity)) : all;
  });
  protected readonly items = computed(() =>
    this.given() ? this.givenShown().slice(0, this.pages() * VERSION_FINDINGS_PAGE) : this.list.items(),
  );

  /**
   * The query of the list, equal by value: the page hands the versions over as a new array on every change detection, and a new
   * array must not restart the list (and lose what "Load more" added).
   */
  private readonly filters = computed<FindingFilters>(
    () => ({
      repository: [this.repository()],
      package: [this.packageName()],
      version: [...this.versions()],
      status: OPEN_STATUSES,
      sort: '-severity',
      ...(this.severity() ? { severity: severitiesOf(this.severity()!) } : {}),
    }),
    { equal: (a, b) => JSON.stringify(a) === JSON.stringify(b) },
  );

  constructor() {
    effect(() => {
      const filters = this.filters();
      if (this.given()) return;
      untracked(() => void this.list.start(this.api.findings(filters)));
    });
    // A new severity starts the handed-over rows at their first page again.
    effect(() => {
      this.severity();
      untracked(() => this.pages.set(1));
    });
    // The page reads its report again while it is open (R-UI-213): when the counts change, the findings shown are read again in place.
    let shown: string | null = null;
    effect(() => {
      const stamp = JSON.stringify(this.counts());
      untracked(() => {
        if (shown !== null && shown !== stamp && !this.given()) void this.list.refresh().catch(() => undefined);
        shown = stamp;
      });
    });
  }

  protected more(): void {
    if (this.given()) this.pages.update((n) => n + 1);
    else this.list.more();
  }

  protected link(f: VulnerabilityFinding): string[] {
    return findingCommands(f, !this.given());
  }

  protected componentsOf(f: VulnerabilityFinding): string {
    return componentsText(f);
  }

  protected severityName(s: BarSeverity): string {
    return severityLabel(s);
  }
}
