import { passes } from '../shared/error-notice';
import { asFailure } from './http/failure';
import { LIVE_FAST_MS, LIVE_MAX_BACKOFF_MS } from './live-refresh';

/**
 * A root store's read again after its read failed (R-UI-216): the store reads once per page load and keeps the answer, so a read that
 * failed while the backend restarted left its screens with the store's "not known" (the malware scanning off in the navigation, the
 * robot for an uploaded avatar) until the page was loaded again. After a failure that may pass (`passes`: no answer, a gateway's, a
 * server's) it reads again by itself with the notice's back-off: 3 s after the first failure, twice as long after each further one, up
 * to 5 minutes; an answer ends it, and a refusal is not read again. Never more than one read is waiting, and none while the window is
 * hidden: a read that falls due then waits until the window is seen again (R-UI-213).
 */
export class ReadAgain {
  private failures = 0;
  private timer: ReturnType<typeof setTimeout> | undefined;
  /** The wait for the window to be seen again, of a read that fell due while it was hidden. */
  private unseen: (() => void) | undefined;

  constructor(private readonly read: () => unknown) {}

  /** The read failed: the next one waits for the back-off, if the failure may pass. */
  failed(e: unknown): void {
    clearTimeout(this.timer);
    this.timer = undefined;
    if (this.unseen) document.removeEventListener('visibilitychange', this.unseen);
    this.unseen = undefined;
    if (!passes(asFailure(e))) return;
    const wait = Math.min(LIVE_FAST_MS * 2 ** this.failures, LIVE_MAX_BACKOFF_MS);
    this.failures++;
    this.timer = setTimeout(() => {
      this.timer = undefined;
      this.whenSeen();
    }, wait);
  }

  /** Reads now, or once the window is seen again. */
  private whenSeen(): void {
    if (typeof document === 'undefined' || document.visibilityState !== 'hidden') {
      void this.read();
      return;
    }
    const seen = (): void => {
      if (document.visibilityState === 'hidden') return;
      document.removeEventListener('visibilitychange', seen);
      this.unseen = undefined;
      void this.read();
    };
    this.unseen = seen;
    document.addEventListener('visibilitychange', seen);
  }


  /** The read answered, or the store was emptied: nothing waits any more. */
  answered(): void {
    this.failures = 0;
    clearTimeout(this.timer);
    this.timer = undefined;
    if (this.unseen) document.removeEventListener('visibilitychange', this.unseen);
    this.unseen = undefined;
  }
}
