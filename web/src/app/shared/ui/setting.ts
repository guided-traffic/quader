import { Component, input } from '@angular/core';
import { Help } from './help';

let nextSettingId = 1;

/**
 * One row of a settings card (R-UI-267): the setting's name at weight 600 with its help sign in a column of
 * `--quader-setting-label` (16 rem), the control beside it; below 40 rem of content the name stands above its control. A label
 * longer than its column wraps between words and keeps the sign after its last word. The control column holds the control with
 * its inline value or note (the default slot) and the lines under it (`[below]`: what becomes public, the meter of a field). A
 * warning that belongs to the row is the row's next sibling, not inside it, so it spans the measure.
 *
 * `for` names the id of a form control: the name is then its `<label>`. Otherwise the name is a span whose id (`labelId`) a control
 * that is named by it points at (`aria-labelledby`), such as a switch or a group of buttons.
 *
 *   <quader-setting #row [label]="name" [help]="help">
 *     <quader-switch [label]="name" … />
 *     <p below class="muted">It also shows every package your builds fetched through a public remote repository.</p>
 *   </quader-setting>
 */
@Component({
  selector: 'quader-setting',
  imports: [Help],
  host: { class: 'quader-setting' },
  template: `
    <div class="setting-label">
      @if (for(); as id) {
        <label class="setting-name" [class.help-anchor]="help()" [for]="id" [id]="labelId">{{ label() }}</label
        ><quader-help [text]="help()" [about]="label()" />
      } @else {
        <span class="setting-name" [class.help-anchor]="help()" [id]="labelId">{{ label() }}</span
        ><quader-help [text]="help()" [about]="label()" />
      }
    </div>
    <div class="setting-control">
      <ng-content />
      <div class="setting-below"><ng-content select="[below]" /></div>
    </div>
  `,
  styles: `
    :host {
      display: grid;
      grid-template-columns: var(--quader-setting-label) minmax(0, 1fr);
      column-gap: var(--quader-space-4);
      /* The name's first line stands on the line of the control's first text: its word, its value, its field's text. */
      align-items: baseline;
      max-inline-size: var(--quader-settings-measure);
      padding-block: var(--quader-space-1);
    }
    @container content (max-width: 40rem) {
      :host {
        grid-template-columns: minmax(0, 1fr);
        row-gap: var(--quader-space-1);
      }
    }
    .setting-name {
      font-weight: 600;
    }
    .setting-control {
      min-inline-size: 0;
    }
    .setting-below {
      display: flex;
      flex-direction: column;
      gap: var(--quader-space-1);
      margin-block-start: var(--quader-space-1);
    }
    .setting-below:empty {
      display: none;
    }
  `,
})
export class Setting {
  /** The setting's name. */
  readonly label = input.required<string>();
  /** What the setting does, behind the help sign after the name; empty shows no sign. */
  readonly help = input('');
  /** The id of the form control the name labels (`<label for>`); null for a control named through `labelId`. */
  readonly for = input<string | null>(null);

  /** The id of the name, for a control that is named by it (`aria-labelledby`). */
  readonly labelId = `quader-setting-${nextSettingId++}`;
}
