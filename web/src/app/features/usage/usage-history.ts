import { formatDate } from '@angular/common';
import {
  ChangeDetectionStrategy,
  Component,
  LOCALE_ID,
  computed,
  inject,
  input,
  signal,
} from '@angular/core';
import { UsagePoint } from '../../api/limits-api';
import { formatBytes, formatCount } from '../limits/figures';
import { DataTable, TableColumn } from '../../shared/ui/data-table';
import { niceMax } from '../vulnerabilities/report-trend';

const WIDTH = 640;
const HEIGHT = 72;
const LEFT = 52;
const RIGHT = 4;
const TOP = 6;
const BOTTOM = 4;

/** The three quantities of the history, each a chart of its own: requests are counted, bytes measured (one axis per chart). */
export type HistorySeries = 'requests' | 'cachedBytes' | 'uncachedBytes';
export const HISTORY_SERIES: readonly HistorySeries[] = [
  'requests',
  'cachedBytes',
  'uncachedBytes',
];

export interface HistoryChart {
  readonly series: HistorySeries;
  /** One rectangle per five minutes with a value, as one path. */
  readonly path: string;
  /** The marks of the limit events: a triangle over each bucket with a refused, limited or would-limit event. */
  readonly marks: readonly { readonly x: number; readonly i: number }[];
  readonly max: number;
  readonly slot: number;
  /** No point has a value: the axis names only its 0, not a scale of nothing ("5 B"). */
  readonly empty: boolean;
}

/** The columns of one chart in viewBox units (exported for the specs). */
export function historyChart(points: readonly UsagePoint[], series: HistorySeries): HistoryChart {
  const n = Math.max(points.length, 1);
  const plotW = WIDTH - LEFT - RIGHT;
  const plotH = HEIGHT - TOP - BOTTOM;
  const slot = plotW / n;
  const barW = Math.max(0.6, slot - (slot > 3 ? 1 : 0.2));
  const max = niceMax(Math.max(0, ...points.map((p) => p[series])));
  let d = '';
  points.forEach((p, i) => {
    const v = p[series];
    if (v <= 0) return;
    const h = Math.max(0.8, (v / max) * plotH);
    d += `M${(LEFT + i * slot).toFixed(2)} ${(TOP + plotH - h).toFixed(2)}h${barW.toFixed(2)}v${h.toFixed(2)}h${(-barW).toFixed(2)}z`;
  });
  const marks =
    series === 'requests'
      ? points.flatMap((p, i) =>
          p.refused + p.limitedEvents + p.wouldLimitEvents > 0
            ? [{ x: LEFT + i * slot + barW / 2, i }]
            : [],
        )
      : [];
  return { series, path: d, marks, max, slot, empty: d === '' };
}

/**
 * The last 24 hours of a caller in five-minute points (R-UI-184, R-UI-185, Q-UI-754): requests, bytes from the cache and bytes
 * from the upstream as three small charts over one time line, each with its own axis, one series each in the data colour; the
 * buckets with refused requests or limit events are marked over the requests in the warning colour, named in the legend. A
 * pointer over a chart or the arrow keys on it read one five-minute point out in words; **Show as table** shows every point.
 */
