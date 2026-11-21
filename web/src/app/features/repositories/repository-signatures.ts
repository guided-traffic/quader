import { Component, computed, effect, input, output, signal, untracked } from '@angular/core';
import { ApiError } from '../../api/problem';
import { ErrorNotice } from '../../shared/error-notice';
import { Checkbox } from '../../shared/ui/checkbox';
import { Disclosure } from '../../shared/ui/disclosure';

/**
 * The detail setting of a remote repository (R-SIG-026, R-SIG-040, Q-SIG-108): the closed section **Signatures** of its settings page,
 * never part of the creation form. One check box, **Fetch the upstream's signatures**, checked by default: Quader asks the upstream in
 * the background for the signatures of what the remote stored, so that the badge is right before any client asks. Unchecked for an
 * upstream whose rate limits are tight, such as Docker Hub without an account, where fetching a signature counts as a pull. Saved on its
 * own with the page's `ETag`.
 */
@Component({
  selector: 'quader-repository-signatures',
  imports: [Checkbox, Disclosure, ErrorNotice],
  template: `
    <quader-disclosure
      i18n-heading="@@repo.signatures.heading"
      heading="Signatures"
      i18n-hint="@@repo.signatures.hint"
      hint="Whether Quader fetches the upstream's signatures of what this repository stores."
      [note]="dirty() ? unsavedNote : ''"
    >
      <form (submit)="submit($event)" novalidate>
        @if (failure(); as e) {
          <quader-error-notice [error]="e" />
        }
        <quader-checkbox [checked]="fetch()" [disabled]="!canChange() || saving()" [describedBy]="'repo-signatures-note'" (checkedChange)="fetch.set($event)">
          <ng-container i18n="@@repo.signatures.fetch">Fetch the upstream's signatures</ng-container>
        </quader-checkbox>
        <div id="repo-signatures-note" class="notes">
          <p class="hint" i18n="@@repo.signatures.what">
            Quader asks the upstream in the background for the signatures of what this repository stores, at most 600 requests an hour,
            so that the badge is right before any client asks. A client's own request for signatures passes through either way.
          </p>
          <p class="hint" i18n="@@repo.signatures.off">
            Switch it off for an upstream with tight rate limits: on Docker Hub without an account, fetching a signature counts as a pull.
          </p>
        </div>
        @if (canChange()) {
          <div class="actions">
            <button class="btn btn-confirm" type="submit" [disabled]="!dirty() || saving()">
              <ng-container i18n="@@repo.settings.save">Save changes</ng-container>
              <span class="visually-hidden" i18n="@@repo.signatures.save-for"> (signatures)</span>
            </button>
            @if (dirty()) {
              <button class="btn btn-tertiary" type="button" (click)="fetch.set(stored())" i18n="@@repo.edit.reset">Discard changes</button>
            }
          </div>
        }
      </form>
    </quader-disclosure>
  `,
  styles: `
    .notes p {
      margin-block: var(--quader-space-1) 0;
    }
    .actions {
      display: flex;
      flex-wrap: wrap;
      gap: var(--quader-space-2);
      margin-block-start: var(--quader-space-4);
    }
  `,
})
export class RepositorySignaturesSection {
  /** `signatures.fetch` as stored; absent is on. */
  readonly stored = input.required<boolean>();
  /** The caller may write the repository's settings, and the repository is not being deleted. */
  readonly canChange = input(false);
  readonly saving = input(false);
  readonly failure = input<ApiError | null>(null);
  readonly save = output<boolean>();

  protected readonly fetch = signal(true);
  protected readonly dirty = computed(() => this.fetch() !== this.stored());
  protected readonly unsavedNote = $localize`:@@repo.settings.unsaved:Unsaved changes`;

  constructor() {
    effect(() => {
      const stored = this.stored();
      untracked(() => this.fetch.set(stored));
    });
  }

  protected submit(event: Event): void {
    event.preventDefault();
    if (this.dirty() && !this.saving()) this.save.emit(this.fetch());
  }
}
