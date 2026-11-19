import { Component, inject } from '@angular/core';
import { ApiError } from '../../api/problem';
import { LimitProfile, LimitsApi } from '../../api/limits-api';
import { FakeLimitsApi } from '../../api/testing/fakes-limits';
import { FakeSessionApi } from '../../api/testing/fakes';
import { SessionApi } from '../../api/session-api';
import { seriousViolations } from '../../testing/axe';
import { buttonByText, choosePrincipal, renderScreen, settle, sleep, text, type, typeInto } from '../../testing/screen';
import { LimitModeLine } from './limit-mode';
import { LimitsBlocksTab } from './limits-blocks-tab';
import { LimitsProfilesTab } from './limits-profiles-tab';
import { LimitsProjectsTab } from './limits-projects-tab';
import { LimitsRulesTab } from './limits-rules-tab';
import { LimitsSettingsState } from './limits-state';
import { LimitProfileEditor } from './profile-editor';
import { ProjectLimitsSection } from './project-limits-section';
import { RepositoryLimitsSection } from './repository-limits-section';
import { TokenLastDayCell, TokenLimitCell, lastDayText, tokenLimitText } from './technical-token-limit';

const overlay = (): HTMLElement => document.querySelector('.cdk-overlay-container') as HTMLElement;
const READER = ['limits:read', 'usage:read', 'settings:read'];
const WRITER = [...READER, 'limits:write'];

/** The tab Rules with the frame's shared settings, as the frame provides them. */
@Component({
  selector: 'quader-test-rules',
  imports: [LimitModeLine, LimitsRulesTab],
  providers: [LimitsSettingsState],
  template: `<quader-limit-mode [canWrite]="true" (loaded)="state.settings.set($event)" /><quader-limits-rules-tab />`,
})
class RulesWithMode {
  readonly state = inject(LimitsSettingsState);
}

/** The caller is an Owner of `project` and holds no capability. */
function ownerOf(get: <A>(token: abstract new (...args: never[]) => A) => A, project: string): void {
  (get(SessionApi) as FakeSessionApi).meResult = { capabilities: [], projects: [{ key: project, role: 'owner' }] as never };
}

function input(root: ParentNode, name: string): HTMLInputElement {
  const el = root.querySelector<HTMLInputElement>(`input[name="${name}"]`);
  if (!el) throw new Error(`no input ${name}`);
  return el;
}

