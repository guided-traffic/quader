import { orderTags, shownDistTags, tagsByVersion } from './dist-tags';

describe('The npm tags of a version', () => {
  it('R-UI-110 R-CUR-018 a version shows latest first, the other tags by name, at most three and the rest behind "+N more"', () => {
    expect(orderTags(['next', 'beta', 'latest'])).toEqual(['latest', 'beta', 'next']);
    expect(tagsByVersion({ next: '2.0.0', latest: '1.0.0', beta: '2.0.0' })).toEqual(
      new Map([
        ['2.0.0', ['beta', 'next']],
        ['1.0.0', ['latest']],
      ]),
    );
    expect(tagsByVersion(undefined).size).toBe(0);
    expect(shownDistTags(['rc', 'next', 'canary', 'beta', 'latest'], 3)).toEqual({ shown: ['latest', 'beta', 'canary'], hidden: ['next', 'rc'] });
    expect(shownDistTags(['next', 'latest'], 3)).toEqual({ shown: ['latest', 'next'], hidden: [] });
    expect(shownDistTags(['b', 'a', 'c', 'd'], null)).toEqual({ shown: ['a', 'b', 'c', 'd'], hidden: [] });
  });
});
