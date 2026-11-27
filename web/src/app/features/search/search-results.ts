import { Component, computed, effect, inject, input, resource, signal, untracked } from '@angular/core';
import { isSystemRepository, isUpstreamProject, projectDescription, systemBadge } from '../../core/upstream-project';
import { NoValue } from '../../shared/ui/no-value';
import { Router, RouterLink } from '@angular/router';
import { PackageSummary, PackagesApi } from '../../api/packages-api';
import { DocumentationHit, SearchApi } from '../../api/search-api';
import { SignatureBadge } from '../../views/signatures/signature-badge';
import { PackageName } from '../../views/packages/package-name';
import { PackageTags } from '../../views/packages/package-tags';
import { Project, ProjectRole, ProjectsApi } from '../../api/projects-api';
import { RepositoriesApi, RepositorySummary, refOf } from '../../api/repositories-api';
import { FormatIconView } from '../../shared/format-icon';
import { DeprecationMark } from '../../shared/ui/deprecation-mark';
import { PagedResource, valueOf } from '../../shared/paged-resource';
import { DataTable, TableColumn } from '../../shared/ui/data-table';
import { PageHeader } from '../../shared/ui/page-header';
import { TabContent, TabGroup, TabPage } from '../../shared/ui/tabs';
import { VisibilityIcon } from '../../shared/ui/visibility-level';
import { ProjectLevel, ProjectLink, ProjectMark } from '../../shared/ui/project-mark';
import { TimeAgo } from '../../shared/ui/time-ago';
import { packageCommands } from '../packages/package-links';
import { projectRoleLabel } from '../projects/project-roles';
import { classIcon, classLabel, classTip, shownClass } from '../repositories/repository-class';
import { Tooltip } from '../../shared/ui/tooltip';
import { MalwareMark, malwareColumn, malwareShown } from '../../shared/malware/malware-mark';
import { MalwareSignaturesStore } from '../../core/malware-signatures';

/** The tabs of the page, in their order (R-UI-098). */
export const RESULT_KINDS = ['repository', 'package', 'project', 'documentation'] as const;
export type ResultKind = (typeof RESULT_KINDS)[number];

/** Rows per page of each tab, with **Load more** (R-API-010). */
const PAGE = 25;

/**
 * The results page of the main area (R-UI-098, Q-UI-411): Enter in the top bar's field without a picked suggestion opens it. Its
 * tabs are the lists of the three kinds with `q`, each the list the caller may read: Repositories (`GET /repositories?q=`) from the
 * parts of the repository list (Q-UI-430: the visibility icon, the key with its description, the project by its mark and its name,
 * format and class; no address, Q-UI-426), Packages (`GET /packages?q=`, the columns of Q-UI-314 with the project and the repository), Projects (`GET
 * /projects?q=`, the start page's columns). `kind` chooses the tab; without it, the first group that had a suggestion when Enter
 * was pressed (navigation state `firstKind`), else Repositories. The page has no users, groups or admin pages and no link into the
 * admin area (R-UI-099). It takes over the stopgap's page of Q-UI-390 (Q-UI-415).
 */