describe('Download limits in the admin area (R-UI-177 to R-UI-182)', () => {
  it('R-UI-177 the auditor sees the mode, the five built-in rules and no Add rule; passes axe', async () => {
    const { root } = await renderScreen(RulesWithMode, { capabilities: READER });
    expect(text(root)).toContain('Mode: Audit. Every limit is counted and shown; nothing is refused or slowed.');
    expect(root.querySelectorAll('tbody tr th .badge').length).toBe(5);
    expect(text(root)).not.toContain('Add rule');
    expect(text(root)).toContain('Block time after too many refused requests: 15 minutes');
    expect(await seriousViolations(root)).toEqual([]);
  });

  it('R-UI-177 R-UI-179 Q-UI-758 the administrator sees Add rule and Switch to Enforce…; a name opens its rule, Delete is the one action', async () => {
    const { root } = await renderScreen(RulesWithMode, { capabilities: WRITER });
    expect(buttonByText(root, 'Add rule')).toBeTruthy();
    expect(buttonByText(root, 'Switch to Enforce…')).toBeTruthy();
    const builtin = [...root.querySelectorAll('tbody tr')].find((r) => text(r).includes('Every signed-in user'))!;
    expect(text(builtin.querySelector('td.actions')!)).toBe('');
    expect(builtin.querySelector('th button.link')).toBeTruthy();
    const user = [...root.querySelectorAll('tbody tr')].find((r) => text(r).includes('Release engineering'))!;
    expect(text(user)).toContain('+ uncached 20 GB/h');
    expect(text(user)).toContain('+ 1 more');
    expect(text(user.querySelector('td.actions')!)).toContain('Delete');
  });

  it('R-UI-182 Q-UI-733 Switch to Enforce shows the last 24 hours first and switches with If-Match', async () => {
    const { fixture, root, get } = await renderScreen(RulesWithMode, { capabilities: WRITER });
    buttonByText(root, 'Switch to Enforce…').click();
    await settle(fixture);
    const dialog = overlay().querySelector('quader-limit-mode-dialog') as HTMLElement;
    expect(text(dialog)).toContain('12 callers would have been limited.');
    expect(text(dialog)).toContain('3,400 requests would have been refused.');
    expect(text(dialog)).toContain('25 downloads would have been slowed down.');
    expect(text(dialog)).toContain('1 caller would have been blocked.');
    const link = [...dialog.querySelectorAll('a')].find((a) => text(a) === 'Requests per second')!;
    expect(link.getAttribute('href')).toContain('state=wouldBeLimited');
    // The three figures with the most events.
    expect(dialog.querySelectorAll('ul.links')[0]!.querySelectorAll('li').length).toBe(3);
    buttonByText(dialog.querySelector('.dialog-actions')!, 'Switch to Enforce').click();
    await settle(fixture);
    const api = get(LimitsApi) as FakeLimitsApi;
    expect(api.calls).toContain('patchSettings {"mode":"enforce"}');
    expect(api.ifMatch).toEqual(['"4"']);
    expect(text(root)).toContain('Mode: Enforce. Limits refuse and slow requests.');
    expect(buttonByText(root, 'Switch to Audit…')).toBeTruthy();
  });

  it('R-UI-182 a preview without limits says so', async () => {
    const { fixture, root } = await renderScreen(RulesWithMode, {
      capabilities: WRITER,
      setup: (get) => {
        (get(LimitsApi) as FakeLimitsApi).preview = { ...(get(LimitsApi) as FakeLimitsApi).preview, subjects: 0, byFigure: [], byKind: [] };
      },
    });
    buttonByText(root, 'Switch to Enforce…').click();
    await settle(fixture);
    expect(text(overlay())).toContain('In the last 24 hours no caller would have been limited.');
  });

  it('R-UI-177 changes the block time in minutes', async () => {
    const { fixture, root, get } = await renderScreen(RulesWithMode, { capabilities: WRITER });
    buttonByText(root, 'Change the block time').click();
    await settle(fixture);
    type(input(root, 'blockMinutes'), '2000');
    buttonByText(root, 'Save changes').click();
    await settle(fixture);
    expect(text(root)).toContain('A whole number of minutes from 1 to 1,440.');
    type(input(root, 'blockMinutes'), '30');
    buttonByText(root, 'Save changes').click();
    await settle(fixture);
    expect((get(LimitsApi) as FakeLimitsApi).calls).toContain('patchSettings {"blockSeconds":1800}');
    expect(text(root)).toContain('30 minutes');
  });

  it('R-UI-179 adds a rule for the group contractors with Restricted and one override: one POST with who, profile and the override', async () => {
    const { fixture, root, get } = await renderScreen(RulesWithMode, { capabilities: WRITER });
    buttonByText(root, 'Add rule').click();
    await settle(fixture);
    const dialog = overlay().querySelector('quader-limit-rule-dialog') as HTMLElement;
    typeInto(dialog, 'Applies to', 'group');
    await settle(fixture);
    await choosePrincipal(fixture, dialog, 'Group', 'web');
    typeInto(dialog, 'Profile', 'restricted');
    await settle(fixture);
    expect(text(dialog)).toContain('50/s · 200 burst');
    buttonByText(dialog, 'Change single figures').click();
    await settle(fixture);
    type(dialog.querySelectorAll<HTMLInputElement>('input[name="uncachedBytesPerHour"]')[0]!, '20');
    await settle(fixture);
    expect(buttonByText(dialog, "Use the profile's value")).toBeTruthy();
    buttonByText(dialog.querySelector('.dialog-actions')!, 'Add rule').click();
    await settle(fixture);
    const api = get(LimitsApi) as FakeLimitsApi;
    expect(api.calls.filter((c) => c.startsWith('createRule'))).toEqual(['createRule instance']);
    expect(api.bodies[0]).toEqual({ profile: 'restricted', overrides: { uncachedBytesPerHour: 20_000_000_000 }, note: '', who: expect.objectContaining({ kind: 'group' }) });
  });

  it('R-UI-179 a technical token is chosen by name in the principal field, from the limits subjects of the project', async () => {
    const { fixture, root, get } = await renderScreen(ProjectLimitsSection, {
      inputs: { project: 'web' },
      setup: (get) => ownerOf(get, 'web'),
    });
    buttonByText(root, 'Add rule').click();
    await settle(fixture);
    const dialog = overlay().querySelector('quader-limit-rule-dialog') as HTMLElement;
    typeInto(dialog, 'Applies to', 'technicalToken');
    await settle(fixture);
    await choosePrincipal(fixture, dialog, 'Technical token', 'deploy', 'sa:web/partner');
    typeInto(dialog, 'Profile', 'technical');
    await settle(fixture);
    buttonByText(dialog.querySelector('.dialog-actions')!, 'Add rule').click();
    await settle(fixture);
    const api = get(LimitsApi) as FakeLimitsApi;
    expect(api.calls).toContain('subjects deploy technicalToken web');
    expect(api.bodies.at(-1)).toMatchObject({ who: { kind: 'technicalToken', technicalToken: 'tt-1' }, profile: 'technical', overrides: {} });
  });

  it('R-UI-179 a 422 limit_above_maximum marks each figure with its cap', async () => {
    const { fixture, root } = await renderScreen(ProjectLimitsSection, {
      inputs: { project: 'web' },
      setup: (get) => {
        ownerOf(get, 'web');
        (get(LimitsApi) as FakeLimitsApi).failures.set(
          'createRule',
          new ApiError(422, { code: 'limit_above_maximum', errors: [{ pointer: '/overrides/cachedBytesPerHour', code: 'limit_above_maximum', cap: 30_000_000_000 } as never] }, null),
        );
      },
    });
    buttonByText(root, 'Add rule').click();
    await settle(fixture);
    const dialog = overlay().querySelector('quader-limit-rule-dialog') as HTMLElement;
    expect(text(dialog)).toContain("You can set up to the instance's values for this caller, or up to the project's maximum (Technical).");
    typeInto(dialog, 'Applies to', 'every');
    await settle(fixture);
    typeInto(dialog, 'Profile', 'unlimited');
    await settle(fixture);
    buttonByText(dialog.querySelector('.dialog-actions')!, 'Add rule').click();
    await settle(fixture);
    expect(text(dialog)).toContain('At most 30 GB per hour here.');
  });

  it('R-UI-178 the profile list names the built-ins, the figures and what uses a profile; an unused own profile can be deleted', async () => {
    const { root } = await renderScreen(LimitsProfilesTab, { capabilities: WRITER });
    const standard = [...root.querySelectorAll('tbody tr')].find((r) => text(r).includes('Standard'))!;
    expect(text(standard)).toContain('Built-in');
    expect(text(standard)).toContain('1 rule');
    expect(standard.querySelector('td.actions button')).toBeNull();
    const own = [...root.querySelectorAll('tbody tr')].find((r) => text(r).includes('Partners'))!;
    expect((own.querySelector('td.actions button') as HTMLButtonElement).disabled).toBe(false);
    expect(await seriousViolations(root)).toEqual([]);
  });

  it('R-UI-178 changing Standard from 300 to 250 GB per hour sends 250000000000 with If-Match; emptying a field sends null', async () => {
    const { fixture, root, get } = await renderScreen(LimitProfileEditor, { capabilities: WRITER, inputs: { key: 'standard' } });
    expect(input(root, 'cachedBytesPerHour').value).toBe('300');
    type(input(root, 'cachedBytesPerHour'), '250');
    type(input(root, 'refusedPerHour'), '');
    await settle(fixture);
    buttonByText(root, 'Save changes').click();
    await settle(fixture);
    const api = get(LimitsApi) as FakeLimitsApi;
    const body = api.bodies[0] as { figures: Record<string, number | null> };
    expect(body.figures['cachedBytesPerHour']).toBe(250_000_000_000);
    expect(body.figures['refusedPerHour']).toBeNull();
    expect(api.ifMatch).toEqual(['"1"']);
    expect(await seriousViolations(root)).toEqual([]);
  });

  it('R-UI-178 Unlimited is read-only, and a reader cannot save', async () => {
    const { root } = await renderScreen(LimitProfileEditor, { capabilities: WRITER, inputs: { key: 'unlimited' } });
    expect(text(root)).toContain('Unlimited is always unlimited.');
    expect(input(root, 'requestsPerHour').readOnly).toBe(true);
    expect(text(root)).not.toContain('Save changes');
  });

  it('R-UI-178 a new profile takes its key from the name', async () => {
    const { fixture, root, get } = await renderScreen(LimitProfileEditor, { capabilities: WRITER });
    typeInto(root, 'Name', 'Nightly mirrors');
    await settle(fixture);
    expect(input(root, 'key').value).toBe('nightly-mirrors');
    type(input(root, 'requestsPerSecond'), '10');
    await settle(fixture);
    buttonByText(root, 'Create profile').click();
    await settle(fixture);
    expect((get(LimitsApi) as FakeLimitsApi).calls).toContain('createProfile nightly-mirrors');
  });

  it('R-UI-177 the tab Projects lists a project with its maximum and rules; sets a maximum', async () => {
    const { fixture, root, get } = await renderScreen(LimitsProjectsTab, { capabilities: WRITER });
    expect(text(root.querySelector('tbody')!)).toContain('Technical + 1 change');
    buttonByText(root.querySelector('tbody')!, 'Technical + 1 change').click();
    await settle(fixture);
    const dialog = overlay().querySelector('quader-limit-maximum-dialog') as HTMLElement;
    typeInto(dialog, 'Profile', 'standard');
    await settle(fixture);
    buttonByText(dialog, 'Save maximum').click();
    await settle(fixture);
    expect((get(LimitsApi) as FakeLimitsApi).calls).toContain('putMaximum web');
  });

  it('R-UI-177 the tab Projects without a row says that no project has a maximum', async () => {
    const { root } = await renderScreen(LimitsProjectsTab, {
      capabilities: WRITER,
      setup: (get) => ((get(LimitsApi) as FakeLimitsApi).projectRowsList = []),
    });
    expect(text(root)).toContain("No project has a maximum. A project's Owner can set limits up to the instance's values.");
  });

  it('R-UI-177 Blocks lists the active block with its refused requests and lifts it after a confirmation; empty says Nothing is blocked', async () => {
    const { fixture, root, get } = await renderScreen(LimitsBlocksTab, { capabilities: WRITER });
    expect(text(root)).toContain('61 of 60 per hour');
    expect(text(root)).toContain('198.51.100.23');
    buttonByText(root, 'Lift').click();
    await settle(fixture);
    expect(text(overlay())).toContain('Lift the block of 198.51.100.23?');
    buttonByText(overlay().querySelector('quader-confirm-dialog')!, 'Lift block').click();
    await sleep(fixture, 50);
    expect((get(LimitsApi) as FakeLimitsApi).calls).toContain('liftBlock 0193f2b1-0d2c-7c00-8a77-1a2b3c4d5e6f');
    expect(text(root)).toContain('Nothing is blocked.');
  });
});

