import { ChangeDetectionStrategy, Component, LOCALE_ID, computed, effect, inject, input, signal, untracked } from '@angular/core';
import { upstreamFirst } from '../../core/upstream-project';
import { Router, RouterLink } from '@angular/router';
import {
  LimitMode,
  LimitsApi,
  UsagePeriod,
  UsageQuery,
  UsageRow,
  UsageScope,
  UsageSort,
  UsageStateFilter,
  UsageSubjectKind,
  usageExportUrl,
} from '../../api/limits-api';
import { collectAll } from '../../api/pagination';
import { ApiError } from '../../api/problem';
import { ProjectsApi } from '../../api/projects-api';
import { ProjectContext } from '../../core/project-context';
import { SelectField, SelectOption } from '../../shared/access/fields';
import { asApiError } from '../../shared/paged-resource';
import { Checkbox } from '../../shared/ui/checkbox';
import { DataTable, TableColumn, TableSort } from '../../shared/ui/data-table';
import { FilterCategory, FilterPanel } from '../../shared/ui/filter-panel';
import { ListChip, ListHead } from '../../shared/ui/list-head';
import { NoValue } from '../../shared/ui/no-value';
import { PageHeader } from '../../shared/ui/page-header';
import { Tooltip } from '../../shared/ui/tooltip';
import { CallerCell } from '../limits/caller-cell';
import { placeText, subjectKindFilterLabel, subjectView } from '../limits/callers';
import { figureName, formatBytes, formatCount } from '../limits/figures';
import { UsageMeter } from './usage-meter';
import { PERIODS, periodLabel, periodPhrase, reachedFilter, stateFilterLabel } from './usage-words';

const KINDS: readonly UsageSubjectKind[] = ['user', 'technicalToken', 'ci', 'certificate', 'anonymous', 'system'];
const SORTS: readonly UsageSort[] = ['requests', 'cachedBytes', 'uncachedBytes', 'upstreamRequests', 'refused', 'utilisation', 'name'];

/** The column a sort is shown on; the caller's column sorts by name, **Now** by the utilisation. */
const SORT_COLUMN: Readonly<Record<UsageSort, string>> = {
  name: 'caller',
  requests: 'requests',
  cachedBytes: 'cached',
  uncachedBytes: 'uncached',
  upstreamRequests: 'upstream',
  refused: 'refused',
  utilisation: 'now',
};

function parsePeriod(v: string | undefined): UsagePeriod {
  return PERIODS.includes(v as UsagePeriod) ? (v as UsagePeriod) : 'day';
}

function parseKinds(v: string | undefined): UsageSubjectKind[] {
  return (v ?? '').split(',').filter((k): k is UsageSubjectKind => KINDS.includes(k as UsageSubjectKind));
}

function parseState(v: string | undefined): UsageStateFilter | undefined {
  return ['close', 'limitReached', 'wouldBeLimited', 'blocked'].includes(v ?? '') ? (v as UsageStateFilter) : undefined;
}

/**
 * The usage overview (R-UI-183, Q-UI-730, Q-UI-732): every caller with its consumption in the period, sortable by each figure,
 * filtered by kind, state and, in the instance, project, with the meter of the tightest hour or day figure now. The address
 * holds the filters, so a link opens the overview filtered (the mode switch's preview, R-UI-182). The instance scope is
 * `/admin/usage` for `usage:read`; the project scope `/projects/{project}/usage` for an Owner, `?repository=` narrows it to one
 * repository.
 */
@Component({
  selector: 'quader-usage-overview',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [CallerCell, Checkbox, DataTable, FilterCategory, FilterPanel, ListHead, NoValue, PageHeader, RouterLink, SelectField, Tooltip, UsageMeter],
  templateUrl: './usage-overview.html',
  styleUrl: './usage.scss',
})
export class UsageOverview {
  private readonly api = inject(LimitsApi);
  private readonly projectsApi = inject(ProjectsApi);
  private readonly router = inject(Router);
  private readonly context = inject(ProjectContext);
  private readonly locale = inject(LOCALE_ID);

  /** `instance` for `/admin/usage`, `project` below a project (route data). */
  readonly scope = input<'instance' | 'project'>('instance');
  /** The project: the route's `:project`, or in the instance scope the filter **Project**. */
  readonly project = input<string | undefined>(undefined);
  readonly repository = input<string | undefined>(undefined);
  readonly period = input<string | undefined>(undefined);
  readonly kind = input<string | undefined>(undefined);
  readonly state = input<string | undefined>(undefined);
  readonly q = input<string | undefined>(undefined);
  readonly sort = input<string | undefined>(undefined);
  readonly order = input<string | undefined>(undefined);

  protected readonly searchLabel = $localize`:@@usage.search:Filter by name or address`;
  protected readonly kinds = KINDS;
  protected readonly periods = PERIODS;

