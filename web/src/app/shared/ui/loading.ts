import { Component, input } from '@angular/core';
import { Spinner } from './spinner';

/**
 * A load in progress (R-UI-217): the spinner before the sentence that says what loads, the one status of the place.
 *
 *   <quader-loading i18n="@@pkg.loading">Loading the package.</quader-loading>                  a line in a section
 *   <quader-loading size="page" i18n="@@repo.loading">Loading the repository.</quader-loading>  a page or a large area
 *   <quader-loading [inline]="true" i18n="@@repo.key.checking">Checking the key.</quader-loading>  inside a line of text
 *
 * The host is the live region (`role="status"`) and the spinner is decoration, so the sentence is said once. A load that answers
 * within 300 ms shows nothing (Q-UI-1230): the element keeps its place from the start (the place the content will take) and becomes
 * visible after the delay, by the global stylesheet's `quader-appear` (`web/src/styles/_spinner.scss`), so nothing moves when it
 * appears and no timer of the page's own is needed. A screen puts it where it showed its sentence: under `@if (loading())`, in a
 * branch that stands while a read runs, never for a live view's read again of what it shows (R-UI-213, Q-UI-1238).
 */
@Component({
  selector: 'quader-loading',
  imports: [Spinner],
  template: `<quader-spinner [size]="size() === 'page' ? 'page' : 'line'" /><span class="quader-loading-text"><ng-content /></span>`,
  host: {
    class: 'quader-loading',
    '[attr.role]': 'status() ? "status" : null',
    '[class.quader-loading-page]': 'size() === "page"',
    '[class.quader-loading-inline]': 'inline()',
  },
})
export class Loading {
  /** `line` (the spinner of 16 px on the sentence's first line) or `page` (48 px above the sentence, for a page or a large area). */
  readonly size = input<'line' | 'page'>('line');
  /** Stands inside a line of text (a field's hint, a sentence of a section) instead of as a block of its own. */
  readonly inline = input(false);
  /** False inside a live region of the place's own (a test's result), which already says the sentence once. */
  readonly status = input(true);
}
