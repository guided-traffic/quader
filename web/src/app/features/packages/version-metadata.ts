import { PackageVersion } from '../../api/packages-api';
import { formatSize } from './format-size';
import { safeHttpUrl } from './package-links';

/*
 * What the tab **Details** of a version's page reads from the format's metadata (R-UI-007, R-UI-111): the members each format's view
 * adapter sends, as lists of facts. A member of the wrong type is skipped, never an error: a publisher wrote it.
 */

/** A link of the metadata: a URL when its scheme is http or https, else the text alone (R-UI-007). */
export interface MetadataLink {
  readonly label: string;
  readonly text: string;
  readonly href: string | null;
}

/**
 * One alternative of a dependency (R-UI-221): the package it names, the name as written (Debian's `python3:any`), and what follows the
 * name as text (a range, a constraint, an architecture restriction).
 */
export interface DependencyAlternative {
  readonly name: string;
  readonly written: string;
  readonly qualifiers: string;
}

/** One dependency: its alternatives (Debian's `a | b`; one for every other format), and a word after it (npm's optional peer). */
export interface DependencyEntry {
  readonly alternatives: readonly DependencyAlternative[];
  readonly mark: string | null;
}

/**
 * One group of a version's dependencies, the same block in every format (R-UI-221, Q-UI-1311): named by the format's own word (npm's
 * field, NuGet's target framework, Debian's relation field), or null for a format whose dependencies are one list (an Ansible
 * collection, a Helm chart); its entries, or its text where the server could not parse it (Debian).
 */
export interface DependencyGroup {
  readonly label: string | null;
  readonly entries: readonly DependencyEntry[];
  readonly text: string | null;
}

/** One platform of an OCI image index (R-OCI-031): the child manifest it names, with its digest, media type and size. */
export interface ImagePlatform {
  /** The platform as the server sends it, or the words for none (a child without a platform). */
  readonly platform: string;
  readonly digest: string;
  readonly mediaType: string;
  readonly size: number | null;
  /** The repository holds the platform's manifest: a remote one once a client fetched it (Q-OCI-221). */
  readonly stored: boolean;
  /**
   * The start of the digest, read by assistive technology only, when the platform alone does not tell the row from another: it is
   * empty, or another child has the same one (a provenance attestation per platform is `unknown/unknown` every time).
   */
  readonly hint: string;
  /** The name of the copy button of the row. */
  readonly copyLabel: string;
}

/** The facts of an OCI tag the view API sends as the version's metadata (R-OCI-031). */
export interface ImageFacts {
  readonly digest: string;
  readonly mediaType: string;
  readonly artifactType: string;
  /** The image's size; null when it is not known (no platform of an index held, R-API-080). */
  readonly size: number | null;
  /** The size counts only the platforms the repository holds (Q-OCI-221). */
  readonly sizePartial: boolean;
  readonly platforms: readonly ImagePlatform[];
}

export function stringRecord(value: unknown): [string, string][] {
  if (typeof value !== 'object' || value === null || Array.isArray(value)) return [];
  return Object.entries(value).flatMap(([k, v]): [string, string][] => (typeof v === 'string' ? [[k, v]] : []));
}

export function strings(value: unknown): string[] {
  if (typeof value === 'string') return value.trim() === '' ? [] : [value];
  return Array.isArray(value) ? value.filter((v): v is string => typeof v === 'string') : [];
}

const text = (v: unknown): string => (typeof v === 'string' ? v.trim() : '');

/** The algorithm and the first twelve hex digits of a digest, which tell two digests apart. */
export function shortDigest(digest: string): string {
  const i = digest.indexOf(':');
  return digest.slice(0, i + 1) + digest.slice(i + 1, i + 13);
}

/**
 * Reads the metadata of an OCI tag; `null` when it carries no digest (any other format's version). Members of the wrong type are
 * skipped: a child without a string digest is not listed, a size that is no number is shown as a dash.
 */
