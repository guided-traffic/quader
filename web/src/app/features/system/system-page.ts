import { Component, computed, inject } from '@angular/core';
import { RouterLink, RouterLinkActive, RouterOutlet } from '@angular/router';
import { SessionStore } from '../../core/session-store';
import { PageHeader } from '../../shared/ui/page-header';
import { SystemRefresh } from './system-refresh';

/**
 * The page **System status** (R-UI-164, the owner's decision W-19, Q-UI-650): one header, the tabs **Status**, **Jobs**,
 * **Backups** and **Storage** (R-UI-196, Q-UI-930) under it, each with its own address (`/admin/system`, `/admin/system/jobs`,
 * `/admin/system/backups`, `/admin/system/storage`), and the tab's content below, as the profile's pages are built. The header's
 * **Pause automatic refresh** and **Refresh now** act on the tab that is shown (Q-UI-651). Status, Backups and Storage need
 * `settings:read`; Jobs answers every signed-in caller with the jobs
 * they started, so a caller without `settings:read` who follows a link to the jobs sees them without a strip of one tab
 * (Q-UI-652).
 */
@Component({
  selector: 'quader-system-page',
  imports: [PageHeader, RouterLink, RouterLinkActive, RouterOutlet],
  providers: [SystemRefresh],
  template: `
    <quader-page-header heading="System status" i18n-heading="@@system.heading">
      <button actions class="btn btn-default" type="button" [attr.aria-pressed]="refresh.paused()" (click)="refresh.togglePause()">
        @if (refresh.paused()) {
          <ng-container i18n="@@system.resume">Resume automatic refresh</ng-container>
        } @else {
          <ng-container i18n="@@system.pause">Pause automatic refresh</ng-container>
        }
      </button>
      <button actions class="btn btn-default" type="button" (click)="refresh.now()" i18n="@@system.refresh">Refresh now</button>
    </quader-page-header>
    @if (tabs()) {
      <nav class="tab-nav system-tabs" aria-label="System status pages" i18n-aria-label="@@system.tabs">
        <a class="tab-link" routerLink="/admin/system" routerLinkActive [routerLinkActiveOptions]="exact" ariaCurrentWhenActive="page" i18n="@@system.tab.status"
          >Status</a
        >
        <a class="tab-link" routerLink="/admin/system/jobs" routerLinkActive [routerLinkActiveOptions]="exact" ariaCurrentWhenActive="page" i18n="@@system.tab.jobs"
          >Jobs</a
        >
        <a class="tab-link" routerLink="/admin/system/backups" routerLinkActive [routerLinkActiveOptions]="exact" ariaCurrentWhenActive="page" i18n="@@system.tab.backups"
          >Backups</a
        >
        <a class="tab-link" routerLink="/admin/system/storage" routerLinkActive [routerLinkActiveOptions]="exact" ariaCurrentWhenActive="page" i18n="@@system.tab.storage"
          >Storage</a
        >
      </nav>
    }
    <router-outlet />
  `,
  styles: `
    .system-tabs {
      margin-block-end: var(--quader-space-4);
    }
  `,
})
export class SystemPage {
  protected readonly refresh = inject(SystemRefresh);
  private readonly session = inject(SessionStore);

  /** The tabs that the caller may open; without `settings:read` only Jobs is left, and one tab is no strip. */
  protected readonly tabs = computed(() => this.session.has('settings:read'));
  /** A tab is current on its own path, whatever filters its address carries; Status is not current on Jobs. */
  protected readonly exact = { paths: 'exact', queryParams: 'ignored', matrixParams: 'ignored', fragment: 'ignored' } as const;
}
