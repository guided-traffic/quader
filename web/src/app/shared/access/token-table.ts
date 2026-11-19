import { Component, computed, input, model, output } from '@angular/core';
import { Checkbox } from '../ui/checkbox';
import { Token, TokenScope } from '../../api/tokens-api';
import { DataTable, TableColumn } from '../ui/data-table';
import { TimeAgo } from '../ui/time-ago';
import { Tooltip } from '../ui/tooltip';

/** Tokens that expire within this many days are marked (R-UI-029). */
export const EXPIRING_SOON_DAYS = 7;

/** One-line summary of what a token may do (R-AUTH-024); the full scope is in the API. */
export function scopeSummary(scope: TokenScope): string {
  const parts: string[] = [];
  if (scope.admin) {
    parts.push($localize`:@@token-table.scope.admin:Administrator: everything on this installation`);
  } else if (scope.inherit) {
    parts.push($localize`:@@token-table.scope.inherit:All repository permissions of the owner, without administration`);
  } else {
    const grants = scope.repositories ?? [];
    parts.push(
      grants.length === 0
        ? $localize`:@@token-table.scope.none:No repository access`
        : $localize`:@@token-table.scope.grants:Repository grants: ${grants.length}:count:`,
    );
  }
  const system = scope.system ?? [];
  if (system.length > 0) parts.push($localize`:@@token-table.scope.system:system: ${system.join(', ')}:list:`);
  return parts.join('; ');
}

/** True for an active token that expires within `EXPIRING_SOON_DAYS` days of `now`. */
export function expiresSoon(token: Pick<Token, 'state' | 'expiresAt'>, now: number): boolean {
  if (token.state !== 'active' || token.expiresAt === null) return false;
  const left = Date.parse(token.expiresAt) - now;
  return left >= 0 && left <= EXPIRING_SOON_DAYS * 86_400_000;
}

/**
 * A table of tokens (R-UI-029, R-UI-030, R-UI-026), its columns from the job of who reads it (Q-UI-332, Q-UI-429): the token (its
 * description, and its hint, which also names its type: `qdr_pat_…`), the owner in the administrator's list of everyone's
 * tokens, the scope, the token and the scope sharing the room the others leave by their content (Q-UI-563), when it expires and
 * when it was last used, and at the row's end
 * **Rotate** and **Revoke**, two quiet buttons with their words, or the state of a token that is no longer active. When it was
 * created is a `narrow` column of an owner's list, shown from 64 rem of the list; through what it was created is no column. Status and expiry warnings carry text and icon, never colour
 * alone. Neither the value nor its hash is part of a listed token, only the `hint` that helps to recognise one.
 */
@Component({
  selector: 'quader-token-table',
  imports: [DataTable, Checkbox, TimeAgo, Tooltip],
  template: `
    <quader-data-table
      [caption]="caption"
      [columns]="columns()"
      [empty]="tokens().length === 0"
      [emptyText]="emptyText()"
      [hasMore]="hasMore()"
      [loading]="loading()"
      (more)="more.emit()"
    >
      @for (t of tokens(); track t.id) {
        <tr>
          @if (selectable()) {
            <td>
              @if (t.state === 'active') {
                <quader-checkbox
                  [checked]="selected().includes(t.id)"
                  (checkedChange)="toggle(t.id, $event)"
                  [ariaLabel]="selectLabel(t)"
                />
              }
            </td>
          }
          <th scope="row" class="wrap-name">
            <!-- A service-account token is a secret of a technical token: its name, and below it the secret's hint (R-UI-181). -->
            @if (t.technicalToken; as tt) {
              <span translate="no">{{ tt.name }}</span>
            } @else {
              {{ t.description }}
            }
            <code translate="no" class="secondary">{{ t.hint }}</code>
          </th>
          @if (showOwner()) {
            <td class="wrap" translate="no">{{ ownerName(t) }}</td>
          }
          <td>
            {{ summary(t) }}
            @if (adminInactive(t)) {
              <br /><span class="badge warn"
                ><span class="material-icons" aria-hidden="true">warning</span
                ><ng-container i18n="@@token-table.admin-inactive"
                  >Administrator rights inactive: the owner is no administrator now</ng-container
                ></span
              >
            }
            @if (t.unknownEndpoints.length > 0) {
              <br /><span class="badge warn"
                ><span class="material-icons" aria-hidden="true">warning</span
                ><ng-container i18n="@@token-table.unknown-endpoints"
                  >Bound to an endpoint that is not configured:</ng-container
                >
                <code translate="no">{{ t.unknownEndpoints.join(', ') }}</code></span
              >
            }
          </td>
          <!-- A time relative to now; the absolute time is its title (Q-UI-431). -->
          <td>
            @if (t.expiresAt; as at) {
              <quader-time-ago [at]="at" absolute="title" />
              @if (soon(t)) {
                <br /><span class="badge warn"
                  ><span class="material-icons" aria-hidden="true">schedule</span
                  ><ng-container i18n="@@token-table.soon">Expires soon</ng-container></span
                >
              }
            } @else {
              <ng-container i18n="@@token-table.never">Never</ng-container>
            }
          </td>
          <td>
            @if (t.lastUsedAt; as at) {
              <quader-time-ago [at]="at" absolute="title" />
            } @else {
              <span class="muted" i18n="@@token-table.not-used">Not used yet</span>
            }
          </td>
          @if (!showOwner()) {
            <td class="narrow-drop"><quader-time-ago [at]="t.createdAt" absolute="title" /></td>
          }
          <!-- An active token's actions, each a quiet button with its word (Q-UI-429); a token that is no longer active says what it
               is instead (Q-UI-332). -->
          <td class="actions">
            @switch (t.state) {
              @case ('active') {
                @if (canRotate()) {
                  <button class="btn btn-tertiary" type="button" [quaderTooltip]="rotateText" (click)="rotate.emit(t)">
                    <ng-container i18n="@@token-table.rotate">Rotate</ng-container>
                    <span class="visually-hidden"> {{ t.description }}</span>
                  </button>
                }
                @if (canRevoke()) {
                  <button class="btn btn-tertiary" type="button" (click)="revoke.emit(t)">
                    <ng-container i18n="@@token-table.revoke">Revoke</ng-container>
                    <span class="visually-hidden"> {{ t.description }}</span>
                  </button>
                }
              }
              @case ('expired') {
                <span class="warn"><span class="material-icons" aria-hidden="true">history</span> <ng-container i18n="@@token-table.state.expired">Expired</ng-container></span>
              }
              @default {
                <span class="bad"><span class="material-icons" aria-hidden="true">block</span> <ng-container i18n="@@token-table.state.revoked">Revoked</ng-container></span>
              }
            }
          </td>
        </tr>
      }
    </quader-data-table>
  `,
  styleUrl: './access.scss',
})
export class TokenTable {
  readonly tokens = input.required<readonly Token[]>();
  /** What the empty list says; the default fits a filtered list (the administrator's token list). */
  readonly emptyText = input($localize`:@@token-table.empty:No token matches.`);
  /** The administrator's list of everyone's tokens: the owner is a column, and the list stacks below 64 rem (Q-UI-332). */
  readonly showOwner = input(false);
  readonly selectable = input(false);
  readonly canRotate = input(false);
  readonly canRevoke = input(true);
  /** IDs of the selected (active) tokens. */
  readonly selected = model<readonly string[]>([]);
  /** More tokens follow: the table shows its own "Load more" button. */
  readonly hasMore = input(false);
  readonly loading = input(false);
  readonly more = output<void>();
  readonly revoke = output<Token>();
  readonly rotate = output<Token>();

