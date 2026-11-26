import { describe, expect, it } from 'vitest';
import { StorageApi } from '../../../api/storage-api';
import { FakeStorageApi, sampleOverview } from '../../../api/testing/fakes-storage';
import { renderScreen, text } from '../../../testing/screen';
import { Dashboard } from '../../system/dashboard';
import { StorageSummary } from './storage-summary';

/** The summary's tiles (R-UI-262) and their compact form on the dashboard (R-UI-266). */
describe('The summary of the stored content (R-UI-262, R-UI-266)', () => {
  const words = (root: HTMLElement) =>
    [...root.querySelectorAll<HTMLElement>('.tile')].map((t) => [t.getAttribute('data-tile'), text(t.querySelector('.state') ?? t.querySelector('.figure')!)]);

  it('R-UI-266 the dashboard reads the one overview, no list of connections, and words each state as the page does', async () => {
    const page = await renderScreen(StorageSummary, { inputs: { overview: sampleOverview() } });
    const card = await renderScreen(Dashboard);
    const fake = card.get(StorageApi) as FakeStorageApi;
    expect(fake.overviewReads).toBe(1);
    expect(fake.connectionReads).toBe(0);
    const dashboard = card.root.querySelector<HTMLElement>('quader-storage-summary')!;
    expect(words(dashboard)).toEqual(words(page.root));
    // The compact form keeps one figure per tile.
    expect(page.root.querySelectorAll('.figure').length).toBeGreaterThan(dashboard.querySelectorAll('.figure').length);
    expect(dashboard.querySelectorAll('.tile').length).toBe(6);
  });
});
