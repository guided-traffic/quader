import { Routes } from '@angular/router';
import { advisoryMatcher } from './vulnerabilities.routes';
import { SECURITY_CONTEXT, VULN_CONTEXT } from './vuln-context';

/**
 * The security dashboard (Q-UI-130, R-VULN-022, R-UI-158): the report over every project, and the finding and advisory screens it
 * links to, mounted below `/security` in the main area, under the side bar's **Security audit**. The screens are a project's, so a
 * change to one is a change to both; `VULN_CONTEXT` keeps the links between them below `/security` and switches on the project
 * filter and the line that says what the counts cover.
 */
export const SECURITY_ROUTES: Routes = [
  {
    path: '',
    providers: [{ provide: VULN_CONTEXT, useValue: SECURITY_CONTEXT }],
    children: [
      {
        path: '',
        pathMatch: 'full',
        title: $localize`:@@route.security.title:Security dashboard`,
        loadComponent: () => import('./vulnerability-report').then((m) => m.VulnerabilityReport),
      },
      {
        // The dashboard's second view (R-MAL-025, Q-UI-871): the blobs the malware scan found or could not check.
        path: 'malware',
        title: $localize`:@@route.security.malware.title:Malware`,
        loadComponent: () => import('./malware-finds').then((m) => m.MalwareFinds),
      },
      {
        // The dashboard's third view (R-SIG-038): invalid signatures and the signatures of blocked signers.
        path: 'signatures',
        title: $localize`:@@route.security.signatures.title:Signatures`,
        loadComponent: () => import('./signature-findings').then((m) => m.SignatureFindings),
      },
      {
        path: 'findings/:id',
        title: $localize`:@@route.vulnerabilities.finding.title:Finding`,
        loadComponent: () => import('./vulnerability-finding').then((m) => m.FindingPage),
      },
      {
        matcher: advisoryMatcher,
        title: $localize`:@@route.vulnerabilities.advisory.title:Advisory`,
        data: { crumbParam: 'id' },
        loadComponent: () => import('./vulnerability-advisory').then((m) => m.AdvisoryPage),
      },
    ],
  },
];
