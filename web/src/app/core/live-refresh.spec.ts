import { Component, resource, signal } from '@angular/core';
import { TestBed } from '@angular/core/testing';
import { vi } from 'vitest';
import { Page } from '../api/pagination';
import { PagedResource, valueOf } from '../shared/paged-resource';
import { settle } from '../testing/screen';
import { LIVE_MAX_BACKOFF_MS, Live, live, refreshResource } from './live-refresh';

function setVisibility(state: 'visible' | 'hidden'): void {
  Object.defineProperty(document, 'visibilityState', { configurable: true, get: () => state });
  document.dispatchEvent(new Event('visibilitychange'));
}

/** A view that is live: `reads` counts its reads, `fail` makes them fail, `every` and `shown` steer it. */
@Component({
  selector: 'quader-live-host',
  template: `
    <input aria-label="Filter" class="filter" />
    <ul>
      @for (row of list.items(); track row.id) {
        <li [attr.data-id]="row.id" [class.open]="open().has(row.id)">{{ row.name }}</li>
      }
    </ul>
    <p class="value">{{ value() }}</p>
  `,
})
class LiveHost {
  readonly every = signal<number | null>(1_000);
  readonly shown = signal(true);
  reads = 0;
  fail = false;
  rows: { id: string; name: string }[] = [];
  /** While set, a page answers only when it settles: a read that is still under way. */
  gate: Promise<void> | null = null;
  readonly open = signal<ReadonlySet<string>>(new Set());
  readonly list = new PagedResource<{ id: string; name: string }>(2);
  readonly fetcher = async (cursor: string | null, limit: number): Promise<Page<{ id: string; name: string }>> => {
    const rows = this.rows;
    if (this.gate) await this.gate;
    const start = cursor === null ? 0 : Number(cursor);
    const items = rows.slice(start, start + limit);
    const end = start + items.length;
    return { items, nextCursor: end < rows.length ? String(end) : null };
  };
  readonly source = signal('one');
  readonly loaded = resource({ params: () => this.source(), loader: async ({ params }) => `${params} as read` });
  protected readonly value = () => valueOf(this.loaded) ?? '';
  readonly handle: Live = live({
    reload: async () => {
      this.reads++;
      if (this.fail) throw new Error('down');
      await this.list.refresh();
    },
    every: () => this.every(),
    when: () => this.shown(),
  });
}

async function host() {
  TestBed.resetTestingModule();
  await TestBed.configureTestingModule({ imports: [LiveHost] }).compileComponents();
  const fixture = TestBed.createComponent(LiveHost);
  const view = fixture.componentInstance;
  view.rows = [
    { id: 'a', name: 'alpha' },
    { id: 'b', name: 'beta' },
    { id: 'c', name: 'gamma' },
  ];
  await view.list.start(view.fetcher);
  await settle(fixture);
  return { fixture, view, root: fixture.nativeElement as HTMLElement };
}

async function advance(fixture: Awaited<ReturnType<typeof host>>['fixture'], ms: number): Promise<void> {
  vi.advanceTimersByTime(ms);
  await settle(fixture);
}

