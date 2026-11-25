import { DatePipe } from '@angular/common';
import { Component, computed, effect, inject, input, signal, untracked } from '@angular/core';
import { Router, RouterLink } from '@angular/router';
import { IdentityApi, RepositoryAction, ServiceAccount } from '../../api/identity-api';
import { ApiError } from '../../api/problem';
import { Versioned } from '../../api/repositories-api';
import { Notifier } from '../../core/notifier';
import { SessionStore } from '../../core/session-store';
import { Confirm } from '../../shared/confirm-dialog';
import { ConflictResolver } from '../../shared/conflict-dialog';
import { TextField } from '../../shared/access/fields';
import { fieldErrorAt } from '../../shared/field-errors';
import { asApiError } from '../../shared/paged-resource';
import { Needs } from '../../shared/access/needs';
import { PrincipalRoles } from '../../shared/access/principal-roles';
import { ErrorNotice } from '../../shared/error-notice';
import { Disclosure } from '../../shared/ui/disclosure';
import { PageHeader } from '../../shared/ui/page-header';
import { TokensApi } from '../../api/tokens-api';
import { AccountRights, actionLabel } from './account-rights';
import { TechnicalTokens } from './technical-tokens';
import { Busy } from '../../shared/ui/busy';
import { Loading } from '../../shared/ui/loading';

/**
 * One service account of the instance, a project or a repository (R-UI-026, R-UI-181): description, for a repository's account its
 * rights on the repository as checkboxes (R-AUTHZ-039), status with disable and enable, and the section **Technical tokens**
 * (`<quader-technical-tokens>`): each token named for the system that uses it, with its secret issued once, rotated, revoked and
 * renewed. Issuing needs recent authentication; the dialog of R-UI-042 handles it.
 */
