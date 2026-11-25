import { TechnicalTokenDescriptionData, TechnicalTokenDescriptionDialog } from './technical-token-description-dialog';
import { Component, DestroyRef, computed, effect, inject, input, output, signal, untracked } from '@angular/core';
import { firstValueFrom } from 'rxjs';
import { newIdempotencyKey } from '../../api/client';
import { ServiceAccount } from '../../api/identity-api';
import { ApiError } from '../../api/problem';
import { AccountAddress, TechnicalToken, TechnicalTokenCreated, TechnicalTokensApi } from '../../api/technical-tokens-api';
import { TokensApi } from '../../api/tokens-api';
import { Notifier } from '../../core/notifier';
import { SessionStore } from '../../core/session-store';
import { Confirm } from '../../shared/confirm-dialog';
import { TokenForm, TokenFormRequest } from '../../shared/access/token-form';
import { TokenSecret } from '../../shared/access/token-secret';
import { ErrorNotice } from '../../shared/error-notice';
import { asApiError } from '../../shared/paged-resource';
import { QuaderDialog } from '../../shared/ui/dialog';
import { TechnicalTokenLimits, accountPlace } from '../limits/technical-token-limit';
import { RotateDialog } from '../tokens/token-manager';
import { TechnicalTokenTable, TokenLimitsView, currentSecret } from './technical-token-table';

/** What the open form issues: a new technical token, or a new secret of the named one. */
type FormFor = { readonly kind: 'token' } | { readonly kind: 'secret'; readonly name: string };

/**
 * The section **Technical tokens** of a service account's page at the instance, a project or a repository (R-UI-181, R-AUTH-045):
 * **New technical token** opens the create form with **Name** first; a token's row rotates and revokes its secret, and a token
 * without a live secret gets a new one. A created secret is shown once through `<quader-token-secret>` and lives in this
 * component's signal only (R-UI-043). Writes use the idempotency key of the open form (R-UI-042); a retried issue that answers
 * `token_already_issued` revokes the secret whose value was lost and issues again with a new key.
 *
 * Who sees what (R-UI-181, R-AUTH-027, R-AUTH-030): the administrator role creates, rotates and revokes; an Owner of the account's
 * project creates (with the bounds of Q-AUTHZ-177), issues new secrets and revokes, and does not rotate, which the server keeps
 * for administrators; `tokens:revoke` revokes. A holder of `identities:write` alone sees the list and creates nothing.
 */
@Component({
  selector: 'quader-technical-tokens',
  imports: [ErrorNotice, TechnicalTokenTable, TokenForm, TokenSecret],
  template: `
    @if (canIssue() && !form()) {
      <div class="toolbar">
        <button class="btn btn-confirm" type="button" (click)="openCreate()" i18n="@@tt.create">New technical token</button>
      </div>
    }

    @if (secret(); as s) {
      <quader-token-secret [value]="s.token" [description]="s.name" (closed)="secret.set(null)" />
    }

    @if (form(); as f) {
      <section class="card" aria-labelledby="tt-form-heading">
        @if (f.kind === 'token') {
          <h2 id="tt-form-heading" i18n="@@tt.form-heading">New technical token</h2>
        } @else {
          <h2 id="tt-form-heading" i18n="@@tt.secret-heading">New secret for {{ f.name }}</h2>
        }
        @if (formError(); as e) {
          <quader-error-notice [error]="e" />
        }
        <quader-token-form
          [mode]="f.kind === 'token' ? 'technical' : 'secret'"
          [submitLabel]="f.kind === 'token' ? createLabel : renewLabel"
          [allowNonExpiring]="isAdmin()"
          [lifetimeCapped]="!isAdmin()"
          tokenType="sat"
          [offerAdmin]="offerAdmin()"
          [busy]="busy()"
          [failure]="formError()"
          (submitted)="submit(f, $event)"
          (cancelled)="closeForm()"
        />
      </section>
    }

    @if (actionError(); as e) {
      <quader-error-notice [error]="e" />
    }
    @if (loadError(); as e) {
      <quader-error-notice [error]="e" (retry)="reload()" />
    }
    @if (loaded()) {
      <quader-technical-token-table
        [tokens]="active()"
        [emptyText]="tokens().length === 0 ? noToken : noActiveToken"
        [canRotate]="isAdmin()"
        [canRevoke]="canRevoke()"
        [canManage]="canManage()"
        [limits]="limits()"
        (rotate)="rotate($event)"
        (revoke)="revoke($event)"
        (setLimit)="setLimit($event)"
        (editDescription)="editDescription($event)"
        (remove)="remove($event)"
      />
      @if (ended().length > 0) {
        <h3 class="ended" i18n="@@tt.ended-heading">Expired and revoked</h3>
        <quader-technical-token-table
          [tokens]="ended()"
          [caption]="endedCaption"
          [ended]="true"
          [canIssue]="canIssue()"
          [canManage]="canManage()"
          (renew)="openRenew($event)"
          (editDescription)="editDescription($event)"
          (remove)="remove($event)"
        />
      }
    } @else if (!loadError()) {
      <p role="status" i18n="@@tt.loading">Loading technical tokens.</p>
    }
  `,
  styleUrl: '../../shared/access/access.scss',
  styles: `
    .ended {
      margin-block: var(--quader-space-4) var(--quader-space-2);
      font: var(--quader-type-title-small);
    }
  `,
})
export class TechnicalTokens {
  readonly account = input.required<ServiceAccount>();
  /** The number of technical tokens changed: the page reads the account again for its count. */
  readonly changed = output<void>();

