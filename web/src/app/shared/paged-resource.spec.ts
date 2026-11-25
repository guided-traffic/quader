import { ResourceRef, resource } from '@angular/core';
import { TestBed } from '@angular/core/testing';
import { ApiError } from '../api/problem';
import { PagedResource, failureOf } from './paged-resource';

/** A read whose answers the test gives one by one: each call waits for `answer` or `fail`. */
class Read<T> {
  calls = 0;
  private pending: { resolve: (v: T) => void; reject: (e: unknown) => void } | null = null;
  readonly load = (): Promise<T> => {
    this.calls++;
    return new Promise<T>((resolve, reject) => (this.pending = { resolve, reject }));
  };
  answer(value: T): void {
    this.pending?.resolve(value);
    this.pending = null;
  }
  fail(e: unknown): void {
    this.pending?.reject(e);
    this.pending = null;
  }
}

const gateway = (): ApiError => new ApiError(502, null, null, null, { call: { method: 'GET', path: '/api/v1/formats' } });

/** Lets the resources and the promises settle. */
async function settle(): Promise<void> {
  for (let i = 0; i < 4; i++) {
    TestBed.tick();
    await Promise.resolve();
    await new Promise((r) => setTimeout(r));
  }
}

describe('the failure of a read for its notice', () => {
  it('R-UI-216 a failed resource keeps its failure while it reads again, and its Retry reads the page\'s other failed reads with it', async () => {
    const page = new Read<string>();
    const formats = new Read<string[]>();
    const address = new Read<string>();
    const { loaded, others, again, failure } = TestBed.runInInjectionContext(() => {
      const loaded = resource({ loader: page.load });
      const others = resource({ loader: formats.load });
      const again = resource({ loader: address.load });
      return { loaded, others, again, failure: failureOf(loaded, { also: [others as ResourceRef<unknown>, again as ResourceRef<unknown>] }) };
    });
    await settle();
    const first = gateway();
    page.fail(first);
    formats.fail(gateway());
    address.answer('https://quader.example.com');
    await settle();
    expect(failure()).toBe(first);

    failure.retry();
    await settle();
    // The page and the formats read again, the address that answered does not; the notice's failure stays the same one meanwhile.
    expect([page.calls, formats.calls, address.calls]).toEqual([2, 2, 1]);
    expect(loaded.isLoading()).toBe(true);
    expect(failure()).toBe(first);

    const second = gateway();
    page.fail(second);
    await settle();
    expect(failure()).toBe(second);

    failure.retry();
    await settle();
    page.answer('npm-local');
    formats.answer(['npm']);
    await settle();
    expect(failure()).toBeNull();
    expect(others.value()).toEqual(['npm']);
    expect(again.value()).toBe('https://quader.example.com');
  });

  it('R-UI-216 a list keeps its failure while it starts again and clears it with the first page', async () => {
    const pages = new Read<{ items: string[]; nextCursor: string | null }>();
    const list = new PagedResource<string>();
    void list.start(pages.load);
    const failed = gateway();
    pages.fail(failed);
    await settle();
    expect(list.error()).toBe(failed);

    void list.reload();
    expect(list.loading()).toBe(true);
    expect(list.error()).toBe(failed);
    pages.answer({ items: ['a'], nextCursor: null });
    await settle();
    expect(list.error()).toBeNull();
    expect(list.items()).toEqual(['a']);
  });
});
