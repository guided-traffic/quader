import { Component, computed, inject, signal } from '@angular/core';
import { QuaderDialog } from '../../shared/ui/dialog';
import { Router, RouterLink } from '@angular/router';
import { firstValueFrom } from 'rxjs';
import { ApiError } from '../../api/problem';
import { Project, ProjectsApi } from '../../api/projects-api';
import { ProjectContext } from '../../core/project-context';
import { isUpstreamProject, upstreamFirst } from '../../core/upstream-project';
import { SessionStore } from '../../core/session-store';
import { Needs } from '../../shared/access/needs';
import { ErrorNotice } from '../../shared/error-notice';
import { PagedResource, asApiError } from '../../shared/paged-resource';
import { DataTable, TableColumn, TableSort } from '../../shared/ui/data-table';
import { ListHead } from '../../shared/ui/list-head';
import { NoValue } from '../../shared/ui/no-value';
import { ProjectLevel, ProjectMark } from '../../shared/ui/project-mark';
import { formatSize } from '../packages/format-size';
import { ProjectDialog } from './project-dialog';
import { PageHeader } from '../../shared/ui/page-header';

/**
 * Projects of the admin area (R-UI-041): the project (its mark, display name and key), visibility, Owners, members, number of
 * repositories and storage; the search by key or name above the list (Q-UI-421), without a drawer: the list has no other filter.
 * The caller's own role is no column here (owner, 2026-11-18, Q-UI-577): the admin area manages projects, and the role decides
 * nothing there; the main area's list keeps it. The columns take their widths from their content (Q-UI-563), so the project's
 * name, the one column with much text, has the room a long display name needs. A holder of `projects:read` sees every project;
 * a member without that capability sees the projects it is a member of (Q-AUTHZ-207). Creating needs `projects:write`; editing
 * through the row also the Owner of the project.
 */
@Component({
  selector: 'quader-project-list',
  imports: [PageHeader, RouterLink, DataTable, ErrorNotice, ListHead, Needs, NoValue, ProjectLevel, ProjectMark],
  template: `
    <quader-page-header heading="Projects" i18n-heading="@@projects.heading">
      <button actions class="btn btn-confirm"
        type="button"
        [disabled]="!canWrite()"
        aria-describedby="needs-projects"
        (click)="create()"
        i18n="@@projects.add"
      >
        New project
      </button>
    </quader-page-header>
    <quader-needs id="needs-projects" capability="projects:write" />
    <p i18n="@@projects.hint">
      A project holds a team's repositories. Its members have a role in it; its Owners manage the repositories, permission policies and service accounts without a system role.
    </p>

    @if (actionError(); as e) {
      <quader-error-notice [error]="e" />
    }

    <quader-list-head [searchLabel]="searchLabel" [search]="q()" (searchChange)="searchFor($event)" [filters]="false" />

    <quader-data-table
      [caption]="caption"
      [columns]="columns"
      [sort]="sort()"
      (sortChange)="sortBy($event)"
      [loaded]="list.loaded()"
      [loading]="list.loading()"
      [error]="list.error()"
      [empty]="list.items().length === 0"
      [emptyText]="q() ? noMatch : noProject"
      [hasMore]="list.hasMore()"
      (more)="list.more()"
      (retry)="list.reload()"
    >
      @for (p of rows(); track p.key) {
        <tr>
          <!-- The project's mark before its name, its key under it: the first column of every list of projects (Q-UI-424). -->
          <th scope="row">
            <span class="icon-name">
              <quader-project-mark [key]="p.key" [name]="p.displayName" size="medium" />
              <span class="icon-name-text">
                <a [routerLink]="['/projects', p.key]">{{ p.displayName || p.key }}</a>
                <!-- The product's project (R-UI-190): its badge, no Owners, no members, and nothing to edit or delete. -->
                @if (builtIn(p.key)) {
                  &ngsp;<span class="badge" i18n="@@projects.built-in">Built-in</span>
                }
                <span class="secondary" translate="no">{{ p.key }}</span>
              </span>
            </span>
          </th>
          <td class="level"><quader-project-level [key]="p.key" [level]="p.visibility" /></td>
          <td class="wrap" translate="no">
            @if (ownerSummary(p); as owners) {
              {{ owners }}
            } @else {
              <quader-no-value />
            }
          </td>
          <!-- A count is a figure, at the column's end (Q-UI-574). -->
          <td class="num">
            @if (builtIn(p.key)) {
              <quader-no-value />
            } @else {
              {{ p.members }}
            }
          </td>
          <td class="num">{{ p.repositories }}</td>
          <td class="num" [attr.title]="p.logicalBytes + ' B'">{{ size(p.logicalBytes) }}</td>
          <td class="actions">
            @if (canEdit(p)) {
              <button class="btn btn-tertiary" type="button" (click)="edit(p)">
                <ng-container i18n="@@projects.edit">Edit</ng-container>
                <span class="visually-hidden"> {{ p.key }}</span>
              </button>
            }
          </td>
        </tr>
      }
    </quader-data-table>
  `,
  styleUrl: '../../shared/access/access.scss',
})
export class ProjectList {
  private readonly api = inject(ProjectsApi);
  private readonly dialog = inject(QuaderDialog);
  private readonly router = inject(Router);
  private readonly context = inject(ProjectContext);
  protected readonly session = inject(SessionStore);

