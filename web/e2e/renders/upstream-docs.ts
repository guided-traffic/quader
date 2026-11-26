import { Page, Route, expect } from '@playwright/test';
import { capsUrl, open, packageUrl, searchUrl, tab } from './upstream';

/*
 * What a package of the upstream tells before a version of it is stored (W-30, G2: remote-curation.md R-CUR-028): the facts of its newest
 * version, its README read through and its dependencies, for an npm, a NuGet and an Ansible Galaxy remote. The seed keeps the remotes of
 * public registries offline, so the walks answer the catalogue in the browser with what the server answers: a README of 300 lines with
 * tables, code, headings and three external images shown as links (the server's renderer makes them links, R-UI-008), the README folded
 * in a row of the search, a reader who may not read the wiki, a README on a host the repository does not reach, a README that failed and
 * a rate limit that holds. The controls by their place, never their words: the German pass walks these screens too.
 */

const readmeUrl = (url: URL): boolean => /^\/api\/v1\/projects\/[^/]+\/repositories\/[^/]+\/upstream\/readme$/.test(url.pathname);
const verdictsUrl = (url: URL): boolean => /^\/api\/v1\/projects\/[^/]+\/repositories\/[^/]+\/upstream\/vulnerabilities$/.test(url.pathname);
const requestsUrl = (url: URL): boolean => /^\/api\/v1\/projects\/[^/]+\/repositories\/[^/]+\/delivery-requests$/.test(url.pathname);

function json(route: Route, body: unknown, status = 200, headers: Record<string, string> = {}): Promise<void> {
  return route.fulfill({ status, contentType: status < 300 ? 'application/json' : 'application/problem+json', headers, body: JSON.stringify(body) });
}

const all = { delivered: true, decidedBy: { mode: 'allExceptBlocked' } };

/** The caps of a remote of the format, for whoever may set rules. */
function caps(format: 'npm' | 'nuget' | 'ansible'): Record<string, unknown> {
  const hint = { npm: 'npmRegistry', nuget: 'nugetSearch', ansible: 'galaxy' }[format];
  return {
    search: 'upstream',
    searchWindow: null,
    searchHint: hint,
    levels: format === 'nuget' ? [] : [format === 'npm' ? 'scope' : 'namespace'],
    levelsReason: format === 'nuget' ? 'NuGet IDs have no levels; search for a prefix with id:.' : null,
    sizes: format === 'ansible' ? 'perVersion' : 'unknown',
    hosts: [],
    mode: 'allExceptBlocked',
    rules: 0,
    openRequests: 0,
    queue: { waiting: 0, running: 0, failed: 0, done: 0 },
    maySetRules: true,
  };
}

function versions(list: readonly string[]): Record<string, unknown>[] {
  return list.map((version, i) => ({
    version,
    published: new Date(Date.now() - (i + 1) * 9 * 86_400_000).toISOString(),
    size: 48_213 + i * 311,
    unpackedSize: null,
    flags: version.includes('-') ? ['prerelease'] : [],
    platforms: [],
    digest: null,
    delivery: all,
    inherited: all,
    stored: i === 2 ? 'whole' : 'none',
    queued: null,
    request: null,
  }));
}