@Component({
  selector: 'quader-search-results',
  imports: [
    MalwareMark,
    RouterLink,
    DataTable,
    DeprecationMark,
    FormatIconView,
    PackageName,
    PackageTags,
    PageHeader,
    NoValue,
    ProjectLevel,
    ProjectLink,
    ProjectMark,
    SignatureBadge,
    TabContent,
    TabGroup,
    TabPage,
    TimeAgo,
    Tooltip,
    VisibilityIcon,
  ],
  template: `
    <quader-page-header [heading]="heading" [description]="description()" />

    <quader-tab-group [selectedIndex]="tab()" (selectedIndexChange)="choose($event)">
      <quader-tab [label]="labels.repository">
        <ng-template quaderTabContent>
          <quader-data-table
            [caption]="labels.repository"
            [columns]="repositoryColumns"
            [loaded]="tooShort() || repositories.loaded()"
            [loading]="repositories.loading()"
            loadingText="Searching."
            i18n-loadingText="@@search.results.loading"
            [error]="repositories.error()"
            [empty]="tooShort() || repositories.items().length === 0"
            [emptyText]="emptyText()"
            [hasMore]="repositories.hasMore()"
            (more)="repositories.more()"
            (retry)="retry(repositories)"
          >
            @for (row of repositories.items(); track refOf(row)) {
              <!-- The parts of the repository list (Q-UI-430): the visibility icon before the key, the description under it, the
                   project by its mark and its name, format and class; no address, which means nothing without its client (Q-UI-426). -->
              <tr>
                <th scope="row">
                  <span class="icon-name">
                    <quader-visibility-icon [level]="row.visibility.artifacts" />
                    <span class="icon-name-text">
                      <a [routerLink]="['/projects', row.project, 'repositories', row.key]" translate="no">{{ row.key }}</a>
                      <!-- A system repository of upstream is found and marked, so that it does not read as one more repository of
                           its project (R-UI-193, Q-UI-902); where its class cell says System, that is the mark (Q-UI-1203). -->
                      @if (system(row) && shownClassOf(row) !== 'system') {
                        &ngsp;<span class="badge">{{ systemText }}</span>
                      }
                      @if (row.description) {
                        <span class="secondary" translate="no">{{ row.description }}</span>
                      }
                    </span>
                  </span>
                </th>
                <td class="project-cell"><quader-project-link [key]="row.project" /></td>
                <td class="format-cell">
                  <span class="format-name"
                    ><quader-format-icon [format]="row.format" /><span translate="no">{{ formatLabel(row.format) }}</span></span
                  >
                </td>
                <td class="class-cell">
                  @let k = shownClassOf(row);
                  <span class="class-name" [quaderTooltip]="classTip(k)" [attr.tabindex]="classTip(k) ? 0 : null"
                    ><span class="material-icons" aria-hidden="true">{{ classIcon(k) }}</span>{{ classText(k) }}</span
                  >
                </td>
              </tr>
            }
          </quader-data-table>
        </ng-template>
      </quader-tab>
      <quader-tab [label]="labels.package">
        <ng-template quaderTabContent>
          <quader-data-table
            [caption]="labels.package"
            [columns]="packageColumns()"
            [loaded]="tooShort() || packages.loaded()"
            [loading]="packages.loading()"
            loadingText="Searching."
            i18n-loadingText="@@search.results.loading"
            [error]="packages.error()"
            [empty]="tooShort() || packages.items().length === 0"
            [emptyText]="emptyText()"
            [hasMore]="packages.hasMore()"
            (more)="packages.more()"
            (retry)="retry(packages)"
          >
            @for (row of packages.items(); track row.id) {
              @let at = placeOf(row);
              <tr>
                <th scope="row" class="wrap-name">
                  <!-- A hit crosses projects: its namespace leads to the name's page in every project (R-UI-220), its tags to the
                       search for them; the deprecation of its latest version (R-API-085) and a verified latest version's badge
                       (R-ANS-057) beside the name. -->
                  <span class="name-line">
                    <quader-package-name [format]="row.format" [name]="row.name" [key]="row.key" [link]="packageLink(row)" />
                    <quader-deprecation-mark [deprecation]="row.deprecation" />
                    <quader-signature-badge [state]="row.signature" />
                  </span>
                  @if (row.description) {
                    <span class="secondary" translate="no">{{ row.description }}</span>
                  }
                  <quader-package-tags [tags]="row.keywords" />
                </th>
                <td class="project-cell"><quader-project-link [key]="at.project" /></td>
                <td class="repository-cell">
                  <a [routerLink]="['/projects', at.project, 'repositories', at.key]" translate="no">{{ at.key }}</a>
                </td>
                <td class="format-cell">
                  <span class="format-name"
                    ><quader-format-icon [format]="row.format" /><span translate="no">{{ formatLabel(row.format) }}</span></span
                  >
                </td>
                <td translate="no">{{ row.latestVersion }}</td>
                <!-- The newest version's malware scan in one word, its details on demand (R-UI-255). -->
                @if (malware()) {
                  <td>
                    @if (row.malware; as m) {
                      <quader-malware-mark [malware]="m" kind="package" [newest]="row.latestVersion" [image]="row.format === 'oci'" />
                    }
                  </td>
                }
                <td><quader-time-ago [at]="row.updatedAt" absolute="title" /></td>
              </tr>
            }
          </quader-data-table>
        </ng-template>
      </quader-tab>
      <quader-tab [label]="labels.project">
        <ng-template quaderTabContent>
          <quader-data-table
            [caption]="labels.project"
            [columns]="projectColumns"
            [loaded]="tooShort() || projects.loaded()"
            [loading]="projects.loading()"
            loadingText="Searching."
            i18n-loadingText="@@search.results.loading"
            [error]="projects.error()"
            [empty]="tooShort() || projects.items().length === 0"
            [emptyText]="emptyText()"
            [hasMore]="projects.hasMore()"
            (more)="projects.more()"
            (retry)="retry(projects)"
          >
            @for (p of projects.items(); track p.key) {
              <tr>
                <th scope="row">
                  <span class="icon-name">
                    <quader-project-mark [key]="p.key" [name]="p.displayName" size="medium" />
                    <span class="icon-name-text">
                      <a [routerLink]="['/projects', p.key]">{{ p.displayName || p.key }}</a>
                      <!-- The product's project, as the start page shows it (R-UI-190): its badge, no role and no members. -->
                      @if (builtIn(p.key)) {
                        &ngsp;<span class="badge" i18n="@@projects.built-in">Built-in</span>
                      }
                      <span class="secondary" translate="no">{{ p.key }}</span>
                    </span>
                  </span>
                </th>
                <td>
                  @if (descriptionOf(p.key, p.description); as description) {
                    <!-- A project's own description is what a person wrote; the built-in project's is the UI's (R-UI-214). -->
                    <span [attr.translate]="builtIn(p.key) ? null : 'no'">{{ description }}</span>
                  } @else {
                    <quader-no-value />
                  }
                </td>
                <td class="level"><quader-project-level [key]="p.key" [level]="p.visibility" /></td>
                <!-- No role: a caller who sees the project through a system role and is no member (Q-UI-561). -->
                <td>
                  @if (p.role) {
                    {{ roleLabel(p.role) }}
                  } @else {
                    <quader-no-value />
                  }
                </td>
                <td class="num">{{ p.repositories }}</td>
                <td class="num">
                  @if (builtIn(p.key)) {
                    <quader-no-value />
                  } @else {
                    {{ p.members }}
                  }
                </td>
              </tr>
            }
          </quader-data-table>
        </ng-template>
      </quader-tab>
      <quader-tab [label]="labels.documentation">
        <ng-template quaderTabContent>
          <!-- The modules, plugins and roles of the newest version of every collection the reader may read, whose documentation
               the reader may read (R-ANS-058); an entry opens its page in its collection, the package's address with ?doc=. -->
          <quader-data-table
            [caption]="labels.documentation"
            [columns]="documentationColumns"
            [loaded]="tooShort() || documentation.loaded()"
            [loading]="documentation.loading()"
            loadingText="Searching."
            i18n-loadingText="@@search.results.loading"
            [error]="documentation.error()"
            [empty]="tooShort() || documentation.items().length === 0"
            [emptyText]="emptyText()"
            [hasMore]="documentation.hasMore()"
            (more)="documentation.more()"
            (retry)="retry(documentation)"
          >
            @for (d of documentation.items(); track d.project + '/' + d.repository + '/' + d.package + '/' + d.type + '/' + d.name) {
              <tr>
                <th scope="row" class="wrap-name">
                  <a [routerLink]="docLink(d)" [queryParams]="{ doc: d.type + '/' + d.name }" translate="no">{{ d.name }}</a>
                  <!-- The entry's type, then its short description: one muted line. -->
                  <span class="secondary" translate="no"
                    >{{ d.type }}@if (d.shortDescription) {&ngsp;·&ngsp;{{ d.shortDescription }}}</span
                  >
                </th>
                <td class="wrap-name">
                  <!-- The collection, with its deprecation in the hit's repository beside its name (R-ANS-060, R-API-085). -->
                  <span class="name-line">
                    <quader-package-name [format]="d.format" [name]="d.package" [link]="docLink(d)" />
                    <quader-deprecation-mark [deprecation]="d.deprecation" />
                  </span>
                  <span class="secondary" translate="no">{{ d.version }}</span>
                </td>
                <td class="project-cell"><quader-project-link [key]="d.project" /></td>
                <td class="repository-cell">
                  <a [routerLink]="['/projects', d.project, 'repositories', d.repository]" translate="no">{{ d.repository }}</a>
                </td>
              </tr>
            }
          </quader-data-table>
        </ng-template>
      </quader-tab>
    </quader-tab-group>
  `,
})
export class SearchResults {
  private readonly router = inject(Router);
  private readonly repositoriesApi = inject(RepositoriesApi);
  private readonly packagesApi = inject(PackagesApi);
  private readonly projectsApi = inject(ProjectsApi);
  private readonly searchApi = inject(SearchApi);

