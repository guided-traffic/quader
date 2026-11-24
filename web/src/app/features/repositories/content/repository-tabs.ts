import { Component, computed, inject, input, resource } from '@angular/core';
import { RouterLink } from '@angular/router';
import { DeliveryRequestsApi } from '../../../api/delivery-requests-api';
import { RemoteDownloadCounts, RemoteDownloadsApi } from '../../../api/remote-downloads-api';
import { SessionStore } from '../../../core/session-store';
import { valueOf } from '../../../shared/paged-resource';

/**
 * The tabs of a repository's page (R-UI-052): its content, for a remote repository **Upstream** and **Downloads** (remote-curation.md
 * R-CUR-018, Q-CUR-1), and **Details**, always the last (Q-UI-1067).
 */
export type RepositoryTab = 'content' | 'upstream' | 'downloads' | 'details';

/** The most open requests the strip counts by itself; more read "100+". */
const REQUESTS_COUNTED = 100;

/**
 * The tab strip of a repository's page (R-UI-052, R-CUR-018, Q-CUR-1, Q-UI-1067): the content, captioned as the list is (**Packages**,
 * **Images**, **Files**), at `…/repositories/{key}`; for a remote repository and a signed-in reader **Upstream** at `…/upstream` and
 * **Downloads** at `…/downloads`; and **Details** at `…/details` where the repository has any, always last. The tabs are links with
 * `aria-current="page"` as the page System status has them (R-UI-164); the page shows the strip only where it holds two tabs or more
 * (Q-UI-652). **Upstream** carries the count of open requests for those who may answer them (`Upstream 3`), **Downloads** the count
 * of failed rows when there are any (`Downloads · 2 failed`): the tab that read them passes its own counts after every answer, the
 * others read them once.
 */
@Component({
  selector: 'quader-repository-tabs',
  imports: [RouterLink],
  template: `
    <nav class="tab-nav repository-tabs" aria-label="Views of the repository" i18n-aria-label="@@repo.tabs">
      <a class="tab-link" [routerLink]="base()" [attr.aria-current]="current() === 'content' ? 'page' : null">{{ contentLabel() }}</a>
      @if (remote()) {
        <a class="tab-link" [routerLink]="upstreamLink()" [attr.aria-current]="current() === 'upstream' ? 'page' : null"
          ><ng-container i18n="@@repo.tab.upstream">Upstream</ng-container
          >@if (openRequests(); as n) {&ngsp;<span class="badge">{{ n }}</span>}</a
        >
        <a class="tab-link" [routerLink]="downloadsLink()" [attr.aria-current]="current() === 'downloads' ? 'page' : null"
          ><ng-container i18n="@@repo.tab.downloads">Downloads</ng-container
          >@if (failed(); as n) {&ngsp;<span class="tab-note" i18n="@@repo.tab.downloads-failed">· {{ n }} failed</span>}</a
        >
      }
      @if (details()) {
        <a class="tab-link" [routerLink]="detailsLink()" [attr.aria-current]="current() === 'details' ? 'page' : null" i18n="@@repo.tab.details"
          >Details</a
        >
      }
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
export class RepositoryTabs {
  private readonly api = inject(RemoteDownloadsApi);
  private readonly requests = inject(DeliveryRequestsApi);
  private readonly session = inject(SessionStore);

  readonly project = input.required<string>();
  readonly key = input.required<string>();
  /** The caption of the content tab: the list's own (Packages, Images, Files). */
  readonly contentLabel = input.required<string>();
  readonly current = input.required<RepositoryTab>();
  /** The tabs **Upstream** and **Downloads**: a remote repository, for a signed-in reader (Q-CUR-150). */
  readonly remote = input(false);
  /** The tab **Details**: the repository has a further address, an upstream or members to show (Q-UI-1067). */
  readonly details = input(false);
  /** The queue's counts as the tab Downloads read them last; `null` makes the strip read them once itself. */
  readonly counts = input<RemoteDownloadCounts | null>(null);
  /**
   * The open requests as the tab Upstream read them last: a number for those who may answer them, `null` for anyone else;
   * `undefined` makes the strip read them once itself.
   */
  readonly requestCount = input<number | null | undefined>(undefined);

  protected readonly base = computed(() => ['/projects', this.project(), 'repositories', this.key()]);
  protected readonly upstreamLink = computed(() => [...this.base(), 'upstream']);
  protected readonly downloadsLink = computed(() => [...this.base(), 'downloads']);
  protected readonly detailsLink = computed(() => [...this.base(), 'details']);

  /** One row of the queue answers the counts; a failure of the read leaves the label without them. */
  private readonly read = resource({
    params: () => (this.remote() && this.counts() === null ? `${this.project()}/${this.key()}` : null),
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

  /**
   * The open requests, read once by the strip for a holder of `manage` or the administrator, who sees every entry (R-CUR-005); anyone
   * else's list holds only their own entries, which the count does not mean.
   */
  private readonly readRequests = resource({
    params: () => (this.remote() && this.requestCount() === undefined ? `${this.project()}/${this.key()}` : null),
    loader: async ({ params }) => {
      if (!params || this.session.state() !== 'authenticated') return null;
      try {
        const [manage] = await this.session.check([{ repository: params, action: 'manage' }]);
        if (!manage && !this.session.hasRole('admin')) return null;
        const page = await this.requests.list(params, { state: 'open' }, null, REQUESTS_COUNTED);
        return { n: page.items.length, more: page.nextCursor !== null };
      } catch {
        return null;
      }
    },
  });

  protected readonly openRequests = computed<string | null>(() => {
    const given = this.requestCount();
    if (given !== undefined) return given && given > 0 ? String(given) : null;
    const r = valueOf(this.readRequests);
    if (!r || r.n === 0) return null;
    return r.more ? `${r.n}+` : String(r.n);
  });
}
