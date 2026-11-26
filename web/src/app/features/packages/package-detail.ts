import { Component, inject, input } from '@angular/core';
import { PackageSummary } from '../../api/packages-api';
import { Breadcrumbs } from '../../core/breadcrumbs';
import { PackageDetailView } from '../../views/packages/package-detail-view';
import { PackageMemory } from './package-memory';

/**
 * A package's page as the main installation shows it (R-UI-007, R-UI-110): the view of its versions
 * (views/packages/package-detail-view.ts, R-UI-223), which the page records in "recently opened" and names in the breadcrumb.
 */
@Component({
  selector: 'quader-package-detail',
  imports: [PackageDetailView],
  template: `
    <quader-package-detail-view
      [project]="project()"
      [format]="format()"
      [name]="name()"
      [q]="q()"
      [sort]="sort()"
      (packageOpened)="opened($event)"
    />
  `,
})
export class PackageDetail {
  private readonly memory = inject(PackageMemory);
  private readonly breadcrumbs = inject(Breadcrumbs);

  /** Route parameters: the project of `/projects/:project/packages`, then the matcher's, bound by component input binding. */
  readonly project = input.required<string>();
  readonly format = input.required<string>();
  readonly name = input.required<string>();
  /** Query parameters: the text the versions are narrowed to and their sort. */
  readonly q = input<string | undefined>();
  readonly sort = input<string | undefined>();

  /** Opening the page records it in "recently opened" (R-UI-007) and names the package in the trail. */
  protected opened(s: PackageSummary): void {
    this.memory.addRecent({ format: s.format, name: s.name, ...(s.key && s.key !== s.name ? { key: s.key } : {}) });
    // The trail names the package by its name where the address holds its key (a role, Q-UI-1317).
    this.breadcrumbs.name(this.name(), s.name);
  }
}
