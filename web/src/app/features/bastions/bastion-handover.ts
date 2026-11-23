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

/** Why a Bastion cannot read a connection's files from the object store, as the clause of its line (storage.md R-STOR-096). */
export function handoverReasonText(reason: DownloadLinkReason): string {
  const detail = reason.detail ?? '';
  switch (reason.code) {
    case 'not_https':
      return $localize`:@@bastion.handover.reason.not-https:its address does not use https`;
    case 'quader_host':
      return $localize`:@@bastion.handover.reason.quader-host:its address lies under ${detail}:host:, an address of Quader`;
    case 'encrypted':
      return $localize`:@@bastion.handover.reason.encrypted:the connection is encrypted`;
    default:
      return $localize`:@@bastion.handover.reason.link-check:its object store did not answer a test link as expected (${detail}:detail:)`;
  }
}

/** One connection whose files a Bastion gets through the main installation, and whether the storage settings can change that. */
export interface HandoverLine {
  readonly connection: string;
  readonly text: string;
  /** An address or a link check the administrator can set right in the storage settings; an encrypted connection is not. */
  readonly fixable: boolean;
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
 * when every connection can serve a handover (Q-BAS-295). An encrypted connection's files are streamed by the main installation until
 * its data keys are handed over (package 4 of docs/developer/bastion-handover.md, Q-BAS-296), whatever its link check says.
 */
export function handoverLines(connections: readonly DownloadLinkConnection[]): HandoverLine[] {
  const out: HandoverLine[] = [];
  for (const c of connections) {
    if (c.encrypted) {
      out.push({ connection: c.name, text: lineOf(c.name, [handoverReasonText({ code: 'encrypted', detail: null })]), fixable: false });
    } else if (!c.handoverUsable) {
      out.push({ connection: c.name, text: lineOf(c.name, c.handoverReasons.map(handoverReasonText)), fixable: true });
    }
  }
  return out;
}

/**
 * The switch **Direct reads from the object store** of a registration's card (R-BAS-071, the handover per blob): the plain path is the
 * switch and one sentence. Who may change a registration (`endpoints:write`) switches it, which reads the registration for its `ETag`
 * (the list carries none) and sends the merge patch `{"handover": …}` with it; a refusal, the `412` of a change made in between
 * included, stands under the sentence and the switch keeps the state the server has. Who may only read sees the state as a word.
 * While it is on, and only where it helps, a line per connection whose files a Bastion gets through the main installation says why,
 * with one link to the storage settings where an address can change that (Q-BAS-295); `connections` is the answer of
 * `GET /api/v1/download-links/settings`, null where the viewer may not read it, so nothing is shown.
 */
@Component({
  selector: 'quader-bastion-handover',
  imports: [ErrorNotice, RouterLink, Switch],
  template: `
    <h3 i18n="@@bastion.handover.heading">Direct reads from the object store</h3>
    <div class="state">
      @if (canWrite()) {
        <quader-switch [checked]="on()" [label]="switchLabel()" [busy]="busy()" [describedBy]="noteId()" (checkedChange)="set($event)" />
      } @else {
        <span class="word" data-testid="bastion-handover-state">{{ word() }}</span>
      }
    </div>
    <p class="hint note" [id]="noteId()" i18n="@@bastion.handover.note">
      Large files go from the object store to this Bastion by a link per file; the main installation decides and counts every download as
      before.
    </p>
    @if (error(); as e) {
      <quader-error-notice [error]="e" />
    }
    @if (lines().length > 0) {
      <div class="reasons" data-testid="bastion-handover-reasons">
        @for (l of lines(); track l.connection) {
          <p class="hint reason">
            @if (l.fixable) {
              <span class="material-icons inline warn" aria-hidden="true">warning</span>
            } @else {
              <span class="material-icons inline" aria-hidden="true">lock</span>
            }{{ l.text }}
          </p>
        }
        @if (fixable()) {
          <p class="hint">
            <a routerLink="/admin/settings/storage" fragment="details" i18n="@@bastion.handover.settings">Public addresses in the storage settings</a>
          </p>
        }
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
  protected readonly word = computed(() => (this.on() ? $localize`:@@switch.on:On` : $localize`:@@switch.off:Off`));
  protected readonly lines = computed(() => {
    const connections = this.connections();
    return this.on() && connections ? handoverLines(connections) : [];
  });
  protected readonly fixable = computed(() => this.lines().some((l) => l.fixable));

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
