import { Component, effect, inject, resource, untracked } from '@angular/core';
import { Router, RouterLink } from '@angular/router';
import { ErrorNotice } from '../../app/shared/error-notice';
import { failureOf, valueOf } from '../../app/shared/paged-resource';
import { Loading } from '../../app/shared/ui/loading';
import { PageHeader } from '../../app/shared/ui/page-header';
import { BastionProjects } from '../data/projects';

/**
 * The start of the Bastion's application (R-UI-229): the projects whose packages it shows, each by its display name with its
 * description as the main installation names them (bastion.md R-BAS-128, Q-UI-1470); the address carries the key. With one project the
 * start is that project's packages, as the main installation starts a caller with one project in it (Q-UI-129).
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
          @for (p of list; track p.key) {
            <li>
              <a [routerLink]="['/projects', p.key, 'packages']" translate="no">{{ p.displayName }}</a>
              @if (p.description) {
                <p class="muted description" translate="no">{{ p.description }}</p>
              }
            </li>
          }
        </ul>
      }
    } @else {
      <quader-loading size="page" i18n="@@bastion.start.loading">Loading the projects.</quader-loading>
    }
  `,
  styles: `
    .project-list {
      margin: 0;
      padding: 0;
      list-style: none;
    }
    .project-list li + li {
      margin-block-start: var(--quader-space-3);
    }
    .project-list a {
      overflow-wrap: anywhere;
    }
    .description {
      max-inline-size: 70ch;
      margin: var(--quader-space-1) 0 0;
      overflow-wrap: anywhere;
    }
  `,
})
export class StartPage {
  private readonly store = inject(BastionProjects);
  private readonly router = inject(Router);

  private readonly answer = resource({ loader: () => this.store.list() });
  protected readonly projects = () => valueOf(this.answer) ?? null;
  protected readonly failure = failureOf(this.answer);
  protected readonly retry = (): void => void this.answer.reload();

  constructor() {
    effect(() => {
      const only = this.projects()?.length === 1 ? this.projects()?.[0] : undefined;
      if (only) {
        untracked(() => void this.router.navigate(['/projects', only.key, 'packages'], { replaceUrl: true }));
      }
    });
  }
}
