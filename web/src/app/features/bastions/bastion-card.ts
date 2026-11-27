import { NoValue } from '../../shared/ui/no-value';
import { TimePipe } from '../../shared/time-pipe';
import { Component, DestroyRef, LOCALE_ID, computed, effect, inject, input, output, signal, untracked } from '@angular/core';
import { QuaderDialog } from '../../shared/ui/dialog';
import { firstValueFrom } from 'rxjs';
import { Bastion, BastionsApi, BastionTokenCreated, BastionTokenSummary } from '../../api/bastions-api';
import { ApiError } from '../../api/problem';
import { Notifier } from '../../core/notifier';
import { SessionStore } from '../../core/session-store';
import { formatDuration } from '../../shared/access/duration';
import { TokenSecret } from '../../shared/access/token-secret';
import { Confirm } from '../../shared/confirm-dialog';
import { ErrorNotice } from '../../shared/error-notice';
import { asApiError } from '../../shared/paged-resource';
import { DataTable, TableColumn } from '../../shared/ui/data-table';
import { TimeAgo } from '../../shared/ui/time-ago';
import { DownloadLinkConnection } from '../../api/download-links-api';
import { BastionTokenDialog, BastionTokenDialogData, BastionTokenDraft } from './bastion-dialogs';
import { BastionHandover } from './bastion-handover';
import { BastionLimits } from './bastion-limits';
import { BastionSwitches } from './bastion-switches';
import { ADDRESS_MARKS, PartsText } from '../../shared/break-parts';
import { Busy } from '../../shared/ui/busy';

/** A token that expires within this many days carries the warning mark (R-BAS-007). */
export const BASTION_TOKEN_WARN_DAYS = 30;

const DAY_MS = 86_400_000;

/** The state of a token against `now`: expired, expiring within the warning window, or fine. */
export function tokenExpiry(token: Pick<BastionTokenSummary, 'expiresAt'>, now: number): 'expired' | 'soon' | 'ok' {
  // The declared token does not expire (R-BAS-127).
  if (!token.expiresAt) return 'ok';
  const left = Date.parse(token.expiresAt) - now;
  if (left <= 0) return 'expired';
  return left < BASTION_TOKEN_WARN_DAYS * DAY_MS ? 'soon' : 'ok';
}

/**
 * One Bastion registration (R-BAS-007): its facts and actions, the switch of its direct reads from the object
 * store (R-BAS-071, `<quader-bastion-handover>`), the switches of its web interface with what the main installation last built for it
 * (R-BAS-102, R-BAS-104, `<quader-bastion-switches>`, in plain view by Q-UI-1616), its limits, throughput and state with the addresses
 * its Bastion connects from (R-UI-240, R-UI-243, `<quader-bastion-limits>`), the last contact of each replica and its tokens. Disable
 * and enable read the registration for its `ETag` first, because the list carries none
 * (R-API-012). A token's value is shown once through `<quader-token-secret>` and lives in this card's
 * signal only (R-UI-043). Which actions show follows the capabilities of R-BAS-004: `endpoints:write`
 * for the registration and for issuing a token, `endpoints:write` or `tokens:revoke` for revoking one.
 */
