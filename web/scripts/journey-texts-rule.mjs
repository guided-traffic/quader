// Q-UI-1046: a journey names a text of a page by the message ID the page takes it from (`ui('nav.security-dashboard')` of
// e2e/support.ts), not by the words, so a change of the words is followed by the journeys without a search. This rule, run by
// `npm run lint` (check-rules.mjs), fails a journey whose ID the translation source does not hold as a plain text, and an ID whose
// text in the source code differs from the translation source: the journey reads messages.xlf, the English page shows the code's
// words, and the two are one text only after `npm run extract-i18n`.

const ENTITIES = { '&amp;': '&', '&lt;': '<', '&gt;': '>', '&quot;': '"', '&apos;': "'" };

/** The plain text of a unit of an XLIFF 2 file, its source or its target: null when the unit is missing or holds an element. */
export function messageText(xlf, id, part = 'source') {
  const start = xlf.indexOf(`<unit id="${id}">`);
  if (start < 0) return null;
  const end = xlf.indexOf('</unit>', start);
  const unit = xlf.slice(start, end);
  const m = new RegExp(`<${part}>([\\s\\S]*?)</${part}>`).exec(unit);
  if (!m || m[1].includes('<')) return null;
  return m[1].replace(/&(amp|lt|gt|quot|apos);/g, (e) => ENTITIES[e]);
}

/** The IDs a journey names: `ui('id')` and `ui('id', 'de')`, with their line. */
export function journeyTextIds(code) {
  const out = [];
  code.split('\n').forEach((line, i) => {
    for (const m of line.matchAll(/\bui\(\s*'([\w.@-]+)'\s*(?:,\s*'(en|de)'\s*)?\)/g)) {
      out.push({ line: i + 1, id: m[1], locale: m[2] ?? 'en' });
    }
  });
  return out;
}

/** The words the source code gives an ID, where it gives them plainly: `$localize` with `:@@id:` and an element's i18n="@@id". */
export function codeText(sources, id) {
  const esc = id.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
  const localize = new RegExp(`:@@${esc}:([^\`$]*)\``);
  const element = new RegExp(`\\si18n="[^"]*@@${esc}"[^>]*>([^<{]*)<`);
  for (const code of sources) {
    const l = localize.exec(code);
    if (l) return l[1];
    const e = element.exec(code);
    if (e && e[1].trim() !== '') return e[1].replace(/\s+/g, ' ').trim();
  }
  return null;
}

/** The findings of one journey file: an ID with no plain text in the locale's file, or whose code text differs from the source. */
export function journeyTextFindings(code, en, de, sources) {
  const found = [];
  for (const { line, id, locale } of journeyTextIds(code)) {
    const text = locale === 'de' ? messageText(de, id, 'target') : messageText(en, id);
    if (text === null) {
      found.push({ line, id, problem: `no plain ${locale === 'de' ? 'German target' : 'source'} of the ID in the translation file` });
      continue;
    }
    if (locale === 'en') {
      const words = codeText(sources, id);
      if (words !== null && words !== text) {
        found.push({ line, id, problem: `the code says "${words}" and messages.xlf "${text}": run npm run extract-i18n` });
      }
    }
  }
  return found;
}
