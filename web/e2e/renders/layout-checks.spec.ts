import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import { Page, expect, test } from '@playwright/test';
import { compile } from 'sass';
import { CODE_BLOCK_SCROLL, LayoutFinding, innerScroll, judge, layoutFindings, loadingPlace } from './layout';
import { root } from './paths';

/**
 * The layout checks of the renders tested on pages of their own (R-UI-176, Q-UI-723): each fixture is a few elements in a page
 * of Chromium with the app's fonts, and says which finding the checks must name, or that they must name none. A check that
 * passes the owner's row of 2026-11-19 is broken, so that row is the first fixture. `make ui-layout-test`; no stack, no seed.
 *
 * The second group renders the shared patterns of the style guide with the app's own global stylesheet (web/src/styles,
 * compiled here): the shared rule of R-UI-175 holds without a running UI (R-UI-176).
 */

const web = join(root, 'web');
const dataUrl = (file: string): string => `data:font/woff2;base64,${readFileSync(join(web, 'node_modules', file)).toString('base64')}`;
const fonts = `
  @font-face { font-family: 'Inter Variable'; font-weight: 100 900; src: url(${dataUrl('@fontsource-variable/inter/files/inter-latin-wght-normal.woff2')}) format('woff2'); }
  @font-face { font-family: 'Material Icons'; font-weight: 400; src: url(${dataUrl('material-icons/iconfont/material-icons-outlined.woff2')}) format('woff2'); }
`;

/** The few rules of the app the plain fixtures need, as the app has them (_tokens.scss, _controls.scss, _icons.scss). */
const base = `
  body { margin: 0; padding: 16px; font: 400 14px / 1.5 'Inter Variable', sans-serif; font-feature-settings: 'cv05' 1; }
  .material-icons { font-family: 'Material Icons'; font-size: 24px; line-height: 1; display: inline-block; white-space: nowrap; font-feature-settings: 'liga'; }
  .btn { box-sizing: border-box; display: inline-flex; align-items: center; justify-content: center; gap: 4px; min-block-size: 32px;
    padding: 0 12px; border: 1px solid #888; font: inherit; font-size: 14px; line-height: 1rem; white-space: nowrap; background: none; }
  .btn-icon { inline-size: 32px; padding: 0; }
  .btn .material-icons { font-size: 1rem; }
  .badge { box-sizing: border-box; display: inline-flex; align-items: center; gap: 4px; min-block-size: 20px; padding: 2px 6px;
    border-radius: 999px; font-size: 12px; line-height: 1rem; white-space: nowrap; vertical-align: middle; background: #ddd; }
  input[type='checkbox'] { inline-size: 1rem; block-size: 1rem; margin: 0.125rem 0 0; }
  table.data { border-collapse: separate; border-spacing: 0; inline-size: 900px; }
  table.data th, table.data td { padding: 16px; text-align: start; vertical-align: top; line-height: 1rem; border-block-end: 1px solid #ccc; }
  table.data tbody th[scope='row'] { font-weight: 600; }
  quader-avatar { display: inline-block; inline-size: 32px; block-size: 32px; border-radius: 50%; background: #888; flex: none; }
  .visually-hidden { position: absolute; inline-size: 1px; block-size: 1px; overflow: hidden; clip-path: inset(50%); }
`;

let sharedCss: string | null = null;
/** The app's global stylesheet, compiled as the build compiles it; the fonts above replace its file URLs. */
function shared(): string {
  // Its own font faces name files relative to the build, which a page of its own cannot load: the fonts above take their place.
  sharedCss ??= compile(join(web, 'src', 'styles', 'main.scss'), { loadPaths: [join(web, 'node_modules'), join(web, 'src', 'styles')] }).css.replace(
    /@font-face\s*\{[^}]*\}/g,
    '',
  );
  return sharedCss;
}

async function check(page: Page, body: string, css = '', withShared = false, lang = 'en'): Promise<LayoutFinding[]> {
  await page.setContent(
    `<!doctype html><html lang="${lang}"><head><style>${withShared ? shared() : base}</style><style>${fonts}${css}</style></head>` +
      `<body><main class="content">${body}</main></body></html>`,
  );
  await page.evaluate(async () => {
    await document.fonts.load('16px "Material Icons"', 'expand_more');
    await document.fonts.load('600 14px "Inter Variable"', 'a');
    await document.fonts.ready;
  });
  return page.evaluate(layoutFindings);
}

const only = (found: readonly LayoutFinding[], check: string): LayoutFinding[] => found.filter((f) => f.check === check);
const describe = (found: readonly LayoutFinding[]): string => found.map((f) => `${f.check} ${f.what}: ${f.detail}`).join('\n');

/** The middle of an element's box and of the first line of a text, relative to a row's top (run in the page). */
async function middles(page: Page, box: string, text: string, row: string): Promise<{ box: number; text: number; row: number }> {
  return page.evaluate(
    ([b, t, r]) => {
      const top = document.querySelector(r!)!.getBoundingClientRect().top;
      const bb = document.querySelector(b!)!.getBoundingClientRect();
      const range = document.createRange();
      range.selectNodeContents(document.querySelector(t!)!.firstChild!);
      const tb = range.getClientRects()[0]!;
      return { box: (bb.top + bb.bottom) / 2 - top, text: (tb.top + tb.bottom) / 2 - top, row: document.querySelector(r!)!.getBoundingClientRect().height };
    },
    [box, text, row],
  );
}

// ---- The owner's row (2026-11-19, the images of a public remote) -------------------------------------------------------------

/** A repository's item: the button that opens the row, the name beside it, the other cells; as the screen built it. */
const ownerRow = (nameCellCss: string): [string, string] => [
  `<table class="data"><tbody><tr id="row">
     <th scope="row" class="wrap-name"><div class="name-cell">
       <button type="button" class="btn btn-tertiary btn-icon" aria-expanded="false" aria-label="Show the versions of guidedtraffic/kubectl"><span class="material-icons expand-chevron" aria-hidden="true">expand_more</span></button>
       <span><a class="item-name" id="name" href="#">guidedtraffic/kubectl</a></span>
     </div></th>
     <td translate="no">latest</td><td class="num">1</td><td>4 minutes ago</td><td class="num">3</td>
   </tr></tbody></table>`,
  `.name-cell { display: flex; align-items: flex-start; gap: 4px; } .name-cell > .btn { flex: none; } ${nameCellCss}`,
];

/** A one-line command with its joined copy button, as the address field builds it; the button's margins are the case's. */
const joinedField = (buttonCss: string): [string, string] => [
  `<quader-address-field><input class="address-field-input" readonly value="docker pull localhost:4200/web-frontend/images/portal/api:1.4.0">
     <quader-copy-button><button type="button" class="btn btn-icon" aria-label="Copy the command"><span class="material-icons" aria-hidden="true">content_copy</span></button></quader-copy-button>
   </quader-address-field>`,
  `quader-address-field { display: flex; align-items: stretch; inline-size: 600px; line-height: 1rem; }
   .address-field-input { box-sizing: border-box; flex: 1 1 auto; min-block-size: 32px; padding: 4px 12px; border: 1px solid #888; font: inherit; }
   quader-copy-button { display: flex; flex: none; margin-inline-start: -1px; ${buttonCss} }`,
];

test.describe('a label kept on one line (R-UI-171)', () => {
  const narrow = 'div.box { inline-size: 220px; } h2 { margin: 0; font-size: 16px; } .toggle { all: unset; display: inline; cursor: pointer; }';

  test('R-UI-171 a button whose label breaks into two lines is found', async ({ page }) => {
    const found = await check(page, '<div class="box"><button type="button" class="btn" style="white-space: normal">Remove a version, import tasks and tombstones</button></div>', narrow);
    expect(only(found, 'broken'), describe(found)).toHaveLength(1);
  });

  test('R-UI-171 the toggle of a section\'s heading wraps as a heading does and passes', async ({ page }) => {
    const found = await check(page, '<div class="box"><h2 class="heading"><button type="button" class="toggle" aria-expanded="false">Remove a version, import tasks and tombstones</button></h2></div>', narrow);
    expect(only(found, 'broken'), describe(found)).toHaveLength(0);
  });

  test('R-UI-171 an inline fold\'s toggle is a button: its broken label is found, named by its label and not by the anchor Angular leaves', async ({ page }) => {
    // As the shared disclosure builds it: the chevron, the label, and the comment of its `@if (note())` block.
    const fold =
      '<div class="box"><div class="disclosure"><button type="button" class="inline-toggle" aria-expanded="false">' +
      '<span class="material-icons" aria-hidden="true">chevron_right</span><span>For a remote repository without upstream keys</span><!--container-->' +
      '</button></div></div>';
    const found = await check(page, fold, `${narrow} .inline-toggle { all: unset; display: inline-flex; align-items: center; gap: 4px; font-weight: 600; }`);
    const broken = only(found, 'broken');
    expect(broken, describe(found)).toHaveLength(1);
    expect(broken[0]!.text).toBe('For a remote repository without upstream keys');
  });
});

test.describe('a fact\'s value broken inside a word (G4-10, R-UI-171)', () => {
  // The System page's card Installation at 390 px (2026-11-23): the term's column as wide as its longest term, the value squeezed.
  const facts = (css: string): string =>
    `<style>dl { display: grid; grid-template-columns: max-content 1fr; gap: 4px 16px; inline-size: 300px; margin: 0; } dd { margin: 0; overflow-wrap: anywhere; } ${css}</style>` +
    '<dl><dt>Answered by</dt><dd>quader-all-7c9d</dd><dt>Licences of the bundled software</dt><dd><a href="x">3rdpartylicenses.txt</a></dd></dl>';

  test('G4-10 a value broken inside its word beside a term that took the room is found, a long token too', async ({ page }) => {
    expect(only(await check(page, facts('')), 'broken').length).toBeGreaterThan(0);
  });

  test('G4-10 the same facts with each term above its value pass', async ({ page }) => {
    expect(only(await check(page, facts('dl { grid-template-columns: minmax(0, 1fr); }')), 'broken')).toEqual([]);
  });
});

test.describe('a cell\'s content running into the next cell (R-UI-173)', () => {
  // The System page's replicas at 1,280 px (2026-11-23): the checks' grid column had a least width of 0, the table made the State
  // column narrower than "malwareUpdaterFreshclam" beside its icon, and the words ran under the heartbeat.
  const replica = (columns: string, word = ''): string =>
    `<table class="data" style="inline-size: 470px"><tbody><tr><td><div style="display: grid; grid-template-columns: ${columns}; column-gap: 12px">` +
    '<span class="badge">Ready</span><div style="display: flex; gap: 4px"><span class="material-icons">warning</span>' +
    `<span style="min-inline-size: 0; ${word}">malwareUpdaterFreshclam</span></div></div></td>` +
    '<td style="inline-size: 300px">1:43:55 PM</td></tr></tbody></table>';

  test('R-UI-173 a check whose column claims no least width runs into the next cell and is found', async ({ page }) => {
    const found = only(await check(page, replica('auto minmax(0, 1fr)')), 'cell-overflow');
    expect(found, describe(found)).toHaveLength(1);
    expect(found[0]!.text).toBe('malwareUpdaterFreshclam');
  });

  test('R-UI-173 the column at least as wide as its longest word passes', async ({ page }) => {
    expect(only(await check(page, replica('auto minmax(min-content, 1fr)')), 'cell-overflow')).toEqual([]);
  });

  test('R-UI-173 a text its own box cuts with an ellipsis is not counted here (the check clipped is its)', async ({ page }) => {
    const cut = 'display: block; overflow: hidden; text-overflow: ellipsis; white-space: nowrap;';
    expect(only(await check(page, replica('auto minmax(0, 1fr)', cut)), 'cell-overflow')).toEqual([]);
  });
});

