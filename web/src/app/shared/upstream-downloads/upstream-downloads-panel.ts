import { NgTemplateOutlet } from '@angular/common';
import { Component, computed, effect, inject, input, model, output, resource, signal, untracked } from '@angular/core';
import { RouterLink } from '@angular/router';
import { ApiError } from '../../api/problem';
import {
  UpstreamDownload,
  UpstreamDownloadFinished,
  UpstreamDownloadList,
  UpstreamDownloadRunning,
  UpstreamDownloadWaiting,
  UpstreamDownloadsApi,
} from '../../api/upstream-downloads-api';
import { FormatCatalog } from '../../core/format-names';
import { PageVisibility } from '../../core/page-visibility';
import { BrowserStorage } from '../../core/storage';
import { FormatIconView } from '../format-icon';
import { valueOf } from '../paged-resource';
import { QuaderDrawer } from '../ui/drawer';
import { TimeAgo } from '../ui/time-ago';
import { finishedFigures, originText, outcomeBadge, runningFigures, subjectOf, subjectTitle } from './download-words';
import { limitQueueText } from './limit-words';
import { Loading } from '../ui/loading';

/** Where this device keeps the newest end of the finished downloads **Hide all** hid (R-UI-212). */
export const HIDDEN_BEFORE_KEY = 'quader.upstream-downloads.hidden-before';
/** How often the open panel reads the list when the answer names no interval (`pollSeconds`, R-OPS-052's default). */
const DEFAULT_POLL_SECONDS = 2;

/**
 * The downloads from upstreams (api-and-ui.md R-UI-211, R-UI-212; operations-and-observability.md R-OPS-049 to R-OPS-052) in Quader's
 * drawer from the right, one row per download, the unit a person asked for (an image, a version's package, a queue row's run), never
 * its files: a filter for both sections, the download queues an upstream's rate limit holds (remote-curation.md R-CUR-011,
 * Q-UI-1101), the running downloads with their progress, speed and time, the last 100 finished with their duration and average,
 * **Hide all** for this device, and a link to the manual. While it is open and the window
 * is seen it reads the whole list every `pollSeconds`; closing it stops the refresh. It announces nothing (the button's name carries
 * the count). A row has no actions: a queued one links its repository's tab Downloads.
 */
@Component({
  selector: 'quader-upstream-downloads-panel',
  imports: [Loading, NgTemplateOutlet, QuaderDrawer, RouterLink, FormatIconView, TimeAgo],
  templateUrl: './upstream-downloads-panel.html',
  styleUrl: './upstream-downloads-panel.scss',
})
export class UpstreamDownloadsPanel {
  private readonly api = inject(UpstreamDownloadsApi);
  private readonly storage = inject(BrowserStorage);
  private readonly page = inject(PageVisibility);
  private readonly catalog = inject(FormatCatalog);

  readonly open = model(false);
  /** The id of the drawer's dialog; the button points `aria-controls` at it. */
  readonly panelId = input.required<string>();
  /** The running downloads after every answer: the button's badge follows the open list. */
  readonly counted = output<number>();
  /** The caller may not read the list (403): the button hides itself. */
  readonly refused = output<void>();

  protected readonly heading = $localize`:@@upstream-downloads.title:Downloads`;
  protected readonly closeLabel = $localize`:@@upstream-downloads.close:Close downloads`;
  protected readonly filterId = 'upstream-downloads-filter';

  protected readonly list = signal<UpstreamDownloadList | null>(null);
  protected readonly refreshFailed = signal(false);
  protected readonly filter = signal('');
  private readonly hiddenBefore = signal<string | null>(this.storage.get('local', HIDDEN_BEFORE_KEY));
  /** **Hide all** needs the browser's local storage; where it cannot be written, there is no button. */
  protected readonly canHide = this.storage.available('local');

  private readonly formats = resource({ loader: () => this.catalog.formats() });

  private readonly query = computed(() => this.filter().trim().toLowerCase());
  protected readonly filtering = computed(() => this.query() !== '');

  /** The queues an upstream's rate limit holds (Q-UI-1101), filtered by the repository and the upstream's name. */
  protected readonly waiting = computed(() => this.list()?.waiting ?? []);
  protected readonly waitingShown = computed(() => {
    const q = this.query();
    return this.waiting().filter((w) => q === '' || [`${w.repository.project}/${w.repository.key}`, w.upstream].some((t) => t.toLowerCase().includes(q)));
  });
  protected readonly waitingHeading = computed(() =>
    this.sectionHeading($localize`:@@upstream-downloads.waiting:Waiting for an upstream`, this.waitingShown().length, this.waiting().length),
  );
  protected readonly running = computed(() => this.list()?.running ?? []);
  protected readonly runningCount = computed(() => this.list()?.runningCount ?? 0);
  protected readonly runningShown = computed(() => this.running().filter((d) => this.matches(d)));
  /** The running downloads the API counted and did not list (more than 200 on one replica). */
  protected readonly runningLeftOut = computed(() => Math.max(0, this.runningCount() - this.running().length));

