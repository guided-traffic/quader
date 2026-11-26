// The four lint rules of R-UI-046, run against the repository's own ESLint configuration: each rule fires on a snippet that breaks
// it, and the storage wrapper may use browser storage. `npm run test:lint` writes a JUnit report that the coverage generator reads.
import assert from 'node:assert/strict';
import { test } from 'node:test';
import { fileURLToPath } from 'node:url';
import { ESLint } from 'eslint';
import { numberFieldsWithoutWidth } from './field-width-rule.mjs';
import { linksToRemovedRoutes } from './removed-routes-rule.mjs';
import { bareIconsInCells } from './status-rule.mjs';
import { wordActionsSideBySide } from './row-actions-rule.mjs';
import { offTheLine } from './line-rule.mjs';
import { separatorsOutsideTheRules } from './separator-rule.mjs';
import { ownMonospace } from './mono-rule.mjs';
import { LONG_WORD, compare, longWords, rewrite } from './i18n-check.mjs';
import { journeyTextFindings } from './journey-texts-rule.mjs';
import { decisionNumbersInTestNames } from './test-names-rule.mjs';
import { tabStripFindings } from './tab-strip-rule.mjs';
import { serverTextBindings, serverTextFindings } from './server-text-rule.mjs';
import { clearedBeforeAnswer, handMadeFailures } from './read-failure-rule.mjs';
import { busyFindings } from './busy-rule.mjs';
import { viewFindings } from './views-rule.mjs';
import { bastionBundleFindings } from './bastion-bundle-rule.mjs';
import { TIME_FORMATTER, directTimeFormatting } from './time-format-rule.mjs';

const root = fileURLToPath(new URL('..', import.meta.url));
const eslint = new ESLint({ cwd: root });

async function messages(code, filePath) {
  const [result] = await eslint.lintText(code, { filePath: `${root}${filePath}` });
  return result.messages.filter((m) => m.severity === 2).map((m) => m.message);
}

const component = 'src/app/features/probe/probe.ts';

test('R-UI-046 a subscribe( call in a component fails the lint', async () => {
  const got = await messages('export function f(source: { subscribe(fn: () => void): void }): void {\n  source.subscribe(() => undefined);\n}\n', component);
  assert.ok(got.some((m) => m.includes('R-UI-046: no subscribe()')), `messages: ${got.join(' | ')}`);
});

test('R-UI-046 importing NgZone fails the lint', async () => {
  const got = await messages("import { NgZone } from '@angular/core';\nexport const zone: NgZone | null = null;\n", component);
  assert.ok(got.some((m) => m.includes('R-UI-046: the app is zoneless')), `messages: ${got.join(' | ')}`);
});

test('R-UI-046 a bypassSecurityTrust call fails the lint', async () => {
  const got = await messages('export function f(s: { bypassSecurityTrustHtml(v: string): unknown }): unknown {\n  return s.bypassSecurityTrustHtml("x");\n}\n', component);
  assert.ok(got.some((m) => m.includes('bypassSecurityTrust* is banned')), `messages: ${got.join(' | ')}`);
});

test('R-UI-046 direct localStorage access outside the storage wrapper fails the lint, inside the wrapper it passes', async () => {
  const code = "export const v = localStorage.getItem('a');\n";
  const outside = await messages(code, component);
  assert.ok(outside.some((m) => m.includes('browser storage only through the storage wrapper')), `messages: ${outside.join(' | ')}`);
  const inside = await messages(code, 'src/app/core/storage.ts');
  assert.ok(!inside.some((m) => m.includes('storage wrapper')), `the wrapper itself is linted: ${inside.join(' | ')}`);
});

test('R-UI-046 a component that follows the rules passes them', async () => {
  const got = await messages("export const answer = 42;\n", component);
  assert.ok(!got.some((m) => m.includes('R-UI-046')), `messages: ${got.join(' | ')}`);
});

test('a number field without a number width fails the check, one with its width passes', () => {
  const bad = [
    '<quader-text-field label="Priority" name="priority" type="number" [(value)]="p" />',
    '<quader-text-field width="full" label="Days" type="number" />',
    '<input class="text-input" type="number" [value]="n()" />',
    '<input\n  class="text-input field-w-name"\n  inputmode="numeric"\n/>',
    '<input class="text-input" pattern="[0-9]*" />',
  ];
  for (const t of bad) assert.equal(numberFieldsWithoutWidth(t).length, 1, t);
  const good = [
    '<quader-text-field width="number" label="Priority" type="number" />',
    '<quader-text-field\n  width="duration"\n  label="Lifetime (seconds)"\n  type="number"\n/>',
    '<input class="text-input field-w-bytes" inputmode="numeric" />',
    `<input class="text-input" [class]="'field-w-' + n.width" inputmode="numeric" />`,
    '<quader-text-field width="name" label="Name" />',
    '<input class="text-input field-w-name" />',
  ];
  for (const t of good) assert.deepEqual(numberFieldsWithoutWidth(t), [], t);
  assert.equal(numberFieldsWithoutWidth('<p>x</p>\n<input type="number" />')[0].line, 2);
});

