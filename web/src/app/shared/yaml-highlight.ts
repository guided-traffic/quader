/**
 * The highlighting of YAML as Ansible writes it (R-ANS-050, Q-ANS-95): the examples of a module or a plugin and a sample that spans lines.
 * A tokenizer of its own, line by line with the one state YAML needs across lines (a block scalar): keys, strings, numbers, booleans and
 * null, comments, the punctuation of lists and flow collections, and Jinja expressions (`{{ … }}`, `{% … %}`) wherever they stand. It never
 * builds HTML: the template draws each token as text in a span of its kind, so a hostile example stays text.
 *
 * The tokens of a line join to the line, character for character; whatever the tokenizer does not understand is `plain`.
 */

/** What a piece of a line is; each kind has one colour (`ansible-doc-entry.scss`, the code tokens of `_tokens.scss`). */
export type YamlKind = 'plain' | 'key' | 'string' | 'number' | 'bool' | 'comment' | 'jinja' | 'punct';

export interface YamlToken {
  readonly text: string;
  readonly kind: YamlKind;
}

/** The longest text highlighted; past it the text is one plain token per line, so a pathological example costs nothing. */
export const MAX_HIGHLIGHT = 256 * 1024;

const NUMBER = /^[-+]?(\d[\d_]*(\.\d*)?([eE][-+]?\d+)?|\.\d+|0x[0-9a-fA-F_]+|0o[0-7_]+|\.inf|\.Inf|\.INF|\.nan|\.NaN|\.NAN)$/;
const BOOL = /^(true|false|True|False|TRUE|FALSE|yes|no|Yes|No|YES|NO|on|off|On|Off|ON|OFF|null|Null|NULL|~)$/;
const JINJA = /\{\{.*?\}\}|\{%.*?%\}|\{#.*?#\}/g;
/** A key at the start of a line's content: a plain one or a quoted one, followed by a colon and a space or the end. */
const KEY = /^((?:[^\s#'"{[\]},&*!|>%@`-]|-(?=\S))[^#]*?|"(?:[^"\\]|\\.)*"|'(?:[^']|'')*')(\s*)(:)(?=\s|$)/;
/** The indicator of a block scalar: `|`, `>`, with chomping and indentation, and perhaps a comment. */
const BLOCK = /^([|>][-+0-9]*)(\s*)(#.*)?$/;

/** The lines of a text, each as its tokens. */
export function highlightYaml(text: string): YamlToken[][] {
  const lines = text.replace(/\n$/, '').split('\n');
  if (text.length > MAX_HIGHLIGHT) return lines.map((l) => (l === '' ? [] : [{ text: l, kind: 'plain' as const }]));
  const out: YamlToken[][] = [];
  /** The column of the key that opened a block scalar; a line indented deeper, or a blank one, belongs to it. */
  let block: number | null = null;
  /** The depth of the flow collections open at the end of the line before: a JSON sample spans lines. */
  const flow = { depth: 0 };
  for (const line of lines) {
    const indent = /^ */.exec(line)?.[0].length ?? 0;
    if (block !== null && (line.trim() === '' || indent > block)) {
      out.push(withJinja(line, 'string'));
      continue;
    }
    const tokens: YamlToken[] = [];
    block = lineTokens(line, tokens, flow);
    out.push(merge(tokens));
  }
  return out;
}

/** Tokenizes one line into out; the column of its key (or of its content) when the line opens a block scalar, else null. */
function lineTokens(line: string, out: YamlToken[], flow: { depth: number }): number | null {
  if (line === '') return null;
  if (/^\s*#/.test(line)) {
    out.push({ text: line, kind: 'comment' });
    return null;
  }
  if (/^(---|\.\.\.)(\s|$)/.test(line)) {
    out.push({ text: line.slice(0, 3), kind: 'punct' });
    return valueTokens(line.slice(3), out, flow) ? 0 : null;
  }
  let rest = line;
  const lead = /^\s*/.exec(rest)?.[0] ?? '';
  push(out, lead, 'plain');
  rest = rest.slice(lead.length);
  // The dashes of list items, `- - x` included.
  for (let m = /^-(\s+|$)/.exec(rest); m; m = /^-(\s+|$)/.exec(rest)) {
    push(out, '-', 'punct');
    push(out, m[1] ?? '', 'plain');
    rest = rest.slice(m[0].length);
  }
  const column = line.length - rest.length;
  const key = KEY.exec(rest);
  if (key) {
    out.push({ text: key[1] ?? '', kind: 'key' });
    push(out, key[2] ?? '', 'plain');
    out.push({ text: ':', kind: 'punct' });
    rest = rest.slice(key[0].length);
  }
  return valueTokens(rest, out, flow) ? column : null;
}

/** Tokenizes the value of a line; true when it is the indicator of a block scalar. */
function valueTokens(value: string, out: YamlToken[], flow: { depth: number }): boolean {
  const lead = /^\s*/.exec(value)?.[0] ?? '';
  const block = flow.depth === 0 ? BLOCK.exec(value.slice(lead.length)) : null;
  if (block) {
    push(out, lead, 'plain');
    out.push({ text: block[1] ?? '', kind: 'punct' });
    push(out, block[2] ?? '', 'plain');
    push(out, block[3] ?? '', 'comment');
    return true;
  }
  flow.depth = scan(value, out, flow.depth);
  return false;
}

/**
 * The scanner of a value: white space, comments, Jinja, quoted strings, flow punctuation and plain scalars. depth is the nesting of flow
 * collections at its start; it returns the nesting at its end.
 */
function scan(s: string, out: YamlToken[], depth: number): number {
  let i = 0;
  while (i < s.length) {
    const c = s.charAt(i);
    if (/\s/.test(c)) {
      const j = skip(s, i, (ch) => /\s/.test(ch));
      push(out, s.slice(i, j), 'plain');
      i = j;
    } else if (c === '#' && (i === 0 || /\s/.test(s.charAt(i - 1)))) {
      out.push({ text: s.slice(i), kind: 'comment' });
      return depth;
    } else if (/^\{[{%#]/.test(s.slice(i, i + 2))) {
      const close = s.charAt(i + 1) === '{' ? '}}' : s.charAt(i + 1) === '%' ? '%}' : '#}';
      const end = s.indexOf(close, i + 2);
      const j = end < 0 ? s.length : end + 2;
      out.push({ text: s.slice(i, j), kind: 'jinja' });
      i = j;
    } else if (c === '"' || c === "'") {
      const j = quoted(s, i);
      const after = s.slice(j);
      const isKey = depth > 0 && /^\s*:(\s|$)/.test(after);
      if (isKey) out.push({ text: s.slice(i, j), kind: 'key' });
      else out.push(...withJinja(s.slice(i, j), 'string'));
      i = j;
    } else if ('[]{},'.includes(c)) {
      depth += c === '[' || c === '{' ? 1 : c === ']' || c === '}' ? -1 : 0;
      depth = Math.max(depth, 0);
      out.push({ text: c, kind: 'punct' });
      i++;
    } else if (depth > 0 && c === ':' && /^(\s|$)/.test(s.charAt(i + 1))) {
      out.push({ text: c, kind: 'punct' });
      i++;
    } else {
      const j = plainEnd(s, i, depth > 0);
      const word = s.slice(i, j);
      const trimmed = word.replace(/\s+$/, '');
      const isKey = depth > 0 && /^:(\s|$)/.test(s.slice(i + trimmed.length).replace(/^\s+/, ''));
      out.push(...(isKey ? [{ text: trimmed, kind: 'key' as const }] : withJinja(trimmed, scalarKind(trimmed))));
      push(out, word.slice(trimmed.length), 'plain');
      i = Math.max(j, i + 1);
    }
  }
  return depth;
}

/** Where a plain scalar that starts at i ends: before a comment, a Jinja expression, and in a flow collection before its punctuation. */
function plainEnd(s: string, i: number, flow: boolean): number {
  let j = i;
  while (j < s.length) {
    const c = s.charAt(j);
    if (c === '#' && j > i && /\s/.test(s.charAt(j - 1))) {
      // The white space before the comment belongs to the white space, not to the scalar.
      while (j > i && /\s/.test(s.charAt(j - 1))) j--;
      return j;
    }
    if (j > i && /^\{[{%#]/.test(s.slice(j, j + 2))) return j;
    if (flow && ('[]{},'.includes(c) || (c === ':' && /^(\s|$)/.test(s.charAt(j + 1))))) return j;
    j++;
  }
  return j;
}

/** The end of the quoted string that opens at i: a double-quoted one with backslash escapes, a single-quoted one with doubled quotes. */
function quoted(s: string, i: number): number {
  const q = s.charAt(i);
  let j = i + 1;
  while (j < s.length) {
    const c = s.charAt(j);
    if (q === '"' && c === '\\') {
      j += 2;
      continue;
    }
    if (c === q) {
      if (q === "'" && s.charAt(j + 1) === "'") {
        j += 2;
        continue;
      }
      return j + 1;
    }
    j++;
  }
  return s.length;
}

function scalarKind(word: string): YamlKind {
  if (NUMBER.test(word)) return 'number';
  if (BOOL.test(word)) return 'bool';
  return 'plain';
}

/** A text of one kind with the Jinja expressions inside it as tokens of their own. */
function withJinja(text: string, kind: YamlKind): YamlToken[] {
  const out: YamlToken[] = [];
  let last = 0;
  for (const m of text.matchAll(JINJA)) {
    push(out, text.slice(last, m.index), kind);
    out.push({ text: m[0], kind: 'jinja' });
    last = m.index + m[0].length;
  }
  push(out, text.slice(last), kind);
  return out;
}

function skip(s: string, i: number, keep: (c: string) => boolean): number {
  let j = i;
  while (j < s.length && keep(s.charAt(j))) j++;
  return j;
}

function push(out: YamlToken[], text: string, kind: YamlKind): void {
  if (text !== '') out.push({ text, kind });
}

/** Neighbours of the same kind become one token, so a line is drawn with as few spans as it needs. */
function merge(tokens: readonly YamlToken[]): YamlToken[] {
  const out: YamlToken[] = [];
  for (const t of tokens) {
    const prev = out.at(-1);
    if (prev && prev.kind === t.kind) out[out.length - 1] = { text: prev.text + t.text, kind: t.kind };
    else out.push(t);
  }
  return out;
}
