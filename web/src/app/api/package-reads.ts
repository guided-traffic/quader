import { PageFetcher } from './pagination';
import {
  ClientSetup,
  ClientSetupQuery,
  ContentsQuery,
  DocsQuery,
  PackageDependents,
  PackageDocEntry,
  PackageDocFile,
  PackageDocTree,
  PackageFilters,
  PackageNamespace,
  PackageReadme,
  PackageSummary,
  PackageVersion,
  PackageVersionContents,
  PackageVersionSummary,
  VersionFilters,
} from './packages-api';

/**
 * What the views of a package read (R-UI-223): the packages, a package and its versions, a version with its README, documentation and
 * contents, the packages that depend on it, a namespace, and the addresses of what a version's package embeds. The main installation's
 * `PackagesApi` extends it with its writes; an application without the management API (the Bastion's web interface) provides its own,
 * and the same views read from wherever that application reads.
 */
export abstract class PackageReads {
  /** One page fetcher; a new filter or sort is a new fetcher (cursor binding, R-API-010). */
  abstract list(filters: PackageFilters): PageFetcher<PackageSummary>;
  /** Resolves a format and a name; rejects with 404 when no visible version exists. */
  abstract lookup(format: string, name: string): Promise<PackageSummary>;
  abstract versions(packageId: string, filters?: VersionFilters): PageFetcher<PackageVersionSummary>;
  abstract version(packageId: string, version: string): Promise<PackageVersion>;
  /** The sanitized README, or `null` when the version has none (404). */
  abstract readme(packageId: string, version: string): Promise<PackageReadme | null>;
  /**
   * The documentation tree of a version (R-ANS-036), or `null` for a 404 or a refusal: the documentation is wiki content and a
   * caller who may not read it is told what it is told for a version that does not exist, so the tab goes without a message (Q-UI-244).
   */
  abstract docs(packageId: string, version: string, query?: DocsQuery): Promise<PackageDocTree | null>;
  /** One page of the documentation: a module, a plugin or a role. */
  abstract docEntry(
    packageId: string,
    version: string,
    type: string,
    name: string,
    repository?: string,
  ): Promise<PackageDocEntry>;
  /** The README or a Markdown file of the documentation as sanitized HTML. */
  abstract docFile(packageId: string, version: string, file: string, repository?: string): Promise<PackageDocFile>;
  /**
   * What decides a publish into a namespace (R-UI-220): `publishing` only for a reader of a repository's configuration; rejects with
   * 404 for a text that is no namespace of the format.
   */
  abstract namespace(format: string, name: string, project?: string): Promise<PackageNamespace>;
  /**
   * What Quader read from a Debian package file of the version (R-DEB-039): its changelog, its copyright and a page of its files, or
   * that it reads them still or could not read them.
   */
  abstract contents(packageId: string, version: string, query?: ContentsQuery): Promise<PackageVersionContents>;
  /**
   * The packages that depend on a package, in the repositories the caller may read (R-API-086): at most 50, with how many there are; in
   * `project` only that project's, as its package list.
   */
  abstract dependents(packageId: string, project?: string): Promise<PackageDependents>;
  /**
   * The address of the picture a version's package embeds (formats/nuget.md R-NUGET-038), which the reader's browser loads: an address of
   * the application that serves the version, never one the package names.
   */
  abstract iconUrl(packageId: string, version: string): string;
  /** The address of the licence file a version's package embeds, served as text (formats/nuget.md R-NUGET-038). */
  abstract licenseUrl(packageId: string, version: string): string;
}

/**
 * The client setup snippets of a repository (R-API-030): what the clients' commands of a version and the page Set up client read, the
 * same read for the main installation and for an application without its management API (R-UI-223).
 */
export abstract class ClientSetupReads {
  abstract get(repository: string, query: ClientSetupQuery): Promise<ClientSetup>;
}
