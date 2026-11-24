// The palette's contrast, computed from the stylesheets themselves (Q-UI-88, Q-UI-96,
// docs/developer/design-language.md). jsdom paints nothing, so axe's `color-contrast` runs only in the
// browser tier; this test holds the accent pairs in the unit tier, where a colour change is made.
import tokensScss from './_tokens.scss' with { loader: 'text' };

type Scheme = 'light' | 'dark';
type Rgba = readonly [number, number, number, number];

/** Splits at the commas that are not inside parentheses: `rgb(1 2 3 / 4%), #fff`. */
function splitTop(s: string): string[] {
  const out: string[] = [];
  let depth = 0;
  let cur = '';
  for (const ch of s) {
    if (ch === '(') depth++;
    if (ch === ')') depth--;
    if (ch === ',' && depth === 0) {
      out.push(cur.trim());
      cur = '';
    } else {
      cur += ch;
    }
  }
  out.push(cur.trim());
  return out;
}

/** Every `light-dark()` pair of the Quader tokens. */
function readPalette(): Map<string, readonly [string, string]> {
  const vals = new Map<string, readonly [string, string]>();
  const root = tokensScss.split(':root {')[1]?.split('\n}')[0] ?? '';
  for (const m of root.matchAll(/^\s*(--quader-[a-z0-9-]+):\s*(.+);\s*$/gm)) {
    const v = (m[2] ?? '').trim();
    const pair = /^light-dark\((.+)\)$/.exec(v);
    const [light = '', dark = ''] = pair ? splitTop(pair[1] ?? '') : [v, v];
    vals.set(m[1] ?? '', [light, dark]);
  }
  return vals;
}

const palette = readPalette();

function colour(name: string, scheme: Scheme): Rgba {
  const pair = palette.get(name);
  if (!pair) throw new Error(`${name} is not set in _tokens.scss`);
  return parse(scheme === 'light' ? pair[0] : pair[1], scheme);
}

function parse(v: string, scheme: Scheme): Rgba {
  const ref = /var\((--[a-z0-9-]+)\)/.exec(v);
  if (ref?.[1] && !v.startsWith('#')) return colour(ref[1], scheme);
  const hex = /^#([0-9a-f]{2})([0-9a-f]{2})([0-9a-f]{2})$/i.exec(v);
  if (hex) return [parseInt(hex[1] ?? '', 16), parseInt(hex[2] ?? '', 16), parseInt(hex[3] ?? '', 16), 1];
  const rgb = /^rgb\((\d+) (\d+) (\d+) \/ (\d+)%\)$/.exec(v);
  if (rgb) return [Number(rgb[1]), Number(rgb[2]), Number(rgb[3]), Number(rgb[4]) / 100];
  throw new Error(`not a colour: ${v}`);
}

/** A translucent colour over an opaque one, composited in sRGB as a browser paints it. */
function over(fg: Rgba, bg: Rgba): Rgba {
  const a = fg[3];
  const mix = (i: 0 | 1 | 2) => a * fg[i] + (1 - a) * bg[i];
  return [mix(0), mix(1), mix(2), 1];
}

function luminance(c: Rgba): number {
  const ch = (x: number) => {
    const s = x / 255;
    return s <= 0.04045 ? s / 12.92 : ((s + 0.055) / 1.055) ** 2.4;
  };
  return 0.2126 * ch(c[0]) + 0.7152 * ch(c[1]) + 0.0722 * ch(c[2]);
}

function ratio(a: Rgba, b: Rgba): number {
  const [la, lb] = [luminance(a), luminance(b)];
  return (Math.max(la, lb) + 0.05) / (Math.min(la, lb) + 0.05);
}

/** Hue in degrees and HSL saturation. */
function hueOf(c: Rgba): { hue: number; saturation: number } {
  const [r, g, b] = [c[0] / 255, c[1] / 255, c[2] / 255];
  const max = Math.max(r, g, b);
  const min = Math.min(r, g, b);
  const l = (max + min) / 2;
  const d = max - min;
  if (d === 0) return { hue: 0, saturation: 0 };
  const saturation = d / (1 - Math.abs(2 * l - 1));
  const h = max === r ? ((g - b) / d) % 6 : max === g ? (b - r) / d + 2 : (r - g) / d + 4;
  return { hue: (h * 60 + 360) % 360, saturation };
}

