// R-UI-217: every load of remote content is accompanied by the spinner the owner picked, the mark's six stones (Q-UI-1229), from one
// component and one mechanism: `<quader-loading>` for a sentence that says what loads, `[quaderBusy]` for a button whose act waits for
// the server, the data table's `loadingText` for a list (src/app/shared/ui/). On 2026-11-25 about fifty places said in a bare paragraph
// that something loaded, without a spinner, and a dozen acts showed nothing at all while they waited.
//
// The rule refuses, in a template (Q-UI-1239):
//  - a loading sentence that is not `<quader-loading>`: an element with an i18n ID whose text starts with "Loading", "Reading",
//    "Searching", "Checking", "Testing", "Fetching" or "Redirecting" (an `<option>` excepted: a select's one option while its choices
//    load, whose field draws the spinner itself);
//  - a `<quader-data-table>` that binds `[loading]` without a `loadingText`, the sentence of what it loads;
//  - a `<button>` whose `[disabled]` reads a flag of an act (`busy()`, `saving()`, `creating()`, `testing()`, `deleting()`, `running()`,
//    `pending()`, `checking()`, `searching()`, `submitting()`, `sending()`, `working()`, `loading()`, and their longer names) without
//    `quaderBusy`, unless it closes or cancels (`quaderDialogClose`, a `(click)` of close, cancel, dismiss, back, reset) or stands in
//    `allowed` with its reason;
// and, in a stylesheet or a component's styles, a `@keyframes` that turns something (`rotate`): a spinner of its own beside the mark.
//
// It reads text, not behaviour: it cannot see a load that shows nothing at all (a section that stays empty until its read answers), an
// act whose button is not disabled while it runs, a flag of another name, or a sentence built in code and bound as `{{ text }}`. Those
// are the inventory of R-UI-217 and the render walk `loading-states`. Run by check-rules.mjs over src, tested by lint-rules.test.mjs.

/** The verbs a sentence of a load starts with. */
const LOAD_VERB = /^(Loading|Reading|Searching|Checking|Testing|Fetching|Redirecting)\b/;

/** A flag of an act, as a template reads it. */
const ACT_FLAG = /\b(?:[a-z]+\.)?(busy|saving|creating|testing|deleting|running|pending|checking|searching|submitting|sending|working|loading)[A-Za-z]*\(\)/;

/** A button that ends a dialog or undoes the form's typing: it acts on nothing of the server's. */
const CLOSES = /quaderDialogClose|\(click\)="\s*(?:close|cancel|dismiss|dialogRef\.close|ref\.close|back|reset)\b/;

/**
 * Buttons that are disabled while an act runs and are not the act, by their file under web/ and a text of the tag, each with its reason.
 * @type {readonly { file: string, tag: string, why: string }[]}
 */
export const allowed = [
  {
    file: 'src/app/features/repositories/content/upload-dialog.ts',
    tag: 'chooser.click()',
    why: 'Choose files is held while files upload; the upload is the act, and each file shows its own progress',
  },
  {
    file: 'src/app/features/repositories/content/upload-dialog.ts',
    tag: '(click)="start()"',
    why: 'Start is held while nothing waits; the files go through the upload client of R-UI-012, and each row shows its progress',
  },
  {
    file: 'src/app/features/repositories/deb-signing-keys.ts',
    tag: 'importing.set(',
    why: 'Import opens the form in place; its submit is the act and carries quaderBusy',
  },
  {
    file: 'src/app/features/profile/profile-avatar.html',
    tag: 'chooser.click()',
    why: "Choose image opens the browser's file picker; the upload that follows is held by the page's notice and Remove's state",
  },
  {
    file: 'src/app/features/ci/name-picker.ts',
    tag: 'add(field)',
    why: 'Add is held while the names to choose from load (the field shows the spinner); adding a name sends nothing',
  },
  {
    file: 'src/app/features/repositories/virtual-members.html',
    tag: '(click)="discard()"',
    why: 'Discard puts the members back as they were loaded; nothing is sent',
  },
];

/** Where a tag ends, past the quoted attribute values. */
function tagEnd(text, i) {
  let quote = null;
  for (; i < text.length; i++) {
    const c = text[i];
    if (quote) {
      if (c === quote) quote = null;
    } else if (c === '"' || c === "'") quote = c;
    else if (c === '>') return i;
  }
  return -1;
}

