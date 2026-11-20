import { isUpstreamName, isUpstreamProject, upstreamFirst, upstreamKeyOf, upstreamName } from './upstream-project';

describe('The built-in project upstream (R-UI-190)', () => {
  it('R-UI-190 is recognised by its key alone, and its repositories by the qualified name upstream/<key>', () => {
    expect(isUpstreamProject('upstream')).toBe(true);
    expect(isUpstreamProject('upstream-mirror')).toBe(false);
    expect(isUpstreamProject(null)).toBe(false);
    expect(isUpstreamName('upstream/npmjs')).toBe(true);
    expect(isUpstreamName('web/npm-local')).toBe(false);
    expect(upstreamKeyOf('upstream/docker-hub')).toBe('docker-hub');
    expect(upstreamKeyOf('npm-local')).toBeNull();
    expect(upstreamName('npmjs')).toBe('upstream/npmjs');
  });

  it('R-UI-190 a list of projects keeps upstream first and every other entry in its own order, whatever that order is', () => {
    const key = (p: { key: string }) => p.key;
    const sorted = [{ key: 'alpha' }, { key: 'upstream' }, { key: 'web' }];
    expect(upstreamFirst(sorted, key).map(key)).toEqual(['upstream', 'alpha', 'web']);
    const reversed = [{ key: 'web' }, { key: 'upstream' }, { key: 'alpha' }];
    expect(upstreamFirst(reversed, key).map(key)).toEqual(['upstream', 'web', 'alpha']);
    // A filter that took it out leaves it out.
    expect(upstreamFirst([{ key: 'web' }, { key: 'alpha' }], key).map(key)).toEqual(['web', 'alpha']);
  });
});
