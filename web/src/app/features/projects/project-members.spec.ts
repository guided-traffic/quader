import { ComponentFixture } from '@angular/core/testing';
import { ProjectsApi } from '../../api/projects-api';
import { SessionApi } from '../../api/session-api';
import { FakeSessionApi } from '../../api/testing/fakes';
import { FakeProjectsApi } from '../../api/testing/fakes-projects';
import { buttonByText, choosePrincipal, expectAccessible, inProject, renderScreen, searchList, settle, sleep, text, type } from '../../testing/screen';
import { ProjectMembersPage } from './project-members-page';

const overlay = (): HTMLElement => document.querySelector('.cdk-overlay-container') as HTMLElement;

async function removeMember(fixture: ComponentFixture<unknown>, members: HTMLElement, principal: string): Promise<void> {
  // The row's one action is its word, named for the member (R-UI-163).
  const remove = [...members.querySelectorAll<HTMLButtonElement>('td.actions button')].find((b) => text(b) === `Remove ${principal}`);
  if (!remove) throw new Error(`no Remove for ${principal}`);
  expect(remove.classList).toContain('btn-tertiary');
  remove.click();
  await settle(fixture);
  await sleep(fixture, 100);
}

type Get = <A>(token: abstract new (...args: never[]) => A) => A;

/** A caller without any capability that is a member of `web` with `role` (Q-AUTHZ-207). */
function asMember(role: 'reader' | 'developer' | 'maintainer' | 'owner') {
  return (get: Get): void => {
    (get(SessionApi) as FakeSessionApi).meResult = {
      roles: [],
      capabilities: [],
      principals: ['user:alice'],
      projects: [{ key: 'web', role }],
    };
  };
}

