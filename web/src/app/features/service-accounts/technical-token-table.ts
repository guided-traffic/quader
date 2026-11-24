import { CdkMenu, CdkMenuItem, CdkMenuTrigger } from '@angular/cdk/menu';
import { Component, computed, input, output } from '@angular/core';
import { LimitProfile, UsageRow } from '../../api/limits-api';
import { TechnicalToken } from '../../api/technical-tokens-api';
import { TokenLastDayCell, TokenLimitCell } from '../limits/technical-token-limit';
import { DataTable, TableColumn } from '../../shared/ui/data-table';
import { TimeAgo } from '../../shared/ui/time-ago';
import { Tooltip } from '../../shared/ui/tooltip';
import { EXPIRING_SOON_DAYS } from '../../shared/access/token-table';

/** The newest secret of a technical token: the live one, or the last one of a token without a live secret (R-AUTH-045). */
export function currentSecret(t: TechnicalToken): TechnicalToken['secrets'][number] | null {
  return t.secrets[0] ?? null;
}

/** The last use of any shown secret of the token, or null when none was used. */
export function lastUsed(t: TechnicalToken): string | null {
  let out: string | null = null;
  for (const s of t.secrets) {
    if (s.lastUsedAt && (out === null || s.lastUsedAt > out)) out = s.lastUsedAt;
  }
  return out;
}

/** What the table shows of the download limits (R-UI-181): the profiles' names, the default, the last 24 hours, who may set. */
export interface TokenLimitsView {
  readonly profiles: readonly LimitProfile[];
  /** The profile a token without a rule of its own gets: Technical, Unlimited for an account that holds `admin`. */
  readonly defaultProfile: string;
  /** The last 24 hours by the token's ID; null for a caller who may not read the usage (the limit stands without its second line). */
  readonly lastDay: ReadonlyMap<string, UsageRow> | null;
  /** The caller may set the token's limit: `limits:write`, or an Owner of the account's project. */
  readonly canSet: boolean;
}

/**
 * A list of technical tokens of a service account (R-UI-181, Q-UI-740): the token (its name, below it the hint of its current secret
 * with when it expires, and its description), its limit with its last 24 hours as the cell's second line (Q-UI-759, Q-UI-761), when
 * it was last used, and at the row's end **Rotate** and **Revoke**, quiet buttons with their words, for an active token; a token
 * without a live secret says **Expired** or **Revoked**, shows no limit and offers **Issue new secret**. The limit opens the rule
 * dialog where the caller may set it (**Set limit**, **Change limit**, **Remove limit**). The section lists the active tokens and the
 * ended ones in a table each, so that the action cells of one table are built one way (R-UI-163, R-UI-176).
 */
