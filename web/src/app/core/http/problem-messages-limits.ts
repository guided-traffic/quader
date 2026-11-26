import { formatDate } from '@angular/common';
import { Problem } from '../../api/problem';
import { Figure } from '../../api/limits-api';
import { limitText } from '../../features/limits/figures';

/** The codes of a refusal by the download limits (R-LIM-020). */
export const LIMIT_REFUSALS: readonly string[] = [
  'rate_limited',
  'download_limit_reached',
  'upstream_limit_reached',
  'too_many_parallel_transfers',
  'caller_blocked',
];

/** The codes of the limits' own operations (R-LIM-031 to R-LIM-033). */
export function describeLimitsProblem(code: string | null): string | null {
  switch (code) {
    case 'limit_above_maximum':
      return $localize`:@@problem.limit_above_maximum:Some figures are above what you may set here: at most the instance's values for this caller, or the project's maximum.`;
    case 'invalid_figure':
      return $localize`:@@problem.invalid_figure:A figure is not valid: a whole number of at least 1, or empty for unlimited; a burst needs the rate and is at least the rate.`;
    case 'invalid_who':
      return $localize`:@@problem.invalid_who:This rule cannot name that caller here.`;
    case 'assignment_exists':
      return $localize`:@@problem.assignment_exists:There is a rule for this caller here already. Change that rule instead.`;
    case 'assignment_builtin':
      return $localize`:@@problem.assignment_builtin:A built-in rule can be changed, not deleted, and applies to the same callers.`;
    case 'profile_in_use':
      return $localize`:@@problem.profile_in_use:The profile is used by a rule or a project's maximum.`;
    case 'profile_builtin':
      return $localize`:@@problem.profile_builtin:A built-in profile cannot be deleted.`;
    case 'profile_immutable':
      return $localize`:@@problem.profile_immutable:Unlimited is always unlimited.`;
    case 'block_ended':
      return $localize`:@@problem.block_ended:The block has ended already.`;
    default:
      return null;
  }
}

/** "14 minutes", "1 minute", "30 seconds": the wait of a refusal. */
export function waitText(seconds: number): string {
  if (seconds >= 90) {
    const minutes = Math.round(seconds / 60);
    return $localize`:@@limits.wait.minutes:${minutes}:count: minutes`;
  }
  if (seconds >= 60) return $localize`:@@limits.minute:1 minute`;
  return seconds === 1 ? $localize`:@@limits.wait.second:1 second` : $localize`:@@limits.wait.seconds:${seconds}:count: seconds`;
}

const FIGURES: readonly string[] = [
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

/**
 * The notice of a download the limits refused (R-UI-186): the refusal in the reader's language, built from the problem's members
 * (`figure`, `limit`, `retryAfterSeconds`) where it has them, else its English `detail` without the link the API adds for a
 * client. "Your download limit of 300 GB per hour is used up. Try again in 14 minutes." A block names its end.
 */
export function limitNotice(problem: Partial<Problem> | null, retryAfter: number | null, locale: string, now = Date.now()): string {
  const code = problem?.code ?? 'rate_limited';
  const wait = typeof problem?.['retryAfterSeconds'] === 'number' ? (problem['retryAfterSeconds'] as number) : retryAfter;
  const figure = typeof problem?.['figure'] === 'string' ? (problem['figure'] as string) : null;
  const limit = typeof problem?.['limit'] === 'number' ? (problem['limit'] as number) : null;
  const again = wait !== null && wait > 0 ? ' ' + $localize`:@@limits.notice.again:Try again in ${waitText(wait)}:wait:.` : '';
  if (code === 'caller_blocked') {
    if (wait === null) return $localize`:@@limits.notice.blocked-short:Downloads are paused for you after too many refused requests.`;
    const time = formatDate(now + wait * 1000, 'shortTime', locale);
    return $localize`:@@limits.notice.blocked:Downloads are paused for you until ${time}:time: after too many refused requests.`;
  }
  const known = figure !== null && FIGURES.includes(figure) && limit !== null;
  const amount = known ? limitText(figure as Figure, limit, locale) : null;
  switch (code) {
    case 'download_limit_reached':
      if (amount) return $localize`:@@limits.notice.download:Your download limit of ${amount}:limit: is used up.` + again;
      break;
    case 'upstream_limit_reached':
      if (amount) return $localize`:@@limits.notice.upstream:Your limit of ${amount}:limit: from the upstream is used up; what is cached can still be downloaded.` + again;
      if (figure?.startsWith('ceiling.')) {
        return $localize`:@@limits.notice.ceiling:The repository's limit from the upstream for all callers like you is used up; what is cached can still be downloaded.` + again;
      }
      break;
    case 'too_many_parallel_transfers':
      if (limit !== null) {
        // Transfers, not downloads: a pull or an install of another client of the caller holds several (R-LIM-007, Q-LIM-293).
        return $localize`:@@limits.notice.parallel:You have ${limit}:count: transfers running, the most you may have at once; each layer of an image and each package of an install is one.` + again;
      }
      break;
    case 'rate_limited':
      if (amount) return $localize`:@@limits.notice.requests:Your request limit of ${amount}:limit: is used up.` + again;
      return $localize`:@@limits.notice.too-many:Too many requests.` + again;
  }
  const detail = typeof problem?.detail === 'string' ? problem.detail.replace(/\s*Your limits:.*$/s, '') : '';
  return detail || $localize`:@@limits.notice.generic:A download limit refused this download.` + again;
}