// Colour vision, as the designer measured the severity scale: CIELAB (D65), CIEDE2000, and the dichromacy matrices of Machado,
// Oliveira and Fernandes (2009) at severity 1.0, applied in linear RGB; the simulated colour is rounded to 8 bits as a screen shows it.
type Lab = readonly [number, number, number];

const toLinear = (v: number): number => {
  const s = v / 255;
  return s <= 0.04045 ? s / 12.92 : ((s + 0.055) / 1.055) ** 2.4;
};

const fromLinear = (x: number): number => {
  const c = Math.min(1, Math.max(0, x));
  const s = c <= 0.0031308 ? c * 12.92 : 1.055 * c ** (1 / 2.4) - 0.055;
  return Math.min(255, Math.max(0, Math.round(s * 255)));
};

function lab(c: Rgba): Lab {
  const [r, g, b] = [toLinear(c[0]), toLinear(c[1]), toLinear(c[2])];
  const x = (0.4124 * r + 0.3576 * g + 0.1805 * b) / 0.95047;
  const y = 0.2126 * r + 0.7152 * g + 0.0722 * b;
  const z = (0.0193 * r + 0.1192 * g + 0.9505 * b) / 1.08883;
  const f = (t: number) => (t > 216 / 24389 ? Math.cbrt(t) : ((24389 / 27) * t + 16) / 116);
  const [fx, fy, fz] = [f(x), f(y), f(z)];
  return [116 * fy - 16, 500 * (fx - fy), 200 * (fy - fz)];
}

/** The hue angle of a colour in CIE LCh, in degrees. */
function lchHue(c: Rgba): number {
  const [, a, b] = lab(c);
  return ((Math.atan2(b, a) * 180) / Math.PI + 360) % 360;
}

/** CIEDE2000 colour difference. */
function deltaE2000(c1: Rgba, c2: Rgba): number {
  const rad = (d: number) => (d * Math.PI) / 180;
  const deg = (r: number) => ((r * 180) / Math.PI + 360) % 360;
  const [l1, a1, b1] = lab(c1);
  const [l2, a2, b2] = lab(c2);
  const cBar = (Math.hypot(a1, b1) + Math.hypot(a2, b2)) / 2;
  const g = 0.5 * (1 - Math.sqrt(cBar ** 7 / (cBar ** 7 + 25 ** 7)));
  const [a1p, a2p] = [(1 + g) * a1, (1 + g) * a2];
  const [c1p, c2p] = [Math.hypot(a1p, b1), Math.hypot(a2p, b2)];
  const [h1p, h2p] = [deg(Math.atan2(b1, a1p)), deg(Math.atan2(b2, a2p))];
  const dLp = l2 - l1;
  const dCp = c2p - c1p;
  let dhp = 0;
  if (c1p * c2p !== 0) dhp = Math.abs(h2p - h1p) <= 180 ? h2p - h1p : h2p - h1p > 180 ? h2p - h1p - 360 : h2p - h1p + 360;
  const dHp = 2 * Math.sqrt(c1p * c2p) * Math.sin(rad(dhp / 2));
  const lBarP = (l1 + l2) / 2;
  const cBarP = (c1p + c2p) / 2;
  let hBarP = h1p + h2p;
  if (c1p * c2p !== 0) hBarP = Math.abs(h1p - h2p) <= 180 ? (h1p + h2p) / 2 : h1p + h2p < 360 ? (h1p + h2p + 360) / 2 : (h1p + h2p - 360) / 2;
  const t =
    1 -
    0.17 * Math.cos(rad(hBarP - 30)) +
    0.24 * Math.cos(rad(2 * hBarP)) +
    0.32 * Math.cos(rad(3 * hBarP + 6)) -
    0.2 * Math.cos(rad(4 * hBarP - 63));
  const dTheta = 30 * Math.exp(-(((hBarP - 275) / 25) ** 2));
  const rc = 2 * Math.sqrt(cBarP ** 7 / (cBarP ** 7 + 25 ** 7));
  const sl = 1 + (0.015 * (lBarP - 50) ** 2) / Math.sqrt(20 + (lBarP - 50) ** 2);
  const sc = 1 + 0.045 * cBarP;
  const sh = 1 + 0.015 * cBarP * t;
  const rt = -Math.sin(rad(2 * dTheta)) * rc;
  return Math.sqrt((dLp / sl) ** 2 + (dCp / sc) ** 2 + (dHp / sh) ** 2 + rt * (dCp / sc) * (dHp / sh));
}

