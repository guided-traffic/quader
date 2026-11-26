import { Component, DestroyRef, ElementRef, afterRenderEffect, computed, inject, resource, signal, untracked } from '@angular/core';
import { ActivatedRoute, RouterLink } from '@angular/router';
import { newIdempotencyKey } from '../../../api/client';
import { DownloadLinkSettings } from '../../../api/download-links-api';
import { JobsApi, jobActive } from '../../../api/jobs-api';
import { StorageApi } from '../../../api/storage-api';
import { EncryptionApi } from '../../../api/system-api';
import { LIVE_FAST_MS, LIVE_SLOW_MS, live, refreshResource } from '../../../core/live-refresh';
import { Notifier } from '../../../core/notifier';
import { SessionStore } from '../../../core/session-store';
import { Needs } from '../../../shared/access/needs';
import { ErrorNotice } from '../../../shared/error-notice';
import { FailureToasts } from '../../../shared/failure-dialog';
import { asApiError, failureOf, valueOf } from '../../../shared/paged-resource';
import { TimePipe } from '../../../shared/time-pipe';
import { Busy } from '../../../shared/ui/busy';
import { Loading } from '../../../shared/ui/loading';
import { PageHeader } from '../../../shared/ui/page-header';
import { Tooltip } from '../../../shared/ui/tooltip';
import { AvailabilitySection } from './availability-section';
import { DAMAGED_SHOWN, DamagedBlobs } from './damaged-blobs';
import { ConnectionsSection } from './connections-section';
import { DownloadLinksSection } from './download-links-section';
import { EncryptionSection } from './encryption-section';
import { shownConnections } from './storage-model';
import { StorageSummary } from './storage-summary';

/** The tasks a holder of `settings:write` runs from the page (B.11): the count of the figures and the reconciliation. */
export type StorageTask = 'storage-summary' | 'storage-reconcile';

/** A run started from the page, while it runs: the page reads every `LIVE_FAST_MS` until it ends (Q-UI-1583). */
interface Run {
  readonly task: StorageTask;
  readonly job: string | null;
}

/**
 * **Admin area › Settings › Storage** (api-and-ui.md R-UI-262, ADR-316): one page for the stored content, in the order of the owner's
 * questions. The header with the time the figures were counted and **Count now**; the summary's six tiles (R-UI-266 shows the same on
 * the dashboard); **Connections** and **Availability** (R-UI-263); **Encryption** (R-UI-264); and **Download links** (R-UI-200), the
 * one setting the page changes. Every state is the server's (Q-STOR-505); the page reads the overview, the connections' list, the
 * blobs no connection holds whole and the root key's ring again every 30 s while it is seen and every 3 s while a run it started runs (R-UI-213, Q-UI-1583). It takes the
 * content panel's width (Q-UI-1580), and no list scrolls by itself (R-UI-215). An explanation is on demand: a column's or a section's
 * help, never a paragraph above a section.
 */
@Component({
  selector: 'quader-storage-page',
  imports: [
    AvailabilitySection,
    Busy,
    ConnectionsSection,
    DamagedBlobs,
    DownloadLinksSection,
    EncryptionSection,
    ErrorNotice,
    Loading,
    Needs,
    PageHeader,
    RouterLink,
    StorageSummary,
    TimePipe,
    Tooltip,
  ],
  templateUrl: './storage-page.html',
  styleUrl: './storage-page.scss',
})
export class StoragePage {
  private readonly api = inject(StorageApi);
  private readonly encryptionApi = inject(EncryptionApi);
  private readonly jobs = inject(JobsApi);
  private readonly session = inject(SessionStore);
  private readonly notifier = inject(Notifier);
  private readonly failureToasts = inject(FailureToasts);
  private readonly host = inject<ElementRef<HTMLElement>>(ElementRef);

  protected readonly overviewResource = resource({ loader: () => this.api.overview() });
  protected readonly connectionsResource = resource({ loader: () => this.api.connections() });
  protected readonly ringResource = resource({ loader: () => this.encryptionApi.status() });
  protected readonly damagedResource = resource({ loader: () => this.api.damagedBlobs(DAMAGED_SHOWN) });

  protected readonly overview = computed(() => valueOf(this.overviewResource) ?? null);
  protected readonly connections = computed(() => valueOf(this.connectionsResource) ?? null);
  protected readonly ring = computed(() => valueOf(this.ringResource) ?? null);
  protected readonly damaged = computed(() => valueOf(this.damagedResource) ?? null);
  /** The download links' settings as their section read them, for the marks Links and Bastion of the connections. */
  protected readonly links = signal<DownloadLinkSettings | null>(null);
  protected readonly shownCount = computed(() => shownConnections(this.connections() ?? []).length);

