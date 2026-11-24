import { afterEach } from 'vitest';
import { ApiError } from '../../api/problem';
import { ProjectsApi } from '../../api/projects-api';
import { RepositoriesApi } from '../../api/repositories-api';
import { SessionApi } from '../../api/session-api';
import { FakeSessionApi } from '../../api/testing/fakes';
import { FakeRepositoriesApi } from '../../api/testing/fakes-catalog';
import { FakeProjectsApi } from '../../api/testing/fakes-projects';
import { BrowserStorage } from '../../core/storage';
import { expectAccessible, renderScreen, settle, text } from '../../testing/screen';
import { ProjectsHome } from './projects-home';

/** The fakes' signed-in principal (`alice`). */
const ALICE = '00000000-0000-0000-0000-000000000001';

/** Stores visits of repositories as the search's store keeps them, newest last visit first in the array's order of `last`. */
function visited(...refs: string[]): void {
  const now = Date.now();
  const repositories = refs.map((id, i) => ({ id, visits: 1, last: now - i * 1000 }));
  new BrowserStorage().set('local', `quader.frequent.${ALICE}`, JSON.stringify({ repositories, projects: [] }));
}

afterEach(() => new BrowserStorage().clear('local'));

describe('Start page', () => {
  it('R-UI-067 R-UI-041 names each project by its display name with the key under it, once', async () => {
    const { root } = await renderScreen(ProjectsHome);
    const heads = [...root.querySelectorAll('thead th')].map((th) => text(th));
    expect(heads.some((h) => h.startsWith('Key'))).toBe(false);
    expect(heads.some((h) => h.startsWith('Display name'))).toBe(false);
    const first = root.querySelector('tbody th[scope="row"]');
    expect(first?.querySelector('.secondary')?.getAttribute('translate')).toBe('no');
    await expectAccessible(root);
  });

  it('R-UI-166 the start page is the list of projects under one heading: no Recently opened, no address field, no Set up client', async () => {
    const { root, fixture, get } = await renderScreen(ProjectsHome, { setup: () => visited('web/generic-local', 'web/releases') });
    await settle(fixture);
    expect([...root.querySelectorAll('h1, h2')].map((h) => `${h.tagName} ${text(h)}`)).toEqual(['H1 Projects']);
    expect(text(root)).not.toContain('Recently opened');
    expect(root.querySelector('quader-address-field')).toBeNull();
    expect([...root.querySelectorAll('a, button')].some((a) => text(a) === 'Set up client')).toBe(false);
    // The start page resolves no remembered repository: that is the side bar's (R-UI-167).
    expect((get(RepositoriesApi) as FakeRepositoriesApi).listFilters.filter((f) => f.name)).toEqual([]);
  });

  it('R-UI-166 the sentence under the title says what the page is for, for a caller with projects, without one and anonymous', async () => {
    const description = (root: HTMLElement) => root.querySelector('.page-header-description')?.textContent?.trim() ?? null;
    const member = await renderScreen(ProjectsHome);
    expect(description(member.root)).toBe('Find a project and explore the repositories and packages it holds.');

    const none = await renderScreen(ProjectsHome, { setup: (get) => ((get(ProjectsApi) as FakeProjectsApi).projects = []) });
    await settle(none.fixture);
    // The empty list says how to get a project; the header adds nothing.
    expect(description(none.root)).toBeNull();
    expect(text(none.root)).toContain('You are not a member of a project yet.');

    const anonymous = await renderScreen(ProjectsHome, {
      setup: (get) => ((get(SessionApi) as FakeSessionApi).whoamiResult = new ApiError(401, null, null)),
    });
    await settle(anonymous.fixture);
    expect(description(anonymous.root)).toBe('The projects with public content. Sign in to see your own.');
  });

  it('Q-UI-414 the start page has no package search of its own: the top bar\'s field is the one search', async () => {
    const { root } = await renderScreen(ProjectsHome);
    // The one field of the page filters its list of projects (Q-UI-421); none searches packages.
    expect([...root.querySelectorAll('input[type="search"]')].map((i) => i.getAttribute('aria-label'))).toEqual(['Filter projects']);
    expect(text(root)).not.toContain('Find a package');
  });

  it('R-UI-146 a project the caller sees through a system role shows the dash as its role, and one without a description the dash too', async () => {
    const { root, fixture } = await renderScreen(ProjectsHome, {
      setup: (get) => {
        const fake = get(ProjectsApi) as FakeProjectsApi;
        fake.projects = fake.projects.map((p) => ({ ...p, role: undefined, description: '' }));
      },
    });
    await settle(fixture);
    const heads = [...root.querySelectorAll('thead th')].map((th) => text(th).replace('help_outline', ''));
    const cells = [...root.querySelectorAll('tbody tr:first-child > td')];
    for (const column of ['Your role', 'Description']) {
      const cell = cells[heads.findIndex((h) => h.startsWith(column)) - 1]!;
      expect(cell.querySelector('quader-no-value'), column).not.toBeNull();
    }
  });

  it('R-UI-095 an anonymous visitor sees the public projects without a role column', async () => {
    const { root, fixture } = await renderScreen(ProjectsHome, {
      setup: (get) => ((get(SessionApi) as FakeSessionApi).whoamiResult = new ApiError(401, null, null)),
    });
    await settle(fixture);
    expect(text(root.querySelector('caption')!)).toBe('Public projects');
    const heads = [...root.querySelectorAll('thead th')].map((th) => text(th).replace('help_outline', ''));
    expect(heads.some((h) => h.startsWith('Your role'))).toBe(false);
    await expectAccessible(root);
  });
});