test.describe('a text cut with room beside it (R-UI-173)', () => {
  // An image's platform digest in a stacked block at 1,024 px: cut where its copy button starts, which holds the room to its right.
  const digest = (button: string): string =>
    '<table class="data"><tbody><tr><td style="inline-size: 400px">' +
    '<code style="display: inline-block; max-inline-size: 300px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; vertical-align: top">' +
    'sha256:b2b6838dabe5159c6022b1f94626ab13d2615ce27c554a7b94b7cc3d4b20cf0a</code>' +
    `${button}</td></tr></tbody></table>`;

  test('R-UI-173 a digest cut beside the room of its copy button passes; cut with the room free beside it is found', async ({ page }) => {
    const beside = await check(page, digest(' <button type="button" class="btn btn-icon" style="inline-size: 90px" aria-label="Copy"><span class="material-icons">content_copy</span></button>'));
    expect(only(beside, 'clipped'), describe(beside)).toEqual([]);
    const free = await check(page, digest(''));
    expect(only(free, 'clipped'), describe(free)).toHaveLength(1);
  });
});

test.describe('a button joined to a field (R-UI-175)', () => {
  test('R-UI-175 a joined button that the line rule gave margins of -8 px is 48 px beside a field of 32 and is found', async ({ page }) => {
    const [html, css] = joinedField('margin-block: -8px;');
    const found = await check(page, html, css);
    const joined = only(found, 'joined');
    expect(joined, describe(found)).toHaveLength(1);
    expect(joined[0]!.detail).toContain('48 px high');
    expect(joined[0]!.detail).toContain('its field 32 px');
  });

  test('R-UI-175 a joined button without margins is as high as its field and passes', async ({ page }) => {
    const [html, css] = joinedField('margin-block: 0;');
    expect(only(await check(page, html, css), 'joined')).toHaveLength(0);
  });
});

test.describe('the owner\'s row: a control beside a name in a table (R-UI-175)', () => {
  test('R-UI-175 the button 8 px below the line of the name is found, as the owner measured it', async ({ page }) => {
    const [html, css] = ownerRow('');
    const found = await check(page, html, css);
    const m = await middles(page, '.name-cell > button', '#name', '#row');
    // As measured in the running UI: the row 65 px high, the button's middle at 32 px, the name's at 24 px.
    expect(Math.round(m.row)).toBe(65);
    expect(Math.round(m.box)).toBe(32);
    expect(Math.round(m.text)).toBe(24);
    const off = only(found, 'off-line');
    expect(off, describe(found)).toHaveLength(1);
    expect(off[0]!.what).toContain('button.btn.btn-tertiary');
    // The name's glyphs stand half a pixel above its line's middle (the font's ascent and descent rounded to whole pixels).
    expect(off[0]!.detail).toMatch(/ 8\.[05] px below/);
  });

  test('R-UI-175 the button centred on the name\'s line by its margins passes, and the row is as high as its text', async ({ page }) => {
    const [html, css] = ownerRow('.name-cell > .btn { margin-block: calc((1lh - 32px) / 2); }');
    const found = await check(page, html, css);
    expect(found, describe(found)).toEqual([]);
    const m = await middles(page, '.name-cell > button', '#name', '#row');
    expect(Math.round(m.row)).toBe(49);
  });
});

// ---- Tables ---------------------------------------------------------------------------------------------------------------------

test.describe('a table\'s row (R-UI-176)', () => {
  const row = (actionCss: string): [string, string] => [
    `<table class="data"><tbody><tr>
       <th scope="row"><a href="#">acme-agent</a></th><td>Debian</td>
       <td class="actions"><button type="button" class="btn btn-icon" aria-haspopup="menu" aria-label="Actions for acme-agent"><span class="material-icons">more_vert</span></button></td>
     </tr></tbody></table>`,
    actionCss,
  ];

  test('R-UI-176 a menu in an action cell at the cell\'s top is 8 px below the row\'s line', async ({ page }) => {
    const found = await check(page, ...row(''));
    const off = only(found, 'row-offset');
    expect(off, describe(found)).toHaveLength(1);
    expect(off[0]!.detail).toMatch(/Actions for acme-agent.* 8\.[05] px below/);
  });

  test('R-UI-176 the menu centred on the line by its margins passes', async ({ page }) => {
    const found = await check(page, ...row('td.actions > .btn { margin-block: calc((1rem - 32px) / 2); vertical-align: top; }'));
    expect(found, describe(found)).toEqual([]);
  });

  test('R-UI-176 a cell whose text stands 2 px lower than the name is found', async ({ page }) => {
    const found = await check(
      page,
      `<table class="data"><tbody><tr><th scope="row">acme-agent</th><td class="low">Debian</td></tr></tbody></table>`,
      'table.data td.low { padding-block-start: 18px; }',
    );
    expect(only(found, 'row-offset'), describe(found)).toHaveLength(1);
  });

  test('R-UI-176 a badge on the line\'s top with margins passes beside texts of another size; on vertical-align: middle it is 1.7 px low', async ({ page }) => {
    const badge = (align: string): [string, string] => [
      `<table class="data"><tbody><tr><th scope="row">acme-agent</th><td><span class="badge">disabled</span></td><td><small>12 px</small></td></tr></tbody></table>`,
      `tbody td .badge { margin-block: calc((1rem - 20px) / 2); vertical-align: ${align}; } small { font-size: 12px; }`,
    ];
    const ok = await check(page, ...badge('top'));
    expect(ok, describe(ok)).toEqual([]);
    // The middle of `vertical-align: middle` is the middle of the lower-case letters, not of the line (Q-UI-720).
    const low = only(await check(page, ...badge('middle')), 'row-offset');
    expect(low).toHaveLength(1);
    expect(low[0]!.detail).toMatch(/1\.\d px below/);
  });

  // A version's delivery and cache on the tab Upstream (Q-CUR-157): the state's badge, and the rule that decided it (a secondary line)
  // or the link to the version's page (a line of the cell's own text) under it. The badge is the cell's first line; the check had
  // measured the line under it and found the cell 16 px low.
  for (const under of ['<span class="secondary">Rule: this version</span>', '<a href="#" class="line">Open the version</a>']) {
    test(`R-UI-172 a badge with ${under.startsWith('<a') ? 'a link' : 'a secondary line'} under it is the cell's first line: on the row's line it passes, 4 px low it is found`, async ({ page }) => {
      const cell = (badgeCss: string): [string, string] => [
        `<table class="data"><tbody><tr><th scope="row">4.98.1<span class="secondary">5 days ago</span></th>
           <td><span class="badge">Allowed</span>${under}</td><td>Not stored</td></tr></tbody></table>`,
        `tbody td .badge { margin-block: calc((1rem - 20px) / 2); vertical-align: top; ${badgeCss} } .secondary { display: block; font-size: 12px; } .line { display: block; }`,
      ];
      const ok = await check(page, ...cell(''));
      expect(ok, describe(ok)).toEqual([]);
      const low = only(await check(page, ...cell('margin-block-start: 2px;')), 'row-offset');
      expect(low, describe(low)).toHaveLength(1);
      expect(low[0]!.detail).toMatch(/the middle of ".*Allowed.*" is 4\.\d px below/);
    });
  }

  // A version's delivery on the tab Upstream (Q-CUR-201): the switch beside the state's badge on one line, the rule under them. The
  // switch (24 px) stands on the middle of the line by its margins, as quader-switch places itself.
  test('R-UI-163 R-UI-172 a switch beside its badge on one line passes', async ({ page }) => {
    const cell = (switchCss: string): [string, string] => [
      `<table class="data"><tbody><tr><th scope="row">4.98.1<span class="secondary">5 days ago</span></th>
         <td><span class="state-line"><button type="button" role="switch" aria-checked="true" aria-label="Deliver acme 4.98.1" class="switch"></button><span class="badge">Allowed</span></span><span class="secondary">Rule: this version</span></td>
         <td><span class="state-line"><span class="muted">Not stored</span><button type="button" class="btn btn-tertiary on-line">Download</button></span></td></tr></tbody></table>`,
      `.state-line { display: inline-flex; align-items: flex-start; gap: 8px; }
       .switch { inline-size: 2.5rem; block-size: 1.5rem; padding: 0; border: 0; border-radius: 12px; ${switchCss} }
       tbody td .badge { margin-block: calc((1rem - 20px) / 2); } .secondary { display: block; font-size: 12px; }
       .on-line { margin-block: calc((1rem - 32px) / 2); }`,
    ];
    const ok = await check(page, ...cell('margin-block: calc((1rem - 1.5rem) / 2);'));
    expect(ok, describe(ok)).toEqual([]);
  });

  test('R-UI-176 a stacked row: the menu on the name\'s line passes, the menu at the block\'s top is found', async ({ page }) => {
    const stacked = (menuCss: string): [string, string] => [
      `<table class="data" style="display:block"><tbody style="display:block"><tr style="display:flex;flex-wrap:wrap;align-items:center">
         <th scope="row" style="flex:1 1 0;align-self:flex-start;display:block"><a href="#">acme-agent</a></th>
         <td class="actions" style="display:block;align-self:flex-start"><button type="button" class="btn btn-icon" aria-haspopup="menu" aria-label="Actions"><span class="material-icons">more_vert</span></button></td>
         <td style="display:block;flex-basis:100%">Format Debian</td>
       </tr></tbody></table>`,
      menuCss,
    ];
    expect(only(await check(page, ...stacked('')), 'row-offset')).toHaveLength(1);
    const ok = await check(page, ...stacked('td.actions > .btn { margin-block: calc((1rem - 32px) / 2); vertical-align: top; }'));
    expect(ok, describe(ok)).toEqual([]);
  });

  // An ended token's row at 1440 px before Q-UI-762: "Ended" with its time and "Expired" under it, centred against "Limit" and
  // "Last used", stood a line above them.
  const facts = (align: string, ended = '<quader-time-ago>3 days ago</quader-time-ago><br /><span class="warn">Expired</span>'): [string, string] => [
    `<div class="quader-data-table stacked" style="inline-size:420px"><table class="data" style="display:block;inline-size:auto"><tbody style="display:block">
       <tr style="display:flex;flex-wrap:wrap;align-items:${align};gap:4px 20px">
         <th scope="row" style="flex:1 1 0;align-self:flex-start;display:block">nightly-mirror</th>
         <td data-label="Limit" style="display:block;order:2">Default (Technical)</td>
         <td data-label="Ended" style="display:block;order:2">${ended}</td>
         <td data-label="Last used" style="display:block;order:2">4 days ago</td>
       </tr></tbody></table></div>`,
    `table.data tbody td[data-label]::before { content: attr(data-label) ' '; color: #666; }
     table.data tbody tr::before { content: ''; order: 1; flex-basis: 100%; block-size: 0; }`,
  ];

  // These blocks put their fields side by side, the form of Q-UI-681 before Q-UI-1050: `stacked-place` names that form (its own
  // fixtures below), and the checks of a field's name and value are measured here apart from it.
  const sideBySide = (f: LayoutFinding): boolean => f.check !== 'stacked-fits' && f.check !== 'stacked-place';

  test('R-UI-174 a stacked row: names of one line on one line pass, a value of two lines centred against its neighbours is found', async ({ page }) => {
    const centred = await check(page, ...facts('center'));
    expect(only(centred, 'stacked-offset'), describe(centred)).toHaveLength(1);
    const ok = (await check(page, ...facts('baseline'))).filter(sideBySide);
    expect(ok, describe(ok)).toEqual([]);
  });

  // T-25's repositories at 390 px (2026-11-23), side by side as Q-UI-681 had them: "Format" beside "Project" in the first block and
  // beside "Class" in the second, as the values' widths fell.
  const twoBlocks = (fieldCss: string): [string, string] => [
    `<div class="quader-data-table stacked" style="inline-size:358px"><table class="data" style="display:block;inline-size:auto"><tbody style="display:block">
       <tr style="display:flex;flex-wrap:wrap;align-items:baseline;gap:4px 20px">
         <th scope="row" style="flex:1 1 0;display:block">assets</th>
         <td data-label="Project" style="display:block;order:2">Web frontend</td>
         <td data-label="Format" style="display:block;order:2">Generic</td>
         <td data-label="Class" style="display:block;order:2">Local</td>
       </tr>
       <tr style="display:flex;flex-wrap:wrap;align-items:baseline;gap:4px 20px">
         <th scope="row" style="flex:1 1 0;display:block">images</th>
         <td data-label="Project" style="display:block;order:2">Customer portal release engineering</td>
         <td data-label="Format" style="display:block;order:2">OCI (Docker)</td>
         <td data-label="Class" style="display:block;order:2">Local</td>
       </tr></tbody></table></div>`,
    `table.data tbody td[data-label]::before { content: attr(data-label) ' '; color: #666; }
     table.data tbody tr::before { content: ''; order: 1; flex-basis: 100%; block-size: 0; }
     ${fieldCss}`,
  ];

  test('R-UI-174 a stacked list whose fields stand side by side, each at another place in each block, is found', async ({ page }) => {
    const found = await check(page, ...twoBlocks(''));
    expect(only(found, 'stacked-place').length, describe(found)).toBeGreaterThan(0);
  });

  test('R-UI-174 the same blocks with a field a line, names in one column and values in the next, pass', async ({ page }) => {
    const grid = `table.data tbody tr > td { flex: 1 1 100% !important; display: table !important; table-layout: fixed; inline-size: 100%; }
      table.data tbody td[data-label]::before { content: attr(data-label); display: table-cell; box-sizing: border-box; inline-size: 80px; padding-inline-end: 16px; }`;
    const found = (await check(page, ...twoBlocks(grid))).filter((f) => f.check !== 'stacked-fits');
    expect(found, describe(found)).toEqual([]);
  });

  test("R-UI-174 a stacked row: a value under its column's name is found", async ({ page }) => {
    const meter = '<div style="display:flex;flex-direction:column"><span><span style="display:inline-block;inline-size:64px;block-size:6px;background:#888"></span> 22%</span><span class="badge">Close to the limit</span></div>';
    const apart = await check(page, ...facts('baseline', meter));
    expect(only(apart, 'label-apart'), describe(apart)).toHaveLength(1);
    // The admin area's CI issuers at 1024 px: one audience of six letters in a block of its own under "Audiences".
    const audience = await check(page, ...facts('baseline', '<div>quader</div>'));
    expect(only(audience, 'label-apart'), describe(audience)).toHaveLength(1);
  });

  test("R-UI-174 a stacked row: a value whose first piece is wider than the room beside its name may start under it", async ({ page }) => {
    // A media type that does not break, and a link that is a box of its own (a repository's long key in the search results).
    const word = (await check(page, ...facts('baseline', '<span>application/vnd.oci.image.manifest.v1+json</span>'))).filter((f) => f.check === 'label-apart');
    expect(word, describe(word)).toEqual([]);
    const link = (
      await check(page, ...facts('baseline', '<a href="#" style="display:inline-block">release-candidates-for-the-customer-portal-frontend</a>'))
    ).filter((f) => f.check === 'label-apart');
    expect(link, describe(link)).toEqual([]);
  });

  test('R-UI-176 a document\'s table is not measured', async ({ page }) => {
    const found = await check(page, `<div class="readme"><table class="data"><tbody><tr><th>a</th><td class="low">b</td></tr></tbody></table></div>`, 'table.data td.low { padding-block-start: 30px; }');
    expect(found, describe(found)).toEqual([]);
  });
});

