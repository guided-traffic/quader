import { ChangeDetectionStrategy, Component, computed, inject, viewChild } from '@angular/core';
import { RouterLink, RouterLinkActive, RouterOutlet } from '@angular/router';
import { LimitSettings } from '../../api/limits-api';
import { Versioned } from '../../api/repositories-api';
import { SessionStore } from '../../core/session-store';
import { PageHeader } from '../../shared/ui/page-header';
import { TabStrip } from '../../shared/ui/tab-strip';
import { LimitModeLine } from './limit-mode';
import { LimitsSettingsState } from './limits-state';

/**
 * **Download limits** of the admin area (R-UI-177, Q-UI-730): the header, the mode with its switch (R-UI-182) and the tab strip
 * of links **Rules**, **Profiles**, **Projects** and **Blocks**, whose pages are the outlet. A holder of `limits:read` without
 * `limits:write` sees every tab read-only.
 */
@Component({
  selector: 'quader-limits-frame',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [LimitModeLine, PageHeader, RouterLink, RouterLinkActive, RouterOutlet, TabStrip],
  providers: [LimitsSettingsState],
  template: `
    <quader-page-header
      heading="Download limits"
      i18n-heading="@@limits.heading"
      description="How much each caller may download, and how fast."
      i18n-description="@@limits.description"
    />
    <quader-limit-mode [canWrite]="canWrite()" (loaded)="state.settings.set($event)" />
    <nav quaderTabStrip class="tab-nav limits-nav" i18n-aria-label="@@limits.nav" aria-label="Download limits">
      <a class="tab-link" routerLink="/admin/limits" routerLinkActive [routerLinkActiveOptions]="{ exact: true }" ariaCurrentWhenActive="page" i18n="@@limits.tab.rules">Rules</a>
      <a class="tab-link" routerLink="/admin/limits/profiles" routerLinkActive ariaCurrentWhenActive="page" i18n="@@limits.tab.profiles">Profiles</a>
      <a class="tab-link" routerLink="/admin/limits/projects" routerLinkActive ariaCurrentWhenActive="page" i18n="@@limits.tab.projects">Projects</a>
      <a class="tab-link" routerLink="/admin/limits/blocks" routerLinkActive ariaCurrentWhenActive="page" i18n="@@limits.tab.blocks">Blocks</a>
    </nav>
    <router-outlet />
  `,
  styles: `
    .limits-nav {
      margin-block-end: var(--quader-space-4);
    }
  `,
})
export class LimitsFrame {
  private readonly session = inject(SessionStore);
  protected readonly state = inject(LimitsSettingsState);
  protected readonly canWrite = computed(() => this.session.has('limits:write'));
  private readonly mode = viewChild(LimitModeLine);

  constructor() {
    // A tab that saved the settings (the block time) hands them on, so that the switch carries the new ETag.
    this.state.saved = (s: Versioned<LimitSettings>) => this.mode()?.take(s);
  }
}

