// The translation source and the German translation file against a fresh extraction (R-UI-047) of both applications, the main
// installation's and the Bastion's (R-UI-227), which share the one German file. `ng extract-i18n` writes the
// source file messages.xlf from the templates and the $localize calls; nothing kept it in step with the code, so the committed
// file drifted (687 location notes and the order of the units), and the German file kept the notes of the day each unit was
// translated and a unit of a removed hint. This script extracts into a temporary directory and fails when:
//   - messages.xlf differs from the extraction by one byte;
//   - messages.de.xlf lacks a unit of the extraction (an English text with no German one), holds a unit the extraction no longer has,
//     or holds a unit whose location notes or whose English source differ from the extraction's;
//   - a German text holds a word of more than LONG_WORD letters without a soft hyphen (`&#xAD;`) at a joint of its compound, so
//     that no run of letters between hyphens, soft hyphens and other characters is longer (Q-UI-1091): such a word is wider than the
//     narrowest heading's line and broke inside itself ("Authentifizierungseinstellung" / "en" at 390 px).
// `--write` puts the extraction in place and rewrites the German file in the extraction's order with the extraction's notes; it
// drops the units the extraction no longer has and keeps every target and every German unit's own source. A missing unit or a
// changed source still fails after `--write`: a person writes the German text (web/src/locale/GLOSSARY.md), and a changed source is
// corrected by hand together with its target. `make web-i18n` writes, `make web-i18n-check` checks (CI: the job web).
import { spawnSync } from 'node:child_process';
import { mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { fileURLToPath } from 'node:url';

const UNIT = /<unit id="([^"]+)">[\s\S]*?<\/unit>/g;
const NOTES = /<notes>[\s\S]*?<\/notes>/;
const SOURCE = /<source>[\s\S]*?<\/source>/;
const TARGET = /<target>[\s\S]*?<\/target>/;

/** units reads the units of an XLIFF 2.0 file in their order: id, the whole element, its notes block and its source. */
export function units(xliff) {
  const out = [];
  for (const m of xliff.matchAll(UNIT)) {
    const text = m[0];
    out.push({
      id: m[1],
      text,
      start: m.index,
      end: m.index + text.length,
      notes: text.match(NOTES)?.[0] ?? '',
      source: text.match(SOURCE)?.[0] ?? '',
      target: text.match(TARGET)?.[0] ?? '',
    });
  }
  return out;
}

/**
 * The most letters a run without a hyphen or a soft hyphen may have in a German text (Q-UI-1091): 20 letters of the first heading's
 * size at 390 px take some 250 px, which every heading's line holds; the page's heading there holds 358 px.
 */
export const LONG_WORD = 20;

/**
 * longWords answers the words of the German file's targets that are longer than LONG_WORD letters without a soft hyphen or a hyphen
 * in them, with their unit: the target's text without its placeholders and markup (whose attributes hold the English code), its
 * soft hyphens written as `&#xAD;`.
 */