// ---- Rows of boxes and running text ------------------------------------------------------------------------------------------

test.describe('a control or an icon beside its text (R-UI-170, R-UI-175)', () => {
  test('R-UI-175 a control whose text is the sibling of its parent is paired with it (two levels away)', async ({ page }) => {
    const found = await check(
      page,
      `<div class="row"><span class="wrap"><button type="button" class="btn">Copy</button></span><span class="text">The address of the repository</span></div>`,
      '.row { display: flex; align-items: flex-start; gap: 8px; }',
    );
    expect(only(found, 'off-line'), describe(found)).toHaveLength(1);
  });

  test('R-UI-175 a check box beside a label of two lines: on the first line passes, centred on both is found', async ({ page }) => {
    const box = (align: string): [string, string] => [
      `<label class="check-row" style="inline-size:200px"><input type="checkbox" /><span>Public issuer: any workload may ask for a token</span></label>`,
      `.check-row { display: flex; align-items: ${align}; gap: 8px; } .check-row > input { flex: none; }`,
    ];
    const ok = await check(page, ...box('flex-start'));
    expect(ok, describe(ok)).toEqual([]);
    // Centred on a label of three lines it shares no height with the first line: icon-apart rather than off-line.
    const centred = await check(page, ...box('center'));
    expect(centred.filter((f) => /^(off-line|icon-apart)$/.test(f.check)), describe(centred)).toHaveLength(1);
  });

  test('R-UI-170 an icon on its own line above its key is icon-apart (W-3)', async ({ page }) => {
    const found = await check(
      page,
      `<div style="inline-size:120px"><span class="material-icons" style="font-size:20px">lock</span> customerportalreleasedelivery</div>`,
    );
    expect(only(found, 'icon-apart'), describe(found)).toHaveLength(1);
  });

  test('R-UI-175 an icon in running text: on vertical-align: middle it is 1.7 px low, on the line\'s top with margins it passes', async ({ page }) => {
    const icon = (style: string): string => `<p>Signed in as <span class="material-icons" style="font-size:18px;${style}">person</span> priya</p>`;
    const low = only(await check(page, icon('vertical-align:middle')), 'off-line');
    expect(low).toHaveLength(1);
    expect(low[0]!.detail).toMatch(/1\.\d px below/);
    const ok = await check(page, icon('vertical-align:top;margin-block:calc((21px - 18px) / 2)'));
    expect(ok, describe(ok)).toEqual([]);
  });

  test('R-UI-175 a picture of two lines (a 32 px avatar beside a name and its user name) is centred on both lines', async ({ page }) => {
    const person = (align: string): [string, string] => [
      `<div class="person"><quader-avatar></quader-avatar><span><strong>Priya Patel</strong><span style="display:block">@priya</span></span></div>`,
      `.person { display: flex; align-items: ${align}; gap: 8px; line-height: 1.25rem; }`,
    ];
    const ok = await check(page, ...person('center'));
    expect(ok, describe(ok)).toEqual([]);
    const top = await check(page, ...person('flex-start'));
    expect(only(top, 'off-line'), describe(top)).toHaveLength(1);
    // Beside a name of one line the avatar is no picture of two lines: centred on that line or found.
    const one = await check(page, `<div class="person"><quader-avatar></quader-avatar><strong>Priya Patel</strong></div>`, '.person { display: flex; align-items: flex-start; gap: 8px; }');
    expect(only(one, 'off-line'), describe(one)).toHaveLength(1);
  });

  test('R-UI-175 a button is never a picture: a 32 px button centred on a name and its description is found', async ({ page }) => {
    const found = await check(
      page,
      `<div class="r"><button type="button" class="btn btn-icon" aria-label="Open"><span class="material-icons">expand_more</span></button><span><a href="#">portal/web</a><span style="display:block">The web image</span></span></div>`,
      '.r { display: flex; align-items: center; gap: 4px; line-height: 1rem; }',
    );
    expect(only(found, 'off-line'), describe(found)).toHaveLength(1);
  });

  test('R-UI-175 the mark after a link of two lines may stand on its last line, centred on it', async ({ page }) => {
    const link = (css: string): [string, string] => [
      `<span class="external-link" style="inline-size:150px"><a href="#">https://github.com/guided-traffic/quader-images</a><span class="material-icons" style="font-size:18px">open_in_new</span></span>`,
      `.external-link { display: inline-flex; gap: 4px; } .external-link > a { min-inline-size: 0; overflow-wrap: anywhere; } ${css}`,
    ];
    const ok = await check(page, ...link('.external-link { align-items: flex-end; } .external-link > .material-icons { margin-block-end: calc((21px - 18px) / 2); }'));
    expect(ok, describe(ok)).toEqual([]);
    // On the last baseline the glyph's box stands on the baseline: 3.5 px above the line's middle.
    const high = await check(page, ...link('.external-link { align-items: last baseline; }'));
    expect(only(high, 'off-line'), describe(high)).toHaveLength(1);
  });

  test('R-UI-175 the exceptions: a control above its text, a field under its label, a multi-line control beside a label', async ({ page }) => {
    const found = await check(
      page,
      `<div class="empty"><span class="material-icons" style="font-size:48px">inventory_2</span><p>There is no package yet.</p></div>
       <div class="field"><label for="k">Key</label><input id="k" class="text-input" /></div>
       <div class="tile"><span class="material-icons">deployed_code</span><div>npm</div></div>
       <div class="side"><label for="d">Description</label><textarea id="d" rows="4"></textarea></div>`,
      `.empty { display: flex; flex-direction: column; align-items: center; }
       .field label { display: block; } .text-input { display: block; min-block-size: 32px; box-sizing: border-box; }
       .tile .material-icons { display: block; }
       .side { display: flex; align-items: flex-start; gap: 8px; }`,
    );
    expect(found, describe(found)).toEqual([]);
  });

  test('R-UI-175 a wrapping toolbar moves a control to its next line without a finding; an icon moved away from its text is found', async ({ page }) => {
    const toolbar = await check(
      page,
      `<div class="bar"><span>Showing 20 of 40 packages in this repository</span><button type="button" class="btn">Load more</button></div>`,
      '.bar { display: flex; flex-wrap: wrap; align-items: center; gap: 8px; inline-size: 300px; }',
    );
    expect(toolbar, describe(toolbar)).toEqual([]);
    const status = await check(
      page,
      `<div class="bar"><span class="material-icons" style="font-size:16px">check_circle</span><span>Succeeded after a very long run of the job</span></div>`,
      '.bar { display: flex; flex-wrap: wrap; gap: 8px; inline-size: 200px; } .bar > span:last-child { flex: 0 0 200px; }',
    );
    expect(only(status, 'icon-apart'), describe(status)).toHaveLength(1);
  });

  test('R-UI-175 a badge with its own word may wrap to a line of its own; an icon may not', async ({ page }) => {
    const badge = await check(page, `<p style="inline-size:120px;line-height:1rem">in 2 hours <span class="badge">Expires soon</span></p>`);
    expect(badge, describe(badge)).toEqual([]);
    const icon = await check(page, `<p style="inline-size:72px">in 2 hours <span class="material-icons" style="font-size:18px">schedule</span></p>`);
    expect(only(icon, 'icon-apart'), describe(icon)).toHaveLength(1);
  });

  test('R-UI-175 a control beside a framed box (a code block) stands on the box and is not measured against its text', async ({ page }) => {
    const found = await check(
      page,
      `<div class="command"><pre class="code-block">npm config set registry https://localhost:4200/api/npm/web-frontend/npm/</pre><quader-copy-button><button type="button" class="btn btn-icon" aria-label="Copy"><span class="material-icons">content_copy</span></button></quader-copy-button></div>`,
      '.command { display: flex; align-items: flex-start; gap: 8px; } .code-block { margin: 0; padding: 12px; border: 1px solid #ccc; }',
    );
    expect(found, describe(found)).toEqual([]);
  });

  test('R-UI-175 a card\'s figure (a 32 px shield beside a text of four lines) is centred on the whole text', async ({ page }) => {
    const card = (align: string): [string, string] => [
      `<div class="card"><quader-grade-shield></quader-grade-shield><div class="body"><p>Grade E · 13 Critical</p><p>More than 5 Critical</p><p>13 · 16 · 0</p><p>Checked 2 hours ago</p></div></div>`,
      `.card { display: grid; grid-template-columns: auto minmax(0, 1fr); column-gap: 12px; align-items: ${align}; } quader-grade-shield { display: block; inline-size: 32px; block-size: 32px; background: #888; } p { margin: 0; }`,
    ];
    const ok = await check(page, ...card('center'));
    expect(ok, describe(ok)).toEqual([]);
    const off = await check(page, ...card('end'));
    expect(off.filter((f) => /^(off-line|icon-apart)$/.test(f.check)), describe(off)).toHaveLength(1);
  });

  test('R-UI-175 a column of controls between two lists is a toolbar of its own, not paired with a text further away', async ({ page }) => {
    const found = await check(
      page,
      `<div class="lists"><section><h3>Available repositories</h3><ul class="box"><li>npm-vendor</li></ul></section>
       <div class="buttons"><button type="button" class="btn">Add</button><button type="button" class="btn">Remove</button><button type="button" class="btn">Move up</button><button type="button" class="btn">Move down</button><button type="button" class="btn">Move to top</button></div>
       <section><h3>Members in resolution order</h3><ul class="box"><li>npm-internal</li></ul><p>Alt with the arrow keys moves the selected member.</p></section></div>`,
      '.lists { display: grid; grid-template-columns: 1fr auto 1fr; gap: 16px; } .buttons { display: flex; flex-direction: column; gap: 8px; } .box { border: 1px solid #ccc; min-block-size: 100px; margin: 0; } h3 { margin: 0; }',
    );
    expect(found, describe(found)).toEqual([]);
  });

  test('R-UI-175 hidden and collapsed content is not measured', async ({ page }) => {
    const found = await check(
      page,
      `<div style="visibility:hidden"><div class="r"><button type="button" class="btn">Open</button><span>name</span></div></div>
       <div style="display:none"><div class="r"><button type="button" class="btn">Open</button><span>name</span></div></div>
       <div class="r"><span class="visually-hidden">Grade E</span><button type="button" class="btn" style="margin-block:calc((21px - 32px) / 2)">Open</button></div>`,
      '.r { display: flex; align-items: flex-start; }',
    );
    expect(found, describe(found)).toEqual([]);
  });
});

