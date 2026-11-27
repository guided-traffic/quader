import { BastionProject, BastionsApi } from '../../api/bastions-api';
import { FakeBastionsApi, sampleBastion, sampleBastionProject } from '../../api/testing/fakes-bastions';
import { seriousViolations } from '../../testing/axe';
import { buttonByText, choosePrincipal, renderScreen, settle, sleep, text, typeInto } from '../../testing/screen';
import { BastionList } from './bastion-list';
import { BastionProjectsPage, distributedNote } from './bastion-projects';

type Get = <A>(token: abstract new (...args: never[]) => A) => A;

const overlay = (): HTMLElement => document.querySelector('.cdk-overlay-container') as HTMLElement;
const card = (root: HTMLElement, name: string): HTMLElement =>
  [...root.querySelectorAll<HTMLElement>('quader-bastion-card')].find((c) => c.querySelector('[data-testid="bastion-name"]')?.textContent?.trim() === name)!;
const section = (root: HTMLElement, name: string): HTMLElement => card(root, name).querySelector<HTMLElement>('[data-testid="bastion-projects"]')!;
const toggle = (root: HTMLElement, name: string): HTMLButtonElement => section(root, name).querySelector<HTMLButtonElement>('button[aria-expanded]')!;
const rows = (el: ParentNode): HTMLElement[] => [...el.querySelectorAll<HTMLElement>('tbody tr')];

/** `n` projects `p01` … allowed for `partners`, each with one repository distributed. */
function many(n: number): BastionProject[] {
  return Array.from({ length: n }, (_, i) => {
    const key = `p${String(i + 1).padStart(2, '0')}`;
    return sampleBastionProject({ project: key, displayName: `Project ${key}`, repositories: [`${key}-npm`] });
  });
}

/** `partners` allows Platform engineering (two repositories) and Upstream (none chosen yet); `standby` allows none. */
function twoAllowed(get: Get): void {
  (get(BastionsApi) as FakeBastionsApi).allowed.set('partners', [
    sampleBastionProject(),
    sampleBastionProject({ project: 'upstream', displayName: 'Upstream', repositories: [] }),
  ]);
}