/** A README of about 300 lines as the server renders it: headings, paragraphs, a wide table, code, a list and three external images as links. */
export function longReadme(title: string): string {
  const out: string[] = [
    `<h1 id="user-content-${title}">${title}</h1>`,
    '<p><a href="https://img.shields.io/npm/v/example.svg" rel="nofollow noopener noreferrer"><span class="external-image">External image: <span class="external-image-alt">npm version</span></span></a> ' +
      '<a href="https://github.com/example/actions/workflows/ci.yml/badge.svg" rel="nofollow noopener noreferrer"><span class="external-image">External image: <span class="external-image-alt">CI</span></span></a> ' +
      '<a href="https://codecov.io/gh/example/branch/main/graph/badge.svg" rel="nofollow noopener noreferrer"><span class="external-image">External image: <span class="external-image-alt">coverage</span></span></a></p>',
    '<p>A library that keeps a long name, a long description and documentation that goes on for a while, so that the README reaches well past the first screen.</p>',
    '<h2 id="user-content-install">Install</h2>',
    '<pre><code class="language-shell">npm install --save-exact example-package-with-a-long-name@4.17.21\n</code></pre>',
    '<h2 id="user-content-options">Options</h2>',
    '<table><thead><tr><th>Option</th><th>Type</th><th>Default</th><th>Description</th></tr></thead><tbody>',
  ];
  for (let i = 1; i <= 24; i++) {
    out.push(`<tr><td><code>option${i}WithALongerName</code></td><td><code>string | number | boolean</code></td><td><code>${i * 10}</code></td><td>What option ${i} changes, in a sentence that is long enough to wrap in a narrow column.</td></tr>`);
  }
  out.push('</tbody></table>');
  for (let s = 1; s <= 12; s++) {
    out.push(`<h2 id="user-content-section-${s}">Section ${s}</h2>`);
    out.push(`<p>Section ${s} explains a part of the API with an example. The paragraph is plain text the upstream wrote, sanitized by the server.</p>`);
    out.push(`<pre><code class="language-js">import { thing${s} } from 'example';\n\nconst result = thing${s}({ retries: ${s}, timeoutMs: ${s * 1000} });\nconsole.log(result);\n</code></pre>`);
    out.push('<ul><li>One point of the section.</li><li>Another point, with <code>inline code</code>.</li><li>A <a href="https://example.org/docs" rel="nofollow noopener noreferrer">link to the docs</a>.</li></ul>');
  }
  out.push('<h2 id="user-content-license">License</h2><p>MIT</p>');
  return out.join('\n');
}

interface Answers {
  readonly format: 'npm' | 'nuget' | 'ansible';
  readonly name: string;
  readonly description: string;
  readonly list: readonly string[];
  readonly about: Record<string, unknown>;
  readonly readme: boolean;
  readonly answer: (route: Route) => Promise<void>;
  readonly tags?: Record<string, string>;
  readonly keywords?: readonly string[];
}

async function answer(p: Page, a: Answers): Promise<void> {
  await p.unrouteAll({ behavior: 'ignoreErrors' });
  await p.route(capsUrl, (r) => json(r, caps(a.format)));
  await p.route(requestsUrl, (r) => json(r, { items: [], nextCursor: null }));
  await p.route(verdictsUrl, (r) => json(r, { notCovered: a.format !== 'npm', versions: [] }));
  await p.route(searchUrl, (r) =>
    json(r, {
      hits: [
        {
          name: a.name,
          description: a.description,
          latest: a.list[0],
          updated: null,
          flags: [],
          ...(a.keywords ? { keywords: a.keywords } : {}),
          delivery: all,
          stored: 'partial',
          request: null,
          level: all,
          inherited: all,
          versionRules: { allow: 0, block: 0 },
        },
      ],
      total: 1,
      nextCursor: null,
      done: true,
      truncated: false,
      note: null,
      withheld: null,
      withheldNote: null,
    }),
  );
  await p.route(packageUrl, (r) =>
    json(r, {
      name: a.name,
      description: a.description,
      tags: a.tags ?? {},
      ...(a.keywords ? { keywords: a.keywords } : {}),
      delivery: all,
      stored: 'partial',
      request: null,
      level: all,
      inherited: all,
      versionRules: { allow: 0, block: 0 },
      versions: versions(a.list),
      total: a.list.length,
      nextCursor: null,
      done: true,
      about: a.about,
      readme: a.readme,
    }),
  );
  await p.route(readmeUrl, a.answer);
}

const available = (version: string, html: string, contentType = 'text/markdown') => (r: Route) =>
  json(r, { version, state: 'available', host: null, until: null, readme: { contentType, html, truncated: false, externalImagesBlocked: 3 } });