// ---- The tolerance (Q-UI-722) ----------------------------------------------------------------------------------------------------

test('R-UI-175 a unit centred by the line rule is within 1 px of its text\'s middle at every text size and line the app uses', async ({ page }) => {
  // Every text size and line height of the tokens, every unit height; the unit centred as R-UI-175 asks (margins of half the
  // difference). What remains is the rounding of the font's ascent and descent to whole pixels and of the boxes to 1/64 px.
  const sizes = [12, 13, 14, 16];
  const lineHeights = ['1rem', '1.25rem', '1.5'];
  const heights = [16, 20, 24, 28, 32];
  let html = '';
  for (const s of sizes) {
    for (const l of lineHeights) {
      for (const h of heights) {
        html += `<div class="r" style="font-size:${s}px;line-height:${l}"><span class="u" style="block-size:${h}px;margin-block:calc((1lh - ${h}px) / 2)"></span><span class="t">Repository ${s} ${l} ${h}</span></div>`;
      }
    }
  }
  await check(page, html, '.r { display: flex; align-items: flex-start; gap: 8px; margin-block: 8px; } .u { flex: none; inline-size: 16px; background: #888; }');
  const offs = await page.evaluate(() =>
    Array.from(document.querySelectorAll('.r')).map((r) => {
      const u = r.querySelector('.u')!.getBoundingClientRect();
      const range = document.createRange();
      range.selectNodeContents(r.querySelector('.t')!.firstChild!);
      const t = range.getClientRects()[0]!;
      return Math.abs((u.top + u.bottom) / 2 - (t.top + t.bottom) / 2);
    }),
  );
  const worst = Math.max(...offs);
  console.log(`Q-UI-722: ${offs.length} centred units, the largest distance of middles ${worst.toFixed(3)} px`);
  expect(worst).toBeLessThanOrEqual(1);
});

test('R-UI-175 an offset of 1.5 px is found, one of 0.5 px is not', async ({ page }) => {
  const at = (shift: number): string =>
    `<div class="r"><span class="material-icons" style="font-size:16px;margin-block:calc((1lh - 16px) / 2);position:relative;top:${shift}px">lock</span><span>platform</span></div>`;
  const css = '.r { display: flex; align-items: flex-start; gap: 8px; line-height: 1rem; } .r .material-icons { line-height: 1rem; }';
  expect(only(await check(page, at(1.5), css), 'off-line')).toHaveLength(1);
  const ok = await check(page, at(0.5), css);
  expect(ok, describe(ok)).toEqual([]);
});

// ---- The shared patterns with the app's own stylesheet (R-UI-176, Q-UI-725) -------------------------------------------------------

/**
 * The patterns of the style guide that put a control or an icon beside a text, built from the global classes only and rendered with
 * web/src/styles compiled: each must stand on its line. A change of a shared rule that breaks the line fails here, without the stack
 * and before a render run. A component's own styles are not in the global stylesheet; its rows are the renders' to measure.
 */
const patterns: readonly { readonly name: string; readonly html: string }[] = [
  {
    name: "a list's row: the button that opens it beside the name, a badge, a status, a copy button after a hash, the row's menu",
    html: `<div class="quader-data-table"><div class="table-frame"><div class="table-scroll"><table class="data"><tbody><tr>
      <th scope="row"><div style="display:flex;align-items:flex-start;gap:4px"><button type="button" class="btn btn-tertiary btn-icon on-line" aria-label="Show the versions of guidedtraffic/kubectl"><span class="material-icons">expand_more</span></button><span><a href="#">guidedtraffic/kubectl</a></span></div></th>
      <td><span class="badge">disabled</span></td>
      <td><span class="ok"><span class="material-icons">check_circle</span>Active</span></td>
      <td><code class="ellipsis">sha256:25e8c833</code><quader-copy-button><button type="button" class="btn btn-icon" aria-label="Copy the digest"><span class="material-icons">content_copy</span></button></quader-copy-button></td>
      <td class="actions"><button type="button" class="btn btn-icon btn-tertiary" aria-haspopup="menu" aria-label="Actions for guidedtraffic/kubectl"><span class="material-icons">more_vert</span></button></td>
    </tr></tbody></table></div></div></div>`,
  },
  {
    name: 'a table of a settings page (a raw `table.data`): a status and a Remove button beside a name',
    html: `<table class="data"><tbody><tr><th scope="row">acme.platform</th><td><span class="ok"><span class="material-icons">check_circle</span> Completed</span></td><td><button type="button" class="btn btn-tertiary">Remove</button></td></tr></tbody></table>`,
  },
  {
    name: "an icon of a line of text: in a paragraph, in a notice, before a visibility level's name",
    html: `<p><span class="material-icons inline">lock</span>Only an administrator of the installation can switch the mirror on.</p>
      <p class="banner-warning"><span class="material-icons inline" aria-hidden="true">info</span><span>You may use this repository but not read or change its settings.</span></p>
      <p class="subject"><quader-visibility-icon><span class="material-icons inline" role="img" aria-label="Private">lock</span></quader-visibility-icon> web-frontend/npm</p>`,
  },
  {
    name: 'a badge in a heading and in running text',
    html: `<h1>Members <span class="badge heading-count">2</span></h1><p>Expires <span class="badge warn">in 2 hours</span> unless rotated.</p>`,
  },
  {
    name: 'a check box beside its label of two lines',
    html: `<label class="check-row" style="inline-size:220px"><input type="checkbox" /><span>Public issuer: any workload may ask for a token</span></label>`,
  },
  {
    name: "a link to another site with its mark on the link's last line",
    html: `<span class="external-link" style="inline-size:180px"><a href="#">https://github.com/guided-traffic/quader-images</a><span class="material-icons" aria-hidden="true">open_in_new</span></span>`,
  },
  {
    name: "a project's mark beside a name of one line, and the medium mark beside a name and its key",
    html: `<p><quader-project-mark class="quader-project-mark" data-letter="P"></quader-project-mark> Platform engineering</p>
      <div style="display:grid;grid-template-columns:auto minmax(0,1fr);column-gap:12px"><quader-project-mark class="quader-project-mark medium" data-letter="P" style="grid-row:1 / span 2;align-self:center"></quader-project-mark><span>Platform engineering</span><span style="grid-column:2;font-size:0.75rem;line-height:1rem">platform</span></div>`,
  },
  {
    // The built-in project's mark holds the icon font's arrow where another holds its letter (Q-UI-837): the same box on the line.
    name: "the built-in project upstream's mark, its arrow, beside a name of one line and beside a name and its key",
    html: `<p><quader-project-mark class="quader-project-mark upstream"></quader-project-mark> Upstream</p>
      <div style="display:grid;grid-template-columns:auto minmax(0,1fr);column-gap:12px"><quader-project-mark class="quader-project-mark medium upstream" style="grid-row:1 / span 2;align-self:center"></quader-project-mark><span>Upstream <span class="badge">Built-in</span></span><span style="grid-column:2;font-size:0.75rem;line-height:1rem">upstream</span></div>`,
  },
];

