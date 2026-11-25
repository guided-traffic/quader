// R-UI-214: a screen never shows a sentence the server wrote as its own. The server sends a code and the screen says it in its
// language with $localize; a server's raw diagnostic text (an error as a handler returned it, a verifier's note) is shown only
// folded under **Technical details** (src/app/shared/technical-details.ts), which takes it as an input. Three English server
// sentences stood in German pages on one day (the upstream search's hint, a failed bulk's detail, the vulnerability trend's basis),
// each bound in a template as `{{ x.basis }}`.
//
// The rule refuses an interpolation of a member whose name the API gives to its English sentences (`detail`, `basis`, `message`,
// `lastMessage`, `lastError`, `discoveryNote`, `explanation`, `withheldNote`, `levelsReason`, `searchNote`). It reads names, not
// types: a screen's own text in a member of that name, and the bindings another package owns, stand in `allowed` with their reason.
// It cannot see a server's sentence that reaches a template through a computed value or a member of another name; the German render
// pass's check `english` (web/e2e/renders/layout.ts) looks at the rendered page for that. Run by check-rules.mjs over src/app,
// tested by lint-rules.test.mjs.

const binding =
  /\{\{\s*([A-Za-z_$][\w$]*(?:\(\))?(?:[?!]?\.[A-Za-z_$][\w$]*(?:\(\))?)*[?!]?\.(detail|basis|message|lastMessage|lastError|discoveryNote|explanation|withheldNote|levelsReason|searchNote))\s*(?:\?\?[^}]*)?\}\}/g;

/**
 * The bindings a file may keep, by its path under web/ and the bound expression, each with why it is no server sentence or whose it is.
 * @type {readonly { file: string, expression: string, why: string }[]}
 */
export const allowed = [
  {
    file: 'src/app/shared/field-errors.ts',
    expression: 'item.message',
    why: 'the translated message of a field problem (fieldMessage); an unknown code falls back to the detail by R-UI-042',
  },
  {
    file: 'src/app/features/repositories/content/upload-dialog.ts',
    expression: 'e.message',
    why: "the screen's own message of a failed upload (describeProblem, or its own sentence)",
  },
  {
    file: 'src/app/shared/error-notice.ts',
    expression: 'f.detail',
    why: "a field problem's English sentence among the facts of a failed answer, folded under Technical details and marked lang=en translate=no (R-UI-216)",
  },
  {
    file: 'src/app/features/repositories/ansible-import-tasks.ts',
    expression: 'm.message',
    why: "the import's log as the Galaxy import API answers it to ansible-galaxy, shown as a log with its level",
  },
  {
    file: 'src/app/features/packages/ansible-import-log.ts',
    expression: 'm.message',
    why: "the import's log as the Galaxy import API answers it to ansible-galaxy, shown as a log with its level",
  },
  {
    file: 'src/app/shared/upstream-downloads/upstream-downloads-panel.html',
    expression: 's.detail',
    why: "a download's subject beside its name (subjectOf: the version, or the file's path), no sentence",
  },
];

/**
 * The interpolations of a server's sentence in a template text, each as its 1-based line and the bound expression.
 * @param {string} text
 * @returns {{ line: number, expression: string }[]}
 */
export function serverTextBindings(text) {
  const found = [];
  for (const m of text.matchAll(binding)) {
    found.push({ line: text.slice(0, m.index).split('\n').length, expression: m[1].replace(/[?!]\./g, '.') });
  }
  return found;
}

/**
 * The findings of a file: its bindings less those `allowed` names for it.
 * @param {string} file the path under web/, with forward slashes
 * @param {string} text
 */
export function serverTextFindings(file, text) {
  return serverTextBindings(text).filter((b) => !allowed.some((a) => a.file === file && a.expression === b.expression));
}
