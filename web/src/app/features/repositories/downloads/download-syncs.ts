import { CdkMenu, CdkMenuItem, CdkMenuTrigger } from '@angular/cdk/menu';
import { Component, computed, effect, inject, input, output, signal, untracked } from '@angular/core';
import { RouterLink } from '@angular/router';
import { newIdempotencyKey } from '../../../api/client';
import { MAX_PAGE_LIMIT } from '../../../api/pagination';
import { ApiError } from '../../../api/problem';
import { RemoteDownloadBulk, RemoteDownloadSync, RemoteDownloadsApi } from '../../../api/remote-downloads-api';
import { LIVE_SLOW_MS, live } from '../../../core/live-refresh';
import { Notifier } from '../../../core/notifier';
import { Confirm } from '../../../shared/confirm-dialog';
import { FailureToasts } from '../../../shared/failure-dialog';
import { asApiError } from '../../../shared/paged-resource';
import { DataTable, TableColumn } from '../../../shared/ui/data-table';
import { TimeAgo } from '../../../shared/ui/time-ago';
import { failureSentence, syncIntervalSentence, versionRangeText } from './download-words';
import { DOWNLOADS_REFRESH_MS } from './downloads-refresh';

/** The syncs the section shows at first and adds per **Show more**: a repository holds at most `max_syncs_per_repository` (100). */
export const SYNCS_PAGE = 20;

/** What a sync's last run, or its last look, says in the cell **Last run** (R-CUR-027): a short word as a badge, and why, under it. */
export interface SyncRunWords {
  readonly text: string;
  /** The badge's class and icon, as a bulk's state shows them. */
  readonly cls: string;
  readonly icon: string;
  /** Why no run started, a sentence under the badge that wraps; null where the word says it all. */
  readonly reason: string | null;
}

/**
 * What the last look at a sync did, in the screen's words (R-CUR-027): a run that runs, ended with news, with nothing new, or failed;
 * or why no run started. `upstream` names the upstream of a rate limit where the screen knows its preset.
 */
export function syncRunWords(s: RemoteDownloadSync, upstream: string | null): SyncRunWords {
  const run = s.lastBulk;
  const waits = (reason: string): SyncRunWords => ({ text: $localize`:@@syncs.run.waits:Waits`, cls: 'badge', icon: 'hourglass_empty', reason });
  const notRun = (reason: string): SyncRunWords => ({ text: $localize`:@@syncs.run.not-run:Not run`, cls: 'badge', icon: 'block', reason });
  switch (s.lastOutcome) {
    case 'overlapped':
      return waits($localize`:@@syncs.run.overlapped:Its previous run has not ended yet.`);
    case 'limited':
      return waits(
        upstream
          ? $localize`:@@syncs.run.limited-of:${upstream}:upstream: limits how often Quader may ask it.`
          : $localize`:@@syncs.run.limited:The upstream limits how often Quader may ask it.`,
      );
    case 'bounded':
      return notRun($localize`:@@syncs.run.bounded:The repository holds as much as its syncs may fill.`);
    case 'disabled':
      return notRun($localize`:@@syncs.run.disabled:The repository is disabled or offline.`);
    case 'paused':
      return notRun($localize`:@@syncs.run.paused-all:The syncs of this repository are paused.`);
  }
  const word = (text: string, cls: string, icon: string): SyncRunWords => ({ text, cls, icon, reason: null });
  // A run the queue's retention removed (R-CUR-011) leaves its time and its start.
  if (!run && s.lastRunAt) return word($localize`:@@syncs.run.started:Started`, 'badge', 'check');
  if (!run) return word($localize`:@@syncs.run.none:No run yet`, 'badge', 'schedule');
  switch (run.state) {
    case 'expanding':
      return word($localize`:@@syncs.run.finding:Finding what is new`, 'badge', 'sync');
    case 'queued':
      return word($localize`:@@syncs.run.running:Downloading ${run.versionsQueued}:count: new`, 'badge', 'sync');
    case 'failed':
      return word($localize`:@@syncs.run.failed:Failed`, 'bad', 'error');
    case 'cancelled':
      return word($localize`:@@syncs.run.cancelled:Cancelled`, 'badge', 'cancel');
  }
  return run.versionsQueued === 0
    ? word($localize`:@@syncs.run.nothing-new:Nothing new`, 'ok', 'check_circle')
    : word($localize`:@@syncs.run.downloaded:Downloaded ${run.versionsQueued}:count: new`, 'ok', 'check_circle');
}

