import { ChangeDetectionStrategy, Component, computed, input } from '@angular/core';

/** The marks after which an address breaks: a base URL, a client address (the Bastion cards, Q-UI-959). */
export const ADDRESS_MARKS = './:';
/** The marks after which an audit action's key breaks (Q-UI-567). */
export const KEY_MARKS = '._';
/** The marks after which a name, a path or a signer's identity (a URL, a distinguished name) breaks (Q-UI-969). */
export const IDENTITY_MARKS = '/.@,';

/**
 * A value in the pieces after which a line may break, each piece ending with its run of `marks` ("https://", "gitlab.", "refs/"); a
 * template puts a `<wbr>` after each. A line then breaks after a mark first, and inside a word only where one piece is longer than the
 * whole line: "…/refs/heads/" / "legacy-build", never "…heads/le" / "gacy-build". `keep` (a version in a file's path) stays one piece
 * wherever it stands as a whole, so it never breaks after one of its dots. The one helper of the Bastion cards' addresses, the audit
 * log's keys and the signatures' names, paths and identities.
 */
export function breakParts(value: string, marks: string, keep: string | null = null): string[] {
  if (value === '') return [];
  const at = keep ? keptAt(value, keep) : -1;
  if (at >= 0) {
    // The kept value is one piece with the run of marks after it; the text on either side is cut as usual.
    let end = at + keep!.length;
    while (end < value.length && marks.includes(value[end]!)) end++;
    return [...breakParts(value.slice(0, at), marks), value.slice(at, end), ...breakParts(value.slice(end), marks, keep)];
  }
  if (marks === '') return [value];
  const set = `[${marks.replace(/[\\\]^-]/g, '\\$&')}]`;
  return value.match(new RegExp(`(?:(?!${set}).)*${set}*`, 'gs'))?.filter((p) => p !== '') ?? [value];
}

/** The first place `keep` stands in `value` as a whole, with no letter or digit right before or after it; -1 where it does not. */
function keptAt(value: string, keep: string): number {
  const alnum = /[\p{L}\p{N}]/u;
  for (let i = value.indexOf(keep); i >= 0; i = value.indexOf(keep, i + 1)) {
    const before = value[i - 1];
    const after = value[i + keep.length];
    if ((before === undefined || !alnum.test(before)) && (after === undefined || !alnum.test(after))) return i;
  }
  return -1;
}

/**
 * A value shown in its pieces (`breakParts`), each whole on its line while it fits: a hyphen inside a piece ("legacy-build",
 * "dmz-frankfurt.") is no place to break, and a piece breaks inside itself only where it is longer than the line. The white space
 * before a piece (the ", O=…" of a distinguished name) stands between the pieces, where it is seen and is a place to break.
 */
@Component({
  selector: 'quader-parts-text',
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `@for (p of pieces(); track $index) {@if (p.space) {&ngsp;}<span class="piece">{{ p.text }}</span><wbr />}`,
  styles: `
    .piece {
      display: inline-block;
      max-inline-size: 100%;
      overflow-wrap: anywhere;
    }
  `,
})
export class PartsText {
  readonly value = input.required<string>();
  readonly marks = input.required<string>();
  /** A text that stays one piece where it stands (a version in a path); none by default. */
  readonly keep = input<string | null>(null);
  protected readonly pieces = computed(() =>
    breakParts(this.value(), this.marks(), this.keep()).map((p) => ({ space: /^\s/.test(p), text: p.trimStart() })),
  );
}