type Matrix = readonly (readonly [number, number, number])[];
const dichromacy: Record<'deuteranopia' | 'protanopia', Matrix> = {
  deuteranopia: [
    [0.367322, 0.860646, -0.227968],
    [0.280085, 0.672501, 0.047413],
    [-0.01182, 0.04294, 0.968881],
  ],
  protanopia: [
    [0.152286, 1.052583, -0.204868],
    [0.114503, 0.786281, 0.099216],
    [-0.003882, -0.048116, 1.051998],
  ],
};

function simulate(c: Rgba, m: Matrix): Rgba {
  const v = [toLinear(c[0]), toLinear(c[1]), toLinear(c[2])];
  const row = (i: number) => {
    const r = m[i] ?? [0, 0, 0];
    return fromLinear(r[0] * (v[0] ?? 0) + r[1] * (v[1] ?? 0) + r[2] * (v[2] ?? 0));
  };
  return [row(0), row(1), row(2), 1];
}

// The surfaces an accent sits on; a pair of names is a translucent fill over a surface.
const surfaces: Record<string, string | readonly [string, string]> = {
  page: '--quader-surface',
  raised: '--quader-surface-raised',
  'table head': '--quader-table-head-bg',
  'row hover': '--quader-row-hover',
  menu: '--quader-surface-container',
  dialog: '--quader-dialog-bg',
  'filled field': '--quader-surface-variant',
  'selected row': '--quader-accent-container',
  'primary-container': '--quader-accent-container',
  'inverse surface': '--quader-inverse-surface',
  primary: '--quader-accent',
  'side bar': '--quader-nav-bg',
  'open group': '--quader-nav-bg-nested',
  'top bar': '--quader-nav-bg-deep',
  'active entry': ['--quader-nav-active', '--quader-nav-bg'],
  'active entry in an open group': ['--quader-nav-active', '--quader-nav-bg-nested'],
  // Quader's own controls (Q-UI-126, _controls.scss).
  'accent fill': '--quader-accent-fill',
  'accent fill hover': '--quader-accent-fill-hover',
  'default button': ['--quader-btn-default-bg', '--quader-surface'],
  'default button hover': ['--quader-btn-default-bg-hover', '--quader-surface'],
  'tertiary button hover': ['--quader-btn-tertiary-bg-hover', '--quader-surface'],
  'danger fill': '--quader-danger-fill',
  control: ['--quader-control-bg', '--quader-surface'],
  // The reference's translucent fills, measured over what they sit on (Q-UI-128): the default button on the page and on
  // the frame (the Admin button), a menu item under the pointer, the search field on the frame, a card's strong fill.
  'default button on the frame': ['--quader-btn-default-bg', '--quader-nav-bg'],
  'default button hover on the frame': ['--quader-btn-default-bg-hover', '--quader-nav-bg'],
  'top bar hover': ['--quader-nav-hover', '--quader-nav-bg'],
  'menu item hover': ['--quader-menu-hover', '--quader-menu-bg'],
  // The panel of the project switcher (Q-UI-129): the menu's fill.
  'switcher panel': '--quader-menu-bg',
  // A project's mark (Q-UI-424): the stone, and the coloured stone it turns into under the pointer.
  'project mark': '--quader-mark-stone',
  'project mark lit': '--quader-mark-accent',
  // The badges (Q-UI-128): each status's pill.
  'neutral badge': '--quader-badge-neutral-bg',
  'positive badge': '--quader-badge-ok-bg',
  'warning badge': '--quader-badge-warn-bg',
  'error badge': '--quader-badge-bad-bg',
  'info badge': '--quader-badge-info-bg',
  search: ['--quader-search-bg', '--quader-nav-bg'],
  card: '--quader-card-bg',
  // The grade shields and the severity bar (R-UI-090, R-UI-092, Q-UI-370): each fill a letter or a count sits on.
  'grade S': '--quader-grade-s',
  'grade A': '--quader-grade-a',
  'grade B': '--quader-grade-b',
  'grade C': '--quader-grade-c',
  'grade D': '--quader-grade-d',
  'grade E': '--quader-grade-e',
  'grade N/A': '--quader-grade-na',
  'critical segment': '--quader-sev-critical',
  'high segment': '--quader-sev-high',
  'medium segment': '--quader-sev-medium',
  'low segment': '--quader-sev-low',
  'unknown stripe': '--quader-sev-unknown',
  'unknown light stripe': '--quader-sev-unknown-light',
  // A snippet's window (Q-UI-801): its text area, its bar, a button under the pointer on the bar, the token's chip and the tint of
  // what the reader replaces.
  'code window': '--quader-code-bg',
  'code bar': '--quader-code-bar',
  'code bar hover': ['--quader-code-hover', '--quader-code-bar'],
  'token chip': ['--quader-code-token-bg', '--quader-code-bg'],
  'reader mark': ['--quader-code-mark-bg', '--quader-code-bg'],
};

