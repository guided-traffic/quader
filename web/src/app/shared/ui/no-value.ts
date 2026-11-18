import { Component } from '@angular/core';

/**
 * A table cell without a value (Q-UI-561): the en dash, never an empty cell. The dash is decorative and a screen reader hears
 * "None" instead, so a reader who moves through the table's cells learns that the cell is empty rather than hearing nothing or
 * "en dash". The data table marks a cell that holds only this as empty (`data-empty`), so a stacked row leaves its line out.
 */
@Component({
  selector: 'quader-no-value',
  template: `<span aria-hidden="true">–</span><span class="visually-hidden" i18n="@@no-value">None</span>`,
  host: { class: 'quader-no-value' },
})
export class NoValue {}
