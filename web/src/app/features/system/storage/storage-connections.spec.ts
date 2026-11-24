import { describe, expect, it } from 'vitest';
import { StorageApi, StorageConnection } from '../../../api/storage-api';
import { FakeStorageApi, sampleConnection, sampleDamagedBlob } from '../../../api/testing/fakes-storage';
import { expectAccessible, renderScreen, text } from '../../../testing/screen';
import { StoragePage } from './storage-page';
import { connectionState, dashboardStorageAlarm, percentOf, storageConditions } from './storage-model';

/**
 * The tab Storage of System status (api-and-ui.md R-UI-196, Q-UI-930, Q-UI-931) over the in-memory fakes: the rows of the
 * connections in their order with their states in words and figures, the fold of a row, the blobs no connection holds whole, and that
 * nothing on the page changes a connection.
 */

const minutesAgo = (n: number): string => new Date(Date.now() - n * 60_000).toISOString();

/** `default` active with every blob; `secondary` filling with half of them and a queue of 12 whose oldest waits 4 minutes. */
function twoConnections(): StorageConnection[] {
  return [
    sampleConnection(),
    sampleConnection(
      {
        id: 'secondary',
        position: 2,
        state: 'filling',
        endpoint: 'https://s3.eu-central-1.amazonaws.com',
        bucket: 'quader-copy',
        filledAt: null,
        queue: { blobs: 12, oldestQueuedAt: minutesAgo(4) },
        capabilities: null,
      },
      { presentCopies: 206_046, presentBytes: 946_670_592_256, backlogBlobs: 206_047, fillRatio: 0.5, verifiedWithinPeriod: 206_046 },
    ),
  ];
}

function fake(get: <A>(token: abstract new (...args: never[]) => A) => A): FakeStorageApi {
  return get(StorageApi) as FakeStorageApi;
}

const rows = (root: HTMLElement) => [...root.querySelectorAll<HTMLTableRowElement>('quader-data-table tbody tr:not(.expansion)')];