  private readonly api = inject(TechnicalTokensApi);
  private readonly tokensApi = inject(TokensApi);
  private readonly session = inject(SessionStore);
  private readonly confirmer = inject(Confirm);
  private readonly notifier = inject(Notifier);
  private readonly dialog = inject(QuaderDialog);
  private readonly tokenLimits = inject(TechnicalTokenLimits);

  protected readonly tokens = signal<readonly TechnicalToken[]>([]);
  /** The download limits' columns (R-UI-181): read once with the list, null for a caller who reads no limits. */
  protected readonly limits = signal<TokenLimitsView | null>(null);
  // The ended tokens show no limit: a token without a live secret downloads nothing, and its rule shows again in the active table
  // when **Issue new secret**, their one action (Q-UI-740), gives it a secret; so the table is a table at 1,024 px (Q-UI-761).
  /** The tokens with a live secret, and the ones without: a table each (R-UI-163). */
  protected readonly active = computed(() => this.tokens().filter((t) => t.state === 'active'));
  protected readonly ended = computed(() => this.tokens().filter((t) => t.state !== 'active'));
  protected readonly noToken = $localize`:@@tt.empty:This account has no technical token yet.`;
  protected readonly noActiveToken = $localize`:@@tt.empty-active:No technical token of this account has a live secret.`;
  protected readonly endedCaption = $localize`:@@tt.ended-caption:Expired and revoked technical tokens`;
  protected readonly loaded = signal(false);
  protected readonly loadError = signal<ApiError | null>(null);
  protected readonly actionError = signal<ApiError | null>(null);
  protected readonly formError = signal<ApiError | null>(null);
  protected readonly form = signal<FormFor | null>(null);
  protected readonly busy = signal(false);
  /** The secret that was just issued; component-local, never stored (R-UI-043). */
  protected readonly secret = signal<TechnicalTokenCreated | null>(null);
  protected readonly createLabel = $localize`:@@tt.create-submit:Create technical token`;
  protected readonly renewLabel = $localize`:@@tt.renew-submit:Issue new secret`;
  private idempotencyKey = newIdempotencyKey();

