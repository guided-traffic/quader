// R-UI-171: a separator drawn by CSS (the `content` of a pseudo-element: a middle dot, a slash, a chevron, a bullet, a bar) is drawn
// in one of two ways, so that none starts a line unseen:
//   - before the thing it separates, by `.dot-before::before` or `.dot-separated > *::before` (_controls.scss): the render check
//     `separator-start` (web/e2e/renders/layout.ts) measures these, and `.dot-separated` hides each line's first dot by its clip;
//   - after the thing before it, by a `:not(:last-child)::after` (a breadcrumb's slash or chevron, a folder's path): it ends a line
//     and never starts one.
// "/ Set up client" started the second line of a breadcrumb at 390 px (`li + li::before`, 2026-11-25), and "· found 1 hour ago" a line
// on the page Malware (2026-11-23). Run by check-rules.mjs over src, tested by lint-rules.test.mjs.

/** The glyphs of a separator, as a character or as a CSS escape. */
const GLYPH = /^\s*content\s*:\s*(['"])\s*(·|•|\/|›|»|\||\\0*(b7|2022|2f|203a|bb|7c)\s*)\s*\1/i;

/** The selectors that may draw a separator: the end of the rule's selector, its nesting resolved by joining. */
const ALLOWED = [/\.dot-before::before$/, /\.dot-separated\s*>\s*\*::before$/, /:not\(:last-child\)::after$/];

/**
 * The declarations of a stylesheet or a component's source that draw a separator outside the app's separator rules, each as its
 * 1-based line and its text.
 * @param {string} text
 * @returns {{ line: number, text: string }[]}
 */
export function separatorsOutsideTheRules(text) {
  const found = [];
  const selectors = [];
  let pending = '';
  text.split('\n').forEach((raw, i) => {
    const line = raw.replace(/\/\/.*$/, '').replace(/\/\*.*?\*\//g, '');
    for (const part of line.split(/([{}])/)) {
      if (part === '{') {
        selectors.push(pending.trim());
        pending = '';
      } else if (part === '}') {
        selectors.pop();
        pending = '';
      } else if (GLYPH.test(part)) {
        // A nested rule's `&` joins its parent without a space; every selector of a list is a rule of its own.
        const innermost = selectors[selectors.length - 1] ?? '';
        const parents = selectors.slice(0, -1).join(' ');
        const each = innermost.split(',').map((s) => {
          const own = s.trim();
          return own.startsWith('&') ? `${parents}${own.slice(1)}` : `${parents} ${own}`;
        });
        const ok = each.length > 0 && each.every((s) => ALLOWED.some((re) => re.test(s.trim())));
        if (!ok) found.push({ line: i + 1, text: raw.trim() });
        pending = '';
      } else if (!part.includes(';')) {
        // A selector may span lines; a declaration ends with a semicolon.
        pending += ` ${part}`;
      } else {
        pending = '';
      }
    }
  });
  return found;
}
