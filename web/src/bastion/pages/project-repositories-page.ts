import { Component, computed, effect, inject, input, resource, untracked, viewChild } from '@angular/core';
import { Router, RouterLink } from '@angular/router';
import { FormatCatalog } from '../../app/core/format-names';
import { LIVE_IDLE_MS, live } from '../../app/core/live-refresh';
import { setupCommands } from '../../app/core/project-links';
import { repositoryDescription, wordedByProduct } from '../../app/core/upstream-project';
import { classIcon, classLabel, classTip, shownClass } from '../../app/features/repositories/repository-class';
import { FormatIconView } from '../../app/shared/format-icon';
import { failureOf, valueOf } from '../../app/shared/paged-resource';
import { DataTable, TableColumn } from '../../app/shared/ui/data-table';
import { ListHead } from '../../app/shared/ui/list-head';
import { Tooltip } from '../../app/shared/ui/tooltip';
import { BastionRepositories, PublicRepository } from '../data/repositories';
import { BastionStateStore } from '../data/state';
import { BastionProjectHead } from './project-head';

/** A repository of the project as its row shows it. */
interface Row {
  readonly key: string;
  readonly ref: string;
  readonly format: string;
  readonly class: string;
  readonly description: string;
}

/**
 * A project's repositories at the Bastion (R-UI-273, W-40's D4), the project's default view: the main installation's list of a
 * project's repositories in the columns a visitor can use, the same cells, classes and words (repository-list.html), composed here
 * rather than shared, since that list is a screen of acts (its filters drawer, deletions, the system section). **Repository** (its key,
 * its description under it), **Format**, **Class** (a system-only format's says System), and the row's **Set up client**, none for a
 * system repository that answers no client. The public set is whole, so **Filter repositories** filters in the browser by key and
 * description; every public repository is listed, an empty one too. While the repositories are hidden (R-UI-274) the address is not
 * found, and a page open when the switch turns on moves to the packages at the state's next read.
 */
@Component({
  selector: 'quader-bastion-project-repositories',
  imports: [BastionProjectHead, DataTable, FormatIconView, ListHead, RouterLink, Tooltip],
  template: `
    <quader-bastion-project-head [project]="project()" current="repositories" />
    <quader-list-head [searchLabel]="searchLabel" [search]="q() ?? ''" (searchChange)="filter($event)" [filters]="false" />
    <quader-data-table
      [caption]="caption"
      [columns]="columns"
      [loaded]="loaded()"
      [loading]="!loaded() && !failure()"
      loadingText="Loading the repositories."
      i18n-loadingText="@@repo.list.loading"
      [error]="failure()"
      [empty]="rows().length === 0"
      [emptyText]="text() ? emptyFilteredText : emptyText"
      (retry)="failure.retry()"
    >
      @for (row of rows(); track row.ref) {
        <tr>
          <th scope="row">
            <span class="icon-name">
              <span class="icon-name-text">
                <a [routerLink]="['/projects', project(), 'repositories', row.key]" translate="no">{{ row.key }}</a>
                @if (row.description) {
                  <span class="secondary" [attr.translate]="worded(row) ? null : 'no'">{{ row.description }}</span>
                }
              </span>
            </span>
          </th>
          <td class="format-cell">
            <span class="format-name"><quader-format-icon [format]="row.format" /><span translate="no">{{ formatLabel(row.format) }}</span></span>
          </td>
          <td class="class-cell">
            <span class="class-name" [quaderTooltip]="classTip(row.class)" [attr.tabindex]="classTip(row.class) ? 0 : null"
              ><span class="material-icons" aria-hidden="true">{{ classIcon(row.class) }}</span>{{ classText(row.class) }}</span
            >
          </td>
          <td class="actions" [class.with-setup]="setupLink(row) !== null">
            @if (setupLink(row); as setup) {
              <a class="btn btn-tertiary row-setup" [routerLink]="setup"
                ><ng-container i18n="@@repo.content.tab.setup">Set up client</ng-container
                ><span class="visually-hidden" translate="no">&ngsp;{{ row.key }}</span></a
              >
            }
          </td>
        </tr>
      }
    </quader-data-table>
  `,
})
export class ProjectRepositoriesPage {
  private readonly repositories = inject(BastionRepositories);
  private readonly catalog = inject(FormatCatalog);
  private readonly state = inject(BastionStateStore);
  private readonly router = inject(Router);

