import { CdkMenu, CdkMenuItem, CdkMenuTrigger } from '@angular/cdk/menu';
import { DecimalPipe } from '@angular/common';
import { Component, computed, input } from '@angular/core';
import { RouterLink } from '@angular/router';
import { PackageVersionSummary } from '../../api/packages-api';
import { DeprecationMark } from '../../shared/ui/deprecation-mark';
import { Loading } from '../../shared/ui/loading';
import { versionCommands, versionsCommands } from './package-links';

/**
 * The switch between a package's versions in the head of a version's page (R-UI-218, Q-UI-1243), for a format whose packages lead
 * with their documentation (Ansible Galaxy, npm, NuGet): a button that names the version shown and opens a menu of the package's
 * newest versions (`versions`, ten by the format's order, the page reads them), the one shown marked as the current page and the
 * package's newest (`latest`, the highest that is no pre-release) marked **Latest**, a pre-release marked as one; under them a link to
 * the table of every version (**All 30 versions**, `…/{name}/-/versions`), so that the list is bounded and the whole is one step away.
 * A version switched to opens on the tab of the address (`tab`), on the same page of a collection's documentation (`doc`) and at the
 * same anchor (the address's fragment, an option's `#parameter-…`): a reader of the documentation reads the other version's.
 */
@Component({
  selector: 'quader-version-switch',
  imports: [CdkMenu, CdkMenuItem, CdkMenuTrigger, DecimalPipe, DeprecationMark, Loading, RouterLink],
  template: `
    <button
      type="button"
      class="btn btn-default switch-trigger"
      [cdkMenuTriggerFor]="menu"
      [attr.aria-label]="triggerLabel()"
      [attr.title]="shown()"
    >
      <span class="material-icons" aria-hidden="true">history</span>
      <span class="switch-version" translate="no">{{ shown() }}</span>
      <span class="material-icons chevron" aria-hidden="true">keyboard_arrow_down</span>
    </button>
    <ng-template #menu>
      <div class="menu version-menu" cdkMenu>
        <p class="menu-heading" i18n="@@pkg.switch.heading">Versions</p>
        @if (loading()) {
          <!-- The ten newest are read with the page; while they are, the menu says so (R-UI-217). -->
          <quader-loading class="switch-loading" i18n="@@pkg.switch.loading">Loading the versions.</quader-loading>
        }
        @for (v of listed(); track v.version) {
          <a
            class="menu-item"
            cdkMenuItem
            [routerLink]="link(v.version)"
            [queryParams]="kept()"
            [preserveFragment]="true"
            [attr.aria-current]="v.version === shown() ? 'page' : null"
          >
            <span class="material-icons check" aria-hidden="true">{{ v.version === shown() ? 'check' : '' }}</span>
            <span class="item-version" translate="no">{{ v.version }}</span>
            @if (v.version === latest()) {
              <span class="badge" i18n="@@pkg.switch.latest">Latest</span>
            } @else if (v.prerelease) {
              <span class="badge muted" i18n="@@pkg.badge.prerelease">Pre-release</span>
            }
            <!-- A deprecated or unlisted version says so here as in every list (R-API-085, Q-UI-1300). -->
            <quader-deprecation-mark [deprecation]="v.deprecation" [unlisted]="v.unlisted" />
          </a>
        }
        <div class="menu-divider" role="separator"></div>
        <a class="menu-item" cdkMenuItem [routerLink]="allLink()" i18n="@@pkg.switch.all"
          >{count(), plural, =1 {All versions} other {All {{ count() | number }} versions}}</a
        >
      </div>
    </ng-template>
  `,
  styles: `
    :host {
      display: inline-block;
      min-inline-size: 0;
      max-inline-size: 100%;
    }
    .switch-trigger {
      gap: var(--quader-space-1);
      max-inline-size: 100%;
      padding-inline-end: var(--quader-space-1);
    }
    .switch-trigger .material-icons {
      flex: none;
      font-size: 1rem;
    }
    /* A long version (a pre-release with a build mark) is cut at the button's end; its title names it whole. */
    .switch-version {
      min-inline-size: 0;
      overflow: hidden;
      text-overflow: ellipsis;
      white-space: nowrap;
    }
    .version-menu {
      max-inline-size: min(24rem, calc(100vw - 2rem));
    }
    .switch-loading {
      padding: var(--quader-space-1) var(--quader-space-3);
    }
    .check {
      flex: none;
      inline-size: 1rem;
      font-size: 1rem;
    }
    .item-version {
      min-inline-size: 0;
      overflow-wrap: anywhere;
    }
    .badge.muted {
      color: var(--quader-text-muted);
    }
  `,
})
export class VersionSwitch {
  readonly project = input.required<string>();
  readonly format = input.required<string>();
  readonly name = input.required<string>();
  /** The version the page shows. */
  readonly shown = input.required<string>();
  /** The package's newest version, the highest that is no pre-release (R-API-029). */
  readonly latest = input.required<string>();
  /** How many versions the package has: the link to all of them names it. */
  readonly count = input.required<number>();
  /** The newest versions by the format's order; null while they are read (the menu then holds the link to all of them). */
  readonly versions = input<readonly PackageVersionSummary[] | null>(null);
  /** The newest versions are being read (the first read; a live read again shows nothing, Q-UI-1238). */
  readonly loading = input(false);
  /** The tab of the address, kept for the version switched to. */
  readonly tab = input<string | null>(null);
  /** The page of a collection's documentation in the address (`doc`), kept for the version switched to. */
  readonly doc = input<string | null>(null);

  /** The query the version switched to opens with: the tab and the documentation's page of the address, where it has them. */
  protected readonly kept = computed(() => ({
    ...(this.tab() ? { tab: this.tab() } : {}),
    ...(this.doc() ? { doc: this.doc() } : {}),
  }));

  protected readonly listed = computed(() => this.versions() ?? []);
  protected readonly triggerLabel = computed(() => $localize`:@@pkg.switch.label:Version ${this.shown()}:version:, switch the version`);

  protected link(version: string): string[] {
    return versionCommands(this.project(), this.format(), this.name(), version);
  }

  protected allLink(): string[] {
    return versionsCommands(this.project(), this.format(), this.name());
  }
}
