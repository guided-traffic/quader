import { formatNumber } from '@angular/common';
import { Figure, FigureOverrides, Figures } from '../../api/limits-api';
import { ConflictField } from '../../shared/conflict-dialog';

/*
 * The figures of a profile as the screens show and edit them (R-UI-178, Q-UI-736): their groups, names, units and the
 * conversion between the API's bytes and the decimal MB and GB of the fields. Pure functions, so that the profile editor, the
 * rule dialog, the maximum dialog and the meters say a figure the same way.
 */

/** Every figure, in the order of R-LIM-004. */
export const FIGURES: readonly Figure[] = [
  'requestsPerSecond',
  'requestsBurst',
  'requestsPerHour',
  'cachedBytesPerSecond',
  'cachedBytesPerHour',
  'uncachedBytesPerSecond',
  'uncachedBytesPerHour',
  'upstreamRequestsPerHour',
  'parallelTransfers',
  'bytesPerDay',
  'refusedPerHour',
];

/** 1 MB and 1 GB are decimal (Q-UI-736). */
export const MB = 1_000_000;
export const GB = 1_000_000_000;

/** The unit a field holds: a count, or bytes shown as MB or GB. */
export type FigureUnit = 'count' | 'mb' | 'gb';

export type FigureGroupId = 'requests' | 'cache' | 'upstream' | 'transfers' | 'refused';

export interface FigureGroup {
  readonly id: FigureGroupId;
  readonly heading: string;
  /** What happens at the limit, one sentence (R-UI-178). */
  readonly help: string;
  readonly figures: readonly Figure[];
}

/** The four groups of the editor and the group of the refused requests (R-UI-178). */
export function figureGroups(): readonly FigureGroup[] {
  return [
    {
      id: 'requests',
      heading: $localize`:@@limits.group.requests:Requests`,
      help: $localize`:@@limits.group.requests-help:At the limit a request is answered 429 with the time to wait.`,
      figures: ['requestsPerSecond', 'requestsBurst', 'requestsPerHour'],
    },
    {
      id: 'cache',
      heading: $localize`:@@limits.group.cache:From the cache`,
      help: $localize`:@@limits.group.cache-help:Above the rate a download is slowed down; above the hourly amount the next download is refused before it starts.`,
      figures: ['cachedBytesPerSecond', 'cachedBytesPerHour'],
    },
    {
      id: 'upstream',
      heading: $localize`:@@limits.group.upstream:From the upstream`,
      help: $localize`:@@limits.group.upstream-help:With the upstream budget used up, what is cached is still served.`,
      figures: ['uncachedBytesPerSecond', 'uncachedBytesPerHour', 'upstreamRequestsPerHour'],
    },
    {
      // A download a person asks for is many transfers (R-LIM-007, Q-LIM-293): the help says what one costs, so that nobody reads the
      // figure as images or installs at once. The numbers are the clients' measured at Q-LIM-293.
      id: 'transfers',
      heading: $localize`:@@limits.group.transfers:Transfers`,
      help: $localize`:@@limits.group.transfers-help:Each layer of an image and each package of an install is one transfer: a pull runs 3 to 6 at once, an npm, pnpm or Yarn install 8 to 64, a NuGet restore one per package. A further transfer waits up to 10 seconds, then is refused, or at once while a server holds many waiting transfers.`,
      figures: ['parallelTransfers', 'bytesPerDay'],
    },
    {
      id: 'refused',
      heading: $localize`:@@limits.group.refused:Refused requests`,
      help: $localize`:@@limits.group.refused-help:Above the limit the caller is blocked for the block time.`,
      figures: ['refusedPerHour'],
    },
  ];
}

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

function factorOf(figure: Figure): number {
  const unit = unitOf(figure);
  return unit === 'mb' ? MB : unit === 'gb' ? GB : 1;
}

/**
 * How the conflict dialog names and shows the members of a profile, a rule and the ceilings (R-UI-042, Q-UI-1210): a figure by its name
 * and its limit in its unit, a missing figure of a profile or a ceiling as unlimited; a ceiling's figure after its class.
 */
