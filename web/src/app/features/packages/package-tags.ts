import { ChangeDetectionStrategy, Component, computed, input } from '@angular/core';
import { RouterLink } from '@angular/router';

/** The tags a hit shows before it folds the rest into "+N" (R-ANS-057). */
const SHOWN = 5;

/**
 * The tags or keywords of a package or a version, one chip everywhere (R-ANS-057, Q-ANS-123, R-UI-221, Q-UI-1312): in a list, a hit, the
 * tab Upstream and a version's Details. Each a small pill, in a project a link to the project's package list filtered by it (and by the
 * format, where it is given: a keyword means something in its format), across projects a link to the search for it; the tab Upstream
 * shows them as text. A list shows five, the rest as "+N" with every tag in its title; a version's Details shows every one.
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
    /* A tag that is a link keeps the badge's own colours: the link colour on the badge's paper failed the contrast in the dark scheme.
       Inside the row, so the page's link rule (.content a, a version's Details) does not draw it as a link of running text. */
    .tag-row a.tag {
      color: var(--badge-fg);
      text-decoration: none;
    }
    .tag-row a.tag:hover,
    .tag-row a.tag:focus-visible {
      text-decoration: underline;
    }
  `,
})
export class PackageTags {
  readonly tags = input<readonly string[]>([]);
  /** The project whose package list a tag filters; null links to the search across projects; `false` shows text. */
  readonly project = input<string | null | false>(null);
  /** The format whose packages a tag filters, part of the address (`?format=npm&keyword=react`); null filters every format. */
  readonly format = input<string | null>(null);
  /** How many are shown before "+N"; null shows every one (a version's Details, R-UI-221). */
  readonly limit = input<number | null>(SHOWN);

  protected readonly label = $localize`:@@pkg.tags.label:Tags`;
  protected readonly shown = computed(() => this.tags().slice(0, this.limit() ?? undefined));
  protected readonly rest = computed(() => Math.max(0, this.tags().length - (this.limit() ?? this.tags().length)));
  protected readonly linked = computed(() => this.project() !== false);
  protected readonly link = computed(() => {
    const p = this.project();
    return typeof p === 'string' ? ['/projects', p, 'packages'] : ['/search'];
  });

  protected params(tag: string): Record<string, string> {
    const format = this.format();
    if (typeof this.project() !== 'string') return { q: tag, kind: 'package' };
    return format ? { format, keyword: tag } : { keyword: tag };
  }
}
