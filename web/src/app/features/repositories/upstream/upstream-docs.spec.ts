import { Router } from '@angular/router';
import { ApiError } from '../../../api/problem';
import { SessionApi } from '../../../api/session-api';
import { UpstreamCatalogueApi } from '../../../api/upstream-catalogue-api';
import { FakeSessionApi } from '../../../api/testing/fakes';
import {
  FakeUpstreamCatalogueApi,
  upstreamAbout,
  upstreamHit,
  upstreamPackage,
  upstreamReadme,
} from '../../../api/testing/fakes-upstream-catalogue';
import { expectAccessible, renderScreen, settle, text } from '../../../testing/screen';
import { contentShapeOf } from '../content/content-shape';
import { RemoteUpstream } from './remote-upstream';
import { dependencyBlocks } from './upstream-docs';

/** The tab of `web/npmjs` at the address `url`, with the catalogue's answers set by `fake`. */
async function open(url: string, fake: (c: FakeUpstreamCatalogueApi) => void, format = 'npm') {
  const r = await renderScreen(RemoteUpstream, {
    inputs: { repository: 'web/npmjs', project: 'web', format, shape: contentShapeOf(format, 'remote') },
    setup: async (get) => {
      (get(SessionApi) as FakeSessionApi).meResult = { capabilities: [], roles: [] };
      fake(get(UpstreamCatalogueApi) as FakeUpstreamCatalogueApi);
      await get(Router).navigateByUrl(url);
    },
  });
  await settle(r.fixture);
  await settle(r.fixture);
  return { ...r, catalogue: r.get(UpstreamCatalogueApi) as FakeUpstreamCatalogueApi };
}

const LOOKUP = '/projects/web/repositories/npmjs/upstream?name=lodash';
const README = '<h1 id="user-content-lodash">lodash</h1><p>The modern build of lodash.</p>';