@Component({
  selector: 'quader-technical-token-table',
  imports: [CdkMenu, CdkMenuItem, CdkMenuTrigger, DataTable, TimeAgo, TokenLastDayCell, TokenLimitCell, Tooltip],
  template: `
    <quader-data-table [caption]="caption()" [columns]="columns()" [empty]="tokens().length === 0" [emptyText]="emptyText()">
      @for (t of tokens(); track t.id) {
        <tr>
          <th scope="row" class="wrap-name">
            <span translate="no">{{ t.name }}</span>
            @if (secretOf(t); as s) {
              <!-- An active token's secret with its expiry on one line (Q-UI-761): the hint names the secret that expires. -->
              <span class="secondary"
                ><code translate="no">{{ s.hint }}</code>
                @if (t.state === 'active') {
                  @if (s.expiresAt) {
                    · <span class="expiry" i18n="@@tt-table.expires-line">expires <quader-time-ago [at]="s.expiresAt" absolute="title" /></span>
                  } @else {
                    · <span class="expiry" i18n="@@tt-table.never-expires">never expires</span>
                  }
                }
              </span>
              @if (t.state === 'active' && soon(t)) {
                <span class="badge warn"
                  ><span class="material-icons" aria-hidden="true">schedule</span
                  ><ng-container i18n="@@tt-table.soon">Expires soon</ng-container></span
                >
              }
            }
            @if (t.description) {
              <span class="secondary">{{ t.description }}</span>
            }
          </th>
          @if (limits(); as l) {
            <td>
              @if (l.canSet) {
                <button class="open link" type="button" (click)="setLimit.emit(t)">
                  <quader-token-limit [limit]="t.limit" [profiles]="l.profiles" [defaultProfile]="l.defaultProfile" />
                  <span class="visually-hidden">, {{ t.limit ? changeWord : setWord }} {{ t.name }}</span>
                </button>
              } @else {
                <quader-token-limit [limit]="t.limit" [profiles]="l.profiles" [defaultProfile]="l.defaultProfile" />
              }
              @if (l.lastDay; as days) {
                <quader-token-last-day class="secondary" [usage]="days.get(t.id) ?? null" />
              }
            </td>
          }
          <!-- A time relative to now; the absolute time is its title (Q-UI-431). -->
          @if (t.state !== 'active') {
            <td>
              <!-- An ended token: when its last secret ended, below it how, so that the cell's first line is on the row's line
                   (R-UI-172). -->
              @if (endOf(t); as at) {
                <quader-time-ago [at]="at" absolute="title" /><br />
              }
              @if (t.state === 'expired') {
                <span class="warn"><span class="material-icons" aria-hidden="true">history</span> <ng-container i18n="@@tt-table.expired">Expired</ng-container></span>
              } @else {
                <span class="bad"><span class="material-icons" aria-hidden="true">block</span> <ng-container i18n="@@tt-table.revoked">Revoked</ng-container></span>
              }
            </td>
          }
          <td>
            @if (used(t); as at) {
              <quader-time-ago [at]="at" absolute="title" />
            } @else {
              <span class="muted no-time" i18n="@@tt-table.not-used">Not used yet</span>
            }
          </td>
          <td class="actions">
            @if (t.state === 'active') {
              @if (canRotate()) {
                <button class="btn btn-tertiary" type="button" [quaderTooltip]="rotateText" (click)="rotate.emit(t)">
                  <ng-container i18n="@@tt-table.rotate">Rotate</ng-container>
                  <span class="visually-hidden"> {{ t.name }}</span>
                </button>
              }
              @if (canRevoke()) {
                <button class="btn btn-tertiary" type="button" (click)="revoke.emit(t)">
                  <ng-container i18n="@@tt-table.revoke">Revoke</ng-container>
                  <span class="visually-hidden"> {{ t.name }}</span>
                </button>
              }
            } @else {
              @if (canIssue()) {
                <button class="btn btn-tertiary" type="button" (click)="renew.emit(t)">
                  <ng-container i18n="@@tt-table.renew">Issue new secret</ng-container>
                  <span class="visually-hidden"> {{ t.name }}</span>
                </button>
              }
            }
            <!-- The token's own changes, which no row offered (G5-51): its description and its removal, in the row's menu beside the
                 words of its secret, which a credential keeps as words (Q-UI-641, Q-UI-1007). -->
            @if (canManage()) {
              <button
                class="btn btn-tertiary btn-icon on-line"
                type="button"
                [attr.aria-label]="menuLabel(t)"
                [cdkMenuTriggerFor]="tokenMenu"
                [cdkMenuTriggerData]="{ $implicit: t }"
              >
                <span class="material-icons" aria-hidden="true">more_vert</span>
              </button>
            }
          </td>
        </tr>
      }
    </quader-data-table>
    <ng-template #tokenMenu let-t>
      <div class="menu" cdkMenu>
        <button class="menu-item" type="button" cdkMenuItem (cdkMenuItemTriggered)="editDescription.emit(t)" i18n="@@tt-table.edit-description">
          Edit description
        </button>
        <button class="menu-item" type="button" cdkMenuItem (cdkMenuItemTriggered)="remove.emit(t)" i18n="@@tt-table.delete">Delete</button>
      </div>
    </ng-template>
  `,
  styleUrl: '../../shared/access/access.scss',
  styles: `
    /* The expiry breaks from the hint, never inside ("expires" / "in 4 weeks" read as two facts). */
    .expiry {
      white-space: nowrap;
    }
    /* The limit opens its rule dialog: the thing the row sets is its own link (R-UI-163, Q-UI-758). */
    .open {
      padding: 0;
      border: 0;
      background: none;
      color: var(--quader-link);
      font: inherit;
      text-align: start;
      cursor: pointer;
    }
    .open:hover {
      text-decoration: underline;
    }
    .open:focus-visible {
      outline: var(--quader-focus-ring);
      outline-offset: 2px;
    }
  `,
})
export class TechnicalTokenTable {
  readonly tokens = input.required<readonly TechnicalToken[]>();
  readonly caption = input($localize`:@@tt-table.caption:Technical tokens`);
  readonly emptyText = input($localize`:@@tt-table.empty:This account has no technical token yet.`);
  /** The tokens without a live secret: the time column says when and how each ended. */
  readonly ended = input(false);
  readonly canRotate = input(false);
  readonly canRevoke = input(false);
  /** The token's description and its removal (`identities:write`, or an Owner of the account's project). */
  readonly canManage = input(false);
  readonly editDescription = output<TechnicalToken>();
  readonly remove = output<TechnicalToken>();
  protected menuLabel(t: TechnicalToken): string {
    return $localize`:@@tt-table.actions-for:Actions for ${t.name}:name:`;
  }
  readonly canIssue = input(false);
  /** The download limits' columns and actions; null leaves them out (a caller who reads neither limits nor usage). */
  readonly limits = input<TokenLimitsView | null>(null);
  readonly rotate = output<TechnicalToken>();
  /** The limit's cell opens the rule dialog: **Set limit**, **Change limit**, and in it **Remove limit** (R-UI-181, Q-UI-759). */
  readonly setLimit = output<TechnicalToken>();
  readonly revoke = output<TechnicalToken>();
  /** **Issue new secret** of a token without a live secret (Q-AUTH-416). */
  readonly renew = output<TechnicalToken>();

