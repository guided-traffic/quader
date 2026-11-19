import { Component, computed, input, output } from '@angular/core';
import { TechnicalToken } from '../../api/technical-tokens-api';
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

/**
 * A list of technical tokens of a service account (R-UI-181, Q-UI-740): the token (its name, below it the hint of its current secret and
 * its description), when its secret expires, when it was last used, and at the row's end **Rotate** and **Revoke**, quiet buttons
 * with their words, for an active token; a token without a live secret says **Expired** or **Revoked** and offers **Issue new
 * secret**. The section lists the active tokens and the ended ones in a table each, so that the action cells of one table are
 * built one way (R-UI-163, R-UI-176). The download-limit columns **Limit** and **Last 24 hours** and the action **Set limit** of R-UI-181 stand between the
 * token and its expiry and before **Revoke** (`columns`, the row's cells, `actions`); the limits area adds them.
 */
@Component({
  selector: 'quader-technical-token-table',
  imports: [DataTable, TimeAgo, Tooltip],
  template: `
    <quader-data-table [caption]="caption()" [columns]="columns()" [empty]="tokens().length === 0" [emptyText]="emptyText()">
      @for (t of tokens(); track t.id) {
        <tr>
          <th scope="row" class="wrap-name">
            <span translate="no">{{ t.name }}</span>
            @if (secretOf(t); as s) {
              <code translate="no" class="secondary">{{ s.hint }}</code>
            }
            @if (t.description) {
              <span class="secondary">{{ t.description }}</span>
            }
          </th>
          <!-- The limits area's columns Limit and Last 24 hours stand here (R-UI-181). -->
          <!-- A time relative to now; the absolute time is its title (Q-UI-431). -->
          <td>
            @if (t.state !== 'active') {
              <!-- An ended token: when its last secret ended, below it how, as "Expires soon" stands below the time of an
                   active token, so that the cell's first line is on the row's line (R-UI-172). -->
              @if (endOf(t); as at) {
                <quader-time-ago [at]="at" absolute="title" /><br />
              }
              @if (t.state === 'expired') {
                <span class="warn"><span class="material-icons" aria-hidden="true">history</span> <ng-container i18n="@@tt-table.expired">Expired</ng-container></span>
              } @else {
                <span class="bad"><span class="material-icons" aria-hidden="true">block</span> <ng-container i18n="@@tt-table.revoked">Revoked</ng-container></span>
              }
            } @else if (secretOf(t)?.expiresAt; as at) {
              <quader-time-ago [at]="at" absolute="title" />
              @if (soon(t)) {
                <br /><span class="badge warn"
                  ><span class="material-icons" aria-hidden="true">schedule</span
                  ><ng-container i18n="@@tt-table.soon">Expires soon</ng-container></span
                >
              }
            } @else {
              <ng-container i18n="@@tt-table.never">Never</ng-container>
            }
          </td>
          <td>
            @if (used(t); as at) {
              <quader-time-ago [at]="at" absolute="title" />
            } @else {
              <span class="muted" i18n="@@tt-table.not-used">Not used yet</span>
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
          </td>
        </tr>
      }
    </quader-data-table>
  `,
  styleUrl: '../../shared/access/access.scss',
})
export class TechnicalTokenTable {
  readonly tokens = input.required<readonly TechnicalToken[]>();
  readonly caption = input($localize`:@@tt-table.caption:Technical tokens`);
  readonly emptyText = input($localize`:@@tt-table.empty:This account has no technical token yet.`);
  /** The tokens without a live secret: the time column says when and how each ended. */
  readonly ended = input(false);
  readonly canRotate = input(false);
  readonly canRevoke = input(false);
  readonly canIssue = input(false);
  readonly rotate = output<TechnicalToken>();
  readonly revoke = output<TechnicalToken>();
  /** **Issue new secret** of a token without a live secret (Q-AUTH-416). */
  readonly renew = output<TechnicalToken>();

  protected readonly rotateText = $localize`:@@tt-table.rotate-tip:Rotate: a new secret now, the old one ends after the overlap you choose. The token keeps its name and its limit.`;
  /** The token wraps and declares its least width (Q-UI-308); the times and the actions are as wide as their content. */
  protected readonly columns = computed<readonly TableColumn[]>(() => [
    { id: 'token', label: $localize`:@@tt-table.token:Token`, width: '10rem', prefer: true },
    this.ended()
      ? { id: 'ended', label: $localize`:@@tt-table.ended:Ended` }
      : { id: 'expires', label: $localize`:@@tt-table.expires:Expires` },
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
