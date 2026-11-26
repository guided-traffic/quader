import { Page, Route, expect } from '@playwright/test';
import { holdCalls } from './routes';

/*
 * The head of a version's page and the panel of a version's commands (R-UI-111, R-UI-218, Q-UI-1240 to Q-UI-1245): the verb's button
 * with its panel open (a tab per client, Set up client at the row's end), the switch between the versions open, and the states the
 * seed does not hold, answered in the browser from the seed's own answers: a package of thirty versions, one of pre-releases only, a
 * version held by three repositories, and a long README with tables, code and an image.
 */

/** Answers a JSON read with the server's own answer, changed. */
async function changed(route: Route, change: (body: Record<string, unknown>) => unknown): Promise<void> {
  const response = await route.fetch();
  if (!response.ok()) return route.fulfill({ response });
  return route.fulfill({ response, json: change((await response.json()) as Record<string, unknown>) });
}

/** The versions a package of thirty shows: 5.4.1 down to 2.0.0, every fifth a pre-release of the next. */
function thirtyVersions(onlyPrereleases: boolean): { version: string; prerelease: boolean }[] {
  const out: { version: string; prerelease: boolean }[] = [];
  for (let i = 0; out.length < 30; i++) {
    const major = 5 - Math.floor(i / 9);
    const minor = 4 - (Math.floor(i / 3) % 3);
    const patch = 2 - (i % 3);
    const pre = onlyPrereleases || i % 5 === 0;
    out.push({ version: `${major}.${minor}.${patch}${pre ? `-rc.${(i % 3) + 1}` : ''}`, prerelease: pre });
  }
  return out;
}

/**
 * The package answers thirty versions (or, with `onlyPrereleases`, thirty pre-releases, the newest of them its latest), with the
 * contents of a large collection on its documentation's tab: the switch's list, its link to all of them, and a strip of long labels.
 */
export async function manyVersions(page: Page, onlyPrereleases = false): Promise<void> {
  const versions = thirtyVersions(onlyPrereleases);
  const latest = onlyPrereleases ? versions[0]!.version : versions.find((v) => !v.prerelease)!.version;
  let seededLatest: string | null = null;
  await page.route((url) => url.pathname === '/api/v1/packages/lookup', (route) =>
    changed(route, (body) => {
      seededLatest = body['latestVersion'] as string;
      return { ...body, versionCount: versions.length, latestVersion: latest };
    }),
  );
  await page.route(
    (url) => /^\/api\/v1\/packages\/[^/]+\/versions$/.test(url.pathname),
    (route) =>
      changed(route, (body) => {
        const items = (body['items'] as Record<string, unknown>[]) ?? [];
        const model = items[0] ?? {};
        const limit = Number(new URL(route.request().url()).searchParams.get('limit') ?? versions.length);
        return {
          items: versions.slice(0, limit).map((v) => ({ ...model, version: v.version, prerelease: v.prerelease })),
          nextCursor: limit < versions.length ? 'next' : null,
        };
      }),
  );
  // What a version of the thirty holds below its address (its README, its documentation, its report) is the seed's newest's.
  await page.route(
    (url) => /^\/api\/v1\/(vulnerabilities\/)?packages\/[^/]+\/versions\/[^/]+\/.+$/.test(url.pathname) || /\/versions\/[^/]+\/(readme|docs|signatures|vulnerabilities)/.test(url.pathname),
    (route) => {
      const url = new URL(route.request().url());
      const parts = url.pathname.split('/');
      const at = parts.indexOf('versions');
      const asked = decodeURIComponent(parts[at + 1] ?? '');
      if (at < 0 || !seededLatest || !versions.some((x) => x.version === asked)) return route.fallback();
      parts[at + 1] = encodeURIComponent(seededLatest);
      url.pathname = parts.join('/');
      return route.continue({ url: url.toString() });
    },
  );
  // A version of the thirty reads as the seed's newest under its own number; a collection's documentation announces 96 entries.
  await page.route(
    (url) => /^\/api\/v1\/packages\/[^/]+\/versions\/[^/]+$/.test(url.pathname),
    async (route) => {
      const url = new URL(route.request().url());
      const asked = decodeURIComponent(url.pathname.split('/').pop()!);
      const v = versions.find((x) => x.version === asked);
      if (!v || !seededLatest) return route.fallback();
      url.pathname = url.pathname.replace(/[^/]+$/, encodeURIComponent(seededLatest));
      const response = await route.fetch({ url: url.toString() });
      if (!response.ok()) return route.fulfill({ response });
      const body = (await response.json()) as Record<string, unknown>;
      const metadata = (body['metadata'] as Record<string, unknown>) ?? {};
      const docs = metadata['docs'] as Record<string, unknown> | undefined;
      return route.fulfill({
        response,
        json: {
          ...body,
          version: asked,
          prerelease: v.prerelease,
          metadata: docs && docs['state'] === 'available' ? { ...metadata, docs: { ...docs, entries: 96 } } : metadata,
        },
      });
    },
  );
}

