import { Component, computed, effect, inject, input, untracked } from '@angular/core';
import { Router, RouterLink } from '@angular/router';
import { firstValueFrom } from 'rxjs';
import { IdentityApi, ServiceAccount } from '../../api/identity-api';
import { SessionStore } from '../../core/session-store';
import { PagedResource } from '../../shared/paged-resource';
import { DataTable, TableColumn } from '../../shared/ui/data-table';
import { QuaderDialog } from '../../shared/ui/dialog';
import { NoValue } from '../../shared/ui/no-value';
import { actionLabel } from './account-rights';
import { ServiceAccountCreateData, ServiceAccountCreateDialog } from './service-account-list';

/**
 * The section **Service accounts** of a repository's settings (R-UI-181, R-AUTH-046): the repository's own accounts, each named
 * `sa:<project>/<repository>/<name>`, with its description, the actions it holds on the repository, its state and its technical
 * tokens; a row opens the account's page. **New service account** opens the create dialog with the rights on this repository. An
 * Owner of the project and a holder of `identities:write` create (Q-AUTH-417).
 */
@Component({
  selector: 'quader-repository-service-accounts',
  imports: [DataTable, NoValue, RouterLink],
  template: `
    @if (canCreate()) {
      <div class="toolbar">
        <button class="btn btn-confirm" type="button" (click)="add()" i18n="@@repo-sas.add">New service account</button>
      </div>
    }
    <quader-data-table
      [caption]="caption"
      [columns]="columns"
      [loaded]="list.loaded()"
      [loading]="list.loading()"
      loadingText="Loading the service accounts."
      i18n-loadingText="@@sa.list.loading"
      [error]="list.error()"
      [empty]="list.items().length === 0"
      i18n-emptyText="@@repo-sas.empty"
      emptyText="This repository has no service account yet."
      [hasMore]="list.hasMore()"
      (more)="list.more()"
      (retry)="list.reload()"
    >
      @for (a of list.items(); track a.id) {
        <tr>
          <th scope="row" class="wrap-name">
            <a [routerLink]="link(a)" translate="no">{{ a.reference }}</a>
            @if (a.description) {
              <span class="secondary">{{ a.description }}</span>
            }
          </th>
          <td>{{ rights(a) }}</td>
          <td>
            @if (a.status === 'active') {
              <span class="ok"><span class="material-icons inline" aria-hidden="true">check_circle</span><ng-container i18n="@@repo-sas.active">Active</ng-container></span>
            } @else {
              <span class="bad"><span class="material-icons inline" aria-hidden="true">block</span><ng-container i18n="@@repo-sas.disabled">Disabled</ng-container></span>
            }
          </td>
          <td class="numeric">
            @if (a.technicalTokens > 0) {
              {{ a.technicalTokens }}
            } @else {
              <quader-no-value />
            }
          </td>
        </tr>
      }
    </quader-data-table>
  `,
  styleUrl: '../../shared/access/access.scss',
})
export class RepositoryServiceAccounts {
  readonly project = input.required<string>();
  readonly repository = input.required<string>();

  private readonly api = inject(IdentityApi);
  private readonly dialog = inject(QuaderDialog);
  private readonly router = inject(Router);
  private readonly session = inject(SessionStore);

  protected readonly list = new PagedResource<ServiceAccount>();
  protected readonly canCreate = computed(() => this.session.has('identities:write') || this.session.isOwnerOf(this.project()));
  protected readonly caption = $localize`:@@repo-sas.caption:Service accounts of the repository`;
  protected readonly columns: readonly TableColumn[] = [
    { id: 'name', label: $localize`:@@repo-sas.col.name:Account`, width: '10rem', prefer: true },
    { id: 'rights', label: $localize`:@@repo-sas.col.rights:Rights`, width: '8rem' },
    { id: 'status', label: $localize`:@@repo-sas.col.status:Status` },
    { id: 'tokens', label: $localize`:@@repo-sas.col.tokens:Technical tokens`, numeric: true },
  ];

  constructor() {
    effect(() => {
      const project = this.project();
      const repository = this.repository();
      untracked(() => void this.list.start(this.api.serviceAccounts({ project, repository, sort: 'name' })));
    });
  }

  protected link(a: ServiceAccount): string[] {
    return ['/projects', this.project(), 'repositories', this.repository(), 'service-accounts', a.name];
  }

  protected rights(a: ServiceAccount): string {
    return (a.actions ?? []).map(actionLabel).join(', ');
  }

  protected async add(): Promise<void> {
    const ref = this.dialog.open<ServiceAccountCreateDialog, ServiceAccountCreateData, ServiceAccount>(ServiceAccountCreateDialog, {
      data: { project: this.project(), repository: this.repository() },
      maxWidth: '36rem',
    });
    const created = await firstValueFrom(ref.closed);
    if (created) await this.router.navigate(this.link(created));
  }
}
