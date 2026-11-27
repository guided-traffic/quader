import { Component, Signal, computed, inject, input, resource } from '@angular/core';
import { packageCommands } from '../../app/features/packages/package-links';
import { valueOf } from '../../app/shared/paged-resource';
import { leadsWithDocumentation } from '../../app/views/repositories/content-shape';
import { PackageDetailView } from '../../app/views/packages/package-detail-view';
import { PackageVersionView } from '../../app/views/packages/package-version-view';
import { BastionProjects } from '../data/projects';
import { BastionStateStore } from '../data/state';
import { BastionTrail, TrailStep, packageTrail } from '../data/trail';

/** The display name of a page's project, its key until it is read (R-BAS-128). */
function projectName(project: () => string): Signal<string> {
  const projects = inject(BastionProjects);
  const named = resource({ params: () => project(), loader: ({ params }) => projects.named(params) });
  return computed(() => valueOf(named)?.displayName ?? project());
}

/**
 * Binds the way to a package's page (R-UI-273, W-40's D20): Projects › the project › Packages › the package, then the version or the
 * table of versions below it, the crawler text's steps (seo.go). `below` is the last step under the package; without it the package is
 * the page's own step.
 */
function bindPackageTrail(
  project: () => string,
  format: () => string,
  name: () => string,
  below: () => string | null,
): void {
  const displayName = projectName(project);
  inject(BastionTrail).bind((): TrailStep[] => {
    const last = below();
    return [
      ...packageTrail(project(), displayName()),
      { label: name(), commands: last === null ? null : packageCommands(project(), format(), name()), literal: true },
      ...(last === null ? [] : [{ label: last, commands: null, literal: true }]),
    ];
  });
}

/**
 * A package's page at the Bastion (R-UI-229): the main installation's view of its versions (views/packages/package-detail-view.ts,
 * R-UI-223) without an act, read from the catalogue. Nothing is recorded: the Bastion keeps no "recently opened". The way above it leads
 * to its project's packages (R-UI-273); a format whose packages lead with their documentation reaches this table at `…/-/versions`, the
 * last step under the package. Its namespace is text: the Bastion has no namespace page (Q-UI-1443).
 */
@Component({
  selector: 'quader-bastion-package',
  imports: [PackageDetailView],
  template: `
    <quader-package-detail-view
      [project]="project()"
      [format]="format()"
      [name]="name()"
      [q]="q()"
      [sort]="sort()"
      [namespaceLinks]="false"
      [downloads]="false"
    />
  `,
})
export class PackagePage {
  readonly project = input.required<string>();
  readonly format = input.required<string>();
  readonly name = input.required<string>();
  readonly q = input<string | undefined>();
  readonly sort = input<string | undefined>();

  constructor() {
    bindPackageTrail(
      () => this.project(),
      () => this.format(),
      () => this.name(),
      () => (leadsWithDocumentation(this.format()) ? $localize`:@@bastion.title.versions:Versions of ${this.name()}:name:` : null),
    );
  }
}

/**
 * A version's page at the Bastion (R-UI-229), below its package on the way (R-UI-273): the main installation's view of a version
 * (views/packages/package-version-view.ts) for a
 * reader who is never signed in, so its tab Vulnerabilities lists the report's own open findings (R-UI-224, W-35's B.4a) and no place
 * of `views/acts.ts` is provided: no deprecation, no gate's release, no import log. Its namespace is text (Q-UI-1443), and it offers no
 * section Used by: the catalogue holds no package's dependents (Q-UI-1446). Its documentation (the README, an Ansible collection's
 * documentation and contents, a Debian package's changelog) is shown while the Bastion's switch docs is on, as its state says, and is
 * gone at the state's next read when it is switched off (R-UI-232, bastion.md R-BAS-126).
 */
@Component({
  selector: 'quader-bastion-version',
  imports: [PackageVersionView],
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
      [signedIn]="false"
      [namespaceLinks]="false"
      [dependents]="false"
      [downloads]="false"
      [advisoryLinks]="false"
      [documentation]="documentation()"
    />
  `,
})
export class VersionPage {
  private readonly state = inject(BastionStateStore);
  protected readonly documentation = computed(() => this.state.state()?.docs ?? false);
  readonly project = input.required<string>();
  readonly format = input.required<string>();
  readonly name = input.required<string>();
  readonly version = input<string | undefined>();
  readonly tab = input<string | undefined>();
  readonly doc = input<string | undefined>();
  readonly q = input<string | undefined>();
  readonly severity = input<string | undefined>();

  constructor() {
    // A package that leads with its documentation opens its newest version at its own address: the package is then the page's step.
    bindPackageTrail(
      () => this.project(),
      () => this.format(),
      () => this.name(),
      () => this.version() ?? null,
    );
  }
}
