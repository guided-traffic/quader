import { DatePipe } from '@angular/common';
import { Component, computed, inject, input, resource } from '@angular/core';
import { LIVE_IDLE_MS, live, refreshResource } from '../../../core/live-refresh';
import { RouterLink } from '@angular/router';
import { PackageSummary, PackagesApi } from '../../../api/packages-api';
import { collectAll } from '../../../api/pagination';
import { splitRef } from '../../../api/repositories-api';
import { failureOf, valueOf } from '../../../shared/paged-resource';
import { DataTable, TableColumn } from '../../../shared/ui/data-table';
import {
  CLAMAV_FORMAT,
  DatabaseVersion,
  ageDays,
  ageText,
  clamavFacts,
  isOutdated,
  outdatedBadge,
  outdatedNote,
  setName,
} from '../../../views/packages/clamav-definitions';
import { packageCommands, versionCommands } from '../../packages/package-links';

/** What tells that a database got a version or lost one. */
function stampOf(packages: readonly PackageSummary[]): string {
  return packages.map((p) => `${p.name} ${p.versionCount} ${p.latestVersion} ${p.updatedAt}`).join('\n');
}

/** One database of the list: its version in use with its build time, and the version of the set before it. */
export interface DatabaseRow {
  readonly name: string;
  readonly versionCount: number;
  /** The version the set in use names; null while none is in use (nothing stored yet, or not marked). */
  readonly inUse: string | null;
  /** The version the set before it names; null where the database's version is the same in both sets or none is marked. */
  readonly previous: string | null;
  readonly builtAt: string | null;
}

/**
 * The databases of `upstream/clamav` on the repository's page (R-UI-194, Q-UI-904): one row per database (`bytecode`, `daily`,
 * `main`), each with the version in use, its build time and its age, `daily` marked **Outdated** past 7 days (Q-UI-912); above the table one muted
 * line names the set in use and, where it differs, the set before it. The versions come with their state (`include=state`), and the
 * one version in use of each database is read for its build time: four reads per database at most, three databases.
 */
@Component({
  selector: 'quader-clamav-databases',
  imports: [DatePipe, DataTable, RouterLink],
  template: `
    @if (sets(); as s) {
      <p class="content-note sets">
        <span><ng-container i18n="@@clamav.set.in-use">Set in use:</ng-container>&ngsp;<code translate="no">{{ s.inUse }}</code></span>
        @if (s.previous) {
          <span><ng-container i18n="@@clamav.set.before">The set before it:</ng-container>&ngsp;<code translate="no">{{ s.previous }}</code></span>
        }
      </p>
    }
    <quader-data-table
      [caption]="caption"
      [columns]="columns"
      [loaded]="rows.hasValue() || failure() !== null"
      [loading]="rows.isLoading()"
      loadingText="Loading the databases."
      i18n-loadingText="@@clamav.databases.loading"
      [error]="failure()"
      (retry)="failure.retry()"
      [empty]="(valueRows() ?? []).length === 0"
      [emptyText]="emptyText"
    >
      @for (r of valueRows() ?? []; track r.name) {
        <tr>
          <th scope="row">
            <span class="icon-name-text">
              <a [routerLink]="databaseLink(r.name)" translate="no">{{ r.name }}</a>
              @if (outdated(r)) {
                <span class="secondary">{{ outdatedText }}</span>
              }
            </span>
          </th>
          <td translate="no">
            @if (r.inUse; as v) {
              <a [routerLink]="versionLink(r.name, v)">{{ v }}</a>
            } @else {
              <span class="muted">–</span>
            }
          </td>
          <td>
            @if (r.builtAt; as at) {
              <time [attr.datetime]="at">{{ at | date: 'medium' }}</time>
            } @else {
              <span class="muted">–</span>
            }
          </td>
          <td class="age-cell">
            @if (age(r); as a) {
              <span class="age">{{ a }}</span>
            } @else {
              <span class="muted">–</span>
            }
            @if (outdated(r)) {
              <span class="badge warn" [attr.title]="outdatedText"
                ><span class="material-icons" aria-hidden="true">warning</span>{{ outdatedWord }}</span
              >
            }
          </td>
          <td class="num">{{ r.versionCount }}</td>
        </tr>
      }
    </quader-data-table>
  `,
  styles: `
    .sets {
      display: flex;
      flex-wrap: wrap;
      gap: var(--quader-space-1) var(--quader-space-4);
    }
    .sets code {
      overflow-wrap: anywhere;
    }
    // The age and its badge are one value that never breaks (Q-UI-563, Q-UI-1064): beside a name's column with room to spare,
    // "8 days" stood over **Outdated** at 1,024 px on the days whose dates of the column Built were a few pixels wider.
    td.age-cell {
      white-space: nowrap;
    }
    .age {
      margin-inline-end: var(--quader-space-2);
    }
  `,
})
export class ClamavDatabases {
  private readonly api = inject(PackagesApi);

