import { DownloadLink } from '../../shared/download-link';
import { NamespaceOf } from './namespace-of';
import { DecimalPipe, NgComponentOutlet, NgTemplateOutlet } from '@angular/common';
import { TimePipe } from '../../shared/time-pipe';
import { Component, computed, effect, inject, input, output, resource, signal, untracked } from '@angular/core';
import { Router, RouterLink } from '@angular/router';
import { PackageDeprecation, PackageDocTree, PackageSummary, PackageVersion, PackageVersionDocs } from '../../api/packages-api';
import { PackageReads } from '../../api/package-reads';
import { VulnerabilityVersionReport } from '../../api/vulnerabilities-api';
import { VulnerabilityReads } from '../../api/vulnerability-reads';
import { CopyButton } from '../../shared/copy-button';
import { ErrorNotice } from '../../shared/error-notice';
import { FormatIconView } from '../../shared/format-icon';
import { FormatCatalog, formatName } from '../../core/format-names';
import { classLabel, isSystemOnly } from '../../features/repositories/repository-class';
import { failureOf, valueOf } from '../../shared/paged-resource';
import { failureMayPass, noneOnRefusal } from '../../shared/none-on-refusal';
import { DataTable, TableColumn } from '../../shared/ui/data-table';
import { PageHeader } from '../../shared/ui/page-header';
import { TabGroup, TabPage } from '../../shared/ui/tabs';
import { checkWaits, worstGrade } from '../../shared/vulnerability/grade-words';
import { barSeverityOf, blockedRepositories, PackageVulnerabilities } from '../vulnerabilities/package-vulnerabilities';
import { AnsibleContents } from './ansible-contents';
import { AnsibleDocs } from './ansible-docs';
import { DebChangelog, DebCopyright, DebFiles } from './deb-contents';
import { AnsibleRoleFacts, AnsibleRoleReadmeNote, roleFacts, roleRemote } from './ansible-role';
import { DeliveryNote } from './delivery-note';
import { formatSize } from '../../features/packages/format-size';
import { TimeAgo } from '../../shared/ui/time-ago';
import { ClientCommandMenu } from './client-command-menu';
import { VersionSwitch } from './version-switch';
import { clamavInstallNote, versionCommand } from './version-command';
import { arrivedLabel, contentShapeOf, leadsWithDocumentation } from '../../features/repositories/content/content-shape';
import { EXTERNAL_REL, packageCommands, versionCommands } from '../../features/packages/package-links';
import { ImageDetails } from './image-details';
import { ImagePart, chartOf, chartValues, cutDigest, heldReason, imageFact, imageFactList, imageLinks, imageParts, kindWord } from './image-facts';
import { CodeWindow } from '../../shared/code/code-window';
import { PackageReadmeView } from './package-readme';
import { distTagsOf } from './package-versions-model';
import { DistTags } from './dist-tags';
import { GradeLabel } from '../../shared/vulnerability/grade';
import {
  DependencyGroup,
  ansibleDependencies,
  ansibleFacts,
  debFacts,
  debLongDescription,
  debRelations,
  debSource,
  deprecationReasonText,
  dependencyNames,
  imageFacts,
  installScriptText,
  licenseLink,
  metadataLinks,
  npmDependencies,
  npmFacts,
  nugetDependencies,
  nugetFacts,
  nugetFrameworks,
  nugetRepository,
  revisionLinks,
  nugetRuns,
  releaseNotes,
  strings,
} from './version-metadata';
import { DependencyList, VersionLinks } from './version-facts';
import { UsedBy } from './used-by';
import { PackageTags } from './package-tags';
import { VersionSize } from './version-size';
import { NoValue } from '../../shared/ui/no-value';
import { MalwareMark, malwareColumn } from '../../shared/malware/malware-mark';
import { MalwareSignaturesStore } from '../../core/malware-signatures';
import { malwareReasonText } from '../../shared/malware/malware-words';
import { MalwareStatus, MalwareSummary } from '../../api/malware-api';
import { SignatureGateRefusedReason, SignatureGateReason, SignatureStatus } from '../../api/signatures-api';
import { contentPath, repositoryOfContent } from '../../api/refs';
import { GATE_ACT, GateActInputs, VERSION_IMPORT_LOG } from '../acts';
import { gateReasonWord } from '../signatures/gate-words';
import { SignatureBadge } from '../signatures/signature-badge';
import { hasSignatureBadge } from '../signatures/signature-words';
import { VersionSignatures } from './version-signatures';
import { systemRepositoryNames, upstreamName, CLAMAV_KEY } from '../../core/upstream-project';
import { systemClosed } from '../../core/system-clients';
import { LIVE_FAST_MS, LIVE_IDLE_MS, live, refreshResource } from '../../core/live-refresh';
import { Disclosure } from '../../shared/ui/disclosure';
import {
  ClamavState,
  ageDays,
  ageOldText,
  clamavFacts,
  isOutdated,
  outdatedBadge,
  outdatedNote,
  stateText,
  verificationText,
} from './clamav-definitions';
import { Loading } from '../../shared/ui/loading';

/** Two answers name the same version. */
function sameVersion(a: { id: string; version: string } | undefined, b: { id: string; version: string } | undefined): boolean {
  return a === b || (!!a && !!b && a.id === b.id && a.version === b.version);
}

export type VersionTab =
  | 'vulnerabilities'
  | 'documentation'
  | 'contents'
  | 'readme'
  | 'values'
  | 'files'
  | 'signatures'
  | 'details'
  | 'changelog'
  | 'import-log';

/** The content routes of the system repositories, `/api/v1/projects/upstream/repositories/<key>/content/` (R-API-025, Q-UI-898). */
const SYSTEM_CONTENT = systemRepositoryNames().map((name) => contentPath(name, ''));
/** The formats whose dependencies name packages of the same format, which the block links where this installation holds them. */
const LINKED_DEPENDENCIES: ReadonlySet<string> = new Set(['npm', 'nuget', 'ansible', 'deb']);
/** How many dependencies of a version are looked up; a name past the bound stays text. */
const MAX_DEPENDENCY_LOOKUPS = 60;
/** How many of the newest versions the switch in the head lists before its link to all of them (Q-UI-1243). */
export const SWITCH_VERSIONS = 10;
/** The content route of `upstream/clamav`, whose files are downloaded while ClamAV definition updates is on (R-UI-194). */
const CLAMAV_CONTENT = contentPath(upstreamName(CLAMAV_KEY), '');

interface TabEntry {
  readonly id: VersionTab;
  readonly label: string;
  /** Shown in the strip as a neutral badge after the label (the Contents tab, R-UI-062). */
  readonly count?: number;
}