export function imageFacts(m: Record<string, unknown>): ImageFacts | null {
  const num = (v: unknown): number | null => (typeof v === 'number' ? v : null);
  const raw = (v: unknown): string => (typeof v === 'string' ? v : '');
  const digest = raw(m['digest']);
  if (digest === '') return null;
  const children = (Array.isArray(m['platforms']) ? m['platforms'] : []).flatMap((p): Record<string, unknown>[] => {
    if (typeof p !== 'object' || p === null) return [];
    const c = p as Record<string, unknown>;
    return typeof c['digest'] === 'string' && c['digest'] !== '' ? [c] : [];
  });
  const count = new Map<string, number>();
  for (const c of children) count.set(raw(c['platform']), (count.get(raw(c['platform'])) ?? 0) + 1);
  const platforms = children.map((c): ImagePlatform => {
    const given = raw(c['platform']);
    const platform = given === '' ? $localize`:@@pkg.image.no-platform:No platform` : given;
    const hint = given === '' || (count.get(given) ?? 0) > 1 ? shortDigest(raw(c['digest'])) : '';
    return {
      platform,
      digest: raw(c['digest']),
      mediaType: raw(c['mediaType']),
      size: num(c['size']),
      stored: c['stored'] !== false,
      hint,
      copyLabel:
        given === ''
          ? $localize`:@@pkg.image.copy-no-platform-digest:Copy digest of the child without a platform (${hint}:digest:)`
          : hint === ''
            ? $localize`:@@pkg.image.copy-platform-digest:Copy digest of ${platform}:platform:`
            : $localize`:@@pkg.image.copy-platform-digest-short:Copy digest of ${platform}:platform: (${hint}:digest:)`,
    };
  });
  return {
    digest,
    mediaType: raw(m['mediaType']),
    artifactType: raw(m['artifactType']),
    size: num(m['size']),
    sizePartial: m['sizePartial'] === true,
    platforms,
  };
}

const single = (name: string, qualifiers: string, mark: string | null = null): DependencyEntry => ({
  alternatives: [{ name, written: name, qualifiers }],
  mark,
});

/**
 * The `dependencyGroups` member of NuGet metadata (formats/nuget.md R-NUGET-007): a group per target framework with `dependencies` of
 * `id` and `range`, a group without a framework named for all of them. A group without dependencies stays, and says so. Anything else is
 * skipped.
 */
export function nugetDependencies(value: unknown): DependencyGroup[] {
  if (!Array.isArray(value)) return [];
  return value.flatMap((g): DependencyGroup[] => {
    if (typeof g !== 'object' || g === null) return [];
    const group = g as Record<string, unknown>;
    const deps = Array.isArray(group['dependencies']) ? group['dependencies'] : [];
    const entries = deps.flatMap((d): DependencyEntry[] => {
      if (typeof d !== 'object' || d === null) return [];
      const dep = d as Record<string, unknown>;
      const id = dep['id'];
      const range = dep['range'] ?? dep['version'];
      return typeof id === 'string' && id !== '' ? [single(id, typeof range === 'string' && range !== '' ? range : '*')] : [];
    });
    const fw = group['targetFramework'];
    return [{ label: typeof fw === 'string' && fw !== '' ? fw : $localize`:@@pkg.meta.all-frameworks:All frameworks`, entries, text: null }];
  });
}

/**
 * The dependencies of an npm version, a group per field of its manifest in the order npm installs them: `dependencies`,
 * `peerDependencies` (a peer `peerDependenciesMeta` marks optional says so), `optionalDependencies`, `bundleDependencies` (with the
 * range `dependencies` gives the name) and `devDependencies`, which an install of the package does not fetch. A field is named as the
 * manifest names it (Q-UI-1311).
 */
