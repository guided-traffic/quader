import { ScannerList } from '../../api/system-api';
import { sampleScannerReplica, sampleScanners, withUpdater } from '../../api/testing/fakes';
import { expectAccessible, renderScreen, text } from '../../testing/screen';
import { databaseLag, podDatabase, scannerGroups, waitingScanners } from './scanner-model';
import { ScannerReplicas } from './scanner-replicas';

/** A badge's word without its icon's glyph name. */
const word = (badge: HTMLElement): string => text(badge).replace(/^(check_circle|hourglass_empty)/, '');

const longName = 'quader-malware-scanner-clamav-6d9c7b5f4-x2v4q-with-a-release-name-of-forty-characters';
/** The blob a ClamAV replica's slot checks now. */
const CALL = '9e11c0ffee'.padEnd(64, '0');

/** Two ClamAV replicas, one of them waiting for its first database with a long pod name, and one image scanner that was lost. */
function list(): ScannerList {
  return sampleScanners({
    items: [
      sampleScannerReplica({ lost: true }),
      sampleScannerReplica({
        kind: 'clamav', instanceUid: 'c1', name: 'quader-malware-scanner-clamav-6d9c7b5f4-x2v4q', toolVersion: '1.5.5',
        slots: { inUse: 1, total: 2 }, database: { version: 'main-62.daily-27841.bytecode-339', builtAt: '2026-11-21T04:00:00Z' },
        calls: [{ sha256: CALL, since: '2026-11-21T08:59:40Z' }],
      }),
      sampleScannerReplica({
        kind: 'clamav', instanceUid: 'c2', name: longName, state: 'starting', reason: 'database', toolVersion: null,
        slots: { inUse: 0, total: 2 }, database: null, calls: [],
      }),
    ],
  });
}

async function render(inputs: Record<string, unknown>) {
  return renderScreen(ScannerReplicas, { inputs });
}