describe('Project members (R-UI-041)', () => {
  it('R-UI-041 R-UI-150 R-UI-163 an Owner adds a member found by the lookup with a role, changes it and removes it with the row\'s word Remove', async () => {
    const { fixture, root, get } = await renderScreen(ProjectMembersPage, {
      inputs: { project: 'web' },
      capabilities: [],
      setup: asMember('owner'),
    });
    const members = root.querySelector('quader-project-members') as HTMLElement;
    expect(text(members)).toContain('web-leads');
    buttonByText(root, 'Add member').click();
    await settle(fixture);
    const dialog = overlay().querySelector('quader-project-member-dialog') as HTMLElement;
    await choosePrincipal(fixture, dialog, 'User or group', 'bo', '@bob');
    expect((get(ProjectsApi) as FakeProjectsApi).lookups.at(-1)).toEqual({ key: 'web', q: 'bo', kinds: ['user', 'group'] });
    type(dialog.querySelector<HTMLSelectElement>('select[name="new-role"]')!, 'maintainer');
    await settle(fixture);
    buttonByText(dialog, 'Add member').click();
    await sleep(fixture, 100);
    const api = get(ProjectsApi) as FakeProjectsApi;
    expect(api.calls).toEqual(['setMember web user:bob maintainer']);
    expect(text(members)).toContain('bob');
    type(members.querySelector<HTMLSelectElement>('select[name="role-user:bob"]')!, 'reader');
    await sleep(fixture, 100);
    expect(api.calls).toContain('setMember web user:bob reader');
    await removeMember(fixture, members, 'user:bob');
    expect(api.calls).toContain('removeMember web user:bob');
    expect(text(members)).not.toContain('bob');
  });

  it('R-UI-041 a member shows its display name, its user name, its source and who the caller is', async () => {
    const { root } = await renderScreen(ProjectMembersPage, {
      inputs: { project: 'web' },
      capabilities: [],
      setup: (get) => {
        asMember('owner')(get);
        (get(ProjectsApi) as FakeProjectsApi).memberLists['web'] = [
          { project: 'web', principal: 'user:alice', kind: 'user', name: 'alice', id: 'a1', displayName: 'Alice Doe', avatarUrl: '/api/v1/avatars/abc', role: 'owner' },
          { project: 'web', principal: 'group:web-leads', kind: 'group', name: 'web-leads', role: 'developer' },
        ];
      },
    });
    const members = root.querySelector('quader-project-members') as HTMLElement;
    const row = members.querySelector('tbody tr') as HTMLElement;
    expect(text(row)).toContain('Alice Doe');
    expect(text(row)).toContain('@alice');
    expect(text(row)).toContain("It's you");
    expect(text(row)).toContain('Direct member');
    expect(row.querySelector('quader-avatar img')?.getAttribute('src')).toBe('/api/v1/avatars/abc');
    // Where the columns do not fit the rows stack (Q-UI-680); the role's cell carries its column's name for its stacked form.
    expect(row.querySelector('td[data-label="Role"] select')).toBeTruthy();
  });

  it('R-UI-142 the list has the head of every list and no tab strip; the number of members follows the heading', async () => {
    const { root } = await renderScreen(ProjectMembersPage, {
      inputs: { project: 'web' },
      capabilities: [],
      setup: (get) => {
        asMember('owner')(get);
        (get(ProjectsApi) as FakeProjectsApi).memberLists['web'] = [
          { project: 'web', principal: 'user:alice', kind: 'user', name: 'alice', role: 'owner' },
          { project: 'web', principal: 'group:web-leads', kind: 'group', name: 'web-leads', role: 'developer' },
        ];
      },
    });
    expect(text(root.querySelector('h1')!)).toBe('Members 2');
    expect(root.querySelector('h1 .badge')?.textContent).toBe('2');
    expect(root.querySelector('.tab-nav, .tab-link, [role="tablist"]')).toBeNull();
    const members = root.querySelector('quader-project-members') as HTMLElement;
    // The search field of a name's width; no Filters button and no drawer, because the list has no other filter (R-UI-100).
    expect(members.querySelector('quader-list-head .list-search input')?.getAttribute('aria-label')).toBe('Filter members');
    expect(members.querySelector('quader-filter-toggle')).toBeNull();
    // The list is the shared data table; its order is chosen in its headers, not with a sort menu.
    expect(members.querySelector('quader-data-table table.data')).toBeTruthy();
    const sortable = [...members.querySelectorAll('thead th[aria-sort]')].map((th) => text(th).replace(/\s*(unfold_more|arrow_upward|arrow_downward)$/, ''));
    expect(sortable).toEqual(['Account', 'Role']);
    expect(members.querySelector('button[aria-label="Reverse the order"]')).toBeNull();
    await expectAccessible(root);
  });

  it('R-UI-041 R-UI-142 the search narrows the list by name and the column headers sort it', async () => {
    const { fixture, root } = await renderScreen(ProjectMembersPage, {
      inputs: { project: 'web' },
      capabilities: [],
      setup: (get) => {
        asMember('owner')(get);
        (get(ProjectsApi) as FakeProjectsApi).memberLists['web'] = [
          { project: 'web', principal: 'user:bob', kind: 'user', name: 'bob', role: 'owner' },
          { project: 'web', principal: 'user:alice', kind: 'user', name: 'alice', role: 'reader' },
          { project: 'web', principal: 'user:carol', kind: 'user', name: 'carol', role: 'developer' },
        ];
      },
    });
    const members = root.querySelector('quader-project-members') as HTMLElement;
    const names = (): (string | undefined)[] => Array.from(members.querySelectorAll('tbody .name')).map((n) => n.textContent?.trim());
    const header = (label: string): HTMLButtonElement =>
      [...members.querySelectorAll<HTMLButtonElement>('thead .th-sort')].find((b) => text(b).startsWith(label))!;
    expect(names()).toEqual(['alice', 'bob', 'carol']);
    expect(header('Account').closest('th')?.getAttribute('aria-sort')).toBe('ascending');
    header('Account').click();
    await settle(fixture);
    expect(names()).toEqual(['carol', 'bob', 'alice']);
    // Role sorts the strongest role first, then by account.
    header('Role').click();
    await settle(fixture);
    expect(names()).toEqual(['bob', 'carol', 'alice']);
    expect(header('Role').closest('th')?.getAttribute('aria-sort')).toBe('ascending');
    await searchList(fixture, members, 'ali');
    expect(names()).toEqual(['alice']);
    await searchList(fixture, members, 'nobody');
    expect(text(members)).toContain('No member matches.');
  });

  it('R-UI-005 R-UI-270 an Owner sees the last Owner\'s role and Remove greyed out with the reason, and neither acts', async () => {
    const { fixture, root, get } = await renderScreen(ProjectMembersPage, {
      inputs: { project: 'web' },
      capabilities: [],
      setup: asMember('owner'),
    });
    const members = root.querySelector('quader-project-members') as HTMLElement;
    const reason = 'A project keeps at least one Owner. Make another member an Owner first.';
    const describedBy = (el: Element): string | undefined => document.getElementById(el.getAttribute('aria-describedby') ?? '')?.textContent ?? undefined;
    const remove = [...members.querySelectorAll<HTMLButtonElement>('td.actions button')].find((b) => text(b) === 'Remove group:web-leads')!;
    const role = members.querySelector<HTMLSelectElement>('select[name="role-group:web-leads"]')!;
    for (const control of [remove, role]) {
      expect(control.disabled).toBe(false);
      expect(control.getAttribute('aria-disabled')).toBe('true');
      expect(describedBy(control)).toBe(reason);
    }
    remove.click();
    await sleep(fixture, 100);
    expect(text(members)).not.toContain(reason);
    expect((get(ProjectsApi) as FakeProjectsApi).memberLists['web']).toHaveLength(1);
    await expectAccessible(root);
  });

  it('R-UI-041 an administrator keeps the last Owner\'s Remove, and a refusal of the server is shown above the list', async () => {
    const { fixture, root, get } = await renderScreen(ProjectMembersPage, {
      inputs: { project: 'web' },
      capabilities: ['projects:write'],
    });
    const members = root.querySelector('quader-project-members') as HTMLElement;
    const role = members.querySelector<HTMLSelectElement>('select[name="role-group:web-leads"]')!;
    expect(role.getAttribute('aria-disabled')).toBeNull();
    // The fake refuses as the server does for an Owner; the screen shows what the server answered.
    await removeMember(fixture, members, 'group:web-leads');
    expect(text(members)).toContain('A project keeps at least one Owner');
    expect((get(ProjectsApi) as FakeProjectsApi).memberLists['web']).toHaveLength(1);
  });

  it('R-UI-041 a Reader sees the members and the roles as text', async () => {
    const { root } = await renderScreen(ProjectMembersPage, {
      inputs: { project: 'web' },
      capabilities: [],
      setup: asMember('reader'),
    });
    const members = root.querySelector('quader-project-members') as HTMLElement;
    expect(text(members)).toContain('web-leads');
    expect(text(members)).toContain('Owner');
    expect(members.querySelector('select')).toBeNull();
    expect(members.querySelector('button[aria-label^="Actions for"]')).toBeNull();
    expect(members.querySelector('td.actions button')).toBeNull();
    expect(root.textContent).not.toContain('Add member');
  });

  it('R-UI-041 a Reader is told whom to ask, the Owners by name, and no permission name', async () => {
    const { root } = await renderScreen(
      ProjectMembersPage,
      inProject({ capabilities: [], setup: asMember('reader') }, '/projects/web/members'),
    );
    // Q-UI-318: a person to ask, not `projects:write`.
    expect(text(root)).toContain('Read only. Ask web-leads, an Owner of web, to change this.');
    expect(text(root)).not.toContain('projects:write');
    await expectAccessible(root);
  });
});