test('R-UI-126 a link to the removed routes /setup and /browse fails the check, a repository\'s own setup and files pass', () => {
  const bad = [
    '<a class="btn btn-default" routerLink="/setup" [queryParams]="{ repository: ref() }">Set up client</a>',
    "void this.router.navigate(['/setup'], { queryParams: { repository } });",
    'const url = `/setup?repository=${ref}`;',
    "<a routerLink=\"/browse\">Browse</a>",
    "return ['/browse', project, key];",
    '<a class="btn btn-default btn-sm" routerLink="/admin/system/storage">Storage</a>',
  ];
  for (const t of bad) assert.equal(linksToRemovedRoutes(t).length, 1, t);
  const good = [
    '<a class="btn btn-default" [routerLink]="setupLink()">Set up client</a>',
    "return [...repositoryCommands(ref), 'setup'];",
    "path: 'setup',",
    "loadComponent: () => import('../setup/client-setup').then((m) => m.ClientSetupPage),",
    "const path = setup ? '/project/setup' : raw;",
    "['/projects', project, 'repositories', key, 'files']",
    '<a class="btn btn-default btn-sm" routerLink="/admin/settings/storage">Storage</a>',
    "<a routerLink=\"/admin/system/backups\">Backups</a>",
  ];
  for (const t of good) assert.deepEqual(linksToRemovedRoutes(t), [], t);
  assert.equal(linksToRemovedRoutes('x\n<a routerLink="/setup">')[0].line, 2);
});

test('R-UI-158 a link to the removed route /admin/security fails the check, the dashboard at /security and the advisory sources pass', () => {
  const bad = [
    '<a routerLink="/admin/security">Security</a>',
    "return ctx.instanceWide ? ['/admin/security', ...rest] : [];",
    "return router.createUrlTree(['/admin', 'security']);",
    "const url = `/admin/security?project=${key}`;",
  ];
  for (const t of bad) assert.equal(linksToRemovedRoutes(t).length, 1, t);
  const good = [
    "return ctx.instanceWide ? ['/security', ...rest] : [];",
    '<a routerLink="/admin/advisory-sources">Advisory sources</a>',
    "route: '/admin/settings/auth',",
    "['/admin', 'upstream', key]",
    "path: 'security',",
  ];
  for (const t of good) assert.deepEqual(linksToRemovedRoutes(t), [], t);
});

test('a link to the removed routes /admin/projects and /admin/repositories fails the check, /projects and /repositories pass', () => {
  const bad = [
    '<a routerLink="/admin/repositories">View repositories</a>',
    '<a routerLink="/admin/repositories/new">New repository</a>',
    "return router.createUrlTree(['/admin', 'projects']);",
    "return router.createUrlTree(['/admin', 'repositories']);",
    "const url = `/admin/projects?q=${q}`;",
    "['/admin/repositories/new', format]",
  ];
  for (const t of bad) assert.equal(linksToRemovedRoutes(t).length, 1, t);
  const good = [
    '<a routerLink="/repositories">View repositories</a>',
    "['/projects', project, 'repositories', 'new']",
    "route: '/repositories',",
    "path: 'repositories',",
    "['/admin', 'upstream', key]",
  ];
  for (const t of good) assert.deepEqual(linksToRemovedRoutes(t), [], t);
});

test('R-UI-145 an icon directly in a table cell fails the check, an icon inside its badge or its .icon-name passes', () => {
  const bad = [
    '<td>\n  @if (ok) {\n    <span class="material-icons inline ok" aria-hidden="true">check_circle</span>\n  }\n  {{ word }}\n</td>',
    '<td><span class="material-icons bad" aria-hidden="true">error</span><span>lost</span></td>',
    '<th scope="row"><span class="material-icons" aria-hidden="true">schedule</span> Waiting</th>',
  ];
  for (const t of bad) assert.equal(bareIconsInCells(t).length, 1, t);
  const good = [
    '<td><span class="ok"><span class="material-icons" aria-hidden="true">check_circle</span>Succeeded</span></td>',
    '<td><span class="badge"><span class="material-icons" aria-hidden="true">cancel</span>Cancelled</span></td>',
    '<td><span class="icon-name"><span class="material-icons inline ok" aria-hidden="true">check_circle</span><span class="icon-name-text">{{ reason }}</span></span></td>',
    '<td><span class="material-icons warn" role="img" aria-label="Not served">warning</span></td>',
    '<p><span class="material-icons ok" aria-hidden="true">check_circle</span> Ready</p>',
    '<td>\n  <quader-format-icon [format]="f" />\n  <br />\n  <span class="class-name"><span class="material-icons" aria-hidden="true">cloud</span>Remote</span>\n</td>',
  ];
  for (const t of good) assert.deepEqual(bareIconsInCells(t), [], t);
  assert.equal(bareIconsInCells('<table>\n<tr>\n<td><span class="material-icons">x</span></td>')[0].line, 3);
});

