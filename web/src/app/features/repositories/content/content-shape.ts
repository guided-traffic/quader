/**
 * What a repository's content is called and what it shows, per format and, for the words that differ, per class (R-UI-052). One shell serves every format; the words and
 * the columns are this small table, not a screen per format.
 */
export interface ContentShape {
  /** The tab and the page's content heading: Images, Packages or Files. */
  readonly itemsLabel: string;
  /** The name column of an item: Image, Package. */
  readonly itemLabel: string;
  /** The column that counts an item's versions: Tags, Versions. */
  readonly versionsLabel: string;
  /** The column of one version: Tag, Version. */
  readonly versionLabel: string;
  readonly filterLabel: string;
  /** What an empty repository does not hold yet, in one sentence: "This repository holds no packages yet." */
  readonly emptyText: string;
  /**
   * What fills it, for a caller who can (R-UI-144, Q-UI-555): publish or push with a client, which the header's **Set up client**
   * shows; in a remote repository, the fetches of every client.
   */
  readonly fillText: string;
  /** What fills it, for a caller who may not publish to it: who does. A remote repository says its `fillText` to everyone. */
  readonly othersFillText: string;
  readonly emptyFilteredText: string;
  /** The date a version arrived: Pushed for an image tag, Fetched for a tag of a remote repository, Published elsewhere. */
  readonly publishedLabel: string;
  /**
   * A version names content (an image's tag names its manifest): an opened item lists the content, one row per image with its tags
   * and its digest (`group=content`, Q-UI-1130), not one row per version.
   */
  readonly digest: boolean;
  /**
   * The word of the button beside a version that opens its commands (Q-UI-1066): the format's client verb, `Pull` for an image,
   * `Download` for a file, `Install` for a package.
   */
  readonly commandVerb: string;
  /**
   * Why this format's versions are not checked for vulnerabilities (shown instead of a column of findings, and as the sentence of the
   * state `format_not_scanned` beside a version's N/A, R-UI-091, Q-UI-1244): the reason of the format, not only that it is not.
   */
  readonly notScannedText: string;
  /**
   * The format's packages lead with their documentation (R-UI-218, the owner's rule of 2026-11-25): a package's name opens the page of
   * its newest version on its documentation (an Ansible collection's tab Documentation, an npm or a NuGet package's README), with a
   * switch between its versions in the head, and the table of every version is one step away (`…/{name}/-/versions`). A format whose
   * content is its list (OCI, Debian, generic files) opens on that table.
   */
  readonly leadsWithDocumentation: boolean;
  /**
   * The link under an opened item's newest versions to the package's page (R-UI-215, Q-UI-1111): "All 103 tags", "All 33 versions";
   * `count` is the number as the locale writes it.
   */
  readonly allVersionsText: (count: string) => string;
}

/**
 * The formats whose packages lead with their documentation (R-UI-218): documentation that belongs to the format and is regularly
 * present is what a package's name opens. One line per format: a format joins by its key here.
 */
const LEADS_WITH_DOCUMENTATION: ReadonlySet<string> = new Set(['ansible', 'npm', 'nuget']);

/** Whether the packages of `format` lead with their documentation (R-UI-218): the routes and the links ask it without a shape. */
export function leadsWithDocumentation(format: string | null | undefined): boolean {
  return !!format && LEADS_WITH_DOCUMENTATION.has(format);
}

/**
 * Why the versions of a format are not checked for vulnerabilities (Q-UI-1244), the reason of each format that is not: an Ansible
 * collection has no public advisory source that lists it by version (Q-ANS-15, H-841); Debian's advisories exist but name source
 * packages and releases, and the matching is not built (Q-DEB-26); a file has no package identity an advisory could name.
 */
export function notScannedText(format: string | null | undefined): string {
  switch (format) {
    case 'ansible':
      return $localize`:@@repo.content.no-scanning.ansible:Collections are not checked for vulnerabilities: no public advisory source lists Ansible collections by version.`;
    case 'deb':
      return $localize`:@@repo.content.no-scanning.deb:Debian packages are not checked for vulnerabilities yet: Debian's advisories name source packages and releases, and matching a package against them is not built yet.`;
    case 'generic':
      return $localize`:@@repo.content.no-scanning.generic:Files are not checked for vulnerabilities: a file has no package name and version that an advisory could name.`;
    default:
      return $localize`:@@repo.content.no-scanning:This package type is not scanned for vulnerabilities yet.`;
  }
}

/**
 * The word of the date a version arrived where it is shown beside the repositories that hold it (a version's page, a package's table,
 * Q-UI-1314): an image's is Pushed, or Fetched where only remote repositories hold it, the words of the repository's content; every other
 * format's is Published. A virtual repository holds nothing of its own and does not count.
 */
