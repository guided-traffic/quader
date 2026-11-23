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
//  - Q-UI-564: the word of a badge or a status starts with a capital (**Disabled**, not "disabled"), as the manual writes it.
//  - R-UI-175: no `vertical-align: middle` and no `align-items: last baseline` in a style: a box beside a text stands on the
//    line's middle by the line rule of _controls.scss (line-rule.mjs).
import { existsSync, readdirSync, readFileSync, statSync } from 'node:fs';
import { join, relative, sep } from 'node:path';
import { fileURLToPath } from 'node:url';
import { numberFieldsWithoutWidth } from './field-width-rule.mjs';
import { linksToRemovedRoutes } from './removed-routes-rule.mjs';
import { bareIconsInCells } from './status-rule.mjs';
import { wordActionsSideBySide } from './row-actions-rule.mjs';
import { offTheLine } from './line-rule.mjs';

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

if (failed) process.exit(1);
console.log('check-rules: ok');
