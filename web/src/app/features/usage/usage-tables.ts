import { ChangeDetectionStrategy, Component, LOCALE_ID, computed, inject, input, signal } from '@angular/core';
import { UsageByCredential, UsageByRepository } from '../../api/limits-api';
import { DataTable, TableColumn, TableSort } from '../../shared/ui/data-table';
import { formatBytes, formatCount } from '../limits/figures';

type RepositorySortKey = 'repository' | 'requests' | 'cachedBytes' | 'uncachedBytes' | 'upstreamRequests' | 'refused';

/** The rows sorted in the browser: the list is the caller's repositories in the period, complete in one answer. */
export function sortRepositories(rows: readonly UsageByRepository[], sort: TableSort): UsageByRepository[] {
  const key = sort.column as RepositorySortKey;
  const dir = sort.direction === 'asc' ? 1 : -1;
  return [...rows].sort((a, b) => {
    const x = a[key];
    const y = b[key];
    return (typeof x === 'string' ? x.localeCompare(y as string) : (x as number) - (y as number)) * dir;
  });
}

/** **By repository** of a caller's page (R-UI-184): every repository it used in the period, sortable. */
@Component({
  selector: 'quader-usage-by-repository',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [DataTable],
  template: `
    <quader-data-table
      caption="By repository"
      i18n-caption="@@usage.by-repository"
      [columns]="columns"
      [sort]="sort()"
      (sortChange)="sort.set($event)"
      [empty]="rows().length === 0"
      [emptyText]="emptyText()"
    >
      @for (r of sorted(); track r.repository) {
        <tr>
          <th scope="row" translate="no">{{ r.repository }}</th>
          <td class="num">{{ count(r.requests) }}</td>
          <td class="num">{{ bytes(r.cachedBytes) }}</td>
          <td class="num">{{ bytes(r.uncachedBytes) }}</td>
          <td class="num narrow-drop">{{ count(r.upstreamRequests) }}</td>
          <td class="num narrow-drop">{{ count(r.refused) }}</td>
        </tr>
      }
    </quader-data-table>
  `,
})
export class UsageByRepositoryTable {
  private readonly locale = inject(LOCALE_ID);
  readonly rows = input.required<readonly UsageByRepository[]>();
  readonly emptyText = input('');
  protected readonly sort = signal<TableSort>({ column: 'requests', direction: 'desc' });
  protected readonly sorted = computed(() => sortRepositories(this.rows(), this.sort()));
  protected readonly columns: readonly TableColumn[] = [
    { id: 'repository', label: $localize`:@@usage.col.repository:Repository`, width: '12rem', prefer: true, sortable: true },
    { id: 'requests', label: $localize`:@@usage.col.requests:Requests`, numeric: true, sortable: true },
    { id: 'cachedBytes', label: $localize`:@@usage.col.cached:From the cache`, numeric: true, sortable: true },
    { id: 'uncachedBytes', label: $localize`:@@usage.col.uncached:From the upstream`, numeric: true, sortable: true },
    { id: 'upstreamRequests', label: $localize`:@@usage.col.upstream:Upstream requests`, numeric: true, sortable: true, narrow: true },
    { id: 'refused', label: $localize`:@@usage.col.refused:Refused`, numeric: true, sortable: true, narrow: true },
  ];

  protected count(n: number): string {
    return formatCount(n, this.locale);
  }

  protected bytes(n: number): string {
    return formatBytes(n, this.locale);
  }
}

/** **By credential** of a user (R-UI-184): the browser session and each personal token by its description and hint. */
@Component({
  selector: 'quader-usage-by-credential',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [DataTable],
  template: `
    <quader-data-table
      caption="By credential"
      i18n-caption="@@usage.by-credential"
      [columns]="columns"
      [empty]="rows().length === 0"
      [emptyText]="emptyText()"
    >
      @for (r of rows(); track r.credential.kind === 'token' ? r.credential.id : 'session') {
        <tr>
          <th scope="row">
            @if (r.credential.kind === 'session') {
              <ng-container i18n="@@usage.credential.session">Browser session</ng-container>
            } @else {
              <span class="token">
                <span>{{ r.credential.description || tokenWord }}</span>
                @if (r.credential.hint) {
                  <code class="secondary" translate="no">{{ r.credential.hint }}…</code>
                }
              </span>
            }
          </th>
          <td class="num">{{ count(r.requests) }}</td>
          <td class="num">{{ bytes(r.cachedBytes) }}</td>
          <td class="num">{{ bytes(r.uncachedBytes) }}</td>
        </tr>
      }
    </quader-data-table>
  `,
  styles: `
    .token {
      display: flex;
      flex-direction: column;
    }
    .secondary {
      color: var(--quader-text-muted);
      font-size: 0.75rem;
    }
  `,
})
export class UsageByCredentialTable {
  private readonly locale = inject(LOCALE_ID);
  readonly rows = input.required<readonly UsageByCredential[]>();
  readonly emptyText = input('');
  protected readonly tokenWord = $localize`:@@usage.credential.token:Personal token`;
  protected readonly columns: readonly TableColumn[] = [
    { id: 'credential', label: $localize`:@@usage.col.credential:Credential`, width: '12rem', prefer: true },
    { id: 'requests', label: $localize`:@@usage.col.requests:Requests`, numeric: true },
    { id: 'cachedBytes', label: $localize`:@@usage.col.cached:From the cache`, numeric: true },
    { id: 'uncachedBytes', label: $localize`:@@usage.col.uncached:From the upstream`, numeric: true },
  ];

  protected count(n: number): string {
    return formatCount(n, this.locale);
  }

  protected bytes(n: number): string {
    return formatBytes(n, this.locale);
  }
}
