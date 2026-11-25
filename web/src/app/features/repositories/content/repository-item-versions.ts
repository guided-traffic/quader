import { Component, LOCALE_ID, computed, effect, inject, input, resource, untracked } from '@angular/core';
import { formatNumber } from '@angular/common';
import { LIVE_FAST_MS, live, refreshResource } from '../../../core/live-refresh';
import { RouterLink } from '@angular/router';
import { TimeAgo } from '../../../shared/ui/time-ago';
import { PackageSummary, PackageVersionSummary, PackagesApi } from '../../../api/packages-api';
import { splitRef } from '../../../api/repositories-api';
import { VulnerabilitiesApi, VulnerabilityGrade } from '../../../api/vulnerabilities-api';
import { SessionStore } from '../../../core/session-store';
import { systemClosed } from '../../../core/system-clients';
import { CopyButton } from '../../../shared/copy-button';
import { PagedResource } from '../../../shared/paged-resource';
import { DataTable, TableColumn } from '../../../shared/ui/data-table';
import { GradeLabel } from '../../../shared/vulnerability/grade';
import { checkWaits } from '../../../shared/vulnerability/grade-words';
import { ClientCommandMenu } from '../../packages/client-command-menu';
import { VersionSize } from '../../packages/version-size';
import { versionCommands, versionsCommands } from '../../packages/package-links';
import { ImageTags } from '../../packages/image-tags';
import { ContentShape } from './content-shape';

/**
 * How many versions an opened item lists: its newest, and a link to the package's page for all of them (R-UI-215, Q-UI-1111). The
 * opened row grows the page by at most this many rows, so a list of a hundred tags never fills the screen inside a row.
 */
export const OPENED_VERSIONS = 10;

/** How many of an image's other tags its row in an opened item names before "+N more" (Q-UI-1130); the package's page names all. */
export const OPENED_OTHER_TAGS = 4;

/**
 * The versions of one item of a repository's content page, opened in its row (R-UI-052): its ten newest by the format's order
 * (R-API-029) under a head of their own and, where it holds more than they show, a link to the package's page that lists them all
 * (R-UI-215, Q-UI-1111); no scroll and no **Load more**. For an image its ten newest images (`group=content`, Q-UI-1130): the leading
 * tag with the image's other tags and its signatures' badge, the digest, the size and pushed time of the leading tag; for a package
 * its versions; the grade of each in this repository (R-UI-094), and at the end of each row the button with
 * the format's client verb (**Pull**, **Install**) that opens the version's commands, one per client (`<quader-client-command-menu>`,
 * Q-UI-1066). A command is the server's install snippet (R-API-030) for this repository, so it carries this installation's real
 * address. A system repository of the project `upstream` and a disabled repository have no command: they answer no client (Q-UI-898,
 * Q-UI-965).
 */