test('R-UI-163 two word actions in one row fail the check, one word, a menu with the copy beside it and words of other branches pass', () => {
  const bad = [
    '<td class="actions">\n  <a class="btn btn-tertiary" routerLink="x">Edit</a>\n  <a class="btn btn-tertiary" routerLink="y">Test</a>\n</td>',
    '<td>\n  @switch (k.state) {\n    @case (\'next\') {\n      <button class="btn btn-default btn-sm">Promote</button>\n      <button class="btn btn-tertiary btn-sm">Remove</button>\n    }\n  }\n</td>',
    '<li><a href="v">View</a><button class="btn btn-tertiary">Rename</button>@if (d) {<button class="btn btn-tertiary">Delete</button>}</li>',
  ];
  for (const t of bad) assert.equal(wordActionsSideBySide(t).length, 1, t);
  const good = [
    '<td class="actions"><button class="btn btn-tertiary" type="button">Revoke<span class="visually-hidden"> ci</span></button></td>',
    '<td class="actions"><quader-copy-button [value]="a" /><button class="btn btn-tertiary btn-icon" [cdkMenuTriggerFor]="m"><span class="material-icons">more_vert</span></button>'
      + '<ng-template #m><div class="menu" cdkMenu><button class="menu-item" cdkMenuItem>Edit</button><button class="menu-item" cdkMenuItem>Delete</button></div></ng-template></td>',
    '<td class="actions with-setup"><a class="btn btn-tertiary row-setup">Set up client</a><button class="btn btn-tertiary btn-icon" aria-label="Actions for x">…</button></td>',
    '<td>@if (a) {<button class="btn btn-tertiary">Reinstate</button>} @else {<button class="btn btn-tertiary">Clear</button>}</td>',
    '<div class="toolbar"><a class="btn btn-confirm">New mapping</a><a class="btn btn-default">Test with claims</a></div>',
    // A state's cell with its switch, its one word and a link under it (Q-CUR-201): the switch is no word.
    '<td class="cache-cell"><span class="state-line"><quader-switch [checked]="on" [label]="l" /><span class="muted">Not stored</span>'
      + '<button class="btn btn-tertiary on-line" type="button">Download</button></span><div class="verdict-line"><a routerLink="v">Open the version</a></div></td>',
  ];
  for (const t of good) assert.deepEqual(wordActionsSideBySide(t), [], t);
  assert.equal(wordActionsSideBySide('<table>\n<tr><td>\n<a class="btn">A</a>\n<a class="btn">B</a></td>')[0].line, 4);
});

function xliff(lang, units) {
  const head = `<?xml version="1.0" encoding="UTF-8" ?>\n<xliff version="2.0" xmlns="urn:oasis:names:tc:xliff:document:2.0" srcLang="en"${lang ? ` trgLang="${lang}"` : ''}>\n  <file id="ngi18n" original="ng.template">\n    `;
  const body = units
    .map(
      ([id, where, source, target]) =>
        `<unit id="${id}">\n      <notes>\n        <note category="location">${where}</note>\n      </notes>\n      <segment>\n        <source>${source}</source>\n` +
        (target === undefined ? '' : `        <target>${target}</target>\n`) +
        '      </segment>\n    </unit>',
    )
    .join('\n    ');
  return `${head}${body}\n  </file>\n</xliff>\n`;
}

