/*
 * Lengths of time in the UI's one short form: the units are abbreviations that need no translation (`d`, `h`, `min`, `s`).
 */

/** A length of time as `1 d 4 h`, `2 h 5 min`, `7 min` or `12 s`: the two largest units, minutes without their seconds. */
export function durationText(seconds: number): string {
  const s = Math.max(0, Math.round(seconds));
  const d = Math.floor(s / 86_400);
  const h = Math.floor((s % 86_400) / 3_600);
  const m = Math.floor((s % 3_600) / 60);
  if (d > 0) return h > 0 ? `${d} d ${h} h` : `${d} d`;
  if (h > 0) return m > 0 ? `${h} h ${m} min` : `${h} h`;
  if (m > 0) return `${m} min`;
  return `${s} s`;
}

/**
 * The time a transfer took or has taken so far (api-and-ui.md R-UI-211): `< 1 s` below one second, `42 s`, `1 min 12 s` below an
 * hour, then the two largest units as `durationText` writes them (`1 h 5 min`).
 */
export function elapsedText(seconds: number): string {
  if (seconds < 1) return '< 1 s';
  const s = Math.round(seconds);
  if (s >= 3_600) return durationText(s);
  const m = Math.floor(s / 60);
  const rest = s % 60;
  if (m === 0) return `${rest} s`;
  return rest > 0 ? `${m} min ${rest} s` : `${m} min`;
}
