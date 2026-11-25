import { CdkMenu, CdkMenuItem, CdkMenuTrigger } from '@angular/cdk/menu';
import {
  Component,
  computed,
  effect,
  inject,
  input,
  resource,
  signal,
  untracked,
} from '@angular/core';
import { LIVE_IDLE_MS, live } from '../../core/live-refresh';
import { Router, RouterLink } from '@angular/router';
import { PackageFilters, PackageSort, PackageSummary, PackagesApi } from '../../api/packages-api';
import { collectAll } from '../../api/pagination';
import { RepositoriesApi, refOf } from '../../api/repositories-api';
import { PagedResource, valueOf } from '../../shared/paged-resource';
import { FormatIconView } from '../../shared/format-icon';
import { DataTable, TableColumn, TableSort } from '../../shared/ui/data-table';
import { FilterCategory, FilterPanel } from '../../shared/ui/filter-panel';
import { ListChip, ListHead } from '../../shared/ui/list-head';
import { TimeAgo } from '../../shared/ui/time-ago';
import { packageCommands } from './package-links';
import { PackageMemory, SavedView } from './package-memory';
import { PageHeader } from '../../shared/ui/page-header';
import { TabStrip } from '../../shared/ui/tab-strip';

type Param = string | readonly string[] | undefined;
type ListTab = 'all' | 'recent' | 'views';
export type CreatedPeriod = '7' | '30' | '90' | 'custom';

const DAY_MS = 86_400_000;

function many(value: Param): string[] {
  return value === undefined ? [] : typeof value === 'string' ? [value] : [...value];
}

/** One chip of a filter that is set, with the query change that removes it. */
interface Chip extends ListChip {
  /** The query change that removes it. */
  readonly remove: Readonly<Record<string, string | readonly string[] | null>>;
}

/**
 * The packages list (R-UI-006), the start page of the packages area. It uses the shared data table and the shared head of a list
 * (Q-UI-420): the search by name or description above the list, the other filters in the drawer, the filters that are set as
 * chips beside **Filters**; the sort is a column header, "Load more" is the table's. Filters, sort and tab live in the query parameters
 * (R-UI-003) and every change restarts the cursor walk, because a cursor is bound to its filters and
 * sort (R-API-010). Filtering and sorting happen on the server (R-API-029); the screen sorts and
 * filters nothing itself. "Recently opened" and "Saved views" are browser-storage features and
 * disappear where storage is blocked.
 */
@Component({
  selector: 'quader-package-list',
  imports: [
    CdkMenu,
    CdkMenuItem,
    CdkMenuTrigger,
    PageHeader,
    RouterLink,
    DataTable,
    FilterCategory,
    FilterPanel,
    FormatIconView,
    ListHead,
    TabStrip,
    TimeAgo,
  ],
  templateUrl: './package-list.html',
  styleUrl: './package-list.scss',
})
export class PackageList {
  private readonly api = inject(PackagesApi);
  private readonly repositories = inject(RepositoriesApi);
  private readonly router = inject(Router);
  protected readonly memory = inject(PackageMemory);

  /** Query parameters, bound by the router's component input binding. */
  /** Route parameter `:project` of `/projects/:project/packages` (Q-UI-129). */
  readonly project = input.required<string>();
  readonly format = input<Param>();
  readonly repository = input<Param>();
  readonly q = input<string | undefined>();
  readonly keyword = input<string | undefined>();
  readonly period = input<string | undefined>();
  readonly from = input<string | undefined>();
  readonly to = input<string | undefined>();
  readonly sort = input<string | undefined>();
  readonly tab = input<string | undefined>();

  protected readonly formats = resource({ loader: () => this.repositories.formats() });
  protected readonly repositoryList = resource({
    params: () => this.project(),
    loader: ({ params }) => collectAll(this.repositories.list({ project: params })),
  });
  /** The address of the list; the tabs and saved views link to it. */
  protected readonly listLink = computed(() => ['/projects', this.project(), 'packages']);
  protected readonly list = new PagedResource<PackageSummary>();

  protected readonly panelOpen = signal(false);
  protected readonly stagedKeyword = signal('');
  protected readonly stagedFormats = signal<readonly string[]>([]);
  protected readonly stagedRepositories = signal<readonly string[]>([]);
  protected readonly stagedPeriod = signal('');
  protected readonly stagedFrom = signal('');
  protected readonly stagedTo = signal('');
  protected readonly viewName = signal('');
  protected readonly renaming = signal<string | null>(null);
  protected readonly renameText = signal('');

  protected readonly activeTab = computed<ListTab>(() => {
    const t = this.tab();
    return this.memory.available() && (t === 'recent' || t === 'views') ? t : 'all';
  });
  protected readonly formatNames = computed(() => many(this.format()));
  protected readonly repositoryNames = computed(() => many(this.repository()));
  protected readonly apiSort = computed<PackageSort>(() => {
    const s = this.sort();
    const known: readonly string[] = ['name', '-name', 'createdAt', '-createdAt', 'updatedAt', '-updatedAt'];
    return s !== undefined && known.includes(s) ? (s as PackageSort) : 'name';
  });
  protected readonly tableSort = computed<TableSort>(() => {
    const s = this.apiSort();
    return { column: s.replace('-', ''), direction: s.startsWith('-') ? 'desc' : 'asc' };
  });

