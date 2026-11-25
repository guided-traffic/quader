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
  /** A version has a content digest worth showing (an image tag). */
  readonly digest: boolean;
  /**
   * The word of the button beside a version that opens its commands (Q-UI-1066): the format's client verb, `Pull` for an image,
   * `Download` for a file, `Install` for a package.
   */
  readonly commandVerb: string;
  /** Says that this format's versions are not matched against advisories (shown instead of a column of findings). */
  readonly notScannedText: string;
  /**
   * The link under an opened item's newest versions to the package's page (R-UI-215, Q-UI-1111): "All 103 tags", "All 33 versions";
   * `count` is the number as the locale writes it.
   */
  readonly allVersionsText: (count: string) => string;
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
      notScannedText: $localize`:@@repo.content.no-scanning:This package type is not scanned for vulnerabilities yet.`,
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
    notScannedText: $localize`:@@repo.content.no-scanning:This package type is not scanned for vulnerabilities yet.`,
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
