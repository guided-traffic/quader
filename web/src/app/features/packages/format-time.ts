const UNITS: readonly (readonly [Intl.RelativeTimeFormatUnit, number])[] = [
  ['year', 365 * 86_400_000],
  ['month', 30 * 86_400_000],
  ['week', 7 * 86_400_000],
  ['day', 86_400_000],
  ['hour', 3_600_000],
  ['minute', 60_000],
];

/**
 * A point in time relative to now in the viewer's language: "4 minutes ago", "yesterday", "3 months ago", "in 29 days". It is the
 * one way the UI writes a time relative to now (Q-UI-431): a list's cell (with the absolute time as the `<time>` element's title,
 * `<quader-time-ago absolute="title">`) and a card's fact (with the absolute time in brackets) both read it. A time less than a
 * minute away reads "now". The locale is the one of the build (`LOCALE_ID`, or `$localize.locale`).
 */
export function relativeTime(iso: string, locale: string, now: number = Date.now()): string {
  const diff = Date.parse(iso) - now;
  const format = new Intl.RelativeTimeFormat(locale, { numeric: 'auto' });
  for (const [unit, ms] of UNITS) {
    if (Math.abs(diff) >= ms) return format.format(Math.trunc(diff / ms), unit);
  }
  return format.format(0, 'second');
}
