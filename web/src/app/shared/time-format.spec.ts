import { Component, signal } from '@angular/core';
import { TestBed } from '@angular/core/testing';
import { FACTORY_TIME_FORMAT, TIME_FORMATS, TimeFormat, formatTime, isTimeFormat, timeFormat } from './time-format';
import { timeFormatOptions } from './time-format-options';
import { TimePipe } from './time-pipe';

// The owner's example (2026-11-26, about 21:40 in Berlin): a Thursday.
const AT = '2026-11-26T20:39:22Z';
const BERLIN = 'Europe/Berlin';
const en = (format: TimeFormat, part?: 'datetime' | 'date' | 'time' | 'zoned' | 'minutes'): string =>
  formatTime(AT, { format, part, zone: BERLIN, locale: 'en-US' });
const de = (format: TimeFormat, part?: 'datetime' | 'date' | 'time' | 'zoned' | 'minutes'): string =>
  formatTime(AT, { format, part, zone: BERLIN, locale: 'de' });

describe('R-UI-250 one formatter writes every time in the form the reader chose', () => {
  afterEach(() => timeFormat.set(FACTORY_TIME_FORMAT));

  it("writes the owner's form by default: day, month, year with dots, the 24-hour time with seconds, the weekday with a dot", () => {
    expect(FACTORY_TIME_FORMAT).toBe('dmy-weekday');
    expect(formatTime(AT, { zone: BERLIN, locale: 'en-US' })).toBe('26.11.2026 21:39:22 (Thu.)');
    expect(formatTime(AT, { zone: BERLIN, locale: 'de' })).toBe('26.11.2026 21:39:22 (Do.)');
  });

  it('writes every form the choice offers, in both languages', () => {
    expect(TIME_FORMATS).toEqual(['dmy-weekday', 'dmy', 'iso', 'dmy-slash', 'mdy', 'locale']);
    expect(en('dmy')).toBe('26.11.2026 21:39:22');
    expect(en('iso')).toBe('2026-11-26 21:39:22');
    expect(en('dmy-slash')).toBe('26/11/2026 21:39:22');
    expect(en('mdy')).toBe('11/26/2026 9:39:22 PM');
    expect(en('locale').replace(/\s/g, ' ')).toBe('Nov 26, 2026, 9:39:22 PM');
    expect(de('dmy')).toBe('26.11.2026 21:39:22');
    expect(de('iso')).toBe('2026-11-26 21:39:22');
    expect(de('mdy')).toBe('11/26/2026 9:39:22 PM');
    expect(de('locale')).toBe('26.11.2026, 21:39:22');
  });

  it('writes a date without its time, and a time of day without its date', () => {
    expect(en('dmy-weekday', 'date')).toBe('26.11.2026 (Thu.)');
    expect(de('dmy-weekday', 'date')).toBe('26.11.2026 (Do.)');
    expect(en('iso', 'date')).toBe('2026-11-26');
    expect(en('mdy', 'date')).toBe('11/26/2026');
    expect(en('dmy-weekday', 'time')).toBe('21:39:22');
    expect(en('mdy', 'time')).toBe('9:39:22 PM');
    expect(en('dmy-weekday', 'minutes')).toBe('21:39');
  });

  it('writes midnight and noon in the American form as 12 AM and 12 PM', () => {
    const us = (iso: string): string => formatTime(iso, { format: 'mdy', zone: 'UTC', locale: 'en-US' });
    expect(us('2026-11-26T00:00:00Z')).toBe('11/26/2026 12:00:00 AM');
    expect(us('2026-11-26T12:00:00Z')).toBe('11/26/2026 12:00:00 PM');
    expect(us('2026-11-26T00:05:09Z')).toBe('11/26/2026 12:05:09 AM');
    expect(formatTime('2026-11-26T00:00:00Z', { format: 'dmy', zone: 'UTC' })).toBe('26.11.2026 00:00:00');
  });

  it("writes each weekday's abbreviation with one dot in the reader's language", () => {
    const days = Array.from({ length: 7 }, (_, i) => `2026-11-${String(23 + i).padStart(2, '0')}T12:00:00Z`);
    expect(days.map((d) => formatTime(d, { part: 'date', zone: 'UTC', locale: 'en-US' }).slice(-6))).toEqual([
      '(Mon.)',
      '(Tue.)',
      '(Wed.)',
      '(Thu.)',
      '(Fri.)',
      '(Sat.)',
      '(Sun.)',
    ]);
    expect(days.map((d) => formatTime(d, { part: 'date', zone: 'UTC', locale: 'de' }).slice(-5))).toEqual([
      '(Mo.)',
      '(Di.)',
      '(Mi.)',
      '(Do.)',
      '(Fr.)',
      '(Sa.)',
      '(So.)',
    ]);
  });

  it("names the zone where the place shows it, and writes nothing for no time", () => {
    expect(formatTime(AT, { part: 'zoned', zone: 'UTC', locale: 'en-US' })).toBe('26.11.2026 20:39:22 (Thu.) UTC');
    expect(formatTime(null)).toBe('');
    expect(formatTime(undefined)).toBe('');
    expect(formatTime('')).toBe('');
    expect(formatTime('not a time')).toBe('');
    expect(formatTime(Date.parse(AT), { zone: 'UTC' })).toBe('26.11.2026 20:39:22 (Thu.)');
  });

  it('follows the form in force, and knows only the forms it offers', () => {
    timeFormat.set('iso');
    expect(formatTime(AT, { zone: 'UTC' })).toBe('2026-11-26 20:39:22');
    expect(isTimeFormat('mdy')).toBe(true);
    expect(isTimeFormat('yyyy')).toBe(false);
    expect(isTimeFormat(null)).toBe(false);
  });

  it("offers every form in the choice with today's time as its example", () => {
    const options = timeFormatOptions(Date.parse(AT));
    expect(options.map((o) => o.value)).toEqual([...TIME_FORMATS]);
    expect(options[0]!.label).toMatch(/^26\.11\.2026 \d\d:39:22 \(Thu\.\) — With the weekday$/);
    expect(options[2]!.label).toMatch(/^2026-11-26 \d\d:39:22 — ISO 8601$/);
  });
});

@Component({
  imports: [TimePipe],
  template: `<time>{{ at() | time }}</time><span>{{ at() | time: 'date' : 'UTC' }}</span>`,
})
class Host {
  readonly at = signal<string | null>('2026-11-26T20:39:22Z');
}

describe('R-UI-250 the pipe time', () => {
  afterEach(() => timeFormat.set(FACTORY_TIME_FORMAT));

  it('writes the form in force and follows a change of it while the page stands', async () => {
    const fixture = TestBed.createComponent(Host);
    await fixture.whenStable();
    const root = fixture.nativeElement as HTMLElement;
    expect(root.querySelector('span')!.textContent).toBe('26.11.2026 (Thu.)');
    timeFormat.set('iso');
    await fixture.whenStable();
    expect(root.querySelector('span')!.textContent).toBe('2026-11-26');
    fixture.componentInstance.at.set(null);
    await fixture.whenStable();
    expect(root.querySelector('time')!.textContent).toBe('');
  });
});