const npmAbout = {
  version: '4.17.21',
  license: 'MIT',
  links: [
    { kind: 'homepage', url: 'https://lodash.com/' },
    { kind: 'repository', url: 'https://github.com/lodash/lodash' },
    { kind: 'issues', url: 'https://github.com/lodash/lodash/issues' },
  ],
  dependencies: [
    { name: '@babel/runtime', range: '^7.26.0', group: null },
    { name: 'tslib', range: '^2.8.1', group: null },
    { name: 'react', range: '>=18 <20', group: 'peer' },
    { name: 'fsevents', range: '~2.3.3', group: 'optional' },
  ],
  deprecation: null,
};

function npmAnswers(over: Partial<Answers> = {}): Answers {
  return {
    format: 'npm',
    name: 'lodash',
    description: 'Lodash modular utilities.',
    list: ['4.17.21', '4.17.20', '4.17.19', '4.17.15', '5.0.0-beta.1'],
    tags: { latest: '4.17.21' },
    keywords: ['modules', 'stdlib', 'util', 'functional', 'server', 'client', 'browser', 'lodash-modularized', 'es6'],
    about: npmAbout,
    readme: true,
    answer: available('4.17.21', longReadme('lodash')),
    ...over,
  };
}

/**
 * An npm remote: a package looked up by name with a README of 300 lines (its first 24 rem, then shown whole), the package opened in a
 * row of the search with the README folded and unfolded, then a reader who may not read the wiki, a README on a host the repository
 * does not reach, a README that failed and a rate limit that holds; each time the versions show.
 */
export async function* upstreamReadmeNpmWalk(p: Page): AsyncGenerator<string> {
  await answer(p, npmAnswers());
  await open(p, '?name=lodash');
  await expect(p.locator(`${tab} [data-testid="upstream-readme"] quader-package-readme .readme`)).toBeVisible();
  yield 'readme';
  await p.locator(`${tab} [data-testid="upstream-readme-toggle"]`).click();
  await expect(p.locator(`${tab} [data-testid="upstream-readme-toggle"]`)).toHaveAttribute('aria-expanded', 'true');
  await p.locator(`${tab} [data-testid="upstream-dependencies"] button.toggle`).click();
  yield 'readme-whole';
  await open(p, '?q=lodash');
  await p.locator(`${tab} button[aria-controls="up-lodash"]`).click();
  await expect(p.locator(`${tab} quader-upstream-package [data-testid="upstream-readme"]`)).toBeVisible();
  yield 'readme-row';
  await p.locator(`${tab} quader-upstream-package [data-testid="upstream-readme"] button.toggle`).click();
  await expect(p.locator(`${tab} quader-upstream-package [data-testid="upstream-readme"] quader-package-readme .readme`)).toBeVisible();
  yield 'readme-row-open';
  await answer(p, npmAnswers({ readme: false, about: { ...npmAbout, deprecation: { message: 'Use the native methods of the platform instead.', reasons: [], alternate: null, alternateRange: null } } }));
  await open(p, '?name=lodash');
  await expect(p.locator(`${tab} [data-testid="upstream-no-readme"]`)).toBeVisible();
  yield 'readme-no-wiki';
  await answer(p, npmAnswers({ answer: (r) => json(r, { version: '4.17.21', state: 'unreachable', host: 'readme-mirror.cdn.example-company.com', until: null, readme: null }) }));
  await open(p, '?name=lodash');
  await expect(p.locator(`${tab} [data-testid="upstream-readme-unreachable"]`)).toBeVisible();
  yield 'readme-unreachable';
  await answer(p, npmAnswers({ answer: (r) => json(r, { code: 'upstream_error', title: 'Upstream error', status: 502, detail: 'The upstream answered Internal Server Error (500) to the README of lodash 4.17.21.' }, 502) }));
  await open(p, '?name=lodash');
  await expect(p.locator(`${tab} [data-testid="upstream-readme"] quader-error-notice`)).toBeVisible();
  yield 'readme-failed';
  const until = new Date(Date.now() + 25 * 60_000).toISOString();
  await answer(p, npmAnswers({ answer: (r) => json(r, { version: '4.17.21', state: 'limited', host: null, until, readme: null }) }));
  await open(p, '?name=lodash');
  await expect(p.locator(`${tab} [data-testid="upstream-readme-limited"]`)).toBeVisible();
  yield 'readme-limited';
  await p.unrouteAll({ behavior: 'ignoreErrors' });
}