/**
 * The section **Kept in sync** of the tab **Downloads** (remote-curation.md R-CUR-018, R-CUR-027, Q-CUR-250): the repository's syncs
 * above its bulk downloads, once it has one. A row says what is kept in sync (the package or the sub-section, the range, the newest
 * so many, who set it up), its last run (its word, its time and a link to the run's bulk, or why no run started) and its next run, or
 * **Paused**. The holders of `manage` and the administrator get the row's menu (R-UI-163): **Run now**, **Pause** or **Resume**,
 * **Remove…**. The section reads itself again while it is seen (R-UI-213): every five seconds while a run of a sync is in progress,
 * every 30 s otherwise, and not while the tab's automatic refresh is paused.
 */
@Component({
  selector: 'quader-download-syncs',
  imports: [CdkMenu, CdkMenuItem, CdkMenuTrigger, DataTable, RouterLink, TimeAgo],
  templateUrl: './download-syncs.html',
  styleUrl: './remote-downloads.scss',
})
export class DownloadSyncs {
  private readonly api = inject(RemoteDownloadsApi);
  private readonly notifier = inject(Notifier);
  private readonly failureToasts = inject(FailureToasts);
  private readonly confirm = inject(Confirm);

  /** The qualified name `<project>/<key>` of the remote repository. */
  readonly repository = input.required<string>();
  /** The caller holds `manage` or is the administrator (R-CUR-005): the rows' menu. */
  readonly manager = input(false);
  /** The tab's automatic refresh is paused: the section reads nothing by itself either. */
  readonly paused = input(false);
  /** The name of the upstream's preset (Docker Hub) for a rate limit's words; null names the upstream plainly. */
  readonly upstreamName = input<string | null>(null);
  /** The reader's principal, whose own sync reads "Set up by you". */
  readonly self = input<string | null>(null);
  /** A person's act changed the queue (a run started): the tab reads its rows and bulks again. */
  readonly changed = output<void>();

  protected readonly syncs = signal<readonly RemoteDownloadSync[]>([]);
  protected readonly interval = signal(3600);
  /** `remote.syncs.paused` of the repository. */
  protected readonly allPaused = signal(false);
  protected readonly loaded = signal(false);
  protected readonly loading = signal(false);
  protected readonly error = signal<ApiError | null>(null);
  private readonly cursor = signal<string | null>(null);
  protected readonly hasMore = computed(() => this.cursor() !== null);
  protected readonly busy = signal<ReadonlySet<string>>(new Set());
  private generation = 0;

  /** The section stands once the repository has a sync, or its read failed. */
  protected readonly shown = computed(() => this.syncs().length > 0 || this.error() !== null);
  /** A run of a sync is in progress: the section reads fast. */
  private readonly running = computed(() => this.syncs().some((s) => !!s.lastBulk && bulkInProgress(s.lastBulk)));
  private readonly liveReads = live({
    reload: () => this.load(this.repository(), false, true),
    every: () => (this.running() ? DOWNLOADS_REFRESH_MS : LIVE_SLOW_MS),
    when: () => !this.paused(),
  });

