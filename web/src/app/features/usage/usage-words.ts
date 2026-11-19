import { LimitMode, UsagePeriod, UsageState, UsageStateFilter } from '../../api/limits-api';

/*
 * The words of the usage screens (R-UI-183 to R-UI-185): the states of a row in either mode, the periods, and the meter's
 * colour class. In Audit a limit is "would have been", never "reached" (owner, decision 17).
 */

/** The state of a row as its text under the meter; nothing for a normal row. */
export function stateWord(state: UsageState, mode: LimitMode): string | null {
  switch (state) {
    case 'close':
      return $localize`:@@usage.state.close:Close to the limit`;
    case 'limitReached':
    case 'wouldBeLimited':
      return mode === 'audit' || state === 'wouldBeLimited'
        ? $localize`:@@usage.state.would:Would have been limited`
        : $localize`:@@usage.state.reached:Limit reached`;
    case 'blocked':
      return $localize`:@@usage.state.blocked:Blocked`;
    case 'unlimited':
      return $localize`:@@usage.state.unlimited:Unlimited`;
    default:
      return null;
  }
}

/** The status a state carries: its colour and icon come with its word, never alone; green is never one of them. */
export function stateTone(state: UsageState): 'warn' | 'bad' | null {
  switch (state) {
    case 'close':
      return 'warn';
    case 'limitReached':
    case 'wouldBeLimited':
    case 'blocked':
      return 'bad';
    default:
      return null;
  }
}

/** The state a ratio stands for, where the API gives a ratio alone (the detail's figures): from 70 % close, from 100 % at the limit. */
export function stateOfRatio(ratio: number | null, mode: LimitMode): UsageState {
  if (ratio === null) return 'unlimited';
  if (ratio >= 1) return mode === 'audit' ? 'wouldBeLimited' : 'limitReached';
  if (ratio >= 0.7) return 'close';
  return 'normal';
}

/** The state filter that means "at the limit" in the mode (R-UI-183). */
export function reachedFilter(mode: LimitMode): UsageStateFilter {
  return mode === 'audit' ? 'wouldBeLimited' : 'limitReached';
}

export function stateFilterLabel(state: UsageStateFilter, mode: LimitMode): string {
  switch (state) {
    case 'close':
      return $localize`:@@usage.state.close:Close to the limit`;
    case 'blocked':
      return $localize`:@@usage.state.blocked:Blocked`;
    default:
      return mode === 'audit' ? $localize`:@@usage.state.would:Would have been limited` : $localize`:@@usage.state.reached:Limit reached`;
  }
}

export const PERIODS: readonly UsagePeriod[] = ['hour', 'day', '30d'];

export function periodLabel(period: UsagePeriod): string {
  switch (period) {
    case 'hour':
      return $localize`:@@usage.period.hour:Last hour`;
    case 'day':
      return $localize`:@@usage.period.day:Last 24 hours`;
    case '30d':
      return $localize`:@@usage.period.30d:Last 30 days`;
  }
}

/** "in the last hour", "in the last 24 hours", "in the last 30 days": the end of an empty state's sentence. */
export function periodPhrase(period: UsagePeriod): string {
  switch (period) {
    case 'hour':
      return $localize`:@@usage.period.phrase-hour:in the last hour`;
    case 'day':
      return $localize`:@@usage.period.phrase-day:in the last 24 hours`;
    case '30d':
      return $localize`:@@usage.period.phrase-30d:in the last 30 days`;
  }
}
