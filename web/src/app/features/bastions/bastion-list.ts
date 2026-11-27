import { Component, computed, inject, signal } from '@angular/core';
import { LIVE_FAST_MS, LIVE_IDLE_MS, live } from '../../core/live-refresh';
import { QuaderDialog } from '../../shared/ui/dialog';
import { firstValueFrom } from 'rxjs';
import { Bastion, BastionsApi } from '../../api/bastions-api';
import { DownloadLinkConnection, DownloadLinksApi } from '../../api/download-links-api';
import { ApiError } from '../../api/problem';
import { SessionStore } from '../../core/session-store';
import { DraftStore } from '../../shared/drafts';
import { Needs } from '../../shared/access/needs';
import { ErrorNotice } from '../../shared/error-notice';
import { asApiError } from '../../shared/paged-resource';
import { BastionCard } from './bastion-card';
import { BastionFold } from './bastion-fold';
import { catalogueLine } from './bastion-state';
import { BastionCreateDialog, BastionTokenDraft } from './bastion-dialogs';
import { PageHeader } from '../../shared/ui/page-header';
import { EmptyState } from '../../shared/ui/empty-state';
import { Loading } from '../../shared/ui/loading';

/** A state the page reads fast while it lasts (R-UI-213): a web interface waiting for its first build, a disable waiting for its replicas. */
function fastState(b: Bastion): boolean {
  return catalogueLine(b)?.kind === 'pending' || b.state === 'disabling' || b.state === 'disableUnconfirmed';
}

/**
 * Administration → Bastions (R-BAS-007): the registrations of the Quader Bastions in front of this
 * installation, under the header's one sentence (Q-UI-1627), each as a card with its settings, its replicas, its tokens and the actions
 * (R-BAS-004), which folds to its head (Q-UI-1640): open where the page holds one Bastion, folded where it holds several unless it
 * warns. There are at most `bastions.max_registrations` registrations, so the list is one page and the screen holds all of them. A
 * registration the caller may not change shows no action; the API decides in any case (R-UI-005). While a registration's direct reads
 * from the object store are on, the page reads the connections' states once for the cards' reasons (R-BAS-071).
 */
@Component({
  selector: 'quader-bastion-list',
  imports: [Loading, EmptyState, PageHeader, BastionCard, ErrorNotice, Needs],
  template: `
    <!-- One sentence under the heading (Q-UI-569, Q-UI-1627): the manual says how to register one. -->
    <quader-page-header
      heading="Bastions"
      i18n-heading="@@bastions.heading"
      description="A read-only cache of this installation for clients outside your network."
      i18n-description="@@bastions.description"
    >
      <button actions class="btn btn-confirm" type="button" [disabled]="!canWrite()" aria-describedby="needs-bastions" (click)="add()" i18n="@@bastions.add">New Bastion</button>
    </quader-page-header>
    <quader-needs id="needs-bastions" capability="endpoints:write" />

    @if (error(); as e) {
      <quader-error-notice [error]="e" (retry)="load()" />
    }
    @if (!loaded()) {
      @if (!error()) {
        <quader-loading size="page" i18n="@@bastions.loading">Loading Bastions.</quader-loading>
      }
    } @else if (items().length === 0) {
      <!-- The action that fills the list is the header's New Bastion, once (Q-UI-565). -->
      <quader-empty-state text="There is no Bastion yet." i18n-text="@@bastions.empty" />
    } @else {
      @for (b of items(); track b.name) {
        <quader-bastion-card [bastion]="b" [single]="items().length === 1" [draft]="restored()" [connections]="connections()" (changed)="load()" />
      }
    }
  `,
  styleUrl: '../../shared/access/access.scss',
})
export class BastionList {
  private readonly api = inject(BastionsApi);
  private readonly links = inject(DownloadLinksApi);
  private readonly dialog = inject(QuaderDialog);
  private readonly session = inject(SessionStore);
  private readonly fold = inject(BastionFold);

  protected readonly items = signal<readonly Bastion[]>([]);
  protected readonly loaded = signal(false);
  protected readonly error = signal<ApiError | null>(null);
  protected readonly canWrite = computed(() => this.session.has('endpoints:write'));
  /** The entries of a token dialog kept across a re-authentication (R-UI-042), taken once; the card of its registration reopens the dialog. */
  protected readonly restored = signal<BastionTokenDraft | null>(inject(DraftStore).take<BastionTokenDraft>());
  /**
   * The connections' states of the download link settings (storage.md R-STOR-096), which the cards explain their direct reads with
   * (R-BAS-071): the reasons where they are on, and before they are switched on the line that an encrypted connection hands each file's
   * key; read once per load of the list while a registration exists and the viewer holds `settings:read`, and null otherwise or when
   * they cannot be read: the cards then show the switch alone.
   */
  protected readonly connections = signal<readonly DownloadLinkConnection[] | null>(null);

  constructor() {
    void this.load();
    // A Bastion's state (seen, its handovers) changes without the viewer: read again every minute in place (R-UI-213), every few
    // seconds while a web interface waits for its first build or a disable waits for its replicas (R-BAS-133: the badge turns from
    // Disabling to Disabled about a second after the last download); a card keeps a token it shows once, as with every load.
    live({
      reload: async () => {
        if (!this.loaded()) return;
        const items = await this.api.list();
        this.items.set(items);
        await this.loadConnections(items);
      },
      every: () => (this.items().some((b) => fastState(b)) ? LIVE_FAST_MS : LIVE_IDLE_MS),
    });
  }

  /** Reads the list again; the cards stay on screen meanwhile, so a card keeps a token it shows once. */
  protected async load(): Promise<void> {
    let items: readonly Bastion[];
    try {
      items = await this.api.list();
      this.error.set(null);
      this.items.set(items);
      this.loaded.set(true);
    } catch (e) {
      this.error.set(asApiError(e));
      return;
    }
    await this.loadConnections(items);
  }

  private async loadConnections(items: readonly Bastion[]): Promise<void> {
    if (!this.session.has('settings:read') || items.length === 0) {
      this.connections.set(null);
      return;
    }
    try {
      this.connections.set((await this.links.settings()).settings.connections);
    } catch {
      this.connections.set(null);
    }
  }

  protected async add(): Promise<void> {
    const ref = this.dialog.open<BastionCreateDialog, undefined, Bastion>(BastionCreateDialog, { maxWidth: '36rem' });
    const created = await firstValueFrom(ref.closed);
    if (!created) return;
    // The card of a Bastion just created opens: its next step, the token, is in it (Q-UI-1640).
    this.fold.set(created.name, true);
    await this.load();
  }
}
