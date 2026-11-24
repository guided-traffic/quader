import { DecimalPipe } from '@angular/common';
import { Component, computed, inject, input, output, resource, signal } from '@angular/core';
import { LIVE_IDLE_MS, live, refreshResource } from '../../core/live-refresh';
import { SEVERITIES, VulnerabilitiesApi, VulnerabilitySeverity, VulnerabilityTrend } from '../../api/vulnerabilities-api';
import { ErrorNotice } from '../../shared/error-notice';
import { asApiError, valueOf } from '../../shared/paged-resource';
import { severityIcon, severityLabel } from './vuln-labels';

export type TrendRange = '30' | '90' | '365';
export const TREND_RANGES: readonly TrendRange[] = ['30', '90', '365'];

const WIDTH = 640;
const HEIGHT = 200;
const LEFT = 40;
const TOP = 8;
const BOTTOM = 24;

/** Bottom to top of a column: the worst level sits on the axis, so its height reads without adding up. */
const STACK: readonly VulnerabilitySeverity[] = ['critical', 'high', 'medium', 'low', 'none', 'unknown'];

/** Each chart's hatch of Unknown needs a pattern ID of its own in the document. */
let next = 0;

export interface TrendSeries {
  readonly severity: VulnerabilitySeverity;
  /** The `d` of one path: one rectangle per day. */
  readonly path: string;
  /**
   * The `d` of the band's edge: a line on the band's top on every day the severity has a finding, drawn 1.5 px wide whatever the
   * scale, so a band thinner than a pixel stays visible. It lies on the true top: the heights, and so the totals the axis
   * shows, stay exact; the line reaches 0.75 px into the band above it.
   */
  readonly edge: string;
  readonly total: number;
}

export interface TrendGeometry {
  readonly series: readonly TrendSeries[];
  readonly yMax: number;
  readonly ticks: readonly { readonly value: number; readonly y: number }[];
  readonly width: number;
  readonly height: number;
  readonly left: number;
  readonly right: number;
  /**
   * The columns touch (365 days: a column of 1.6 viewBox units, no gap). The hairline in the surface's colour that parts the
   * columns of 30 and 90 days is left out then: around so narrow a column it striped every band and washed the pale fills out.
   */
  readonly touching: boolean;
}

/** A round upper bound for the axis: 5, 10, 20, 50, 100, … never below the largest total. */
export function niceMax(value: number): number {
  if (value <= 5) return 5;
  const power = 10 ** Math.floor(Math.log10(value));
  for (const m of [1, 2, 5, 10]) if (value <= m * power) return m * power;
  return 10 * power;
}

/** The bars of the stacked chart in viewBox units (exported for the specs). */
export function trendGeometry(trend: VulnerabilityTrend): TrendGeometry {
  const n = trend.points.length;
  const totals = trend.points.map((p) => SEVERITIES.reduce((sum, s) => sum + p.counts[s], 0));
  const yMax = niceMax(Math.max(0, ...totals));
  const plotW = WIDTH - LEFT - 4;
  const plotH = HEIGHT - TOP - BOTTOM;
  const slot = n === 0 ? plotW : plotW / n;
  const touching = slot <= 4;
  const barW = Math.max(0.5, slot - (touching ? 0 : 1));
  const yOf = (v: number): number => TOP + plotH - (v / yMax) * plotH;
  const series = STACK.map((severity): TrendSeries => {
    let d = '';
    let edge = '';
    let total = 0;
    trend.points.forEach((p, i) => {
      const below = STACK.slice(0, STACK.indexOf(severity)).reduce((sum, s) => sum + p.counts[s], 0);
      const v = p.counts[severity];
      total += v;
      if (v <= 0) return;
      const x = LEFT + i * slot;
      const top = yOf(below + v);
      const h = yOf(below) - top;
      d += `M${x.toFixed(2)} ${top.toFixed(2)}h${barW.toFixed(2)}v${h.toFixed(2)}h${(-barW).toFixed(2)}z`;
      edge += `M${x.toFixed(2)} ${top.toFixed(2)}h${barW.toFixed(2)}`;
    });
    return { severity, path: d, edge, total };
  });
  const ticks = [0, yMax / 2, yMax].map((value) => ({ value, y: yOf(value) }));
  return { series, yMax, ticks, width: WIDTH, height: HEIGHT, left: LEFT, right: WIDTH - 4, touching };
}