  /** The text of the search, the query parameter `q`. */
  readonly q = input<string | undefined>();
  /** The tab, the query parameter `kind`. */
  readonly kind = input<string | undefined>();

  /** The first group that had a suggestion when Enter was pressed, handed over by the field (navigation state). */
  private readonly firstKind = signal<ResultKind | null>(
    ((): ResultKind | null => {
      const state = this.router.getCurrentNavigation()?.extras.state ?? (typeof history === 'undefined' ? null : history.state);
      const k = (state as Record<string, unknown> | null)?.['firstKind'];
      return RESULT_KINDS.includes(k as ResultKind) ? (k as ResultKind) : null;
    })(),
  );

  protected readonly term = computed(() => (this.q() ?? '').trim());
  protected readonly tooShort = computed(() => this.term().length < 2);
  /** The selected tab: `kind`; without it (or with `user` or `group`) the field's first group, else Repositories. */
  protected readonly tab = computed(() => {
    const k = this.kind();
    const chosen = RESULT_KINDS.includes(k as ResultKind) ? (k as ResultKind) : (this.firstKind() ?? 'repository');
    return RESULT_KINDS.indexOf(chosen);
  });

  protected readonly repositories = new PagedResource<RepositorySummary>(PAGE);
  protected readonly packages = new PagedResource<PackageSummary>(PAGE);
  protected readonly projects = new PagedResource<Project>(PAGE);
  protected readonly documentation = new PagedResource<DocumentationHit>(PAGE);
  private readonly formats = resource({ loader: () => this.repositoriesApi.formats() });

