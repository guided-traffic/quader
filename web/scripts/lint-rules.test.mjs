// The four lint rules of R-UI-046, run against the repository's own ESLint configuration: each rule fires on a snippet that breaks
// it, and the storage wrapper may use browser storage. `npm run test:lint` writes a JUnit report that the coverage generator reads.
import assert from 'node:assert/strict';
import { test } from 'node:test';
import { fileURLToPath } from 'node:url';
import { ESLint } from 'eslint';
import { numberFieldsWithoutWidth } from './field-width-rule.mjs';
import { linksToRemovedRoutes } from './removed-routes-rule.mjs';

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
