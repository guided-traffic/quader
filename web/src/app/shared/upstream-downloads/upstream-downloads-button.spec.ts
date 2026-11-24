import { vi } from 'vitest';
import { UpstreamDownloadsApi } from '../../api/upstream-downloads-api';
import { FakeUpstreamDownloadsApi } from '../../api/testing/fakes-upstream-downloads';
import { renderScreen, settle } from '../../testing/screen';
import { DOWNLOADS_CAPABILITY, SUMMARY_POLL_MS, UpstreamDownloadsButton } from './upstream-downloads-button';

/** The button as a caller with `capabilities` sees it, the fake answering `running` as the number of running downloads. */
async function open(options: { capabilities?: readonly string[]; running?: number; failWith?: number } = {}) {
  const r = await renderScreen(UpstreamDownloadsButton, {
    capabilities: options.capabilities ?? [DOWNLOADS_CAPABILITY],
    setup: (get) => {
      const fake = get(UpstreamDownloadsApi) as FakeUpstreamDownloadsApi;
      fake.runningCount = options.running ?? 0;
      fake.failWith = options.failWith ?? null;
    },
  });
  return { ...r, fake: r.get(UpstreamDownloadsApi) as FakeUpstreamDownloadsApi };
}

const theButton = (root: HTMLElement): HTMLButtonElement | null => root.querySelector('button.downloads-button');

function setVisibility(state: 'visible' | 'hidden'): void {
  Object.defineProperty(document, 'visibilityState', { configurable: true, get: () => state });
  document.dispatchEvent(new Event('visibilitychange'));
}

describe('The Downloads button of the top bar', () => {
  afterEach(() => {
    setVisibility('visible');
  });

  it('R-UI-210 a caller without content:metadata-read gets no button, and nothing is asked', async () => {
    const { root, fake } = await open({ capabilities: ['settings:read'] });
    expect(theButton(root)).toBeNull();
    expect(root.querySelector('quader-upstream-downloads-panel')).toBeNull();
    expect(fake.summaryCalls).toBe(0);
  });

  it('R-UI-210 with nothing running there is no badge and the name is Downloads; three running show 3 and say so', async () => {
    const idle = await open({ running: 0 });
    const b = theButton(idle.root)!;
    expect(b.getAttribute('aria-label')).toBe('Downloads');
    expect(b.getAttribute('title')).toBe('Downloads');
    expect(b.getAttribute('aria-haspopup')).toBe('dialog');
    expect(b.getAttribute('aria-expanded')).toBe('false');
    expect(b.querySelector('.downloads-count')).toBeNull();

    const busy = await open({ running: 3 });
    const c = theButton(busy.root)!;
    expect(c.querySelector('.downloads-count')?.textContent?.trim()).toBe('3');
    expect(c.getAttribute('aria-label')).toBe('Downloads, 3 running');
  });

  it('R-UI-210 above 99 the badge reads 99+ and the name keeps the number', async () => {
    const { root } = await open({ running: 140 });
    expect(theButton(root)!.querySelector('.downloads-count')?.textContent?.trim()).toBe('99+');
    expect(theButton(root)!.getAttribute('aria-label')).toBe('Downloads, 140 running');
  });

  it('R-UI-210 the count is read every 10 s while the window is seen, not while it is hidden, and at once when it is seen again', async () => {
    vi.useFakeTimers({ toFake: ['setInterval', 'clearInterval'] });
    try {
      const { fixture, fake } = await open({ running: 1 });
      expect(fake.summaryCalls).toBe(1);
      vi.advanceTimersByTime(SUMMARY_POLL_MS);
      await settle(fixture);
      expect(fake.summaryCalls).toBe(2);
      setVisibility('hidden');
      await settle(fixture);
      vi.advanceTimersByTime(SUMMARY_POLL_MS * 3);
      await settle(fixture);
      expect(fake.summaryCalls).toBe(2);
      setVisibility('visible');
      await settle(fixture);
      expect(fake.summaryCalls).toBe(3);
    } finally {
      vi.useRealTimers();
    }
  });

  it('R-UI-210 a failed read keeps the last count; a refusal hides the button', async () => {
    vi.useFakeTimers({ toFake: ['setInterval', 'clearInterval'] });
    try {
      const { root, fixture, fake } = await open({ running: 5 });
      fake.failWith = 503;
      vi.advanceTimersByTime(SUMMARY_POLL_MS);
      await settle(fixture);
      expect(theButton(root)!.querySelector('.downloads-count')?.textContent?.trim()).toBe('5');
      fake.failWith = 403;
      vi.advanceTimersByTime(SUMMARY_POLL_MS);
      await settle(fixture);
      expect(theButton(root)).toBeNull();
      vi.advanceTimersByTime(SUMMARY_POLL_MS * 3);
      await settle(fixture);
      expect(fake.summaryCalls).toBe(3);
    } finally {
      vi.useRealTimers();
    }
  });
});
