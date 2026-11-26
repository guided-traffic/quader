import { CdkMenu, CdkMenuItem, CdkMenuTrigger } from '@angular/cdk/menu';
import { Component, computed, inject, input, resource, signal, viewChild } from '@angular/core';
import { firstValueFrom } from 'rxjs';
import { PackageDeprecation, PackageSummary, PackageVersion, PackagesApi } from '../../api/packages-api';
import { ApiError } from '../../api/problem';
import { Breadcrumbs } from '../../core/breadcrumbs';
import { SessionStore } from '../../core/session-store';
import { ErrorNotice } from '../../shared/error-notice';
import { asApiError, valueOf } from '../../shared/paged-resource';
import { QuaderDialog } from '../../shared/ui/dialog';
import { PackageVersionView } from '../../views/packages/package-version-view';
import { DeprecationDialog, DeprecationDialogData } from './deprecation-dialog';
import { providePackageActs } from './package-acts';
import { PackageMemory } from './package-memory';

/**
 * The page of one version (R-UI-111) as the main installation shows it: the view of the version (views/packages/package-version-view.ts,
 * R-UI-223) with the main installation's acts. The page records the package in "recently opened" and names it in the breadcrumb, offers
 * the deprecation in the view's head (R-API-085, Q-UI-1301) and its refusal above the tabs, and provides the acts the view's places draw
 * (package-acts.ts): the signature gate's release, a role's Upload collection, an Ansible collection's Import log.
 */
@Component({
  selector: 'quader-package-version',
  imports: [CdkMenu, CdkMenuItem, CdkMenuTrigger, ErrorNotice, PackageVersionView],
  providers: providePackageActs(),
  template: `
    <quader-package-version-view
      [project]="project()"
      [format]="format()"
      [name]="name()"
      [version]="version()"
      [tab]="tab()"
      [doc]="doc()"
      [q]="q()"
      [severity]="severity()"
      [signedIn]="signedIn()"
      (packageOpened)="opened($event)"
      (versionShown)="detail.set($event)"
    >
      <!-- The version's acts for those who may deploy it (R-API-085, Q-UI-1301): a menu at the end of the head, as a repository's. -->
      <ng-container versionActs>
        @if (mayDeprecate() && detail()) {
          <button class="btn btn-default btn-icon" type="button" data-testid="version-acts" [attr.aria-label]="actsLabel" [cdkMenuTriggerFor]="actsMenu">
            <span class="material-icons" aria-hidden="true">more_vert</span>
          </button>
          <ng-template #actsMenu>
            <div class="menu" cdkMenu>
              @if (deprecated()) {
                <button class="menu-item" cdkMenuItem type="button" (cdkMenuItemTriggered)="openDeprecation()" i18n="@@pkg.acts.change-deprecation">
                  Change deprecation
                </button>
                <button class="menu-item" cdkMenuItem type="button" [disabled]="deprecationBusy()" (cdkMenuItemTriggered)="undoDeprecation()" i18n="@@pkg.acts.undo-deprecation">
                  Undo deprecation
                </button>
              } @else if (deprecatesPackage()) {
                <button class="menu-item" cdkMenuItem type="button" (cdkMenuItemTriggered)="openDeprecation()" i18n="@@pkg.acts.deprecate-collection">
                  Deprecate collection
                </button>
              } @else {
                <button class="menu-item" cdkMenuItem type="button" (cdkMenuItemTriggered)="openDeprecation()" i18n="@@pkg.acts.deprecate-version">
                  Deprecate version
                </button>
              }
            </div>
          </ng-template>
        }
      </ng-container>
      <ng-container versionNotices>
        @if (deprecationFailure(); as f) {
          <quader-error-notice [error]="f" />
        }
      </ng-container>
    </quader-package-version-view>
  `,
})
export class PackageVersionPage {
  private readonly api = inject(PackagesApi);
  private readonly memory = inject(PackageMemory);
  private readonly breadcrumbs = inject(Breadcrumbs);
  private readonly session = inject(SessionStore);
  private readonly dialog = inject(QuaderDialog);

