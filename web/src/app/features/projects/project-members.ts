import { CdkMenu, CdkMenuItem, CdkMenuTrigger } from '@angular/cdk/menu';
import { Component, computed, effect, inject, input, output, signal, untracked } from '@angular/core';
import { firstValueFrom } from 'rxjs';
import { ApiError } from '../../api/problem';
import { PROJECT_ROLES, ProjectMember, ProjectRole, ProjectsApi } from '../../api/projects-api';
import { SessionStore } from '../../core/session-store';
import { Notifier } from '../../core/notifier';
import { SelectOption } from '../../shared/access/fields';
import { QuaderAvatar } from '../../shared/ui/avatar';
import { DataTable, TableColumn, TableSort } from '../../shared/ui/data-table';
import { QuaderDialog } from '../../shared/ui/dialog';
import { ListHead } from '../../shared/ui/list-head';
import { ErrorNotice } from '../../shared/error-notice';
import { asApiError } from '../../shared/paged-resource';
import { ProjectNeeds } from './project-access';
import { ProjectMemberChoice, ProjectMemberDialog, ProjectMemberDialogData } from './project-member-dialog';
import { projectRoleLabel } from './project-roles';

/**
 * The members of a project with their roles (R-UI-041, R-AUTHZ-026, Q-AUTHZ-207, Q-UI-214): the list, adding a user or group
 * with a role in a dialog, changing a role and removing a member. The Owner of the project and the holder of `projects:write`
 * change it; every other member reads. Removing or demoting the last Owner is refused by the server (409
 * `last_owner`, Q-AUTHZ-217) and the refusal is shown here; an administrator of Quader may leave a project without an
 * Owner, so the UI does not hide the action. The API decides in any case (R-UI-005).
 *
 * The list is built like every other list (R-UI-142, Q-UI-550): the head with the search by name (no drawer: the list has no
 * other filter), the shared data table, sorted by its column headers Account and Role. The answer holds every member, so the
 * search and the sort run in the browser and are exact. The number of members is the page's heading's (`count`).
 */
