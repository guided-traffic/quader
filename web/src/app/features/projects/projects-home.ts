import { Component, computed, inject, resource, signal } from '@angular/core';
import { Router, RouterLink } from '@angular/router';
import { firstValueFrom } from 'rxjs';
import { collectAll } from '../../api/pagination';
import { Project, ProjectRole, ProjectsApi } from '../../api/projects-api';
import { ProjectContext } from '../../core/project-context';
import { SessionStore } from '../../core/session-store';
import { valueOf } from '../../shared/paged-resource';
import { Checkbox } from '../../shared/ui/checkbox';
import { QuaderDialog } from '../../shared/ui/dialog';
import { VisibilityIcon, levelName } from '../../shared/ui/visibility-level';
import { DataTable, TableColumn } from '../../shared/ui/data-table';
import { ListHead } from '../../shared/ui/list-head';
import { ProjectMark } from '../../shared/ui/project-mark';
import { PageHeader } from '../../shared/ui/page-header';
import { ProjectDialog } from './project-dialog';
import { projectRoleLabel } from './project-roles';

/**
 * The start page (Q-UI-129, R-UI-166): the list of the projects the caller may see (`GET /api/v1/projects`), under its one heading
 * **Projects**: the place to find a project and explore what it holds. The repositories opened last are the side bar's **Recently
 * opened** (R-UI-167), and the top bar's field is the one search (Q-UI-414). An anonymous visitor sees the public projects, without
 * a role. A caller with exactly one project never sees it: the start route sends them into that project. Creating a project needs
 * `projects:write` (R-UI-041).
 */
@Component({
  selector: 'quader-projects-home',
  imports: [RouterLink, Checkbox, DataTable, ListHead, PageHeader, ProjectMark, VisibilityIcon],
  template: `
    <quader-page-header [heading]="heading" [description]="hint()">
      @if (canCreate()) {
        <button actions class="btn btn-confirm" type="button" (click)="create()" i18n="@@projects-home.add">New project</button>
      }
    </quader-page-header>

    <!-- The head of the list (Q-UI-420): the search by key or name, and the list's one filter beside it; no drawer for one choice. -->
    <quader-list-head class="end-beside" [searchLabel]="searchLabel" [search]="q()" (searchChange)="q.set($event)" [filters]="false">
      @if (session.authenticated() && hasOthers()) {
        <quader-checkbox headEnd [checked]="onlyMine()" (checkedChange)="onlyMine.set($event)">
          <ng-container i18n="@@projects-home.mine">Your projects</ng-container>
        </quader-checkbox>
      }
    </quader-list-head>
    <quader-data-table
      [caption]="caption()"
      [columns]="columns()"
      [loaded]="context.loaded()"
      [loading]="!context.loaded()"
      [empty]="shown().length === 0"
      [emptyText]="emptyText()"
    >
      @for (p of shown(); track p.key) {
        <tr>
          <!-- The project's mark before its name, its key under it: the first column of every list of projects (Q-UI-424). -->
          <th scope="row">
            <span class="icon-name">
              <quader-project-mark [key]="p.key" [name]="p.displayName" size="medium" />
              <span class="icon-name-text">
                <a [routerLink]="['/projects', p.key]">{{ p.displayName || p.key }}</a>
                <span class="secondary" translate="no">{{ p.key }}</span>
              </span>
            </span>
          </th>
          <td>{{ p.description }}</td>
          <td class="level"><quader-visibility-icon [level]="p.visibility" />{{ levelText(p.visibility) }}</td>
          @if (!anonymous()) {
            <td>{{ p.role ? roleLabel(p.role) : '' }}</td>
          }
          <td class="num">{{ p.repositories }}</td>
          <td class="num">{{ p.members }}</td>
        </tr>
      }
    </quader-data-table>
  `,
  styleUrl: '../../shared/access/access.scss',
})
export class ProjectsHome {
  protected readonly context = inject(ProjectContext);
  protected readonly session = inject(SessionStore);
  private readonly projectsApi = inject(ProjectsApi);
  private readonly dialog = inject(QuaderDialog);
  private readonly router = inject(Router);

