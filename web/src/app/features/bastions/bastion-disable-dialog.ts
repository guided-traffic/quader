import { Component, inject, signal } from '@angular/core';
import { BastionDisableMode } from '../../api/bastions-api';
import { DIALOG_DATA, DialogClose, DialogRef, DialogTitle } from '../../shared/ui/dialog';

/** What the dialog is opened with: the Bastion's name. */
export interface BastionDisableDialogData {
  readonly name: string;
}

/**
 * **Disable…** of a Bastion's head (R-UI-240, bastion.md R-BAS-133, Q-BAS-512): the two ways as a radio group, each with its one
 * sentence, **Soft** chosen; then what follows for its clients. It answers the chosen way, or nothing when cancelled; the card sends it.
 * Soft lets the downloads that have started finish, hard cuts them too; the Bastion is told over its connection within seconds and its
 * token stays valid.
 */
@Component({
  selector: 'quader-bastion-disable-dialog',
  imports: [DialogTitle, DialogClose],
  template: `
    <h2 quaderDialogTitle i18n="@@bastion.disable-title">Disable <span translate="no">{{ data.name }}</span>?</h2>
    <div class="dialog-body">
      <fieldset class="ways">
        <legend class="visually-hidden" i18n="@@bastion.disable.way">How to disable it</legend>
        <label class="way">
          <input type="radio" name="bastion-disable-way" value="soft" [checked]="mode() === 'soft'" aria-describedby="bastion-disable-soft" (change)="mode.set('soft')" />
          <span class="text">
            <span class="name" i18n="@@bastion.disable.soft">Soft</span>
            <span class="muted" id="bastion-disable-soft" i18n="@@bastion.disable.soft-text">No new requests. Downloads that have started finish.</span>
          </span>
        </label>
        <label class="way">
          <input type="radio" name="bastion-disable-way" value="hard" [checked]="mode() === 'hard'" aria-describedby="bastion-disable-hard" (change)="mode.set('hard')" />
          <span class="text">
            <span class="name" i18n="@@bastion.disable.hard">Hard</span>
            <span class="muted" id="bastion-disable-hard" i18n="@@bastion.disable.hard-text">No new requests, and running downloads are cut.</span>
          </span>
        </label>
      </fieldset>
      <p i18n="@@bastion.disable-text">Its replicas stop within seconds. Clients get 503 Service Unavailable until you enable it again.</p>
    </div>
    <div class="dialog-actions">
      <button class="btn btn-tertiary" type="button" quaderDialogClose i18n="@@bastion.disable-cancel">Cancel</button>
      <button class="btn btn-confirm" type="button" (click)="confirm()" i18n="@@bastion.disable-confirm">Disable</button>
    </div>
  `,
  styleUrl: '../../shared/access/access.scss',
  styles: `
    .ways {
      min-inline-size: 0;
      margin: 0;
      padding: 0;
      border: 0;
    }
    /* The radio, and beside it the way's name with its sentence under it, as the visibility levels stand (Q-UI-228). */
    .way {
      display: flex;
      align-items: flex-start;
      gap: var(--quader-space-2);
      margin-block-end: var(--quader-space-3);
    }
    .way > input {
      flex: none;
    }
    .text {
      display: flex;
      flex-direction: column;
    }
    .name {
      font-weight: 600;
    }
    .muted {
      font-size: var(--quader-text-muted-size);
    }
  `,
})
export class BastionDisableDialog {
  protected readonly data = inject<BastionDisableDialogData>(DIALOG_DATA);
  private readonly ref = inject<DialogRef<BastionDisableMode, BastionDisableDialog>>(DialogRef);

  /** Soft first: it is the way that breaks nothing that runs. */
  protected readonly mode = signal<BastionDisableMode>('soft');

  protected confirm(): void {
    this.ref.close(this.mode());
  }
}
