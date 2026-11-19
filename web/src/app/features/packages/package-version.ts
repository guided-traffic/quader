import { DecimalPipe } from '@angular/common';
import { Component, computed, effect, inject, input, resource, signal, untracked } from '@angular/core';
import { Router, RouterLink } from '@angular/router';
import { PackageDocTree, PackageVersionDocs, PackagesApi } from '../../api/packages-api';
import { VulnerabilitiesApi, VulnerabilityVersionReport } from '../../api/vulnerabilities-api';
import { CopyButton } from '../../shared/copy-button';
import { ErrorNotice } from '../../shared/error-notice';
import { FormatIconView } from '../../shared/format-icon';
import { asApiError, valueOf } from '../../shared/paged-resource';
import { DataTable, TableColumn } from '../../shared/ui/data-table';
import { PageHeader } from '../../shared/ui/page-header';
import { TabGroup, TabPage } from '../../shared/ui/tabs';
import { worstGrade } from '../../shared/vulnerability/grade-words';
import { barSeverityOf, blockedRepositories, PackageVulnerabilities } from '../vulnerabilities/package-vulnerabilities';
import { AnsibleContents } from './ansible-contents';
import { AnsibleDocs } from './ansible-docs';
import { AnsibleImportLog } from './ansible-import-log';
import { formatSize } from './format-size';
import { TimeAgo } from '../../shared/ui/time-ago';
import { InstallPanel } from './install-panel';
import { EXTERNAL_REL, packageCommands } from './package-links';
import { PackageMemory } from './package-memory';
import { PackageReadmeView } from './package-readme';
import { distTagsOf } from './package-versions-model';
import { GradeLabel } from '../../shared/vulnerability/grade';
import {
  ansibleFacts,
  debFacts,
  dependencyGroups,
  deprecationOf,
  frameworkGroups,
  imageFacts,
  metadataLinks,
  nugetFacts,
  stringRecord,
  strings,
} from './version-metadata';
import { VersionSize } from './version-size';
import { NoValue } from '../../shared/ui/no-value';

export type VersionTab = 'vulnerabilities' | 'documentation' | 'contents' | 'readme' | 'files' | 'details' | 'import-log';

interface TabEntry {
  readonly id: VersionTab;
  readonly label: string;
  /** Shown in the strip as a neutral badge after the label (the Contents tab, R-UI-062). */
  readonly count?: number;
}

/**
 * The page of one version (R-UI-111, the owner's walk of 2026-11-18): `/packages/{format}/{name…}@{version}`, an address of its
 * own that can be linked and sent. The header names the package and the version with its tags and its facts (published, size or that
 * it is not known, the last download by a client, the downloads, licences; R-API-079, R-API-080) and its vulnerabilities in one
 * statement (R-UI-112); the install command follows at the page's width,
 * so it is read in full (R-UI-009). The tabs: **Vulnerabilities** (a grade card per repository and the open findings at the page's
 * width, R-VULN-023), the documentation of an Ansible collection (R-UI-060), **README**, **Files**, **Details** (the format's
 * metadata and the repositories that hold the version) and an Ansible collection's **Import log**. The open tab is the `tab`
 * parameter; the first tab is Vulnerabilities where the version is graded, else its documentation (Q-UI-456).
 */
@Component({
  selector: 'quader-package-version',
  imports: [
    TimeAgo,
    DecimalPipe,
    RouterLink,
    TabGroup,
    TabPage,
    PageHeader,
    CopyButton,
    DataTable,
    ErrorNotice,
    FormatIconView,
    InstallPanel,
    PackageReadmeView,
    AnsibleContents,
    AnsibleDocs,
    AnsibleImportLog,
    PackageVulnerabilities,
    GradeLabel,
    VersionSize,
    NoValue,
  ],
  templateUrl: './package-version.html',
  styleUrl: './package-version.scss',
})
export class PackageVersionPage {
  private readonly api = inject(PackagesApi);
  private readonly vulnerabilityApi = inject(VulnerabilitiesApi);
  private readonly router = inject(Router);
  private readonly memory = inject(PackageMemory);

  /** Route parameters: the project, then the matcher's format, name and version. */
  readonly project = input.required<string>();
  readonly format = input.required<string>();
  readonly name = input.required<string>();
  readonly version = input.required<string>();
  /** Query parameters: the open tab, the open page of the Documentation tab and its search text (R-UI-060), the severity filter. */
  readonly tab = input<string | undefined>();
  readonly doc = input<string | undefined>();
  readonly q = input<string | undefined>();
  readonly severity = input<string | undefined>();