test('R-UI-047 the German file is held to the extraction: a missing, a removed and a moved unit and a changed source are named', () => {
  const extracted = xliff('', [
    ['a', 'src/a.ts:10', 'Save'],
    ['b', 'src/b.html:4,5', 'Delete'],
    ['c', 'src/c.ts:7', 'New text'],
    ['d', 'src/d.ts:1', 'Open'],
  ]);
  const german = xliff('de', [
    ['b', 'src/b.html:2,3', 'Delete', 'Löschen'],
    ['a', 'src/a.ts:10', 'Save', 'Speichern'],
    ['d', 'src/d.ts:1', 'Opened', 'Geöffnet'],
    ['gone', 'src/gone.ts:3', 'Old', 'Alt'],
  ]);
  assert.deepEqual(compare(extracted, german), { missing: ['c'], stale: ['gone'], notes: ['b'], source: ['d'] });
  const de = xliff('de', [
    ['a', 'src/a.ts:10', 'Save', 'Speichern'],
    ['b', 'src/b.html:4,5', 'Delete', 'Löschen'],
    ['c', 'src/c.ts:7', 'New text', 'Neuer Text'],
    ['d', 'src/d.ts:1', 'Open', 'Öffnen'],
  ]);
  assert.deepEqual(compare(extracted, de), { missing: [], stale: [], notes: [], source: [] });
});

test('R-UI-171 a German word longer than the narrowest heading\'s line carries a soft hyphen at a joint of its compound', () => {
  assert.equal(LONG_WORD, 20);
  const german = xliff('de', [
    ['a', 'src/a.ts:1', 'Authentication settings', 'Authentifizierungseinstellungen'],
    ['b', 'src/b.ts:1', 'Authentication settings', 'Authentifizierungs&#xAD;einstellungen'],
    ['c', 'src/c.ts:1', 'Group sync', 'Gruppen-Synchronisierungseinstellung'],
    ['d', 'src/d.ts:1', 'Policies of {$PH}', 'Richtlinien von <ph id="0" equiv="PH" disp="{{ averyveryverylongidentifiername }}"/>'],
    ['e', 'src/e.ts:1', 'Unit', 'Organisationseinheit'],
  ]);
  assert.deepEqual(longWords(german), [
    { id: 'a', word: 'Authentifizierungseinstellungen' },
    { id: 'c', word: 'Synchronisierungseinstellung' },
  ]);
});

test('R-UI-047 the rewrite takes the extraction\'s order and notes and keeps every target and a changed source', () => {
  const extracted = xliff('', [
    ['a', 'src/a.ts:10', 'Save'],
    ['b', 'src/b.html:4,5', 'Delete'],
    ['c', 'src/c.ts:7', 'New text'],
    ['d', 'src/d.ts:1', 'Open'],
  ]);
  const german = xliff('de', [
    ['b', 'src/b.html:2,3', 'Delete', 'Löschen'],
    ['a', 'src/a.ts:10', 'Save', 'Speichern'],
    ['d', 'src/d.ts:1', 'Opened', 'Geöffnet'],
    ['gone', 'src/gone.ts:3', 'Old', 'Alt'],
  ]);
  const out = rewrite(extracted, german);
  assert.equal(
    out,
    xliff('de', [
      ['a', 'src/a.ts:10', 'Save', 'Speichern'],
      ['b', 'src/b.html:4,5', 'Delete', 'Löschen'],
      ['d', 'src/d.ts:1', 'Opened', 'Geöffnet'],
    ]),
  );
  assert.deepEqual(compare(extracted, out), { missing: ['c'], stale: [], notes: [], source: ['d'] });
  assert.equal(rewrite(extracted, out), out, 'a second rewrite changes nothing');
});

test('R-UI-175 vertical-align: middle and align-items: last baseline fail the check, the line rule and a table cell\'s alignment pass', () => {
  const bad = [
    '.material-icons.inline {\n  font-size: 1.125rem;\n  vertical-align: middle;\n}',
    ':host {\n  display: inline-block;\n  vertical-align: middle;\n}',
    '.external-link {\n  display: inline-flex;\n  align-items: last baseline;\n}',
    '.table td .chip {\n  vertical-align: middle;\n}',
  ];
  for (const css of bad) assert.equal(offTheLine(css).length, 1, css);
  const good = [
    '.material-icons.inline {\n  vertical-align: top;\n  margin-block: calc((var(--quader-line) - 1em) / 2);\n}',
    'table.data {\n  thead th {\n    vertical-align: middle;\n  }\n}',
    'td.num,\nth.num {\n  vertical-align: middle;\n}',
    '// at `vertical-align: middle` they stood 1.5 px under it.\n.x { vertical-align: top; }',
  ];
  for (const css of good) assert.deepEqual(offTheLine(css), [], css);
});

