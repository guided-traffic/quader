import { CdkMenu, CdkMenuItem, CdkMenuTrigger } from '@angular/cdk/menu';
import { Component, computed, effect, inject, input, signal, untracked } from '@angular/core';
import { RouterLink } from '@angular/router';
import { firstValueFrom } from 'rxjs';
import { newIdempotencyKey } from '../../../api/client';
import { DeliveryRequest, DeliveryRequestAllow, DeliveryRequestsApi, DeliveryRequestState } from '../../../api/delivery-requests-api';
import { ApiError } from '../../../api/problem';
import { Notifier } from '../../../core/notifier';
import { SessionStore } from '../../../core/session-store';
import { ErrorNotice } from '../../../shared/error-notice';
import { asApiError } from '../../../shared/paged-resource';
import { QuaderDialog } from '../../../shared/ui/dialog';
import { TimeAgo } from '../../../shared/ui/time-ago';
import { versionCommands } from '../../packages/package-links';
import { downloadStateText, failureLine } from '../downloads/download-words';
import { AllowDialog, AllowDialogData, DeclineDialog, DeclineDialogData } from './request-dialogs';
import { UpstreamActions } from './upstream-actions';
import { FailureToasts } from '../../../shared/failure-dialog';

/** How many entries the panel shows at first. */
const PANEL_PAGE = 20;

/**
 * The requests of the tab **Upstream** (remote-curation.md R-CUR-015, R-CUR-018), set apart in a bordered panel with its count: for
 * those who may set rules the open entries of the repository, each with its subject, who asked (and their notes) or how often clients
 * were refused, its examination with the versions it stored and a link to their verdicts, and **Allow…** and **Decline…** in the
 * row's menu; for anyone else **Your requests**, each with its state, the reason of a decline and **Withdraw** while it is open. The
 * panel is not shown while it has nothing to list.
 */
