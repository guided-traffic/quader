import { ChangeDetectionStrategy, Component, LOCALE_ID, computed, inject, input } from '@angular/core';

/**
 * A sentence of the translation split at the place of its value (Q-UI-1213): what stands before and after it, so that a template
 * shows the value as a value of running text (`<code translate="no">`, which breaks at none of its hyphens and dots while it fits on a
 * line of its own) and the words around it as the sentence of the reader's language says them. `sentence` builds the sentence with
 * the text it is given as its value.
 */
export interface AroundValue {
  readonly before: string;
  readonly after: string;
}

/** A mark no translation holds, standing for the value while the sentence is split. */
const MARK = '\u0000';

export function aroundValue(sentence: (value: string) => string): AroundValue {
  const text = sentence(MARK);
  const at = text.indexOf(MARK);
  return at < 0 ? { before: text, after: '' } : { before: text.slice(0, at), after: text.slice(at + MARK.length) };
}

/**
 * Values as one list of the reader's language, "registry-1.docker.io and auth.docker.io", each a value of running text (Q-UI-1213): a
 * host, a key or a path that stands in a sentence keeps its hyphens and dots on one line while it fits on one.
 */
@Component({
  selector: 'quader-value-list',
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `@for (p of parts(); track $index) {@if (p.type === 'element') {<code translate="no">{{ p.value }}</code>} @else {{{ p.value }}}}`,
})
export class ValueList {
  private readonly locale = inject(LOCALE_ID);
  readonly values = input.required<readonly string[]>();
  protected readonly parts = computed(() => new Intl.ListFormat(this.locale, { type: 'conjunction' }).formatToParts(this.values()));
}
