import { signal } from '@angular/core';

/**
 * The one formatter of an absolute time in both applications (R-UI-250): every date and time a screen shows goes through
 * [formatTime], or through the pipe `time` of `time-pipe.ts`, which calls it. Nothing else in `web/src` formats a time: no
 * `DatePipe`, no `| date`, no `formatDate`, no `Intl.DateTimeFormat`, no `toLocale(Date|Time)String`; `scripts/time-format-rule.mjs`
 * refuses them (Q-UI-1562). A time relative to now ("4 minutes ago") is `relativeTime` of `features/packages/format-time.ts`, and its
 * absolute time comes from here.
 *
 * The form is the reader's choice (Q-UI-1560): their preference, else the installation's default, else the factory default, the
 * owner's `26.11.2026 21:39:22 (Thu.)`. [timeFormat] holds the form in force; the main application's `PreferencesStore` sets it, the
 * Bastion's application the installation's default its state names (`BastionStateStore`, Q-UI-1467, which revised Q-UI-1565). Times
 * stand in the browser's zone unless a caller names one.
 */

/** The forms a reader can choose, in the order the choice offers them (Q-UI-1561). */
export const TIME_FORMATS = ['dmy-weekday', 'dmy', 'iso', 'dmy-slash', 'mdy', 'locale'] as const;
export type TimeFormat = (typeof TIME_FORMATS)[number];

/** The factory default, the owner's form (2026-11-26): day, month, year with dots, the 24-hour time with seconds, the weekday. */
export const FACTORY_TIME_FORMAT: TimeFormat = 'dmy-weekday';

/**
 * What of a time a place shows:
 *  - `datetime`: the date and the time of day (the default);
 *  - `date`: the date alone, `26.11.2026 (Thu.)`;
 *  - `time`: the time of day alone, `21:39:22`, where the day is clear from the place (a row of today, the day's heading above);
 *  - `zoned`: the date and the time with the zone's name, `26.11.2026 21:39:22 (Thu.) UTC`, where the place says which zone it shows
 *    (the audit log);
 *  - `minutes`: the time of day without seconds, `21:39`: the axis of a chart, whose ticks lie minutes apart (Q-UI-1563).
 */
export type TimePart = 'datetime' | 'date' | 'time' | 'zoned' | 'minutes';

/** The form in force. A signal, so that a template and a computed value that format a time follow a change of the choice at once. */
export const timeFormat = signal<TimeFormat>(FACTORY_TIME_FORMAT);

/** Whether a value is one of the forms; a stored value the build does not know is not applied. */
export function isTimeFormat(value: unknown): value is TimeFormat {
  return typeof value === 'string' && (TIME_FORMATS as readonly string[]).includes(value);
}

/** The language of the build: the one `$localize` was compiled for, English where the build has none. */
function buildLocale(): string {
  return (typeof $localize !== 'undefined' && $localize.locale) || 'en-US';
}

const cache = new Map<string, Intl.DateTimeFormat>();
/** One `Intl.DateTimeFormat` per locale, zone and options: a list of a hundred rows builds it once. */
function intl(locale: string, zone: string | undefined, options: Intl.DateTimeFormatOptions): Intl.DateTimeFormat {
  const key = `${locale}|${zone ?? ''}|${JSON.stringify(options)}`;
  let f = cache.get(key);
  if (!f) {
    f = new Intl.DateTimeFormat(locale, { ...options, timeZone: zone });
    cache.set(key, f);
  }
  return f;
}

interface Fields {
  readonly year: string;
  readonly month: string;
  readonly day: string;
  readonly hour: string;
  readonly minute: string;
  readonly second: string;
}

/** The numbers of the date and the 24-hour time in the zone, each with its leading zero. */
function fields(at: Date, zone: string | undefined): Fields {
  const parts = intl('en-US', zone, {
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
    hourCycle: 'h23',
  }).formatToParts(at);
  const get = (type: Intl.DateTimeFormatPartTypes): string => parts.find((p) => p.type === type)?.value ?? '';
  return { year: get('year'), month: get('month'), day: get('day'), hour: get('hour'), minute: get('minute'), second: get('second') };
}

