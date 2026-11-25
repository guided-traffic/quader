import { Component, DestroyRef, computed, effect, inject, input, signal, untracked } from '@angular/core';
import { LIVE_IDLE_MS, live } from '../../core/live-refresh';
import { Checkbox } from '../../shared/ui/checkbox';
import { DialogClose, DialogRef, DialogTitle, QuaderDialog } from '../../shared/ui/dialog';
import { firstValueFrom } from 'rxjs';
import { newIdempotencyKey } from '../../api/client';
import { ApiError } from '../../api/problem';
import { CreateTokenRequest, Token, TokenCreated, TokensApi } from '../../api/tokens-api';
import { Notifier } from '../../core/notifier';
import { SessionStore } from '../../core/session-store';
import { Confirm } from '../../shared/confirm-dialog';
import { DraftStore } from '../../shared/drafts';
import { SelectField } from '../../shared/access/fields';
import { PagedResource, asApiError } from '../../shared/paged-resource';
import { TokenDraft, TokenForm } from '../../shared/access/token-form';
import { TokenSecret } from '../../shared/access/token-secret';
import { TokenTable } from '../../shared/access/token-table';
import { ErrorNotice } from '../../shared/error-notice';
import { Disclosure } from '../../shared/ui/disclosure';
import { Loading } from '../../shared/ui/loading';
import { Busy } from '../../shared/ui/busy';

/**
 * Whose tokens the manager shows and what it may do with them: the caller's own or a user's. A service account's tokens are its
 * technical tokens, which the account's page manages (`<quader-technical-tokens>`, R-AUTH-045, R-UI-181).
 */
export type TokenOwnerScope = { readonly kind: 'own' } | { readonly kind: 'user'; readonly name: string };

interface RotateChoice {
  readonly overlapSeconds: number;
}

/** Asks how long the old value of a rotated token stays valid (R-AUTH-030). */
@Component({
  selector: 'quader-rotate-dialog',
  imports: [DialogTitle, DialogClose, SelectField],
  template: `
    <h2 quaderDialogTitle i18n="@@rotate.title">Rotate this token?</h2>
    <div class="dialog-body">
      <p i18n="@@rotate.text">
        A new token with the same scope is created and shown once. Everything that uses the old token must switch to it.
      </p>
      <quader-select-field
        i18n-label="@@rotate.overlap"
        label="The old token stays valid for"
        [options]="options"
        [(value)]="overlap"
      />
    </div>
    <div class="dialog-actions">
      <button class="btn btn-tertiary" type="button" quaderDialogClose i18n="@@rotate.cancel">Cancel</button>
      <button class="btn btn-confirm" type="button" (click)="confirm()" i18n="@@rotate.confirm">Rotate</button>
    </div>
  `,
})
export class RotateDialog {
  private readonly ref = inject<DialogRef<RotateChoice, RotateDialog>>(DialogRef);
  protected readonly overlap = signal('0');
  protected readonly options = [
    { value: '0', label: $localize`:@@rotate.overlap.none:Not at all (revoked now)` },
    { value: '3600', label: $localize`:@@rotate.overlap.hour:One hour` },
    { value: '86400', label: $localize`:@@rotate.overlap.day:One day` },
  ];

  protected confirm(): void {
    this.ref.close({ overlapSeconds: Number.parseInt(this.overlap(), 10) });
  }
}

/**
 * The tokens of one owner: list with state filter and paging, create where the caller may
 * (R-UI-029, R-UI-026), revoke one or all with confirmation, rotate. Revoking all is destructive, so it is a danger button last,
 * in a closed section **Advanced** (ui-style-guide.md, Settings pages): the manager's own at its end, or the page's own where the
 * page has one (`revokeAllHere` false, `offersRevokeAll`, `revokeAllTokens`). A created or rotated value is
 * shown once through `<quader-token-secret>` and lives in this component's signal only (R-UI-043);
 * it is dropped when the panel is dismissed or the component is destroyed. Writes use the
 * idempotency key of the open form (R-UI-042); a retried creation that answers
 * `token_already_issued` revokes the token whose value was lost and creates again with a new key.
 */
