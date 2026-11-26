import { describe, expect, it, vi } from 'vitest';
import { JobsApi } from '../../../api/jobs-api';
import { DownloadLinksApi } from '../../../api/download-links-api';
import { StorageApi, StorageConnection, StorageOverview } from '../../../api/storage-api';
import { FakeJobsApi, sampleJob } from '../../../api/testing/fakes-catalog';
import { FakeDownloadLinksApi, downloadLinkSettings, linkConnection } from '../../../api/testing/fakes-download-links';
import { FakeStorageApi, sampleConnection, sampleDamagedBlob, sampleOverview } from '../../../api/testing/fakes-storage';
import { LIVE_FAST_MS, LIVE_SLOW_MS } from '../../../core/live-refresh';
import { expectAccessible, renderScreen, settle, text } from '../../../testing/screen';
import { StoragePage } from './storage-page';

/**
 * Admin area › Settings › Storage (api-and-ui.md R-UI-262 to R-UI-264) over the in-memory fakes: the summary's tiles in the server's
 * states, the connections with their role, use and fold, the availability with its loss line, the encryption with configured against
 * actual, the acts only for a holder of `settings:write`, and the cadence of its reads.
 */

const TIB = 1024 ** 4;
const minutesAgo = (n: number): string => new Date(Date.now() - n * 60_000).toISOString();

type Get = <A>(token: abstract new (...args: never[]) => A) => A;
const storage = (get: Get) => get(StorageApi) as FakeStorageApi;

/**
 * Two connections as the plan's sketch draws them: `default` plain, nearly full (85 % of 2 TiB), the only holder of 1,234 blobs, asking
 * its store for nothing; `archive` encrypted since 22.11., filling, unreachable on 1 of 3 replicas, asking its store for kms.
 */
function twoConnections(): StorageConnection[] {
  return [
    sampleConnection({ capacityBytes: 2 * TIB }, { presentBytes: Math.round(0.85 * 2 * TIB), soleCopies: 1_234, soleBytes: 12 * 1024 ** 3 }),
    sampleConnection(
      {
        id: 'archive',
        position: 2,
        state: 'filling',
        endpoint: 'https://s3.eu-central-1.amazonaws.com',
        bucket: 'q-archive',
        encrypted: true,
        encryptedSince: '2026-11-22T08:02:11Z',
        keyedNames: true,
        capacityBytes: TIB,
        replicas: { configured: 3, live: 3, circuitOpen: 1 },
        queue: { blobs: 1_234, oldestQueuedAt: minutesAgo(4) },
        filledAt: null,
        passStartedAt: minutesAgo(15),
        storageKeys: [
          { id: 'sk-archive-2', generation: 2, state: 'active', createdAt: '2026-11-24T08:02:11Z', retiredAt: null, destroyedAt: null, copies: 410_000 },
          { id: 'sk-archive-1', generation: 1, state: 'retired', createdAt: '2026-11-22T08:02:11Z', retiredAt: '2026-11-24T08:02:11Z', destroyedAt: null, copies: 859 },
        ],
        settings: { ...sampleConnection().settings!, region: 'eu-central-1', sseMode: 'kms', sseKmsKeyId: 'alias/quader' },
      },
      { presentCopies: 410_859, presentBytes: Math.round(0.96 * TIB), keyedCopies: 410_859, fillRatio: 0.997 },
    ),
  ];
}

function twoOverview(): StorageOverview {
  const o = sampleOverview();
  return {
    ...o,
    connections: { state: 'degraded', configured: 2, reachable: 1, unreachable: [{ id: 'archive', circuitOpen: 1, configured: 3 }], detached: [] },
    availability: {
      state: 'at_risk',
      target: 2,
      minCopies: 1,
      perBlob: [
        { copies: 2, blobs: 410_859, bytes: 1_880_456_282_624 },
        { copies: 1, blobs: 1_234, bytes: 12_884_901_888 },
      ],
      belowTarget: { blobs: 1_234, bytes: 12_884_901_888 },
      loss: [{ connection: 'default', blobs: 1_234, bytes: 12_884_901_888 }],
    },
    encryption: { state: 'partly', encrypted: ['archive'], plain: ['default'], providerConfigured: ['archive'], encryptedShare: 0.5 },
    integrity: { ...o.integrity, authenticatedReads: ['archive'] },
  };
}

function two(get: Get): void {
  storage(get).connectionsResult = twoConnections();
  storage(get).overviewResult = twoOverview();
  (get(DownloadLinksApi) as FakeDownloadLinksApi).current = downloadLinkSettings({
    enabled: true,
    connections: [
      linkConnection({ name: 'default' }),
      linkConnection({ name: 'archive', encrypted: true, usable: false, reasons: [{ code: 'encrypted', detail: null }] }),
    ],
  });
}

