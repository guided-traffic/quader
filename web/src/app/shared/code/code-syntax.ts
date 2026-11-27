/**
 * The syntax of a snippet in a code window (R-UI-121, Q-UI-801): a small tokenizer per language the API names (`ini`, `yaml`, `json`,
 * `xml`, `text` for a file; `shell`, `powershell`, `cmd` for a command), the marks of the reader's parts laid over it, the prompt of
 * each line of a command, and the wrap rule. Everything here is a pure function of the text, so a new format adds no highlighter:
 * its snippets name one of these languages. No library: the snippets are a handful of short, known shapes.
 */

/** What a piece of a line is; each kind has one colour of the window (`_code-window.scss`). */
export type SegmentKind =
  | 'plain'
  | 'command'
  | 'flag'
  | 'string'
  | 'key'
  | 'url'
  | 'variable'
  | 'punct'
  | 'comment'
  /** A part the reader replaces with their own value: `<package>`, `myapp:1.0`. */
  | 'value'
  /** Where the token goes: the reference to the variable, the placeholder, or the token the page wrote in. */
  | 'token';

/** A piece of a line. `keep` holds it on one line; `pieces` are where a long one may break (an address after its slashes). */
export interface Segment {
  readonly text: string;
  readonly kind: SegmentKind;
  readonly keep: boolean;
  readonly pieces: readonly string[];
}

/** One line of a snippet, and for a command the prompt before it: none on a line that continues the line before. */
export interface CodeLine {
  readonly segments: readonly Segment[];
  readonly prompt: string;
}

/** The languages of `ClientSnippet.language` (R-API-030). */
export type CodeLanguage = 'ini' | 'yaml' | 'json' | 'shell' | 'powershell' | 'cmd' | 'xml' | 'text';

/** A part of the text that is the reader's (`ClientSnippet.marks`). */
export interface CodeMark {
  readonly text: string;
  readonly kind: 'value' | 'token';
}

/** The command lines: a snippet in one of them is a command, drawn in a terminal window with a prompt. */
export const COMMAND_LANGUAGES: readonly CodeLanguage[] = ['shell', 'powershell', 'cmd'];

/** The prompt a terminal shows before a command, by its command line. */
export function promptOf(language: CodeLanguage): string {
  return language === 'powershell' ? 'PS>' : language === 'cmd' ? '>' : '$';
}

/**
 * The longest piece that is held on one line. A flag, a mark and a token's reference never break inside (`--password-stdin`,
 * `<package>`, `${QUADER_TOKEN}`); a longer one, such as a token of 57 characters written into the snippet, may, or it would
 * push the window wider than a phone.
 */
export const KEEP_MAX = 32;

/**
 * The wrap rule: whether a piece of a line stays on one line when the line wraps. A word (a command, a flag, a value, a key, a mark,
 * a token's reference) of up to `KEEP_MAX` characters does; white space never does, so the line breaks between words; an address and
 * a string break at their own places (`breakPieces`), a comment and punctuation like text.
 */
export function keepsTogether(kind: SegmentKind, text: string): boolean {
  const word = kind !== 'url' && kind !== 'string' && kind !== 'comment' && kind !== 'punct';
  return word && text.length <= KEEP_MAX && !/\s/.test(text);
}

/** Where an address or a path may break: after each `/`, `?` and `&`, so a long key moves to the next line whole. */
export function breakPieces(kind: SegmentKind, text: string): readonly string[] {
  if (kind !== 'url' && kind !== 'string') return [text];
  const out = text.split(/(?<=[/?&])/);
  return out.length > 0 ? out : [text];
}

function seg(text: string, kind: SegmentKind): Segment {
  return { text, kind, keep: keepsTogether(kind, text), pieces: breakPieces(kind, text) };
}

/** An address or a path: a scheme, a host with a port or a path, or a path that starts with `/`, `./` or `~/`. */
export function isAddress(word: string): boolean {
  return /^[a-z][a-z0-9+.-]*:\/\//i.test(word) || /^[\w.-]+:\d+\//.test(word) || /^(\.{0,2}\/|~\/)[^\s]/.test(word) || /^[\w.-]+\.[a-z]{2,}\//i.test(word);
}

function valueKind(word: string): SegmentKind {
  return isAddress(word) ? 'url' : 'plain';
}