test.describe("the shared patterns with the app's stylesheet (R-UI-176)", () => {
  for (const p of patterns) {
    test(`R-UI-176 ${p.name}`, async ({ page }) => {
      const found = await check(page, p.html, '', true);
      expect(found, describe(found)).toEqual([]);
    });
  }

  // A stacked row with the app's stylesheet (Q-UI-762, Q-UI-763): an ended token's time of two lines beside its neighbours, and a
  // meter in `td.meter-cell` (the meter's own styles, usage-meter.ts, inline) with its state under the bar. The table is stacked by
  // hand, so the check that its columns would fit is left out.
  const stackedFacts = `<div class="quader-data-table stacked" style="inline-size:390px"><div class="table-frame"><div class="table-scroll"><table class="data"><tbody>
      <tr>
        <th scope="row"><span>nightly-mirror</span></th>
        <td data-label="Limit"><button type="button" class="link">Default (Technical)</button><span class="secondary">Not used in 24 h</span></td>
        <td data-label="Ended">3 days ago<br /><span class="warn"><span class="material-icons" aria-hidden="true">history</span> Expired</span></td>
        <td data-label="Last used">4 days ago</td>
        <td class="meter-cell" data-label="Now"><span style="display:flex;flex-direction:column;gap:2px"><span style="display:flex;align-items:flex-start;gap:8px"><span style="flex:1 1 4rem;max-inline-size:8rem;block-size:6px;margin-block:calc((var(--quader-line) - 6px) / 2);background:#888"></span><span>22%</span></span><span class="badge warn" style="align-self:flex-start"><span class="material-icons" aria-hidden="true">warning</span>Close to the limit</span></span></td>
        <td class="actions"><button type="button" class="btn btn-tertiary">Issue new secret</button></td>
      </tr></tbody></table></div></div></div>`;
  // A Debian version's Details in German (2026-11-24): beside the term "Suites und Komponenten" the maintainer's address had 164 px at 390 px
  // and broke inside itself. With the app's stylesheet the terms stand above their values below 36 rem of content (Q-UI-1077) and
  // beside them above it.
  const debianFacts = (px: number): string =>
    `<div style="container: content / inline-size; inline-size: ${px}px"><dl class="facts">
       <dt>Suites und Komponenten</dt><dd>stable/main</dd>
       <dt>Maintainer</dt><dd>Platform engineering &lt;platform@example.invalid&gt;</dd>
     </dl></div>`;
  test('R-UI-171 a fact list below 36 rem of content puts each term above its value, and its long value does not break inside itself', async ({ page }) => {
    const narrow = await check(page, debianFacts(326), '', true);
    expect(only(narrow, 'broken'), describe(narrow)).toEqual([]);
    const tops = (): Promise<number[]> => page.evaluate(() => ['dt', 'dd'].map((t) => Math.round(document.querySelector(`dl.facts > ${t}`)!.getBoundingClientRect().top)));
    const [dt, dd] = await tops();
    expect(dd).toBeGreaterThan(dt!);
    await check(page, debianFacts(700), '', true);
    const [wideDt, wideDd] = await tops();
    expect(wideDd).toBe(wideDt);
  });

  test("R-UI-174 a stacked row's facts with the app's stylesheet: names on one line, the meter on its name's line", async ({ page }) => {
    const found = (await check(page, stackedFacts, '', true)).filter((f) => f.check !== 'stacked-fits');
    expect(found, describe(found)).toEqual([]);
  });

  test("R-UI-174 the guard can fail: the stacked row's facts side by side and centred, and the meter as a box after its name", async ({ page }) => {
    // The form before Q-UI-1050 (Q-UI-681: as many fields to a line as fit, each its name before its value), then centred.
    const before = `.quader-data-table.stacked table.data tbody tr { align-items: center; }
      .quader-data-table.stacked table.data tbody tr > td:not(.actions) { display: block; flex: 0 1 auto; inline-size: auto; }
      .quader-data-table.stacked table.data td[data-label]::before { content: attr(data-label) ' '; display: inline; inline-size: auto; padding: 0; }
      .quader-data-table.stacked table.data td.meter-cell:not([data-empty]) { display: block; flex-basis: auto; }`;
    const found = await check(page, stackedFacts, before, true);
    expect(only(found, 'stacked-offset').length, describe(found)).toBeGreaterThan(0);
    expect(only(found, 'stacked-place').length, describe(found)).toBeGreaterThan(0);
    expect(only(found, 'label-apart'), describe(found)).toHaveLength(1);
  });

  // The admin area's repository list at 390 px (the full render run of 2026-11-19): a format, a class and a visibility level, each
  // its icon and its word, on the lines of a block.
  const levelFacts = `<div class="quader-data-table stacked" style="inline-size:358px"><div class="table-frame"><div class="table-scroll"><table class="data"><tbody>
      <tr>
        <th scope="row"><span>apt</span></th>
        <td class="format-cell" data-label="Format"><span class="format-name"><quader-format-icon><span class="material-icons" aria-hidden="true">inventory_2</span></quader-format-icon><span translate="no">Debian (APT)</span></span></td>
        <td class="class-cell" data-label="Class"><span class="class-name"><span class="material-icons" aria-hidden="true">layers</span>Virtual</span></td>
        <td class="level-cell" data-label="Visibility"><quader-visibility-icon><span class="material-icons inline" role="img" aria-label="Private">lock</span></quader-visibility-icon>Private</td>
        <td data-label="Last activity">1 hour ago</td>
      </tr></tbody></table></div></div></div>`;
  test("R-UI-174 a stacked row's format, class and visibility level with the app's stylesheet: the names on one line", async ({ page }) => {
    const found = (await check(page, levelFacts, '', true)).filter((f) => f.check !== 'stacked-fits');
    expect(found, describe(found)).toEqual([]);
  });

  // An image's platforms at 390 px: a media type of 45 characters is wider than its field's column of values and breaks there; a tag
  // of six letters that a box of its own breaks, beside the room it would need, is still found.
  const identifierFacts = (tag: string): string => `<div class="quader-data-table stacked" style="inline-size:358px"><div class="table-frame"><div class="table-scroll"><table class="data"><tbody>
      <tr>
        <th scope="row"><span>linux/amd64</span></th>
        <td class="media-type" data-label="Media type" translate="no">application/vnd.oci.image.manifest.v1+json</td>
        <td data-label="Tag" translate="no">${tag}</td>
      </tr></tbody></table></div></div></div>`;
  test("R-UI-174 an identifier longer than its field's column of values breaks there; a short one broken with room is found", async ({ page }) => {
    const css = '.quader-data-table.stacked td.media-type { white-space: normal; overflow-wrap: anywhere; }';
    const ok = (await check(page, identifierFacts('latest'), css, true)).filter((f) => f.check === 'broken');
    expect(ok, describe(ok)).toEqual([]);
    const squeezed = await check(page, identifierFacts('<span style="display:inline-block;inline-size:20px;white-space:normal;overflow-wrap:anywhere">latest</span>'), css, true);
    expect(only(squeezed, 'broken').map((f) => f.text), describe(squeezed)).toEqual(['latest']);
  });

  // A Debian repository's versions opened in its packages at 390 px in German (2026-11-24): the column of names was the list's around
  // them, 82 px for "Aktualisiert", and "Veröffentlicht" broke inside itself there. A name of two words wraps between them.
  const namedFacts = (names: string): string => `<div class="quader-data-table stacked" style="inline-size:358px;--quader-stack-names:${names}"><div class="table-frame"><div class="table-scroll"><table class="data"><tbody>
      <tr>
        <th scope="row"><span>1.2.0-1</span></th>
        <td data-label="Größe">716 B</td>
        <td data-label="Veröffentlicht"><time>vor 59 Minuten</time></td>
        <td data-label="Zuletzt verwendet"><time>gestern</time></td>
      </tr></tbody></table></div></div></div>`;
  test("R-UI-174 a column's name broken inside its word is found; one that wraps between its words passes", async ({ page }) => {
    const narrow = await check(page, namedFacts('82px'), '', true);
    expect(only(narrow, 'broken').map((f) => f.text), describe(narrow)).toEqual(['Veröffentlicht']);
    const wide = (await check(page, namedFacts('100px'), '', true)).filter((f) => f.check === 'broken');
    expect(wide, describe(wide)).toEqual([]);
  });

  // A place's usage at 390 px (2026-11-26, the caller's and the own usage page): a stacked row's name in a number's cell. `td.num` is
  // nowrap, the name inherited it and ran over its figure; it wraps in its column since then. The measurement copies the name's
  // white-space, so the defect is measured as it stood.
  const numberFacts = `<div class="quader-data-table stacked" style="inline-size:358px"><div class="table-frame"><div class="table-scroll"><table class="data"><tbody>
      <tr>
        <th scope="row"><span>Bastion edge-1</span></th>
        <td class="num" data-label="From the upstream in the last hour">1.2 GiB</td>
        <td class="num" data-label="Requests">1,204</td>
      </tr></tbody></table></div></div></div>`;
  test("R-UI-174 a stacked row's name in a number's cell wraps in its column; one that runs over its figure is found", async ({ page }) => {
    const ok = (await check(page, numberFacts, '', true)).filter((f) => f.check !== 'stacked-fits');
    expect(ok, describe(ok)).toEqual([]);
    const nowrap = await check(page, numberFacts, '.quader-data-table.stacked table.data td[data-label]::before { white-space: inherit; }', true);
    expect(only(nowrap, 'name-over').map((f) => f.text), describe(nowrap)).toEqual(['From the upstream in the last hour']);
  });

  // The help sign after a setting's name (R-UI-267), with the app's `.help-anchor` and `.help-sign`, in a box as wide as the name's
  // text on one line, so the sign has no room on that line. The anchored name takes its last word to the next line with the sign,
  // which stands on that line's middle; without the anchor the sign alone takes the next line and is found.
  const helpAfter = (anchor: boolean): string =>
    `<div id="box" style="display:inline-block"><span class="setting-name${anchor ? ' help-anchor' : ''}" style="font-weight:600">Direct reads from the object store</span><quader-help><button type="button" class="help-sign" aria-label="About Direct reads from the object store"><span class="material-icons" aria-hidden="true">info</span></button></quader-help></div>`;
  async function narrowed(page: Page): Promise<{ found: LayoutFinding[]; lines: number; off: number }> {
    const { lines, off } = await page.evaluate(() => {
      const box = document.getElementById('box')!;
      const name = box.querySelector<HTMLElement>('.setting-name')!;
      const reserved = parseFloat(getComputedStyle(name).paddingInlineEnd);
      box.style.display = 'block';
      box.style.inlineSize = `${Math.ceil(name.getBoundingClientRect().width - reserved)}px`;
      const rects = Array.from(name.getClientRects());
      const last = rects[rects.length - 1]!;
      const sign = box.querySelector('button')!.getBoundingClientRect();
      return { lines: rects.length, off: Math.abs((sign.top + sign.bottom) / 2 - (last.top + last.bottom) / 2) };
    });
    return { found: await page.evaluate(layoutFindings), lines, off };
  }

  test('R-UI-175 R-UI-267 a help sign after a name of two lines stands on the last line, on its middle', async ({ page }) => {
    await check(page, helpAfter(true), '', true);
    const anchored = await narrowed(page);
    expect(anchored.found, describe(anchored.found)).toEqual([]);
    expect(anchored.lines).toBe(2);
    expect(anchored.off).toBeLessThan(1);
    await check(page, helpAfter(false), '', true);
    const apart = await narrowed(page);
    expect(apart.lines).toBe(1);
    expect(only(apart.found, 'icon-apart'), describe(apart.found)).toHaveLength(1);
  });

  test("R-UI-176 the guard can fail: the owner's row without the line rule is found with the app's stylesheet", async ({ page }) => {
    const html = patterns[0]!.html.replace('btn btn-tertiary btn-icon on-line', 'btn btn-tertiary btn-icon');
    expect(only(await check(page, html, '', true), 'off-line')).toHaveLength(1);
  });
});

test.describe('a heading broken inside a word (R-UI-171)', () => {
  // The German heading of the authentication settings at 390 px (2026-11-24): "Authentifizierungseinstellung" / "en" in a heading of
  // 358 px at the first heading's size, as the page header sets it (`overflow-wrap: anywhere`, the last resort for a name).
  const css = `h1 { margin: 0; inline-size: 358px; font-size: 24px; font-weight: 600; line-height: 1.25; overflow-wrap: anywhere; }`;
  const lines = (page: Page): Promise<number> =>
    page.evaluate(() => {
      const range = document.createRange();
      range.selectNodeContents(document.querySelector('h1')!.firstChild!);
      return new Set(Array.from(range.getClientRects()).filter((r) => r.width > 0.5).map((r) => Math.round(r.top))).size;
    });

  test('R-UI-171 a heading broken between two letters of its word is found', async ({ page }) => {
    const found = await check(page, '<h1>Authentifizierungseinstellungen</h1>', css);
    const broken = only(found, 'heading-broken');
    expect(broken, describe(found)).toHaveLength(1);
    expect(broken[0]!.detail).toMatch(/^the word "Authentifizierungseinstellungen" broken inside itself/);
  });

  test('R-UI-171 the same word with a soft hyphen at the joint of its compound breaks there with a hyphen and passes', async ({ page }) => {
    const found = await check(page, '<h1>Authentifizierungs&#xAD;einstellungen</h1>', css);
    expect(await lines(page)).toBe(2);
    expect(only(found, 'heading-broken'), describe(found)).toEqual([]);
  });

  test('R-UI-171 a heading that wraps between its words, and a long name that breaks where it must, pass', async ({ page }) => {
    const words = await check(page, '<h1>Einstellungen zur Authentifizierung</h1>', css);
    expect(await lines(page)).toBe(2);
    expect(only(words, 'heading-broken'), describe(words)).toEqual([]);
    const name = await check(page, '<h1 translate="no">customerportalreleasedeliveryartifacts</h1>', css);
    expect(await lines(page)).toBe(2);
    expect(only(name, 'heading-broken'), describe(name)).toEqual([]);
  });
});

