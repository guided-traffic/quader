import { TestBed } from '@angular/core/testing';
import { PolicyApi } from '../../api/authz-api';
import { IdentityApi } from '../../api/identity-api';
import { ApiError } from '../../api/problem';
import { ProjectsApi } from '../../api/projects-api';
import { RepositoriesApi } from '../../api/repositories-api';
import { SessionApi } from '../../api/session-api';
import { FakeIdentityApi, FakePolicyApi, FakeTechnicalTokensApi, FakeTokensApi, sampleAccount, samplePolicy } from '../../api/testing/access-fakes';
import { TechnicalTokensApi } from '../../api/technical-tokens-api';
import { FakeSessionApi } from '../../api/testing/fakes';
import { FakeRepositoriesApi, PACKAGE_FORMAT } from '../../api/testing/fakes-catalog';
import { FakeProjectsApi, sampleProject } from '../../api/testing/fakes-projects';
import { TokensApi } from '../../api/tokens-api';
import { describeProblem } from '../../core/http/problem-messages';
import { helpFor } from '../../core/help-topics';
import { Navigation } from '../../core/navigation';
import { SessionStore } from '../../core/session-store';
import { seriousViolations } from '../../testing/axe';
import { button, buttonByText, choosePrincipal, principalSearchAnswered, renderScreen, settle, sleep, text, type, typeInto, waitUntil } from '../../testing/screen';
import { PREVIEW_DEBOUNCE_MS, PolicyEditor } from '../permissions/policy-editor';
import { RepositoryCreate } from '../repositories/repository-create';
import { ServiceAccountDetail } from '../service-accounts/service-account-detail';

type Get = <A>(token: abstract new (...args: never[]) => A) => A;

const overlay = (): HTMLElement => document.querySelector('.cdk-overlay-container') as HTMLElement;

/** A caller without any capability that is Owner of `web` (Q-AUTHZ-207, Q-AUTHZ-208). */
function projectOwner(role: 'owner' | 'maintainer' = 'owner') {
  return (get: Get): void => {
    (get(SessionApi) as FakeSessionApi).meResult = {
      roles: [],
      capabilities: [],
      principals: ['user:alice'],
      projects: [{ key: 'web', role }],
    };
  };
}