  protected readonly setWord = $localize`:@@tt-table.set-limit:Set limit`;
  protected readonly changeWord = $localize`:@@tt-table.change-limit:Change limit`;
  protected readonly rotateText = $localize`:@@tt-table.rotate-tip:Rotate: a new secret now, the old one ends after the overlap you choose. The token keeps its name and its limit.`;
  /**
   * The token wraps and declares its least width (Q-UI-308); the times and the actions are as wide as their content. An active
   * token's expiry stands beside its secret's hint, so that the list is a table at 1,024 px (Q-UI-761).
   */
  protected readonly columns = computed<readonly TableColumn[]>(() => [
    { id: 'token', label: $localize`:@@tt-table.token:Token`, width: '10rem', prefer: true },
    ...(this.limits()
      ? [
          {
            id: 'limit',
            label: $localize`:@@tt-table.limit:Limit`,
            help: this.limits()?.lastDay
              ? $localize`:@@tt-table.help.limit-day:The download limit of this token: its own rule at the account's place, or the default of technical tokens. Below it, what the token downloaded in the last 24 hours.`
              : $localize`:@@tt-table.help.limit:The download limit of this token: its own rule at the account's place, or the default of technical tokens.`,
          },
        ]
      : []),
    ...(this.ended() ? [{ id: 'ended', label: $localize`:@@tt-table.ended:Ended` }] : []),
    {
      id: 'lastUsed',
      label: $localize`:@@tt-table.last-used:Last used`,
      help: $localize`:@@tt-table.help.last-used:The last time a secret of the token authenticated a request. Not every use is recorded immediately.`,
    },
    { id: 'actions', label: $localize`:@@tt-table.actions:Actions`, hideLabel: true },
  ]);

  private readonly now = computed(() => {
    this.tokens();
    return Date.now();
  });

  protected secretOf(t: TechnicalToken): TechnicalToken['secrets'][number] | null {
    return currentSecret(t);
  }

  /** When the last secret of an ended token ended: its revocation, else its expiry. */
  protected endOf(t: TechnicalToken): string | null {
    const s = currentSecret(t);
    return s?.revokedAt ?? s?.expiresAt ?? null;
  }

  protected used(t: TechnicalToken): string | null {
    return lastUsed(t);
  }

  protected soon(t: TechnicalToken): boolean {
    const at = currentSecret(t)?.expiresAt;
    if (t.state !== 'active' || !at) return false;
    const left = Date.parse(at) - this.now();
    return left >= 0 && left <= EXPIRING_SOON_DAYS * 86_400_000;
  }
}
