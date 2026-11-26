// Repository rules that are plain text searches over web/. They run as part of `npm run lint`.
//  - R-ARCH-004: no NgModule(, provideZoneChangeDetection or zone.js in src or angular.json.
//  - R-UI-001: no bypassSecurityTrust or addSvgIcon in src.
//  - R-ARCH-007: no string literal that starts with /files/ or /api/<format>/ in src, except the
//    files client in src/app/api/ and generated code.
//  - R-UI-050: every manual page that src/app/core/help-topics.ts names is a file of docs/manual/.
//  - Q-UI-335: every number field of a template names its width (field-width-rule.mjs).
//  - R-UI-126, R-UI-158: no link to the removed routes /setup, /browse and /admin/security (removed-routes-rule.mjs).
//  - Q-UI-552: no bare <details> in a template: every fold is <quader-disclosure>, with the app's one chevron.
//  - Q-UI-560: no icon stands directly in a table cell: it is one element with its word (status-rule.mjs).
//  - R-UI-163: no row shows two word actions at once: several actions are the row's menu (row-actions-rule.mjs).
//  - Q-UI-1006: no template interpolates a principal's reference (`…By`, `by`, `lastDownloader`) as it is; it goes through personName.
//  - Q-UI-564: the word of a badge or a status starts with a capital (**Disabled**, not "disabled"), as the manual writes it.
//  - R-UI-217: a loading sentence is <quader-loading>, a list that loads says what (loadingText), a button held while its act runs
//    carries [quaderBusy], and no stylesheet spins something of its own (busy-rule.mjs).
//  - Q-UI-1211: no stylesheet names a monospace face of its own: the face is the token --quader-font-mono's (mono-rule.mjs).
//  - R-UI-175: no `vertical-align: middle` and no `align-items: last baseline` in a style: a box beside a text stands on the
//    line's middle by the line rule of _controls.scss (line-rule.mjs).
//  - Q-UI-1046: every text a journey names by its message ID (`ui('…')` of e2e/support.ts) is a plain unit of the translation files,
//    and the code's words for it are messages.xlf's (journey-texts-rule.mjs).
//  - Q-UI-1074: a tab strip (class tab-nav) carries quaderTabStrip and its component imports TabStrip, which scrolls it
//    (tab-strip-rule.mjs).
//  - R-UI-171: a separator drawn by CSS (a middle dot, a slash, a chevron) only by the app's separator rules, which the render check
//    separator-start measures (separator-rule.mjs).
//  - R-UI-214: no template interpolates a server's English sentence (`{{ x.detail }}`, `{{ x.basis }}`, …): the screen says a
//    code in its own words, a raw text goes through <quader-technical-details> (server-text-rule.mjs).
//  - R-UI-216: a failed read's notice stays in place while the read is tried again: a resource's failure is failureOf, never made
//    by hand, and a read of its own clears its notice's failure after the answer, not before (read-failure-rule.mjs).
//  - R-UI-223: a file below src/app/views/ imports no SessionStore, no ApiClient, no QuaderDialog, no API service class and no act: the
//    views are what a second application composes without a session (views-rule.mjs).
//  - a test's name carries a requirement ID, never a decision's number: no `describe`, `it` or `test` title of a spec, a journey or
//    a script's test names a `Q-…` or an `ADR-…` (test-names-rule.mjs).
import { existsSync, readdirSync, readFileSync, statSync } from 'node:fs';
import { join, relative, sep } from 'node:path';
import { fileURLToPath } from 'node:url';
import { numberFieldsWithoutWidth } from './field-width-rule.mjs';
import { linksToRemovedRoutes } from './removed-routes-rule.mjs';
import { bareIconsInCells } from './status-rule.mjs';
import { wordActionsSideBySide } from './row-actions-rule.mjs';
import { offTheLine } from './line-rule.mjs';
import { separatorsOutsideTheRules } from './separator-rule.mjs';
import { ownMonospace } from './mono-rule.mjs';
import { journeyTextFindings } from './journey-texts-rule.mjs';
import { decisionNumbersInTestNames } from './test-names-rule.mjs';
import { tabStripFindings } from './tab-strip-rule.mjs';
import { serverTextFindings } from './server-text-rule.mjs';
import { clearedBeforeAnswer, handMadeFailures } from './read-failure-rule.mjs';
import { busyFindings } from './busy-rule.mjs';
import { viewFindings } from './views-rule.mjs';