  protected readonly caption = $localize`:@@token-table.caption:Tokens`;
  protected readonly rotateText = $localize`:@@token-table.rotate-tip:Rotate: a new value now, the old one ends after the overlap you choose`;
  /**
   * The token, the owner and the scope wrap and declare their least widths (Q-UI-308); the times and the actions never break and
   * are as wide as their content (Q-UI-563). No column `prefer`s: the token's description and the scope share what the others
   * leave in proportion to their content, so neither is pinned to its least while the other has room. A scope that took all the
   * room left the administrator's **Client setup: npm, web-frontend/npm-vendor, 2026-11-18** three lines in 10 rem beside
   * "Repository grants: 1" in 280 px (2026-11-18). The list stacks where these least widths do not fit (Q-UI-680).
   */
  protected readonly columns = computed<readonly TableColumn[]>(() => [
    ...(this.selectable()
      ? [{ id: 'select', label: $localize`:@@token-table.select:Select`, hideLabel: true, width: '2rem' }]
      : []),
    { id: 'description', label: $localize`:@@token-table.token:Token`, width: '10rem' },
    ...(this.showOwner() ? [{ id: 'owner', label: $localize`:@@token-table.owner:Owner`, width: '7rem' }] : []),
    {
      id: 'scope',
      label: $localize`:@@token-table.scope:Scope`,
      help: $localize`:@@token-table.help.scope:What the token may do: all permissions of its owner, or only the listed repository grants.`,
      width: '9rem',
    },
    { id: 'expires', label: $localize`:@@token-table.expires:Expires` },
    {
      id: 'lastUsed',
      label: $localize`:@@token-table.last-used:Last used`,
      help: $localize`:@@token-table.help.last-used:The last time the token authenticated a request. Not every use is recorded immediately.`,
    },
    ...(this.showOwner()
      ? []
      : [
          { id: 'created', label: $localize`:@@token-table.created:Created`, narrow: true },
        ]),
    { id: 'actions', label: $localize`:@@token-table.actions:Actions`, hideLabel: true },
  ]);

  private readonly now = computed(() => {
    this.tokens();
    return Date.now();
  });

  /**
   * An admin token whose owner does not hold the role has no administrator rights until the owner holds it (R-AUTH-024). The
   * record states the owner's role at the time of the read (`ownerIsAdmin`, Q-AUTH-209), so every list shows it, also the token
   * administration's list of other people's tokens (Q-UI-221).
   */
  protected adminInactive(t: Token): boolean {
    return t.state === 'active' && t.scope.admin === true && !t.ownerIsAdmin;
  }

  protected summary(t: Token): string {
    return scopeSummary(t.scope);
  }

  protected soon(t: Token): boolean {
    return expiresSoon(t, this.now());
  }

  protected ownerName(t: Token): string {
    return t.owner.kind === 'service_account' ? `sa:${t.owner.name}` : t.owner.name;
  }

  protected selectLabel(t: Token): string {
    return $localize`:@@token-table.select-one:Select ${t.description}:description:`;
  }

  protected toggle(id: string, on: boolean): void {
    this.selected.update((list) => (on ? [...new Set([...list, id])] : list.filter((x) => x !== id)));
  }
}
