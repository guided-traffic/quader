import { Component, computed, inject } from '@angular/core';
import { Notifier } from '../core/notifier';

/**
 * The live regions of R-UI-042: confirmations in a polite `role="status"` region, errors in a
 * `role="alert"` region. Both containers exist from the start, because a region that appears
 * together with its text is not announced reliably.
 */
@Component({
  selector: 'quader-notices',
  imports: [],
  template: `
    <div class="notices">
      <div role="status">
        @for (n of statuses(); track n.id) {
          <div class="notice">
            <span>{{ n.text }}</span>
            @if (n.action; as action) {
              <button class="btn btn-tertiary on-line" type="button" (click)="action.run(); notifier.dismiss(n.id)">
                {{ action.label }}
              </button>
            }
            <button class="btn btn-tertiary on-line" type="button" (click)="notifier.dismiss(n.id)" i18n="@@notice.dismiss">
              Dismiss
            </button>
          </div>
        }
      </div>
      <div role="alert">
        @for (n of alerts(); track n.id) {
          <div class="notice alert">
            <span>{{ n.text }}</span>
            @if (n.action; as action) {
              <button class="btn btn-tertiary on-line" type="button" (click)="action.run(); notifier.dismiss(n.id)">
                {{ action.label }}
              </button>
            }
            <button class="btn btn-tertiary on-line" type="button" (click)="notifier.dismiss(n.id)" i18n="@@notice.dismiss-alert">
              Dismiss
            </button>
          </div>
        }
      </div>
    </div>
  `,
  styles: `
    .notices {
      position: fixed;
      inset-block-end: var(--quader-space-4);
      inset-inline-start: var(--quader-space-4);
      z-index: 900;
      display: flex;
      flex-direction: column;
      gap: var(--quader-space-2);
      max-inline-size: min(40rem, calc(100vw - 2 * var(--quader-space-4)));
    }
    /* A text of two lines keeps its buttons on the middle of its first line (R-UI-175, on-line), as the error notice does. */
    .notice {
      display: flex;
      align-items: flex-start;
      gap: var(--quader-space-2);
      padding: var(--quader-space-2) var(--quader-space-4);
      border-radius: var(--quader-radius-m);
      background: var(--quader-inverse-surface);
      color: var(--quader-on-inverse-surface);
    }
    .notice.alert {
      background: var(--quader-error-container);
      color: var(--quader-on-error-container);
    }
    .notice button {
      flex: none;
      color: inherit;
    }
  `,
})
export class Notices {
  protected readonly notifier = inject(Notifier);
  protected readonly statuses = computed(() => this.notifier.notices().filter((n) => n.kind === 'status'));
  protected readonly alerts = computed(() => this.notifier.notices().filter((n) => n.kind === 'alert'));
}
