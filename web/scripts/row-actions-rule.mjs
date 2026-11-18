// R-UI-163, Q-UI-640: a row with one action names it, a row with several has a menu. The menu is the `more_vert` button with
// `cdkMenuTriggerFor` and its entries are `.menu-item`s; beside it at most one frequent action stands on its own (the copy
// button, a project's repository list's Set up client). So a row never shows two words side by side: the rule refuses a table
// cell or a list item that holds two buttons or links in the form of a word (`btn` without `btn-icon`) at the same time. The
// branches of a template (`@else`, `@case`, `@default`) are counted apart, because only one of them is shown; `@if` blocks one
// after the other are counted together, because they can be shown together. Run by check-rules.mjs over src/app with the
// allow-list there, tested by lint-rules.test.mjs.

const voidTags = new Set(['area', 'base', 'br', 'col', 'embed', 'hr', 'img', 'input', 'link', 'meta', 'source', 'track', 'wbr']);
const rowTags = new Set(['td', 'th', 'li']);

/**
 * The rows of a template text that show two or more actions as words at once, each as the 1-based line of the second word and
 * its tag.
 * @param {string} text
 * @returns {{ line: number, tag: string }[]}
 */
export function wordActionsSideBySide(text) {
  const found = [];
  const tokens = /<(\/?)([a-zA-Z][\w-]*)\b((?:[^>"']|"[^"]*"|'[^']*')*?)(\/?)>|@(?:else|case|default)\b/g;
  // The open elements; each open row cell carries the number of words seen in its current branch.
  const stack = [];
  for (const m of text.matchAll(tokens)) {
    const [whole, closing, rawName, attrs, selfClosing] = m;
    if (rawName === undefined) {
      // Another branch of a control-flow block: what the branch before it showed is not shown with it.
      const cell = [...stack].reverse().find((e) => rowTags.has(e.name));
      if (cell) cell.words = 0;
      continue;
    }
    const name = rawName.toLowerCase();
    if (closing) {
      const at = stack.map((e) => e.name).lastIndexOf(name);
      if (at >= 0) stack.length = at;
      continue;
    }
    const isWord = (name === 'button' || name === 'a') && /\bclass="(?:[^"]*\s)?btn(?:\s[^"]*)?"/.test(attrs) && !/\bclass="[^"]*\bbtn-icon\b/.test(attrs);
    if (isWord) {
      const cell = [...stack].reverse().find((e) => rowTags.has(e.name));
      if (cell && ++cell.words === 2) found.push({ line: text.slice(0, m.index).split('\n').length, tag: whole });
    }
    if (!selfClosing && !voidTags.has(name)) stack.push({ name, words: 0 });
  }
  return found;
}
