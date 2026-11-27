import { Component, DestroyRef, LOCALE_ID, computed, effect, inject, input, linkedSignal, output, signal, untracked } from '@angular/core';
import { QuaderDialog } from '../../shared/ui/dialog';
import { firstValueFrom } from 'rxjs';
import { Bastion, BastionDisableMode, BastionsApi, BastionTokenCreated, BastionTokenSummary } from '../../api/bastions-api';
import { ApiError } from '../../api/problem';
import { Notifier } from '../../core/notifier';
import { SessionStore } from '../../core/session-store';
import { formatDuration } from '../../shared/access/duration';
import { TokenSecret } from '../../shared/access/token-secret';
import { Confirm } from '../../shared/confirm-dialog';
import { ErrorNotice } from '../../shared/error-notice';
import { asApiError } from '../../shared/paged-resource';
import { formatTime } from '../../shared/time-format';
import { DataTable, TableColumn } from '../../shared/ui/data-table';
import { Disclosure } from '../../shared/ui/disclosure';
import { Help } from '../../shared/ui/help';
import { TimeAgo } from '../../shared/ui/time-ago';
import { Tooltip } from '../../shared/ui/tooltip';
import { Unavailable } from '../../shared/ui/unavailable';
import { relativeTime } from '../packages/format-time';
import { DownloadLinkConnection } from '../../api/download-links-api';
import { BastionDisableDialog, BastionDisableDialogData } from './bastion-disable-dialog';
import { BastionTokenDialog, BastionTokenDialogData, BastionTokenDraft } from './bastion-dialogs';
import { BastionFold, cardWarns } from './bastion-fold';
import { BastionHandover } from './bastion-handover';
import { BastionLimits } from './bastion-limits';
import { BastionSwitches } from './bastion-switches';
import { ContactHealth, Deployment, TokenHealth, contactHealth, deployments, headHosts, newestContact, tokenExpiry, tokenHealth } from './bastion-state';
import { ADDRESS_MARKS, PartsText } from '../../shared/break-parts';
import { Busy } from '../../shared/ui/busy';

/** The badge of a Bastion's state (bastion.md Q-BAS-511): its word, its tone and its icon; green only for the one positive state. */
interface StateBadge {
  readonly word: string;
  readonly tone: 'ok' | 'warn' | 'neutral';
  readonly icon: string;
}

/** The badge of each of the six states the main installation computes (R-BAS-133, R-UI-240). */
export function stateBadge(state: Bastion['state']): StateBadge {
  switch (state) {
    case 'connected':
      return { word: $localize`:@@bastion.state.connected:Connected`, tone: 'ok', icon: 'check_circle' };
    case 'noContact':
      return { word: $localize`:@@bastion.state.no-contact:No contact yet`, tone: 'neutral', icon: 'schedule' };
    case 'contactLost':
      return { word: $localize`:@@bastion.state.lost:Contact lost`, tone: 'warn', icon: 'warning' };
    case 'disabling':
      return { word: $localize`:@@bastion.state.disabling:Disabling`, tone: 'neutral', icon: 'pause_circle' };
    case 'disabled':
      return { word: $localize`:@@bastion.state.disabled:Disabled`, tone: 'neutral', icon: 'block' };
    case 'disableUnconfirmed':
      return { word: $localize`:@@bastion.state.unconfirmed:Disable not confirmed`, tone: 'warn', icon: 'warning' };
  }
}

/**
 * One Bastion registration (R-BAS-007, R-UI-240), a card that folds to its head (Q-UI-1640), top to bottom (Q-UI-1620): the head with the
 * fold's chevron and the name, the host of each deployment in grey (Q-UI-1641, Q-UI-1643), the Bastion's state as one badge (bastion.md
 * Q-BAS-511) with the downloads a soft disable still lets run, the mark **Helm configuration** of a declared registration with its help
 * (Q-UI-1629), and at its end the Bastion's own acts: **Disable…** (soft or hard, R-BAS-133), **Enable**, **Disable hard…** while a soft
 * disable drains, and **Delete**, offered unavailable with its reason on a declared registration (R-UI-270). Under the head, folding with
 * the card, the description; the main settings, the rows of its web interface (`<quader-bastion-switches>`, R-BAS-102, R-BAS-104) and of
 * its direct reads from the object store (`<quader-bastion-handover>`, R-BAS-071); **Limits** with **Details** (its first group the web
 * interface's **Hide repositories**, R-BAS-131) and, at their end, **Created** (`<quader-bastion-limits>`, R-UI-240, R-UI-243); and the
 * closed sections **Replicas**, grouped by deployment where there are several, and **Tokens** with **Issue token** or **Rotate token** and
 * **Invalidate** per token (Q-UI-1642), one line each, which turn into a warning and open by themselves (Q-UI-1622, Q-UI-1623). A change
 * reads the registration for its `ETag` first, because the list carries none (R-API-012). A token's value is shown once through
 * `<quader-token-secret>` and lives in this card's signal only (R-UI-043). Which acts show follows the capabilities of R-BAS-004:
 * `endpoints:write` for the registration and for issuing a token, `endpoints:write` or `tokens:revoke` for invalidating one.
 */