@Component({
  selector: 'quader-token-manager',
  imports: [Busy, Checkbox, Disclosure, ErrorNotice, Loading, TokenForm, TokenSecret, TokenTable],
  template: `
    <div class="toolbar">
      @if (createInToolbar() && offersCreate()) {
        <button class="btn btn-confirm" type="button" (click)="openForm()" i18n="@@tokens.create">Create token</button>
      }
      <quader-checkbox [checked]="showAll()" (checkedChange)="setShowAll($event)">
        <ng-container i18n="@@tokens.show-all">Show revoked and expired tokens</ng-container>
      </quader-checkbox>
    </div>

    @if (secret(); as s) {
      <quader-token-secret [value]="s.token" [description]="s.description" (closed)="secret.set(null)" />
    }

    @if (formOpen()) {
      <section class="card" aria-labelledby="token-form-heading">
        <h2 id="token-form-heading" i18n="@@tokens.form-heading">New token</h2>
        @if (formError(); as e) {
          <quader-error-notice [error]="e" />
        }
        <quader-token-form
          [submitLabel]="createLabel"
          [offerAdmin]="offerAdmin()"
          [busy]="busy()"
          [failure]="formError()"
          [restore]="restored()"
          (submitted)="create($event)"
          (draft)="lastDraft = $event"
          (cancelled)="closeForm()"
        />
      </section>
    }

    @if (actionError(); as e) {
      <quader-error-notice [error]="e" />
    }
    @if (list.error(); as e) {
      <quader-error-notice [error]="e" (retry)="list.reload()" />
    }
    @if (list.loaded()) {
      <quader-token-table
        [tokens]="list.items()"
        [emptyText]="emptyText()"
        [canRotate]="canRotate()"
        [canRevoke]="canRevoke()"
        [hasMore]="list.hasMore()"
        [loading]="list.loading()"
        (more)="list.more()"
        (revoke)="revoke($event)"
        (rotate)="rotate($event)"
      />
    } @else if (list.loading() && !list.error()) {
      <quader-loading i18n="@@tokens.loading">Loading tokens.</quader-loading>
    }

    @if (revokeAllHere() && offersRevokeAll()) {
      <quader-disclosure i18n-heading="@@tokens.advanced" heading="Advanced" i18n-hint="@@tokens.advanced-hint" hint="Revoke every token at once.">
        <p class="muted" i18n="@@tokens.revoke-all-note">Every token stops working at once, and the tools that use them are refused. This cannot be undone.</p>
        <p>
          <button quaderBusy class="btn btn-danger" type="button" (click)="revokeAllTokens()" i18n="@@tokens.revoke-all">Revoke all tokens</button>
        </p>
      </quader-disclosure>
    }
  `,
  styleUrl: '../../shared/access/access.scss',
})
export class TokenManager {
  readonly scope = input.required<TokenOwnerScope>();
  /** False when the page's header holds **Create token** (`offersCreate`, `openForm`). */
  readonly createInToolbar = input(true);
  /**
   * True: **Revoke all tokens** is in the manager's own closed section Advanced at its end (the profile). False: the page puts it
   * into its own section Advanced, last on the page, with `offersRevokeAll` and `revokeAllTokens` (a user, a service account).
   */
  readonly revokeAllHere = input(true);

  private readonly api = inject(TokensApi);
  protected readonly session = inject(SessionStore);
  private readonly confirmer = inject(Confirm);
  private readonly notifier = inject(Notifier);
  private readonly drafts = inject(DraftStore);
  private readonly dialog = inject(QuaderDialog);

  protected readonly list = new PagedResource<Token>();
  protected readonly showAll = signal(false);
  protected readonly formOpen = signal(false);
  protected readonly busy = signal(false);
  protected readonly formError = signal<ApiError | null>(null);
  protected readonly actionError = signal<ApiError | null>(null);
  protected readonly restored = signal<TokenDraft | null>(null);
  /** The value of a token that was just created; component-local, never stored (R-UI-043). */
  protected readonly secret = signal<TokenCreated | null>(null);
  protected readonly createLabel = $localize`:@@tokens.create-submit:Create token`;
  protected lastDraft: TokenDraft | null = null;

  private idempotencyKey = newIdempotencyKey();

  /** Administrator rights are offered to a caller who holds the role, for their own token (Q-UI-220). */
  protected readonly offerAdmin = computed(() => this.session.hasRole('admin') && this.scope().kind === 'own');

  protected readonly owner = computed(() => {
    const s = this.scope();
    return s.kind === 'own' ? 'me' : s.name;
  });
  /** Whether **Create token** is offered now: the caller may create and the form is closed. */
  protected readonly offersCreate = computed(() => this.canCreate() && !this.formOpen());
  /** Whether the form is open; a page whose header holds **Create token** hides it meanwhile. */
  readonly isFormOpen = this.formOpen.asReadonly();
  /** The empty list says what is missing; nothing is filtered but the revoked and expired tokens (UI-A-20, UI-A-25). */
  protected readonly emptyText = computed(() =>
    this.showAll()
      ? $localize`:@@tokens.empty-all:No tokens yet.`
      : $localize`:@@tokens.empty-active:No active tokens. Revoked and expired ones are shown with the switch above.`,
  );
  /** A personal token is created by its owner only; there is no impersonation (R-AUTH-027). */
  protected readonly canCreate = computed(() => this.scope().kind === 'own');
  protected readonly canRotate = computed(() => this.scope().kind === 'own');
  /** The owner, or `tokens:revoke`. */
  protected readonly canRevoke = computed(() => this.scope().kind === 'own' || this.session.has('tokens:revoke'));
  protected readonly canRevokeAll = computed(() => this.canRevoke());
  private readonly held = signal(false);
  /**
   * Whether a page opens its section Tokens (Q-UI-333): the list has held a token since the owner's tokens were first read. It
   * does not turn false again for the same owner, so revoking the last token, or a reload for the revoked and expired ones,
   * leaves the section open with its empty line instead of folding it away under the reader. A page binds it one way, so a
   * reader who closes the section keeps it closed. Set by effects only: a page reads it before `scope` is bound.
   */
  readonly heldTokens = this.held.asReadonly();
  /**
   * Whether **Revoke all tokens** is offered: the owner has a token in the list, and the caller may. The page reads it in its own
   * template, which can run before `scope` is bound (the section Advanced of a user without other actions, such as the
   * break-glass account): the list is checked first, and it holds a token only after it was read for a bound `scope`.
   */
  readonly offersRevokeAll = computed(() => this.list.items().length > 0 && this.canRevokeAll());