function surface(name: string, scheme: Scheme): Rgba {
  const s = surfaces[name];
  if (s === undefined) throw new Error(`unknown surface ${name}`);
  return typeof s === 'string' ? colour(s, scheme) : over(colour(s[0], scheme), colour(s[1], scheme));
}

function measure(fg: string, on: string, scheme: Scheme): number {
  const bg = surface(on, scheme);
  let f = colour(fg, scheme);
  if (f[3] < 1) f = over(f, bg);
  return ratio(f, bg);
}

// Text: WCAG 1.4.3, 4.5:1. The same pairs as the table of design-language.md.
const textPairs: readonly (readonly [string, string])[] = [
  ['--quader-accent', 'page'],
  ['--quader-accent', 'raised'],
  ['--quader-accent', 'table head'],
  ['--quader-accent', 'row hover'],
  ['--quader-accent', 'menu'],
  ['--quader-accent', 'dialog'],
  ['--quader-accent', 'filled field'],
  ['--quader-on-accent', 'primary'],
  ['--quader-link', 'page'],
  ['--quader-link', 'raised'],
  ['--quader-link', 'table head'],
  ['--quader-link', 'row hover'],
  ['--quader-link', 'dialog'],
  ['--quader-link', 'selected row'],
  ['--quader-on-accent-container', 'selected row'],
  ['--quader-on-surface', 'selected row'],
  ['--quader-text-muted', 'selected row'],
  ['--quader-on-accent-container', 'primary-container'],
  ['--quader-inverse-accent', 'inverse surface'],
  ['--quader-on-surface', 'page'],
  ['--quader-on-surface', 'raised'],
  ['--quader-text-muted', 'page'],
  ['--quader-text-muted', 'filled field'],
  ['--quader-text-muted', 'dialog'],
  ['--quader-status-bad', 'page'],
  ['--quader-status-bad', 'filled field'],
  ['--quader-status-warn', 'page'],
  ['--quader-status-warn', 'filled field'],
  ['--quader-status-ok', 'page'],
  ['--quader-status-ok', 'raised'],
  ['--quader-status-ok', 'dialog'],
  ['--quader-status-ok', 'selected row'],
  ['--quader-nav-active-fg', 'active entry'],
  ['--quader-nav-active-fg', 'active entry in an open group'],
  // Quader's own controls (Q-UI-126): the label of each button on its fill, at rest and under the pointer, and the
  // text and placeholder of a field.
  ['--quader-on-accent', 'accent fill'],
  ['--quader-on-accent', 'accent fill hover'],
  ['--quader-control-fg', 'default button'],
  ['--quader-control-fg', 'default button hover'],
  ['--quader-control-fg', 'tertiary button hover'],
  ['--quader-control-fg', 'page'],
  ['--quader-on-danger', 'danger fill'],
  ['--quader-control-fg', 'control'],
  ['--quader-control-placeholder', 'control'],
  // The frame and its controls (Q-UI-128): the Admin button, the hover of the top bar and the sidebar, menus, the
  // search field, the sidebar's text and a card's title and muted text.
  ['--quader-control-fg', 'default button on the frame'],
  ['--quader-control-fg', 'default button hover on the frame'],
  ['--quader-nav-fg', 'side bar'],
  ['--quader-nav-fg', 'top bar hover'],
  ['--quader-control-fg', 'menu item hover'],
  // The project switcher (Q-UI-129): the projects on the menu fill, and the headings and keys in the muted tone of the frame.
  ['--quader-control-fg', 'switcher panel'],
  ['--quader-nav-fg-muted', 'switcher panel'],
  ['--quader-search-fg', 'search'],
  ['--quader-search-muted', 'search'],
  ['--quader-heading', 'card'],
  ['--quader-text-muted', 'card'],
  // A badge's text on its pill (Q-UI-128).
  ['--quader-badge-neutral-fg', 'neutral badge'],
  ['--quader-badge-ok-fg', 'positive badge'],
  ['--quader-badge-warn-fg', 'warning badge'],
  ['--quader-badge-bad-fg', 'error badge'],
  ['--quader-badge-info-fg', 'info badge'],
  // A grade's letter on its crest and a count on its segment (R-UI-090, R-UI-092).
  ['--quader-grade-s-ink', 'grade S'],
  ['--quader-grade-ink', 'grade A'],
  ['--quader-grade-ink', 'grade B'],
  ['--quader-grade-ink', 'grade C'],
  ['--quader-grade-ink', 'grade D'],
  ['--quader-grade-ink-light', 'grade E'],
  ['--quader-grade-na-ink', 'grade N/A'],
  ['--quader-sev-ink-light', 'critical segment'],
  ['--quader-sev-ink-light', 'high segment'],
  ['--quader-sev-ink', 'medium segment'],
  ['--quader-sev-ink', 'low segment'],
  ['--quader-on-surface', 'unknown stripe'],
  ['--quader-on-surface', 'unknown light stripe'],
  // The text of a snippet's window in both schemes (Q-UI-801): every syntax colour, the line numbers and prompts, the comments, the
  // tab and the buttons of the bar, the token's chip and the reader's marks.
  ...['fg', 'dim', 'gutter', 'key', 'url', 'string', 'command', 'flag', 'punct'].map((c): [string, string] => [`--quader-code-${c}`, 'code window']),
  ['--quader-code-fg', 'code bar'],
  ['--quader-code-fg', 'code bar hover'],
  ['--quader-code-token-fg', 'token chip'],
  ['--quader-code-mark-fg', 'reader mark'],
];

