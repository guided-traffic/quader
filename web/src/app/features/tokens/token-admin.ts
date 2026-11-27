import { Component, computed, inject, signal } from '@angular/core';
import { Checkbox } from '../../shared/ui/checkbox';
import { ApiError } from '../../api/problem';
import { Token, TokenFilters, TokensApi } from '../../api/tokens-api';
import { Notifier } from '../../core/notifier';
import { SessionStore } from '../../core/session-store';
import { Confirm } from '../../shared/confirm-dialog';
import { SelectField, SelectOption, TextField } from '../../shared/access/fields';
import { Needs } from '../../shared/access/needs';
import { PagedResource, asApiError } from '../../shared/paged-resource';
import { TokenTable, tokenName } from '../../shared/access/token-table';
import { ErrorNotice } from '../../shared/error-notice';
import { FilterPanel } from '../../shared/ui/filter-panel';
import { ListChip, ListHead } from '../../shared/ui/list-head';
import { PageHeader } from '../../shared/ui/page-header';
import { PrincipalField, choiceFromRef } from '../../shared/ui/principal-field';
import { Loading } from '../../shared/ui/loading';
import { Busy } from '../../shared/ui/busy';

/**
 * Tokens, the token administration (R-UI-030): every token with filters (type, owner, state, expiring before,
 * unused for N days), revoke one or the selected ones with confirmation, and "Revoke by value" for
 * a leaked token. The pasted value is sent once and cleared; it lives in this component's signal
 * and in no store (R-UI-043). Issuances of CI pipelines (`/api/v1/ci/issuances`) belong to the CI
 * work package and are not shown here.
 */
@Component({
  selector: 'quader-token-admin',
  imports: [Busy, Loading, 
    PageHeader,
    Checkbox,
    ErrorNotice,
    FilterPanel,
    ListHead,
    Needs,
    PrincipalField,
    SelectField,
    TextField,
    TokenTable,
  ],
  templateUrl: './token-admin.html',
  styleUrl: '../../shared/access/access.scss',
})
export class TokenAdmin {
  private readonly api = inject(TokensApi);
  private readonly confirmer = inject(Confirm);
  private readonly notifier = inject(Notifier);
  protected readonly session = inject(SessionStore);

  protected readonly list = new PagedResource<Token>();
  /** The owner to filter by, chosen from the shared field (R-UI-150): a user's or a service account's reference. */
  protected readonly owner = signal('');
  /** Tokens belong to users and service accounts, of the instance and of every project. */
  protected readonly ownerKinds = ['user', 'service_account'] as const;
  protected readonly type = signal('');
  protected readonly state = signal('active');
  protected readonly expiresBefore = signal('');
  protected readonly unusedDays = signal('');
  protected readonly selected = signal<readonly string[]>([]);
  protected readonly panelOpen = signal(false);
  /** The drawer's values the list shows now, as they stood when they were applied. */
  private readonly applied = signal({ owner: '', type: '', state: 'active', expiresBefore: '', unusedDays: '' });
  /** The filters that are set, as the head's chips (Q-UI-422); the default state "active" is not one. */
  protected readonly chips = computed<readonly ListChip[]>(() => {
    const a = this.applied();
    const out: ListChip[] = [];
    const named = (options: readonly SelectOption[], value: string): string => options.find((o) => o.value === value)?.label ?? value;
    if (a.owner) out.push({ key: 'owner', label: $localize`:@@tokens-admin.owner:Owner`, value: choiceFromRef(a.owner).name });
    if (a.type) out.push({ key: 'type', label: $localize`:@@tokens-admin.type:Type`, value: named(this.typeOptions, a.type) });
    if (a.state !== 'active') out.push({ key: 'state', label: $localize`:@@tokens-admin.state:State`, value: named(this.stateOptions, a.state) });
    if (a.expiresBefore) out.push({ key: 'expiresBefore', label: $localize`:@@tokens-admin.expires-before:Expires before`, value: a.expiresBefore });
    const days = Number.parseInt(a.unusedDays, 10);
    if (Number.isFinite(days) && days > 0) out.push({ key: 'unusedDays', label: $localize`:@@tokens-admin.unused:Not used for (days)`, value: String(days) });
    return out;
  });
  /** An unfiltered list says that no active token exists yet; "No token matches." is for a filter (UI-A-25, Q-UI-565). */
  protected readonly noActive = $localize`:@@tokens-admin.empty:There is no active token yet.`;
  protected readonly noMatch = $localize`:@@token-table.empty:No token matches.`;
  protected readonly actionError = signal<ApiError | null>(null);

  protected readonly pasted = signal('');
  protected readonly reveal = signal(false);
  protected readonly valueError = signal<ApiError | null>(null);
  protected readonly busy = signal(false);

