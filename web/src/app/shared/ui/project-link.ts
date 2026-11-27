import { Component, computed, inject, input } from '@angular/core';
import { RouterLink } from '@angular/router';
import { ProjectContext } from '../../core/project-context';
import { ProjectMark } from './project-mark';

/**
 * The project of a row (Q-UI-424): its mark and its display name, linked to the project; the key where the caller does not see
 * the project's record. A long name breaks inside itself beside the mark (Q-UI-425). A module of its own: it reads the main
 * installation's projects of the session (`ProjectContext`), which the mark beside it does not, so the Bastion's application, which
 * draws the mark on its start (R-UI-273), holds no session (R-UI-228).
 */
@Component({
  selector: 'quader-project-link',
  imports: [ProjectMark, RouterLink],
  template: `<span class="icon-name"
    ><quader-project-mark [key]="key()" [name]="shown()" /><a [routerLink]="['/projects', key()]" translate="no" [attr.title]="title()">{{
      shown()
    }}</a></span
  >`,
  host: { class: 'quader-project-link' },
})
export class ProjectLink {
  private readonly context = inject(ProjectContext);

  readonly key = input.required<string>();

  /** The display name of the project the caller sees, else its key. */
  protected readonly shown = computed(() => this.context.projects().find((p) => p.key === this.key())?.displayName || this.key());
  /** The key, where the name shown is not the key. */
  protected readonly title = computed(() => (this.shown() === this.key() ? null : this.key()));
}
