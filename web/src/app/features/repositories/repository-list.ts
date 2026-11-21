import { CdkMenu, CdkMenuItem, CdkMenuTrigger } from '@angular/cdk/menu';
import { NgTemplateOutlet } from '@angular/common';
import { VisibilityIcon, levelName } from '../../shared/ui/visibility-level';
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
import { ActivatedRoute, Router, RouterLink } from '@angular/router';
import { newIdempotencyKey } from '../../api/client';
import { JobReference, RepositoriesApi, RepositoryFilters, RepositorySummary, refOf } from '../../api/repositories-api';
import { ApiError } from '../../api/problem';
import { Notifier } from '../../core/notifier';
import { SessionStore } from '../../core/session-store';
import { ProjectContext } from '../../core/project-context';
import {
  CLAMAV_KEY,
  UPSTREAM_PROJECT,
  isSystemRepository,
  isUpstreamProject,
  mayServeClients,
  projectDescription,
  systemBadge,
  systemRepositoryNames,
  upstreamName,
} from '../../core/upstream-project';
import { SystemClients } from '../../core/system-clients';
import { UpstreamApi } from '../../api/upstream-api';
import { stateBadge } from '../upstream/upstream-words';
import { Confirm } from '../../shared/confirm-dialog';
import { Disclosure } from '../../shared/ui/disclosure';
import { PageHeader } from '../../shared/ui/page-header';
import { CopyButton } from '../../shared/copy-button';
import { ErrorNotice } from '../../shared/error-notice';
import { JobProgress } from '../../shared/job-progress';
import { DataTable, TableColumn } from '../../shared/ui/data-table';
import { FilterCategory, FilterPanel } from '../../shared/ui/filter-panel';
import { ListChip, ListHead } from '../../shared/ui/list-head';
import { NoValue } from '../../shared/ui/no-value';
import { ProjectLink } from '../../shared/ui/project-mark';
import { TimeAgo } from '../../shared/ui/time-ago';
import { PagedResource, asApiError, valueOf } from '../../shared/paged-resource';
import { collectAll } from '../../api/pagination';
import { FormatIconView } from '../../shared/format-icon';
import { FrequentPlaces } from '../../core/frequent-places';
import { setupCommands } from '../../core/project-links';
import { classIcon, classLabel } from './repository-class';

/**
 * Whether the list's filters admit a repository, by the rules the server applies to the list (R-REPO-012): `q` a case-insensitive
 * substring of `<project>/<key>` or of the description (Q-API-279, `queryMatches` of internal/repository/service.go), `class` and
 * `format` the value, `state=inactive` a disabled repository. The section of the system repositories filters its rows with it in
 * the browser, so the head's filter covers both tables (Q-UI-893, Q-UI-894).
 */
export function listAdmits(r: RepositorySummary, f: RepositoryFilters): boolean {
  const q = (f.q ?? '').toLowerCase();
  return (
    (!q || refOf(r).toLowerCase().includes(q) || r.description.toLowerCase().includes(q)) &&
    (!f.class || r.class === f.class) &&
    (!f.format || r.format === f.format) &&
    (f.state !== 'inactive' || r.disabled)
  );
}

/**
 * The repository list (R-UI-020), its columns from the job of its kind of user (Q-UI-330). Inside a project, the plain user's:
 * the repository (visibility icon, key and description), format, class, and the row's **Set up client** beside its menu: the
 * address is no column (Q-UI-426), it stands where its client is set up. In the admin area, the administrator's: the repository,
 * project (mark and name), format, class, visibility, last activity, and a quiet copy button for the address beside the menu
 * (Q-UI-302). Where its columns do not fit the rows stack (Q-UI-680). The head of the list is the search by key or
 * description, **Filters** and the filters that are set (Q-UI-420 to Q-UI-422); the filters live in the query parameters
 * (R-UI-003) and restart the cursor walk; "Load more" continues it. A repository is addressed inside its project (R-REPO-002): its key links to
 * the content page `/projects/<project>/repositories/<key>` (R-UI-052), its Settings action to `…/settings`. Delete asks for the key, may offer to detach the repository from
 * the virtual repositories that include it, and follows the deletion job until the row is gone.
 *
 * The page of the built-in project `upstream` keeps its system repositories out of the list: they stand below it in the closed
 * section **System repositories**, with their number, one sentence and a table of the same columns, without **Set up client**
 * (R-UI-192, the owner's Q-UI-891). The filter of the head covers both tables (Q-UI-894).
 */