  protected readonly typeOptions: readonly SelectOption[] = [
    { value: '', label: $localize`:@@tokens-admin.any:Any` },
    { value: 'pat', label: $localize`:@@tokens-admin.type.pat:Personal (pat)` },
    { value: 'sat', label: $localize`:@@tokens-admin.type.sat:Service account (sat)` },
    { value: 'cit', label: $localize`:@@tokens-admin.type.cit:Pipeline (cit)` },
  ];
  protected readonly stateOptions: readonly SelectOption[] = [
    { value: 'active', label: $localize`:@@tokens-admin.state.active:Active` },
    { value: 'expired', label: $localize`:@@tokens-admin.state.expired:Expired` },
    { value: 'revoked', label: $localize`:@@tokens-admin.state.revoked:Revoked` },
    { value: 'all', label: $localize`:@@tokens-admin.state.all:All` },
  ];

  constructor() {
    void this.apply();
  }

  /** Starts a new traversal with the current filters (a cursor is bound to its filters). */
  protected async apply(): Promise<void> {
    this.selected.set([]);
    const days = Number.parseInt(this.unusedDays(), 10);
    const filters: TokenFilters = {
      owner: ownerParam(this.owner()),
      type: (this.type() || undefined) as TokenFilters['type'],
      state: this.state() as TokenFilters['state'],
      expiresBefore: this.expiresBefore() ? `${this.expiresBefore()}T23:59:59Z` : undefined,
      lastUsedBefore:
        Number.isFinite(days) && days > 0 ? new Date(Date.now() - days * 86_400_000).toISOString() : undefined,
    };
    this.applied.set({ owner: this.owner(), type: this.type(), state: this.state(), expiresBefore: this.expiresBefore(), unusedDays: this.unusedDays() });
    await this.list.start(this.api.list(filters));
  }

  /** The drawer's fields back to the filters the list shows: it closed without **Apply**. */
  protected restage(): void {
    const a = this.applied();
    this.owner.set(a.owner);
    this.type.set(a.type);
    this.state.set(a.state);
    this.expiresBefore.set(a.expiresBefore);
    this.unusedDays.set(a.unusedDays);
  }

  protected removeChip(chip: ListChip): void {
    this.restage();
    const field = { owner: this.owner, type: this.type, expiresBefore: this.expiresBefore, unusedDays: this.unusedDays }[chip.key];
    if (chip.key === 'state') this.state.set('active');
    else field?.set('');
    void this.apply();
  }

  protected clear(): void {
    this.owner.set('');
    this.type.set('');
    this.state.set('active');
    this.expiresBefore.set('');
    this.unusedDays.set('');
    void this.apply();
  }

  protected async revokeOne(token: Token): Promise<void> {
    const description = tokenName(token);
    const answer = await this.confirmer.ask({
      title: $localize`:@@tokens-admin.revoke-title:Revoke this token?`,
      lines: [$localize`:@@tokens-admin.revoke-text:“${description}:description:” of ${token.owner.name}:owner: stops working at once on every replica.`],
      confirmLabel: $localize`:@@tokens-admin.revoke-confirm:Revoke`,
    });
    if (answer !== null) await this.revoke([token.id]);
  }

  protected async revokeSelected(): Promise<void> {
    const ids = this.selected();
    if (ids.length === 0) return;
    const answer = await this.confirmer.ask({
      title: $localize`:@@tokens-admin.revoke-selected-title:Revoke the selected tokens?`,
      lines: [$localize`:@@tokens-admin.revoke-selected-text:The selected tokens (${ids.length}:count:) stop working at once on every replica. This cannot be undone.`],
      confirmLabel: $localize`:@@tokens-admin.revoke-confirm:Revoke`,
    });
    if (answer !== null) await this.revoke(ids);
  }

  private async revoke(ids: readonly string[]): Promise<void> {
    this.actionError.set(null);
    const failed: string[] = [];
    for (const id of ids) {
      try {
        await this.api.revoke(id);
      } catch (e) {
        failed.push(id);
        this.actionError.set(asApiError(e));
      }
    }
    if (failed.length < ids.length) this.notifier.status($localize`:@@tokens-admin.revoked:Tokens revoked.`);
    await this.apply();
  }

  protected async revokeByValue(event: Event): Promise<void> {
    event.preventDefault();
    const value = this.pasted().trim();
    if (!value) return;
    this.busy.set(true);
    this.valueError.set(null);
    try {
      const revoked = await this.api.revokeByValue(value);
      // Only the administrator's 200 names a token; the empty 202 says nothing about the value (Q-AUTH-6).
      this.notifier.status(
        revoked
          ? $localize`:@@tokens-admin.value-revoked:The token of ${revoked.owner}:owner: was revoked.`
          : $localize`:@@tokens-admin.value-accepted:The request was accepted. If the value is a token, it is revoked; this answer does not say whether it was one.`,
      );
      await this.apply();
    } catch (e) {
      this.valueError.set(asApiError(e));
    } finally {
      // The value was sent once; it is gone from the form whatever the answer was (R-UI-043).
      this.pasted.set('');
      this.reveal.set(false);
      this.busy.set(false);
    }
  }
}

/** The API names a token's owner by the username, or by `sa:<name>` for a service account; the field holds a reference. */
function ownerParam(ref: string): string | undefined {
  if (!ref) return undefined;
  const chosen = choiceFromRef(ref);
  return chosen.kind === 'user' ? chosen.name : ref;
}