  /** The qualified name of the repository, `upstream/clamav`. */
  readonly repository = input.required<string>();
  /** The time the ages count from; the clock where it is not given (a test sets it). */
  readonly now = input<number | null>(null);

  protected readonly caption = $localize`:@@clamav.databases:Databases`;
  protected readonly emptyText = $localize`:@@clamav.databases.empty:Quader has stored no ClamAV definitions yet. The malware scan's updater fills this repository.`;
  protected readonly outdatedWord = outdatedBadge();
  protected readonly outdatedText = outdatedNote();
  protected readonly columns: readonly TableColumn[] = [
    { id: 'database', label: $localize`:@@clamav.col.database:Database` },
    { id: 'in-use', label: $localize`:@@clamav.col.in-use:In use` },
    { id: 'built', label: $localize`:@@clamav.col.built:Built` },
    { id: 'age', label: $localize`:@@clamav.col.age:Age` },
    { id: 'versions', label: $localize`:@@repo.content.versions:Versions`, numeric: true },
  ];

  protected readonly rows = resource({
    params: () => this.repository(),
    loader: async ({ params }): Promise<DatabaseRow[]> => this.read(params, await this.databases(params)),
  });
  /** The databases as the list named them at the last full read: the live read compares its own list with it. */
  private shownStamp = '';

  constructor() {
    // The updater stores a new set every few hours (R-UI-213): the list of databases, one request, is read every minute, and the
    // versions and the set in use only when it says a database changed.
    live({
      reload: async () => {
        const repository = this.repository();
        const packages = await this.databases(repository);
        if (stampOf(packages) === this.shownStamp && this.rows.hasValue()) return;
        await refreshResource(this.rows, () => this.read(repository, packages));
      },
      every: () => LIVE_IDLE_MS,
    });
  }

  private databases(repository: string): Promise<PackageSummary[]> {
    return collectAll(this.api.list({ repository: [repository], format: [CLAMAV_FORMAT], sort: 'name' }));
  }

  private read(repository: string, packages: readonly PackageSummary[]): Promise<DatabaseRow[]> {
    this.shownStamp = stampOf(packages);
    return Promise.all(
      packages.map(async (p): Promise<DatabaseRow> => {
        const versions = await collectAll(this.api.versions(p.id, { repository: [repository], include: ['state'], sort: '-version' }));
        const inUse = versions.find((v) => v.state === 'in_use')?.version ?? null;
        const previous = versions.find((v) => v.state === 'previous')?.version ?? null;
        const facts = inUse ? clamavFacts((await this.api.version(p.id, inUse)).metadata) : null;
        return { name: p.name, versionCount: p.versionCount, inUse, previous, builtAt: facts?.builtAt ?? null };
      }),
    );
  }
  protected readonly valueRows = computed(() => valueOf(this.rows));
  /** The list's failure, kept while it reads again (R-UI-216). */
  protected readonly failure = failureOf(this.rows);

  /** The set in use and, where it differs, the set before it (Q-MAL-82): a database that is not in the set before it keeps its version. */
  protected readonly sets = computed(() => {
    const rows = (this.valueRows() ?? []).filter((r) => r.inUse !== null);
    if (rows.length === 0) return null;
    const inUse = setName(rows.map((r): DatabaseVersion => ({ database: r.name, version: r.inUse! })));
    const previous = setName(rows.map((r): DatabaseVersion => ({ database: r.name, version: r.previous ?? r.inUse! })));
    return { inUse, previous: previous !== inUse && rows.some((r) => r.previous !== null) ? previous : null };
  });

  private clock(): number {
    return this.now() ?? Date.now();
  }

  protected outdated(r: DatabaseRow): boolean {
    return isOutdated(r.name, r.builtAt, this.clock());
  }

  protected age(r: DatabaseRow): string | null {
    const days = ageDays(r.builtAt, this.clock());
    return days === null ? null : ageText(days);
  }

  protected databaseLink(name: string): string[] {
    return packageCommands(splitRef(this.repository()).project, CLAMAV_FORMAT, name);
  }

  protected versionLink(name: string, version: string): string[] {
    return versionCommands(splitRef(this.repository()).project, CLAMAV_FORMAT, name, version);
  }
}
