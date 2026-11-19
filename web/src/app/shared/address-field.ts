import { Component, ViewEncapsulation, input } from '@angular/core';
import { CopyButton } from './copy-button';

/**
 * One client address in a read-only, bordered input with the copy button joined to its right edge, as the reference
 * shows its clone address (R-UI-065). The address is the input's value: never cut with an ellipsis, and scrolled inside
 * the field where it is longer than the field. An empty address renders nothing.
 *
 * The field fills the line it is on: alone on its line at least `--quader-address-measure` (Q-UI-301) and as wide as its
 * address where that is longer, up to the line (Q-UI-683), so a long address is read whole where there is room and the
 * fields of a stack of short addresses have one width. A field shorter than its address shows the address's start, the host,
 * and scrolls.
 */
@Component({
  selector: 'quader-address-field',
  imports: [CopyButton],
  encapsulation: ViewEncapsulation.None,
  host: { '[style.--quader-address-chars]': 'value().length' },
  template: `
    @if (value()) {
      <input
        type="text"
        class="address-field-input"
        readonly
        spellcheck="false"
        autocomplete="off"
        translate="no"
        [value]="value()"
        [attr.aria-label]="label()"
        (focus)="selectAll($event)"
        (click)="selectAll($event)"
      />
      <quader-copy-button [value]="value()" [label]="copyLabel()" />
    }
  `,
  styles: `
    /* The font is the input's, so that a character of --quader-address-chars is one of the address's; the rest is the input's
       padding and border and the joined copy button, as the one-line command's field counts it (Q-UI-455). */
    quader-address-field {
      display: flex;
      align-items: stretch;
      box-sizing: border-box;
      inline-size: 100%;
      max-inline-size: max(var(--quader-address-measure), calc((var(--quader-address-chars, 0) + 2) * 1ch + 4.5rem));
      font-family: var(--quader-font-mono);
      font-size: 0.875rem;
    }

    /* Several addresses of one repository, stacked. */
    quader-address-field + quader-address-field {
      margin-block-start: var(--quader-space-2);
    }

    /* The control tokens of .text-input, restated: the read-only grey of .text-input[readonly] is not wanted here. */
    quader-address-field .address-field-input {
      box-sizing: border-box;
      flex: 1 1 auto;
      inline-size: 0;
      min-inline-size: 8rem;
      min-block-size: var(--quader-control-height);
      padding: var(--quader-space-1) var(--quader-space-3);
      border: 1px solid var(--quader-control-border);
      border-start-start-radius: var(--quader-control-radius);
      border-end-start-radius: var(--quader-control-radius);
      border-start-end-radius: 0;
      border-end-end-radius: 0;
      background: var(--quader-control-bg);
      color: var(--quader-control-fg);
      font-family: var(--quader-font-mono);
      font-size: 0.875rem;
      text-overflow: clip;
    }

    quader-address-field .address-field-input:hover:not(:focus-visible) {
      border-color: var(--quader-control-border-hover);
    }

    quader-address-field .address-field-input:focus-visible {
      position: relative;
      z-index: 1;
      outline: var(--quader-focus-ring);
      outline-offset: 1px;
    }

    /* The joined button is a part of the field and as high as the input, not a control beside a text: the rules that put a
       cell's copy button on its line leave it out (_list-screens.scss, R-UI-175). */
    quader-address-field quader-copy-button {
      display: flex;
      flex: none;
      margin-inline-start: -1px;
    }

    quader-address-field quader-copy-button .btn {
      border: 1px solid var(--quader-control-border);
      border-start-start-radius: 0;
      border-end-start-radius: 0;
    }

    quader-address-field quader-copy-button .btn:hover {
      border-color: var(--quader-control-border-hover);
    }

    quader-address-field quader-copy-button .btn:focus-visible {
      position: relative;
      z-index: 1;
    }
  `,
})
export class AddressField {
  /** The address, one line. */
  readonly value = input.required<string>();
  /** The accessible name of the field; in a list it names the row. */
  readonly label = input($localize`:@@address.label:Client address`);
  /** The accessible name and title of the copy button. */
  readonly copyLabel = input($localize`:@@address.copy:Copy client address`);

  protected selectAll(event: Event): void {
    (event.target as HTMLInputElement).select();
  }
}
