import { Page, PagedList, READ_ON_REQUESTS, collectAll } from './pagination';

function pages(...items: string[][]): (cursor: string | null) => Promise<Page<string>> {
  return async (cursor) => {
    const index = cursor === null ? 0 : Number(cursor);
    return { items: items[index] ?? [], nextCursor: index + 1 < items.length ? String(index + 1) : null };
  };
}

describe('pagination', () => {
  it('R-API-010 collectAll walks to the last page', async () => {
    expect(await collectAll(pages(['a', 'b'], ['c'], ['d']))).toEqual(['a', 'b', 'c', 'd']);
  });

  it('R-API-010 collectAll stops on a repeated cursor instead of looping', async () => {
    const loop = async (): Promise<Page<string>> => ({ items: ['x'], nextCursor: 'same' });
    await expect(collectAll(loop)).rejects.toThrow('repeated a cursor');
  });

  it('R-API-010 PagedList reads on after a short and an empty page with a cursor, asking only for the rows still missing, and adds them at once', async () => {
    const asked: number[] = [];
    const answers: Page<string>[] = [
      { items: ['a', 'b', 'c'], nextCursor: '1' },
      { items: [], nextCursor: '2' },
      { items: ['d', 'e', 'f', 'g', 'h', 'i', 'j'], nextCursor: '3' },
    ];
    const list = new PagedList<string>(async (_cursor, limit) => {
      asked.push(limit);
      // While it reads on, nothing of the page is in the list yet.
      expect(list.items).toEqual([]);
      return answers.shift()!;
    }, 10);
    expect(await list.loadMore()).toEqual(['a', 'b', 'c', 'd', 'e', 'f', 'g', 'h', 'i', 'j']);
    expect(asked).toEqual([10, 7, 7]);
    expect(list.hasMore).toBe(true);
  });

  it('R-API-010 PagedList stops reading on after eight requests and keeps its cursor for the next page', async () => {
    let n = 0;
    const list = new PagedList<string>(async () => ({ items: n === 9 ? ['z'] : [], nextCursor: String(++n) }), 10);
    expect(await list.loadMore()).toEqual([]);
    expect(n).toBe(READ_ON_REQUESTS);
    expect(list.hasMore).toBe(true);
  });

  it('R-UI-006 PagedList loads more until nextCursor is null', async () => {
    const list = new PagedList(pages(['a'], ['b']), 1);
    expect(list.hasMore).toBe(true);
    expect(await list.loadMore()).toEqual(['a']);
    expect(list.hasMore).toBe(true);
    expect(await list.loadMore()).toEqual(['a', 'b']);
    expect(list.hasMore).toBe(false);
    expect(await list.loadMore()).toEqual(['a', 'b']);
  });
});
