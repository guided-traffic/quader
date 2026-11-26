import { Component, computed, inject, input } from '@angular/core';
import { PackageDetailView } from '../../app/views/packages/package-detail-view';
import { PackageVersionView } from '../../app/views/packages/package-version-view';
import { BastionStateStore } from '../data/state';

/**
 * A package's page at the Bastion (R-UI-229): the main installation's view of its versions (views/packages/package-detail-view.ts,
 * R-UI-223) without an act, read from the catalogue. Nothing is recorded: the Bastion keeps no "recently opened" and no trail. Its
 * namespace is text: the Bastion has no namespace page (Q-UI-1443).
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
}

/**
 * A version's page at the Bastion (R-UI-229): the main installation's view of a version (views/packages/package-version-view.ts) for a
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
}
