import { Component, input } from '@angular/core';
import { Disclosure } from './ui/disclosure';

/**
 * A server's own text, folded under **Technical details** below the screen's sentence (R-UI-214): the screen says what happened in
 * the reader's language, and the text the server recorded (an error as a handler or a host returned it, the English sentence of a
 * failure, a format's note) stays one click away for whoever has to give it to an administrator or search a log for it. The text
 * is the server's English and is marked so (`lang="en"`, not translated); it is never the only thing the screen says. `code` adds
 * the code the text belongs to, as code, so a report carries both.
 *
 * A template hands the text in as an input and never interpolates a server's sentence itself: the rule R-UI-214 of
 * scripts/check-rules.mjs (server-text-rule.mjs) refuses an interpolation of `x.detail` and its kin outside this component.
 */
@Component({
  selector: 'quader-technical-details',
  imports: [Disclosure],
  template: `
    <quader-disclosure [inline]="true" [heading]="heading">
      <p class="raw" lang="en" translate="no" data-testid="technical-details">{{ text() }}@if (code(); as c) {@if (text()) {&ngsp;}<code>{{ c }}</code>}</p>
    </quader-disclosure>
  `,
  styles: `
    :host {
      display: block;
      margin-block: var(--quader-space-1);
    }
    .raw {
      margin: 0;
      color: var(--quader-text-muted);
      /* It names addresses, paths and keys: broken anywhere rather than pushing its region sideways. */
      overflow-wrap: anywhere;
    }
  `,
})
export class TechnicalDetails {
  /** The server's text as it came. */
  readonly text = input<string | null>(null);
  /** The code the text belongs to, when the screen said the code in words. */
  readonly code = input<string | null>(null);

  protected readonly heading = $localize`:@@technical-details.heading:Technical details`;
}