test.describe('a short text cramped beside a column with room to spare (R-UI-173)', () => {
  // W-5's row (2026-11-20): a version's grade, "3 findings ·" over "0 fixable", beside a column whose width was set wider than any of
  // its values at 1,024 px. The grade as quader-grade builds it: the shield, then the parts, each on one line.
  const grade = '<span class="grade"><quader-grade-shield></quader-grade-shield><span class="grade-text"><span class="grade-part">3 findings ·</span> <span class="grade-part">0 fixable</span></span></span>';
  const versions = (published: string, table = 560): string =>
    `<table class="data" style="inline-size:${table}px"><thead><tr><th scope="col">Version</th><th scope="col" class="fixed">Published</th><th scope="col">Vulnerabilities</th></tr></thead>` +
    `<tbody><tr><th scope="row">4.0.1</th><td class="fixed">${published}</td><td>${grade}</td></tr>` +
    `<tr><th scope="row">4.0.0</th><td class="fixed">2 days ago</td><td>${grade}</td></tr></tbody></table>`;
  const css = (fixed: string): string => `
    .grade { display: flex; align-items: flex-start; gap: 8px; }
    quader-grade-shield { display: inline-block; flex: none; inline-size: 20px; block-size: 20px; background: #888; }
    .grade-text { min-inline-size: 0; }
    .grade-part { white-space: nowrap; }
    th.fixed, td.fixed { inline-size: ${fixed}; }`;

  test('R-UI-173 the grade in two lines beside a fixed column that no row fills is found', async ({ page }) => {
    const found = await check(page, versions('4 minutes ago'), css('300px'));
    const cramped = only(found, 'cramped');
    expect(cramped, describe(found)).toHaveLength(1);
    expect(cramped[0]!.text).toBe('3 findings · 0 fixable');
  });

  test('R-UI-173 the room a column of relative times keeps for the widest one of the language is not room to give', async ({ page }) => {
    // The signature releases at 1,440 px (2026-11-24): "Released" kept 14 px beside its "now" for "59 minutes ago" (Q-UI-1069).
    const timed = versions('4 minutes ago').replace('<th scope="col" class="fixed">Published</th>', '<th scope="col" class="fixed time-column">Published</th>');
    const found = await check(page, timed, `${css('auto')} th.time-column { min-inline-size: 300px; }`);
    expect(only(found, 'cramped'), describe(found)).toEqual([]);
  });

  test('R-UI-173 the same grade passes where the column takes the width its values need, and stands on one line', async ({ page }) => {
    const found = await check(page, versions('4 minutes ago'), css('auto'));
    expect(only(found, 'cramped'), describe(found)).toEqual([]);
  });

  test('R-UI-173 a wrapped grade beside a column whose own text wraps has no room to take, and passes', async ({ page }) => {
    const found = await check(page, versions('A description of the version that wraps as prose does in its column', 520), css('300px'));
    expect(only(found, 'cramped'), describe(found)).toEqual([]);
  });

  test('R-UI-173 a mark laid out under its text (the session in use, a block under its time) is no wrap, and passes', async ({ page }) => {
    // The sessions at 1,440 px (2026-11-24): "This session" wrapped under the time in a column of 11 rem; laid out under it, it stays.
    const cell = (badge: string): string =>
      '<table class="data" style="inline-size:560px"><tbody><tr><th scope="row">Nov 24, 2026, 1:29:28 AM' + badge + '</th>' +
      '<td class="fixed">127.0.0.1</td></tr></tbody></table>';
    const inline = await check(page, cell(' <span class="badge">This session</span>'), `${css('300px')} th { inline-size: 11rem; }`);
    expect(only(inline, 'cramped'), describe(inline)).toHaveLength(1);
    const block = await check(page, cell('<span class="badge" style="display:flex;inline-size:fit-content">This session</span>'), `${css('300px')} th { inline-size: 11rem; }`);
    expect(only(block, 'cramped'), describe(block)).toEqual([]);
  });

  test('R-UI-173 a short value in a column of long values that wrap (an owner\'s path) wraps as they do, and passes', async ({ page }) => {
    const owners = (long: string): string =>
      '<table class="data" style="inline-size:560px"><tbody>' +
      '<tr><td class="owner">sa:web-frontend/release-pipeline</td><td class="fixed">in 4 weeks</td></tr>' +
      `<tr><td class="owner">${long}</td><td class="fixed">in 2 months</td></tr></tbody></table>`;
    const cssOwners = `${css('300px')} td.owner { inline-size: 9rem; }`;
    const shortOnly = await check(page, owners('sa:backup-reader'), cssOwners);
    expect(only(shortOnly, 'cramped'), describe(shortOnly)).toHaveLength(1);
    const withLong = await check(page, owners('sa:web-frontend/npm-internal/frontend-release-pipeline-of-the-customer-portal-team'), cssOwners);
    expect(only(withLong, 'cramped'), describe(withLong)).toEqual([]);
  });

  test('R-UI-173 a text longer than a summary (a sentence) wrapped beside the same column is not cramped', async ({ page }) => {
    const long = '<table class="data" style="inline-size:560px"><tbody><tr><td class="fixed">2 days ago</td>' +
      '<td>A long description of the version that wraps as prose does</td></tr></tbody></table>';
    const found = await check(page, long, css('300px'));
    expect(only(found, 'cramped'), describe(found)).toEqual([]);
  });
});

test.describe("the trend's axis with the app's stylesheet (R-VULN-022)", () => {
  // The vulnerabilities' trend as report-trend.ts builds it: its axis's numbers inside the SVG scaled with the viewBox, to about 6 px at
  // 390 px in every engine (W15, 2026-11-24). As text of the page they keep the small text size at every width, each on its grid line.
  const figure = (width: number): string =>
    `<div style="inline-size:${width}px"><div class="trend-figure"><div class="trend-axis" aria-hidden="true"><span class="trend-axis-sizer">2,000</span>` +
    [8, 92, 176].map((y, i) => `<span class="trend-tick" style="top:${(y / 200) * 100}%">${['2,000', '1,000', '0'][i]}</span>`).join('') +
    `</div><svg class="trend-chart" viewBox="40 0 596 200">` +
    [8, 92, 176].map((y) => `<line class="trend-grid" x1="40" x2="636" y1="${y}" y2="${y}" />`).join('') +
    `</svg></div></div>`;

  for (const width of [358, 1100]) {
    test(`R-VULN-022 the axis's numbers keep the small text size and stand on their grid lines in a chart ${width} px wide`, async ({ page }) => {
      await check(page, figure(width), '', true);
      const m = await page.evaluate(() => {
        const ticks = [...document.querySelectorAll<HTMLElement>('.trend-tick')];
        const lines = [...document.querySelectorAll('line.trend-grid')];
        const range = document.createRange();
        return ticks.map((t, i) => {
          range.selectNodeContents(t.firstChild!);
          const glyphs = range.getBoundingClientRect();
          const line = lines[i]!.getBoundingClientRect();
          return { size: parseFloat(getComputedStyle(t).fontSize), height: glyphs.height, off: (glyphs.top + glyphs.bottom) / 2 - (line.top + line.bottom) / 2, svgLeft: document.querySelector('svg')!.getBoundingClientRect().left, right: glyphs.right };
        });
      });
      for (const t of m) {
        expect(t.size).toBe(12);
        expect(t.height).toBeGreaterThanOrEqual(12);
        expect(Math.abs(t.off)).toBeLessThanOrEqual(1);
        expect(t.right).toBeLessThan(t.svgLeft);
      }
    });
  }
});

test.describe('a tab strip wider than its place (R-UI-173)', () => {
  // A version's tabs in German at 390 px (2026-11-24, `package-version-oci-not-checked`): Dateien, Signaturen, Details,
  // Schwachstellen in a strip of 358 px.
  const strip = (cls: string): string =>
    `<div style="inline-size:358px"><nav class="${cls}" aria-label="Ansichten">` +
    ['Dateien', 'Signaturen', 'Details', 'Schwachstellen'].map((t, i) => `<a class="tab-link" href="#"${i === 0 ? ' aria-current="page"' : ''}>${t}</a>`).join('') +
    '</nav></div>';
  // A scrollbar that takes room, where an engine draws a styled one (WebKit): the fixtures' Chromium hides its scrollbars (the
  // renders' does not) and Firefox overlays them, so there the guard has nothing to find.
  const classic = '::-webkit-scrollbar { height: 12px; background: #ccc; } ::-webkit-scrollbar-thumb { background: #888; }';

  test("R-UI-173 the app's strip scrolls its tabs without a scrollbar", async ({ page }) => {
    const found = await check(page, strip('tab-nav'), classic, true);
    expect(only(found, 'tab-scrollbar'), describe(found)).toEqual([]);
    expect(await page.locator('.tab-nav').evaluate((el) => el.scrollWidth > el.clientWidth)).toBe(true);
  });

  test('R-UI-173 the guard can fail: a strip that scrolls with the scrollbar of an engine that lays one out is found', async ({ page }) => {
    const css = `.bare-strip { display: flex; overflow-x: auto; } .bare-strip > a { flex: none; padding: 16px 12px; } ${classic}`;
    const found = await check(page, strip('bare-strip').replace('<nav ', '<nav role="tablist" '), css, true);
    const laidOut = await page.locator('.bare-strip').evaluate((el: HTMLElement) => el.offsetHeight - el.clientHeight);
    expect(only(found, 'tab-scrollbar'), describe(found)).toHaveLength(laidOut >= 1 ? 1 : 0);
  });
});

test.describe('a list\'s cell without a value (R-UI-146)', () => {
  // The start page's projects as an administrator who is a member of none (2026-11-23, `projects-details-details-dark-1440.png`):
  // **Your role** empty in every row, beside the dashes of the other columns.
  const projects = (role: string): string =>
    `<table class="data"><thead><tr>
       <th scope="col"><span class="th-inner"><span>Project</span></span></th>
       <th scope="col"><span class="th-inner"><span>Your role</span></span></th>
       <th scope="col"><span class="th-inner"><span>Owners</span></span></th>
       <th scope="col"><span class="th-inner"><span class="visually-hidden">Actions</span></span></th>
     </tr></thead><tbody><tr>
       <th scope="row"><a href="#">Web frontend</a></th>
       <td>${role}</td>
       <td><quader-no-value><span aria-hidden="true">–</span><span class="visually-hidden">None</span></quader-no-value></td>
       <td class="actions"></td>
     </tr></tbody></table>`;

  test('R-UI-146 an administrator without membership: an empty role cell is found, the dash passes, an empty action cell is no finding', async ({ page }) => {
    const empty = await check(page, projects(''));
    expect(only(empty, 'empty-cell'), describe(empty)).toHaveLength(1);
    expect(only(empty, 'empty-cell')[0]!.detail).toContain('Your role');
    const dash = await check(page, projects('<quader-no-value><span aria-hidden="true">–</span><span class="visually-hidden">None</span></quader-no-value>'));
    expect(only(dash, 'empty-cell'), describe(dash)).toEqual([]);
  });
});

