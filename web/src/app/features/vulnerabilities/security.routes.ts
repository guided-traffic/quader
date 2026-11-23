import { Routes } from '@angular/router';
import { CrumbSpec } from '../../core/breadcrumbs';
import { advisoryMatcher, advisoryTitle, findingTitle } from './vulnerabilities.routes';
import { SECURITY_CONTEXT, VULN_CONTEXT } from './vuln-context';

/**
 * The pages of the side bar's **Security audit** (Q-UI-130, R-VULN-022, R-UI-158, Q-UI-1005), mounted below `/security` in the main
 * area: the security dashboard, the report over every project with the finding and advisory screens it links to; the malware finds;
 * the signatures. The screens are a project's, so a
 * change to one is a change to both; `VULN_CONTEXT` keeps the links between them below `/security` and switches on the project
 * filter and the line that says what the counts cover.
 */
export const SECURITY_ROUTES: Routes = [
  {
    path: '',
    providers: [{ provide: VULN_CONTEXT, useValue: SECURITY_CONTEXT }],
    children: [
      {
        // The malware finds (R-MAL-025): an entry of its own under Security audit, "Security audit / Malware" (Q-UI-1005).
        path: 'malware',
        title: $localize`:@@route.security.malware.title:Malware`,
        loadComponent: () => import('./malware-finds').then((m) => m.MalwareFinds),
      },
      {
        // Invalid signatures and the signatures of blocked signers (R-SIG-038): an entry of its own as well (Q-UI-1005).
        path: 'signatures',
        title: $localize`:@@route.security.signatures.title:Signatures`,
        loadComponent: () => import('./signature-findings').then((m) => m.SignatureFindings),
      },
      {
        // The dashboard of the vulnerability findings, with its findings and advisories under it: "Security audit / Security dashboard".
        path: '',
        data: { crumbs: [{ label: $localize`:@@route.security.title:Security dashboard`, link: true }] satisfies CrumbSpec[] },
        children: [
          {
            path: '',
            pathMatch: 'full',
            title: $localize`:@@route.security.title:Security dashboard`,
            loadComponent: () => import('./vulnerability-report').then((m) => m.VulnerabilityReport),
          },
          {
            path: 'findings/:id',
            title: findingTitle,
            loadComponent: () => import('./vulnerability-finding').then((m) => m.FindingPage),
          },
          {
            matcher: advisoryMatcher,
            title: advisoryTitle,
            data: { crumbParam: 'id' },
            loadComponent: () => import('./vulnerability-advisory').then((m) => m.AdvisoryPage),
          },
        ],
      },
    ],
  },
];
