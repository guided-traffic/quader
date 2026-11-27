import { Component, computed, inject, signal } from '@angular/core';
import { ApiError } from '../../../api/problem';
import { StorageApi, StorageFinding } from '../../../api/storage-api';
import { TextField } from '../../../shared/access/fields';
import { ErrorNotice } from '../../../shared/error-notice';
import { fieldErrorAt } from '../../../shared/field-errors';
import { asApiError } from '../../../shared/paged-resource';
import { TimePipe } from '../../../shared/time-pipe';
import { Busy } from '../../../shared/ui/busy';
import { DIALOG_DATA, DialogClose, DialogRef, DialogTitle } from '../../../shared/ui/dialog';
import { formatSize } from '../../packages/format-size';
import { reasonWord, sourceWord } from './storage-history';

/** The longest note the API takes (R-STOR-111). */
export const ACKNOWLEDGE_NOTE_MAX = 500;

/**
 * **Acknowledge** a damaged copy (api-and-ui.md R-UI-265, Q-UI-1607; storage.md R-STOR-111): the dialog names the copy, says in one
 * sentence that acknowledging repairs nothing (Q-STOR-530), and takes an optional note that every reader of the page sees (H-2436). The
 * answer is the finding as acknowledged; a refusal of the API stays in the dialog with everything it carried (R-UI-216).
 */
@Component({
  selector: 'quader-storage-acknowledge-dialog',
  imports: [Busy, DialogClose, DialogTitle, ErrorNotice, TextField, TimePipe],
  template: `
    <h2 quaderDialogTitle i18n="@@storage.ack.title">Acknowledge the damaged copy?</h2>
    <form (submit)="submit($event)" novalidate>
      <div class="dialog-body">
        @if (error(); as e) {
          @if (noteError() === null) {
            <quader-error-notice [error]="e" />
          }
        }
        <dl class="facts">
          <dt i18n="@@storage.ack.copy">Copy</dt>
          <dd>
            <span translate="no">{{ finding.connection }}</span> · <code translate="no">{{ finding.sha256.slice(0, 12) }}</code> ·
            {{ size(finding.storedSize) }}
          </dd>
          <dt i18n="@@storage.ack.found">Found</dt>
          <dd>{{ finding.detectedAt | time }} · {{ source }} · {{ reason }}</dd>
        </dl>
        <p i18n="@@storage.ack.effect">Acknowledging records that you know of it. It repairs nothing: the finding stays open until its copy is written again, found again or deleted.</p>
        <quader-text-field
          width="full"
          i18n-label="@@storage.ack.note"
          label="Note"
          name="note"
          type="textarea"
          [(value)]="note"
          [error]="noteError()"
          [hint]="noteHint"
        />
      </div>
      <div class="dialog-actions">
        <button class="btn btn-tertiary" type="button" quaderDialogClose i18n="@@storage.ack.cancel">Cancel</button>
        <button class="btn btn-confirm" type="submit" [disabled]="busy()" [quaderBusy]="busy()" i18n="@@storage.ack.submit">Acknowledge</button>
      </div>
    </form>
  `,
  styles: `
    .facts {
      display: grid;
      grid-template-columns: max-content minmax(0, 1fr);
      gap: var(--quader-space-1) var(--quader-space-3);
      margin: 0 0 var(--quader-space-3);
    }
    .facts dt {
      color: var(--quader-text-muted);
    }
    .facts dd {
      margin: 0;
      overflow-wrap: anywhere;
    }
  `,
})
export class AcknowledgeDialog {
  protected readonly finding = inject<StorageFinding>(DIALOG_DATA);
  private readonly api = inject(StorageApi);
  private readonly ref = inject<DialogRef<StorageFinding, AcknowledgeDialog>>(DialogRef);

  protected readonly size = formatSize;
  protected readonly source = sourceWord(this.finding.source);
  protected readonly reason = reasonWord(this.finding.reason);
  protected readonly note = signal(this.finding.acknowledged?.note ?? '');
  protected readonly busy = signal(false);
  protected readonly error = signal<ApiError | null>(null);

  protected readonly noteHint = $localize`:@@storage.ack.note-hint:Optional, at most 500 characters. Every reader of this page sees it.`;
  protected readonly noteError = computed(() => {
    if (this.note().trim().length > ACKNOWLEDGE_NOTE_MAX) return $localize`:@@storage.ack.note-long:The note is longer than 500 characters.`;
    return fieldErrorAt(this.error(), '/note');
  });

  protected async submit(event: Event): Promise<void> {
    event.preventDefault();
    if (this.busy() || this.note().trim().length > ACKNOWLEDGE_NOTE_MAX) return;
    this.busy.set(true);
    this.error.set(null);
    try {
      this.ref.close(await this.api.acknowledge(this.finding.id, this.note()));
    } catch (e) {
      this.error.set(asApiError(e));
    } finally {
      this.busy.set(false);
    }
  }
}
