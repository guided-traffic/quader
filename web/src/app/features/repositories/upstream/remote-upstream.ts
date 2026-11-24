import { Component, computed, effect, inject, input, output, resource, signal, untracked } from '@angular/core';
import { toSignal } from '@angular/core/rxjs-interop';
import { ActivatedRoute, Router, RouterLink } from '@angular/router';
import { firstValueFrom, map } from 'rxjs';
import { ApiError } from '../../../api/problem';
import { UpstreamCatalogue, UpstreamCatalogueApi, UpstreamHit, UpstreamUnknownLevel } from '../../../api/upstream-catalogue-api';
import { SessionStore } from '../../../core/session-store';
import { ErrorNotice } from '../../../shared/error-notice';
import { asApiError, valueOf } from '../../../shared/paged-resource';
import { Checkbox } from '../../../shared/ui/checkbox';
import { DataTable, TableColumn } from '../../../shared/ui/data-table';
import { QuaderDialog } from '../../../shared/ui/dialog';
import { NoValue } from '../../../shared/ui/no-value';
import { ContentShape } from '../content/content-shape';
import { DeliveryRequestsPanel } from './delivery-requests-panel';
import { DeliveryRulesData, DeliveryRulesDialog } from './delivery-rules-dialog';
import { UpstreamActions } from './upstream-actions';
import { UpstreamPackageView, groupedCount } from './upstream-package';
import {
  catalogueState,
  levelKindText,
  modeSentence,
  nameRuleText,
  searchHintText,
  sectionExample,
  stateBadge,
  suggestionText,
  unknownLevelText,
} from './upstream-words';

/** One page of packages the tab lists: a search's hits or a level's packages, with what the answer says about them. */
interface ListPage {
  readonly hits: readonly UpstreamHit[];
  readonly levels: readonly string[];
  readonly total: number | null;
  readonly truncated: boolean;
  readonly withheld: number | null;
  readonly next: string | null;
  /** The upstream does not have the level (Q-CUR-165); null for a search and for a level it has or does not tell apart. */
  readonly unknown: UpstreamUnknownLevel | null;
}

/** The query of the tab, in its address (R-CUR-018): a search text, a package looked up by name, a level, names only. */
interface UpstreamQuery {
  readonly q: string;
  readonly name: string;
  readonly level: readonly string[];
  readonly namesOnly: boolean;
}

function levelOf(param: string | null): readonly string[] {
  return (param ?? '').split('/').filter((p) => p !== '').map((p) => decodeURIComponent(p));
}

/**
 * The tab **Upstream** of a remote repository (remote-curation.md R-CUR-018, `…/repositories/{key}/upstream`): the line about the
 * delivery mode with **Change** and **Rules (n)**, the requests set apart, the search of the upstream where it answers one (or of the
 * index Quader holds, with **Names only**), the lookup by exact name, which is all an upstream without a search offers and which says
 * so, the levels where the upstream has them, and the packages, each opened in place to its versions and acts. Everything a person
 * types is in the address (`q`, `name`, `level`, `namesOnly`), so the refusal's link of R-CUR-004 (`?name=`) opens the package.
 */
@Component({
  selector: 'quader-remote-upstream',
  imports: [Checkbox, DataTable, DeliveryRequestsPanel, ErrorNotice, NoValue, RouterLink, UpstreamPackageView],
  providers: [UpstreamActions],
  templateUrl: './remote-upstream.html',
  styleUrl: './remote-upstream.scss',
})
export class RemoteUpstream {
  private readonly api = inject(UpstreamCatalogueApi);
  private readonly session = inject(SessionStore);
  private readonly router = inject(Router);
  private readonly route = inject(ActivatedRoute);
  private readonly dialog = inject(QuaderDialog);
  protected readonly actions = inject(UpstreamActions);

  /** The qualified name `<project>/<key>` of the remote repository. */
  readonly repository = input.required<string>();
  readonly project = input.required<string>();
  readonly format = input.required<string>();
  readonly shape = input.required<ContentShape>();
  /** The action a client's fill needs here (`remote.cacheFillAction`); a reader's view does not name it, then `read`. */
  readonly fillAction = input<'read' | 'deploy'>('read');
  /** The capabilities after every answer, for the tab's label in the strip (`Upstream 3`). */
  readonly capsChange = output<UpstreamCatalogue>();