  /** Route parameters: the project, then the matcher's format, name and version. */
  readonly project = input.required<string>();
  readonly format = input.required<string>();
  readonly name = input.required<string>();
  /** The version of the address; absent on a package's own address, which shows its newest version (R-UI-218, Q-UI-1242). */
  readonly version = input<string | undefined>();
  /** Query parameters: the open tab, the open page of the Documentation tab and its search text (R-UI-060), the severity filter. */
  readonly tab = input<string | undefined>();
  readonly doc = input<string | undefined>();
  readonly q = input<string | undefined>();
  readonly severity = input<string | undefined>();

  private readonly view = viewChild.required(PackageVersionView);
  /**
   * The reader has a session: a visitor without one reads the tab Vulnerabilities from the report's own findings, each linking to its
   * advisory, and is offered no link into a screen that needs a session (R-UI-224).
   */
  protected readonly signedIn = computed(() => this.session.authenticated());

  /** Opening the page records the package in "recently opened" (R-UI-007) and names it in the trail. */
  protected opened(s: PackageSummary): void {
    this.memory.addRecent({ format: s.format, name: s.name, ...(s.key && s.key !== s.name ? { key: s.key } : {}) });
    // The trail names the package by its name where the address holds its key (a role, Q-UI-1317).
    this.breadcrumbs.name(this.name(), s.name);
  }

  // ---- Deprecating the version, or an Ansible collection as a whole (R-API-085, Q-UI-1301) ----

  /** The version the view shows; null until it has answered. */
  protected readonly detail = signal<PackageVersion | null>(null);
  /** The version's deprecation, its own or its package's (R-API-085). */
  protected readonly deprecated = computed<PackageDeprecation | null>(() => this.detail()?.deprecation ?? null);
  /** The local repositories that hold the version, for a format whose protocol carries a deprecation (npm, NuGet, Ansible Galaxy). */
  private readonly deprecationHolders = computed(
    () => {
      const d = this.detail();
      if (!d || !['npm', 'nuget', 'ansible'].includes(d.format)) return undefined;
      const keys = d.repositories.filter((r) => r.class === 'local').map((r) => r.key);
      return keys.length > 0 ? { name: d.name, keys } : undefined;
    },
    { equal: (a, b) => a?.name === b?.name && (a?.keys ?? []).join('\n') === (b?.keys ?? []).join('\n') },
  );
  /** Those of them in which the reader may deploy the package: the act stands only for them, the server decides again. */
  protected readonly deprecationRepositories = resource({
    params: () => this.deprecationHolders(),
    loader: async ({ params }) => {
      const allowed = await this.session.check(params.keys.map((repository) => ({ repository, action: 'deploy', package: params.name })));
      return params.keys.filter((_, i) => allowed[i]);
    },
  });
  protected readonly mayDeprecate = computed(() => (valueOf(this.deprecationRepositories) ?? []).length > 0);
  /** Ansible Galaxy deprecates the collection, not the version: the menu's words say which. */
  protected readonly deprecatesPackage = computed(() => this.format() === 'ansible');
  protected readonly actsLabel = $localize`:@@pkg.acts.label:More actions`;
  protected readonly deprecationBusy = signal(false);
  protected readonly deprecationFailure = signal<ApiError | null>(null);

  protected async openDeprecation(): Promise<void> {
    const d = this.detail();
    if (!d) return;
    const data: DeprecationDialogData = {
      format: d.format, name: d.name, version: d.version, repositories: valueOf(this.deprecationRepositories) ?? [], current: this.deprecated(),
    };
    const ref = this.dialog.open<DeprecationDialog, DeprecationDialogData, PackageDeprecation>(DeprecationDialog, { data, width: '36rem' });
    if (await firstValueFrom(ref.closed)) this.view().changed();
  }

  /** **Undo deprecation**: the same act, without a dialog; its refusal is shown above the tabs, whole (R-UI-216). */
  protected async undoDeprecation(): Promise<void> {
    const d = this.detail();
    const repository = (valueOf(this.deprecationRepositories) ?? [])[0];
    if (!d || !repository || this.deprecationBusy()) return;
    this.deprecationBusy.set(true);
    this.deprecationFailure.set(null);
    try {
      await this.api.clearDeprecation(repository, d.name, this.deprecatesPackage() ? undefined : d.version);
      this.view().changed();
    } catch (e) {
      this.deprecationFailure.set(asApiError(e));
    } finally {
      this.deprecationBusy.set(false);
    }
  }
}