/** The lines of a snippet with their segments and prompts, the marks laid over them. */
export function highlight(content: string, language: CodeLanguage, marks: readonly CodeMark[] = []): CodeLine[] {
  const lines = content.replace(/\n$/, '').split('\n');
  const tokenized =
    language === 'shell' || language === 'powershell' || language === 'cmd'
      ? commandLines(lines, language)
      : lines.map((l) => ({ segments: fileLine(l, language), prompt: '' }));
  return tokenized.map((l) => ({ prompt: l.prompt, segments: overlay(l.segments, marks) }));
}

/** Lays the marks over a line's segments: every place a mark's text stands becomes one segment of its kind. */
export function overlay(segments: readonly Segment[], marks: readonly CodeMark[]): Segment[] {
  const text = segments.map((s) => s.text).join('');
  const ranges: { from: number; to: number; kind: 'value' | 'token' }[] = [];
  // The longest first, so a mark inside another one (`<package>` in an example) does not cut it.
  for (const m of [...marks].sort((a, b) => b.text.length - a.text.length)) {
    if (m.text === '') continue;
    for (let at = text.indexOf(m.text); at >= 0; at = text.indexOf(m.text, at + m.text.length)) {
      const to = at + m.text.length;
      if (!ranges.some((r) => at < r.to && to > r.from)) ranges.push({ from: at, to, kind: m.kind });
    }
  }
  if (ranges.length === 0) return [...segments];
  ranges.sort((a, b) => a.from - b.from);
  const out: Segment[] = [];
  let pos = 0;
  const pushPart = (s: Segment, from: number, to: number, start: number) => {
    if (to > from) out.push(seg(s.text.slice(from - start, to - start), s.kind));
  };
  let ri = 0;
  for (const s of segments) {
    const start = pos;
    const end = pos + s.text.length;
    let cur = start;
    while (cur < end) {
      const r = ranges[ri];
      if (!r || r.from >= end) {
        pushPart(s, cur, end, start);
        cur = end;
      } else if (cur < r.from) {
        pushPart(s, cur, r.from, start);
        cur = r.from;
      } else {
        // Inside a mark: it is emitted once, where it starts.
        if (cur === r.from) out.push(seg(text.slice(r.from, r.to), r.kind));
        cur = Math.min(end, r.to);
        if (cur === r.to) ri++;
      }
    }
    pos = end;
  }
  return out;
}

// ---- Commands ------------------------------------------------------------------------------------------------------------------

/** The character that continues a command on the next line. */
function continuationOf(language: CodeLanguage): string {
  return language === 'powershell' ? '`' : language === 'cmd' ? '^' : '\\';
}

function isComment(line: string, language: CodeLanguage): boolean {
  const t = line.trimStart();
  return language === 'cmd' ? /^(rem\b|::)/i.test(t) : t.startsWith('#');
}

/**
 * The lines of a command. A line starts a command unless it continues the one before: after the continuation character, inside a
 * here-document (`<<EOF` to `EOF`) and inside a block (`name() {` to `}`, or a function whose body is a subshell, `name() (` to `)`).
 * Only a starting line gets the prompt, and a comment gets none: it is no command.
 */
