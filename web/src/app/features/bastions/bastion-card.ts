import { NoValue } from '../../shared/ui/no-value';
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
import { DataTable, TableColumn } from '../../shared/ui/data-table';
import { Disclosure } from '../../shared/ui/disclosure';
import { Help } from '../../shared/ui/help';
import { TimeAgo } from '../../shared/ui/time-ago';
import { relativeTime } from '../packages/format-time';
import { DownloadLinkConnection } from '../../api/download-links-api';
import { BastionTokenDialog, BastionTokenDialogData, BastionTokenDraft } from './bastion-dialogs';
import { BastionHandover } from './bastion-handover';
import { BastionLimits } from './bastion-limits';
import { BastionSwitches } from './bastion-switches';
import { ContactHealth, TokenHealth, contactHealth, tokenExpiry, tokenHealth } from './bastion-state';
import { ADDRESS_MARKS, PartsText } from '../../shared/break-parts';
import { Busy } from '../../shared/ui/busy';

/**
 * One Bastion registration (R-BAS-007, R-UI-240), top to bottom (Q-UI-1620): the head with the name, the state, the mark **Helm
 * configuration** of a declared registration with its help (Q-UI-1629) and the actions, the description under it; the main settings,
 * the rows of its web interface (`<quader-bastion-switches>`, R-BAS-102, R-BAS-104) and of its direct reads from the object store
 * (`<quader-bastion-handover>`, R-BAS-071); **Limits** with **Details** and, at their end, **Created** (`<quader-bastion-limits>`,
 * R-UI-240, R-UI-243); and the closed sections **Replicas** and **Tokens**, one line each, which turn into a warning and open by
 * themselves when a replica is silent or the replicas disagree (Q-UI-1622), or a token expires within 30 days or has expired (Q-UI-1623).
 * Disable and enable read the registration for its `ETag` first, because the list carries none (R-API-012). A token's value is shown
 * once through `<quader-token-secret>` and lives in this card's signal only (R-UI-043). Which actions show follows the capabilities of
 * R-BAS-004: `endpoints:write` for the registration and for issuing a token, `endpoints:write` or `tokens:revoke` for revoking one.
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
    NoValue,
    PartsText,
    TimeAgo,
    TokenSecret,
  ],
  template: `
    <section class="card" [attr.aria-labelledby]="headingId()">
      <div class="head">
        <div class="title">
          <h2 [id]="headingId()" translate="no">{{ bastion().name }}</h2>
          @if (!bastion().disabled) {
            <span class="ok state"><span class="material-icons" aria-hidden="true">check_circle</span> <ng-container i18n="@@bastion.enabled">Enabled</ng-container></span>
          } @else {
            <span class="bad state"><span class="material-icons" aria-hidden="true">block</span> <ng-container i18n="@@bastion.disabled">Disabled</ng-container></span>
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
          <div class="row-actions">
            <button class="btn btn-confirm" type="button" [disabled]="busy()" [quaderBusy]="busy()" (click)="issue()">
              <ng-container i18n="@@bastion.issue">Issue token</ng-container>
              <span class="visually-hidden"> {{ bastion().name }}</span>
            </button>
            @if (!bastion().disabled) {
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
      </div>
      @if (bastion().description) {
        <!-- What the administrator wrote, in their language: never translated (R-UI-214). -->
        <p class="description" translate="no" data-testid="bastion-description">{{ bastion().description }}</p>
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

      <quader-bastion-limits [bastion]="bastion()" (changed)="changed.emit()">
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
            @for (c of bastion().contacts; track c.replica) {
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
        </quader-disclosure>
      }

      <!-- The tokens, one line (Q-UI-1623): a token that expires within 30 days or has expired opens the section by itself. -->
      @if (tokens().kind === 'none') {
        <div class="section-plain" data-testid="bastion-tokens">
          <h3 i18n="@@bastion.tokens">Tokens</h3>
          <p class="muted" i18n="@@bastion.tokens-empty">No token yet: issue one for the Bastion's chart.</p>
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
                  <!-- The declared token ends with a change of its Secret (Q-UI-1499): no Revoke. -->
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
        </quader-disclosure>
      }
    </section>
  `,
  styleUrl: '../../shared/access/access.scss',
  styles: `
    /* The head: the name with its state and source on the left, the actions at its end; the actions take a line of their own where the
       card is narrow. The buttons stand on the middle of the name's line (R-UI-175). */
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
    .card .title h2 {
      margin: 0;
      overflow-wrap: anywhere;
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
    /* A section without entries: its heading and its one line where the fold would stand. */
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

  protected readonly helmName = $localize`:@@bastion.source.helm:Helm configuration`;
  protected readonly declaredHelp = $localize`:@@bastion.source.helm-help:Its description, its addresses and one token are set in bastions.declared of the Helm configuration and changed there. To delete the Bastion, remove it there first.`;
  /** The declared token's name, the screen's whatever the server stored (Q-UI-1629). */
  protected readonly helmTokenName = $localize`:@@bastion.token.helm:Token of the Helm configuration`;

  protected readonly canWrite = computed(() => this.session.has('endpoints:write'));
  protected readonly canRevoke = computed(() => this.session.has('endpoints:write') || this.session.has('tokens:revoke'));
  protected readonly headingId = computed(() => `bastion-${this.bastion().name}`);
  protected readonly contactsCaption = computed(() => $localize`:@@bastion.contacts-caption:Replicas of ${this.bastion().name}:name:`);
  protected readonly tokensCaption = computed(() => $localize`:@@bastion.tokens-caption:Tokens of ${this.bastion().name}:name:`);

  /*
   * The tables have one form at every width but the narrow one (Q-UI-959): each column declares its least width and its values fit it
   * or break inside it, so the rows stack by the card's width alone, never by a long value. What is the same for most rows stands
   * under the row's name, as the reference's list of runners does: a replica's address and version, a token's hint and creation. The
   * reported base URL and a token's description take the room the others leave. A time is relative, its date and time its title
   * (Q-UI-431).
   */
  protected readonly contactColumns: readonly TableColumn[] = [
    {
      id: 'replica',
      label: $localize`:@@bastion.col.replica:Replica`,
      help: $localize`:@@bastion.help.replica:Under its name: the address the main installation saw it connect from, and its version.`,
      width: '16rem',
    },
    { id: 'time', label: $localize`:@@bastion.col.time:Last contact`, width: '7.5rem' },
    {
      id: 'base',
      label: $localize`:@@bastion.col.base:Reported base URL`,
      help: $localize`:@@bastion.help.base:The address the Bastion says clients reach it on. Set up client offers it while it is in config.server.additional_base_urls of the Helm configuration.`,
      width: '10rem',
      prefer: true,
    },
  ];
  protected readonly tokenColumns: readonly TableColumn[] = [
    { id: 'description', label: $localize`:@@bastion.col.token:Token`, width: '9rem', prefer: true },
    // As wide as "in 12 months" and the mark "Expires soon" under it (Q-UI-1623); the mark's number made it 11 rem.
    { id: 'expires', label: $localize`:@@bastion.col.expires:Expires`, width: '8rem' },
    {
      id: 'lastUsed',
      label: $localize`:@@bastion.col.last-used:Last used`,
      help: $localize`:@@bastion.help.last-used:The last time the token authenticated a request. When you renew, revoke the old token once the new one shows a recent time.`,
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

  protected readonly contacts = computed<ContactHealth>(() => contactHealth(this.bastion(), this.now()));
  protected readonly tokens = computed<TokenHealth>(() => tokenHealth(this.bastion(), this.now()));
  protected readonly contactsWarn = computed(() => {
    const h = this.contacts();
    return (h.kind === 'silent' && h.warn) || (h.kind === 'current' && (h.releases.length > 1 || h.baseUrls));
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
        if (h.baseUrls) warnings.push($localize`:@@bastion.replicas.base-urls:The replicas report different base URLs.`);
        return warnings.length > 0
          ? warnings.join(' ')
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

  protected expiry(t: BastionTokenSummary): 'expired' | 'soon' | 'ok' {
    return tokenExpiry(t, this.now());
  }

  /** A token as the screen names it: the declared one by the screen's words, every other by its description. */
  private tokenName(t: BastionTokenSummary): string {
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
        $localize`:@@bastion.disable-text:Its replicas stop within seconds. Clients get 503 Service Unavailable until you enable it again.`,
      ],
      confirmLabel: $localize`:@@bastion.disable-confirm:Disable`,
    });
    if (answer === null) return;
    await this.setDisabled('soft', $localize`:@@bastion.disabled-done:Bastion disabled.`);
  }

  protected async enable(): Promise<void> {
    await this.setDisabled(null, $localize`:@@bastion.enabled-done:Bastion enabled.`);
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
