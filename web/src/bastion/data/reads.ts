import { Injectable, Provider, inject, signal } from '@angular/core';
import { ApiError } from '../../app/api/problem';
import { ClientSetupReads, PackageReads } from '../../app/api/package-reads';
import {
  ClientSetup,
  ClientSetupQuery,
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
} from '../../app/api/packages-api';
import { Child, FileReads, Item } from '../../app/api/file-reads';
import { Page, PageFetcher } from '../../app/api/pagination';
import { splitRef } from '../../app/api/refs';
import { SignatureReads } from '../../app/api/signature-reads';
import { SignatureFileReport, SignatureRelease, SignatureVersionReport } from '../../app/api/signatures-api';
import {
  VulnerabilityAdvisoryDetail,
  VulnerabilityFinding,
  VulnerabilityPackageReport,
  VulnerabilityVersionReport,
} from '../../app/api/vulnerabilities-api';
import { VulnerabilityReads } from '../../app/api/vulnerability-reads';
import { LegalApi, LegalPages } from '../../app/api/legal-api';
import type { MalwareSignatures } from '../../app/api/malware-api';
import { MALWARE_SIGNATURES_READ } from '../../app/core/malware-signatures';
import type { VulnerabilityDatabase } from '../../app/api/vulnerabilities-api';
import { VULNERABILITY_DATABASE_READ } from '../../app/core/vulnerability-database';
import { FormatDescriptor } from '../../app/api/repositories-api';
import { FormatCatalog, withFormatWords } from '../../app/core/format-names';
import { SIGNED_IN } from '../../app/core/signed-in';
import { SystemClients } from '../../app/core/system-clients';
import { narrowDocTree } from '../../app/views/packages/docs-model';
import { BastionData, DATA_ROOT, seg } from './http';
import { BASTION_FORMATS } from './formats';
import { VersionTimes } from './times';

/** The path of a package's documents in the catalogue's grammar (R-BAS-103). */
function packagePath(id: string): string {
  return `packages/${seg(id)}`;
}

function versionPath(id: string, version: string): string {
  return `${packagePath(id)}/versions/${seg(version)}`;
}

/** The documents of a repository's files in the catalogue's grammar (R-BAS-103): `files/<project>/<key>/<kind>`. */
function filesPath(repository: string, kind: 'folder' | 'file' | `setup/${string}`): string {
  const { project, key } = splitRef(repository);
  return `files/${seg(project)}/${seg(key)}/${kind}`;
}

/** A path inside a repository, each of its segments escaped: `a b/c.txt` is `a%20b/c.txt`. */
function segments(path: string): string {
  return path.split('/').filter((s) => s !== '').map(seg).join('/');
}

/** A 404: the catalogue does not hold the document, which the main installation's reads answer as "none" where they do. */
function missing(e: unknown): boolean {
  return e instanceof ApiError && e.status === 404;
}

/** The answer of a read the catalogue has no document for: what the main installation answers a caller who may not read it. */
function notHeld(): ApiError {
  return new ApiError(404, null, null, null);
}

/**
 * The package reads of the Bastion's application (R-UI-228): the catalogue's documents below `/ui/data/v1/`, in the main installation's
 * shapes with the times of their version's state put back (times.ts, Q-UI-1451), so the shared views read them unchanged. What the
 * catalogue does not hold (the packages that depend on one, a namespace's page, a NuGet package's embedded icon and licence) answers as
 * the main installation answers a caller who may not read it.
 */
@Injectable()
export class BastionPackageReads extends PackageReads {
  private readonly data = inject(BastionData);
  private readonly times = inject(VersionTimes);

  override list(filters: PackageFilters): PageFetcher<PackageSummary> {
    return this.data.page<PackageSummary>('packages', {
      q: filters.q,
      format: filters.format,
      project: filters.project,
      repository: filters.repository,
      deprecated: filters.deprecated ? true : undefined,
      sort: filters.sort,
    });
  }

  override lookup(format: string, name: string): Promise<PackageSummary> {
    return this.data.get<PackageSummary>('packages/lookup', { format, name });
  }