/**
 * The view of one version (R-UI-111, R-UI-223, the owner's walk of 2026-11-18): `/packages/{format}/{name…}@{version}`, an address of its
 * own that can be linked and sent; for a format whose packages lead with their documentation (R-UI-218: Ansible Galaxy, npm, NuGet) the
 * package's own address `/packages/{format}/{name…}` is this page of its newest version (`version` absent: the package's
 * `latestVersion`, the highest that is no pre-release, Q-UI-1242). The header names the package and the version with its tags and its
 * facts (published, size or that it is not known, the last download by a client, the downloads, licences; R-API-079, R-API-080) and its
 * vulnerabilities in one statement (R-UI-112); at its right the switch between the versions where the format leads with its
 * documentation (Q-UI-1243) and the button with the format's client verb that opens the clients' commands (Q-UI-1241), or the note
 * why no client can install the version. The tabs: **Vulnerabilities** (a grade card per repository and the open findings at the page's
 * width, R-VULN-023), the documentation of an Ansible collection (R-UI-060), **README**, **Files**, **Details** (the format's
 * metadata and the repositories that hold the version) and an Ansible collection's **Import log**. The open tab is the `tab`
 * parameter; a format that leads with its documentation shows it first and opens on it where the version carries one (Q-UI-1245),
 * else the first tab is Vulnerabilities where the version is graded, else its documentation (Q-UI-456).
 *
 * A view (R-UI-223): it reads through `PackageReads` and `VulnerabilityReads`, takes `signedIn` as an input and acts nowhere. The page
 * that composes it projects the acts of the head (`[versionActs]`, the deprecation's menu) and their refusals above the tabs
 * (`[versionNotices]`), and provides the places of views/acts.ts: the signature gate's release in a refused artifact's banner and the
 * tab Import log. It tells the page the package it opened (`packageOpened`) and the version it shows (`versionShown`).
 */
@Component({
  selector: 'quader-package-version-view',
  imports: [
    Loading,
    NamespaceOf,
    TimeAgo,
    DecimalPipe,
    RouterLink,
    TabGroup,
    TabPage,
    PageHeader,
    CopyButton,
    ImageDetails,
    CodeWindow,
    DataTable,
    ErrorNotice,
    FormatIconView,
    ClientCommandMenu,
    VersionSwitch,
    PackageReadmeView,
    AnsibleRoleFacts,
    AnsibleRoleReadmeNote,
    DeliveryNote,
    AnsibleContents,
    AnsibleDocs,
    NgComponentOutlet,
    DebChangelog,
    DebCopyright,
    DebFiles,
    PackageVulnerabilities,
    GradeLabel,
    VersionSize,
    NoValue,
    DownloadLink,
    MalwareMark,
    NgTemplateOutlet,
    SignatureBadge,
    VersionSignatures,
    Disclosure,
    TimePipe,
    DistTags,
    DependencyList,
    UsedBy,
    PackageTags,
    VersionLinks,
  ],
  templateUrl: './package-version-view.html',
  styleUrl: './package-version-view.scss',
})
export class PackageVersionView {
  private readonly api = inject(PackageReads);
  private readonly vulnerabilityApi = inject(VulnerabilityReads);
  private readonly router = inject(Router);

  /** Route parameters: the project, then the matcher's format, name and version. */
  readonly project = input.required<string>();
  readonly format = input.required<string>();
  /** Whether the name's namespace links to its page; false where the application has none (the Bastion's, Q-UI-1443). */
  readonly namespaceLinks = input(true);
  /**
   * Whether the section Used by is offered; false where the application reads no package's dependents (the Bastion's catalogue holds
   * no such document, Q-UI-1446), which would stand as a failure in place of the section.
   */
  readonly dependents = input(true);
  /**
   * Whether the application has download figures; false where it has none (the Bastion's catalogue carries no download count and no
   * time of a last download, Q-BAS-358): no column Last download, no count, no "never" (Q-UI-1449).
   */
  readonly downloads = input(true);
  /** Whether a finding's identifier links to its advisory's page; false where the application has none (the Bastion's, Q-UI-1450). */
  readonly advisoryLinks = input(true);
  /**
   * Whether the application shows the documentation a version carries: its README, an Ansible collection's documentation and contents,
   * a Debian package's changelog and copyright. False at a Bastion whose documentation is switched off (bastion.md R-BAS-126, R-UI-232):
   * those tabs are gone at once, before the catalogue drops their documents, and the version opens on its first other tab.
   */
  readonly documentation = input(true);
  /** The format's name in the header, never its key (R-UI-006): **OCI (Docker)**, not `oci`. */
  protected readonly formatName = formatName(() => this.format());
  readonly name = input.required<string>();
  /** The version of the address; absent on a package's own address, which shows its newest version (R-UI-218, Q-UI-1242). */
  readonly version = input<string | undefined>();
  /** Query parameters: the open tab, the open page of the Documentation tab and its search text (R-UI-060), the severity filter. */
  readonly tab = input<string | undefined>();
  readonly doc = input<string | undefined>();
  readonly q = input<string | undefined>();
  readonly severity = input<string | undefined>();
  /**
   * The reader has a session: a visitor without one reads the tab Vulnerabilities from the report's own findings, each linking to its
   * advisory, and is offered no link into a screen that needs a session (R-UI-224).
   */
  readonly signedIn = input.required<boolean>();
  /** The package the view opened, once it has answered and whenever it answers anew: the page records it (R-UI-007). */
  readonly packageOpened = output<PackageSummary>();
  /** The version the view shows, null until it has answered: the page's acts are about it. */
  readonly versionShown = output<PackageVersion | null>();

  protected readonly summary = resource({
    params: () => ({ format: this.format(), name: this.name() }),
    loader: ({ params }) => this.api.lookup(params.format, params.name),
  });
  protected readonly pkg = computed(() => valueOf(this.summary));
  /** The version shown: the address's, or the package's newest (Q-UI-1242); null until the package has answered. */
  protected readonly shown = computed(() => this.version() ?? this.pkg()?.latestVersion ?? null);
  /** The format's packages lead with their documentation (R-UI-218): it comes first, and the head switches between the versions. */
  protected readonly leads = computed(() => leadsWithDocumentation(this.format()));
  /** The documentation comes first (R-UI-218): where the format leads with it and the application shows it. */
  private readonly docsFirst = computed(() => this.leads() && this.documentation());
  protected readonly details = resource({
    params: () => {
      const id = valueOf(this.summary)?.id;
      const version = this.shown();
      return id && version ? { id, version } : undefined;
    },
    loader: ({ params }) => this.api.version(params.id, params.version),
  });
  protected readonly detail = computed(() => valueOf(this.details));
  /**
   * The README's version, equal by value: the page reads the version again while it is open (R-UI-213), and a new answer of the same
   * version must not ask for its README again.
   */
  private readonly readmeKey = computed(
    () => {
      const d = this.detail();
      // The README of an Ansible collection is the first page of its Documentation tab (R-UI-060), not a tab of its own; a role's is
      // its own tab, the first of its page (R-ANS-056).
      return d?.hasReadme && this.documentation() && (d.format !== 'ansible' || roleFacts(d) !== null)
        ? { id: d.packageId, version: d.version }
        : undefined;
    },
    { equal: sameVersion },
  );
  protected readonly readme = resource({
    params: () => this.readmeKey(),
    loader: ({ params }) => this.api.readme(params.id, params.version),
  });