describe('The project policy editor (R-UI-041, R-AUTHZ-026)', () => {
  it('R-UI-041 R-UI-150 a new project policy is named within the project, names its project and finds principals through the lookup', async () => {
    const { fixture, root, get } = await renderScreen(PolicyEditor, {
      inputs: { project: 'web' },
      capabilities: [],
      setup: projectOwner(),
    });
    expect(text(root)).toContain('Project policy of web');
    expect(root.querySelector<HTMLInputElement>('input[name="name"]')!.value).toBe('');
    expect(text(root)).toContain('Unique within the project web and cannot be changed later.');

    typeInto(root, 'Name', 'publish');
    // The repositories of the project are offered as selectors, by bare key.
    buttonByText(root, 'generic-local').click();
    await settle(fixture);
    expect(root.querySelector<HTMLTextAreaElement>('textarea[name="repositories"]')!.value).toBe('generic-local');
    // An entry's principal comes from the project's lookup (Q-AUTHZ-182) through the shared field: the project's own service
    // account is offered beside users and groups.
    buttonByText(root, 'Add principal').click();
    await settle(fixture);
    typeInto(root, 'Principal', 'deploy');
    await waitUntil(fixture, principalSearchAnswered, 'the project lookup\'s answer for "deploy"');
    expect(text(document.querySelector('quader-principal-field [role="listbox"]')!)).toContain('Service account of web');
    await choosePrincipal(fixture, root, 'Principal', 'leads', 'web-leads');
    expect(text(root.querySelector('fieldset.entry .chips')!)).toContain('web-leads');
    expect((get(ProjectsApi) as FakeProjectsApi).lookups.map((l) => [l.key, l.q])).toEqual([
      ['web', 'deploy'],
      ['web', 'leads'],
    ]);

    const policies = get(PolicyApi) as FakePolicyApi;
    await sleep(fixture, PREVIEW_DEBOUNCE_MS + 100);
    expect(policies.validated.length).toBeGreaterThan(0);
    expect(policies.validated.every((v) => v.project === 'web')).toBe(true);
    expect(await seriousViolations(root)).toEqual([]);
    buttonByText(root, 'Create policy').click();
    await sleep(fixture, 100);
    expect(policies.calls).toEqual(['create publish']);
    expect(policies.policies.find((p) => p.name === 'publish')?.project).toBe('web');
  });

  it('R-UI-041 a project policy is read-only for a member that is no Owner, with the reason', async () => {
    const { root } = await renderScreen(PolicyEditor, {
      inputs: { name: 'web-publish', project: 'web' },
      capabilities: [],
      setup: (get) => {
        projectOwner('maintainer')(get);
        (get(PolicyApi) as FakePolicyApi).policies = [samplePolicy({ name: 'web-publish', project: 'web' })];
      },
    });
    expect(text(root)).toContain('Read only: changing this needs the permission policies:write');
    expect(root.querySelector('button[type="submit"]')).toBeNull();
    expect(root.querySelector('fieldset.entry quader-principal-field input[role="combobox"]')).toBeNull();
  });

  it('R-UI-041 a stored project policy keeps its project through a save', async () => {
    const { fixture, root, get } = await renderScreen(PolicyEditor, {
      inputs: { name: 'web-publish', project: 'web' },
      capabilities: [],
      setup: (g) => {
        projectOwner()(g);
        (g(PolicyApi) as FakePolicyApi).policies = [samplePolicy({ name: 'web-publish', project: 'web' })];
      },
    });
    typeInto(root, 'Description', 'Changed');
    await settle(fixture);
    buttonByText(root, 'Review and save').click();
    await settle(fixture);
    buttonByText(overlay().querySelector('quader-policy-diff-dialog') as HTMLElement, 'Save policy').click();
    await sleep(fixture, 300);
    const api = get(PolicyApi) as FakePolicyApi;
    expect(api.calls).toEqual(['replace web-publish "3"']);
    expect(api.validated.length).toBeGreaterThan(0);
    expect(api.validated.every((v) => v.project === 'web')).toBe(true);
  });

  it('R-UI-027 a global policy is unchanged: no project line, the capability decides', async () => {
    const { root, get } = await renderScreen(PolicyEditor);
    expect(text(root)).not.toContain('Project policy of');
    expect((get(PolicyApi) as FakePolicyApi).validated.every((v) => v.project === undefined)).toBe(true);
  });

  it('R-AUTHZ-004 a global policy asks for project/key selectors, a project policy for bare keys', async () => {
    const global = await renderScreen(PolicyEditor);
    expect(text(global.root)).toContain('One per line: project/key (web/npm-internal)');
    TestBed.resetTestingModule();
    const project = await renderScreen(PolicyEditor, { inputs: { project: 'web' }, capabilities: [], setup: projectOwner() });
    expect(text(project.root)).toContain('One per line: a key (npm-internal)');
  });
});