const root = join(fileURLToPath(new URL('.', import.meta.url)), '..');

function* walk(dir) {
  for (const name of readdirSync(dir)) {
    const path = join(dir, name);
    if (statSync(path).isDirectory()) yield* walk(path);
    else yield path;
  }
}

const sources = [...walk(join(root, 'src'))].filter((p) => /\.(ts|html|scss|css)$/.test(p));
const rules = [
  { id: 'R-ARCH-004', re: /NgModule\(|provideZoneChangeDetection|zone\.js/, files: [...sources, join(root, 'angular.json')] },
  { id: 'R-UI-001', re: /bypassSecurityTrust|addSvgIcon/, files: sources },
  {
    id: 'Q-UI-552',
    re: /<details[\s>]/,
    files: sources.filter((p) => /\.(ts|html)$/.test(p) && !p.endsWith('.spec.ts')),
  },
  {
    id: 'Q-UI-564',
    re: /class="(badge|ok|warn|bad)( [^"]*)?"[^>]*>(<ng-container[^>]*>)?[a-z]/,
    files: sources.filter((p) => /\.(ts|html)$/.test(p) && !p.endsWith('.spec.ts')),
  },
  {
    // Q-UI-1006: a principal's reference (`releasedBy`, `grantedBy`, `lastDownloader`, a stamp's `by`) goes through `personName`
    // (`person(…)`, `principal(…)`): a template that interpolates it as it is prints `user:priya` to a person.
    id: 'Q-UI-1006',
    re: /\{\{\s*[\w.?!]*\.(releasedBy|createdBy|updatedBy|grantedBy|deletedBy|liftedBy|modifiedBy|blockedBy|withdrawnBy|lastDownloader|by)\s*(\?\?[^}]*)?\}\}/,
    files: sources.filter((p) => /\.(ts|html)$/.test(p) && !p.endsWith('.spec.ts')),
  },
  {
    id: 'R-ARCH-007',
    re: /['"`]\/(files\/|api\/(?!v[0-9]+\/)[a-z][a-z0-9-]*\/)/,
    files: sources.filter((p) => {
      const rel = relative(root, p).split(sep).join('/');
      return !rel.startsWith('src/app/api/');
    }),
  },
];

let failed = false;
for (const rule of rules) {
  for (const file of rule.files) {
    readFileSync(file, 'utf8')
      .split('\n')
      .forEach((line, i) => {
        if (rule.re.test(line)) {
          failed = true;
          console.error(`${rule.id}: ${relative(root, file)}:${i + 1}: ${line.trim()}`);
        }
      });
  }
}
// The "?" of a screen opens its manual page; a page that does not exist would open a 404.
const topics = readFileSync(join(root, 'src/app/core/help-topics.ts'), 'utf8');
for (const [, section, page] of topics.matchAll(/section: '([a-z]+)', page: '([a-z0-9-]+)'/g)) {
  if (!existsSync(join(root, '..', 'docs', 'manual', section, `${page}.md`))) {
    failed = true;
    console.error(`R-UI-050: src/app/core/help-topics.ts names docs/manual/${section}/${page}.md, which does not exist`);
  }
}

// A number field is as wide as its number, never the column (Q-UI-335).
for (const file of sources.filter((p) => /\.(ts|html)$/.test(p) && !p.endsWith('.spec.ts'))) {
  for (const { line, tag } of numberFieldsWithoutWidth(readFileSync(file, 'utf8'))) {
    failed = true;
    console.error(`Q-UI-335: ${relative(root, file)}:${line}: a number field without the width number, duration or bytes: ${tag.split('\n')[0].trim()}`);
  }
}

// Set up client and the files are pages of their repository; the old routes answer "Page not found" (R-UI-126, ADR-203).
for (const file of sources.filter((p) => /\.(ts|html)$/.test(p) && !p.endsWith('.spec.ts'))) {
  for (const { line, text } of linksToRemovedRoutes(readFileSync(file, 'utf8'))) {
    failed = true;
    console.error(`R-UI-126: ${relative(root, file)}:${line}: a link to a removed route (/setup, /browse, /admin/security): ${text}`);
  }
}

