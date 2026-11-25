import { ChangeDetectionStrategy, Component, Injectable, LOCALE_ID, computed, inject, input } from '@angular/core';
import { firstValueFrom } from 'rxjs';
import { LimitPlace, LimitProfile, LimitRule, LimitsApi, UsageRow, UsageScope } from '../../api/limits-api';
import { Versioned } from '../../api/repositories-api';
import { TechnicalToken } from '../../api/technical-tokens-api';
import { Confirm } from '../../shared/confirm-dialog';
import { passes } from '../../shared/error-notice';
import { asApiError } from '../../shared/paged-resource';
import { QuaderDialog } from '../../shared/ui/dialog';
import { formatBytes, formatCount } from './figures';
import { LimitRuleDialog, RuleDialogData } from './rule-dialog';

/*
 * The limit and the last 24 hours of a technical token, for the section **Technical tokens** of a service account's page
 * (R-UI-181, Q-UI-734, Q-UI-759, Q-UI-761). The token list of the service accounts' feature places both in its column **Limit**:
 * the limit on the row's line, the last 24 hours as the cell's second line; the limit opens `TechnicalTokenLimits.edit`:
 * **Set limit**, **Change limit** and in it **Remove limit**. The token's own rule is the `limit` member of the technical token
 * (R-AUTH-045); nothing here knows the page.
 */

/** The token's own rule as the token API carries it (R-AUTH-045): the rule's ID, its profile and how many figures it changes. */
export type TokenLimit = NonNullable<TechnicalToken['limit']>;

/** The place of an account's rules (R-LIM-038): the instance, its project, or its repository (`<project>/<repo>`). */
export function accountPlace(account: { readonly project?: string | null; readonly repository?: string | null }): LimitPlace {
  if (account.project && account.repository) return { kind: 'repository', repository: `${account.project}/${account.repository}` };
  if (account.project) return { kind: 'project', project: account.project };
  return { kind: 'instance' };
}

/** "Restricted", "Restricted + 2 changes", "Default (Technical)" (R-UI-181). */
export function tokenLimitText(limit: TokenLimit | null, profiles: readonly LimitProfile[], defaultProfile: string): string {
  if (!limit) return $localize`:@@limits.token.default:Default (${defaultProfile}:profile:)`;
  const name = profiles.find((p) => p.key === limit.profile)?.name ?? limit.profile;
  const n = limit.overrides;
  if (n === 0) return name;
  return n === 1
    ? $localize`:@@limits.projects.one-change:${name}:profile: + 1 change`
    : $localize`:@@limits.projects.changes:${name}:profile: + ${n}:count: changes`;
}

/**
 * "1,204 requests · 3.2 GB in 24 h": the requests and the bytes from the cache and from the upstream together, "Not used in 24 h"
 * for none (R-UI-181, Q-UI-761).
 */
export function lastDayText(row: Pick<UsageRow, 'requests' | 'cachedBytes' | 'uncachedBytes'> | null, locale: string): string {
  if (!row || row.requests === 0) return $localize`:@@limits.token.no-use-day:Not used in 24 h`;
  const requests =
    row.requests === 1
      ? $localize`:@@limits.token.one-request:1 request`
      : $localize`:@@limits.token.requests:${formatCount(row.requests, locale)}:count: requests`;
  const bytes = formatBytes(row.cachedBytes + row.uncachedBytes, locale);
  return $localize`:@@limits.token.last-day:${requests}:requests: · ${bytes}:bytes: in 24 h`;
}

/** The cell **Limit** of a token's row. */
@Component({
  selector: 'quader-token-limit',
  changeDetection: ChangeDetectionStrategy.OnPush,
  // The profile and its changes break between each other, never inside ("Default (Technical)", "+ 1 change").
  template: `@for (part of parts(); track $index) {@if ($index > 0) {{{ ' ' }}}<span class="part">{{ part }}</span>}`,
  styles: `
    .part {
      white-space: nowrap;
    }
  `,
})
export class TokenLimitCell {
  readonly limit = input<TokenLimit | null>(null);
  readonly profiles = input<readonly LimitProfile[]>([]);
  /** The profile a token without a rule gets: Technical, or Unlimited for an account that holds `admin` (R-LIM-038). */
  readonly defaultProfile = input('Technical');
  protected readonly parts = computed(() => {
    const [name, ...rest] = tokenLimitText(this.limit(), this.profiles(), this.defaultProfile()).split(' + ');
    return rest.length ? [name ?? '', `+ ${rest.join(' + ')}`] : [name ?? ''];
  });
}