@Component({
  selector: 'quader-usage-history',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [DataTable],
  template: `
    <section class="history" aria-labelledby="history-title">
      <div class="history-head">
        <h2 id="history-title" i18n="@@usage.history.title">Last 24 hours</h2>
        <button
          class="btn btn-tertiary"
          type="button"
          [attr.aria-expanded]="asTable()"
          aria-controls="history-body"
          (click)="asTable.set(!asTable())"
        >
          @if (asTable()) {
            <ng-container i18n="@@vuln.trend.show-chart">Show as chart</ng-container>
          } @else {
            <ng-container i18n="@@vuln.trend.show-table">Show as table</ng-container>
          }
        </button>
      </div>
      <div id="history-body">
        @if (points().length === 0) {
          <p class="empty">{{ emptyText() }}</p>
        } @else if (asTable()) {
          <!-- The shared data table: the columns that do not fit give way and then the rows stack (R-UI-174), as every list; its
               288 points scroll in a region of their own, so the page below stays near. -->
          <div
            class="points-scroll"
            tabindex="0"
            role="region"
            i18n-aria-label="@@usage.history.table-label"
            aria-label="Five-minute points of the last 24 hours"
          >
            <quader-data-table
              class="points"
              caption="Five-minute points of the last 24 hours"
              i18n-caption="@@usage.history.table-label"
              [columns]="tableColumns"
            >
              @for (p of points(); track p.start) {
                <tr>
                  <th scope="row">
                    <time [attr.datetime]="p.start">{{ time(p.start) }}</time>
                  </th>
                  <td class="num">{{ count(p.requests) }}</td>
                  <td class="num">{{ bytes(p.cachedBytes) }}</td>
                  <td class="num">{{ bytes(p.uncachedBytes) }}</td>
                  <td class="num narrow-drop">{{ count(p.upstreamRequests) }}</td>
                  <td class="num narrow-drop">{{ count(p.refused) }}</td>
                  <td class="num">{{ count(p.limitedEvents + p.wouldLimitEvents) }}</td>
                </tr>
              }
            </quader-data-table>
          </div>
        } @else {
          <div
            class="charts"
            tabindex="0"
            role="group"
            [attr.aria-label]="chartsLabel"
            aria-describedby="history-readout"
            (keydown)="key($event)"
            (pointerleave)="active.set(null)"
            (blur)="active.set(null)"
          >
            @for (c of charts(); track c.series) {
              <div class="chart">
                <span class="chart-label">{{ seriesLabel(c.series) }}</span>
                <svg
                  [attr.viewBox]="'0 0 ' + width + ' ' + height"
                  preserveAspectRatio="none"
                  aria-hidden="true"
                  (pointermove)="point($event)"
                >
                  <line
                    class="grid"
                    [attr.x1]="left"
                    [attr.x2]="width - 4"
                    [attr.y1]="top"
                    [attr.y2]="top"
                  />
                  <line
                    class="axis"
                    [attr.x1]="left"
                    [attr.x2]="width - 4"
                    [attr.y1]="height - 4"
                    [attr.y2]="height - 4"
                  />
                  <path class="bars" [attr.d]="c.path" />
                  @for (m of c.marks; track m.i) {
                    <path class="mark" [attr.d]="'M' + m.x + ' 0l3 5h-6z'" />
                  }
                  @if (active() !== null) {
                    <line
                      class="cursor"
                      [attr.x1]="left + (active()! + 0.5) * c.slot"
                      [attr.x2]="left + (active()! + 0.5) * c.slot"
                      [attr.y1]="0"
                      [attr.y2]="height"
                    />
                  }
                </svg>
                <span class="tick tick-max">{{ c.empty ? '' : tickText(c.series, c.max) }}</span>
                <span class="tick tick-zero">0</span>
              </div>
            }
            <div class="times" aria-hidden="true">
              @for (t of times(); track t.at) {
                <span [style.inset-inline-start.%]="t.percent">{{ t.label }}</span>
              }
            </div>
          </div>
          <p id="history-readout" class="readout" aria-live="polite">{{ readout() }}</p>
          <ul class="legend" i18n-aria-label="@@usage.history.legend" aria-label="Legend">
            <li>
              <span class="swatch" aria-hidden="true"></span
              ><ng-container i18n="@@usage.history.legend-bars">Five minutes each</ng-container>
            </li>
            <li>
              <span class="material-icons warn" aria-hidden="true">warning</span>
              <ng-container i18n="@@usage.history.legend-marks"
                >Refused requests or limit events</ng-container
              >
            </li>
          </ul>
        }
      </div>
    </section>
  `,
  styles: `
    :host {
      display: block;
    }
    /* The button stands on the middle of the heading's one line (R-UI-175). */
    .history-head {
      display: flex;
      flex-wrap: wrap;
      align-items: center;
      gap: var(--quader-space-3);
    }
    h2 {
      margin: 0;
      margin-inline-end: auto;
      font: var(--quader-type-title-medium);
    }
    .empty {
      margin: var(--quader-space-2) 0 0;
      color: var(--quader-text-muted);
    }
    .charts {
      position: relative;
      margin-block-start: var(--quader-space-2);
      padding-block-end: 1.25rem;
      border-radius: var(--quader-radius-s);
    }
    .charts:focus-visible {
      outline: var(--quader-focus-ring);
      outline-offset: 2px;
    }
    .chart {
      position: relative;
      margin-block-end: var(--quader-space-2);
    }
    .chart-label {
      display: block;
      color: var(--quader-text-muted);
      font: var(--quader-type-body-small);
    }
    svg {
      display: block;
      inline-size: 100%;
      block-size: 4.5rem;
    }
    .grid {
      stroke: var(--quader-border);
      stroke-width: 1;
      vector-effect: non-scaling-stroke;
    }
    .axis {
      stroke: var(--quader-control-border);
      stroke-width: 1;
      vector-effect: non-scaling-stroke;
    }
    .bars {
      fill: var(--quader-chart-low);
    }
    .mark {
      fill: var(--quader-status-warn);
    }
    .cursor {
      stroke: var(--quader-text);
      stroke-width: 1;
      vector-effect: non-scaling-stroke;
    }
    .tick {
      position: absolute;
      inset-inline-start: 0;
      inline-size: 3rem;
      color: var(--quader-text-muted);
      font: var(--quader-type-body-small);
      font-variant-numeric: tabular-nums;
      white-space: nowrap;
      overflow: hidden;
      text-overflow: ellipsis;
    }
    .tick-max {
      inset-block-start: 1rem;
    }
    .tick-zero {
      inset-block-end: 0;
    }
    .times {
      position: absolute;
      inset-block-end: 0;
      /* The plot starts at LEFT of WIDTH: 52 of 640. */
      inset-inline: 8.125% 0;
      block-size: 1rem;
    }
    .times span {
      position: absolute;
      transform: translateX(-50%);
      color: var(--quader-text-muted);
      font: var(--quader-type-body-small);
      white-space: nowrap;
    }
    .readout {
      min-block-size: var(--quader-line);
      margin: var(--quader-space-1) 0 0;
      font-variant-numeric: tabular-nums;
    }
    .legend {
      display: flex;
      flex-wrap: wrap;
      gap: var(--quader-space-2) var(--quader-space-4);
      margin: var(--quader-space-1) 0 0;
      padding: 0;
      list-style: none;
      color: var(--quader-text-muted);
      font: var(--quader-type-body-small);
    }
    .legend li {
      display: inline-flex;
      align-items: center;
      gap: var(--quader-space-1);
    }
    .legend .material-icons {
      font-size: 1rem;
    }
    .warn {
      color: var(--quader-status-warn);
    }
    .swatch {
      inline-size: 0.75rem;
      block-size: 0.75rem;
      border-radius: 2px;
      background: var(--quader-chart-low);
    }
    .points-scroll {
      max-block-size: 24rem;
      margin-block-start: var(--quader-space-2);
      overflow-y: auto;
    }
    .points-scroll:focus-visible {
      outline: var(--quader-focus-ring);
      outline-offset: 2px;
    }
    .points {
      display: block;
    }
  `,
})
export class UsageHistory {
  private readonly locale = inject(LOCALE_ID);

