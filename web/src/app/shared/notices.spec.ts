import { TestBed } from '@angular/core/testing';
import { vi } from 'vitest';
import { Notifier, STATUS_LIFETIME_MS, STATUS_RESUME_MIN_MS } from '../core/notifier';
import { expectAccessible, settle } from '../testing/screen';
import { Notices } from './notices';

async function render() {
  TestBed.resetTestingModule();
  await TestBed.configureTestingModule({ imports: [Notices] }).compileComponents();
  const fixture = TestBed.createComponent(Notices);
  await settle(fixture);
  const root = fixture.nativeElement as HTMLElement;
  return { fixture, root, notifier: TestBed.inject(Notifier) };
}

const texts = (region: Element | null) => [...(region?.querySelectorAll('.toast-text') ?? [])].map((t) => t.textContent?.trim());

describe('Notifier', () => {
  afterEach(() => vi.useRealTimers());

  it('R-UI-042 shows one confirmation at a time: a new one replaces the confirmation still shown and keeps its own time', () => {
    vi.useFakeTimers();
    TestBed.resetTestingModule();
    const n = TestBed.inject(Notifier);
    n.status('Ubuntu is on.');
    vi.advanceTimersByTime(STATUS_LIFETIME_MS - 1000);
    n.status('Ubuntu security updates is on.');
    expect(n.notices().map((x) => x.text)).toEqual(['Ubuntu security updates is on.']);
    vi.advanceTimersByTime(STATUS_LIFETIME_MS - 1);
    expect(n.notices().map((x) => x.text)).toEqual(['Ubuntu security updates is on.']);
    vi.advanceTimersByTime(1);
    expect(n.notices()).toEqual([]);
  });

  it('R-UI-042 a notice with an action and an error keep their place beside a new confirmation', () => {
    TestBed.resetTestingModule();
    const n = TestBed.inject(Notifier);
    n.status('The bulk download is queued.', { label: 'Show downloads', run: () => undefined });
    n.alert('The clipboard is not available.');
    n.status('Ubuntu is on.');
    n.status('Ubuntu ports is on.');
    expect(n.notices().map((x) => [x.kind, x.text])).toEqual([
      ['status', 'The bulk download is queued.'],
      ['alert', 'The clipboard is not available.'],
      ['status', 'Ubuntu ports is on.'],
    ]);
  });

  it("R-UI-042 a confirmation's time stops while the pointer or the focus is on it and goes on with what was left", () => {
    vi.useFakeTimers();
    TestBed.resetTestingModule();
    const n = TestBed.inject(Notifier);
    const id = n.status('Address saved.');
    vi.advanceTimersByTime(STATUS_LIFETIME_MS - 3000);
    n.pause(id);
    vi.advanceTimersByTime(STATUS_LIFETIME_MS * 2);
    expect(n.notices().length).toBe(1);
    n.resume(id);
    vi.advanceTimersByTime(2999);
    expect(n.notices().length).toBe(1);
    vi.advanceTimersByTime(1);
    expect(n.notices()).toEqual([]);
    // Paused at its last moment, it still stays the least time once the pointer leaves.
    const late = n.status('Account saved.');
    vi.advanceTimersByTime(STATUS_LIFETIME_MS - 10);
    n.pause(late);
    n.resume(late);
    vi.advanceTimersByTime(STATUS_RESUME_MIN_MS - 1);
    expect(n.notices().length).toBe(1);
    vi.advanceTimersByTime(1);
    expect(n.notices()).toEqual([]);
  });
});

describe('Notices', () => {
  it('R-UI-042 keeps both live regions from the start and shows a confirmation as a toast in the polite one', async () => {
    const { fixture, root, notifier } = await render();
    const status = root.querySelector('[role="status"]');
    const alert = root.querySelector('[role="alert"]');
    expect(status).not.toBeNull();
    expect(alert).not.toBeNull();
    notifier.status('Ubuntu is on.');
    notifier.status('Ubuntu ports is on.');
    await settle(fixture);
    expect(texts(status)).toEqual(['Ubuntu ports is on.']);
    expect(texts(alert)).toEqual([]);
    await expectAccessible(root);
  });

  it('R-UI-042 closes a toast with the icon button named Close, not a word', async () => {
    const { fixture, root, notifier } = await render();
    notifier.status('Copied to the clipboard.');
    await settle(fixture);
    const close = root.querySelector<HTMLButtonElement>('[role="status"] .toast button.toast-close');
    expect(close?.getAttribute('aria-label')).toBe('Close');
    expect(close?.querySelector('.material-icons')?.getAttribute('aria-hidden')).toBe('true');
    expect(close?.textContent?.trim()).toBe('close');
    close!.click();
    await settle(fixture);
    expect(notifier.notices()).toEqual([]);
    expect(root.querySelector('.toast')).toBeNull();
  });

  it('R-UI-042 runs an action and closes its toast; an error shows its icon in the alert region', async () => {
    const { fixture, root, notifier } = await render();
    const run = vi.fn();
    notifier.status('The bulk download is queued; its rows wait in the tab Downloads.', { label: 'Show downloads', run });
    notifier.alert('The clipboard is not available. Select the text and copy it.');
    await settle(fixture);
    const alert = root.querySelector('[role="alert"] .toast.alert');
    expect(alert?.querySelector('.toast-icon')?.getAttribute('aria-hidden')).toBe('true');
    expect(alert?.querySelector('.toast-close')?.getAttribute('aria-label')).toBe('Close');
    await expectAccessible(root);
    root.querySelector<HTMLButtonElement>('[role="status"] .toast-action')!.click();
    await settle(fixture);
    expect(run).toHaveBeenCalledOnce();
    expect(notifier.notices().map((n) => n.kind)).toEqual(['alert']);
  });

  it("R-UI-042 holds a confirmation's time while the pointer is on its toast", async () => {
    const { fixture, root, notifier } = await render();
    const pause = vi.spyOn(notifier, 'pause');
    const resume = vi.spyOn(notifier, 'resume');
    const id = notifier.status('Account saved.');
    await settle(fixture);
    const toast = root.querySelector('.toast')!;
    toast.dispatchEvent(new MouseEvent('mouseenter'));
    toast.dispatchEvent(new MouseEvent('mouseleave'));
    expect(pause).toHaveBeenCalledWith(id);
    expect(resume).toHaveBeenCalledWith(id);
  });
});