  /**
   * What the scanning says of the version in every readable repository (R-VULN-023). The page works without it: a refusal (no
   * scanning on this replica, a reader who may not ask) leaves no tab, no grade and no banner, and no error. A failure that may pass is
   * shown and read again (R-UI-216): swallowed, the page hid the tab, the grade and the banner "Blocked in" until a reload.
   */
  protected readonly vulnerabilities = resource({
    params: () => {
      const id = valueOf(this.summary)?.id;
      const version = this.shown();
      return id && version ? { id, version } : undefined;
    },
    loader: ({ params }): Promise<VulnerabilityVersionReport | null> =>
      noneOnRefusal(this.vulnerabilityApi.versionReport(params.id, params.version)),
  });
  protected readonly vulnerabilityReport = computed(() => valueOf(this.vulnerabilities) ?? null);
  /** The version's grade, the worst of its repositories (R-VULN-038, Q-UI-375): the same grade the row of the versions table shows. */
  protected readonly grade = computed(() => worstGrade((this.vulnerabilityReport()?.repositories ?? []).map((r) => r.grade)));
  /** The link the grade's details end with: the tab of the version's findings, on this page (Q-UI-1538). */
  protected readonly findingsTab = { commands: [], query: { tab: 'vulnerabilities' } } as const;
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
    if (!this.documentation()) return false;
    const a = this.docsAnnouncement();
    return a !== null && (a.state !== 'none' || this.detail()?.hasReadme === true);
  });
  protected readonly docsReadmeOnly = computed(() => this.docsAnnounced() && this.docsAnnouncement()?.state === 'none');
  /** The tab Import log the page provides (R-UI-063, views/acts.ts); none in an application without the main installation's acts. */
  protected readonly importLog = inject(VERSION_IMPORT_LOG, { optional: true });
  /** The local repository that holds the version: the Import log tab reads its tasks (R-UI-063). */
  protected readonly localRepository = computed(() => {
    const d = this.detail();
    return d?.format === 'ansible' && this.importLog?.offered() ? (d.repositories.find((r) => r.class === 'local')?.key ?? null) : null;
  });

  /** Every tab the version could show, in their order (Q-UI-456). */
  private readonly announcedTabs = computed<readonly TabEntry[]>(() => {
    const d = this.detail();
    const announcement = this.docsAnnouncement();
    const vulnerabilities: TabEntry[] =
      this.vulnerabilityReport() !== null ? [{ id: 'vulnerabilities', label: $localize`:@@pkg.tab.vulnerabilities:Vulnerabilities` }] : [];
    // A graded version opens on its vulnerabilities; one that is not scanned opens on what it is, and the tab says why it is not. A
    // format that leads with its documentation shows it first (R-UI-218): its README tab stands even where the version has none, and
    // says so (Q-UI-1245).
    const gradedFirst = this.grade() !== null && this.grade()?.grade !== 'N/A';
    // A role's README tab is there before its README is: it says why there is none yet (R-ANS-056).
    const readme: TabEntry[] =
      d && this.documentation() && ((d.format !== 'ansible' && (d.hasReadme || this.leads())) || roleFacts(d) !== null)
        ? [{ id: 'readme', label: $localize`:@@pkg.tab.readme:README` }]
        : [];
    return [
      ...(this.docsFirst() ? readme : []),
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
      ...(this.docsFirst() ? [] : readme),
      // A Helm chart's values.yaml, its documentation beside the README (R-OCI-042).
      ...(this.chartValues() ? [{ id: 'values' as const, label: $localize`:@@pkg.tab.values:Values` }] : []),
      { id: 'files', label: $localize`:@@pkg.tab.files:Files` },
      // The details of the signatures while the installation checks them (R-SIG-035): the member is absent while they are off.
      ...(d?.signatures !== undefined ? [{ id: 'signatures' as const, label: $localize`:@@pkg.tab.signatures:Signatures` }] : []),
      { id: 'details', label: $localize`:@@pkg.tab.details:Details` },
      // The changelog of a Debian package file (R-DEB-039, Q-UI-1272): a detail, after the facts.
      ...(d?.format === 'deb' && this.documentation() ? [{ id: 'changelog' as const, label: $localize`:@@pkg.tab.changelog:Changelog` }] : []),
      ...(gradedFirst ? [] : vulnerabilities),
      ...(this.localRepository() !== null ? [{ id: 'import-log' as const, label: $localize`:@@pkg.tab.import-log:Import log` }] : []),
    ];
  });
  /** Whether the tab that is open needs the tree: asked of the announced tabs, so the tree is requested only when its tab is shown. */
  private readonly wantsDocs = computed(() => {
    const tabs = this.announcedTabs();
    const first = tabs.find((t) => t.id !== 'readme' || this.detail()?.hasReadme === true)?.id;
    const open = tabs.find((t) => t.id === this.tab())?.id ?? first;
    return (open === 'documentation' || open === 'contents') && !this.docsReadmeOnly();
  });
  private readonly docsKey = computed(() => {
    const d = this.detail();
    return d ? `${d.packageId}@${d.version}` : null;
  });
  /** The version whose tree was asked for: the tree is read once per version and kept while the user looks at another tab. */
  private readonly docsAsked = signal<string | null>(null);
  private readonly docsVersion = computed(
    () => {
      const d = this.detail();
      return d && this.docsAsked() === this.docsKey() ? { id: d.packageId, version: d.version } : undefined;
    },
    { equal: sameVersion },
  );
  protected readonly docs = resource({
    params: () => this.docsVersion(),
    loader: ({ params }) => this.api.docs(params.id, params.version),
  });
  /** The whole tree; null while it loads, and after a refusal (then the tabs are gone). */
  protected readonly docsTree = computed<PackageDocTree | null>(() => valueOf(this.docs) ?? null);
  private readonly docsGone = computed(() => this.docs.hasValue() && this.docs.value() === null);
  /** The tree's failure, kept while its tab reads it again (R-UI-216). */
  protected readonly docsFailure = failureOf(this.docs);

  /**
   * What Quader read from a Debian package file of the version (R-DEB-039): the changelog, the copyright and the first page of the
   * files. Read once per version and again with the page while the reader has not run yet (R-UI-213).
   */
  private readonly debKey = computed(
    () => {
      const d = this.detail();
      return d?.format === 'deb' ? { id: d.packageId, version: d.version } : undefined;
    },
    { equal: sameVersion },
  );
  protected readonly debContents = resource({
    params: () => this.debKey(),
    loader: ({ params }) => this.api.contents(params.id, params.version, { limit: 100 }),
  });
  protected readonly debContentsValue = computed(() => valueOf(this.debContents) ?? null);
  protected readonly debContentsFailure = failureOf(this.debContents);

  protected readonly tabs = computed<readonly TabEntry[]>(() =>
    this.docsGone() ? this.announcedTabs().filter((t) => t.id !== 'documentation' && t.id !== 'contents') : this.announcedTabs(),
  );
  /** The version the page shows, by value. */
  private readonly shownVersion = computed(() => `${this.format()}\n${this.name()}\n${this.shown() ?? ''}`);
  /**
   * The tab the page opens on: the first, but a README tab of a version without a README (Q-UI-1245), which says only that there is
   * none: a format that leads with its documentation opens such a version on its first tab with content. A role opens on its README
   * tab all the same, whose note says why it has none yet and how a role is published (R-ANS-056, Q-ANS-111).
   */
  private readonly firstTab = computed(
    () => this.tabs().find((t) => t.id !== 'readme' || this.detail()?.hasReadme === true || this.role() !== null)?.id,
  );
  /**
   * The tab the page opened on without a tab in its address, fixed once the version and its scanning have answered: the page reads
   * the version again while it is open (R-UI-213), and a scan that finishes meanwhile adds the tab Vulnerabilities in front, which
   * must not switch the tab the person reads.
   */
  private readonly landingTab = signal<{ readonly key: string; readonly id: VersionTab } | null>(null);
  protected readonly activeIndex = computed(() => {
    const landing = this.landingTab();
    const wanted = this.tab() ?? (landing?.key === this.shownVersion() ? landing.id : this.firstTab());
    const i = this.tabs().findIndex((t) => t.id === wanted);
    return i < 0 ? 0 : i;
  });
  /**
   * The tab that is open. The strip creates the content of every tab with the screen and shows one of them, so a tab that reads
   * from the server on its own (the documentation tabs, the import log, the findings) is drawn only while it is the open one.
   */
  protected readonly activeTab = computed(() => this.tabs()[this.activeIndex()]?.id);


  /** The page's title: the package and the version, as a reader names it. */
  protected readonly heading = computed(() => `${this.pkg()?.name ?? this.name()} ${this.shown() ?? ''}`.trimEnd());
  /** The facts of a role an Ansible Galaxy remote read (R-ANS-056); null for every other version. */
  protected readonly role = computed(() => roleFacts(this.detail()));
  /** The remote that holds the role, whose tab Upstream decides it. */
  protected readonly roleRemote = computed(() => {
    const d = this.detail();
    return d && this.role() ? roleRemote(d) : null;
  });
  /** The distribution tags that point at this version (npm `latest`, Q-UI-453). */
  protected readonly tags = computed(() =>
    distTagsOf(this.detail()?.metadata)
      .filter(([, v]) => v === this.shown())
      .map(([t]) => t),
  );
  protected readonly metadata = computed(() => this.detail()?.metadata ?? {});
  /**
   * The version's dependencies in the one block of every format (R-UI-221): npm's fields, NuGet's target frameworks, an Ansible
   * collection's list, Debian's relations, a Helm chart's dependencies.
   */
  protected readonly dependencies = computed<readonly DependencyGroup[]>(() => {
    const m = this.metadata();
    switch (this.detail()?.format) {
      case 'npm':
        return npmDependencies(m);
      case 'nuget':
        return nugetDependencies(m['dependencyGroups']);
      case 'ansible':
        return ansibleDependencies(m);
      case 'deb':
        return debRelations(m);
      case 'oci': {
        const deps = this.chart()?.dependencies ?? [];
        const entries = deps.map((d) => ({
          alternatives: [{ name: d.name, written: d.name, qualifiers: d.repository === '' ? d.range : `${d.range} · ${d.repository}` }],
          mark: null,
        }));
        return entries.length > 0 ? [{ label: null, entries, text: null }] : [];
      }
      default:
        return [];
    }
  });
  /**
   * The section **Used by** (R-API-086): in every format whose versions name dependencies of its own packages, on a version's page; a
   * remote's tab Upstream has none (an upstream's dependents are not this installation's to know), and an Ansible role none (a collection
   * depends on collections).
   */
  protected readonly usedBy = computed(
    () => this.dependents() && LINKED_DEPENDENCIES.has(this.detail()?.format ?? '') && this.role() === null,
  );
  /** The package's normalized name, which the filter of the package list takes; the address's name until the package has answered. */
  protected readonly packageKey = computed(() => this.pkg()?.key ?? this.name());
  /** The keywords or tags the version declares (R-UI-221): chips to the packages that share one. */
  protected readonly keywords = computed(() => this.detail()?.keywords ?? []);
  /** A NuGet version the publisher unlisted (R-NUGET-006): it restores when pinned and is hidden from search. */
  protected readonly unlisted = computed(() => this.detail()?.unlisted === true);
  protected readonly authors = computed(() => strings(this.metadata()['authors']));
  /**
   * The facts of the format that only it has, in its section **Package details** (R-UI-221): npm's runtimes, types, the tarball's facts and
   * its publisher; NuGet's; Debian's control fields; an Ansible collection's Ansible versions; an image's annotations and labels.
   */
  protected readonly packageFacts = computed<readonly [string, string][]>(() => {
    const d = this.detail();
    switch (d?.format) {
      case 'npm':
        return npmFacts(d.metadata);
      case 'nuget':
        return nugetFacts(d.metadata);
      case 'deb':
        return debFacts(d.metadata);
      case 'ansible':
        return ansibleFacts(d);
      case 'oci':
        return imageFactList(d.metadata);
      default:
        return [];
    }
  });
  /** The test id of the section, by the format (an image's facts are its annotations' and labels'). */
  protected readonly packageFactsId = computed(() => (this.format() === 'oci' ? 'image-facts' : `${this.format()}-facts`));
  /** The version runs a script of its own when it is installed (npm, R-NPM-007): said under the facts, where a person decides. */
  protected readonly installScript = computed(() => (this.metadata()['hasInstallScript'] === true ? installScriptText() : null));
  /** The address of the licence, beside the licence on the facts line (R-UI-221). */
  protected readonly licenseHref = computed(() => licenseLink(this.metadata()));
  /**
   * The licence file a NuGet version's package embeds (R-NUGET-038): this installation's address of its text, the licence's link on the
   * facts line; null for a licence expression or none.
   */
  protected readonly licenseFile = computed(() => {
    const d = this.detail();
    const file = this.metadata()['licenseFile'];
    return d && typeof file === 'string' && file !== '' ? { href: this.api.licenseUrl(d.packageId, d.version), name: file } : null;
  });
  /**
   * The picture the version's package embeds (NuGet's icon, R-NUGET-038), in the page's head: read through this installation's API, which
   * serves a PNG or a JPEG only; nothing where the version has none or the image does not load.
   */
  protected readonly icon = computed(() => {
    const d = this.detail();
    return d && this.metadata()['hasIcon'] === true && this.iconFailed() !== `${d.packageId}@${d.version}` ? this.api.iconUrl(d.packageId, d.version) : null;
  });
  /** The version whose icon did not load: the head shows none for it. */
  protected readonly iconFailed = signal<string | null>(null);
  protected iconError(): void {
    const d = this.detail();
    if (d) this.iconFailed.set(`${d.packageId}@${d.version}`);
  }
  /** NuGet's target frameworks, the folders' and the dependency groups' beside them where they differ (R-NUGET-038). */
  protected readonly frameworks = computed(() => (this.format() === 'nuget' ? nugetFrameworks(this.metadata()) : { folders: [], dependencies: [] }));
  /**
   * The symbol package the version holds (NuGet's `.snupkg`, R-NUGET-020, R-NUGET-038): its file's name, for the fact **Symbols**; null
   * for none.
   */
  protected readonly symbols = computed(() => {
    const f = this.format() === 'nuget' ? this.detail()?.files.find((x) => x.path.toLowerCase().endsWith('.snupkg')) : undefined;
    return f ? this.fileName(f.path) : null;
  });
  /** The release notes of a NuGet version (R-NUGET-038): a section of their own, their line breaks kept, as text. */
  protected readonly releaseNotes = computed(() => (this.format() === 'nuget' ? releaseNotes(this.metadata()) : ''));
  /**
   * What runs when a project uses a NuGet package (R-NUGET-039, H-2171): the build logic MSBuild imports into every build, the analyzers
   * the compiler loads, the PowerShell scripts Visual Studio runs for a packages.config project; said under the links where npm's install
   * script is said, each with the files, as text.
   */
  protected readonly runs = computed(() => {
    const r = this.format() === 'nuget' ? nugetRuns(this.metadata()) : null;
    if (!r) return [];
    const list = (files: readonly string[]) => files.join(', ');
    const out: { readonly kind: string; readonly icon: string; readonly text: string; readonly files: string }[] = [];
    if (r.build.length > 0) {
      out.push({ kind: 'build', icon: 'build', text: $localize`:@@pkg.runs.build:Adds build logic that runs in every build of a project that references it:`, files: list(r.build) });
    }
    if (r.analyzers.length > 0) {
      out.push({ kind: 'analyzers', icon: 'code', text: $localize`:@@pkg.runs.analyzers:Adds analyzers or source generators that run in the compiler:`, files: list(r.analyzers) });
    }
    if (r.scripts.length > 0) {
      out.push({ kind: 'scripts', icon: 'terminal', text: $localize`:@@pkg.runs.scripts:Runs PowerShell scripts when Visual Studio installs it into a packages.config project:`, files: list(r.scripts) });
    }
    return out;
  });
  /**
   * The word of the date the version arrived (Q-UI-1314): an image's is Pushed, or Fetched where only remote repositories hold it, as in
   * the repository's content; every other format's is Published.
   */
  protected readonly arrived = computed(() =>
    arrivedLabel(this.format(), (this.detail()?.repositories ?? []).map((r) => r.class)),
  );
  protected readonly debLongDescription = computed(() => debLongDescription(this.metadata()));
  protected readonly debSource = computed(() => debSource(this.metadata()));
  /** The package list filtered by the source package: the binaries of one source (R-DEB-038, Q-DEB-142). */
  protected readonly sourceLink = computed(() => ['/projects', this.project(), 'packages']);
  protected readonly copyrightHeading = $localize`:@@pkg.meta.deb-copyright-file:Copyright file`;
  /** A Helm chart's facts (R-OCI-042); null for anything else. */
  protected readonly chart = computed(() => (this.format() === 'oci' ? chartOf(this.metadata()) : null));
  /** Why a chart's facts and values are held (R-OCI-040): a download of its files would not be served to this reader now. */
  protected readonly chartHeld = computed(() =>
    this.format() === 'oci' && this.metadata()['kind'] === 'helm-chart' && !this.chart() ? heldReason(this.metadata()) : null,
  );
  /** A Helm chart's values.yaml (R-OCI-042). */
  protected readonly chartValues = computed(() => (this.format() === 'oci' ? chartValues(this.metadata()) : null));
  protected readonly copyValues = $localize`:@@pkg.chart.copy-values:Copy values.yaml`;
  /** The version's deprecation, its own or its package's (R-API-085): the banner says it, with NuGet's reasons and alternate package. */
  protected readonly deprecated = computed<PackageDeprecation | null>(() => this.detail()?.deprecation ?? null);
  protected readonly reasonsText = computed(() => (this.deprecated()?.reasons ?? []).map(deprecationReasonText).join('; '));
  /** The alternate package's page where the reader may read it (R-API-085), else none and its name stays text. */
  protected readonly alternateLink = computed(() => {
    const a = this.deprecated()?.alternatePackage;
    return a?.packageId ? packageCommands(this.project(), this.format(), a.name) : null;
  });
  protected readonly alternateRange = computed(() => {
    const r = this.deprecated()?.alternatePackage?.range;
    return r && r !== '*' ? r : null;
  });

  /**
   * After an act of the page changed the version (a deprecation): the view, its switch and the package's row show the new state at
   * once, without a reload (R-UI-213).
   */
  changed(): void {
    this.details.reload();
    this.switchVersions.reload();
    this.summary.reload();
  }
  protected readonly links = computed(() => {
    const d = this.detail();
    if (!d) return [];
    if (d.format === 'oci') return imageLinks(d.metadata);
    // NuGet's repository branch and commit after its source (R-NUGET-038, Q-UI-1362): links where the page knows the host's addresses.
    return d.format === 'nuget' ? [...metadataLinks(d), ...revisionLinks(nugetRepository(d.metadata))] : metadataLinks(d);
  });
  /** The digest, media type, artifact type and size of an OCI tag, and the platforms of an index (R-OCI-031). */
  protected readonly image = computed(() => imageFacts(this.metadata()));

  // ---- The clients' commands and the switch between the versions in the head (Q-UI-1241, Q-UI-1243) ----

  /** The format's client verb, the button's word (Q-UI-1066). */
  protected readonly verb = computed(() => contentShapeOf(this.format()).commandVerb);
  /** What the head offers a client: the verb's button, ClamAV's Set up client, or the note why no client can install the version. */
  protected readonly command = computed(() => {
    const d = this.detail();
    return d ? versionCommand(this.format(), d, this.clamavClosed()) : null;
  });
  protected readonly clamavNote = clamavInstallNote();
  /**
   * The newest versions the switch offers (Q-UI-1243): one page of ten by the format's order, read with the page and again while it is
   * seen (R-UI-213), only where the format leads with its documentation.
   */
  protected readonly switchVersions = resource({
    params: () => (this.leads() ? this.pkg()?.id : undefined),
    loader: async ({ params }) => (await this.api.versions(params, { sort: '-version' })(null, SWITCH_VERSIONS)).items,
  });
  /** The tab the switch keeps for the version it switches to: the one in the address (the same tab of the other version). */
  protected readonly switchTab = computed(() => this.tab() ?? null);

  // ---- An image's facts (formats/oci.md R-OCI-039 to R-OCI-041) ----

  /**
   * The description under the name: an image's own, from its annotations or else its labels (R-OCI-039), the package's for every
   * other format and for an image that has none.
   */
  protected readonly description = computed(() => {
    const own = this.format() === 'oci' ? imageFact(this.metadata(), 'description') : '';
    return own !== '' ? own : this.pkg()?.description;
  });
  /** The licence of the facts line: the version's, or an image's from its labels. */
  protected readonly licenses = computed<readonly string[]>(() => {
    const d = this.detail();
    if (!d) return [];
    if (d.licenses.length > 0 || d.format !== 'oci') return d.licenses;
    const labeled = imageFact(d.metadata, 'licenses');
    return labeled !== '' ? [labeled] : [];
  });
  /**
   * The version's kind where its format has one: an OCI artifact's (`image`, `helm-chart`, …, R-OCI-042), a NuGet package's
   * (`dotnet-tool`, `template`, `msbuild-sdk`, R-NUGET-037); the head's panel offers the kind's clients and forms. null for none.
   */
  protected readonly artifactKind = computed(() => {
    const k = this.metadata()['kind'];
    return typeof k === 'string' ? k : null;
  });
  /** An image's digest, the panel's pull by digest (R-OCI-041). */
  protected readonly imageDigest = computed(() => {
    const d = this.metadata()['digest'];
    return typeof d === 'string' && d.startsWith('sha256:') ? d : null;
  });
  /** The other tags of the image's digest (R-OCI-041), each a link to its page. */
  protected readonly otherTags = computed(() => this.detail()?.aliases ?? []);
  protected readonly otherTagsMore = computed(() => this.detail()?.aliasesMore === true);
  protected tagLink(tag: string): string[] {
    return versionCommands(this.project(), this.format(), this.name(), tag);
  }
  protected readonly otherTagsLabel = $localize`:@@pkg.image.other-tags:Other tags of this image`;
  /** The authors of an image, from its annotations or else its labels; every other format's list. */
  protected readonly authorList = computed(() => {
    if (this.format() !== 'oci') return this.authors();
    const a = imageFact(this.metadata(), 'authors');
    return a !== '' ? [a] : [];
  });
  /** The word for what the tag holds, with its media type as the title (R-OCI-041). */
  protected readonly kind = computed(() => kindWord(typeof this.metadata()['kind'] === 'string' ? (this.metadata()['kind'] as string) : ''));
  /** The images of the version: the tag's own, or each platform of an index the repository holds, with its configuration and layers. */
  protected readonly parts = computed<readonly ImagePart[]>(() => {
    const img = this.image();
    return img ? imageParts(this.metadata(), img.platforms) : [];
  });
  /** The image of a platform's row, when the repository holds it. */
  protected partOf(digest: string): ImagePart | null {
    return this.parts().find((p) => p.digest === digest) ?? null;
  }
  /** The platforms whose row is opened. */
  protected readonly openPlatforms = signal<ReadonlySet<string>>(new Set());
  protected togglePlatform(digest: string): void {
    const next = new Set(this.openPlatforms());
    if (next.has(digest)) next.delete(digest);
    else next.add(digest);
    this.openPlatforms.set(next);
  }
  protected platformOpenLabel(platform: string): string {
    return $localize`:@@pkg.image.open-platform:Configuration and layers of ${platform}:platform:`;
  }
  /** An image opens its configuration, layers and history; another artifact (a Helm chart) its layers alone. */
  protected readonly imageDetailsHeading = computed(() =>
    this.metadata()['kind'] === 'image' ? $localize`:@@pkg.image.details:Configuration, layers and history` : $localize`:@@pkg.layers.heading:Layers`,
  );
  /** The heading of the manifest's facts: an image's, or another artifact's (a Helm chart, a signature). */
  protected readonly imageHeading = computed(() =>
    this.metadata()['kind'] === 'image' || this.metadata()['kind'] === undefined ? $localize`:@@pkg.image.heading:Image` : $localize`:@@pkg.image.artifact-heading:Artifact`,
  );
  protected readonly cut = cutDigest;
  protected readonly copyManifest = $localize`:@@pkg.file.copy-manifest:Copy the manifest's digest`;
  protected readonly copyConfig = $localize`:@@pkg.file.copy-config:Copy the config's digest`;
  protected copyLayerOf(platform: string | null, n: number): string {
    return platform === null
      ? $localize`:@@pkg.layers.copy:Copy the digest of layer ${n}:number:`
      : $localize`:@@pkg.file.copy-platform-layer:Copy the digest of layer ${n}:number: of ${platform}:platform:`;
  }

  // ---- The ClamAV definitions (R-UI-194, R-CLAMAV-003) ----

  /** Every piece of a definition container's metadata: the header, the verification, the signature file, the members. */
  protected readonly clamav = computed(() => (this.format() === 'clamav' ? clamavFacts(this.metadata()) : null));
  /** `upstream/clamav` answers no client now: its files offer no download (R-UI-193 while closed, Q-UI-908). */
  private readonly clamavClosed = systemClosed(() => (this.format() === 'clamav' ? upstreamName(CLAMAV_KEY) : null));
  protected readonly clamavAge = computed(() => {
    const days = ageDays(this.clamav()?.builtAt ?? null, Date.now());
    return days === null ? null : ageOldText(days);
  });
  /** Only `daily` is outdated by its age (Q-UI-912). */
  protected readonly clamavOutdated = computed(() => {
    const c = this.clamav();
    return c ? isOutdated(c.database, c.builtAt, Date.now()) : false;
  });
  protected readonly outdatedWord = outdatedBadge();
  protected readonly outdatedText = outdatedNote();
  protected readonly stateText = stateText;
  protected readonly verification = computed(() => {
    const c = this.clamav();
    return c ? verificationText(c) : '';
  });
  protected readonly membersColumns: readonly TableColumn[] = [
    { id: 'name', label: $localize`:@@clamav.member.name:Name` },
    { id: 'size', label: $localize`:@@pkg.file.size:Size`, numeric: true },
    {
      id: 'lines',
      label: $localize`:@@clamav.member.lines:Lines`,
      numeric: true,
      help: $localize`:@@clamav.member.lines-help:The member's lines that are neither empty nor a comment.`,
    },
  ];
  protected readonly membersCaption = $localize`:@@clamav.members:Members`;
  protected readonly checksumsHeading = $localize`:@@clamav.checksums:Checksums of the archive`;
  protected readonly memberDigestsHeading = $localize`:@@clamav.member.digests:SHA-256 of each member`;
  /** The badge class of a version's state: the one in use is the positive one (R-UI-194). */
  protected stateClass(state: ClamavState): string {
    return state === 'in_use' ? 'ok' : state === 'files_removed' ? 'badge muted' : 'badge';
  }

  /**
   * The packages a version depends on that this installation holds (Q-UI-245, R-UI-221): each of them is a link to its page, the others
   * stay text, in every format whose dependencies name its own packages (a Helm chart's name a chart repository's). One lookup per
   * dependency, which rejects for a package that no visible repository holds; the lookups are bounded, a name past the bound stays text.
   */
  private readonly dependencyNames = computed(
    () => {
      const d = this.detail();
      if (!d || !LINKED_DEPENDENCIES.has(d.format)) return undefined;
      const names = dependencyNames(this.dependencies()).slice(0, MAX_DEPENDENCY_LOOKUPS);
      return names.length > 0 ? { format: d.format, names } : undefined;
    },
    {
      equal: (a, b) =>
        (a?.names ?? []).join('\n') === (b?.names ?? []).join('\n') && a?.format === b?.format && (a === undefined) === (b === undefined),
    },
  );
  protected readonly heldDependencies = resource({
    params: () => this.dependencyNames(),
    loader: async ({ params }): Promise<ReadonlySet<string>> => {
      const held = new Set<string>();
      await Promise.all(
        params.names.map(async (name) => {
          try {
            await this.api.lookup(params.format, name);
            held.add(name);
          } catch (e) {
            // Not held, or not visible to this caller: the name stays text. A failure that may pass is no such answer: the lookups
            // are read again with the page (R-UI-216).
            if (failureMayPass(e)) throw e;
          }
        }),
      );
      return held;
    },
  });
  /**
   * The failure of the version's read, kept while its notice reads it again (R-UI-216); the reads beside it are read again with it
   * when they failed too, so the page that comes back is the page of a fresh load: the scanning's report (its tab, its grade, its
   * banner), the README, the tree of the documentation, the dependencies this installation holds.
   */
  protected readonly loadFailure = failureOf([this.summary, this.details], {
    also: [this.vulnerabilities, this.readme, this.docs, this.heldDependencies, this.debContents],
  });
  protected readonly readmeFailure = failureOf(this.readme);
  /** The scanning report's failure that may pass: without it, the tab Vulnerabilities, the grade and "Blocked in" were gone. */
  protected readonly vulnerabilitiesFailure = failureOf(this.vulnerabilities);
  /** The names of the dependencies this installation holds: the block links them to their pages. */
  protected readonly held = computed<ReadonlySet<string>>(() => valueOf(this.heldDependencies) ?? new Set());

  protected readonly externalRel = EXTERNAL_REL;
  protected readonly size = formatSize;
  /** A repository's page, inside its own project. */
  protected repositoryLink(qualified: string): string[] {
    const i = qualified.indexOf('/');
    return ['/projects', qualified.slice(0, i), 'repositories', qualified.slice(i + 1)];
  }

  /**
   * The malware scan's fact per file (R-MAL-024, Q-UI-872): a column while the answer carries the member, which it does whenever the
   * scanning is on; a generated file has none.
   */
  protected readonly malwareShown = computed(() => (this.detail()?.files ?? []).some((f) => f.malware !== undefined));
  /** The version's member `malware`, its worst stored part's (R-MAL-039): the mark beside its grade (R-UI-255). */
  protected readonly versionMalware = computed<MalwareSummary | null>(() => (this.malwareMarks.available ? (this.detail()?.malware ?? null) : null));
  /** Today's signatures for the marks; absent in the Bastion's application, whose header and parts carry no mark yet (W-36 P8). */
  private readonly malwareMarks = inject(MalwareSignaturesStore);
  /**
   * An image index's platforms, stored of named (Q-MAL-134): the details say "3 of 10 platforms stored", counted from the index's
   * platforms and not from `partsNotStored`, which cannot count the configs and layers of a platform never fetched. Null for an image
   * of one platform and every other format.
   */
  protected readonly storedPlatforms = computed(() => {
    const platforms = this.image()?.platforms ?? [];
    if (platforms.length === 0) return null;
    return { stored: platforms.filter((p) => p.stored).length, total: platforms.length };
  });
  /**
   * The verdict of each of an image's parts by its digest (R-MAL-039): its files' (the tag's manifest document) and the parts its files
   * do not list (`malwareParts`: each platform's manifest, each config and each layer). A part not stored reads unchecked (Q-MAL-134).
   */
  private readonly partVerdicts = computed(() => {
    const d = this.detail();
    const out = new Map<string, MalwareStatus>();
    for (const f of d?.files ?? []) if (f.malware) out.set(f.sha256, f.malware);
    for (const p of d?.malwareParts ?? []) out.set(p.sha256, p.malware);
    return out;
  });
  /** A part's verdict by its digest, `sha256:<hex>` or the bare hex; null where the answer has none. */
  protected partMalware(digest: string): MalwareStatus | null {
    const hex = digest.slice(digest.indexOf(':') + 1);
    return this.partVerdicts().get(hex) ?? null;
  }
  /** The image's parts have a column of marks while the answer carries their verdicts. */
  protected readonly partsMalwareShown = computed(
    () => this.malwareMarks.available && this.partVerdicts().size > 0 && this.detail()?.malwareParts !== undefined,
  );
  /** The files the gate refuses now, each with a banner above the tabs (R-MAL-024). */
  protected readonly refusedFiles = computed(() => (this.detail()?.files ?? []).filter((f) => f.malware?.refused === true));
  /**
   * Why a refused file could not be checked, the words after "could not be checked for malware:" (R-MAL-024, Q-UI-1204): the limit
   * it passed in plain words with its configured value, or the reason; empty when the verdict names none.
   */
  protected notCheckedWhy(m: MalwareStatus | undefined): string {
    return m ? malwareReasonText(m.state, m.reason, m.limit, m.limitValue) : '';
  }
  /** The file name of a path, which the banner names. */
  protected fileName(path: string): string {
    return path.split('/').filter((p) => p !== '').pop() ?? path;
  }

  /**
   * The badge per file (R-SIG-035): a column while some file carries one of the four states with a badge or a repository's gate refuses
   * it (R-SIG-074); a file without either shows no value, and the tab Signatures says why.
   */
  protected readonly signatureShown = computed(() =>
    (this.detail()?.files ?? []).some((f) => f.signatures?.refused === true || hasSignatureBadge(f.signatures)),
  );

  // ---- The signature gate of the repositories that hold the version (R-SIG-074) ----

  /** The gate's release the page provides (R-SIG-070, views/acts.ts); none in an application without the main installation's acts. */
  protected readonly gateAct = inject(GATE_ACT, { optional: true });
  private readonly reloadDetails = (): void => {
    this.details.reload();
  };
  protected reasonWord(reason: SignatureGateRefusedReason | null | undefined): string {
    return gateReasonWord(reason ?? 'missing');
  }
  /**
   * The artifacts a repository's gate refuses now, one banner each: the repository that holds the file (its content route names it),
   * the artifact as a reader names it (the version where it has one file there, else the file) and the reason.
   */
  protected readonly signatureRefusals = computed(() => {
    const files = this.detail()?.files ?? [];
    const perRepository = new Map<string, number>();
    for (const f of files) {
      const r = repositoryOfContent(f.contentUrl) ?? '';
      perRepository.set(r, (perRepository.get(r) ?? 0) + 1);
    }
    return files
      .filter((f) => f.signatures?.refused === true)
      .map((f) => {
        const repository = repositoryOfContent(f.contentUrl) ?? '';
        return {
          repository,
          sha256: f.sha256,
          name: perRepository.get(repository) === 1 ? this.heading() : this.fileName(f.path),
          reason: (f.signatures?.refusedReason ?? 'missing') as SignatureGateReason,
        };
      })
      .map((f) => {
        const act: GateActInputs = {
          act: 'release',
          place: 'banner',
          repository: f.repository,
          artifact: { name: f.name, sha256: f.sha256, reason: f.reason },
          state: null,
          changed: this.reloadDetails,
        };
        return { ...f, act };
      });
  });
  /** The platforms of an image index, which pass through the index's own signature (R-SIG-063). */
  protected readonly imagePlatforms = computed(() => (this.image()?.platforms ?? []).map((p) => p.platform));
  /** The version's member `signatures` while it carries a badge (R-SIG-035). */
  protected readonly signatureBadged = computed(() => this.badged(this.detail()?.signatures));
  protected badged(s: SignatureStatus | undefined): SignatureStatus | null {
    return hasSignatureBadge(s) ? s : null;
  }

  protected readonly fileColumns = computed<readonly TableColumn[]>(() => [
    // One form at 1,440 and 1,024 px whatever the files hold (Q-UI-1002, the rule of Q-UI-969): a file's size and its SHA-256 stand under
    // its path, the hash as its first twelve characters with the whole as its title and its copy button; the malware scan and the
    // signature keep their columns. Blocks only at 390 px.
    { id: 'path', label: $localize`:@@pkg.file.path:File`, width: '12rem' },
    ...(this.malwareShown() ? [malwareColumn('file')] : []),
    ...(this.signatureShown() ? [{ id: 'signature', label: $localize`:@@pkg.file.signature:Signature`, width: '9rem' }] : []),
  ]);
  /**
   * Whether a file is held by a system repository of the project `upstream` (R-UI-193): its `contentUrl` is the content route of
   * one, which answers every download 404 like its address for downloads by path (R-REPO-070, R-REPO-075), so it offers no download
   * (Q-UI-898). The API names the holding repository in that route only.
   */
  protected inSystemRepository(f: { readonly contentUrl: string }): boolean {
    if (f.contentUrl.startsWith(CLAMAV_CONTENT)) return this.clamavClosed();
    return SYSTEM_CONTENT.some((prefix) => f.contentUrl.startsWith(prefix));
  }
  /** The repositories that hold the version and are disabled: they answer every download 404 (R-REPO-005, Q-UI-965). */
  private readonly disabledHolders = computed(
    () => new Set((this.detail()?.repositories ?? []).filter((r) => !r.available && r.class !== 'virtual').map((r) => r.key)),
  );
  /**
   * Whether a file is held by a disabled repository: its `contentUrl` is that repository's content route, which answers 404, so it
   * offers no download, as the file of a disabled repository does not (Q-UI-965); the install panel says why.
   */
  protected inDisabledRepository(f: { readonly contentUrl: string }): boolean {
    const repo = repositoryOfContent(f.contentUrl);
    return repo !== null && this.disabledHolders().has(repo);
  }
  /**
   * Whether a file's path is its download (Q-UI-1051): not where the malware scan's gate or a repository's signature gate refuses it
   * (Q-UI-882, R-SIG-074), and not in a system repository or a disabled one, which answer every download 404 (Q-UI-898, Q-UI-965).
   */
  protected offersDownload(f: PackageVersion['files'][number]): boolean {
    return !(f.malware?.refused === true || f.signatures?.refused === true || this.inSystemRepository(f) || this.inDisabledRepository(f));
  }
  protected readonly downloadTitle = $localize`:@@pkg.file.download-link:Download`;
  protected readonly repositoryColumns = computed<readonly TableColumn[]>(() => [
    { id: 'repository', label: $localize`:@@pkg.repo.repository:Repository` },
    { id: 'class', label: $localize`:@@pkg.repo.class:Class` },
    { id: 'published', label: this.arrived() },
  ]);
  protected readonly platformColumns: readonly TableColumn[] = [
    { id: 'platform', label: $localize`:@@pkg.image.platform:Platform` },
    { id: 'digest', label: $localize`:@@pkg.image.digest:Digest` },
    { id: 'mediaType', label: $localize`:@@pkg.image.media-type:Media type` },
    { id: 'size', label: $localize`:@@pkg.image.size:Size`, numeric: true },
  ];
  protected readonly tableCaptionPlatforms = $localize`:@@pkg.image.platforms-caption:Platforms of this image index`;
  protected readonly imageFileColumns = computed<readonly TableColumn[]>(() => [
    { id: 'part', label: $localize`:@@pkg.file.part:Part`, width: '6rem' },
    { id: 'digest', label: $localize`:@@pkg.image.digest:Digest`, prefer: true },
    { id: 'size', label: $localize`:@@pkg.image.size:Size`, numeric: true, width: '6rem' },
    ...(this.partsMalwareShown() ? [malwareColumn('file')] : []),
  ]);
  protected imageFilesCaption(platform: string | null): string {
    return platform === null
      ? $localize`:@@pkg.file.image-caption:Manifest, config and layers of the image`
      : $localize`:@@pkg.file.platform-caption:Manifest, config and layers of ${platform}:platform:`;
  }
  protected readonly notFetchedTitle = $localize`:@@pkg.image.not-fetched-title:No client has fetched this platform through Quader yet, so its manifest and its size are not held.`;
  protected readonly neverTitle = $localize`:@@pkg.ver.never-title:No client has downloaded this version through Quader yet.`;
  protected readonly copyImageDigest = $localize`:@@pkg.image.copy-digest:Copy digest`;
  protected readonly tableCaptionFiles = $localize`:@@pkg.file.caption:Files of this version`;
  protected readonly tableCaptionRepositories = $localize`:@@pkg.repo.caption:Repositories that hold this version`;
  protected readonly copySha = $localize`:@@pkg.file.copy-sha:Copy SHA-256`;
  /** A hash's first twelve characters, the form a list shows it in (the style guide's rule of a hash). */
  protected shortSha(sha: string): string {
    return sha.slice(0, 12);
  }
  private readonly formatCatalog = inject(FormatCatalog);
  private readonly catalog = resource({ loader: () => this.formatCatalog.formats() });
  /** The class of a repository that holds the version: System where the format is system only (Q-UI-1203), else its class. */
  protected classText(cls: string): string {
    return classLabel(isSystemOnly(this.format(), valueOf(this.catalog)) ? 'system' : cls);
  }

  /**
   * Something of the version has not answered yet (R-UI-213): a file's malware scan, a signature check, an image's vulnerability
   * scan, or the read of an image's stored configuration (R-OCI-040). The page reads the version again every few seconds until it has,
   * and every minute after. A scan waits only while its state passes on its own (R-VULN-039, Q-UI-1122): a file queued for its malware
   * scan, an image queued or being scanned; one that waits for what the installation lacks (no scanner, no database) reads every minute,
   * as a page with nothing in flight, and so does a configuration the repository does not hold.
   */
  protected readonly inFlight = computed(() => {
    const d = this.detail();
    if (!d) return false;
    const signaturePending = (s: SignatureStatus | undefined): boolean => s?.state === 'not_checked' || s?.refusedReason === 'pending';
    if (signaturePending(d.signatures)) return true;
    if (this.debContentsValue()?.state === 'pending') return true;
    if (d.files.some((f) => f.malware?.waiting === 'queued' || signaturePending(f.signatures))) return true;
    if (this.parts().some((p) => p.configuration?.state === 'not-read')) return true;
    return (this.vulnerabilityReport()?.repositories ?? []).some((r) => checkWaits(r.grade.state));
  });

  constructor() {
    effect(() => {
      const key = this.shownVersion();
      const settled = this.details.hasValue() && !this.vulnerabilities.isLoading() && !this.summary.isLoading();
      const first = this.firstTab();
      untracked(() => {
        if (settled && first && this.landingTab()?.key !== key) this.landingTab.set({ key, id: first });
      });
    });
    live({
      reload: async () => {
        const id = valueOf(this.summary)?.id;
        const version = this.shown();
        if (!id || !version) return;
        await Promise.all([
          refreshResource(this.details, () => this.api.version(id, version)),
          this.switchVersions.hasValue()
            ? refreshResource(this.switchVersions, async () => (await this.api.versions(id, { sort: '-version' })(null, SWITCH_VERSIONS)).items)
            : Promise.resolve(),
          this.vulnerabilities.hasValue()
            ? refreshResource(this.vulnerabilities, () => noneOnRefusal(this.vulnerabilityApi.versionReport(id, version)))
            : Promise.resolve(),
          // The reader of a Debian package file runs after the upload (R-DEB-039): its contents are read again until it ran.
          this.debContentsValue()?.state === 'pending'
            ? refreshResource(this.debContents, () => this.api.contents(id, version, { limit: 100 }))
            : Promise.resolve(),
        ]);
      },
      every: () => (this.inFlight() ? LIVE_FAST_MS : LIVE_IDLE_MS),
    });
    // The page records the package it opened in "recently opened" (R-UI-007) and names it in the trail.
    effect(() => {
      const s = valueOf(this.summary);
      if (s) untracked(() => this.packageOpened.emit(s));
    });
    // The page's acts are about the version shown.
    effect(() => {
      const d = this.detail() ?? null;
      untracked(() => this.versionShown.emit(d));
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
