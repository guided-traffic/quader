import { IdentityApi } from '../../api/identity-api';
import { TechnicalTokensApi } from '../../api/technical-tokens-api';
import { TokensApi } from '../../api/tokens-api';
import {
  FakeIdentityApi,
  FakeTechnicalTokensApi,
  FakeTokensApi,
  SECRET_VALUE,
  problem,
  sampleAccount,
  sampleTechnicalToken,
} from '../../api/testing/access-fakes';
import { AuthzMe, SessionApi } from '../../api/session-api';
import { FakeSessionApi } from '../../api/testing/fakes';
import { buttonByText, renderScreen, settle, storedText, text, typeInto, sleep } from '../../testing/screen';
import { seriousViolations } from '../../testing/axe';
import { ServiceAccountDetail } from './service-account-detail';
import { ServiceAccountList } from './service-account-list';
import { RepositoryServiceAccounts } from './repository-service-accounts';

const overlay = (): HTMLElement => document.querySelector('.cdk-overlay-container') as HTMLElement;
type Get = <A>(t: abstract new (...a: never[]) => A) => A;

/** The section **Technical tokens** of the page (R-UI-181). */
const technicalSection = (root: HTMLElement): HTMLElement =>
  [...root.querySelectorAll<HTMLElement>('quader-disclosure')].find((d) => d.querySelector('.heading')?.textContent?.includes('Technical tokens'))!;

const asRole = (roles: AuthzMe['roles'], capabilities: string[]) => ({
  inputs: { name: 'ci-bot' },
  capabilities,
  setup: (api: Get) => {
    const me = api(SessionApi) as FakeSessionApi;
    me.meResult = { roles, capabilities };
    (api(IdentityApi) as FakeIdentityApi).accounts = [sampleAccount({ technicalTokens: 1 })];
  },
});

