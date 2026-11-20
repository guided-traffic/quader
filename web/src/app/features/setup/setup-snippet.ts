import { Component, computed, input } from '@angular/core';
import { ClientSnippet } from '../../api/packages-api';
import { CodeWindow } from '../../shared/code/code-window';

/**
 * One snippet of the page **Set up client** (R-UI-121, Q-UI-800): at most one short sentence of what the window does not say
 * already, then the snippet as a window, an editor for a file and a terminal for a command (`<quader-code-window>`), its marks the
 * server's (R-API-030). The copy button copies the text as it is.
 *
 * Secrets (R-UI-043): the text may hold a token the page wrote into it; it is only displayed and copied, never stored.
 */
@Component({
  selector: 'quader-setup-snippet',
  imports: [CodeWindow],
  template: `
    @if (captionParts(); as parts) {
      <p class="why">@for (part of parts; track $index) {@if ($odd) {<code translate="no">{{ part }}</code>} @else {{{ part }}}}</p>
    }
    @if (note(); as n) {
      <p class="why">{{ n }}</p>
    }
    <quader-code-window
      [content]="snippet().content"
      [language]="snippet().language"
      [fileName]="snippet().fileName"
      [marks]="snippet().marks"
      [snippetId]="snippet().id"
      [downloadable]="downloadable()"
    />
  `,
  styles: `
    :host {
      display: block;
      margin-block-end: var(--quader-space-4);
    }
    .why {
      max-inline-size: 70ch;
      margin-block: 0 var(--quader-space-2);
    }
  `,
})
export class SetupSnippet {
  readonly snippet = input.required<ClientSnippet>();
  /** The sentence that says what the window does not (by the snippet's id, R-API-030); null for none. */
  readonly explanation = input<string | null>(null);
  /** A second sentence, for a target that needs one (a Bastion). */
  readonly note = input<string | null>(null);
  /** Whether a file snippet offers **Download**. */
  readonly downloadable = input(true);

  /** The sentence in its parts: a name between backticks is code (`.npmrc`), the rest text. */
  protected readonly captionParts = computed(() => this.explanation()?.split('`') ?? null);
}
