import { Component, computed, input } from '@angular/core';
import { Tooltip } from './tooltip';

/**
 * The help sign of a setting, a section's heading and a list's column (R-UI-267, Q-UI-1618): the info sign after the last word of its
 * label, whose sentence is the tooltip's bubble. The bubble shows on hover and on keyboard focus and stays while the pointer is on
 * the sign or the bubble; a click, a tap, Enter or Space pins it until the next one, a press outside or Escape (`quaderTooltipPin`).
 * The button is named "About {about}" ("About the column {about}" for a column) and described by the sentence, so a screen reader
 * says both on focus whether the bubble shows or not. An empty text renders nothing.
 *
 * The sign never stands inside a `<label>`: its tap does nothing but pin. "?" (`help_outline`) keeps one meaning, a link that opens
 * the manual; this sign is ⓘ, an explanation in place.
 *
 *   <label class="field-label help-anchor" for="x">Maximum throughput</label><quader-help [text]="help" about="Maximum throughput" />
 *
 * In running text the name before it carries `help-anchor` (_controls.scss): the name keeps the sign's room at the end of its last
 * line, so a name of two lines carries the sign after its last word, never alone on a third line.
 */
@Component({
  selector: 'quader-help',
  imports: [Tooltip],
  host: { class: 'quader-help' },
  template: `
    @if (text()) {
      <button type="button" class="help-sign" [quaderTooltip]="text()" [quaderTooltipPin]="true" [attr.aria-label]="name()">
        <span class="material-icons" aria-hidden="true">info</span>
      </button>
    }
  `,
})
export class Help {
  /** The help: one or two plain sentences, at most about 200 characters in English (the bubble's 200 px, Q-UI-1065). */
  readonly text = input.required<string>();
  /** What it explains: a setting's label, a column's label, a section's heading. */
  readonly about = input.required<string>();
  /** The help of a list's column: named "About the column {about}". */
  readonly column = input(false);

  protected readonly name = computed(() => {
    const about = this.about();
    return this.column() ? $localize`:@@table.help-label:About the column ${about}:column:` : $localize`:@@help.about:About ${about}:name:`;
  });
}
