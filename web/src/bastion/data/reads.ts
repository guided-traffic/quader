import { Injectable, Provider, inject, signal } from '@angular/core';
import { ApiError } from '../../app/api/problem';
import { ClientSetupReads, PackageReads } from '../../app/api/package-reads';
import {
  ClientSetup,
  ClientSetupQuery,
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
} from '../../app/api/packages-api';
import { PageFetcher } from '../../app/api/pagination';
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
import { FormatDescriptor } from '../../app/api/repositories-api';
import { FormatCatalog } from '../../app/core/format-names';
import { SIGNED_IN } from '../../app/core/signed-in';
import { SystemClients } from '../../app/core/system-clients';
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

  /** Every version in the main installation's order; the views sort and group them in the browser (Q-BAS-396). */
  override versions(packageId: string): PageFetcher<PackageVersionSummary> {
    return this.data.page<PackageVersionSummary>(`${packagePath(packageId)}/versions`);
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

  override async docs(packageId: string, version: string): Promise<PackageDocTree | null> {
    try {
      return await this.times.with(packageId, version, 'docs', this.data.get<PackageDocTree>(`${versionPath(packageId, version)}/docs`));
    } catch (e) {
      if (missing(e)) return null;
      throw e;
    }
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
    let path = `setup/${seg(project)}/${seg(repo)}/${seg(query.client)}`;
    if (query.package && query.version) {
      const id = await this.idOf(repository, query.package);
      if (id === null) throw notHeld();
      path += `/${seg(id)}/${seg(query.version)}`;
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
 * findings for a reader without a session (R-UI-224). The catalogue holds no report per package and no advisory: those answer as a
 * read the caller may not make, and the views show the version's report alone.
 */
@Injectable()
export class BastionVulnerabilityReads extends VulnerabilityReads {
  private readonly data = inject(BastionData);
  private readonly times = inject(VersionTimes);

  override versionReport(packageId: string, version: string): Promise<VulnerabilityVersionReport> {
    const report = this.data.get<VulnerabilityVersionReport>(`${versionPath(packageId, version)}/vulnerabilities`);
    return this.times.with(packageId, version, 'vulnerabilities', report);
  }

  override async packageReport(): Promise<VulnerabilityPackageReport> {
    throw notHeld();
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
 * descriptor of the main installation's formats (Q-UI-1436).
 */
@Injectable()
export class BastionFormatCatalog extends FormatCatalog {
  override formats(): Promise<readonly FormatDescriptor[]> {
    return Promise.resolve(BASTION_FORMATS);
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
 * The reads of the Bastion's application, bound to the catalogue (R-UI-228): the views' abstract reads, the legal pages, the format
 * catalogue, the system repositories' switch, and a reader who is never signed in.
 */
export function provideBastionReads(): Provider[] {
  return [
    { provide: PackageReads, useClass: BastionPackageReads },
    { provide: ClientSetupReads, useClass: BastionClientSetupReads },
    { provide: VulnerabilityReads, useClass: BastionVulnerabilityReads },
    { provide: SignatureReads, useClass: BastionSignatureReads },
    { provide: LegalApi, useClass: BastionLegalApi },
    { provide: FormatCatalog, useClass: BastionFormatCatalog },
    { provide: SystemClients, useClass: BastionSystemClients },
    { provide: SIGNED_IN, useValue: signal(false).asReadonly() },
  ];
}