@Component({
  selector: 'quader-bastion-card',
  imports: [
    Busy,
    BastionHandover,
    BastionLimits,
    BastionSwitches,
    DataTable,
    Disclosure,
    ErrorNotice,
    Help,
    PartsText,
    TimeAgo,
    TokenSecret,
    Tooltip,
    Unavailable,
  ],
  template: `
    <section class="card" [class.folded]="!open()" [attr.aria-labelledby]="nameId()">
      <div class="head">
        <div class="title">
          <!-- The name and the hosts are the heading; the fold's button holds the chevron and the name, so the region and the button are
               named by the Bastion alone (Q-UI-1640, Q-UI-1641). -->
          <h2>
            <button class="fold" type="button" [attr.aria-expanded]="open()" [attr.aria-controls]="bodyId()" (click)="toggle()">
              <span class="material-icons chevron" aria-hidden="true">chevron_right</span
              ><span [id]="nameId()" translate="no" data-testid="bastion-name">{{ bastion().name }}</span>
            </button>
            @if (hosts().length > 0) {
              <span class="hosts" translate="no" data-testid="bastion-hosts"
                >@for (h of hosts(); track h; let last = $last) {<span class="host"><quader-parts-text [value]="h" [marks]="addressMarks" /></span
                  >@if (!last) {<span class="visually-hidden">, </span>}}</span
              >
            }
          </h2>
          <span
            [class]="badgeClass()"
            data-testid="bastion-state"
            [attr.tabindex]="stateTip() ? 0 : null"
            [quaderTooltip]="stateTip()"
            [quaderTooltipPin]="true"
            ><span class="material-icons" aria-hidden="true">{{ badge().icon }}</span>{{ badge().word }}</span
          >
          @if (runningText(); as running) {
            <span class="running" data-testid="bastion-running">{{ running }}</span>
          }
          @if (bastion().declared) {
            <!-- A registration of the Helm configuration (R-BAS-127, Q-UI-1629): a lock and two words, what they mean behind the sign.
                 Words, not a badge: the sign explains the name before it, and a badge's word is no name the sign can stand beside. -->
            <span class="source" data-testid="bastion-declared"
              ><span class="material-icons inline" aria-hidden="true">lock</span><span class="help-anchor">{{ helmName }}</span
              ><quader-help [text]="declaredHelp" [about]="helmName"
            /></span>
          }
        </div>
        @if (canWrite()) {
          <!-- The Bastion's own acts (Q-UI-1641); the token's stand in Tokens (Q-UI-1642). Enable is the primary act while it is disabled. -->
          <div class="row-actions">
            @if (bastion().disabled; as d) {
              <button class="btn btn-confirm" type="button" [disabled]="busy()" [quaderBusy]="busy()" (click)="enable()">
                <ng-container i18n="@@bastion.enable">Enable</ng-container>
                <span class="visually-hidden" translate="no"> {{ bastion().name }}</span>
              </button>
              @if (bastion().state === 'disabling' && d.mode === 'soft') {
                <button class="btn btn-default" type="button" [disabled]="busy()" [quaderBusy]="busy()" (click)="disableHard()">
                  <ng-container i18n="@@bastion.disable-hard">Disable hard…</ng-container>
                  <span class="visually-hidden" translate="no"> {{ bastion().name }}</span>
                </button>
              }
            } @else {
              <button class="btn btn-default" type="button" [disabled]="busy()" [quaderBusy]="busy()" (click)="disable()">
                <ng-container i18n="@@bastion.disable">Disable…</ng-container>
                <span class="visually-hidden" translate="no"> {{ bastion().name }}</span>
              </button>
            }
            <!-- A declared registration is not deleted here (Q-UI-1498): the next run of quader migrate would create it again. The
                 button stays, unavailable, its reason its tooltip (R-UI-270). -->
            <button
              class="btn btn-tertiary"
              type="button"
              data-testid="bastion-delete"
              [disabled]="busy()"
              [quaderBusy]="busy()"
              [quaderUnavailable]="bastion().declared ? deleteReason : ''"
              (click)="remove()"
            >
              <ng-container i18n="@@bastion.delete">Delete</ng-container>
              <span class="visually-hidden" translate="no"> {{ bastion().name }}</span>
            </button>
          </div>
        }
      </div>

      <!-- An act's refusal and a token shown once stand under the head, also while the card is folded. -->
      @if (actionError(); as e) {
        <quader-error-notice [error]="e" />
      }
      @if (secret(); as s) {
        <quader-token-secret [value]="s.token" [description]="secretNote(s)" (closed)="secret.set(null)" />
      }

      <div class="collapse-region" [class.open]="open()" [id]="bodyId()">
        <div>
          @if (bastion().description) {
            <!-- What the administrator wrote, in their language: never translated (R-UI-214). -->
            <p class="description" translate="no" data-testid="bastion-description">{{ bastion().description }}</p>
          }

          <!-- The main settings, rows of name and control (Q-UI-1620, Q-UI-1621): what the internet sees first (Q-UI-1616), then the
               direct reads. -->
          <div class="main-settings">
            <quader-bastion-switches [bastion]="bastion()" (changed)="changed.emit()" />
            <quader-bastion-handover [bastion]="bastion()" [connections]="connections()" (changed)="changed.emit()" />
          </div>

          <quader-bastion-limits [bastion]="bastion()" (changed)="changed.emit()">
            <!-- The first group of Details: what the web interface hides (R-BAS-131, W-40's P6). -->
            <quader-bastion-switches details-start part="details" [bastion]="bastion()" (changed)="changed.emit()" />
            <p details-end class="created" data-testid="bastion-created" i18n="@@bastion.created">Created <quader-time-ago [at]="bastion().createdAt" absolute="title" /></p>
          </quader-bastion-limits>

          <!-- The replicas, one line (Q-UI-1622): a warning opens the section by itself; none yet is a plain line in its place. -->
          @if (contacts().kind === 'none') {
            <div class="section-plain" data-testid="bastion-replicas">
              <h3 i18n="@@bastion.replicas.heading">Replicas</h3>
              <p class="muted" i18n="@@bastion.replicas.none">No replica has made contact yet.</p>
            </div>
          } @else {
            <quader-disclosure
              data-testid="bastion-replicas"
              i18n-heading="@@bastion.replicas.heading"
              heading="Replicas"
              [level]="3"
              [count]="bastion().contacts.length"
              [note]="contactsNote()"
              [noteTone]="contactsWarn() ? 'warn' : 'muted'"
              [(expanded)]="replicasOpen"
            >
              <quader-data-table [caption]="contactsCaption()" [columns]="contactColumns" [empty]="false">
                @for (d of groups(); track d.baseUrl) {
                  <!-- A pair of deployments under one registration (Q-UI-1643): the replicas grouped by the base URL they report. -->
                  @if (groups().length > 1) {
                    <tr class="group-row">
                      <th scope="rowgroup" colspan="2" data-testid="bastion-deployment">{{ groupLabel(d) }}</th>
                    </tr>
                  }
                  @for (c of d.contacts; track c.replica) {
                    <tr>
                      <!-- The replica's name, and under it what is the same for most rows (Q-UI-959): the address it connected from and its
                           version. A name of 63 characters breaks after its hyphens. -->
                      <th scope="row" class="wrap-name replica">
                        <span translate="no">{{ c.replica }}</span>
                        <span class="secondary"
                          ><span class="dot-separated" data-testid="bastion-contact-facts"
                            >@if (c.clientAddress; as v) {<span translate="no"><quader-parts-text [value]="v" [marks]="addressMarks" /></span>}<span
                              i18n="@@bastion.contact.release"
                              >Version <span translate="no">{{ c.release }}</span></span
                            ></span
                          ></span
                        >
                      </th>
                      <td><quader-time-ago [at]="c.contactAt" absolute="title" /></td>
                    </tr>
                  }
                }
              </quader-data-table>
            </quader-disclosure>
          }

          <!-- The tokens, one line (Q-UI-1623): a token that expires within 30 days or has expired opens the section by itself. The
               token's acts stand here, never in the head (Q-UI-1642). -->
          @if (tokens().kind === 'none') {
            <div class="section-plain" data-testid="bastion-tokens">
              <h3 i18n="@@bastion.tokens">Tokens</h3>
              <p class="muted" i18n="@@bastion.tokens-empty">No token yet.</p>
              @if (canWrite()) {
                <button
                  class="btn btn-confirm on-line"
                  type="button"
                  [disabled]="busy()"
                  [quaderBusy]="busy()"
                  [quaderUnavailable]="bastion().declared ? rotateReason : ''"
                  (click)="issue()"
                >
                  <ng-container i18n="@@bastion.token.issue">Issue token</ng-container>
                  <span class="visually-hidden" translate="no"> {{ bastion().name }}</span>
                </button>
              }
            </div>
          } @else {
            <quader-disclosure
              data-testid="bastion-tokens"
              i18n-heading="@@bastion.tokens"
              heading="Tokens"
              [level]="3"
              [count]="bastion().tokens.length"
              [note]="tokensNote()"
              [noteTone]="tokensWarn() ? 'warn' : 'muted'"
              [(expanded)]="tokensOpen"
            >
              @if (canWrite()) {
                <div class="section-actions">
                  <button
                    class="btn btn-default"
                    type="button"
                    data-testid="bastion-rotate"
                    [disabled]="busy()"
                    [quaderBusy]="busy()"
                    [quaderUnavailable]="bastion().declared ? rotateReason : ''"
                    (click)="issue()"
                  >
                    <ng-container i18n="@@bastion.token.rotate">Rotate token</ng-container>
                    <span class="visually-hidden" translate="no"> {{ bastion().name }}</span>
                  </button>
                </div>
              }
              <quader-data-table [caption]="tokensCaption()" [columns]="tokenColumns" [empty]="false">
                @for (t of bastion().tokens; track t.id) {
                  <tr>
                    <!-- The name, and under it the hint, the Secret of the declared token and when the token was created (Q-UI-959). -->
                    <th scope="row">
                      @if (t.declared) {
                        <span data-testid="bastion-token-declared">{{ helmTokenName }}</span>
                      } @else {
                        <span translate="no">{{ t.description }}</span>
                      }
                      <span class="secondary"
                        ><span class="dot-separated"
                          ><code translate="no">{{ t.hint }}</code
                          >@if (t.declared && bastion().declared?.secretName; as secretName) {<span i18n="@@bastion.token.secret"
                              >Secret <code translate="no">{{ secretName }}</code></span
                            >}<span i18n="@@bastion.token.created">Created <quader-time-ago [at]="t.createdAt" absolute="title" /></span></span
                        ></span
                      >
                    </th>
                    <td>
                      @if (t.expiresAt; as at) {
                        <quader-time-ago [at]="at" absolute="title" />
                      } @else {
                        <span i18n="@@bastion.token.never">Never</span>
                      }
                      @switch (expiry(t)) {
                        @case ('expired') {
                          <span class="expiry-mark"><span class="badge bad"><span class="material-icons" aria-hidden="true">error</span><ng-container i18n="@@bastion.token.expired">Expired</ng-container></span></span>
                        }
                        @case ('soon') {
                          <span class="expiry-mark"><span class="badge warn"><span class="material-icons" aria-hidden="true">schedule</span><ng-container i18n="@@bastion.token.soon">Expires soon</ng-container></span></span>
                        }
                      }
                    </td>
                    <td>
                      @if (t.lastUsedAt; as at) {
                        <quader-time-ago [at]="at" absolute="title" />
                      } @else {
                        <span class="muted no-time" i18n="@@bastion.token.unused">Not used yet</span>
                      }
                    </td>
                    <td class="actions">
                      <!-- Every token, the declared one included: the last resort of a leak (R-BAS-133, Q-UI-1642). -->
                      @if (canRevoke()) {
                        <button class="btn btn-tertiary" type="button" [disabled]="busy()" [quaderBusy]="busy()" (click)="invalidate(t)">
                          <ng-container i18n="@@bastion.token.invalidate">Invalidate</ng-container>
                          <span class="visually-hidden" translate="no"> {{ tokenName(t) }}</span>
                        </button>
                      }
                    </td>
                  </tr>
                }
              </quader-data-table>
            </quader-disclosure>
          }
        </div>
      </div>
    </section>
  `,
  styleUrl: '../../shared/access/access.scss',
  styles: `
    /* The head: the fold, the name, the hosts, the state and the source on the left, the acts at its end; the acts take a line of their
       own where the card is narrow. The buttons stand on the middle of the name's line (R-UI-175). */
    .head {
      display: flex;
      flex-wrap: wrap;
      align-items: center;
      justify-content: space-between;
      gap: var(--quader-space-2) var(--quader-space-4);
    }
    .title {
      display: flex;
      flex-wrap: wrap;
      align-items: center;
      gap: var(--quader-space-1) var(--quader-space-3);
      min-inline-size: 0;
    }
    /* The fold's button and the hosts on the middle of one line (R-UI-175): the heading's line, 24 px, holds both, so the badge and the
       acts beside it stand on the same middle. */
    .card .title h2 {
      display: flex;
      flex-wrap: wrap;
      align-items: center;
      gap: 0 var(--quader-space-3);
      min-inline-size: 0;
      margin: 0;
      overflow-wrap: anywhere;
    }
    /* The fold's button reads as the heading: the chevron before the name, the heading's colour and weight. */
    .fold {
      display: inline-flex;
      align-items: center;
      gap: var(--quader-space-1);
      min-inline-size: 0;
      padding: 0;
      border: 0;
      background: none;
      color: var(--quader-heading);
      font: inherit;
      text-align: start;
      cursor: pointer;
    }
    .fold:focus-visible {
      outline: var(--quader-focus-ring);
      outline-offset: 2px;
    }
    .chevron {
      font-size: 1.25rem;
      transition: transform var(--quader-motion-duration) var(--quader-motion-easing);
    }
    [aria-expanded='true'] > .chevron {
      transform: rotate(90deg);
    }
    /* The hosts of its deployments, in the muted colour of a fact beside the name at the text's size; separated by a dot, each on a
       line of its own where the card is narrow (Q-UI-1641). */
    .hosts {
      display: inline-flex;
      flex-wrap: wrap;
      column-gap: 0.25em;
      min-inline-size: 0;
      color: var(--quader-text-muted);
      font-size: var(--quader-text-size);
      font-weight: 400;
    }
    .host:not(:last-child)::after {
      content: ' ·';
    }
    /* Narrow: the name on a line of its own, each host on a line of its own under it, where the name starts; the state and the mark on
       the next line, the acts on theirs (W-41's 390 px). */
    @container content (max-width: 40rem) {
      .card .title h2 {
        flex-basis: 100%;
      }
      .hosts {
        flex-basis: 100%;
        flex-direction: column;
        padding-inline-start: calc(1.25rem + var(--quader-space-1));
      }
      .host:not(:last-child)::after {
        content: none;
      }
    }
    .running {
      color: var(--quader-text-muted);
      font-size: var(--quader-text-muted-size);
    }
    .head .row-actions {
      margin: 0;
    }
    /* The lock, the words and the sign stay on one line in the muted colour of a fact beside the name; the sign stands in the room
       the words keep for it (help-anchor). */
    .source {
      color: var(--quader-text-muted);
      font-size: var(--quader-text-muted-size);
      white-space: nowrap;
    }
    .source > .material-icons {
      margin-inline-end: var(--quader-space-1);
    }
    /* What folds starts where the name starts, after the chevron. */
    .collapse-region > div {
      padding-inline-start: calc(1.25rem + var(--quader-space-1));
    }
    .description {
      margin: var(--quader-space-1) 0 0;
      max-inline-size: var(--quader-settings-measure);
      color: var(--quader-text-muted);
    }
    .main-settings {
      margin-block: var(--quader-space-4);
    }
    .created {
      margin-block: var(--quader-space-4) 0;
    }
    quader-error-notice,
    quader-token-secret {
      display: block;
      margin-block-start: var(--quader-space-3);
    }
    /* A section without entries: its heading and its one line where the fold would stand, and the act that fills it after them. */
    .section-plain {
      display: flex;
      flex-wrap: wrap;
      align-items: baseline;
      gap: 0 var(--quader-space-2);
      padding-block: var(--quader-space-3);
      border-block-start: 1px solid var(--quader-border);
    }
    /* Its heading stands where a fold's heading stands after its chevron, so the sections' names read down one line. */
    .section-plain h3 {
      margin: 0;
      padding-inline-start: calc(1.25rem + var(--quader-space-1));
      font-size: var(--quader-text-size);
      font-weight: 600;
    }
    .section-plain p {
      margin: 0;
      font-size: var(--quader-text-muted-size);
    }
    .section-plain .btn {
      margin-inline-start: var(--quader-space-2);
    }
    .section-actions {
      margin-block-end: var(--quader-space-3);
    }
    /* A replica's name (a pod name has up to 63 characters) breaks after its hyphens, an address (IPv6 has 39) after its colons and
       dots (breakParts), and anywhere where a part is still longer than its room, so that no value widens its column. */
    th.replica {
      overflow-wrap: anywhere;
    }
    tr.group-row > th {
      padding-block-start: var(--quader-space-3);
      font-weight: 600;
      overflow-wrap: anywhere;
    }
    /* A token's mark stands on a line of its own under the expiry: after a line break the badge, 20 px on a line of 16, touched the
       time above it. */
    .expiry-mark {
      display: block;
      margin-block-start: var(--quader-space-1);
    }
  `,
})
export class BastionCard {
  readonly bastion = input.required<Bastion>();
  /** The page holds this one Bastion: its card opens by itself (Q-UI-1640). */
  readonly single = input(false);
  /** Entries of the token dialog kept across a re-authentication; the card opens the dialog with them once. */
  readonly draft = input<BastionTokenDraft | null>(null);
  /** The connections' states of the download link settings, for the reasons of the direct reads (R-BAS-071); null when not read. */
  readonly connections = input<readonly DownloadLinkConnection[] | null>(null);
  /** The registration changed or went away: the page reads the list again. */
  readonly changed = output<void>();

