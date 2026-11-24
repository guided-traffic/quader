import { Component, computed, effect, inject, input, resource, untracked } from '@angular/core';
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
import { ClientCommandMenu } from '../../packages/client-command-menu';
import { VersionSize } from '../../packages/version-size';
import { versionCommands } from '../../packages/package-links';
import { ContentShape } from './content-shape';

/** How many versions an opened item lists at a time. */
const VERSIONS_PAGE = 20;

/**
 * The versions of one item of a repository's content page, opened in its row (R-UI-052): for an image its tags with digest, size and
 * pushed time, for a package its versions; the grade of each in this repository (R-UI-094), and at the end of each row the button with
 * the format's client verb (**Pull**, **Install**) that opens the version's commands, one per client (`<quader-client-command-menu>`,
 * Q-UI-1066). A command is the server's install snippet (R-API-030) for this repository, so it carries this installation's real
 * address. A system repository of the project `upstream` and a disabled repository have no command: they answer no client (Q-UI-898,
 * Q-UI-965).
 */
@Component({
  selector: 'quader-repository-item-versions',
  imports: [TimeAgo, RouterLink, CopyButton, DataTable, GradeLabel, ClientCommandMenu, VersionSize],
  template: `
    <quader-data-table
      [caption]="caption()"
      [columns]="columns()"
      [loaded]="versions.loaded()"
      [loading]="versions.loading()"
      [error]="versions.error()"
      [empty]="versions.items().length === 0"
      [emptyText]="shape().emptyText"
      [hasMore]="versions.hasMore()"
      (more)="versions.more()"
      (retry)="versions.more()"
    >
      @for (v of versions.items(); track v.version) {
        <tr>
          <th scope="row">
            <a [routerLink]="versionLink(v.version)" translate="no">{{ v.version }}</a>
            @if (v.prerelease) {
              <span class="badge" i18n="@@repo.content.prerelease">Pre-release</span>
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
                <quader-grade [grade]="g" />
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
  `,
  styles: `
    :host {
      display: block;
      padding: var(--quader-space-3) 0 var(--quader-space-3) var(--quader-space-6);
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

  protected readonly versions = new PagedResource<PackageVersionSummary>(VERSIONS_PAGE);

  /** The grade of every version in this repository, in one request for the item (R-VULN-039). */
  protected readonly report = resource({
    params: () => (this.scanned() && this.session.state() !== 'anonymous' ? { id: this.pkg().id, repository: this.repository() } : undefined),
    loader: ({ params }) => this.vulnerabilities.packageReport(params.id, [params.repository]),
  });
  private readonly gradeByVersion = computed(() => {
    const report = this.report.hasValue() ? this.report.value() : undefined;
    return new Map((report?.versions ?? []).map((v) => [v.version, v.grade] as const));
  });

  protected readonly caption = computed(() => `${this.shape().versionsLabel}: ${this.pkg().name}`);
  protected readonly copyDigestLabel = $localize`:@@repo.content.copy-digest:Copy digest`;
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
    effect(() => {
      const id = this.pkg().id;
      const repository = this.repository();
      const digest = this.shape().digest;
      untracked(() => {
        void this.versions.start(
          this.api.versions(id, { repository: [repository], sort: '-publishedAt', include: digest ? ['digest'] : [] }),
        );
      });
    });
  }

  protected gradeOf(version: string): VulnerabilityGrade | undefined {
    return this.gradeByVersion().get(version);
  }

  /** `sha256:abc…` keeps the algorithm and a stretch a person can compare; the copy button has the whole digest. */
  protected shortDigest(digest: string): string {
    return digest.length > 19 ? `${digest.slice(0, 19)}…` : digest;
  }
}
