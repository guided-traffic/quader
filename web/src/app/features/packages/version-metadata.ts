import { PackageVersion } from '../../api/packages-api';
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

export interface DependencyGroup {
  readonly label: string;
  readonly entries: readonly (readonly [string, string])[];
}

/** The dependencies of a NuGet package for one target framework (`null`: declared for all). */
export interface FrameworkGroup {
  readonly framework: string | null;
  readonly entries: readonly (readonly [string, string])[];
}

/** One platform of an OCI image index (R-OCI-031): the child manifest it names, with its digest, media type and size. */
export interface ImagePlatform {
  /** The platform as the server sends it, or the words for none (a child without a platform). */
  readonly platform: string;
  readonly digest: string;
  readonly mediaType: string;
  readonly size: number | null;
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
  readonly size: number | null;
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
      hint,
      copyLabel:
        given === ''
          ? $localize`:@@pkg.image.copy-no-platform-digest:Copy digest of the child without a platform (${hint}:digest:)`
          : hint === ''
            ? $localize`:@@pkg.image.copy-platform-digest:Copy digest of ${platform}:platform:`
            : $localize`:@@pkg.image.copy-platform-digest-short:Copy digest of ${platform}:platform: (${hint}:digest:)`,
    };
  });
  return { digest, mediaType: raw(m['mediaType']), artifactType: raw(m['artifactType']), size: num(m['size']), platforms };
}

/**
 * The `dependencyGroups` member of NuGet metadata (formats/nuget.md R-NUGET-007): a list of groups with an optional
 * `targetFramework` and `dependencies` of `id` and `range`. Anything else is skipped.
 */
export function frameworkGroups(value: unknown): FrameworkGroup[] {
  if (!Array.isArray(value)) return [];
  return value.flatMap((g): FrameworkGroup[] => {
    if (typeof g !== 'object' || g === null) return [];
    const group = g as Record<string, unknown>;
    const deps = Array.isArray(group['dependencies']) ? group['dependencies'] : [];
    const entries = deps.flatMap((d): [string, string][] => {
      if (typeof d !== 'object' || d === null) return [];
      const dep = d as Record<string, unknown>;
      const id = dep['id'];
      const range = dep['range'] ?? dep['version'];
      return typeof id === 'string' ? [[id, typeof range === 'string' && range !== '' ? range : '*']] : [];
    });
    const fw = group['targetFramework'];
    return [{ framework: typeof fw === 'string' && fw !== '' ? fw : null, entries }];
  });
}

/** The npm dependency kinds that are present, each with its entries. */
export function dependencyGroups(m: Record<string, unknown>): DependencyGroup[] {
  return [
    { label: $localize`:@@pkg.meta.dependencies:Dependencies`, entries: stringRecord(m['dependencies']) },
    { label: $localize`:@@pkg.meta.dev-dependencies:Development dependencies`, entries: stringRecord(m['devDependencies']) },
    { label: $localize`:@@pkg.meta.peer-dependencies:Peer dependencies`, entries: stringRecord(m['peerDependencies']) },
    { label: $localize`:@@pkg.meta.optional-dependencies:Optional dependencies`, entries: stringRecord(m['optionalDependencies']) },
  ].filter((g) => g.entries.length > 0);
}

/** The plain members of a NuGet version (Q-NUGET-48): text, a name list or a version, shown as given. */
export function nugetFacts(m: Record<string, unknown>): [string, string][] {
  const entries: [string, string][] = [
    [$localize`:@@pkg.meta.min-client:Minimum client version`, text(m['minClientVersion'])],
    [$localize`:@@pkg.meta.package-types:Package types`, strings(m['packageTypes']).join(', ')],
    [$localize`:@@pkg.meta.language:Language`, text(m['language'])],
    [$localize`:@@pkg.meta.copyright:Copyright`, text(m['copyright'])],
    [$localize`:@@pkg.meta.release-notes:Release notes`, text(m['releaseNotes'])],
  ];
  return entries.filter((e) => e[1] !== '');
}

/**
 * The facts of a Debian version (R-DEB-030): the architectures of its files, its coordinates (suite and component, shown as
 * `stable/main`) and the control fields the view adapter sends. Installed-Size is in KiB, as the control field says.
 */
export function debFacts(m: Record<string, unknown>): [string, string][] {
  const size = text(m['installedSize']);
  const entries: [string, string][] = [
    [$localize`:@@pkg.meta.deb-architectures:Architectures`, strings(m['architectures']).join(', ')],
    [$localize`:@@pkg.meta.deb-coordinates:Suites and components`, strings(m['coordinates']).join(', ')],
    [$localize`:@@pkg.meta.deb-maintainer:Maintainer`, text(m['maintainer'])],
    [$localize`:@@pkg.meta.deb-section:Section`, text(m['section'])],
    [$localize`:@@pkg.meta.deb-priority:Priority`, text(m['priority'])],
    [$localize`:@@pkg.meta.deb-installed-size:Installed size`, size === '' ? '' : $localize`:@@pkg.meta.deb-kib:${size}:size: KiB`],
    [$localize`:@@pkg.meta.deb-depends:Depends`, text(m['depends'])],
    [$localize`:@@pkg.meta.deb-source:Source package`, text(m['source'])],
  ];
  return entries.filter((e) => e[1] !== '');
}

/**
 * The facts of an Ansible collection version (R-ANS-026): the Ansible versions it needs and its tags. The licences are the version's
 * facts, the tags the generic keywords of the package; its dependencies, authors and links are shown as for the other formats.
 */
export function ansibleFacts(d: PackageVersion, keywords: readonly string[]): [string, string][] {
  if (d.format !== 'ansible') return [];
  const entries: [string, string][] = [
    [$localize`:@@pkg.meta.ansible-requires:Requires Ansible`, text(d.metadata['requiresAnsible'])],
    [$localize`:@@pkg.meta.ansible-tags:Tags`, keywords.join(', ')],
  ];
  return entries.filter((e) => e[1] !== '');
}

/** The links of a version: homepage, licence, documentation, source and issue tracker, those it declares. */
export function metadataLinks(d: PackageVersion): MetadataLink[] {
  const m = d.metadata;
  const entries: [string, unknown][] = [
    [$localize`:@@pkg.meta.homepage:Homepage`, m['homepage']],
    [$localize`:@@pkg.meta.license-url:License`, m['licenseUrl']],
    // The Documentation tab of an Ansible collection holds its documentation; the link out is the collection's own site (Q-UI-245).
    [d.format === 'ansible' ? $localize`:@@pkg.meta.docs-site:Docs site` : $localize`:@@pkg.meta.documentation:Documentation`, m['documentationUrl']],
    [$localize`:@@pkg.meta.source:Source repository`, m['repositoryUrl']],
    [$localize`:@@pkg.meta.bugs:Issue tracker`, m['bugsUrl']],
  ];
  return entries.flatMap(([label, value]): MetadataLink[] =>
    typeof value === 'string' && value.trim() !== '' ? [{ label, text: value, href: safeHttpUrl(value) }] : [],
  );
}

/** A deprecation message of the version, or null. */
export function deprecationOf(m: Record<string, unknown>): string | null {
  const d = m['deprecated'];
  return typeof d === 'string' && d !== '' ? d : null;
}