  private readonly api = inject(BastionsApi);
  private readonly confirmer = inject(Confirm);
  private readonly dialog = inject(QuaderDialog);
  private readonly notifier = inject(Notifier);
  private readonly session = inject(SessionStore);
  private readonly locale = inject(LOCALE_ID);
  private readonly fold = inject(BastionFold);

  protected readonly busy = signal(false);
  protected readonly actionError = signal<ApiError | null>(null);
  /** The value of a token that was just issued; card-local, never stored (R-UI-043). */
  protected readonly secret = signal<BastionTokenCreated | null>(null);

  protected readonly helmName = $localize`:@@bastion.source.helm:Helm configuration`;
  protected readonly declaredHelp = $localize`:@@bastion.source.helm-help:Its description, its addresses and its token are set in bastions.declared of the Helm configuration and changed there.`;
  /** Why a declared registration's Delete cannot act (R-UI-270, Q-UI-1636). */
  protected readonly deleteReason = $localize`:@@bastion.delete.helm:Set in bastions.declared of the Helm configuration: remove it there first, then delete it here.`;
  /** Why a declared registration's token is not issued or rotated here (R-BAS-133, Q-BAS-517). */
  protected readonly rotateReason = $localize`:@@bastion.token.rotate.helm:Its token is set in bastions.declared of the Helm configuration: put a new value into its Secret.`;
  /** The declared token's name, the screen's whatever the server stored (Q-UI-1629). */
  protected readonly helmTokenName = $localize`:@@bastion.token.helm:Token of the Helm configuration`;

