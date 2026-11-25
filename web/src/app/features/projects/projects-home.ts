import { Component, computed, inject, resource, signal } from '@angular/core';
import { Router, RouterLink } from '@angular/router';
import { firstValueFrom } from 'rxjs';
import { collectAll } from '../../api/pagination';
import { Project, ProjectRole, ProjectsApi } from '../../api/projects-api';
import { DisclosureMemory } from '../../core/disclosure-memory';
import { ProjectContext } from '../../core/project-context';
import { isUpstreamProject, projectDescription, upstreamFirst } from '../../core/upstream-project';
import { SessionStore } from '../../core/session-store';
import { failureOf, valueOf } from '../../shared/paged-resource';
import { Checkbox } from '../../shared/ui/checkbox';
import { QuaderDialog } from '../../shared/ui/dialog';
import { DataTable, TableColumn } from '../../shared/ui/data-table';
import { ListHead } from '../../shared/ui/list-head';
import { NoValue } from '../../shared/ui/no-value';
import { ProjectLevel, ProjectMark } from '../../shared/ui/project-mark';
import { PageHeader } from '../../shared/ui/page-header';
import { formatSize } from '../packages/format-size';
import { ProjectDialog } from './project-dialog';
import { projectRoleLabel } from './project-roles';

/** The remembered choice of the start page's administrator columns, kept as an opened Details section is (Q-UI-967, Q-UI-1041). */
const DETAILS_ID = 'projects.details';

/**
 * The start page (Q-UI-129, R-UI-166): the list of the projects the caller may see (`GET /api/v1/projects`), under its one heading
 * **Projects**: the place to find a project and explore what it holds. The repositories opened last are the side bar's **Recently
 * opened** (R-UI-167), and the top bar's field is the one search (Q-UI-414). An anonymous visitor sees the public projects, without
 * a role. A caller with exactly one project never sees it: the start route sends them into that project. Creating a project needs
 * `projects:write` (R-UI-041). A holder of `projects:read` gets the administrator's facts of a project on demand (the owner's T-25,
 * Q-UI-1041): **Show Owners and storage** in the head adds the columns Owners and Storage (the logical size the project is charged,
 * R-STOR-049), and the choice is kept for the viewer as an opened Details section is (Q-UI-967); a plain user's list stays as it is.
 */
