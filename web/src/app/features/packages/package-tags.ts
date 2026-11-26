import { ChangeDetectionStrategy, Component, computed, input } from '@angular/core';
import { RouterLink } from '@angular/router';

/** The tags a hit shows before it folds the rest into "+N" (R-ANS-057). */
const SHOWN = 5;

/**
 * The tags or keywords of a package in a list, a hit or the tab Upstream (R-ANS-057, Q-ANS-123): each a small pill, in a project a link
 * to the project's package list filtered by it, across projects a link to the search for it; the tab Upstream shows them as text.
 * Five at most, the rest as "+N" with every tag in its title.
 */
@Component({
  selector: 'quader-package-tags',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [RouterLink],
  template: `
    @if (tags().length > 0) {
      <span class="tag-row" [attr.aria-label]="label" role="group">
        @for (t of shown(); track t) {
          @if (linked()) {
            <a class="badge tag" [routerLink]="link()" [queryParams]="params(t)" translate="no">{{ t }}</a>
          } @else {
            <span class="badge tag" translate="no">{{ t }}</span>
          }
        }
        @if (rest() > 0) {
          <span class="badge tag more" [attr.title]="tags().join(', ')" translate="no">+{{ rest() }}</span>
        }
      </span>
    }
  `,
  styles: `
    :host {
      display: block;
    }
    .tag-row {
      display: flex;
      flex-wrap: wrap;
      gap: var(--quader-space-1);
      margin-block-start: var(--quader-space-1);
    }
    .tag {
      font-weight: 400;
      max-inline-size: 16rem;
      overflow: hidden;
      text-overflow: ellipsis;
      white-space: nowrap;
    }
    /* A tag that is a link keeps the badge's own colours: the link colour on the badge's paper failed the contrast in the dark scheme. */
    a.tag {
      color: var(--badge-fg);
      text-decoration: none;
    }
    a.tag:hover,
    a.tag:focus-visible {
      text-decoration: underline;
    }
  `,
})
export class PackageTags {
  readonly tags = input<readonly string[]>([]);
  /** The project whose package list a tag filters; null links to the search across projects; `false` shows text. */
  readonly project = input<string | null | false>(null);

  protected readonly label = $localize`:@@pkg.tags.label:Tags`;
  protected readonly shown = computed(() => this.tags().slice(0, SHOWN));
  protected readonly rest = computed(() => Math.max(0, this.tags().length - SHOWN));
  protected readonly linked = computed(() => this.project() !== false);
  protected readonly link = computed(() => {
    const p = this.project();
    return typeof p === 'string' ? ['/projects', p, 'packages'] : ['/search'];
  });

  protected params(tag: string): Record<string, string> {
    return typeof this.project() === 'string' ? { keyword: tag } : { q: tag, kind: 'package' };
  }
}
