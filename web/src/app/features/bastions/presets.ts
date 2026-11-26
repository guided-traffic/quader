import { Bastion, BastionPreset } from '../../api/bastions-api';
import { CallerKind, LimitProfile, LimitRule } from '../../api/limits-api';
import { everyLabel } from '../limits/callers';
import { formatCount } from '../limits/figures';

/** A registration's limits (R-BAS-093) and the parts of it the screens read. */
export type BastionLimits = Bastion['limits'];
export type BastionFlood = BastionLimits['flood'];
export type PresetName = Exclude<BastionLimits['preset'], 'custom'>;

/** The three presets in the order of the segmented control (R-UI-240). */
export const PRESET_NAMES: readonly PresetName[] = ['internet', 'partnerNetwork', 'internal'];

/** The kinds of a Bastion place's five kind rules, in the order a preset writes and names them (R-LIM-046). */
export const PRESET_KINDS: readonly CallerKind[] = ['anonymous', 'user', 'technicalToken', 'ci', 'administrator'];

/** A preset's name on the segmented control and in its dialog. */
export function presetLabel(preset: BastionLimits['preset']): string {
  switch (preset) {
    case 'internet':
      return $localize`:@@bastion.preset.internet:Internet`;
    case 'partnerNetwork':
      return $localize`:@@bastion.preset.partner:Partner network`;
    case 'internal':
      return $localize`:@@bastion.preset.internal:Internal`;
    case 'custom':
      return $localize`:@@bastion.preset.custom:Custom`;
  }
}

/** The one sentence under the segmented control for the chosen preset (R-UI-240). */
export function presetSentence(preset: BastionLimits['preset']): string {
  switch (preset) {
    case 'internet':
      return $localize`:@@bastion.preset.internet-sentence:Strict limits for callers from the internet; administrators too.`;
    case 'partnerNetwork':
      return $localize`:@@bastion.preset.partner-sentence:The installation's own profiles for known partners.`;
    case 'internal':
      return $localize`:@@bastion.preset.internal-sentence:No limits, everything counted.`;
    case 'custom':
      return $localize`:@@bastion.preset.custom-sentence:The rules or the flood figures differ from every preset.`;
  }
}

/** One flood figure: its path in `BastionFlood`, read and named for the dialog of a preset and for the details. */
export interface FloodFigure {
  readonly key: string;
  readonly read: (f: BastionFlood) => number;
  readonly label: () => string;
}

/** The numeric flood figures in the order the details show them (R-UI-240). */
export const FLOOD_FIGURES: readonly FloodFigure[] = [
  { key: 'anonymous.requestsPerSecond', read: (f) => f.anonymous.requestsPerSecond, label: () => $localize`:@@bastion.flood.anonymous-rps:Anonymous callers: requests a second` },
  { key: 'anonymous.burst', read: (f) => f.anonymous.burst, label: () => $localize`:@@bastion.flood.anonymous-burst:Anonymous callers: burst` },
  { key: 'anonymous.missesPerMinute', read: (f) => f.anonymous.missesPerMinute, label: () => $localize`:@@bastion.flood.anonymous-misses:Anonymous callers: misses a minute` },
  { key: 'authenticated.requestsPerSecond', read: (f) => f.authenticated.requestsPerSecond, label: () => $localize`:@@bastion.flood.signed-rps:Signed-in callers: requests a second` },
  { key: 'authenticated.burst', read: (f) => f.authenticated.burst, label: () => $localize`:@@bastion.flood.signed-burst:Signed-in callers: burst` },
  { key: 'authenticated.missesPerMinute', read: (f) => f.authenticated.missesPerMinute, label: () => $localize`:@@bastion.flood.signed-misses:Signed-in callers: misses a minute` },
  { key: 'gui.requestsPerSecond', read: (f) => f.gui.requestsPerSecond, label: () => $localize`:@@bastion.flood.pages-rps:Pages of the web interface: requests a second` },
  { key: 'gui.burst', read: (f) => f.gui.burst, label: () => $localize`:@@bastion.flood.pages-burst:Pages of the web interface: burst` },
  { key: 'failedAuth.maxFailures', read: (f) => f.failedAuth.maxFailures, label: () => $localize`:@@bastion.flood.failures:Failed sign-ins before a timeout` },
  { key: 'failedAuth.windowSeconds', read: (f) => f.failedAuth.windowSeconds, label: () => $localize`:@@bastion.flood.failures-window:Failed sign-ins counted over, in seconds` },
  { key: 'failedAuth.timeoutSeconds', read: (f) => f.failedAuth.timeoutSeconds, label: () => $localize`:@@bastion.flood.timeout:Timeout after failed sign-ins, in seconds` },
  { key: 'blocking.threshold', read: (f) => f.blocking.threshold, label: () => $localize`:@@bastion.flood.block-threshold:Refused requests before a block` },
  { key: 'blocking.windowSeconds', read: (f) => f.blocking.windowSeconds, label: () => $localize`:@@bastion.flood.block-window:Refused requests counted over, in seconds` },
  { key: 'blocking.durationSeconds', read: (f) => f.blocking.durationSeconds, label: () => $localize`:@@bastion.flood.block-duration:First block, in seconds` },
  { key: 'blocking.maxDurationSeconds', read: (f) => f.blocking.maxDurationSeconds, label: () => $localize`:@@bastion.flood.block-max:Longest block, in seconds` },
];

