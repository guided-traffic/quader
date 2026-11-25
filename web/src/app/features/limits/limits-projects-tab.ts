import { ChangeDetectionStrategy, Component, LOCALE_ID, computed, inject, signal } from '@angular/core';
import { RouterLink } from '@angular/router';
import { firstValueFrom } from 'rxjs';
import { LimitProfile, LimitsApi, ProjectLimitsRow } from '../../api/limits-api';
import { collectAll } from '../../api/pagination';
import { ApiError } from '../../api/problem';
import { ProjectsApi } from '../../api/projects-api';
import { Notifier } from '../../core/notifier';
import { SessionStore } from '../../core/session-store';
import { upstreamFirst } from '../../core/upstream-project';
import { Confirm } from '../../shared/confirm-dialog';
import { asApiError } from '../../shared/paged-resource';
import { DataTable, TableColumn } from '../../shared/ui/data-table';
import { QuaderDialog } from '../../shared/ui/dialog';
import { ListHead } from '../../shared/ui/list-head';
import { ProjectMark } from '../../shared/ui/project-mark';
import { formatCount } from './figures';
import { LimitMaximumDialog, MaximumDialogData } from './maximum-dialog';

/** "Technical", "Technical + 2 changes", "None" (R-UI-177). */
export function maximumText(row: ProjectLimitsRow, profiles: readonly LimitProfile[]): string {
  const m = row.maximum?.value;
  if (!m) return $localize`:@@limits.projects.none:None`;
  const name = profiles.find((p) => p.key === m.profile)?.name ?? m.profile;
  const n = Object.keys(m.overrides).length;
  if (n === 0) return name;
  return n === 1
    ? $localize`:@@limits.projects.one-change:${name}:profile: + 1 change`
    : $localize`:@@limits.projects.changes:${name}:profile: + ${n}:count: changes`;
}

/**
 * The tab **Projects** of **Download limits** (R-UI-177): every project with a maximum or a rule, its maximum and its number of
 * rules (at the project and its repositories), and **Set maximum**, **Change maximum** and **Remove maximum** for a holder of
 * `limits:write`. The head's search finds a project by key or name; **Add maximum** asks for the project first.
 */
@Component({
  selector: 'quader-limits-projects-tab',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [DataTable, ListHead, ProjectMark, RouterLink],
  template: `
    <quader-list-head [searchLabel]="searchLabel" [search]="q()" (searchChange)="q.set($event)" [filters]="false">
      @if (canWrite()) {
        <button headEnd class="btn btn-confirm" type="button" (click)="add()" i18n="@@limits.projects.add">Add maximum</button>
      }
    </quader-list-head>
    <quader-data-table
      caption="Projects with download limits"
      i18n-caption="@@limits.projects.caption"
      [columns]="columns()"
      [loaded]="rows() !== null"
      [loading]="rows() === null && !failure()"
      [error]="failure()"
      (retry)="load()"
      [empty]="shown().length === 0"
      [emptyText]="emptyText()"
    >
      @for (r of shown(); track r.project) {
        <tr>
          <th scope="row">
            <span class="icon-name">
              <quader-project-mark [key]="r.project" [name]="r.displayName" size="medium" />
              <span class="icon-name-text">
                <a [routerLink]="['/projects', r.project, 'settings']">{{ r.displayName || r.project }}</a>
                <span class="secondary" translate="no">{{ r.project }}</span>
              </span>
            </span>
          </th>
          <td>
            @if (canWrite()) {
              <button class="open link" type="button" (click)="edit(r)">
                @if (r.maximum) {
                  {{ maximum(r) }}
                } @else {
                  <ng-container i18n="@@limits.projects.set">Set maximum</ng-container>
                }
                <span class="visually-hidden" translate="no"> {{ r.project }}</span>
              </button>
            } @else {
              {{ maximum(r) }}
            }
          </td>
          <td class="num">{{ count(r.rules) }}</td>
          @if (canWrite()) {
            <td class="actions">
              @if (r.maximum) {
                <button class="btn btn-tertiary" type="button" (click)="remove(r)">
                  <ng-container i18n="@@limits.projects.remove">Remove maximum</ng-container>
                  <span class="visually-hidden" translate="no"> {{ r.project }}</span>
                </button>
              }
            </td>
          }
        </tr>
      }
    </quader-data-table>
  `,
  styles: `
    /* The maximum opens its dialog: the thing the row sets is its own link (R-UI-163). */
    .open {
      padding: 0;
      border: 0;
      background: none;
      color: var(--quader-link);
      font: inherit;
      text-align: start;
      cursor: pointer;
    }
    .open:hover {
      text-decoration: underline;
    }
    .open:focus-visible {
      outline: var(--quader-focus-ring);
      outline-offset: 2px;
    }
  `,
})
export class LimitsProjectsTab {
  private readonly api = inject(LimitsApi);
  private readonly projectsApi = inject(ProjectsApi);
  private readonly session = inject(SessionStore);
  private readonly dialog = inject(QuaderDialog);
  private readonly confirmer = inject(Confirm);
  private readonly notifier = inject(Notifier);
  private readonly locale = inject(LOCALE_ID);