  constructor() {
    inject(DestroyRef).onDestroy(() => this.secret.set(null));
    effect(() => {
      this.owner();
      this.showAll();
      untracked(() => void this.reload());
    });
    // A token's last use and its expiry move without the owner: the tokens shown are read again every minute in place (R-UI-213).
    live({ reload: () => this.list.refresh(), every: () => LIVE_IDLE_MS });
    // Another owner on the same page starts closed again; its own tokens open the section.
    effect(() => {
      this.owner();
      this.held.set(false);
    });
    effect(() => {
      if (this.list.items().length > 0) this.held.set(true);
    });
    // A draft kept across re-authentication (R-UI-042) reopens the form.
    effect(() => {
      if (!this.canCreate()) return;
      untracked(() => {
        const draft = this.drafts.take<TokenDraft>();
        if (draft) {
          this.restored.set(draft);
          this.openForm();
          this.notifier.status($localize`:@@tokens.draft-restored:Your entries were restored. Create the token again.`);
        }
      });
    });
  }

  private reload(): Promise<void> {
    const state = this.showAll() ? 'all' : 'active';
    return this.list.start(this.api.list({ owner: this.owner(), state }));
  }

  protected setShowAll(on: boolean): void {
    this.showAll.set(on);
  }

  openForm(): void {
    this.idempotencyKey = newIdempotencyKey();
    this.formError.set(null);
    this.formOpen.set(true);
  }

  protected closeForm(): void {
    this.formOpen.set(false);
    this.restored.set(null);
  }

  protected async create(request: CreateTokenRequest): Promise<void> {
    this.busy.set(true);
    this.formError.set(null);
    try {
      const created = await this.createOnce(request);
      this.secret.set(created);
      this.closeForm();
      await this.reload();
    } catch (e) {
      const error = asApiError(e);
      this.formError.set(error);
      if (error.code === 'reauthentication_required' && this.lastDraft) this.drafts.keep(this.lastDraft);
    } finally {
      this.busy.set(false);
    }
  }

  private async createOnce(request: CreateTokenRequest): Promise<TokenCreated> {
    const send = (key: string): Promise<TokenCreated> => this.api.create(request, key);
    try {
      return await send(this.idempotencyKey);
    } catch (e) {
      // The first attempt created a token whose value was lost with its answer: revoke it,
      // create again with a new key, and show only the new one (R-UI-042).
      const lost = e instanceof ApiError && e.code === 'token_already_issued' ? e.problem?.['tokenId'] : null;
      if (typeof lost !== 'string') throw e;
      await this.api.revoke(lost);
      this.idempotencyKey = newIdempotencyKey();
      return send(this.idempotencyKey);
    }
  }

  protected async revoke(token: Token): Promise<void> {
    const answer = await this.confirmer.ask({
      title: $localize`:@@tokens.revoke-title:Revoke this token?`,
      lines: [$localize`:@@tokens.revoke-text:“${token.description}:description:” stops working at once on every replica. This cannot be undone.`],
      confirmLabel: $localize`:@@tokens.revoke-confirm:Revoke`,
    });
    if (answer === null) return;
    await this.act(async () => {
      await this.api.revoke(token.id);
      this.notifier.status($localize`:@@tokens.revoked:Token revoked.`);
    });
  }

  async revokeAllTokens(): Promise<void> {
    const answer = await this.confirmer.ask({
      title: $localize`:@@tokens.revoke-all-title:Revoke all tokens?`,
      lines: [$localize`:@@tokens.revoke-all-text:Every token of this owner stops working at once. This cannot be undone.`],
      confirmLabel: $localize`:@@tokens.revoke-all-confirm:Revoke all`,
    });
    if (answer === null) return;
    const s = this.scope();
    await this.act(async () => {
      if (s.kind === 'own') await this.api.revokeAllOwn();
      else await this.api.revokeAllOfUser(s.name);
      this.notifier.status($localize`:@@tokens.revoked-all:All tokens revoked.`);
    });
  }

  protected async rotate(token: Token): Promise<void> {
    const ref = this.dialog.open<RotateDialog, undefined, RotateChoice>(RotateDialog, { maxWidth: '32rem' });
    const choice = await firstValueFrom(ref.closed);
    if (!choice) return;
    const key = newIdempotencyKey();
    await this.act(async () => {
      this.secret.set(await this.api.rotate(token.id, choice.overlapSeconds, key));
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