/** A NuGet remote: a package looked up by name with its README, its licence expression, its project's address and its dependencies per target framework. */
export async function* upstreamReadmeNuGetWalk(p: Page): AsyncGenerator<string> {
  const list = ['13.0.4', '13.0.3', '13.0.2', '13.0.1', '12.0.3'];
  await answer(p, {
    format: 'nuget',
    name: 'newtonsoft.json',
    description: 'Json.NET is a popular high-performance JSON framework for .NET',
    list,
    keywords: ['json'],
    about: {
      version: '13.0.4',
      license: 'MIT',
      links: [{ kind: 'homepage', url: 'https://www.newtonsoft.com/json' }],
      dependencies: [
        { name: 'Microsoft.CSharp', range: '[4.3.0, )', group: '.NETStandard1.0' },
        { name: 'NETStandard.Library', range: '[1.6.1, )', group: '.NETStandard1.0' },
        { name: 'System.ComponentModel.TypeConverter', range: '[4.3.0, )', group: '.NETStandard1.0' },
        { name: 'System.Runtime.Serialization.Primitives', range: '[4.3.0, )', group: '.NETStandard1.3' },
      ],
      deprecation: null,
    },
    readme: true,
    answer: available('13.0.4', longReadme('Json.NET')),
  });
  await open(p, '?name=newtonsoft.json');
  await expect(p.locator(`${tab} [data-testid="upstream-readme"] quader-package-readme .readme`)).toBeVisible();
  yield 'readme';
  await p.unrouteAll({ behavior: 'ignoreErrors' });
}

/** An Ansible Galaxy remote: a collection looked up by name with the README of its highest version's docs-blob, its links and the collections it depends on. */
export async function* upstreamReadmeGalaxyWalk(p: Page): AsyncGenerator<string> {
  await answer(p, {
    format: 'ansible',
    name: 'community.docker',
    description: 'Modules and plugins for working with Docker',
    list: ['4.8.1', '4.8.0', '4.7.0', '4.6.2', '3.13.10'],
    keywords: ['docker', 'containers', 'compose', 'swarm'],
    about: {
      version: '4.8.1',
      license: 'GPL-3.0-or-later OR Apache-2.0',
      links: [
        { kind: 'repository', url: 'https://github.com/ansible-collections/community.docker' },
        { kind: 'issues', url: 'https://github.com/ansible-collections/community.docker/issues' },
        { kind: 'documentation', url: 'https://docs.ansible.com/ansible/latest/collections/community/docker/' },
      ],
      dependencies: [{ name: 'community.library_inventory_filtering_v1', range: '>=1.0.0', group: null }],
      deprecation: null,
    },
    readme: true,
    answer: available('4.8.1', longReadme('Docker Community Collection'), 'text/html'),
  });
  await open(p, '?name=community.docker');
  await expect(p.locator(`${tab} [data-testid="upstream-readme"] quader-package-readme .readme`)).toBeVisible();
  yield 'readme';
  await p.unrouteAll({ behavior: 'ignoreErrors' });
}

/** The answers of the README walks that are its states, not failures. */
export const readmeExpected = [
  { match: /^GET \/api\/v1\/projects\/[^/]+\/repositories\/[^/]+\/upstream\/readme(\?\S*)? answered 502$/, why: 'the walk answers a README that failed to render its state' },
];
