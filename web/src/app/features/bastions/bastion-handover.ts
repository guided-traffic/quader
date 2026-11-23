import { Component, computed, inject, input, linkedSignal, output, signal } from '@angular/core';
import { RouterLink } from '@angular/router';
import { Bastion, BastionsApi } from '../../api/bastions-api';
import { DownloadLinkConnection, DownloadLinkReason } from '../../api/download-links-api';
import { ApiError } from '../../api/problem';
import { Notifier } from '../../core/notifier';
import { SessionStore } from '../../core/session-store';
import { ErrorNotice } from '../../shared/error-notice';
import { asApiError } from '../../shared/paged-resource';
import { Switch } from '../../shared/ui/switch';

/**
 * Why a Bastion cannot read a connection's files from the object store, as the clause of its line (storage.md R-STOR-096): the
 * reasons of `handoverReasons`, which never name `encrypted`, since an encrypted connection's files are read with each file's key
 * (R-STOR-095, Q-BAS-300).
 */
export function handoverReasonText(reason: DownloadLinkReason): string {
  const detail = reason.detail ?? '';
  switch (reason.code) {
    case 'not_https':
      return $localize`:@@bastion.handover.reason.not-https:its address does not use https`;
    case 'quader_host':
      return $localize`:@@bastion.handover.reason.quader-host:its address lies under ${detail}:host:, an address of Quader`;
    default:
      return $localize`:@@bastion.handover.reason.link-check:its object store did not answer a test link as expected (${detail}:detail:)`;
  }
}

/** One connection whose files a Bastion gets through the main installation, with the reasons of its line. */
export interface HandoverLine {
  readonly connection: string;
  readonly text: string;
}

function lineOf(name: string, reasons: readonly string[]): string {
  if (reasons.length === 0) {
    return $localize`:@@bastion.handover.line-plain:A Bastion gets the files of ${name}:name: through the main installation.`;
  }
  const why = reasons.join('; ');
  return $localize`:@@bastion.handover.line:A Bastion gets the files of ${name}:name: through the main installation: ${why}:reasons:.`;
}

/**
 * The connections whose files a Bastion does not read from the object store, each with its line, in the order of the answer; empty
 * when every connection can serve a handover (Q-BAS-295). A plain and an encrypted connection are judged alike, by `handoverUsable`
 * and `handoverReasons`: an encrypted connection's files are read with each file's key (R-STOR-095, Q-BAS-300).
 */
export function handoverLines(connections: readonly DownloadLinkConnection[]): HandoverLine[] {
  return connections
    .filter((c) => !c.handoverUsable)
    .map((c) => ({ connection: c.name, text: lineOf(c.name, c.handoverReasons.map(handoverReasonText)) }));
}

/**
 * The switch **Direct reads from the object store** of a registration's card (R-BAS-071, the handover per blob): the plain path is the
 * switch and one sentence. Who may change a registration (`endpoints:write`) switches it, which reads the registration for its `ETag`
 * (the list carries none) and sends the merge patch `{"handover": …}` with it; a refusal, the `412` of a change made in between
 * included, stands under the sentence and the switch keeps the state the server has. Who may only read gets the state as a sentence,
 * "**Direct reads from the object store** are on.", as the settings pages give a switch to an auditor (Q-UI-960).
 * While it is on, and only where it helps, a line per connection whose files a Bastion gets through the main installation says why,
 * with one link to the storage settings, where its address and test link stand, a plain or an encrypted connection's alike (Q-BAS-295,
 * Q-BAS-300); `connections` is the answer of
 * `GET /api/v1/download-links/settings`, null where the viewer may not read it, so nothing is shown.
 */