  protected readonly caption = $localize`:@@syncs.caption:Downloads this repository keeps in sync, newest first`;
  protected readonly intervalText = computed(() => syncIntervalSentence(this.interval()));
  protected readonly hasActions = computed(() => this.manager() && this.syncs().length > 0);
  protected readonly columns = computed<readonly TableColumn[]>(() => [
    { id: 'subject', label: $localize`:@@syncs.col.subject:Kept in sync`, width: '16rem' },
    { id: 'last', label: $localize`:@@syncs.col.last:Last run`, width: '16rem' },
    { id: 'next', label: $localize`:@@syncs.col.next:Next run` },
    ...(this.hasActions() ? [{ id: 'actions', label: $localize`:@@downloads.col.actions:Actions`, hideLabel: true }] : []),
  ]);
  protected readonly runWords = (s: RemoteDownloadSync): SyncRunWords => syncRunWords(s, this.upstreamName());
  protected readonly failure = failureSentence;

  constructor() {
    effect(() => {
      const repository = this.repository();
      untracked(() => void this.load(repository, true));
    });
  }

  /** Reads the syncs again, as many as are shown; a quiet (live) read keeps what is shown when it fails and throws (R-UI-213). */
  private async load(repository: string, fresh: boolean, quiet = false): Promise<void> {
    const generation = fresh ? ++this.generation : this.generation;
    if (fresh) {
      this.syncs.set([]);
      this.cursor.set(null);
      this.loaded.set(false);
    }
    const limit = Math.min(MAX_PAGE_LIMIT, Math.max(SYNCS_PAGE, this.syncs().length));
    this.loading.set(true);
    try {
      const page = await this.api.listSyncs(repository, null, limit);
      if (generation !== this.generation) return;
      this.syncs.set(page.items);
      this.cursor.set(page.nextCursor);
      this.interval.set(page.intervalSeconds);
      this.allPaused.set(page.paused);
      this.error.set(null);
      this.loaded.set(true);
    } catch (e) {
      if (quiet && this.loaded()) throw e;
      if (generation === this.generation) this.error.set(asApiError(e));
    } finally {
      if (generation === this.generation) this.loading.set(false);
    }
  }

  /** Reads the syncs at once: the tab calls it after a person's act, and after its own. */
  refresh(): void {
    void this.liveReads.now();
  }

  protected retry(): void {
    void this.load(this.repository(), true);
  }

  protected async more(): Promise<void> {
    const cursor = this.cursor();
    if (cursor === null || this.loading()) return;
    const generation = this.generation;
    this.loading.set(true);
    try {
      const page = await this.api.listSyncs(this.repository(), cursor, SYNCS_PAGE);
      if (generation !== this.generation) return;
      const seen = new Set(this.syncs().map((s) => s.id));
      this.syncs.update((list) => [...list, ...page.items.filter((s) => !seen.has(s.id))]);
      this.cursor.set(page.nextCursor);
    } catch (e) {
      if (generation === this.generation) this.error.set(asApiError(e));
    } finally {
      if (generation === this.generation) this.loading.set(false);
    }
  }

  /** What is kept in sync: every version of a package, or a sub-section. */
  protected title(s: RemoteDownloadSync): string {
    return s.level === 'package'
      ? $localize`:@@syncs.package:Every version of ${s.subject}:subject:`
      : $localize`:@@syncs.section:The sub-section ${s.subject}:subject:`;
  }

  /** The range a sync was set up with (Q-CUR-193): "From 5.0.0 up to 5.4.7"; null without one. */
  protected range(s: RemoteDownloadSync): string | null {
    return versionRangeText(s.bounds);
  }

  /** How many of the newest versions it keeps: "The newest 2". */
  protected newest(s: RemoteDownloadSync): string {
    return s.bounds.includePrereleases
      ? $localize`:@@syncs.newest:The newest ${s.bounds.maxVersions}:count:, pre-releases included`
      : $localize`:@@syncs.newest-no-pre:The newest ${s.bounds.maxVersions}:count:, without pre-releases`;
  }

  protected setUpBySelf(s: RemoteDownloadSync): boolean {
    return !!s.createdBy?.id && s.createdBy.id === this.self();
  }

