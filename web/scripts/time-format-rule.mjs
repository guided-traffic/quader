// R-UI-250, Q-UI-1562: every absolute time a screen shows is written by the one formatter, `formatTime` of
// src/app/shared/time-format.ts or the pipe `time` of src/app/shared/time-pipe.ts, in the form the reader chose. A time formatted another
// way stands in another form than its neighbours and ignores the choice: 72 files did it five ways before the formatter existed
// (`DatePipe`, `| date`, `formatDate`, `Intl.DateTimeFormat`, `toLocaleString`). The rule refuses those in the code and the templates
// of both applications; the formatter's own module is the one place that may use `Intl.DateTimeFormat`. A number's `toLocaleString()`
// is no time and stays allowed. Run by check-rules.mjs over src, tested by lint-rules.test.mjs.

/** The ways of formatting a time beside the formatter. */
const DIRECT = [
  { re: /\|\s*date\b/, what: 'the pipe date' },
  { re: /\bDatePipe\b/, what: 'DatePipe' },
  { re: /\bformatDate\s*\(/, what: 'formatDate' },
  { re: /\bIntl\.DateTimeFormat\b/, what: 'Intl.DateTimeFormat' },
  { re: /\.toLocale(Date|Time)String\s*\(/, what: 'toLocaleDateString or toLocaleTimeString' },
  { re: /\bnew Date\([^)]*\)\.toLocaleString\s*\(/, what: "a Date's toLocaleString" },
];

/** The formatter's own module. */
export const TIME_FORMATTER = 'src/app/shared/time-format.ts';

/**
 * The lines of a source or a template that format a time beside the formatter, each as its 1-based line, the way found and the text.
 * A line that is a comment is no use of anything.
 * @param {string} rel the file's path relative to web/
 * @param {string} text
 * @returns {{ line: number, what: string, text: string }[]}
 */
export function directTimeFormatting(rel, text) {
  if (rel === TIME_FORMATTER) return [];
  const found = [];
  text.split('\n').forEach((raw, i) => {
    const trimmed = raw.trim();
    if (trimmed.startsWith('//') || trimmed.startsWith('*') || trimmed.startsWith('/*') || trimmed.startsWith('<!--')) return;
    for (const { re, what } of DIRECT) {
      if (re.test(raw)) {
        found.push({ line: i + 1, what, text: trimmed });
        return;
      }
    }
  });
  return found;
}
