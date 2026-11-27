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
import { catalogueLine } from './bastion-state';
import { BastionCreateDialog, BastionTokenDraft } from './bastion-dialogs';
import { PageHeader } from '../../shared/ui/page-header';
import { EmptyState } from '../../shared/ui/empty-state';
import { Loading } from '../../shared/ui/loading';

/**
 * Administration → Bastions (R-BAS-007): the registrations of the Quader Bastions in front of this
 * installation, each as a card with the last contact of every replica, its tokens and the actions
 * (R-BAS-004). There are at most `bastions.max_registrations` registrations, so the list is one page
 * and the screen holds all of them. A registration the caller may not change shows no action; the
 * API decides in any case (R-UI-005). While a registration's direct reads from the object store are
 * on, the page reads the connections' states once for the cards' reasons (R-BAS-071).
 */
@Component({
  selector: 'quader-bastion-list',
  imports: [Loading, EmptyState, PageHeader, BastionCard, ErrorNotice, Needs],
  template: `
    <quader-page-header heading="Bastions" i18n-heading="@@bastions.heading">
      <button actions class="btn btn-confirm" type="button" [disabled]="!canWrite()" aria-describedby="needs-bastions" (click)="add()" i18n="@@bastions.add">New Bastion</button>
    </quader-page-header>
    <quader-needs id="needs-bastions" capability="endpoints:write" />
    <p i18n="@@bastions.hint">
      A Bastion is a read-only cache of this installation for clients outside your network. Register it here, issue the token its chart reads, and check that every replica has made contact.
    </p>

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
        <quader-bastion-card [bastion]="b" [draft]="restored()" [connections]="connections()" (changed)="load()" />
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

  protected readonly items = signal<readonly Bastion[]>([]);
  protected readonly loaded = signal(false);
  protected readonly error = signal<ApiError | null>(null);
  protected readonly canWrite = computed(() => this.session.has('endpoints:write'));
  /** The entries of a token dialog kept across a re-authentication (R-UI-042), taken once; the card of its registration reopens the dialog. */
  protected readonly restored = signal<BastionTokenDraft | null>(inject(DraftStore).take<BastionTokenDraft>());
  /**
   * The connections' states of the download link settings (storage.md R-STOR-096), which the cards whose direct reads are on explain
   * themselves with (R-BAS-071); read once per load of the list, only while a registration has them on and the viewer holds
   * `settings:read`, and null otherwise or when they cannot be read: the cards then show the switch and its sentence alone.
   */
  protected readonly connections = signal<readonly DownloadLinkConnection[] | null>(null);

  constructor() {
    void this.load();
    // A Bastion's state (seen, its handovers) changes without the viewer: read again every minute in place (R-UI-213), every few
    // seconds while a web interface waits for its first build; a card keeps a token it shows once, as with every load.
    live({
      reload: async () => {
        if (!this.loaded()) return;
        const items = await this.api.list();
        this.items.set(items);
        await this.loadConnections(items);
      },
      every: () => (this.items().some((b) => catalogueLine(b)?.kind === 'pending') ? LIVE_FAST_MS : LIVE_IDLE_MS),
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
    if (!this.session.has('settings:read') || !items.some((b) => b.switches.handover)) {
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
    if (created) await this.load();
  }
}