/** The marks of the severity scale, worst first: Critical to Low, then None and Unknown, which are no step of it. */
const severityMarks = [
  '--quader-sev-critical-mark',
  '--quader-sev-high-mark',
  '--quader-sev-medium-mark',
  '--quader-sev-low-mark',
  '--quader-sev-none',
  '--quader-sev-unknown-mark',
];

// Marks: WCAG 1.4.11, 3:1 (focus ring, tab underline, checkbox, the navigation's bar and icon).
const markPairs: readonly (readonly [string, string])[] = [
  ['--quader-accent', 'page'],
  ['--quader-accent', 'raised'],
  ['--quader-accent', 'dialog'],
  ['--quader-accent', 'filled field'],
  ['--quader-nav-accent', 'side bar'],
  ['--quader-nav-accent', 'open group'],
  ['--quader-nav-accent', 'top bar'],
  ['--quader-nav-accent', 'active entry'],
  ['--quader-nav-accent', 'active entry in an open group'],
  // The search field's outline against the frame it sits on (Q-UI-127).
  ['--quader-search-border', 'top bar'],
  // The outline of a field against the page and its own fill, and the primary button against the page (Q-UI-126).
  ['--quader-control-border', 'page'],
  ['--quader-control-border', 'control'],
  ['--quader-accent-fill', 'page'],
  // The current tab's line on the page, a badge's icon on its pill (Q-UI-128).
  ['--quader-tab-indicator', 'page'],
  ['--quader-badge-neutral-icon', 'neutral badge'],
  ['--quader-badge-ok-icon', 'positive badge'],
  ['--quader-badge-warn-icon', 'warning badge'],
  ['--quader-badge-bad-icon', 'error badge'],
  ['--quader-badge-info-icon', 'info badge'],
  // Quader's mark, its stones and its coloured stone, on the top bar and on the page of the sign-in and signed-out screens.
  ['--quader-mark-stone', 'top bar'],
  ['--quader-mark-accent', 'top bar'],
  ['--quader-mark-stone', 'page'],
  ['--quader-mark-accent', 'page'],
  // A project's mark (Q-UI-424): the stone on the page and on the menu of the project switcher, its letter on the stone and
  // on the coloured stone.
  ['--quader-mark-stone', 'switcher panel'],
  ['--quader-mark-accent', 'switcher panel'],
  ['--quader-mark-letter', 'project mark'],
  ['--quader-mark-letter', 'project mark lit'],
  // The focus ring inside a snippet's window, and the dashed line under what the reader replaces (Q-UI-801).
  ['--quader-code-focus', 'code window'],
  ['--quader-code-focus', 'code bar'],
  ['--quader-code-mark-line', 'reader mark'],
  // The marks of the one severity scale (R-UI-092, R-VULN-022): a band's edge on the raised surface of the trend, a badge's icon on
  // the page, a card, the table head and a row under the pointer.
  ...severityMarks.flatMap((m) => ['page', 'raised', 'card', 'table head', 'row hover'].map((on): [string, string] => [m, on])),
  // The usage screens' series (R-UI-183, R-UI-184): the history's bars on the page and the raised surface, the meter on its track.
  ['--quader-chart-usage', 'page'],
  ['--quader-chart-usage', 'raised'],
  ['--quader-chart-usage', 'card'],
];

