import { Component, computed, effect, inject, resource, untracked } from '@angular/core';
import { Router, RouterLink } from '@angular/router';
import { LIVE_IDLE_MS, live } from '../../app/core/live-refresh';
import { isUpstreamProject, projectDescription, upstreamFirst } from '../../app/core/upstream-project';
import { failureOf, valueOf } from '../../app/shared/paged-resource';
import { DataTable, TableColumn } from '../../app/shared/ui/data-table';
import { NoValue } from '../../app/shared/ui/no-value';
import { PageHeader } from '../../app/shared/ui/page-header';
import { ProjectMark } from '../../app/shared/ui/project-mark';
import { BastionProjects } from '../data/projects';
import { BastionRepositories } from '../data/repositories';
import { BastionStateStore } from '../data/state';

/**
 * The start of the Bastion's application (R-UI-229, R-UI-273, W-40's D3): the main installation's table of projects in the columns a
 * visitor can use. **Project** (its mark, its display name, its key under it), **Description** (a team's as written, the built-in
 * project's in the reader's language, R-UI-272) and, while the repositories are shown, **Repositories**, the count of the project's
 * public repositories, which tells a visitor where the content is. The project Upstream first (Q-UI-837). No visibility (everything here
 * is public), no role, no members. With one project the start is that project, as the main installation starts a caller with one
 * project in it (Q-UI-129); with none it says that nothing is shown here yet (a Bastion that serves no project, W-41).
 */
@Component({
  selector: 'quader-bastion-start',
  imports: [DataTable, NoValue, PageHeader, ProjectMark, RouterLink],
  template: `
    <quader-page-header heading="Projects" i18n-heading="@@bastion.start.title" />
    <quader-data-table
      [caption]="caption"
      [columns]="columns()"
      [loaded]="answer.hasValue()"
      [loading]="!answer.hasValue() && !failure()"
      loadingText="Loading the projects."
      i18n-loadingText="@@bastion.start.loading"
      [error]="failure()"
      (retry)="failure.retry()"
      [empty]="projects().length === 0"
      [emptyText]="emptyText"
    >
      @for (p of projects(); track p.key) {
        <tr>
          <th scope="row">
            <span class="icon-name">
              <quader-project-mark [key]="p.key" [name]="p.displayName" size="medium" />
              <span class="icon-name-text">
                <a [routerLink]="['/projects', p.key]" translate="no">{{ p.displayName || p.key }}</a>
                <span class="secondary" translate="no">{{ p.key }}</span>
              </span>
            </span>
          </th>
          <td>
            @if (descriptionOf(p.key, p.description); as description) {
              <!-- A project's own description is what a person wrote; the built-in project's is the UI's (R-UI-272, Q-UI-895). -->
              <span class="description" [attr.translate]="builtIn(p.key) ? null : 'no'">{{ description }}</span>
            } @else {
              <quader-no-value />
            }
          </td>
          @if (counted()) {
            <td class="num">{{ countOf(p.key) }}</td>
          }
        </tr>
      }
    </quader-data-table>
  `,
  styles: `
    .description {
      overflow-wrap: anywhere;
    }
  `,
})
export class StartPage {
  private readonly store = inject(BastionProjects);
  private readonly repositories = inject(BastionRepositories);
  private readonly state = inject(BastionStateStore);
  private readonly router = inject(Router);

  protected readonly caption = $localize`:@@bastion.start.title:Projects`;
  protected readonly emptyText = $localize`:@@bastion.start.nothing:Nothing is shown here yet.`;
  protected readonly answer = resource({ loader: () => this.store.list() });
  protected readonly failure = failureOf(this.answer);
  /** The projects, Upstream first and the rest in the order of their keys. */
  protected readonly projects = computed(() => upstreamFirst(valueOf(this.answer) ?? [], (p) => p.key));
  /**
   * The public repositories, for the counts: read while the repositories are shown; a failed read (the switch turned on between two
   * reads of the state) leaves the column out rather than the page.
   */
  private readonly listed = resource({
    params: () => (this.state.repositoriesShown() ? true : undefined),
    loader: () => this.repositories.list().catch(() => null),
  });
  protected readonly counted = computed(() => this.state.repositoriesShown() && !!valueOf(this.listed));
  private readonly counts = computed(() => {
    const out = new Map<string, number>();
    for (const r of valueOf(this.listed) ?? []) {
      const project = r.name.slice(0, r.name.indexOf('/'));
      out.set(project, (out.get(project) ?? 0) + 1);
    }
    return out;
  });
  protected readonly columns = computed<readonly TableColumn[]>(() => [
    { id: 'name', label: $localize`:@@projects.col.project:Project`, width: '12rem' },
    // Stacked, the description is the line under the name and needs no name of its own (R-UI-174).
    { id: 'description', label: $localize`:@@projects-home.col.description:Description`, width: '12rem', stackLabel: false },
    ...(this.counted() ? [{ id: 'repositories', label: $localize`:@@projects.col.repositories:Repositories`, numeric: true }] : []),
  ]);
  protected readonly descriptionOf = projectDescription;
  protected readonly builtIn = isUpstreamProject;

  constructor() {
    effect(() => {
      const list = this.projects();
      const only = list.length === 1 ? list[0] : undefined;
      if (only) untracked(() => void this.router.navigate(['/projects', only.key], { replaceUrl: true }));
    });
    live({
      reload: async () => {
        this.answer.reload();
        this.listed.reload();
      },
      every: () => LIVE_IDLE_MS,
    });
  }

  protected countOf(project: string): number {
    return this.counts().get(project) ?? 0;
  }
}