  /** The overview's failure is the page's (R-UI-216); its notice's Retry reads it again. */
  protected readonly failure = failureOf(this.overviewResource);
  protected readonly connectionsFailure = failureOf(this.connectionsResource);
  protected readonly damagedFailure = failureOf(this.damagedResource);

  protected readonly canWrite = computed(() => this.session.has('settings:write'));
  protected readonly now = signal(Date.now());
  /** The runs started here that have not ended. */
  protected readonly runs = signal<readonly Run[]>([]);
  protected readonly starting = signal<StorageTask | null>(null);
  private readonly keys = new Map<StorageTask, string>();

  protected readonly connectionsHelp = $localize`:@@storage.connections.help:Set in config.storage.connections and only shown here. The manual says how to add or remove one.`;
  protected readonly availabilityHelp = $localize`:@@storage.availability.help:Every connection without drain holds every blob. A blob whose only copy is lost is unavailable until a copy returns.`;
  protected readonly encryptionHelp = $localize`:@@storage.encryption.help:Quader's own encryption is set per connection (encrypted). The object store's server-side encryption is the store's.`;

  constructor() {
    // Read again while the page is seen, fast while a run started here runs (R-UI-213, Q-UI-1583).
    live({ reload: () => this.refresh(), every: () => (this.runs().length > 0 ? LIVE_FAST_MS : LIVE_SLOW_MS) });
    // A tile of the dashboard leads here with its section as the fragment: the page shows it once the section stands.
    const fragment = inject(ActivatedRoute).snapshot.fragment;
    let moved = false;
    afterRenderEffect(() => {
      if (moved || !fragment || !this.connections()) return;
      const target = untracked(() => this.host.nativeElement.querySelector<HTMLElement>(`#${CSS.escape(fragment)}`));
      if (!target) return;
      moved = true;
      this.goTo(fragment);
    });
    inject(DestroyRef).onDestroy(() => this.keys.clear());
  }

  /** Reads the overview, the connections and the ring again in place, and the jobs of the runs started here. */
  protected async refresh(): Promise<void> {
    this.now.set(Date.now());
    await Promise.all([
      refreshResource(this.overviewResource, () => this.api.overview()),
      refreshResource(this.connectionsResource, () => this.api.connections()),
      refreshResource(this.ringResource, () => this.encryptionApi.status()),
      refreshResource(this.damagedResource, () => this.api.damagedBlobs(DAMAGED_SHOWN)),
      this.followRuns(),
    ]);
  }

  /** The page notice's Retry: every read that failed. */
  protected retry(): void {
    this.overviewResource.reload();
    if (this.connectionsResource.error()) this.connectionsResource.reload();
    if (this.ringResource.error()) this.ringResource.reload();
    if (this.damagedResource.error()) this.damagedResource.reload();
  }

  protected running(task: StorageTask): boolean {
    return this.starting() === task || this.runs().some((r) => r.task === task);
  }

  /** Moves to a section of the page and puts the focus on its heading (Q-UI-1581). */
  protected goTo(section: string): void {
    const target = this.host.nativeElement.querySelector<HTMLElement>(`#${CSS.escape(section)}`);
    if (!target) return;
    target.scrollIntoView({ block: 'start' });
    target.querySelector<HTMLElement>('h2')?.focus({ preventScroll: true });
  }

  /** **Count now** and **Run the reconciliation now** (B.11): the existing task, audited as `system.task_triggered`. */
  protected async run(task: StorageTask): Promise<void> {
    if (this.running(task)) return;
    this.starting.set(task);
    const key = this.keys.get(task) ?? newIdempotencyKey();
    this.keys.set(task, key);
    try {
      const job = await this.jobs.runTask(task, undefined, key);
      this.keys.delete(task);
      this.runs.update((r) => [...r, { task, job: job.id }]);
    } catch (e) {
      const error = asApiError(e);
      if (error.status !== 0 && error.status !== 503) this.keys.delete(task);
      this.failureToasts.alert(error);
    } finally {
      this.starting.set(null);
    }
  }

  /** Drops the runs whose job ended, and says so: the figures read with them are the run's. */
  private async followRuns(): Promise<void> {
    const runs = untracked(() => this.runs());
    if (runs.length === 0) return;
    const ended: Run[] = [];
    await Promise.all(
      runs.map(async (r) => {
        if (!r.job) return;
        try {
          const job = await this.jobs.job(r.job);
          if (!jobActive(job.state)) ended.push(r);
        } catch {
          ended.push(r);
        }
      }),
    );
    if (ended.length === 0) return;
    this.runs.update((list) => list.filter((r) => !ended.includes(r)));
    if (ended.some((r) => r.task === 'storage-summary')) {
      this.notifier.status($localize`:@@storage.counted:The figures are counted again.`);
      await refreshResource(this.overviewResource, () => this.api.overview());
      await refreshResource(this.connectionsResource, () => this.api.connections());
    }
  }
}
