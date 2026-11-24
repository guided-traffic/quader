import { DestroyRef, ResourceRef, Signal, effect, inject, signal, untracked } from '@angular/core';
import { PageVisibility } from './page-visibility';

/**
 * How often a live view reads again (api-and-ui.md R-UI-213, Q-UI-1075): fast while something on it runs (a scan that has not
 * answered, a download that waits or runs, a job), slow while nothing does. An idle view costs one request per `LIVE_SLOW_MS` or
 * `LIVE_IDLE_MS`; a view whose state changes only by the person's own act is not live.
 */
export const LIVE_FAST_MS = 3_000;
/** A list that a client's push or pull, a scan or another replica's work changes: the packages, the versions, the files. */
export const LIVE_SLOW_MS = 30_000;
/** A summary or a report that changes in minutes, not seconds: the dashboards of the scanning, the ClamAV databases. */
export const LIVE_IDLE_MS = 60_000;
/** The longest wait after failed reads: a read that fails doubles the wait up to this (or the view's own interval, if longer). */
export const LIVE_MAX_BACKOFF_MS = 300_000;

export interface LiveOptions {
  /**
   * Reads the view's state again, in place: what is shown stays until the answer replaces it, and a failure is thrown, so that the
   * next read waits longer. It never clears a list, closes a row or resets a field (see `refreshResource` and `PagedResource.refresh`).
   */
  readonly reload: () => Promise<unknown> | unknown;
  /**
   * The wait between two reads in milliseconds, read as a signal: `LIVE_FAST_MS` while something runs, `LIVE_SLOW_MS` or
   * `LIVE_IDLE_MS` while nothing does, or null while the view needs no read (nothing on it can change).
   */
  readonly every: () => number | null;
  /** Whether the view is shown and wants its reads (a paused refresh, a tab that is not open); true when left out. */
  readonly when?: () => boolean;
}

/** The handle of a live view. */
export interface Live {
  /** Reads at once and starts the wait anew: after the person's own act (a download started, an allow, a rescan). */
  now(): Promise<void>;
  /** The reads that failed in a row; zero after a read that answered. */
  readonly failures: Signal<number>;
}

/**
 * Makes a view live (R-UI-213): it is read again every `every()` milliseconds while the window is seen and `when()` holds, at once
 * when the window is seen again after it was hidden (or the view shown again), and a failed read waits twice as long as the one
 * before, up to `LIVE_MAX_BACKOFF_MS`. The first read is the view's own: the helper only reads again. Reads never overlap: a tick
 * that comes while one runs is skipped. Call it in an injection context (a constructor); it ends with the view.
 */
export function live(options: LiveOptions): Live {
  const visibility = inject(PageVisibility);
  const failures = signal(0);
  let timer: ReturnType<typeof setTimeout> | undefined;
  let running: Promise<void> | null = null;
  let destroyed = false;
  /** The view was not seen (a hidden window, a paused refresh) since its last read: reading again is due at once. */
  let away = false;
  /** The wait the effect last set, so that a read schedules the next with the current interval. */
  let interval: number | null = null;
  let active = false;

  const clear = (): void => {
    clearTimeout(timer);
    timer = undefined;
  };
  const delay = (): number | null => {
    if (interval === null) return null;
    const n = failures();
    if (n === 0) return interval;
    return Math.min(interval * 2 ** n, Math.max(LIVE_MAX_BACKOFF_MS, interval));
  };
  const schedule = (): void => {
    clear();
    const wait = untracked(delay);
    if (!active || destroyed || wait === null) return;
    timer = setTimeout(() => void read(), wait);
  };
  const read = async (): Promise<void> => {
    if (running) return running;
    clear();
    running = (async () => {
      try {
        await untracked(() => options.reload());
        failures.set(0);
      } catch {
        failures.update((n) => n + 1);
      } finally {
        running = null;
      }
    })();
    await running;
    schedule();
  };

  effect((onCleanup) => {
    const seen = visibility.visible() && (options.when?.() ?? true);
    const every = options.every();
    untracked(() => {
      interval = every;
      active = seen && every !== null;
      if (!seen) {
        away = true;
        clear();
        return;
      }
      if (away) {
        away = false;
        void read();
        return;
      }
      if (!running) schedule();
    });
    onCleanup(clear);
  });
  inject(DestroyRef).onDestroy(() => {
    destroyed = true;
    clear();
  });

  return {
    failures: failures.asReadonly(),
    // A read that already runs may have started before the act: the act's own read follows it.
    now: async () => {
      if (running) await running;
      await read();
    },
  };
}

/**
 * Reads a resource's value again without its loading state (R-UI-213): the shown value stays until `read` answers and is then
 * replaced with `set`, and a failure leaves it and is thrown (for the back-off of `live`). An answer is dropped when the resource
 * changed meanwhile (its parameters moved on, or another read set it), and no read starts while the resource loads for new
 * parameters.
 */
export async function refreshResource<T>(ref: ResourceRef<T>, read: () => Promise<T>): Promise<void> {
  if (untracked(() => ref.isLoading())) return;
  const before = untracked(() => (ref.hasValue() ? ref.value() : undefined));
  const value = await read();
  const now = untracked(() => (ref.hasValue() ? ref.value() : undefined));
  if (untracked(() => ref.isLoading()) || now !== before) return;
  ref.set(value);
}
