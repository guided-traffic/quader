import { ChangeDetectionStrategy, Component, Injectable, LOCALE_ID, computed, inject, input } from '@angular/core';
import { firstValueFrom } from 'rxjs';
import { LimitPlace, LimitProfile, LimitRule, LimitsApi, UsageRow, UsageScope } from '../../api/limits-api';
import { Versioned } from '../../api/repositories-api';
import { Confirm } from '../../shared/confirm-dialog';
import { QuaderDialog } from '../../shared/ui/dialog';
import { formatBytes, formatCount } from './figures';
import { LimitRuleDialog, RuleDialogData } from './rule-dialog';

/*
 * The limit and the last 24 hours of a technical token, for the section **Technical tokens** of a service account's page
 * (R-UI-181, Q-UI-734). The page belongs to the service accounts' feature; it places the two cells in its token list as the
 * columns **Limit** and **Last 24 hours** and calls `TechnicalTokenLimits` for **Set limit**, **Change limit** and **Remove
 * limit**. Nothing here knows the page.
 */

/** A technical token as the limit needs it: its ID and its name. */
export interface TechnicalTokenRef {
  readonly id: string;
  readonly name: string;
  readonly account: string;
}

/** The place of an account's rules (R-LIM-038): the instance, its project, or its repository (`<project>/<repo>`). */
export function accountPlace(account: { readonly project?: string | null; readonly repository?: string | null }): LimitPlace {
  if (account.project && account.repository) return { kind: 'repository', repository: `${account.project}/${account.repository}` };
  if (account.project) return { kind: 'project', project: account.project };
  return { kind: 'instance' };
}

/** "Restricted", "Restricted + 2 changes", "Default (Technical)" (R-UI-181). */
export function tokenLimitText(rule: LimitRule | null, profiles: readonly LimitProfile[], defaultProfile: string): string {
  if (!rule) return $localize`:@@limits.token.default:Default (${defaultProfile}:profile:)`;
  const name = profiles.find((p) => p.key === rule.profile)?.name ?? rule.profile;
  const n = Object.keys(rule.overrides).length;
  if (n === 0) return name;
  return n === 1
    ? $localize`:@@limits.projects.one-change:${name}:profile: + 1 change`
    : $localize`:@@limits.projects.changes:${name}:profile: + ${n}:count: changes`;
}

/** "1,204 requests · 3.2 GB": the requests and the bytes from the cache and from the upstream together (R-UI-181). */
export function lastDayText(row: Pick<UsageRow, 'requests' | 'cachedBytes' | 'uncachedBytes'> | null, locale: string): string {
  if (!row || row.requests === 0) return $localize`:@@limits.token.no-use:Not used`;
  const requests =
    row.requests === 1
      ? $localize`:@@limits.token.one-request:1 request`
      : $localize`:@@limits.token.requests:${formatCount(row.requests, locale)}:count: requests`;
  return `${requests} · ${formatBytes(row.cachedBytes + row.uncachedBytes, locale)}`;
}

/** The cell **Limit** of a token's row. */
@Component({
  selector: 'quader-token-limit',
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `{{ text() }}`,
})
export class TokenLimitCell {
  readonly rule = input<LimitRule | null>(null);
  readonly profiles = input<readonly LimitProfile[]>([]);
  /** The profile a token without a rule gets: Technical, or Unlimited for an account that holds `admin` (R-LIM-038). */
  readonly defaultProfile = input('Technical');
  protected readonly text = computed(() => tokenLimitText(this.rule(), this.profiles(), this.defaultProfile()));
}

/** The cell **Last 24 hours** of a token's row. */
@Component({
  selector: 'quader-token-last-day',
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `{{ text() }}`,
})
export class TokenLastDayCell {
  private readonly locale = inject(LOCALE_ID);
  readonly usage = input<Pick<UsageRow, 'requests' | 'cachedBytes' | 'uncachedBytes'> | null>(null);
  protected readonly text = computed(() => lastDayText(this.usage(), this.locale));
}

/** The reads and the actions behind the two cells. */
@Injectable({ providedIn: 'root' })
export class TechnicalTokenLimits {
  private readonly api = inject(LimitsApi);
  private readonly dialog = inject(QuaderDialog);
  private readonly confirmer = inject(Confirm);

  /** The rules of the account's place that name a single technical token, by the token's ID. */
  async rules(place: LimitPlace): Promise<ReadonlyMap<string, LimitRule>> {
    const rules = await this.api.rules(place);
    return new Map(rules.flatMap((r) => (r.who.kind === 'technicalToken' ? [[r.who.technicalToken, r] as const] : [])));
  }

  /**
   * The last 24 hours of the technical tokens in a scope, by the token's ID (the overview's rows of the kind, R-LIM-034). A caller
   * who may not read the usage gets an empty map, and the column shows nothing.
   */
  async lastDay(scope: UsageScope): Promise<ReadonlyMap<string, UsageRow>> {
    try {
      const out = new Map<string, UsageRow>();
      let cursor: string | null = null;
      do {
        const page = await this.api.usage(scope, { period: 'day', kind: ['technicalToken'] }, cursor, 200);
        for (const row of page.items) out.set(row.subject.id, row);
        cursor = page.next ?? page.nextCursor ?? null;
      } while (cursor);
      return out;
    } catch {
      return new Map();
    }
  }

  /** **Set limit** or **Change limit**: the rule dialog with the token fixed and the account's place (R-UI-181). */
  async edit(place: LimitPlace, token: TechnicalTokenRef, rule: LimitRule | null, capped: boolean): Promise<Versioned<LimitRule> | null> {
    const data: RuleDialogData = rule
      ? { place, rule, capped }
      : { place, who: { kind: 'technicalToken', technicalToken: token.id, name: token.name, account: token.account }, capped };
    const ref = this.dialog.open<LimitRuleDialog, RuleDialogData, Versioned<LimitRule>>(LimitRuleDialog, {
      data,
      maxWidth: '44rem',
      width: '44rem',
    });
    return (await firstValueFrom(ref.closed)) ?? null;
  }

  /** **Remove limit**: after a confirmation, the token gets its kind's default again. */
  async remove(place: LimitPlace, token: TechnicalTokenRef, rule: LimitRule): Promise<boolean> {
    const answer = await this.confirmer.ask({
      title: $localize`:@@limits.token.remove-title:Remove the limit of ${token.name}:name:?`,
      lines: [$localize`:@@limits.token.remove-text:The token gets the default limit of technical tokens from the next request on.`],
      confirmLabel: $localize`:@@limits.token.remove-confirm:Remove limit`,
    });
    if (answer === null) return false;
    const stored = await this.api.rule(place, rule.id);
    await this.api.deleteRule(place, rule.id, stored.etag);
    return true;
  }
}
