import { Pipe, PipeTransform } from '@angular/core';
import { TimePart, formatTime, timeFormat } from './time-format';

/**
 * A time in the form the reader chose (R-UI-250), in a template: `{{ at | time }}`, `{{ at | time: 'date' }}`,
 * `{{ at | time: 'zoned' : 'UTC' }}`. Not pure, because the form can change while the page stands (a save of the preferences, the
 * installation's default read again); it keeps its last answer and formats again only when the value, the part, the zone or the form
 * changed, so a list of a hundred rows costs a comparison per row and check.
 */
@Pipe({ name: 'time', pure: false })
export class TimePipe implements PipeTransform {
  private last: { value: unknown; part: TimePart; zone: string | undefined; format: string; text: string } | null = null;

  transform(value: string | number | Date | null | undefined, part: TimePart = 'datetime', zone?: string): string {
    const format = timeFormat();
    const l = this.last;
    if (l && l.value === value && l.part === part && l.zone === zone && l.format === format) return l.text;
    const text = formatTime(value, { part, zone, format });
    this.last = { value, part, zone, format, text };
    return text;
  }
}
