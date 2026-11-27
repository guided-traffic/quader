import { Component, computed, inject, input, resource } from '@angular/core';
import { RouterLink } from '@angular/router';
import { isUpstreamProject, projectDescription } from '../../app/core/upstream-project';
import { valueOf } from '../../app/shared/paged-resource';
import { PageHeader } from '../../app/shared/ui/page-header';
import { TabStrip } from '../../app/shared/ui/tab-strip';
import { BastionProjects } from '../data/projects';
import { BastionStateStore } from '../data/state';
import { BastionTrail, projectTrail } from '../data/trail';

/** A project's view at the Bastion: its repositories or its packages (R-UI-273). */
export type ProjectView = 'repositories' | 'packages';

/**
 * The head of a project's page at the Bastion (R-UI-273, W-40's D2): its display name and description as the main installation names
 * them (bastion.md R-BAS-128; the built-in project's description is the UI's, R-UI-272), and under them the tabs **Repositories** |
 * **Packages**, two links with `aria-current`, the pattern of a repository's tabs: the Bastion has no side bar, which is where the main
 * installation offers the two (R-UI-227). While the repositories are hidden the project has its packages alone and no strip (R-UI-274).
 * It binds the way to the page: Projects › the project.
 */
@Component({
  selector: 'quader-bastion-project-head',
  imports: [PageHeader, RouterLink, TabStrip],
  template: `
    <quader-page-header [heading]="displayName()" [name]="true" [description]="description()" [descriptionAsWritten]="!builtIn()" />
    @if (shown()) {
      <nav quaderTabStrip class="tab-nav project-tabs" i18n-aria-label="@@bastion.project.tabs" aria-label="Views of the project">
        <a
          class="tab-link"
          [routerLink]="['/projects', project(), 'repositories']"
          [attr.aria-current]="current() === 'repositories' ? 'page' : null"
          i18n="@@route.repositories.title"
          >Repositories</a
        >
        <a
          class="tab-link"
          [routerLink]="['/projects', project(), 'packages']"
          [attr.aria-current]="current() === 'packages' ? 'page' : null"
          i18n="@@nav.packages"
          >Packages</a
        >
      </nav>
    }
  `,
  styles: `
    :host {
      display: block;
    }
    .project-tabs {
      margin-block-end: var(--quader-space-4);
    }
  `,
})
export class BastionProjectHead {
  private readonly projects = inject(BastionProjects);
  private readonly state = inject(BastionStateStore);

  readonly project = input.required<string>();
  readonly current = input.required<ProjectView>();

  private readonly named = resource({ params: () => this.project(), loader: ({ params }) => this.projects.named(params) });
  /** The display name once it is read; until then the key of the address. */
  protected readonly displayName = computed(() => valueOf(this.named)?.displayName ?? this.project());
  protected readonly description = computed(() => projectDescription(this.project(), valueOf(this.named)?.description));
  protected readonly builtIn = computed(() => isUpstreamProject(this.project()));
  protected readonly shown = this.state.repositoriesShown;

  constructor() {
    inject(BastionTrail).bind(() => projectTrail(this.project(), this.displayName(), true));
  }

  /** Reads the project's name and description again (R-UI-213): the page's live refresh calls it. */
  reload(): void {
    this.named.reload();
  }
}