  protected readonly canWrite = computed(() => this.session.has('endpoints:write'));
  protected readonly canRevoke = computed(() => this.session.has('endpoints:write') || this.session.has('tokens:revoke'));
  protected readonly nameId = computed(() => `bastion-${this.bastion().name}`);
  protected readonly bodyId = computed(() => `bastion-${this.bastion().name}-body`);
  protected readonly contactsCaption = computed(() => $localize`:@@bastion.contacts-caption:Replicas of ${this.bastion().name}:name:`);
  protected readonly tokensCaption = computed(() => $localize`:@@bastion.tokens-caption:Tokens of ${this.bastion().name}:name:`);

  /*
   * The tables have one form at every width but the narrow one (Q-UI-959): each column declares its least width and its values fit it
   * or break inside it, so the rows stack by the card's width alone, never by a long value. What is the same for most rows stands
   * under the row's name, as the reference's list of runners does: a replica's address and version, a token's hint and creation. A
   * token's description takes the room the others leave. A time is relative, its date and time its title (Q-UI-431). The replicas'
   * base URL is their deployment's: the head names its host and a pair's groups name it (Q-UI-1643).
   */
  protected readonly contactColumns: readonly TableColumn[] = [
    {
      id: 'replica',
      label: $localize`:@@bastion.col.replica:Replica`,
      help: $localize`:@@bastion.help.replica:Under its name: the address the main installation saw it connect from, and its version.`,
      width: '16rem',
      prefer: true,
    },
    { id: 'time', label: $localize`:@@bastion.col.time:Last contact`, width: '7.5rem' },
  ];
  protected readonly tokenColumns: readonly TableColumn[] = [
    { id: 'description', label: $localize`:@@bastion.col.token:Token`, width: '9rem', prefer: true },
    // As wide as "in 12 months" and the mark "Expires soon" under it (Q-UI-1623); the mark's number made it 11 rem.
    { id: 'expires', label: $localize`:@@bastion.col.expires:Expires`, width: '8rem' },
    {
      id: 'lastUsed',
      label: $localize`:@@bastion.col.last-used:Last used`,
      help: $localize`:@@bastion.help.last-used:The last time the token authenticated a request. When you rotate, invalidate the old token once the new one shows a recent time.`,
      width: '7.5rem',
    },
    { id: 'actions', label: $localize`:@@bastion.col.actions:Actions`, hideLabel: true },
  ];