describe('An upstream package before a version is stored (R-CUR-028)', () => {
  it('R-CUR-028 the lookup shows the facts of the newest version, its README read through and its dependencies folded', async () => {
    const { root, catalogue } = await open(LOOKUP, (c) => {
      c.packages['lodash'] = upstreamPackage({
        readme: true,
        about: upstreamAbout({
          links: [
            { kind: 'homepage', url: 'https://lodash.com/' },
            { kind: 'repository', url: 'javascript:alert(1)' },
          ],
          dependencies: [
            { name: 'a', range: '^1.0.0', group: null },
            { name: 'b', range: '>=2', group: 'peer' },
          ],
        }),
      });
      c.readmes['lodash'] = upstreamReadme(README);
    });
    const docs = root.querySelector<HTMLElement>('[data-testid="upstream-lookup"] quader-upstream-docs')!;
    expect(text(docs.querySelector('[data-testid="upstream-about"]')!)).toContain('Version 4.17.21');
    expect(text(docs.querySelector('[data-testid="upstream-about"]')!)).toContain('License MIT');
    // An address that is no http or https address is no link (R-UI-007).
    const links = [...docs.querySelectorAll<HTMLAnchorElement>('[data-testid="upstream-about"] a')];
    expect(links.map((a) => [text(a), a.getAttribute('href'), a.getAttribute('rel')])).toEqual([['Homepage', 'https://lodash.com/', 'nofollow noopener noreferrer']]);
    expect(catalogue.calls).toContain('readme lodash 4.17.21');
    const readme = docs.querySelector<HTMLElement>('[data-testid="upstream-readme"]')!;
    expect(text(readme.querySelector('h3')!)).toBe('README of 4.17.21');
    expect(text(readme.querySelector('quader-package-readme')!)).toContain('The modern build of lodash.');
    const deps = docs.querySelector<HTMLElement>('[data-testid="upstream-dependencies"]')!;
    expect(text(deps.querySelector('button')!)).toBe('chevron_rightDependencies of 4.17.21 2');
    expect(deps.querySelector('button')!.getAttribute('aria-expanded')).toBe('false');
    await expectAccessible(docs);
  });

  it('R-CUR-028 the README is asked once for a package and a version, not with every read of its versions', async () => {
    const { fixture, catalogue } = await open(LOOKUP, (c) => {
      c.packages['lodash'] = upstreamPackage({ readme: true, about: upstreamAbout() });
      c.readmes['lodash'] = upstreamReadme(README);
    });
    await settle(fixture);
    expect(catalogue.calls.filter((call) => call.startsWith('readme'))).toEqual(['readme lodash 4.17.21']);
  });

  it('R-CUR-028 R-AUTHZ-034 a reader who may not read the wiki is told what a version without a README tells, and nothing is asked', async () => {
    const { root, catalogue } = await open(LOOKUP, (c) => (c.packages['lodash'] = upstreamPackage({ readme: false, about: upstreamAbout() })));
    expect(text(root.querySelector('[data-testid="upstream-no-readme"]')!)).toBe('This version has no README to show.');
    expect(catalogue.calls.some((call) => call.startsWith('readme'))).toBe(false);
    expect(root.querySelector('[data-testid="upstream-about"]')).not.toBeNull();
  });

  it('R-CUR-028 a README on a host the repository does not reach, a rate limit and a failure are said in place; the versions show', async () => {
    const far = await open(LOOKUP, (c) => {
      c.packages['lodash'] = upstreamPackage({ readme: true, about: upstreamAbout() });
      c.readmes['lodash'] = { version: '4.17.21', state: 'unreachable', host: 'readmes.example.org', until: null, readme: null };
    });
    const note = far.root.querySelector<HTMLElement>('[data-testid="upstream-readme-unreachable"]')!;
    expect(text(note)).toBe('The upstream keeps this README on readmes.example.org, a host this repository does not reach; Quader did not ask there.');
    expect(text(note.querySelector('code')!)).toBe('readmes.example.org');

    const until = new Date(Date.now() + 10 * 60_000).toISOString();
    const limited = await open(LOOKUP, (c) => {
      c.packages['lodash'] = upstreamPackage({ readme: true, about: upstreamAbout() });
      c.readmes['lodash'] = { version: '4.17.21', state: 'limited', host: null, until, readme: null };
    });
    expect(text(limited.root.querySelector('[data-testid="upstream-readme-limited"]')!)).toContain('The upstream rate-limits this repository until');
    expect(limited.root.querySelectorAll('quader-upstream-package tbody tr').length).toBe(2);

    const failed = await open(LOOKUP, (c) => {
      c.packages['lodash'] = upstreamPackage({ readme: true, about: upstreamAbout() });
      c.readmeError = new ApiError(502, { code: 'upstream_error', detail: 'The upstream answered 500 to the README of lodash 4.17.21.' }, 'rid');
    });
    expect(text(failed.root.querySelector('[data-testid="upstream-readme"] quader-error-notice')!)).toContain('The upstream answered 500 to the README');
    expect(failed.root.querySelectorAll('quader-upstream-package tbody tr').length).toBe(2);

    const budget = await open(LOOKUP, (c) => {
      c.packages['lodash'] = upstreamPackage({ readme: true, about: upstreamAbout() });
      c.readmeError = new ApiError(429, { code: 'catalogue_budget_exhausted', detail: 'x' }, 'rid', 30);
    });
    expect(text(budget.root.querySelector('[data-testid="upstream-readme-state"]')!)).toBe("warningThe upstream catalogue's budget is used up; try again in 30 s.");
  });

  it('R-CUR-028 opened in a row of the search, the README is folded and read when the row opens', async () => {
    const { root, fixture, catalogue } = await open('/projects/web/repositories/npmjs/upstream?q=lodash', (c) => {
      c.hits = [upstreamHit()];
      c.packages['lodash'] = upstreamPackage({ readme: true, about: upstreamAbout() });
      c.readmes['lodash'] = upstreamReadme(README);
    });
    root.querySelector<HTMLButtonElement>('quader-data-table tbody tr:not(.expansion) button[aria-expanded]')!.click();
    await settle(fixture);
    await settle(fixture);
    const fold = root.querySelector<HTMLElement>('quader-upstream-package [data-testid="upstream-readme"]')!;
    expect(fold.tagName).toBe('QUADER-DISCLOSURE');
    expect(fold.querySelector('button[aria-expanded]')!.getAttribute('aria-expanded')).toBe('false');
    expect(catalogue.calls).toContain('readme lodash 4.17.21');
  });

  it('R-CUR-028 an image and a Debian package say why they show no README', async () => {
    const image = await open('/projects/web/repositories/npmjs/upstream?name=library%2Fnginx', (c) => (c.packages['library/nginx'] = upstreamPackage({ name: 'library/nginx', tags: {}, readme: false, about: null })), 'oci');
    expect(text(image.root.querySelector('[data-testid="upstream-no-readme"]')!)).toContain('An image carries no README.');
    const deb = await open('/projects/web/repositories/npmjs/upstream?name=curl', (c) => (c.packages['curl'] = upstreamPackage({ name: 'curl', tags: {}, readme: false, about: null })), 'deb');
    expect(text(deb.root.querySelector('[data-testid="upstream-no-readme"]')!)).toContain('long description');
  });

  it('R-CUR-028 the dependencies stand in the blocks of a stored version: plain, peer, optional, per target framework', () => {
    const blocks = dependencyBlocks(
      upstreamAbout({
        dependencies: [
          { name: 'a', range: '^1', group: null },
          { name: 'b', range: '^2', group: 'optional' },
          { name: 'System.Memory', range: '[4.6.3, )', group: '.NETStandard2.0' },
        ],
      }),
    );
    expect(blocks.map((b) => [b.label, b.entries.length])).toEqual([
      ['Dependencies', 1],
      ['Optional dependencies', 1],
      ['Dependencies for .NETStandard2.0', 1],
    ]);
  });
});
