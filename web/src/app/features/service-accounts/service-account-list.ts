import { Component, inject, signal } from '@angular/core';
import { DIALOG_DATA, DialogClose, DialogRef, DialogTitle, QuaderDialog } from '../../shared/ui/dialog';
import { Router, RouterLink } from '@angular/router';
import { firstValueFrom } from 'rxjs';
import { newIdempotencyKey } from '../../api/client';
import { IdentityApi, RepositoryAction, ServiceAccount } from '../../api/identity-api';
import { ApiError } from '../../api/problem';
import { Notifier } from '../../core/notifier';
import { SessionStore } from '../../core/session-store';
import { TextField } from '../../shared/access/fields';
import { fieldErrorAt } from '../../shared/field-errors';
import { Needs } from '../../shared/access/needs';
import { PagedResource, asApiError } from '../../shared/paged-resource';
import { ErrorNotice } from '../../shared/error-notice';
import { DataTable, TableColumn, TableSort } from '../../shared/ui/data-table';
import { ListHead } from '../../shared/ui/list-head';
import { NoValue } from '../../shared/ui/no-value';
import { ProjectLink } from '../../shared/ui/project-mark';
import { PageHeader } from '../../shared/ui/page-header';
import { AccountRights } from './account-rights';
import { Busy } from '../../shared/ui/busy';

/**
 * What the dialog is opened with: the project key to create a project service account, named within it (R-AUTHZ-026), and with
 * it a repository key to create an account of that repository (R-AUTH-046).
 */
export interface ServiceAccountCreateData {
  readonly project?: string;
  readonly repository?: string;
}

/**
 * "New service account" (R-UI-026, R-UI-181). The idempotency key lives as long as the dialog (R-UI-042). Opened
 * with a project it creates a project service account: the name is unique within the project and the account
 * belongs to the project for good (R-UI-041). Opened with a repository too, it creates an account of the repository with its
 * **Rights on this repository**, Read ticked (R-AUTHZ-039).
 */
@Component({
  selector: 'quader-service-account-create-dialog',
  imports: [Busy, AccountRights, DialogTitle, DialogClose, ErrorNotice, TextField],
  template: `
    <h2 quaderDialogTitle i18n="@@sa-create.title">New service account</h2>
    <form (submit)="submit($event)" novalidate>
      <div class="dialog-body">
        @if (error(); as e) {
          <quader-error-notice [error]="e" />
        }
        <quader-text-field
          width="name"
          i18n-label="@@sa-create.name"
          label="Name"
          name="name"
          [(value)]="name"
          [required]="true"
          [error]="fieldError('/name')"
          [hint]="nameHint"
        />
        @if (repository) {
          <quader-text-field
            width="name"
            i18n-label="@@sa-create.description-optional"
            label="Description (optional)"
            name="description"
            [(value)]="description"
            [error]="fieldError('/description')"
          />
          <quader-account-rights [(actions)]="actions" [error]="fieldError('/actions')" />
        } @else {
          <quader-text-field width="name" i18n-label="@@sa-create.description" label="Description" name="description" [(value)]="description" [error]="fieldError('/description')" />
        }
      </div>
      <div class="dialog-actions">
        <button class="btn btn-tertiary" type="button" quaderDialogClose i18n="@@sa-create.cancel">Cancel</button>
        <button class="btn btn-confirm" type="submit" [disabled]="busy()" [quaderBusy]="busy()" i18n="@@sa-create.submit">Create service account</button>
      </div>
    </form>
  `,
  styleUrl: '../../shared/access/access.scss',
})
export class ServiceAccountCreateDialog {
  private readonly api = inject(IdentityApi);
  private readonly ref = inject<DialogRef<ServiceAccount, ServiceAccountCreateDialog>>(DialogRef);
  private readonly notifier = inject(Notifier);
  private readonly data = inject<ServiceAccountCreateData | null>(DIALOG_DATA, { optional: true });
  private readonly key = newIdempotencyKey();

  protected readonly project = this.data?.project;
  protected readonly repository = this.data?.repository;
  protected readonly nameHint = this.repository
    ? $localize`:@@sa-create.name-hint-repository:Unique within the repository ${this.project}:project:/${this.repository}:repository: and cannot be changed later. Lower-case letters, digits and -.`
    : this.project
      ? $localize`:@@sa-create.name-hint-project:Unique within the project ${this.project}:project: and cannot be changed later. Lower-case letters, digits and -.`
      : $localize`:@@sa-create.name-hint:Lower-case letters, digits and -, two to 63 characters`;
  protected readonly name = signal('');
  protected readonly description = signal('');
  /** The rights of a repository's account; Read is ticked (R-UI-181). */
  protected readonly actions = signal<readonly RepositoryAction[]>(['read']);
  protected readonly busy = signal(false);
  protected readonly error = signal<ApiError | null>(null);

  protected fieldError(pointer: string): string | null {
    return fieldErrorAt(this.error(), pointer);
  }

  protected async submit(event: Event): Promise<void> {
    event.preventDefault();
    this.busy.set(true);
    this.error.set(null);
    try {
      const body = { name: this.name().trim(), description: this.description().trim() };
      const created = await this.api.createServiceAccount(
        this.project ?? null,
        this.repository ? { ...body, actions: [...this.actions()] } : body,
        this.key,
        this.repository ?? null,
      );
      this.notifier.status($localize`:@@sa-create.created:Service account created.`);
      this.ref.close(created);
    } catch (e) {
      this.error.set(asApiError(e));
    } finally {
      this.busy.set(false);
    }
  }
}