  /** An address breaks after its dots, slashes and colons, each piece whole while it fits (`PartsText`). */
  protected readonly addressMarks = ADDRESS_MARKS;

  /** The time the card is judged at: the moment the registration was read. */
  private readonly now = computed(() => {
    this.bastion();
    return Date.now();
  });

  protected readonly hosts = computed(() => headHosts(this.bastion(), this.now()));
  protected readonly groups = computed<Deployment[]>(() => deployments(this.bastion()));
  protected readonly badge = computed(() => stateBadge(this.bastion().state));
  protected readonly badgeClass = computed(() => {
    const tone = this.badge().tone;
    return tone === 'neutral' ? 'badge state' : `badge state ${tone}`;
  });
  /** What the badge's tooltip says (Q-BAS-511): how long the contact is lost, the way and time of a disable, an unconfirmed one's reason. */
  protected readonly stateTip = computed(() => {
    const b = this.bastion();
    switch (b.state) {
      case 'contactLost': {
        const newest = newestContact(b.contacts);
        return newest ? $localize`:@@bastion.state.lost-tip:No replica has made contact for ${this.duration(newest.contactAt)}:duration:.` : '';
      }
      case 'disabling':
      case 'disabled': {
        const d = b.disabled;
        if (!d) return '';
        const since = formatTime(d.at, { part: 'datetime' });
        return d.mode === 'hard'
          ? $localize`:@@bastion.state.disabled-hard-tip:Hard, since ${since}:time:`
          : $localize`:@@bastion.state.disabled-soft-tip:Soft, since ${since}:time:`;
      }
      case 'disableUnconfirmed':
        return $localize`:@@bastion.state.unconfirmed-tip:No replica has confirmed it yet. The main installation refuses its requests; its replicas serve from their cache until their next contact, at most their grace period.`;
      default:
        return '';
    }
  });
  /** "3 downloads still running" after the badge while a soft disable lets them finish (R-BAS-133). */
  protected readonly runningText = computed(() => {
    const b = this.bastion();
    if (b.state !== 'disabling' || b.running <= 0) return '';
    return b.running === 1
      ? $localize`:@@bastion.state.running-one:1 download still running`
      : $localize`:@@bastion.state.running:${b.running}:count: downloads still running`;
  });