@Component({
  selector: 'quader-bastion-card',
  imports: [Busy, BastionHandover, BastionLimits, BastionSwitches, TimePipe, DataTable, ErrorNotice, NoValue, PartsText, TimeAgo, TokenSecret],
  template: `
    <section class="card" [attr.aria-labelledby]="headingId()">
      <h2 [id]="headingId()" translate="no">{{ bastion().name }}</h2>
      <dl class="facts">
        <dt i18n="@@bastion.status">Status</dt>
        <dd>
          @if (bastion().enabled) {
            <span class="ok"><span class="material-icons" aria-hidden="true">check_circle</span> <ng-container i18n="@@bastion.enabled">Enabled</ng-container></span>
          } @else {
            <span class="bad"><span class="material-icons" aria-hidden="true">block</span> <ng-container i18n="@@bastion.disabled">Disabled</ng-container></span>
          }
        </dd>
        @if (bastion().description) {
          <dt i18n="@@bastion.description">Description</dt>
          <!-- What the administrator wrote, in their language: never translated (R-UI-214). -->
          <dd translate="no">{{ bastion().description }}</dd>
        }
        <dt i18n="@@bastion.created">Created</dt>
        <dd>{{ bastion().createdAt | time }}</dd>
      </dl>
      @if (bastion().declared; as d) {
        <!-- A registration the configuration declares (R-BAS-127, Q-UI-1497): the words of an account the configuration declares. -->
        <p class="source-note" role="note" data-testid="bastion-declared">
          <span class="material-icons inline" aria-hidden="true">lock</span>
          @if (d.secretName) {
            <span i18n="@@bastion.declared-helm"
              >From the Helm values, token from Secret <span class="with-stop"><code class="secret-name" translate="no">{{ d.secretName }}</code>.</span> Its description,
              addresses and that token are changed or removed there; to delete it, remove it there first.</span
            >
          } @else {
            <span i18n="@@bastion.declared-file"
              >From the installation's configuration file. Its description, addresses and declared token are changed or removed there; to
              delete it, remove it there first.</span
            >
          }
        </p>
      }

      @if (canWrite()) {
        <div class="row-actions">
          <button class="btn btn-confirm" type="button" [disabled]="busy()" [quaderBusy]="busy()" (click)="issue()">
            <ng-container i18n="@@bastion.issue">Issue token</ng-container>
            <span class="visually-hidden"> {{ bastion().name }}</span>
          </button>
          @if (bastion().enabled) {
            <button class="btn btn-tertiary" type="button" [disabled]="busy()" [quaderBusy]="busy()" (click)="disable()">
              <ng-container i18n="@@bastion.disable">Disable</ng-container>
              <span class="visually-hidden"> {{ bastion().name }}</span>
            </button>
          } @else {
            <button class="btn btn-tertiary" type="button" [disabled]="busy()" [quaderBusy]="busy()" (click)="enable()">
              <ng-container i18n="@@bastion.enable">Enable</ng-container>
              <span class="visually-hidden"> {{ bastion().name }}</span>
            </button>
          }
          <!-- A declared registration is not deleted here (Q-UI-1498): the next run of quader migrate would create it again. -->
          @if (!bastion().declared) {
            <button class="btn btn-tertiary" type="button" [disabled]="busy()" [quaderBusy]="busy()" (click)="remove()">
              <ng-container i18n="@@bastion.delete">Delete</ng-container>
              <span class="visually-hidden"> {{ bastion().name }}</span>
            </button>
          }
        </div>
      }

      @if (actionError(); as e) {
        <quader-error-notice [error]="e" />
      }
      @if (secret(); as s) {
        <quader-token-secret [value]="s.token" [description]="secretNote(s)" (closed)="secret.set(null)" />
      }

      <!-- The main settings, rows of name and control (Q-UI-1620, Q-UI-1621): what the internet sees first (Q-UI-1616), then the
           direct reads. -->
      <div class="main-settings">
        <quader-bastion-switches [bastion]="bastion()" (changed)="changed.emit()" />
        <quader-bastion-handover [bastion]="bastion()" [connections]="connections()" (changed)="changed.emit()" />
      </div>

      <quader-bastion-limits [bastion]="bastion()" (changed)="changed.emit()" />

      <h3 i18n="@@bastion.contacts">Last contact</h3>
      <quader-data-table
        [caption]="contactsCaption()"
        [columns]="contactColumns"
        [empty]="bastion().contacts.length === 0"
        i18n-emptyText="@@bastion.contacts-empty"
        emptyText="No replica has contacted the main installation with a token of this registration yet. Its replicas appear here after their first handshake."
      >
        @for (c of bastion().contacts; track c.replica) {
          <tr>
            <!-- The replica's name, and under it what is the same for most rows (Q-UI-959): the address it connected from, its
                 release and its interface version. A name of 63 characters breaks after its hyphens. -->
            <th scope="row" class="wrap-name replica">
              <span translate="no">{{ c.replica }}</span>
              <span class="secondary"
                ><span class="dot-separated" data-testid="bastion-contact-facts"
                  >@if (c.clientAddress; as v) {<span translate="no"><quader-parts-text [value]="v" [marks]="addressMarks" /></span>}<span
                    i18n="@@bastion.contact.release"
                    >Release <span translate="no">{{ c.release }}</span></span
                  ><span i18n="@@bastion.contact.interface">Interface version {{ c.interfaceVersion }}</span></span
                ></span
              >
            </th>
            <td><quader-time-ago [at]="c.contactAt" absolute="title" /></td>
            <!-- The URL takes the room the other columns leave and breaks after a dot or a slash there. -->
            <td translate="no" class="wrap base-url">
              @if (c.baseUrl; as v) {
                <quader-parts-text [value]="v" [marks]="addressMarks" />
              } @else {
                <quader-no-value />
              }
            </td>
          </tr>
        }
      </quader-data-table>

      <h3 i18n="@@bastion.tokens">Tokens</h3>
      <quader-data-table
        [caption]="tokensCaption()"
        [columns]="tokenColumns"
        [empty]="bastion().tokens.length === 0"
        i18n-emptyText="@@bastion.tokens-empty"
        emptyText="This registration has no token. Issue one to connect a Bastion."
      >
        @for (t of bastion().tokens; track t.id) {
          <tr>
            <!-- The description, and under it the hint and when the token was created (Q-UI-959). -->
            <th scope="row">
              <span translate="no">{{ t.description }}</span>
              <span class="secondary"
                ><span class="dot-separated"
                  ><code translate="no">{{ t.hint }}</code
                  ><span i18n="@@bastion.token.created">Created <quader-time-ago [at]="t.createdAt" absolute="title" /></span
                  >@if (t.declared) {<span class="declared-token" data-testid="bastion-token-declared"
                      ><span class="material-icons inline" aria-hidden="true">lock</span>&ngsp;<ng-container i18n="@@bastion.token.declared"
                        >In the configuration</ng-container
                      ></span
                    >}</span
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
                  <span class="expiry-mark"><span class="badge warn"><span class="material-icons" aria-hidden="true">schedule</span><ng-container i18n="@@bastion.token.soon">Expires in</ng-container>&ngsp;{{ remaining(t) }}</span></span>
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
              <!-- The declared token ends with a change of its Secret (Q-UI-1499): no Revoke; the line under its name says why. -->
              @if (!t.declared && canRevoke()) {
                <button class="btn btn-tertiary" type="button" [disabled]="busy()" [quaderBusy]="busy()" (click)="revoke(t)">
                  <ng-container i18n="@@bastion.token.revoke">Revoke</ng-container>
                  <span class="visually-hidden"> {{ t.description }}</span>
                </button>
              }
            </td>
          </tr>
        }
      </quader-data-table>
    </section>
  `,
  styleUrl: '../../shared/access/access.scss',
  styles: `
    /* A replica's name (a pod name has up to 63 characters) breaks after its hyphens, an address (IPv6 has 39) and a reported base URL
       after their colons, dots and slashes (breakParts), and anywhere where a part is still longer than its room, so that no value
       widens its column: at 390 px a base URL of 38 characters pushed the page sideways, and at 1440 px it stacked one card's rows
       while the next card kept its table. */
    th.replica,
    td.base-url {
      overflow-wrap: anywhere;
    }
    /* A token's mark stands on a line of its own under the expiry: after a line break the badge, 20 px on a line of 16, touched the
       time above it. */
    .expiry-mark {
      display: block;
      margin-block-start: var(--quader-space-1);
    }
    /* The lock line of a declared registration, as an account the configuration declares has it (Q-UI-1161, Q-UI-1497): the sign keeps
       to the first line, and a long Secret's name breaks inside the sentence's measure. */
    .source-note {
      display: flex;
      align-items: flex-start;
      gap: var(--quader-space-1);
      max-inline-size: var(--quader-settings-measure);
      margin: var(--quader-space-2) 0 var(--quader-space-3);
    }
    .source-note .secret-name {
      overflow-wrap: anywhere;
    }
    .main-settings {
      margin-block: var(--quader-space-4);
    }
  `,
})
export class BastionCard {
  readonly bastion = input.required<Bastion>();
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