test("R-UI-171 a separator drawn by CSS outside the app's separator rules fails the check; a measured dot and a separator that ends its item pass", () => {
  const bad = [
    ".find-where > span::before {\n  content: '·';\n  margin-inline: 8px;\n}",
    '.trail a + a::before {\n  content: "/";\n}',
    ".crumbs {\n  span + span::before {\n    content: '›';\n  }\n}",
    ".meta::after {\n  content: '\\00b7';\n}",
    ".dot-before::before,\n.facts > *::before {\n  content: '·';\n}",
    "nav ol {\n  li + li::before {\n    content: '/';\n  }\n}",
  ];
  for (const css of bad) assert.equal(separatorsOutsideTheRules(css).length, 1, css);
  const good = [
    ".dot-before::before {\n  content: '·';\n  color: var(--quader-text-muted);\n}",
    ".dot-separated {\n  display: inline-flex;\n\n  > *::before {\n    content: '·';\n  }\n}",
    ".level-trail {\n  display: flex;\n\n  li:not(:last-child)::after {\n    content: '›';\n  }\n}",
    ".path {\n  .seg:not(:last-child)::after {\n    content: '/';\n  }\n}",
    ".sort::after {\n  content: 'arrow_upward';\n}",
    "// a line that starts with content: '·' in a comment\n.x { content: ''; }",
  ];
  for (const css of good) assert.deepEqual(separatorsOutsideTheRules(css), [], css);
});

test('R-UI-046 a monospace face is the token\'s: a stylesheet that names one of its own fails the lint', () => {
  const bad = [
    '.raw {\n  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;\n}',
    'input { font-family: monospace; }',
    '.x {\n  font: 400 0.8rem / 1.5 Menlo, Consolas, monospace;\n}',
  ];
  for (const css of bad) assert.equal(ownMonospace(css).length, 1, css);
  const good = [
    '.raw {\n  font-family: var(--quader-font-mono);\n}',
    ':root {\n  --quader-font-mono: ui-monospace, SFMono-Regular, Menlo, monospace;\n}',
    '// the browser default monospace stood here\n.x { font: var(--quader-type-label-large); }',
  ];
  for (const css of good) assert.deepEqual(ownMonospace(css), [], css);
});

test("a journey's text named by its ID must be a plain unit of the translation files and the code's words", () => {
  const en =
    '<unit id="nav.a">\n<segment>\n<source>Security dashboard</source>\n</segment>\n</unit>\n' +
    '<unit id="nav.b">\n<segment>\n<source>Malware</source>\n</segment>\n</unit>\n' +
    '<unit id="nav.c">\n<segment>\n<source>Found <ph id="0"/> items</source>\n</segment>\n</unit>\n' +
    '<unit id="nav.d">\n<segment>\n<source>Signatures &amp; keys</source>\n</segment>\n</unit>\n';
  const de = '<unit id="nav.a">\n<segment>\n<source>Security dashboard</source>\n<target>Sicherheits-Dashboard</target>\n</segment>\n</unit>\n';
  const code = ["label: $localize`:@@nav.a:Security dashboard`,", "label: $localize`:@@nav.b:Malware scan`,", '<h2 i18n="@@nav.d">Signatures &amp; keys</h2>'];
  const journey = [
    "await nav.getByRole('link', { name: ui('nav.a') }).click();",
    "await expect(x).toHaveAccessibleName(ui('nav.a', 'de'));",
    "await nav.getByRole('link', { name: ui('nav.b') }).click();",
    "await nav.getByRole('link', { name: ui('nav.c') }).click();",
    "await nav.getByRole('link', { name: ui('nav.gone') }).click();",
    "await nav.getByRole('link', { name: ui('nav.b', 'de') }).click();",
  ].join('\n');
  const found = journeyTextFindings(journey, en, de, code);
  assert.deepEqual(
    found.map((f) => [f.line, f.id]),
    [
      [3, 'nav.b'],
      [4, 'nav.c'],
      [5, 'nav.gone'],
      [6, 'nav.b'],
    ],
  );
  assert.match(found[0].problem, /the code says "Malware scan" and messages.xlf "Malware"/);
  assert.match(found[1].problem, /no plain source/);
  assert.match(found[3].problem, /no plain German target/);
});

test("a test name that carries a decision's number fails the check, one that carries a requirement ID passes", () => {
  const spec = [
    "describe('The list (Q-UI-420, R-UI-100)', () => {",
    "  it('R-UI-100 a chip removes only its filter', () => {});",
    "  it('ADR-203 the old route answers Page not found', () => {});",
    '  it(',
    "    'a title on its own line ' +",
    "      'that goes on with Q-AUTH-6',",
    '    () => {},',
    '  );',
    '  test.describe(`a template ${kind} with Q-CI-26`, () => {});',
    "  it.each([['a'], ['b']])('a row %s of Q-UI-700', () => {});",
    "  test.step('Q-UI-1046 a step of a journey', async () => {});",
    "  it('names the decision only in its comment', () => {}); // Q-UI-422",
    "  it('reads a word like Q-X or QUI-1 as no number', () => {});",
    "  test.beforeAll('Q-UI-1 a hook has no title to check', () => {});",
    "  expect(/Q-UI-1/.test('Q-UI-1')).toBe(true);",
    "  expect(/it\\('Q-UI-2/.test(source)).toBe(true);",
    `  const fixture = "it('Q-UI-3 the source of another test is no test of this file')";`,
    '});',
  ].join('\n');
  const found = decisionNumbersInTestNames(spec);
  assert.deepEqual(
    found.map((f) => f.line),
    [1, 3, 5, 9, 10, 11],
  );
  assert.equal(found[2].title, 'a title on its own line that goes on with Q-AUTH-6');
});