  protected readonly searchLabel = $localize`:@@limits.projects.search:Filter projects`;
  protected readonly canWrite = computed(() => this.session.has('limits:write'));
  protected readonly q = signal('');
  protected readonly rows = signal<readonly ProjectLimitsRow[] | null>(null);
  private readonly profiles = signal<readonly LimitProfile[]>([]);
  protected readonly failure = signal<ApiError | null>(null);
  /** The rows that match the search, the built-in project `upstream` first (Q-UI-837). */
  protected readonly shown = computed(() => {
    const q = this.q().trim().toLocaleLowerCase();
    return upstreamFirst(
      (this.rows() ?? []).filter((r) => !q || r.project.includes(q) || r.displayName.toLocaleLowerCase().includes(q)),
      (r) => r.project,
    );
  });
  protected readonly emptyText = computed(() =>
    this.q().trim()
      ? $localize`:@@limits.projects.no-match:No project matches.`
      : $localize`:@@limits.projects.empty:No project has a maximum. A project's Owner can set limits up to the instance's values.`,
  );
  protected readonly columns = computed<readonly TableColumn[]>(() => [
    { id: 'project', label: $localize`:@@limits.projects.col-project:Project`, width: '12rem', prefer: true },
    { id: 'maximum', label: $localize`:@@limits.projects.col-maximum:Maximum`, width: '10rem' },
    { id: 'rules', label: $localize`:@@limits.projects.col-rules:Rules`, numeric: true },
    ...(this.canWrite() ? [{ id: 'actions', label: $localize`:@@limits.rules.col-actions:Actions`, hideLabel: true }] : []),
  ]);

  constructor() {
    void this.load();
  }

  protected async load(): Promise<void> {
    try {
      const [rows, profiles] = await Promise.all([this.api.projectRows(), this.api.profiles().catch((e: unknown) => asApiError(e))]);
      // The failure stays until the answer, so the notice stays in place while it reads again (R-UI-216). A failed read of the
      // profiles is shown beside the projects, whose maximum then names its profile by its key, and the notice's Retry reads it again.
      this.failure.set(profiles instanceof ApiError ? profiles : null);
      this.profiles.set(profiles instanceof ApiError ? [] : profiles);
      this.rows.set(rows);
    } catch (e) {
      this.failure.set(asApiError(e));
    }
  }

  protected maximum(r: ProjectLimitsRow): string {
    return maximumText(r, this.profiles());
  }

  protected count(n: number): string {
    return formatCount(n, this.locale);
  }

  protected async add(): Promise<void> {
    const withMaximum = new Set((this.rows() ?? []).filter((r) => r.maximum).map((r) => r.project));
    const projects = upstreamFirst(
      (await collectAll(this.projectsApi.list({}))).filter((p) => !withMaximum.has(p.key)),
      (p) => p.key,
    );
    await this.open({ maximum: null, projects: projects.map((p) => ({ key: p.key, displayName: p.displayName })) });
  }

  protected async edit(r: ProjectLimitsRow): Promise<void> {
    await this.open({ project: r.project, maximum: r.maximum });
  }

  private async open(data: MaximumDialogData): Promise<void> {
    const ref = this.dialog.open<LimitMaximumDialog, MaximumDialogData, unknown>(LimitMaximumDialog, { data, maxWidth: '44rem', width: '44rem' });
    if (await firstValueFrom(ref.closed)) {
      this.notifier.status($localize`:@@limits.projects.saved:Maximum saved.`);
      await this.load();
    }
  }

  protected async remove(r: ProjectLimitsRow): Promise<void> {
    const answer = await this.confirmer.ask({
      title: $localize`:@@limits.projects.remove-title:Remove the maximum of ${r.project}:project:?`,
      lines: [
        $localize`:@@limits.projects.remove-text:The project's Owners can then set limits up to the instance's values only. Their rules above that are lowered from the next request on.`,
      ],
      confirmLabel: $localize`:@@limits.projects.remove-confirm:Remove maximum`,
    });
    if (answer === null) return;
    this.failure.set(null);
    try {
      await this.api.deleteMaximum(r.project, r.maximum?.etag);
      this.notifier.status($localize`:@@limits.projects.removed:Maximum removed.`);
      await this.load();
    } catch (e) {
      this.failure.set(asApiError(e));
    }
  }
}