  protected readonly filters = computed<PackageFilters>(() => {
    const range = this.createdRange();
    return {
      project: this.project(),
      format: this.formatNames(),
      repository: this.repositoryNames(),
      q: this.q(),
      keyword: this.keyword(),
      createdFrom: range.from,
      createdTo: range.to,
      sort: this.apiSort(),
    };
  });
  protected readonly chips = computed<readonly Chip[]>(() => {
    const out: Chip[] = [];
    const typeLabel = $localize`:@@pkg.filter.type:Package type`;
    for (const f of this.formatNames()) {
      out.push({
        key: `format:${f}`,
        label: typeLabel,
        value: this.formatLabel(f),
        remove: { format: this.formatNames().filter((x) => x !== f) },
      });
    }
    const repoLabel = $localize`:@@pkg.filter.repository:Repository`;
    for (const r of this.repositoryNames()) {
      out.push({
        key: `repository:${r}`,
        label: repoLabel,
        value: r,
        remove: { repository: this.repositoryNames().filter((x) => x !== r) },
      });
    }
    if (this.keyword()) {
      out.push({
        key: 'keyword',
        label: $localize`:@@pkg.filter.keyword:Keyword`,
        value: this.keyword() ?? '',
        remove: { keyword: null },
      });
    }
    const created = this.createdLabel();
    if (created) {
      out.push({
        key: 'created',
        label: $localize`:@@pkg.filter.created:Created`,
        value: created,
        remove: { period: null, from: null, to: null },
      });
    }
    return out;
  });
  protected readonly filtered = computed(() => this.chips().length > 0 || !!this.q());

  protected readonly formatOptions = computed(() =>
    (valueOf(this.formats) ?? []).filter((f) => f.name !== 'generic'),
  );
  protected readonly repositoryOptions = computed(() =>
    (valueOf(this.repositoryList) ?? []).filter((r) => r.format !== 'generic').map((r) => refOf(r)),
  );

  protected readonly tableCaption = $localize`:@@pkg.list.caption:Packages`;
  protected readonly emptyText = $localize`:@@pkg.list.empty:No package has been published yet.`;
  protected readonly emptyFilteredText = $localize`:@@pkg.list.empty-filtered:No package matches the filters.`;
  protected readonly searchLabel = $localize`:@@pkg.list.search:Filter packages`;
  /**
   * The columns a reader chooses by (Q-UI-314). The name wraps after its hyphens and declares its least (Q-UI-308); the format,
   * the version and the time never break and are as wide as their values (Q-UI-563). Their guessed widths summed to 46 rem, and
   * the list stood as blocks at 1,024 px with the side bar open (R-UI-174).
   */
  protected readonly columns: readonly TableColumn[] = [
    { id: 'name', label: $localize`:@@pkg.col.package:Package`, sortable: true, width: '14rem' },
    { id: 'type', label: $localize`:@@pkg.col.format:Format`, stackLabel: false },
    // The words of the repository page's package table (Q-UI-432): **Latest**, not **Latest version**.
    {
      id: 'latest',
      label: $localize`:@@pkg.col.latest:Latest`,
      help: $localize`:@@pkg.help.latest:The highest stable version you may see. A pre-release is shown only while no stable version exists.`,
    },
    {
      id: 'updatedAt',
      label: $localize`:@@pkg.col.updated:Updated`,
      sortable: true,
      help: $localize`:@@pkg.help.updated:When the latest version you may see was published.`,
    },
  ];
  protected commands = (format: string, name: string): string[] => packageCommands(this.project(), format, name);
  protected readonly periods: readonly { value: CreatedPeriod; label: string }[] = [
    { value: '7', label: $localize`:@@pkg.period.7:Last 7 days` },
    { value: '30', label: $localize`:@@pkg.period.30:Last 30 days` },
    { value: '90', label: $localize`:@@pkg.period.90:Last 90 days` },
    { value: 'custom', label: $localize`:@@pkg.period.custom:Custom period` },
  ];

  private defaultChecked = false;

  /**
   * The list's Retry (R-UI-216): the page of the list that failed, and the reads of the filter drawer's choices (the package types,
   * the repositories) that failed with it; read once and never again, the drawer offered no choice after the page came back.
   */
  protected retry(): void {
    void this.list.more();
    this.rereadChoices();
  }

  private rereadChoices(): void {
    for (const r of [this.formats, this.repositoryList]) if (r.error()) r.reload();
  }