/** The weekday's abbreviation in the reader's language with one dot: "Thu.", "Do.". */
function weekday(at: Date, zone: string | undefined, locale: string): string {
  return `${intl(locale, zone, { weekday: 'short' }).format(at).replace(/\.$/, '')}.`;
}

/** The American time of day: 12-hour, no leading zero, `AM` or `PM`; midnight is `12:00:00 AM`, noon `12:00:00 PM`. */
function americanTime(f: Fields, seconds: boolean): string {
  const h = Number(f.hour);
  const hour = h % 12 === 0 ? 12 : h % 12;
  return `${hour}:${f.minute}${seconds ? `:${f.second}` : ''} ${h < 12 ? 'AM' : 'PM'}`;
}

/** The zone's short name in the reader's language: "UTC", "GMT+1", "MEZ". */
function zoneName(at: Date, zone: string | undefined, locale: string): string {
  return intl(locale, zone, { timeZoneName: 'short' }).formatToParts(at).find((p) => p.type === 'timeZoneName')?.value ?? '';
}

/** The form's date. */
function dateOf(format: TimeFormat, f: Fields, at: Date, zone: string | undefined, locale: string): string {
  switch (format) {
    case 'iso':
      return `${f.year}-${f.month}-${f.day}`;
    case 'dmy-slash':
      return `${f.day}/${f.month}/${f.year}`;
    case 'mdy':
      return `${f.month}/${f.day}/${f.year}`;
    case 'locale':
      return intl(locale, zone, { dateStyle: 'medium' }).format(at);
    default:
      return `${f.day}.${f.month}.${f.year}`;
  }
}

/** The form's time of day. */
function timeOf(format: TimeFormat, f: Fields, at: Date, zone: string | undefined, locale: string, seconds: boolean): string {
  if (format === 'mdy') return americanTime(f, seconds);
  if (format === 'locale') return intl(locale, zone, { timeStyle: seconds ? 'medium' : 'short' }).format(at);
  return `${f.hour}:${f.minute}${seconds ? `:${f.second}` : ''}`;
}

export interface TimeOptions {
  /** What of the time the place shows; `datetime` by default. */
  readonly part?: TimePart;
  /** The zone (an IANA name such as `UTC`); the browser's by default. */
  readonly zone?: string;
  /** The form; the one in force ([timeFormat]) by default. */
  readonly format?: TimeFormat;
  /** The reader's language; the build's by default. */
  readonly locale?: string;
}

/**
 * A time as the reader chose to read it: `26.11.2026 21:39:22 (Thu.)` in the factory default. A value is an RFC 3339 text, a
 * number of milliseconds or a `Date`; an empty or unreadable value is the empty text, as a place without the time shows nothing.
 */
export function formatTime(value: string | number | Date | null | undefined, options: TimeOptions = {}): string {
  if (value === null || value === undefined || value === '') return '';
  const at = value instanceof Date ? value : new Date(value);
  if (Number.isNaN(at.getTime())) return '';
  const part = options.part ?? 'datetime';
  const format = options.format ?? timeFormat();
  const locale = options.locale ?? buildLocale();
  const zone = options.zone;
  const f = fields(at, zone);
  if (part === 'time' || part === 'minutes') return timeOf(format, f, at, zone, locale, part === 'time');
  if (format === 'locale' && part !== 'date') {
    const text = intl(locale, zone, { dateStyle: 'medium', timeStyle: 'medium' }).format(at);
    return part === 'zoned' ? `${text} ${zoneName(at, zone, locale)}` : text;
  }
  const date = dateOf(format, f, at, zone, locale);
  const day = format === 'dmy-weekday' ? ` (${weekday(at, zone, locale)})` : '';
  if (part === 'date') return `${date}${day}`;
  const text = `${date} ${timeOf(format, f, at, zone, locale, true)}${day}`;
  return part === 'zoned' ? `${text} ${zoneName(at, zone, locale)}` : text;
}
