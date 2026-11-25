import { Component, Injectable, inject } from '@angular/core';
import { DIALOG_DATA, DialogClose, DialogTitle, QuaderDialog } from './ui/dialog';
import { firstValueFrom } from 'rxjs';
import { CompareField, CompareValue, ConflictData, diffFields } from './conflict-dialog';

export interface DiffData extends ConflictData {
  readonly title: string;
  readonly confirmLabel: string;
}

/** "What will change" before a save (R-EP-017): the stored values against the draft, field by field. */
@Component({
  selector: 'quader-diff-dialog',
  imports: [DialogTitle, DialogClose, CompareField, CompareValue],
  template: `
    <h2 quaderDialogTitle>{{ data.title }}</h2>
    <div class="dialog-body">
      @if (rows.length === 0) {
        <p i18n="@@diff.none">Nothing differs from the stored version.</p>
      } @else {
        <!-- The comparison's layout is the dialogs' (_dialog.scss, .compare): one line per field, or a block per field at 390 px. -->
        <div class="compare-region">
          <table class="compare">
            <caption class="visually-hidden" i18n="@@diff.caption">
              Fields that change
            </caption>
            <thead>
              <tr>
                <th scope="col" i18n="@@conflict.field">Field</th>
                <th scope="col" i18n="@@diff.stored">Stored now</th>
                <th scope="col" i18n="@@diff.new">After saving</th>
              </tr>
            </thead>
            <tbody>
              @for (row of rows; track row.pointer) {
                <tr>
                  <th scope="row"><quader-compare-field [row]="row" /></th>
                  <td data-label="Stored now" i18n-data-label="@@diff.stored"><quader-compare-value [value]="row.theirs" /></td>
                  <td data-label="After saving" i18n-data-label="@@diff.new"><quader-compare-value [value]="row.mine" /></td>
                </tr>
              }
            </tbody>
          </table>
        </div>
      }
    </div>
    <div class="dialog-actions">
      <button class="btn btn-tertiary" type="button" [quaderDialogClose]="false" i18n="@@diff.cancel">
        Cancel
      </button>
      <button class="btn btn-confirm"
        type="button"
        [disabled]="rows.length === 0"
        [quaderDialogClose]="true"
      >
        {{ data.confirmLabel }}
      </button>
    </div>
  `,
})
export class DiffDialog {
  protected readonly data = inject<DiffData>(DIALOG_DATA);
  protected readonly rows = diffFields(this.data);
}

@Injectable({ providedIn: 'root' })
export class DiffReview {
  private readonly dialog = inject(QuaderDialog);

  /** True when the user confirms the save. */
  async confirm(data: DiffData): Promise<boolean> {
    const ref = this.dialog.open<DiffDialog, DiffData, boolean>(DiffDialog, {
      data,
      maxWidth: '48rem',
      width: '48rem',
    });
    return (await firstValueFrom(ref.closed)) === true;
  }
}