describe('Download limits of a project and a repository (R-UI-180)', () => {
  it('R-UI-180 the project section is closed and names its rules and maximum; open, the maximum and the rule table', async () => {
    const { root } = await renderScreen(ProjectLimitsSection, { capabilities: [], inputs: { project: 'web' } });
    const toggle = root.querySelector('quader-disclosure .toggle')!;
    expect(toggle.getAttribute('aria-expanded')).toBe('false');
    expect(text(toggle)).toContain('1 rule · Maximum: Technical');
    expect(text(root)).toContain('Maximum given by an administrator: Technical');
    expect(text(root)).toContain('Web developers');
  });

  it('R-UI-180 an Owner opens a remote: no rule, the ceilings, a save per class', async () => {
    const { root } = await renderScreen(RepositoryLimitsSection, {
      inputs: { project: 'platform', repository: 'dockerhub', remote: true },
      setup: (get) => {
        (get(LimitsApi) as FakeLimitsApi).ceilingsByRepo.clear();
        ownerOf(get, 'platform');
      },
    });
    expect(root.querySelector('quader-disclosure .toggle .note')).toBeNull();
    expect(text(root)).toContain("No rule here. Callers get the instance's limits.");
    expect(text(root)).toContain('All anonymous callers');
    expect(root.querySelector<HTMLInputElement>('#ceiling-anonymous-gb')!.value).toBe('');
    expect(root.querySelector<HTMLInputElement>('#ceiling-anonymous-gb')!.placeholder).toBe('Unlimited');
  });

  it('R-LIM-032 saving the anonymous ceiling sends both classes with If-Match', async () => {
    const { fixture, root, get } = await renderScreen(RepositoryLimitsSection, {
      capabilities: ['limits:write', 'limits:read'],
      inputs: { project: 'platform', repository: 'dockerhub', remote: true },
    });
    const gb = root.querySelector<HTMLInputElement>('#ceiling-anonymous-gb')!;
    expect(gb.value).toBe('10');
    type(gb, '5');
    await settle(fixture);
    const save = [...root.querySelectorAll<HTMLButtonElement>('form.ceiling button')].find((b) => text(b).includes('All anonymous callers'))!;
    save.click();
    await settle(fixture);
    const api = get(LimitsApi) as FakeLimitsApi;
    expect(api.bodies[0]).toEqual({
      anonymous: { uncachedBytesPerHour: 5_000_000_000, upstreamRequestsPerHour: 100 },
      signedIn: { uncachedBytesPerHour: 50_000_000_000, upstreamRequestsPerHour: 2000 },
    });
    expect(api.ifMatch).toEqual(['"1"']);
    expect(await seriousViolations(root)).toEqual([]);
  });
});