export function npmDependencies(m: Record<string, unknown>): DependencyGroup[] {
  const optionalPeers = new Set(strings(m['optionalPeers']));
  const optional = $localize`:@@pkg.meta.optional-peer:optional`;
  const required = stringRecord(m['dependencies']);
  const ranges = new Map(required);
  const group = (label: string, entries: DependencyEntry[]): DependencyGroup => ({ label, entries, text: null });
  return [
    group('dependencies', required.map(([n, r]) => single(n, r))),
    group('peerDependencies', stringRecord(m['peerDependencies']).map(([n, r]) => single(n, r, optionalPeers.has(n) ? optional : null))),
    group('optionalDependencies', stringRecord(m['optionalDependencies']).map(([n, r]) => single(n, r))),
    group('bundleDependencies', strings(m['bundleDependencies']).map((n) => single(n, ranges.get(n) ?? ''))),
    group('devDependencies', stringRecord(m['devDependencies']).map(([n, r]) => single(n, r))),
  ].filter((g) => g.entries.length > 0);
}

/** The dependencies of an Ansible collection version (R-ANS-026): one list of collections and their ranges. */
export function ansibleDependencies(m: Record<string, unknown>): DependencyGroup[] {
  const entries = stringRecord(m['dependencies']).map(([n, r]) => single(n, r));
  return entries.length > 0 ? [{ label: null, entries, text: null }] : [];
}

/** The plain members of a NuGet version (Q-NUGET-48): text, a name list or a version, shown as given. */
export function nugetFacts(m: Record<string, unknown>): [string, string][] {
  const entries: [string, string][] = [
    [$localize`:@@pkg.meta.title:Title`, text(m['title'])],
    [$localize`:@@pkg.meta.min-client:Minimum client version`, text(m['minClientVersion'])],
    [$localize`:@@pkg.meta.package-types:Package types`, strings(m['packageTypes']).join(', ')],
    [$localize`:@@pkg.meta.language:Language`, text(m['language'])],
    [$localize`:@@pkg.meta.copyright:Copyright`, text(m['copyright'])],
    [$localize`:@@pkg.meta.release-notes:Release notes`, text(m['releaseNotes'])],
    [
      $localize`:@@pkg.meta.license-acceptance:Licence acceptance`,
      m['requireLicenseAcceptance'] === true ? $localize`:@@pkg.meta.license-acceptance-yes:required before it is installed` : '',
    ],
    [
      $localize`:@@pkg.meta.development-dependency:Development dependency`,
      m['developmentDependency'] === true ? $localize`:@@pkg.meta.development-dependency-yes:yes, a project that uses it does not pass it on` : '',
    ],
  ];
  return entries.filter((e) => e[1] !== '');
}

/**
 * The facts of an npm version (R-UI-221): the runtimes it requires, the types it declares, what Quader read of its tarball (the unpacked
 * size, the file count, whether it runs an install script, R-NPM-007) and who published it.
 */
export function npmFacts(m: Record<string, unknown>): [string, string][] {
  const num = (v: unknown): number | null => (typeof v === 'number' && Number.isFinite(v) && v > 0 ? v : null);
  const unpacked = num(m['unpackedSize']);
  const files = num(m['fileCount']);
  const entries: [string, string][] = [
    [$localize`:@@pkg.meta.engines:Required runtimes`, stringRecord(m['engines']).map(([k, v]) => `${k} ${v}`).join(', ')],
    [$localize`:@@pkg.meta.types:Types`, text(m['types'])],
    [$localize`:@@pkg.meta.unpacked-size:Unpacked size`, unpacked === null ? '' : formatSize(unpacked)],
    [$localize`:@@pkg.meta.file-count:Files`, files === null ? '' : new Intl.NumberFormat($localize.locale || 'en-US').format(files)],
    [$localize`:@@pkg.meta.install-script:Install script`, m['hasInstallScript'] === true ? installScriptText() : ''],
    [$localize`:@@pkg.meta.publisher:Publisher`, text(m['publisher'])],
  ];
  return entries.filter((e) => e[1] !== '');
}

/** What an npm version's install script does, said where a person decides to install it (R-UI-221): it runs code of the package. */
export function installScriptText(): string {
  return $localize`:@@pkg.meta.install-script-yes:Runs a script of the package when it is installed`;
}

