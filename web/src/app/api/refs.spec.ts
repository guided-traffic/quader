import { repositoryOfContent } from './refs';

// The repository of a version's file address (R-API-025, and api-and-ui.md R-UI-229 for a Bastion, whose catalogue writes the package
// route as the file's contentUrl, bastion.md Q-BAS-398). Strings alone; milliseconds.

describe('The repository of a file address', () => {
  it("R-UI-229 a file's download is the Bastion's package route, which names the repository that holds it", () => {
    expect(repositoryOfContent('/api/npm/platform/npm-open-source/config-merge/-/config-merge-2.3.0.tgz')).toBe('platform/npm-open-source');
    expect(repositoryOfContent('/files/platform/tools/bin/agent')).toBe('platform/tools');
    expect(repositoryOfContent('/v2/platform/image-cache/vendor/tools/agent/manifests/sha256:ab')).toBe('platform/image-cache');
    expect(repositoryOfContent('/api/nuget/platform/nuget-internal/v3/flatcontainer/a/1.0.0/a.1.0.0.nupkg')).toBe('platform/nuget-internal');
  });

  it('R-UI-229 a content route of the main installation names its repository, a path of the management API none', () => {
    expect(repositoryOfContent('/api/v1/projects/platform/repositories/npm-open-source/content/x.tgz')).toBe('platform/npm-open-source');
    expect(repositoryOfContent('/api/v1/projects/my%20p/repositories/r/content/x')).toBe('my p/r');
    expect(repositoryOfContent('/api/v1/packages/x')).toBeNull();
    expect(repositoryOfContent('/ui/data/v1/packages/x')).toBeNull();
  });
});