@Component({
  selector: 'quader-projects-home',
  imports: [RouterLink, Checkbox, DataTable, ListHead, NoValue, PageHeader, ProjectLevel, ProjectMark],
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
      <!-- The administrator's columns, on demand (Q-UI-1041). -->
      @if (mayShowDetails()) {
        <quader-checkbox headEnd [checked]="details()" (checkedChange)="setDetails($event)">
          <ng-container i18n="@@projects-home.details">Show Owners and storage</ng-container>
        </quader-checkbox>
      }
    </quader-list-head>
    <quader-data-table
      [caption]="caption()"
      [columns]="columns()"
      [loaded]="context.loaded() && !listFailure()"
      [loading]="!context.loaded()"
      loadingText="Loading the projects."
      i18n-loadingText="@@projects.loading"
      [error]="listFailure()"
      (retry)="retryList()"
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
                <!-- The product's project, no team's (R-UI-190, Q-UI-835): its badge after the name; it has no role and no members. -->
                @if (builtIn(p.key)) {
                  &ngsp;<span class="badge" i18n="@@projects.built-in">Built-in</span>
                }
                <span class="secondary" translate="no">{{ p.key }}</span>
              </span>
            </span>
          </th>
          <td>
            @if (descriptionOf(p.key, p.description); as description) {
              <!-- A project's own description is what a person wrote; the built-in project's is the UI's (R-UI-214). -->
              <span [attr.translate]="builtIn(p.key) ? null : 'no'">{{ description }}</span>
            } @else {
              <quader-no-value />
            }
          </td>
          <td class="level"><quader-project-level [key]="p.key" [level]="p.visibility" /></td>
          @if (!anonymous()) {
            <!-- No role: a caller who sees the project through a system role and is no member (Q-UI-561). -->
            <td>
              @if (p.role) {
                {{ roleLabel(p.role) }}
              } @else {
                <quader-no-value />
              }
            </td>
          }
          @if (details()) {
            <!-- The Owners by name; the built-in project has none (R-UI-190), and a project without one shows the dash (Q-UI-561). -->
            <td class="wrap" translate="no">
              @if (ownerSummary(p); as owners) {
                {{ owners }}
              } @else {
                <quader-no-value />
              }
            </td>
          }
          <td class="num">{{ p.repositories }}</td>
          <td class="num">
            @if (builtIn(p.key)) {
              <quader-no-value />
            } @else {
              {{ p.members }}
            }
          </td>
          @if (details()) {
            <!-- A figure at the column's end (Q-UI-574), the exact bytes its title. -->
            <td class="num" [attr.title]="p.logicalBytes + ' B'">{{ size(p.logicalBytes) }}</td>
          }
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

  private readonly memory = inject(DisclosureMemory);
  /** The administrator's facts are for the holder of `projects:read`, who sees every project (R-UI-041, Q-UI-1041). */
  protected readonly mayShowDetails = computed(() => this.session.has('projects:read'));
  /** The columns Owners and Storage are shown: asked for, and allowed. */
  protected readonly details = computed(() => this.mayShowDetails() && this.memory.isOpen(DETAILS_ID));
  protected setDetails(on: boolean): void {
    this.memory.set(DETAILS_ID, on);
  }
  protected readonly size = formatSize;
  /** "web-leads, alice": the Owners by name, as the API orders them; the `user:` and `group:` of a reference is not shown. */
  protected ownerSummary(p: Project): string {
    return p.owners.map((o) => o.replace(/^(user|group):/, '')).join(', ');
  }

  /** The search of the project list (Q-UI-421). */
  protected readonly q = signal('');
  protected readonly searchLabel = $localize`:@@projects.search:Filter projects`;
  protected readonly onlyMine = signal(false);
  /** The memberships alone, from the server (`membership=true`); read only while the filter is on (R-UI-054). */
  private readonly mine = resource({
    params: () => (this.onlyMine() ? true : undefined),
    loader: async () => collectAll(this.projectsApi.list({ membership: true })),
  });
  private readonly mineFailure = failureOf(this.mine);
  /**
   * The failure of the list shown, kept while it reads again (R-UI-216): the store's projects, or the memberships while the filter
   * is on. Either used to read as an empty list ("You are not a member of a project yet").
   */
  protected readonly listFailure = computed(() => (this.onlyMine() ? this.mineFailure() : this.context.failure()));
  protected retryList(): void {
    if (this.onlyMine()) this.mineFailure.retry();
    else void this.context.refresh();
  }
  /** The caller sees projects it is no member of: the filter has something to do. */
  protected readonly hasOthers = computed(() => this.context.projects().some((p) => p.role === undefined));
  /**
   * The built-in project `upstream` first whenever it is shown (the owner's Q-UI-837), then the caller's projects, then the others
   * its level admits, each group in key order. A search that does not match `upstream` takes it out like any project.
   */
  protected readonly shown = computed<readonly Project[]>(() => {
    const q = this.q().trim().toLowerCase();
    const source = this.onlyMine() ? (valueOf(this.mine) ?? []) : this.context.projects();
    const matching = source.filter((p) => !q || p.key.includes(q) || p.displayName.toLowerCase().includes(q));
    return upstreamFirst([...matching.filter((p) => p.role !== undefined), ...matching.filter((p) => p.role === undefined)], (p) => p.key);
  });
  protected readonly builtIn = isUpstreamProject;
  /** The built-in project's description is the UI's localized sentence (Q-UI-895). */
  protected readonly descriptionOf = projectDescription;
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
  /**
   * The project's name first, its key under it (the style guide's Lists). The name and the description wrap and declare their
   * least widths, and share the rest by their content; the level, the role and the counts never break and are as wide as their
   * values (Q-UI-563). Without a least width the description was one word wide; the list's form follows these widths (Q-UI-680).
   */
  protected readonly columns = computed<readonly TableColumn[]>(() =>
    this.allColumns.filter(
      (c) => !(c.id === 'role' && this.anonymous()) && !((c.id === 'owners' || c.id === 'storage') && !this.details()),
    ),
  );
  private readonly allColumns: readonly TableColumn[] = [
    { id: 'name', label: $localize`:@@projects.col.project:Project`, width: '12rem' },
    // Stacked, the description is the line under the name and needs no name of its own (R-UI-174).
    { id: 'description', label: $localize`:@@projects-home.col.description:Description`, width: '12rem', stackLabel: false },
    { id: 'visibility', label: $localize`:@@projects.col.visibility:Visibility` },
    {
      id: 'role',
      label: $localize`:@@projects.col.role:Your role`,
      help: $localize`:@@projects.help.role:Your role in the project; a dash when you see it through a system role and are no member.`,
    },
    // On demand (Q-UI-1041): the Owners wrap, a list of names.
    { id: 'owners', label: $localize`:@@projects.col.owners:Owners` },
    { id: 'repositories', label: $localize`:@@projects.col.repositories:Repositories`, numeric: true },
    { id: 'members', label: $localize`:@@projects.col.members:Members`, numeric: true },
    {
      id: 'storage',
      label: $localize`:@@projects.col.storage:Storage`,
      numeric: true,
      help: $localize`:@@projects.help.storage:The logical size the project is charged: the full size of every file in its repositories, also where another project holds the same content.`,
    },
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