export function commandLines(lines: readonly string[], language: CodeLanguage): { segments: Segment[]; prompt: string }[] {
  const prompt = promptOf(language);
  const cont = continuationOf(language);
  let continued = false;
  // After a pipe at the end of a line, the continued line starts with a command.
  let expectNext = false;
  let heredoc: string | null = null;
  // The closing lines of the blocks open now, innermost last.
  const blocks: string[] = [];
  return lines.map((line) => {
    if (heredoc !== null) {
      const end = line.trim() === heredoc;
      if (end) heredoc = null;
      // The body of a here-document is the text of a file: its words, so an address in it breaks after its slashes.
      return { segments: end ? [seg(line, 'punct')] : words(line), prompt: '' };
    }
    const depth = blocks.length;
    const starts = !continued && depth === 0;
    if (line.trim() === '') {
      continued = false;
      return { segments: [], prompt: '' };
    }
    if (isComment(line, language)) {
      continued = false;
      return { segments: [seg(line, 'comment')], prompt: '' };
    }
    if (depth > 0 && line.trim() === blocks[depth - 1]) {
      blocks.pop();
      return { segments: [seg(line, 'punct')], prompt: '' };
    }
    const { segments, endsContinued, opensHeredoc, expectCommand } = commandWords(line, language, cont, starts || depth > 0 || (continued && expectNext));
    continued = endsContinued;
    expectNext = expectCommand;
    if (opensHeredoc) heredoc = opensHeredoc;
    const opens = /\(\)\s*([{(])\s*$/.exec(line);
    if (opens) blocks.push(opens[1] === '{' ? '}' : ')');
    return { segments, prompt: starts ? prompt : '' };
  });
}

/** Splits a command line into words, quotes and operators, and names each. */
function commandWords(
  line: string,
  language: CodeLanguage,
  cont: string,
  startsCommand: boolean,
): { segments: Segment[]; endsContinued: boolean; opensHeredoc: string | null; expectCommand: boolean } {
  const out: Segment[] = [];
  let expectCommand = startsCommand;
  let opensHeredoc: string | null = null;
  let endsContinued = false;
  const re = /(\s+)|(&&|\|\||[|;])|(<<-?\s*'?[A-Za-z_]+'?)|(>>?|<)|((?:"(?:[^"\\]|\\.)*"?|'[^']*'?|[^\s|;<>"'])+)/g;
  const trimmed = line.replace(/\s+$/, '');
  const lastWord = trimmed.slice(trimmed.lastIndexOf(' ') + 1);
  const hasContinuation = trimmed.length > 0 && lastWord === cont;
  const body = hasContinuation ? line.slice(0, line.lastIndexOf(cont)) : line;
  for (let m = re.exec(body); m; m = re.exec(body)) {
    const [w, space, op, here, redirect, word] = m;
    if (space) out.push(seg(space, 'plain'));
    else if (op) {
      out.push(seg(op, 'punct'));
      expectCommand = true;
    } else if (here) {
      out.push(seg(here, 'punct'));
      opensHeredoc = here.replace(/^<<-?\s*/, '').replace(/'/g, '');
    } else if (redirect) out.push(seg(redirect, 'punct'));
    else if (word) {
      out.push(...wordSegments(word, language, expectCommand));
      if (expectCommand && !/^[A-Za-z_][A-Za-z0-9_]*=/.test(word)) expectCommand = word === 'sudo';
    } else out.push(seg(w, 'plain'));
  }
  if (hasContinuation) {
    out.push(seg(line.slice(line.lastIndexOf(cont)), 'punct'));
    endsContinued = true;
  }
  return { segments: out, endsContinued, opensHeredoc, expectCommand };
}

/** One word of a command: the command itself, a flag (and its value after `=`), a variable, a quoted string or a value. */
function wordSegments(word: string, language: CodeLanguage, isCommand: boolean): Segment[] {
  const assign = /^([A-Za-z_][A-Za-z0-9_]*)(=)(.*)$/.exec(word);
  if (assign) return [seg(assign[1] ?? '', 'variable'), seg('=', 'punct'), ...quotedParts(assign[3] ?? '', language)];
  if (isCommand && language === 'powershell' && word.startsWith('$')) return [seg(word, 'variable')];
  if (isCommand && !/["']/.test(word)) return [seg(word, 'command')];
  if (/^-{1,2}[A-Za-z]/.test(word)) {
    const eq = word.indexOf('=');
    if (eq > 0) return [seg(word.slice(0, eq), 'flag'), seg('=', 'punct'), ...quotedParts(word.slice(eq + 1), language)];
    if (!/["']/.test(word)) return [seg(word, 'flag')];
  }
  return quotedParts(word, language);
}

/** The quoted and the bare parts of a word: a quote is a string, a variable is one, the rest an address or a value. */
function quotedParts(word: string, language: CodeLanguage): Segment[] {
  const out: Segment[] = [];
  for (const m of word.matchAll(/"(?:[^"\\]|\\.)*"?|'[^']*'?|[^"']+/g)) {
    const part = m[0];
    if (part.startsWith('"') || part.startsWith("'")) out.push(seg(part, 'string'));
    else if (language === 'powershell' && /^\$[A-Za-z_][\w:]*$/.test(part)) out.push(seg(part, 'variable'));
    else if (language === 'cmd' && /^%[A-Za-z_]\w*%$/.test(part)) out.push(seg(part, 'variable'));
    else out.push(seg(part, valueKind(part)));
  }
  return out;
}

// ---- Files ---------------------------------------------------------------------------------------------------------------------

function fileLine(line: string, language: CodeLanguage): Segment[] {
  if (line === '') return [];
  switch (language) {
    case 'ini':
      return iniLine(line);
    case 'yaml':
      return yamlLine(line);
    case 'xml':
      return xmlLine(line);
    case 'json':
      return jsonLine(line);
    default:
      return [seg(line, 'plain')];
  }
}

function words(text: string): Segment[] {
  return [...text.matchAll(/\s+|[^\s]+/g)].map((m) => seg(m[0], /^\s/.test(m[0]) ? 'plain' : valueKind(m[0])));
}

function iniLine(line: string): Segment[] {
  if (/^\s*[#;]/.test(line)) return [seg(line, 'comment')];
  const section = /^(\s*)(\[)([^\]]*)(\])(.*)$/.exec(line);
  if (section) return [seg(section[1] ?? '', 'plain'), seg('[', 'punct'), seg(section[3] ?? '', 'key'), seg(']', 'punct'), ...words(section[5] ?? '')];
  const kv = /^(\s*)([^\s=[\]]+)(\s*=\s*)(.*)$/.exec(line);
  if (kv) {
    const value = kv[4] ?? '';
    return [seg(kv[1] ?? '', 'plain'), seg(kv[2] ?? '', 'key'), seg(kv[3] ?? '', 'punct'), ...(value.startsWith('"') ? [seg(value, 'string')] : words(value))];
  }
  return words(line);
}

function yamlLine(line: string): Segment[] {
  if (/^\s*#/.test(line)) return [seg(line, 'comment')];
  if (/^(---|\.\.\.)\s*$/.test(line)) return [seg(line, 'punct')];
  const m = /^(\s*)(- )?([^\s:#"'][^:#]*?)(:)(\s.*|)$/.exec(line);
  const out: Segment[] = [];
  if (!m) {
    const item = /^(\s*)(- )(.*)$/.exec(line);
    if (item) return [seg(item[1] ?? '', 'plain'), seg(item[2] ?? '', 'punct'), ...yamlValue(item[3] ?? '')];
    return yamlValue(line);
  }
  out.push(seg(m[1] ?? '', 'plain'));
  if (m[2]) out.push(seg(m[2], 'punct'));
  out.push(seg(m[3] ?? '', 'key'), seg(':', 'punct'));
  const rest = m[5] ?? '';
  const lead = /^\s*/.exec(rest)?.[0] ?? '';
  if (lead) out.push(seg(lead, 'plain'));
  out.push(...yamlValue(rest.slice(lead.length)));
  return out;
}

function yamlValue(value: string): Segment[] {
  if (value === '') return [];
  if (value.startsWith('"') || value.startsWith("'")) return [seg(value, 'string')];
  return [seg(value, valueKind(value))];
}

function xmlLine(line: string): Segment[] {
  const out: Segment[] = [];
  for (const m of line.matchAll(/<!--.*?(?:-->|$)|<[^>]*>?|[^<]+/g)) {
    const part = m[0];
    if (part.startsWith('<!--')) out.push(seg(part, 'comment'));
    else if (part.startsWith('<')) out.push(...xmlTag(part));
    else out.push(seg(part, 'plain'));
  }
  return out;
}

function xmlTag(tag: string): Segment[] {
  const out: Segment[] = [];
  const head = /^(<[/?]?)([^\s/>?]*)/.exec(tag);
  if (!head) return [seg(tag, 'plain')];
  out.push(seg(head[1] ?? '', 'punct'), seg(head[2] ?? '', 'command'));
  const rest = tag.slice(head[0].length);
  for (const m of rest.matchAll(/(\s+)|([^\s=/>?"]+)|(=)|("[^"]*"?)|([/?]?>|[/?])/g)) {
    const [, space, name, eq, value, close] = m;
    if (space) out.push(seg(space, 'plain'));
    else if (name) out.push(seg(name, 'key'));
    else if (eq) out.push(seg(eq, 'punct'));
    else if (value) out.push(seg(value, 'string'));
    else if (close) out.push(seg(close, 'punct'));
  }
  return out;
}

function jsonLine(line: string): Segment[] {
  const out: Segment[] = [];
  for (const m of line.matchAll(/("(?:[^"\\]|\\.)*")(\s*:)?|([{}[\],:])|(\s+)|([^\s{}[\],:"]+)/g)) {
    const [, str, colon, punct, space, bare] = m;
    if (str) {
      out.push(seg(str, colon ? 'key' : 'string'));
      if (colon) out.push(seg(colon, 'punct'));
    } else if (punct) out.push(seg(punct, 'punct'));
    else if (space) out.push(seg(space, 'plain'));
    else if (bare) out.push(seg(bare, 'plain'));
  }
  return out;
}