  private readonly query = toSignal(
    this.route.queryParamMap.pipe(
      map((q): UpstreamQuery => ({ q: q.get('q') ?? '', name: q.get('name') ?? '', level: levelOf(q.get('level')), namesOnly: q.get('namesOnly') === 'true' })),
    ),
    { initialValue: { q: '', name: '', level: [], namesOnly: false } as UpstreamQuery },
  );
  protected readonly q = computed(() => this.query().q.trim());
  protected readonly lookupName = computed(() => this.query().name.trim());
  protected readonly level = computed(() => this.query().level);
  protected readonly namesOnly = computed(() => this.query().namesOnly);
  /** The text of the field: what the address holds until the person types. */
  protected readonly text = signal('');

  protected readonly capsRead = resource({
    params: () => ({ repository: this.repository(), changed: this.actions.changed() }),
    loader: ({ params }) => this.api.caps(params.repository),
  });
  /** The last capabilities stay while they are read again after a write. */
  private lastCaps: UpstreamCatalogue | undefined;
  protected readonly caps = computed(() => {
    const c = valueOf(this.capsRead);
    if (c) this.lastCaps = c;
    return c ?? this.lastCaps;
  });
  protected readonly capsFailure = computed(() => {
    const e = this.capsRead.error();
    return e ? asApiError(e) : null;
  });
  protected readonly capsState = computed(() => {
    const f = this.capsFailure();
    return f ? catalogueState(f) : null;
  });

  /** The upstream answers a text query, by itself or over Quader's index. */
  protected readonly searches = computed(() => {
    const c = this.caps();
    return !!c && c.search !== 'none';
  });
  /** The sentence under the search field, in the screen's language, by the code the server sends (Q-CUR-166). */
  protected readonly hintText = computed(() => searchHintText(this.caps()?.searchHint));
  /** The levels above a package, outermost first; none where the upstream lists none. */
  protected readonly levelKinds = computed(() => this.caps()?.levels ?? []);
  protected readonly hasLevels = computed(() => this.levelKinds().length > 0 && !this.caps()?.levelsReason);

  protected readonly modeText = computed(() => {
    const c = this.caps();
    return c ? modeSentence(c.mode) : null;
  });
  /** **Change** leads to the settings' section Delivery for those who may set the configuration (R-CUR-005). */
  protected readonly mayChangeMode = computed(
    () => this.session.has('repositories:write-config') || this.session.canWriteRepositories(this.project()),
  );
  protected readonly settingsLink = computed(() => {
    const slash = this.repository().indexOf('/');
    return ['/projects', this.project(), 'repositories', this.repository().slice(slash + 1), 'settings'];
  });

  // ---- The list: a search's hits or a level's packages ----

  protected readonly page = signal<ListPage | null>(null);
  protected readonly listLoading = signal(false);
  protected readonly listFailure = signal<ApiError | null>(null);
  protected readonly listState = computed(() => {
    const f = this.listFailure();
    return f ? catalogueState(f) : null;
  });
  private generation = 0;
  private shownKey = '';
  protected readonly opened = signal<ReadonlySet<string>>(new Set());

  /** What the list shows: a search, a level, or nothing (yet, or beside a package looked up). */
  protected readonly listing = computed<'search' | 'level' | null>(() => {
    // A package looked up stands alone; **Close** returns to the list.
    if (this.lookupName() !== '') return null;
    if (this.q() !== '' && this.searches()) return 'search';
    if (this.hasLevels()) return 'level';
    return null;
  });

  /** The root of the levels lists nothing (an npm scope, a Docker Hub namespace is entered by its name, Q-CUR-63): no list. */
  protected readonly emptyRoot = computed(() => {
    const p = this.page();
    return this.listing() === 'level' && this.level().length === 0 && !!p && p.levels.length === 0 && p.hits.length === 0;
  });

  protected readonly badge = stateBadge;
  protected readonly kindText = levelKindText;
  protected readonly count = groupedCount;

  protected readonly caption = computed(() =>
    this.listing() === 'search'
      ? $localize`:@@upstream.results.caption:Packages of the upstream that match the search`
      : $localize`:@@upstream.level.caption:Packages of the upstream at this level`,
  );
  protected readonly columns = computed<readonly TableColumn[]>(() => [
    { id: 'name', label: this.shape().itemLabel, width: '14rem', prefer: true },
    { id: 'latest', label: $localize`:@@upstream.col.latest:Latest` },
    { id: 'state', label: $localize`:@@upstream.col.state:State` },
    { id: 'stored', label: $localize`:@@upstream.col.stored:Cache` },
  ]);

