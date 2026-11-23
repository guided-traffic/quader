import { DatePipe } from '@angular/common';
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
 * A time relative to now with its absolute date and time (Q-UI-378, Q-UI-431). `absolute="brackets"` (the default) writes both,
 * "3 minutes ago (Nov 12, 2026, 2:22:37 PM)": the form of a card's facts (R-UI-093) and of any place that is no list.
 * `absolute="title"` writes the relative time alone and puts the absolute one into the `<time>` element's title: the form of every
 * list's cell, whose column is too narrow for both.
 */
@Component({
  selector: 'quader-time-ago',
  imports: [DatePipe],
  template: `@if (absolute() === 'title') {
      <time [attr.datetime]="at()" [attr.title]="at() | date: 'medium' : zone()">{{ relative() }}</time>
    } @else {
      {{ relative() }} (<time [attr.datetime]="at()">{{ at() | date: 'medium' : zone() }}</time>)
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
  /** Where the absolute time stands: in brackets after the relative one, or in the title of a list's cell. */
  readonly absolute = input<'brackets' | 'title'>('brackets');
  /** The zone of the absolute time ("UTC" where the page shows times in UTC, as the audit log can); the browser's by default. */
  readonly zone = input<string | undefined>(undefined);
  /** The time it is now, in milliseconds; a test sets it, the page leaves it to the clock. */
  readonly now = input<number | null>(null);

  protected readonly relative = computed(() => relativeTime(this.at(), this.locale, this.now() ?? Date.now()));
}
