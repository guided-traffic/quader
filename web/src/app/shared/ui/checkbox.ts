import { NgTemplateOutlet } from '@angular/common';
import { Component, input, output } from '@angular/core';
import { Help } from './help';
import { Unavailable } from './unavailable';

let nextCheckboxId = 1;

/**
 * A native checkbox with its label as the label element's own text (Q-UI-126): the platform widget is
 * keyboard-operable and announced with its name and state. The label is the content; `describedBy` points the
 * input at a note (a hint or a ceiling) by its id.
 *
 * `help` is what the setting does, behind the help sign after the label's last word (R-UI-267), named by `about` (the label's
 * words). The sign stands outside the `<label>`, which then names the box by `for`: its tap pins the sentence and never toggles
 * the box. A line that stays visible under the box (a refusal, what becomes public) is still the note of `describedBy`.
 *
 * `unavailable` is why a box that cannot change cannot (R-UI-270, Q-UI-1654): the box stays, greyed out, and says the reason as its
 * tooltip on hover, focus and tap (`quaderUnavailable` on the input), in place of a line under it; an empty reason leaves it as it is.
 *
 *   <quader-checkbox [checked]="x()" (checkedChange)="x.set($event)">Label</quader-checkbox>
 *   <quader-checkbox [checked]="x()" help="What it does." about="Label" (checkedChange)="x.set($event)">Label</quader-checkbox>
 */
@Component({
  selector: 'quader-checkbox',
  imports: [Help, NgTemplateOutlet, Unavailable],
  template: `
    <ng-template #content><ng-content /></ng-template>
    @if (help()) {
      <div class="check-row">
        <input
          type="checkbox"
          [id]="id"
          [checked]="checked()"
          [disabled]="disabled() && !unavailable()"
          [quaderUnavailable]="unavailable()"
          [attr.aria-label]="ariaLabel()"
          [attr.aria-describedby]="describedBy()"
          (change)="checkedChange.emit($any($event.target).checked)"
        />
        <span
          ><label class="help-anchor" [for]="id"><ng-container [ngTemplateOutlet]="content" /></label
          ><quader-help [text]="help()" [about]="about()"
        /></span>
      </div>
    } @else {
      <label class="check-row">
        <input
          type="checkbox"
          [checked]="checked()"
          [disabled]="disabled() && !unavailable()"
          [quaderUnavailable]="unavailable()"
          [attr.aria-label]="ariaLabel()"
          [attr.aria-describedby]="describedBy()"
          (change)="checkedChange.emit($any($event.target).checked)"
        />
        <span><ng-container [ngTemplateOutlet]="content" /></span>
      </label>
    }
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
  /** Why the box cannot change, as its tooltip (R-UI-270); empty where it can, or where the reader may not change it at all. */
  readonly unavailable = input('');
  /** The name, for a checkbox that has no visible label (a row's selection box). */
  readonly ariaLabel = input<string | null>(null);
  /** What the setting does, behind the help sign after the label (R-UI-267); empty shows no sign. */
  readonly help = input('');
  /** What the sign explains: the label's words, for its name "About {about}". */
  readonly about = input('');
  readonly checkedChange = output<boolean>();

  protected readonly id = `quader-checkbox-${nextCheckboxId++}`;
}