test('R-UI-173 a tab strip without quaderTabStrip or without the import of TabStrip fails the check; a marked, imported one passes', () => {
  const marked = '<nav quaderTabStrip class="tab-nav profile-nav" aria-label="Profile pages"><a class="tab-link">Profile</a></nav>';
  const bare = marked.replace('quaderTabStrip ', '');
  const ok = { unmarked: [], unimported: false };
  assert.deepEqual(tabStripFindings(`@Component({ imports: [RouterLink, TabStrip], template: \`${marked}\` })`), ok);
  assert.deepEqual(tabStripFindings("@Component({ imports: [TabStrip], templateUrl: './list.html' })", marked), ok);
  // The tablist of the tab group: the attributes on lines of their own.
  assert.deepEqual(tabStripFindings('@Component({ imports: [TabList, TabStrip], template: `<div\n  ngTabList\n  quaderTabStrip\n  class="tab-nav"\n>` })'), ok);
  assert.deepEqual(tabStripFindings(`@Component({ imports: [RouterLink, TabStrip], template: \`${bare}\` })`).unmarked, [bare.slice(0, bare.indexOf('>') + 1)]);
  assert.equal(tabStripFindings(`@Component({ imports: [RouterLink], template: \`${marked}\` })`).unimported, true);
  assert.equal(tabStripFindings("@Component({ imports: [RouterLink], templateUrl: './list.html' })", marked).unimported, true);
  assert.equal(tabStripFindings('@Component({ template: `<div quaderTabStrip class="view-tabs tab-nav"></div>` })').unimported, true);
  assert.deepEqual(tabStripFindings('@Component({ imports: [RouterLink], template: `<nav class="tab-navigation"></nav>` })'), ok);
  assert.deepEqual(tabStripFindings('/** The links of `<nav class="tab-nav">`. */ @Directive({ selector: \'[quaderTabStrip]\' })'), ok);
});

test('R-UI-214 a template that binds a server sentence fails the rule, technical details and a code in words pass', () => {
  const bad = [
    '<p class="hint">{{ basis }}</p>\n<p>{{ trend().basis }}</p>',
    '<span translate="no">{{ err.detail }}</span>',
    '<dd>{{ s.errors.lastMessage ?? \'–\' }}</dd>',
    '<p class="hint" translate="no">{{ p?.note }}</p><span>{{ w.detail }}</span>',
    ': {{ l.discoveryNote }}',
    '<span>{{ i.keys!.lastError }}</span>',
  ];
  assert.deepEqual(
    bad.flatMap((t) => serverTextBindings(t).map((b) => b.expression)),
    ['trend().basis', 'err.detail', 's.errors.lastMessage', 'w.detail', 'l.discoveryNote', 'i.keys.lastError'],
  );
  const good = [
    '<quader-technical-details [text]="err.detail" />',
    '<p class="hint">{{ noteText(code) }}</p>',
    '<span>{{ errorText(err.code) }}</span>',
    '<span>{{ detailOf(err) }}</span>',
    '<span>{{ r.description }}</span>',
  ];
  assert.deepEqual(good.flatMap((t) => serverTextBindings(t)), []);
});

test('R-UI-214 an allowed binding passes in its own file only', () => {
  const log = '<li><span class="level" translate="no">{{ m.level }}</span>&ngsp;<span lang="en" translate="no">{{ m.message }}</span></li>';
  assert.deepEqual(serverTextFindings('src/app/features/packages/ansible-import-task.ts', log), []);
  assert.deepEqual(serverTextFindings('src/app/features/packages/other.ts', log), [{ line: 1, expression: 'm.message' }]);
});