  protected readonly summary = resource({
    params: () => ({ format: this.format(), name: this.name() }),
    loader: ({ params }) => this.api.lookup(params.format, params.name),
  });
  protected readonly pkg = computed(() => valueOf(this.summary));
  protected readonly details = resource({
    params: () => {
      const id = valueOf(this.summary)?.id;
      return id ? { id, version: this.version() } : undefined;
    },
    loader: ({ params }) => this.api.version(params.id, params.version),
  });
  protected readonly detail = computed(() => valueOf(this.details));
  protected readonly readme = resource({
    params: () => {
      const d = this.detail();
      // The README of an Ansible collection is the first page of its Documentation tab (R-UI-060), not a tab of its own.
      return d?.hasReadme && d.format !== 'ansible' ? { id: d.packageId, version: d.version } : undefined;
    },
    loader: ({ params }) => this.api.readme(params.id, params.version),
  });

  /**
   * What the scanning says of the version in every readable repository (R-VULN-023). The page works without it: an answer that
   * fails (no scanning on this replica, a reader who may not ask) leaves no tab, no grade and no banner, and no error.
   */
  protected readonly vulnerabilities = resource({
    params: () => {
      const id = valueOf(this.summary)?.id;
      return id ? { id, version: this.version() } : undefined;
    },
    loader: async ({ params }): Promise<VulnerabilityVersionReport | null> => {
      try {
        return await this.vulnerabilityApi.versionReport(params.id, params.version);
      } catch {
        return null;
      }
    },
  });
  protected readonly vulnerabilityReport = computed(() => valueOf(this.vulnerabilities) ?? null);
  /** The version's grade, the worst of its repositories (R-VULN-038, Q-UI-375): the same grade the row of the versions table shows. */
  protected readonly grade = computed(() => worstGrade((this.vulnerabilityReport()?.repositories ?? []).map((r) => r.grade)));
  protected readonly blockedIn = computed(() => blockedRepositories(this.vulnerabilityReport()));
  protected readonly severityFilter = computed(() => barSeverityOf(this.severity()));

  /**
   * What `metadata.docs` of the version announces (R-UI-060): absent for a format without documentation and for a caller who may not
   * read the wiki of the repository (R-ANS-037, Q-UI-244), and then there is no Documentation tab and no request.
   */
  protected readonly docsAnnouncement = computed<PackageVersionDocs | null>(() => {
    const d = this.detail();
    return d?.format === 'ansible' ? (d.metadata.docs ?? null) : null;
  });
  private readonly docsAnnounced = computed(() => {
    const a = this.docsAnnouncement();
    return a !== null && (a.state !== 'none' || this.detail()?.hasReadme === true);
  });
  protected readonly docsReadmeOnly = computed(() => this.docsAnnounced() && this.docsAnnouncement()?.state === 'none');
  /** The local repository that holds the version: the Import log tab reads its tasks (R-UI-063). */
  protected readonly localRepository = computed(() => {
    const d = this.detail();
    return d?.format === 'ansible' ? (d.repositories.find((r) => r.class === 'local')?.key ?? null) : null;
  });