  /**
   * Every version in the main installation's order (Q-BAS-396); with `group=content` an image's list by content, the catalogue's
   * document of the main installation's answer (`packages/<id>/images`, bastion.md R-BAS-130), whose leading tag and order are the
   * server's. A `repository` keeps the rows its repositories hold, as the main installation narrows them: a repository's opened row lists
   * what that repository holds; an image's other tags in a second repository stay named, since a row is one manifest (Q-BAS-501).
   */
  override versions(packageId: string, filters: VersionFilters = {}): PageFetcher<PackageVersionSummary> {
    const held = filters.repository && filters.repository.length > 0 ? new Set(filters.repository) : null;
    const keep = (items: readonly PackageVersionSummary[]): readonly PackageVersionSummary[] =>
      held === null ? items : items.filter((v) => v.repositories.some((r) => held.has(r)));
    if (filters.group === 'content') {
      return async (cursor, limit): Promise<Page<PackageVersionSummary>> => {
        const all = keep((await this.data.get<{ items: readonly PackageVersionSummary[] }>(`${packagePath(packageId)}/images`)).items);
        const from = cursor === null ? 0 : Number(cursor);
        const to = from + limit;
        return { items: all.slice(from, to), nextCursor: to < all.length ? String(to) : null };
      };
    }
    const pages = this.data.page<PackageVersionSummary>(`${packagePath(packageId)}/versions`);
    return async (cursor, limit) => {
      const page = await pages(cursor, limit);
      return held === null ? page : { ...page, items: keep(page.items) };
    };
  }

  override version(packageId: string, version: string): Promise<PackageVersion> {
    return this.times.with(packageId, version, 'version', this.data.get<PackageVersion>(versionPath(packageId, version)));
  }

  override async readme(packageId: string, version: string): Promise<PackageReadme | null> {
    try {
      return await this.times.with(packageId, version, 'readme', this.data.get<PackageReadme>(`${versionPath(packageId, version)}/readme`));
    } catch (e) {
      if (missing(e)) return null;
      throw e;
    }
  }

  /**
   * A version's documentation tree; with `q`, narrowed in the browser by the main installation's rule (docs-model.ts `narrowDocTree`,
   * bastion.md R-BAS-124): the catalogue holds the whole tree with each entry's search words, and the Bastion runs no search of its own
   * for a version. A text the main installation refuses (fewer than 2 characters) is refused the same way.
   */
  override async docs(packageId: string, version: string, query: DocsQuery = {}): Promise<PackageDocTree | null> {
    let tree: PackageDocTree;
    try {
      tree = await this.times.with(packageId, version, 'docs', this.data.get<PackageDocTree>(`${versionPath(packageId, version)}/docs`));
    } catch (e) {
      if (missing(e)) return null;
      throw e;
    }
    if (query.q === undefined) return tree;
    const narrowed = narrowDocTree(tree, query.q);
    if (narrowed === null) throw new ApiError(422, null, null, null);
    return narrowed;
  }

  override docEntry(packageId: string, version: string, type: string, name: string): Promise<PackageDocEntry> {
    const entry = this.data.get<PackageDocEntry>(`${versionPath(packageId, version)}/docs/entries/${seg(type)}/${seg(name)}`);
    return this.times.with(packageId, version, 'docEntry', entry);
  }

  override docFile(packageId: string, version: string, file: string): Promise<PackageDocFile> {
    const path = file.split('/').map(seg).join('/');
    return this.times.with(packageId, version, 'docFile', this.data.get<PackageDocFile>(`${versionPath(packageId, version)}/docs/files/${path}`));
  }

  override async namespace(): Promise<PackageNamespace> {
    throw notHeld();
  }

  override contents(packageId: string, version: string): Promise<PackageVersionContents> {
    return this.times.with(packageId, version, 'contents', this.data.get<PackageVersionContents>(`${versionPath(packageId, version)}/contents`));
  }

  override async dependents(): Promise<PackageDependents> {
    throw notHeld();
  }

  /** The catalogue keeps no embedded picture: an address that answers 404, so the view shows the format's icon. */
  override iconUrl(packageId: string, version: string): string {
    return `${DATA_ROOT}${versionPath(packageId, version)}/icon`;
  }

  override licenseUrl(packageId: string, version: string): string {
    return `${DATA_ROOT}${versionPath(packageId, version)}/license`;
  }
}

/**
 * The client set-up of the Bastion's application (R-UI-228): the catalogue's set-up documents, rendered by the main installation on the
 * Bastion's addresses without a credential and without publish lines (R-BAS-104). A version's set-up is named by the package's catalogue
 * ID, which a search of the repository finds by the name the view asks with.
 */
@Injectable()
export class BastionClientSetupReads extends ClientSetupReads {
  private readonly data = inject(BastionData);
  private readonly ids = new Map<string, Promise<string | null>>();