export function limitConflictFields(locale: string): (pointer: string) => ConflictField | null {
  const unlimited = $localize`:@@limits.unlimited:Unlimited`;
  const named: Record<string, string> = {
    '/name': $localize`:@@limits.profile.name:Name`,
    '/description': $localize`:@@limits.profile.description:Description (optional)`,
    '/profile': $localize`:@@limits.rule.profile:Profile`,
    '/note': $localize`:@@limits.rule.note:Note (optional)`,
  };
  const classes: Record<string, string> = {
    anonymous: $localize`:@@limits.ceilings.anonymous:All anonymous callers`,
    signedIn: $localize`:@@limits.ceilings.signed-in:All signed-in callers`,
  };
  return (pointer) => {
    if (named[pointer]) return { label: named[pointer] };
    const [, member, figure] = pointer.split('/');
    if (!member || !figure || !(FIGURES as readonly string[]).includes(figure)) return null;
    const f = figure as Figure;
    const show = (v: unknown): string => limitText(f, typeof v === 'number' ? v : null, locale);
    if (member === 'overrides') return { label: figureName(f), show };
    if (member === 'figures') return { label: figureName(f), show, empty: unlimited };
    if (classes[member]) return { label: `${classes[member]} · ${figureName(f)}`, show, empty: unlimited };
    return null;
  };
}

/** The label of a figure's field inside its group (R-UI-178). */
export function fieldLabel(figure: Figure): string {
  switch (figure) {
    case 'requestsPerSecond':
      return $localize`:@@limits.field.requests-second:Per second`;
    case 'requestsBurst':
      return $localize`:@@limits.field.burst:Burst`;
    case 'requestsPerHour':
      return $localize`:@@limits.field.requests-hour:Per hour`;
    case 'cachedBytesPerSecond':
    case 'uncachedBytesPerSecond':
      return $localize`:@@limits.field.mb-second:MB per second`;
    case 'cachedBytesPerHour':
    case 'uncachedBytesPerHour':
      return $localize`:@@limits.field.gb-hour:GB per hour`;
    case 'upstreamRequestsPerHour':
      return $localize`:@@limits.field.upstream-requests:Upstream requests per hour`;
    case 'parallelTransfers':
      return $localize`:@@limits.field.parallel:In parallel`;
    case 'bytesPerDay':
      return $localize`:@@limits.field.gb-day:GB per day`;
    case 'refusedPerHour':
      return $localize`:@@limits.field.refused-hour:Per hour`;
  }
}

/** The name of a figure on its own, as a meter's row and a refusal name it (R-UI-184). */
export function figureName(figure: Figure | string): string {
  switch (figure) {
    case 'requestsPerSecond':
      return $localize`:@@limits.figure.requests-second:Requests per second`;
    case 'requestsBurst':
      return $localize`:@@limits.figure.burst:Burst`;
    case 'requestsPerHour':
      return $localize`:@@limits.figure.requests-hour:Requests per hour`;
    case 'cachedBytesPerSecond':
      return $localize`:@@limits.figure.cache-second:From the cache per second`;
    case 'cachedBytesPerHour':
      return $localize`:@@limits.figure.cache-hour:From the cache per hour`;
    case 'uncachedBytesPerSecond':
      return $localize`:@@limits.figure.upstream-second:From the upstream per second`;
    case 'uncachedBytesPerHour':
      return $localize`:@@limits.figure.upstream-hour:From the upstream per hour`;
    case 'upstreamRequestsPerHour':
      return $localize`:@@limits.figure.upstream-requests:Upstream requests per hour`;
    case 'parallelTransfers':
      return $localize`:@@limits.figure.parallel:Parallel transfers`;
    case 'bytesPerDay':
      return $localize`:@@limits.figure.day:Volume per day`;
    case 'refusedPerHour':
      return $localize`:@@limits.figure.refused:Refused per hour`;
    case 'ceiling.anonymous':
      return $localize`:@@limits.figure.ceiling-anonymous:Ceiling of all anonymous callers`;
    case 'ceiling.signedIn':
      return $localize`:@@limits.figure.ceiling-signed-in:Ceiling of all signed-in callers`;
    default:
      return figure;
  }
}

