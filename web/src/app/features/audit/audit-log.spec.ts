import { Router } from '@angular/router';
import { AuditApi, AuditEvent } from '../../api/audit-api';
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

  it('R-UI-215 closing an opened event gives the focus back to the row it was opened from', async () => {
    const { root, fixture, router } = await open({ event: '01933b1e-7c2a-7f4e-9d1b-6a2f0c9e8b10' });
    const opener = root.querySelector<HTMLButtonElement>('tbody th button.link[aria-current="true"]')!;
    expect(opener).toBeTruthy();
    button(root.querySelector('section.detail')!, 'Close').click();
    await settle(fixture);
    expect(router.navigate).toHaveBeenLastCalledWith([], { queryParams: { event: null }, queryParamsHandling: 'merge' });
    expect(document.activeElement).toBe(opener);
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
    expect(heads.map((h) => h.replace(/info$|Help.*$/, '').trim())).toEqual(['Time', 'Actor', 'Action', 'Target', 'Outcome']);
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
      audit.rows = () => () =>
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

  it('R-UI-213 the log is read again every 30 s with its range moved to now, in place: a new event appears and the rows shown stay', async () => {
    vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] });
    try {
      const { root, fixture, audit } = await open();
      const rows = root.querySelectorAll('tbody tr');
      const calls = audit.filterCalls.length;
      const before = audit.filterCalls.at(-1)!;
      audit.eventList = [sampleEvent({ id: 'evt-new', action: 'auth.login', category: 'auth' }), ...audit.eventList];
      vi.advanceTimersByTime(30_000);
      await settle(fixture);
      expect(audit.filterCalls.length).toBe(calls + 1);
      const after = audit.filterCalls.at(-1)!;
      expect(after.to! >= before.to!).toBe(true);
      expect({ ...after, from: '', to: '' }).toEqual({ ...before, from: '', to: '' });
      expect(root.querySelectorAll('tbody tr')).toHaveLength(rows.length + 1);
      expect(root.querySelectorAll('tbody tr')[1]).toBe(rows[0]);
    } finally {
      vi.useRealTimers();
    }
  });

  describe('R-OPS-055 a download is one row', () => {
    const alice = { type: 'user', id: 'u-1', name: 'alice', tokenId: 't-1', authMethod: 'bearer' } as const;
    /** An item's event as the server writes it from R-OPS-054 on: its unit, its package, its version where its path names one. */
    function item(i: number, at: number, over: { unit?: string; version?: string; actor?: typeof alice | null; ip?: string } = {}) {
      const unit = over.unit ?? 'acme/app';
      const path = i === 0 ? `${unit}/_tags/1.4.0` : `${unit}/_blobs/sha256:${String(i).padStart(64, '0')}`;
      return sampleEvent({
        id: `0193b4a0-0000-7000-8000-${String(at).padStart(6, '0')}${String(i).padStart(6, '0')}`,
        occurredAt: new Date(Date.UTC(2026, 10, 26, 10, 0, 0) + at).toISOString(),
        action: 'artifact.downloaded',
        category: 'content',
        actor: over.actor === null ? { type: 'anonymous', id: null, name: null, tokenId: null, authMethod: null } : (over.actor ?? alice),
        source: { ip: over.ip ?? '10.0.0.5', userAgent: 'docker/27.3.1' },
        resource: { type: 'repository', id: null },
        repository: 'platform/images',
        path,
        details: {
          path,
          sha256: i.toString(16).padStart(4, '0').repeat(16),
          size: 1000 + i,
          package: unit,
          unit,
          ...(over.version ? { version: over.version } : {}),
        },
      });
    }
    /** A pull of 13 items by alice, an anonymous pull of the same image beside it, and an event before the events named their unit. */
    function world(audit: FakeAuditApi): void {
      const pull = Array.from({ length: 13 }, (_, i) => item(i, i * 1000, i === 0 ? { version: '1.4.0' } : {}));
      const anon = [item(50, 2500, { actor: null, ip: '10.0.0.9' }), item(51, 3500, { actor: null, ip: '10.0.0.9' })];
      const old = sampleEvent({ id: 'evt-old', occurredAt: '2026-11-26T09:59:00.000Z', action: 'artifact.downloaded', category: 'content', repository: 'platform/images', path: 'acme/old/_blobs/sha256:1', details: { path: 'acme/old/_blobs/sha256:1', sha256: '1'.padStart(64, '0') } });
      audit.eventList = [...pull, ...anon, old];
    }

    it('R-OPS-055 R-UI-037 a pull is one row naming its image, its tag, its items and their bytes; another caller and an older event are rows of their own', async () => {
      const { root } = await open({}, world);
      const rows = [...root.querySelectorAll<HTMLTableRowElement>('table.data > tbody > tr:not(.expansion)')];
      expect(rows).toHaveLength(3);
      const pull = rows.find((r) => r.textContent?.includes('1.4.0'))!;
      const target = pull.querySelector('td.target')!;
      expect(target.querySelector('.subject-name')?.textContent).toBe('acme/app');
      expect(target.querySelector('.subject-version')?.textContent).toBe('1.4.0');
      expect(target.textContent).toContain('13 items');
      // The row names the act in one word, and no key: its items name their actions (R-OPS-056).
      expect(pull.querySelector('th[scope="row"] button.link')?.textContent?.trim()).toBe('Downloaded');
      expect(pull.querySelector('th[scope="row"] .action-key')).toBeNull();
      expect(target.textContent).toContain('12.8 KiB');
      expect(pull.querySelector('td.nowrap .secondary')?.textContent?.trim()).toBe('for 12 s');
      // The anonymous pull names no tag: its first item's digest, cut to 12 hex digits.
      const anon = rows.find((r) => r.textContent?.includes('2 items'))!;
      expect(anon.querySelector('.subject-version')?.textContent).toBe('sha256:003200320032');
      // The event before the unit is the event's row as before, without a chevron.
      expect(rows.at(-1)!.querySelector('[aria-expanded]')).toBeNull();
      await expectAccessible(root);
    });

    it('R-OPS-055 R-UI-215 an opened download lists its first ten items, each opening its event, and links to all of them; it stays open when the log is read again', async () => {
      vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] });
      try {
        const { root, fixture, audit, router } = await open({}, world);
        const toggle = root.querySelector<HTMLButtonElement>('td.target button[aria-label="Show the items: Downloaded acme/app 1.4.0"]')!;
        expect(toggle.getAttribute('aria-label')).toBe('Show the items: Downloaded acme/app 1.4.0');
        toggle.click();
        await settle(fixture);
        expect(toggle.getAttribute('aria-expanded')).toBe('true');
        const items = () => [...root.querySelectorAll('.download-items tbody tr')];
        expect(items()).toHaveLength(10);
        expect(items()[0]!.querySelector('th a')?.textContent).toBe('acme/app/_tags/1.4.0');
        expect(items()[0]!.querySelector('code')?.getAttribute('title')).toBe('0'.repeat(64));
        const all = root.querySelector<HTMLAnchorElement>('.download-items a.all-items')!;
        expect(all.textContent?.trim()).toBe('All 13 items');
        (items()[1]!.querySelector('button.link') as HTMLButtonElement).click();
        const [, extras] = (router.navigate as ReturnType<typeof vi.fn>).mock.calls.at(-1)!;
        expect(extras.queryParams).toEqual({ event: audit.eventList[1]!.id });
        // A fourteenth item arrives: the row grows in place and stays open (R-UI-213).
        audit.eventList = [...audit.eventList, item(13, 13_000)];
        vi.advanceTimersByTime(30_000);
        await settle(fixture);
        expect(root.querySelectorAll('td.target button[aria-expanded="true"]')).toHaveLength(1);
        expect(toggle.getAttribute('aria-expanded')).toBe('true');
        expect(toggle.closest('td.target')?.textContent).toContain('14 items');
        expect(items()).toHaveLength(10);
      } finally {
        vi.useRealTimers();
      }
    });

    it('R-OPS-055 the log of one download lists every item as an event, oldest first, names the download as a filter and offers no export', async () => {
      const { root } = await open({ group: '0193b4a0-0000-7000-8000-000000000000' }, world);
      const rows = [...root.querySelectorAll('table.data > tbody > tr:not(.expansion)')];
      expect(rows).toHaveLength(13);
      expect(rows[0]!.querySelector('td.target')?.textContent).toContain('acme/app/_tags/1.4.0');
      expect(chipTexts(root)).toEqual(['Events of: acme/app 1.4.0']);
      expect(root.querySelectorAll('a[download]')).toHaveLength(0);
      expect(root.querySelector('caption')?.textContent).toContain('The events of one row');
      await expectAccessible(root);
    });
  });

  describe('R-OPS-056 a push, a publish and a delete are one row', () => {
    const priya = { type: 'user', id: 'u-2', name: 'priya', tokenId: 't-2', authMethod: 'bearer' } as const;
    let n = 0;
    function change(at: number, action: string, details: Record<string, unknown>, over: Partial<AuditEvent> = {}): AuditEvent {
      n++;
      return sampleEvent({
        id: `0193b4a1-0000-7000-8000-${String(at).padStart(6, '0')}${String(n).padStart(6, '0')}`,
        occurredAt: new Date(Date.UTC(2026, 10, 26, 11, 0, 0) + at).toISOString(),
        action,
        category: action.startsWith('authz.') ? 'authz' : 'content',
        actor: priya,
        source: { ip: '10.0.0.7', userAgent: 'docker/27.3.1' },
        resource: { type: 'repository', id: null },
        repository: 'platform/images',
        path: typeof details['path'] === 'string' ? details['path'] : null,
        details,
        ...over,
      });
    }
    const sha = (i: number) => i.toString(16).padStart(4, '0').repeat(16);
    /** A push of an image of two layers with its tag, its allowed writes, and a delete of an npm package of three versions. */
    function world(audit: FakeAuditApi): void {
      n = 0;
      const file = (at: number, path: string, size: number, version?: string) =>
        change(at, 'artifact.deployed', { path, sha256: sha(at + 1), size, overwritten: false, package: 'acme/app', unit: 'acme/app', ...(version ? { version } : {}) });
      const write = (at: number) => change(at, 'authz.write.allowed', { package: 'acme/app', unit: 'acme/app', reason: 'allow_grant' });
      const push = [
        file(0, 'acme/app/_blobs/sha256:' + sha(1), 2048),
        write(1),
        file(2, 'acme/app/_blobs/sha256:' + sha(2), 4096),
        write(3),
        file(4, 'acme/app/_tags/1.4.0', 1024, '1.4.0'),
        write(5),
        change(6, 'package.published', { package: 'acme/app', version: '1.4.0', format: 'oci', unit: 'acme/app', files: [{ path: 'acme/app', sha256: sha(9), size: 1024 }] },
          { resource: { type: 'package', id: null } }),
      ];
      const del = ['1.0.0', '1.1.0', '2.0.0'].flatMap((v, i) => [
        change(60_000 + i * 10, 'artifact.deleted', { path: `left-pad/-/left-pad-${v}.tgz`, sha256: sha(20 + i), size: 100, package: 'left-pad', version: v, unit: 'left-pad' }, { repository: 'web/npm-local' }),
        change(60_001 + i * 10, 'npm.unpublish', { package: 'left-pad', version: v, integrity: 'sha512-x', unit: 'left-pad' }, { repository: 'web/npm-local' }),
      ]);
      audit.eventList = [...push, ...del];
    }

    it('R-OPS-056 R-UI-037 a push is one row naming the image, its tag, its items and the bytes of its files; its writes are a row beside it; a delete of three versions names three', async () => {
      const { root } = await open({}, world);
      const rows = [...root.querySelectorAll<HTMLTableRowElement>('table.data > tbody > tr:not(.expansion)')];
      expect(rows.map((r) => r.querySelector('th[scope="row"] button.link')?.textContent?.trim())).toEqual(['Deleted', 'Writes allowed', 'Pushed']);
      const push = rows[2]!.querySelector('td.target')!;
      expect(push.querySelector('.subject-name')?.textContent).toBe('acme/app');
      expect(push.querySelector('.subject-version')?.textContent).toBe('1.4.0');
      expect(push.textContent).toContain('4 items');
      expect(push.textContent).toContain('7.0 KiB');
      const del = rows[0]!.querySelector('td.target')!;
      expect(del.querySelector('.subject-name')?.textContent).toBe('left-pad');
      expect(del.querySelector('.subject-version')?.textContent).toBe('3 versions');
      expect(del.textContent).toContain('6 items');
      await expectAccessible(root);
    });

    it('R-OPS-056 R-UI-215 an opened push lists its files with their paths and the version it published by its action, without a link', async () => {
      const { root, fixture } = await open({}, world);
      const toggle = root.querySelector<HTMLButtonElement>('td.target button[aria-label="Show the items: Pushed acme/app 1.4.0"]')!;
      toggle.click();
      await settle(fixture);
      const items = [...root.querySelectorAll('.download-items tbody tr')];
      expect(items).toHaveLength(4);
      expect(items[0]!.querySelector('th a')?.textContent).toContain('acme/app/_blobs/sha256:');
      const published = items[3]!.querySelector('th')!;
      expect(published.querySelector('a')).toBeNull();
      expect(published.querySelector('.item-act')?.textContent?.trim()).toBe('Package version published 1.4.0');
      expect(items[3]!.querySelector('code')?.getAttribute('title')).toBe(sha(9));
      expect(root.querySelector('.download-items a.all-items')).toBeNull();
      await expectAccessible(root);
    });
  });
});
