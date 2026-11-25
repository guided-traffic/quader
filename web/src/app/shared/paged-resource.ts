import { ResourceRef, Signal, computed, signal } from '@angular/core';
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
 * The failure of a resource for its notice, kept while the resource reads again (R-UI-216, Q-UI-1184): the notice stays in place
 * during a retry instead of giving way to "Loading." and coming back, so it can say that it tries again and count its attempts. A
 * value or a failure of the new read replaces it.
 */
export function failureOf(ref: ResourceRef<unknown>): Signal<ApiError | null> {
  let last: ApiError | null = null;
  return computed(() => {
    const e = ref.error();
    if (e) return (last = asFailure(e));
    if (last && ref.isLoading() && !ref.hasValue()) return last;
    return (last = null);
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

  /** Starts a new traversal and loads its first page. */
  start(fetcher: PageFetcher<T>, limit: number | undefined = this.defaultLimit): Promise<void> {
    this.fetcher = fetcher;
    this.limit = limit;
    this.list = new PagedList(fetcher, limit);
    this.error.set(null);
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

  /** Loads the next page of the current traversal. */
  async more(): Promise<void> {
    const list = this.list;
    if (!list || this.loading()) return;
    const generation = this.generation;
    this.loading.set(true);
    this.error.set(null);
    try {
      const items = await list.loadMore();
      if (generation !== this.generation) return;
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
