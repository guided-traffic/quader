import { ResourceRef, Signal, computed, signal, untracked } from '@angular/core';
import { ApiError } from '../api/problem';
import { DEFAULT_PAGE_LIMIT, Page, PagedList, PageFetcher, READ_ON_REQUESTS } from '../api/pagination';
import { asFailure } from '../core/http/failure';

/**
 * Wraps any failure in an `ApiError`, so that `<quader-error-notice>` can show it: an `ApiError` as it is, a failure of the page's
 * own code as such, with its message (R-UI-216; it used to say that the server could not be reached).
 */
export function asApiError(e: unknown): ApiError {
  return asFailure(e);
}

/**
 * The failure of a view's reads for its notice, and the notice's Retry (R-UI-216, Q-UI-1195): the one form in which a screen shows a
 * failed `resource()`.
 */
export interface ReadFailure extends Signal<ApiError | null> {
  /**
   * The notice's `(retry)`: reads again each read of the view that failed, and each read named in `also` that failed with it, so
   * that the page that comes back is the page of a fresh load (Q-UI-1196). A read that has its value is not read again.
   */
  retry(): void;
}

/** The reads a notice's Retry reads again with the view's own, without showing their failure in its place. */
export interface ReadFailureOptions {
  /**
   * The page's other reads whose failure has no notice of its own here, or whose notice is not shown while the view failed (the
   * formats of a repository's page): read once and never again, the page that came back would show their empty or wrong state.
   */
  readonly also?: readonly ResourceRef<unknown>[];
}

/**
 * The failure of a view's reads for its notice (R-UI-216, Q-UI-1184, Q-UI-1195): the first failure of `reads` in their order, kept
 * while a read that failed reads again, so the notice stays in place during a retry instead of giving way to "Loading." and coming
 * back: it says that it tries again and counts its attempts. A value or a failure of the new read replaces it. Its `retry` reads
 * again what failed, `also` included.
 */
export function failureOf(reads: ResourceRef<unknown> | readonly ResourceRef<unknown>[], options: ReadFailureOptions = {}): ReadFailure {
  const refs: readonly ResourceRef<unknown>[] = Array.isArray(reads) ? reads : [reads as ResourceRef<unknown>];
  const also = options.also ?? [];
  let last: ApiError | null = null;
  let raw: unknown = undefined;
  const failure = computed(() => {
    for (const ref of refs) {
      const e = ref.error();
      if (!e) continue;
      // The same failure is the same notice: a page's own fault would otherwise become a new error at every change of another read.
      if (e !== raw || last === null) {
        raw = e;
        last = asFailure(e);
      }
      return last;
    }
    if (last && refs.some((ref) => ref.isLoading() && !ref.hasValue())) return last;
    raw = undefined;
    return (last = null);
  });
  return Object.assign(failure, {
    retry: (): void => {
      for (const ref of [...refs, ...also]) if (untracked(() => ref.error())) ref.reload();
    },
  });
}

/**
 * The value of a resource, or undefined while it loads or after it failed. `resource.value()`
 * throws in the error state, which a template that shows the failure next to the rest must avoid.
 */
export function valueOf<T>(r: ResourceRef<T>): Exclude<T, undefined> | undefined {
  return r.hasValue() ? (r.value() as Exclude<T, undefined>) : undefined;
}

/**
 * A list with a "Load more" button (R-UI-006, R-API-010) as signals. `start` begins a new
 * traversal, which is how a filter or sort change restarts: a cursor is bound to its filters.
 * An answer of an earlier traversal that arrives late is dropped.
 */
export class PagedResource<T> {
  readonly items = signal<readonly T[]>([]);
  readonly loading = signal(false);
  readonly error = signal<ApiError | null>(null);
  /** True once the first page has arrived. */
  readonly loaded = signal(false);
  readonly hasMore = signal(false);

  private list: PagedList<T> | null = null;
  private fetcher: PageFetcher<T> | null = null;
  private limit: number | undefined;
  private generation = 0;

  /** `limit` is the page size of every traversal that does not pass its own. */
  constructor(private readonly defaultLimit?: number) {}

