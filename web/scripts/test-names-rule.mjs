// A test's name carries the number of the requirement it holds (`R-UI-…`, `R-CUR-…`), never a decision's (`Q-…`, `ADR-…`):
// the evidence record reads the requirement IDs from the names of the tests, and a decision's number in a name points at the
// reasoning instead of at what is held (web-ui.md, "Write specs next to the code"). The rule reads the title of every
// `describe`, `it` and `test` call of a spec or a journey, with their modifiers (`test.describe`, `it.only`, `it.each(…)(…)`,
// `test.step`), including a title that is a concatenation or a template literal, and reports a title that carries a `Q-` or an
// `ADR-` number. Run by check-rules.mjs over web/src, web/e2e and web/scripts, tested by lint-rules.test.mjs.

const decisionNumber = /\b(Q-[A-Z][A-Z0-9]*-\d+|ADR-\d+)\b/;

/**
 * The titles of the test calls in a source text, each as its 1-based line and its text (a template literal's `${…}` kept as
 * written).
 * @param {string} text
 * @returns {{ line: number, title: string }[]}
 */
export function testTitles(text) {
  const titles = [];
  const call = /(describe|it|test)((?:\.[A-Za-z]+)*)\s*\(/y;
  for (let at = 0; at < text.length; ) {
    // Strings, comments and regular expressions are stepped over whole: a test's fixture may hold the source of another test.
    const skipped = skipNonCode(text, at);
    if (skipped !== at) {
      at = skipped;
      continue;
    }
    call.lastIndex = at;
    const m = /[\w$.]/.test(text[at - 1] ?? '') ? null : call.exec(text);
    if (!m) {
      at += 1;
      continue;
    }
    at = m.index + m[0].length;
    const modifiers = m[2];
    // Hooks and settings of a test file take no title.
    if (/\.(beforeAll|afterAll|beforeEach|afterEach|use|info|setTimeout|slow|extend|expect)\b/.test(modifiers)) continue;
    let i = at;
    if (/\.each$/.test(modifiers)) {
      // `it.each(table)(title, …)`: the title is the first argument of the second call.
      i = skipBalanced(text, i - 1);
      if (i < 0) continue;
      i = skipSpace(text, i);
      if (text[i] !== '(') continue;
      i += 1;
    }
    const start = skipSpace(text, i);
    const parts = [];
    let j = start;
    for (;;) {
      const literal = readString(text, j);
      if (!literal) break;
      parts.push(literal.value);
      j = skipSpace(text, literal.end);
      if (text[j] !== '+') break;
      j = skipSpace(text, j + 1);
    }
    if (parts.length === 0) continue;
    titles.push({ line: text.slice(0, start).split('\n').length, title: parts.join('') });
  }
  return titles;
}

/**
 * The test titles of a source text that carry a decision's number.
 * @param {string} text
 * @returns {{ line: number, title: string }[]}
 */
export function decisionNumbersInTestNames(text) {
  return testTitles(text).filter(({ title }) => decisionNumber.test(title));
}

// At a string, a comment or a regular expression literal that starts at i, the index after it; otherwise i.
function skipNonCode(text, i) {
  const c = text[i];
  if (c === "'" || c === '"' || c === '`') {
    const literal = readString(text, i);
    return literal ? literal.end : i + 1;
  }
  if (c !== '/') return i;
  if (text[i + 1] === '/') {
    const end = text.indexOf('\n', i);
    return end < 0 ? text.length : end;
  }
  if (text[i + 1] === '*') {
    const end = text.indexOf('*/', i + 2);
    return end < 0 ? text.length : end + 2;
  }
  // A slash after an operand divides; after an operator or an opening bracket it starts a regular expression.
  let p = i - 1;
  while (p >= 0 && /\s/.test(text[p])) p -= 1;
  if (p >= 0 && !/[(,=:[!&|?{};+\-*%<>~^]/.test(text[p]) && !/\b(return|typeof|case)$/.test(text.slice(0, p + 1))) return i;
  let inClass = false;
  for (let j = i + 1; j < text.length; j += 1) {
    const d = text[j];
    if (d === '\\') j += 1;
    else if (d === '\n') return i + 1;
    else if (d === '[') inClass = true;
    else if (d === ']') inClass = false;
    else if (d === '/' && !inClass) return j + 1;
  }
  return i + 1;
}

function skipSpace(text, i) {
  for (;;) {
    while (i < text.length && /\s/.test(text[i])) i += 1;
    if (text.startsWith('//', i)) {
      const end = text.indexOf('\n', i);
      i = end < 0 ? text.length : end + 1;
    } else if (text.startsWith('/*', i)) {
      const end = text.indexOf('*/', i + 2);
      i = end < 0 ? text.length : end + 2;
    } else {
      return i;
    }
  }
}

// From an opening parenthesis at i, the index after its closing one, or -1; strings inside are skipped.
function skipBalanced(text, i) {
  let depth = 0;
  while (i < text.length) {
    const c = text[i];
    if (c === "'" || c === '"' || c === '`') {
      const literal = readString(text, i);
      if (!literal) return -1;
      i = literal.end;
      continue;
    }
    if (c === '(' || c === '[' || c === '{') depth += 1;
    else if (c === ')' || c === ']' || c === '}') {
      depth -= 1;
      if (depth === 0) return i + 1;
    }
    i += 1;
  }
  return -1;
}

// A string or template literal that starts at i: its value (escapes resolved for quotes, `${…}` kept) and the index after it.
function readString(text, i) {
  const quote = text[i];
  if (quote !== "'" && quote !== '"' && quote !== '`') return null;
  let value = '';
  let j = i + 1;
  while (j < text.length) {
    const c = text[j];
    if (c === '\\') {
      value += text[j + 1] ?? '';
      j += 2;
      continue;
    }
    if (c === quote) return { value, end: j + 1 };
    if (quote === '`' && c === '$' && text[j + 1] === '{') {
      const end = skipBalanced(text, j + 1);
      if (end < 0) return null;
      value += text.slice(j, end);
      j = end;
      continue;
    }
    if (c === '\n' && quote !== '`') return null;
    value += c;
    j += 1;
  }
  return null;
}
