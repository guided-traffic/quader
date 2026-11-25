// Q-UI-1211: the monospace face is the one of the token `--quader-font-mono` (_tokens.scss), which main.scss gives `code`, `kbd`, `pre`
// and `samp` and which a class names where another element holds monospace text (a field that holds an address). A stylesheet that
// writes a face of its own (`monospace`, `ui-monospace`, Menlo, Courier, …) stands beside the token and drifts from it: four did, each
// with another stack, while `code` itself had none and stood in the browser's default (Courier on macOS). The rule refuses a
// `font-family` or a `font` declaration that names a monospace face anywhere but in the token itself. Run by check-rules.mjs over src,
// tested by lint-rules.test.mjs.

/** A monospace face by name: the generic family, the system ones and the faces of the stacks that stood in the app. */
const MONO = /\b(monospace|ui-monospace|SFMono-Regular|SF Mono|Menlo|Monaco|Consolas|Courier|DejaVu Sans Mono|Liberation Mono|Ubuntu Mono)\b/i;

/**
 * The declarations of a stylesheet or a component's source that name a monospace face of their own, each as its 1-based line and its
 * text. The token's definition (`--quader-font-mono: …`) is not a declaration of a face for an element.
 * @param {string} text
 * @returns {{ line: number, text: string }[]}
 */
export function ownMonospace(text) {
  const found = [];
  text.split('\n').forEach((raw, i) => {
    const line = raw.replace(/\/\/.*$/, '').replace(/\/\*.*?\*\//g, '');
    const declaration = /(?:^|[\s;{])(font-family|font)\s*:\s*([^;]*)/.exec(line);
    if (declaration && MONO.test(declaration[2])) found.push({ line: i + 1, text: raw.trim() });
  });
  return found;
}