  /** The project of the address and the query's filter, bound by component input binding. */
  readonly project = input.required<string>();
  readonly q = input<string | undefined>();

  private readonly head = viewChild(BastionProjectHead);
  protected readonly searchLabel = $localize`:@@repo.list.search:Filter repositories`;
  protected readonly caption = $localize`:@@repo.list.caption:Repositories`;
  protected readonly emptyText = $localize`:@@bastion.repositories.empty:This project shows no repository here.`;
  protected readonly emptyFilteredText = $localize`:@@repo.list.empty-filtered:No repository matches.`;
  protected readonly columns: readonly TableColumn[] = [
    { id: 'key', label: $localize`:@@repo.col.repository:Repository`, width: '13rem', prefer: true },
    { id: 'format', label: $localize`:@@repo.col.format:Format` },
    {
      id: 'class',
      label: $localize`:@@repo.col.class:Class`,
      help: $localize`:@@repo.help.class:Local repositories hold what you upload, remote repositories fetch and cache from another registry, virtual repositories combine other repositories behind one address.`,
    },
    { id: 'actions', label: $localize`:@@repo.col.actions:Actions`, hideLabel: true },
  ];

  private readonly list = resource({ loader: () => this.repositories.list() });
  private readonly formats = resource({ loader: () => this.catalog.formats() });
  protected readonly failure = failureOf(this.list, { also: [this.formats] });
  protected readonly loaded = computed(() => this.list.hasValue());
  /** The filter's text, trimmed; none for an empty one. */
  protected readonly text = computed(() => this.q()?.trim().toLowerCase() ?? '');

  /** The project's public repositories in the public set's order, with their class as a row shows it and their description's words. */
  private readonly all = computed<readonly Row[]>(() => {
    const prefix = `${this.project()}/`;
    const formats = valueOf(this.formats);
    return (valueOf(this.list) ?? [])
      .filter((r) => r.name.startsWith(prefix))
      .map((r: PublicRepository) => {
        const key = r.name.slice(prefix.length);
        return {
          key,
          ref: r.name,
          format: r.format,
          class: shownClass({ class: r.class ?? 'local', format: r.format }, formats),
          description: repositoryDescription(this.project(), key, r.description ?? ''),
        };
      });
  });
  /** The rows the filter leaves: its text in the key or the description (Q-UI-1648). */
  protected readonly rows = computed(() => {
    const text = this.text();
    if (text === '') return this.all();
    return this.all().filter((r) => r.key.toLowerCase().includes(text) || r.description.toLowerCase().includes(text));
  });

  protected readonly classIcon = classIcon;
  protected readonly classTip = classTip;
  protected readonly classText = classLabel;

  constructor() {
    // The switch Hide repositories turned on while the page is open (R-UI-274): the project's packages, without a reload (R-UI-213).
    effect(() => {
      if (!this.state.repositoriesShown()) {
        untracked(() => void this.router.navigate(['/projects', this.project(), 'packages'], { replaceUrl: true }));
      }
    });
    live({
      reload: async () => {
        this.head()?.reload();
        this.list.reload();
      },
      every: () => LIVE_IDLE_MS,
    });
  }

  protected formatLabel(format: string): string {
    return (valueOf(this.formats) ?? []).find((f) => f.name === format)?.displayName ?? format;
  }

  /** A description the product words (the project Upstream's registries) is the reader's language; a team's stays as written. */
  protected worded(row: Row): boolean {
    return wordedByProduct(this.project(), row.key) || row.class === 'system';
  }

  /** Set up client of a row's repository; none for a system repository that answers no client (Q-UI-896). */
  protected setupLink(row: Row): string[] | null {
    return setupCommands(row.ref, true);
  }

  protected filter(text: string): void {
    void this.router.navigate([], { queryParams: { q: text || null }, queryParamsHandling: 'merge', replaceUrl: true });
  }
}
