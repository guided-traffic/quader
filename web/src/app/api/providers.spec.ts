import { provideHttpClient } from '@angular/common/http';
import { provideHttpClientTesting } from '@angular/common/http/testing';
import { TestBed } from '@angular/core/testing';
import { describe, expect, it } from 'vitest';
import { AnsibleApi, HttpAnsibleApi } from './ansible-api';
import { AuditApi, HttpAuditApi } from './audit-api';
import { AuthSettingsApi, HttpAuthSettingsApi } from './auth-settings-api';
import { HttpPolicyApi, HttpRolesApi, PolicyApi, RolesApi } from './authz-api';
import { BackupsApi, HttpBackupsApi } from './backups-api';
import { BastionsApi, HttpBastionsApi } from './bastions-api';
import { CiApi, HttpCiApi } from './ci-api';
import { DebApi, HttpDebApi } from './deb-api';
import { DeliveryRequestsApi, HttpDeliveryRequestsApi } from './delivery-requests-api';
import { DeliveryRulesApi, HttpDeliveryRulesApi } from './delivery-rules-api';
import { DownloadLinksApi, HttpDownloadLinksApi } from './download-links-api';
import { EndpointsApi, HttpEndpointsApi } from './endpoints-api';
import { FilesApi, HttpFilesApi } from './files-api';
import { GuidesApi, HttpGuidesApi } from './guides-api';
import { HttpIdentityApi, IdentityApi } from './identity-api';
import { HttpJobsApi, JobsApi } from './jobs-api';
import { HttpLimitsApi, LimitsApi } from './limits-api';
import { HttpLoginApi, LoginApi } from './login-api';
import { HttpMalwareApi, MalwareApi } from './malware-api';
import { HttpManualApi, ManualApi } from './manual-api';
import { HttpNuGetApi, NuGetApi } from './nuget-api';
import { HttpPackagesApi, PackagesApi } from './packages-api';
import { DraftPolicyExplainer, PolicyExplainer } from './policy-explainer';
import { HttpPreferencesApi, PreferencesApi } from './preferences-api';
import { HttpProjectsApi, ProjectsApi } from './projects-api';
import { HttpRemoteDownloadsApi, RemoteDownloadsApi } from './remote-downloads-api';
import { HttpRepositoriesApi, RepositoriesApi } from './repositories-api';
import { HttpSearchApi, SearchApi } from './search-api';
import { HttpSessionApi, SessionApi } from './session-api';
import { HttpSessionsApi, SessionsApi } from './sessions-api';
import { HttpSignaturesApi, SignaturesApi } from './signatures-api';
import { HttpSigningApi, SigningApi } from './signing-api';
import { HttpStorageApi, StorageApi } from './storage-api';
import { EncryptionApi, HttpEncryptionApi, HttpSystemApi, SystemApi } from './system-api';
import { HttpTechnicalTokensApi, TechnicalTokensApi } from './technical-tokens-api';
import { HttpTokensApi, TokensApi } from './tokens-api';
import { HttpUpstreamApi, UpstreamApi } from './upstream-api';
import { HttpUpstreamCatalogueApi, UpstreamCatalogueApi } from './upstream-catalogue-api';
import { HttpUpstreamDownloadsApi, UpstreamDownloadsApi } from './upstream-downloads-api';
import { HttpVisibilityApi, VisibilityApi } from './visibility-api';
import { HttpVulnerabilitiesApi, VulnerabilitiesApi } from './vulnerabilities-api';

type Service = abstract new (...args: never[]) => unknown;

// Each abstract service of api/ with the implementation its own @Injectable names; providers.ts lists none of them.
const BOUND: readonly (readonly [Service, Service])[] = [
  [AnsibleApi, HttpAnsibleApi],
  [AuditApi, HttpAuditApi],
  [AuthSettingsApi, HttpAuthSettingsApi],
  [BackupsApi, HttpBackupsApi],
  [BastionsApi, HttpBastionsApi],
  [CiApi, HttpCiApi],
  [DebApi, HttpDebApi],
  [DeliveryRequestsApi, HttpDeliveryRequestsApi],
  [DeliveryRulesApi, HttpDeliveryRulesApi],
  [DownloadLinksApi, HttpDownloadLinksApi],
  [EncryptionApi, HttpEncryptionApi],
  [EndpointsApi, HttpEndpointsApi],
  [FilesApi, HttpFilesApi],
  [GuidesApi, HttpGuidesApi],
  [IdentityApi, HttpIdentityApi],
  [JobsApi, HttpJobsApi],
  [LimitsApi, HttpLimitsApi],
  [LoginApi, HttpLoginApi],
  [MalwareApi, HttpMalwareApi],
  [ManualApi, HttpManualApi],
  [NuGetApi, HttpNuGetApi],
  [PackagesApi, HttpPackagesApi],
  [PolicyApi, HttpPolicyApi],
  [PolicyExplainer, DraftPolicyExplainer],
  [PreferencesApi, HttpPreferencesApi],
  [ProjectsApi, HttpProjectsApi],
  [RemoteDownloadsApi, HttpRemoteDownloadsApi],
  [RepositoriesApi, HttpRepositoriesApi],
  [RolesApi, HttpRolesApi],
  [SearchApi, HttpSearchApi],
  [SessionApi, HttpSessionApi],
  [SessionsApi, HttpSessionsApi],
  [SignaturesApi, HttpSignaturesApi],
  [SigningApi, HttpSigningApi],
  [StorageApi, HttpStorageApi],
  [SystemApi, HttpSystemApi],
  [TechnicalTokensApi, HttpTechnicalTokensApi],
  [TokensApi, HttpTokensApi],
  [UpstreamApi, HttpUpstreamApi],
  [UpstreamCatalogueApi, HttpUpstreamCatalogueApi],
  [UpstreamDownloadsApi, HttpUpstreamDownloadsApi],
  [VisibilityApi, HttpVisibilityApi],
  [VulnerabilitiesApi, HttpVulnerabilitiesApi],
];

describe('The API services bound where they are used (R-UI-001)', () => {
  it('R-UI-001 every service of api/ resolves to its HTTP implementation without a provider of the application', () => {
    TestBed.resetTestingModule();
    TestBed.configureTestingModule({ providers: [provideHttpClient(), provideHttpClientTesting()] });
    for (const [service, implementation] of BOUND) {
      expect(TestBed.inject(service), service.name).toBeInstanceOf(implementation);
    }
  });
});