const tile = (root: HTMLElement, kind: string): string => text(root.querySelector(`[data-tile="${kind}"]`)!);
const section = (root: HTMLElement, id: string): HTMLElement => root.querySelector<HTMLElement>(`section#${id}`)!;
const rows = (el: HTMLElement) => [...el.querySelectorAll<HTMLTableRowElement>('quader-data-table tbody tr:not(.expansion)')];

describe('Admin area › Settings › Storage (R-UI-262)', () => {
  it('R-UI-262 one store: the header names when the figures were counted, the tiles say Healthy, One store, Off, Verified and None, and the sections follow in order', async () => {
    const { root } = await renderScreen(StoragePage);
    expect(text(root.querySelector('h1')!)).toBe('Storage');
    expect(text(root.querySelector('.figures-line')!)).toContain('Figures counted 22.11.2026');
    expect(tile(root, 'connections')).toContain('Healthy');
    expect(tile(root, 'connections')).toContain('1 of 1 reachable');
    expect(tile(root, 'availability')).toContain('One store');
    expect(tile(root, 'availability')).toContain('No copy elsewhere');
    expect(tile(root, 'encryption')).toContain('Off');
    expect(tile(root, 'integrity')).toContain('Verified');
    expect(tile(root, 'problems')).toContain('None');
    expect(tile(root, 'stored')).toContain('1.7 TiB');
    expect(tile(root, 'stored')).toContain('in 412,093 blobs');
    // Green only for a positive state: the tiles of a plain single store are no success.
    expect(root.querySelector('[data-tile="availability"] [data-tone]')?.getAttribute('data-tone')).toBe('neutral');
    expect(root.querySelector('[data-tile="encryption"] [data-tone]')?.getAttribute('data-tone')).toBe('neutral');
    expect([...root.querySelectorAll('h2')].map(text).filter((h) => h !== 'Summary')).toEqual(['Connections 1', 'Availability', 'Encryption', 'Download links']);
    expect(text(section(root, 'availability'))).toContain('One store: no copy elsewhere');
    expect(root.querySelector('[data-tile="connections"] a')?.getAttribute('href')).toBe('/admin/settings/storage#connections');
    await expectAccessible(root);
  });

  it('R-UI-263 two connections: the role with the marks of the links, the use against the capacity nearly full and full, an unreachable and a filling one, and the loss line', async () => {
    const { root } = await renderScreen(StoragePage, { setup: two });
    expect(tile(root, 'connections')).toContain('Degraded');
    expect(tile(root, 'connections')).toContain('archive unreachable on 1 of 3 replicas');
    expect(tile(root, 'availability')).toContain('At risk');
    expect(tile(root, 'availability')).toContain('1,234 blobs (12.0 GiB) below the target');
    expect(tile(root, 'encryption')).toContain('1 of 2 connections');
    expect(tile(root, 'encryption')).toContain('50 % of the bytes');
    expect(tile(root, 'problems')).toContain('Ongoing');
    expect(tile(root, 'problems')).toContain('archive unreachable');
    const [first, second] = rows(section(root, 'connections'));
    expect(text(first!)).toContain('Uploads 1. · Reads 1.');
    expect(text(first!)).toContain('Links');
    expect(first!.querySelector('.mark.on')).toBeTruthy();
    expect(text(first!)).toContain('1.7 TiB of 2.0 TiB');
    expect(text(first!)).toContain('Nearly full');
    expect(text(second!)).toContain('Unreachable');
    expect(text(second!)).toContain('on 1 of 3 replicas');
    expect(text(second!)).toContain('Filling');
    expect(text(second!)).toContain('Full');
    expect(text(second!)).toContain('eu-central-1');
    const availability = section(root, 'availability');
    expect(text(availability)).toContain('Target 2 copies per blob · written before answering: 1');
    expect(text(availability)).toContain('2 copies: 410,859');
    expect(text(availability)).toContain('Losing default: 1,234 blobs (12.0 GiB) unavailable');
    const [d, a] = rows(availability);
    expect(text(d!)).toContain('1,234 · 12.0 GiB');
    expect(text(a!)).toContain('1,234 blobs, oldest 4 minutes');
    expect(text(a!)).toContain('Pass since');
    await expectAccessible(root);
  });

  it('R-UI-263 several connections without a sole copy survive the loss of any one, in green', async () => {
    const { root } = await renderScreen(StoragePage, {
      setup: (get) => {
        storage(get).connectionsResult = [sampleConnection(), sampleConnection({ id: 'copy', position: 2 })];
        const o = sampleOverview();
        storage(get).overviewResult = { ...o, availability: { ...o.availability, state: 'redundant', target: 2, loss: [] } };
      },
    });
    expect(tile(root, 'availability')).toContain('Every blob on 2 connections');
    expect(root.querySelector('[data-tile="availability"] [data-tone]')?.getAttribute('data-tone')).toBe('ok');
    expect(section(root, 'availability').querySelector('.loss-line.ok')?.textContent).toContain('Survives the loss of any one connection');
  });

  it('R-UI-263 a detached connection warns with the blobs only it holds and has no role; a row\'s fold names every key of its element', async () => {
    const { root, fixture } = await renderScreen(StoragePage, {
      setup: (get) =>
        (storage(get).connectionsResult = [
          sampleConnection({ capacityBytes: 5 * TIB }),
          sampleConnection(
            { id: 'old', position: null, state: 'detached', endpoint: null, settings: null, credentialSource: null, drain: null, replicas: { configured: 0, live: 3, circuitOpen: 0 } },
            { soleCopies: 12 },
          ),
          sampleConnection({ id: 'gone', state: 'retired', retiredAt: '2026-11-01T00:00:00Z' }),
        ]),
    });
    const connections = section(root, 'connections');
    const [, detached] = rows(connections);
    expect(text(detached!)).toContain('Detached');
    expect(text(detached!)).toContain('12 blobs only here');
    expect(detached!.querySelectorAll('td')[1]!.querySelector('quader-no-value')).toBeTruthy();
    expect(text(connections)).toContain('Retired, their ids are not used again: gone');
    const toggle = rows(connections)[0]!.querySelector<HTMLButtonElement>('th button')!;
    toggle.click();
    await settle(fixture);
    const facts = new Map([...root.querySelectorAll('#connection-default dt')].map((dt) => [text(dt), text(dt.nextElementSibling!)]));
    expect(facts.get('Credentials credentials_source')).toBe('static');
    expect(facts.get('Server-side encryption sse_mode')).toBe('none');
    expect(facts.get('Capacity capacity')).toBe('5.0 TiB');
    expect(facts.get('Scrub scrub_period, scrub_bytes_per_second')).toBe('every 30 days, at most 50.0 MiB/s');
    expect(facts.get('Encrypted by Quader encrypted, keyed_names')).toBe('false, false');
    expect(facts.get('First seen')).toContain('20.11.2026');
  });

  it('R-UI-264 encryption: Quader on since its first key with the share in the format, the store configured beside "not checked yet", the keys by identifier, what Quader encrypts and the root key', async () => {
    const { root, fixture } = await renderScreen(StoragePage, { setup: two });
    const enc = section(root, 'encryption');
    const [plain, encrypted] = rows(enc);
    expect(text(plain!)).toContain('Off');
    expect(text(plain!)).toContain('Configured none');
    expect(text(encrypted!)).toContain('On since 22.11.2026');
    expect(text(encrypted!)).toContain('100 % in the format');
    expect(text(encrypted!)).toContain('Keyed names');
    expect(text(encrypted!)).toContain('Configured kms alias/quader');
    expect(text(encrypted!)).toContain('Actual not checked yet');
    expect(text(encrypted!)).not.toContain('Differs');
    expect(text(encrypted!)).toContain('sk-archive-2');
    expect(text(encrypted!)).toContain('active, generation 2');
    expect(text(encrypted!)).toContain('1 retired · 0 destroyed');
    encrypted!.querySelector<HTMLButtonElement>('button[aria-expanded]')!.click();
    await settle(fixture);
    expect(text(encrypted!)).toContain('sk-archive-1 generation 1, retired');
    const encrypts = [...enc.querySelectorAll('ul.encrypts li .family')].map(text);
    expect(encrypts).toContain('Artifacts on archive');
    expect(encrypts).toContain('Artifacts on default');
    expect(encrypts).toContain('Backups: database');
    expect(text(enc.querySelector('.root-key')!)).toContain('Root key 2026-10 2 keys · check_circleRings agree');
    // Identifiers only: no key material, no credential and no signed address on the page.
    expect(text(root)).not.toMatch(/X-Amz-Signature|secret_access_key|BEGIN /);
    await expectAccessible(root);
  });

  it('R-UI-264 a share of an encrypted connection below 100 % in the format differs, in the warning tone', async () => {
    const { root } = await renderScreen(StoragePage, {
      setup: (get) => (storage(get).connectionsResult = [sampleConnection({ encrypted: true, encryptedSince: '2026-11-22T08:02:11Z' }, { keyedCopies: 400_000 })]),
    });
    const row = rows(section(root, 'encryption'))[0]!;
    expect(text(row)).toContain('97 % in the format');
    expect(text(row.querySelector('span.warn')!)).toContain('Differs');
  });

  it('R-UI-262 the auditor reads the page and is offered no act; the administrator counts now and runs the reconciliation', async () => {
    const reader = await renderScreen(StoragePage, { capabilities: ['settings:read'], setup: two });
    const acts = [...reader.root.querySelectorAll('button')].map((b) => text(b));
    expect(acts.some((a) => /Count now|Run the reconciliation now|Check again|Save changes|Test link/.test(a))).toBe(false);
    expect(text(reader.root)).toContain('Read only: changing this needs the permission settings:write');

    const admin = await renderScreen(StoragePage, { setup: two });
    const jobs = admin.get(JobsApi) as FakeJobsApi;
    jobs.jobList = [sampleJob({ id: 'job-run-1', kind: 'storage-summary', state: 'running', finishedAt: null })];
    const count = [...admin.root.querySelectorAll<HTMLButtonElement>('.figures-line button')].find((b) => text(b) === 'Count now')!;
    count.click();
    await settle(admin.fixture);
    expect(jobs.runs.map((r) => r.name)).toEqual(['storage-summary']);
    expect(text(admin.root.querySelector('.figures-line')!)).toContain('Counting…');
    const reconcile = [...admin.root.querySelectorAll<HTMLButtonElement>('#availability button')].find((b) => text(b) === 'Run the reconciliation now')!;
    reconcile.click();
    await settle(admin.fixture);
    expect(jobs.runs.map((r) => r.name)).toEqual(['storage-summary', 'storage-reconcile']);
  });

  it('R-UI-263 the blobs no connection holds whole are a closed section of Availability, and an administrator checks one again', async () => {
    const { root, fixture, get } = await renderScreen(StoragePage, {
      setup: (g) => (storage(g).damagedResult = { items: [sampleDamagedBlob()], more: true }),
    });
    const fold = section(root, 'availability').querySelector('quader-storage-damaged-blobs')!;
    fold.querySelector<HTMLButtonElement>('button')!.click();
    await settle(fixture);
    expect(text(fold)).toContain('3a7bd3e2360a');
    expect(text(fold)).toContain('default: missing');
    expect(text(fold)).toContain('quader admin storage verify lists every one');
    fold.querySelector<HTMLButtonElement>('td.actions button')!.click();
    await settle(fixture);
    expect(storage(get).rechecks).toEqual([sampleDamagedBlob().sha256]);
    expect(text(fold)).toContain('default: present');

    const none = await renderScreen(StoragePage);
    expect(none.root.querySelector('quader-storage-damaged-blobs')).toBeNull();
  });
});

