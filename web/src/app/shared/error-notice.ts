import { Component, computed, input, output } from '@angular/core';
import { ApiError } from '../api/problem';
import { describeProblem } from '../core/http/problem-messages';

/**
 * Renders a failed API call (R-UI-042): the translated message of the problem `code`, or the
 * server's English `detail` plus the request ID with a copy button for an unknown code
 * ("give this ID to your administrator"). A 503 and a lost connection offer "Retry".
 */
@Component({
  selector: 'quader-error-notice',
  imports: [],
  template: `
    <div class="notice" role="alert">
      <span class="material-icons" aria-hidden="true">error</span>
      <div class="body">
        <p class="text">{{ message().text }}</p>
        @if (!message().known && error().requestId) {
          <p class="request">
            <span i18n="@@error.request-id">Request ID</span>:
            <code translate="no">{{ error().requestId }}</code>
            <button class="btn btn-tertiary on-line" type="button" (click)="copyRequestId()" i18n="@@error.copy">
              Copy
            </button>
          </p>
          <p class="hint" i18n="@@error.hint">Give this ID to your administrator.</p>
        }
      </div>
      <!-- Retry stands on the middle of the message's first line (R-UI-175, on-line): at the row's top it stood 5.5 px under it. -->
      @if (retryable()) {
        <button class="btn btn-tertiary on-line" type="button" (click)="retry.emit()" i18n="@@error.retry">Retry</button>
      }
    </div>
  `,
  styles: `
    .notice {
      display: flex;
      align-items: flex-start;
      gap: var(--quader-space-3);
      padding: var(--quader-space-3) var(--quader-space-4);
      border: 1px solid var(--quader-error);
      border-radius: var(--quader-radius-m);
      background: var(--quader-error-container);
      color: var(--quader-on-error-container);
    }
    /* The icon stands on the middle of the message's first line (R-UI-175): 24 px on a line of 21 px. */
    .notice > .material-icons {
      margin-block: calc((var(--quader-line) - 24px) / 2);
    }
    .body {
      flex: 1 1 auto;
    }
    p {
      margin: 0;
    }
    .hint {
      margin-block-start: var(--quader-space-1);
    }
    /* A request ID is one token of 32 characters: at 390 px it pushed Copy 29 px past the panel (the walk of the notice's states, G5-20).
       It breaks inside itself where the line ends; Copy follows it. */
    .request code {
      overflow-wrap: anywhere;
    }
    .body {
      min-inline-size: 0;
    }
  `,
})
export class ErrorNotice {
  readonly error = input.required<ApiError>();
  readonly retry = output<void>();

  protected readonly message = computed(() => describeProblem(this.error()));
  protected readonly retryable = computed(() => {
    const s = this.error().status;
    return s === 503 || s === 0;
  });

  protected async copyRequestId(): Promise<void> {
    const id = this.error().requestId;
    if (id) await navigator.clipboard.writeText(id);
  }
}
