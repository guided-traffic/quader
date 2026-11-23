import { Component, computed, effect, inject, input, output, resource, signal, untracked } from '@angular/core';
import { toSignal } from '@angular/core/rxjs-interop';
import { ActivatedRoute, Router, RouterLink } from '@angular/router';
import { map } from 'rxjs';
import { newIdempotencyKey } from '../../../api/client';
import { DEFAULT_PAGE_LIMIT, MAX_PAGE_LIMIT } from '../../../api/pagination';
import { ApiError } from '../../../api/problem';
import {
  DOWNLOAD_FAILURE_CLASSES,
  RemoteDownload,
  RemoteDownloadBulk,
  RemoteDownloadCounts,
  RemoteDownloadFailureClass,
  RemoteDownloadFilters,
  RemoteDownloadState,
  RemoteDownloadsApi,
  downloadActive,
  downloadRetryable,
} from '../../../api/remote-downloads-api';
import { describeProblem } from '../../../core/http/problem-messages';
import { Notifier } from '../../../core/notifier';
import { SessionStore } from '../../../core/session-store';
import { ErrorNotice } from '../../../shared/error-notice';
import { asApiError, valueOf } from '../../../shared/paged-resource';
import { DataTable, TableColumn } from '../../../shared/ui/data-table';
import { NoValue } from '../../../shared/ui/no-value';
import { TimeAgo } from '../../../shared/ui/time-ago';
import { formatSize } from '../../packages/format-size';
import {
  downloadOutcomeText,
  downloadStateText,
  downloadSubject,
  failureClassText,
  failureLine,
  failureStepText,
} from './download-words';

/** How often the tab reads the queue again while a row waits or runs (R-CUR-018; WCAG 2.2.2: with a pause control). */
export const DOWNLOADS_REFRESH_MS = 5_000;

/** The states the tab offers as filters, with the counts of the list's answer (R-CUR-018). Cancelled rows are under All. */
const FILTER_STATES = ['waiting', 'running', 'failed', 'done'] as const;
type FilterState = (typeof FILTER_STATES)[number];

/**
 * The tab **Downloads** of a remote repository (remote-curation.md R-CUR-018, `…/repositories/{key}/downloads`): the repository's
 * download queue (R-CUR-011, R-CUR-012). The counts by state are the filter, with **Failure** by class; the filters and a bulk live
 * in the address (`state`, `failure`, `bulk`). A row names its subject, its origin, its state with the attempt, its progress and its
 * failure in one line, and opens in place to the whole failure: the step, the code, the address without its query, the upstream's
 * status, the excerpt of its answer and Quader's sentence. A row offers **Retry** or **Cancel** to whoever may (R-CUR-005); a
 * holder of `manage` retries every failed row that matches. The list reads itself again every five seconds while a row waits or
 * runs, until **Pause automatic refresh** (the form of the page System status, Q-UI-651).
 */
@Component({
  selector: 'quader-remote-downloads',
  imports: [DataTable, ErrorNotice, NoValue, RouterLink, TimeAgo],
  templateUrl: './remote-downloads.html',
  styleUrl: './remote-downloads.scss',
})
export class RemoteDownloads {
  private readonly api = inject(RemoteDownloadsApi);
  private readonly session = inject(SessionStore);
  private readonly notifier = inject(Notifier);
  private readonly router = inject(Router);
  private readonly route = inject(ActivatedRoute);

  /** The qualified name `<project>/<key>` of the remote repository. */
  readonly repository = input.required<string>();
  /**
   * The action a client's fill needs in this repository (`remote.cacheFillAction`, R-AUTHZ-008): the full document has it; a reader's
   * view does not, and then the default, `read`, is asked.
   */
  readonly fillAction = input<'read' | 'deploy'>('read');
  /** The queue's counts after every answer, for the tab's label in the strip (`Downloads · 2 failed`). */
  readonly countsChange = output<RemoteDownloadCounts>();

  private readonly query = toSignal(this.route.queryParamMap.pipe(map((q) => ({ state: q.get('state'), failure: q.get('failure'), bulk: q.get('bulk') }))), {
    initialValue: { state: null, failure: null, bulk: null },
  });

