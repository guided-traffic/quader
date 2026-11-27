import { ComponentFixture } from '@angular/core/testing';
import { describe, expect, it, vi } from 'vitest';
import { StorageApi } from '../../../api/storage-api';
import { ApiError } from '../../../api/problem';
import { FakeStorageApi, sampleConnection, sampleFinding, sampleHistory, sampleOverview } from '../../../api/testing/fakes-storage';
import { button, expectAccessible, renderScreen, settle, text, type } from '../../../testing/screen';
import { IntegritySection, checkMarks } from './integrity-section';

/**
 * The section Integrity (api-and-ui.md R-UI-265; storage.md R-STOR-111): the checks of a plain and an encrypted connection as marks, the
 * scrub's coverage with **Scrub now**, the copies damaged in the period, and the findings with every state, **Check again** and the
 * acknowledge dialog.
 */

type Get = <A>(token: abstract new (...args: never[]) => A) => A;
const storage = (get: Get) => get(StorageApi) as FakeStorageApi;

const plain = sampleConnection();
const encrypted = sampleConnection(
  { id: 'archive', position: 2, encrypted: true, encryptedSince: '2026-11-22T08:02:11Z' },
  { keyedCopies: 412_093, verifiedWithinPeriod: 400_000, scrubOverdue: 12 },
);

function inputs(over: Record<string, unknown> = {}): Record<string, unknown> {
  return {
    overview: sampleOverview(),
    connections: [plain, encrypted],
    history: sampleHistory('30d', ['default', 'archive'], {}, {
      default: { count: 3, bytes: 3_250_585, byOutcome: { rewritten: 3 }, bySource: { scrub: 3 } },
      archive: { count: 2, bytes: 2048, byOutcome: { open: 1, acknowledged: 1 }, bySource: { download: 2 } },
    }),
    period: '30d',
    canWrite: true,
    ...over,
  };
}

/** Opens the menu of a finding's row and chooses an entry (R-UI-163). */
async function choose(list: Element, fixture: ComponentFixture<unknown>, label: string): Promise<void> {
  list.querySelector<HTMLButtonElement>('td.actions button')!.click();
  await settle(fixture);
  const item = [...document.querySelectorAll<HTMLButtonElement>('.cdk-overlay-container .menu-item')].find((b) => text(b) === label);
  if (!item) throw new Error(`no menu entry ${label}`);
  item.click();
  await settle(fixture);
}

const rowOf = (root: HTMLElement, id: string): HTMLTableRowElement =>
  [...root.querySelectorAll<HTMLTableRowElement>('quader-data-table tbody tr')].find((r) => text(r.querySelector('th')!) === id)!;