/**
 * The facts of a Debian version (R-DEB-030, R-DEB-038): the architectures of its files, its coordinates (suite and component, shown as
 * `stable/main`) and the control fields the view adapter sends; Installed-Size, which the field gives in KiB, in the unit a person reads. The relations
 * are lists of their own ({@link debRelations}); the source package is a link of its own (Q-UI-1271).
 */
export function debFacts(m: Record<string, unknown>): [string, string][] {
  const kib = Number(text(m['installedSize']));
  const entries: [string, string][] = [
    [$localize`:@@pkg.meta.deb-architectures:Architectures`, strings(m['architectures']).join(', ')],
    [$localize`:@@pkg.meta.deb-coordinates:Suites and components`, strings(m['coordinates']).join(', ')],
    [$localize`:@@pkg.meta.deb-maintainer:Maintainer`, text(m['maintainer'])],
    [$localize`:@@pkg.meta.deb-section:Section`, text(m['section'])],
    [$localize`:@@pkg.meta.deb-priority:Priority`, text(m['priority'])],
    // Installed-Size is in KiB (Debian policy 5.6.20); the screen says it in the unit a person reads, as every size (Q-UI-1316).
    [
      $localize`:@@pkg.meta.deb-installed-size:Installed size`,
      text(m['installedSize']) !== '' && Number.isFinite(kib) && kib >= 0 ? formatSize(kib * 1024) : '',
    ],
    [$localize`:@@pkg.meta.deb-multi-arch:Multi-Arch`, text(m['multiArch'])],
    [$localize`:@@pkg.meta.deb-essential:Essential`, m['essential'] === true ? $localize`:@@pkg.meta.deb-essential-yes:yes, APT refuses to remove it` : ''],
  ];
  return entries.filter((e) => e[1] !== '');
}

/** The source package of a Debian version: its name without the version in parentheses, or null (R-DEB-038). */
export function debSource(m: Record<string, unknown>): { readonly name: string; readonly text: string } | null {
  const source = text(m['source']);
  if (source === '') return null;
  return { name: source.split(/\s/)[0] ?? source, text: source };
}

/** The relation fields in the order `apt show` lists them, with their labels. */
function relationLabels(): readonly (readonly [string, string])[] {
  return [
    ['preDepends', $localize`:@@pkg.meta.deb-pre-depends:Pre-Depends`],
    ['depends', $localize`:@@pkg.meta.deb-depends:Depends`],
    ['recommends', $localize`:@@pkg.meta.deb-recommends:Recommends`],
    ['suggests', $localize`:@@pkg.meta.deb-suggests:Suggests`],
    ['enhances', $localize`:@@pkg.meta.deb-enhances:Enhances`],
    ['breaks', $localize`:@@pkg.meta.deb-breaks:Breaks`],
    ['conflicts', $localize`:@@pkg.meta.deb-conflicts:Conflicts`],
    ['replaces', $localize`:@@pkg.meta.deb-replaces:Replaces`],
    ['provides', $localize`:@@pkg.meta.deb-provides:Provides`],
  ];
}

function alternative(v: unknown): DependencyAlternative | null {
  if (typeof v !== 'object' || v === null) return null;
  const r = v as Record<string, unknown>;
  const name = text(r['name']);
  if (name === '') return null;
  const parts: string[] = [];
  const arch = text(r['arch']);
  if (text(r['op']) !== '' && text(r['version']) !== '') parts.push(`${text(r['op'])} ${text(r['version'])}`);
  const archs = strings(r['architectures']);
  if (archs.length > 0) parts.push(`[${archs.join(' ')}]`);
  for (const p of Array.isArray(r['profiles']) ? r['profiles'] : []) {
    const terms = strings(p);
    if (terms.length > 0) parts.push(`<${terms.join(' ')}>`);
  }
  return { name, written: arch === '' ? name : `${name}:${arch}`, qualifiers: parts.join(' ') };
}

