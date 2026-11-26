// R-UI-223: the reading parts of the package screens are views, the files below src/app/views/, which a second application composes
// without a session and without acts (the Bastion's web interface, W-35). A view reads through the abstract read classes of
// src/app/api/ (`PackageReads`, `VulnerabilityReads`, `SignatureReads`, `ClientSetupReads`), takes "signed in" as an input, and draws
// an act of the main installation only in a place the composing page fills (src/app/views/acts.ts).
//
// The rule refuses, in a TypeScript file below src/app/views/ (one directory: a new format's view falls under it by its place):
//  - an import of `SessionStore`, `ApiClient` or `QuaderDialog`;
//  - an import of an API service class, a name that ends in `Api` (`PackagesApi`, `SignaturesApi`): those classes carry the writes, the
//    acts; a view reads through a `…Reads` class;
//  - an import from a dialog's module (`…-dialog`, `ui/dialog`): a dialog is an act;
//  - an import from a module of src/app/features/ or src/app/shared/ that itself imports one of the above: an act behind a helper
//    (`GateActions`, `collectionUpload`).
//
// It reads import statements, one level deep: a module of core/ is the application's plumbing that each application binds itself
// (`FormatCatalog`, `SystemClients`), and what core/ and shared/ reach further is the import-graph check of the second application's
// build (W-35 P9). Type-only imports (`import type`) are no dependency and pass. Run by check-rules.mjs, tested by lint-rules.test.mjs.

/** The one place of the views, relative to web/. */
export const VIEWS = 'src/app/views/';

/** A name a view never imports. */
const BANNED_NAME = /^(SessionStore|ApiClient|QuaderDialog|[A-Za-z]+Api)$/;
/** A dialog's module. */
const DIALOG_MODULE = /(^|\/)([a-z0-9-]+-)?dialog$/;
/** The folders whose modules are looked into, one level. */
const LOOKED_INTO = /^src\/app\/(features|shared)\//;

/**
 * The value imports of a TypeScript text: each statement's names, its module and its line. `import type` statements and `type` names
 * are left out.
 * @param {string} source
 * @returns {{ names: string[], from: string, line: number }[]}
 */
export function valueImports(source) {
  const out = [];
  for (const m of source.matchAll(/^import\s+(type\s+)?([^;]*?)\s+from\s+'([^']+)';?/gms)) {
    if (m[1]) continue;
    const clause = m[2];
    const names = [];
    const braces = /\{([^}]*)\}/s.exec(clause);
    if (braces) {
      for (const part of braces[1].split(',')) {
        const p = part.trim();
        if (p === '' || p.startsWith('type ')) continue;
        names.push(p.split(/\s+as\s+/)[0].trim());
      }
    }
    const head = clause.replace(/\{[^}]*\}/s, '').replace(/,/g, ' ').trim();
    if (head !== '' && !head.startsWith('*')) names.push(head.split(/\s+/)[0]);
    const line = source.slice(0, m.index).split('\n').length;
    out.push({ names, from: m[3], line });
  }
  return out;
}

/**
 * Why one import statement of a module acts, or null: the banned name it imports or the dialog's module.
 * @param {{ names: string[], from: string }} imp
 * @returns {string | null}
 */
function actOf(imp) {
  const name = imp.names.find((n) => BANNED_NAME.test(n));
  if (name) return name;
  if (imp.from.startsWith('.') && DIALOG_MODULE.test(imp.from)) return `a dialog (${imp.from})`;
  return null;
}

/**
 * Resolves a relative module specifier of a file to its path relative to web/, without the extension.
 * @param {string} file the importing file, relative to web/
 * @param {string} spec
 */
export function resolveModule(file, spec) {
  const parts = file.split('/').slice(0, -1);
  for (const seg of spec.split('/')) {
    if (seg === '.' || seg === '') continue;
    if (seg === '..') parts.pop();
    else parts.push(seg);
  }
  return parts.join('/');
}

/**
 * The findings of one file: each import that brings a session, the API client, a dialog or an act into a view.
 * @param {string} file the file, relative to web/ (`src/app/views/packages/used-by.ts`)
 * @param {string} source its text
 * @param {(module: string) => string | null} read the text of a module by its path relative to web/ without `.ts`, or null
 * @returns {{ line: number, problem: string }[]}
 */
export function viewFindings(file, source, read) {
  if (!file.startsWith(VIEWS) || !file.endsWith('.ts') || file.endsWith('.spec.ts')) return [];
  const findings = [];
  for (const imp of valueImports(source)) {
    const act = actOf(imp);
    if (act) {
      findings.push({ line: imp.line, problem: `a view imports ${act} from '${imp.from}'; read through a …Reads class, act in a place of acts.ts` });
      continue;
    }
    if (!imp.from.startsWith('.')) continue;
    const module = resolveModule(file, imp.from);
    if (!LOOKED_INTO.test(module)) continue;
    const text = read(module);
    if (text === null) continue;
    const inner = valueImports(text).map(actOf).find((a) => a !== null);
    if (inner) {
      findings.push({ line: imp.line, problem: `a view imports '${imp.from}', which acts through ${inner}; move the act into a place of acts.ts` });
    }
  }
  return findings;
}
