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

test('Q-UI-335 a number field without a number width fails the check, one with its width passes', () => {
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
  ];
  for (const t of bad) assert.equal(linksToRemovedRoutes(t).length, 1, t);
  const good = [
    '<a class="btn btn-default" [routerLink]="setupLink()">Set up client</a>',
    "return [...repositoryCommands(ref), 'setup'];",
    "path: 'setup',",
    "loadComponent: () => import('../setup/client-setup').then((m) => m.ClientSetupPage),",
    "const path = setup ? '/project/setup' : raw;",
    "['/projects', project, 'repositories', key, 'files']",
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

test('Q-UI-1040 a link to the removed routes /admin/projects and /admin/repositories fails the check, /projects and /repositories pass', () => {
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

test('Q-UI-560 an icon directly in a table cell fails the check, an icon inside its badge or its .icon-name passes', () => {
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
  ];
  for (const t of good) assert.deepEqual(wordActionsSideBySide(t), [], t);
  assert.equal(wordActionsSideBySide('<table>\n<tr><td>\n<a class="btn">A</a>\n<a class="btn">B</a></td>')[0].line, 4);
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