/** The relations of a Debian version that are present, each a list of groups of alternatives (R-DEB-038), in the block of R-UI-221. */
export function debRelations(m: Record<string, unknown>): DependencyGroup[] {
  const parsed = typeof m['relations'] === 'object' && m['relations'] !== null ? (m['relations'] as Record<string, unknown>) : {};
  const raw = typeof m['relationsText'] === 'object' && m['relationsText'] !== null ? (m['relationsText'] as Record<string, unknown>) : {};
  return relationLabels().flatMap(([key, label]): DependencyGroup[] => {
    const t = text(raw[key]);
    if (t !== '') return [{ label, entries: [], text: t }];
    const entries = (Array.isArray(parsed[key]) ? (parsed[key] as unknown[]) : []).flatMap((g): DependencyEntry[] => {
      const alternatives = (Array.isArray(g) ? g : []).flatMap((a) => alternative(a) ?? []);
      return alternatives.length > 0 ? [{ alternatives, mark: null }] : [];
    });
    return entries.length > 0 ? [{ label, entries, text: null }] : [];
  });
}

/** The names the dependencies of a version name, each once: the lookups of the packages the installation holds (R-UI-221). */
export function dependencyNames(groups: readonly DependencyGroup[]): string[] {
  return [...new Set(groups.flatMap((g) => g.entries.flatMap((e) => e.alternatives.map((a) => a.name))))];
}

/** A paragraph of the extended description of a Debian version (R-DEB-038): text the screen wraps, or lines shown as written. */
export interface DescriptionBlock {
  readonly text: string;
  readonly verbatim: boolean;
}

export function debLongDescription(m: Record<string, unknown>): DescriptionBlock[] {
  const blocks = Array.isArray(m['longDescription']) ? m['longDescription'] : [];
  return blocks.flatMap((b): DescriptionBlock[] => {
    if (typeof b !== 'object' || b === null) return [];
    const r = b as Record<string, unknown>;
    return typeof r['text'] === 'string' && r['text'] !== '' ? [{ text: r['text'], verbatim: r['verbatim'] === true }] : [];
  });
}

/**
 * The facts of an Ansible collection version (R-ANS-026): the Ansible versions it needs. Its tags are the version's keywords, its
 * dependencies, authors and links are shown as for the other formats (R-UI-221).
 */
export function ansibleFacts(d: PackageVersion): [string, string][] {
  if (d.format !== 'ansible') return [];
  const requires = text(d.metadata['requiresAnsible']);
  return requires === '' ? [] : [[$localize`:@@pkg.meta.ansible-requires:Requires Ansible`, requires]];
}

/**
 * The links of a version: homepage, documentation, source and issue tracker, those it declares; one row under the facts line (R-UI-221).
 * The address of the licence stands beside the licence ({@link licenseLink}).
 */
export function metadataLinks(d: PackageVersion): MetadataLink[] {
  const m = d.metadata;
  const entries: [string, unknown][] = [
    [$localize`:@@pkg.meta.homepage:Homepage`, m['homepage']],
    // The Documentation tab of an Ansible collection holds its documentation; the link out is the collection's own site (Q-UI-245).
    [d.format === 'ansible' ? $localize`:@@pkg.meta.docs-site:Docs site` : $localize`:@@pkg.meta.documentation:Documentation`, m['documentationUrl']],
    [$localize`:@@pkg.meta.source:Source repository`, m['repositoryUrl']],
    [$localize`:@@pkg.meta.bugs:Issue tracker`, m['bugsUrl']],
  ];
  return entries.flatMap(([label, value]): MetadataLink[] =>
    typeof value === 'string' && value.trim() !== '' ? [{ label, text: value, href: safeHttpUrl(value) }] : [],
  );
}

/** The address of a version's licence (NuGet's `licenseUrl`), when it is http or https; the licence on the facts line links to it. */
export function licenseLink(m: Record<string, unknown>): string | null {
  const u = m['licenseUrl'];
  return typeof u === 'string' ? safeHttpUrl(u) : null;
}

/** A deprecation message of the version, or null. */
export function deprecationOf(m: Record<string, unknown>): string | null {
  const d = m['deprecated'];
  return typeof d === 'string' && d !== '' ? d : null;
}