  protected readonly heading = $localize`:@@search.results.heading:Search`;
  protected readonly labels: Record<ResultKind, string> = {
    repository: $localize`:@@search.group.repositories:Repositories`,
    package: $localize`:@@search.group.packages:Packages`,
    project: $localize`:@@search.group.projects:Projects`,
    documentation: $localize`:@@search.group.documentation:Documentation`,
  };
  protected readonly description = computed(() =>
    this.term() ? $localize`:@@search.results.description:What you may read that matches “${this.term()}:text:”.` : '',
  );
  protected readonly emptyText = computed(() =>
    this.tooShort()
      ? $localize`:@@search.results.too-short:Type at least two characters.`
      : $localize`:@@search.results.none:Nothing you may read matches “${this.term()}:text:”.`,
  );

  /** The repository list's parts (Q-UI-430): the repository takes what the others leave (Q-UI-330). */
  protected readonly repositoryColumns: readonly TableColumn[] = [
    { id: 'key', label: $localize`:@@repo.col.repository:Repository`, width: '14rem', prefer: true },
    { id: 'project', label: $localize`:@@projects.col.project:Project`, width: '11rem' },
    { id: 'format', label: $localize`:@@repo.col.format:Format`, width: '8rem' },
    {
      id: 'class',
      label: $localize`:@@repo.col.class:Class`,
      help: $localize`:@@repo.help.class:Local repositories hold what you upload, remote repositories fetch and cache from another registry, virtual repositories combine other repositories behind one address.`,
      width: '5rem',
    },
  ];
  /** What a reader chooses a package by (Q-UI-314), with where it is: the project and the repository that hold it. */
  private readonly malwareMarks = inject(MalwareSignaturesStore);
  /** The hits carry the malware state of their newest version (R-MAL-039): the column **Malware scan** (R-UI-255). */
  protected readonly malware = computed(() => malwareShown(this.malwareMarks, this.packages.items()));
  protected readonly packageColumns = computed<readonly TableColumn[]>(() => [
    { id: 'name', label: $localize`:@@pkg.col.package:Package`, width: '14rem', prefer: true },
    { id: 'project', label: $localize`:@@projects.col.project:Project`, width: '11rem' },
    { id: 'repository', label: $localize`:@@repo.col.repository:Repository`, width: '8rem' },
    { id: 'type', label: $localize`:@@pkg.col.format:Format`, width: '7rem', stackLabel: false },
    {
      id: 'latest',
      label: $localize`:@@pkg.col.latest:Latest`,
      help: $localize`:@@pkg.help.latest:The highest stable version you may see. A pre-release is shown only while no stable version exists.`,
      width: '8rem',
    },
    ...(this.malware() ? [malwareColumn('package')] : []),
    {
      id: 'updatedAt',
      label: $localize`:@@pkg.col.updated:Updated`,
      help: $localize`:@@pkg.help.updated:When the latest version you may see was published.`,
      width: '7rem',
    },
  ]);
  /** What a reader chooses an entry of the documentation by (R-ANS-058): the entry, the collection it documents, where it is. */
  protected readonly documentationColumns: readonly TableColumn[] = [
    { id: 'entry', label: $localize`:@@search.col.entry:Entry`, width: '14rem', prefer: true },
    { id: 'collection', label: $localize`:@@search.col.collection:Collection`, width: '12rem' },
    { id: 'project', label: $localize`:@@projects.col.project:Project`, width: '11rem' },
    { id: 'repository', label: $localize`:@@repo.col.repository:Repository`, width: '8rem' },
  ];
  /** The start page's columns (Q-UI-311), the project's mark before its name (Q-UI-424). */
  protected readonly projectColumns: readonly TableColumn[] = [
    { id: 'name', label: $localize`:@@projects.col.project:Project`, width: '14rem' },
    { id: 'description', label: $localize`:@@projects-home.col.description:Description` },
    { id: 'visibility', label: $localize`:@@projects.col.visibility:Visibility`, width: '8rem' },
    {
      id: 'role',
      label: $localize`:@@projects.col.role:Your role`,
      help: $localize`:@@projects.help.role:Your role in the project; a dash when you see it through a system role and are no member.`,
      width: '7rem',
    },
    { id: 'repositories', label: $localize`:@@projects.col.repositories:Repositories`, numeric: true, width: '7rem' },
    { id: 'members', label: $localize`:@@projects.col.members:Members`, numeric: true, width: '5rem' },
  ];
  protected readonly refOf = refOf;
  protected readonly classText = classLabel;