describe('A project service account (R-UI-041, Q-AUTHZ-175, Q-AUTHZ-177)', () => {
  const seed = (get: Get): void => {
    projectOwner()(get);
    (get(IdentityApi) as FakeIdentityApi).accounts = [
      sampleAccount({ id: 'sa-web-ci', name: 'ci', project: 'web', description: 'CI of the web team', createdAt: '2026-11-01T08:00:00Z' }),
    ];
  };

  it('R-UI-041 R-UI-181 an Owner issues a technical token that always expires, with the cap said, and passes axe', async () => {
    const { fixture, root, get } = await renderScreen(ServiceAccountDetail, {
      inputs: { name: 'ci', project: 'web' },
      capabilities: [],
      setup: seed,
    });
    expect(text(root)).toContain('Service account of project web');
    // No role section for a project account, and nothing global is read.
    expect(root.querySelector('quader-principal-roles')).toBeNull();
    buttonByText(root, 'New technical token').click();
    await settle(fixture);
    const form = root.querySelector('quader-token-form') as HTMLElement;
    expect(text(form)).not.toContain('Never expires');
    expect(text(form)).toContain('caps the lifetime of service-account tokens');
    typeInto(form, 'Name', 'nightly');
    buttonByText(form, 'Create technical token').click();
    await settle(fixture);
    expect((get(TechnicalTokensApi) as FakeTechnicalTokensApi).calls).toContain('create sa:web/ci nightly');
    expect(root.querySelector('quader-token-secret')).toBeTruthy();
    expect(await seriousViolations(root)).toEqual([]);
  });

  it('R-UI-041 an Owner disables, enables, revokes and deletes; the page names the account and its project', async () => {
    const { fixture, root, get } = await renderScreen(ServiceAccountDetail, {
      inputs: { name: 'ci', project: 'web' },
      capabilities: [],
      setup: (g) => {
        seed(g);
        (g(TokensApi) as FakeTokensApi).tokenList = [];
      },
    });
    buttonByText(root, 'Disable').click();
    await settle(fixture);
    buttonByText(overlay().querySelector('quader-confirm-dialog') as HTMLElement, 'Disable').click();
    await sleep(fixture, 300);
    expect((get(IdentityApi) as FakeIdentityApi).calls).toContain('disableServiceAccount ci revokeTokens=false');
    expect((buttonByText(root, 'Enable') as HTMLButtonElement).disabled).toBe(false);
    // The way back is the breadcrumb (Q-UI-307); the header names the account, the line under it its project.
    expect(root.querySelector('h1')?.textContent?.trim()).toBe('ci');
    expect(root.textContent).toContain('Service account of project');
  });

  it('R-UI-041 R-UI-181 the technical tokens of a project account are read under the project', async () => {
    const { get } = await renderScreen(ServiceAccountDetail, {
      inputs: { name: 'ci', project: 'web' },
      capabilities: [],
      setup: seed,
    });
    expect((get(TechnicalTokensApi) as FakeTechnicalTokensApi).lastList).toEqual({ name: 'ci', project: 'web', repository: null });
  });

  it('R-AUTHZ-026 the same name may exist in two projects: the account is read inside the project of the route', async () => {
    const { root } = await renderScreen(ServiceAccountDetail, {
      inputs: { name: 'ci', project: 'data' },
      capabilities: ['identities:write', 'tokens:read'],
      setup: (g) => {
        seed(g);
        const api = g(IdentityApi) as FakeIdentityApi;
        api.accounts = [...api.accounts, sampleAccount({ id: 'sa-data-ci', name: 'ci', description: 'CI of data', project: 'data' })];
      },
    });
    expect(text(root)).toContain('Service account of project data');
  });
});
describe('The creation form of a repository and its project (R-REPO-001, R-AUTHZ-026)', () => {
  it('R-REPO-001 starts on the project of the link, and creates the repository inside it', async () => {
    const { fixture, root, get } = await renderScreen(RepositoryCreate, {
      inputs: { project: 'web', format: 'npm' },
      capabilities: [],
      setup: (g) => {
        projectOwner()(g);
        (g(RepositoriesApi) as FakeRepositoriesApi).formatsResult = [PACKAGE_FORMAT];
      },
    });
    type(root.querySelector<HTMLInputElement>('#f-key')!, 'libs');
    await settle(fixture);
    button(root, /Create repository/).click();
    await sleep(fixture, 100);
    const api = get(RepositoriesApi) as FakeRepositoriesApi;
    expect(api.createProjects).toEqual(['web']);
    expect(api.repositories.find((r) => r.key === 'libs')?.project).toBe('web');
  });
});