describe('The section Integrity (R-UI-265)', () => {
  it('R-UI-265 a plain connection says "Digest checks only"; an encrypted one has every read checked since its first key', async () => {
    expect(checkMarks(plain, true).map((m) => [m.kind, m.on])).toEqual([
      ['upload', true],
      ['download', true],
      ['digest-only', false],
      ['writes', true],
    ]);
    const { root } = await renderScreen(IntegritySection, { inputs: inputs() });
    const p = rowOf(root, 'default');
    const e = rowOf(root, 'archive');
    expect(text(p)).toContain('Digest at upload');
    expect(text(p)).toContain('Download check');
    expect(text(p)).toContain('Digest checks only');
    expect(text(p)).not.toContain('Every read checked');
    expect(text(e)).toContain('Every read checked since 22.11.2026');
    expect(text(e)).toContain('97 % within 30 days');
    expect(text(e)).toContain('12 copies overdue');
    expect(text(p)).toContain('3 copies (3.1 MiB)');
    expect(text(p)).toContain('all resolved');
    expect(text(e)).toContain('1 open');
    await expectAccessible(root);
  });

  it('R-UI-265 the download check off is named with its dash; the auditor has no Scrub now, the administrator scrubs one connection', async () => {
    const off = sampleOverview();
    const reader = await renderScreen(IntegritySection, {
      inputs: inputs({ overview: { ...off, integrity: { ...off.integrity, downloadVerify: false } }, canWrite: false }),
    });
    expect(text(rowOf(reader.root, 'default'))).toContain('Download check off');
    expect([...reader.root.querySelectorAll('button')].some((b) => /Scrub now|Check again|Acknowledge/.test(text(b)))).toBe(false);

    const admin = await renderScreen(IntegritySection, { inputs: inputs() });
    const emit = vi.spyOn(admin.fixture.componentInstance.scrub, 'emit');
    button(rowOf(admin.root, 'archive'), 'Scrub now').click();
    expect(emit).toHaveBeenCalledWith('archive');
    // A scrub that runs shows its spinner on its own connection only.
    admin.fixture.componentRef.setInput('scrubbing', new Set(['archive']));
    await settle(admin.fixture);
    expect(button(rowOf(admin.root, 'archive'), 'Scrub now').disabled).toBe(true);
    expect(button(rowOf(admin.root, 'default'), 'Scrub now').disabled).toBe(false);
  });

  it('R-UI-265 the findings: Open by default while open ones exist, every state in words, and All on demand', async () => {
    const o = sampleOverview();
    const { root, fixture, get } = await renderScreen(IntegritySection, {
      inputs: inputs({ overview: { ...o, problems: { ...o.problems, state: 'open', open: 1 } } }),
      setup: (g) =>
        (storage(g).findingsResult = [
          sampleFinding({ id: 3, resolution: null, resolvedAt: null, source: 'download', reason: 'not_found', unavailable: true, connection: 'archive' }),
          sampleFinding({
            id: 2,
            resolution: null,
            resolvedAt: null,
            acknowledged: { at: '2026-11-21T09:00:00Z', by: { id: 'u1', name: 'priya' }, note: 'Lifecycle rule removed it.' },
          }),
          sampleFinding({ id: 1 }),
        ]),
    });
    const list = root.querySelector('quader-storage-findings')!;
    expect(storage(get).findingQueries.at(-1)).toEqual({ period: '30d', state: 'open' });
    expect(list.querySelectorAll('tbody tr').length).toBe(1);
    expect(text(list)).toContain('Download · Not found');
    expect(text(list)).toContain('No other copy');
    expect(text(list)).toContain('lodash/-/lodash-4.17.21.tgz');
    expect(text(list)).toContain('+ 2 more');
    button(list, 'All').click();
    await settle(fixture);
    expect(storage(get).findingQueries.at(-1)).toEqual({ period: '30d', state: 'all' });
    expect(text(list)).toContain('Acknowledged');
    expect(text(list)).toContain('by priya');
    expect(text(list)).toContain('Lifecycle rule removed it.');
    expect(text(list)).toContain('Repaired');
    // A resolved finding has no act: neither Check again nor Acknowledge.
    expect(list.querySelectorAll('tbody tr')[2]!.querySelector('button')).toBeNull();
  });

  it('R-UI-265 with no open finding the list shows All; an empty period says so', async () => {
    const { root, get } = await renderScreen(IntegritySection, { inputs: inputs() });
    expect(storage(get).findingQueries.at(-1)).toEqual({ period: '30d', state: 'all' });
    expect(text(root.querySelector('quader-storage-findings')!)).toContain('No damaged copy in this period.');
  });

  it('R-UI-265 Acknowledge opens the dialog, sends the note, shows the row acknowledged and never resolved; a refusal stays in the dialog', async () => {
    const { root, fixture, get } = await renderScreen(IntegritySection, {
      inputs: inputs(),
      setup: (g) => (storage(g).findingsResult = [sampleFinding({ id: 7, resolution: null, resolvedAt: null })]),
    });
    const list = root.querySelector('quader-storage-findings')!;
    await choose(list, fixture, 'Acknowledge');
    const dialog = document.querySelector<HTMLElement>('quader-storage-acknowledge-dialog')!;
    expect(text(dialog)).toContain('Acknowledge the damaged copy?');
    expect(text(dialog)).toContain('It repairs nothing');
    storage(get).acknowledgeResult = new ApiError(409, { code: 'finding_resolved', detail: 'The finding is resolved.' }, null);
    type(dialog.querySelector('textarea')!, 'The bucket lifecycle rule removed it.');
    button(dialog, 'Acknowledge').click();
    await settle(fixture);
    expect(text(dialog)).toContain('The finding is resolved.');
    storage(get).acknowledgeResult = null;
    button(dialog, 'Acknowledge').click();
    await settle(fixture);
    expect(storage(get).acknowledgements.at(-1)).toEqual({ id: 7, note: 'The bucket lifecycle rule removed it.' });
    expect(document.querySelector('quader-storage-acknowledge-dialog')).toBeNull();
    expect(text(list)).toContain('Acknowledged');
    expect(text(list)).toContain('The bucket lifecycle rule removed it.');
    // Acknowledged is not resolved: its acts stay.
    expect(list.querySelector('td.actions button')).toBeTruthy();
  });

  it('R-UI-265 Check again reads the copies of a finding\'s blob anew', async () => {
    const { root, fixture, get } = await renderScreen(IntegritySection, {
      inputs: inputs(),
      setup: (g) => (storage(g).findingsResult = [sampleFinding({ id: 9, resolution: null, resolvedAt: null })]),
    });
    await choose(root.querySelector('quader-storage-findings')!, fixture, 'Check again');
    expect(storage(get).rechecks).toEqual([sampleFinding().sha256]);
  });
});
