/** The envelope of every list endpoint (R-API-010). */
export interface Page<T> {
  readonly items: readonly T[];
  /** Opaque; null on the last page. */
  readonly nextCursor: string | null;
  /** Present only where the endpoint accepts `includeTotal=true`. */
  readonly total?: number;
}

/** Default and maximum page size (R-API-010, `api.page_size_*`). */
export const DEFAULT_PAGE_LIMIT = 50;
export const MAX_PAGE_LIMIT = 500;

/** Fetches one page; `cursor` is null for the first. */
export type PageFetcher<T> = (cursor: string | null, limit: number) => Promise<Page<T>>;

/**
 * The most requests one page of a list takes (R-API-010): a page may be short, even empty, while its cursor is set (a list by content
 * whose read budget ended, Q-API-432), and a list reads on until it has its page or the list ends, but never more than this often. A
 * list that stops at the bound keeps its cursor: **Load more** goes on from there.
 */
export const READ_ON_REQUESTS = 8;

/**
 * Walks from the first page to the last and returns every item. The cursor is never inspected
 * (R-API-010: opaque); a repeated cursor ends the walk with an error so that a misbehaving
 * server cannot make the UI loop forever.
 */
export async function collectAll<T>(
  fetchPage: PageFetcher<T>,
  limit: number = MAX_PAGE_LIMIT,
): Promise<T[]> {
  const items: T[] = [];
  const seen = new Set<string>();
  let cursor: string | null = null;
  do {
    const page: Page<T> = await fetchPage(cursor, limit);
    items.push(...page.items);
    cursor = page.nextCursor;
    if (cursor !== null) {
      if (seen.has(cursor)) throw new Error('pagination: the server repeated a cursor');
      seen.add(cursor);
    }
  } while (cursor !== null);
  return items;
}

/**
 * Accumulating list for a "Load more" button (R-UI-006): keeps the items of the pages loaded so
 * far and the cursor of the next one. A new instance starts a new traversal, which is also how a
 * filter or sort change restarts (a cursor is bound to its filters and sort).
 */
export class PagedList<T> {
  private nextCursor: string | null = null;
  private started = false;
  private _items: readonly T[] = [];

  constructor(
    private readonly fetchPage: PageFetcher<T>,
    private readonly limit: number = DEFAULT_PAGE_LIMIT,
  ) {}

  /**
   * A traversal that has read `items` up to `nextCursor` already: a list read again in place (R-UI-213) continues from where the
   * new read stopped, so **Load more** after it neither repeats nor skips a row.
   */
  static resumed<T>(fetchPage: PageFetcher<T>, limit: number, items: readonly T[], nextCursor: string | null): PagedList<T> {
    const list = new PagedList(fetchPage, limit);
    list.started = true;
    list._items = items;
    list.nextCursor = nextCursor;
    return list;
  }

  get items(): readonly T[] {
    return this._items;
  }

  /** True before the first load and while a next page exists. */
  get hasMore(): boolean {
    return !this.started || this.nextCursor !== null;
  }

  /**
   * Loads the next page and returns the whole list so far. A short or empty answer with a cursor is read on (R-API-010), each request
   * asking only for the rows still missing, until the page is whole, the list ends or `READ_ON_REQUESTS` requests were made; the rows
   * are added once, at the end, so a list shown from them never shows a part of the page.
   */
  async loadMore(): Promise<readonly T[]> {
    if (!this.hasMore) return this._items;
    const added: T[] = [];
    let requests = 0;
    do {
      const page = await this.fetchPage(this.nextCursor, this.limit - added.length);
      this.started = true;
      this.nextCursor = page.nextCursor;
      added.push(...page.items);
      requests++;
    } while (this.nextCursor !== null && added.length < this.limit && requests < READ_ON_REQUESTS);
    this._items = [...this._items, ...added];
    return this._items;
  }
}
