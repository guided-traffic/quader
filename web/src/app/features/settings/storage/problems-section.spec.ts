import { describe, expect, it } from 'vitest';
import { Router } from '@angular/router';
import { StorageApi } from '../../../api/storage-api';
import { FakeStorageApi, sampleConnection, sampleHistory, sampleOverview, sampleProblem } from '../../../api/testing/fakes-storage';
import { button, expectAccessible, renderScreen, settle, text } from '../../../testing/screen';
import { ProblemsSection } from './problems-section';
import { StoragePage } from './storage-page';

/**
 * The section Problems and the period of Settings › Storage (api-and-ui.md R-UI-262, R-UI-265; storage.md R-STOR-111): the strips per
 * connection, the problems with their words, the empty sentence, the period in the address and the tile Problems of the period.
 */

type Get = <A>(token: abstract new (...args: never[]) => A) => A;
const storage = (get: Get) => get(StorageApi) as FakeStorageApi;

describe('The section Problems (R-UI-265)', () => {
  it('R-UI-265 a quiet period: a strip per connection with no tone, and "No problem in this period."', async () => {
    const { root } = await renderScreen(ProblemsSection, { inputs: { period: '30d', history: sampleHistory('30d', ['default', 'archive']) } });
    expect(root.querySelectorAll('quader-storage-timeline').length).toBe(2);
    expect(root.querySelectorAll('button.cell[data-tone="none"]').length).toBe(60);
    expect(text(root)).toContain('No problem in this period.');
    await expectAccessible(root);
  });

  it('R-UI-265 an outage and repaired copies: each with when, what, how long or how much, and whether it lasts', async () => {
    const { root, get } = await renderScreen(ProblemsSection, {
      inputs: { period: '30d', history: sampleHistory('30d', ['default', 'archive'], { archive: { 26: { tone: 'warning', unreachableSeconds: 4_320, replicas: 3 } } }) },
      setup: (g) =>
        (storage(g).problemsResult = [
          sampleProblem({ kind: 'copies_damaged', connection: 'default', startedAt: '2026-11-21T03:12:09Z', endedAt: '2026-11-21T03:12:10Z', seconds: 1, count: 3, bytes: 3_250_585, resolved: 3, codes: ['digest'], sources: ['scrub'], replicas: 0, occurrences: 0 }),
          sampleProblem({ connection: 'archive' }),
          sampleProblem({ kind: 'uploads_refused', connection: 'default', startedAt: '2026-11-22T10:00:00Z', endedAt: null, tone: 'error', count: 2, codes: ['timeout'] }),
        ]),
    });
    expect(storage(get).problemQueries.at(-1)).toEqual({ period: '30d' });
    const rows = [...root.querySelectorAll<HTMLTableRowElement>('quader-data-table tbody tr')].map(text);
    expect(rows[0]).toContain('Copies damaged');
    expect(rows[0]).toContain('Scrub, Digest');
    expect(rows[0]).toContain('3 copies (3.1 MiB)');
    expect(rows[0]).toContain('Ended');
    expect(rows[1]).toContain('Unreachable');
    expect(rows[1]).toContain('timeout');
    expect(rows[1]).toContain('on 3 replicas');
    expect(rows[1]).toContain('1 h 12 min');
    expect(rows[2]).toContain('Uploads refused');
    expect(rows[2]).toContain('2 uploads');
    expect(rows[2]).toContain('Ongoing');
    expect(root.querySelector('button.cell[data-tone="warning"]')!.getAttribute('aria-label')).toContain('unreachable 1 h 12 min on 3 replicas');
  });
});

describe('The period of Settings › Storage (R-UI-262)', () => {
  it('R-UI-262 the period is a group in the header, 30 d by default; another one goes into the address and every read of the period asks anew', async () => {
    const { root, fixture, get } = await renderScreen(StoragePage);
    const group = root.querySelector<HTMLElement>('.period')!;
    expect([...group.querySelectorAll('button')].map(text)).toEqual(['24 h', '7 d', '30 d', '90 d', '1 year']);
    expect(text(group.querySelector('[aria-pressed="true"]')!)).toBe('30 d');
    expect(storage(get).overviewPeriods.at(-1)).toBe('30d');
    expect(storage(get).historyReads.at(-1)).toBe('30d');
    button(group, '7 d').click();
    await settle(fixture);
    expect(get(Router).url).toContain('period=7d');
    fixture.componentRef.setInput('period', '7d');
    await settle(fixture);
    expect(storage(get).overviewPeriods.at(-1)).toBe('7d');
    expect(storage(get).historyReads.at(-1)).toBe('7d');
    expect(storage(get).problemQueries.at(-1)).toEqual({ period: '7d' });
    expect(storage(get).findingQueries.at(-1)).toEqual({ period: '7d', state: 'all' });
    expect(text(group.querySelector('[aria-pressed="true"]')!)).toBe('7 d');
  });

  it('R-UI-262 the tile Problems is the period\'s: resolved with its count and the worst, a link to the section, and so is Integrity', async () => {
    const { root } = await renderScreen(StoragePage, {
      setup: (get) => {
        storage(get).connectionsResult = [sampleConnection()];
        storage(get).overviewResult = sampleOverview({
          problems: {
            period: '30d',
            state: 'resolved',
            count: 3,
            open: 0,
            acknowledged: 1,
            worst: { kind: 'unreachable', connection: 'archive', startedAt: '2026-11-19T01:10:00Z', seconds: 4_320, tone: 'warning' },
          },
        });
      },
    });
    const tile = text(root.querySelector('[data-tile="problems"]')!);
    expect(tile).toContain('Problems · 30 d');
    expect(tile).toContain('Resolved');
    expect(tile).toContain('3 resolved');
    expect(tile).toContain('1 acknowledged');
    expect(tile).toContain('Worst: archive unreachable 1 h 12 min');
    expect(root.querySelector('[data-tile="problems"] [data-tone]')?.getAttribute('data-tone')).toBe('neutral');
    expect(root.querySelector('[data-tile="problems"] a')?.getAttribute('href')).toBe('/admin/settings/storage#problems');
    expect(root.querySelector('[data-tile="integrity"] a')?.getAttribute('href')).toBe('/admin/settings/storage#integrity');
  });
});