@Component({
  selector: 'quader-repository-list',
  imports: [
    FormatIconView,
    CdkMenu,
    CdkMenuItem,
    CdkMenuTrigger,
    NgTemplateOutlet,
    RouterLink,
    CopyButton,
    DataTable,
    Disclosure,
    ErrorNotice,
    FilterCategory,
    FilterPanel,
    JobProgress,
    ListHead,
    NoValue,
    PageHeader,
    ProjectLink,
    TimeAgo,
    VisibilityIcon,
  ],
  templateUrl: './repository-list.html',
  styleUrl: './repository-list.scss',
})
export class RepositoryList {
  private readonly api = inject(RepositoriesApi);
  private readonly router = inject(Router);
  private readonly session = inject(SessionStore);
  private readonly notifier = inject(Notifier);
  private readonly confirm = inject(Confirm);
  private readonly route = inject(ActivatedRoute);
  private readonly frequent = inject(FrequentPlaces);

  /** Query parameters, bound by the router's component input binding. */
  readonly q = input<string | undefined>();
  readonly class = input<string | undefined>();
  readonly format = input<string | undefined>();
  readonly state = input<string | undefined>();
  /**
   * Route parameter `:project` below `/projects/:project/repositories`: the list is that project's (Q-UI-129). In the admin
   * area it is the query parameter `project`, a filter of the instance-wide list.
   */
  readonly project = input<string | undefined>();

  protected readonly formats = resource({ loader: () => this.api.formats() });
  /** A format's name as the catalog spells it for people (`OCI (Docker)`), its key while the catalog loads. */
  protected formatLabel(name: string): string {
    return (valueOf(this.formats) ?? []).find((f) => f.name === name)?.displayName ?? name;
  }
  protected readonly list = new PagedResource<RepositorySummary>();
  /** Repositories whose deletion job is being followed, by qualified name. */
  protected readonly deleting = signal<ReadonlyMap<string, JobReference>>(new Map());
  protected readonly deleteFailure = signal<ApiError | null>(null);
  protected readonly stagedClass = signal('');
  protected readonly stagedFormat = signal('');
  protected readonly stagedInactive = signal(false);
  protected readonly stagedProject = signal('');
  protected readonly panelOpen = signal(false);

  protected readonly canCreate = computed(() => this.session.has('repositories:write-config'));
  /** The list belongs to a project: its screens drop the project column and the project filter. */
  protected readonly inProject = (): boolean => this.route.snapshot.paramMap.has('project');
  /** The project the list is limited to: the project of the screen, or the filter of the admin list. */
  protected readonly projectKey = computed(() => this.project() || null);
  /** Create goes to the wizard of the project when the list is limited to one the caller manages; nothing is created in `upstream`. */
  protected readonly canCreateHere = computed(
    () => !isUpstreamProject(this.projectKey()) && (this.canCreate() || this.session.canWriteRepositories(this.projectKey())),
  );
  /** A repository may be deleted by the capability and by the Owners of its project; a well-known one never (R-UI-190). */
  protected canDeleteRow(row: RepositorySummary): boolean {
    return !isUpstreamProject(row.project) && this.session.canWriteRepositories(row.project);
  }