  constructor() {
    // The drawer's choices that failed are read again when it opens (R-UI-216).
    effect(() => {
      if (this.panelOpen()) untracked(() => this.rereadChoices());
    });
    effect(() => {
      const filters = this.filters();
      untracked(() => {
        this.restage();
        void this.list.start(this.api.list(filters));
      });
    });
    // Clients push and pull packages: the rows shown are read again every minute in place while the list is seen (R-UI-213).
    live({ reload: () => this.list.refresh(), every: () => LIVE_IDLE_MS });
    // A list opened without any query lands on the default saved view, once.
    effect(() => {
      const view = this.memory.defaultView();
      if (this.defaultChecked || !view) return;
      this.defaultChecked = true;
      if (!this.filtered() && !this.sort() && !this.tab()) void this.router.navigate([], { queryParams: view.query, replaceUrl: true });
    });
  }

  protected formatLabel(name: string): string {
    return (valueOf(this.formats) ?? []).find((f) => f.name === name)?.displayName ?? name;
  }

  private createdRange(): { from: string | undefined; to: string | undefined } {
    const p = this.period();
    if (p === '7' || p === '30' || p === '90') {
      return { from: new Date(Date.now() - Number(p) * DAY_MS).toISOString(), to: undefined };
    }
    if (p === 'custom') {
      const from = this.from() ? `${this.from()}T00:00:00.000Z` : undefined;
      // The end day is included, the API's upper bound is exclusive.
      const to = this.to() ? new Date(Date.parse(`${this.to()}T00:00:00.000Z`) + DAY_MS).toISOString() : undefined;
      return { from, to };
    }
    return { from: undefined, to: undefined };
  }

  private createdLabel(): string | null {
    const p = this.period();
    if (p === '7' || p === '30' || p === '90') return this.periods.find((x) => x.value === p)?.label ?? null;
    if (p === 'custom' && (this.from() || this.to())) {
      return `${this.from() ?? '…'} – ${this.to() ?? '…'}`;
    }
    return null;
  }

  protected stageFormat(value: string, on: boolean): void {
    this.stagedFormats.update((v) => (on ? [...new Set([...v, value])] : v.filter((x) => x !== value)));
  }

  protected stageRepository(value: string, on: boolean): void {
    this.stagedRepositories.update((v) => (on ? [...new Set([...v, value])] : v.filter((x) => x !== value)));
  }

  /** The drawer's values back to the applied ones: after a change of the query, and when the drawer closes without **Apply**. */
  protected restage(): void {
    this.stagedKeyword.set(this.keyword() ?? '');
    this.stagedFormats.set(this.formatNames());
    this.stagedRepositories.set(this.repositoryNames());
    this.stagedPeriod.set(this.period() ?? '');
    this.stagedFrom.set(this.from() ?? '');
    this.stagedTo.set(this.to() ?? '');
  }

  protected submitFilters(): void {
    const custom = this.stagedPeriod() === 'custom';
    this.navigate({
      format: [...this.stagedFormats()],
      repository: [...this.stagedRepositories()],
      keyword: this.stagedKeyword().trim() || null,
      period: this.stagedPeriod() || null,
      from: custom ? this.stagedFrom() || null : null,
      to: custom ? this.stagedTo() || null : null,
    });
  }

  /** **Clear all** of the drawer: its filters go; the search text is the field's and stays. */
  protected clearFilters(): void {
    this.navigate({ format: null, repository: null, keyword: null, period: null, from: null, to: null });
  }

  /** **Clear all filters** under a list that matches nothing: the search text goes too. */
  protected clearEverything(): void {
    this.navigate({ format: null, repository: null, q: null, keyword: null, period: null, from: null, to: null });
  }

  protected removeChip(chip: ListChip): void {
    const change: Record<string, string | readonly string[] | null> = {};
    for (const [k, v] of Object.entries((chip as Chip).remove)) change[k] = Array.isArray(v) && v.length === 0 ? null : v;
    this.navigate(change);
  }

  protected sortBy(sort: TableSort): void {
    this.navigate({ sort: `${sort.direction === 'desc' ? '-' : ''}${sort.column}` });
  }

  protected saveView(): void {
    const query: Record<string, string | readonly string[]> = {};
    const add = (k: string, v: Param): void => {
      if (v !== undefined && v !== '' && v.length !== 0) query[k] = v;
    };
    add('format', this.format());
    add('repository', this.repository());
    add('q', this.q());
    add('keyword', this.keyword());
    add('period', this.period());
    add('from', this.from());
    add('to', this.to());
    add('sort', this.sort());
    if (this.memory.saveView(this.viewName(), query)) this.viewName.set('');
  }

  /** The menu of a saved view, named by the view (R-UI-163). */
  protected viewActionsLabel(name: string): string {
    return $localize`:@@pkg.views.actions-of:Actions for ${name}:name:`;
  }

  protected startRename(view: SavedView): void {
    this.renaming.set(view.id);
    this.renameText.set(view.name);
  }

  protected finishRename(view: SavedView): void {
    this.memory.renameView(view.id, this.renameText());
    this.renaming.set(null);
  }

  protected navigate(queryParams: Record<string, string | readonly string[] | null>): void {
    void this.router.navigate([], { queryParams, queryParamsHandling: 'merge' });
  }
}