  /** The state the list shows; none shows every row. */
  protected readonly state = computed<FilterState | null>(() => {
    const s = this.query().state;
    return (FILTER_STATES as readonly string[]).includes(s ?? '') ? (s as FilterState) : null;
  });
  /** The failure class the list shows; none shows every row. */
  protected readonly failure = computed<RemoteDownloadFailureClass | null>(() => {
    const c = this.query().failure;
    return (DOWNLOAD_FAILURE_CLASSES as readonly string[]).includes(c ?? '') ? (c as RemoteDownloadFailureClass) : null;
  });
  /** The bulk whose rows the list shows (the link of a row's origin **Bulk**). */
  protected readonly bulkId = computed(() => this.query().bulk || null);
  private readonly filters = computed<RemoteDownloadFilters>(() => ({
    state: this.state() ?? undefined,
    class: this.failure() ?? undefined,
    bulk: this.bulkId() ?? undefined,
  }));
  protected readonly filtered = computed(() => this.state() !== null || this.failure() !== null);

  protected readonly rows = signal<readonly RemoteDownload[]>([]);
  protected readonly counts = signal<RemoteDownloadCounts | null>(null);
  protected readonly loaded = signal(false);
  protected readonly loading = signal(false);
  protected readonly listError = signal<ApiError | null>(null);
  private readonly nextCursor = signal<string | null>(null);
  protected readonly hasMore = computed(() => this.nextCursor() !== null);
  /** A new filter starts a new traversal; an answer of an earlier one that arrives late is dropped. */
  private generation = 0;

  protected readonly opened = signal<ReadonlySet<string>>(new Set());
  protected readonly paused = signal(false);
  protected readonly busy = signal<ReadonlySet<string>>(new Set());

  protected readonly bulkRead = resource({
    params: () => {
      const id = this.bulkId();
      return id ? { repository: this.repository(), id } : null;
    },
    loader: ({ params }) => (params ? this.api.bulk(params.repository, params.id) : Promise.resolve(null)),
  });
  protected readonly bulk = computed<RemoteDownloadBulk | null>(() => valueOf(this.bulkRead) ?? null);
  protected readonly bulkFailure = computed(() => {
    const e = this.bulkRead.error();
    return e ? asApiError(e) : null;
  });

  private readonly self = computed(() => this.session.whoami()?.principal.id ?? null);
  /**
   * What the caller may do in the repository (R-CUR-005): `manage` (the roles Maintainer and Owner) or the system role `admin`, and
   * the fill of a client, which retries and cancels a row a probe or a client caused.
   */
  private readonly rights = resource({
    params: () => ({ repository: this.repository(), fill: this.fillAction() }),
    loader: async ({ params }) => {
      if (this.session.state() !== 'authenticated') return { manage: false, fill: false };
      const [manage, read, fill] = await this.session.check([
        { repository: params.repository, action: 'manage' },
        { repository: params.repository, action: 'read' },
        { repository: params.repository, action: params.fill },
      ]);
      return { manage: !!manage || this.session.hasRole('admin'), fill: !!read && !!fill };
    },
  });
  protected readonly manager = computed(() => valueOf(this.rights)?.manage ?? false);
  private readonly mayFill = computed(() => valueOf(this.rights)?.fill ?? false);

  /** The list reads itself again while the repository has a row that waits or runs, or a bulk expands. */
  protected readonly live = computed(() => {
    const c = this.counts();
    const b = this.bulk();
    return (c !== null && c.waiting + c.running > 0) || (b !== null && (b.state === 'expanding' || b.state === 'queued'));
  });

  protected readonly states = FILTER_STATES;
  protected readonly classes = DOWNLOAD_FAILURE_CLASSES;
  protected readonly classText = failureClassText;
  protected readonly stepText = failureStepText;
  protected readonly stateText = downloadStateText;
  protected readonly outcomeText = downloadOutcomeText;
  protected readonly subject = downloadSubject;
  protected readonly line = failureLine;
  protected readonly size = formatSize;

  protected readonly caption = $localize`:@@downloads.caption:Downloads of this repository, running and waiting first`;
  protected readonly emptyText = $localize`:@@downloads.empty:There is no download yet.`;
  protected readonly emptyFiltered = $localize`:@@downloads.empty-filtered:No download matches.`;
  protected readonly stateGroupLabel = $localize`:@@downloads.filter.state:State`;