@Component({
  selector: 'quader-project-members',
  imports: [CdkMenu, CdkMenuItem, CdkMenuTrigger, DataTable, ErrorNotice, ListHead, ProjectNeeds, QuaderAvatar],
  template: `
    @if (writeError(); as e) {
      @if (e.code === 'last_owner') {
        <p class="notice" role="alert">
          <span class="material-icons" aria-hidden="true">error</span>
          <span i18n="@@project-members.last-owner">A project keeps at least one Owner. Make another member an Owner first.</span>
        </p>
      } @else {
        <quader-error-notice [error]="e" />
      }
    }

    <quader-list-head [searchLabel]="searchLabel" [search]="filter()" (searchChange)="filter.set($event)" [filters]="false" />

    <quader-data-table
      [caption]="caption"
      [columns]="columns"
      [sort]="sort()"
      (sortChange)="sort.set($event)"
      [loaded]="loaded()"
      [loading]="!loaded()"
      [error]="loadError()"
      (retry)="load()"
      [empty]="shown().length === 0"
      [emptyText]="emptyText()"
    >
      @for (m of shown(); track m.principal) {
        <tr>
          <th scope="row" class="person">
            @if (m.kind === 'group') {
              <span class="avatar group" aria-hidden="true"><span class="material-icons">groups</span></span>
            } @else {
              <quader-avatar class="avatar" aria-hidden="true" [seed]="m.id ?? m.principal" [imageUrl]="m.avatarUrl" />
            }
            <span class="who">
              <span class="line">
                <span class="name" translate="no">{{ m.displayName || m.name }}</span>
                @if (isMe(m)) {
                  <span class="badge info" i18n="@@project-members.me">It's you</span>
                }
              </span>
              @if (m.kind === 'group') {
                <span class="secondary" i18n="@@project-members.kind-group">Group</span>
              } @else {
                <span class="secondary" translate="no">&#64;{{ m.name }}</span>
              }
            </span>
          </th>
          <td i18n="@@project-members.direct">Direct member</td>
          <td>
            @if (canWrite()) {
              <select
                class="select-input role"
                [name]="'role-' + m.principal"
                [attr.aria-label]="roleOf(m)"
                [disabled]="busy()"
                (change)="change(m, $any($event.target).value)"
              >
                @for (o of roleOptions; track o.value) {
                  <option [value]="o.value" [selected]="o.value === m.role">{{ o.label }}</option>
                }
              </select>
            } @else {
              {{ roleText(m.role) }}
            }
          </td>
          <td class="actions">
            @if (canWrite()) {
              <button
                class="btn btn-tertiary btn-icon"
                type="button"
                [attr.aria-label]="actionsOf(m)"
                [disabled]="busy()"
                [cdkMenuTriggerFor]="rowMenu"
              >
                <span class="material-icons" aria-hidden="true">more_vert</span>
              </button>
              <ng-template #rowMenu>
                <div class="menu" cdkMenu>
                  <button type="button" class="menu-item" cdkMenuItem (cdkMenuItemTriggered)="remove(m)" i18n="@@project-members.remove">
                    Remove member
                  </button>
                </div>
              </ng-template>
            }
          </td>
        </tr>
      }
    </quader-data-table>

    @if (!canWrite()) {
      <quader-project-needs capability="projects:write" [state]="'denied'" />
    }
  `,
  styleUrl: '../../shared/access/access.scss',
  styles: `
    :host {
      display: block;
    }
    /* A group has no picture: its icon on the neutral fill, in the place and the size of an avatar (th.person, 32 px). */
    .avatar.group {
      display: inline-flex;
      align-items: center;
      justify-content: center;
      background: var(--quader-badge-neutral-bg);
      color: var(--quader-badge-neutral-fg);
    }
    .avatar.group .material-icons {
      font-size: 1.25rem;
    }
    /* The name and the badge It's you on one line where they fit; the badge never breaks. */
    .line {
      display: flex;
      flex-wrap: wrap;
      align-items: center;
      gap: var(--quader-space-1) var(--quader-space-2);
    }
    .name {
      overflow-wrap: anywhere;
    }
    .role {
      inline-size: auto;
    }
    .notice {
      display: flex;
      gap: var(--quader-space-3);
      margin-block: 0 var(--quader-space-3);
      padding: var(--quader-space-3) var(--quader-space-4);
      border: 1px solid var(--quader-error);
      border-radius: var(--quader-radius-m);
      background: var(--quader-error-container);
      color: var(--quader-on-error-container);
    }
  `,
})
export class ProjectMembers {
  readonly project = input.required<string>();
  /** Emitted after a write so that the counts of the project screen are read again. */
  readonly changed = output<void>();
  /** The number of members after every read: the page's heading carries it (Q-UI-550). */
  readonly count = output<number>();

  private readonly api = inject(ProjectsApi);
  private readonly session = inject(SessionStore);
  private readonly notifier = inject(Notifier);
  private readonly dialog = inject(QuaderDialog);

