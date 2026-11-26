import { ChangeDetectionStrategy, Component, LOCALE_ID, computed, inject, input } from '@angular/core';
import { BudgetFigures, UsageBudget } from '../../api/limits-api';
import { DataTable, TableColumn } from '../../shared/ui/data-table';
import { formatBytes, formatCount } from '../limits/figures';
import { placeFilterLabel } from './usage-words';

/** One row of **By place**: where, and what the caller took there in the windows of its budgets. */
export interface PlaceUsage {
  readonly key: string;
  readonly label: string;
  readonly requestsHour: number;
  readonly uncachedHour: number;
  readonly bytesDay: number;
  readonly total: boolean;
}

const used = (f: BudgetFigures, k: keyof BudgetFigures): number => f[k].used ?? 0;

/**
 * A caller's consumption per place (R-UI-242, Q-LIM-306, Q-UI-1488): the main installation's places together as **This installation**,
 * each Bastion's as "Through <name>", and the sum, from the budgets of the caller's detail: requests in the last hour, bytes from the
 * upstream in the last hour, and bytes in the last 24 hours. A request counts in the budget of the one rule that decided it, so the
 * budgets do not overlap and their sum is the caller's. Empty where the caller has no Bastion's budget: then the installation's is all.
 */
export function placeUsage(budgets: readonly UsageBudget[]): PlaceUsage[] {
  if (!budgets.some((b) => b.place.kind === 'bastion')) return [];
  const rows = new Map<string, PlaceUsage>();
  const add = (key: string, label: string, f: BudgetFigures, total = false): void => {
    const r = rows.get(key);
    rows.set(key, {
      key,
      label,
      total,
      requestsHour: (r?.requestsHour ?? 0) + used(f, 'requestsPerHour'),
      uncachedHour: (r?.uncachedHour ?? 0) + used(f, 'uncachedBytesPerHour'),
      bytesDay: (r?.bytesDay ?? 0) + used(f, 'bytesPerDay'),
    });
  };
  for (const b of budgets) {
    if (b.place.kind !== 'bastion') add('installation', placeFilterLabel('installation'), b.figures);
  }
  for (const b of [...budgets].filter((x) => x.place.kind === 'bastion').sort((x, y) => placeName(x).localeCompare(placeName(y)))) {
    add(`bastion:${placeName(b)}`, placeFilterLabel(`bastion:${placeName(b)}`), b.figures);
  }
  for (const b of budgets) add('total', $localize`:@@usage.places.total:All places`, b.figures, true);
  return [...rows.values()];
}

function placeName(b: UsageBudget): string {
  return b.place.kind === 'bastion' ? b.place.bastion : '';
}

/** **By place** of a caller's page (R-UI-242): one row per place and the sum. */
@Component({
  selector: 'quader-usage-places',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [DataTable],
  template: `
    <quader-data-table caption="Usage by place" i18n-caption="@@usage.places.caption" [columns]="columns" [empty]="rows().length === 0">
      @for (r of rows(); track r.key) {
        <tr [class.total]="r.total">
          <th scope="row" translate="no">{{ r.label }}</th>
          <td class="num">{{ count(r.requestsHour) }}</td>
          <td class="num">{{ bytes(r.uncachedHour) }}</td>
          <td class="num">{{ bytes(r.bytesDay) }}</td>
        </tr>
      }
    </quader-data-table>
  `,
  styles: `
    tr.total th,
    tr.total td {
      font-weight: 600;
    }
    /* A stacked row's column names are measured in the regular weight: the sum's names keep it, or the longest broke a line. */
    tr.total td::before {
      font-weight: 400;
    }
  `,
})
export class UsagePlaces {
  private readonly locale = inject(LOCALE_ID);
  readonly budgets = input.required<readonly UsageBudget[]>();

  protected readonly rows = computed(() => placeUsage(this.budgets()));
  protected readonly columns: readonly TableColumn[] = [
    { id: 'place', label: $localize`:@@usage.places.col-place:Place`, width: '10rem', prefer: true },
    { id: 'requests', label: $localize`:@@usage.places.col-requests:Requests in the last hour`, numeric: true },
    { id: 'uncached', label: $localize`:@@usage.places.col-uncached:From the upstream in the last hour`, numeric: true },
    { id: 'day', label: $localize`:@@usage.places.col-day:Bytes in the last 24 hours`, numeric: true },
  ];

  protected count(n: number): string {
    return formatCount(n, this.locale);
  }

  protected bytes(n: number): string {
    return formatBytes(n, this.locale);
  }
}
