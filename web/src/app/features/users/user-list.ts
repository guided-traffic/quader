import { Component, computed, inject, signal } from '@angular/core';
import { QuaderDialog } from '../../shared/ui/dialog';
import { Router, RouterLink } from '@angular/router';
import { firstValueFrom } from 'rxjs';
import { IdentityApi, User, UserFilters } from '../../api/identity-api';
import { SessionStore } from '../../core/session-store';
import { SelectField, SelectOption, TextField } from '../../shared/access/fields';
import { Needs } from '../../shared/access/needs';
import { PagedResource } from '../../shared/paged-resource';
import { DataTable, TableColumn, TableSort } from '../../shared/ui/data-table';
import { FilterPanel } from '../../shared/ui/filter-panel';
import { ListChip, ListHead } from '../../shared/ui/list-head';
import { TimeAgo } from '../../shared/ui/time-ago';
import { UserCreateDialog } from './user-create-dialog';
import { originLabel } from './user-labels';
import { PageHeader } from '../../shared/ui/page-header';
import { QuaderAvatar } from '../../shared/ui/avatar';

/**
 * Users (R-UI-024): username, display name, email, origin, status and last sign-in; the search by name or email above the list,
 * the filters status, origin, group, administrator and external in the drawer, the ones that are set as chips (Q-UI-420), and the badge External next to the name of an external user
 * (R-UI-056); the user name column sorts through
 * the API's `sort` parameter; cursor paging with "Load more". "Add user"
 * pre-provisions a user linked to an identity-provider identity; there are no password fields.
 */
@Component({
  selector: 'quader-user-list',
  imports: [
    QuaderAvatar,
    PageHeader,
    RouterLink,
    DataTable,
    FilterPanel,
    ListHead,
    Needs,
    SelectField,
    TextField,
    TimeAgo,
  ],
  templateUrl: './user-list.html',
  styleUrl: '../../shared/access/access.scss',
})
export class UserList {
  private readonly api = inject(IdentityApi);
  private readonly dialog = inject(QuaderDialog);
  private readonly router = inject(Router);
  protected readonly session = inject(SessionStore);

  protected readonly list = new PagedResource<User>();
  protected readonly q = signal('');
  protected readonly status = signal('');
  protected readonly origin = signal('');
  protected readonly group = signal('');
  protected readonly admin = signal('');
  protected readonly external = signal('');
  protected readonly panelOpen = signal(false);
  /** The filters the list shows now; the drawer's fields stage the next ones. */
  protected readonly applied = signal<UserFilters>({});
  protected readonly searchLabel = $localize`:@@users.search:Filter users`;
  /** The filters of the drawer that are set, as the head's chips (Q-UI-422). */
  protected readonly chips = computed<readonly ListChip[]>(() => {
    const f = this.applied();
    const out: ListChip[] = [];
    const named = (options: readonly SelectOption[], value: string): string => options.find((o) => o.value === value)?.label ?? value;
    if (f.status) out.push({ key: 'status', label: $localize`:@@users.status:Status`, value: named(this.statusOptions, f.status) });
    if (f.origin) out.push({ key: 'origin', label: $localize`:@@users.origin:Origin`, value: named(this.originOptions, f.origin) });
    if (f.group) out.push({ key: 'group', label: $localize`:@@users.group:Group`, value: f.group });
    if (f.admin !== undefined) out.push({ key: 'admin', label: $localize`:@@users.admin:Administrator`, value: named(this.adminOptions, String(f.admin)) });
    if (f.external !== undefined) {
      out.push({ key: 'external', label: $localize`:@@users.external:External`, value: named(this.externalOptions, String(f.external)) });
    }
    return out;
  });
  protected readonly sort = signal<TableSort>({ column: 'username', direction: 'asc' });

  protected readonly caption = $localize`:@@users.caption:Users`;
  protected readonly columns: readonly TableColumn[] = [
    { id: 'username', label: $localize`:@@users.col.user:User`, sortable: true, width: '14rem' },
    { id: 'email', label: $localize`:@@users.col.email:Email` },
    {
      id: 'origin',
      label: $localize`:@@users.col.origin:Origin`,
      help: $localize`:@@users.help.origin:How the account came to exist: at first sign-in through the identity provider, created by an administrator, provisioned by SCIM, or the built-in break-glass account.`,
    },
    { id: 'status', label: $localize`:@@users.col.status:Status` },
    { id: 'lastLogin', label: $localize`:@@users.col.last-login:Last sign-in` },
  ];

  protected readonly statusOptions: readonly SelectOption[] = [
    { value: '', label: $localize`:@@users.any:Any` },
    { value: 'active', label: $localize`:@@users.status.active:Active` },
    { value: 'disabled', label: $localize`:@@users.status.disabled:Disabled` },
    { value: 'locked', label: $localize`:@@users.status.locked:Locked` },
  ];
  protected readonly originOptions: readonly SelectOption[] = [
    { value: '', label: $localize`:@@users.any:Any` },
    { value: 'jit', label: originLabel('jit') },
    { value: 'admin', label: originLabel('admin') },
    { value: 'scim', label: originLabel('scim') },
    { value: 'system', label: originLabel('system') },
  ];
  protected readonly adminOptions: readonly SelectOption[] = [
    { value: '', label: $localize`:@@users.any:Any` },
    { value: 'true', label: $localize`:@@users.admin.yes:Administrators` },
    { value: 'false', label: $localize`:@@users.admin.no:Not administrators` },
  ];
  protected readonly externalOptions: readonly SelectOption[] = [
    { value: '', label: $localize`:@@users.any:Any` },
    { value: 'true', label: $localize`:@@users.external.yes:External users` },
    { value: 'false', label: $localize`:@@users.external.no:Not external` },
  ];
  protected readonly originLabel = originLabel;

  constructor() {
    void this.apply();
  }

  protected async apply(): Promise<void> {
    const filters: UserFilters = {
      q: this.q().trim() || undefined,
      status: (this.status() || undefined) as UserFilters['status'],
      origin: (this.origin() || undefined) as UserFilters['origin'],
      group: this.group().trim() || undefined,
      admin: this.admin() === '' ? undefined : this.admin() === 'true',
      external: this.external() === '' ? undefined : this.external() === 'true',
    };
    this.applied.set(filters);
    const sort = this.sort();
    await this.list.start(
      this.api.users({ ...filters, sort: sort.direction === 'desc' ? '-username' : 'username' }),
    );
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

  /** The drawer's fields back to the filters the list shows: it closed without **Apply**. */
  protected restage(): void {
    const f = this.applied();
    this.status.set(f.status ?? '');
    this.origin.set(f.origin ?? '');
    this.group.set(f.group ?? '');
    this.admin.set(f.admin === undefined ? '' : String(f.admin));
    this.external.set(f.external === undefined ? '' : String(f.external));
  }

  protected removeChip(chip: ListChip): void {
    const field = { status: this.status, origin: this.origin, group: this.group, admin: this.admin, external: this.external }[chip.key];
    this.restage();
    field?.set('');
    void this.apply();
  }

  /** **Clear all** of the drawer: its filters go; the search text is the field's and stays. */
  protected clear(): void {
    this.status.set('');
    this.origin.set('');
    this.group.set('');
    this.admin.set('');
    this.external.set('');
    void this.apply();
  }

  protected async add(): Promise<void> {
    const ref = this.dialog.open<UserCreateDialog, undefined, User>(UserCreateDialog, { maxWidth: '36rem' });
    const created = await firstValueFrom(ref.closed);
    if (created) await this.router.navigate(['/admin/users', created.username]);
  }
}
