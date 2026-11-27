import { Component, computed, input } from '@angular/core';
import { Schema } from '../../api/types';
import { Tooltip } from './tooltip';

/*
 * A visibility level's name and icon, apart from the level controls of visibility-level.ts: the top bar's project switcher shows
 * the icon, and the controls' sentences load with the pages that choose a level, not with the first page.
 */

/** A visibility level (R-AUTHZ-033), from the most restrictive. */
export type VisibilityLevel = Schema<'VisibilityLevel'>;

/** The icon of each level, the ones the owner's GitLab uses: a lock, a shield, a globe (Q-UI-230). */
export const VISIBILITY_ICONS: Readonly<Record<VisibilityLevel, string>> = { private: 'lock', internal: 'shield', public: 'public' };

/** The name of a level as the UI writes it. */
export function levelName(level: VisibilityLevel): string {
  switch (level) {
    case 'private':
      return $localize`:@@visibility.private:Private`;
    case 'internal':
      return $localize`:@@visibility.internal:Internal`;
    case 'public':
      return $localize`:@@visibility.public:Public`;
  }
}

/**
 * The level's icon before the name of a project or a repository (Q-UI-230): the level's name is its accessible label and its
 * tooltip. Rendered inline, so it sits in front of the name in a list cell or a header.
 */
@Component({
  selector: 'quader-visibility-icon',
  imports: [Tooltip],
  template: `<span class="material-icons inline" role="img" [attr.aria-label]="name()" [quaderTooltip]="name()">{{ icon() }}</span>`,
})
export class VisibilityIcon {
  readonly level = input.required<VisibilityLevel>();
  protected readonly name = computed(() => levelName(this.level()));
  protected readonly icon = computed(() => VISIBILITY_ICONS[this.level()]);
}