test("R-UI-216 a resource's failure made by hand and a read that clears its notice's failure before the answer fail the rule", () => {
  const made = [
    'readonly failure = computed(() => {\n  const e = this.loaded.error();\n  return e ? asApiError(e) : null;\n});',
    'readonly failure = computed(() => toApiError(this.rows.error()));',
    '@if (contents.error(); as e) {\n  <quader-error-notice [error]="error(e)" (retry)="contents.reload()" />\n}',
  ];
  assert.deepEqual(made.map((t) => handMadeFailures(t).length), [1, 1, 1]);
  assert.deepEqual(handMadeFailures('readonly failure = failureOf(this.loaded, { also: [this.formats] });'), []);

  const notice = '@if (loadError(); as e) {\n  <quader-error-notice [error]="e" (retry)="load()" />\n}';
  const table = '<quader-data-table\n  [loaded]="loaded()"\n  [hasMore]="rows().length > 0"\n  [error]="error()"\n  (retry)="load()"\n>';
  const clearing =
    'class A {\n  async load(): Promise<void> {\n    this.loadError.set(null);\n    this.error.set(null);\n    try {\n      this.v.set(await this.api.get());\n    } catch (e) {\n      this.loadError.set(asApiError(e));\n    }\n  }\n}';
  const keeping =
    'class A {\n  async load(): Promise<void> {\n    try {\n      this.v.set(await this.api.get());\n      this.loadError.set(null);\n      this.error.set(null);\n    } catch (e) {\n      this.loadError.set(asApiError(e));\n    }\n  }\n}';
  assert.deepEqual(clearedBeforeAnswer(notice, clearing).map((f) => f.line), [1]);
  assert.deepEqual(clearedBeforeAnswer(table, clearing).map((f) => f.line), [1]);
  assert.deepEqual(clearedBeforeAnswer(notice, keeping), []);
  assert.deepEqual(clearedBeforeAnswer(table, keeping), []);
  // A method the retry calls before it waits counts as its own.
  const delegating = 'class A {\n  load(): void {\n    this.reset();\n    void this.fetch();\n  }\n  private reset(): void {\n    this.loadError.set(null);\n  }\n}';
  assert.deepEqual(clearedBeforeAnswer(notice, delegating).map((f) => f.line), [1]);
});

test('R-UI-217 a loading sentence without the spinner, a list without its sentence, a held act without it and a spin of its own fail the rule', () => {
  const file = 'src/app/features/probe/probe.ts';
  const bad = [
    '<p role="status" i18n="@@probe.loading">Loading the members.</p>',
    '<span i18n="@@probe.reading">Reading the package from the upstream.</span>',
    '<quader-data-table [caption]="c" [columns]="cols" [loading]="list.loading()" />',
    '<button class="btn btn-confirm" type="submit" [disabled]="saving() || !dirty()" i18n="@@probe.save">Save changes</button>',
    '@keyframes turn {\n  to {\n    transform: rotate(360deg);\n  }\n}',
  ];
  assert.deepEqual(
    bad.map((t) => busyFindings(file, t).length),
    [1, 1, 1, 1, 1],
  );
  const good = [
    '<quader-loading i18n="@@probe.loading">Loading the members.</quader-loading>',
    '<option value="" selected i18n="@@probe.options">Loading repositories…</option>',
    '<quader-data-table [caption]="c" [columns]="cols" [loading]="list.loading()" loadingText="Loading the members." i18n-loadingText="@@probe.l" />',
    '<button class="btn btn-confirm" type="submit" [disabled]="saving() || !dirty()" [quaderBusy]="saving()" i18n="@@probe.save">Save changes</button>',
    '<button class="btn btn-tertiary" type="button" [disabled]="saving()" quaderDialogClose i18n="@@probe.cancel">Cancel</button>',
    '<button class="btn" type="button" [disabled]="!dirty()" (click)="save()">Save</button>',
    '@keyframes quader-appear {\n  from {\n    visibility: hidden;\n  }\n}',
  ];
  assert.deepEqual(good.flatMap((t) => busyFindings(file, t)), []);
});

test('R-UI-217 a held button that is no act passes in its own file only', () => {
  const add = '<button class="btn btn-default" type="button" [disabled]="loading() !== null" (click)="add(field)">Add</button>';
  assert.deepEqual(busyFindings('src/app/features/ci/name-picker.ts', add), []);
  assert.equal(busyFindings('src/app/features/ci/other.ts', add).length, 1);
});