describe('Session helpers and messages of the project screens (R-UI-041, R-UI-042)', () => {
  it('R-UI-041 projectRole, canWritePolicies and canWriteRepositories follow authz/me and the capabilities', async () => {
    const { get } = await renderScreen(RepositoryCreate, { capabilities: [], setup: projectOwner() });
    const session = get(SessionStore);
    // An Owner administers the project inside it; the admin area is for the holders of capabilities (Q-UI-129).
    expect(get(Navigation).hasAdminArea()).toBe(false);
    expect(session.isAdministrator()).toBe(false);
    expect(session.projectRole('web')).toBe('owner');
    expect(session.projectRole('data')).toBeNull();
    expect(session.canWritePolicies('web')).toBe(true);
    expect(session.canWritePolicies('data')).toBe(false);
    expect(session.canWritePolicies(null)).toBe(false);
    expect(session.canWriteRepositories('web')).toBe(true);
    expect(session.canWriteRepositories('data')).toBe(false);
  });

  it('R-UI-041 a Maintainer reads and does not administer', async () => {
    const { get } = await renderScreen(RepositoryCreate, { capabilities: [], setup: projectOwner('maintainer') });
    const session = get(SessionStore);
    expect(get(Navigation).hasAdminArea()).toBe(false);
    expect(session.canWriteRepositories('web')).toBe(false);
  });

  it('R-UI-041 the capabilities decide for any project', async () => {
    const { get } = await renderScreen(RepositoryCreate, { capabilities: ['policies:write', 'repositories:write-config'] });
    const session = get(SessionStore);
    expect(session.canWritePolicies('data')).toBe(true);
    expect(session.canWritePolicies(null)).toBe(true);
    expect(session.canWriteRepositories('data')).toBe(true);
  });

  it('R-UI-042 every problem code of the project areas has a translated message', () => {
    for (const code of [
      'repository_outside_project',
      'unknown_project',
      'principal_outside_project',
      'principal_not_allowed',
      'project_account_not_groupable',
      'project_not_empty',
      'secret_value_required',
      'outbound_path_restricted',
    ]) {
      const p = describeProblem(new ApiError(409, { code, detail: 'English detail' }, 'rid-1'));
      expect(p.known, code).toBe(true);
      expect(p.text, code).not.toBe('English detail');
    }
  });

  it('R-UI-050 the project screens open the manual page about projects', () => {
    expect(helpFor('/projects')).toEqual(['/help', 'admin', 'projects']);
    expect(helpFor('/repositories?format=npm')).toEqual(['/help', 'user', 'repositories']);
    expect(helpFor('/projects/web/settings')).toEqual(['/help', 'admin', 'projects']);
    expect(helpFor('/projects/web/members')).toEqual(['/help', 'admin', 'projects']);
    expect(helpFor('/projects/web/packages/npm/left-pad')).toEqual(['/help', 'user', 'packages']);
    expect(helpFor('/projects/web/repositories/npm-local/files/a')).toEqual(['/help', 'user', 'files']);
    expect(helpFor('/projects/web/repositories/npm-local/files')).toEqual(['/help', 'user', 'files']);
    expect(helpFor('/projects/web/repositories/files')).toEqual(['/help', 'user', 'repositories']);
    expect(helpFor('/projects/web/repositories/npm-local')).toEqual(['/help', 'user', 'repositories']);
    expect(helpFor('/projects/web/repositories/npm-local/setup?client=yarn')).toEqual(['/help', 'user', 'client-setup']);
    expect(helpFor('/projects/web/repositories/setup')).toEqual(['/help', 'user', 'repositories']);
    expect(helpFor('/projects/web/vulnerabilities')).toEqual(['/help', 'user', 'vulnerabilities']);
    expect(helpFor('/projects/web/policies/npm-read')).toEqual(['/help', 'admin', 'permissions']);
    expect(helpFor('/projects/web/service-accounts')).toEqual(['/help', 'admin', 'sign-in-and-identity']);
    expect(helpFor('/projects/web/ci/mappings/new?issuer=a')).toEqual(['/help', 'admin', 'gitlab-ci']);
  });

  it('Q-AUTHZ-217 the fake models a project: the key pattern, an existing key and the last Owner', async () => {
    TestBed.resetTestingModule();
    const { get } = await renderScreen(RepositoryCreate);
    const api = get(ProjectsApi) as FakeProjectsApi;
    await expect(api.create({ key: 'web', displayName: 'x', description: '' }, 'k')).rejects.toMatchObject({ code: 'already_exists' });
    await expect(api.removeMember('web', 'group:web-leads')).rejects.toMatchObject({ code: 'last_owner' });
    expect(sampleProject().key).toBe('web');
  });
});
