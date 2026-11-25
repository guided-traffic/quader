import { Component, computed, inject, signal } from '@angular/core';
import { DeliveryRequestAllow } from '../../../api/delivery-requests-api';
import { TextField } from '../../../shared/access/fields';
import { Checkbox } from '../../../shared/ui/checkbox';
import { DIALOG_DATA, DialogClose, DialogRef, DialogTitle } from '../../../shared/ui/dialog';

export interface AskDialogData {
  /** `name version`, or the name alone. */
  readonly subject: string;
  /** The request names one version, not the package. */
  readonly version: boolean;
}

/**
 * **Request** (R-CUR-015): what is asked for, what happens next, and an optional note for those who decide. Closes with the note
 * (`''` for none), or without a result when cancelled.
 */
@Component({
  selector: 'quader-ask-dialog',
  imports: [DialogClose, DialogTitle, TextField],
  template: `
    <h2 quaderDialogTitle i18n="@@upstream.ask.title">Request {{ data.subject }}</h2>
    <form (submit)="submit($event)" novalidate>
      <div class="dialog-body">
        <p>
          @if (data.version) {
            <ng-container i18n="@@upstream.ask.version">Those who may allow it in this repository see your request. Once allowed, the version is delivered here.</ng-container>
          } @else {
            <ng-container i18n="@@upstream.ask.package">Those who may allow it in this repository see your request. Once allowed, the package is delivered here with every version, future ones included.</ng-container>
          }
        </p>
        <quader-text-field
          width="full"
          type="textarea"
          i18n-label="@@upstream.ask.note"
          label="Why you need it (optional)"
          name="note"
          [(value)]="note"
          i18n-hint="@@upstream.ask.note-hint"
          hint="Read by those who decide, and by you."
        />
      </div>
      <div class="dialog-actions">
        <button class="btn btn-tertiary" type="button" quaderDialogClose i18n="@@upstream.dialog.cancel">Cancel</button>
        <button class="btn btn-confirm" type="submit" i18n="@@upstream.ask.submit">Send request</button>
      </div>
    </form>
  `,
})
export class AskDialog {
  protected readonly data = inject<AskDialogData>(DIALOG_DATA);
  private readonly ref = inject<DialogRef<string, AskDialog>>(DialogRef);
  protected readonly note = signal('');

  protected submit(event: Event): void {
    event.preventDefault();
    this.ref.close(this.note().trim().slice(0, 500));
  }
}

export interface AllowDialogData {
  readonly name: string;
  /** The version the entry asks for; null for the package. */
  readonly version: string | null;
}

/**
 * **Allow** a request (R-CUR-015): as asked, or a version's entry widened to its package; with **Download every version** a bulk of the
 * package after the allow; an optional note of the rule. Closes with the choice, or without a result when cancelled.
 */