/** The width class of a figure's field (Q-UI-335): a rate or a count of six digits, an hourly or daily amount of eight. */
export function fieldWidth(figure: Figure): 'number' | 'duration' {
  switch (figure) {
    case 'requestsPerHour':
    case 'cachedBytesPerHour':
    case 'uncachedBytesPerHour':
    case 'upstreamRequestsPerHour':
    case 'bytesPerDay':
      return 'duration';
    default:
      return 'number';
  }
}

/** The value of a figure as its field shows it: the count, or the bytes in MB or GB without grouping; empty is unlimited. */
export function toField(figure: Figure, value: number | null | undefined): string {
  if (value === null || value === undefined) return '';
  const shown = value / factorOf(figure);
  // At most three decimals of MB or GB: whatever the API holds below that is not a value a person typed.
  return String(Math.round(shown * 1000) / 1000);
}

/** The result of reading a field: a value (null for an empty field, unlimited) or why it is not one. */
export type FieldValue = { readonly ok: true; readonly value: number | null } | { readonly ok: false; readonly error: string };

/**
 * Reads a figure's field (R-UI-178): empty is unlimited; a count is a whole number of at least 1; MB and GB take a decimal point
 * or comma and are converted to bytes, rounded down, at least 1 byte.
 */
export function fromField(figure: Figure, text: string): FieldValue {
  const t = text.trim().replace(/[\s_]/g, '');
  if (t === '') return { ok: true, value: null };
  const unit = unitOf(figure);
  if (unit === 'count') {
    if (!/^\d+$/.test(t) || Number(t) < 1 || !Number.isSafeInteger(Number(t))) {
      return { ok: false, error: $localize`:@@limits.field.count-invalid:A whole number of at least 1, or empty for unlimited.` };
    }
    return { ok: true, value: Number(t) };
  }
  const normal = t.replace(',', '.');
  if (!/^\d+(\.\d+)?$/.test(normal) && !/^\.\d+$/.test(normal)) {
    return { ok: false, error: $localize`:@@limits.field.amount-invalid:A number above 0, or empty for unlimited.` };
  }
  const bytes = Math.floor(Number(normal) * factorOf(figure) + 1e-6);
  if (bytes < 1 || !Number.isSafeInteger(bytes)) {
    return { ok: false, error: $localize`:@@limits.field.amount-invalid:A number above 0, or empty for unlimited.` };
  }
  return { ok: true, value: bytes };
}

/** True where every figure is unlimited. */
export function allUnlimited(figures: Figures | FigureOverrides): boolean {
  return FIGURES.every((f) => figures[f] === null || figures[f] === undefined);
}