  protected readonly busy = signal(false);
  protected readonly actionError = signal<ApiError | null>(null);
  /** The value of a token that was just issued; card-local, never stored (R-UI-043). */
  protected readonly secret = signal<BastionTokenCreated | null>(null);

  protected readonly canWrite = computed(() => this.session.has('endpoints:write'));
  protected readonly canRevoke = computed(() => this.session.has('endpoints:write') || this.session.has('tokens:revoke'));
  protected readonly headingId = computed(() => `bastion-${this.bastion().name}`);
  protected readonly contactsCaption = computed(() => $localize`:@@bastion.contacts-caption:Last contact of the replicas of ${this.bastion().name}:name:`);
  protected readonly tokensCaption = computed(() => $localize`:@@bastion.tokens-caption:Tokens of ${this.bastion().name}:name:`);

  /*
   * The tables have one form at every width but the narrow one (Q-UI-959): each column declares its least width and its values fit it
   * or break inside it, so the rows stack by the card's width alone, never by a long value. What is the same for most rows stands
   * under the row's name, as the reference's list of runners does: a replica's address, release and interface version, a token's
   * hint and creation. The reported base URL and a token's description take the room the others leave. A time is relative, its date
   * and time its title (Q-UI-431).
   */
  protected readonly contactColumns: readonly TableColumn[] = [
    {
      id: 'replica',
      label: $localize`:@@bastion.col.replica:Replica`,
      help: $localize`:@@bastion.help.replica:Under its name: the address the main installation saw the replica connect from, its release, and the version of the interface between the Bastion and this installation, on which both sides must agree.`,
      width: '16rem',
    },
    { id: 'time', label: $localize`:@@bastion.col.time:Time`, width: '7.5rem' },
    {
      id: 'base',
      label: $localize`:@@bastion.col.base:Reported base URL`,
      help: $localize`:@@bastion.help.base:The address the Bastion says clients reach it on. Client setup uses it when it is listed in server.additional_base_urls.`,
      width: '10rem',
      prefer: true,
    },
  ];
  protected readonly tokenColumns: readonly TableColumn[] = [
    { id: 'description', label: $localize`:@@bastion.col.token:Token`, width: '9rem', prefer: true },
    { id: 'expires', label: $localize`:@@bastion.col.expires:Expires`, width: '11rem' },
    {
      id: 'lastUsed',
      label: $localize`:@@bastion.col.last-used:Last used`,
      help: $localize`:@@bastion.help.last-used:The last time the token authenticated a request. When you renew, revoke the old token once the new one shows a recent time.`,
      width: '7.5rem',
    },
    { id: 'actions', label: $localize`:@@bastion.col.actions:Actions`, hideLabel: true },
  ];