  /** The project scope reads the project's list; the instance's filter **Project** narrows the instance's list (Q-UI-755). */
  protected readonly usageScope = computed<UsageScope>(() =>
    this.scope() === 'instance'
      ? { projectFilter: this.project() ?? null }
      : { project: this.project() ?? null, repository: this.repository() ?? null },
  );
  protected readonly query = computed<UsageQuery>(() => {
    const sort = SORTS.includes(this.sort() as UsageSort) ? (this.sort() as UsageSort) : undefined;
    return {
      period: parsePeriod(this.period()),
      kind: parseKinds(this.kind()),
      state: parseState(this.state()),
      q: this.q()?.trim() || undefined,
      sort,
      order: this.order() === 'asc' ? 'asc' : sort ? 'desc' : undefined,
    };
  });
  protected readonly activePeriod = computed(() => this.query().period ?? 'day');

  protected readonly rows = signal<readonly UsageRow[]>([]);
  protected readonly next = signal<string | null>(null);
  protected readonly mode = signal<LimitMode>('audit');
  protected readonly loaded = signal(false);
  protected readonly loading = signal(false);
  protected readonly failure = signal<ApiError | null>(null);
  private generation = 0;

  /** The staged values of the drawer. */
  protected readonly stagedKinds = signal<readonly UsageSubjectKind[]>([]);
  protected readonly stagedState = signal('');
  protected readonly stagedProject = signal('');
  protected readonly panelOpen = signal(false);
  protected readonly projectOptions = signal<readonly SelectOption[]>([]);

  protected readonly heading = computed(() => {
    if (this.scope() === 'instance') return $localize`:@@usage.heading:Usage`;
    const repo = this.repository();
    if (repo) return $localize`:@@usage.heading-repository:Usage of ${repo}:repository:`;
    const name = this.context.current()?.displayName || this.project() || '';
    return $localize`:@@usage.heading-project:Usage of ${name}:project:`;
  });

  protected readonly chips = computed<readonly ListChip[]>(() => {
    const q = this.query();
    const out: ListChip[] = [];
    if (q.kind?.length) {
      out.push({ key: 'kind', label: $localize`:@@usage.filter.kind:Kind`, value: q.kind.map(subjectKindFilterLabel).join(', ') });
    }
    if (q.state) out.push({ key: 'state', label: $localize`:@@usage.filter.state:State`, value: stateFilterLabel(q.state, this.mode()) });
    if (this.scope() === 'instance' && this.project()) {
      out.push({ key: 'project', label: $localize`:@@usage.filter.project:Project`, value: this.project()! });
    }
    return out;
  });

  protected readonly stateOptions = computed<readonly SelectOption[]>(() => [
    { value: '', label: $localize`:@@usage.filter.any:Any` },
    { value: 'close', label: stateFilterLabel('close', this.mode()) },
    { value: reachedFilter(this.mode()), label: stateFilterLabel(reachedFilter(this.mode()), this.mode()) },
    { value: 'blocked', label: stateFilterLabel('blocked', this.mode()) },
  ]);

  protected readonly tableSort = computed<TableSort | null>(() => {
    const q = this.query();
    return q.sort ? { column: SORT_COLUMN[q.sort], direction: q.order ?? 'desc' } : null;
  });

  protected readonly columns = computed<readonly TableColumn[]>(() => [
    { id: 'caller', label: $localize`:@@usage.col.caller:Caller`, width: '9rem', prefer: true, sortable: true },
    { id: 'requests', label: $localize`:@@usage.col.requests:Requests`, numeric: true, sortable: true },
    { id: 'cached', label: $localize`:@@usage.col.cached:From the cache`, numeric: true, sortable: true },
    { id: 'uncached', label: $localize`:@@usage.col.uncached:From the upstream`, numeric: true, sortable: true },
    { id: 'upstream', label: $localize`:@@usage.col.upstream:Upstream requests`, numeric: true, sortable: true, narrow: true },
    { id: 'refused', label: $localize`:@@usage.col.refused:Refused`, numeric: true, sortable: true, narrow: true },
    // The meter is as wide as its figure and its state, no least width. Upstream requests and Refused give way below 80 rem of the
    // list (narrowBelow, Q-UI-760): beside the side bar at 1440 px the seven figure headers leave the caller no room.
    { id: 'now', label: $localize`:@@usage.col.now:Now`, sortable: true },
  ]);

  protected readonly emptyText = computed(() => {
    const q = this.query();
    if (q.q || q.kind?.length || q.state || (this.scope() === 'instance' && this.project())) {
      return $localize`:@@usage.empty.filtered:No caller matches.`;
    }
    if (this.scope() === 'project' && !this.repository() && q.period === 'day') {
      return $localize`:@@usage.empty.project:Nobody downloaded from this project in the last 24 hours.`;
    }
    return $localize`:@@usage.empty:No downloads ${periodPhrase(q.period ?? 'day')}:period:.`;
  });

  protected readonly exportUrl = computed(() => usageExportUrl(this.usageScope(), this.query()));
  protected readonly reachedState = computed(() => reachedFilter(this.mode()));

  constructor() {
    effect(() => {
      const scope = this.usageScope();
      const query = this.query();
      untracked(() => void this.load(scope, query));
    });
    effect(() => {
      if (this.scope() === 'instance') untracked(() => void this.loadProjects());
    });
  }

