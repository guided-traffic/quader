/** A gate setting as the order of R-SIG-061 compares it: on or off, and the accepted references (empty: any anchor). */
export interface GateSetting {
  readonly on: boolean;
  readonly anchors: readonly string[];
}

/** What a change of the setting is (R-SIG-061): nothing, a narrowing, or a weakening (which a project Owner may not make). */
export type GateOrder = 'none' | 'narrowing' | 'weakening';

/**
 * The order of R-SIG-061 as a function of the stored and the new setting: switching on narrows, switching off weakens; any anchor to a
 * list narrows, a list to any anchor weakens; a reference added weakens, one removed from a list that stays non-empty narrows, and a
 * swap is a weakening. Anchors stored while the gate is off are no constraint (Q-SIG-134): a change of them while it stays off is none.
 */
export function gateOrder(stored: GateSetting, next: GateSetting): GateOrder {
  if (!stored.on && !next.on) return 'none';
  if (!stored.on) return 'narrowing';
  if (!next.on) return 'weakening';
  const before = new Set(stored.anchors);
  const after = new Set(next.anchors);
  if (before.size === 0 && after.size === 0) return 'none';
  if (before.size === 0) return 'narrowing';
  if (after.size === 0) return 'weakening';
  if ([...after].some((r) => !before.has(r))) return 'weakening';
  return after.size < before.size ? 'narrowing' : 'none';
}