describe('Service accounts (R-UI-026, R-UI-181)', () => {
  it('R-UI-026 lists name, description, level and status with the hint when to use one, and passes axe', async () => {
    const { root } = await renderScreen(ServiceAccountList);
    expect(text(root)).toContain('CI systems that cannot sign in through OpenID Connect');
    const rows = root.querySelectorAll('tbody tr');
    expect(rows).toHaveLength(1);
    expect(text(rows[0]!)).toContain('ci-bot');
    expect(text(rows[0]!)).toContain('Instance');
    expect(text(rows[0]!)).toContain('Active');
    expect(await seriousViolations(root)).toEqual([]);
  });

  it('R-UI-181 a project\'s account names its project, a repository\'s account its repository and its level; an instance account has the dash', async () => {
    const { root } = await renderScreen(ServiceAccountList, {
      setup: (get) => {
        const identity = get(IdentityApi) as FakeIdentityApi;
        identity.accounts = [
          sampleAccount({ project: 'web-frontend', description: '' }),
          sampleAccount({ id: 'sa-2', name: 'ops' }),
          sampleAccount({ id: 'sa-3', name: 'release', project: 'web', repository: 'npm-local' }),
        ];
      },
    });
    const [first, second, third] = [...root.querySelectorAll('tbody tr')];
    expect(first!.querySelector('td quader-project-link quader-project-mark')).toBeTruthy();
    expect(text(first!)).toContain('Project');
    expect(second!.querySelectorAll('td quader-no-value')).toHaveLength(1);
    expect(text(third!)).toContain('Repository');
    expect(text(third!)).toContain('web/npm-local');
    expect(third!.querySelector('a')?.getAttribute('href')).toContain('/projects/web/repositories/npm-local/service-accounts/release');
  });

  it('R-UI-026 creates a service account with an idempotency key', async () => {
    const { fixture, root, get } = await renderScreen(ServiceAccountList);
    buttonByText(root, 'New service account').click();
    await settle(fixture);
    const dialog = overlay().querySelector('quader-service-account-create-dialog') as HTMLElement;
    typeInto(dialog, 'Name', 'release-bot');
    buttonByText(dialog, 'Create service account').click();
    await settle(fixture);
    const api = get(IdentityApi) as FakeIdentityApi;
    expect(api.calls).toContain('createServiceAccount release-bot');
    expect(api.keys).toHaveLength(1);
  });

  it('R-UI-026 disables an account and asks whether to revoke its tokens (default: keep)', async () => {
    const { fixture, root, get } = await renderScreen(ServiceAccountDetail, { inputs: { name: 'ci-bot' } });
    buttonByText(root, 'Disable').click();
    await settle(fixture);
    buttonByText(overlay().querySelector('quader-confirm-dialog') as HTMLElement, 'Disable').click();
    await sleep(fixture, 300);
    expect((get(IdentityApi) as FakeIdentityApi).calls).toContain('disableServiceAccount ci-bot revokeTokens=false');
    expect(text(root)).toContain('Disabled');
  });

  it('R-API-012 saving the description sends If-Match; a 412 opens the conflict dialog', async () => {
    const { fixture, root, get } = await renderScreen(ServiceAccountDetail, { inputs: { name: 'ci-bot' } });
    const api = get(IdentityApi) as FakeIdentityApi;
    typeInto(root, 'Description', 'Deploy and release jobs');
    await settle(fixture);
    buttonByText(root, 'Save changes').click();
    await settle(fixture);
    expect(api.ifMatch).toEqual(['"1"']);
    api.bumpRevision('service-account:ci-bot');
    api.bumpRevision('service-account:ci-bot');
    typeInto(root, 'Description', 'Something else');
    await settle(fixture);
    buttonByText(root, 'Save changes').click();
    await settle(fixture);
    expect(overlay().querySelector('quader-conflict-dialog')).toBeTruthy();
  });

  it('R-UI-181 issues a technical token with its name first, shows the secret once and never stores it', async () => {
    const { fixture, root, get } = await renderScreen(ServiceAccountDetail, asRole(['admin'], ['identities:read', 'identities:write']));
    buttonByText(root, 'New technical token').click();
    await settle(fixture);
    const form = root.querySelector('quader-token-form') as HTMLElement;
    const labels = [...form.querySelectorAll('label')].map((l) => text(l));
    expect(labels[0]).toContain('Name');
    expect(text(form)).toContain('The system that uses this token, for example nightly-mirror. It cannot be changed.');
    typeInto(form, 'Name', 'release');
    typeInto(form, 'Description (optional)', 'the release pipeline');
    // A service-account token may be non-expiring for the administrator role.
    expect(text(form)).toContain('Never expires');
    buttonByText(form, 'Create technical token').click();
    await settle(fixture);
    const tt = get(TechnicalTokensApi) as FakeTechnicalTokensApi;
    expect(tt.calls).toContain('create sa:ci-bot release');
    expect(tt.lastRequest?.description).toBe('the release pipeline');
    const secret = root.querySelector('quader-token-secret') as HTMLElement;
    expect((secret.querySelector('input') as HTMLInputElement).value).toBe(SECRET_VALUE.replace('qdr_pat_', 'qdr_sat_'));
    expect(text(secret)).toContain('shown only once');
    // R-UI-043: the value is in the component only.
    expect(storedText()).not.toContain('qdr_');
    expect(await seriousViolations(root)).toEqual([]);
    buttonByText(secret, 'I have copied it').click();
    await settle(fixture);
    expect(root.querySelector('quader-token-secret')).toBeNull();
    expect(root.innerHTML).not.toContain('qdr_sat_ABCD');
    expect(text(technicalSection(root))).toContain('release');
  });

  it('R-UI-181 a name that is no system name is refused before the request', async () => {
    const { fixture, root, get } = await renderScreen(ServiceAccountDetail, asRole(['admin'], ['identities:read']));
    buttonByText(root, 'New technical token').click();
    await settle(fixture);
    const form = root.querySelector('quader-token-form') as HTMLElement;
    typeInto(form, 'Name', 'Nightly Mirror');
    buttonByText(form, 'Create technical token').click();
    await settle(fixture);
    expect(text(form)).toContain('Enter the name of the system');
    expect((get(TechnicalTokensApi) as FakeTechnicalTokensApi).calls).toEqual([]);
  });

  it('R-UI-042 a refused create keeps the form and shows the error', async () => {
    const { fixture, root, get } = await renderScreen(ServiceAccountDetail, asRole(['admin'], ['identities:read']));
    buttonByText(root, 'New technical token').click();
    await settle(fixture);
    typeInto(root.querySelector('quader-token-form')!, 'Name', 'x');
    (get(TechnicalTokensApi) as FakeTechnicalTokensApi).failure = problem(422, 'ttl_exceeds_maximum');
    buttonByText(root.querySelector('quader-token-form')!, 'Create technical token').click();
    await settle(fixture);
    expect(text(root)).toContain('longer than the maximum');
    expect(root.querySelector('quader-token-form')).toBeTruthy();
    expect(root.querySelector('quader-token-secret')).toBeNull();
  });

  it('R-AUTH-027 a security administrator (identities:write, no admin role) sees the tokens, no New technical token and no Rotate', async () => {
    const { root } = await renderScreen(
      ServiceAccountDetail,
      asRole(['security-admin'], ['identities:read', 'identities:write', 'tokens:read', 'tokens:revoke', 'endpoints:read']),
    );
    expect(root.textContent).not.toContain('New technical token');
    expect(root.textContent).not.toContain('Rotate');
    expect(root.querySelector('quader-technical-token-table')).toBeTruthy();
    expect(text(technicalSection(root))).toContain('Revoke');
  });

  it('R-AUTH-027 the admin role sees New technical token and Rotate', async () => {
    const { root } = await renderScreen(ServiceAccountDetail, asRole(['admin'], ['identities:read', 'identities:write', 'tokens:revoke']));
    expect(root.textContent).toContain('New technical token');
    expect(root.textContent).toContain('Rotate');
  });

  it('R-UI-181 a row names the token, the hint of its secret and its description', async () => {
    const { root } = await renderScreen(ServiceAccountDetail, asRole(['admin'], ['identities:read']));
    const row = technicalSection(root).querySelector('tbody tr') as HTMLElement;
    expect(text(row.querySelector('th')!)).toContain('nightly-mirror');
    expect(text(row.querySelector('th')!)).toContain('qdr_sat_7Hq2');
    expect(text(row.querySelector('th')!)).toContain('The nightly mirror job');
    // Q-UI-761: the expiry stands beside the secret's hint, and the list has no column Expires.
    expect(text(row.querySelector('th .secondary')!)).toMatch(/^qdr_sat_7Hq2 · (expires .+|never expires)$/);
    expect([...technicalSection(root).querySelectorAll('thead th')].map((h) => text(h))).not.toContain('Expires');
  });

  it('R-UI-181 an expired token says Expired and gets a new secret through the form without its name', async () => {
    const { fixture, root, get } = await renderScreen(ServiceAccountDetail, {
      ...asRole(['admin'], ['identities:read']),
      setup: (api: Get) => {
        asRole(['admin'], ['identities:read']).setup(api);
        (api(TechnicalTokensApi) as FakeTechnicalTokensApi).tokens['sa:ci-bot'] = [
          sampleTechnicalToken({ state: 'expired', secrets: [{ ...sampleTechnicalToken().secrets[0]!, expiresAt: '2026-01-01T00:00:00Z' }] }),
        ];
      },
    });
    const section = technicalSection(root);
    expect(text(section)).toContain('Expired');
    expect(section.textContent).not.toContain('Rotate');
    buttonByText(section, 'Issue new secret').click();
    await settle(fixture);
    const form = root.querySelector('quader-token-form') as HTMLElement;
    expect(text(root)).toContain('New secret for nightly-mirror');
    expect(form.querySelector('input[name="name"]')).toBeNull();
    expect(form.querySelector('input[name="description"]')).toBeNull();
    buttonByText(form, 'Issue new secret').click();
    await settle(fixture);
    expect((get(TechnicalTokensApi) as FakeTechnicalTokensApi).calls).toContain('newSecret sa:ci-bot nightly-mirror');
    expect(root.querySelector('quader-token-secret')).toBeTruthy();
  });

  it('R-UI-181 revoking a technical token revokes its live secrets', async () => {
    const { fixture, root, get } = await renderScreen(ServiceAccountDetail, asRole(['admin'], ['identities:read', 'tokens:revoke']));
    buttonByText(technicalSection(root), 'Revoke').click();
    await settle(fixture);
    buttonByText(overlay().querySelector('quader-confirm-dialog') as HTMLElement, 'Revoke').click();
    await sleep(fixture, 300);
    expect((get(TokensApi) as FakeTokensApi).calls).toContain('revoke 00000000-0000-7000-8000-0000000000c2');
  });

  it('R-UI-181 G5-51 the row menu edits a technical token\'s description with its ETag and deletes the token after the question', async () => {
    const { fixture, root, get } = await renderScreen(ServiceAccountDetail, asRole(['admin'], ['identities:read', 'identities:write', 'tokens:revoke']));
    const fake = get(TechnicalTokensApi) as FakeTechnicalTokensApi;
    const name = sampleTechnicalToken().name;
    const menu = technicalSection(root).querySelector<HTMLButtonElement>(`button[aria-label="Actions for ${name}"]`)!;
    expect(menu).toBeTruthy();
    // The secret's words stay words beside the menu (Q-UI-641).
    expect(buttonByText(technicalSection(root), 'Revoke')).toBeTruthy();
    menu.click();
    await settle(fixture);
    expect([...overlay().querySelectorAll('.menu-item')].map((b) => text(b as HTMLElement))).toEqual(['Edit description', 'Delete']);
    buttonByText(overlay(), 'Edit description').click();
    await sleep(fixture, 100);
    const dialog = overlay().querySelector('quader-technical-token-description-dialog') as HTMLElement;
    const field = dialog.querySelector<HTMLInputElement>('#tt-description')!;
    field.value = 'Deploys the customer portal';
    field.dispatchEvent(new Event('input'));
    await settle(fixture);
    buttonByText(dialog, 'Save changes').click();
    await sleep(fixture, 300);
    expect(fake.calls).toContain(`get sa:ci-bot ${name}`);
    expect(fake.calls).toContain(`update sa:ci-bot ${name}`);
    expect(text(technicalSection(root))).toContain('Deploys the customer portal');

    technicalSection(root).querySelector<HTMLButtonElement>(`button[aria-label="Actions for ${name}"]`)!.click();
    await settle(fixture);
    buttonByText(overlay(), 'Delete').click();
    await settle(fixture);
    const confirm = overlay().querySelector('quader-confirm-dialog') as HTMLElement;
    expect(text(confirm)).toContain('Delete this technical token?');
    buttonByText(confirm, 'Delete').click();
    await sleep(fixture, 300);
    expect(fake.calls.some((c) => c.startsWith(`remove sa:ci-bot ${name}`))).toBe(true);
  });

  it('G5-51 a caller who may only read the account gets no menu of a technical token', async () => {
    const { root } = await renderScreen(ServiceAccountDetail, asRole(['auditor'], ['identities:read']));
    expect(technicalSection(root).querySelector('button[aria-label^="Actions for"]')).toBeNull();
  });

  it('R-UI-026 R-UI-181 the technical tokens are a quiet section, open while the account holds one', async () => {
    const closed = await renderScreen(ServiceAccountDetail, { inputs: { name: 'ci-bot' } });
    expect(technicalSection(closed.root).querySelector('.toggle')!.getAttribute('aria-expanded')).toBe('false');
    const open = await renderScreen(ServiceAccountDetail, asRole(['admin'], ['identities:read']));
    expect(technicalSection(open.root).querySelector('.toggle')!.getAttribute('aria-expanded')).toBe('true');
  });

  it('R-EP-011 the token form binds a token to the endpoints chosen, and leaves it unbound otherwise', async () => {
    const { fixture, root, get } = await renderScreen(ServiceAccountDetail, asRole(['admin'], ['identities:read', 'endpoints:read']));
    buttonByText(root, 'New technical token').click();
    await settle(fixture);
    const form = root.querySelector('quader-token-form') as HTMLElement;
    expect(text(form)).toContain('Endpoints');
    typeInto(form, 'Name', 'customer-acme');
    const boxes = Array.from(form.querySelectorAll<HTMLInputElement>('.endpoints input[type="checkbox"]'));
    expect(boxes.map((b) => b.closest('quader-checkbox')?.textContent?.trim())).toEqual(['default', 'customers']);
    boxes[1]!.click();
    await settle(fixture);
    buttonByText(form, 'Create technical token').click();
    await settle(fixture);
    expect((get(TechnicalTokensApi) as FakeTechnicalTokensApi).lastRequest?.scope).toEqual({ inherit: true, endpoints: ['customers'] });
  });

  describe('R-UI-026 administrator rights in the form of a technical token', () => {
    const withAccount = (isAdmin: boolean, roles: AuthzMe['roles'], project: string | null = null) => ({
      inputs: project ? { name: 'ci-bot', project } : { name: 'ci-bot' },
      capabilities: ['identities:read', 'identities:write'],
      setup: (api: Get) => {
        const me = api(SessionApi) as FakeSessionApi;
        me.meResult = { roles, capabilities: ['identities:read', 'identities:write'] };
        const ids = api(IdentityApi) as FakeIdentityApi;
        ids.accounts = [sampleAccount({ isAdmin, project })];
      },
    });
    const radios = (root: HTMLElement): string[] =>
      [...root.querySelectorAll('quader-token-form input[type="radio"]')].map((r) => text(r.closest('label')!));

    it('an administrator is offered the Administrator choice for an account that holds the role, and it sends admin with inherit', async () => {
      const { fixture, root, get } = await renderScreen(ServiceAccountDetail, withAccount(true, ['admin']));
      expect(text(root.querySelector('[data-testid="sa-admin"]')!)).toBe('Held by this account');
      buttonByText(root, 'New technical token').click();
      await settle(fixture);
      expect(radios(root)[0]).toContain('Administrator: everything on this installation');
      typeInto(root.querySelector('quader-token-form')!, 'Name', 'ops');
      root.querySelector<HTMLInputElement>('quader-token-form input[type="radio"]')!.click();
      await settle(fixture);
      buttonByText(root.querySelector('quader-token-form')!, 'Create technical token').click();
      await settle(fixture);
      expect((get(TechnicalTokensApi) as FakeTechnicalTokensApi).lastRequest?.scope).toEqual({ inherit: true, admin: true });
    });

    it('an account without the role is offered no Administrator choice: it would take effect nowhere', async () => {
      const { fixture, root } = await renderScreen(ServiceAccountDetail, withAccount(false, ['admin']));
      expect(text(root.querySelector('[data-testid="sa-admin"]')!)).toBe('Not held');
      buttonByText(root, 'New technical token').click();
      await settle(fixture);
      expect(radios(root).join('|')).not.toContain('Administrator');
    });

    it('a project account holds no system role: no choice and no role line', async () => {
      const { root } = await renderScreen(ServiceAccountDetail, withAccount(true, ['admin'], 'web'));
      expect(root.querySelector('[data-testid="sa-admin"]')).toBeNull();
    });
  });

  it('R-UI-026 the page is one column: no card, the description edited in place with Save changes, Advanced last', async () => {
    const { fixture, root } = await renderScreen(ServiceAccountDetail, { inputs: { name: 'ci-bot' } });
    expect(root.querySelector('.settings > .card')).toBeNull();
    const save = buttonByText(root, 'Save changes') as HTMLButtonElement;
    expect(save.disabled).toBe(true);
    typeInto(root, 'Description', 'Changed');
    await settle(fixture);
    expect(save.disabled).toBe(false);
    expect(root.querySelector<HTMLInputElement>('input[name="id"]')!.readOnly).toBe(true);
    const headings = [...root.querySelectorAll('.toggle')].map((e) => text(e));
    expect(headings[headings.length - 1]).toMatch(/Advanced$/);
  });

  it('R-EP-011 without endpoints:read the form has no endpoint field', async () => {
    const { fixture, root } = await renderScreen(ServiceAccountDetail, asRole(['admin'], ['identities:read', 'identities:write']));
    buttonByText(root, 'New technical token').click();
    await settle(fixture);
    expect(text(root.querySelector('quader-token-form')!)).not.toContain('Endpoints');
  });
});