test('R-UI-223 a view that imports the session, the API client, a dialog, an API class or an act behind a helper fails the rule', () => {
  const view = 'src/app/views/packages/probe.ts';
  const modules = {
    'src/app/shared/signatures/gate-releases': "import { SessionStore } from '../../core/session-store';\nimport { QuaderDialog } from '../ui/dialog';\n",
    'src/app/shared/format-size': 'export function formatSize(n: number): string {\n  return `${n}`;\n}\n',
    'src/app/core/format-names': "import { RepositoriesApi } from '../api/repositories-api';\n",
  };
  const read = (m) => modules[m] ?? null;
  const bad = [
    "import { SessionStore } from '../../core/session-store';",
    "import { ApiClient } from '../../api/client';",
    "import { QuaderDialog } from '../../shared/ui/dialog';",
    "import { PackageVersion, PackagesApi } from '../../api/packages-api';",
    "import {\n  DeprecationDialogData,\n  reasonText,\n} from '../../features/packages/deprecation-dialog';",
    "import { GateActions } from '../../shared/signatures/gate-releases';",
  ];
  assert.deepEqual(
    bad.map((t) => viewFindings(view, t, read).length),
    [1, 1, 1, 1, 1, 1],
  );
  const good = [
    "import { PackageVersion } from '../../api/packages-api';",
    "import { PackageReads } from '../../api/package-reads';",
    "import type { SessionStore } from '../../core/session-store';",
    "import { formatSize } from '../../shared/format-size';",
    "import { FormatCatalog } from '../../core/format-names';",
    "import { Component, inject } from '@angular/core';",
  ];
  assert.deepEqual(good.flatMap((t) => viewFindings(view, t, read)), []);
});

test('R-UI-223 the rule holds the files below src/app/views/ only, and not their specs', () => {
  const t = "import { SessionStore } from '../../core/session-store';";
  assert.equal(viewFindings('src/app/views/vulnerabilities/probe.ts', t, () => null).length, 1);
  assert.deepEqual(viewFindings('src/app/features/packages/package-version.ts', t, () => null), []);
  assert.deepEqual(viewFindings('src/app/views/packages/probe.spec.ts', t, () => null), []);
});

test('R-UI-228 the Bastion bundle check names the client, the session, a screen and a dialog that put bytes into the build', () => {
  const output = (inputs) => ({ outputs: { 'chunk-A.js': { inputs } } });
  const used = (...paths) => Object.fromEntries(paths.map((p) => [p, { bytesInOutput: 10 }]));
  const meta = output({
    ...used(
      'src/app/api/client.ts',
      'src/app/api/packages-api.ts',
      'src/app/api/legal-api-http.ts',
      'src/app/core/session-store.ts',
      'src/app/features/login/login.ts',
      'src/app/features/admin/thing.ts',
      'src/app/features/packages/deprecation-dialog.ts',
      'src/app/shared/ui/dialog.ts',
    ),
    // Tree-shaken: in the graph without a byte, so it passes.
    'src/app/api/repositories-api.ts': { bytesInOutput: 0 },
  });
  const found = bastionBundleFindings(meta).map((f) => f.path);
  assert.deepEqual(found, [
    'src/app/api/client.ts',
    'src/app/api/legal-api-http.ts',
    'src/app/api/packages-api.ts',
    'src/app/core/session-store.ts',
    'src/app/features/admin/thing.ts',
    'src/app/features/login/login.ts',
    'src/app/features/packages/deprecation-dialog.ts',
    'src/app/shared/ui/dialog.ts',
  ]);
  const clean = output(
    used(
      'src/app/api/package-reads.ts',
      'src/app/api/legal-api.ts',
      'src/app/api/refs.ts',
      'src/app/views/packages/package-version-view.ts',
      'src/app/features/packages/package-links.ts',
      'src/app/features/limits/figures.ts',
      'src/bastion/data/reads.ts',
      'node_modules/@angular/core/fesm2022/core.mjs',
    ),
  );
  assert.deepEqual(bastionBundleFindings(clean), []);
});

test('R-UI-250 a time formatted beside the one formatter fails the lint, in the code and in a template', () => {
  const bad = [
    "<td>{{ at | date: 'medium' }}</td>",
    "<time>{{ at|date }}</time>",
    "import { DatePipe } from '@angular/common';",
    "const t = formatDate(at, 'shortTime', locale);",
    "const f = new Intl.DateTimeFormat(locale, { dateStyle: 'medium' });",
    'const d = at.toLocaleDateString();',
    'const d = new Date(at).toLocaleString();',
  ];
  for (const line of bad) assert.equal(directTimeFormatting('src/app/features/probe/probe.ts', line).length, 1, line);
  const good = [
    "<td>{{ at | time }}</td>",
    "const t = formatTime(at, { part: 'time' });",
    'const n = count.toLocaleString();',
    '// no DatePipe here, nor | date',
    ' * Intl.DateTimeFormat is the formatter\'s own.',
  ];
  for (const line of good) assert.deepEqual(directTimeFormatting('src/app/features/probe/probe.ts', line), [], line);
  assert.deepEqual(directTimeFormatting(TIME_FORMATTER, "const f = new Intl.DateTimeFormat('en-US');"), []);
});