/**
 * The trend of the open findings by severity (R-VULN-022): a stacked column per day, drawn here as an SVG (no chart
 * library, Q-VULN-53), over 30, 90 or 365 days, its axis's numbers as text of the page beside the drawing at the app's small text
 * size whatever the drawing's scale (Q-UI-1062), with a "Show as table" alternative that carries every number. The
 * chart is an image with a text alternative; the table is the accessible form of the data. Its colours are the severity
 * bar's (one scale, R-UI-092): the bands its fills, each with an edge in the severity's mark tone, Unknown its hatch. The
 * edges are drawn after every band, the worst last, so where two thin bands meet the worse one stays on top.
 */
@Component({
  selector: 'quader-vuln-trend',
  imports: [DecimalPipe, ErrorNotice],
  template: `
    <section class="vuln-trend" aria-labelledby="vuln-trend-title">
      <div class="vuln-trend-head">
        <h2 id="vuln-trend-title" i18n="@@vuln.trend.title">Open findings over time</h2>
        <div class="vuln-range" role="group" aria-label="Period" i18n-aria-label="@@vuln.trend.period">
          @for (r of ranges; track r) {
            <button class="btn btn-default" type="button" [attr.aria-pressed]="range() === r" (click)="rangeChange.emit(r)">
              {{ rangeLabel(r) }}
            </button>
          }
        </div>
        <button class="btn btn-tertiary" type="button" [attr.aria-expanded]="asTable()" aria-controls="vuln-trend-body" (click)="asTable.set(!asTable())">
          @if (asTable()) {
            <ng-container i18n="@@vuln.trend.show-chart">Show as chart</ng-container>
          } @else {
            <ng-container i18n="@@vuln.trend.show-table">Show as table</ng-container>
          }
        </button>
      </div>
      <div id="vuln-trend-body">
        @if (failure(); as f) {
          <quader-error-notice [error]="f" (retry)="trend.reload()" />
        } @else if (!trend.hasValue()) {
          <p role="status" i18n="@@vuln.trend.loading">Loading the trend.</p>
        } @else if (asTable()) {
          <div class="trend-table-scroll" tabindex="0" role="region" aria-label="Open findings per day" i18n-aria-label="@@vuln.trend.table-label">
            <table class="data trend-table">
              <thead>
                <tr>
                  <th scope="col" i18n="@@vuln.trend.col-date">Date</th>
                  @for (s of stack; track s) {
                    <th scope="col" class="num">{{ label(s) }}</th>
                  }
                </tr>
              </thead>
              <tbody>
                @for (p of points(); track p.date) {
                  <tr>
                    <th scope="row">
                      <time [attr.datetime]="p.date">{{ p.date }}</time>
                    </th>
                    @for (s of stack; track s) {
                      <td class="num">{{ p.counts[s] | number }}</td>
                    }
                  </tr>
                }
              </tbody>
            </table>
          </div>
        } @else if (geometry(); as g) {
          <div class="trend-figure">
            <!-- The axis's numbers are text of the page, not of the drawing: in the SVG they scaled with it, to about 6 px at a phone's
                 width. The chart's alternative is its description and the table, so they are hidden from assistive technology. -->
            <div class="trend-axis" aria-hidden="true">
              <span class="trend-axis-sizer">{{ g.yMax | number }}</span>
              @for (t of g.ticks; track t.value) {
                <span class="trend-tick" [style.top.%]="(t.y / g.height) * 100">{{ t.value | number }}</span>
              }
            </div>
            <svg class="trend-chart" [class.touching]="g.touching" [attr.viewBox]="g.left + ' 0 ' + (g.right - g.left) + ' ' + g.height" role="img" aria-labelledby="vuln-trend-title vuln-trend-desc">
              <desc id="vuln-trend-desc">{{ description() }}</desc>
              @for (t of g.ticks; track t.value) {
                <line class="trend-grid" [attr.x1]="g.left" [attr.x2]="g.right" [attr.y1]="t.y" [attr.y2]="t.y" />
              }
              <defs>
                <pattern [attr.id]="hatchId" patternUnits="userSpaceOnUse" width="4" height="4" patternTransform="rotate(45)">
                  <rect class="trend-hatch-b" width="4" height="4" />
                  <rect class="trend-hatch-a" width="2" height="4" />
                </pattern>
              </defs>
              @for (s of g.series; track s.severity) {
                @if (s.path) {
                  <path
                    [class]="'trend-series trend-' + s.severity"
                    [attr.fill]="s.severity === 'unknown' ? 'url(#' + hatchId + ')' : null"
                    [attr.d]="s.path"
                  />
                }
              }
              @for (s of edges(); track s.severity) {
                @if (s.edge) {
                  <path [class]="'trend-edge trend-' + s.severity" [attr.d]="s.edge" />
                }
              }
            </svg>
          </div>
          <ul class="trend-legend" aria-label="Legend" i18n-aria-label="@@vuln.trend.legend">
            @for (s of stack; track s) {
              <li>
                <span class="trend-swatch" [class]="'trend-swatch trend-' + s" aria-hidden="true"></span>
                <span class="material-icons" aria-hidden="true">{{ icon(s) }}</span>
                <span>{{ label(s) }}</span>
              </li>
            }
          </ul>
          <p class="hint">{{ basis() }}</p>
        }
      </div>
    </section>
  `,
})
export class ReportTrend {
  private readonly api = inject(VulnerabilitiesApi);

