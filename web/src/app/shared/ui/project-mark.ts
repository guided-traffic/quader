import { Component, computed, inject, input } from '@angular/core';
import { RouterLink } from '@angular/router';
import { ProjectContext } from '../../core/project-context';

/** The first letter of a project's display name, or of its key: the letter of the project's mark. */
export function letterOf(displayName: string | null | undefined, key: string): string {
  return ((displayName ?? '').trim().charAt(0) || key.charAt(0)).toUpperCase();
}

/**
 * A project's mark (Q-UI-424): its first letter on the accent fill, the one icon of a project wherever it stands, before its name
 * in every list of projects, in the search's suggestions, in the project switcher and wherever a row names its project. The
 * letter is drawn by the stylesheet (`::before`): as text it would be part of the name of the link around it, a letter the
 * visible name does not start with (axe `label-content-name-mismatch`). Decorative: the name stands beside it.
 *
 * `size`: `small`, 20 px, beside a name of one line (a row's project, a suggestion); `medium`, 32 px, beside a name with a second
 * line (the first column of a list of projects, the switcher).
 */
@Component({
  selector: 'quader-project-mark',
  template: '',
  host: {
    class: 'quader-project-mark',
    '[class.medium]': "size() === 'medium'",
    '[attr.data-letter]': 'letter()',
    'aria-hidden': 'true',
  },
})
export class ProjectMark {
  readonly key = input.required<string>();
  readonly name = input<string | null | undefined>(null);
  readonly size = input<'small' | 'medium'>('small');

  protected readonly letter = computed(() => letterOf(this.name(), this.key()));
}

/**
 * The project of a row (Q-UI-424): its mark and its display name, linked to the project; the key where the caller does not see
 * the project's record. A long name breaks inside itself beside the mark (Q-UI-425).
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