  protected readonly contacts = computed<ContactHealth>(() => contactHealth(this.bastion(), this.now()));
  protected readonly tokens = computed<TokenHealth>(() => tokenHealth(this.bastion(), this.now()));
  protected readonly contactsWarn = computed(() => {
    const h = this.contacts();
    return (h.kind === 'silent' && h.warn) || (h.kind === 'current' && (h.releases.length > 1 || h.silentDeployments.length > 0));
  });
  protected readonly tokensWarn = computed(() => {
    const k = this.tokens().kind;
    return k === 'expired' || k === 'soon';
  });
  /**
   * The sections' open state: closed while healthy; a change into a warning opens the section (the source turns true), and nothing
   * else sets it, so a live read keeps what the viewer chose and the screen never closes a section.
   */
  protected readonly replicasOpen = linkedSignal<boolean, boolean>({
    source: () => this.contactsWarn(),
    computation: (warn, previous) => warn || (previous?.value ?? false),
  });
  protected readonly tokensOpen = linkedSignal<boolean, boolean>({
    source: () => this.tokensWarn(),
    computation: (warn, previous) => warn || (previous?.value ?? false),
  });

  /** The card shows a warning somewhere (Q-UI-1640): it is never folded away. */
  private readonly warns = computed(() => cardWarns(this.bastion(), this.now(), this.connections()));
  /**
   * The card's fold (Q-UI-1640): at first the viewer's kept choice, or open where the page holds one Bastion and folded where it holds
   * several; open whatever was kept while it warns. Later a change into a warning opens it, a live read keeps what the viewer chose, and
   * the screen never folds a card.
   */
  protected readonly open = linkedSignal<boolean, boolean>({
    source: () => this.warns(),
    computation: (warn, previous) => {
      if (previous === undefined) return warn || (this.fold.choice(untracked(this.bastion).name) ?? untracked(this.single));
      return (warn && !previous.source) || previous.value;
    },
  });

