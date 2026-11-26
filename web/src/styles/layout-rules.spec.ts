// The shared layout rules of R-UI-145, read from the stylesheets themselves. jsdom lays nothing out, so a component test can hold
// the structure a rule selects (a status's badge, a format's `.format-name`) but not the rule; this test holds the rules, so that
// a change of a selector that would bring a defect of the admin area's walk back (2026-11-18) fails here.
import catalogScss from './_catalog-screens.scss' with { loader: 'text' };
import controlsScss from './_controls.scss' with { loader: 'text' };
import listScss from './_list-screens.scss' with { loader: 'text' };
import policyEditorScss from '../app/features/permissions/policy-editor.scss' with { loader: 'text' };
import mainScss from './main.scss' with { loader: 'text' };
import packageDetailScss from '../app/views/packages/package-detail-view.scss' with { loader: 'text' };

/** The declarations of the first rule whose selector line matches, without comments. */
function block(scss: string, selector: RegExp): string {
  const at = scss.search(selector);
  expect(at, `no rule for ${selector}`).toBeGreaterThan(-1);
  const open = scss.indexOf('{', at);
  return scss.slice(open + 1, scss.indexOf('}', open)).replace(/\/\/.*$/gm, '');
}

describe('Shared layout rules (R-UI-145)', () => {
  it('R-UI-145 a format, a class and a type with their 20 px icon are centred on the 16 px line of a data table\'s cell', () => {
    const rule = block(listScss, /tbody td :is\(\.format-name, \.class-name, \.type-cell\) \{/);
    expect(rule).toContain('margin-block: calc((1rem - 1.25rem) / 2)');
  });

  it('R-UI-175 a control in a cell of every other table is centred on the cell\'s first line of text by the line rule', () => {
    const rule = block(catalogScss, /tbody :is\(th, td\) > :is\(\.btn, quader-copy-button, \.row-actions\) \{/);
    expect(rule).toContain('margin-block: calc((var(--quader-line) - var(--quader-control-height)) / 2)');
    expect(rule).toContain('vertical-align: top');
  });

  it('R-UI-175 a control reads the line of the box it stands in: every box sets --quader-line, a control inherits it', () => {
    expect(controlsScss).toMatch(/@property --quader-line \{[^}]*syntax: '<length>';[^}]*inherits: true;[^}]*initial-value: 21px;/);
    expect(block(controlsScss, /^\* \{/m)).toContain('--quader-line: 1lh');
    expect(block(controlsScss, /^:where\(/m)).toContain('--quader-line: inherit');
    expect(block(controlsScss, /^\.on-line \{/m)).toContain('margin-block: calc((var(--quader-line) - var(--quader-control-height)) / 2)');
  });

  it('R-UI-145 a row\'s header that is an identifier stays on one line, as a cell of one does', () => {
    const at = listScss.indexOf("th[scope='row'][translate='no']:not(.wrap-name),");
    expect(at).toBeGreaterThan(-1);
    expect(listScss.slice(at, listScss.indexOf('}', at))).toContain('white-space: nowrap');
  });

  it('R-UI-171 a date or a time never breaks inside itself, in a fact or a sentence as in a list\'s cell', () => {
    expect(block(mainScss, /^time \{/m)).toContain('white-space: nowrap');
  });

  it('R-UI-172 the items of a version\'s cell stand at the top of their line, and an untrusted signer\'s words are the cell\'s second line', () => {
    expect(block(packageDetailScss, /^\.version-cell \{/m)).toContain('align-items: flex-start');
    expect(block(packageDetailScss, /> quader-signature-badge \{/)).toContain('display: contents');
  });

  it('R-UI-145 a visibility level in a cell of its own keeps its icon and its word on one line', () => {
    expect(block(listScss, /^td:is\(\.level, \.level-cell\) \{/m)).toContain('white-space: nowrap');
  });

  it('R-UI-145 a check box keeps its size beside a label of several lines', () => {
    expect(block(catalogScss, /> input \{/)).toContain('flex: none');
  });

  it('R-UI-145 the policy editor\'s save bar takes the content\'s bottom padding into itself, so nothing shows below it', () => {
    const rule = block(policyEditorScss, /^\.save-bar \{/m);
    expect(rule).toContain('inset-block-end: calc(-1 * var(--page-end))');
    expect(rule).toContain('margin-block-end: calc(-1 * var(--page-end))');
    expect(rule).toContain('calc(var(--quader-space-3) + var(--page-end))');
  });
});
