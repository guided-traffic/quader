import { PackagesApi } from '../../../api/packages-api';
import { CLAMAV_NOW, clamavFixture } from '../../../api/testing/fakes-clamav';
import { FakePackagesApi } from '../../../api/testing/fakes-packages';
import { expectAccessible, renderScreen, text } from '../../../testing/screen';
import { ClamavDatabases } from './clamav-databases';

describe('The databases of the ClamAV definitions (R-UI-194)', () => {
  it('R-UI-194 Q-UI-912 lists bytecode, daily and main with the version in use and its age, no Outdated on main or bytecode, and names the set in use and the one before it', async () => {
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

  it('R-UI-194 Q-UI-912 a daily built more than 7 days ago carries Outdated, with the reason as its note and the badge\'s tooltip', async () => {
    const { root } = await renderScreen(ClamavDatabases, {
      inputs: { repository: 'upstream/clamav', now: CLAMAV_NOW },
      setup: (get) => clamavFixture(get(PackagesApi) as FakePackagesApi, { dailyDays: 8 }),
    });
    const daily = root.querySelector('tbody tr:nth-child(2)')!;
    expect(text(daily.querySelector('th')!)).toContain('ClamAV warns about a database older than 7 days.');
    expect(daily.querySelector('.badge.warn')!.getAttribute('title')).toBe('ClamAV warns about a database older than 7 days.');
    expect(root.querySelectorAll('.badge.warn')).toHaveLength(1);
  });
});