// A status in a cell is one element, so its icon never stands on another line than its word (Q-UI-560).
for (const file of sources.filter((p) => /\.(ts|html)$/.test(p) && !p.endsWith('.spec.ts'))) {
  for (const { line, tag } of bareIconsInCells(readFileSync(file, 'utf8'))) {
    failed = true;
    console.error(`Q-UI-560: ${relative(root, file)}:${line}: an icon directly in a table cell; put it and its word into the badge or .icon-name: ${tag.split('\n')[0].trim()}`);
  }
}

// A row with one action names it, a row with several has a menu (R-UI-163). The token rows keep Rotate and Revoke as words: a
// credential's Revoke is never in a menu (Q-UI-641); so do the rows of a service account's technical tokens (R-UI-181).
const twoWordsAllowed = new Set(['src/app/shared/access/token-table.ts', 'src/app/features/service-accounts/technical-token-table.ts']);
for (const file of sources.filter((p) => /\.(ts|html)$/.test(p) && !p.endsWith('.spec.ts'))) {
  const rel = relative(root, file).split(sep).join('/');
  if (twoWordsAllowed.has(rel)) continue;
  for (const { line, tag } of wordActionsSideBySide(readFileSync(file, 'utf8'))) {
    failed = true;
    console.error(`R-UI-163: ${rel}:${line}: two word actions in one row; put the row's actions into its menu: ${tag.split('\n')[0].trim()}`);
  }
}

// A control or an icon beside a text stands on the middle of its line, not on the lower-case letters' (R-UI-175, Q-UI-725).
for (const file of sources.filter((p) => /\.(ts|scss|css)$/.test(p) && !p.endsWith('.spec.ts'))) {
  for (const { line, text } of offTheLine(readFileSync(file, 'utf8'))) {
    failed = true;
    console.error(`R-UI-175: ${relative(root, file)}:${line}: a box placed off the line of its text; use the line rule of _controls.scss (vertical-align: top and margins from --quader-line): ${text}`);
  }
}

// A separator drawn by CSS is one the render check separator-start knows, so none starts a line unseen (R-UI-171).
for (const file of sources.filter((p) => /\.(ts|scss|css)$/.test(p) && !p.endsWith('.spec.ts'))) {
  for (const { line, text } of separatorsOutsideTheRules(readFileSync(file, 'utf8'))) {
    failed = true;
    console.error(`R-UI-171: ${relative(root, file)}:${line}: a separator drawn by CSS outside the separator rules; use .dot-before, .dot-separated or a breadcrumb's li + li::before: ${text}`);
  }
}

// Every monospace text takes the token's face, never a stack of its own (Q-UI-1211).
for (const file of sources.filter((p) => /\.(ts|scss|css)$/.test(p) && !p.endsWith('.spec.ts'))) {
  for (const { line, text } of ownMonospace(readFileSync(file, 'utf8'))) {
    failed = true;
    console.error(`Q-UI-1211: ${relative(root, file)}:${line}: a monospace face of its own; use var(--quader-font-mono), which main.scss gives code, kbd, pre and samp: ${text}`);
  }
}

// Every tab strip scrolls the same way where its tabs do not fit: its component imports the directive that does it (Q-UI-1074).
for (const file of sources.filter((p) => p.endsWith('.ts') && !p.endsWith('.spec.ts'))) {
  const source = readFileSync(file, 'utf8');
  const url = /templateUrl:\s*'([^']+)'/.exec(source);
  const template = url ? readFileSync(join(file, '..', url[1]), 'utf8') : '';
  const { unmarked, unimported } = tabStripFindings(source, template);
  for (const tag of unmarked) {
    failed = true;
    console.error(`Q-UI-1074: ${relative(root, file)}: a tab strip (class tab-nav) without quaderTabStrip: ${tag}`);
  }
  if (unimported) {
    failed = true;
    console.error(`Q-UI-1074: ${relative(root, file)}: a tab strip (class tab-nav) in a component that does not import TabStrip (src/app/shared/ui/tab-strip.ts)`);
  }
}