@Component({
  selector: 'quader-allow-dialog',
  imports: [Checkbox, DialogClose, DialogTitle, TextField],
  template: `
    <h2 quaderDialogTitle i18n="@@upstream.allow.title">Allow {{ subject() }}</h2>
    <form (submit)="submit($event)" novalidate>
      <div class="dialog-body">
        @if (data.version) {
          <fieldset class="plain choice">
            <legend class="field-label" i18n="@@upstream.allow.what">What is allowed</legend>
            <label class="mode-option">
              <input type="radio" name="as" [checked]="scope() === 'asked'" (change)="scope.set('asked')" />
              <span class="mode-title" i18n="@@upstream.allow.as-asked">This version</span>
              <span class="hint" i18n="@@upstream.allow.as-asked-hint">Only {{ subject() }} is delivered.</span>
            </label>
            <label class="mode-option">
              <input type="radio" name="as" [checked]="scope() === 'package'" (change)="scope.set('package')" />
              <span class="mode-title" i18n="@@upstream.allow.as-package">Every version</span>
              <span class="hint" i18n="@@upstream.allow.as-package-hint">{{ data.name }} is delivered with every version, future ones included.</span>
            </label>
          </fieldset>
        } @else {
          <p i18n="@@upstream.allow.package-text">{{ data.name }} is delivered with every version, future ones included.</p>
        }
        <quader-checkbox [checked]="every()" (checkedChange)="every.set($event)">
          <ng-container i18n="@@upstream.allow.download-every">Download every version into the cache now</ng-container>
        </quader-checkbox>
        <p class="hint" i18n="@@upstream.allow.download-hint">Without it, what was examined is delivered at once and the version asked for is downloaded.</p>
        <quader-text-field
          width="full"
          type="textarea"
          i18n-label="@@upstream.allow.note"
          label="Note of the rule (optional)"
          name="note"
          [(value)]="note"
          i18n-hint="@@upstream.allow.note-hint"
          hint="Seen only by those who may read the rules."
        />
      </div>
      <div class="dialog-actions">
        <button class="btn btn-tertiary" type="button" quaderDialogClose i18n="@@upstream.dialog.cancel">Cancel</button>
        <button class="btn btn-confirm" type="submit" i18n="@@upstream.allow.submit">Allow</button>
      </div>
    </form>
  `,
  styles: `
    .choice {
      display: flex;
      flex-direction: column;
      gap: var(--quader-space-2);
      margin: 0 0 var(--quader-space-4);
      padding: 0;
      border: 0;

      /* Without the browser's 2 px of padding, the label starts where the fields' labels start (Allow at 1,440 and 390 px). */
      legend {
        margin-block-end: var(--quader-space-2);
        padding: 0;
      }
    }
    // A choice and its sentence, as the mode of a remote's mirror (mirror-settings.ts).
    .mode-option {
      display: grid;
      grid-template-columns: auto 1fr;
      column-gap: var(--quader-space-2);
      padding: var(--quader-space-3);
      border: 1px solid var(--quader-border);
      border-radius: var(--quader-radius-l);

      input {
        grid-row: 1 / span 2;
      }
      .hint {
        grid-column: 2;
        margin: 0;
      }
    }
    .mode-title {
      font-weight: 600;
    }
    p.hint {
      margin-block: var(--quader-space-1) var(--quader-space-3);
    }
  `,
})
export class AllowDialog {
  protected readonly data = inject<AllowDialogData>(DIALOG_DATA);
  private readonly ref = inject<DialogRef<DeliveryRequestAllow, AllowDialog>>(DialogRef);
  protected readonly scope = signal<'asked' | 'package'>('asked');
  protected readonly every = signal(false);
  protected readonly note = signal('');
  protected readonly subject = computed(() => (this.data.version ? `${this.data.name} ${this.data.version}` : this.data.name));

  protected submit(event: Event): void {
    event.preventDefault();
    const note = this.note().trim();
    this.ref.close({ as: this.data.version ? this.scope() : 'asked', downloadEveryVersion: this.every(), ...(note ? { note: note.slice(0, 1000) } : {}) });
  }
}

export interface DeclineDialogData {
  readonly subject: string;
}

/**
 * **Decline** a request (R-CUR-015): a reason of 1 to 1,000 characters, which the askers read, and whether what its examination stored
 * is discarded (on by default, R-CUR-016). Closes with both, or without a result when cancelled.
 */
@Component({
  selector: 'quader-decline-dialog',
  imports: [Checkbox, DialogClose, DialogTitle, TextField],
  template: `
    <h2 quaderDialogTitle i18n="@@upstream.decline.title">Decline {{ data.subject }}</h2>
    <form (submit)="submit($event)" novalidate>
      <div class="dialog-body">
        <quader-text-field
          width="full"
          type="textarea"
          i18n-label="@@upstream.decline.reason"
          label="Reason"
          name="reason"
          [(value)]="reason"
          [required]="true"
          [error]="error()"
          i18n-hint="@@upstream.decline.reason-hint"
          hint="Those who asked read it."
        />
        <quader-checkbox [checked]="discard()" (checkedChange)="discard.set($event)">
          <ng-container i18n="@@upstream.decline.discard">Discard what was fetched for examination</ng-container>
        </quader-checkbox>
      </div>
      <div class="dialog-actions">
        <button class="btn btn-tertiary" type="button" quaderDialogClose i18n="@@upstream.dialog.cancel">Cancel</button>
        <button class="btn btn-danger" type="submit" i18n="@@upstream.decline.submit">Decline</button>
      </div>
    </form>
  `,
})
export class DeclineDialog {
  protected readonly data = inject<DeclineDialogData>(DIALOG_DATA);
  private readonly ref = inject<DialogRef<{ reason: string; discard: boolean }, DeclineDialog>>(DialogRef);
  protected readonly reason = signal('');
  protected readonly discard = signal(true);
  protected readonly error = computed(() =>
    this.reason().trim() === '' ? $localize`:@@upstream.decline.reason-required:Say why, in a sentence.` : null,
  );

  protected submit(event: Event): void {
    event.preventDefault();
    const reason = this.reason().trim();
    if (reason === '') return;
    this.ref.close({ reason: reason.slice(0, 1000), discard: this.discard() });
  }
}
