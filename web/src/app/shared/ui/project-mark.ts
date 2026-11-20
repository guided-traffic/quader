import { Component, computed, inject, input } from '@angular/core';
import { RouterLink } from '@angular/router';
import { ProjectContext } from '../../core/project-context';
import { isUpstreamProject } from '../../core/upstream-project';
import { VisibilityIcon, VisibilityLevel, levelName } from './visibility-level';

/** The first letter of a project's display name, or of its key: the letter of the project's mark. */
export function letterOf(displayName: string | null | undefined, key: string): string {
  return ((displayName ?? '').trim().charAt(0) || key.charAt(0)).toUpperCase();
}

/**
 * A project's mark (Q-UI-424): its first letter on a stone of Quader's mark, which turns into the coloured stone under the pointer
 * or the keyboard's focus of its row; the one icon of a project wherever it stands, before its name
 * in every list of projects, in the search's suggestions, in the project switcher and wherever a row names its project. The
 * letter is drawn by the stylesheet (`::before`): as text it would be part of the name of the link around it, a letter the
 * visible name does not start with (axe `label-content-name-mismatch`). Decorative: the name stands beside it.
 *
 * The built-in project `upstream` carries an arrow pointing up where every other project carries its initial (the owner's
 * Q-UI-837): the icon font's `arrow_upward`, drawn the same way, in the letter's colour, on the same stone and with the same change.
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
    '[class.upstream]': 'upstream()',
    '[attr.data-letter]': 'upstream() ? null : letter()',
    'aria-hidden': 'true',
  },
})
export class ProjectMark {
  readonly key = input.required<string>();
  readonly name = input<string | null | undefined>(null);
  readonly size = input<'small' | 'medium'>('small');

  protected readonly letter = computed(() => letterOf(this.name(), this.key()));
  protected readonly upstream = computed(() => isUpstreamProject(this.key()));
}

/**
 * A project's level in a list (R-UI-054): its icon and its name; for the built-in project `upstream` the en dash, heard as "Each
 * registry has its own visibility" (Q-UI-844). That project is stored public, which admits nobody (R-AUTHZ-041): each of its
 * registries has a level of its own, so a level of the project would say something false.
 */
@Component({
  selector: 'quader-project-level',
  imports: [VisibilityIcon],
  template: `@if (builtIn()) {<span aria-hidden="true" [attr.title]="ownLevels">–</span><span class="visually-hidden">{{ ownLevels }}</span>} @else {<quader-visibility-icon [level]="level()" />{{ name() }}}`,
  host: { class: 'quader-project-level' },
})
export class ProjectLevel {
  readonly key = input.required<string>();
  readonly level = input.required<VisibilityLevel>();

  protected readonly builtIn = computed(() => isUpstreamProject(this.key()));
  protected readonly name = computed(() => levelName(this.level()));
  protected readonly ownLevels = $localize`:@@projects.level.built-in:Each registry has its own visibility.`;
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
