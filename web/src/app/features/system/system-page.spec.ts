import { TestBed } from '@angular/core/testing';
import { Router, provideRouter, withComponentInputBinding } from '@angular/router';
import { RouterTestingHarness } from '@angular/router/testing';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { BackupsApi } from '../../api/backups-api';
import { JobsApi } from '../../api/jobs-api';
import { SessionApi } from '../../api/session-api';
import { SystemApi } from '../../api/system-api';
import { FakeBackupsApi } from '../../api/testing/fakes-backups';
import { FakeJobsApi } from '../../api/testing/fakes-catalog';
import { FakeSessionApi, FakeSystemApi, provideFakeApis } from '../../api/testing/fakes';
import { SessionStore } from '../../core/session-store';
import { seriousViolations } from '../../testing/axe';
import { text } from '../../testing/screen';
import { REFRESH_INTERVAL_MS } from './system-status';
import { SYSTEM_ROUTES } from './system.routes';

/** The page System status as the router builds it: the header, the tab strip and the tab's route below (R-UI-164). */
async function harness(capabilities?: string[]): Promise<RouterTestingHarness> {
  TestBed.resetTestingModule();
  TestBed.configureTestingModule({
    providers: [provideRouter([{ path: 'admin/system', children: SYSTEM_ROUTES }], withComponentInputBinding()), ...provideFakeApis()],
  });
  if (capabilities) (TestBed.inject(SessionApi) as FakeSessionApi).meResult = { capabilities };
  await TestBed.inject(SessionStore).load();
  return RouterTestingHarness.create();
}

async function open(h: RouterTestingHarness, url: string): Promise<HTMLElement> {
  await h.navigateByUrl(url);
  return settled(h);
}

async function settled(h: RouterTestingHarness): Promise<HTMLElement> {
  for (let i = 0; i < 6; i++) {
    await h.fixture.whenStable();
    h.detectChanges();
  }
  return h.routeNativeElement as HTMLElement;
}

const tabs = (root: HTMLElement) => [...root.querySelectorAll<HTMLAnchorElement>('nav.tab-nav a.tab-link')];
const current = (root: HTMLElement) => tabs(root).filter((a) => a.getAttribute('aria-current') === 'page').map((a) => text(a));
const button = (root: HTMLElement, label: string) => [...root.querySelectorAll<HTMLButtonElement>('button')].filter((b) => text(b) === label);