  /** Every tab the version could show, in their order (Q-UI-456). */
  private readonly announcedTabs = computed<readonly TabEntry[]>(() => {
    const d = this.detail();
    const announcement = this.docsAnnouncement();
    const vulnerabilities: TabEntry[] =
      this.vulnerabilityReport() !== null ? [{ id: 'vulnerabilities', label: $localize`:@@pkg.tab.vulnerabilities:Vulnerabilities` }] : [];
    // A graded version opens on its vulnerabilities; one that is not scanned opens on what it is, and the tab says why it is not.
    const gradedFirst = this.grade() !== null && this.grade()?.grade !== 'N/A';
    return [
      ...(gradedFirst ? vulnerabilities : []),
      ...(this.docsAnnounced() ? [{ id: 'documentation' as const, label: $localize`:@@pkg.tab.documentation:Documentation` }] : []),
      ...(this.docsAnnounced() && announcement?.state !== 'none'
        ? [
            {
              id: 'contents' as const,
              label: $localize`:@@pkg.tab.contents:Contents`,
              ...(announcement?.entries !== undefined ? { count: announcement.entries } : {}),
            },
          ]
        : []),
      ...(d?.hasReadme && d.format !== 'ansible' ? [{ id: 'readme' as const, label: $localize`:@@pkg.tab.readme:README` }] : []),
      { id: 'files', label: $localize`:@@pkg.tab.files:Files` },
      { id: 'details', label: $localize`:@@pkg.tab.details:Details` },
      ...(gradedFirst ? [] : vulnerabilities),
      ...(this.localRepository() !== null ? [{ id: 'import-log' as const, label: $localize`:@@pkg.tab.import-log:Import log` }] : []),
    ];
  });
  /** Whether the tab that is open needs the tree: asked of the announced tabs, so the tree is requested only when its tab is shown. */
  private readonly wantsDocs = computed(() => {
    const tabs = this.announcedTabs();
    const i = tabs.findIndex((t) => t.id === this.tab());
    const open = tabs[i < 0 ? 0 : i]?.id;
    return (open === 'documentation' || open === 'contents') && !this.docsReadmeOnly();
  });
  private readonly docsKey = computed(() => {
    const d = this.detail();
    return d ? `${d.packageId}@${d.version}` : null;
  });
  /** The version whose tree was asked for: the tree is read once per version and kept while the user looks at another tab. */
  private readonly docsAsked = signal<string | null>(null);
  protected readonly docs = resource({
    params: () => {
      const d = this.detail();
      return d && this.docsAsked() === this.docsKey() ? { id: d.packageId, version: d.version } : undefined;
    },
    loader: ({ params }) => this.api.docs(params.id, params.version),
  });
  /** The whole tree; null while it loads, and after a refusal (then the tabs are gone). */
  protected readonly docsTree = computed<PackageDocTree | null>(() => valueOf(this.docs) ?? null);
  private readonly docsGone = computed(() => this.docs.hasValue() && this.docs.value() === null);
  protected readonly docsFailure = computed(() => {
    const e = this.docs.error();
    return e ? asApiError(e) : null;
  });

  protected readonly tabs = computed<readonly TabEntry[]>(() =>
    this.docsGone() ? this.announcedTabs().filter((t) => t.id !== 'documentation' && t.id !== 'contents') : this.announcedTabs(),
  );
  protected readonly activeIndex = computed(() => {
    const i = this.tabs().findIndex((t) => t.id === this.tab());
    return i < 0 ? 0 : i;
  });
  /**
   * The tab that is open. The strip creates the content of every tab with the screen and shows one of them, so a tab that reads
   * from the server on its own (the documentation tabs, the import log, the findings) is drawn only while it is the open one.
   */
  protected readonly activeTab = computed(() => this.tabs()[this.activeIndex()]?.id);

  protected readonly loadFailure = computed(() => {
    const e = this.summary.error() ?? this.details.error();
    return e ? asApiError(e) : null;
  });
  protected readonly readmeFailure = computed(() => {
    const e = this.readme.error();
    return e ? asApiError(e) : null;
  });

  /** The page's title: the package and the version, as a reader names it. */
  protected readonly heading = computed(() => `${this.name()} ${this.version()}`);
  /** The distribution tags that point at this version (npm `latest`, Q-UI-453). */
  protected readonly tags = computed(() =>
    distTagsOf(this.detail()?.metadata)
      .filter(([, v]) => v === this.version())
      .map(([t]) => t),
  );
  protected readonly metadata = computed(() => this.detail()?.metadata ?? {});
  protected readonly dependencyGroups = computed(() => dependencyGroups(this.metadata()));
  protected readonly frameworkGroups = computed(() => frameworkGroups(this.metadata()['dependencyGroups']));
  /** A NuGet version the publisher unlisted (R-NUGET-006): it restores when pinned and is hidden from search. */
  protected readonly unlisted = computed(() => this.metadata()['listed'] === false);
  protected readonly engines = computed(() => stringRecord(this.metadata()['engines']));
  protected readonly authors = computed(() => strings(this.metadata()['authors']));
  protected readonly nugetFacts = computed(() => nugetFacts(this.metadata()));
  protected readonly debFacts = computed(() => debFacts(this.metadata()));
  protected readonly ansibleFacts = computed(() => {
    const d = this.detail();
    return d ? ansibleFacts(d, valueOf(this.summary)?.keywords ?? []) : [];
  });
  protected readonly deprecated = computed(() => deprecationOf(this.metadata()));
  protected readonly links = computed(() => {
    const d = this.detail();
    return d ? metadataLinks(d) : [];
  });
  /** The digest, media type, artifact type and size of an OCI tag, and the platforms of an index (R-OCI-031). */
  protected readonly image = computed(() => imageFacts(this.metadata()));

