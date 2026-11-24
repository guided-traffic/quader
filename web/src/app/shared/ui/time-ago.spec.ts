import { Component, signal } from '@angular/core';
import { TestBed } from '@angular/core/testing';
import { expectAccessible, settle, text } from '../../testing/screen';
import { TimeAgo, timeAgo } from './time-ago';

const NOW = Date.parse('2026-11-12T14:00:00Z');
const before = (ms: number) => new Date(NOW - ms).toISOString();

describe('Time ago', () => {
  it.each([
    [0, 'now'],
    [59_000, 'now'],
    [60_000, '1 minute ago'],
    [59 * 60_000, '59 minutes ago'],
    [60 * 60_000, '1 hour ago'],
    [23 * 3_600_000 + 59 * 60_000, '23 hours ago'],
    [24 * 3_600_000, 'yesterday'],
    [45 * 86_400_000, 'last month'],
    [-29 * 86_400_000, 'in 4 weeks'],
  ] as const)('R-UI-093 R-UI-103 a time %d ms ago reads "%s", the one wording of a relative time', (ms, words) => {
    expect(timeAgo(before(ms), NOW)).toBe(words);
  });

  it('R-UI-093 shows the relative time and the absolute time in brackets, with the machine-readable time', async () => {
    @Component({ imports: [TimeAgo], template: `<p><quader-time-ago [at]="at()" [now]="now" /></p>` })
    class Host {
      readonly at = signal(before(3 * 60_000));
      readonly now = NOW;
    }
    await TestBed.configureTestingModule({ imports: [Host] }).compileComponents();
    const fixture = TestBed.createComponent(Host);
    await settle(fixture);
    const root = fixture.nativeElement as HTMLElement;
    expect(text(root)).toMatch(/^3 minutes ago \(Nov 12, 2026, \d+:57:00 [AP]M\)$/);
    expect(root.querySelector('time')?.getAttribute('datetime')).toBe(before(3 * 60_000));
    await expectAccessible(root);
  });

  it('R-UI-103 in a list shows the relative time alone, the absolute time as the title of the time element', async () => {
    @Component({ imports: [TimeAgo], template: `<p><quader-time-ago [at]="at()" [now]="now" absolute="title" /></p>` })
    class Host {
      readonly at = signal(before(2 * 86_400_000));
      readonly now = NOW;
    }
    await TestBed.configureTestingModule({ imports: [Host] }).compileComponents();
    const fixture = TestBed.createComponent(Host);
    await settle(fixture);
    const root = fixture.nativeElement as HTMLElement;
    expect(text(root)).toBe('2 days ago');
    const time = root.querySelector('time')!;
    expect(time.getAttribute('datetime')).toBe(before(2 * 86_400_000));
    expect(time.getAttribute('title')).toMatch(/^Nov 10, 2026, \d+:00:00\s[AP]M$/);
    await expectAccessible(root);
  });
});
