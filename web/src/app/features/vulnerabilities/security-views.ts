import { ChangeDetectionStrategy, Component } from '@angular/core';
import { RouterLink, RouterLinkActive } from '@angular/router';

/**
 * The three views of the security dashboard (R-UI-158, R-MAL-025, Q-UI-871, R-SIG-038): **Vulnerabilities** (`/security`),
 * **Malware** (`/security/malware`) and **Signatures** (`/security/signatures`), links under the page's title, each its own address
 * with its own filters. Every view puts the strip
 * directly under its header; the side bar keeps its one entry.
 */
@Component({
  selector: 'quader-security-views',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [RouterLink, RouterLinkActive],
  template: `
    <nav class="tab-nav security-views" aria-label="Security dashboard views" i18n-aria-label="@@security.views">
      <a class="tab-link" routerLink="/security" routerLinkActive [routerLinkActiveOptions]="exact" ariaCurrentWhenActive="page" i18n="@@security.view.vulnerabilities"
        >Vulnerabilities</a
      >
      <a class="tab-link" routerLink="/security/malware" routerLinkActive [routerLinkActiveOptions]="exact" ariaCurrentWhenActive="page" i18n="@@security.view.malware"
        >Malware</a
      >
      <a class="tab-link" routerLink="/security/signatures" routerLinkActive [routerLinkActiveOptions]="exact" ariaCurrentWhenActive="page" i18n="@@security.view.signatures"
        >Signatures</a
      >
    </nav>
  `,
  styles: `
    .security-views {
      margin-block-end: var(--quader-space-4);
    }
  `,
})
export class SecurityViews {
  /** A view is current on its own path, whatever filters its address carries. */
  protected readonly exact = { paths: 'exact', queryParams: 'ignored', matrixParams: 'ignored', fragment: 'ignored' } as const;
}