@Component({
  selector: 'quader-service-account-detail',
  imports: [Loading, Busy, AccountRights, PageHeader, DatePipe, ErrorNotice, Needs, PrincipalRoles, Disclosure, RouterLink, TextField, TechnicalTokens],
  template: `
    <quader-page-header [heading]="name()" [name]="true">
      <span meta class="badge" i18n="@@sa.heading">Service account</span>
    </quader-page-header>
    @if (repositoryKey(); as repo) {
      <p class="subject">
        <ng-container i18n="@@sa.repository">Service account of repository</ng-container>&ngsp;
        <a [routerLink]="['/projects', projectKey(), 'repositories', repo, 'settings']" translate="no">{{ projectKey() }}/{{ repo }}</a>
      </p>
    } @else if (projectKey(); as key) {
      <p class="subject">
        <ng-container i18n="@@sa.project">Service account of project</ng-container>&ngsp;
        <span translate="no">{{ key }}</span>
      </p>
    }
    @if (!canWrite()) {
      <quader-needs capability="identities:write" />
    }

    @if (loadError(); as e) {
      <quader-error-notice [error]="e" (retry)="load()" />
    }
    @if (actionError(); as e) {
      <quader-error-notice [error]="e" />
    }

    @if (account(); as a) {
      <div class="settings">
        <form (submit)="save($event)" novalidate>
          <div class="row">
            <quader-text-field width="name" i18n-label="@@sa.description" label="Description" name="description" [(value)]="description" [readonly]="!canWrite()" [error]="fieldError('/description')" />
            <quader-text-field width="name" i18n-label="@@sa.id" label="ID" name="id" [value]="a.id" [readonly]="true" />
          </div>
          @if (a.repository) {
            <quader-account-rights [(actions)]="actions" [readonly]="!canWrite()" [error]="fieldError('/actions')" />
          }
          @if (canWrite()) {
            <button class="btn btn-confirm" type="submit" [disabled]="busy() || !dirty()" [quaderBusy]="busy()" i18n="@@sa.save-changes">Save changes</button>
          }
        </form>

        <quader-disclosure remember="service-account.details" i18n-heading="@@sa.details" heading="Details" [hint]="a.project ? detailsHintPlace : detailsHintInstance">
          <dl class="facts">
            <dt i18n="@@sa.status">Status</dt>
            <dd>
              @if (a.status === 'active') {
                <span class="ok"><span class="material-icons" aria-hidden="true">check_circle</span> <ng-container i18n="@@sa.active">Active</ng-container></span>
              } @else {
                <span class="bad"><span class="material-icons" aria-hidden="true">block</span> <ng-container i18n="@@sa.disabled">Disabled</ng-container></span>
              }
            </dd>
            <dt i18n="@@sa.reference">Reference</dt>
            <dd><code translate="no">{{ a.reference }}</code></dd>
            @if (!a.project) {
              <dt i18n="@@sa.admin">Administrator role</dt>
              <dd data-testid="sa-admin">
                @if (a.isAdmin) {
                  <ng-container i18n="@@sa.admin.yes">Held by this account</ng-container>
                } @else {
                  <ng-container i18n="@@sa.admin.no">Not held</ng-container>
                }
              </dd>
            }
            <dt i18n="@@sa.created">Created</dt>
            <dd>{{ a.createdAt | date: 'medium' }}</dd>
          </dl>
        </quader-disclosure>

        @if (session.has('roles:read') && !a.project) {
          <quader-principal-roles [principal]="a.reference" />
        }

        <!-- A quiet section like its siblings, open while the account holds a technical token (R-UI-181, Q-UI-333). -->
        <quader-disclosure
          i18n-heading="@@sa.technical-tokens"
          heading="Technical tokens"
          i18n-hint="@@sa.technical-tokens-hint"
          hint="One token for each system that uses this account, each with a limit of its own."
          [expanded]="a.technicalTokens > 0"
        >
          @if (canReadTokens()) {
            <quader-technical-tokens [account]="a" (changed)="load()" />
          } @else {
            <p i18n="@@sa.tokens-denied">You need the permission to read identities to manage the tokens of this account.</p>
          }
        </quader-disclosure>

        @if (canWrite() || offersRevokeAll()) {
          <quader-disclosure
            i18n-heading="@@sa.advanced"
            heading="Advanced"
            [hint]="advancedHint()"
          >
            @if (offersRevokeAll()) {
              <p class="muted" i18n="@@tokens.revoke-all-note">Every token stops working at once, and the tools that use them are refused. This cannot be undone.</p>
              <p>
                <button quaderBusy class="btn btn-danger" type="button" (click)="revokeAll()" i18n="@@tokens.revoke-all">Revoke all tokens</button>
              </p>
            }
            @if (canWrite()) {
              <p class="muted">
                @if (a.status === 'active') {
                  <ng-container i18n="@@sa.disable-note">A disabled account can no longer authenticate; you can also revoke its tokens.</ng-container>
                } @else {
                  <ng-container i18n="@@sa.enable-note">The account is disabled and cannot authenticate. Enabling it lets its tokens work again.</ng-container>
                }
              </p>
              <p>
                @if (a.status === 'active') {
                  <button class="btn btn-tertiary" type="button" [disabled]="busy()" [quaderBusy]="busy()" (click)="disable()" i18n="@@sa.disable">Disable</button>
                } @else {
                  <button class="btn btn-tertiary" type="button" [disabled]="busy()" [quaderBusy]="busy()" (click)="enable()" i18n="@@sa.enable">Enable</button>
                }
              </p>
              <p class="muted" i18n="@@sa.delete-note">Deleting the account removes it and its tokens. This cannot be undone.</p>
              <button class="btn btn-danger" type="button" [disabled]="busy()" [quaderBusy]="busy()" (click)="remove()" i18n="@@sa.delete">Delete</button>
            }
          </quader-disclosure>
        }
      </div>
    } @else if (!loadError()) {
      <quader-loading size="page" i18n="@@sa.loading">Loading the service account.</quader-loading>
    }
  `,
  styleUrl: '../../shared/access/access.scss',
  styles: `
    .settings {
      max-inline-size: var(--quader-settings-measure);
    }
    form {
      padding-block-end: var(--quader-space-6);
    }
    .row {
      display: grid;
      grid-template-columns: minmax(0, 5fr) minmax(0, 4fr);
      gap: var(--quader-space-6);
    }
    @media (max-width: 40rem) {
      .row {
        grid-template-columns: minmax(0, 1fr);
      }
    }
  `,
})
export class ServiceAccountDetail {
  /** Route parameter `:name`: bare within its place. */
  readonly name = input.required<string>();
  /** Route parameter `:project`; absent for an instance-wide account (R-AUTHZ-026). */
  readonly project = input<string>();
  /** Route parameter `:key` of a repository's account (R-AUTH-046): the repository's key within the project. */
  readonly key = input<string>();

