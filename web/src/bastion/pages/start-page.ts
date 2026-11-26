import { Component, effect, inject, resource, untracked } from '@angular/core';
import { Router, RouterLink } from '@angular/router';
import { ErrorNotice } from '../../app/shared/error-notice';
import { failureOf, valueOf } from '../../app/shared/paged-resource';
import { Loading } from '../../app/shared/ui/loading';
import { PageHeader } from '../../app/shared/ui/page-header';
import { BastionData } from '../data/http';

/**
 * The start of the Bastion's application (R-UI-229): the projects whose packages it shows. With one project the start is that
 * project's packages, as the main installation starts a caller with one project in it (Q-UI-129).
 */
@Component({
  selector: 'quader-bastion-start',
  imports: [ErrorNotice, Loading, PageHeader, RouterLink],
  template: `
    <quader-page-header heading="Projects" i18n-heading="@@bastion.start.title" />
    @if (failure(); as f) {
      <quader-error-notice [error]="f" (retry)="retry()" />
    } @else if (projects(); as list) {
      @if (list.length === 0) {
        <p i18n="@@bastion.start.empty">No package is published here yet.</p>
      } @else {
        <ul class="plain-list project-list">
          @for (p of list; track p) {
            <li>
              <a [routerLink]="['/projects', p, 'packages']" translate="no">{{ p }}</a>
            </li>
          }
        </ul>
      }
    } @else {
      <quader-loading size="page" i18n="@@bastion.start.loading">Loading the projects.</quader-loading>
    }
  `,
})
export class StartPage {
  private readonly data = inject(BastionData);
  private readonly router = inject(Router);

  private readonly answer = resource({ loader: () => this.data.get<{ items: readonly string[] }>('projects') });
  protected readonly projects = () => valueOf(this.answer)?.items ?? null;
  protected readonly failure = failureOf(this.answer);
  protected readonly retry = (): void => void this.answer.reload();

  constructor() {
    effect(() => {
      const list = this.projects();
      if (list?.length === 1) {
        untracked(() => void this.router.navigate(['/projects', list[0], 'packages'], { replaceUrl: true }));
      }
    });
  }
}
