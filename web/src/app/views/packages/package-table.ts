import { Component, input, output } from '@angular/core';
import { PackageSummary } from '../../api/packages-api';
import { FormatIconView } from '../../shared/format-icon';
import { PagedResource } from '../../shared/paged-resource';
import { SignatureBadge } from '../signatures/signature-badge';
import { DataTable, TableColumn, TableSort } from '../../shared/ui/data-table';
import { DeprecationMark } from '../../shared/ui/deprecation-mark';
import { TimeAgo } from '../../shared/ui/time-ago';
import { packageCommands } from '../../features/packages/package-links';
import { PackageName } from './package-name';
import { PackageTags } from './package-tags';

/**
 * The table of a list of packages (R-UI-006, R-UI-223): what a reader chooses a package by (Q-UI-314), a row per package of the list the
 * page reads, sorted by the column headers, "Load more" and its Retry the table's. The page owns the list, its filters and its sort.
 */
@Component({
  selector: 'quader-package-table',
  imports: [DataTable, DeprecationMark, FormatIconView, PackageName, PackageTags, SignatureBadge, TimeAgo],
  template: `
    <quader-data-table
      [caption]="tableCaption"
      [columns]="columns"
      [sort]="sort()"
      [loaded]="list().loaded()"
      [loading]="list().loading()"
      loadingText="Loading the packages."
      i18n-loadingText="@@pkg.list.loading"
      [error]="list().error()"
      [empty]="list().items().length === 0"
      [emptyText]="emptyText()"
      [hasMore]="list().hasMore()"
      (sortChange)="sortChange.emit($event)"
      (more)="list().more()"
      (retry)="retry.emit()"
    >
      @for (row of list().items(); track row.id) {
        <!-- What a reader chooses a package by (Q-UI-314): its name and description, format, latest version, when it
             changed. Counts and the first publication are on the package's page. -->
        <tr>
          <th scope="row" class="wrap-name">
            <!-- The name's namespace links to its page, the rest to the package by its key (R-UI-220, R-ANS-056); the deprecation of
                 the version the row names, its latest (R-API-085, Q-UI-1300), and a verified latest version's badge beside it; the tags
                 filter the list by one of them (R-ANS-057). -->
            <span class="name-line">
              <quader-package-name
                [format]="row.format"
                [name]="row.name"
                [key]="row.key"
                [link]="commands(row.format, row.key ?? row.name)"
                [project]="project()"
              />
              <quader-deprecation-mark [deprecation]="row.deprecation" />
              <quader-signature-badge [state]="row.signature" />
            </span>
            @if (row.description) {
              <span class="secondary" translate="no">{{ row.description }}</span>
            }
            <quader-package-tags [tags]="row.keywords" [project]="project()" [format]="row.format" />
          </th>
          <td>
            <span class="type-cell">
              <quader-format-icon [format]="row.format" />
              <span translate="no">{{ formatLabel()(row.format) }}</span>
            </span>
          </td>
          <td translate="no">{{ row.latestVersion }}</td>
          <td><quader-time-ago [at]="row.updatedAt" absolute="title" /></td>
        </tr>
      }
    </quader-data-table>
  `,
  styleUrl: './package-table.scss',
})
export class PackageTable {
  /** The packages, read by the page with its filters and its sort. */
  readonly list = input.required<PagedResource<PackageSummary>>();
  /** The project of the list: the rows link into it. */
  readonly project = input.required<string>();
  readonly sort = input<TableSort | null>(null);
  /** What the table says without a row. */
  readonly emptyText = input.required<string>();
  /** A format's name, never its key (R-UI-006). */
  readonly formatLabel = input.required<(format: string) => string>();
  readonly sortChange = output<TableSort>();
  /** The list's Retry: the page reads the failed page again, and what failed with it. */
  readonly retry = output<void>();

  protected readonly tableCaption = $localize`:@@pkg.list.caption:Packages`;
  /**
   * The columns a reader chooses by (Q-UI-314). The name wraps after its hyphens and declares its least (Q-UI-308); the format,
   * the version and the time never break and are as wide as their values (Q-UI-563). Their guessed widths summed to 46 rem, and
   * the list stood as blocks at 1,024 px with the side bar open (R-UI-174).
   */
  protected readonly columns: readonly TableColumn[] = [
    { id: 'name', label: $localize`:@@pkg.col.package:Package`, sortable: true, width: '14rem' },
    { id: 'type', label: $localize`:@@pkg.col.format:Format`, stackLabel: false },
    // The words of the repository page's package table (Q-UI-432): **Latest**, not **Latest version**.
    {
      id: 'latest',
      label: $localize`:@@pkg.col.latest:Latest`,
      help: $localize`:@@pkg.help.latest:The highest stable version you may see. A pre-release is shown only while no stable version exists.`,
    },
    {
      id: 'updatedAt',
      label: $localize`:@@pkg.col.updated:Updated`,
      sortable: true,
      help: $localize`:@@pkg.help.updated:When the latest version you may see was published.`,
    },
  ];
  protected commands = (format: string, name: string): string[] => packageCommands(this.project(), format, name);
}