  override async get(repository: string, query: ClientSetupQuery): Promise<ClientSetup> {
    const [project, repo] = repository.split('/');
    if (!project || !repo) throw notHeld();
    // A file's commands are the set-up of its path, which the builder rendered with its SHA-256 in the check (Q-UI-1454); a folder's
    // upload is no read of a visitor.
    if (query.path !== undefined) {
      if (query.path === '' || query.path.endsWith('/')) throw notHeld();
      return this.data.get<ClientSetup>(`${filesPath(repository, `setup/${seg(query.client)}`)}/${segments(query.path)}`);
    }
    let path = `setup/${seg(project)}/${seg(repo)}/${seg(query.client)}`;
    if (query.package) {
      // A version's set-up names it. A package's without a version, the pull by digest, is the catalogue's set-up of the package
      // (`setup/<p>/<r>/<c>/<id>`, R-BAS-130): the repository's would name the snippet's placeholder image (`myapp:1.0@sha256:…`).
      const id = await this.idOf(repository, query.package);
      if (id === null) throw notHeld();
      path += `/${seg(id)}`;
      if (query.version) path += `/${seg(query.version)}`;
    }
    return this.data.get<ClientSetup>(path);
  }

  private idOf(repository: string, name: string): Promise<string | null> {
    const key = `${repository}\n${name}`;
    let found = this.ids.get(key);
    if (!found) {
      found = this.data
        .get<{ items: readonly PackageSummary[] }>('packages', { repository: [repository], q: name, limit: 100 })
        .then((page) => page.items.find((p) => p.name === name || p.key === name)?.id ?? null);
      found.catch(() => this.ids.delete(key));
      this.ids.set(key, found);
    }
    return found;
  }
}

/**
 * The vulnerability reads of the Bastion's application (R-UI-228): a version's report of open findings (W-35's B.4a), which carries its
 * findings for a reader without a session (R-UI-224), and a package's grade per version, the column of its versions' table (bastion.md
 * R-BAS-128). The catalogue holds no advisory: that answers as a read the caller may not make.
 */
@Injectable()
export class BastionVulnerabilityReads extends VulnerabilityReads {
  private readonly data = inject(BastionData);
  private readonly times = inject(VersionTimes);

  override versionReport(packageId: string, version: string): Promise<VulnerabilityVersionReport> {
    const report = this.data.get<VulnerabilityVersionReport>(`${versionPath(packageId, version)}/vulnerabilities`);
    return this.times.with(packageId, version, 'vulnerabilities', report);
  }

  /**
   * The grade of every version as the catalogue keeps it (`packages/<id>/vulnerabilities`): over every public repository that holds the
   * version, the worst first, as the main installation answers an anonymous caller; the document has no narrower form, so a repository
   * named here narrows nothing (no view of the Bastion names one).
   */
  override packageReport(packageId: string): Promise<VulnerabilityPackageReport> {
    return this.data.get<VulnerabilityPackageReport>(`${packagePath(packageId)}/vulnerabilities`);
  }

  override findings(): PageFetcher<VulnerabilityFinding> {
    return async () => {
      throw notHeld();
    };
  }

  override async advisory(): Promise<VulnerabilityAdvisoryDetail> {
    throw notHeld();
  }
}

/**
 * The signature reads of the Bastion's application (R-UI-228): a version's report as the catalogue keeps it (W-35's B.9). A file's
 * report, the gate's releases and the receipt attestation's download are the main installation's; the views read none of them for a
 * reader who cannot act.
 */
@Injectable()
export class BastionSignatureReads extends SignatureReads {
  private readonly data = inject(BastionData);
  private readonly times = inject(VersionTimes);

  override version(packageId: string, version: string): Promise<SignatureVersionReport> {
    return this.times.with(packageId, version, 'signatures', this.data.get<SignatureVersionReport>(`${versionPath(packageId, version)}/signatures`));
  }

  override async file(): Promise<SignatureFileReport> {
    throw notHeld();
  }

  override async releases(): Promise<readonly SignatureRelease[]> {
    return [];
  }

  override attestationUrl(): string {
    return '';
  }
}

/**
 * The folders and files of a public generic repository at the Bastion (R-UI-229, Q-UI-1454): the catalogue's document of a folder holds
 * every entry of it (R-BAS-104), which the reads answer in pages of the view's size, so the list keeps its **Load more**; a file is its
 * description as the main installation answered the anonymous caller, its download address the Bastion's own (R-BAS-107).
 */