  private readonly api = inject(IdentityApi);
  private readonly tokensApi = inject(TokensApi);
  private readonly confirmer = inject(Confirm);
  private readonly conflicts = inject(ConflictResolver);
  private readonly notifier = inject(Notifier);
  private readonly router = inject(Router);
  protected readonly session = inject(SessionStore);
  /**
   * **Revoke all tokens** of an account of the instance or a project that holds a technical token, for `tokens:revoke` or an Owner
   * of its project (R-AUTH-029); a repository's account ends its tokens one by one, or with a disable.
   */
  protected readonly offersRevokeAll = computed(() => {
    const a = this.account();
    return (
      a !== null &&
      !a.repository &&
      a.technicalTokens > 0 &&
      (this.session.has('tokens:revoke') || this.session.isOwnerOf(this.projectKey()))
    );
  });
  /** The hint of **Details**: only an account of the instance can hold the administrator role (R-AUTH-046, R-AUTHZ-039). */
  protected readonly detailsHintInstance = $localize`:@@sa.details-hint:Status, administrator role and when the account was created.`;
  protected readonly detailsHintPlace = $localize`:@@sa.details-hint-place:Status, reference and when the account was created.`;
  /** The hint of the section Advanced names **Revoke all tokens** while it is offered. */
  protected readonly advancedHint = computed(() =>
    this.offersRevokeAll()
      ? $localize`:@@sa.advanced-hint-tokens:Revoke all tokens, disable or delete this account.`
      : $localize`:@@sa.advanced-hint:Disable or delete this account.`,
  );

  protected readonly account = signal<ServiceAccount | null>(null);
  /** The `ETag` the record was read with; every write sends it as `If-Match` (Q-API-42). */
  private etag = '';
  protected readonly loadError = signal<ApiError | null>(null);
  protected readonly actionError = signal<ApiError | null>(null);
  protected readonly description = signal('');
  /** The rights of a repository's account as the form holds them (R-AUTHZ-039). */
  protected readonly actions = signal<readonly RepositoryAction[]>([]);
  protected readonly busy = signal(false);
  protected readonly dirty = computed(() => {
    const a = this.account();
    return a !== null && (this.description().trim() !== a.description || this.actionsChanged(a));
  });

  /** The project of the account, or null for an instance-wide account (R-AUTHZ-026). */
  protected readonly projectKey = computed(() => this.project() ?? null);
  /** The repository of a repository's account (R-AUTH-046), null otherwise. */
  protected readonly repositoryKey = computed(() => this.key() ?? null);
  /** `identities:write`, or the Owner role of the account's project. */
  protected readonly canWrite = computed(
    () => this.session.has('identities:write') || this.session.isOwnerOf(this.projectKey()),
  );
  /** The technical tokens are read with `identities:read`, or by an Owner of the account's project (R-AUTH-045). */
  protected readonly canReadTokens = computed(
    () => this.session.has('identities:read') || this.session.isOwnerOf(this.projectKey()),
  );

  constructor() {
    effect(() => {
      this.name();
      untracked(() => void this.load());
    });
  }

  protected async load(): Promise<void> {
    try {
      this.adopt(await this.api.serviceAccount(this.name(), this.projectKey(), this.repositoryKey()));
      this.loadError.set(null);
    } catch (e) {
      this.account.set(null);
      this.loadError.set(asApiError(e));
    }
  }

  private adopt(loaded: Versioned<ServiceAccount>): void {
    this.etag = loaded.etag;
    this.account.set(loaded.value);
    this.description.set(loaded.value.description);
    this.actions.set(loaded.value.actions ?? []);
  }

  private actionsChanged(a: ServiceAccount): boolean {
    if (!a.repository) return false;
    const stored = a.actions ?? [];
    const now = this.actions();
    return stored.length !== now.length || stored.some((x) => !now.includes(x));
  }

  protected fieldError(pointer: string): string | null {
    return fieldErrorAt(this.actionError(), pointer);
  }

