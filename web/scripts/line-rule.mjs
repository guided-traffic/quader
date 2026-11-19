// R-UI-175, Q-UI-725: a control or an icon beside a text stands on the middle of the text's first line. `vertical-align: middle`
// puts a box's middle on the middle of the lower-case letters, which is 1.5 to 1.7 px under the middle of the line for the app's
// type (measured in web/e2e/renders/layout-checks.spec.ts); `align-items: last baseline` stood a mark 3.5 px above it. Every icon,
// badge, shield and code of the app that was placed so stood off its line (the renders of W-23, 2026-11-19). The line rule of
// _controls.scss does it instead: `vertical-align: top` with margins of half the difference to the line (`--quader-line`). The rule
// refuses both declarations in a stylesheet or a component's styles, except `vertical-align` on a table's cell, where it aligns
// the cell's content, not a box on a line. Run by check-rules.mjs over src, tested by lint-rules.test.mjs.

/**
 * The declarations of a stylesheet or a component's source that place a box off the line, each as its 1-based line and its text.
 * @param {string} text
 * @returns {{ line: number, text: string }[]}
 */
export function offTheLine(text) {
  const found = [];
  // The selectors of the open rules: a declaration belongs to the innermost one.
  const selectors = [];
  let pending = '';
  const lines = text.split('\n');
  lines.forEach((raw, i) => {
    const line = raw.replace(/\/\/.*$/, '').replace(/\/\*.*?\*\//g, '');
    for (const part of line.split(/([{}])/)) {
      if (part === '{') {
        selectors.push(pending.trim());
        pending = '';
      } else if (part === '}') {
        selectors.pop();
        pending = '';
      } else {
        const declaration = /^\s*(vertical-align\s*:\s*middle|align-items\s*:\s*last\s+baseline)\b/.exec(part);
        if (!declaration) {
          // A selector may span lines; a declaration ends with a semicolon.
          if (!part.includes(';')) pending += ` ${part}`;
          else pending = '';
          continue;
        }
        const selector = selectors[selectors.length - 1] ?? '';
        // The last compound of each selector of the list: a table's cell (`th`, `td`, `thead th`, `td.num`) aligns its content.
        const cell = selector
          .split(',')
          .every((s) => /(^|[\s>+~])(&)?(th|td)([.:[][^\s>+~]*)?\s*$/.test(s.trim()) || /^&?(th|td)\b/.test(s.trim()));
        if (/^vertical-align/.test(declaration[1]) && cell && selector !== '') continue;
        found.push({ line: i + 1, text: raw.trim() });
      }
    }
  });
  return found;
}