describe('A repository\'s service accounts (R-UI-181, R-AUTH-046, R-AUTHZ-039)', () => {
  const seedRepository = (get: Get): void => {
    (get(IdentityApi) as FakeIdentityApi).accounts = [
      sampleAccount({ id: 'sa-r', name: 'ci', project: 'web', repository: 'npm-local', actions: ['read', 'deploy'], technicalTokens: 1 }),
    ];
  };

  it('R-UI-181 the section lists the accounts with their reference, rights, state and technical tokens, and creates one with its rights', async () => {
    const { fixture, root, get } = await renderScreen(RepositoryServiceAccounts, {
      inputs: { project: 'web', repository: 'npm-local' },
      capabilities: ['identities:write', 'identities:read'],
      setup: seedRepository,
    });
    const row = root.querySelector('tbody tr') as HTMLElement;
    expect(text(row)).toContain('sa:web/npm-local/ci');
    expect(text(row)).toContain('Read, Deploy');
    expect(text(row)).toContain('Active');
    buttonByText(root, 'New service account').click();
    await settle(fixture);
    const dialog = overlay().querySelector('quader-service-account-create-dialog') as HTMLElement;
    expect(text(dialog)).toContain('Rights on this repository');
    const boxes = [...dialog.querySelectorAll<HTMLInputElement>('.rights input[type="checkbox"]')];
    expect(boxes.map((b) => b.checked)).toEqual([true, false, false, false, false, false]);
    expect(text(dialog)).toContain('Publish new files and versions.');
    typeInto(dialog, 'Name', 'release');
    boxes[2]!.click();
    await settle(fixture);
    buttonByText(dialog, 'Create service account').click();
    await settle(fixture);
    expect((get(IdentityApi) as FakeIdentityApi).calls).toContain('createServiceAccount web/npm-local/release read,deploy');
    expect(await seriousViolations(root)).toEqual([]);
  });

  it('R-UI-181 the page of a repository\'s account names its repository and holds its rights with Save changes', async () => {
    const { fixture, root, get } = await renderScreen(ServiceAccountDetail, {
      inputs: { name: 'ci', project: 'web', key: 'npm-local' },
      capabilities: ['identities:write', 'identities:read'],
      setup: seedRepository,
    });
    expect(text(root)).toContain('Service account of repository web/npm-local');
    const boxes = [...root.querySelectorAll<HTMLInputElement>('.rights input[type="checkbox"]')];
    expect(boxes.map((b) => b.checked)).toEqual([true, false, true, false, false, false]);
    const save = buttonByText(root, 'Save changes') as HTMLButtonElement;
    expect(save.disabled).toBe(true);
    boxes[2]!.click();
    await settle(fixture);
    expect(save.disabled).toBe(false);
    save.click();
    await settle(fixture);
    expect((get(IdentityApi) as FakeIdentityApi).calls).toContain('updateServiceAccount ci read');
    expect((get(TechnicalTokensApi) as FakeTechnicalTokensApi).lastList).toEqual({ name: 'ci', project: 'web', repository: 'npm-local' });
  });
});