  readonly points = input.required<readonly UsagePoint[]>();
  readonly emptyText = input($localize`:@@usage.history.empty:No downloads in the last 24 hours.`);
  protected readonly tableColumns: readonly TableColumn[] = [
    { id: 'time', label: $localize`:@@usage.history.col-time:Time` },
    { id: 'requests', label: $localize`:@@usage.col.requests:Requests`, numeric: true },
    { id: 'cached', label: $localize`:@@usage.col.cached:From the cache`, numeric: true },
    { id: 'uncached', label: $localize`:@@usage.col.uncached:From the upstream`, numeric: true },
    {
      id: 'upstream',
      label: $localize`:@@usage.col.upstream:Upstream requests`,
      numeric: true,
      narrow: true,
    },
    { id: 'refused', label: $localize`:@@usage.col.refused:Refused`, numeric: true, narrow: true },
    { id: 'limited', label: $localize`:@@usage.history.col-limited:Limit events`, numeric: true },
  ];

  protected readonly width = WIDTH;
  protected readonly height = HEIGHT;
  protected readonly left = LEFT;
  protected readonly top = TOP;
  protected readonly asTable = signal(false);
  /** The point the read-out names: by the pointer, or by the arrow keys. */
  protected readonly active = signal<number | null>(null);
  protected readonly chartsLabel = $localize`:@@usage.history.charts-label:Requests, from the cache and from the upstream in five-minute points. Arrow keys read one point out.`;

