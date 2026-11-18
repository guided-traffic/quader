import { Component, computed, inject, input } from '@angular/core';
import { DOCUMENT } from '@angular/common';
import { ClientSnippet } from '../../api/packages-api';
import { CopyButton } from '../../shared/copy-button';

/**
 * One snippet of the page **Set up client** (R-UI-121, Q-UI-486): the sentence that says what it does, the file it belongs in, the
 * text with its copy button and, for a file, **Download**. A line longer than the block wraps instead of being cut off: its
 * continuation is indented, so a wrapped line does not read as two (a long repository key makes every address line long, and at
 * 390 px a block that scrolls sideways hides the end of a command). The copy button copies the text as it is, line breaks
 * only where the snippet has them.
 *
 * Secrets (R-UI-043): the text may hold a token the page wrote into it; it is only displayed and copied, never stored.
 */
@Component({
  selector: 'quader-setup-snippet',
  imports: [CopyButton],
  template: `
    @if (explanation(); as why) {
      <p class="why">{{ why }}</p>
    }
    @if (note(); as n) {
      <p class="why">{{ n }}</p>
    }
    @if (snippet().fileName) {
      <p class="target">
        <span><span i18n="@@setup.target-file">File</span>: <code translate="no">{{ snippet().fileName }}</code></span>
        @if (downloadable()) {
          <button class="btn btn-tertiary" type="button" (click)="download()" i18n="@@setup.download">Download</button>
        }
      </p>
    }
    <!-- The copy button alone beside the block, so every block of the page ends at the same edge. -->
    <div class="command">
      <pre class="code-block" translate="no" tabindex="0" [attr.data-snippet]="snippet().id">@for (line of lines(); track $index) {<span class="line">{{ line }}</span>{{ $last ? ending() : newline }}}</pre>
      <quader-copy-button [value]="snippet().content" [label]="copyLabel" />
    </div>
  `,
  styles: `
    :host {
      display: block;
      margin-block-end: var(--quader-space-4);
    }
    .why,
    .target {
      max-inline-size: 70ch;
      margin-block: 0 var(--quader-space-2);
    }
    .target {
      display: flex;
      flex-wrap: wrap;
      align-items: center;
      gap: var(--quader-space-1) var(--quader-space-3);
    }
    .command {
      display: flex;
      align-items: flex-start;
      gap: var(--quader-space-2);
    }
    pre {
      flex: 1 1 auto;
      min-inline-size: 0;
      white-space: normal;
    }
    .line {
      display: block;
      min-block-size: 1lh;
      padding-inline-start: 2ch;
      text-indent: -2ch;
      white-space: pre-wrap;
      overflow-wrap: anywhere;
    }
  `,
})
export class SetupSnippet {
  private readonly document = inject(DOCUMENT);

  readonly snippet = input.required<ClientSnippet>();
  /** The sentence that says what the snippet does (by its id, R-API-030); null for none. */
  readonly explanation = input<string | null>(null);
  /** A second sentence, for a target that needs one (a Bastion). */
  readonly note = input<string | null>(null);
  /** Whether a file snippet offers **Download**. */
  readonly downloadable = input(true);

  protected readonly copyLabel = $localize`:@@setup.copy:Copy snippet`;
  /** The lines of the text; a trailing line break makes no empty last line. */
  protected readonly lines = computed(() => this.snippet().content.replace(/\n$/, '').split('\n'));
  /**
   * The line breaks stand between the line elements as text, so the block's text is the snippet's (a selection copies it, and
   * a journey compares it); with the block's white space collapsed they take no room between the lines.
   */
  protected readonly newline = '\n';
  protected readonly ending = computed(() => (this.snippet().content.endsWith('\n') ? '\n' : ''));

  /** Offers the file through a Blob URL that is revoked right after the click (R-UI-043). */
  protected download(): void {
    const sn = this.snippet();
    if (!sn.fileName) return;
    const url = URL.createObjectURL(new Blob([sn.content], { type: 'text/plain;charset=utf-8' }));
    try {
      const a = this.document.createElement('a');
      a.href = url;
      a.download = sn.fileName;
      a.click();
    } finally {
      URL.revokeObjectURL(url);
    }
  }
}