/** One line of the preset's dialog (Q-UI-1481): what it changes, from what to what. */
export interface PresetChange {
  readonly what: string;
  readonly from: string;
  readonly to: string;
}

/** What applying a preset replaces (Q-UI-1481): the kind rules' profiles, the flood figures, the mode; empty where it changes nothing. */
export interface PresetChanges {
  readonly kinds: readonly PresetChange[];
  readonly flood: readonly PresetChange[];
  readonly exemptNetworks: PresetChange | null;
  readonly mode: PresetChange | null;
}

/**
 * What applying `preset`, as the server lists and applies it (`GET /api/v1/bastions/presets`, Q-UI-1485), to a registration with
 * `limits` and the place's `rules` replaces (Q-UI-1481): each kind rule whose profile or changed figures differ from the preset's (the
 * rule's profile named by `profiles`, its changed figures counted), each flood figure that differs, the exempt networks where any are
 * listed, and the mode. A kind rule the place does not hold is named as added.
 */
export function presetChanges(
  limits: BastionLimits,
  rules: readonly LimitRule[],
  profiles: readonly LimitProfile[],
  preset: BastionPreset,
  locale: string,
): PresetChanges {
  const name = (key: string): string => profiles.find((p) => p.key === key)?.name ?? key;
  const kindChanges: PresetChange[] = [];
  for (const kind of PRESET_KINDS) {
    const rule = rules.find((r) => r.who.kind === 'every' && r.who.callerKind === kind);
    const wanted = preset.kinds[kind];
    const changed = rule ? Object.keys(rule.overrides).length : 0;
    if (rule && rule.profile === wanted && changed === 0) continue;
    const from = !rule
      ? $localize`:@@bastion.preset.no-rule:no rule`
      : changed === 0
        ? name(rule.profile)
        : changed === 1
          ? $localize`:@@bastion.preset.profile-one-changed:${name(rule.profile)}:profile: with 1 changed figure`
          : $localize`:@@bastion.preset.profile-changed:${name(rule.profile)}:profile: with ${changed}:count: changed figures`;
    kindChanges.push({ what: everyLabel(kind), from, to: name(wanted) });
  }
  const floodChanges = FLOOD_FIGURES.filter((f) => f.read(limits.flood) !== f.read(preset.flood)).map((f) => ({
    what: f.label(),
    from: formatCount(f.read(limits.flood), locale),
    to: formatCount(f.read(preset.flood), locale),
  }));
  const exempt = limits.flood.exemptNetworks;
  return {
    kinds: kindChanges,
    flood: floodChanges,
    exemptNetworks:
      exempt.length > 0
        ? { what: $localize`:@@bastion.flood.exempt:Networks without flood protection`, from: exempt.join(', '), to: $localize`:@@bastion.preset.none:none` }
        : null,
    mode:
      limits.mode !== preset.mode
        ? { what: $localize`:@@bastion.preset.mode:Mode`, from: modeName(limits.mode), to: modeName(preset.mode) }
        : null,
  };
}

/** The owner's two words of the mode (decision 17), the same in every language. */
export function modeName(mode: BastionLimits['mode']): string {
  return mode === 'enforce' ? 'Enforce' : 'Audit';
}

/** Whether a preset's dialog has anything to name. */
export function changesNothing(c: PresetChanges): boolean {
  return c.kinds.length === 0 && c.flood.length === 0 && c.exemptNetworks === null && c.mode === null;
}
