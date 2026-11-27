import { PackagesApi } from '../../api/packages-api';
import { CLAMAV_NOW, clamavFixture } from '../../api/testing/fakes-clamav';
import { FakePackagesApi } from '../../api/testing/fakes-packages';
import { expectAccessible, renderScreen, settle, text } from '../../testing/screen';
import { vi } from 'vitest';
import { ClamavDatabases } from './clamav-databases';

describe('The databases of the ClamAV definitions (R-UI-194)', () => {
  it('R-UI-194 lists bytecode, daily and main with the version in use and its age, no Outdated on main or bytecode, and names the set in use and the one before it', async () => {
    const { root } = await renderScreen(ClamavDatabases, {
      inputs: { repository: 'upstream/clamav', now: CLAMAV_NOW },
      setup: (get) => clamavFixture(get(PackagesApi) as FakePackagesApi),
    });
    const rows = [...root.querySelectorAll('tbody tr')].map((tr) => [...tr.querySelectorAll('th, td')].map((c) => text(c)).join(' | '));
    expect([...root.querySelectorAll('tbody th a')].map((a) => text(a))).toEqual(['bytecode', 'daily', 'main']);
    expect(rows[1]).toContain('28190');
    expect(rows[1]).toContain('under a day');
    expect(rows[1]).not.toContain('Outdated');
    // main at 8 days and bytecode at 6: their age, no badge and no sentence (Q-UI-912).
    expect(rows[0]).toContain('6 days');
    expect(rows[2]).toContain('8 days');
    expect(rows.join(' ')).not.toContain('Outdated');
    expect(rows.join(' ')).not.toContain('ClamAV warns');
    expect([...root.querySelectorAll('.sets > span')].map((x) => text(x))).toEqual([
      'Set in use: main-63.daily-28190.bytecode-339',
      'The set before it: main-63.daily-28189.bytecode-339',
    ]);
    expect(root.querySelector('tbody tr:nth-child(2) th a')!.getAttribute('href')).toBe('/projects/upstream/packages/clamav/daily');
    await expectAccessible(root);
  });

  it('R-UI-194 a daily built more than 7 days ago carries Outdated, with the reason as its note and the badge\'s tooltip', async () => {
    const { root } = await renderScreen(ClamavDatabases, {
      inputs: { repository: 'upstream/clamav', now: CLAMAV_NOW },
      setup: (get) => clamavFixture(get(PackagesApi) as FakePackagesApi, { dailyDays: 8 }),
    });
    const daily = root.querySelector('tbody tr:nth-child(2)')!;
    expect(text(daily.querySelector('th')!)).toContain('ClamAV warns about a database older than 7 days.');
    expect(daily.querySelector('.badge.warn')!.getAttribute('title')).toBe('ClamAV warns about a database older than 7 days.');
    expect(root.querySelectorAll('.badge.warn')).toHaveLength(1);
  });

  it('R-UI-213 the list of databases is read every minute, and the versions and the set in use only when it changed', async () => {
    vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] });
    try {
      const { root, fixture, get } = await renderScreen(ClamavDatabases, {
        inputs: { repository: 'upstream/clamav', now: CLAMAV_NOW },
        setup: (get) => clamavFixture(get(PackagesApi) as FakePackagesApi),
      });
      const fake = get(PackagesApi) as FakePackagesApi;
      const versionReads = () => fake.versionFilters.length;
      const lists = fake.listFilters.length;
      const before = versionReads();
      vi.advanceTimersByTime(60_000);
      await settle(fixture);
      expect(fake.listFilters.length).toBe(lists + 1);
      expect(versionReads()).toBe(before);
      // The updater stored a new daily: the list says so, and the set in use is read again.
      fake.packages = fake.packages.map((p) => (p.name === 'daily' ? { ...p, versionCount: p.versionCount + 1, updatedAt: '2026-11-24T10:00:00Z' } : p));
      vi.advanceTimersByTime(60_000);
      await settle(fixture);
      expect(versionReads()).toBe(before + 3);
      expect(root.querySelectorAll('tbody tr')).toHaveLength(3);
    } finally {
      vi.useRealTimers();
    }
  });
});
