import { formatNumber } from '@angular/common';
import { Figure } from '../../api/limits-api';

/*
 * A figure's limit in words and the units it is written in, apart from the editors of figures.ts: a refusal's notice on any page says
 * a limit (problem-messages-limits.ts), and the editors' words load with the pages of the limits, not with the first page.
 */

/** 1 MB and 1 GB are decimal (Q-UI-736). */
export const MB = 1_000_000;
export const GB = 1_000_000_000;

/** The unit a field holds: a count, or bytes shown as MB or GB. */
export type FigureUnit = 'count' | 'mb' | 'gb';

export function unitOf(figure: Figure): FigureUnit {
  switch (figure) {
    case 'cachedBytesPerSecond':
    case 'uncachedBytesPerSecond':
      return 'mb';
    case 'cachedBytesPerHour':
    case 'uncachedBytesPerHour':
    case 'bytesPerDay':
      return 'gb';
    default:
      return 'count';
  }
}

export function factorOf(figure: Figure): number {
  const unit = unitOf(figure);
  return unit === 'mb' ? MB : unit === 'gb' ? GB : 1;
}

export function n(value: number, locale: string, digits = '1.0-1'): string {
  return formatNumber(value, locale, digits);
}

/** A byte count in decimal units with one decimal (R-UI-183: "1.2 GB"); below 1 kB the bytes. */
export function formatBytes(bytes: number, locale: string): string {
  const units: readonly [number, string][] = [
    [1e12, 'TB'],
    [GB, 'GB'],
    [MB, 'MB'],
    [1e3, 'kB'],
  ];
  for (const [size, unit] of units) {
    if (Math.abs(bytes) >= size) return `${n(bytes / size, locale)} ${unit}`;
  }
  return `${n(bytes, locale, '1.0-0')} B`;
}

/** A count with grouping ("50,000"). */
export function formatCount(value: number, locale: string): string {
  return n(value, locale, '1.0-0');
}

/** A figure's limit in its unit and window: "50,000 per hour", "300 GB per hour", "200 MB per second". */
export function limitText(figure: Figure, value: number | null | undefined, locale: string): string {
  if (value === null || value === undefined) return $localize`:@@limits.unlimited:Unlimited`;
  const unit = unitOf(figure);
  const amount = unit === 'count' ? formatCount(value, locale) : `${n(value / factorOf(figure), locale, '1.0-3')} ${unit === 'mb' ? 'MB' : 'GB'}`;
  switch (figure) {
    case 'requestsPerSecond':
    case 'cachedBytesPerSecond':
    case 'uncachedBytesPerSecond':
      return $localize`:@@limits.limit.per-second:${amount}:amount: per second`;
    case 'requestsBurst':
      return $localize`:@@limits.limit.burst:${amount}:amount: burst`;
    case 'bytesPerDay':
      return $localize`:@@limits.limit.per-day:${amount}:amount: per day`;
    case 'parallelTransfers':
      return $localize`:@@limits.limit.parallel:${amount}:amount: transfers in parallel`;
    default:
      return $localize`:@@limits.limit.per-hour:${amount}:amount: per hour`;
  }
}
