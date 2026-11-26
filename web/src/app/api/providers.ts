import { Provider } from '@angular/core';
import { HttpLoginApi, LoginApi } from './login-api';
import { HttpPreferencesApi, PreferencesApi } from './preferences-api';
import { HttpSessionApi, SessionApi } from './session-api';
// >>> M1 ui-a
import { AuditApi, HttpAuditApi } from './audit-api';
import { EndpointsApi, HttpEndpointsApi } from './endpoints-api';
import { FilesApi, HttpFilesApi } from './files-api';
import { HttpJobsApi, JobsApi } from './jobs-api';
import { HttpRepositoriesApi, RepositoriesApi } from './repositories-api';
// <<< M1 ui-a
import { EncryptionApi, HttpEncryptionApi, HttpSystemApi, SystemApi } from './system-api';
// >>> M1 ui-b
import { AuthSettingsApi, HttpAuthSettingsApi } from './auth-settings-api';
import { HttpPolicyApi, HttpRolesApi, PolicyApi, RolesApi } from './authz-api';
import { HttpIdentityApi, IdentityApi } from './identity-api';
import { HttpProjectsApi, ProjectsApi } from './projects-api';
import { HttpVisibilityApi, VisibilityApi } from './visibility-api';
import { DraftPolicyExplainer, PolicyExplainer } from './policy-explainer';
import { HttpSessionsApi, SessionsApi } from './sessions-api';
import { HttpTokensApi, TokensApi } from './tokens-api';
import { HttpTechnicalTokensApi, TechnicalTokensApi } from './technical-tokens-api';
// <<< M1 ui-b
// >>> M2 ui-packages
import { ClientSetupApi, HttpClientSetupApi, HttpPackagesApi, PackagesApi } from './packages-api';
import { GuidesApi, HttpGuidesApi } from './guides-api';
import { HttpManualApi, ManualApi } from './manual-api';
// <<< M2 ui-packages
// >>> M4 ci
import { CiApi, HttpCiApi } from './ci-api';
// <<< M4 ci
// >>> M5b bastion
import { BastionsApi, HttpBastionsApi } from './bastions-api';
// <<< M5b bastion
// >>> M4b vulnerabilities
import { HttpVulnerabilitiesApi, VulnerabilitiesApi } from './vulnerabilities-api';
// <<< M4b vulnerabilities
// >>> G2 backups
import { BackupsApi, HttpBackupsApi } from './backups-api';
// <<< G2 backups
// >>> F0 ansible
import { AnsibleApi, HttpAnsibleApi } from './ansible-api';
import { HttpNuGetApi, NuGetApi } from './nuget-api';
// <<< F0 ansible
// >>> T17 deb
import { DebApi, HttpDebApi } from './deb-api';
// <<< T17 deb
// >>> T-22 search
import { HttpSearchApi, SearchApi } from './search-api';
// <<< T-22 search
// >>> W-26 limits
import { HttpLimitsApi, LimitsApi } from './limits-api';
// <<< W-26 limits
// >>> upstream
import { HttpUpstreamApi, UpstreamApi } from './upstream-api';
// <<< upstream
// >>> malware
import { HttpMalwareApi, MalwareApi } from './malware-api';
// <<< malware
// >>> download-links
import { DownloadLinksApi, HttpDownloadLinksApi } from './download-links-api';
// <<< download-links
// >>> signatures
import { HttpSignaturesApi, SignaturesApi } from './signatures-api';
import { HttpSigningApi, SigningApi } from './signing-api';
// <<< signatures
// >>> storage connections
import { HttpStorageApi, StorageApi } from './storage-api';
// <<< storage connections
// >>> T-24 downloads
import { HttpRemoteDownloadsApi, RemoteDownloadsApi } from './remote-downloads-api';
// <<< T-24 downloads
// >>> T-24 upstream
import { DeliveryRequestsApi, HttpDeliveryRequestsApi } from './delivery-requests-api';
import { DeliveryRulesApi, HttpDeliveryRulesApi } from './delivery-rules-api';
import { HttpUpstreamCatalogueApi, UpstreamCatalogueApi } from './upstream-catalogue-api';
// <<< T-24 upstream
// >>> T-26 upstream downloads
import { HttpUpstreamDownloadsApi, UpstreamDownloadsApi } from './upstream-downloads-api';
// <<< T-26 upstream downloads

/**
 * Binds every service interface of `api/` to its HTTP implementation. Specs and component tests
 * replace the bindings with the in-memory fakes of `testing/fakes.ts` (`provideFakeApis`).
 */
