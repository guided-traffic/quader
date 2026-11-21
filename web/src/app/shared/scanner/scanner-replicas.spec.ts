import { ScannerList } from '../../api/system-api';
import { sampleScannerReplica, sampleScanners, withUpdater } from '../../api/testing/fakes';
import { expectAccessible, renderScreen, text } from '../../testing/screen';
import { scannerGroups, waitingScanners } from './scanner-model';
import { ScannerReplicas } from './scanner-replicas';

const longName = 'quader-malware-scanner-clamav-6d9c7b5f4-x2v4q-with-a-release-name-of-forty-characters';

/** Two ClamAV replicas, one of them waiting for its first database with a long pod name, and one image scanner that was lost. */
function list(): ScannerList {
  return sampleScanners({
    items: [
      sampleScannerReplica({ lost: true }),
      sampleScannerReplica({
        kind: 'clamav', instanceUid: 'c1', name: 'quader-malware-scanner-clamav-6d9c7b5f4-x2v4q', toolVersion: '1.5.5',
        slots: { inUse: 1, total: 2 }, database: { version: 'main-62.daily-27841.bytecode-339', builtAt: '2026-11-21T04:00:00Z' },
      }),
      sampleScannerReplica({
        kind: 'clamav', instanceUid: 'c2', name: longName, state: 'starting', reason: 'database', toolVersion: null,
        slots: { inUse: 0, total: 2 }, database: null,
      }),
    ],
  });
}

async function render(inputs: Record<string, unknown>) {
  return renderScreen(ScannerReplicas, { inputs });
}

describe('The scanner replicas (R-OPS-048, Q-UI-897)', () => {
  it('Q-UI-897 the System page shows each replica with its node, version, state and why, since when, heartbeat and tool, and passes axe', async () => {
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

  it('Q-UI-897 a settings page folds the replicas closed, with the ClamAV version, the database and its date, and the slots', async () => {
    const { root } = await render({ list: list(), kind: 'clamav' });
    const fold = root.querySelector('quader-disclosure')!;
    expect(text(fold.querySelector('button')!)).toContain('Replicas of malware-scanner-clamav (2)');
    expect(fold.querySelector('button')!.getAttribute('aria-expanded')).toBe('false');
    const headers = [...root.querySelectorAll('thead th .th-inner > span:first-child')].map((s) => text(s));
    expect(headers).toEqual(['Name', 'State', 'Tool', 'Signature database', 'Slots']);
    const rows = [...root.querySelectorAll('tbody tr')].map((tr) => text(tr));
    expect(rows[0]).toContain('main-62.daily-27841.bytecode-339');
    expect(rows[0]).toContain('Nov 21, 2026');
    expect(rows[0]).toContain('1 of 2');
    expect(rows[1]).toContain('0 of 2');
    await expectAccessible(root);
  });

  it('Q-UI-897 the image scanner has no database column, and a lost replica says so', async () => {
    const { root } = await render({ list: list(), kind: 'trivy' });
    const headers = [...root.querySelectorAll('thead th .th-inner > span:first-child')].map((s) => text(s));
    expect(headers).toEqual(['Name', 'State', 'Tool', 'Slots']);
    expect(text(root.querySelector('tbody tr')!)).toContain('Lost');
    expect(text(root.querySelector('tbody tr')!)).toContain('Trivy 0.67.2');
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
    all.kinds = all.kinds.map((k) => (k.kind === 'trivy' ? { ...k, replicas: 'inProcess' } : k));
    const { root } = await render({ list: all, kind: 'trivy', view: 'system' });
    expect(text(root.querySelector('[data-testid="scanners-none-trivy"]')!)).toContain('the development role all analyses images in its own process');
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

  it('R-UI-188 Q-UI-913 the updater has no slots: its rows show its state, freshclam\'s version and the set it holds with the age of its daily', async () => {
    const { root } = await render({ list: withUpdater(sampleScanners(), { state: 'starting', reason: 'seed' }), kind: 'freshclam', view: 'open' });
    expect(root.querySelector('quader-disclosure')).toBeNull();
    const heads = [...root.querySelectorAll('thead th')].map((th) => text(th).replace('help_outline', '').trim());
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