@Component({
  selector: 'quader-delivery-requests-panel',
  imports: [CdkMenu, CdkMenuItem, CdkMenuTrigger, ErrorNotice, RouterLink, TimeAgo],
  template: `
    @if (failure(); as f) {
      <quader-error-notice [error]="f" (retry)="load(true)" />
    } @else if (rows().length > 0) {
      <section class="requests" aria-labelledby="upstream-requests-title" data-testid="upstream-requests">
        <h2 id="upstream-requests-title" class="requests-title">
          @if (actions.manager()) {
            <ng-container i18n="@@requests.title">Requests</ng-container>
          } @else {
            <ng-container i18n="@@requests.title-mine">Your requests</ng-container>
          }
          @if (shownCount(); as n) {
            &ngsp;<span class="badge">{{ n }}</span>
          }
        </h2>
        <ul class="request-list">
          @for (r of rows(); track r.id) {
            <li class="request">
              <div class="request-main">
                <p class="request-subject">
                  <span class="subject-name" translate="no">{{ r.name }}</span>
                  @if (r.version) {
                    &ngsp;<span class="subject-version" translate="no">{{ r.version }}</span>
                  } @else {
                    &ngsp;<span class="muted" i18n="@@requests.every-version">every version</span>
                  }
                  @if (!actions.manager()) {
                    &ngsp;<span class="badge">{{ stateText(r.state) }}</span>
                  }
                </p>
                <p class="request-facts dot-separated">
                  @if (askedText(r); as a) {
                    <span>{{ a }}</span>
                  }
                  @if (r.refusals > 0) {
                    <span i18n="@@requests.refusals">{r.refusals, plural, =1 {refused to clients once} other {refused to clients {{ r.refusals }} times}}</span>
                  }
                  <span><quader-time-ago [at]="r.lastAt" absolute="title" /></span>
                </p>
                <p class="request-facts dot-separated">
                  @if (r.examined.length > 0) {
                    @for (x of r.examined; track x.version) {
                      <span
                        ><ng-container i18n="@@requests.examined">Examined <code translate="no">{{ x.version }}</code></ng-container>&ngsp;<a [routerLink]="versionLink(r.name, x.version)" i18n="@@requests.verdicts">Verdicts</a></span
                      >
                    }
                  }
                  @if (r.examination; as e) {
                    @if (e.state !== 'done' || r.examined.length === 0) {
                      <span
                        ><ng-container i18n="@@requests.examination">Examination</ng-container>: {{ downloadState(e.state) }}@if (e.failure; as f) {&ngsp;·&ngsp;{{ failureText(f) }}}</span
                      >
                    }
                  } @else if (r.examinationRefused; as why) {
                    <span><ng-container i18n="@@requests.examination-refused">Not examined</ng-container>: {{ why }}</span>
                  } @else if (r.examined.length === 0) {
                    <span i18n="@@requests.not-examined">Not examined</span>
                  }
                </p>
                @if (actions.manager()) {
                  @for (a of notes(r); track $index) {
                    <p class="request-note"><span class="note-by">{{ a.name }}</span>:&ngsp;<span translate="no">{{ a.note }}</span></p>
                  }
                }
                @if (r.state === 'declined' && r.declineReason) {
                  <p class="request-note"><ng-container i18n="@@requests.declined-because">Declined</ng-container>:&ngsp;<span translate="no">{{ r.declineReason }}</span></p>
                }
              </div>
              @if (actions.manager() && r.state === 'open') {
                <button
                  class="btn btn-tertiary btn-icon on-line request-menu"
                  type="button"
                  [disabled]="busy().has(r.id)"
                  [attr.aria-label]="menuLabel(r)"
                  [cdkMenuTriggerFor]="requestMenu"
                >
                  <span class="material-icons" aria-hidden="true">more_vert</span>
                </button>
                <ng-template #requestMenu>
                  <div class="menu" cdkMenu>
                    <button class="menu-item" type="button" cdkMenuItem (cdkMenuItemTriggered)="allow(r)" i18n="@@requests.allow">Allow…</button>
                    <button class="menu-item" type="button" cdkMenuItem (cdkMenuItemTriggered)="decline(r)" i18n="@@requests.decline">Decline…</button>
                  </div>
                </ng-template>
              } @else if (!actions.manager() && r.state === 'open' && mine(r)) {
                <button class="btn btn-tertiary on-line" type="button" [disabled]="busy().has(r.id)" (click)="withdraw(r)">
                  <ng-container i18n="@@requests.withdraw">Withdraw</ng-container><span class="visually-hidden" translate="no">&ngsp;{{ subject(r) }}</span>
                </button>
              }
            </li>
          }
        </ul>
        @if (next() !== null) {
          <button class="btn btn-default" type="button" [disabled]="loading()" (click)="load(false)" i18n="@@requests.more">Load more</button>
        }
      </section>
    }
  `,
  styles: `
    :host {
      display: block;
    }
    // Set apart: a bordered panel with its count (R-CUR-018).
    .requests {
      margin-block-end: var(--quader-space-4);
      padding: var(--quader-space-3) var(--quader-space-4);
      border: 1px solid var(--quader-border);
      border-radius: var(--quader-radius-m);
    }
    .requests-title {
      display: flex;
      align-items: flex-start;
      gap: var(--quader-space-2);
      margin: 0 0 var(--quader-space-2);
      font-size: var(--quader-text-size);
      font-weight: 600;
    }
    .request-list {
      margin: 0;
      padding: 0;
      list-style: none;
    }
    .request {
      display: flex;
      align-items: flex-start;
      gap: var(--quader-space-3);
      padding-block: var(--quader-space-2);
      border-block-start: 1px solid var(--quader-border);

      &:first-child {
        border-block-start: 0;
      }
    }
    .request-main {
      flex: 1 1 auto;
      min-inline-size: 0;
    }
    // A line of facts keeps its inline margins: .dot-separated moves itself by the width of a dot.
    .request-main p {
      margin-block: 0;
      overflow-wrap: anywhere;
    }
    .request-facts {
      display: flex;
      flex-wrap: wrap;
      color: var(--quader-text-muted);
    }
    .subject-name {
      font-weight: 600;
    }
    .request-note {
      color: var(--quader-text-muted);
    }
    .note-by {
      font-weight: 600;
    }
    .request-menu {
      flex: none;
    }
  `,
})
export class DeliveryRequestsPanel {
  private readonly api = inject(DeliveryRequestsApi);
  private readonly notifier = inject(Notifier);
  private readonly failureToasts = inject(FailureToasts);
  private readonly dialog = inject(QuaderDialog);
  private readonly session = inject(SessionStore);
  protected readonly actions = inject(UpstreamActions);

  readonly project = input.required<string>();
  readonly format = input.required<string>();
  /** The repository's open entries as the capabilities count them, for those who may set rules. */
  readonly openCount = input<number | null>(null);
  /**
   * The tab's counts as it read them last (R-UI-213): when they change (a request came in, an allow queued a download), the entries
   * are read again in place.
   */
  readonly revision = input('');

  protected readonly rows = signal<readonly DeliveryRequest[]>([]);
  protected readonly next = signal<string | null>(null);
  protected readonly loading = signal(false);
  protected readonly failure = signal<ApiError | null>(null);
  protected readonly busy = signal<ReadonlySet<string>>(new Set());
  private generation = 0;

  private readonly self = computed(() => this.session.whoami()?.principal.id ?? null);
  /** The count of the panel's title: the open entries for those who decide, the listed ones for an asker. */
  protected readonly shownCount = computed(() => (this.actions.manager() ? (this.openCount() ?? this.rows().length) : this.rows().length));

  protected readonly downloadState = downloadStateText;

  constructor() {
    effect(() => {
      const repository = this.actions.repository();
      const known = this.actions.known();
      this.actions.manager();
      this.actions.changed();
      if (!repository || !known) return;
      untracked(() => void this.load(true));
    });
    let shown: string | null = null;
    effect(() => {
      const revision = this.revision();
      untracked(() => {
        if (shown !== null && shown !== revision && !this.loading()) void this.load(true);
        shown = revision;
      });
    });
  }