  /** The projects whose findings are counted (Q-UI-129). */
  readonly project = input<readonly string[]>([]);
  readonly repository = input<readonly string[]>([]);
  readonly format = input<readonly string[]>([]);
  readonly range = input<TrendRange>('90');
  readonly refresh = input(0);
  readonly rangeChange = output<TrendRange>();

  protected readonly ranges = TREND_RANGES;
  protected readonly stack = STACK;
  protected readonly asTable = signal(false);
  protected readonly hatchId = `quader-trend-hatch-${next++}`;

  protected readonly trend = resource({
    params: () => ({ project: this.project(), repository: this.repository(), format: this.format(), range: this.range(), refresh: this.refresh() }),
    loader: ({ params }) => this.read(params),
  });

  constructor() {
    // Today's point moves with every scan: read again every minute in place while the page is seen (R-UI-213).
    live({
      reload: () =>
        refreshResource(this.trend, () =>
          this.read({ project: this.project(), repository: this.repository(), format: this.format(), range: this.range() }),
        ),
      every: () => LIVE_IDLE_MS,
    });
  }

  private read(params: {
    readonly project: readonly string[];
    readonly repository: readonly string[];
    readonly format: readonly string[];
    readonly range: TrendRange;
  }) {
    const to = new Date();
    const from = new Date(to.getTime() - (Number(params.range) - 1) * 86_400_000);
    const day = (d: Date): string => d.toISOString().slice(0, 10);
    return this.api.trend({ from: day(from), to: day(to), project: params.project, repository: params.repository, format: params.format });
  }
  protected readonly failure = computed(() => {
    const e = this.trend.error();
    return e ? asApiError(e) : null;
  });
  protected readonly basis = computed(() => valueOf(this.trend)?.basis ?? '');
  protected readonly points = computed(() => valueOf(this.trend)?.points ?? []);
  protected readonly geometry = computed(() => {
    const t = valueOf(this.trend);
    return t ? trendGeometry(t) : null;
  });
  /** The edges in drawing order: the top of the stack first, Critical last, so the worst is on top where edges overlap. */
  protected readonly edges = computed(() => [...(this.geometry()?.series ?? [])].reverse());
  /** The text alternative of the image: the span and the numbers of the last day. */
  protected readonly description = computed(() => {
    const t = valueOf(this.trend);
    const last = t?.points[t.points.length - 1];
    if (!t || !last) return $localize`:@@vuln.trend.empty:No data in this period.`;
    const parts = STACK.map((s) => `${severityLabel(s)} ${last.counts[s]}`).join(', ');
    return $localize`:@@vuln.trend.description:Stacked columns of the open findings per day from ${t.from}:from: to ${t.to}:to:. On ${t.to}:last: there were ${parts}:counts:.`;
  });

  protected label(s: VulnerabilitySeverity): string {
    return severityLabel(s);
  }

  protected icon(s: VulnerabilitySeverity): string {
    return severityIcon(s);
  }

  protected rangeLabel(r: TrendRange): string {
    switch (r) {
      case '30':
        return $localize`:@@vuln.trend.range-30:30 days`;
      case '90':
        return $localize`:@@vuln.trend.range-90:90 days`;
      default:
        return $localize`:@@vuln.trend.range-365:365 days`;
    }
  }
}
