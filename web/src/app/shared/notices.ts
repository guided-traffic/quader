import { NgTemplateOutlet } from '@angular/common';
import { Component, computed, inject } from '@angular/core';
import { Notice, Notifier } from '../core/notifier';

/**
 * The live regions of R-UI-042, drawn as the reference's toast (Q-UI-1063): confirmations in a polite `role="status"` region,
 * errors in a `role="alert"` region. Both containers exist from the start, because a region that appears together with its text
 * is not announced reliably. The toasts stand at the bottom right of the window, the owner's place (2026-11-24); one confirmation
 * at a time (the `Notifier` replaces it), notices with an action and errors each in their own toast. The text comes first, then
 * the action as a quiet link-like button and the close button "×" named **Close**.
 */
@Component({
  selector: 'quader-notices',
  imports: [NgTemplateOutlet],
  template: `
    <div class="toaster">
      <div class="region" role="status">
        @for (n of statuses(); track n.id) {
          <ng-container *ngTemplateOutlet="toast; context: { $implicit: n }" />
        }
      </div>
      <div class="region" role="alert">
        @for (n of alerts(); track n.id) {
          <ng-container *ngTemplateOutlet="toast; context: { $implicit: n }" />
        }
      </div>
    </div>
    <ng-template #toast let-n>
      <div
        class="toast"
        [class.alert]="n.kind === 'alert'"
        animate.enter="toast-in"
        (mouseenter)="notifier.pause(n.id)"
        (mouseleave)="notifier.resume(n.id)"
        (focusin)="notifier.pause(n.id)"
        (focusout)="notifier.resume(n.id)"
      >
        @if (n.kind === 'alert') {
          <span class="material-icons toast-icon" aria-hidden="true">error</span>
        }
        <span class="toast-text">{{ n.text }}</span>
        @if (n.action; as action) {
          <button class="toast-action" type="button" (click)="run(n, action.run)">{{ action.label }}</button>
        }
        <button class="toast-close" type="button" i18n-aria-label="@@notice.close" aria-label="Close" (click)="notifier.dismiss(n.id)">
          <span class="material-icons" aria-hidden="true">close</span>
        </button>
      </div>
    </ng-template>
  `,
  styles: `
    /* The reference's toaster: 24 px from the window's corner, at most 586 px wide; below 576 px across the window with 8 px each side. */
    .toaster {
      position: fixed;
      inset-block-end: var(--quader-space-6);
      inset-inline-end: var(--quader-space-6);
      z-index: 1100;
      display: flex;
      flex-direction: column;
      max-inline-size: 36.625rem;
    }
    @media (max-width: 35.999rem) {
      .toaster {
        inset-inline: 0;
        padding-inline: var(--quader-space-2);
        max-inline-size: none;
      }
    }
    .region {
      display: flex;
      flex-direction: column;
      gap: var(--quader-space-3);
    }
    .toaster:has(> [role='status'] > .toast) > [role='alert']:has(> .toast) {
      margin-block-start: var(--quader-space-3);
    }
    /* The reference's toast: 16 px by 24 px of padding, 8 px round, its shadow, 14 px text on a line of 16 px at weight 300. A control
       stands on the middle of the text's first line (R-UI-175), which for one line is the toast's middle, as the reference centres it. */
    .toast {
      --quader-line: 1rem;
      display: flex;
      align-items: flex-start;
      padding: var(--quader-space-4) var(--quader-space-6);
      border-radius: var(--quader-radius-m);
      box-shadow: var(--quader-elevation-2);
      background: var(--quader-feedback-strong-bg);
      color: var(--quader-feedback-strong-fg);
      font-size: var(--quader-text-size);
      line-height: 1rem;
    }
    .toast-text {
      flex: 1 1 auto;
      min-inline-size: 0;
      padding-inline-end: var(--quader-space-2);
      font-weight: 300;
      overflow-wrap: break-word;
    }
    .toast-action,
    .toast-close {
      flex: none;
      margin-inline-start: var(--quader-space-4);
      border: 0;
      background: transparent;
      color: inherit;
      font: inherit;
      cursor: pointer;
    }
    .toast-action {
      margin-block: calc((var(--quader-line) - var(--quader-control-height)) / 2);
      padding: var(--quader-space-2);
      color: var(--quader-feedback-strong-link);
      font-weight: 600;
      white-space: nowrap;
    }
    .toast-close {
      display: inline-flex;
      align-items: center;
      justify-content: center;
      inline-size: 24px;
      block-size: 24px;
      margin-block: calc((var(--quader-line) - 24px) / 2);
      padding: 0;
      border-radius: var(--quader-control-radius);
    }
    .toast-close:hover {
      background: var(--quader-btn-tertiary-bg-hover);
    }
    .toast-close .material-icons,
    .toast-icon {
      font-size: 1rem;
    }
    .toast-icon {
      flex: none;
      margin-inline-end: var(--quader-space-2);
    }
    .toast-action:focus-visible,
    .toast-close:focus-visible {
      outline: 2px solid var(--quader-feedback-strong-link);
      outline-offset: 2px;
    }
    /* An error keeps the toast's shape in the colours of the error notice (error-notice.ts), with its icon, so it is not told by colour alone. */
    .toast.alert {
      border: 1px solid var(--quader-error);
      background: var(--quader-error-container);
      color: var(--quader-on-error-container);
    }
    .toast.alert .toast-action {
      color: inherit;
      text-decoration: underline;
    }
    .toast.alert :is(.toast-action, .toast-close):focus-visible {
      outline-color: currentColor;
    }
    .toast-in {
      animation: quader-toast-in var(--quader-motion-duration) var(--quader-motion-easing);
    }
    @keyframes quader-toast-in {
      from {
        opacity: 0;
      }
    }
  `,
})
export class Notices {
  protected readonly notifier = inject(Notifier);
  protected readonly statuses = computed(() => this.notifier.notices().filter((n) => n.kind === 'status'));
  protected readonly alerts = computed(() => this.notifier.notices().filter((n) => n.kind === 'alert'));

  protected run(n: Notice, action: () => void): void {
    action();
    this.notifier.dismiss(n.id);
  }
}
