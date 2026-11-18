import { RepositoriesApi } from '../../api/repositories-api';
import { FakeRepositoriesApi, GENERIC_FORMAT, PACKAGE_FORMAT } from '../../api/testing/fakes-catalog';
import { GENERIC_REPOSITORY, NPM_REPOSITORIES } from '../../api/testing/fakes-packages';
import { expectAccessible, renderScreen, text } from '../../testing/screen';
import { GuideFormat } from './guide-format';
import { GuideView } from './guide-view';
import { GuidesHome } from './guides-home';

/** A format of the catalog without clients, and therefore without a guide. */
const CLIENTLESS = { ...GENERIC_FORMAT, name: 'raw', displayName: 'Raw', clients: [] };

function catalog(get: <A>(token: abstract new (...args: never[]) => A) => A): void {
  const repos = get(RepositoriesApi) as FakeRepositoriesApi;
  repos.formatsResult = [GENERIC_FORMAT, PACKAGE_FORMAT, CLIENTLESS];
  repos.repositories = [...NPM_REPOSITORIES, { ...GENERIC_REPOSITORY }];
}

describe('Guides', () => {
  it('R-UI-124 R-UI-127 the navigation entry shows the formats that have a guide as tiles, and nothing else to choose', async () => {
    const r = await renderScreen(GuidesHome, { setup: catalog });
    expect(text(r.root.querySelector('h1')!)).toBe('Guides');
    const tiles = [...r.root.querySelectorAll<HTMLAnchorElement>('quader-format-tiles a.tile')];
    expect(tiles.map((a) => text(a.querySelector('.tile-name')!))).toEqual(['Generic', 'npm']);
    expect(tiles.map((a) => a.getAttribute('href'))).toEqual(['/guides/generic', '/guides/npm']);
    expect(r.root.querySelector('input, select, fieldset')).toBeNull();
    await expectAccessible(r.root);
  });

  it('R-UI-124 a tile leads to the repositories of its format: virtual first, the class of each, Set up client per row', async () => {
    const r = await renderScreen(GuideFormat, { inputs: { format: 'npm' }, setup: catalog });
    expect(text(r.root.querySelector('h1')!)).toBe('npm repositories');
    const rows = [...r.root.querySelectorAll('tbody tr')];
    expect(rows.map((row) => text(row.querySelector('th a')!))).toEqual(['web/npm-all', 'web/npm-readonly', 'web/npm-local', 'web/npm-remote']);
    expect(text(rows[0]!)).toContain('Virtual');
    expect(text(rows[0]!)).toContain('recommended');
    const setup = rows[0]!.querySelector<HTMLAnchorElement>('a.btn');
    expect(text(setup!)).toBe('Set up client');
    expect(setup?.classList.contains('btn-default')).toBe(true);
    expect(setup?.getAttribute('href')).toBe('/projects/web/repositories/npm-all/setup');
    // The guide without a repository is behind the list, one link per class of the format.
    const classes = [...r.root.querySelectorAll<HTMLAnchorElement>('section.classes a')].map((a) => a.getAttribute('href'));
    expect(classes).toEqual(['/guides/npm/local', '/guides/npm/remote', '/guides/npm/virtual']);
    await expectAccessible(r.root);
  });

  it('R-UI-124 a reader without a repository of the format is told so, and the class guides stay', async () => {
    const r = await renderScreen(GuideFormat, {
      inputs: { format: 'npm' },
      setup: (get) => {
        catalog(get);
        (get(RepositoriesApi) as FakeRepositoriesApi).repositories = [];
      },
    });
    expect(text(r.root)).toContain('You can use no repository of this package type.');
    expect(r.root.querySelectorAll('section.classes a').length).toBe(3);
  });

  it('R-UI-124 a format without a guide is not offered', async () => {
    const r = await renderScreen(GuideFormat, { inputs: { format: 'raw' }, setup: catalog });
    expect(text(r.root)).toContain('This installation has no guide for this package type.');
  });

  it('R-UI-125 the explanation: what the class is and the endpoint note stand open, the rest are closed disclosures, and code is copyable', async () => {
    const html = (body: string) => body;
    const r = await renderScreen(GuideView, {
      inputs: {
        sections: [
          { id: 'npm.local.what', topic: 'what', title: 'What a local repository is', html: html('<p>A local repository holds what you publish.</p><h3>Published versions never change</h3><p>More.</p>') },
          { id: 'npm.configure', topic: 'configure', title: 'Point the client at the repository', html: html('<pre><code>@your-scope:registry=https://x/\n</code></pre>') },
          { id: 'npm.endpoint', topic: 'endpoint', title: 'On this endpoint', html: html('<p>Hidden versions answer 404.</p>') },
          { id: 'npm.troubleshooting', topic: 'troubleshooting', title: 'Troubleshooting', html: html('<table><tr><td>404</td></tr></table>') },
        ],
      },
    });
    expect([...r.root.querySelectorAll('section.guide-open > h2')].map((h) => text(h))).toEqual(['What a local repository is', 'On this endpoint']);
    expect([...r.root.querySelectorAll<HTMLElement>('quader-disclosure.guide-more')].map((d) => d.dataset['section'])).toEqual([
      'npm.configure',
      'npm.troubleshooting',
    ]);
    // An open section shows its introduction; the text from its first subheading folds under More about this.
    const what = r.root.querySelector('[data-section="npm.local.what"]')!;
    expect(text(what.querySelector(':scope > quader-package-readme')!)).toBe('A local repository holds what you publish.');
    expect(text(what.querySelector('quader-disclosure.guide-rest')!)).toContain('More about this');
    expect(text(what.querySelector('quader-disclosure.guide-rest')!)).toContain('Published versions never change');
    const code = r.root.querySelector('[data-section="npm.configure"] .prose-code')!;
    expect(code.querySelector('pre')).toBeTruthy();
    expect(code.querySelector('quader-copy-button button')?.getAttribute('aria-label')).toBe('Copy code');
    expect(r.root.querySelector('section.guide-open')?.getAttribute('lang') ?? r.root.querySelector('section.guide-open > h2')?.getAttribute('lang')).toBe('en');
    await expectAccessible(r.root);
  });
});
