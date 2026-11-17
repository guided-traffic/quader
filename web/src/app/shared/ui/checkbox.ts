import { Component, input, output } from '@angular/core';

/**
 * A native checkbox with its label as the label element's own text (Q-UI-126): the platform widget is
 * keyboard-operable and announced with its name and state. The label is the content; `describedBy` points the
 * input at a note (a hint or a ceiling) by its id.
 *
 *   <quader-checkbox [checked]="x()" (checkedChange)="x.set($event)">Label</quader-checkbox>
 */
@Component({
  selector: 'quader-checkbox',
  template: `
    <label class="check-row">
      <input
        type="checkbox"
        [checked]="checked()"
        [disabled]="disabled()"
        [attr.aria-label]="ariaLabel()"
        [attr.aria-describedby]="describedBy()"
        (change)="checkedChange.emit($any($event.target).checked)"
      />
      <span><ng-content /></span>
    </label>
  `,
  styles: `
    :host {
      display: block;
    }
  `,
})
export class Checkbox {
  readonly checked = input(false);
  readonly disabled = input(false);
  readonly describedBy = input<string | null>(null);
  /** The name, for a checkbox that has no visible label (a row's selection box). */
  readonly ariaLabel = input<string | null>(null);
  readonly checkedChange = output<boolean>();
}