  private async loadProjects(): Promise<void> {
    try {
      const all = upstreamFirst(await collectAll(this.projectsApi.list({})), (p) => p.key);
      this.projectOptions.set([
        { value: '', label: $localize`:@@usage.filter.any:Any` },
        ...all.map((p) => ({ value: p.key, label: p.displayName || p.key })),
      ]);
    } catch {
      this.projectOptions.set([]);
    }
  }

  protected async load(scope = this.usageScope(), query = this.query()): Promise<void> {
    const generation = ++this.generation;
    this.loading.set(true);
    this.failure.set(null);
    try {
      const page = await this.api.usage(scope, query, null);
      if (generation !== this.generation) return;
      this.rows.set(page.items);
      this.next.set(page.nextCursor);
      this.mode.set(page.mode);
      this.loaded.set(true);
    } catch (e) {
      if (generation !== this.generation) return;
      this.failure.set(asApiError(e));
      this.rows.set([]);
    } finally {
      if (generation === this.generation) this.loading.set(false);
    }
  }

  protected async more(): Promise<void> {
    const cursor = this.next();
    if (!cursor || this.loading()) return;
    const generation = this.generation;
    this.loading.set(true);
    try {
      const page = await this.api.usage(this.usageScope(), this.query(), cursor);
      if (generation !== this.generation) return;
      this.rows.update((r) => [...r, ...page.items]);
      this.next.set(page.nextCursor);
    } catch (e) {
      this.failure.set(asApiError(e));
    } finally {
      this.loading.set(false);
    }
  }

  /** Changes the address's filters; the list follows the address. */
  protected navigate(params: Record<string, string | null>): void {
    void this.router.navigate([], { queryParams: params, queryParamsHandling: 'merge' });
  }

  protected setPeriod(p: UsagePeriod): void {
    this.navigate({ period: p === 'day' ? null : p });
  }

  protected searchFor(text: string): void {
    this.navigate({ q: text || null });
  }

  protected setSort(sort: TableSort): void {
    const entry = (Object.entries(SORT_COLUMN) as [UsageSort, string][]).find(([, c]) => c === sort.column);
    if (entry) this.navigate({ sort: entry[0], order: sort.direction });
  }

  protected openedDrawer(): void {
    this.stagedKinds.set(this.query().kind ?? []);
    this.stagedState.set(this.query().state ?? '');
    this.stagedProject.set(this.scope() === 'instance' ? (this.project() ?? '') : '');
  }

  protected toggleKind(kind: UsageSubjectKind, on: boolean): void {
    this.stagedKinds.update((list) => (on ? [...list.filter((k) => k !== kind), kind] : list.filter((k) => k !== kind)));
  }

  protected applyFilters(): void {
    const params: Record<string, string | null> = {
      kind: this.stagedKinds().length ? KINDS.filter((k) => this.stagedKinds().includes(k)).join(',') : null,
      state: this.stagedState() || null,
    };
    if (this.scope() === 'instance') params['project'] = this.stagedProject() || null;
    this.navigate(params);
  }

  protected clearFilters(): void {
    const params: Record<string, string | null> = { kind: null, state: null };
    if (this.scope() === 'instance') params['project'] = null;
    this.stagedKinds.set([]);
    this.stagedState.set('');
    this.stagedProject.set('');
    this.navigate(params);
    this.panelOpen.set(false);
  }

  protected removeChip(chip: ListChip): void {
    this.navigate({ [chip.key]: null });
  }

  protected setStateView(state: UsageStateFilter | null): void {
    this.navigate({ state });
  }

  protected kindLabel(kind: UsageSubjectKind): string {
    return subjectKindFilterLabel(kind);
  }

  protected periodText(p: UsagePeriod): string {
    return periodLabel(p);
  }

  protected caller(r: UsageRow) {
    return subjectView(r.subject);
  }

  /** The caller's page in the scope of the overview (R-UI-184). */
  protected link(r: UsageRow): readonly unknown[] {
    return this.scope() === 'project'
      ? ['/projects', this.project(), 'usage', r.subject.kind, r.subject.id]
      : ['/admin/usage', r.subject.kind, r.subject.id];
  }

  protected count(n: number): string {
    return formatCount(n, this.locale);
  }

  protected bytes(n: number): string {
    return formatBytes(n, this.locale);
  }

  protected exactBytes(n: number): string {
    return $localize`:@@usage.exact-bytes:${formatCount(n, this.locale)}:bytes: bytes`;
  }

  /** The meter's tooltip: "From the cache per hour at the instance: 82 %". */
  protected meterTooltip(r: UsageRow): string {
    const u = r.utilisation;
    if (!u) return '';
    const percent = Math.round(u.ratio * 100);
    return $localize`:@@usage.meter-tooltip:${figureName(u.figure)}:figure: ${placeText(u.place)}:place:: ${percent}:percent: % of the limit`;
  }

  protected meterLabel(r: UsageRow): string {
    const u = r.utilisation;
    return u ? `${figureName(u.figure)} ${placeText(u.place)}` : '';
  }
}