/** The version shown is held by three repositories a client can use: the panel offers them in its select. */
export async function threeRepositories(page: Page): Promise<void> {
  await page.route(
    (url) => /^\/api\/v1\/packages\/[^/]+\/versions\/[^/]+$/.test(url.pathname),
    (route) =>
      changed(route, (body) => {
        const held = (body['repositories'] as { key: string; class: string; publishedAt: string; available: boolean }[]) ?? [];
        const first = held[0];
        if (!first) return body;
        const project = first.key.split('/')[0];
        const more = [
          { key: `${project}/npm-mirror-of-the-public-registry`, class: 'remote', publishedAt: first.publishedAt, available: true },
          { key: `${project}/npm-all-teams`, class: 'virtual', publishedAt: first.publishedAt, available: true },
        ];
        return { ...body, repositories: [...held, ...more].slice(0, 3) };
      }),
  );
}

/** A long README: headings, a table wider than a phone, code, a list and an image. */
const LONG_README = `
<h1 id="user-content-acme-ui-kit">@acme/ui-kit</h1>
<p>The components of the customer portal: buttons, fields, tables and the dialogs every page of the portal uses. Each component takes the
theme of the page it stands on and follows the reader's colour scheme.</p>
<p><img src="/ui/favicon.svg" alt="The kit's mark" width="64" height="64"></p>
<h2 id="user-content-install">Install</h2>
<pre><code class="language-shell">npm install @acme/ui-kit
npm install --save-dev @acme/ui-kit-testing</code></pre>
<h2 id="user-content-components">Components</h2>
<table><thead><tr><th>Component</th><th>Element</th><th>Since</th><th>Accessible name from</th><th>Notes</th></tr></thead>
<tbody>
<tr><td>Button</td><td><code>acme-button</code></td><td>1.0.0</td><td>its text</td><td>Three sizes; a tertiary form for a toolbar.</td></tr>
<tr><td>Text field</td><td><code>acme-text-field</code></td><td>1.0.0</td><td>its label</td><td>Shows its error under the field.</td></tr>
<tr><td>Data table</td><td><code>acme-data-table</code></td><td>1.1.0</td><td>its caption</td><td>Sorts in the browser; pages of 50.</td></tr>
</tbody></table>
<h2 id="user-content-use">Use</h2>
<pre><code class="language-ts">import { AcmeButton } from '@acme/ui-kit';

@Component({ imports: [AcmeButton], template: '&lt;acme-button kind="confirm"&gt;Save&lt;/acme-button&gt;' })
export class SaveBar {}</code></pre>
<ul><li>Every component is standalone.</li><li>The theme is read from the page's tokens; nothing to configure.</li></ul>
`;

/** The README of every version is the long one. */
export async function longReadme(page: Page): Promise<void> {
  await page.route(
    (url) => /^\/api\/v1\/packages\/[^/]+\/versions\/[^/]+\/readme$/.test(url.pathname),
    (route) => route.fulfill({ status: 200, contentType: 'application/json', json: { html: LONG_README, truncated: false, externalImagesBlocked: 0 } }),
  );
}

/** Opens the head's verb button and waits for the chosen client's command. */
async function openCommand(page: Page): Promise<void> {
  await page.locator('main quader-page-header quader-client-command-menu button').click();
  await expect(page.locator('.client-command-panel').locator('quader-address-field input, quader-code-window').first()).toBeVisible();
}

/** The version's page as it opens, then its head's command panel open, then the switch between the versions open. */
export async function* versionHead(page: Page): AsyncGenerator<string> {
  await expect(page.locator('main quader-page-header h1')).toBeVisible();
  await page.waitForLoadState('networkidle');
  yield '';
  if ((await page.locator('main quader-page-header quader-client-command-menu').count()) > 0) {
    await openCommand(page);
    yield 'install';
    await page.keyboard.press('Escape');
  }
  if ((await page.locator('main quader-version-switch').count()) > 0) {
    await page.locator('main quader-version-switch button').click();
    await expect(page.locator('.version-menu')).toBeVisible();
    yield 'switch';
    await page.keyboard.press('Escape');
  }
}