  /**
   * Subject, origin, state, progress and failure: the subject and the failure wrap and share the room the others leave by what they
   * hold, the rest never breaks (Q-UI-563). With the failure preferring the room, a long package name broke into seven lines beside
   * an empty failure (2026-11-24). The action column is there only while a row offers one.
   */
  protected readonly hasActions = computed(() => this.rows().some((r) => this.action(r) !== null));
  protected readonly columns = computed<readonly TableColumn[]>(() => [
    { id: 'subject', label: $localize`:@@downloads.col.subject:Package`, width: '14rem' },
    { id: 'origin', label: $localize`:@@downloads.col.origin:Origin` },
    { id: 'state', label: $localize`:@@downloads.col.state:State` },
    { id: 'progress', label: $localize`:@@downloads.col.progress:Progress` },
    { id: 'failure', label: $localize`:@@downloads.col.failure:Failure`, width: '14rem' },
    ...(this.hasActions() ? [{ id: 'actions', label: $localize`:@@downloads.col.actions:Actions`, hideLabel: true }] : []),
  ]);

  constructor() {
    effect(() => {
      const repository = this.repository();
      const filters = this.filters();
      untracked(() => {
        this.opened.set(new Set());
        void this.load(repository, filters, true);
      });
    });
    effect((onCleanup) => {
      if (this.paused() || !this.live()) return;
      const timer = setInterval(() => this.refresh(), DOWNLOADS_REFRESH_MS);
      onCleanup(() => clearInterval(timer));
    });
  }

  /**
   * Reads the first page again: as many rows as are shown (at least one page, at most the largest), so a refresh keeps what
   * **Load more** added and the rows that are open. A new filter starts over with one page.
   */
  private async load(repository: string, filters: RemoteDownloadFilters, fresh: boolean): Promise<void> {
    const generation = fresh ? ++this.generation : this.generation;
    if (fresh) {
      this.loaded.set(false);
      this.rows.set([]);
      this.nextCursor.set(null);
    }
    const limit = Math.min(MAX_PAGE_LIMIT, Math.max(DEFAULT_PAGE_LIMIT, this.rows().length));
    this.loading.set(true);
    try {
      const page = await this.api.list(repository, filters, null, limit);
      if (generation !== this.generation) return;
      this.rows.set(page.items);
      this.nextCursor.set(page.nextCursor);
      this.counts.set(page.counts);
      this.countsChange.emit(page.counts);
      this.listError.set(null);
      this.loaded.set(true);
    } catch (e) {
      if (generation === this.generation) this.listError.set(asApiError(e));
    } finally {
      if (generation === this.generation) this.loading.set(false);
    }
  }

  /** The automatic refresh and the one after an action: the rows, and the bulk the list shows. */
  protected refresh(): void {
    void this.load(this.repository(), this.filters(), false);
    if (this.bulkId()) this.bulkRead.reload();
  }

  protected retryList(): void {
    void this.load(this.repository(), this.filters(), true);
  }

  protected async more(): Promise<void> {
    const cursor = this.nextCursor();
    if (cursor === null || this.loading()) return;
    const generation = this.generation;
    this.loading.set(true);
    try {
      const page = await this.api.list(this.repository(), this.filters(), cursor, DEFAULT_PAGE_LIMIT);
      if (generation !== this.generation) return;
      const seen = new Set(this.rows().map((r) => r.id));
      this.rows.update((rows) => [...rows, ...page.items.filter((r) => !seen.has(r.id))]);
      this.nextCursor.set(page.nextCursor);
      this.counts.set(page.counts);
    } catch (e) {
      if (generation === this.generation) this.listError.set(asApiError(e));
    } finally {
      if (generation === this.generation) this.loading.set(false);
    }
  }

  // ---- The filters (in the address) ----

  protected setState(state: FilterState | null): void {
    this.navigate({ state });
  }

  protected setFailure(value: string): void {
    this.navigate({ failure: value || null });
  }

  protected bulkLink(id: string): Record<string, string | null> {
    return { bulk: id, state: null, failure: null };
  }

  protected clearBulk(): void {
    this.navigate({ bulk: null });
  }

  private navigate(change: Readonly<Record<string, string | null>>): void {
    void this.router.navigate([], { relativeTo: this.route, queryParams: change, queryParamsHandling: 'merge', replaceUrl: true });
  }

