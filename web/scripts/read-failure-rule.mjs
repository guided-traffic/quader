// R-UI-216, Q-UI-1195: a failed read's notice stays in place while the read is tried again. Its failure is cleared by the read's
// answer, never by its start: a screen whose failure became null at the start of the retry showed "Loading." in the notice's place
// and the notice again after it, so the notice could neither say that it tries again nor count its attempts (H-1963, about fifty
// screens, the repository's settings among them).
//
// The rule refuses the two forms that did it:
//  - a resource's failure made by hand (`computed` of `x.error()` through asApiError, asFailure, toApiError or `instanceof ApiError`,
//    or a notice bound to `fn(e)` of `@if (x.error(); as e)`): it is `failureOf` of src/app/shared/paged-resource.ts, which keeps the
//    failure while the resource reads again and whose `retry` reads the page's other failed reads with it;
//  - a read of its own whose notice's Retry clears the notice's failure before the read answers: `@if (loadError(); as e)` with
//    `<quader-error-notice [error]="e" (retry)="load()">`, or `<quader-data-table [error]="error()" (retry)="load()">`, and
//    `this.loadError.set(null)` in `load` (or a method it calls) before its first `await` or `.then(`. The failure is set to null
//    after the answer instead.
// It reads text, not types: a failure that reaches a notice by another way (a member of another name, a read in a service) is not
// seen. PagedResource keeps its failure by itself. Run by check-rules.mjs over src/app, tested by lint-rules.test.mjs.

/** The 1-based line of an offset. */
function lineOf(text, offset) {
  return text.slice(0, offset).split('\n').length;
}

/** The text between the bracket at `open` and its partner, or null when it is not closed. */
function balanced(text, open, left, right) {
  let depth = 0;
  for (let i = open; i < text.length; i++) {
    if (text[i] === left) depth++;
    else if (text[i] === right) {
      depth--;
      if (depth === 0) return text.slice(open + 1, i);
    }
  }
  return null;
}

const converter = /\basApiError\(|\basFailure\(|\btoApiError\(|instanceof ApiError\b/;

/**
 * A resource's failure made by hand, each as its 1-based line and what it is.
 * @param {string} text a component's TypeScript or template
 * @returns {{ line: number, problem: string }[]}
 */
export function handMadeFailures(text) {
  const found = [];
  for (const m of text.matchAll(/\bcomputed\(/g)) {
    const body = balanced(text, m.index + m[0].length - 1, '(', ')');
    if (body !== null && /\.error\(\)/.test(body) && converter.test(body)) {
      found.push({ line: lineOf(text, m.index), problem: "a resource's failure made by hand; use failureOf(…) of shared/paged-resource.ts" });
    }
  }
  const inTemplate = /@(?:else\s+)?if\s*\(\s*[\w.]+\.error\(\);\s*as\s+(\w+)\s*\)\s*\{\s*<quader-error-notice\b[^>]*\[error\]="[\w.]+\(\s*(\w+)\s*\)"/g;
  for (const m of text.matchAll(inTemplate)) {
    if (m[1] === m[2]) {
      found.push({ line: lineOf(text, m.index), problem: "a resource's failure converted in the template; use failureOf(…) of shared/paged-resource.ts" });
    }
  }
  return found;
}

/** The body of a method of the class text, or null. */
function methodBody(ts, name) {
  const def = new RegExp(`\\n\\s*(?:(?:protected|private|public|readonly|async|override)\\s+)*${name}\\s*\\([^)]*\\)\\s*(?::[^{=]+)?\\{`, 'g');
  const m = def.exec(ts);
  if (!m) return null;
  return balanced(ts, m.index + m[0].length - 1, '{', '}');
}

/** The part of a body before its first wait for an answer. */
function beforeAnswer(body) {
  const at = body.search(/\bawait\b|\.then\(/);
  return at < 0 ? body : body.slice(0, at);
}

/** The text of the tag that starts at `at`, up to its closing `>` outside quotes. */
function tagAt(text, at) {
  let quote = null;
  for (let i = at; i < text.length; i++) {
    const c = text[i];
    if (quote) {
      if (c === quote) quote = null;
    } else if (c === '"' || c === "'") quote = c;
    else if (c === '>') return text.slice(at, i);
  }
  return text.slice(at);
}

/** Whether `retry` (a method of the class) clears `failure` before its read answers, itself or in a method it calls first. */
function clears(ts, retry, failure) {
  const body = methodBody(ts, retry);
  if (body === null) return false;
  const cleared = new RegExp(`this\\.${failure}\\.set\\(\\s*null\\s*\\)`);
  const start = beforeAnswer(body);
  const called = [...start.matchAll(/this\.(\w+)\(/g)].map((c) => methodBody(ts, c[1])).filter((b) => b !== null);
  return [start, ...called.map(beforeAnswer)].some((b) => cleared.test(b));
}

/**
 * The notices and lists of a read whose Retry clears their failure before the read answers, each as the 1-based line of the notice
 * or the list in the template and what it is: `@if (loadError(); as e) { <quader-error-notice [error]="e" (retry)="load()" />`, and
 * `<quader-data-table [error]="error()" (retry)="load()">`.
 * @param {string} template the component's template (its .html, or its .ts with the inline template)
 * @param {string} ts the component's TypeScript
 * @returns {{ line: number, problem: string }[]}
 */
export function clearedBeforeAnswer(template, ts) {
  const found = [];
  const report = (at, retry, failure) =>
    found.push({
      line: lineOf(template, at),
      problem: `${retry}() clears ${failure} before its read answers; set it to null after the answer, so the notice stays while it reads again`,
    });
  const notice = /@(?:else\s+)?if\s*\(\s*(\w+)\(\);\s*as\s+(\w+)\s*\)\s*\{\s*(?=<quader-error-notice\b)/g;
  for (const m of template.matchAll(notice)) {
    const [, failure, alias] = m;
    const tag = tagAt(template, m.index + m[0].length);
    if (!tag.includes(`[error]="${alias}"`)) continue;
    const retry = /\(retry\)="(\w+)\(/.exec(tag);
    if (retry && clears(ts, retry[1], failure)) report(m.index, retry[1], failure);
  }
  for (const m of template.matchAll(/<quader-data-table\b/g)) {
    const tag = tagAt(template, m.index);
    const failure = /\[error\]="(\w+)\(\)"/.exec(tag);
    const retry = /\(retry\)="(\w+)\(/.exec(tag);
    if (failure && retry && clears(ts, retry[1], failure[1])) report(m.index, retry[1], failure[1]);
  }
  return found;
}