  async load(fresh: boolean): Promise<void> {
    const generation = fresh ? ++this.generation : this.generation;
    const manager = this.actions.manager();
    this.loading.set(true);
    try {
      const page = await this.api.list(
        this.actions.repository(),
        manager ? { state: 'open' } : { mine: true },
        fresh ? null : this.next(),
        PANEL_PAGE,
      );
      if (generation !== this.generation) return;
      const items = manager ? page.items : page.items.filter((r) => r.state !== 'withdrawn');
      this.rows.update((rows) => (fresh ? items : [...rows, ...items]));
      this.next.set(page.nextCursor);
      this.failure.set(null);
    } catch (e) {
      if (generation === this.generation) this.failure.set(asApiError(e));
    } finally {
      if (generation === this.generation) this.loading.set(false);
    }
  }

  protected failureText(f: Parameters<typeof failureLine>[0]): string {
    return failureLine(f).label;
  }

  protected subject(r: DeliveryRequest): string {
    return r.version ? `${r.name} ${r.version}` : r.name;
  }

  protected stateText(state: DeliveryRequestState): string {
    switch (state) {
      case 'open':
        return $localize`:@@requests.state.open:Open`;
      case 'allowed':
        return $localize`:@@requests.state.allowed:Allowed`;
      case 'declined':
        return $localize`:@@requests.state.declined:Declined`;
      case 'withdrawn':
        return $localize`:@@requests.state.withdrawn:Withdrawn`;
    }
  }

  /** Who asked: "asked by 2 people" for those who decide, "asked by you" for the asker; nothing for an entry only clients made. */
  protected askedText(r: DeliveryRequest): string | null {
    if (r.askerCount <= 0) return null;
    if (!this.actions.manager()) return $localize`:@@requests.asked-by-you:asked by you`;
    const name = r.askers[0]?.name;
    if (r.askerCount === 1 && name) return $localize`:@@requests.asked-by-one:asked by ${name}:name:`;
    return r.askerCount === 1
      ? $localize`:@@requests.asked-by-a-person:asked by 1 person`
      : $localize`:@@requests.asked-by-many:asked by ${r.askerCount}:count: people`;
  }

  /** The askers' notes, for those who decide. */
  protected notes(r: DeliveryRequest): readonly { readonly name: string; readonly note: string }[] {
    return r.askers.filter((a) => !!a.note).map((a) => ({ name: a.name ?? '', note: a.note ?? '' }));
  }

  protected mine(r: DeliveryRequest): boolean {
    return r.askers.some((a) => a.id !== null && a.id === this.self());
  }

  protected menuLabel(r: DeliveryRequest): string {
    return $localize`:@@requests.menu:Actions for ${this.subject(r)}:subject:`;
  }

  protected versionLink(name: string, version: string): string[] {
    return versionCommands(this.project(), this.format(), name, version);
  }

  private async act<T>(r: DeliveryRequest, call: () => Promise<T>, done: string): Promise<void> {
    if (this.busy().has(r.id)) return;
    this.busy.update((s) => new Set([...s, r.id]));
    try {
      await call();
      this.notifier.status(done);
      this.actions.changed.update((n) => n + 1);
    } catch (e) {
      this.failureToasts.alert(e);
    } finally {
      this.busy.update((s) => {
        const next = new Set(s);
        next.delete(r.id);
        return next;
      });
    }
  }

  protected async allow(r: DeliveryRequest): Promise<void> {
    const ref = this.dialog.open<AllowDialog, AllowDialogData, DeliveryRequestAllow>(AllowDialog, { data: { name: r.name, version: r.version } });
    const choice = await firstValueFrom(ref.closed);
    if (!choice) return;
    await this.act(
      r,
      () => this.api.allow(this.actions.repository(), r.id, choice, newIdempotencyKey()),
      $localize`:@@requests.allowed:${this.subject(r)}:subject: is allowed; its download starts.`,
    );
  }

  protected async decline(r: DeliveryRequest): Promise<void> {
    const ref = this.dialog.open<DeclineDialog, DeclineDialogData, { reason: string; discard: boolean }>(DeclineDialog, { data: { subject: this.subject(r) } });
    const choice = await firstValueFrom(ref.closed);
    if (!choice) return;
    await this.act(
      r,
      () => this.api.decline(this.actions.repository(), r.id, choice, newIdempotencyKey()),
      $localize`:@@requests.declined:The request for ${this.subject(r)}:subject: is declined.`,
    );
  }

  protected async withdraw(r: DeliveryRequest): Promise<void> {
    await this.act(
      r,
      () => this.api.withdraw(this.actions.repository(), r.id, newIdempotencyKey()),
      $localize`:@@requests.withdrawn:Your request for ${this.subject(r)}:subject: is withdrawn.`,
    );
  }
}
