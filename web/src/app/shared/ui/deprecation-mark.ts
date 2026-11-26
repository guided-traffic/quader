import { Component, computed, input } from '@angular/core';

/** What a mark needs of a deprecation (R-API-085): its scope and its message; `true` where a list knows only that there is one. */
export interface DeprecationLike {
  readonly scope?: 'version' | 'package';
  readonly message?: string;
}

/**
 * The one look of a deprecated or an unlisted version or package (R-API-085, Q-UI-1300), wherever one is listed or named: the table of a
 * package's versions, the switch in a version's head, a repository's and the product's package lists, the search's hits and
 * suggestions, and the tab Upstream of a remote. **Deprecated** is a warning badge; its title is the publisher's message, plain text.
 * **Unlisted** (NuGet: hidden from search, still restored by its number) is a plain badge of its own word and never a deprecation.
 * Nothing renders for a version that is neither.
 */
@Component({
  selector: 'quader-deprecation-mark',
  template: `
    @if (deprecated()) {
      <span class="badge warn" data-testid="deprecated-mark" [attr.title]="title()">
        <span class="material-icons" aria-hidden="true">warning</span>
        <ng-container i18n="@@mark.deprecated">Deprecated</ng-container>
      </span>
    }
    @if (unlisted()) {
      <span class="badge" data-testid="unlisted-mark" [attr.title]="unlistedTitle">
        <span class="material-icons" aria-hidden="true">visibility_off</span>
        <ng-container i18n="@@mark.unlisted">Unlisted</ng-container>
      </span>
    }
  `,
  styles: `
    :host {
      display: contents;
    }
  `,
})
export class DeprecationMark {
  /** The deprecation, `true` when only its existence is known, nothing for none. */
  readonly deprecation = input<DeprecationLike | boolean | null | undefined>(null);
  /** The version is unlisted (NuGet, R-NUGET-006). */
  readonly unlisted = input<boolean | null | undefined>(false);

  protected readonly deprecated = computed(() => !!this.deprecation());
  /** The message as the badge's title, plain text; none when the publisher gave none. */
  protected readonly title = computed(() => {
    const d = this.deprecation();
    return typeof d === 'object' && d?.message ? d.message : null;
  });
  protected readonly unlistedTitle = $localize`:@@mark.unlisted-title:Hidden from search; still downloaded when a project asks for exactly this version.`;
}
