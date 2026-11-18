import { Component, computed, effect, inject, input, signal, untracked } from '@angular/core';
import { ActivatedRoute, RouterLink } from '@angular/router';
import { PermissionPolicy, PolicyApi, PolicyFilters } from '../../api/authz-api';
import { SessionStore } from '../../core/session-store';
import { TextField } from '../../shared/access/fields';
import { Needs } from '../../shared/access/needs';
import { PagedResource } from '../../shared/paged-resource';
import { DataTable, TableColumn, TableSort } from '../../shared/ui/data-table';
import { FilterPanel } from '../../shared/ui/filter-panel';
import { ListChip, ListHead } from '../../shared/ui/list-head';
import { ProjectLink } from '../../shared/ui/project-mark';
import { expandActions } from './policy-model';
import { PageHeader } from '../../shared/ui/page-header';

/** "Users: 2, groups: 3": the entries of a policy by kind of principal (R-UI-027). */
export function principalSummary(policy: Pick<PermissionPolicy, 'entries'>): string {
  const count = { user: 0, group: 0, sa: 0 };
  for (const e of policy.entries) {
    if (e.principal.startsWith('user:')) count.user++;
    else if (e.principal.startsWith('group:')) count.group++;
    else if (e.principal.startsWith('sa:')) count.sa++;
  }
  const parts: string[] = [];
  if (count.user) parts.push($localize`:@@policies.sum.users:Users: ${count.user}:count:`);
  if (count.group) parts.push($localize`:@@policies.sum.groups:Groups: ${count.group}:count:`);
  if (count.sa) parts.push($localize`:@@policies.sum.sas:Service accounts: ${count.sa}:count:`);
  return parts.join(', ');
}

/** The union of the actions of all entries, in canonical order. */
export function actionSummary(policy: Pick<PermissionPolicy, 'entries'>): string {
  return expandActions(policy.entries.flatMap((e) => e.actions)).join(', ');
}

/**
 * Permission policies (R-UI-027): name, project (mark and name), description, principal summary, resources summary and actions;
 * filters by principal, repository and project in the drawer, the ones that are set as chips beside **Filters** (Q-UI-420); the name column sorts
 * through the API's `sort` parameter; cursor paging. The `principal` query parameter
 * opens the list filtered.
 */
@Component({
  selector: 'quader-policy-list',
  imports: [PageHeader, RouterLink, DataTable, FilterPanel, ListHead, Needs, ProjectLink, TextField],
  templateUrl: './policy-list.html',
  styleUrl: '../../shared/access/access.scss',
})
export class PolicyList {
  /** Query parameters, bound by the router's component input binding. */
  readonly principal = input<string | undefined>();
  readonly repository = input<string | undefined>();
  /** A project key: only the policies of that project (R-UI-041); the list is instance-wide without it (Q-UI-129). */
  readonly project = input<string | undefined>();

  private readonly api = inject(PolicyApi);
  private readonly route = inject(ActivatedRoute);
  protected readonly session = inject(SessionStore);

  protected readonly list = new PagedResource<PermissionPolicy>();
  protected readonly principalFilter = signal('');
  protected readonly repositoryFilter = signal('');
  protected readonly projectFilter = signal('');
  protected readonly panelOpen = signal(false);
  /** The list is a project's (`/projects/<key>/policies`): the project is the route's and no filter. */
  protected readonly inProject = (): boolean => this.route.snapshot.paramMap.has('project');
  /** The filters the list shows now; the drawer's fields stage the next ones. */
  protected readonly applied = signal<PolicyFilters>({});
  protected readonly chips = computed<readonly ListChip[]>(() => {
    const f = this.applied();
    const out: ListChip[] = [];
    if (f.principal) out.push({ key: 'principal', label: $localize`:@@policies.principal:Principal`, value: f.principal });
    if (f.repository) out.push({ key: 'repository', label: $localize`:@@policies.repository:Repository`, value: f.repository });
    if (f.project && !this.inProject()) out.push({ key: 'project', label: $localize`:@@policies.project:Project`, value: f.project });
    return out;
  });
  protected readonly sort = signal<TableSort>({ column: 'name', direction: 'asc' });
  protected readonly caption = $localize`:@@policies.caption:Permission policies`;
  protected readonly columns: readonly TableColumn[] = [
    { id: 'name', label: $localize`:@@policies.col.name:Name`, sortable: true },
    {
      id: 'project',
      label: $localize`:@@policies.col.project:Project`,
      help: $localize`:@@policies.help.project:The project a policy belongs to; a dash is a global policy. A project's policy grants on that project's repositories only.`,
    },
    { id: 'description', label: $localize`:@@policies.col.description:Description` },
    {
      id: 'principals',
      label: $localize`:@@policies.col.principals:Principals`,
      help: $localize`:@@policies.help.principals:Who the policy grants to, counted by kind: users, groups and service accounts.`,
    },
    {
      id: 'resources',
      label: $localize`:@@policies.col.resources:Repositories`,
      help: $localize`:@@policies.help.resources:The repository keys or patterns the policy covers. Open the policy to see which repositories they match now.`,
    },
    { id: 'actions', label: $localize`:@@policies.col.actions:Actions` },
  ];
  protected readonly principalSummary = principalSummary;
  protected readonly actionSummary = actionSummary;

  constructor() {
    effect(() => {
      this.principalFilter.set(this.principal() ?? '');
      this.repositoryFilter.set(this.repository() ?? '');
      this.projectFilter.set(this.project() ?? '');
      untracked(() => void this.apply());
    });
  }

  protected async apply(): Promise<void> {
    const filters: PolicyFilters = {
      principal: this.principalFilter().trim() || undefined,
      repository: this.repositoryFilter().trim() || undefined,
      project: this.projectFilter().trim() || undefined,
      // Groups are expanded by the API unless told otherwise; the list shows policies that name
      // the principal or one of its groups, as the filter says.
    };
    this.applied.set(filters);
    const sort = this.sort();
    await this.list.start(this.api.list({ ...filters, sort: sort.direction === 'desc' ? '-name' : 'name' }));
  }

  protected sortBy(sort: TableSort): void {
    this.sort.set(sort);
    void this.apply();
  }

  /** The drawer's fields back to the filters the list shows: it closed without **Apply**. */
  protected restage(): void {
    const f = this.applied();
    this.principalFilter.set(f.principal ?? '');
    this.repositoryFilter.set(f.repository ?? '');
    this.projectFilter.set(f.project ?? '');
  }

  protected removeChip(chip: ListChip): void {
    this.restage();
    if (chip.key === 'principal') this.principalFilter.set('');
    if (chip.key === 'repository') this.repositoryFilter.set('');
    if (chip.key === 'project') this.projectFilter.set('');
    void this.apply();
  }

  protected clear(): void {
    this.principalFilter.set('');
    this.repositoryFilter.set('');
    if (!this.inProject()) this.projectFilter.set('');
    void this.apply();
  }

  /** `/projects/<project>/policies/<name>` for a project's policy, `/admin/permissions/<name>` otherwise. */
  protected policyLink(p: PermissionPolicy): string[] {
    return p.project ? ['/projects', p.project, 'policies', p.name] : ['/admin/permissions', p.name];
  }

  protected repositorySummary(p: PermissionPolicy): string {
    const repos = p.resources.repositories ?? [];
    const shown = repos.slice(0, 3).join(', ');
    return repos.length > 3 ? $localize`:@@policies.sum.more:${shown}:list: and ${repos.length - 3}:count: more` : shown;
  }
}