  protected readonly contactsNote = computed(() => {
    const h = this.contacts();
    switch (h.kind) {
      case 'none':
        return '';
      case 'silent':
        return h.warn
          ? $localize`:@@bastion.replicas.silent:No replica has made contact for ${this.duration(h.since)}:duration:.`
          : $localize`:@@bastion.replicas.quiet:Last contact ${this.ago(h.since)}:time:`;
      case 'current': {
        const warnings: string[] = [];
        if (h.releases.length > 1) {
          warnings.push($localize`:@@bastion.replicas.releases:The replicas run different versions: ${h.releases.join(', ')}:versions:.`);
        }
        for (const d of h.silentDeployments) {
          warnings.push($localize`:@@bastion.replicas.address-silent:${d.host}:host:: no contact for ${this.duration(d.since)}:duration:.`);
        }
        if (warnings.length > 0) return warnings.join(' ');
        return h.deployments > 1
          ? $localize`:@@bastion.replicas.ok-many:${h.deployments}:count: addresses · last contact ${this.ago(h.last)}:time: · version ${h.release}:release:`
          : $localize`:@@bastion.replicas.ok:Last contact ${this.ago(h.last)}:time: · version ${h.release}:release:`;
      }
    }
  });

  protected readonly tokensNote = computed(() => {
    const h = this.tokens();
    switch (h.kind) {
      case 'none':
        return '';
      case 'expired':
        return $localize`:@@bastion.tokens.expired:“${this.tokenName(h.token)}:description:” has expired.`;
      case 'soon':
        return $localize`:@@bastion.tokens.soon:“${this.tokenName(h.token)}:description:” expires ${this.ago(h.token.expiresAt!)}:time:.`;
      case 'ok':
        return h.next === null
          ? $localize`:@@bastion.tokens.never:None expires`
          : $localize`:@@bastion.tokens.next:The next expires ${this.ago(h.next)}:time:`;
    }
  });

  constructor() {
    inject(DestroyRef).onDestroy(() => this.secret.set(null));
    // A draft kept across re-authentication (R-UI-042) reopens the token dialog of its registration.
    let restored = false;
    effect(() => {
      const draft = this.draft();
      if (restored || !draft || draft.bastion !== this.bastion().name || !this.canWrite()) return;
      restored = true;
      untracked(() => {
        this.notifier.status($localize`:@@bastion.draft-restored:Your entries were restored. Issue the token again.`);
        void this.issue(draft);
      });
    });
  }

  /** The viewer folds or opens the card; the choice is kept for the next visit (Q-UI-1640). */
  protected toggle(): void {
    const open = !this.open();
    this.open.set(open);
    this.fold.set(this.bastion().name, open);
  }

  protected expiry(t: BastionTokenSummary): 'expired' | 'soon' | 'ok' {
    return tokenExpiry(t, this.now());
  }

  /** "pkgs-eu.example.com · 2 replicas": the head of a deployment's rows. */
  protected groupLabel(d: Deployment): string {
    const host = d.host || $localize`:@@bastion.replicas.no-base-url:No base URL reported`;
    return d.contacts.length === 1
      ? $localize`:@@bastion.replicas.group-one:${host}:host: · 1 replica`
      : $localize`:@@bastion.replicas.group:${host}:host: · ${d.contacts.length}:count: replicas`;
  }

  /** A token as the screen names it: the declared one by the screen's words, every other by its description. */
  protected tokenName(t: BastionTokenSummary): string {
    return t.declared ? this.helmTokenName : t.description;
  }

  /** "1 minute ago", "in 3 weeks": the one relative form of the card's times (Q-UI-431). */
  private ago(iso: string): string {
    return relativeTime(iso, this.locale, this.now());
  }