@Component({
  selector: 'quader-repository-item-versions',
  imports: [TimeAgo, RouterLink, CopyButton, DataTable, GradeLabel, ClientCommandMenu, VersionSize, ImageTags],
  template: `
    <quader-data-table
      [caption]="caption()"
      [columns]="columns()"
      [loaded]="versions.loaded()"
      [loading]="versions.loading()"
      loadingText="Loading the versions."
      i18n-loadingText="@@repo.versions.loading"
      [error]="versions.error()"
      [empty]="versions.items().length === 0"
      [emptyText]="shape().emptyText"
      (retry)="retryVersions()"
    >
      @for (v of versions.items(); track v.digest ?? v.version) {
        <tr>
          <th scope="row">
            @if (shape().digest) {
              <!-- An image: its leading tag, its other tags and its signatures' badge (Q-UI-1130). -->
              <quader-image-tags
                [tag]="v.version"
                [link]="versionLink(v.version)"
                [prerelease]="v.prerelease"
                [name]="pkg().name"
                [aliases]="v.aliases ?? []"
                [more]="v.aliasesMore ?? false"
                [limit]="otherTags"
                [signatures]="v.signatures ?? null"
              />
            } @else {
              <a [routerLink]="versionLink(v.version)" translate="no">{{ v.version }}</a>
              @if (v.prerelease) {
                <span class="badge" i18n="@@repo.content.prerelease">Pre-release</span>
              }
            }
          </th>
          @if (shape().digest) {
            <!-- The digest and its copy button on one line: at 1,024 px the button went under the digest (R-UI-175). -->
            <td class="digest-cell">
              @if (v.digest; as d) {
                <code class="ellipsis" translate="no" [attr.title]="d">{{ shortDigest(d) }}</code>
                <quader-copy-button [value]="d" [label]="copyDigestLabel" />
              } @else {
                –
              }
            </td>
          }
          <td class="num"><quader-version-size [bytes]="v.sizeBytes ?? null" [partial]="v.sizePartial" /></td>
          <td>
            <quader-time-ago [at]="v.publishedAt" absolute="title" />
          </td>
          @if (scanned()) {
            <td>
              @if (gradeOf(v.version); as g) {
                <quader-grade [grade]="g" [image]="pkg().format === 'oci'" [format]="pkg().format" />
              }
            </td>
          }
          <!-- A system repository and a disabled one answer every client's request 404: their versions have no command, and no row
               offers the button (Q-UI-898, Q-UI-965). -->
          @if (!closed()) {
            <td class="actions">
              <quader-client-command-menu
                [format]="pkg().format"
                [name]="pkg().name"
                [version]="v.version"
                [repository]="repository()"
                [verb]="shape().commandVerb"
              />
            </td>
          }
        </tr>
      }
    </quader-data-table>
    @if (more() > 0) {
      <p class="all-versions">
        <a [routerLink]="packageLink()">{{ allText() }}<span class="visually-hidden">: <span translate="no">{{ pkg().name }}</span></span></a>
      </p>
    }
  `,
  styles: `
    :host {
      display: block;
      padding: var(--quader-space-3) 0 var(--quader-space-3) var(--quader-space-6);
    }
    /* Under the last row, closed by a hairline as a row is: the next item's row stood without a line above it. */
    .all-versions {
      margin: 0;
      padding: var(--quader-space-3) var(--quader-space-4);
      border-block-end: 1px solid var(--quader-border);
    }
    .digest-cell {
      white-space: nowrap;
    }
    code.ellipsis {
      display: inline-block;
      max-inline-size: 24ch;
      overflow: hidden;
      text-overflow: ellipsis;
      /* On the line's top, as tall as the line, so its middle is the line's (R-UI-175). */
      vertical-align: top;
      white-space: nowrap;
    }
    .num {
      text-align: end;
      font-variant-numeric: tabular-nums;
    }
    .actions {
      text-align: end;
      white-space: nowrap;
    }
  `,
})
export class RepositoryItemVersions {
  private readonly api = inject(PackagesApi);
  private readonly vulnerabilities = inject(VulnerabilitiesApi);
  private readonly session = inject(SessionStore);

  readonly pkg = input.required<PackageSummary>();
  /** The qualified name of the repository whose versions are shown. */
  readonly repository = input.required<string>();
  readonly shape = input.required<ContentShape>();
  /** The list carries grades: the column of grades is shown (Q-UI-377). */
  readonly scanned = input(false);

  protected readonly versions = new PagedResource<PackageVersionSummary>(OPENED_VERSIONS);
  private readonly locale = inject(LOCALE_ID);

  /** The grade of every version in this repository, in one request for the item (R-VULN-039). */
  /** The item's ID alone: the list above reads its rows again (R-UI-213), and a new row object is no new item. */
  private readonly pkgId = computed(() => this.pkg().id);
  /**
   * What the list above says changed about the item: a version added or removed, the latest, the grade. When it changes, the versions
   * shown here are read again in place; the list above is the one that is live, so an open row costs no requests of its own.
   */
  private readonly stamp = computed(() => {
    const p = this.pkg();
    const g = p.vulnerabilityGrade;
    return `${p.versionCount} ${p.latestVersion} ${p.updatedAt} ${g?.grade ?? ''} ${g?.checkedAt ?? ''} ${g?.state ?? ''}`;
  });
  protected readonly report = resource({
    params: () => (this.scanned() && this.session.state() !== 'anonymous' ? { id: this.pkgId(), repository: this.repository() } : undefined),
    loader: ({ params }) => this.vulnerabilities.packageReport(params.id, [params.repository]),
  });
  private readonly gradeByVersion = computed(() => {
    const report = this.report.hasValue() ? this.report.value() : undefined;
    return new Map((report?.versions ?? []).map((v) => [v.version, v.grade] as const));
  });

