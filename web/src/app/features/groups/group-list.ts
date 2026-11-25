import { Component, computed, inject, signal } from '@angular/core';
import { QuaderDialog } from '../../shared/ui/dialog';
import { Router, RouterLink } from '@angular/router';
import { firstValueFrom } from 'rxjs';
import { Group, GroupFilters, IdentityApi } from '../../api/identity-api';
import { SessionStore } from '../../core/session-store';
import { SelectField, SelectOption } from '../../shared/access/fields';
import { Needs } from '../../shared/access/needs';
import { PagedResource } from '../../shared/paged-resource';
import { DataTable, TableColumn, TableSort } from '../../shared/ui/data-table';
import { FilterPanel } from '../../shared/ui/filter-panel';
import { ListChip, ListHead } from '../../shared/ui/list-head';
import { NoValue } from '../../shared/ui/no-value';
import { GroupCreateDialog } from './group-create-dialog';
import { PageHeader } from '../../shared/ui/page-header';

export function groupOriginLabel(origin: Group['origin']): string {
  switch (origin) {
    case 'admin':
      return $localize`:@@group.origin.admin:Created by an administrator`;
    case 'oidc':
      return $localize`:@@group.origin.oidc:Identity provider`;
    default:
      return $localize`:@@group.origin.scim:SCIM`;
  }
}

/** Groups (R-UI-025): name, description, origin and member count, with the search by name, the origin in the drawer (Q-UI-420), a sortable name and paging. */
@Component({
  selector: 'quader-group-list',
  imports: [
    PageHeader,
    RouterLink,
    DataTable,
    FilterPanel,
    ListHead,
    Needs,
    NoValue,
    SelectField,
  ],
  template: `
    <quader-page-header heading="Groups" i18n-heading="@@groups.heading">
      <button actions class="btn btn-confirm" type="button" [disabled]="!session.has('identities:write')" aria-describedby="needs-groups" (click)="add()" i18n="@@groups.add">New group</button>
    </quader-page-header>
    <quader-needs id="needs-groups" capability="identities:write" />

    <quader-list-head
      [searchLabel]="searchLabel"
      [search]="applied().q ?? ''"
      (searchChange)="searchFor($event)"
      [chips]="chips()"
      (removeChip)="removeChip($event)"
      [(filtersOpen)]="panelOpen"
      [controls]="panel.panelId"
    />
    <quader-filter-panel #panel [(open)]="panelOpen" [activeCount]="chips().length" (apply)="apply()" (clearAll)="clear()" (dismissed)="restage()">
      <quader-select-field i18n-label="@@groups.origin" label="Origin" name="origin" [options]="originOptions" [(value)]="origin" />
    </quader-filter-panel>

    <quader-data-table
      [caption]="caption"
      [columns]="columns"
      [sort]="sort()"
      (sortChange)="sortBy($event)"
      [loaded]="list.loaded()"
      [loading]="list.loading()"
      loadingText="Loading the groups."
      i18n-loadingText="@@groups.loading"
      [error]="list.error()"
      [empty]="list.items().length === 0"
      [emptyText]="applied().q || chips().length > 0 ? noMatch : noGroup"
      [hasMore]="list.hasMore()"
      (more)="list.more()"
      (retry)="list.reload()"
    >
      @for (g of list.items(); track g.id) {
        <tr>
          <th scope="row"><a [routerLink]="['/admin/groups', g.name]" translate="no">{{ g.name }}</a></th>
          <td>
            @if (g.description) {
              {{ g.description }}
            } @else {
              <quader-no-value />
            }
          </td>
          <td>{{ originLabel(g.origin) }}</td>
          <td class="num">{{ g.memberCount }}</td>
        </tr>
      }
    </quader-data-table>
  `,
  styleUrl: '../../shared/access/access.scss',
})
export class GroupList {
  private readonly api = inject(IdentityApi);
  private readonly dialog = inject(QuaderDialog);
  private readonly router = inject(Router);
  protected readonly session = inject(SessionStore);

  protected readonly list = new PagedResource<Group>();
  protected readonly q = signal('');
  protected readonly origin = signal('');
  protected readonly panelOpen = signal(false);
  /** The filters the list shows now; the drawer's field stages the next one. */
  protected readonly applied = signal<GroupFilters>({});
  protected readonly searchLabel = $localize`:@@groups.search:Filter groups`;
  /** An empty list says there is no group yet; a search or a filter that finds none says so (Q-UI-565). */
  protected readonly noGroup = $localize`:@@groups.empty-none:There is no group yet.`;
  protected readonly noMatch = $localize`:@@groups.empty:No group matches.`;
  protected readonly chips = computed<readonly ListChip[]>(() => {
    const origin = this.applied().origin;
    return origin ? [{ key: 'origin', label: $localize`:@@groups.origin:Origin`, value: groupOriginLabel(origin) }] : [];
  });
  protected readonly sort = signal<TableSort>({ column: 'name', direction: 'asc' });
  protected readonly caption = $localize`:@@groups.caption:Groups`;
  protected readonly columns: readonly TableColumn[] = [
    { id: 'name', label: $localize`:@@groups.col.name:Name`, sortable: true },
    { id: 'description', label: $localize`:@@groups.col.description:Description` },
    {
      id: 'origin',
      label: $localize`:@@groups.col.origin:Origin`,
      help: $localize`:@@groups.help.origin:Where the group comes from: created by an administrator, mapped from the identity provider, or provisioned by SCIM.`,
    },
    { id: 'members', label: $localize`:@@groups.col.members:Members`, numeric: true },
  ];
  protected readonly originLabel = groupOriginLabel;
  protected readonly originOptions: readonly SelectOption[] = [
    { value: '', label: $localize`:@@groups.any:Any` },
    { value: 'admin', label: groupOriginLabel('admin') },
    { value: 'oidc', label: groupOriginLabel('oidc') },
    { value: 'scim', label: groupOriginLabel('scim') },
  ];

  constructor() {
    void this.apply();
  }

  protected async apply(): Promise<void> {
    const filters: GroupFilters = {
      q: this.q().trim() || undefined,
      origin: (this.origin() || undefined) as GroupFilters['origin'],
    };
    this.applied.set(filters);
    const sort = this.sort();
    await this.list.start(this.api.groups({ ...filters, sort: sort.direction === 'desc' ? '-name' : 'name' }));
  }

  protected sortBy(sort: TableSort): void {
    this.sort.set(sort);
    void this.apply();
  }

  protected searchFor(text: string): void {
    this.restage();
    this.q.set(text);
    void this.apply();
  }

  /** The drawer's field back to the filter the list shows: it closed without **Apply**. */
  protected restage(): void {
    this.origin.set(this.applied().origin ?? '');
  }

  protected removeChip(chip: ListChip): void {
    if (chip.key === 'origin') this.origin.set('');
    void this.apply();
  }

  /** **Clear all** of the drawer: its filter goes; the search text is the field's and stays. */
  protected clear(): void {
    this.origin.set('');
    void this.apply();
  }

  protected async add(): Promise<void> {
    const ref = this.dialog.open<GroupCreateDialog, undefined, Group>(GroupCreateDialog, { maxWidth: '36rem' });
    const created = await firstValueFrom(ref.closed);
    if (created) await this.router.navigate(['/admin/groups', created.name]);
  }
}
