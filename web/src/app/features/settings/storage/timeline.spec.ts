import { describe, expect, it } from 'vitest';
import { PERIOD_CELLS, sampleHistory } from '../../../api/testing/fakes-storage';
import { STORAGE_PERIODS } from '../../../api/storage-api';
import { expectAccessible, renderScreen, settle, text } from '../../../testing/screen';
import { cellSummary, durationText, stripSummary } from './storage-history';
import { Timeline } from './timeline';

/**
 * A connection's strip of the period (api-and-ui.md R-UI-265, Q-UI-1600, Q-UI-1601): its cells for each period, their tones as the server
 * gave them, each cell a button named by its summary, a click that names it under the strip, and the compact form of tone only.
 */
describe('The strip of a period (R-UI-265)', () => {
  it('R-UI-265 each period has its cells: 24, 28, 30, 30 and 52', async () => {
    for (const period of STORAGE_PERIODS) {
      const h = sampleHistory(period);
      const { root } = await renderScreen(Timeline, {
        inputs: { buckets: h.connections[0]!.buckets, bucketSeconds: h.bucketSeconds, label: 'Cells of default' },
      });
      expect(root.querySelectorAll('button.cell').length).toBe(PERIOD_CELLS[period].cells);
    }
  });

  it('R-UI-265 a cell carries the server\'s tone and its words; a click names it under the strip; the strip has its summary', async () => {
    const h = sampleHistory('24h', ['archive'], {
      archive: {
        3: { tone: 'warning', unreachableSeconds: 4_320, replicas: 3, readFallovers: 310 },
        10: { tone: 'error', uploadsRefused: 2 },
      },
    });
    const { root, fixture } = await renderScreen(Timeline, {
      inputs: { buckets: h.connections[0]!.buckets, bucketSeconds: h.bucketSeconds, label: 'Cells of archive' },
    });
    const cells = [...root.querySelectorAll<HTMLButtonElement>('button.cell')];
    expect(cells[3]!.dataset['tone']).toBe('warning');
    expect(cells[10]!.dataset['tone']).toBe('error');
    expect(cells[0]!.dataset['tone']).toBe('none');
    expect(cells[3]!.getAttribute('aria-label')).toContain('unreachable 1 h 12 min on 3 replicas, 310 reads from another copy');
    expect(cells[0]!.getAttribute('aria-label')).toContain(': nothing');
    cells[3]!.click();
    await settle(fixture);
    expect(text(root.querySelector('.picked')!)).toContain('unreachable 1 h 12 min on 3 replicas');
    expect(cells[3]!.getAttribute('aria-pressed')).toBe('true');
    expect(text(root.querySelector('.visually-hidden')!)).toBe('24 cells: 1 with clients refused, 1 absorbed by Quader.');
    await expectAccessible(root);
  });

  it('R-UI-265 the compact strip is one image named by its summary, with no button', async () => {
    const h = sampleHistory('30d');
    const { root } = await renderScreen(Timeline, {
      inputs: { buckets: h.connections[0]!.buckets, bucketSeconds: h.bucketSeconds, label: 'Cells of default', compact: true },
    });
    expect(root.querySelector('button')).toBeNull();
    expect(root.querySelector('[role="img"]')!.getAttribute('aria-label')).toBe('Cells of default: Nothing happened in this period.');
    expect(root.querySelectorAll('.cell').length).toBe(30);
  });

  it('R-UI-265 the words: a length in its two largest units, a cell of a day by its date', () => {
    expect([durationText(30), durationText(240), durationText(4_320), durationText(3_600), durationText(97_200)]).toEqual([
      'less than a minute',
      '4 min',
      '1 h 12 min',
      '1 h',
      '1 d 3 h',
    ]);
    const day = sampleHistory('30d', ['default'], { default: { 0: { damagedCopies: 3, damagedBytes: 3 * 1024 ** 2 } } });
    expect(cellSummary(day.connections[0]!.buckets[0]!, day.bucketSeconds)).toMatch(/^\d\d\.\d\d\.2026 \(\w+\.\): 3 copies damaged \(3\.0 MiB\)$/);
    expect(stripSummary(day.connections[0]!.buckets)).toBe('Nothing happened in this period.');
  });
});