  protected readonly address = computed<AccountAddress>(() => {
    const a = this.account();
    return { name: a.name, project: a.project ?? null, repository: a.repository ?? null };
  });
  /** The administrator role: creates, rotates, and may make a secret that never expires. */
  protected readonly isAdmin = computed(() => this.session.hasRole('admin'));
  /** An Owner of the account's project issues its technical tokens (R-AUTH-027, Q-AUTHZ-177). */
  private readonly owner = computed(() => this.session.isOwnerOf(this.account().project ?? null));
  protected readonly canIssue = computed(() => this.isAdmin() || this.owner());
  /**
   * Administrator rights are offered for the token of an instance-wide account that holds the role itself, to a caller who holds
   * it: for any other account the choice would take effect nowhere (Q-UI-220).
   */
  protected readonly offerAdmin = computed(() => this.isAdmin() && this.account().level === 'instance' && this.account().isAdmin === true);
  protected readonly canRevoke = computed(() => this.session.has('tokens:revoke') || this.owner());
  /** Changes a token's description and deletes the token: `identities:write`, or an Owner of the account's project (R-AUTH-045). */
  protected readonly canManage = computed(() => this.session.has('identities:write') || this.owner());
  /** Reads the limits: `limits:read`, or an Owner of the account's project (R-LIM-029, R-API-082). */
  private readonly canReadLimits = computed(() => this.session.has('limits:read') || this.owner());
  /** Sets a token's limit at its account's place: `limits:write`, or an Owner below its project (R-UI-181, R-LIM-016). */
  private readonly canSetLimit = computed(() => this.session.has('limits:write') || this.owner());

  constructor() {
    inject(DestroyRef).onDestroy(() => this.secret.set(null));
    effect(() => {
      this.address();
      untracked(() => void this.reload());
    });
  }

  async reload(): Promise<void> {
    try {
      const [tokens, limits] = await Promise.all([this.api.list(this.address()), this.readLimits()]);
      this.loadError.set(null);
      this.tokens.set(tokens);
      this.limits.set(limits);
      this.loaded.set(true);
    } catch (e) {
      this.loadError.set(asApiError(e));
    }
  }

  /**
   * What the table shows of the limits: the profiles' names, the default of the account's tokens (Unlimited for an account that
   * holds `admin`, R-LIM-038) and the last 24 hours of the tokens, read in the instance's usage for a holder of `usage:read` and in
   * the project's for its Owner.
   */
  private async readLimits(): Promise<TokenLimitsView | null> {
    if (!this.canReadLimits()) return null;
    const a = this.account();
    const scope = this.session.has('usage:read') || !a.project ? {} : { project: a.project };
    const [profiles, lastDay] = await Promise.all([this.tokenLimits.profiles(), this.tokenLimits.lastDay(scope)]);
    const defaultKey = a.isAdmin ? 'unlimited' : 'technical';
    const defaultProfile = profiles.find((p) => p.key === defaultKey)?.name ?? (a.isAdmin ? 'Unlimited' : 'Technical');
    return { profiles, defaultProfile, lastDay, canSet: this.canSetLimit() };
  }

  /** **Set limit**, **Change limit** and **Remove limit**: the rule dialog at the account's place, capped for an Owner (R-UI-181, R-LIM-016). */
  protected async setLimit(t: TechnicalToken): Promise<void> {
    const place = accountPlace(this.address());
    const capped = !this.session.has('limits:write') && place.kind !== 'instance';
    await this.act(async () => {
      const answer = await this.tokenLimits.edit(place, t, capped);
      if (answer === 'removed') this.notifier.status($localize`:@@tt.limit-removed:Limit removed.`);
      else if (answer) this.notifier.status($localize`:@@tt.limit-saved:Limit saved.`);
    });
  }

  protected openCreate(): void {
    this.open({ kind: 'token' });
  }

  protected openRenew(t: TechnicalToken): void {
    this.open({ kind: 'secret', name: t.name });
  }

  private open(f: FormFor): void {
    this.idempotencyKey = newIdempotencyKey();
    this.formError.set(null);
    this.form.set(f);
  }

  protected closeForm(): void {
    this.form.set(null);
  }

