import { Component, input, model } from '@angular/core';

let nextDisclosureId = 1;

/**
 * The one collapsible section of every screen (Q-UI-322, Q-UI-319): the details of a content page, the groups of a settings page
 * and of an editor, closed at first unless the screen opens it. A small heading at the text's size with a chevron before it, one
 * muted line of hint, and the content that folds with the animation of the style guide.
 */
@Component({
  selector: 'quader-disclosure',
  template: `
    <div class="disclosure">
      <h2 class="heading">
        <button class="toggle" type="button" [attr.aria-expanded]="expanded()" [attr.aria-controls]="bodyId" (click)="expanded.set(!expanded())">
          <span class="material-icons chevron" aria-hidden="true">chevron_right</span>
          <span>{{ heading() }}</span>
          @if (note()) {
            <span class="note">{{ note() }}</span>
          }
        </button>
      </h2>
      @if (hint()) {
        <p class="hint">{{ hint() }}</p>
      }
      <div class="collapse-region" [class.open]="expanded()" [id]="bodyId">
        <div class="body"><ng-content /></div>
      </div>
    </div>
  `,
  styles: `
    :host {
      display: block;
    }
    .disclosure {
      padding-block: var(--quader-space-3);
      border-block-start: 1px solid var(--quader-border);
    }
    .heading {
      margin: 0;
      font-size: var(--quader-text-size);
      font-weight: 600;
    }
    .toggle {
      display: inline-flex;
      align-items: center;
      gap: var(--quader-space-1);
      padding: 0;
      border: 0;
      background: none;
      color: var(--quader-heading);
      font: inherit;
      cursor: pointer;
    }
    .toggle:focus-visible {
      outline: var(--quader-focus-ring);
      outline-offset: 2px;
    }
    .chevron {
      font-size: 1.25rem;
      transition: transform var(--quader-motion-duration) var(--quader-motion-easing);
    }
    .note {
      margin-inline-start: var(--quader-space-2);
      color: var(--quader-text-muted);
      font-size: var(--quader-text-muted-size);
      font-weight: 400;
    }
    .toggle[aria-expanded='true'] .chevron {
      transform: rotate(90deg);
    }
    .hint {
      margin: var(--quader-space-1) 0 0;
      padding-inline-start: calc(1.25rem + var(--quader-space-1));
      color: var(--quader-text-muted);
      font-size: var(--quader-text-muted-size);
    }
    .body {
      padding-block-start: var(--quader-space-3);
      padding-inline-start: calc(1.25rem + var(--quader-space-1));
    }
  `,
})
export class Disclosure {
  readonly heading = input.required<string>();
  /** One muted line under the heading that says what is inside. */
  readonly hint = input('');
  /** A short state after the heading, such as "Unsaved changes" of a closed section that holds one (R-UI-087). */
  readonly note = input('');
  readonly expanded = model(false);

  protected readonly bodyId = `quader-disclosure-${nextDisclosureId++}`;
}