@Injectable()
export class BastionFileReads extends FileReads {
  private readonly data = inject(BastionData);

  override item(repository: string, path: string): Promise<Item> {
    return this.data.get<Item>(`${filesPath(repository, 'file')}/${segments(path)}`);
  }

  override children(repository: string, path: string, limit: number): PageFetcher<Child> {
    const at = path === '' ? filesPath(repository, 'folder') : `${filesPath(repository, 'folder')}/${segments(path)}`;
    return async (cursor): Promise<Page<Child>> => {
      const all = (await this.data.get<{ items: readonly Child[] }>(at)).items;
      const from = cursor === null ? 0 : Number(cursor);
      const to = from + limit;
      return { items: all.slice(from, to), nextCursor: to < all.length ? String(to) : null };
    };
  }
}

/** The Bastion's own imprint and privacy notice (R-BAS-100, R-BAS-118) in the shape of the main installation's. */
@Injectable()
export class BastionLegalApi extends LegalApi {
  private readonly data = inject(BastionData);

  override pages(locale: 'en' | 'de'): Promise<LegalPages> {
    return this.data.get<LegalPages>('legal', { locale });
  }
}

/**
 * The formats as the Bastion's application names them: the product's own table (./formats.ts), since the catalogue holds no
 * descriptor of the main installation's formats (Q-UI-1436), with the names the UI shows (R-UI-272).
 */
@Injectable()
export class BastionFormatCatalog extends FormatCatalog {
  override formats(): Promise<readonly FormatDescriptor[]> {
    return Promise.resolve(withFormatWords(BASTION_FORMATS));
  }
}

/**
 * Whether a system repository serves its clients, at the Bastion (R-UI-228): its catalogue shows a repository only while it is public,
 * and the ClamAV definitions are public only while their updates are switched on (R-REPO-075), so a system repository a page names
 * serves them.
 */
@Injectable()
export class BastionSystemClients extends SystemClients {
  override served(): Promise<boolean> {
    return Promise.resolve(true);
  }

  override forget(): void {
    // Nothing is kept.
  }
}

/**
 * Today's signatures of the malware scan at the Bastion (R-UI-258, malware-scanning.md R-MAL-042): what the catalogue's state carries of
 * the main installation's set in use and the bounds of a check's class, in the main installation's shape, without the time of its
 * import and the engine. The marks of every page share it (`MalwareSignaturesStore`), as the main installation's do.
 */
function bastionMalwareSignatures(): () => Promise<MalwareSignatures> {
  const data = inject(BastionData);
  return () => data.get<MalwareSignatures>('malware/signatures');
}

/**
 * Today's vulnerability database of the image scanning at the Bastion (R-UI-258, vulnerability-scanning.md R-VULN-052): the unit, the
 * digest and the build time the catalogue's state carries, in the main installation's shape, without when the installation last
 * confirmed it. The grades of an image share it (`VulnerabilityDatabaseStore`), so their details say the database then and today.
 */
function bastionVulnerabilityDatabase(): () => Promise<VulnerabilityDatabase> {
  const data = inject(BastionData);
  return () => data.get<VulnerabilityDatabase>('vulnerabilities/database');
}

/**
 * The reads of the Bastion's application, bound to the catalogue (R-UI-228): the views' abstract reads, the legal pages, the format
 * catalogue, the system repositories' switch, today's malware signatures, and a reader who is never signed in. A panel of commands leads
 * to the Bastion's own page Set up client (bastion.md R-BAS-129), so `SETUP_PAGES` keeps its default.
 */
export function provideBastionReads(): Provider[] {
  return [
    { provide: PackageReads, useClass: BastionPackageReads },
    { provide: ClientSetupReads, useClass: BastionClientSetupReads },
    { provide: VulnerabilityReads, useClass: BastionVulnerabilityReads },
    { provide: SignatureReads, useClass: BastionSignatureReads },
    { provide: FileReads, useClass: BastionFileReads },
    { provide: LegalApi, useClass: BastionLegalApi },
    { provide: FormatCatalog, useClass: BastionFormatCatalog },
    { provide: SystemClients, useClass: BastionSystemClients },
    { provide: MALWARE_SIGNATURES_READ, useFactory: bastionMalwareSignatures },
    { provide: VULNERABILITY_DATABASE_READ, useFactory: bastionVulnerabilityDatabase },
    { provide: SIGNED_IN, useValue: signal(false).asReadonly() },
  ];
}