  constructor() {
    effect(() => {
      this.actions.repository.set(this.repository());
      this.actions.fillAction.set(this.fillAction());
      this.actions.format.set(this.format());
    });
    effect(() => {
      const c = valueOf(this.capsRead);
      if (c) untracked(() => this.capsChange.emit(c));
    });
    // The field shows what the address holds: a search's text, else the name looked up.
    effect(() => {
      const value = this.q() || this.lookupName();
      untracked(() => this.text.set(value));
    });
    effect(() => {
      const listing = this.listing();
      const q = this.q();
      const level = this.level();
      const namesOnly = this.namesOnly();
      this.actions.changed();
      untracked(() => {
        if (listing === null) {
          this.page.set(null);
          return;
        }
        void this.load(listing, q, level, namesOnly, true);
      });
    });
  }

  private async load(listing: 'search' | 'level', q: string, level: readonly string[], namesOnly: boolean, fresh: boolean): Promise<void> {
    const generation = fresh ? ++this.generation : this.generation;
    const cursor = fresh ? null : (this.page()?.next ?? null);
    if (fresh) {
      this.listFailure.set(null);
    }
    this.listLoading.set(true);
    try {
      let next: ListPage;
      if (listing === 'search') {
        const p = await this.api.search(this.repository(), { q, namesOnly, cursor });
        next = { hits: p.hits, levels: [], total: p.total, truncated: p.truncated, withheld: p.withheld, next: p.done ? null : p.nextCursor, unknown: null };
      } else {
        const p = await this.api.level(this.repository(), level, cursor);
        next = {
          hits: p.packages,
          levels: p.levels,
          total: p.total,
          truncated: false,
          withheld: p.withheld,
          next: p.done ? null : p.nextCursor,
          unknown: p.unknown,
        };
      }
      if (generation !== this.generation) return;
      const before = this.page();
      this.page.set(
        fresh || !before
          ? next
          : { ...next, hits: [...before.hits, ...next.hits], levels: [...before.levels, ...next.levels], withheld: (before.withheld ?? 0) + (next.withheld ?? 0) },
      );
      // A new query closes the rows; a read after a write keeps what is open.
      const key = `${listing} ${q} ${level.join('/')} ${namesOnly}`;
      if (fresh && key !== this.shownKey) this.opened.set(new Set());
      this.shownKey = key;
      this.listFailure.set(null);
    } catch (e) {
      if (generation === this.generation) {
        this.listFailure.set(asApiError(e));
        if (fresh) this.page.set(null);
      }
    } finally {
      if (generation === this.generation) this.listLoading.set(false);
    }
  }

  protected more(): void {
    const listing = this.listing();
    if (listing && this.page()?.next && !this.listLoading()) void this.load(listing, this.q(), this.level(), this.namesOnly(), false);
  }

  protected retryList(): void {
    const listing = this.listing();
    if (listing) void this.load(listing, this.q(), this.level(), this.namesOnly(), true);
  }

  // ---- The address ----

  private navigate(change: Readonly<Record<string, string | null>>): void {
    void this.router.navigate([], { relativeTo: this.route, queryParams: change, queryParamsHandling: 'merge' });
  }

  protected search(event: Event): void {
    event.preventDefault();
    const text = this.text().trim();
    if (text === '') return;
    if (this.searches()) this.navigate({ q: text, name: null });
    else this.navigate({ name: text, q: null });
  }

  protected lookup(): void {
    const text = this.text().trim();
    if (text !== '') this.navigate({ name: text, q: null });
  }

  protected clearLookup(): void {
    this.navigate({ name: null });
  }

  protected setNamesOnly(on: boolean): void {
    this.navigate({ namesOnly: on ? 'true' : null });
  }

  protected levelLink(path: readonly string[]): Record<string, string | null> {
    return { level: path.length > 0 ? path.map((p) => encodeURIComponent(p)).join('/') : null, q: null, name: null };
  }

  /** The path of a child level of the current one. */
  protected childPath(child: string): readonly string[] {
    return [...this.level(), child];
  }

  /** The kind of the level the list shows (`suite` at the root of a Debian remote), or of a level below it. */
  protected kindAt(depth: number): string {
    const kinds = this.levelKinds();
    return kinds[Math.min(depth, kinds.length - 1)] ?? '';
  }