  protected readonly charts = computed(() =>
    HISTORY_SERIES.map((s) => historyChart(this.points(), s)),
  );

  /** A label every six hours, at the point it names. */
  protected readonly times = computed(() => {
    const pts = this.points();
    const n = pts.length;
    return pts.flatMap((p, i) => {
      const d = new Date(p.start);
      return d.getMinutes() === 0 && d.getHours() % 6 === 0
        ? [{ at: p.start, label: this.time(p.start), percent: ((i + 0.5) / n) * 100 }]
        : [];
    });
  });

  protected readonly readout = computed(() => {
    const i = this.active();
    const p = i === null ? null : this.points()[i];
    if (!p) return '';
    const parts = [
      $localize`:@@usage.history.read-requests:${this.count(p.requests)}:n: requests`,
      $localize`:@@usage.history.read-cached:${this.bytes(p.cachedBytes)}:n: from the cache`,
      $localize`:@@usage.history.read-uncached:${this.bytes(p.uncachedBytes)}:n: from the upstream`,
    ];
    if (p.refused > 0)
      parts.push($localize`:@@usage.history.read-refused:${this.count(p.refused)}:n: refused`);
    const events = p.limitedEvents + p.wouldLimitEvents;
    if (events > 0)
      parts.push($localize`:@@usage.history.read-events:${this.count(events)}:n: limit events`);
    return `${this.time(p.start)}: ${parts.join(' · ')}`;
  });

  protected seriesLabel(s: HistorySeries): string {
    switch (s) {
      case 'requests':
        return $localize`:@@usage.col.requests:Requests`;
      case 'cachedBytes':
        return $localize`:@@usage.col.cached:From the cache`;
      case 'uncachedBytes':
        return $localize`:@@usage.col.uncached:From the upstream`;
    }
  }

  protected tickText(s: HistorySeries, max: number): string {
    return s === 'requests' ? this.count(max) : this.bytes(max);
  }

  protected time(iso: string): string {
    return formatDate(iso, 'shortTime', this.locale);
  }

  protected count(n: number): string {
    return formatCount(n, this.locale);
  }

  protected bytes(n: number): string {
    return formatBytes(n, this.locale);
  }

  /** The point under the pointer, from its place across the plot. */
  protected point(event: PointerEvent): void {
    const svg = event.currentTarget as SVGSVGElement;
    const box = svg.getBoundingClientRect();
    if (box.width === 0) return;
    const x = ((event.clientX - box.left) / box.width) * WIDTH;
    const n = this.points().length;
    const i = Math.floor(((x - LEFT) / (WIDTH - LEFT - RIGHT)) * n);
    this.active.set(i >= 0 && i < n ? i : null);
  }

  protected key(event: KeyboardEvent): void {
    const n = this.points().length;
    if (n === 0) return;
    const now = this.active();
    let next: number;
    if (event.key === 'ArrowRight') next = now === null ? n - 1 : Math.min(n - 1, now + 1);
    else if (event.key === 'ArrowLeft') next = now === null ? n - 1 : Math.max(0, now - 1);
    else if (event.key === 'Home') next = 0;
    else if (event.key === 'End') next = n - 1;
    else if (event.key === 'Escape') {
      this.active.set(null);
      return;
    } else return;
    event.preventDefault();
    this.active.set(next);
  }
}
