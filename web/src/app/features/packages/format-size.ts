const UNITS = ['B', 'KiB', 'MiB', 'GiB', 'TiB'] as const;

/** The locale of the build: the one `$localize` was compiled for, English where the build has none. */
function buildLocale(): string {
  return (typeof $localize !== 'undefined' && $localize.locale) || 'en-US';
}

/**
 * A size in bytes in binary units, one decimal from KiB up, with the decimal mark of the locale (`47.1 KiB`, German `47,1 KiB`);
 * the unit is not translated. The locale is the build's unless given.
 */
export function formatSize(bytes: number, locale: string = buildLocale()): string {
  let value = bytes;
  let unit = 0;
  while (value >= 1024 && unit < UNITS.length - 1) {
    value /= 1024;
    unit++;
  }
  if (unit === 0) return `${value} B`;
  const number = new Intl.NumberFormat(locale, { minimumFractionDigits: 1, maximumFractionDigits: 1, useGrouping: false }).format(value);
  return `${number} ${UNITS[unit]}`;
}