  protected readonly caption = computed(() => `${this.shape().versionsLabel}: ${this.pkg().name}`);
  protected readonly copyDigestLabel = $localize`:@@repo.content.copy-digest:Copy digest`;
  /** How many of an image's other tags a row names (Q-UI-1130). */
  protected readonly otherTags = OPENED_OTHER_TAGS;
  /**
   * The item holds more than the rows show: the list goes on past them, or a row leaves some of its image's tags out. The link to all
   * of them stands under the list (R-UI-215, Q-UI-1111, Q-UI-1150); an image whose rows show every tag has none.
   */
  protected readonly more = computed(() => {
    if (!this.versions.loaded()) return 0;
    const cut = this.versions.items().some((v) => (v.aliases?.length ?? 0) > OPENED_OTHER_TAGS || v.aliasesMore === true);
    return this.versions.hasMore() || cut ? this.pkg().versionCount : 0;
  });
  /** "All 103 tags", "All 33 versions": the count the row above shows, which for an image counts its tags. */
  protected readonly allText = computed(() => this.shape().allVersionsText(formatNumber(this.more(), this.locale)));
  /** The package's page, which lists every version with filter, sort and **Load more** (R-UI-110). */
  protected packageLink(): string[] {
    return versionsCommands(splitRef(this.repository()).project, this.pkg().format, this.pkg().name);
  }
  /** The page of a version (R-UI-111). */
  protected versionLink(version: string): string[] {
    return versionCommands(splitRef(this.repository()).project, this.pkg().format, this.pkg().name, version);
  }
  protected readonly columns = computed<readonly TableColumn[]>(() => {
    const s = this.shape();
    const columns: TableColumn[] = [{ id: 'version', label: s.versionLabel }];
    if (s.digest) columns.push({ id: 'digest', label: $localize`:@@repo.content.col.digest:Digest` });
    columns.push(
      { id: 'size', label: $localize`:@@repo.content.col.size:Size`, numeric: true },
      { id: 'published', label: s.publishedLabel },
    );
    if (this.scanned()) columns.push({ id: 'vulnerabilities', label: $localize`:@@repo.content.col.vulnerabilities:Vulnerabilities` });
    if (!this.closed()) columns.push({ id: 'actions', label: $localize`:@@repo.col.actions:Actions`, hideLabel: true });
    return columns;
  });
  /** A system repository of the project `upstream` (R-UI-193): no command for any of its versions (Q-UI-898). */
  protected readonly system = systemClosed(() => this.repository());
  /** The repository is disabled and answers no client: no command either (Q-UI-965). */
  readonly disabled = input(false);
  /** No client reaches the versions: a system repository that answers no client, or a disabled one. */
  protected readonly closed = computed(() => this.system() || this.disabled());

  constructor() {
    let shownStamp: string | null = null;
    effect(() => {
      const stamp = this.stamp();
      untracked(() => {
        if (shownStamp !== null && shownStamp !== stamp) void this.readAgain();
        shownStamp = stamp;
      });
    });
    effect(() => {
      const id = this.pkgId();
      const repository = this.repository();
      const images = this.shape().digest;
      untracked(() => {
        // The newest first by the format's order (R-API-029); an image's list by content, one row per manifest with its digest
        // (Q-API-432). A short or empty page with a cursor is read on until the ten are there (R-API-010, `PagedList`).
        void this.versions.start(
          this.api.versions(id, { repository: [repository], sort: '-version', ...(images ? { group: 'content' as const } : {}) }),
        );
      });
    });
    // While a version shown here waits for its scan (queued, scanning), its grade passes on its own: the rows are read again every
    // few seconds, not only when the row above changes (R-UI-213, Q-UI-1151); otherwise the row above's changes are enough.
    live({ reload: () => this.readAgain(), every: () => (this.waiting() ? LIVE_FAST_MS : null) });
  }

  /** A version shown here waits for its scan: its grade's state passes on its own (R-VULN-039, `checkWaits`). */
  private readonly waiting = computed(() => {
    const grades = this.gradeByVersion();
    return this.versions.items().some((v) => {
      const g = grades.get(v.version);
      return g !== undefined && checkWaits(g.state);
    });
  });

  /**
   * The list's Retry (R-UI-216): the versions, and their grades when their read failed with them, so the rows that come back carry
   * their grades as a fresh load does (Q-UI-1196).
   */
  protected retryVersions(): void {
    void this.versions.reload();
    if (this.report.error()) this.report.reload();
  }

  /** The versions and their grades again, in place; a failure keeps what is shown, and the next change of the row tries again. */
  private async readAgain(): Promise<void> {
    try {
      await this.versions.refresh();
      const params = { id: this.pkgId(), repository: this.repository() };
      if (this.report.hasValue()) await refreshResource(this.report, () => this.vulnerabilities.packageReport(params.id, [params.repository]));
      // Grades whose read failed are read again with the versions, not only on the person's Retry.
      else if (this.report.error()) this.report.reload();
    } catch {
      // Keep what is shown.
    }
  }

  protected gradeOf(version: string): VulnerabilityGrade | undefined {
    return this.gradeByVersion().get(version);
  }

  /** `sha256:abc…` keeps the algorithm and a stretch a person can compare; the copy button has the whole digest. */
  protected shortDigest(digest: string): string {
    return digest.length > 19 ? `${digest.slice(0, 19)}…` : digest;
  }
}