  protected readonly members = signal<readonly ProjectMember[]>([]);
  protected readonly loaded = signal(false);
  protected readonly busy = signal(false);
  /** A read of the list that failed: the table shows it with its retry. */
  protected readonly loadError = signal<ApiError | null>(null);
  /** A change the server refused (the last Owner, Q-AUTHZ-217): shown above the list, which stays. */
  protected readonly writeError = signal<ApiError | null>(null);
  protected readonly filter = signal('');
  protected readonly sort = signal<TableSort>({ column: 'account', direction: 'asc' });
  protected readonly searchLabel = $localize`:@@project-members.filter:Filter members`;
  protected readonly columns: readonly TableColumn[] = [
    { id: 'account', label: $localize`:@@project-members.col.account:Account`, sortable: true, width: '16rem' },
    { id: 'source', label: $localize`:@@project-members.col.source:Source` },
    { id: 'role', label: $localize`:@@project-members.col.role:Role`, sortable: true },
    { id: 'actions', label: $localize`:@@project-members.col.actions:Actions`, hideLabel: true },
  ];
  protected readonly refs = computed(() => this.members().map((m) => m.principal));
  /** The members that match the filter, in the order of the sorted column; Role sorts Owner first, then by account. */
  protected readonly shown = computed(() => {
    const text = this.filter().trim().toLowerCase();
    const found = this.members().filter((m) => !text || accountOf(m).toLowerCase().includes(text) || m.name.toLowerCase().includes(text));
    const { column, direction } = this.sort();
    const by =
      column === 'role'
        ? (a: ProjectMember, b: ProjectMember) => PROJECT_ROLES.indexOf(b.role) - PROJECT_ROLES.indexOf(a.role) || accountOf(a).localeCompare(accountOf(b))
        : (a: ProjectMember, b: ProjectMember) => accountOf(a).localeCompare(accountOf(b));
    return [...found].sort((a, b) => (direction === 'desc' ? by(b, a) : by(a, b)));
  });
  protected readonly emptyText = computed(() =>
    this.members().length === 0
      ? $localize`:@@project-members.empty:The project has no member yet.`
      : $localize`:@@project-members.no-match:No member matches.`,
  );
  protected readonly canWrite = computed(
    () => this.session.has('projects:write') || this.session.isOwnerOf(this.project()),
  );
  protected readonly caption = $localize`:@@project-members.caption:Members of the project`;
  protected readonly roleOptions: readonly SelectOption[] = PROJECT_ROLES.map((r) => ({
    value: r,
    label: projectRoleLabel(r),
  }));

  constructor() {
    effect(() => {
      this.project();
      untracked(() => void this.load());
    });
  }

  protected isMe(m: ProjectMember): boolean {
    return m.kind === 'user' && m.name === this.session.whoami()?.principal?.name;
  }

  protected actionsOf(m: ProjectMember): string {
    return $localize`:@@project-members.actions-of:Actions for ${m.principal}:member:`;
  }

  /** Opens the dialog that adds a member. */
  async openAdd(): Promise<void> {
    const data: ProjectMemberDialogData = { project: this.project(), exclude: this.refs() };
    const ref = this.dialog.open<ProjectMemberDialog, ProjectMemberDialogData, ProjectMemberChoice>(ProjectMemberDialog, { data, width: '32rem' });
    const choice = await firstValueFrom(ref.closed);
    if (choice) await this.add(choice);
  }

  protected roleOf(m: ProjectMember): string {
    return $localize`:@@project-members.role-of:Role of ${m.principal}:member:`;
  }

  protected roleText(role: ProjectRole): string {
    return projectRoleLabel(role);
  }

  protected async load(): Promise<void> {
    this.loadError.set(null);
    try {
      this.members.set(await this.api.members(this.project()));
      this.count.emit(this.members().length);
    } catch (e) {
      this.loadError.set(asApiError(e));
    } finally {
      this.loaded.set(true);
    }
  }

  private async add(choice: ProjectMemberChoice): Promise<void> {
    await this.write(async () => {
      await this.api.setMember(this.project(), choice.principal, choice.role);
      this.notifier.status($localize`:@@project-members.added:Member added.`);
    });
  }

  protected async change(m: ProjectMember, role: string): Promise<void> {
    if (role === m.role) return;
    await this.write(async () => {
      await this.api.setMember(this.project(), m.principal, role as ProjectRole);
      this.notifier.status($localize`:@@project-members.changed:Role changed.`);
    });
  }

  protected async remove(m: ProjectMember): Promise<void> {
    await this.write(async () => {
      await this.api.removeMember(this.project(), m.principal);
      this.notifier.status($localize`:@@project-members.removed:Member removed.`);
    });
  }

  private async write(step: () => Promise<void>): Promise<void> {
    this.busy.set(true);
    this.writeError.set(null);
    let failure: ApiError | null = null;
    try {
      await step();
      this.changed.emit();
    } catch (e) {
      failure = asApiError(e);
    }
    await this.load();
    this.writeError.set(failure);
    this.busy.set(false);
  }
}

function accountOf(m: ProjectMember): string {
  return m.displayName || m.name;
}