  /** The name of another person who set the sync up, where the API names them (R-CUR-012). */
  protected setUpBy(s: RemoteDownloadSync): string | null {
    if (!s.createdBy?.id || s.createdBy.id === this.self()) return null;
    return s.createdBy.name ?? null;
  }

  /** The run's rows in one line, where it queued any: "Running 1 · Waiting 2", or what it left out. */
  protected runLine(b: RemoteDownloadBulk): string | null {
    const parts: string[] = [];
    if (b.versionsRefused > 0) parts.push($localize`:@@syncs.run.refused:${b.versionsRefused}:count: not delivered`);
    return parts.length ? parts.join(' · ') : null;
  }

  /** The failure of a last run that failed, for its sentence under the run's line; null otherwise. */
  protected failedRun(s: RemoteDownloadSync): RemoteDownloadBulk['failure'] {
    const b = s.lastBulk;
    return b && b.state === 'failed' ? b.failure : null;
  }

  protected runLink(b: RemoteDownloadBulk): Record<string, string | null> {
    return { bulk: b.id, state: null, failure: null };
  }

  protected runLinkLabel(s: RemoteDownloadSync): string {
    return $localize`:@@syncs.run.open:The downloads of the last run that keeps ${s.subject}:subject: in sync`;
  }

  protected menuLabel(s: RemoteDownloadSync): string {
    return $localize`:@@syncs.menu:Actions for ${s.subject}:subject:`;
  }

  protected isBusy(s: RemoteDownloadSync): boolean {
    return this.busy().has(s.id);
  }

  protected run(s: RemoteDownloadSync): Promise<void> {
    return this.act(s, async () => {
      await this.api.runSync(this.repository(), s.id, newIdempotencyKey());
      this.notifier.status($localize`:@@syncs.run-started:A run that keeps ${s.subject}:subject: in sync has started.`);
      this.changed.emit();
    });
  }

  protected setPaused(s: RemoteDownloadSync, paused: boolean): Promise<void> {
    return this.act(s, async () => {
      const next = await this.api.pauseSync(this.repository(), s.id, paused, newIdempotencyKey());
      this.syncs.update((list) => list.map((x) => (x.id === s.id ? next : x)));
      this.notifier.status(
        paused
          ? $localize`:@@syncs.paused:The sync of ${s.subject}:subject: is paused: it starts no run until it is resumed.`
          : $localize`:@@syncs.resumed:${s.subject}:subject: is kept in sync again.`,
      );
    });
  }

  protected async remove(s: RemoteDownloadSync): Promise<void> {
    const answer = await this.confirm.ask({
      title: $localize`:@@syncs.remove.title:Stop keeping ${s.subject}:subject: in sync?`,
      lines: [
        $localize`:@@syncs.remove.text:Quader stops downloading what is new of it. What it stored stays, and a run in progress ends as a bulk download.`,
      ],
      confirmLabel: $localize`:@@syncs.remove.confirm:Remove`,
    });
    if (answer === null) return;
    await this.act(s, async () => {
      await this.api.removeSync(this.repository(), s.id, newIdempotencyKey());
      this.syncs.update((list) => list.filter((x) => x.id !== s.id));
      this.notifier.status($localize`:@@syncs.removed:${s.subject}:subject: is no longer kept in sync.`);
    });
  }

  private async act(s: RemoteDownloadSync, fn: () => Promise<void>): Promise<void> {
    if (this.isBusy(s)) return;
    this.busy.update((set) => new Set([...set, s.id]));
    try {
      await fn();
    } catch (e) {
      this.failureToasts.alert(e);
    } finally {
      this.busy.update((set) => {
        const next = new Set(set);
        next.delete(s.id);
        return next;
      });
      this.refresh();
    }
  }
}

/** A run that expands or whose rows wait or run. */
function bulkInProgress(b: RemoteDownloadBulk): boolean {
  return b.state === 'expanding' || b.state === 'queued';
}