@Component({
  selector: 'quader-bastion-handover',
  imports: [ErrorNotice, RouterLink, Switch],
  template: `
    @if (canWrite()) {
      <h3 i18n="@@bastion.handover.heading">Direct reads from the object store</h3>
      <div class="state">
        <quader-switch [checked]="on()" [label]="switchLabel()" [busy]="busy()" [describedBy]="noteId()" (checkedChange)="set($event)" />
      </div>
    } @else {
      <!-- Who may only read gets the state as the settings pages give it to an auditor (Q-UI-960): the switch's name and its state. -->
      <p class="state-text" data-testid="bastion-handover-state">
        @if (on()) {
          <ng-container i18n="@@bastion.handover.state-on"><strong>Direct reads from the object store</strong> are on.</ng-container>
        } @else {
          <ng-container i18n="@@bastion.handover.state-off"><strong>Direct reads from the object store</strong> are off.</ng-container>
        }
      </p>
    }
    <p class="hint note" [id]="noteId()" i18n="@@bastion.handover.note">
      Large files go from the object store to this Bastion by a link per file, and for an encrypted store with that file's key; the main
      installation decides and counts every download as before.
    </p>
    @if (error(); as e) {
      <quader-error-notice [error]="e" />
    }
    @if (lines().length > 0) {
      <div class="reasons" data-testid="bastion-handover-reasons">
        @for (l of lines(); track l.connection) {
          <p class="hint reason"><span class="material-icons inline warn" aria-hidden="true">warning</span>{{ l.text }}</p>
        }
        <p class="hint">
          <a routerLink="/admin/settings/storage" fragment="details" i18n="@@bastion.handover.settings">Public addresses in the storage settings</a>
        </p>
      </div>
    }
  `,
  styles: `
    :host {
      display: block;
    }
    .state {
      display: flex;
      align-items: flex-start;
    }
    /* Where the heading stands for who may change it: the card's sections keep their distance. */
    .state-text {
      margin-block: var(--quader-space-5) 0;
    }
    .note,
    .reason {
      max-inline-size: var(--quader-settings-measure);
    }
    .reasons p {
      margin-block: var(--quader-space-2) 0;
    }
    quader-error-notice {
      display: block;
      margin-block-start: var(--quader-space-2);
    }
  `,
})
export class BastionHandover {
  readonly bastion = input.required<Bastion>();
  /** The connections' states of `GET /api/v1/download-links/settings`; null where they were not read. */
  readonly connections = input<readonly DownloadLinkConnection[] | null>(null);
  /** The registration changed: the page reads the list again. */
  readonly changed = output<void>();

  private readonly api = inject(BastionsApi);
  private readonly notifier = inject(Notifier);
  private readonly session = inject(SessionStore);

  protected readonly busy = signal(false);
  protected readonly error = signal<ApiError | null>(null);
  /** The state the server answered last; it follows the list's registration whenever that is read again. */
  protected readonly on = linkedSignal(() => this.bastion().handover);

  protected readonly canWrite = computed(() => this.session.has('endpoints:write'));
  protected readonly noteId = computed(() => `bastion-handover-note-${this.bastion().name}`);
  protected readonly switchLabel = computed(
    () => $localize`:@@bastion.handover.switch-label:Direct reads from the object store for ${this.bastion().name}:name:`,
  );
  protected readonly lines = computed(() => {
    const connections = this.connections();
    return this.on() && connections ? handoverLines(connections) : [];
  });

  protected async set(handover: boolean): Promise<void> {
    if (this.busy()) return;
    this.busy.set(true);
    this.error.set(null);
    try {
      const name = this.bastion().name;
      const { etag } = await this.api.get(name);
      const { value } = await this.api.setHandover(name, handover, etag);
      this.on.set(value.handover);
      this.notifier.status(
        value.handover
          ? $localize`:@@bastion.handover.on-done:Direct reads from the object store switched on.`
          : $localize`:@@bastion.handover.off-done:Direct reads from the object store switched off.`,
      );
      this.changed.emit();
    } catch (e) {
      this.error.set(asApiError(e));
    } finally {
      this.busy.set(false);
    }
  }
}
