import { ComponentFixture } from '@angular/core/testing';
import { ProjectsApi } from '../../api/projects-api';
import { SessionApi } from '../../api/session-api';
import { FakeSessionApi } from '../../api/testing/fakes';
import { FakeProjectsApi } from '../../api/testing/fakes-projects';
import { buttonByText, choosePrincipal, expectAccessible, inProject, renderScreen, searchList, settle, sleep, text, type } from '../../testing/screen';
import { ProjectMembersPage } from './project-members-page';

const overlay = (): HTMLElement => document.querySelector('.cdk-overlay-container') as HTMLElement;

async function removeMember(fixture: ComponentFixture<unknown>, members: HTMLElement, principal: string): Promise<void> {
  members.querySelector<HTMLButtonElement>(`button[aria-label="Actions for ${principal}"]`)!.click();
  await settle(fixture);
  buttonByText(overlay(), 'Remove member').click();
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

describe('Project members (R-UI-041, Q-AUTHZ-207, Q-AUTHZ-217)', () => {
  it('Q-AUTHZ-207 R-UI-150 an Owner adds a member found by the lookup with a role, changes it and removes it', async () => {
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

  it('R-UI-041 Q-UI-214 a member shows its display name, its user name, its source and who the caller is', async () => {
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
    // Below 48 rem the rows stack (Q-UI-306); the role's cell carries its column's name for its own line.
    expect(row.querySelector('td[data-label="Role"] select')).toBeTruthy();
  });

  it('R-UI-142 Q-UI-550 the list has the head of every list and no tab strip; the number of members follows the heading', async () => {
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

  it('R-UI-041 Q-UI-214 R-UI-142 the search narrows the list by name and the column headers sort it', async () => {
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

  it('Q-AUTHZ-217 removing or demoting the last Owner is refused and the refusal is shown', async () => {
    const { fixture, root, get } = await renderScreen(ProjectMembersPage, {
      inputs: { project: 'web' },
      capabilities: [],
      setup: asMember('owner'),
    });
    const members = root.querySelector('quader-project-members') as HTMLElement;
    await removeMember(fixture, members, 'group:web-leads');
    expect(text(members)).toContain('A project keeps at least one Owner');
    expect((get(ProjectsApi) as FakeProjectsApi).memberLists['web']).toHaveLength(1);
    type(members.querySelector<HTMLSelectElement>('select[name="role-group:web-leads"]')!, 'reader');
    await sleep(fixture, 100);
    expect(text(members)).toContain('A project keeps at least one Owner');
    expect((get(ProjectsApi) as FakeProjectsApi).memberLists['web']![0]!.role).toBe('owner');
  });

  it('Q-AUTHZ-207 a Reader sees the members and the roles as text', async () => {
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