  protected async save(event: Event): Promise<void> {
    event.preventDefault();
    const a = this.account();
    if (!a || !this.dirty()) return;
    const patch: { description?: string; actions?: RepositoryAction[] } = {};
    if (this.description().trim() !== a.description) patch.description = this.description().trim();
    if (this.actionsChanged(a)) patch.actions = [...this.actions()];
    this.busy.set(true);
    this.actionError.set(null);
    try {
      const result = await this.conflicts.save<ServiceAccount>({
        etag: this.etag,
        patch,
        fields: {
          '/description': $localize`:@@sa.description:Description`,
          '/actions': {
            label: $localize`:@@sa-rights.legend:Rights on this repository`,
            show: (v) => (Array.isArray(v) ? v.map((a) => actionLabel(a as RepositoryAction)) : [String(v)]),
          },
        },
        send: (etag) => this.api.updateServiceAccount(a.name, this.projectKey(), patch, etag, this.repositoryKey()),
        reload: () => this.api.serviceAccount(a.name, this.projectKey(), this.repositoryKey()),
      });
      if (result.outcome !== 'dismissed') this.adopt(result.current);
      if (result.outcome === 'saved') this.notifier.status($localize`:@@sa.saved:Service account saved.`);
    } catch (e) {
      this.actionError.set(asApiError(e));
    } finally {
      this.busy.set(false);
    }
  }

  protected async disable(): Promise<void> {
    const answer = await this.confirmer.ask({
      title: $localize`:@@sa.disable-title:Disable this service account?`,
      lines: [$localize`:@@sa.disable-text:${this.name()}:account: can no longer authenticate.`],
      confirmLabel: $localize`:@@sa.disable-confirm:Disable`,
      choice: $localize`:@@sa.disable-revoke:Also revoke all tokens of this account`,
      choiceChecked: false,
    });
    if (answer === null) return;
    await this.act(async () => {
      await this.api.disableServiceAccount(this.name(), this.projectKey(), { revokeTokens: answer.checked }, this.repositoryKey());
      await this.load();
    }, $localize`:@@sa.disabled-done:Service account disabled.`);
  }

  protected async enable(): Promise<void> {
    await this.act(async () => {
      await this.api.enableServiceAccount(this.name(), this.projectKey(), this.repositoryKey());
      await this.load();
    }, $localize`:@@sa.enabled-done:Service account enabled.`);
  }

  protected async remove(): Promise<void> {
    const answer = await this.confirmer.ask({
      title: $localize`:@@sa.delete-title:Delete this service account?`,
      lines: [$localize`:@@sa.delete-text:${this.name()}:account: and its tokens are removed. This cannot be undone.`],
      confirmLabel: $localize`:@@sa.delete-confirm:Delete`,
    });
    if (answer === null) return;
    this.actionError.set(null);
    try {
      await this.api.deleteServiceAccount(this.name(), this.projectKey(), this.repositoryKey());
      this.notifier.status($localize`:@@sa.deleted:Service account deleted.`);
      const projectKey = this.projectKey();
      const repo = this.repositoryKey();
      await this.router.navigate(
        repo && projectKey
          ? ['/projects', projectKey, 'repositories', repo, 'settings']
          : projectKey
            ? ['/projects', projectKey, 'service-accounts']
            : ['/admin/service-accounts'],
      );
    } catch (e) {
      this.actionError.set(asApiError(e));
    }
  }

  /** Revokes every token of the account at once (R-AUTH-029); the technical tokens stay, listed as revoked. */
  protected async revokeAll(): Promise<void> {
    const answer = await this.confirmer.ask({
      title: $localize`:@@tokens.revoke-all-title:Revoke all tokens?`,
      lines: [$localize`:@@tokens.revoke-all-text:Every token of this owner stops working at once. This cannot be undone.`],
      confirmLabel: $localize`:@@tokens.revoke-all-confirm:Revoke all`,
    });
    if (answer === null) return;
    await this.act(async () => {
      await this.tokensApi.revokeAllOfServiceAccount(this.name(), this.projectKey());
      await this.load();
    }, $localize`:@@tokens.revoked-all:All tokens revoked.`);
  }

  private async act(run: () => Promise<void>, done: string): Promise<void> {
    this.busy.set(true);
    this.actionError.set(null);
    try {
      await run();
      this.notifier.status(done);
    } catch (e) {
      this.actionError.set(asApiError(e));
    } finally {
      this.busy.set(false);
    }
  }
}