  private readonly upstreamApi = inject(UpstreamApi);
  private readonly systemClients = inject(SystemClients);
  /**
   * The retired well-known repositories, by key, for a caller who reads the admin list (R-UI-190): their rows say **No longer
   * offered**, every other one that is off **Off**. Read only once the list shows one of them that is off.
   */
  private readonly retired = resource({
    params: () =>
      this.session.has('repositories:read-config') && this.list.items().some((r) => isUpstreamProject(r.project) && r.disabled)
        ? true
        : undefined,
    loader: async (): Promise<ReadonlySet<string>> => {
      try {
        return new Set((await this.upstreamApi.list()).filter((r) => r.state === 'retired').map((r) => r.key));
      } catch {
        return new Set();
      }
    },
  });
  /** The state's badge of a row of the project `upstream` that is off (R-UI-190); null for any other row. */
  protected wellKnownBadge(row: RepositorySummary): string | null {
    if (!isUpstreamProject(row.project) || !row.disabled) return null;
    return (valueOf(this.retired)?.has(row.key) ?? false) ? stateBadge('retired') : stateBadge('off');
  }
  /** The settings are offered to who may read or change the configuration (UI-A-06). */
  protected canConfigureRow(row: RepositorySummary): boolean {
    return this.session.has('repositories:read-config') || this.session.canWriteRepositories(row.project);
  }
  private readonly projects = inject(ProjectContext);
  protected readonly heading = $localize`:@@repo.list.heading:Repositories`;
  /**
   * The one muted line under the heading in a project (Q-UI-317): its description, and for its plain user (at most a reader, no
   * system role) whom to ask, since their sidebar has no members or settings. The built-in project's description is the UI's
   * localized sentence, not the stored English one (Q-UI-895).
   */
  protected readonly headerLine = computed(() => {
    const key = this.inProject() ? this.projectKey() : null;
    if (!key) return '';
    const p = this.projects.projects().find((x) => x.key === key);
    const description = projectDescription(key, p?.description);
    const plain = !this.session.isAdministrator() && (this.session.projectRole(key) ?? 'reader') === 'reader';
    const owners = (p?.owners ?? []).map((o) => o.replace(/^(user|group):/, ''));
    if (!plain || owners.length === 0) return description;
    const ask = $localize`:@@repo.list.ask:Ask ${owners.join(', ')}:owners: for access or a new repository.`;
    return description ? `${description} ${ask}` : ask;
  });
  protected readonly refOf = refOf;
  protected readonly formatOptions = computed(() =>
    (valueOf(this.formats) ?? []).map((f) => f.name),
  );
  /** The project filter of the panel: the admin list only, the project's list is that project whatever the filter says. */
  protected readonly filterProject = computed(() => (this.inProject() ? undefined : this.project()));
  protected readonly filtered = computed(
    () => !!(this.q() || this.class() || this.format() || this.state() || this.filterProject()),
  );

  // ---- The system repositories of the project `upstream` (R-UI-192) ----

