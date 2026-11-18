// Q-UI-560: an icon in a table cell stands in one element with its word. A status is the badge: its icon and its word in a span
// with .ok, .warn, .bad or .badge (_controls.scss draws it as one pill that never breaks); an icon before a text that may wrap (a
// verdict's reason) is `.icon-name`, the icon in a column of its own beside the text (Q-UI-425). An icon that stands directly in
// the cell, beside its word as a text node or a sibling span, breaks from it where the column is narrow ("check_circle" on one
// line, "succeeded" on the next, the jobs of 2026-11-18). The rule refuses a `material-icons` element that is a direct child of a
// `<td>` or a `<th>`, unless it is an image of its own (`role="img"` with its accessible name); the control flow of a template
// (`@if`, `@switch`, `@case`) is no element. Run by check-rules.mjs over src/app, tested by lint-rules.test.mjs.

const voidTags = new Set(['area', 'base', 'br', 'col', 'embed', 'hr', 'img', 'input', 'link', 'meta', 'source', 'track', 'wbr']);

/**
 * The icons of a template text that stand directly in a table cell, each as its 1-based line and its tag.
 * @param {string} text
 * @returns {{ line: number, tag: string }[]}
 */
export function bareIconsInCells(text) {
  const found = [];
  const tags = /<(\/?)([a-zA-Z][\w-]*)\b((?:[^>"']|"[^"]*"|'[^']*')*?)(\/?)>/g;
  // The open elements; a cell's depth is where it was opened, so a direct child is one level below it.
  const stack = [];
  for (const m of text.matchAll(tags)) {
    const [whole, closing, rawName, attrs, selfClosing] = m;
    const name = rawName.toLowerCase();
    if (closing) {
      const at = stack.lastIndexOf(name);
      if (at >= 0) stack.length = at;
      continue;
    }
    const parent = stack[stack.length - 1];
    if ((parent === 'td' || parent === 'th') && /\bclass="[^"]*\bmaterial-icons\b/.test(attrs) && !/\brole="img"/.test(attrs)) {
      found.push({ line: text.slice(0, m.index).split('\n').length, tag: whole });
    }
    if (!selfClosing && !voidTags.has(name)) stack.push(name);
  }
  return found;
}
