// Generates Quader's brand files (run: node scripts/brand-logo.mjs in web/):
//   public/brand/quader-{mark,horizontal,square}.svg          Quader, the product
//   public/brand/quader-bastion-{mark,horizontal,square}.svg  Quader Bastion, the internet-facing cache
//   public/favicon.svg, public/favicon.ico                     the wall for the main installation's browser tab
//   src/bastion/public/favicon.svg, src/bastion/public/favicon.ico
//                                                              the tower for the Bastion's browser tab (its build alone)
//   src/app/shared/brand/mark-geometry.ts                      the wall, its lockup and the tower for the inline mark
// Every file of public/brand that this run does not write is removed, so the directory holds exactly the set above.
//
// Both marks are stones on a 64 grid with a margin of 4: every stone is 16 high, the joints are 4, the corners are rounded
// by 1.5. At 16 px a stone is 4 px and a joint 1 px, so at every multiple of 16 px each edge falls on a whole pixel. The
// coordinates are the owner's (2026-11-17) and are not tuned here.
//   Quader: a wall of three courses, the right stone of the middle course coloured.
//   Quader Bastion: a tower of three courses, the battlements with the middle merlon coloured, the overhanging crown and
//   the shaft.
//
// The wordmark is outlined paths of Inter (no <text>, so it looks the same on every platform): "Quader" in Bold, and in
// the Bastion's lockup "Bastion" after it in Regular. The favicon's ICO frames are rasterised by this script itself, so
// no image tool is needed and the bytes are deterministic.
import { Buffer } from 'node:buffer';
import { mkdirSync, readdirSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { createRequire } from 'node:module';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { crc32, deflateSync } from 'node:zlib';
import opentype from 'opentype.js';
import * as prettier from 'prettier';

const webDir = join(fileURLToPath(new URL('.', import.meta.url)), '..');
const publicDir = join(webDir, 'public');
const outDir = join(publicDir, 'brand');
const bastionPublicDir = join(webDir, 'src', 'bastion', 'public');
const geometryFile = join(webDir, 'src', 'app', 'shared', 'brand', 'mark-geometry.ts');

const r = (n) => Math.round(n * 100) / 100;

// ---- The marks ---------------------------------------------------------------------------------

/** The side of the mark's square box. */
const GRID = 64;
/** The corner radius of every stone. */
const RADIUS = 1.5;

/** A stone as [x, y, width, height] on the 64 grid. */
const MARKS = {
  quader: {
    name: 'Quader',
    stones: [
      [4, 4, 36, 16],
      [44, 4, 16, 16],
      [4, 24, 16, 16],
      [4, 44, 36, 16],
      [44, 44, 16, 16],
    ],
    coloured: [24, 24, 36, 16],
  },
  bastion: {
    name: 'Quader Bastion',
    stones: [
      [4, 4, 12, 16],
      [48, 4, 12, 16],
      [4, 24, 24, 16],
      [32, 24, 28, 16],
      [12, 44, 24, 16],
      [40, 44, 12, 16],
    ],
    coloured: [24, 4, 16, 16],
  },
};

// ---- Colours -----------------------------------------------------------------------------------

/** The stones (and the wordmark) and the coloured stone, on a light and on a dark ground (owner, 2026-11-17). */
const COLOURS = {
  light: { stone: '#28272d', coloured: '#b4690e' },
  dark: { stone: '#ececef', coloured: '#e9a13b' },
};
/** The dark rounded tile of the square forms and of the ICO: the dark scheme's page colour. */
const TILE = '#18171d';

const rules = (c) => `.s{fill:${c.stone}}.c{fill:${c.coloured}}`;

/** A style block: one scheme fixed, or both following prefers-color-scheme. */
function style(scheme) {
  if (scheme === 'auto') {
    return `<style>${rules(COLOURS.light)}@media (prefers-color-scheme: dark){${rules(COLOURS.dark)}}</style>`;
  }
  return `<style>${rules(COLOURS[scheme])}</style>`;
}

const rect = ([x, y, w, h], cls) =>
  `<rect class="${cls}" x="${x}" y="${y}" width="${w}" height="${h}" rx="${RADIUS}"/>`;

/** The mark's stones in its own 64 box: the plain stones with the class s, the coloured one with the class c. */
function markShapes(mark) {
  return mark.stones.map((s) => rect(s, 's')).join('') + rect(mark.coloured, 'c');
}

// ---- The wordmark as paths ---------------------------------------------------------------------

// Inter of @fontsource/inter, static files of the same Inter release as the UI's variable font
// (@fontsource-variable/inter): opentype.js reads .woff but not .woff2, and cannot instance a variable font's outlines.
function loadInter(weight) {
  const file = join(
    dirname(createRequire(import.meta.url).resolve('@fontsource/inter/package.json')),
    'files',
    `inter-latin-${weight}-normal.woff`,
  );
  const buf = readFileSync(file);
  return opentype.parse(buf.buffer.slice(buf.byteOffset, buf.byteOffset + buf.length));
}
const bold = loadInter(700);
const regular = loadInter(400);

/**
 * The text as path data, its pen starting at x on the baseline y, set at fontSize with the font's pair kerning (which is 0
 * for every pair of these words in the static files). Returns the path data, the left and right edge of the ink and the
 * pen's end.
 */
function textPath(font, text, x, y, fontSize) {
  const glyphs = [...text].map((c) => font.charToGlyph(c));
  const scale = fontSize / font.unitsPerEm;
  let pen = x;
  let d = '';
  let left = Infinity;
  let right = -Infinity;
  glyphs.forEach((g, i) => {
    d += g.getPath(pen, y, fontSize).toPathData(2);
    const box = g.getBoundingBox();
    if (box.x2 > box.x1) {
      left = Math.min(left, pen + box.x1 * scale);
      right = Math.max(right, pen + box.x2 * scale);
    }
    const kern = i + 1 < glyphs.length ? font.getKerningValue(g, glyphs[i + 1]) : 0;
    pen += (g.advanceWidth + kern) * scale;
  });
  return { d, left, right, end: pen };
}

/**
 * The words of a lockup as one path, the ink starting at x: "Quader" in Bold, then the further words (Bastion) after a
 * space in Regular. Returns the path data and the right edge of the ink.
 */
function wordmark(words, x, baseline, fontSize) {
  // Where the pen has to start so the ink of the Q begins at x.
  const shift = x - textPath(bold, words[0], 0, baseline, fontSize).left;
  const first = textPath(bold, words[0], shift, baseline, fontSize);
  let d = first.d;
  let right = first.right;
  let pen = first.end;
  for (const word of words.slice(1)) {
    pen += (regular.charToGlyph(' ').advanceWidth * fontSize) / regular.unitsPerEm;
    const next = textPath(regular, word, pen, baseline, fontSize);
    d += next.d;
    right = next.right;
    pen = next.end;
  }
  return { d, right };
}

// The lockup's word has a cap height of 40 (Inter's cap height is 1490 of 2048 units), centred on the mark's box: its caps
// run from 12 to the baseline at 52, the Q's tail ends at 55.3. The ink starts one stone height (16) after the mark's
// right edge at 60. The lockup ends at least the mark's margin of 4 after the ink, its width rounded up to a multiple of
// 8, so at every multiple of 16 px of height it is a whole and even number of pixels wide: centred in a box of even
// width, the mark's edges stay on whole pixels.
const CAP_HEIGHT = 40;
const WORD_SIZE = (CAP_HEIGHT * bold.unitsPerEm) / bold.tables.os2.sCapHeight;
const WORD_BASELINE = GRID / 2 + CAP_HEIGHT / 2;
const WORD_GAP = 16;
const MARGIN = 4;

function lockup(mark) {
  const word = wordmark(mark.name.split(' '), GRID - MARGIN + WORD_GAP, WORD_BASELINE, WORD_SIZE);
  return { word: word.d, width: Math.ceil((word.right + MARGIN) / 8) * 8 };
}

// ---- The three forms ---------------------------------------------------------------------------

function markSvg(mark) {
  return (
    `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 ${GRID} ${GRID}" role="img" aria-label="${mark.name}">` +
    style('auto') +
    markShapes(mark) +
    `</svg>\n`
  );
}

/** The horizontal lockup: the mark, then the wordmark, in the colours of the viewer's colour scheme. */
function horizontalSvg(mark) {
  const l = lockup(mark);
  return (
    `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 ${l.width} ${GRID}" role="img" aria-label="${mark.name}">` +
    style('auto') +
    markShapes(mark) +
    `<path class="s" d="${l.word}"/>` +
    `</svg>\n`
  );
}

// The square app icon: the mark above its words on the dark tile, the same in both schemes. The mark is drawn at 3 times
// its size (each edge still on a whole unit), the words one per line, each line centred.
const SQUARE = 512;
const SQUARE_RADIUS = 96;
const SQUARE_MARK_SCALE = 3;
const SQUARE_WORD_SIZE = 88;
/** Between the mark's last course and the first line's caps, and between two lines' baselines. */
const SQUARE_GAP = 48;
const SQUARE_LEADING = 1.25;

function squareSvg(mark) {
  const words = mark.name.split(' ');
  const cap = (SQUARE_WORD_SIZE * bold.tables.os2.sCapHeight) / bold.unitsPerEm;
  const markInk = (GRID - 2 * MARGIN) * SQUARE_MARK_SCALE;
  const lines = words.length;
  const height = markInk + SQUARE_GAP + cap + (lines - 1) * SQUARE_WORD_SIZE * SQUARE_LEADING;
  // On a whole unit, so the mark's edges stay on the grid.
  const top = Math.round((SQUARE - height) / 2);
  const markX = SQUARE / 2 - (GRID / 2) * SQUARE_MARK_SCALE;
  const markY = top - MARGIN * SQUARE_MARK_SCALE;
  let paths = '';
  words.forEach((word, i) => {
    const font = i === 0 ? bold : regular;
    const baseline = top + markInk + SQUARE_GAP + cap + i * SQUARE_WORD_SIZE * SQUARE_LEADING;
    const probe = textPath(font, word, 0, baseline, SQUARE_WORD_SIZE);
    const x = SQUARE / 2 - (probe.left + probe.right) / 2;
    paths += `<path class="s" d="${textPath(font, word, x, baseline, SQUARE_WORD_SIZE).d}"/>`;
  });
  return (
    `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 ${SQUARE} ${SQUARE}" role="img" aria-label="${mark.name}">` +
    style('dark') +
    `<rect width="${SQUARE}" height="${SQUARE}" rx="${SQUARE_RADIUS}" fill="${TILE}"/>` +
    `<g transform="translate(${r(markX)} ${r(markY)}) scale(${SQUARE_MARK_SCALE})">${markShapes(mark)}</g>` +
    paths +
    `</svg>\n`
  );
}

// ---- The favicon -------------------------------------------------------------------------------

// favicon.svg is the mark without a tile, its colours following the colour scheme. favicon.ico is the mark at its full
// size in the dark scheme's colours on the dark tile, whose corner radius of 12 stays clear of the corner stones (a radius
// above 15 would cut them): at 16 px the tile shows as a border of 1 px around the stones. The main installation's are the
// wall, the Bastion's the tower; each application's build serves its own.
const TILE_RADIUS = 12;
const FAVICON_SIZES = [16, 32, 48];

function faviconSvg(mark) {
  return (
    `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 ${GRID} ${GRID}">` +
    style('auto') +
    markShapes(mark) +
    `</svg>\n`
  );
}

/** Whether point q lies in the rounded rect [x, y, w, h] with corner radius rad. */
function inRoundedRect(q, [x, y, w, h], rad) {
  if (q[0] < x || q[0] > x + w || q[1] < y || q[1] > y + h) return false;
  const cx = Math.min(Math.max(q[0], x + rad), x + w - rad);
  const cy = Math.min(Math.max(q[1], y + rad), y + h - rad);
  return Math.hypot(q[0] - cx, q[1] - cy) <= rad;
}

const hex = (h) => [1, 3, 5].map((k) => parseInt(h.slice(k, k + 2), 16));

/** The ICO's picture at size × size pixels as RGBA, by 8 × 8 samples per pixel: the same shapes as the SVG. */
function faviconPixels(mark, size) {
  const tile = hex(TILE);
  const shapes = [
    ...mark.stones.map((s) => ({ rect: s, rgb: hex(COLOURS.dark.stone) })),
    { rect: mark.coloured, rgb: hex(COLOURS.dark.coloured) },
  ];
  const N = 8;
  const px = new Uint8Array(size * size * 4);
  for (let y = 0; y < size; y++) {
    for (let x = 0; x < size; x++) {
      let a = 0;
      const sum = [0, 0, 0];
      for (let j = 0; j < N; j++) {
        for (let i = 0; i < N; i++) {
          const q = [((x + (i + 0.5) / N) / size) * GRID, ((y + (j + 0.5) / N) / size) * GRID];
          if (!inRoundedRect(q, [0, 0, GRID, GRID], TILE_RADIUS)) continue;
          let rgb = tile;
          for (const s of shapes) if (inRoundedRect(q, s.rect, RADIUS)) rgb = s.rgb;
          a++;
          for (let ch = 0; ch < 3; ch++) sum[ch] += rgb[ch];
        }
      }
      const o = (y * size + x) * 4;
      for (let ch = 0; ch < 3; ch++) px[o + ch] = a ? Math.round(sum[ch] / a) : 0;
      px[o + 3] = Math.round((a / (N * N)) * 255);
    }
  }
  return px;
}

function png(size, rgba) {
  const chunk = (type, data) => {
    const head = Buffer.alloc(8);
    head.writeUInt32BE(data.length, 0);
    head.write(type, 4, 'latin1');
    const crc = Buffer.alloc(4);
    crc.writeUInt32BE(crc32(Buffer.concat([head.subarray(4), data])), 0);
    return Buffer.concat([head, data, crc]);
  };
  const ihdr = Buffer.alloc(13);
  ihdr.writeUInt32BE(size, 0);
  ihdr.writeUInt32BE(size, 4);
  ihdr.set([8, 6, 0, 0, 0], 8); // 8 bits, RGBA, no interlace
  const rows = Buffer.alloc((size * 4 + 1) * size);
  for (let y = 0; y < size; y++) {
    rows[y * (size * 4 + 1)] = 0; // filter: none
    Buffer.from(rgba.buffer, y * size * 4, size * 4).copy(rows, y * (size * 4 + 1) + 1);
  }
  return Buffer.concat([
    Buffer.from([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a]),
    chunk('IHDR', ihdr),
    chunk('IDAT', deflateSync(rows, { level: 9 })),
    chunk('IEND', Buffer.alloc(0)),
  ]);
}

/** An ICO whose frames are PNGs (every browser that reads .ico reads those). */
function ico(frames) {
  const head = Buffer.alloc(6);
  head.writeUInt16LE(1, 2); // type: icon
  head.writeUInt16LE(frames.length, 4);
  let offset = 6 + 16 * frames.length;
  const dir = frames.map(([size, data]) => {
    const e = Buffer.alloc(16);
    e[0] = size;
    e[1] = size;
    e.writeUInt16LE(1, 4); // planes
    e.writeUInt16LE(32, 6); // bits per pixel
    e.writeUInt32LE(data.length, 8);
    e.writeUInt32LE(offset, 12);
    offset += data.length;
    return e;
  });
  return Buffer.concat([head, ...dir, ...frames.map(([, data]) => data)]);
}

// ---- The app's inline mark ---------------------------------------------------------------------

// shared/brand/quader-mark.ts draws the wall (and, on the sign-in and signed-out screens, the lockup) inline, and the
// Bastion's top bar the tower, so their colours are the tokens of the theme the viewer chose, which an <img> of the files
// above cannot follow. The main installation's build never references BASTION_MARK, so its bundle drops it.
async function geometrySource(quader, bastion) {
  const box = ([x, y, width, height]) => ({ x, y, width, height });
  const geometry = (mark) =>
    JSON.stringify({
      viewBox: `0 0 ${GRID} ${GRID}`,
      radius: RADIUS,
      stones: mark.stones.map(box),
      coloured: box(mark.coloured),
    });
  const l = lockup(quader);
  const source =
    `// Generated by web/scripts/brand-logo.mjs; do not edit, run the script.\n\n` +
    `/** Quader's mark on its 64 grid: the plain stones and the coloured one, each corner rounded by \`radius\`. */\n` +
    `export const MARK = ${geometry(quader)} as const;\n\n` +
    `/** The horizontal lockup: the mark at the left of a box of the same height, then the wordmark as one path. */\n` +
    `export const LOCKUP = ${JSON.stringify({ viewBox: `0 0 ${l.width} ${GRID}`, word: l.word })} as const;\n\n` +
    `/** The Quader Bastion's mark, the tower, on the same grid: the Bastion's application shows it (Q-UI-1617). */\n` +
    `export const BASTION_MARK = ${geometry(bastion)} as const;\n`;
  return prettier.format(source, {
    ...(await prettier.resolveConfig(geometryFile)),
    filepath: geometryFile,
  });
}

// ---- Writing -----------------------------------------------------------------------------------

mkdirSync(outDir, { recursive: true });
const written = new Set();
const write = (name, content) => {
  writeFileSync(join(outDir, name), content);
  written.add(name);
};
for (const [key, mark] of Object.entries(MARKS)) {
  const base = key === 'quader' ? 'quader' : `quader-${key}`;
  write(`${base}-mark.svg`, markSvg(mark));
  write(`${base}-horizontal.svg`, horizontalSvg(mark));
  write(`${base}-square.svg`, squareSvg(mark));
}
for (const name of readdirSync(outDir)) {
  if (!written.has(name)) rmSync(join(outDir, name), { recursive: true, force: true });
}

for (const [dir, mark] of [
  [publicDir, MARKS.quader],
  [bastionPublicDir, MARKS.bastion],
]) {
  mkdirSync(dir, { recursive: true });
  writeFileSync(join(dir, 'favicon.svg'), faviconSvg(mark));
  writeFileSync(
    join(dir, 'favicon.ico'),
    ico(FAVICON_SIZES.map((n) => [n, png(n, faviconPixels(mark, n))])),
  );
}

writeFileSync(geometryFile, await geometrySource(MARKS.quader, MARKS.bastion));
console.log(
  `brand: wrote ${outDir}, the favicons in ${publicDir} and ${bastionPublicDir}, and ${geometryFile}`,
);
