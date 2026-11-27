import { Bastion } from '../../api/bastions-api';
import { LimitsApi } from '../../api/limits-api';
import { ApiError } from '../../api/problem';
import { inDays, sampleBastion } from '../../api/testing/fakes-bastions';
import { FakeLimitsApi, sampleBastionProfiles, sampleProfiles } from '../../api/testing/fakes-limits';
import { seriousViolations } from '../../testing/axe';
import { buttonByText, choosePrincipal, renderScreen, settle, text, typeInto } from '../../testing/screen';
import { BastionBlocksTab, BastionPageState, BastionRulesTab } from './bastion-limits-page';
import { blockReasonText } from './bastion-state';

const overlay = (): HTMLElement => document.querySelector('.cdk-overlay-container') as HTMLElement;
const WRITER = ['endpoints:read', 'limits:read', 'limits:write'];

type Get = <A>(token: abstract new (...args: never[]) => A) => A;

/** The page's state as the frame sets it for `partners`: its name, and the registration where one is given. */
function onPage(get: Get, bastion: Bastion | null = null): void {
  const state = get(BastionPageState);
  state.name.set('partners');
  state.bastion.set(bastion);
  (get(LimitsApi) as FakeLimitsApi).profileList = [...sampleProfiles(), ...sampleBastionProfiles()];
}