  /**
   * Starts a new traversal and loads its first page. A failure of the traversal before stays until the first page answers (R-UI-216,
   * Q-UI-1195): the list's notice stays in place while it reads again, instead of giving way to "Loading." and coming back.
   */
  start(fetcher: PageFetcher<T>, limit: number | undefined = this.defaultLimit): Promise<void> {
    this.fetcher = fetcher;
    this.limit = limit;
    this.list = new PagedList(fetcher, limit);
    this.items.set([]);
    this.loaded.set(false);
    this.hasMore.set(false);
    this.generation++;
    this.loading.set(false);
    return this.more();
  }

  /** Starts the current traversal again from the first page. */
  async reload(): Promise<void> {
    if (this.fetcher) await this.start(this.fetcher, this.limit);
  }

  /**
   * Reads the rows shown again in place (R-UI-213): the pages of the current traversal from the first, until as many rows as are shown
   * have been read again or the list ends, and then replaces the rows at once. Nothing is cleared while it reads, so the rows, the
   * open rows (kept by the screen per row's key), the focus and the scroll stay; a row that is gone goes, a new one appears in its
   * place. A failure leaves the rows and the error as they are and is thrown (the back-off of `live`). An answer is dropped when a
   * new traversal or **Load more** started meanwhile; no read starts while one of those runs or before the first page arrived. `next`
   * is the same query asked anew, for a query whose bounds move with the clock; it becomes the traversal's.
   */
  async refresh(next?: PageFetcher<T>): Promise<void> {
    if (!this.fetcher || this.loading() || !this.loaded()) return;
    // A list whose query moves with the clock (the audit log's last 24 hours) reads with the query of now, the same filters.
    const fetcher = next ?? this.fetcher;
    const generation = this.generation;
    const list = this.list;
    const before = this.items();
    const shown = before.length;
    const limit = this.limit ?? DEFAULT_PAGE_LIMIT;
    const items: T[] = [];
    let cursor: string | null = null;
    const seen = new Set<string>();
    // The pages shown are read again whole, so a new row within them appears. A short page with a cursor is read on (R-API-010), each
    // request for the rows its page still misses, so a page read again never holds more rows than a page, and with no more requests
    // than `READ_ON_REQUESTS` per page.
    const pagesShown = Math.max(1, Math.ceil(shown / limit));
    const target = pagesShown * limit;
    let requests = 0;
    do {
      const page: Page<T> = await fetcher(cursor, Math.min(limit, target - items.length));
      requests++;
      if (generation !== this.generation || list !== this.list || this.loading() || this.items() !== before) return;
      items.push(...page.items);
      cursor = page.nextCursor;
      if (cursor !== null) {
        if (seen.has(cursor)) throw new Error('pagination: the server repeated a cursor');
        seen.add(cursor);
      }
    } while (cursor !== null && items.length < target && requests < READ_ON_REQUESTS * pagesShown);
    this.fetcher = fetcher;
    this.list = PagedList.resumed(fetcher, limit, items, cursor);
    this.items.set(items);
    this.hasMore.set(cursor !== null);
    this.error.set(null);
  }

  /** Loads the next page of the current traversal; a failure before stays until the page answers (R-UI-216). */
  async more(): Promise<void> {
    const list = this.list;
    if (!list || this.loading()) return;
    const generation = this.generation;
    this.loading.set(true);
    try {
      const items = await list.loadMore();
      if (generation !== this.generation) return;
      this.error.set(null);
      this.items.set(items);
      this.hasMore.set(list.hasMore);
      this.loaded.set(true);
    } catch (e) {
      if (generation === this.generation) this.error.set(asApiError(e));
    } finally {
      if (generation === this.generation) this.loading.set(false);
    }
  }

  /** Replaces one item in place, e.g. after a toggle. */
  replace(match: (item: T) => boolean, next: T): void {
    this.items.update((list) => list.map((i) => (match(i) ? next : i)));
  }

  /** Drops items, e.g. after a delete. */
  remove(match: (item: T) => boolean): void {
    this.items.update((list) => list.filter((i) => !match(i)));
  }
}