/** The second line of a token's **Limit**: its last 24 hours. */
@Component({
  selector: 'quader-token-last-day',
  changeDetection: ChangeDetectionStrategy.OnPush,
  // The requests and the bytes break between each other, never inside a number and its unit; in a list of 50 rem and more (the
  // service account's page at 1440 px) the line does not break at all, and at 1024 px it may (Q-UI-761).
  template: `@for (part of parts(); track $index) {@if ($index > 0) {{{ ' · ' }}}<span class="part">{{ part }}</span>}`,
  styles: `
    .part {
      white-space: nowrap;
    }
    @container table (min-width: 50rem) {
      :host {
        white-space: nowrap;
      }
    }
  `,
})
export class TokenLastDayCell {
  private readonly locale = inject(LOCALE_ID);
  readonly usage = input<Pick<UsageRow, 'requests' | 'cachedBytes' | 'uncachedBytes'> | null>(null);
  protected readonly parts = computed(() => lastDayText(this.usage(), this.locale).split(' · '));
}

/** The reads and the actions behind the two cells. */
@Injectable({ providedIn: 'root' })
export class TechnicalTokenLimits {
  private readonly api = inject(LimitsApi);
  private readonly dialog = inject(QuaderDialog);
  private readonly confirmer = inject(Confirm);

  /**
   * The profiles, for the names in **Limit**; a caller who may not read them gets none, and the cell names the profile's key. A
   * failure that may pass (no answer, a gateway's, a server's) is thrown, so the page's notice shows it and its Retry reads the
   * profiles again (R-UI-216): taken for a refusal, the keys stood in place of the names until the page was opened again.
   */
  async profiles(): Promise<readonly LimitProfile[]> {
    try {
      return await this.api.profiles();
    } catch (e) {
      if (passes(asApiError(e))) throw e;
      return [];
    }
  }

  /**
   * The last 24 hours of the technical tokens in a scope, by the token's ID (the overview's rows of the kind, R-LIM-034). A caller
   * who may not read the usage gets null, and the column is not shown.
   */
  async lastDay(scope: UsageScope): Promise<ReadonlyMap<string, UsageRow> | null> {
    try {
      const out = new Map<string, UsageRow>();
      let cursor: string | null = null;
      do {
        const page = await this.api.usage(scope, { period: 'day', kind: ['technicalToken'] }, cursor, 200);
        for (const row of page.items) out.set(row.subject.id, row);
        cursor = page.nextCursor;
      } while (cursor);
      return out;
    } catch (e) {
      // A refusal hides the column; a failure that may pass is the page's, which reads it again (R-UI-216).
      if (passes(asApiError(e))) throw e;
      return null;
    }
  }

  /**
   * **Set limit** or **Change limit**: the rule dialog with the token fixed and the account's place (R-UI-181); a token's own rule
   * offers **Remove limit** in it (Q-UI-759). The answer says what changed: the rule, `'removed'`, or null for nothing.
   */
  async edit(place: LimitPlace, token: TechnicalToken, capped: boolean): Promise<Versioned<LimitRule> | 'removed' | null> {
    const rule = token.limit ? (await this.api.rule(place, token.limit.rule)).value : null;
    const data: RuleDialogData = rule
      ? { place, rule, capped, tokenRule: true }
      : { place, who: { kind: 'technicalToken', technicalToken: token.id, name: token.name, account: token.account.reference }, capped };
    const ref = this.dialog.open<LimitRuleDialog, RuleDialogData, Versioned<LimitRule> | 'remove'>(LimitRuleDialog, {
      data,
      maxWidth: '44rem',
      width: '44rem',
    });
    const answer = (await firstValueFrom(ref.closed)) ?? null;
    if (answer === 'remove') return (await this.remove(place, token)) ? 'removed' : null;
    return answer;
  }

  /** **Remove limit**: after a confirmation, the token gets its kind's default again. */
  async remove(place: LimitPlace, token: TechnicalToken): Promise<boolean> {
    if (!token.limit) return false;
    const answer = await this.confirmer.ask({
      title: $localize`:@@limits.token.remove-title:Remove the limit of ${token.name}:name:?`,
      lines: [$localize`:@@limits.token.remove-text:The token gets the default limit of technical tokens from the next request on.`],
      confirmLabel: $localize`:@@limits.token.remove-confirm:Remove limit`,
    });
    if (answer === null) return false;
    const stored = await this.api.rule(place, token.limit.rule);
    await this.api.deleteRule(place, token.limit.rule, stored.etag);
    return true;
  }
}