describe('The scanner replicas (R-OPS-048)', () => {
  it('R-UI-038 the System page shows each replica with its node, version, state and why, since when, heartbeat and tool, and passes axe', async () => {
    const { root } = await render({ list: list(), kind: 'clamav', view: 'system' });
    const headers = [...root.querySelectorAll('thead th .th-inner > span:first-child')].map((s) => text(s));
    expect(headers.slice(0, 7)).toEqual(['Name', 'Node', 'Version', 'State', 'State changed', 'Heartbeat', 'Tool']);
    const rows = [...root.querySelectorAll('tbody tr')];
    expect(rows).toHaveLength(2);
    expect(text(rows[0]!)).toContain('ClamAV 1.5.5');
    expect(text(rows[0]!)).toContain('Ready');
    // The replica that is not ready is shown, with the reason in words, not hidden (R-OPS-048).
    expect(text(rows[1]!)).toContain('Starting');
    expect(text(rows[1]!)).toContain('No database loaded yet');
    expect(rows[1]!.querySelector('th[scope="row"]')!.classList).toContain('wrap-name');
    expect(text(rows[1]!.querySelector('th')!)).toBe(longName);
    // No fold on the System page: it is the detail view.
    expect(root.querySelector('quader-disclosure')).toBeNull();
    await expectAccessible(root);
  });

  it('R-MAL-023 a settings page folds the replicas closed, with the ClamAV version, the database and its date, and the slots', async () => {
    const { root } = await render({ list: list(), kind: 'clamav' });
    const fold = root.querySelector('quader-disclosure')!;
    expect(text(fold.querySelector('button')!)).toContain('Replicas of malware-scanner-clamav (2)');
    expect(fold.querySelector('button')!.getAttribute('aria-expanded')).toBe('false');
    const headers = [...root.querySelectorAll('thead th .th-inner > span:first-child')].map((s) => text(s));
    expect(headers).toEqual(['Name', 'State', 'Tool', 'Signature database', 'Slots', 'Working on']);
    const rows = [...root.querySelectorAll('tbody tr')].map((tr) => text(tr));
    expect(rows[0]).toContain('main-62.daily-27841.bytecode-339');
    expect(rows[0]).toContain('21.11.2026 (Sat.)');
    expect(rows[0]).toContain('1 of 2');
    expect(rows[1]).toContain('0 of 2');
    await expectAccessible(root);
  });

  it('R-MAL-041 a malware scanner says what it works on by its own report: the file where the page knows it, else the digest', async () => {
    const bare = await render({ list: list(), kind: 'clamav' });
    const cells = [...bare.root.querySelectorAll('[data-testid="scanner-calls"]')];
    expect(text(cells[0]!)).toContain('sha256:9e11c0ffee00');
    expect(cells[0]!.querySelector('code')!.getAttribute('title')).toBe(`sha256:${CALL}`);
    expect(cells[0]!.querySelector('time')!.getAttribute('title')).toMatch(/^21\.11\.2026 \d\d:59:40 \(Sat\.\)$/);
    // A replica whose slots hold nothing has no call to name.
    expect(text(cells[1]!)).not.toContain('sha256');
    const named = await render({
      list: list(),
      kind: 'clamav',
      callItems: new Map([[CALL, { repository: 'acme/assets', path: 'builds/agent-setup.msi' }]]),
    });
    expect(text(named.root.querySelector('[data-testid="scanner-calls"]')!)).toContain('builds/agent-setup.msi');
    // The updater runs no calls, and an image scanner reports none: neither has the column.
    const trivy = await render({ list: list(), kind: 'trivy' });
    expect(trivy.root.querySelector('[data-testid="scanner-calls"]')).toBeNull();
  });

  it('R-OPS-048 the image scanner has the column of the database its pod holds, and a lost replica says so', async () => {
    const { root } = await render({ list: list(), kind: 'trivy' });
    const headers = [...root.querySelectorAll('thead th .th-inner > span:first-child')].map((s) => text(s));
    expect(headers).toEqual(['Name', 'State', 'Tool', 'Database', 'Slots']);
    expect(text(root.querySelector('tbody tr')!)).toContain('Lost');
    expect(text(root.querySelector('tbody tr')!)).toContain('Trivy 0.67.2');
    expect(text(root.querySelector('tbody tr')!)).toContain('No database yet');
  });

  it('R-VULN-049 each image scanner says whether its pod holds the database in use with the age of its build, an older one, or none yet, on the System page too', async () => {
    const inUse = 'a1b2c3d4e5f6'.padEnd(64, '0');
    const older = '9f8e7d6c5b4a'.padEnd(64, '1');
    const pods = sampleScanners({
      items: [
        sampleScannerReplica({ instanceUid: 't1', name: 'trivy-a', database: { version: inUse, builtAt: '2026-11-21T07:02:00Z' } }),
        sampleScannerReplica({ instanceUid: 't2', name: 'trivy-b', database: { version: older, builtAt: null } }),
        sampleScannerReplica({ instanceUid: 't3', name: 'trivy-c', state: 'starting', reason: 'starting', toolVersion: null, database: null }),
        sampleScannerReplica({ kind: 'clamav', instanceUid: 'c1', name: 'clamav-a', toolVersion: '1.5.5', database: { version: 'main-62.daily-27841.bytecode-339', builtAt: '2026-11-21T04:00:00Z' } }),
      ],
    });
    for (const view of ['system', 'settings'] as const) {
      const { root } = await render({ list: pods, kind: 'trivy', view });
      const headers = [...root.querySelectorAll('thead th .th-inner > span:first-child')].map((s) => text(s));
      // The settings' column Database; on the System page the line under the tool, so that its table keeps its columns.
      expect(headers.includes('Database')).toBe(view === 'settings');
      const cells = [...root.querySelectorAll(view === 'settings' ? 'tbody td.database-cell' : 'tbody td.tool-cell')];
      expect(cells.map((c) => word(c.querySelector<HTMLElement>('.database-state .ok, .database-state .badge')!))).toEqual([
        'Database in use',
        'Older database',
        'No database yet',
      ]);
      if (view === 'system') expect(text(cells[0]!)).toContain('Trivy 0.67.2');
      // The state is a badge with its icon and its word, green only for the database in use (never by colour alone).
      expect(cells[0]!.querySelector('.ok .material-icons')).not.toBeNull();
      expect(cells[1]!.querySelector('.ok')).toBeNull();
      // The age of the build beside the badge of the database in use only; the digest is the cell's title, not its text.
      expect(text(cells[0]!)).toContain('built');
      expect(cells[0]!.querySelector('time')!.getAttribute('datetime')).toBe('2026-11-21T07:02:00Z');
      expect(cells[0]!.querySelector('.database-state')!.getAttribute('title')).toBe(`sha256:${inUse}`);
      expect(text(cells[0]!)).not.toContain('a1b2c3d4e5f6');
      expect(text(cells[1]!)).not.toContain('built');
      expect(cells[1]!.querySelector('.database-state')!.getAttribute('title')).toBe(`sha256:${older}`);
      expect(cells[2]!.querySelector('.database-state')!.hasAttribute('title')).toBe(false);
      await expectAccessible(root);
    }
    // ClamAV keeps its own column on the settings pages and none on the System page.
    const clamSystem = await render({ list: pods, kind: 'clamav', view: 'system' });
    expect(clamSystem.root.querySelector('.database-state')).toBeNull();
    const clamSettings = await render({ list: pods, kind: 'clamav' });
    expect([...clamSettings.root.querySelectorAll('thead th .th-inner > span:first-child')].map((s) => text(s))).toContain('Signature database');
    expect(clamSettings.root.querySelector('td.database-cell')).toBeNull();
  });

  it('R-OPS-048 the installation counts its ready image scanners that do not hold the database in use; lost and starting ones do not count', () => {
    const current = { version: 'a'.repeat(64), builtAt: '2026-11-21T07:02:00Z' };
    const lag = (items: Parameters<typeof sampleScanners>[0]) => databaseLag(scannerGroups(sampleScanners(items)).find((g) => g.kind === 'trivy')!);
    expect(lag({ items: [sampleScannerReplica({ database: current }), sampleScannerReplica({ instanceUid: 't2', database: current })] })).toEqual({ ready: 2, lagging: 0 });
    expect(
      lag({
        items: [
          sampleScannerReplica({ database: current }),
          sampleScannerReplica({ instanceUid: 't2', database: { version: 'b'.repeat(64), builtAt: null } }),
          sampleScannerReplica({ instanceUid: 't3', database: null }),
          sampleScannerReplica({ instanceUid: 't4', state: 'starting', database: null }),
          sampleScannerReplica({ instanceUid: 't5', lost: true, database: null }),
        ],
      }),
    ).toEqual({ ready: 3, lagging: 2 });
    expect(podDatabase(sampleScannerReplica({ database: current }))).toBe('current');
    // ClamAV's rows name the database they scan with; the installation's statement is the image scanner's alone.
    expect(databaseLag(scannerGroups(sampleScanners()).find((g) => g.kind === 'clamav')!)).toBeNull();
  });

  it('R-OPS-048 a kind without a replica says why, in a sentence, for each way it can have none', async () => {
    for (const [replicas, words] of [
      ['reporting', 'No replica of malware-scanner-clamav has reported itself'],
      ['unverified', 'the scan listener cannot tell their kind'],
      ['none', 'no ClamAV scanner is configured (malware.scanner.url is empty)'],
    ] as const) {
      const empty = sampleScanners({ items: [] });
      empty.kinds = empty.kinds.map((k) => (k.kind === 'clamav' ? { ...k, replicas } : k));
      const { root } = await render({ list: empty, kind: 'clamav' });
      expect(root.querySelector('table')).toBeNull();
      expect(text(root.querySelector('[data-testid="scanners-none-clamav"]')!)).toContain(words);
    }
    const all = sampleScanners({ items: [] });
    all.kinds = all.kinds.map((k) => (k.kind === 'trivy' ? { ...k, replicas: 'none' } : k));
    const { root } = await render({ list: all, kind: 'trivy', view: 'system' });
    expect(text(root.querySelector('[data-testid="scanners-none-trivy"]')!)).toContain('vulnerabilities.images.scanner.url is empty');
    await expectAccessible(root);
  });

  it('H-1161 what a scanner reports is bound as text', async () => {
    const hostile = sampleScanners({ items: [sampleScannerReplica({ name: '<img src=x onerror=alert(1)>', reason: '<b>x</b>' })] });
    const { root } = await render({ list: hostile, kind: 'trivy', view: 'system' });
    expect(root.querySelector('img')).toBeNull();
    expect(root.querySelector('tbody b')).toBeNull();
    expect(text(root.querySelector('tbody th')!)).toBe('<img src=x onerror=alert(1)>');
  });

  it('R-OPS-048 the groups count the live and the ready replicas, and a reporting kind with none ready waits', () => {
    const groups = scannerGroups(list());
    expect(groups.map((g) => `${g.deployment} ${g.live}/${g.ready} ${g.inUse}/${g.slots}`)).toEqual([
      'vuln-scanner-trivy 0/0 0/0',
      'malware-scanner-clamav 2/1 1/4',
    ]);
    expect(waitingScanners(groups)).toEqual(['vuln-scanner-trivy']);
    // A kind only a row names (its scanning was switched off) keeps its group, named by the rule of its deployment.
    const orphan = scannerGroups({ kinds: [], items: [sampleScannerReplica()] });
    expect(orphan.map((g) => [g.deployment, g.replicas])).toEqual([['vuln-scanner-trivy', null]]);
    expect(waitingScanners(orphan)).toEqual([]);
  });

  it('R-UI-188 the updater has no slots: its rows show its state, freshclam\'s version and the set it holds with the age of its daily', async () => {
    const { root } = await render({ list: withUpdater(sampleScanners(), { state: 'starting', reason: 'seed' }), kind: 'freshclam', view: 'open' });
    expect(root.querySelector('quader-disclosure')).toBeNull();
    const heads = [...root.querySelectorAll('thead th')].map((th) => text(th).replace(/info$/, '').trim());
    expect(heads).toEqual(['Name', 'State', 'Tool', 'Definitions it holds']);
    const row = text(root.querySelector('tbody tr')!);
    expect(row).toContain('freshclam 1.5.5');
    expect(row).toContain('main-63.daily-28190.bytecode-339');
    expect(row).toContain('daily built');
    expect(row).toContain('Seed not finished yet');
    expect(row).not.toContain('of 0');
    await expectAccessible(root);
  });
});