  /** "20 minutes", "3 hours": how long since `iso`, in whole minutes, hours or days. */
  private duration(iso: string): string {
    const seconds = Math.max(60, Math.floor((this.now() - Date.parse(iso)) / 1000));
    const unit = seconds >= 86_400 ? 86_400 : seconds >= 3_600 ? 3_600 : 60;
    return formatDuration(Math.floor(seconds / unit) * unit, this.locale);
  }

  protected secretNote(s: BastionTokenCreated): string {
    return $localize`:@@bastion.secret-note:For ${this.bastion().name}:name:: ${s.description}:description:`;
  }

  /** **Issue token** while the registration has none, **Rotate token** once it has one (Q-UI-1642): a new token beside the old. */
  protected async issue(draft?: BastionTokenDraft): Promise<void> {
    const ref = this.dialog.open<BastionTokenDialog, BastionTokenDialogData, BastionTokenCreated>(BastionTokenDialog, {
      data: { name: this.bastion().name, rotate: this.bastion().tokens.length > 0, draft },
      maxWidth: '36rem',
    });
    const created = await firstValueFrom(ref.closed);
    if (!created) return;
    this.secret.set(created);
    this.changed.emit();
  }

  /** **Disable…**: the dialog asks soft or hard (R-BAS-133); the Bastion is told over its connection. */
  protected async disable(): Promise<void> {
    const name = this.bastion().name;
    const ref = this.dialog.open<BastionDisableDialog, BastionDisableDialogData, BastionDisableMode>(BastionDisableDialog, {
      data: { name },
      maxWidth: '36rem',
    });
    const mode = await firstValueFrom(ref.closed);
    if (!mode) return;
    await this.setDisabled(mode, $localize`:@@bastion.disabled-done:${name}:name: is being disabled.`);
  }

  /** **Disable hard…** while a soft disable lets downloads finish: they are cut (R-BAS-133). */
  protected async disableHard(): Promise<void> {
    const name = this.bastion().name;
    const running = this.bastion().running;
    const answer = await this.confirmer.ask({
      title: $localize`:@@bastion.disable-hard-title:Disable ${name}:name: hard?`,
      lines: [
        running === 1
          ? $localize`:@@bastion.disable-hard-text-one:1 running download is cut.`
          : $localize`:@@bastion.disable-hard-text:${running}:count: running downloads are cut.`,
      ],
      confirmLabel: $localize`:@@bastion.disable-hard-confirm:Disable hard`,
    });
    if (answer === null) return;
    await this.setDisabled('hard', $localize`:@@bastion.disabled-done:${name}:name: is being disabled.`);
  }

  protected async enable(): Promise<void> {
    const name = this.bastion().name;
    await this.setDisabled(null, $localize`:@@bastion.enabled-done:${name}:name: is enabled.`);
  }

  private async setDisabled(disabled: BastionDisableMode | null, done: string): Promise<void> {
    await this.act(async () => {
      const { etag } = await this.api.get(this.bastion().name);
      await this.api.setDisabled(this.bastion().name, disabled, etag);
    }, done);
  }

  protected async remove(): Promise<void> {
    const name = this.bastion().name;
    const answer = await this.confirmer.ask({
      title: $localize`:@@bastion.delete-title:Delete this Bastion?`,
      lines: [$localize`:@@bastion.delete-text:${name}:name: and its tokens are removed; the tokens stop working at once. This cannot be undone.`],
      confirmLabel: $localize`:@@bastion.delete-confirm:Delete`,
      typeToConfirm: name,
    });
    if (answer === null) return;
    await this.act(() => this.api.delete(name), $localize`:@@bastion.deleted:Bastion deleted.`);
  }

  /** **Invalidate** a token, the declared one included (R-BAS-133): it stops working at once, for good. */
  protected async invalidate(t: BastionTokenSummary): Promise<void> {
    const description = this.tokenName(t);
    const lines = [
      $localize`:@@bastion.invalidate-text:“${description}:description:” stops working at once on every replica, for good. A replica that still uses it can no longer reach the main installation.`,
    ];
    if (t.declared) {
      lines.push($localize`:@@bastion.invalidate-helm:The next rollout stops at its migration until the Secret of bastions.declared holds a new value.`);
    }
    const answer = await this.confirmer.ask({
      title: $localize`:@@bastion.invalidate-title:Invalidate this token?`,
      lines,
      confirmLabel: $localize`:@@bastion.invalidate-confirm:Invalidate`,
    });
    if (answer === null) return;
    await this.act(() => this.api.invalidateToken(this.bastion().name, t.id), $localize`:@@bastion.invalidated:Token invalidated.`);
  }

  private async act(run: () => Promise<void>, done: string): Promise<void> {
    this.busy.set(true);
    this.actionError.set(null);
    try {
      await run();
      this.notifier.status(done);
      this.changed.emit();
    } catch (e) {
      this.actionError.set(asApiError(e));
    } finally {
      this.busy.set(false);
    }
  }
}
