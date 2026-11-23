import { Component, computed, inject, input, resource } from '@angular/core';
import { RouterLink } from '@angular/router';
import { RemoteDownloadCounts, RemoteDownloadsApi } from '../../../api/remote-downloads-api';
import { valueOf } from '../../../shared/paged-resource';

/** The tabs of a remote repository's page (remote-curation.md R-CUR-018, Q-CUR-1): its content and **Downloads**. */
export type RemoteTab = 'content' | 'downloads';

/**
 * The tab strip of a remote repository's page (R-CUR-018, Q-CUR-1): the content, captioned as the list is (**Packages**,
 * **Images**, **Files**), at `…/repositories/{key}`, and **Downloads** at `…/repositories/{key}/downloads`, links with
 * `aria-current="page"` as the page System status has them (R-UI-164). **Downloads** carries the count of failed rows when there
 * are any (`Downloads · 2 failed`): the tab Downloads passes its own counts after every answer, the content tab reads them once.
 * The tab **Upstream** joins with the catalogue's screens (R5b, Q-CUR-130).
 */
@Component({
  selector: 'quader-remote-tabs',
  imports: [RouterLink],
  template: `
    <nav class="tab-nav remote-tabs" aria-label="Views of the repository" i18n-aria-label="@@repo.tabs">
      <a class="tab-link" [routerLink]="base()" [attr.aria-current]="current() === 'content' ? 'page' : null">{{ contentLabel() }}</a>
      <a class="tab-link" [routerLink]="downloadsLink()" [attr.aria-current]="current() === 'downloads' ? 'page' : null"
        ><ng-container i18n="@@repo.tab.downloads">Downloads</ng-container
        >@if (failed(); as n) {&ngsp;<span class="tab-note" i18n="@@repo.tab.downloads-failed">· {{ n }} failed</span>}</a
      >
    </nav>
  `,
  styles: `
    :host {
      display: block;
      margin-block-end: var(--quader-space-4);
    }
    .tab-note {
      font-weight: 400;
    }
  `,
})
export class RemoteTabs {
  private readonly api = inject(RemoteDownloadsApi);

  readonly project = input.required<string>();
  readonly key = input.required<string>();
  /** The caption of the content tab: the list's own (Packages, Images, Files). */
  readonly contentLabel = input.required<string>();
  readonly current = input.required<RemoteTab>();
  /** The queue's counts as the tab Downloads read them last; `null` makes the strip read them once itself. */
  readonly counts = input<RemoteDownloadCounts | null>(null);

  protected readonly base = computed(() => ['/projects', this.project(), 'repositories', this.key()]);
  protected readonly downloadsLink = computed(() => [...this.base(), 'downloads']);

  /** One row of the queue answers the counts; a failure of the read leaves the label without them. */
  private readonly read = resource({
    params: () => (this.counts() === null ? `${this.project()}/${this.key()}` : null),
    loader: async ({ params }) => {
      if (!params) return null;
      try {
        return (await this.api.list(params, {}, null, 1)).counts;
      } catch {
        return null;
      }
    },
  });

  protected readonly failed = computed(() => {
    const c = this.counts() ?? valueOf(this.read) ?? null;
    return c && c.failed > 0 ? c.failed : null;
  });
}
