import { Component, ViewEncapsulation, computed, inject, input } from '@angular/core';
import { DOCUMENT } from '@angular/common';
import { CopyButton } from '../copy-button';
import { COMMAND_LANGUAGES, CodeLanguage, CodeMark, KEEP_MAX, highlight } from './code-syntax';

/** The name a terminal window shows for its command line. Program names: not translated. */
const TERMINAL_TITLES: Readonly<Record<string, string>> = { shell: 'shell', powershell: 'PowerShell', cmd: 'cmd' };

/**
 * A snippet as a window (R-UI-121, Q-UI-801): a file is an editor window, its tab the file's name, its lines numbered, **Download**
 * and the copy button in its bar; a command is a terminal window, each command after its prompt. The window is light paper in the
 * light scheme and dark ink in the dark one, with the syntax colours of its language (`code-syntax.ts`); what the reader replaces
 * and where the token goes are marked. A long line wraps with a hanging indent and never inside a flag, a mark or a token's
 * reference, so nothing scrolls sideways. The copy button copies the text exactly as it is; the prompts and the numbers are drawn by
 * the stylesheet and are no part of it. A window of the editor or the terminal of no product: no window dots, no logo.
 *
 * Secrets (R-UI-043): the text may hold a token the page wrote into it; it is only displayed and copied, never stored.
 */
@Component({
  selector: 'quader-code-window',
  imports: [CopyButton],
  host: {
    '[class.terminal]': 'terminal()',
    '[class.editor]': '!terminal()',
    '[style.--code-gutter]': 'gutter().line',
    '[style.--code-number]': 'gutter().number',
  },
  template: `
    <figure class="window" [attr.aria-label]="name()">
      <div class="bar">
        <span class="tab"
          ><span class="material-icons" aria-hidden="true">{{ terminal() ? 'terminal' : 'description' }}</span
          ><span class="tab-title" translate="no">{{ title() }}</span></span
        >
        <span class="actions">
          @if (!terminal() && downloadable()) {
            <button class="btn btn-tertiary download" type="button" [attr.aria-label]="downloadLabel()" [title]="downloadLabel()" (click)="download()">
              <span class="material-icons" aria-hidden="true">download</span><span class="btn-label" i18n="@@setup.download">Download</span>
            </button>
          }
          <quader-copy-button [value]="content()" [label]="copyLabel()" [quiet]="true" />
        </span>
      </div>
      <pre class="code" translate="no" tabindex="0" [attr.data-snippet]="snippetId()" [class.folding]="shownLines() !== null" [style.--lines]="shownLines()">@for (line of lines(); track $index) {<span class="line" [attr.data-prompt]="line.prompt || null">@for (s of line.segments; track $index) {<span [class]="'k-' + s.kind" [class.keep]="s.keep" [attr.title]="markTitle(s.kind)">@for (p of s.pieces; track $index) {@if (s.pieces.length > 1 && p.length <= keepMax) {<span class="keep">{{ p }}</span>} @else {{{ p }}}@if (!$last) {<wbr />}}</span>}</span>{{ $last ? ending() : newline }}}</pre>
    </figure>
  `,
  styleUrl: './code-window.scss',
  encapsulation: ViewEncapsulation.None,
})
export class CodeWindow {
  private readonly document = inject(DOCUMENT);

  /** The text, exactly as it is copied. */
  readonly content = input.required<string>();
  readonly language = input<CodeLanguage>('text');
  /** The file the text belongs in; null for a command. A file is an editor window, a command a terminal. */
  readonly fileName = input<string | null>(null);
  readonly marks = input<readonly CodeMark[]>([]);
  /** The stable id of a setup snippet (R-API-030), on the text for a journey to find it. */
  readonly snippetId = input<string | null>(null);
  /** The title of a terminal in place of its command line's name ("bash, zsh"). */
  readonly label = input<string | null>(null);
  readonly copyLabel = input($localize`:@@setup.copy:Copy snippet`);
  /** Whether a file offers **Download**. */
  readonly downloadable = input(true);
  /** How many lines the text shows, the rest scrolled inside it and opened by the host's expander (the install panel, R-UI-009); null for all. */
  readonly shownLines = input<number | null>(null);

  protected readonly terminal = computed(() => this.fileName() === null);
  /** An address breaks after a slash, not at a hyphen of a key: each of its pieces up to this length is held on one line. */
  protected readonly keepMax = KEEP_MAX;
  protected readonly lines = computed(() => {
    const language = this.terminal() && !COMMAND_LANGUAGES.includes(this.language()) ? 'shell' : this.language();
    return highlight(this.content(), language, this.marks());
  });
  protected readonly title = computed(() => this.fileName() ?? this.label() ?? TERMINAL_TITLES[this.language()] ?? 'shell');
  protected readonly name = computed(() =>
    this.terminal() ? $localize`:@@code.window.command:Command, ${this.title()}:title:` : $localize`:@@code.window.file:File ${this.title()}:file:`,
  );
  protected readonly downloadLabel = computed(() => $localize`:@@code.window.download:Download ${this.fileName() ?? ''}:file:`);
  /** The room before a line (`line`) and the box its number or its prompt stands at the end of (`number`). */
  protected readonly gutter = computed(() => {
    if (this.terminal()) {
      const width = Math.max(1, ...this.lines().map((l) => l.prompt.length));
      return { line: `${width + 3}ch`, number: `${width + 1.5}ch` };
    }
    const width = String(this.lines().length).length;
    return { line: `${width + 4.5}ch`, number: `${width + 2.5}ch` };
  });
  /**
   * The line breaks stand between the line elements as text, so the block's text is the snippet's (a selection copies it, and a
   * journey compares it); with the block's white space collapsed they take no room between the lines.
   */
  protected readonly newline = '\n';
  protected readonly ending = computed(() => (this.content().endsWith('\n') ? '\n' : ''));

  private readonly valueTitle = $localize`:@@code.mark.value:Replace with your own value`;
  private readonly tokenTitle = $localize`:@@code.mark.token:Your token goes here`;

  protected markTitle(kind: string): string | null {
    return kind === 'value' ? this.valueTitle : kind === 'token' ? this.tokenTitle : null;
  }

  /** Offers the file through a Blob URL that is revoked right after the click (R-UI-043). */
  protected download(): void {
    const name = this.fileName();
    if (!name) return;
    const url = URL.createObjectURL(new Blob([this.content()], { type: 'text/plain;charset=utf-8' }));
    try {
      const a = this.document.createElement('a');
      a.href = url;
      a.download = name;
      a.click();
    } finally {
      URL.revokeObjectURL(url);
    }
  }
}