describe('A live view', () => {
  beforeEach(() => vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] }));
  afterEach(() => {
    vi.useRealTimers();
    setVisibility('visible');
  });

  it('R-UI-213 is read again every interval while the window is seen, not while it is hidden, and at once when it is seen again', async () => {
    const { fixture, view } = await host();
    expect(view.reads).toBe(0);
    await advance(fixture, 1_000);
    expect(view.reads).toBe(1);
    await advance(fixture, 1_000);
    expect(view.reads).toBe(2);
    setVisibility('hidden');
    await settle(fixture);
    await advance(fixture, 10_000);
    expect(view.reads).toBe(2);
    setVisibility('visible');
    await settle(fixture);
    expect(view.reads).toBe(3);
    await advance(fixture, 1_000);
    expect(view.reads).toBe(4);
  });

  it('R-UI-213 waits twice as long after each failed read, up to the bound, and the interval again after one that answers', async () => {
    const { fixture, view } = await host();
    view.fail = true;
    await advance(fixture, 1_000);
    expect(view.reads).toBe(1);
    expect(view.handle.failures()).toBe(1);
    await advance(fixture, 1_999);
    expect(view.reads).toBe(1);
    await advance(fixture, 1);
    expect(view.reads).toBe(2);
    await advance(fixture, 4_000);
    expect(view.reads).toBe(3);
    view.fail = false;
    await advance(fixture, 8_000);
    expect(view.reads).toBe(4);
    expect(view.handle.failures()).toBe(0);
    await advance(fixture, 1_000);
    expect(view.reads).toBe(5);

    view.fail = true;
    view.every.set(100_000);
    await settle(fixture);
    for (let i = 0; i < 4; i++) await advance(fixture, LIVE_MAX_BACKOFF_MS);
    // 100 s, then 200 s, then the bound of 300 s: never longer, however many reads failed.
    expect(view.handle.failures()).toBeGreaterThanOrEqual(3);
    const before = view.reads;
    await advance(fixture, LIVE_MAX_BACKOFF_MS);
    expect(view.reads).toBe(before + 1);
  });

  it('R-UI-213 follows the interval the view asks for: fast while something runs, none while nothing can change', async () => {
    const { fixture, view } = await host();
    view.every.set(200);
    await settle(fixture);
    await advance(fixture, 200);
    expect(view.reads).toBe(1);
    view.every.set(null);
    await settle(fixture);
    await advance(fixture, 60_000);
    expect(view.reads).toBe(1);
    view.every.set(1_000);
    await settle(fixture);
    await advance(fixture, 1_000);
    expect(view.reads).toBe(2);
  });

  it('R-UI-213 reads nothing while the view is not shown or its refresh is paused, and at once when it is shown again', async () => {
    const { fixture, view } = await host();
    view.shown.set(false);
    await settle(fixture);
    await advance(fixture, 5_000);
    expect(view.reads).toBe(0);
    view.shown.set(true);
    await settle(fixture);
    expect(view.reads).toBe(1);
  });

  it("R-UI-213 reads at once after the person's own act and starts the wait anew", async () => {
    const { fixture, view } = await host();
    await advance(fixture, 600);
    await view.handle.now();
    await settle(fixture);
    expect(view.reads).toBe(1);
    await advance(fixture, 600);
    expect(view.reads).toBe(1);
    await advance(fixture, 400);
    expect(view.reads).toBe(2);
  });

  it('R-UI-213 a read keeps the rows, the open row, the focus and what is typed, and shows what changed in place', async () => {
    const { fixture, view, root } = await host();
    view.open.set(new Set(['b']));
    await settle(fixture);
    const filter = root.querySelector<HTMLInputElement>('input.filter')!;
    filter.focus();
    filter.value = 'be';
    const beta = root.querySelector('li[data-id="b"]');
    // A client pushed delta; beta's name changed.
    view.rows = [
      { id: 'a', name: 'alpha' },
      { id: 'b', name: 'beta 2' },
      { id: 'c', name: 'gamma' },
      { id: 'd', name: 'delta' },
    ];
    await advance(fixture, 1_000);
    expect(view.reads).toBe(1);
    // The list showed its first page of two: the read covers those two rows again, and Load more stays.
    const rows = [...root.querySelectorAll('li')];
    expect(rows.map((r) => r.textContent?.trim())).toEqual(['alpha', 'beta 2']);
    expect(root.querySelector('li[data-id="b"]')).toBe(beta);
    expect(beta?.classList.contains('open')).toBe(true);
    expect(document.activeElement).toBe(filter);
    expect(filter.value).toBe('be');
    // Load more continues after what the read covered: gamma and delta, once each.
    expect(view.list.hasMore()).toBe(true);
    await view.list.more();
    await settle(fixture);
    expect([...root.querySelectorAll('li')].map((r) => r.dataset['id'])).toEqual(['a', 'b', 'c', 'd']);
  });

  it('R-UI-213 a failed read keeps the rows and shows no error', async () => {
    const { fixture, view, root } = await host();
    view.fail = true;
    await advance(fixture, 1_000);
    expect([...root.querySelectorAll('li')].map((r) => r.dataset['id'])).toEqual(['a', 'b']);
    expect(view.list.error()).toBeNull();
  });

  it('R-UI-213 a read of a list that a new filter restarted meanwhile is dropped', async () => {
    const { view } = await host();
    let release: () => void = () => undefined;
    view.gate = new Promise<void>((resolve) => (release = resolve));
    const refreshing = view.list.refresh();
    view.gate = null;
    view.rows = [{ id: 'x', name: 'the filtered row' }];
    await view.list.start(view.fetcher);
    release();
    await refreshing;
    expect(view.list.items().map((r) => r.id)).toEqual(['x']);
  });

  it('R-UI-213 R-API-010 a list read again reads on past a short and an empty page, for the rows its page still misses, and holds no more rows than its pages', async () => {
    const rows = Array.from({ length: 30 }, (_, i) => `r${i}`);
    const sizes: (number | undefined)[] = [];
    const asked: number[] = [];
    const list = new PagedResource<string>(10);
    await list.start(async (cursor, limit) => {
      asked.push(limit);
      // The cursor is the position and the request, so an empty page's cursor is a new one, as the server's walk is.
      const start = Number.parseInt(cursor ?? '0', 10);
      const end = start + Math.min(sizes.shift() ?? limit, limit);
      return { items: rows.slice(start, end), nextCursor: end < rows.length ? `${end}.${asked.length}` : null };
    });
    expect(list.items()).toHaveLength(10);
    sizes.push(3, 0);
    asked.length = 0;
    await list.refresh();
    expect(asked).toEqual([10, 7, 7]);
    expect(list.items()).toEqual(rows.slice(0, 10));
    expect(list.hasMore()).toBe(true);
  });

  it('R-UI-213 a resource read again keeps its value until the answer, keeps it on a failure, and drops a stale answer', async () => {
    const { fixture, view, root } = await host();
    expect(root.querySelector('.value')?.textContent).toBe('one as read');
    await refreshResource(view.loaded, async () => 'one again');
    await settle(fixture);
    expect(root.querySelector('.value')?.textContent).toBe('one again');
    await expect(refreshResource(view.loaded, () => Promise.reject(new Error('down')))).rejects.toThrow('down');
    await settle(fixture);
    expect(root.querySelector('.value')?.textContent).toBe('one again');
    let answer: (v: string) => void = () => undefined;
    const pending = refreshResource(view.loaded, () => new Promise<string>((r) => (answer = r)));
    view.source.set('two');
    await settle(fixture);
    answer('one, late');
    await pending;
    await settle(fixture);
    expect(root.querySelector('.value')?.textContent).toBe('two as read');
  });
});
