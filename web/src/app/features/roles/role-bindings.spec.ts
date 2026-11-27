import { RolesApi } from '../../api/authz-api';
import { FakeRolesApi, problem } from '../../api/testing/access-fakes';
import { SessionApi } from '../../api/session-api';
import { FakeSessionApi } from '../../api/testing/fakes';
import { buttonByText, choosePrincipal, renderScreen, settle, text, sleep, unavailableReason } from '../../testing/screen';
import { seriousViolations } from '../../testing/axe';
import { RoleBindings } from './role-bindings';

const overlay = (): HTMLElement => document.querySelector('.cdk-overlay-container') as HTMLElement;

describe('System roles (R-UI-024)', () => {
  it('R-UI-024 lists the five fixed roles with capabilities and holders, and passes axe', async () => {
    const { root } = await renderScreen(RoleBindings);
    const sections = root.querySelectorAll('section.card');
    expect(sections).toHaveLength(5);
    expect(text(sections[0]!)).toContain('admin');
    expect(text(sections[0]!)).toContain('group:platform');
    expect(text(sections[2]!)).toContain('user:bob');
    expect(text(sections[3]!)).toContain('Nobody holds this role');
    expect(await seriousViolations(root)).toEqual([]);
  });

  it('R-UI-005 R-UI-270 the last administrator binding cannot be removed: Unbind is unavailable with the reason as its tooltip', async () => {
    const { fixture, root, get } = await renderScreen(RoleBindings);
    const adminRow = root.querySelector('section.card tbody tr') as HTMLElement;
    const unbind = adminRow.querySelector('button') as HTMLButtonElement;
    expect(unbind.getAttribute('aria-disabled')).toBe('true');
    expect(unavailableReason(unbind)).toBe('The last administrator binding cannot be removed.');
    expect(text(root)).not.toContain('cannot be removed');
    unbind.click();
    await settle(fixture);
    expect(overlay()?.querySelector('quader-confirm-dialog')).toBeNull();
    expect((get(RolesApi) as FakeRolesApi).calls.some((c) => c.startsWith('unbind'))).toBe(false);
  });

  it('R-AUTHZ-022 R-UI-150 binds a role to a principal chosen from the search, reads the lists again and refreshes the caller\'s capabilities', async () => {
    const { fixture, root, get } = await renderScreen(RoleBindings);
    const me = get(SessionApi) as FakeSessionApi;
    let refreshed = 0;
    const original = me.authzMe.bind(me);
    me.authzMe = async () => {
      refreshed++;
      return original();
    };
    const auditor = root.querySelectorAll('section.card')[2] as HTMLElement;
    await choosePrincipal(fixture, auditor, 'Bind to', 'leads', 'web-leads');
    buttonByText(auditor, 'Bind').click();
    await settle(fixture);
    expect((get(RolesApi) as FakeRolesApi).calls).toContain('bind auditor group:web-leads');
    expect(text(root.querySelectorAll('section.card')[2]!)).toContain('group:web-leads');
    expect(refreshed).toBe(1);
  });

  it('R-AUTHZ-022 unbinds after confirmation', async () => {
    const { fixture, root, get } = await renderScreen(RoleBindings);
    const auditor = root.querySelectorAll('section.card')[2] as HTMLElement;
    auditor.querySelector('tbody button')!.dispatchEvent(new Event('click'));
    await settle(fixture);
    buttonByText(overlay().querySelector('quader-confirm-dialog') as HTMLElement, 'Unbind').click();
    await sleep(fixture, 300);
    expect((get(RolesApi) as FakeRolesApi).calls).toContain('unbind auditor user:bob');
  });

  it('R-UI-042 a refused bind shows the translated reason', async () => {
    const { fixture, root, get } = await renderScreen(RoleBindings);
    (get(RolesApi) as FakeRolesApi).failure = problem(409, 'auto_join_admin_conflict');
    const admin = root.querySelectorAll('section.card')[0] as HTMLElement;
    await choosePrincipal(fixture, admin, 'Bind to', 'leads', 'web-leads');
    buttonByText(admin, 'Bind').click();
    await settle(fixture);
    expect(text(root)).toContain('An auto-join group cannot hold the administrator role');
  });

  it('R-AUTHZ-010 R-UI-270 shows the source of every binding; Unbind of one from the configuration is unavailable, the key its tooltip', async () => {
    const { root } = await renderScreen(RoleBindings, {
      setup: (get) => {
        const api = get(RolesApi) as FakeRolesApi;
        api.bindingList = [
          ...api.bindingList,
          {
            role: 'admin',
            principal: 'group:gh-wds18-k8s-main-admins',
            grantedAt: '2026-11-02T08:00:00Z',
            grantedBy: '',
            source: 'configuration',
          },
        ];
      },
    });
    const admin = root.querySelectorAll('section.card')[0] as HTMLElement;
    const rows = Array.from(admin.querySelectorAll('tbody tr')) as HTMLElement[];
    const declared = rows.find((r) => text(r).includes('group gh-wds18-k8s-main-admins'))!;
    const made = rows.find((r) => text(r).includes('group platform'))!;
    expect(text(declared)).toContain('Helm configuration');
    // No granter: the dash of a cell without a value, never an empty cell.
    expect(declared.querySelectorAll('td')[1]!.querySelector('quader-no-value')).not.toBeNull();
    const fixed = declared.querySelector('button') as HTMLButtonElement;
    expect(fixed.getAttribute('aria-disabled')).toBe('true');
    expect(unavailableReason(fixed)).toBe('Declared in config.authz.role_bindings of the Helm configuration: remove it there.');
    // No paragraph says it again beside the table.
    expect(text(admin)).not.toContain('authz.role_bindings');
    expect(text(made)).toContain('API');
    // Two holders of admin: the binding made through the API may go, the configuration's stays.
    expect(made.querySelector('button')!.hasAttribute('aria-disabled')).toBe(false);
    expect(await seriousViolations(root)).toEqual([]);
  });

  it('R-UI-042 a refused unbind of a binding from the configuration shows the translated reason', async () => {
    const { fixture, root, get } = await renderScreen(RoleBindings);
    (get(RolesApi) as FakeRolesApi).failure = problem(409, 'role_binding_managed_by_configuration');
    const auditor = root.querySelectorAll('section.card')[2] as HTMLElement;
    auditor.querySelector('tbody button')!.dispatchEvent(new Event('click'));
    await settle(fixture);
    buttonByText(overlay().querySelector('quader-confirm-dialog') as HTMLElement, 'Unbind').click();
    await sleep(fixture, 300);
    expect(text(root)).toContain('This role binding is declared in config.authz.role_bindings of the Helm configuration and can only be removed there.');
  });

  it('R-UI-005 without roles:write there is no bind or unbind', async () => {
    const { root } = await renderScreen(RoleBindings, { capabilities: ['roles:read'] });
    expect(root.querySelector('tbody button')).toBeNull();
    expect(root.querySelector('form')).toBeNull();
  });
});
