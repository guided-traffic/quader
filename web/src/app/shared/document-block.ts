import { Component, LOCALE_ID, computed, inject, input } from '@angular/core';
import { formatNumber } from '@angular/common';
import { CopyButton } from './copy-button';

/**
 * A string of a document longer than this is shown shortened (Q-UI-995): a Debian remote's stored document carries the archive's
 * keyring, one line of about 77,000 characters, which made its code block half a million pixels wide.
 */
export const DOCUMENT_STRING_LIMIT = 200;
/** How much of a shortened string is shown before the mark of its length. */
const SHOWN_HEAD = 64;

/**
 * The value with every string longer than `limit` replaced by its start and its length, "-----BEGIN PGP PUBLIC KEY BLOCK-----…
 * (77,012 characters)"; everything else as it is. `mark` words the length (the locale's number and the translated unit).
 */
export function shortenLongStrings(value: unknown, mark: (count: number) => string, limit = DOCUMENT_STRING_LIMIT): unknown {
  if (typeof value === 'string') return value.length > limit ? `${value.slice(0, SHOWN_HEAD)}… ${mark(value.length)}` : value;
  if (Array.isArray(value)) return value.map((v) => shortenLongStrings(v, mark, limit));
  if (value !== null && typeof value === 'object') {
    return Object.fromEntries(Object.entries(value).map(([k, v]) => [k, shortenLongStrings(v, mark, limit)]));
  }
  return value;
}

/** Whether the value holds a string that `shortenLongStrings` shortens. */
export function holdsLongString(value: unknown, limit = DOCUMENT_STRING_LIMIT): boolean {
  if (typeof value === 'string') return value.length > limit;
  if (Array.isArray(value)) return value.some((v) => holdsLongString(v, limit));
  if (value !== null && typeof value === 'object') return Object.values(value).some((v) => holdsLongString(v, limit));
  return false;
}

/** The mark of a shortened string's length in the reader's language: "(77,012 characters)". */
export function lengthMark(locale: string): (count: number) => string {
  return (count) => {
    const n = formatNumber(count, locale);
    return $localize`:@@document.shortened:(${n}:count: characters)`;
  };
}

/**
 * A stored or an API document, or a request that carries one (Q-UI-995): the text in a code block, every string longer than 200
 * characters shortened to its start and its length, and beside it the copy button, which copies the whole text. A short document
 * shows as it is copied. `text` turns the document into what is shown and copied (JSON by default; a request with curl builds its
 * command around the body), so the shown and the copied text differ only in the shortened strings.
 */
@Component({
  selector: 'quader-document-block',
  imports: [CopyButton],
  template: `
    <div class="document">
      <pre class="code-block" [class.wrap]="wrap()" translate="no" tabindex="0">{{ shown() }}</pre>
      <quader-copy-button [value]="whole()" [label]="copyLabel()" />
    </div>
    @if (shortened()) {
      <p class="hint" i18n="@@document.shortened-hint">Long values are shortened here. The copy button copies them whole.</p>
    }
  `,
  styles: `
    :host {
      display: block;
    }
    /* The shared pattern of a block to copy (the backups' command, the advisory sources' commands): the block takes the line, the
       button stands beside its top. */
    .document {
      display: flex;
      align-items: flex-start;
      gap: var(--quader-space-2);

      pre {
        flex: 1;
        min-inline-size: 0;
        margin: 0;
      }

      /* A narrow place (an opened audit event beside its log) wraps a long line instead of scrolling it sideways. */
      pre.wrap {
        white-space: pre-wrap;
        overflow-wrap: anywhere;
      }
    }
  `,
})
export class DocumentBlock {
  private readonly locale = inject(LOCALE_ID);

  /** The document: an object as the API answers it, or a text. */
  readonly document = input.required<unknown>();
  /** What the reader copies it as; JSON with two spaces by default. */
  readonly text = input<(document: unknown) => string>(toJson);
  readonly copyLabel = input($localize`:@@document.copy:Copy the whole text`);
  /** Wrap a long line where the block is narrow, instead of scrolling it sideways (an opened audit event). */
  readonly wrap = input(false);

  protected readonly whole = computed(() => this.text()(this.document()));
  /** Only the strings inside a document are shortened: a text given whole (a command) is shown as it is. */
  protected readonly shortened = computed(() => typeof this.document() !== 'string' && holdsLongString(this.document()));
  protected readonly shown = computed(() =>
    this.shortened() ? this.text()(shortenLongStrings(this.document(), lengthMark(this.locale))) : this.whole(),
  );
}

/** A document as JSON with two spaces; a text as it is. */
export function toJson(document: unknown): string {
  return typeof document === 'string' ? document : JSON.stringify(document, null, 2);
}