const lineOf = (text, index) => text.slice(0, index).split('\n').length;

/**
 * The loading sentences of a template text that are no `<quader-loading>`.
 * @param {string} text
 * @returns {{ line: number, tag: string }[]}
 */
export function bareLoadingSentences(text) {
  const found = [];
  for (const m of text.matchAll(/<([a-z][\w-]*)\b([^>]*\bi18n="@@[^"]+"[^>]*)>\s*([^<]*)/g)) {
    const [, name, , content] = m;
    if (name === 'quader-loading' || name === 'option') continue;
    if (LOAD_VERB.test(content.trim())) found.push({ line: lineOf(text, m.index), tag: `<${name}> ${content.trim()}` });
  }
  return found;
}

/**
 * The data tables of a template text that bind `[loading]` without a `loadingText`.
 * @param {string} text
 * @returns {{ line: number, tag: string }[]}
 */
export function tablesWithoutLoadingText(text) {
  const found = [];
  for (const m of text.matchAll(/<quader-data-table\b/g)) {
    const end = tagEnd(text, m.index + m[0].length);
    const tag = text.slice(m.index, end);
    if (/\[loading\]=/.test(tag) && !/\bloadingText\b|\[loadingText\]/.test(tag)) {
      found.push({ line: lineOf(text, m.index), tag: '<quader-data-table [loading]>' });
    }
  }
  return found;
}

/**
 * The buttons of a template text that are disabled while an act runs and carry no `quaderBusy`.
 * @param {string} text
 * @returns {{ line: number, tag: string }[]}
 */
export function actsWithoutSpinner(text) {
  const found = [];
  for (const m of text.matchAll(/<button\b/g)) {
    const end = tagEnd(text, m.index + m[0].length);
    const tag = text.slice(m.index, end + 1);
    const disabled = /\[disabled\]="([^"]*)"/.exec(tag);
    if (!disabled || !ACT_FLAG.test(disabled[1]) || /quaderBusy/.test(tag) || CLOSES.test(tag)) continue;
    found.push({ line: lineOf(text, m.index), tag: tag.replace(/\s+/g, ' ') });
  }
  return found;
}

/**
 * The spins of a stylesheet text that are not the mark's: a `@keyframes` whose frames turn something.
 * @param {string} text
 * @returns {{ line: number, tag: string }[]}
 */
export function spinsOfTheirOwn(text) {
  const found = [];
  for (const m of text.matchAll(/@keyframes\s+([\w-]+)\s*\{/g)) {
    let depth = 1;
    let i = m.index + m[0].length;
    for (; i < text.length && depth > 0; i++) {
      if (text[i] === '{') depth++;
      else if (text[i] === '}') depth--;
    }
    if (/\brotate[XYZ3d]*\s*\(|\brotate\s*:/.test(text.slice(m.index, i))) found.push({ line: lineOf(text, m.index), tag: `@keyframes ${m[1]}` });
  }
  return found;
}

/**
 * The findings of a file under web/: its loading sentences, tables and acts without the spinner, less what `allowed` names for it, and
 * its spins of its own.
 * @param {string} file the path under web/, with forward slashes
 * @param {string} text
 * @returns {{ line: number, tag: string, problem: string }[]}
 */
export function busyFindings(file, text) {
  const out = [];
  if (/\.(scss|css|ts)$/.test(file)) {
    for (const f of spinsOfTheirOwn(text)) out.push({ ...f, problem: 'a spin of its own; a load shows the mark (<quader-spinner>, _spinner.scss)' });
  }
  if (/\.(html|ts)$/.test(file)) {
    for (const f of bareLoadingSentences(text)) out.push({ ...f, problem: 'a loading sentence without the spinner; write it as <quader-loading i18n="@@…">…</quader-loading>' });
    for (const f of tablesWithoutLoadingText(text)) out.push({ ...f, problem: 'a list that loads without its sentence; give it loadingText="Loading the …." with i18n-loadingText' });
    for (const f of actsWithoutSpinner(text)) {
      if (allowed.some((a) => a.file === file && f.tag.includes(a.tag))) continue;
      out.push({ ...f, problem: 'a button held while its act runs, without the spinner; give it [quaderBusy] with the same flag' });
    }
  }
  return out;
}
