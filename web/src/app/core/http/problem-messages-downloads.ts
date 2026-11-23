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
    default:
      return null;
  }
}