describe("A Bastion's projects on its card and its page (R-UI-240, R-BAS-134)", () => {
  it("R-BAS-134 R-UI-240 the card's section Projects is one closed line: the projects allowed and the repositories distributed, or that none may distribute yet", async () => {
    const { root } = await renderScreen(BastionList, { setup: twoAllowed });
    const partners = toggle(root, 'partners');
    expect(partners.getAttribute('aria-expanded')).toBe('false');
    expect(text(partners)).toBe('chevron_rightProjects 22 repositories distributed');
    // The section's help behind the sign after its line, outside the toggle (R-UI-267).
    const sign = section(root, 'partners').querySelector('h3 > quader-help button.help-sign')!;
    expect(sign.getAttribute('aria-label')).toBe('About Projects');
    // A writer's section without a project opens to the act that fills it; no count.
    expect(text(toggle(root, 'standby'))).toBe('chevron_rightProjectsNo project may distribute through it yet.');
    expect([distributedNote(0), distributedNote(1), distributedNote(5)]).toEqual([
      'No repository distributed yet',
      '1 repository distributed',
      '5 repositories distributed',
    ]);
    // The section stands between Limits' rows and Details.
    const order = [...card(root, 'partners').querySelectorAll('quader-bastion-limits .sections > quader-bastion-projects, quader-bastion-limits .sections > quader-disclosure.details')];
    expect(order.map((e) => e.tagName.toLowerCase())).toEqual(['quader-bastion-projects', 'quader-disclosure']);
    expect(await seriousViolations(root)).toEqual([]);
  });

  it('R-BAS-134 R-UI-240 opened: each project with its mark, name and key, the repositories distributed there or None yet, and when and by whom it was allowed', async () => {
    const { fixture, root } = await renderScreen(BastionList, {
      setup: (g) =>
        (g(BastionsApi) as FakeBastionsApi).allowed.set('partners', [
          sampleBastionProject({ repositories: ['a', 'b', 'c', 'd', 'e', 'f', 'g'] }),
          sampleBastionProject({ project: 'upstream', displayName: 'Upstream', repositories: [] }),
        ]),
    });
    toggle(root, 'partners').click();
    await settle(fixture);
    const [platform, upstream] = rows(section(root, 'partners'));
    expect(text(platform!.querySelector('th')!)).toBe('Platform engineeringplatform');
    expect(platform!.querySelector('th a')!.getAttribute('href')).toBe('/projects/platform');
    // The first five keys, each a link to its repository, then how many more.
    const cell = platform!.querySelector('[data-testid="bastion-project-repositories"]')!;
    expect([...cell.querySelectorAll('a')].map((a) => a.getAttribute('href'))).toEqual(
      ['a', 'b', 'c', 'd', 'e'].map((r) => `/projects/platform/repositories/${r}`),
    );
    expect(text(cell)).toBe('abcdeand 2 more');
    expect(text(upstream!.querySelector('[data-testid="bastion-project-repositories"]')!)).toBe('None yet');
    expect(text(platform!.querySelectorAll('td')[1]!)).toBe('2 days agoroot');
    expect(await seriousViolations(section(root, 'partners'))).toEqual([]);
  });

  it('R-BAS-134 Allow a project lists the matching projects while one types; Allow sends the one chosen and the section lists it', async () => {
    const { fixture, root, get } = await renderScreen(BastionList);
    toggle(root, 'standby').click();
    await settle(fixture);
    const form = section(root, 'standby').querySelector<HTMLElement>('[data-testid="bastion-projects-allow"]')!;
    const allow = buttonByText(form, 'Allow') as HTMLButtonElement;
    expect(allow.disabled).toBe(true);
    await choosePrincipal(fixture, form, 'Allow a project', 'plat', 'Platform');
    // The chip names the project by its display name; Allow is offered now.
    expect(text(form.querySelector('.chip')!)).toContain('Platform');
    expect(allow.disabled).toBe(false);
    allow.click();
    await sleep(fixture, 100);
    expect((get(BastionsApi) as FakeBastionsApi).calls).toContain('allowProject standby platform');
    expect(text(toggle(root, 'standby'))).toBe('chevron_rightProjects 1No repository distributed yet');
    expect(rows(section(root, 'standby')).map((r) => text(r.querySelector('th')!))).toEqual(['platformplatform']);
    // The field is empty again and no longer offers what the list holds.
    expect(form.querySelector('.chip')).toBeNull();
    typeInto(form, 'Allow a project', 'plat');
    await sleep(fixture, 350);
    expect([...document.querySelectorAll('quader-principal-field [role="option"]')].map((o) => text(o))).toEqual(['Web platformweb']);
  });

  it('R-BAS-134 Remove says that the distributed repositories stop and their owners choose again, and takes the project off', async () => {
    const { fixture, root, get } = await renderScreen(BastionList, { setup: twoAllowed });
    toggle(root, 'partners').click();
    await settle(fixture);
    buttonByText(rows(section(root, 'partners'))[0]!, 'Remove').click();
    await settle(fixture);
    const dialog = overlay().querySelector('quader-confirm-dialog') as HTMLElement;
    expect(text(dialog)).toContain('Remove platform from partners?');
    expect(text(dialog)).toContain(
      "Its 2 distributed repositories are no longer served there, within seconds. Their owners' choices go with it: allowing the project again distributes nothing until they choose again.",
    );
    buttonByText(dialog, 'Remove').click();
    await sleep(fixture, 100);
    expect((get(BastionsApi) as FakeBastionsApi).calls).toContain('removeProject partners platform');
    expect(rows(section(root, 'partners')).map((r) => text(r.querySelector('th')!))).toEqual(['Upstreamupstream']);
    expect(text(toggle(root, 'partners'))).toBe('chevron_rightProjects 1No repository distributed yet');
  });

  it('R-UI-240 R-BAS-134 the card shows ten of eleven projects and leads to the page with all of them', async () => {
    const { fixture, root, get } = await renderScreen(BastionList, { setup: (g) => (g(BastionsApi) as FakeBastionsApi).allowed.set('partners', many(11)) });
    expect((get(BastionsApi) as FakeBastionsApi).projectReads.find((r) => r.name === 'partners')?.query.limit).toBe(10);
    toggle(root, 'partners').click();
    await settle(fixture);
    expect(text(toggle(root, 'partners'))).toBe('chevron_rightProjects 1111 repositories distributed');
    expect(rows(section(root, 'partners'))).toHaveLength(10);
    const all = section(root, 'partners').querySelector('[data-testid="bastion-projects-all"]')!;
    expect(text(all)).toBe('All 11 projects');
    expect(all.getAttribute('href')).toBe('/admin/bastions/partners/projects');
  });

  it('R-UI-240 R-BAS-134 the page lists every project with the filter by key or name and Load more; passes axe', async () => {
    const { fixture, root, get } = await renderScreen(BastionProjectsPage, {
      inputs: { name: 'partners' },
      setup: (g) => {
        const api = g(BastionsApi) as FakeBastionsApi;
        api.bastions = [sampleBastion()];
        api.allowed.set('partners', many(60));
      },
    });
    expect(text(root.querySelector('h1')!)).toBe('Projects of partners');
    expect(text(root.querySelector('quader-page-header')!)).toContain('The projects whose owners may distribute their repositories through this Bastion. A new Bastion allows none.');
    expect(rows(root)).toHaveLength(50);
    buttonByText(root, 'Load more').click();
    await sleep(fixture, 50);
    expect(rows(root)).toHaveLength(60);
    const filter = root.querySelector<HTMLInputElement>('quader-list-head input[type="search"]')!;
    filter.value = 'p07';
    filter.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true }));
    await sleep(fixture, 50);
    expect(rows(root).map((r) => text(r.querySelector('th')!))).toEqual(['Project p07p07']);
    expect((get(BastionsApi) as FakeBastionsApi).projectReads.at(-1)?.query).toMatchObject({ q: 'p07', limit: 50 });
    expect(root.querySelector('[data-testid="bastion-projects-all"]')).toBeNull();
    expect(await seriousViolations(root)).toEqual([]);
  });

  it('R-BAS-134 endpoints:read alone sees the projects and no act; without a project, the section is its one line', async () => {
    const { fixture, root } = await renderScreen(BastionList, { capabilities: ['endpoints:read'], setup: twoAllowed });
    toggle(root, 'partners').click();
    await settle(fixture);
    expect(section(root, 'partners').querySelector('form')).toBeNull();
    expect(rows(section(root, 'partners'))).toHaveLength(2);
    expect([...section(root, 'partners').querySelectorAll('tbody button')]).toEqual([]);
    expect(text(section(root, 'standby'))).toMatch(/^Projects\s*info\s*No project may distribute through it yet\.$/);
    expect(section(root, 'standby').querySelector('button[aria-expanded]')).toBeNull();
  });
});