  /** The page of the built-in project: its system repositories stand apart, below the list (the owner's Q-UI-891). */
  protected readonly upstreamPage = computed(() => this.inProject() && isUpstreamProject(this.projectKey()));
  /** The rows of the list: on the page of `upstream` without its system repositories, which its section holds. */
  protected readonly rows = computed(() =>
    this.upstreamPage() ? this.list.items().filter((r) => !isSystemRepository(r.project, r.key)) : this.list.items(),
  );
  /**
   * The system repositories the caller may see, asked for by name once per visit (Q-UI-893): two at most, so the section neither
   * depends on the page of the list they would fall on nor asks again for each filter. A caller who reads none (they are always
   * Private) gets an empty answer and no section.
   */
  protected readonly systemLoad = resource({
    params: () => (this.upstreamPage() ? systemRepositoryNames() : undefined),
    loader: ({ params }) => collectAll(this.api.list({ project: UPSTREAM_PROJECT, name: params })),
  });
  protected readonly systemFailure = computed(() => {
    const e = this.systemLoad.error();
    return e ? asApiError(e) : null;
  });
  /** The section's rows: those the filters of the head admit, by the list's own rules (Q-UI-894). */
  protected readonly systemRows = computed(() => {
    const filters = { q: this.q(), class: this.class(), format: this.format(), state: this.state() };
    return (valueOf(this.systemLoad) ?? []).filter((r) => listAdmits(r, filters));
  });
  /** The section stands where the caller reads a system repository, or where their read failed and says so. */
  protected readonly showSystem = computed(
    () => this.upstreamPage() && ((valueOf(this.systemLoad) ?? []).length > 0 || this.systemFailure() !== null),
  );
  /** The number after the heading: the matches of the filters, every one without a filter; none while the read failed. */
  protected readonly systemCount = computed(() => (this.systemLoad.hasValue() ? this.systemRows().length : null));
  /** Closed at every visit; a filter opens it where it matches one of them and closes it where it matches none (Q-UI-894). */
  protected readonly systemOpen = signal(false);
  protected readonly systemHeading = $localize`:@@repo.system.heading:System repositories`;
  protected readonly systemHint = $localize`:@@repo.system.hint-clamav:Quader writes these repositories itself. Its scanners read them, and an administrator can offer the ClamAV definitions to your ClamAV installations.`;
  protected readonly systemEmptyText = $localize`:@@repo.system.empty-filtered:No system repository matches.`;
  /** The filters of the drawer that are set, as the head's chips (Q-UI-422); the search text is the field's. */
  protected readonly chips = computed<readonly ListChip[]>(() => {
    const out: ListChip[] = [];
    const project = this.filterProject();
    if (project) out.push({ key: 'project', label: $localize`:@@repo.col.project:Project`, value: project });
    const format = this.format();
    if (format) out.push({ key: 'format', label: $localize`:@@repo.col.format:Format`, value: this.formatLabel(format) });
    const cls = this.class();
    if (cls) out.push({ key: 'class', label: $localize`:@@repo.col.class:Class`, value: this.classText(cls) });
    if (this.state() === 'inactive') {
      out.push({ key: 'state', label: $localize`:@@repo.filter.state:State`, value: $localize`:@@repo.filter.inactive:Disabled only` });
    }
    return out;
  });
  protected readonly searchLabel = $localize`:@@repo.list.search:Filter repositories`;
  constructor() {
    effect(() => {
      const filters = {
        q: this.q(),
        class: this.class(),
        format: this.format(),
        state: this.state(),
        project: this.projectKey() ?? undefined,
      };
      untracked(() => {
        this.restage();
        void this.list.start(this.api.list(filters));
      });
    });
    // The section of the system repositories follows each change of the filters, and the arrival of its rows: open where a filter
    // matches one of them, closed where none or no filter is set; the reader's own toggle holds until the next change (Q-UI-894).
    effect(() => {
      const open = this.filtered() && this.systemRows().length > 0;
      untracked(() => this.systemOpen.set(open));
    });
  }

  protected classText = classLabel;
  protected readonly tableCaption = $localize`:@@repo.list.caption:Repositories`;
  protected readonly emptyText = $localize`:@@repo.list.empty:There is no repository yet.`;
  protected readonly emptyFilteredText = $localize`:@@repo.list.empty-filtered:No repository matches.`;
  /** No sort control: the repository list is ordered by key and the API takes no sort parameter. */
  protected readonly columns = computed<readonly TableColumn[]>(() => {
    const admin = !this.inProject();
    return this.allColumns().filter((c) => {
      if (c.id === 'project' || c.id === 'activity') return admin && (c.id === 'project' || this.showActivity());
      if (c.id === 'visibility') return admin;
      if (c.id === 'actions-admin') return admin;
      if (c.id === 'actions') return !admin;
      return true;
    });
  });
  /**
   * The last activity is a fact of maintenance: the administrator's (Q-UI-330), and only while a loaded row has one, so that it
   * is no column of dashes (Q-UI-305). A project's list is the plain user's; its maintainers find it in the repository's Details.
   */
  protected readonly showActivity = computed(
    () => !this.inProject() && this.canCreate() && this.list.items().some((r) => !!r.lastActivityAt),
  );
  /**
   * The columns take their widths from their content (Q-UI-563): a format, a class, a level, a time and the actions never break,
   * so each is as wide as its longest value and declares no width; the repository and the project wrap and declare their least
   * (Q-UI-308). Inside a project the repository is the one column that wraps, and it takes what the others leave (`prefer`,
   * Q-UI-330). In the admin area the repository and the project share that room in proportion to their content: a pinned project
   * of 11 rem broke **Customer portal release engineering and delivery** into three lines beside a Last activity of 7.5 rem that
   * held "4 minutes ago" (2026-11-18). The project is its mark and its name (Q-UI-424).
   */
  private readonly allColumns = computed<readonly TableColumn[]>(() => [
    { id: 'key', label: $localize`:@@repo.col.repository:Repository`, width: '13rem', prefer: this.inProject() },
    // Its mark and "Customer portal release" on one line: at 11 rem the seed's longest project stood in three lines at 1,280 px
    // beside descriptions that took the room (2026-11-18).
    { id: 'project', label: $localize`:@@repo.col.project:Project`, width: '12rem' },
    { id: 'format', label: $localize`:@@repo.col.format:Format` },
    {
      id: 'class',
      label: $localize`:@@repo.col.class:Class`,
      help: $localize`:@@repo.help.class:Local repositories hold what you upload, remote repositories fetch and cache from another registry, virtual repositories combine other repositories behind one address.`,
    },
    // The admin area's column: who reads the artifacts, an icon and a word (Q-UI-330); inside a project the icon stands before the name.
    { id: 'visibility', label: $localize`:@@repo.col.visibility:Visibility` },
    {
      id: 'activity',
      label: $localize`:@@repo.col.activity:Last activity`,
      help: $localize`:@@repo.help.activity:The last time a client read from or wrote to the repository. A dash means nothing has been recorded yet.`,
      // Below 64 rem of the list's width the other columns take this room: the admin area's list at 1280 px.
      narrow: true,
    },
    // Inside a project: **Set up client** and the menu.
    { id: 'actions', label: $localize`:@@repo.col.actions:Actions`, hideLabel: true },
    // The admin area's row has the copy button beside the menu.
    { id: 'actions-admin', label: $localize`:@@repo.col.actions:Actions`, hideLabel: true },
  ]);