test.describe('the bound of an allowed finding', () => {
  const region = (px: string): LayoutFinding => ({ check: 'sideways', what: 'pre.code-block', text: '', detail: `the region scrolls ${px} px sideways`, count: 1 });

  test('a code block that scrolls a long line is allowed, and one wider than a few screens fails its screen', () => {
    expect(judge('repository-settings-npm-local', [region('1240')]).defects).toEqual([]);
    expect(judge('repository-settings-npm-local', [region(String(CODE_BLOCK_SCROLL))]).defects).toEqual([]);
    // The Debian remote's stored document of 2026-11-20 to 2026-11-23: a keyring of 77,000 characters on one line.
    expect(judge('repository-settings-deb-remote', [region('602311')]).defects).toHaveLength(1);
    expect(judge('repository-settings-deb-remote', [region('4,001')]).defects).toHaveLength(1);
  });

  test('an allowed finding is matched by its place in every language, and the same words elsewhere stay a defect', () => {
    const key = 'release-candidates-for-the-customer-portal-delivery';
    const button = (what: string, text: string): LayoutFinding => ({ check: 'broken', what, text, detail: 'a button in 2 lines, 326 px wide', count: 1 });
    const inGroup = 'section > div.selector-buttons > button.btn.btn-default';
    expect(judge('policy-new', [button(inGroup, `Add selector ${key}`)]).defects).toEqual([]);
    expect(judge('policy-new', [button(inGroup, `Selektor hinzufügen ${key}`)]).defects).toEqual([]);
    expect(judge('policy-new', [button('div.save-bar > button.btn.btn-confirm', `Add selector ${key}`)]).defects).toHaveLength(1);
  });
});

test.describe('an English sentence in a German page (R-UI-214)', () => {
  // The trend's basis as it stood in the German dashboard on 2026-11-24, a server's sentence bound as it came.
  const basis = 'A finding counts under its current severity, from the day it was detected until the day it resolved.';

  test('R-UI-214 the server sentence of the German dashboard is found', async ({ page }) => {
    const found = only(await check(page, `<p class="hint">${basis}</p>`, '', false, 'de'), 'english');
    expect(found.map((f) => f.what), describe(found)).toEqual(['main.content > p.hint']);
  });

  test('R-UI-214 German text, code, a technical detail, a name and the same sentence in an English page pass', async ({ page }) => {
    const german =
      '<p>Ein Befund zählt unter seinem aktuellen Schweregrad, vom Tag seiner Erkennung bis zum Tag seiner Behebung.</p>' +
      `<pre><code>${basis}</code></pre>` +
      `<p lang="en" translate="no">${basis}</p>` +
      `<span translate="no">${basis}</span>` +
      '<p>Die Version 1.4.0 von left-pad liegt im Repository npm-remote.</p>';
    expect(describe(only(await check(page, german, '', false, 'de'), 'english'))).toBe('');
    expect(describe(only(await check(page, `<p class="hint">${basis}</p>`), 'english'))).toBe('');
  });
});

