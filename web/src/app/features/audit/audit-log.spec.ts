import { Router } from '@angular/router';
import { AuditApi } from '../../api/audit-api';
import { ApiError } from '../../api/problem';
import { FakeAuditApi, sampleEvent } from '../../api/testing/fakes-catalog';
import { applyFilters, button, chipTexts, choosePrincipal, expectAccessible, openFilters, renderScreen, settle, text, type } from '../../testing/screen';
import { AuditLog } from './audit-log';

async function open(inputs: Record<string, unknown> = {}, setup?: (audit: FakeAuditApi) => void) {
  const rendered = await renderScreen(AuditLog, {
    inputs,
    setup: (get) => {
      vi.spyOn(get(Router), 'navigate').mockResolvedValue(true);
      setup?.(get(AuditApi) as FakeAuditApi);
    },
  });
  return {
    ...rendered,
    audit: rendered.get(AuditApi) as FakeAuditApi,
    router: rendered.get(Router),
  };
}

describe('Audit log', () => {
  it('R-UI-037 shows time, actor, action as a translated label, target, outcome with icon and text, and source address', async () => {
    const { root } = await open();
    const t = text(root);
    expect(t).toContain('Permission policy changed');
    expect(t).toContain('authz.policy.updated');
    expect(t).toContain('File downloaded');
    expect(t).toContain('generic-local/releases/1.0/app.zip');
    expect(t).toContain('Denied');
    expect(t).toContain('10.12.4.7');
    expect(root.querySelector('table th[scope="col"]')).toBeTruthy();
    await expectAccessible(root);
  });

  it('R-UI-037 R-UI-145 the outcome is a badge, its icon and its word with a capital in one element', async () => {
    const { root } = await open();
    const outcomes = [...root.querySelectorAll('tbody tr')].map((r) => r.querySelector('td :is(.ok, .bad, .warn)'));
    expect(outcomes.map((o) => o?.className)).toEqual(['ok', 'warn']);
    expect(outcomes.map((o) => o!.textContent!.replace(o!.firstElementChild!.textContent!, '').trim())).toEqual(['Success', 'Denied']);
    for (const o of outcomes) expect(o!.firstElementChild!.classList).toContain('material-icons');
  });

  it('R-UI-037 R-UI-145 an action\'s key breaks after its dots and underscores only, never inside a word', async () => {
    const { root } = await open({}, (audit) => (audit.eventList = [sampleEvent({ action: 'user.groups.claim_missing' })]));
    const key = root.querySelector('tbody th .action-key')!;
    expect(key.textContent).toBe('user.groups.claim_missing');
    expect(key.querySelectorAll('wbr')).toHaveLength(4);
    expect([...key.childNodes].filter((n) => n.nodeType === Node.TEXT_NODE).map((n) => n.textContent)).toEqual([
      'user.',
      'groups.',
      'claim_',
      'missing',
    ]);
  });

  it('R-UI-037 an opened event sets the actor\'s kind apart under its name, and an action without a name shows its key once, breaking after a dot or an underscore', async () => {
    const event = sampleEvent({ action: 'storage.object_lock.retention_extended' });
    // The event of the address is the one opened (the row's button navigates there).
    const { root } = await open({ event: event.id }, (audit) => (audit.eventList = [event]));
    const facts = [...root.querySelectorAll('section.detail dl.facts dt')];
    const value = (name: string) => facts.find((dt) => dt.textContent?.trim() === name)!.nextElementSibling as HTMLElement;
    const action = value('Action');
    // The key alone, where the action has no name of its own; in its parts, each with its dot or underscore.
    expect(action.textContent?.trim()).toBe('storage.object_lock.retention_extended');
    expect([...action.querySelectorAll('code.fact-key wbr')]).toHaveLength(5);
    const actor = value('Actor');
    const kind = actor.querySelector('.fact-secondary')!;
    expect(kind.textContent).toBe(`${event.actor.type}${event.actor.authMethod ? ', ' + event.actor.authMethod : ''}`);
    expect(actor.firstElementChild).not.toBe(kind);
  });

  it('R-UI-037 R-UI-145 a target the entry knows by name shows the name, its kind and identifier under it; another its identifier on one line', async () => {
    const own = sampleEvent({
      id: 'own',
      action: 'user.groups.claim_missing',
      outcome: 'failure',
      actor: { type: 'user', id: '0199f1c4-7e2b-7a10-9c3d-1b2e3f4a5b6c', name: 'ada', tokenId: null, authMethod: 'session' },
      resource: { type: 'user', id: '0199f1c4-7e2b-7a10-9c3d-1b2e3f4a5b6c' },
    });
    const other = sampleEvent({ id: 'other', resource: { type: 'user', id: '0199f1c4-0000-7000-8000-000000000001' } });
    const { root } = await open({}, (audit) => (audit.eventList = [own, other]));
    const [first, second] = [...root.querySelectorAll<HTMLElement>('tbody tr td.target')];
    expect(first!.querySelector('.read-text')!.textContent!.trim()).toBe('ada');
    const kind = first!.querySelector('.target-id')!;
    expect(kind.textContent!.trim()).toBe('user');
    expect(kind.getAttribute('title')).toBe('0199f1c4-7e2b-7a10-9c3d-1b2e3f4a5b6c');
    expect(text(second!)).not.toContain('alice');
    const id = second!.querySelector('.target-id')!;
    expect(id.textContent!.trim()).toBe('0199f1c4-0000-7000-8000-000000000001');
    expect(id.getAttribute('title')).toBe('0199f1c4-0000-7000-8000-000000000001');
  });

  it('R-UI-037 an unknown action is shown as the name the server sent', async () => {
    const { root } = await open(
      {},
      (audit) => (audit.eventList = [sampleEvent({ action: 'future.thing.happened' })]),
    );
    expect(text(root)).toContain('future.thing.happened');
  });

  it('R-UI-037 the outcome warning shows the warning icon and word, never the failure icon; the filter offers it', async () => {
    const { root, fixture } = await open({}, (audit) => {
      audit.eventList = [sampleEvent({ action: 'user.groups.claim_missing', category: 'identity', outcome: 'warning', reason: 'absent' })];
    });
    const cell = [...root.querySelectorAll('tbody td')].find((td) => text(td).includes('warning'))!;
    expect(cell.querySelector('.material-icons')?.textContent).toBe('warning');
    expect(cell.querySelector('.bad')).toBeNull();
    expect(text(cell)).toContain('Warning');
    expect(text(root)).toContain('Group claim missing at sign-in');
    const drawer = await openFilters(fixture, root);
    const options = [...drawer.querySelectorAll<HTMLOptionElement>('#a-outcome option')].map((o) => o.value);
    expect(options).toContain('warning');
  });

  it('R-UI-037 R-VULN-024 every vulnerability action has a plain name, also the import of advisory data', async () => {
    const actions = [
      ['vulnerability.source.imported', 'Advisory data imported'],
      ['vulnerability.source.failed', 'Advisory sync failed'],
      ['vulnerability.finding.status_changed', 'Finding status changed'],
      ['vulnerability.finding.reopened', 'Finding reopened'],
      ['vulnerability.download_blocked', 'Download blocked by a finding'],
      ['vulnerability.claimed_name_match', 'Advisory matches a claimed package name'],
      ['vulnerability.advisory.kind_changed', 'Advisory kind changed'],
    ] as const;
    const { root } = await open({}, (audit) => (audit.eventList = actions.map(([action], i) => sampleEvent({ id: `vuln-${i}`, action }))));
    const t = text(root);
    for (const [, label] of actions) expect(t).toContain(label);
  });

  it('R-UI-037 the filters from the URL reach the API with the time range resolved against the clock', async () => {
    const { audit } = await open({
      range: '7d',
      category: 'authz',
      outcome: 'success',
      actor: 'alice',
      repository: 'generic-local',
      action: 'authz.*',
    });
    const filter = audit.filterCalls.at(-1)!;
    expect(filter).toMatchObject({
      category: 'authz',
      outcome: 'success',
      actor: 'alice',
      repository: 'generic-local',
      action: 'authz.*',
    });
    const span = Date.parse(filter.to!) - Date.parse(filter.from!);
    expect(span).toBe(7 * 24 * 3_600_000);
  });

  it('R-UI-037 R-UI-150 Apply puts the filters into the URL and keeps the other parameters; the actor is chosen from the search', async () => {
    const { root, fixture, router } = await open();
    const drawer = await openFilters(fixture, root);
    await choosePrincipal(fixture, drawer, 'Actor', 'sam', '@sam');
    type(drawer.querySelector<HTMLSelectElement>('#a-category')!, 'authz');
    type(drawer.querySelector<HTMLSelectElement>('#a-range')!, '7d');
    await settle(fixture);
    await applyFilters(fixture);
    const [, extras] = (router.navigate as ReturnType<typeof vi.fn>).mock.calls.at(-1)!;
    expect(extras.queryParams).toMatchObject({
      actor: 'sam',
      category: 'authz',
      range: '7d',
      event: null,
    });
    expect(extras.queryParamsHandling).toBe('merge');
  });

  it('R-UI-037 a custom range offers date and time fields', async () => {
    const { root, fixture } = await open({
      range: 'custom',
      from: '2026-10-01T00:00',
      to: '2026-10-02T00:00',
    });
    expect(chipTexts(root)).toEqual(['Time range: 2026-10-01T00:00 – 2026-10-02T00:00']);
    const drawer = await openFilters(fixture, root);
    expect(drawer.querySelector('#a-from')).toBeTruthy();
    expect(drawer.querySelector('#a-to')).toBeTruthy();
    await settle(fixture);
    await expectAccessible(drawer);
    await expectAccessible(root);
  });

  it('R-UI-037 "Load more" continues the keyset walk', async () => {
    const { root, fixture } = await open({}, (audit) => {
      audit.eventList = Array.from({ length: 150 }, (_, i) =>
        sampleEvent({
          id: `id-${i}`,
          occurredAt: `2026-10-28T11:${String(i % 60).padStart(2, '0')}:00Z`,
        }),
      );
    });
    expect(root.querySelectorAll('tbody tr').length).toBe(100);
    button(root, 'Load more').click();
    await settle(fixture);
    expect(root.querySelectorAll('tbody tr').length).toBe(150);
  });

  it('R-UI-037 an event opens in a panel with the full event, the before and after of the details, and links to actor and target', async () => {
    const { root } = await open({ event: '01933b1e-7c2a-7f4e-9d1b-6a2f0c9e8b10' });
    const panel = root.querySelector('section.detail')!;
    expect(panel.textContent).toContain('artifact.downloaded');
    expect(panel.querySelector('a[href="/admin/users/alice"]')).toBeTruthy();
    expect(panel.querySelector('a[href="/projects/web/repositories/generic-local"]')).toBeTruthy();
    expect(
      panel.querySelector('a[href="/projects/web/repositories/generic-local/files/releases/1.0/app.zip"]'),
    ).toBeTruthy();
    await expectAccessible(root);
  });

  it('R-UI-037 the detail of a policy change shows the details object with before and after', async () => {
    const { root } = await open({ event: '01933b1e-7c2a-7f4e-9d1b-6a2f0c9e8b11' });
    const pre = root.querySelector('section.detail pre')!.textContent!;
    expect(pre).toContain('"before"');
    expect(pre).toContain('"after"');
  });

  it('R-UI-037 a time is relative with the moment as its title, which follows the locale and switches to UTC with the toggle', async () => {
    const local = await open();
    const utc = await open({ utc: '1' });
    const localTime = local.root.querySelector('tbody time')!;
    const utcTime = utc.root.querySelector('tbody time')!;
    expect(utcTime.textContent).toMatch(/ago|now/);
    expect(utcTime.getAttribute('title')).toContain('11:58:02');
    expect(localTime.getAttribute('title')).toBeTruthy();
    expect((utc.root.querySelector('#a-utc') as HTMLInputElement).checked).toBe(true);
  });

  it('R-UI-037 R-UI-145 an actor without a name is its identifier on one line, cut in its column with the whole as its title, and a kind breaks after its underscore', async () => {
    // The job ui-layout met such an event (vulnerability.source.imported by an account of a system job): its UUID whole made the log
    // fall to blocks at 1,440 px.
    const id = '01a208c6-2bda-776d-a125-d83c6b9ca7ac';
    const nameless = sampleEvent({
      id: 'nameless',
      action: 'vulnerability.source.imported',
      actor: { type: 'service_account', id, name: null, tokenId: null, authMethod: null },
      resource: { type: 'vulnerability_source', id: 'osv:npm' },
    });
    const named = sampleEvent({ id: 'named', actor: { type: 'user', id: '0199f1c4-7e2b-7a10-9c3d-1b2e3f4a5b6c', name: 'ada', tokenId: null, authMethod: 'session' } });
    const { root } = await open({}, (audit) => (audit.eventList = [nameless, named]));
    const [first, second] = [...root.querySelectorAll<HTMLElement>('tbody tr td.actor')];
    const cut = first!.querySelector('.actor-id')!;
    expect(cut.textContent!.trim()).toBe(id);
    expect(cut.getAttribute('title')).toBe(id);
    // A name stands as it is, not cut.
    expect(second!.querySelector('.actor-id')).toBeNull();
    expect(text(second!)).toContain('ada');
    // The target's kind breaks after its underscore, as an action's key does, so that it gives the column no least width of a word.
    const kind = root.querySelector<HTMLElement>('tbody tr td.target')!;
    expect(kind.querySelector('span wbr')).toBeTruthy();
    expect(text(kind)).toContain('vulnerability_source');
  });

  it('R-UI-037 the log keeps five columns: the address stands under the actor, the project under the target', async () => {
    const { root } = await open();
    const heads = [...root.querySelectorAll('table thead th')].map((th) => text(th as HTMLElement));
    expect(heads.map((h) => h.replace(/help_outline|Help.*$/, '').trim())).toEqual(['Time', 'Actor', 'Action', 'Target', 'Outcome']);
    const actorWithAddress = [...root.querySelectorAll('tbody td.actor')].find((td) => text(td as HTMLElement).includes('10.12.4.7'));
    expect(actorWithAddress?.querySelector('.secondary')?.textContent).toContain('10.12.4.7');
    expect(root.querySelector('tbody td.target .target-project quader-project-link')).toBeTruthy();
  });

  it('R-UI-037 the resource, its kind and its identifier, is a filter of the drawer, a chip, and reaches the API', async () => {
    const { root, fixture, router } = await open();
    const drawer = await openFilters(fixture, root);
    type(drawer.querySelector<HTMLInputElement>('#a-resource-type')!, 'bastion');
    type(drawer.querySelector<HTMLInputElement>('#a-resource-id')!, '0193b4a0-0000-7000-8000-00000000b001');
    await settle(fixture);
    await applyFilters(fixture);
    const [, extras] = (router.navigate as ReturnType<typeof vi.fn>).mock.calls.at(-1)!;
    expect(extras.queryParams).toMatchObject({ resourceType: 'bastion', resourceId: '0193b4a0-0000-7000-8000-00000000b001', event: null });
    const filtered = await open({ resourceType: 'bastion', resourceId: '0193b4a0-0000-7000-8000-00000000b001' });
    expect(filtered.audit.filterCalls.at(-1)).toMatchObject({ resourceType: 'bastion', resourceId: '0193b4a0-0000-7000-8000-00000000b001' });
    expect(chipTexts(filtered.root)).toEqual(['Resource kind: bastion', 'Resource ID: 0193b4a0-0000-7000-8000-00000000b001']);
  });

  it('R-UI-037 the export links carry the current filter and both formats', async () => {
    const { root } = await open({ range: '1h', category: 'authz' });
    const links = [...root.querySelectorAll<HTMLAnchorElement>('a[download]')];
    expect(links.map((a) => a.textContent?.trim())).toEqual(['Export CSV', 'Export JSON Lines']);
    expect(links[0]?.getAttribute('href')).toContain('format=csv');
    expect(links[1]?.getAttribute('href')).toContain('format=jsonl');
    expect(links[0]?.getAttribute('href')).toContain('from=');
  });

  it('R-UI-037 an empty range, a failing query and the loading state are announced and pass the accessibility check', async () => {
    const empty = await open({}, (audit) => (audit.eventList = []));
    expect(text(empty.root)).toContain('There is no event in the last 24 hours.');
    await expectAccessible(empty.root);
    const none = await open({ actor: 'nobody' }, (audit) => (audit.eventList = []));
    expect(text(none.root)).toContain('No event matches.');

    const failing = await open({}, (audit) => {
      audit.events = () => () =>
        Promise.reject(new ApiError(400, { code: 'invalid_audit_range', detail: 'x' }, null));
    });
    expect(text(failing.root)).toContain('at most 400 days');
    await expectAccessible(failing.root);
  });

  it('R-UI-037 an event that does not exist shows the error in the panel', async () => {
    const { root } = await open({ event: 'unknown' });
    expect(root.querySelector('section.detail')?.textContent).toContain('does not exist');
    await expectAccessible(root);
  });
});
