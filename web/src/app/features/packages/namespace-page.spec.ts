import { describe, expect, it } from 'vitest';
import { PackageSummary, PackagesApi } from '../../api/packages-api';
import { RepositoriesApi } from '../../api/repositories-api';
import { FakePackagesApi } from '../../api/testing/fakes-packages';
import { ANSIBLE_FORMAT, FakeRepositoriesApi, OCI_FORMAT, PACKAGE_FORMAT } from '../../api/testing/fakes-catalog';
import { expectAccessible, renderScreen, settle, text } from '../../testing/screen';
import { NamespacePage } from './namespace-page';
import { isNamespace, nameParts, namespaceCommands, namespacesOf } from './namespaces';
import { allNamespacesMatcher, namespaceMatcher } from './packages.routes';
import { UrlSegment, UrlSegmentGroup, Route } from '@angular/router';

const pkg = (format: string, name: string, extra: Partial<PackageSummary> = {}): PackageSummary => ({
  id: `pkg_${name.replace(/[^a-z0-9]/g, '')}`,
  format,
  name,
  description: `About ${name}`,
  latestVersion: '1.0.0',
  keywords: [],
  repositories: ['platform/collections'],
  createdAt: '2026-11-01T00:00:00.000Z',
  updatedAt: '2026-11-20T00:00:00.000Z',
  versionCount: 1,
  downloadCount: 0,
  ...extra,
});

async function open(inputs: Record<string, unknown>, setup?: (fake: FakePackagesApi) => void) {
  const r = await renderScreen(NamespacePage, {
    inputs,
    setup: (get) => {
      (get(RepositoriesApi) as FakeRepositoriesApi).formatsResult = [ANSIBLE_FORMAT, PACKAGE_FORMAT, OCI_FORMAT];
      const fake = get(PackagesApi) as FakePackagesApi;
      fake.packages = [
        pkg('ansible', 'acme.tools', { keywords: ['linux'], signature: 'verified_public' }),
        pkg('ansible', 'acme.net'),
        pkg('ansible', 'other.docker'),
        pkg('oci', 'team/app/web', { repositories: ['platform/images'] }),
      ];
      setup?.(fake);
    },
  });
  await settle(r.fixture);
  return { ...r, fake: r.get(PackagesApi) as FakePackagesApi };
}

describe('The namespaces of a name (R-UI-220)', () => {
  const ansible = ANSIBLE_FORMAT.namespaces!;
  const npm = PACKAGE_FORMAT.namespaces!;
  const oci = OCI_FORMAT.namespaces!;

  it('R-UI-220 finds the namespace of a collection, the scope of an npm package and every path prefix of an image', () => {
    expect(namespacesOf(ansible, 'acme.tools')).toEqual(['acme']);
    expect(namespacesOf(npm, '@acme/ui')).toEqual(['@acme']);
    expect(namespacesOf(npm, 'left-pad')).toEqual([]);
    expect(namespacesOf(oci, 'team/app/web')).toEqual(['team', 'team/app']);
    expect(nameParts(oci, 'team/app/web')).toEqual([
      { text: 'team', namespace: 'team' },
      { text: 'app', namespace: 'team/app' },
      { text: 'web', namespace: null },
    ]);
    expect(nameParts(null, 'Contoso.Logging')).toEqual([{ text: 'Contoso.Logging', namespace: null }]);
    expect([isNamespace(ansible, 'acme'), isNamespace(ansible, 'acme.tools'), isNamespace(npm, 'acme'), isNamespace(oci, 'team//app')]).toEqual([
      true,
      false,
      false,
      false,
    ]);
    expect(namespaceCommands('platform', 'oci', 'team/app')).toEqual(['/projects', 'platform', 'packages', '-', 'namespaces', 'oci', 'team', 'app']);
    expect(namespaceCommands(null, 'npm', '@acme')).toEqual(['/namespaces', 'npm', '@acme']);
  });
});

describe('The addresses of a name (R-UI-220)', () => {
  const segs = (...p: string[]) => p.map((x) => new UrlSegment(x, {}));
  const params = (m: ReturnType<typeof namespaceMatcher>) => m && { format: m.posParams!['format']!.path, name: m.posParams!['name']!.path };
  const group = new UrlSegmentGroup([], {});
  const route: Route = {};

  it('R-UI-220 the address in a project and the address across projects name the format and the whole name', () => {
    expect(params(namespaceMatcher(segs('-', 'namespaces', 'oci', 'team', 'app'), group, route))).toEqual({ format: 'oci', name: 'team/app' });
    expect(params(allNamespacesMatcher(segs('oci', 'team', 'app'), group, route))).toEqual({ format: 'oci', name: 'team/app' });
    expect(namespaceMatcher(segs('npm', '@acme', 'ui'), group, route)).toBeNull();
    expect(allNamespacesMatcher(segs('ansible'), group, route)).toBeNull();
  });
});