  protected async submit(f: FormFor, request: TokenFormRequest): Promise<void> {
    this.busy.set(true);
    this.formError.set(null);
    const { name, description, ...secret } = request;
    const send = (key: string): Promise<TechnicalTokenCreated> =>
      f.kind === 'token'
        ? this.api.create(this.address(), { ...secret, name: name ?? '', ...(description ? { description } : {}) }, key)
        : this.api.newSecret(this.address(), f.name, secret, key);
    try {
      let created: TechnicalTokenCreated;
      try {
        created = await send(this.idempotencyKey);
      } catch (e) {
        // The first attempt issued a secret whose value was lost with its answer: revoke it, issue again with a new key, and
        // show only the new one (R-UI-042).
        const lost = e instanceof ApiError && e.code === 'token_already_issued' ? e.problem?.['tokenId'] : null;
        if (typeof lost !== 'string') throw e;
        await this.tokensApi.revoke(lost);
        this.idempotencyKey = newIdempotencyKey();
        created = await send(this.idempotencyKey);
      }
      this.secret.set(created);
      this.closeForm();
      await this.reload();
      if (f.kind === 'token') this.changed.emit();
    } catch (e) {
      this.formError.set(asApiError(e));
    } finally {
      this.busy.set(false);
    }
  }

  /** Rotates the token's current secret (R-AUTH-030): a new secret now, the old one ends after the chosen overlap. */
  protected async rotate(t: TechnicalToken): Promise<void> {
    const current = currentSecret(t);
    if (!current) return;
    const ref = this.dialog.open<RotateDialog, undefined, { overlapSeconds: number }>(RotateDialog, { maxWidth: '32rem' });
    const choice = await firstValueFrom(ref.closed);
    if (!choice) return;
    const key = newIdempotencyKey();
    await this.act(async () => {
      const rotated = await this.tokensApi.rotate(current.id, choice.overlapSeconds, key);
      this.secret.set({ ...t, token: rotated.token, secretId: rotated.id });
    });
  }

  /** Revokes the token's live secrets; the technical token stays, listed as revoked, and can get a new secret (R-AUTH-045). */
  protected async revoke(t: TechnicalToken): Promise<void> {
    const answer = await this.confirmer.ask({
      title: $localize`:@@tt.revoke-title:Revoke this technical token?`,
      lines: [
        $localize`:@@tt.revoke-text:${t.name}:name: stops working at once on every replica. It keeps its name and its limit, and can get a new secret later.`,
      ],
      confirmLabel: $localize`:@@tt.revoke-confirm:Revoke`,
    });
    if (answer === null) return;
    await this.act(async () => {
      for (const s of t.secrets) await this.tokensApi.revoke(s.id);
      this.notifier.status($localize`:@@tt.revoked:Technical token revoked.`);
    });
  }

  /** **Edit description** of the row's menu (G5-51): the token's description, saved with the token's `ETag`. */
  protected async editDescription(t: TechnicalToken): Promise<void> {
    const ref = this.dialog.open<TechnicalTokenDescriptionDialog, TechnicalTokenDescriptionData, string>(TechnicalTokenDescriptionDialog, {
      maxWidth: '32rem',
      data: { account: this.address(), token: t },
    });
    if ((await firstValueFrom(ref.closed)) === undefined) return;
    this.notifier.status($localize`:@@tt.description-saved:Description saved.`);
    await this.reload();
  }

  /** **Delete** of the row's menu (G5-51): revokes the token's live secrets and deletes it, after the question every removal asks. */
  protected async remove(t: TechnicalToken): Promise<void> {
    const answer = await this.confirmer.ask({
      title: $localize`:@@tt.delete-title:Delete this technical token?`,
      lines: [$localize`:@@tt.delete-text:${t.name}:name: and its secrets are removed: it stops working at once on every replica. This cannot be undone.`],
      confirmLabel: $localize`:@@tt.delete-confirm:Delete`,
    });
    if (answer === null) return;
    await this.act(async () => {
      await this.api.remove(this.address(), t.name);
      this.notifier.status($localize`:@@tt.deleted:Technical token deleted.`);
      this.changed.emit();
    });
  }

  private async act(run: () => Promise<void>): Promise<void> {
    this.actionError.set(null);
    try {
      await run();
      await this.reload();
    } catch (e) {
      this.actionError.set(asApiError(e));
    }
  }
}
