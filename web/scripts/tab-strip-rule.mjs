// R-UI-173, Q-UI-1074: every tab strip scrolls the same way where its tabs do not fit, by the directive `TabStrip`
// (src/app/shared/ui/tab-strip.ts, the attribute `quaderTabStrip`). A strip without it scrolls without its fades and never brings
// the current tab into view. The rule refuses an element of the class `tab-nav` that does not carry the attribute, and a component
// whose template (inline, or its templateUrl's file) has one while its imports lack `TabStrip`, where the attribute does nothing.
// Run by check-rules.mjs over src/app, tested by lint-rules.test.mjs.

const tags = /<([a-zA-Z][\w-]*)\b((?:[^>"']|"[^"]*"|'[^']*')*)>/g;
const stripClass = /\bclass="(?:[^"]*\s)?tab-nav(?:\s[^"]*)?"/;

/**
 * What is wrong with the tab strips of a component: each strip without the attribute as its tag, and whether the component lacks
 * the import while it has a strip.
 * @param {string} source the component's TypeScript
 * @param {string} [template] the text of its templateUrl's file, if it has one
 * @returns {{ unmarked: string[], unimported: boolean }}
 */
export function tabStripFindings(source, template = '') {
  if (!/@Component\(/.test(source)) return { unmarked: [], unimported: false };
  const strips = [...`${source}\n${template}`.matchAll(tags)].filter((m) => stripClass.test(m[2]));
  const unmarked = strips.filter((m) => !/(^|\s)quaderTabStrip(\s|=|$)/.test(m[2])).map((m) => m[0].split('\n')[0].trim());
  const imports = /\bimports:\s*\[([^\]]*)\]/.exec(source);
  const unimported = strips.length > 0 && (!imports || !/\bTabStrip\b/.test(imports[1]));
  return { unmarked, unimported };
}