  protected readonly levelText = levelName;

  protected readonly classIcon = classIcon;
  /**
   * Set up client of a row's repository (R-UI-126); none for a system repository while it answers no client (Q-UI-896), and for
   * `upstream/clamav` while ClamAV definition updates is on (R-UI-192, Q-UI-908).
   */
  protected setupLink(row: RepositorySummary): string[] | null {
    return setupCommands(refOf(row), !this.closed(row));
  }

  /** The list holds `upstream/clamav`: its switch is asked once, so that its row offers what it answers (Q-UI-908). */
  private readonly listsClamav = computed(() =>
    [...this.list.items(), ...(valueOf(this.systemLoad) ?? [])].some((r) => isUpstreamProject(r.project) && r.key === CLAMAV_KEY),
  );
  private readonly clamavServed = resource({
    params: () => (this.listsClamav() ? upstreamName(CLAMAV_KEY) : undefined),
    loader: ({ params }) => this.systemClients.served(params),
  });
  /** A system repository that answers no client now (R-UI-193): `scanner-trivy` always, `clamav` while its switch is off. */
  protected closed(row: RepositorySummary): boolean {
    return this.system(row) && !(mayServeClients(row.key) && valueOf(this.clamavServed) === true);
  }
  /** The repository of the ClamAV definitions, whose menu leads to its databases and their versions (R-UI-192, R-UI-194). */
  protected isClamav(row: RepositorySummary): boolean {
    return isUpstreamProject(row.project) && row.key === CLAMAV_KEY;
  }
  /** The admin area's copy button: its name and its tooltip carry the address itself (Q-UI-330). */
  protected copyValueLabel(address: string): string {
    return $localize`:@@repo.copy-address-value:Copy client address ${address}:address:`;
  }
  /** The menu's "Copy client address": the same as the row's copy button. */
  protected async copyAddress(address: string): Promise<void> {
    try {
      await navigator.clipboard.writeText(address);
      this.notifier.status($localize`:@@copy.done:Copied to the clipboard.`);
    } catch {
      this.notifier.alert($localize`:@@copy.failed:The clipboard is not available. Select the text and copy it.`);
    }
  }
  protected readonly deleteLabel = $localize`:@@repo.delete.progress:Repository deletion`;

  /**
   * The client address of a row, copied by the admin area's copy button and its menu (Q-UI-330). The server serves absolute URLs
   * (public base URL plus path): copied as they are. None for a system repository, whose address answers every client 404 (Q-UI-898).
   */
  protected address(row: RepositorySummary): string {
    return this.closed(row) ? '' : (row.addresses[0] ?? '');
  }

  /** A system repository of the project `upstream` (R-UI-192, R-UI-193). */
  protected system(row: RepositorySummary): boolean {
    return isSystemRepository(row.project, row.key);
  }
  protected readonly systemBadge = systemBadge();

