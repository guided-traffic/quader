/**
 * The codes of a remote repository's download queue (remote-curation.md R-CUR-010 to R-CUR-012): the refusals a screen meets when it
 * starts, retries or cancels a download ahead of a client.
 */
export function describeDownloadsProblem(code: string | null): string | null {
  switch (code) {
    case 'not_retryable':
      return $localize`:@@problem.not_retryable:Only a failed or cancelled download can be retried. Load the list again to see its state.`;
    case 'already_queued':
      return $localize`:@@problem.already_queued:This version waits or runs in another download already.`;
    case 'download_queue_full':
      return $localize`:@@problem.download_queue_full:The queue of this repository is full. Try again when some of its downloads have run.`;
    // >>> T-24 upstream
    case 'catalogue_not_offered':
      return $localize`:@@problem.catalogue_not_offered:The upstream of this repository does not answer this.`;
    case 'catalogue_not_found':
      return $localize`:@@problem.catalogue_not_found:The upstream has no package or version of this name.`;
    case 'catalogue_budget_exhausted':
      return $localize`:@@problem.catalogue_budget_exhausted:The upstream catalogue's budget is used up. Try again in a minute.`;
    case 'catalogue_answer_too_large':
      return $localize`:@@problem.catalogue_answer_too_large:The upstream's answer is larger than the catalogue reads.`;
    case 'upstream_assumed_offline':
      return $localize`:@@problem.upstream_assumed_offline:The upstream is assumed offline after repeated failures; Quader retries later.`;
    case 'too_many_rules':
      return $localize`:@@problem.too_many_rules:This repository has as many rules as it may hold. Remove some, or allow a sub-section instead.`;
    case 'invalid_version':
      return $localize`:@@problem.invalid_version:This version is not one the format reads.`;
    case 'already_delivered':
      return $localize`:@@problem.already_delivered:This is delivered in this repository already; there is nothing to ask for.`;
    case 'too_many_requests':
      return $localize`:@@problem.too_many_requests:You have as many open requests in this repository as you may. Withdraw one, or wait for a decision.`;
    case 'delivered':
      return $localize`:@@problem.delivered:A version the rules deliver is not discarded.`;
    case 'not_allowed_in_repository':
      return $localize`:@@problem.not_allowed_in_repository:This repository does not deliver it: a rule blocks it, or it delivers only what is allowed.`;
    // <<< T-24 upstream
    default:
      return null;
  }
}