  protected readonly heading = $localize`:@@projects-home.heading:Projects`;
  protected readonly anonymous = computed(() => this.session.state() === 'anonymous');
  /**
   * The sentence under the title says what the page is for (R-UI-166): finding a project and what it holds. A signed-in caller
   * without a project gets none; the empty list says how to get one.
   */
  protected readonly hint = computed(() => {
    if (this.anonymous()) return $localize`:@@projects-home.hint-anonymous:The projects with public content. Sign in to see your own.`;
    if (this.context.loaded() && this.context.projects().length === 0) return '';
    return $localize`:@@projects-home.hint:Find a project and explore the repositories and packages it holds.`;
  });
  protected readonly canCreate = computed(() => this.session.has('projects:write'));

  /** The search of the project list (Q-UI-421). */
  protected readonly q = signal('');
  protected readonly searchLabel = $localize`:@@projects.search:Filter projects`;
  protected readonly levelText = levelName;
  protected readonly onlyMine = signal(false);
  /** The memberships alone, from the server (`membership=true`); read only while the filter is on (R-UI-054). */
  private readonly mine = resource({
    params: () => (this.onlyMine() ? true : undefined),
    loader: async () => collectAll(this.projectsApi.list({ membership: true })),
  });
  /** The caller sees projects it is no member of: the filter has something to do. */
  protected readonly hasOthers = computed(() => this.context.projects().some((p) => p.role === undefined));
  /** The caller's projects first, then the others its level admits, each group in key order. */
  protected readonly shown = computed<readonly Project[]>(() => {
    const q = this.q().trim().toLowerCase();
    const source = this.onlyMine() ? (valueOf(this.mine) ?? []) : this.context.projects();
    const matching = source.filter((p) => !q || p.key.includes(q) || p.displayName.toLowerCase().includes(q));
    return [...matching.filter((p) => p.role !== undefined), ...matching.filter((p) => p.role === undefined)];
  });
  protected readonly emptyText = computed(() => {
    if (this.context.projects().length > 0) return $localize`:@@projects-home.empty-filtered:No project matches.`;
    return this.session.state() === 'anonymous'
      ? $localize`:@@projects-home.empty-anonymous:No project is public. Sign in to see your projects.`
      : $localize`:@@projects-home.empty:You are not a member of a project yet. An Owner of a project adds you with a role.`;
  });
  /** An anonymous visitor has no projects of its own: the list is the public projects, without a role. */
  protected readonly caption = computed(() =>
    this.anonymous()
      ? $localize`:@@projects-home.caption-public:Public projects`
      : $localize`:@@projects-home.caption:Your projects`,
  );
  /** The project's name first, its key under it (the style guide's Lists); the list's budget (Q-UI-308). */
  protected readonly columns = computed<readonly TableColumn[]>(() =>
    this.anonymous() ? this.allColumns.filter((c) => c.id !== 'role') : this.allColumns,
  );
  private readonly allColumns: readonly TableColumn[] = [
    { id: 'name', label: $localize`:@@projects.col.project:Project`, width: '14rem' },
    { id: 'description', label: $localize`:@@projects-home.col.description:Description` },
    { id: 'visibility', label: $localize`:@@projects.col.visibility:Visibility`, width: '8rem' },
    {
      id: 'role',
      label: $localize`:@@projects.col.role:Your role`,
      help: $localize`:@@projects.help.role:Your role in the project; empty when you see it through a system role and are no member.`,
      width: '7rem',
    },
    { id: 'repositories', label: $localize`:@@projects.col.repositories:Repositories`, numeric: true, width: '7rem' },
    { id: 'members', label: $localize`:@@projects.col.members:Members`, numeric: true, width: '5rem' },
  ];

  protected roleLabel(role: ProjectRole): string {
    return projectRoleLabel(role);
  }

  protected async create(): Promise<void> {
    const ref = this.dialog.open<ProjectDialog, { project?: undefined }, Project>(ProjectDialog, {
      data: {},
      maxWidth: '40rem',
    });
    const created = await firstValueFrom(ref.closed);
    if (created) {
      await this.context.refresh();
      await this.router.navigate(['/projects', created.key, 'settings']);
    }
  }
}