function failures(pairs: readonly (readonly [string, string])[], min: number): string[] {
  const out: string[] = [];
  for (const [fg, on] of pairs) {
    for (const scheme of ['light', 'dark'] as const) {
      const r = measure(fg, on, scheme);
      if (r < min) out.push(`${fg} on ${on} (${scheme}): ${r.toFixed(2)}:1`);
    }
  }
  return out;
}

/** The grades that are green on purpose: the good end of the scale (Q-UI-370). */
const goodGrades = ['--quader-grade-s', '--quader-grade-a', '--quader-grade-b', '--quader-grade-s-ink'];

describe('palette', () => {
  it('R-UI-044 the accent, the link and their neighbours read as text at 4.5:1 on every surface they sit on, in both schemes', () => {
    expect(failures(textPairs, 4.5)).toEqual([]);
  });

  it('R-UI-044 the focus ring and the accent marks keep 3:1 against what is next to them (WCAG 1.4.11)', () => {
    expect(failures(markPairs, 3)).toEqual([]);
  });

  it('R-UI-044 the focus ring is the accent', () => {
    const ring = '2px solid var(--quader-accent)';
    expect(palette.get('--quader-focus-ring')).toEqual([ring, ring]);
  });

  it('R-UI-002 the accent is blue, and green marks a positive status only', () => {
    const isGreen = (c: Rgba) => {
      const { hue, saturation } = hueOf(c);
      return saturation > 0.15 && hue >= 75 && hue <= 165;
    };
    for (const scheme of ['light', 'dark'] as const) {
      for (const name of ['--quader-accent', '--quader-link', '--quader-nav-accent']) {
        const { hue } = hueOf(colour(name, scheme));
        expect(hue, `${name} (${scheme})`).toBeGreaterThanOrEqual(200);
        expect(hue, `${name} (${scheme})`).toBeLessThanOrEqual(220);
      }
      expect(isGreen(colour('--quader-status-ok', scheme)), `--quader-status-ok (${scheme})`).toBe(true);
      const green: string[] = [];
      for (const name of palette.keys()) {
        // The positive status, as text or as its badge (Q-UI-128), and the good grades S, A and B with S's letter (Q-UI-370).
        if (name === '--quader-status-ok' || name.startsWith('--quader-badge-ok-') || goodGrades.includes(name)) continue;
        let c: Rgba;
        try {
          c = colour(name, scheme);
        } catch {
          continue; // a length, a shadow or another token that is no colour
        }
        if (isGreen(c)) green.push(name);
      }
      expect(green, scheme).toEqual([]);
    }
  });

  it('R-UI-092 a severity without findings is faint, and its 0 keeps 4.5:1 on its faint fill in both schemes', () => {
    const faint: string[] = [];
    for (const scheme of ['light', 'dark'] as const) {
      const page = colour('--quader-surface', scheme);
      const ink = colour('--quader-on-surface', scheme);
      for (const name of ['--quader-sev-critical', '--quader-sev-high', '--quader-sev-medium', '--quader-sev-low', '--quader-sev-unknown', '--quader-sev-unknown-light']) {
        const c = colour(name, scheme);
        const fill = over([c[0], c[1], c[2], 0.28], page);
        const r = ratio(ink, fill);
        if (r < 4.5) faint.push(`--quader-on-surface on ${name} at 28 % (${scheme}): ${r.toFixed(2)}:1`);
      }
    }
    expect(faint).toEqual([]);
  });

  it('R-UI-002 R-UI-090 the grades from C to E and N/A are not green: green stays a good sign', () => {
    const isGreen = (c: Rgba) => {
      const { hue, saturation } = hueOf(c);
      return saturation > 0.15 && hue >= 75 && hue <= 165;
    };
    for (const scheme of ['light', 'dark'] as const) {
      for (const name of ['--quader-grade-c', '--quader-grade-d', '--quader-grade-e', '--quader-grade-na']) expect(isGreen(colour(name, scheme)), name).toBe(false);
    }
  });

  it('R-UI-092 R-VULN-022 neighbouring severities stay apart under deuteranopia and protanopia: a fill and a mark each at least 9 (CIEDE2000)', () => {
    // Critical, High, Medium, Low and None in the order of the scale; the fills of the bar and the trend, and the marks of the edges and
    // the badge. A later change of one tone fails here before two steps of the scale look alike to a red-green deficient reader.
    const fills = ['--quader-sev-critical', '--quader-sev-high', '--quader-sev-medium', '--quader-sev-low', '--quader-sev-none'];
    const marks = severityMarks.slice(0, 5);
    const close: string[] = [];
    for (const scheme of ['light', 'dark'] as const) {
      for (const [vision, m] of Object.entries(dichromacy)) {
        for (const names of [fills, marks]) {
          for (let i = 1; i < names.length; i++) {
            const [a = '', b = ''] = [names[i - 1], names[i]];
            const d = deltaE2000(simulate(colour(a, scheme), m), simulate(colour(b, scheme), m));
            if (d < 9) close.push(`${a} and ${b} (${scheme}, ${vision}): ${d.toFixed(1)}`);
          }
        }
      }
    }
    expect(close).toEqual([]);
  });

  it('R-UI-092 R-VULN-022 a severity\'s mark is a tone of its fill: the same hue within 12 degrees in LCh, in both schemes', () => {
    const apart: string[] = [];
    for (const scheme of ['light', 'dark'] as const) {
      for (const s of ['critical', 'high', 'medium', 'low']) {
        const d = Math.abs(lchHue(colour(`--quader-sev-${s}-mark`, scheme)) - lchHue(colour(`--quader-sev-${s}`, scheme)));
        const dh = Math.min(d, 360 - d);
        if (dh > 12) apart.push(`${s} (${scheme}): ${dh.toFixed(1)} degrees`);
      }
    }
    expect(apart).toEqual([]);
  });

  it('R-UI-044 the logo plate lifts a dark mark off the dark scheme: the mark reaches 3:1 on it, and the plate stands out from every surface', () => {
    // Debian's swirl (#a80030, its open use logo) has no variant for a dark ground; on the dark surfaces it is 1.9:1 to 2.3:1.
    const swirl: Rgba = [0xa8, 0x00, 0x30, 1];
    const plate = colour('--quader-logo-plate', 'dark');
    expect(ratio(swirl, plate)).toBeGreaterThanOrEqual(3);
    for (const name of ['--quader-surface', '--quader-surface-raised', '--quader-table-head-bg', '--quader-row-hover', '--quader-accent-container']) {
      expect(ratio(plate, colour(name, 'dark')), name).toBeGreaterThanOrEqual(3);
    }
  });
});