describe('The reads of Settings › Storage (R-UI-262, R-UI-213)', () => {
  function setVisibility(state: 'visible' | 'hidden'): void {
    Object.defineProperty(document, 'visibilityState', { configurable: true, get: () => state });
    document.dispatchEvent(new Event('visibilitychange'));
  }

  it('R-UI-262 the page reads again every 30 s while seen, every 3 s while its count runs, and not while hidden', async () => {
    vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] });
    try {
      const { root, fixture, get } = await renderScreen(StoragePage);
      const fake = storage(get);
      const jobs = get(JobsApi) as FakeJobsApi;
      const reads = fake.overviewReads;
      vi.advanceTimersByTime(LIVE_SLOW_MS - 1);
      await settle(fixture);
      expect(fake.overviewReads).toBe(reads);
      vi.advanceTimersByTime(1);
      await settle(fixture);
      expect(fake.overviewReads).toBe(reads + 1);

      jobs.jobList = [sampleJob({ id: 'job-run-1', kind: 'storage-summary', state: 'running', finishedAt: null })];
      [...root.querySelectorAll<HTMLButtonElement>('.figures-line button')].find((b) => text(b) === 'Count now')!.click();
      await settle(fixture);
      vi.advanceTimersByTime(LIVE_FAST_MS);
      await settle(fixture);
      expect(fake.overviewReads).toBe(reads + 2);
      jobs.jobList = [sampleJob({ id: 'job-run-1', kind: 'storage-summary', state: 'succeeded' })];
      vi.advanceTimersByTime(LIVE_FAST_MS);
      await settle(fixture);
      const afterRun = fake.overviewReads;
      expect(text(root.querySelector('.figures-line')!)).toContain('Count now');
      vi.advanceTimersByTime(LIVE_FAST_MS);
      await settle(fixture);
      expect(fake.overviewReads).toBe(afterRun);

      setVisibility('hidden');
      vi.advanceTimersByTime(LIVE_SLOW_MS * 4);
      await settle(fixture);
      expect(fake.overviewReads).toBe(afterRun);
    } finally {
      setVisibility('visible');
      vi.useRealTimers();
    }
  });
});
