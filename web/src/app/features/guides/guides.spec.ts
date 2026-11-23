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

  it('R-UI-124 Q-UI-430 a tile leads to the repositories of its format, built from the parts of the repository list: virtual first, Set up client per row', async () => {
    const r = await renderScreen(GuideFormat, { inputs: { format: 'npm' }, setup: catalog });
    expect(text(r.root.querySelector('h1')!)).toBe('npm repositories');
    expect([...r.root.querySelectorAll('thead th')].map((th) => text(th))).toEqual(['Repository', 'Project', 'Class', 'Actions']);
    const rows = [...r.root.querySelectorAll('tbody tr')];
    expect(rows.map((row) => text(row.querySelector('th .icon-name-text > a')!))).toEqual(['npm-all', 'npm-readonly', 'npm-local', 'npm-remote']);
    // The name cell of every repository list: the visibility icon, the key, the description under it (Q-UI-425).
    expect(rows[0]!.querySelector('th .icon-name > quader-visibility-icon')).not.toBeNull();
    expect(rows[0]!.querySelector('td.project-cell quader-project-link')).not.toBeNull();
    expect(text(rows[0]!.querySelector('td.class-cell')!)).toContain('Virtual');
    expect(text(rows[0]!.querySelector('th')!)).toContain('Recommended');
    // The row's action in the form of a row (Q-UI-487): quiet, its name ending with the key.
    const setup = rows[0]!.querySelector<HTMLAnchorElement>('td.actions a.btn');
    expect(text(setup!)).toBe('Set up client npm-all');
    expect(setup?.classList.contains('btn-tertiary')).toBe(true);
    expect(setup?.getAttribute('href')).toBe('/projects/web/repositories/npm-all/setup');
    // The guide without a repository is behind the list, one link per class of the format.
    const classes = [...r.root.querySelectorAll<HTMLAnchorElement>('section.classes a')].map((a) => a.getAttribute('href'));
    expect(classes).toEqual(['/guides/npm/local', '/guides/npm/remote', '/guides/npm/virtual']);
    await expectAccessible(r.root);
  });

  it('R-UI-193 Q-UI-901 a system repository of upstream is not among the repositories clients can use; a registry of upstream is', async () => {
    const r = await renderScreen(GuideFormat, {
      inputs: { format: 'npm' },
      setup: (get) => {
        catalog(get);
        const repos = get(RepositoriesApi) as FakeRepositoriesApi;
        repos.repositories = [
          ...repos.repositories,
          { key: 'npmjs', project: 'upstream', class: 'remote', format: 'npm', description: 'npm public registry' },
          // As the product declares it only for OCI and generic; npm here, so that the format's one list shows the rule.
          { key: 'scanner-trivy', project: 'upstream', class: 'remote', format: 'npm', description: 'Image scanning database' },
        ];
      },
    });
    const keys = [...r.root.querySelectorAll('tbody tr')].map((row) => text(row.querySelector('th .icon-name-text > a')!));
    expect(keys).toContain('npmjs');
    expect(keys).not.toContain('scanner-trivy');
    // Every row left has its Set up client.
    expect(r.root.querySelectorAll('tbody td.actions.with-setup a.row-setup')).toHaveLength(keys.length);
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

  it('R-UI-125 the explanation: every section is a closed row titled with its heading, in the server\'s order, and code is copyable', async () => {
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
    const rows = [...r.root.querySelectorAll<HTMLElement>('quader-disclosure.guide-more')];
    expect(rows.map((d) => d.dataset['section'])).toEqual(['npm.local.what', 'npm.configure', 'npm.endpoint', 'npm.troubleshooting']);
    expect(rows.map((d) => text(d.querySelector('h3 button span:last-child')!))).toEqual([
      'What a local repository is',
      'Point the client at the repository',
      'On this endpoint',
      'Troubleshooting',
    ]);
    // Closed, the first one too; the whole section is inside its row, with no second fold.
    expect(r.root.querySelectorAll('button[aria-expanded="true"]').length).toBe(0);
    expect(r.root.querySelector('section.guide-open, quader-disclosure.guide-rest')).toBeNull();
    expect(text(rows[0]!)).toContain('Published versions never change');
    const code = r.root.querySelector('[data-section="npm.configure"] .prose-code')!;
    expect(code.querySelector('pre')).toBeTruthy();
    expect(code.querySelector('quader-copy-button button')?.getAttribute('aria-label')).toBe('Copy code');
    expect(rows[0]!.getAttribute('lang')).toBe('en');
    await expectAccessible(r.root);
  });

  it('R-UI-125 Q-UI-971 every cell of a table carries its column\'s name, which a narrow page shows when the rows stand as blocks', async () => {
    const r = await renderScreen(GuideView, {
      inputs: {
        sections: [
          {
            id: 'clamav.troubleshooting',
            topic: 'troubleshooting',
            title: 'Troubleshooting',
            html: '<table><thead><tr><th>What <code>freshclam</code> reports</th><th>Why</th><th>What to do</th></tr></thead><tbody><tr><td><code>401</code></td><td>No token</td><td>Put a valid token into the address</td></tr></tbody></table>',
          },
        ],
      },
    });
    const cells = [...r.root.querySelectorAll('.readme tbody td')].map((c) => c.getAttribute('data-label'));
    expect(cells).toEqual(['What freshclam reports', 'Why', 'What to do']);
  });
});
