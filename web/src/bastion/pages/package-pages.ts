import { Component, input } from '@angular/core';
import { PackageDetailView } from '../../app/views/packages/package-detail-view';
import { PackageVersionView } from '../../app/views/packages/package-version-view';

/**
 * A package's page at the Bastion (R-UI-229): the main installation's view of its versions (views/packages/package-detail-view.ts,
 * R-UI-223) without an act, read from the catalogue. Nothing is recorded: the Bastion keeps no "recently opened" and no trail.
 */
@Component({
  selector: 'quader-bastion-package',
  imports: [PackageDetailView],
  template: `
    <quader-package-detail-view [project]="project()" [format]="format()" [name]="name()" [q]="q()" [sort]="sort()" />
  `,
})
export class PackagePage {
  readonly project = input.required<string>();
  readonly format = input.required<string>();
  readonly name = input.required<string>();
  readonly q = input<string | undefined>();
  readonly sort = input<string | undefined>();
}

/**
 * A version's page at the Bastion (R-UI-229): the main installation's view of a version (views/packages/package-version-view.ts) for a
 * reader who is never signed in, so its tab Vulnerabilities lists the report's own open findings (R-UI-224, W-35's B.4a) and no place
 * of `views/acts.ts` is provided: no deprecation, no gate's release, no import log.
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
    />
  `,
})
export class VersionPage {
  readonly project = input.required<string>();
  readonly format = input.required<string>();
  readonly name = input.required<string>();
  readonly version = input<string | undefined>();
  readonly tab = input<string | undefined>();
  readonly doc = input<string | undefined>();
  readonly q = input<string | undefined>();
  readonly severity = input<string | undefined>();
}