function n(value: number, locale: string, digits = '1.0-1'): string {
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

/**
 * The eleven figures in one compact line (R-UI-178): "200/s · 1,000 burst · 50,000/h · cache 200 MB/s, 300 GB/h · upstream 50 MB/s,
 * 30 GB/h, 2,000 req/h · 250 parallel transfers · 1,000 GB/day · 500 refused/h". An unlimited figure is left out; nothing set reads "Unlimited".
 */
export function compactFigures(figures: Figures | FigureOverrides, locale: string): string {
  const v = (f: Figure): number | null => figures[f] ?? null;
  const c = (x: number): string => formatCount(x, locale);
  const mb = (x: number): string => n(x / MB, locale, '1.0-3');
  const gb = (x: number): string => n(x / GB, locale, '1.0-3');
  const parts: string[] = [];
  const rps = v('requestsPerSecond');
  if (rps !== null) parts.push(`${c(rps)}/s`);
  const burst = v('requestsBurst');
  if (burst !== null) parts.push($localize`:@@limits.compact.burst:${c(burst)}:n: burst`);
  const rph = v('requestsPerHour');
  if (rph !== null) parts.push(`${c(rph)}/h`);
  const cache: string[] = [];
  const cs = v('cachedBytesPerSecond');
  if (cs !== null) cache.push(`${mb(cs)} MB/s`);
  const ch = v('cachedBytesPerHour');
  if (ch !== null) cache.push(`${gb(ch)} GB/h`);
  if (cache.length) parts.push($localize`:@@limits.compact.cache:cache ${cache.join(', ')}:values:`);
  const up: string[] = [];
  const us = v('uncachedBytesPerSecond');
  if (us !== null) up.push(`${mb(us)} MB/s`);
  const uh = v('uncachedBytesPerHour');
  if (uh !== null) up.push(`${gb(uh)} GB/h`);
  const ur = v('upstreamRequestsPerHour');
  if (ur !== null) up.push($localize`:@@limits.compact.upstream-requests:${c(ur)}:n: req/h`);
  if (up.length) parts.push($localize`:@@limits.compact.upstream:upstream ${up.join(', ')}:values:`);
  const par = v('parallelTransfers');
  // The figure's words stay on one line ("64 parallel transfers" never breaks before its last word).
  if (par !== null) parts.push($localize`:@@limits.compact.parallel:${c(par)}:n: parallel transfers`.replace(/ /g, ' '));
  const day = v('bytesPerDay');
  // The day is a word of the language ("GB/Tag"); the hour and the second keep their units' signs, which German writes alike.
  if (day !== null) parts.push($localize`:@@limits.compact.day:${gb(day)}:n: GB/day`);
  const ref = v('refusedPerHour');
  if (ref !== null) parts.push($localize`:@@limits.compact.refused:${c(ref)}:n: refused/h`);
  // A number keeps its unit on its line ("100 GB/day" never breaks between the two).
  return parts.length ? parts.join(' · ').replace(/(\d) (?=\p{L})/gu, '$1 ') : $localize`:@@limits.unlimited:Unlimited`;
}

/** One override as the rule table names it after the profile: "+ uncached 20 GB/h", "+ requests unlimited". */
export function overrideText(figure: Figure, value: number | null, locale: string): string {
  const what = shortName(figure);
  if (value === null) return $localize`:@@limits.override.unlimited:+ ${what}:figure: unlimited`;
  const unit = unitOf(figure);
  const amount =
    unit === 'count' ? formatCount(value, locale) : unit === 'mb' ? `${n(value / MB, locale, '1.0-3')} MB` : `${n(value / GB, locale, '1.0-3')} GB`;
  return `+ ${what} ${amount}${windowSuffix(figure)}`;
}

function windowSuffix(figure: Figure): string {
  switch (figure) {
    case 'requestsPerSecond':
    case 'cachedBytesPerSecond':
    case 'uncachedBytesPerSecond':
      return '/s';
    case 'bytesPerDay':
      return $localize`:@@limits.window.day:/day`;
    case 'parallelTransfers':
    case 'requestsBurst':
      return '';
    default:
      return '/h';
  }
}

function shortName(figure: Figure): string {
  switch (figure) {
    case 'requestsPerSecond':
    case 'requestsPerHour':
      return $localize`:@@limits.short.requests:requests`;
    case 'requestsBurst':
      return $localize`:@@limits.short.burst:burst`;
    case 'cachedBytesPerSecond':
    case 'cachedBytesPerHour':
      return $localize`:@@limits.short.cache:cache`;
    case 'uncachedBytesPerSecond':
    case 'uncachedBytesPerHour':
      return $localize`:@@limits.short.uncached:uncached`;
    case 'upstreamRequestsPerHour':
      return $localize`:@@limits.short.upstream-requests:upstream requests`;
    case 'parallelTransfers':
      return $localize`:@@limits.short.parallel:parallel transfers`;
    case 'bytesPerDay':
      return $localize`:@@limits.short.day:volume`;
    case 'refusedPerHour':
      return $localize`:@@limits.short.refused:refused`;
  }
}

/** The overrides of a rule as the table shows them: at most two, then "+ N more" (R-UI-179). */
export function overridesSummary(overrides: FigureOverrides, locale: string): string[] {
  const set = FIGURES.filter((f) => f in overrides);
  const shown = set.slice(0, 2).map((f) => overrideText(f, overrides[f] ?? null, locale));
  if (set.length > 2) shown.push($localize`:@@limits.override.more:+ ${set.length - 2}:count: more`);
  return shown;
}
