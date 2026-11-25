import { DownloadLink } from '../../shared/download-link';
import { DatePipe, DecimalPipe } from '@angular/common';
import { Component, computed, effect, inject, input, resource, signal, untracked } from '@angular/core';
import { Router, RouterLink } from '@angular/router';
import { PackageDocTree, PackageVersion, PackageVersionDocs, PackagesApi } from '../../api/packages-api';
import { VulnerabilitiesApi, VulnerabilityVersionReport } from '../../api/vulnerabilities-api';
import { CopyButton } from '../../shared/copy-button';
import { ErrorNotice } from '../../shared/error-notice';
import { FormatIconView } from '../../shared/format-icon';
import { FormatCatalog, formatName } from '../../core/format-names';
import { classLabel, isSystemOnly } from '../repositories/repository-class';
import { failureOf, valueOf } from '../../shared/paged-resource';
import { failureMayPass, noneOnRefusal } from '../../shared/none-on-refusal';
import { DataTable, TableColumn } from '../../shared/ui/data-table';
import { PageHeader } from '../../shared/ui/page-header';
import { TabGroup, TabPage } from '../../shared/ui/tabs';
import { checkWaits, worstGrade } from '../../shared/vulnerability/grade-words';
import { barSeverityOf, blockedRepositories, PackageVulnerabilities } from '../vulnerabilities/package-vulnerabilities';
import { AnsibleContents } from './ansible-contents';
import { AnsibleDocs } from './ansible-docs';
import { AnsibleImportLog } from './ansible-import-log';
import { DebChangelog, DebCopyright, DebFiles } from './deb-contents';
import { formatSize } from './format-size';
import { TimeAgo } from '../../shared/ui/time-ago';
import { ClientCommandMenu } from './client-command-menu';
import { VersionSwitch } from './version-switch';
import { clamavInstallNote, versionCommand } from './version-command';
import { contentShapeOf, leadsWithDocumentation } from '../repositories/content/content-shape';
import { EXTERNAL_REL, packageCommands, versionCommands } from './package-links';
import { ImageDetails } from './image-details';
import { ImagePart, chartOf, chartValues, cutDigest, imageFact, imageFactList, imageLinks, imageParts, kindWord } from './image-facts';
import { CodeWindow } from '../../shared/code/code-window';
import { PackageMemory } from './package-memory';
import { PackageReadmeView } from './package-readme';
import { distTagsOf } from './package-versions-model';
import { DistTags } from './dist-tags';
import { GradeLabel } from '../../shared/vulnerability/grade';
import {
  ansibleFacts,
  debFacts,
  debLongDescription,
  debRelationNames,
  debRelations,
  debSource,
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
import { MalwareStatusView } from '../../shared/malware/malware-status';
import { malwareReasonText } from '../../shared/malware/malware-words';
import { MalwareStatus } from '../../api/malware-api';
import { SignatureGateRefusedReason, SignatureGateReason, SignatureStatus } from '../../api/signatures-api';
import { repositoryOfContent } from '../../api/files-api';
import { GateActions } from '../../shared/signatures/gate-releases';
import { gateReasonWord } from '../../shared/signatures/gate-words';
import { SignatureBadge } from '../../shared/signatures/signature-badge';
import { hasSignatureBadge } from '../../shared/signatures/signature-words';
import { VersionSignatures } from './version-signatures';
import { contentPath } from '../../api/files-api';
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
 * The page of one version (R-UI-111, the owner's walk of 2026-11-18): `/packages/{format}/{name…}@{version}`, an address of its
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
 */
@Component({
  selector: 'quader-package-version',
  imports: [Loading, 
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
    AnsibleContents,
    AnsibleDocs,
    AnsibleImportLog,
    DebChangelog,
    DebCopyright,
    DebFiles,
    PackageVulnerabilities,
    GradeLabel,
    VersionSize,
    NoValue,
    DownloadLink,
    MalwareStatusView,
    SignatureBadge,
    VersionSignatures,
    Disclosure,
    DatePipe,
    DistTags,
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

  protected readonly summary = resource({
    params: () => ({ format: this.format(), name: this.name() }),
    loader: ({ params }) => this.api.lookup(params.format, params.name),
  });
  protected readonly pkg = computed(() => valueOf(this.summary));
  /** The version shown: the address's, or the package's newest (Q-UI-1242); null until the package has answered. */
  protected readonly shown = computed(() => this.version() ?? this.pkg()?.latestVersion ?? null);
  /** The format's packages lead with their documentation (R-UI-218): it comes first, and the head switches between the versions. */
  protected readonly leads = computed(() => leadsWithDocumentation(this.format()));
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
      // The README of an Ansible collection is the first page of its Documentation tab (R-UI-060), not a tab of its own.
      return d?.hasReadme && d.format !== 'ansible' ? { id: d.packageId, version: d.version } : undefined;
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
    // A graded version opens on its vulnerabilities; one that is not scanned opens on what it is, and the tab says why it is not. A
    // format that leads with its documentation shows it first (R-UI-218): its README tab stands even where the version has none, and
    // says so (Q-UI-1245).
    const gradedFirst = this.grade() !== null && this.grade()?.grade !== 'N/A';
    const readme: TabEntry[] =
      d && d.format !== 'ansible' && (d.hasReadme || this.leads()) ? [{ id: 'readme', label: $localize`:@@pkg.tab.readme:README` }] : [];
    return [
      ...(this.leads() ? readme : []),
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
      ...(this.leads() ? [] : readme),
      // A Helm chart's values.yaml, its documentation beside the README (R-OCI-042).
      ...(this.chartValues() ? [{ id: 'values' as const, label: $localize`:@@pkg.tab.values:Values` }] : []),
      { id: 'files', label: $localize`:@@pkg.tab.files:Files` },
      // The details of the signatures while the installation checks them (R-SIG-035): the member is absent while they are off.
      ...(d?.signatures !== undefined ? [{ id: 'signatures' as const, label: $localize`:@@pkg.tab.signatures:Signatures` }] : []),
      { id: 'details', label: $localize`:@@pkg.tab.details:Details` },
      // The changelog of a Debian package file (R-DEB-039, Q-UI-1272): a detail, after the facts.
      ...(d?.format === 'deb' ? [{ id: 'changelog' as const, label: $localize`:@@pkg.tab.changelog:Changelog` }] : []),
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
   * none: a format that leads with its documentation opens such a version on its first tab with content.
   */
  private readonly firstTab = computed(() => this.tabs().find((t) => t.id !== 'readme' || this.detail()?.hasReadme === true)?.id);
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
  protected readonly heading = computed(() => `${this.name()} ${this.shown() ?? ''}`.trimEnd());
  /** The distribution tags that point at this version (npm `latest`, Q-UI-453). */
  protected readonly tags = computed(() =>
    distTagsOf(this.detail()?.metadata)
      .filter(([, v]) => v === this.shown())
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
  protected readonly debRelations = computed(() => debRelations(this.metadata()));
  protected readonly debLongDescription = computed(() => debLongDescription(this.metadata()));
  protected readonly debSource = computed(() => debSource(this.metadata()));
  /** The package list filtered by the source package: the binaries of one source (R-DEB-038, Q-DEB-142). */
  protected readonly sourceLink = computed(() => ['/projects', this.project(), 'packages']);
  protected readonly copyrightHeading = $localize`:@@pkg.meta.deb-copyright-file:Copyright file`;
  protected readonly ansibleFacts = computed(() => {
    const d = this.detail();
    return d ? ansibleFacts(d, valueOf(this.summary)?.keywords ?? []) : [];
  });
  /** The deprecation of a version: npm's message; a Helm chart its Chart.yaml marks deprecated (R-OCI-042). */
  protected readonly deprecated = computed(() =>
    this.chart()?.deprecated ? $localize`:@@pkg.chart.deprecated:This chart is deprecated.` : deprecationOf(this.metadata()),
  );
  /** A Helm chart's facts (R-OCI-042); null for anything else. */
  protected readonly chart = computed(() => (this.format() === 'oci' ? chartOf(this.metadata()) : null));
  /** A Helm chart's values.yaml (R-OCI-042). */
  protected readonly chartValues = computed(() => (this.format() === 'oci' ? chartValues(this.metadata()) : null));
  protected readonly copyValues = $localize`:@@pkg.chart.copy-values:Copy values.yaml`;
  protected readonly links = computed(() => {
    const d = this.detail();
    if (!d) return [];
    return d.format === 'oci' ? imageLinks(d.metadata) : metadataLinks(d);
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
  /** An OCI artifact's kind (`image`, `helm-chart`, …): the head's panel offers its kind's clients (R-OCI-042); null for other formats. */
  protected readonly artifactKind = computed(() => {
    const k = this.format() === 'oci' ? this.metadata()['kind'] : undefined;
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
  /** The facts of the image's annotations and labels: title, version, revision, created, vendor, base image. */
  protected readonly imageFacts = computed(() => (this.format() === 'oci' ? imageFactList(this.metadata()) : []));
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
  protected readonly imageDetailsHeading = $localize`:@@pkg.image.details:Configuration, layers and history`;
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
   * The collections a collection version depends on that this installation holds (Q-UI-245): each of them is a link to its page, the
   * others stay muted text. One lookup per dependency, which rejects for a package that no visible repository holds.
   */
  private readonly dependencyNames = computed(
    () => {
      const d = this.detail();
      if (d?.format !== 'ansible' && d?.format !== 'deb') return undefined;
      // A Debian version names its relations (R-DEB-038); the lookups are bounded, a name past the bound stays text.
      const names =
        d.format === 'deb' ? debRelationNames(d.metadata).slice(0, 60) : stringRecord(d.metadata['dependencies']).map(([name]) => name);
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
  /** The router commands of a dependency's page, or null when the installation does not hold it. */
  protected dependencyLink(name: string): string[] | null {
    const format = this.detail()?.format;
    return (format === 'ansible' || format === 'deb') && valueOf(this.heldDependencies)?.has(name)
      ? packageCommands(this.project(), format, name)
      : null;
  }

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

  private readonly gateActions = inject(GateActions);
  protected readonly canRelease = computed(() => this.gateActions.canRelease());
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
      });
  });
  protected async releaseFor(f: { readonly repository: string; readonly sha256: string; readonly name: string; readonly reason: SignatureGateReason }): Promise<void> {
    const done = await this.gateActions.release({ repository: f.repository, sha256: f.sha256, artifact: f.name, reason: f.reason });
    if (done) this.details.reload();
  }
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
    ...(this.malwareShown() ? [{ id: 'malware', label: $localize`:@@pkg.file.malware:Malware scan`, width: '9rem' }] : []),
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
  protected readonly imageFileColumns: readonly TableColumn[] = [
    { id: 'part', label: $localize`:@@pkg.file.part:Part`, width: '6rem' },
    { id: 'digest', label: $localize`:@@pkg.image.digest:Digest`, prefer: true },
    { id: 'size', label: $localize`:@@pkg.image.size:Size`, numeric: true, width: '6rem' },
  ];
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
   * Something of the version has not answered yet (R-UI-213): a file's malware scan, a signature check, or an image's vulnerability
   * scan. The page reads the version again every few seconds until it has, and every minute after. A scan waits only while its state
   * passes on its own (R-VULN-039, Q-UI-1122): a file queued for its malware scan, an image queued or being scanned; one that waits for
   * what the installation lacks (no scanner, no database) reads every minute, as a page with nothing in flight.
   */
  protected readonly inFlight = computed(() => {
    const d = this.detail();
    if (!d) return false;
    const signaturePending = (s: SignatureStatus | undefined): boolean => s?.state === 'not_checked' || s?.refusedReason === 'pending';
    if (signaturePending(d.signatures)) return true;
    if (this.debContentsValue()?.state === 'pending') return true;
    if (d.files.some((f) => f.malware?.waiting === 'queued' || signaturePending(f.signatures))) return true;
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
