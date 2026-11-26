import { TIME_FORMATS, TimeFormat, formatTime } from './time-format';

/** The name of a form in the choice (Q-UI-1561). */
export function timeFormatName(format: TimeFormat): string {
  switch (format) {
    case 'dmy-weekday':
      return $localize`:@@time-format.dmy-weekday:With the weekday`;
    case 'dmy':
      return $localize`:@@time-format.dmy:Without the weekday`;
    case 'iso':
      return $localize`:@@time-format.iso:ISO 8601`;
    case 'dmy-slash':
      return $localize`:@@time-format.dmy-slash:British`;
    case 'mdy':
      return $localize`:@@time-format.mdy:American`;
    case 'locale':
      return $localize`:@@time-format.locale:Your language's own`;
  }
}

/**
 * A form as the choice offers it: the time `now` in it, then its name, "2026-11-26 21:39:22 — ISO 8601". The example first: a closed select
 * at 390 px cuts the end of its text, and the example is what a reader chooses by.
 */
export function timeFormatLabel(format: TimeFormat, now: number = Date.now()): string {
  return `${formatTime(now, { format })} — ${timeFormatName(format)}`;
}

/** Every form as the choice offers it, in the order of `TIME_FORMATS`, each with today's time as its example. */
export function timeFormatOptions(now: number = Date.now()): { value: TimeFormat; label: string }[] {
  return TIME_FORMATS.map((format) => ({ value: format, label: timeFormatLabel(format, now) }));
}