export function arrivedLabel(format: string, classes: readonly string[]): string {
  const holders = classes.filter((c) => c !== 'virtual');
  return contentShapeOf(format, holders.length > 0 && holders.every((c) => c === 'remote') ? 'remote' : 'local').publishedLabel;
}

export function contentShapeOf(format: string, repositoryClass = ''): ContentShape {
  const remote = repositoryClass === 'remote';
  if (format === 'oci') {
    const fill = remote
      ? $localize`:@@repo.content.oci.fill-remote:It fills as clients pull images through it; Set up client shows how.`
      : $localize`:@@repo.content.oci.fill:Push an image with your client; Set up client shows how.`;
    return {
      itemsLabel: $localize`:@@repo.content.oci.items:Images`,
      itemLabel: $localize`:@@repo.content.oci.item:Image`,
      versionsLabel: $localize`:@@repo.content.oci.versions:Tags`,
      versionLabel: $localize`:@@repo.content.oci.version:Tag`,
      filterLabel: $localize`:@@repo.content.oci.filter:Filter images`,
      emptyText: remote
        ? $localize`:@@repo.content.oci.empty-remote3:This repository has cached no images yet.`
        : $localize`:@@repo.content.oci.empty3:This repository holds no images yet.`,
      fillText: fill,
      othersFillText: remote ? fill : $localize`:@@repo.content.oci.fill-others:Members who may push to it fill it with their client.`,
      emptyFilteredText: $localize`:@@repo.content.oci.empty-filtered:No image matches the filter.`,
      publishedLabel: remote
        ? $localize`:@@repo.content.oci.fetched:Fetched`
        : $localize`:@@repo.content.oci.published:Pushed`,
      digest: true,
      commandVerb: $localize`:@@repo.content.oci.verb:Pull`,
      notScannedText: notScannedText(format),
      leadsWithDocumentation: leadsWithDocumentation(format),
      allVersionsText: (count) => $localize`:@@repo.content.oci.all-versions:All ${count}:count: tags`,
    };
  }
  const fetched = $localize`:@@repo.content.fill-remote:It fills as clients fetch through it; Set up client shows how.`;
  if (format === 'generic') {
    const fill = remote ? fetched : $localize`:@@repo.content.files.fill:Upload files with your client; Set up client shows how.`;
    return {
      ...contentShapeOf('', repositoryClass),
      itemsLabel: $localize`:@@repo.content.files:Files`,
      emptyText: remote
        ? $localize`:@@repo.content.files.empty-remote:This repository has cached no files yet.`
        : $localize`:@@repo.content.files.empty3:This repository holds no files yet.`,
      fillText: fill,
      othersFillText: remote ? fill : $localize`:@@repo.content.files.fill-others:Members who may upload to it fill it with their client.`,
      commandVerb: $localize`:@@repo.content.files.verb:Download`,
      notScannedText: notScannedText(format),
      leadsWithDocumentation: leadsWithDocumentation(format),
    };
  }
  const fill = remote ? fetched : $localize`:@@repo.content.fill:Publish a package with your client; Set up client shows how.`;
  return {
    itemsLabel: $localize`:@@repo.content.items:Packages`,
    itemLabel: $localize`:@@repo.content.item:Package`,
    versionsLabel: $localize`:@@repo.content.versions:Versions`,
    versionLabel: $localize`:@@repo.content.version:Version`,
    filterLabel: $localize`:@@repo.content.filter:Filter packages`,
    emptyText: remote
      ? $localize`:@@repo.content.empty-remote3:This repository has cached no packages yet.`
      : $localize`:@@repo.content.empty3:This repository holds no packages yet.`,
    fillText: fill,
    othersFillText: remote ? fill : $localize`:@@repo.content.fill-others:Members who may publish to it fill it with their client.`,
    emptyFilteredText: $localize`:@@repo.content.empty-filtered:No package matches the filter.`,
    publishedLabel: $localize`:@@repo.content.published:Published`,
    digest: false,
    commandVerb: $localize`:@@repo.content.verb:Install`,
    notScannedText: notScannedText(format),
    leadsWithDocumentation: leadsWithDocumentation(format),
    allVersionsText: (count) => $localize`:@@repo.content.all-versions:All ${count}:count: versions`,
  };
}

/**
 * The empty state of a repository's content (R-UI-144, Q-UI-555): what it does not hold yet, then what fills it for this caller.
 * Who may publish is told to publish and pointed at **Set up client** in the header; who may not is told who does; a remote
 * repository fills by the fetches of every client. A disabled repository says only what it does not hold: no client can use it,
 * and its header has no **Set up client**.
 */
export function emptyContentText(shape: ContentShape, fill: 'publisher' | 'other' | 'none'): string {
  if (fill === 'none') return shape.emptyText;
  return `${shape.emptyText} ${fill === 'publisher' ? shape.fillText : shape.othersFillText}`;
}