describe('The tab Storage of System status (R-UI-196)', () => {
  it('R-UI-196 two connections render two rows in order, Active and Filling "50 % of the blobs", and a queue of 12 blobs whose oldest is 4 minutes', async () => {
    const { root } = await renderScreen(StoragePage, { setup: (get) => (fake(get).connectionsResult = twoConnections()) });
    const [first, second] = rows(root);
    expect(text(first!.querySelector('th')!)).toContain('default');
    expect(text(first!.querySelector('th')!)).toContain('object.storage.eu01.example.net · quader-blobs/quader');
    expect(text(first!)).toContain('Active');
    expect(text(first!)).toContain('100 %, 1.7 TiB');
    expect(text(first!)).toContain('100 % read within 30 days');
    expect(text(second!.querySelector('th')!)).toContain('secondary');
    expect(text(second!)).toContain('Filling');
    expect(text(second!)).toContain('50 % of the blobs');
    expect(text(second!)).toContain('12 blobs, oldest 4 minutes');
    expect(text(second!)).not.toContain('Behind for over an hour');
    expect(text(root.querySelector('p.summary')!)).toContain('Every connection is healthy');
    await expectAccessible(root);
  });

  it('R-UI-196 a connection unreachable on 1 of 3 replicas says so, a detached one warns with the blobs only it holds, and the line names both', async () => {
    const { root } = await renderScreen(StoragePage, {
      setup: (get) =>
        (fake(get).connectionsResult = [
          sampleConnection({ replicas: { configured: 3, live: 3, circuitOpen: 1 } }),
          sampleConnection(
            { id: 'archive', position: null, state: 'detached', endpoint: null, settings: null, credentialSource: null, drain: null, replicas: { configured: 0, live: 3, circuitOpen: 0 } },
            { soleCopies: 12 },
          ),
        ]),
    });
    const [first, second] = rows(root);
    expect(text(first!)).toContain('Unreachable');
    expect(text(first!)).toContain('on 1 of 3 replicas');
    expect(first!.querySelector('span.bad')).toBeTruthy();
    expect(text(second!)).toContain('Detached');
    expect(text(second!)).toContain('12 blobs only here');
    expect(second!.querySelector('span.warn')).toBeTruthy();
    const line = text(root.querySelector('p.summary')!);
    expect(line).toContain('Unreachable from some replicas: default');
    expect(line).toContain('In no replica\'s configuration but holding copies: archive');
  });

  it('R-UI-196 one connection shows one row whose Backlog is the en dash; draining, drained and a partial configuration are words too', async () => {
    const { root } = await renderScreen(StoragePage);
    expect(rows(root)).toHaveLength(1);
    const cells = rows(root)[0]!.querySelectorAll('td');
    expect(cells[2]!.querySelector('quader-no-value')).toBeTruthy();
    expect(text(root.querySelector('p.summary')!)).toContain('The connection is healthy');

    const other = await renderScreen(StoragePage, {
      setup: (get) =>
        (fake(get).connectionsResult = [
          sampleConnection({ state: 'draining', drain: true }, { presentCopies: 1203 }),
          sampleConnection({ id: 'old', state: 'drained', drain: true, replicas: { configured: 2, live: 3, circuitOpen: 0 } }),
          sampleConnection({ id: 'gone', state: 'retired', retiredAt: '2026-11-01T00:00:00Z' }),
        ]),
    });
    const [draining, drained] = rows(other.root);
    expect(rows(other.root)).toHaveLength(2);
    expect(text(draining!)).toContain('Draining');
    expect(text(draining!)).toContain('1,203 copies left');
    expect(text(drained!)).toContain('Drained');
    // what a drained connection asks for stands once, in the line over the table (Q-UI-933)
    expect(text(other.root.querySelector('p.summary')!)).toContain('Drained, can be removed from the configuration: old');
    expect(text(drained!)).toContain('Configured on 2 of 3 replicas');
    expect(text(other.root)).toContain('Retired, their ids are not used again: gone');
  });

  it('R-UI-196 a row\'s name opens its fold with the settings as read and the capabilities, and the page offers no control that changes a connection', async () => {
    const { root, fixture } = await renderScreen(StoragePage, { setup: (get) => (fake(get).connectionsResult = twoConnections()) });
    const toggle = rows(root)[0]!.querySelector<HTMLButtonElement>('th button')!;
    expect(toggle.getAttribute('aria-expanded')).toBe('false');
    toggle.click();
    fixture.detectChanges();
    await fixture.whenStable();
    expect(toggle.getAttribute('aria-expanded')).toBe('true');
    const facts = new Map([...root.querySelectorAll('#connection-default dt')].map((dt) => [text(dt), text(dt.nextElementSibling!)]));
    expect(facts.get('Order')).toContain('Number 1 in the list');
    expect(facts.get('Credentials')).toBe('static');
    expect(facts.get('Checksums')).toContain('payload sha256');
    expect(facts.get('Found by the start check')).toContain('Payload checksums enforced');
    expect(facts.get('Found by the start check')).toContain('Copies keep and replace metadata');
    expect(facts.get('Scrub')).toBe('every copy read whole once in 30 days, at most 50.0 MiB per second');
    expect(root.querySelectorAll('input, select, textarea')).toHaveLength(0);
    const labels = [...root.querySelectorAll('button')].map((b) => b.getAttribute('aria-label') ?? text(b));
    expect(labels.every((l) => /^Details of the connection|Blobs no connection holds whole|The last runs of the storage tasks/.test(l))).toBe(true);
    await expectAccessible(root);
  });

  it('R-UI-196 the blobs no connection holds whole list their copies and first item, and an administrator checks one again', async () => {
    const { root, fixture, get } = await renderScreen(StoragePage, {
      setup: (get) => (fake(get).damagedResult = { items: [sampleDamagedBlob()], more: true }),
    });
    expect(text(root.querySelector('p.summary')!)).toContain('More than 1 blobs are held whole by no connection');
    const section = root.querySelectorAll('quader-disclosure')[0]!;
    section.querySelector<HTMLButtonElement>('button')!.click();
    fixture.detectChanges();
    await fixture.whenStable();
    expect(text(section)).toContain('3a7bd3e2360a');
    expect(text(section)).toContain('Missing');
    expect(text(section)).toContain('default: missing');
    expect(text(section)).toContain('platform/npm-local: left-pad/-/left-pad-1.3.0.tgz');
    expect(text(section)).toContain('quader admin storage verify lists every one');
    section.querySelector<HTMLButtonElement>('td.actions button')!.click();
    for (let i = 0; i < 3; i++) await fixture.whenStable();
    fixture.detectChanges();
    expect(fake(get).rechecks).toEqual([sampleDamagedBlob().sha256]);
    expect(text(section)).toContain('default: present');

    const reader = await renderScreen(StoragePage, {
      capabilities: ['settings:read'],
      setup: (get) => (fake(get).damagedResult = { items: [sampleDamagedBlob()], more: false }),
    });
    expect(reader.root.querySelector('td.actions')).toBeNull();
    expect(text(reader.root.querySelector('p.summary')!)).toContain('1 blob is held whole by no connection');
  });
});

describe('The derivations of the tab Storage and the dashboard (R-UI-196, R-UI-197)', () => {
  it('R-UI-196 a share is rounded down, so a connection with a blob missing never shows 100 %', () => {
    expect([percentOf(1), percentOf(0.996), percentOf(0.5), percentOf(0.0001), percentOf(0)]).toEqual([100, 99, 50, 1, 0]);
    expect(connectionState(sampleConnection({ state: 'filling' }, { fillRatio: 0.25 }))).toEqual({ kind: 'filling', percent: 25 });
    expect(connectionState(sampleConnection({ state: 'filling', summary: null }))).toEqual({ kind: 'filling', percent: null });
  });

  it('R-UI-196 a backlog older than an hour is behind unless the connection fills; copies past their period are behind', () => {
    const old = sampleConnection({ queue: { blobs: 3, oldestQueuedAt: minutesAgo(90) } }, { scrubOverdue: 2 });
    const kinds = storageConditions([old], null, Date.now()).map((c) => c.kind);
    expect(kinds).toEqual(['backlog-behind', 'scrub-behind']);
    expect(storageConditions([{ ...old, state: 'filling' }], null, Date.now()).map((c) => c.kind)).toEqual(['scrub-behind']);
  });

  it('R-UI-197 one of several connections unreachable on every replica is a warning, every one an error, one replica of three nothing', () => {
    const down = (id: string) => sampleConnection({ id, replicas: { configured: 3, live: 3, circuitOpen: 3 } });
    expect(dashboardStorageAlarm([down('default'), sampleConnection({ id: 'secondary' })])).toBe('warning');
    expect(dashboardStorageAlarm([down('default'), down('secondary')])).toBe('error');
    expect(dashboardStorageAlarm([sampleConnection({ replicas: { configured: 3, live: 3, circuitOpen: 1 } })])).toBeNull();
  });
});
