import { provideHttpClient } from '@angular/common/http';
import { signal } from '@angular/core';
import { TestBed } from '@angular/core/testing';
import { Router, provideRouter, withComponentInputBinding } from '@angular/router';
import { vi } from 'vitest';
import { App } from './app';
import { routes } from './app.routes';
import { ApiError } from './api/problem';
import { RepositoriesApi } from './api/repositories-api';
import { SessionApi } from './api/session-api';
import { FakeSessionApi, provideFakeApis } from './api/testing/fakes';
import { FakeRepositoriesApi } from './api/testing/fakes-catalog';
import { FrequentPlaces } from './core/frequent-places';
import { Layout } from './core/layout';
import { BrowserStorage } from './core/storage';
import { seriousViolations } from './testing/axe';

/** The fakes' signed-in principal (`alice`). */
const ALICE = '00000000-0000-0000-0000-000000000001';

/** Stores visits of repositories as the search's store keeps them: the first is the newest. */
function visited(...refs: string[]): void {
  const now = Date.now();
  const repositories = refs.map((id, i) => ({ id, visits: 1, last: now - i * 1000 }));
  new BrowserStorage().set('local', `quader.frequent.${ALICE}`, JSON.stringify({ repositories, projects: [] }));
}

interface Options {
  readonly refs?: readonly string[];
  readonly pinned?: boolean;
  readonly setup?: () => void;
}

async function render(url: string, options: Options = {}) {
  new BrowserStorage().clear('local');
  await TestBed.configureTestingModule({
    imports: [App],
    providers: [
      provideRouter(routes, withComponentInputBinding()),
      provideHttpClient(),
      ...provideFakeApis(),
      { provide: Layout, useValue: { pinned: signal(options.pinned ?? true) } },
    ],
  }).compileComponents();
  if (options.refs) visited(...options.refs);
  options.setup?.();
  const fixture = TestBed.createComponent(App);
  await TestBed.inject(Router).navigateByUrl(url);
  await fixture.whenStable();
  await fixture.whenStable();
  const root = fixture.nativeElement as HTMLElement;
  const named = () => (TestBed.inject(RepositoriesApi) as FakeRepositoriesApi).listFilters.filter((f) => f.name).map((f) => f.name);
  const toggle = () =>
    [...root.querySelectorAll<HTMLButtonElement>('aside nav button.group-toggle')].find((b) => b.textContent?.includes('Recently opened')) ?? null;
  return { fixture, root, named, toggle };
}