  protected countOf(state: FilterState): number | null {
    const c = this.counts();
    return c ? c[state] : null;
  }

  // ---- A row ----

  protected isOpen(r: RemoteDownload): boolean {
    return this.opened().has(r.id);
  }

  protected toggle(r: RemoteDownload): void {
    this.opened.update((s) => {
      const next = new Set(s);
      if (next.has(r.id)) next.delete(r.id);
      else next.add(r.id);
      return next;
    });
  }

  protected openLabel(r: RemoteDownload): string {
    return $localize`:@@downloads.row.open:Details of ${downloadSubject(r)}:subject:`;
  }

  /** Who or what put the row into the queue (R-CUR-018); a person's name only to whom the API names it (R-CUR-012). */
  protected originText(r: RemoteDownload): string {
    switch (r.origin) {
      case 'person': {
        const by = r.startedBy;
        if (by?.id && by.id === this.self()) return $localize`:@@downloads.origin.you:You`;
        return by?.name ?? $localize`:@@downloads.origin.person:A person`;
      }
      case 'request':
        return $localize`:@@downloads.origin.request:Request`;
      case 'allow':
        return $localize`:@@downloads.origin.allow:Allow`;
      case 'bulk':
        return $localize`:@@downloads.origin.bulk:Bulk`;
      case 'probe':
        return $localize`:@@downloads.origin.probe:Probe`;
      case 'client':
        return $localize`:@@downloads.origin.client:Client`;
    }
  }

  /** The person who started a row or a bulk, where the API names them: **You**, their name, or nobody (`null`). */
  protected starterText(by: RemoteDownload['startedBy']): string | null {
    if (!by?.id) return null;
    if (by.id === this.self()) return $localize`:@@downloads.origin.you:You`;
    return by.name ?? null;
  }

  /** The attempt of a row: "Attempt 2 of 5", for a row that has made one. */
  protected attemptText(r: RemoteDownload): string | null {
    if (r.attempts <= 0) return null;
    return $localize`:@@downloads.attempt:Attempt ${r.attempts}:attempt: of ${r.maxAttempts}:max:`;
  }

  /** The files a row stored or found stored, of the files it named, and the bytes; nothing before it named any. */
  protected progressText(r: RemoteDownload): string | null {
    if (r.files <= 0) return null;
    const done = r.stored + r.present;
    const files = $localize`:@@downloads.progress.files:Files: ${done}:done: of ${r.files}:files:`;
    return r.bytes > 0 ? `${files} · ${formatSize(r.bytes)}` : files;
  }

  /**
   * The row's one action (R-UI-163): **Retry** for a failed or cancelled row, **Cancel** for one that waits or runs, to the person
   * who started it, a holder of `manage` and the administrator, and for a row a probe or a client caused to whoever may fill the
   * repository with that version (R-CUR-005). The server decides again.
   */
  protected action(r: RemoteDownload): 'retry' | 'cancel' | null {
    const kind = downloadRetryable(r.state) ? 'retry' : downloadActive(r.state) ? 'cancel' : null;
    if (kind === null) return null;
    const own = !!r.startedBy?.id && r.startedBy.id === this.self();
    const caused = (r.origin === 'probe' || r.origin === 'client') && r.delivery === 'delivered' && this.mayFill();
    return own || this.manager() || caused ? kind : null;
  }

  protected isBusy(r: RemoteDownload): boolean {
    return this.busy().has(r.id);
  }

  protected async act(r: RemoteDownload): Promise<void> {
    const kind = this.action(r);
    if (kind === null || this.isBusy(r)) return;
    this.busy.update((s) => new Set([...s, r.id]));
    try {
      const updated =
        kind === 'retry'
          ? await this.api.retry(this.repository(), r.id, newIdempotencyKey())
          : await this.api.cancel(this.repository(), r.id, newIdempotencyKey());
      this.rows.update((rows) => rows.map((x) => (x.id === r.id ? updated : x)));
      this.notifier.status(
        kind === 'retry'
          ? $localize`:@@downloads.retried:${downloadSubject(r)}:subject: waits for its download again.`
          : $localize`:@@downloads.cancelled:The download of ${downloadSubject(r)}:subject: is cancelled.`,
      );
    } catch (e) {
      this.notifier.alert(describeProblem(asApiError(e)).text);
    } finally {
      this.busy.update((s) => {
        const next = new Set(s);
        next.delete(r.id);
        return next;
      });
      this.refresh();
    }
  }

