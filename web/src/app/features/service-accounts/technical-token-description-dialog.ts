import { Component, inject, signal } from '@angular/core';
import { ApiError } from '../../api/problem';
import { AccountAddress, TechnicalToken, TechnicalTokensApi } from '../../api/technical-tokens-api';
import { ErrorNotice } from '../../shared/error-notice';
import { asApiError } from '../../shared/paged-resource';
import { DIALOG_DATA, DialogClose, DialogRef, DialogTitle } from '../../shared/ui/dialog';

export interface TechnicalTokenDescriptionData {
  readonly account: AccountAddress;
  readonly token: TechnicalToken;
}

/**
 * **Edit description** of a technical token's row (G5-51, R-AUTH-045): the one field of the token that changes, read with its `ETag`
 * as the dialog opens and saved with it, so that a change somebody made meanwhile is refused (412) instead of overwritten. Closes with
 * the description saved; the name stays.
 */
@Component({
  selector: 'quader-technical-token-description-dialog',
  imports: [DialogTitle, DialogClose, ErrorNotice],
  template: `
    <h2 quaderDialogTitle i18n="@@tt-desc.title">Edit description</h2>
    <div class="dialog-body">
      <p class="hint">
        <span i18n="@@tt-desc.of">Of the technical token</span>&ngsp;<code translate="no">{{ data.token.name }}</code>.
      </p>
      <div class="field">
        <label class="field-label" for="tt-description" i18n="@@tt-desc.label">Description (optional)</label>
        <input
          id="tt-description"
          class="text-input field-w-name"
          autocomplete="off"
          maxlength="1000"
          [value]="description()"
          (input)="description.set($any($event.target).value)"
          (keydown.enter)="save()"
        />
      </div>
      @if (readFailure(); as f) {
        <quader-error-notice [error]="f" (retry)="read()" />
      } @else if (failure(); as f) {
        <quader-error-notice [error]="f" />
      }
    </div>
    <div class="dialog-actions">
      <button class="btn btn-tertiary" type="button" quaderDialogClose i18n="@@tt-desc.cancel">Cancel</button>
      <button class="btn btn-confirm" type="button" [disabled]="busy() || unchanged() || readFailure() !== null" (click)="save()" i18n="@@tt-desc.save">Save changes</button>
    </div>
  `,
  styles: `
    /* The token's name is the dialog's context, not the field's hint: a step apart from the field's label (the render of G5-51). */
    .dialog-body > .hint {
      margin-block: 0 var(--quader-space-3);
    }
    .field {
      display: flex;
      flex-direction: column;
      gap: var(--quader-space-1);
    }
  `,
})
export class TechnicalTokenDescriptionDialog {
  protected readonly data = inject<TechnicalTokenDescriptionData>(DIALOG_DATA);
  private readonly ref = inject<DialogRef<string, TechnicalTokenDescriptionDialog>>(DialogRef);
  private readonly api = inject(TechnicalTokensApi);

  protected readonly description = signal(this.data.token.description ?? '');
  protected readonly busy = signal(true);
  protected readonly failure = signal<ApiError | null>(null);
  /**
   * The read of the token failed: its notice offers Retry and reads it again by itself, and stays while it does (R-UI-216); nothing
   * is saved without the version the read gives.
   */
  protected readonly readFailure = signal<ApiError | null>(null);
  /** The token's version as the dialog opened: the save sends it in `If-Match`. */
  private etag = '';

  constructor() {
    void this.read();
  }

  protected async read(): Promise<void> {
    this.busy.set(true);
    try {
      const current = await this.api.get(this.data.account, this.data.token.name);
      this.readFailure.set(null);
      this.etag = current.etag;
      this.description.set(current.value.description ?? '');
    } catch (e) {
      this.readFailure.set(asApiError(e));
    } finally {
      this.busy.set(false);
    }
  }

  protected unchanged(): boolean {
    return this.description().trim() === (this.data.token.description ?? '');
  }

  protected async save(): Promise<void> {
    if (this.busy() || this.unchanged() || this.readFailure()) return;
    this.busy.set(true);
    this.failure.set(null);
    try {
      const saved = await this.api.update(this.data.account, this.data.token.name, this.description().trim(), this.etag);
      this.ref.close(saved.value.description ?? '');
    } catch (e) {
      this.failure.set(asApiError(e));
    } finally {
      this.busy.set(false);
    }
  }
}