  protected readonly size = formatSize;
  protected readonly list = new PagedResource<Project>();
  protected readonly q = signal('');
  protected readonly sort = signal<TableSort>({ column: 'key', direction: 'asc' });
  protected readonly actionError = signal<ApiError | null>(null);
  protected readonly canWrite = computed(() => this.session.has('projects:write'));
  protected readonly caption = $localize`:@@projects.caption:Projects`;
  protected readonly searchLabel = $localize`:@@projects.search:Filter projects`;
  /** An empty list says there is no project yet; a search that finds none says so (Q-UI-565). */
  protected readonly noProject = $localize`:@@projects.empty:There is no project yet.`;
  protected readonly noMatch = $localize`:@@projects.empty-filtered:No project matches.`;
  protected readonly columns: readonly TableColumn[] = [
    { id: 'key', label: $localize`:@@projects.col.project:Project`, sortable: true, width: '14rem' },
    { id: 'visibility', label: $localize`:@@projects.col.visibility:Visibility` },
    { id: 'owners', label: $localize`:@@projects.col.owners:Owners` },
    { id: 'members', label: $localize`:@@projects.col.members:Members`, numeric: true },
    { id: 'repositories', label: $localize`:@@projects.col.repositories:Repositories`, numeric: true },
    {
      id: 'storage',
      label: $localize`:@@projects.col.storage:Storage`,
      numeric: true,
      help: $localize`:@@projects.help.storage:The logical size the project is charged: the full size of every file in its repositories, also where another project holds the same content.`,
    },
    { id: 'actions', label: $localize`:@@projects.col.actions:Actions`, hideLabel: true },
  ];

  constructor() {
    void this.apply();
  }

  /** "web-leads, alice": the Owners by name, as the API orders them; the `user:` and `group:` of a reference is not shown. */
  protected ownerSummary(p: Project): string {
    return p.owners.map((o) => o.replace(/^(user|group):/, '')).join(', ');
  }

  /** `projects:write`, or the Owner role of the project (Q-AUTHZ-208); the built-in project's name and level are the product's. */
  protected canEdit(p: Project): boolean {
    return !isUpstreamProject(p.key) && (this.canWrite() || this.session.isOwnerOf(p.key));
  }

  protected readonly builtIn = isUpstreamProject;
  /**
   * The loaded page in the server's order, the built-in project `upstream` first under either direction of the sort (the owner's
   * Q-UI-837): the API lists it first, and the list keeps it there whatever it is sorted by.
   */
  protected readonly rows = computed(() => upstreamFirst(this.list.items(), (p) => p.key));

  protected async apply(): Promise<void> {
    const q = this.q().trim() || undefined;
    await this.list.start(this.api.list({ q, sort: this.sort().direction === 'desc' ? '-key' : 'key' }));
  }

  protected sortBy(sort: TableSort): void {
    this.sort.set(sort);
    void this.apply();
  }

  protected searchFor(text: string): void {
    this.q.set(text);
    void this.apply();
  }

  protected async create(): Promise<void> {
    const ref = this.dialog.open<ProjectDialog, { project?: undefined }, Project>(ProjectDialog, {
      data: {},
      maxWidth: '40rem',
    });
    const created = await firstValueFrom(ref.closed);
    if (created) await this.router.navigate(['/projects', created.key, 'settings']);
  }

  protected async edit(p: Project): Promise<void> {
    this.actionError.set(null);
    try {
      const loaded = await this.api.get(p.key);
      const ref = this.dialog.open<ProjectDialog, { project: typeof loaded }, Project>(ProjectDialog, {
        data: { project: loaded },
        maxWidth: '40rem',
      });
      const saved = await firstValueFrom(ref.closed);
      if (saved) {
        this.list.replace((x) => x.key === saved.key, saved);
        void this.context.refresh();
      }
    } catch (e) {
      this.actionError.set(asApiError(e));
    }
  }
}