/**
 * Service accounts (R-UI-026, R-UI-181): name, description, the level (Instance, Project, Repository) with the project and the
 * repository of an account that has one, and the status, with the search by name above the list (Q-UI-421) and cursor paging; the
 * list has no other filter, so no drawer. The hint says when to use one.
 */
@Component({
  selector: 'quader-service-account-list',
  imports: [PageHeader, RouterLink, DataTable, ListHead, Needs, NoValue, ProjectLink],
  template: `
    <quader-page-header heading="Service accounts" i18n-heading="@@sas.heading">
      <button actions class="btn btn-confirm" type="button" [disabled]="!session.has('identities:write')" aria-describedby="needs-sas" (click)="add()" i18n="@@sas.add">New service account</button>
    </quader-page-header>
    <quader-needs id="needs-sas" capability="identities:write" />
    <p i18n="@@sas.hint">
      Use a service account for CI systems that cannot sign in through OpenID Connect and for long-running automation. Its tokens act as the account, not as a person.
    </p>

    <quader-list-head [searchLabel]="searchLabel" [search]="q()" (searchChange)="searchFor($event)" [filters]="false" />

    <quader-data-table
      [caption]="caption"
      [columns]="columns"
      [sort]="sort()"
      (sortChange)="sortBy($event)"
      [loaded]="list.loaded()"
      [loading]="list.loading()"
      loadingText="Loading the service accounts."
      i18n-loadingText="@@sa.list.loading"
      [error]="list.error()"
      [empty]="list.items().length === 0"
      [emptyText]="q() ? noMatch : noAccount"
      [hasMore]="list.hasMore()"
      (more)="list.more()"
      (retry)="list.reload()"
    >
      @for (a of list.items(); track a.id) {
        <tr>
          <th scope="row"><a [routerLink]="saLink(a)" translate="no">{{ a.name }}</a></th>
          <td>
            @if (a.description) {
              {{ a.description }}
            } @else {
              <quader-no-value />
            }
          </td>
          <td>{{ levelText(a) }}</td>
          <!-- A row names its project by its mark and its name (Q-UI-424), and a repository's account its repository beside it. -->
          <td>
            @if (a.project) {
              <quader-project-link [key]="a.project" />
              @if (a.repository) {
                <span class="secondary" translate="no">{{ a.project }}/{{ a.repository }}</span>
              }
            } @else {
              <quader-no-value />
            }
          </td>
          <td>
            @if (a.status === 'active') {
              <span class="ok"><span class="material-icons inline" aria-hidden="true">check_circle</span><ng-container i18n="@@sas.active">Active</ng-container></span>
            } @else {
              <span class="bad"><span class="material-icons inline" aria-hidden="true">block</span><ng-container i18n="@@sas.disabled">Disabled</ng-container></span>
            }
          </td>
        </tr>
      }
    </quader-data-table>
  `,
  styleUrl: '../../shared/access/access.scss',
})
export class ServiceAccountList {
  private readonly api = inject(IdentityApi);
  private readonly dialog = inject(QuaderDialog);
  private readonly router = inject(Router);
  protected readonly session = inject(SessionStore);

  protected readonly list = new PagedResource<ServiceAccount>();
  protected readonly q = signal('');
  protected readonly sort = signal<TableSort>({ column: 'name', direction: 'asc' });
  protected readonly caption = $localize`:@@sas.caption:Service accounts`;
  protected readonly searchLabel = $localize`:@@sas.search:Filter service accounts`;
  /** An empty list says there is no service account yet; a search that finds none says so (Q-UI-565). */
  protected readonly noAccount = $localize`:@@sas.empty-none:There is no service account yet.`;
  protected readonly noMatch = $localize`:@@sas.empty:No service account matches.`;
  protected readonly columns: readonly TableColumn[] = [
    { id: 'name', label: $localize`:@@sas.col.name:Name`, sortable: true },
    { id: 'description', label: $localize`:@@sas.col.description:Description` },
    { id: 'level', label: $localize`:@@sas.col.level:Level` },
    { id: 'project', label: $localize`:@@sas.col.project:Project` },
    { id: 'status', label: $localize`:@@sas.col.status:Status` },
  ];

  constructor() {
    void this.apply();
  }

  /**
   * `/projects/<project>/repositories/<repository>/service-accounts/<name>` for a repository's account,
   * `/projects/<project>/service-accounts/<name>` for a project's, `/admin/service-accounts/<name>` otherwise.
   */
  protected saLink(a: ServiceAccount): string[] {
    if (a.project && a.repository) return ['/projects', a.project, 'repositories', a.repository, 'service-accounts', a.name];
    return a.project ? ['/projects', a.project, 'service-accounts', a.name] : ['/admin/service-accounts', a.name];
  }

  protected levelText(a: ServiceAccount): string {
    switch (a.level) {
      case 'repository':
        return $localize`:@@sas.level.repository:Repository`;
      case 'project':
        return $localize`:@@sas.level.project:Project`;
      default:
        return $localize`:@@sas.level.instance:Instance`;
    }
  }

  protected async apply(): Promise<void> {
    const q = this.q().trim() || undefined;
    const sort = this.sort();
    await this.list.start(this.api.serviceAccounts({ q, sort: sort.direction === 'desc' ? '-name' : 'name' }));
  }

  protected sortBy(sort: TableSort): void {
    this.sort.set(sort);
    void this.apply();
  }

  protected searchFor(text: string): void {
    this.q.set(text);
    void this.apply();
  }

  protected async add(): Promise<void> {
    const ref = this.dialog.open<ServiceAccountCreateDialog, undefined, ServiceAccount>(
      ServiceAccountCreateDialog,
      { maxWidth: '36rem' },
    );
    const created = await firstValueFrom(ref.closed);
    if (created) await this.router.navigate(this.saLink(created));
  }
}