  protected actionsLabel(row: RepositorySummary): string {
    return $localize`:@@repo.actions-of:Actions for ${row.key}:key:`;
  }

  protected applyFilter(change: Readonly<Record<string, string | null>>): void {
    void this.router.navigate([], { queryParams: change, queryParamsHandling: 'merge' });
  }

  /** The drawer's values back to the applied ones: after a change of the query, and when the drawer closes without **Apply**. */
  protected restage(): void {
    this.stagedClass.set(this.class() ?? '');
    this.stagedFormat.set(this.format() ?? '');
    this.stagedInactive.set(this.state() === 'inactive');
    this.stagedProject.set(this.filterProject() ?? '');
  }

  protected removeChip(chip: ListChip): void {
    this.applyFilter({ [chip.key]: null });
  }

  /** The drawer's apply button: all staged values reach the query at once. */
  protected submitFilters(): void {
    this.applyFilter({
      class: this.stagedClass() || null,
      format: this.stagedFormat() || null,
      state: this.stagedInactive() ? 'inactive' : null,
      ...(this.inProject() ? {} : { project: this.stagedProject().trim() || null }),
    });
  }

  /** **Clear all** of the drawer: its filters go; the search text is the field's and stays. */
  protected clearFilters(): void {
    this.applyFilter({ class: null, format: null, state: null, ...(this.inProject() ? {} : { project: null }) });
  }

  protected async remove(row: RepositorySummary): Promise<void> {
    this.deleteFailure.set(null);
    const idempotencyKey = newIdempotencyKey();
    let offerDetach = false;
    for (;;) {
      const answer = await this.confirm.ask({
        title: $localize`:@@repo.delete.title:Delete repository ${row.key}:key:?`,
        lines: [
          $localize`:@@repo.delete.text:The repository and everything in it are deleted for good. Deletion runs as a background job; the key stays taken until it has finished.`,
        ],
        confirmLabel: $localize`:@@repo.delete.confirm:Delete repository`,
        typeToConfirm: row.key,
        choice: offerDetach
          ? $localize`:@@repo.delete.detach:Also remove it from the virtual repositories that include it`
          : undefined,
      });
      if (answer === null) return;
      try {
        const ref = refOf(row);
        const outcome = await this.api.delete(ref, idempotencyKey, answer.checked);
        // The deletion is accepted: the repository leaves the remembered places at once, also while its job runs (R-UI-167).
        this.frequent.forgetRepository(row.project, row.key);
        if (outcome.done) this.gone(ref);
        else this.deleting.update((m) => new Map(m).set(ref, outcome.job));
        return;
      } catch (e) {
        const error = asApiError(e);
        if (error.code === 'repository_referenced' && !offerDetach) {
          // The choice of R-UI-020: ask again, this time with the detach option.
          offerDetach = true;
          continue;
        }
        this.deleteFailure.set(error);
        return;
      }
    }
  }

  protected gone(ref: string): void {
    this.deleting.update((m) => {
      const next = new Map(m);
      next.delete(ref);
      return next;
    });
    this.list.remove((r) => refOf(r) === ref);
    this.notifier.status($localize`:@@repo.delete.done:Repository ${ref}:key: deleted.`);
  }

  protected deletionFailed(ref: string): void {
    this.deleting.update((m) => {
      const next = new Map(m);
      next.delete(ref);
      return next;
    });
    this.notifier.alert(
      $localize`:@@repo.delete.failed:Deleting repository ${ref}:key: failed. Its row is kept; try again.`,
    );
    void this.list.start(
      this.api.list({
        q: this.q(),
        class: this.class(),
        format: this.format(),
        state: this.state(),
        project: this.projectKey() ?? undefined,
      }),
    );
  }

  protected jobEnded(ref: string, state: string): void {
    if (state === 'succeeded') this.gone(ref);
    else this.deletionFailed(ref);
  }

  /** Records the opening of a row's repository; one in deletion is no place to go back to (R-UI-167). */
  protected visit(row: RepositorySummary): void {
    if (row.lifecycle === 'deleting' || this.deleting().has(refOf(row))) return;
    this.frequent.visitRepository(row.project, row.key);
  }
}