  /**
   * The collections a collection version depends on that this installation holds (Q-UI-245): each of them is a link to its page, the
   * others stay muted text. One lookup per dependency, which rejects for a package that no visible repository holds.
   */
  protected readonly heldDependencies = resource({
    params: () => {
      const d = this.detail();
      if (d?.format !== 'ansible') return undefined;
      const names = stringRecord(d.metadata['dependencies']).map(([name]) => name);
      return names.length > 0 ? names : undefined;
    },
    loader: async ({ params }): Promise<ReadonlySet<string>> => {
      const held = new Set<string>();
      await Promise.all(
        params.map(async (name) => {
          try {
            await this.api.lookup('ansible', name);
            held.add(name);
          } catch {
            // Not held, or not visible to this caller: the name stays text.
          }
        }),
      );
      return held;
    },
  });
  /** The router commands of a dependency's page, or null when the installation does not hold it. */
  protected dependencyLink(name: string): string[] | null {
    return this.detail()?.format === 'ansible' && valueOf(this.heldDependencies)?.has(name)
      ? packageCommands(this.project(), 'ansible', name)
      : null;
  }

  protected readonly externalRel = EXTERNAL_REL;
  protected readonly size = formatSize;
  /** A repository's page, inside its own project. */
  protected repositoryLink(qualified: string): string[] {
    const i = qualified.indexOf('/');
    return ['/projects', qualified.slice(0, i), 'repositories', qualified.slice(i + 1)];
  }

  protected readonly fileColumns: readonly TableColumn[] = [
    { id: 'path', label: $localize`:@@pkg.file.path:File` },
    { id: 'size', label: $localize`:@@pkg.file.size:Size`, numeric: true },
    {
      id: 'sha256',
      label: $localize`:@@pkg.file.sha256:SHA-256`,
      help: $localize`:@@pkg.help.sha256:The checksum of the file's content. Compare it with the output of sha256sum on the downloaded file.`,
    },
    { id: 'download', label: $localize`:@@pkg.file.download:Download`, hideLabel: true },
  ];
  protected readonly repositoryColumns: readonly TableColumn[] = [
    { id: 'repository', label: $localize`:@@pkg.repo.repository:Repository` },
    { id: 'class', label: $localize`:@@pkg.repo.class:Class` },
    { id: 'published', label: $localize`:@@pkg.repo.published:Published` },
  ];
  protected readonly platformColumns: readonly TableColumn[] = [
    { id: 'platform', label: $localize`:@@pkg.image.platform:Platform` },
    { id: 'digest', label: $localize`:@@pkg.image.digest:Digest` },
    { id: 'mediaType', label: $localize`:@@pkg.image.media-type:Media type` },
    { id: 'size', label: $localize`:@@pkg.image.size:Size`, numeric: true },
  ];
  protected readonly tableCaptionPlatforms = $localize`:@@pkg.image.platforms-caption:Platforms of this image index`;
  protected readonly notFetchedTitle = $localize`:@@pkg.image.not-fetched-title:No client has fetched this platform through Quader yet, so its manifest and its size are not held.`;
  protected readonly neverTitle = $localize`:@@pkg.ver.never-title:No client has downloaded this version through Quader yet.`;
  protected readonly copyImageDigest = $localize`:@@pkg.image.copy-digest:Copy digest`;
  protected readonly tableCaptionFiles = $localize`:@@pkg.file.caption:Files of this version`;
  protected readonly tableCaptionRepositories = $localize`:@@pkg.repo.caption:Repositories that hold this version`;
  protected readonly copySha = $localize`:@@pkg.file.copy-sha:Copy SHA-256`;
  protected readonly classLabels: Record<string, string> = {
    local: $localize`:@@repo.class.local:Local`,
    remote: $localize`:@@repo.class.remote:Remote`,
    virtual: $localize`:@@repo.class.virtual:Virtual`,
  };

  constructor() {
    // Opening the page records the package in "recently opened" (R-UI-007).
    effect(() => {
      const s = valueOf(this.summary);
      if (s) untracked(() => this.memory.addRecent({ format: s.format, name: s.name }));
    });
    // The tree is asked for when a tab that shows it is open, and then once for the version (R-UI-060).
    effect(() => {
      if (!this.wantsDocs()) return;
      const key = this.docsKey();
      untracked(() => this.docsAsked.set(key));
    });
  }

  protected selectTab(index: number): void {
    const id = this.tabs()[index]?.id;
    if (id && id !== this.tabs()[this.activeIndex()]?.id) void this.router.navigate([], { queryParams: { tab: id }, queryParamsHandling: 'merge' });
  }
}