  /** A level entered by its name, where the root lists none (an npm scope, a Docker Hub namespace, Q-CUR-63). */
  protected readonly levelText = signal('');

  protected enterLevel(event: Event): void {
    event.preventDefault();
    const name = this.levelText().trim();
    if (name !== '') {
      this.navigate(this.levelLink([name]));
      this.levelText.set('');
    }
  }

  /** **Download this sub-section…** at a level that holds packages: the preview first (R-CUR-010). */
  protected downloadSection(): void {
    const path = this.level();
    const section = path.map((p) => encodeURIComponent(p)).join('/');
    void this.actions.downloadBulk({ level: 'section', section }, $localize`:@@upstream.preview.title-section:The sub-section ${path.join(' / ')}:section:`);
  }

  // ---- A row ----

  protected isOpen(h: UpstreamHit): boolean {
    return this.opened().has(h.name);
  }

  protected toggle(h: UpstreamHit): void {
    this.opened.update((s) => {
      const next = new Set(s);
      if (next.has(h.name)) next.delete(h.name);
      else next.add(h.name);
      return next;
    });
  }

  protected openLabel(h: UpstreamHit): string {
    return $localize`:@@upstream.row.open:Versions of ${h.name}:name:`;
  }

  protected rowId(h: UpstreamHit): string {
    return 'up-' + h.name.replace(/[^A-Za-z0-9_-]/g, '_');
  }

  // ---- The words of the list ----

  /** "11,235 hits", and where the upstream's window ends before its total, how far it reaches. */
  protected totalText(p: ListPage): string | null {
    if (this.listing() !== 'search' || p.total === null) return null;
    const window = this.caps()?.searchWindow ?? null;
    if (p.truncated && window !== null) {
      return $localize`:@@upstream.results.total-window:${groupedCount(p.total)}:total: hits; the upstream shows at most the first ${groupedCount(window)}:window:`;
    }
    return $localize`:@@upstream.results.total:${groupedCount(p.total)}:total: hits`;
  }

  /** In mode `onlyAllowed` the hits a reader does not see (Q-CUR-14). */
  protected withheldText(p: ListPage): string | null {
    const n = p.withheld ?? 0;
    if (n <= 0) return null;
    return n === 1
      ? $localize`:@@upstream.results.withheld-one:1 hit is not allowed in this repository; look a package up by its exact name to ask for it.`
      : $localize`:@@upstream.results.withheld:${n}:count: hits are not allowed in this repository; look a package up by its exact name to ask for it.`;
  }

  protected emptyText(): string {
    if (this.listing() === 'search') return $localize`:@@upstream.results.empty:No package of the upstream matches “${this.q()}:query:”.`;
    const unknown = this.page()?.unknown;
    const path = this.level();
    if (unknown && path.length > 0) {
      const said = unknownLevelText(this.kindAt(path.length - 1), unknown.upstream, path.at(-1) ?? '');
      const rule = nameRuleText(unknown.rule, unknown.upstream, unknown.forbidden);
      return rule ? `${said} ${rule}` : said;
    }
    return $localize`:@@upstream.level.empty:This level lists no package.`;
  }

  /** The level the entered name may have meant (Q-CUR-165): the current path with its last part replaced; null without one. */
  protected readonly suggestion = computed(() => {
    const s = this.page()?.unknown?.suggestion;
    const path = this.level();
    if (!s || path.length === 0) return null;
    return { text: suggestionText(s), path: [...path.slice(0, -1), s] };
  });

  /** The label of the search field: a search of the upstream, of Quader's index, or a lookup by exact name. */
  protected readonly fieldLabel = computed(() => {
    switch (this.caps()?.search) {
      case 'upstream':
        return $localize`:@@upstream.search.label:Search the upstream`;
      case 'index':
        return $localize`:@@upstream.search.label-index:Search the index`;
      default:
        return $localize`:@@upstream.lookup.label:Look up by name`;
    }
  });

  /** **Rules (n)**: the rule list, read only for `repositories:read-config` (R-CUR-005). */
  protected async openRules(): Promise<void> {
    const ref = this.dialog.open<DeliveryRulesDialog, DeliveryRulesData, boolean>(DeliveryRulesDialog, {
      data: { repository: this.repository(), canChange: this.actions.manager(), example: sectionExample(this.format()) },
      width: '52rem',
      maxWidth: '52rem',
    });
    const changed = await firstValueFrom(ref.closed);
    if (changed) this.actions.changed.update((n) => n + 1);
  }
}