export function provideApiServices(): Provider[] {
  return [
    { provide: SessionApi, useClass: HttpSessionApi },
    { provide: LoginApi, useClass: HttpLoginApi },
    { provide: PreferencesApi, useClass: HttpPreferencesApi },
    { provide: SystemApi, useClass: HttpSystemApi },
    { provide: EncryptionApi, useClass: HttpEncryptionApi },
    // >>> M1 ui-b
    { provide: IdentityApi, useClass: HttpIdentityApi },
    { provide: TokensApi, useClass: HttpTokensApi },
    { provide: TechnicalTokensApi, useClass: HttpTechnicalTokensApi },
    { provide: SessionsApi, useClass: HttpSessionsApi },
    { provide: AuthSettingsApi, useClass: HttpAuthSettingsApi },
    { provide: PolicyApi, useClass: HttpPolicyApi },
    { provide: RolesApi, useClass: HttpRolesApi },
    { provide: ProjectsApi, useClass: HttpProjectsApi },
    { provide: VisibilityApi, useClass: HttpVisibilityApi },
    { provide: PolicyExplainer, useClass: DraftPolicyExplainer },
    // <<< M1 ui-b
    // >>> M1 ui-a
    { provide: RepositoriesApi, useClass: HttpRepositoriesApi },
    { provide: FilesApi, useClass: HttpFilesApi },
    { provide: EndpointsApi, useClass: HttpEndpointsApi },
    { provide: AuditApi, useClass: HttpAuditApi },
    { provide: JobsApi, useClass: HttpJobsApi },
    // <<< M1 ui-a
    // >>> M2 ui-packages
    { provide: PackagesApi, useClass: HttpPackagesApi },
    { provide: ClientSetupApi, useClass: HttpClientSetupApi },
    { provide: GuidesApi, useClass: HttpGuidesApi },
    { provide: ManualApi, useClass: HttpManualApi },
    // <<< M2 ui-packages
    // >>> M4 ci
    { provide: CiApi, useClass: HttpCiApi },
    // <<< M4 ci
    // >>> M5b bastion
    { provide: BastionsApi, useClass: HttpBastionsApi },
    // <<< M5b bastion
    // >>> M4b vulnerabilities
    { provide: VulnerabilitiesApi, useClass: HttpVulnerabilitiesApi },
    // <<< M4b vulnerabilities
    // >>> G2 backups
    { provide: BackupsApi, useClass: HttpBackupsApi },
    // <<< G2 backups
    // >>> F0 ansible
    { provide: AnsibleApi, useClass: HttpAnsibleApi },
    { provide: NuGetApi, useClass: HttpNuGetApi },
    // <<< F0 ansible
    // >>> T17 deb
    { provide: DebApi, useClass: HttpDebApi },
    // <<< T17 deb
    // >>> T-22 search
    { provide: SearchApi, useClass: HttpSearchApi },
    // <<< T-22 search
    // >>> W-26 limits
    { provide: LimitsApi, useClass: HttpLimitsApi },
    // <<< W-26 limits
    // >>> upstream
    { provide: UpstreamApi, useClass: HttpUpstreamApi },
    // <<< upstream
    // >>> malware
    { provide: MalwareApi, useClass: HttpMalwareApi },
    // <<< malware
    // >>> download-links
    { provide: DownloadLinksApi, useClass: HttpDownloadLinksApi },
    // <<< download-links
    // >>> signatures
    { provide: SignaturesApi, useClass: HttpSignaturesApi },
    { provide: SigningApi, useClass: HttpSigningApi },
    // <<< signatures
    // >>> storage connections
    { provide: StorageApi, useClass: HttpStorageApi },
    // <<< storage connections
    // >>> T-24 downloads
    { provide: RemoteDownloadsApi, useClass: HttpRemoteDownloadsApi },
    // <<< T-24 downloads
    // >>> T-24 upstream
    { provide: UpstreamCatalogueApi, useClass: HttpUpstreamCatalogueApi },
    { provide: DeliveryRulesApi, useClass: HttpDeliveryRulesApi },
    { provide: DeliveryRequestsApi, useClass: HttpDeliveryRequestsApi },
    // <<< T-24 upstream
    // >>> T-26 upstream downloads
    { provide: UpstreamDownloadsApi, useClass: HttpUpstreamDownloadsApi },
    // <<< T-26 upstream downloads
  ];
}