describe("A Bastion's limits page (R-UI-241)", () => {
  it('R-UI-241 the five kind rules of the preset come first, marked From the preset, and cannot be deleted; passes axe', async () => {
    const { root } = await renderScreen(BastionRulesTab, { capabilities: WRITER, providers: [BastionPageState], setup: (g) => onPage(g) });
    expect(text(root)).toContain(
      "These rules count what this Bastion serves. The installation's own rules count what it serves, including what this Bastion fetches.",
    );
    const rows = [...root.querySelectorAll('tbody tr')];
    // The preset's order, then the others as resolution tries them (an address before a technical token, by name).
    const expected = ['Every anonymous caller', 'Every signed-in user', 'Every technical token', 'Every CI job', 'Every administrator', '203.0.113.0/24', 'deploy-prod'];
    expect(rows).toHaveLength(expected.length);
    rows.forEach((r, i) => expect(text(r.querySelector('th')!)).toContain(expected[i]));
    expect(rows.slice(0, 5).every((r) => text(r).includes('From the preset') && !r.querySelector('td.actions button'))).toBe(true);
    expect(text(rows[0]!)).toContain('Bastion: anonymous');
    expect(rows.slice(5).every((r) => !text(r).includes('From the preset') && r.querySelector('td.actions button'))).toBe(true);
    expect(text(root)).not.toContain('Built-in');
    expect(await seriousViolations(root)).toEqual([]);
  });

  it('R-UI-241 a rule for a technical token is added with one POST to the Bastion place, and the kinds offer every administrator', async () => {
    const { fixture, root, get } = await renderScreen(BastionRulesTab, { capabilities: WRITER, providers: [BastionPageState], setup: (g) => onPage(g) });
    const api = get(LimitsApi) as FakeLimitsApi;
    buttonByText(root, 'Add rule').click();
    await settle(fixture);
    const dialog = overlay().querySelector('quader-limit-rule-dialog') as HTMLElement;
    typeInto(dialog, 'Applies to', 'every');
    await settle(fixture);
    const kinds = [...dialog.querySelectorAll<HTMLOptionElement>('select[name="callerKind"] option')].map((o) => o.value);
    expect(kinds).toContain('administrator');
    typeInto(dialog, 'Applies to', 'technicalToken');
    await settle(fixture);
    await choosePrincipal(fixture, dialog, 'Technical token', 'deploy', 'sa:web/partner');
    typeInto(dialog, 'Profile', 'bastion-signed-in');
    await settle(fixture);
    buttonByText(dialog.querySelector('.dialog-actions')!, 'Add rule').click();
    await settle(fixture);
    expect(api.calls.filter((c) => c.startsWith('createRule'))).toEqual(['createRule bastion:partners']);
    expect(api.bodies.at(-1)).toMatchObject({ who: { kind: 'technicalToken', technicalToken: 'tt-1' }, profile: 'bastion-signed-in', overrides: {} });
  });

  it('R-UI-241 a reader sees the rules without Add rule and without an action', async () => {
    const { root } = await renderScreen(BastionRulesTab, { capabilities: ['limits:read'], providers: [BastionPageState], setup: (g) => onPage(g) });
    expect(() => buttonByText(root, 'Add rule')).toThrow();
    expect(root.querySelector('td.actions')).toBeNull();
  });

  it('R-UI-241 the blocks of two replicas are listed with address, end, reason and replica, the one ending last first; an ended one is not', async () => {
    const bastion = sampleBastion({
      status: [
        {
          replica: 'quader-bastion-7d9c-x2k4q',
          reportedAt: inDays(-1 / 1440),
          cache: { shared: false, bytes: 0, capacityBytes: 1, hits: 0, misses: 0, evictions: 0, protectedBytes: 0 },
          throughput: { limited: false, bytesPerSecondByClass: { anonymous: 0, signedIn: 0, catalogue: 0 } },
          limited: 0,
          blocked: 2,
          blocks: [
            { address: '203.0.113.9/32', until: inDays(0.01), reason: 'refusals' },
            { address: '198.51.100.7/32', until: inDays(-0.01), reason: 'refusals' },
          ],
          operation: { disabled: null, running: 0 },
        },
        {
          replica: 'quader-bastion-7d9c-m8v2j',
          reportedAt: inDays(-2 / 1440),
          cache: { shared: false, bytes: 0, capacityBytes: 1, hits: 0, misses: 0, evictions: 0, protectedBytes: 0 },
          throughput: { limited: false, bytesPerSecondByClass: { anonymous: 0, signedIn: 0, catalogue: 0 } },
          limited: 0,
          blocked: 1,
          blocks: [{ address: '2001:db8:4::/64', until: inDays(0.02), reason: 'failed_auth' }],
          operation: { disabled: null, running: 0 },
        },
      ],
    });
    const { root } = await renderScreen(BastionBlocksTab, { capabilities: WRITER, providers: [BastionPageState], setup: (g) => onPage(g, bastion) });
    const rows = [...root.querySelectorAll('tbody tr')].map((r) => [...r.querySelectorAll('th, td')].map((c) => text(c)));
    expect(rows.map((r) => [r[0], r[2], r[3]])).toEqual([
      ['2001:db8:4::/64', 'Too many failed sign-ins', 'quader-bastion-7d9c-m8v2j'],
      ['203.0.113.9/32', 'Too many requests refused by its flood protection', 'quader-bastion-7d9c-x2k4q'],
    ]);
    expect(root.querySelector('button')).toBeNull();
    expect(await seriousViolations(root)).toEqual([]);
  });

  it('R-UI-241 each reason of the closed list reads in words, and a code outside it stands as it came', () => {
    expect(['refusals', 'failed_auth', 'refused_requests', 'something_new'].map(blockReasonText)).toEqual([
      'Too many requests refused by its flood protection',
      'Too many failed sign-ins',
      'Too many requests refused by its download limits',
      'something_new',
    ]);
  });

  it('R-UI-241 no report says so, and a registration that cannot be read shows the answer with a retry', async () => {
    const empty = await renderScreen(BastionBlocksTab, { providers: [BastionPageState], setup: (g) => onPage(g, sampleBastion()) });
    expect(text(empty.root)).toContain('No replica reported a block in the last hour.');
    const failed = await renderScreen(BastionBlocksTab, {
      providers: [BastionPageState],
      setup: (g) => {
        onPage(g);
        g(BastionPageState).failure.set(new ApiError(403, { code: 'forbidden', detail: 'endpoints:read is needed' }, 'req-7'));
      },
    });
    expect(text(failed.root)).toContain('req-7');
  });
});