test.describe('an overlay inside the window, and its content inside the overlay (R-UI-173)', () => {
  /** The overlay container as the CDK writes it, with one pane placed by `paneCss`. */
  const overlay = (paneCss: string, content: string): string =>
    `<p>The page</p><div class="cdk-overlay-container" style="position: fixed; inset: 0; z-index: 1000">` +
    `<div class="cdk-overlay-pane" style="position: absolute; box-sizing: border-box; background: #fff; ${paneCss}">${content}</div></div>`;
  /**
   * W-12 (2026-11-18): the owner filter of the tokens, a drawer of 21 rem, its field's list of matches 24 rem wide in the top layer.
   * The list is placed under its field by the page, as the person-search field places it (`placeList`), at `width` or the field's.
   */
  const drawer = (width: string | null, right = 0): string =>
    overlay(
      `top: 0; right: ${right}px; inline-size: 21rem; block-size: 100%`,
      `<div role="dialog" aria-label="Filters" style="display: flex; flex-direction: column; block-size: 100%">
         <h2 style="margin: 0; padding: 12px 24px">Filters</h2>
         <div class="body" style="flex: 1; overflow: auto; padding: 8px 24px">
           <label for="owner">Owner</label>
           <input id="owner" class="text-input" style="box-sizing: border-box; display: block; inline-size: 100%" value="pri">
           <div class="popup" popover="manual" style="position: fixed; inset: auto; margin: 0; padding: 4px; border: 1px solid #888; box-sizing: border-box">
             <div role="listbox" aria-label="Matches"><div role="option">Priya Raman-Oberhauser</div><div role="option">platform-release-bot</div></div>
           </div>
         </div>
       </div>
       <script>{
         // A block: the page's global scope outlives setContent, and a second fixture would declare the names again.
         const field = document.getElementById('owner').getBoundingClientRect();
         const list = document.querySelector('.popup');
         list.style.left = field.left + 'px';
         list.style.top = field.bottom + 4 + 'px';
         list.style.inlineSize = ${width === null ? "field.width + 'px'" : `'${width}'`};
         list.showPopover();
       }</script>`,
    );

  test("R-UI-173 W-12: a person-search list 24 rem wide in a drawer of 21 rem is found past the drawer's edge", async ({ page }) => {
    // At the window's right edge, as the drawer stands, the list reaches past the window too; the finding names the first measured.
    const found = only(await check(page, drawer('24rem')), 'overlay-out');
    expect(found.map((f) => `${f.what}: ${f.detail}`), describe(found)).toEqual([expect.stringMatching(/div\.popup: 72 px past the window's right edge/)]);
    // Away from the window's edge, the drawer's edge alone.
    const inside = only(await check(page, drawer('24rem', 200)), 'overlay-out');
    expect(inside.map((f) => `${f.what}: ${f.detail}`), describe(inside)).toEqual([expect.stringMatching(/div\.popup: 72 px past the right edge of the div\.cdk-overlay-pane it stands in$/)]);
  });

  test("R-UI-173 the same list as wide as its field stays inside the drawer and passes", async ({ page }) => {
    const found = await check(page, drawer(null));
    expect(only(found, 'overlay-out'), describe(found)).toEqual([]);
  });

  /** A row's menu of 160 px hung from its button at x `left` in a phone's window. */
  const menu = (left: number): string =>
    overlay(
      `top: 120px; left: ${left}px; inline-size: 160px; padding: 4px 0`,
      '<div role="menu" aria-label="Actions for npm-internal"><div role="menuitem">Settings</div><div role="menuitem">Move to another project</div></div>',
    );

  test("R-UI-173 a menu of 160 px at x 300 in a window of 390 px is found past the window's edge; flipped to its button's right end it passes", async ({ page }) => {
    await page.setViewportSize({ width: 390, height: 844 });
    const out = only(await check(page, menu(300)), 'overlay-out');
    expect(out.map((f) => f.detail), describe(out)).toEqual([expect.stringMatching(/^70 px past the window's right edge \(the window 390 x 844 px\)$/)]);
    const flipped = await check(page, menu(214));
    expect(only(flipped, 'overlay-out'), describe(flipped)).toEqual([]);
  });

  /** A dialog of 36 rem whose body scrolls and holds `body`. */
  const dialog = (body: string): string =>
    overlay(
      'top: 40px; left: 50%; transform: translateX(-50%); inline-size: min(36rem, calc(100vw - 2rem)); max-block-size: calc(100vh - 80px); display: flex',
      `<div role="dialog" aria-label="Rules" style="display: flex; flex-direction: column; min-inline-size: 0; inline-size: 100%">
         <h2 style="margin: 0; padding: 24px 24px 8px">Rules</h2><div class="dialog-body" style="overflow: auto; padding: 8px 24px">${body}</div></div>`,
    );
  const wideTable =
    '<table style="inline-size: 900px"><tbody><tr><th scope="row">release-candidates</th><td>Allow</td><td>web-frontend/npm-internal</td></tr></tbody></table>';

  test("R-UI-173 a table of 900 px in a dialog's body scrolls sideways at 1,024 px and is found; at 390 px a region may scroll", async ({ page }) => {
    await page.setViewportSize({ width: 1024, height: 768 });
    const wide = only(await check(page, dialog(wideTable)), 'sideways');
    expect(wide.map((f) => `${f.what}: ${f.detail}`), describe(wide)).toEqual([expect.stringMatching(/^div\.dialog-body: the region scrolls \d+ px sideways in an overlay$/)]);
    await page.setViewportSize({ width: 390, height: 844 });
    const phone = await check(page, dialog(wideTable));
    expect(phone.filter((f) => /^(sideways|overlay-out)$/.test(f.check)), describe(phone)).toEqual([]);
  });

  test('R-UI-173 a code block that scrolls a long command in a dialog is allowed as in a page', async ({ page }) => {
    await page.setViewportSize({ width: 1024, height: 768 });
    const command = `<pre class="code-block" style="overflow-x: auto">docker pull localhost:4200/web-frontend/images/${'portal-'.repeat(30)}api:1.4.0</pre>`;
    const found = only(await check(page, dialog(command)), 'sideways');
    expect(found, describe(found)).toHaveLength(1);
    expect(judge('repository-settings-oci-local', found).defects).toEqual([]);
  });
});

test.describe('a text cut by the box that holds it (R-UI-173)', () => {
  /**
   * A version's facts as one line of `.dot-separated` (_controls.scss) in a column of 220 px: the version, then when it was published,
   * a part that wraps inside itself. Before 3d3fd6618 (2026-11-19) a part had no `padding-inline-start` and no `text-indent`: its
   * second line started under the clip that hides each line's first dot, and lost its first letters ("ov 18, 2026").
   */
  const facts =
    '<div style="inline-size: 220px"><span class="dot-separated"><span translate="no">4.0.1</span><span>published 4 minutes ago (Nov 18, 2026, 1:29:28 PM)</span></span></div>';
  const before = '.dot-separated > * { padding-inline-start: 0; text-indent: 0; }';

  test("R-UI-173 a part's second line under the clip of its line of facts is found", async ({ page }) => {
    const found = only(await check(page, facts, before, true), 'cut');
    expect(found.map((f) => f.detail), describe(found)).toEqual([expect.stringMatching(/^a line of its text starts \d+ px left of where span\.dot-separated shows it, and loses its first letters$/)]);
  });

  test("R-UI-173 with the app's stylesheet the part's lines start where the clip shows them, and pass", async ({ page }) => {
    const found = await check(page, facts, '', true);
    expect(only(found, 'cut'), describe(found)).toEqual([]);
  });

  test('R-UI-173 a text cut with an ellipsis (clipped is its check) and a text in a region that scrolls are not cut', async ({ page }) => {
    const long = 'sha256:b2b6838dabe5159c6022b1f94626ab13d2615ce27c554a7b94b7cc3d4b20cf0a';
    const found = await check(
      page,
      `<div style="inline-size: 200px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap">${long}</div>` +
        `<div style="inline-size: 200px; overflow-x: auto; white-space: nowrap">${long}</div>`,
    );
    expect(only(found, 'cut'), describe(found)).toEqual([]);
    const bare = await check(page, `<div style="inline-size: 200px; overflow: hidden; white-space: nowrap">${long}</div>`);
    expect(only(bare, 'cut'), describe(bare)).toHaveLength(1);
  });
});

test.describe('a separator drawn by CSS at the start of a line (R-UI-171)', () => {
  /**
   * A malware find's place and time as before 4c4486c62 (2026-11-23): the repositories, then "· found 1 hour ago" with the dot of
   * `.dot-before`, in a cell as wide as the repositories and a little more, so that the time wrapped and its dot started the line.
   */
  const find = (room: string): string =>
    `<div class="cell"><span class="find-where"><a href="#" translate="no" style="white-space: nowrap">web-frontend/assets-quarantine</a>` +
    ` <span class="dot-before">found <span>1 hour ago</span></span></span></div>` +
    `<script>{ const a = document.querySelector('.cell a').getBoundingClientRect(); document.querySelector('.cell').style.inlineSize = ${room}; }</script>`;

  test('R-UI-171 the dot before "found 1 hour ago" at the start of the wrapped line is found', async ({ page }) => {
    const found = only(await check(page, find("a.width + 30 + 'px'"), '', true), 'separator-start');
    expect(found.map((f) => f.detail), describe(found)).toEqual([expect.stringMatching(/^the separator "·" starts a line at y \d+; what it follows \("web-frontend\/assets-quarantine"\) ends on the line at y \d+$/)]);
  });

  test('R-UI-171 the same find on one line, the place and the time a line each, and a wrapped line of facts whose dots the clip hides pass', async ({ page }) => {
    const wide = await check(page, find("'600px'"), '', true);
    expect(only(wide, 'separator-start'), describe(wide)).toEqual([]);
    const lines = await check(
      page,
      '<div style="inline-size: 240px"><span style="display: block"><a href="#">web-frontend/assets-quarantine</a></span><span style="display: block">found 1 hour ago</span></div>',
      '',
      true,
    );
    expect(only(lines, 'separator-start'), describe(lines)).toEqual([]);
    const facts = await check(
      page,
      '<div style="inline-size: 160px"><span class="dot-separated"><span>4.0.1</span><span>web-frontend</span><span>npm-internal</span><span>4 minutes ago</span></span></div>',
      '',
      true,
    );
    expect(only(facts, 'separator-start'), describe(facts)).toEqual([]);
    // The fixture wraps: three lines of facts, so their first dots are those the clip hides.
    expect(await page.locator('.dot-separated').evaluate((el) => el.getBoundingClientRect().height)).toBeGreaterThan(40);
  });
});

test.describe('one scrollbar per page (R-UI-215)', () => {
  /** A page whose `main.content` is the shell's panel, the one thing that scrolls, 600 px high; measured by `innerScroll`. */
  async function scrolls(page: Page, body: string, css = '', withShared = false): Promise<LayoutFinding[]> {
    await page.setContent(
      `<!doctype html><html lang="en"><head><style>${withShared ? shared() : base}</style><style>${fonts}${css}</style>` +
        `<style>html, body { block-size: 100%; margin: 0; } main.content { position: relative; block-size: 600px; overflow: auto; padding: 24px; box-sizing: border-box; }</style></head>` +
        `<body><main class="content">${body}</main></body></html>`,
    );
    await page.evaluate(() => document.fonts.ready);
    return page.evaluate(innerScroll);
  }
  const rows = (n: number, cell = (i: number): string => `<th scope="row">image-${i}</th><td>${i} tags</td>`): string =>
    Array.from({ length: n }, (_, i) => `<tr>${cell(i)}</tr>`).join('');
  /**
   * A list as `<quader-data-table>` writes it, with a row opened under the first that holds a second list of 30 tags; `capped`, each
   * list's room has the height cap and the scroll the data table had before R-UI-215.
   */
  const owners = (capped: boolean): string => {
    const room = capped ? ' style="max-block-size: 400px; overflow: auto"' : '';
    return `<div class="quader-data-table"><div class="table-frame"><div class="table-scroll"${room}><table class="data">
       <thead><tr><th scope="col">Image</th><th scope="col">Tags</th></tr></thead>
       <tbody><tr><th scope="row">chart-render-kit</th><td>103 tags</td></tr>
       <tr class="expansion"><td colspan="2"><div class="quader-data-table"><div class="table-frame"><div class="table-scroll"${room}><table class="data">
         <thead><tr><th scope="col">Tag</th><th scope="col">Size</th></tr></thead><tbody>${rows(30, (i) => `<th scope="row">1.${i}.0</th><td>8.7 MiB</td>`)}</tbody></table></div></div></div></td></tr>
       ${rows(30)}</tbody></table></div></div></div>`;
  };

  test("R-UI-215 the owner's picture: a list's own region and the opened image's region in it are found, two scrollbars beside the page's", async ({ page }) => {
    const found = await scrolls(page, owners(true));
    expect(found.map((f) => f.detail.replace(/\d[\d,]*/g, 'N')), describe(found)).toEqual([
      'scrolls N px of its own in the page (N px high, no frame); the page scrolls instead',
      'scrolls N px inside div.table-scroll, which scrolls too',
    ]);
  });

  test("R-UI-215 the same lists with the app's stylesheet scroll with the page: nothing scrolls by itself", async ({ page }) => {
    const found = await scrolls(page, owners(false), '', true);
    expect(found, describe(found)).toEqual([]);
    // The page holds them and scrolls: the one scrollbar.
    expect(await page.evaluate(() => document.querySelector('main')!.scrollHeight > document.querySelector('main')!.clientHeight)).toBe(true);
  });

  const boxed = (style: string): string =>
    `<div class="box" tabindex="0" role="region" aria-label="Points" style="overflow-y: auto; ${style}"><table class="data"><tbody>${rows(40)}</tbody></table></div>`;
  test('R-UI-215 a framed box of 18 rem passes; at 30 rem, without its frame or inside another box that scrolls it is found', async ({ page }) => {
    expect(await scrolls(page, boxed('max-block-size: 18rem; border: 1px solid #ccc'))).toEqual([]);
    const tall = await scrolls(page, boxed('max-block-size: 30rem; border: 1px solid #ccc'));
    expect(tall.map((f) => f.detail), describe(tall)).toEqual([expect.stringMatching(/framed, above the 384 px of a boxed list/)]);
    const bare = await scrolls(page, boxed('max-block-size: 18rem'));
    expect(bare.map((f) => f.detail), describe(bare)).toEqual([expect.stringMatching(/no frame/)]);
    const nested = await scrolls(
      page,
      `<div class="outer" style="max-block-size: 20rem; overflow: auto; border: 1px solid #ccc">${boxed('max-block-size: 18rem; border: 1px solid #ccc')}<p style="block-size: 30rem">More</p></div>`,
    );
    expect(nested.map((f) => f.detail), describe(nested)).toEqual([expect.stringMatching(/inside div\.outer, which scrolls too/)]);
  });

  const column = (inner: string): string =>
    `<div style="display: grid; grid-template-columns: 15rem 1fr; gap: 32px; align-items: start">
       <div class="side" style="position: sticky; top: 0; max-block-size: 30rem; overflow-y: auto">${inner}</div>
       <article style="block-size: 3000px">The page</article>
     </div>`;
  const entries = Array.from({ length: 60 }, (_, i) => `<li><a href="#">Entry ${i}</a></li>`).join('');
  test('R-UI-215 a navigation column that sticks beside the page keeps its scroll; a side column that is no navigation is found', async ({ page }) => {
    expect(await scrolls(page, column(`<nav aria-label="Contents"><ul>${entries}</ul></nav>`))).toEqual([]);
    expect(await scrolls(page, column(`<ul role="tree" aria-label="Documentation">${entries}</ul>`))).toEqual([]);
    const preview = await scrolls(page, column(`<h2>Preview</h2><ul>${entries}</ul>`));
    expect(preview.map((f) => f.what), describe(preview)).toEqual([expect.stringMatching(/div\.side$/)]);
  });

  test('R-UI-215 an overlay keeps one scroll; a block that scrolls inside it is found', async ({ page }) => {
    const lines = Array.from({ length: 40 }, (_, i) => `line ${i}`).join('\n');
    const dialog = (pre: string): string =>
      `<p>The page</p><div class="cdk-overlay-container" style="position: fixed; inset: 0"><div class="cdk-overlay-pane" style="position: absolute; inset: 40px">
         <div class="dialog-body" style="max-block-size: 300px; overflow: auto"><pre style="overflow: auto; ${pre}">${lines}</pre><p style="block-size: 400px">More</p></div>
       </div></div>`;
    expect(await scrolls(page, dialog(''))).toEqual([]);
    const nested = await scrolls(page, dialog('max-block-size: 14rem'));
    expect(nested.map((f) => f.detail), describe(nested)).toEqual([expect.stringMatching(/^scrolls \d+ px inside div\.dialog-body, which scrolls too$/)]);
  });

  test('R-UI-215 a text area with a long value is a field and no finding', async ({ page }) => {
    const found = await scrolls(page, `<textarea rows="4" aria-label="Keys">${Array.from({ length: 80 }, (_, i) => `key ${i}`).join('\n')}</textarea>`);
    expect(found, describe(found)).toEqual([]);
  });
});

test.describe('a page that loads has its spinner in the middle and does not scroll (R-UI-252)', () => {
  /** The shell's panel, 600 px high, with the app's own stylesheet: the rule that sets a page's load into the panel is under test. */
  async function loads(page: Page, body: string): Promise<LayoutFinding[]> {
    await page.setViewportSize({ width: 1024, height: 700 });
    await page.setContent(
      `<!doctype html><html lang="en"><head><style>${shared()}</style><style>${fonts}</style>` +
        `<style>html, body { block-size: 100%; margin: 0; } main.content { position: relative; display: flex; flex-direction: column; ` +
        `block-size: 600px; overflow: auto; padding: 24px 32px; box-sizing: border-box; } .quader-loading { animation: none !important; }</style></head>` +
        `<body><main class="content">${body}</main></body></html>`,
    );
    await page.evaluate(() => document.fonts.ready);
    return page.evaluate(loadingPlace);
  }
  const load = (cls = 'quader-loading quader-loading-page'): string =>
    `<quader-loading class="${cls}" role="status"><span class="quader-spinner quader-spinner-page"><svg viewBox="0 0 64 64"></svg></span>` +
    `<span class="quader-loading-text">Loading your account.</span></quader-loading>`;
  const screen = (inner: string): string =>
    `<quader-profile><h1>Profile</h1><nav class="tab-nav" style="block-size: 40px">Tabs</nav>${inner}</quader-profile><quader-legal-footer>Imprint</quader-legal-footer>`;

  test('R-UI-252 a page whose whole content loads has its spinner in the middle of what stands below its tabs, and nothing scrolls', async ({ page }) => {
    const found = await loads(page, screen(load()));
    expect(found, describe(found)).toEqual([]);
    // An error that stays above while the page tries again moves the middle down with it.
    const failed = await loads(page, screen(`<div data-testid="error-notice" style="block-size: 120px">Unavailable</div>${load()}`));
    expect(failed, describe(failed)).toEqual([]);
    // A view inside its page's host (a package's page), and a screen routed inside a frame, take the same place.
    const view = await loads(page, screen(`<quader-package-detail-view>${load()}</quader-package-detail-view>`));
    expect(view, describe(view)).toEqual([]);
  });

  test("R-UI-252 a page's load at the top left, as before, is found; so is a page that scrolls while it loads", async ({ page }) => {
    const top = await loads(page, screen(load('quader-loading quader-loading-page" style="flex: none; align-items: flex-start')));
    expect(top.map((f) => f.check), describe(top)).toEqual(['loading-place']);
    // Set into the panel's end as one try did (2026-11-26): its own height, at the bottom over the legal line.
    const low = await loads(page, screen(load('quader-loading quader-loading-page" style="position: absolute; inset-inline: 0; inset-block-end: 0')));
    expect(low.map((f) => f.check), describe(low)).toEqual(['loading-place']);
    const tall = await loads(page, screen(`${load()}<div style="block-size: 2000px"></div>`));
    expect(tall.map((f) => f.check), describe(tall)).toContain('loading-scroll');
    // An error above the load that is taller than the panel (a German notice at 390 px) scrolls for itself, not for the load.
    const long = await loads(page, screen(`<div data-testid="error-notice" style="block-size: 700px">Unavailable</div>${load()}`));
    expect(long.map((f) => f.check), describe(long)).not.toContain('loading-scroll');
  });

  test('R-UI-252 the first paint stands in the middle of the window, and a list that loads in its table is no page load', async ({ page }) => {
    await page.setViewportSize({ width: 1024, height: 700 });
    await page.setContent(
      `<!doctype html><html lang="en"><head><style>${shared()}</style><style>${fonts} .quader-loading { animation: none !important; }</style></head>` +
        `<body>${load('quader-boot quader-loading quader-loading-page')}</body></html>`,
    );
    const boot = await page.evaluate(loadingPlace);
    expect(boot, describe(boot)).toEqual([]);
    const table = await loads(page, screen(`<quader-data-table>${load('quader-loading quader-loading-page table-status')}</quader-data-table>`));
    expect(table, describe(table)).toEqual([]);
  });
});