  protected actionLabel(r: RemoteDownload): string {
    return this.action(r) === 'retry' ? $localize`:@@downloads.retry:Retry` : $localize`:@@downloads.cancel:Cancel`;
  }

  // ---- Every failed row, and the bulk ----

  /** **Retry all failed** with the class filter and the bulk the list shows: the holders of `manage` and the administrator. */
  protected readonly offersRetryAll = computed(() => this.manager() && (this.counts()?.failed ?? 0) > 0);
  protected readonly retryingAll = signal(false);

  protected async retryAll(): Promise<void> {
    if (this.retryingAll()) return;
    this.retryingAll.set(true);
    try {
      const n = await this.api.retryFailed(
        this.repository(),
        { class: this.failure() ?? undefined, bulk: this.bulkId() ?? undefined },
        newIdempotencyKey(),
      );
      this.notifier.status($localize`:@@downloads.retried-all:Failed downloads that wait again: ${n}:count:.`);
    } catch (e) {
      this.notifier.alert(describeProblem(asApiError(e)).text);
    } finally {
      this.retryingAll.set(false);
      this.refresh();
    }
  }

  /** **Cancel bulk** while it expands or its rows wait: the person who started it, a holder of `manage`, the administrator. */
  protected readonly offersCancelBulk = computed(() => {
    const b = this.bulk();
    if (!b || (b.state !== 'expanding' && b.state !== 'queued')) return false;
    return this.manager() || (!!b.startedBy?.id && b.startedBy.id === this.self());
  });

  protected async cancelBulk(): Promise<void> {
    const b = this.bulk();
    if (!b) return;
    try {
      await this.api.cancelBulk(this.repository(), b.id, newIdempotencyKey());
      this.notifier.status($localize`:@@downloads.bulk.cancelled:The bulk download of ${b.subject}:subject: is cancelled.`);
    } catch (e) {
      this.notifier.alert(describeProblem(asApiError(e)).text);
    } finally {
      this.refresh();
    }
  }

  /** What a bulk downloads: every version of a package, or a sub-section of the upstream. */
  protected bulkTitle(b: RemoteDownloadBulk): string {
    return b.level === 'package'
      ? $localize`:@@downloads.bulk.package:Every version of ${b.subject}:subject:`
      : $localize`:@@downloads.bulk.section:The sub-section ${b.subject}:subject:`;
  }

  protected bulkStateText(b: RemoteDownloadBulk): string {
    switch (b.state) {
      case 'expanding':
        return $localize`:@@downloads.bulk.state.expanding:Finding versions`;
      case 'queued':
        return $localize`:@@downloads.bulk.state.queued:Queued`;
      case 'done':
        return $localize`:@@downloads.bulk.state.done:Done`;
      case 'bounded':
        return $localize`:@@downloads.bulk.state.bounded:Stopped at a bound`;
      case 'cancelled':
        return $localize`:@@downloads.bulk.state.cancelled:Cancelled`;
      case 'failed':
        return $localize`:@@downloads.bulk.state.failed:Failed`;
    }
  }

  protected bulkCounts(b: RemoteDownloadBulk): string {
    return $localize`:@@downloads.bulk.counts:${b.versionsQueued}:queued: of ${b.versionsFound}:found: versions found are queued`;
  }

  protected bulkBounds(b: RemoteDownloadBulk): string {
    const bytes = formatSize(b.bounds.maxBytes);
    return b.bounds.includePrereleases
      ? $localize`:@@downloads.bulk.bounds:At most ${b.bounds.maxVersions}:versions: versions and ${bytes}:bytes:, pre-releases included`
      : $localize`:@@downloads.bulk.bounds-no-pre:At most ${b.bounds.maxVersions}:versions: versions and ${bytes}:bytes:, without pre-releases`;
  }

  protected bulkRows(b: RemoteDownloadBulk): readonly { readonly state: RemoteDownloadState; readonly n: number }[] {
    const order: readonly RemoteDownloadState[] = ['running', 'waiting', 'failed', 'done', 'cancelled'];
    return order.map((state) => ({ state, n: b.rows[state] ?? 0 })).filter((x) => x.n > 0);
  }
}
