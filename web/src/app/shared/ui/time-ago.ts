import { TimePipe } from '../time-pipe';
import { Component, LOCALE_ID, computed, inject, input } from '@angular/core';
import { relativeTime } from '../../features/packages/format-time';

/** The locale of the build: the one `$localize` was compiled for, English where the build has none. */
function buildLocale(): string {
  return (typeof $localize !== 'undefined' && $localize.locale) || 'en-US';
}

/**
 * How long ago a time was, in the one wording of the UI (Q-UI-431): "now", "4 minutes ago", "yesterday", "3 months ago". It is
 * `relativeTime` in the locale of the build, for the callers that have no `LOCALE_ID` at hand.
 */
export function timeAgo(iso: string, now: number = Date.now()): string {
  return relativeTime(iso, buildLocale(), now);
}

/**
 * A time relative to now with its absolute date and time (Q-UI-378, Q-UI-431). `absolute="beside"` (the default) writes both, joined by
 * a dot, "3 hours ago · 26.11.2026 21:22:18 (Thu.)": the form of a card's facts (R-UI-093) and of any place that is no list. Brackets
 * around the absolute time read as brackets inside brackets once the reader's form ends in its weekday (R-UI-250, Q-UI-1525).
 * `absolute="title"` writes the relative time alone and puts the absolute one into the `<time>` element's title: the form of every
 * list's cell, whose column is too narrow for both.
 */
@Component({
  selector: 'quader-time-ago',
  imports: [TimePipe],
  template: `@if (absolute() === 'title') {
      <time [attr.datetime]="at()" [attr.title]="at() | time: 'datetime' : zone()">{{ relative() }}</time>
    } @else {
      {{ relative() }} · <time [attr.datetime]="at()">{{ at() | time: 'datetime' : zone() }}</time>
    }`,
  // The date and the time are one value: a line breaks before or after it, never inside it.
  styles: `
    time {
      white-space: nowrap;
    }
  `,
})
export class TimeAgo {
  private readonly locale = inject(LOCALE_ID);
  /** The time, RFC 3339. */
  readonly at = input.required<string>();
  /** Where the absolute time stands: beside the relative one after a dot, or in the title of a list's cell. */
  readonly absolute = input<'beside' | 'title'>('beside');
  /** The zone of the absolute time ("UTC" where the page shows times in UTC, as the audit log can); the browser's by default. */
  readonly zone = input<string | undefined>(undefined);
  /** The time it is now, in milliseconds; a test sets it, the page leaves it to the clock. */
  readonly now = input<number | null>(null);

  protected readonly relative = computed(() => relativeTime(this.at(), this.locale, this.now() ?? Date.now()));
}