  private readonly hiddenAt = computed(() => {
    const at = this.hiddenBefore();
    const ms = at === null ? NaN : Date.parse(at);
    return Number.isNaN(ms) ? null : ms;
  });
  /** The finished downloads not hidden on this device, newest first as the API orders them. */
  protected readonly finished = computed(() => {
    const before = this.hiddenAt();
    const all = this.list()?.finished ?? [];
    return before === null ? all : all.filter((d) => Date.parse(d.finishedAt) > before);
  });
  protected readonly hiddenCount = computed(() => (this.list()?.finished.length ?? 0) - this.finished().length);
  protected readonly finishedShown = computed(() => this.finished().filter((d) => this.matches(d)));

  protected readonly runningHeading = computed(() =>
    this.sectionHeading($localize`:@@upstream-downloads.running:Running`, this.runningShown().length, this.runningCount()),
  );
  protected readonly finishedHeading = computed(() =>
    this.sectionHeading($localize`:@@upstream-downloads.finished:Finished`, this.finishedShown().length, this.finished().length),
  );

  constructor() {
    effect((onCleanup) => {
      if (!this.open() || !this.page.visible()) return;
      let stopped = false;
      let timer: ReturnType<typeof setTimeout> | undefined;
      const tick = async (): Promise<void> => {
        await this.load();
        if (!stopped) timer = setTimeout(() => void tick(), this.pollMs());
      };
      untracked(() => void tick());
      onCleanup(() => {
        stopped = true;
        clearTimeout(timer);
      });
    });
  }

  private pollMs(): number {
    const s = this.list()?.pollSeconds ?? DEFAULT_POLL_SECONDS;
    return Math.max(1, s) * 1000;
  }

  private async load(): Promise<void> {
    // The formats' names read again with the list after their read failed: they stood as keys until the page was opened again (R-UI-216).
    if (this.formats.error()) this.formats.reload();
    try {
      const list = await this.api.list();
      this.list.set(list);
      this.refreshFailed.set(false);
      this.counted.emit(list.runningCount);
    } catch (e) {
      if (e instanceof ApiError && e.status === 403) {
        this.refused.emit();
        this.open.set(false);
        return;
      }
      this.refreshFailed.set(true);
    }
  }

  private sectionHeading(word: string, shown: number, total: number): string {
    return this.filtering()
      ? $localize`:@@upstream-downloads.section-filtered:${word}:section: (${shown}:shown: of ${total}:total:)`
      : `${word} (${total})`;
  }

  /** Case-insensitive, part of the text: the name, the version, the first file's path and the repository. */
  private matches(d: UpstreamDownload): boolean {
    const q = this.query();
    if (q === '') return true;
    return [d.name, d.version, d.path, `${d.repository.project}/${d.repository.key}`].some((t) => t.toLowerCase().includes(q));
  }

  protected setFilter(value: string): void {
    this.filter.set(value);
  }

  /** Hides, on this device, every finished download listed now: the newest end is kept, and a later end shows again. */
  protected hideAll(): void {
    const newest = (this.list()?.finished ?? []).reduce<string | null>(
      (max, d) => (max === null || Date.parse(d.finishedAt) > Date.parse(max) ? d.finishedAt : max),
      null,
    );
    if (newest === null || !this.storage.set('local', HIDDEN_BEFORE_KEY, newest)) return;
    this.hiddenBefore.set(newest);
  }

  protected showAgain(): void {
    this.storage.remove('local', HIDDEN_BEFORE_KEY);
    this.hiddenBefore.set(null);
  }

  /** A link inside the drawer leads away from it: the drawer closes, and the focus returns to the button. */
  protected close(): void {
    this.open.set(false);
  }

  /** The format's name beside its icon (R-UI-006): its `displayName`, the key until the formats are read. */
  protected formatLabel(key: string): string {
    return (valueOf(this.formats) ?? []).find((f) => f.name === key)?.displayName ?? key;
  }

  protected readonly subjectOf = subjectOf;
  protected readonly subjectTitle = subjectTitle;
  protected readonly originText = originText;
  protected readonly runningFigures = runningFigures;
  protected readonly finishedFigures = finishedFigures;
  protected readonly outcomeBadge = outcomeBadge;

  protected progressLabel(d: UpstreamDownloadRunning): string {
    const s = subjectOf(d);
    const subject = s.detail === '' ? s.name : `${s.name} ${s.detail}`;
    return $localize`:@@upstream-downloads.progress:Progress of ${subject}:subject:`;
  }

  protected moreText(n: number): string {
    return $localize`:@@upstream-downloads.more:and ${n}:count: more running`;
  }

  protected hiddenText(n: number): string {
    return $localize`:@@upstream-downloads.hidden:Hidden on this device: ${n}:count:.`;
  }

  protected repositoryLink(d: UpstreamDownload): readonly string[] {
    return ['/projects', d.repository.project, 'repositories', d.repository.key];
  }

  protected queueLink(d: UpstreamDownload): readonly string[] {
    return [...this.repositoryLink(d), 'downloads'];
  }

  /** A held queue in one sentence: how many wait, who limits, and until when (Q-UI-1101). */
  protected waitText(w: UpstreamDownloadWaiting): string {
    return limitQueueText({ ...w, repository: `${w.repository.project}/${w.repository.key}` });
  }

  protected waitingQueueLink(w: UpstreamDownloadWaiting): readonly string[] {
    return ['/projects', w.repository.project, 'repositories', w.repository.key, 'downloads'];
  }

  protected isFinished(d: UpstreamDownload): d is UpstreamDownloadFinished {
    return 'finishedAt' in d;
  }
}