describe('The page of a name (R-UI-220)', () => {
  it('R-UI-220 lists the packages of a namespace in the project with their tags and badge; a reader of the packages sees no publishing', async () => {
    const r = await open({ project: 'platform', format: 'ansible', name: 'acme' });
    expect(text(r.root.querySelector('h1')!)).toBe('Namespace acme');
    expect(r.fake.listFilters.at(-1)).toEqual({ project: 'platform', format: ['ansible'], namespace: 'acme', sort: 'name' });
    const names = [...r.root.querySelectorAll('tbody tr th quader-package-name a:last-of-type')].map((a) => a.getAttribute('aria-label'));
    expect(names).toEqual(['acme.net', 'acme.tools']);
    const tools = r.root.querySelectorAll('tbody tr')[1]!;
    expect(tools.querySelector('[data-testid="signature-badge"]')?.getAttribute('data-state')).toBe('verified_public');
    expect(tools.querySelector('.tag')?.getAttribute('href')).toBe('/projects/platform/packages?keyword=linux');
    expect(r.root.querySelector('[data-testid="namespace-publishing"]')).toBeNull();
    expect(r.root.querySelector('[data-testid="namespace-every-project"]')?.getAttribute('href')).toBe('/namespaces/ansible/acme');
    await expectAccessible(r.root);
  });

  it('R-UI-220 tells a reader of the configuration in plain words what decides a publish, repository by repository', async () => {
    const r = await open({ project: 'platform', format: 'ansible', name: 'acme' }, (fake) => {
      fake.namespaceFixtures.set('ansible:acme', {
        format: 'ansible',
        name: 'acme',
        kind: 'namespace',
        publishing: {
          repositories: [
            {
              project: 'platform',
              key: 'collections',
              packageOwnership: 'enforce',
              autoClaim: true,
              claims: [{ pattern: 'acme.*', scope: 'members', source: 'manual', covers: 'all' }],
            },
            { project: 'platform', key: 'sandbox', packageOwnership: 'off', autoClaim: true, claims: [] },
          ],
        },
      });
    });
    const section = r.root.querySelector('[data-testid="namespace-publishing"]')!;
    expect(text(section)).toContain('Who may publish into the name');
    expect(text(section)).toContain('acme.');
    const repos = [...section.querySelectorAll('.ns-repo')];
    expect(repos.map((li) => text(li.querySelector('.ns-repo-name')!))).toEqual(['platform/collections', 'platform/sandbox']);
    expect(text(repos[0]!)).toContain('Package ownership is enforced');
    expect(text(repos[0]!)).toContain('Claims acme.* (every name of it; in the virtual repositories that contain this repository; set by hand)');
    expect(text(repos[0]!)).not.toContain('A first publish of a new name claims');
    expect(text(repos[1]!)).toContain('Package ownership is off');
    expect(text(repos[1]!)).toContain('A first publish of a new name claims that name for this repository.');
    expect([...repos[0]!.querySelectorAll('.ns-line a')].map((a) => a.getAttribute('href'))).toEqual([
      '/projects/platform/members',
      '/projects/platform/policies',
    ]);
    await expectAccessible(r.root);
  });

  it('R-UI-220 a path prefix across every project names its outer levels and the project of each package', async () => {
    const r = await open({ format: 'oci', name: 'team/app' });
    expect(text(r.root.querySelector('h1')!)).toBe('Path team/app');
    expect(r.fake.listFilters.at(-1)).toEqual({ project: undefined, format: ['oci'], namespace: 'team/app', sort: 'name' });
    expect([...r.root.querySelectorAll('.ns-parents a')].map((a) => a.getAttribute('href'))).toEqual(['/namespaces/oci/team']);
    expect(r.root.querySelector('[data-testid="namespace-every-project"]')).toBeNull();
    const row = r.root.querySelector('tbody tr')!;
    expect(row.querySelector('td.project-cell a')?.getAttribute('href')).toBe('/projects/platform');
    expect(row.querySelector('th quader-package-name a:last-of-type')?.getAttribute('href')).toBe('/projects/platform/packages/oci/team/app/web');
    expect([...row.querySelectorAll('[data-testid="namespace-link"]')].map((a) => a.getAttribute('href'))).toEqual([
      '/namespaces/oci/team',
      '/namespaces/oci/team/app',
    ]);
  });
});