/**
 * A NuGet version's forms of installing it (R-NUGET-037, Q-UI-1360): the page as it opens, the panel on the .NET CLI's default form, and
 * where the version has the tab Visual Studio, that tab with its Central Package Management chosen, a form of two lines.
 */
export async function* formsHead(page: Page): AsyncGenerator<string> {
  await expect(page.locator('main quader-page-header h1')).toBeVisible();
  await page.waitForLoadState('networkidle');
  yield '';
  await openCommand(page);
  yield 'install';
  const panel = page.locator('.client-command-panel');
  const vs = panel.getByRole('tab', { name: 'Visual Studio' });
  if ((await vs.count()) > 0) {
    await vs.click();
    // The tabs keep every client's panel, the ones not chosen hidden: the select is the one in Visual Studio's panel.
    const chosen = panel.getByRole('tabpanel', { name: 'Visual Studio' });
    // By the form's snippet id, which no language changes.
    await chosen.locator('.command-form select').selectOption('visual-studio.install.cpm');
    await expect(chosen.locator('quader-code-window')).toBeVisible();
    yield 'install-vs';
  }
  await page.keyboard.press('Escape');
}

/** A head of thirty versions: as it opens, the switch's list bounded at ten with its link to all of them, and the command panel. */
export function manyVersionsHead(onlyPrereleases = false): (page: Page) => AsyncGenerator<string> {
  return async function* (page: Page) {
    await manyVersions(page, onlyPrereleases);
    await page.reload();
    yield* versionHead(page);
  };
}

/** A version held by three repositories: the panel's select of them above its tabs. */
export async function* threeRepositoriesHead(page: Page): AsyncGenerator<string> {
  await threeRepositories(page);
  await page.reload();
  await expect(page.locator('main quader-page-header h1')).toBeVisible();
  await openCommand(page);
  await expect(page.locator('.client-command-panel .command-repository select')).toBeVisible();
  yield 'install';
}

/** The newest version with a long README, as the package's name opens it. */
export async function* longReadmeHead(page: Page): AsyncGenerator<string> {
  await longReadme(page);
  await page.reload();
  await expect(page.locator('main quader-package-readme table')).toBeVisible();
  await page.waitForLoadState('networkidle');
  yield '';
}

/** The panel of a version's commands in an opened row of a repository's list: a tab per client, Set up client at the row's end. */
export async function* rowCommand(page: Page): AsyncGenerator<string> {
  await page.locator('main .name-cell button[aria-expanded="false"]').first().click();
  const versions = page.locator('main .collapse-region.open quader-repository-item-versions');
  await expect(versions.locator('table.data tbody tr').first()).toBeVisible();
  await versions.locator('tbody quader-client-command-menu button').first().click();
  await expect(page.locator('.client-command-panel').locator('quader-address-field input, quader-code-window').first()).toBeVisible();
  yield 'menu';
  // The last tab chosen, whose command is the longest of most formats (Yarn Classic, Podman, Visual Studio).
  const tabs = page.locator('.client-command-panel [role="tab"]');
  if ((await tabs.count()) > 1) {
    await tabs.last().click();
    yield 'menu-last-tab';
  }
}

/**
 * The loads of the head (R-UI-217): the panel opened while the clients' commands are held (each tab's place holds the spinner and
 * "Loading the command."), then the switch opened while its ten newest versions are held (the menu's spinner). Each held call is
 * released before the next state. The controls are found by their place, never by their words: the German pass walks it.
 */
export async function* headLoading(page: Page): AsyncGenerator<string> {
  await expect(page.locator('main quader-page-header h1')).toBeVisible();
  let release = await holdCalls(page, (url) => url.pathname.endsWith('/client-setup'));
  await page.locator('main quader-page-header quader-client-command-menu button').click();
  const panel = page.locator('.client-command-panel');
  await expect(panel.locator('[role="tabpanel"]:not([inert]) quader-loading, .command-body quader-loading').first()).toBeVisible();
  yield 'install-loading';
  await release();
  await page.keyboard.press('Escape');
  if ((await page.locator('main quader-version-switch').count()) === 0) return;
  release = await holdCalls(page, (url) => /^\/api\/v1\/packages\/[^/]+\/versions$/.test(url.pathname));
  await page.reload();
  await expect(page.locator('main quader-page-header h1')).toBeVisible();
  await page.locator('main quader-version-switch button').click();
  await expect(page.locator('.version-menu quader-loading')).toBeVisible();
  yield 'switch-loading';
  await release();
  await page.unrouteAll({ behavior: 'ignoreErrors' });
}