// A screen says a server's code in its own words and folds a raw text under Technical detail; it never binds the server's English
// sentence as its own (R-UI-214).
for (const file of sources.filter((p) => /\.(ts|html)$/.test(p) && !p.endsWith('.spec.ts'))) {
  const rel = relative(root, file).split(sep).join('/');
  for (const { line, expression } of serverTextFindings(rel, readFileSync(file, 'utf8'))) {
    failed = true;
    console.error(
      `R-UI-214: ${rel}:${line}: {{ ${expression} }} shows a server's English sentence; say its code with $localize, or pass a raw text to <quader-technical-details [text]>`,
    );
  }
}

// A failed read's notice stays in place while the read is tried again (R-UI-216, Q-UI-1195): the failure is cleared by the answer.
for (const file of sources.filter((p) => /\.(ts|html)$/.test(p) && !p.endsWith('.spec.ts'))) {
  const rel = relative(root, file).split(sep).join('/');
  if (rel === 'src/app/shared/paged-resource.ts') continue;
  const text = readFileSync(file, 'utf8');
  const ts = file.endsWith('.html') ? (existsSync(file.replace(/\.html$/, '.ts')) ? readFileSync(file.replace(/\.html$/, '.ts'), 'utf8') : '') : text;
  for (const { line, problem } of [...handMadeFailures(text), ...clearedBeforeAnswer(text, ts)]) {
    failed = true;
    console.error(`R-UI-216: ${rel}:${line}: ${problem}`);
  }
}

// Every load shows the spinner of the mark: a loading sentence is <quader-loading>, a list says what it loads, a button held while its
// act runs carries [quaderBusy], and nothing spins in a way of its own (R-UI-217, Q-UI-1239).
for (const file of sources.filter((p) => /\.(ts|html|scss|css)$/.test(p) && !p.endsWith('.spec.ts'))) {
  const rel = relative(root, file).split(sep).join('/');
  if (rel === 'src/styles/_spinner.scss') continue;
  for (const { line, tag, problem } of busyFindings(rel, readFileSync(file, 'utf8'))) {
    failed = true;
    console.error(`R-UI-217: ${rel}:${line}: ${problem}: ${tag}`);
  }
}

// The views read through the abstract reads and act nowhere: a second application composes them without a session (R-UI-223).
for (const file of sources.filter((p) => p.endsWith('.ts'))) {
  const rel = relative(root, file).split(sep).join('/');
  const read = (module) => (existsSync(join(root, `${module}.ts`)) ? readFileSync(join(root, `${module}.ts`), 'utf8') : null);
  for (const { line, problem } of viewFindings(rel, readFileSync(file, 'utf8'), read)) {
    failed = true;
    console.error(`R-UI-223: ${rel}:${line}: ${problem}`);
  }
}

// A journey takes a page's text from the source the page takes it from, by its ID (Q-UI-1046).
{
  const en = readFileSync(join(root, 'src/locale/messages.xlf'), 'utf8');
  const de = readFileSync(join(root, 'src/locale/messages.de.xlf'), 'utf8');
  const code = sources.filter((p) => /\.(ts|html)$/.test(p) && !p.endsWith('.spec.ts')).map((p) => readFileSync(p, 'utf8'));
  for (const file of [...walk(join(root, 'e2e'))].filter((p) => p.endsWith('.ts'))) {
    for (const { line, id, problem } of journeyTextFindings(readFileSync(file, 'utf8'), en, de, code)) {
      failed = true;
      console.error(`Q-UI-1046: ${relative(root, file)}:${line}: ui('${id}'): ${problem}`);
    }
  }
}

// A test's name carries the requirement it holds, never a decision's number: the evidence record reads the IDs from the names.
{
  const tests = [
    ...[...walk(join(root, 'src'))].filter((p) => p.endsWith('.spec.ts')),
    ...[...walk(join(root, 'e2e'))].filter((p) => p.endsWith('.ts')),
    ...readdirSync(join(root, 'scripts')).filter((n) => n.endsWith('.test.mjs')).map((n) => join(root, 'scripts', n)),
  ];
  for (const file of tests) {
    for (const { line, title } of decisionNumbersInTestNames(readFileSync(file, 'utf8'))) {
      failed = true;
      console.error(`test names: ${relative(root, file)}:${line}: a test name with a decision's number; name the requirement it holds: ${title}`);
    }
  }
}

if (failed) process.exit(1);
console.log('check-rules: ok');