describe('A technical token’s limit and last 24 hours (R-UI-181)', () => {
  it('R-UI-181 names the limit and the last 24 hours of a token', async () => {
    expect(tokenLimitText(null, [], 'Technical')).toBe('Default (Technical)');
    expect(tokenLimitText({ rule: '0193f2a6-0000-7000-8000-000000000001', profile: 'restricted', overrides: 1 }, [], 'Technical')).toBe(
      'restricted + 1 change',
    );
    expect(tokenLimitText({ rule: 'r', profile: 'technical', overrides: 0 }, [{ key: 'technical', name: 'Technical' } as LimitProfile], 'Technical')).toBe(
      'Technical',
    );
    expect(lastDayText({ requests: 1204, cachedBytes: 3_000_000_000, uncachedBytes: 200_000_000 }, 'en-US')).toBe('1,204 requests · 3.2 GB in 24 h');
    expect(lastDayText(null, 'en-US')).toBe('Not used in 24 h');
    expect(lastDayText({ requests: 0, cachedBytes: 0, uncachedBytes: 0 }, 'en-US')).toBe('Not used in 24 h');
    const { root } = await renderScreen(TokenLastDayCell, { inputs: { usage: { requests: 1, cachedBytes: 10, uncachedBytes: 0 } } });
    expect(text(root)).toBe('1 request · 10 B in 24 h');
    const cell = await renderScreen(TokenLimitCell, { inputs: { limit: null } });
    expect(text(cell.root)).toBe('Default (Technical)');
  });
});