describe('The page System status (R-UI-164, Q-UI-650)', () => {
  afterEach(() => vi.useRealTimers());

  it('R-UI-164 R-UI-196 has one header with the title System status and the tabs Status, Jobs, Backups and Storage, each with its own address', async () => {
    const h = await harness();
    for (const [url, tab, region] of [
      ['/admin/system', 'Status', 'Replicas by role'],
      ['/admin/system/jobs', 'Jobs', 'Jobs, newest first'],
      ['/admin/system/backups', 'Backups', 'Runs'],
      ['/admin/system/storage', 'Storage', 'Where Quader keeps your artifacts'],
    ] as const) {
      const root = await open(h, url);
      expect([...root.querySelectorAll('h1')].map((e) => text(e))).toEqual(['System status']);
      expect(tabs(root).map((a) => [text(a), a.getAttribute('href')])).toEqual([
        ['Status', '/admin/system'],
        ['Jobs', '/admin/system/jobs'],
        ['Backups', '/admin/system/backups'],
        ['Storage', '/admin/system/storage'],
      ]);
      expect(current(root)).toEqual([tab]);
      expect(text(root)).toContain(region);
      // One pair of refresh controls, the header's.
      expect(button(root, 'Refresh now')).toHaveLength(1);
      expect(button(root, 'Pause automatic refresh')).toHaveLength(1);
      expect(root.querySelector('quader-page-header')!.contains(button(root, 'Refresh now')[0]!)).toBe(true);
      expect(await seriousViolations(root)).toEqual([]);
    }
  });

  it('R-UI-164 the status tab has no links to Jobs and Backups of its own: the tabs are the way there', async () => {
    const h = await harness();
    const root = await open(h, '/admin/system');
    const links = [...root.querySelectorAll<HTMLAnchorElement>('a[href^="/admin/system/"]')].filter((a) => !a.classList.contains('tab-link'));
    expect(links.map((a) => a.getAttribute('href'))).toEqual([]);
  });

  it('R-UI-164 Refresh now reloads the tab that is shown', async () => {
    const h = await harness();
    let root = await open(h, '/admin/system/jobs');
    const jobs = TestBed.inject(JobsApi) as FakeJobsApi;
    const backups = TestBed.inject(BackupsApi) as FakeBackupsApi;
    const system = TestBed.inject(SystemApi) as FakeSystemApi;
    const info = vi.spyOn(system, 'info');
    const jobCalls = jobs.filterCalls.length;
    const summaryCalls = backups.summaryCalls;
    button(root, 'Refresh now')[0]!.click();
    await settled(h);
    expect(jobs.filterCalls.length).toBe(jobCalls + 1);
    expect(backups.summaryCalls).toBe(summaryCalls);
    expect(info).not.toHaveBeenCalled();

    root = await open(h, '/admin/system/backups');
    const afterOpen = backups.summaryCalls;
    const jobsAfter = jobs.filterCalls.length;
    button(root, 'Refresh now')[0]!.click();
    await settled(h);
    expect(backups.summaryCalls).toBe(afterOpen + 1);
    expect(jobs.filterCalls.length).toBe(jobsAfter);
  });

  it('R-UI-164 the pause holds across the tabs while the page is open and stops each tab\'s timer (WCAG 2.2.2)', async () => {
    vi.useFakeTimers({ toFake: ['setInterval', 'clearInterval'] });
    const h = await harness();
    let root = await open(h, '/admin/system');
    const system = TestBed.inject(SystemApi) as FakeSystemApi;
    const info = vi.spyOn(system, 'info');
    vi.advanceTimersByTime(REFRESH_INTERVAL_MS);
    await settled(h);
    expect(info).toHaveBeenCalledTimes(1);

    button(root, 'Pause automatic refresh')[0]!.click();
    root = await settled(h);
    expect(button(root, 'Resume automatic refresh')[0]!.getAttribute('aria-pressed')).toBe('true');
    vi.advanceTimersByTime(REFRESH_INTERVAL_MS * 4);
    await settled(h);
    expect(info).toHaveBeenCalledTimes(1);

    tabs(root).find((a) => text(a) === 'Backups')!.click();
    root = await settled(h);
    expect(current(root)).toEqual(['Backups']);
    expect(button(root, 'Resume automatic refresh')).toHaveLength(1);
    const backups = TestBed.inject(BackupsApi) as FakeBackupsApi;
    const summaryCalls = backups.summaryCalls;
    vi.advanceTimersByTime(REFRESH_INTERVAL_MS * 4);
    await settled(h);
    expect(backups.summaryCalls).toBe(summaryCalls);
  });

  it('R-UI-164 a filter belongs to its tab: the address keeps it, another tab drops it, and the way back is the browser\'s', async () => {
    const h = await harness();
    let root = await open(h, '/admin/system/jobs?kind=gc&state=failed');
    expect(current(root)).toEqual(['Jobs']);
    expect(text(root)).toContain('gc');
    const jobs = TestBed.inject(JobsApi) as FakeJobsApi;
    expect(jobs.filterCalls.at(-1)).toEqual({ kind: ['gc'], state: ['failed'] });
    // The tabs link to their tab without the filters of another.
    expect(tabs(root).map((a) => a.getAttribute('href'))).toEqual(['/admin/system', '/admin/system/jobs', '/admin/system/backups', '/admin/system/storage']);
    tabs(root).find((a) => text(a) === 'Backups')!.click();
    root = await settled(h);
    expect(TestBed.inject(Router).url).toBe('/admin/system/backups');
    tabs(root).find((a) => text(a) === 'Jobs')!.click();
    root = await settled(h);
    expect(TestBed.inject(Router).url).toBe('/admin/system/jobs');
    expect(current(root)).toEqual(['Jobs']);
    expect(jobs.filterCalls.at(-1)).toEqual({ kind: [], state: [] });
  });

  it('Q-UI-652 R-UI-196 a caller without settings:read who follows a link to the jobs sees them without a strip of one tab, and no tab Storage', async () => {
    const h = await harness([]);
    const root = await open(h, '/admin/system/jobs');
    expect(root.querySelector('nav.tab-nav')).toBeNull();
    expect(root.querySelector('a[href="/admin/system/storage"]')).toBeNull();
    expect([...root.querySelectorAll('h1')].map((e) => text(e))).toEqual(['System status']);
    expect(text(root)).toContain('Jobs, newest first');
    expect(await seriousViolations(root)).toEqual([]);
  });
});
