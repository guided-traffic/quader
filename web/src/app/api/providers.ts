import { Provider, inject } from '@angular/core';
import { SessionStore } from '../core/session-store';
import { SIGNED_IN } from '../core/signed-in';
import { FormatCatalog } from '../core/format-names';
import { HttpFormatCatalog } from '../core/format-catalog-http';
import { SystemClients } from '../core/system-clients';
import { HttpSystemClients } from '../core/system-clients-http';
import { MEMORY_OWNER } from '../core/disclosure-memory';
import { RepositoryDestinationReads } from '../core/repository-destinations';
import { DESTINATIONS } from '../views/packages/destinations';
import { ClientSetupReads, PackageReads } from './package-reads';
import { SignatureReads } from './signature-reads';
import { VulnerabilityReads } from './vulnerability-reads';
import { LegalApi } from './legal-api';
import { HttpLegalApi } from './legal-api-http';
import { FileReads } from './file-reads';
import { FilesApi } from './files-api';
import { HttpClientSetupReads, PackagesApi } from './packages-api';
import { SignaturesApi } from './signatures-api';
import { VulnerabilitiesApi, vulnerabilityDatabaseRead } from './vulnerabilities-api';
import { IMAGE_SCANNING_PAGE, VULNERABILITY_DATABASE_READ } from '../core/vulnerability-database';
import { malwareSignaturesRead } from './malware-api';
import { MALWARE_SIGNATURES_READ } from '../core/malware-signatures';

/**
 * The reads the views take (R-UI-223), bound to the main installation's services that extend them, and whether the reader has a session.
 * `provideApiServices` and the fakes' `provideFakeApis` both spread it, so a view reads what the page beside it reads.
 */
export function provideViewReads(): Provider[] {
  return [
    { provide: PackageReads, useExisting: PackagesApi },
    { provide: VulnerabilityReads, useExisting: VulnerabilitiesApi },
    { provide: SignatureReads, useExisting: SignaturesApi },
    { provide: FileReads, useExisting: FilesApi },
    { provide: FormatCatalog, useClass: HttpFormatCatalog },
    { provide: SystemClients, useClass: HttpSystemClients },
    { provide: SIGNED_IN, useFactory: () => inject(SessionStore).authenticated },
    // Every command panel offers the Bastions that distribute its repository (Q-UI-1657); the Bastion's application binds none.
    { provide: DESTINATIONS, useClass: RepositoryDestinationReads },
    // The memory of an opened section is kept per signed-in principal (Q-UI-967); the Bastion's application keeps it for the tab.
    {
      provide: MEMORY_OWNER,
      useFactory: () => {
        const session = inject(SessionStore);
        return () => session.whoami()?.principal?.name ?? null;
      },
    },
  ];
}

/**
 * The main installation's bindings of what the Bastion's application binds to its own reads (R-UI-228): the view reads, the legal
 * pages, the setup of a client and the dates of the scanners' databases.
 *
 * Every other service of `api/` is not listed here: its abstract class names its HTTP implementation
 * (`@Injectable({ providedIn: 'root', useClass: forwardRef(() => Http…) })`), so it is built where it is first injected and its
 * code lies in the chunk of the screens that use it, not in the first page's. Specs and component tests replace the bindings
 * with the in-memory fakes of `testing/fakes.ts` (`provideFakeApis`).
 */
export function provideApiServices(): Provider[] {
  return [
    ...provideViewReads(),
    { provide: LegalApi, useClass: HttpLegalApi },
    { provide: ClientSetupReads, useClass: HttpClientSetupReads },
    { provide: VULNERABILITY_DATABASE_READ, useFactory: vulnerabilityDatabaseRead },
    // The grade details lead a reader of the settings to the image scanning's page (R-VULN-053); the Bastion binds none.
    {
      provide: IMAGE_SCANNING_PAGE,
      useFactory: (): (() => boolean) => {
        const session = inject(SessionStore);
        return () => session.has('settings:read');
      },
    },
    { provide: MALWARE_SIGNATURES_READ, useFactory: malwareSignaturesRead },
  ];
}