  constructor() {
    // Each tab reads its list when it is shown, with the text; a text of fewer than two characters asks nothing.
    effect(() => {
      const q = this.term();
      const tab = RESULT_KINDS[this.tab()]!;
      if (q.length < 2) return;
      untracked(() => {
        if (tab === 'repository') void this.repositories.start(this.repositoriesApi.list({ q }));
        else if (tab === 'package') void this.packages.start(this.packagesApi.list({ q, sort: 'relevance' }));
        else if (tab === 'documentation') void this.documentation.start(this.searchApi.documentation(q));
        else void this.projects.start(this.projectsApi.list({ q }));
      });
    });
  }

  /** A tab chosen: its kind goes into the address, so a reload and a link show the same tab. */
  protected choose(index: number): void {
    const kind = RESULT_KINDS[index];
    if (!kind || index === this.tab()) return;
    void this.router.navigate(['/search'], { queryParams: { q: this.term() || null, kind }, replaceUrl: true });
  }

  /** The project and the repository of a package: the first repository the caller reads that holds it (`<project>/<key>`). */
  protected placeOf(row: PackageSummary): { project: string; key: string } {
    const [project = '', key = ''] = (row.repositories[0] ?? '').split('/');
    return { project, key };
  }

  protected packageLink(row: PackageSummary): string[] {
    // The address is the key, the name the rules see (an Ansible role's role/<id>, R-ANS-056); the row shows the name.
    return packageCommands(this.placeOf(row).project, row.format, row.key ?? row.name);
  }

  /** The address of the collection an entry documents: its newest version's page, which `?doc=` opens on the entry (R-ANS-049). */
  protected docLink(d: DocumentationHit): string[] {
    return packageCommands(d.project, d.format, d.package);
  }

  /**
   * A list's Retry (R-UI-216): the list, and the formats whose names the rows show when they failed with it (the rows named a format by
   * its key until a reload).
   */
  protected retry(list: PagedResource<unknown>): void {
    void list.reload();
    if (this.formats.error()) this.formats.reload();
  }

  protected formatLabel(name: string): string {
    return (valueOf(this.formats) ?? []).find((f) => f.name === name)?.displayName ?? name;
  }

  protected readonly classIcon = classIcon;
  protected readonly classTip = classTip;
  /** What a row's class cell shows: System for a repository of a system-only format (Q-UI-1203), else its class. */
  protected shownClassOf(row: { readonly class: string; readonly format: string }): string {
    return shownClass(row, valueOf(this.formats));
  }

  protected readonly builtIn = isUpstreamProject;
  /** A system repository of the project `upstream`, with its mark (R-UI-193, Q-UI-902). */
  protected system(row: RepositorySummary): boolean {
    return isSystemRepository(row.project, row.key);
  }
  protected readonly systemText = systemBadge();
  /** The built-in project's description is the UI's localized sentence (Q-UI-895). */
  protected readonly descriptionOf = projectDescription;

  protected roleLabel(role: ProjectRole): string {
    return projectRoleLabel(role);
  }

}