  /**
   * A URL's or an address's parts, each ending after a dot, a slash or a colon, where a narrow column may break it (`<wbr>`: nothing
   * is added to a copy). Broken anywhere instead, an IPv6 address left its dot of the facts' line alone on a line above it.
   */
  /** An address breaks after its dots, slashes and colons, each piece whole while it fits (`PartsText`). */
  protected readonly addressMarks = ADDRESS_MARKS;

  private readonly now = computed(() => {
    this.bastion();
    return Date.now();
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

  protected expiry(t: BastionTokenSummary): 'expired' | 'soon' | 'ok' {
    return tokenExpiry(t, this.now());
  }

  protected remaining(t: BastionTokenSummary): string {
    if (!t.expiresAt) return '';
    const seconds = Math.max(60, Math.ceil((Date.parse(t.expiresAt) - this.now()) / 1000));
    // Whole days, rounded up, while a day or more is left; else whole minutes.
    return formatDuration(seconds >= 86_400 ? Math.ceil(seconds / 86_400) * 86_400 : Math.ceil(seconds / 60) * 60, this.locale);
  }

  protected secretNote(s: BastionTokenCreated): string {
    return $localize`:@@bastion.secret-note:For ${this.bastion().name}:name:: ${s.description}:description:`;
  }

  protected async issue(draft?: BastionTokenDraft): Promise<void> {
    const ref = this.dialog.open<BastionTokenDialog, BastionTokenDialogData, BastionTokenCreated>(BastionTokenDialog, {
      data: { name: this.bastion().name, draft },
      maxWidth: '36rem',
    });
    const created = await firstValueFrom(ref.closed);
    if (!created) return;
    this.secret.set(created);
    this.changed.emit();
  }

  protected async disable(): Promise<void> {
    const answer = await this.confirmer.ask({
      title: $localize`:@@bastion.disable-title:Disable this Bastion?`,
      lines: [
        $localize`:@@bastion.disable-text:The tokens of ${this.bastion().name}:name: are refused until you enable it again. Its replicas treat the main installation as unreachable and serve from their cache within the grace period.`,
      ],
      confirmLabel: $localize`:@@bastion.disable-confirm:Disable`,
    });
    if (answer === null) return;
    await this.setEnabled(false, $localize`:@@bastion.disabled-done:Bastion disabled.`);
  }

  protected async enable(): Promise<void> {
    await this.setEnabled(true, $localize`:@@bastion.enabled-done:Bastion enabled.`);
  }

  private async setEnabled(enabled: boolean, done: string): Promise<void> {
    await this.act(async () => {
      const { etag } = await this.api.get(this.bastion().name);
      await this.api.setEnabled(this.bastion().name, enabled, etag);
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

  protected async revoke(t: BastionTokenSummary): Promise<void> {
    const answer = await this.confirmer.ask({
      title: $localize`:@@bastion.revoke-title:Revoke this token?`,
      lines: [
        $localize`:@@bastion.revoke-text:“${t.description}:description:” stops working at once on every replica. A replica that still uses it treats the main installation as unreachable. When you renew, revoke the old token only after the new one shows a recent time in Last used. This cannot be undone.`,
      ],
      confirmLabel: $localize`:@@bastion.revoke-confirm:Revoke`,
    });
    if (answer === null) return;
    await this.act(() => this.api.revokeToken(this.bastion().name, t.id), $localize`:@@bastion.revoked:Token revoked.`);
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