export function longWords(german) {
  const out = [];
  for (const u of units(german)) {
    const text = u.target
      .replace(/^<target>|<\/target>$/g, '')
      .replace(/<[^>]*>/g, ' ')
      .replace(/&#xAD;|&#173;|\u00AD/gi, '\u00AD');
    for (const m of text.matchAll(/[\p{L}\p{M}\u00AD]+/gu)) {
      if (m[0].split('\u00AD').some((part) => part.length > LONG_WORD)) out.push({ id: u.id, word: m[0].replace(/\u00AD/g, '') });
    }
  }
  return out;
}

/**
 * compare holds the German file to the extraction. It answers the ids of the units the German file lacks (missing), holds beyond
 * the extraction (stale), and holds with other location notes (notes) or another English source (source).
 */
export function compare(extracted, german) {
  const ex = new Map(units(extracted).map((u) => [u.id, u]));
  const de = new Map(units(german).map((u) => [u.id, u]));
  const out = { missing: [], stale: [], notes: [], source: [] };
  for (const [id, u] of ex) {
    const d = de.get(id);
    if (!d) {
      out.missing.push(id);
      continue;
    }
    if (d.notes !== u.notes) out.notes.push(id);
    if (d.source !== u.source) out.source.push(id);
  }
  for (const id of de.keys()) {
    if (!ex.has(id)) out.stale.push(id);
  }
  return out;
}

/**
 * rewrite answers the German file in the extraction's order: each unit the German file has is the extraction's unit with the German
 * file's target after its source, and with the German file's own source where the English changed, so that a changed text stays
 * visible to the check. Units the extraction no longer has are dropped; units the German file lacks are left out, since only a
 * person can write their target. The head and the tail of the German file (its xliff and file elements) stay as they are.
 */
export function rewrite(extracted, german) {
  const de = units(german);
  if (de.length === 0) throw new Error('the German file holds no unit');
  const byId = new Map(de.map((u) => [u.id, u]));
  const head = german.slice(0, de[0].start);
  const tail = german.slice(de[de.length - 1].end);
  const indent = head.slice(head.lastIndexOf('\n') + 1);
  const out = [];
  for (const u of units(extracted)) {
    const d = byId.get(u.id);
    if (!d) continue;
    let text = u.text;
    if (d.source !== u.source) text = text.replace(u.source, () => d.source);
    const sourceEnd = text.indexOf('</source>') + '</source>'.length;
    const lineStart = text.lastIndexOf('\n', text.indexOf('<source>')) + 1;
    const sourceIndent = text.slice(lineStart, text.indexOf('<source>'));
    text = `${text.slice(0, sourceEnd)}\n${sourceIndent}${d.target}${text.slice(sourceEnd)}`;
    out.push(text);
  }
  return head + out.join(`\n${indent}`) + tail;
}

/**
 * merge answers the extraction of the main installation's application with the units only the Bastion's application has (bastion.md
 * R-BAS-116, R-UI-227) appended in that extraction's order: both applications are built with the one German file. A unit both hold
 * keeps the main installation's notes; its text is one, since an ID names one text.
 */
export function merge(main, bastion) {
  const ours = units(main);
  const have = new Set(ours.map((u) => u.id));
  const extra = units(bastion).filter((u) => !have.has(u.id));
  if (extra.length === 0 || ours.length === 0) return main;
  const head = main.slice(0, ours[0].start);
  const indent = head.slice(head.lastIndexOf('\n') + 1);
  const end = ours[ours.length - 1].end;
  return main.slice(0, end) + extra.map((u) => `\n${indent}${u.text}`).join('') + main.slice(end);
}

function extractProject(webDir, project) {
  const dir = mkdtempSync(join(tmpdir(), 'quader-i18n-'));
  try {
    const ng = join(webDir, 'node_modules', '.bin', 'ng');
    const r = spawnSync(ng, ['extract-i18n', project, '--format', 'xlf2', '--output-path', dir, '--out-file', 'messages.xlf'], {
      cwd: webDir,
      stdio: ['ignore', 'inherit', 'inherit'],
    });
    if (r.status !== 0) throw new Error(`ng extract-i18n ${project} exited with ${r.status ?? r.signal}`);
    return readFileSync(join(dir, 'messages.xlf'), 'utf8');
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
}

/** The extraction of both applications: the main installation's, then what only the Bastion's holds. */
function extract(webDir) {
  return merge(extractProject(webDir, 'web'), extractProject(webDir, 'bastion-gui'));
}

function list(label, ids) {
  if (ids.length === 0) return;
  console.error(`${label} (${ids.length}):`);
  for (const id of ids.slice(0, 50)) console.error(`  ${id}`);
  if (ids.length > 50) console.error(`  … and ${ids.length - 50} more`);
}

function main(write) {
  const webDir = fileURLToPath(new URL('..', import.meta.url));
  const sourcePath = join(webDir, 'src', 'locale', 'messages.xlf');
  const germanPath = join(webDir, 'src', 'locale', 'messages.de.xlf');
  const extracted = extract(webDir);
  let source = readFileSync(sourcePath, 'utf8');
  let german = readFileSync(germanPath, 'utf8');
  if (write) {
    if (source !== extracted) writeFileSync(sourcePath, extracted);
    const next = rewrite(extracted, german);
    if (next !== german) writeFileSync(germanPath, next);
    source = extracted;
    german = next;
  }
  let ok = true;
  if (source !== extracted) {
    ok = false;
    console.error('src/locale/messages.xlf differs from a fresh extraction: run make web-i18n and commit the result.');
  }
  const d = compare(extracted, german);
  list('units with no German text in src/locale/messages.de.xlf: write their targets', d.missing);
  list('units of src/locale/messages.de.xlf the extraction no longer has: run make web-i18n', d.stale);
  list('units of src/locale/messages.de.xlf with stale location notes: run make web-i18n', d.notes);
  list('units whose English source changed since the German text was written: correct the source and the target', d.source);
  if (d.missing.length + d.stale.length + d.notes.length + d.source.length > 0) ok = false;
  const long = longWords(german);
  list(
    `German words of more than ${LONG_WORD} letters without a soft hyphen (&#xAD;) at a joint of the compound (web/src/locale/GLOSSARY.md)`,
    long.map((w) => `${w.id}: ${w.word}`),
  );
  if (long.length > 0) ok = false;
  console.log(ok ? `i18n: ${units(extracted).length} units, the source and the German file match the extraction` : 'i18n: failed');
  return ok;
}

if (process.argv[1] && import.meta.url === new URL(`file://${process.argv[1]}`).href) {
  process.exit(main(process.argv.includes('--write')) ? 0 : 1);
}
