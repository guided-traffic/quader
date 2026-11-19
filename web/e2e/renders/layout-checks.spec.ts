import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import { Page, expect, test } from '@playwright/test';
import { compile } from 'sass';
import { LayoutFinding, layoutFindings } from './layout';
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

async function check(page: Page, body: string, css = '', withShared = false): Promise<LayoutFinding[]> {
  await page.setContent(
    `<!doctype html><html lang="en"><head><style>${withShared ? shared() : base}</style><style>${fonts}${css}</style></head>` +
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

test('Q-UI-722 a unit centred by the line rule is within 1 px of its text\'s middle at every text size and line the app uses', async ({ page }) => {
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

test('Q-UI-722 an offset of 1.5 px is found, one of 0.5 px is not', async ({ page }) => {
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
];

test.describe("the shared patterns with the app's stylesheet (R-UI-176)", () => {
  for (const p of patterns) {
    test(`R-UI-176 ${p.name}`, async ({ page }) => {
      const found = await check(page, p.html, '', true);
      expect(found, describe(found)).toEqual([]);
    });
  }

  test("R-UI-176 the guard can fail: the owner's row without the line rule is found with the app's stylesheet", async ({ page }) => {
    const html = patterns[0]!.html.replace('btn btn-tertiary btn-icon on-line', 'btn btn-tertiary btn-icon');
    expect(only(await check(page, html, '', true), 'off-line')).toHaveLength(1);
  });
});
