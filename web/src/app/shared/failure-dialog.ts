import { Component, Injectable, inject } from '@angular/core';
import { ApiError } from '../api/problem';
import { asFailure } from '../core/http/failure';
import { Notifier } from '../core/notifier';
import { ErrorNotice } from './error-notice';
import { failureSentence } from './failure-report';
import { DIALOG_DATA, DialogClose, DialogTitle, QuaderDialog } from './ui/dialog';

/**
 * The whole notice of a failure that a compact place said in one sentence (R-UI-216, Q-UI-1187): a toast's **Details**, a row of an
 * upload. The same notice as on a page, with its facts, its folded part and **Copy report**; nothing is read again from here.
 */
@Component({
  selector: 'quader-failure-dialog',
  imports: [DialogTitle, DialogClose, ErrorNotice],
  template: `
    <h2 quaderDialogTitle i18n="@@failure.dialog.title">What failed</h2>
    <div class="dialog-body">
      <quader-error-notice [error]="error" />
    </div>
    <div class="dialog-actions">
      <button class="btn btn-default" type="button" quaderDialogClose i18n="@@failure.dialog.close">Close</button>
    </div>
  `,
})
export class FailureDialog {
  protected readonly error = inject<ApiError>(DIALOG_DATA);
}

/**
 * A failure said in a toast (R-UI-042, R-UI-216): its sentence, or the screen's own where it has a better one, and **Details**,
 * which opens the whole notice. The toasts of a failed action use it, so a toast is never the only place its facts stand.
 */
@Injectable({ providedIn: 'root' })
export class FailureToasts {
  private readonly notifier = inject(Notifier);
  private readonly dialog = inject(QuaderDialog);

  /** Says the failure in an alert toast with **Details**; `sentence` replaces the failure's own where the screen knows more. */
  alert(failure: unknown, sentence?: string): number {
    const error = asFailure(failure);
    return this.notifier.alert(sentence ?? failureSentence(error), {
      label: $localize`:@@failure.details:Details`,
      run: () => this.details(error),
    });
  }

  /** Opens the whole notice of a failure. */
  details(failure: unknown): void {
    this.dialog.open(FailureDialog, { data: asFailure(failure), maxWidth: '52rem' });
  }
}