describe('Recently opened in the side bar (R-UI-167)', () => {
  afterEach(() => new BrowserStorage().clear('local'));

  it('R-UI-167 a folding group at the end of Your work, folded at first, with the last five repositories the server confirmed, each a link with its project\'s mark', async () => {
    const { fixture, root, named, toggle } = await render('/projects', {
      refs: ['web/releases', 'platform/gone', 'web/generic-local', 'web/one', 'web/two', 'web/six'],
    });
    // One request for the newest five; the sixth is not asked for.
    expect(named()).toEqual([['web/releases', 'platform/gone', 'web/generic-local', 'web/one', 'web/two']]);
    const button = toggle()!;
    expect(button).not.toBeNull();
    // The last entry of the block Your work, after Projects, Repositories and Guides.
    const block = root.querySelector<HTMLElement>('[role="group"][aria-labelledby="nav-your-work"]')!;
    expect([...block.querySelectorAll(':scope > ul > li')].map((li) => li.querySelector('.nav-label')?.textContent?.trim())).toEqual([
      'Projects',
      'Repositories',
      'Guides',
      'Recently opened',
    ]);
    // Folded when the page loads for the first time.
    const region = root.querySelector<HTMLElement>(`#${button.getAttribute('aria-controls')}`)!;
    expect(button.getAttribute('aria-expanded')).toBe('false');
    expect(region.classList.contains('open')).toBe(false);

    button.click();
    await fixture.whenStable();
    expect(button.getAttribute('aria-expanded')).toBe('true');
    expect(region.classList.contains('open')).toBe(true);
    const links = [...region.querySelectorAll<HTMLAnchorElement>('a')];
    // Newest first; a repository the caller may not read (or that is gone) is left out, not shown as a dead link.
    expect(links.map((a) => a.getAttribute('href'))).toEqual(['/projects/web/repositories/releases', '/projects/web/repositories/generic-local']);
    expect(links.map((a) => a.querySelector('.recent-key')?.textContent?.trim())).toEqual(['releases', 'generic-local']);
    // The accessible name starts with the visible key and carries the project's name and the full name; the title is the same.
    expect(links[0]!.getAttribute('aria-label')).toBe('releases in Web platform (web/releases)');
    expect(links[0]!.getAttribute('title')).toBe('releases in Web platform (web/releases)');
    expect(links[0]!.querySelector('quader-project-mark')?.getAttribute('data-letter')).toBe('W');
    // No address, no button.
    expect(region.querySelector('quader-address-field, button, input')).toBeNull();
    expect(await seriousViolations(root)).toEqual([]);
  });

  it('R-UI-167 the group keeps the viewer\'s choice: opened once, it is open on the next visit', async () => {
    const first = await render('/projects', { refs: ['web/releases'] });
    first.toggle()!.click();
    await first.fixture.whenStable();
    const stored = new BrowserStorage().entries('local').find(([k]) => k.startsWith('quader.nav.'))!;
    expect(JSON.parse(stored[1])).toEqual({ collapsed: false, closed: [], opened: ['recent'] });

    TestBed.resetTestingModule();
    await TestBed.configureTestingModule({
      imports: [App],
      providers: [provideRouter(routes, withComponentInputBinding()), provideHttpClient(), ...provideFakeApis(), { provide: Layout, useValue: { pinned: signal(true) } }],
    }).compileComponents();
    const fixture = TestBed.createComponent(App);
    await TestBed.inject(Router).navigateByUrl('/guides');
    await fixture.whenStable();
    await fixture.whenStable();
    const again = [...(fixture.nativeElement as HTMLElement).querySelectorAll('button.group-toggle')].find((b) => b.textContent?.includes('Recently opened'));
    expect(again?.getAttribute('aria-expanded')).toBe('true');
  });

  it('R-UI-167 the remembered repositories are confirmed once per session: navigating asks nothing again, a newly opened one is asked alone', async () => {
    const { fixture, root, named } = await render('/projects', { refs: ['web/releases', 'web/generic-local'] });
    expect(named()).toEqual([['web/releases', 'web/generic-local']]);
    await TestBed.inject(Router).navigateByUrl('/guides');
    await fixture.whenStable();
    await TestBed.inject(Router).navigateByUrl('/projects');
    await fixture.whenStable();
    expect(named()).toHaveLength(1);

    // A repository opened since becomes the newest: only its name is asked, and it joins the group.
    TestBed.inject(FrequentPlaces).visitRepository('web', 'docs');
    await fixture.whenStable();
    await fixture.whenStable();
    expect(named()).toEqual([['web/releases', 'web/generic-local'], ['web/docs']]);
    expect(root.textContent).toContain('Recently opened');
  });

  it('R-UI-167 the group is not shown while no stored repository is readable, with nothing stored, without browser storage, or to an anonymous visitor', async () => {
    const none = await render('/projects', { refs: ['platform/gone'] });
    expect(none.named()).toEqual([['platform/gone']]);
    expect(none.toggle()).toBeNull();
    TestBed.resetTestingModule();

    const empty = await render('/projects');
    expect(empty.named()).toEqual([]);
    expect(empty.toggle()).toBeNull();
    TestBed.resetTestingModule();

    const blocked = await render('/projects', {
      refs: ['web/releases'],
      setup: () => vi.spyOn(TestBed.inject(BrowserStorage), 'available').mockReturnValue(false),
    });
    expect(blocked.named()).toEqual([]);
    expect(blocked.toggle()).toBeNull();
    TestBed.resetTestingModule();

    const anonymous = await render('/projects', {
      refs: ['web/releases'],
      setup: () => ((TestBed.inject(SessionApi) as FakeSessionApi).whoamiResult = new ApiError(401, null, null)),
    });
    expect(anonymous.named()).toEqual([]);
    expect(anonymous.toggle()).toBeNull();
  });

  it('R-UI-167 the group is the home side bar\'s: a project\'s side bar and the admin area have none, and ask nothing', async () => {
    const project = await render('/projects/web/packages', { refs: ['web/releases'] });
    expect(project.toggle()).toBeNull();
    expect(project.named()).toEqual([]);
    await TestBed.inject(Router).navigateByUrl('/admin/system');
    await project.fixture.whenStable();
    expect(project.toggle()).toBeNull();
    expect(project.named()).toEqual([]);
  });

  it('R-UI-167 below the pinned width the group is in the drawer, asked for when the drawer opens', async () => {
    const { fixture, root, named, toggle } = await render('/projects', { refs: ['web/releases'], pinned: false });
    expect(root.querySelector('aside.sidebar')).toBeNull();
    expect(named()).toEqual([]);
    root.querySelector<HTMLButtonElement>('header button[aria-label="Open navigation"]')!.click();
    await fixture.whenStable();
    await fixture.whenStable();
    expect(named()).toEqual([['web/releases']]);
    expect(toggle()?.getAttribute('aria-expanded')).toBe('false');
  });

  it('R-UI-167 a repository that is gone leaves the group at once, without a new sign-in; the stored one that moves up is asked alone', async () => {
    const five = ['web/releases', 'web/one', 'web/two', 'web/three', 'web/four'];
    const { fixture, root, named, toggle } = await render('/projects', { refs: [...five, 'web/generic-local'] });
    const links = () => [...root.querySelectorAll<HTMLAnchorElement>('a.recent-link')].map((a) => a.getAttribute('href'));
    expect(named()).toEqual([five]);
    expect(links()).toEqual(['/projects/web/repositories/releases']);

    // What the settings page, the list and the repository's own page do when the repository is deleted, moved or not found.
    TestBed.inject(FrequentPlaces).forgetRepository('web', 'releases');
    fixture.detectChanges();
    // Its one entry is gone before any answer: the group is not shown.
    expect(links()).toEqual([]);
    expect(toggle()).toBeNull();

    await fixture.whenStable();
    await fixture.whenStable();
    // The sixth is one of the five now: one request for its name, none for the four already answered.
    expect(named()).toEqual([five, ['web/generic-local']]);
    expect(links()).toEqual(['/projects/web/repositories/generic-local']);
  });
});
