import { DecimalPipe } from '@angular/common';
import { Component, DestroyRef, computed, effect, inject, resource, signal } from '@angular/core';
import { RouterLink } from '@angular/router';
import { JobsApi, Task } from '../../../api/jobs-api';
import { ApiError } from '../../../api/problem';
import { DamagedBlob, DamagedBlobCopy, StorageApi, StorageConnection } from '../../../api/storage-api';
import { describeProblem } from '../../../core/http/problem-messages';
import { Notifier } from '../../../core/notifier';
import { SessionStore } from '../../../core/session-store';
import { ErrorNotice } from '../../../shared/error-notice';
import { jobStateText } from '../../../shared/job-progress';
import { asApiError, valueOf } from '../../../shared/paged-resource';
import { DataTable, TableColumn } from '../../../shared/ui/data-table';
import { Disclosure } from '../../../shared/ui/disclosure';
import { NoValue } from '../../../shared/ui/no-value';
import { TimeAgo } from '../../../shared/ui/time-ago';
import { formatSize } from '../../packages/format-size';
import { SystemRefresh } from '../system-refresh';
import { REFRESH_INTERVAL_MS } from '../system-status';
import {
  backlog,
  connectionState,
  damagedCopies,
  fill,
  hostOf,
  partlyConfigured,
  retiredConnections,
  scrub,
  shownConnections,
  storageConditions,
  summaryAt,
  unreachable,
} from './storage-model';

/** The blobs no connection holds whole that the tab lists (R-UI-196); the operator check lists every one. */
export const DAMAGED_SHOWN = 20;

/** The tasks of the stored content whose last runs the tab shows on demand, in the order they act. */
export const STORAGE_TASKS = ['storage-reconcile', 'scrub', 'orphan-sweep', 'storage-summary'] as const;

/**
 * The tab **Storage** of the page System status (api-and-ui.md R-UI-196, Q-UI-930, Q-UI-931): where Quader keeps the artifacts. The
 * plain view first: one row per object-store connection with its state in words, how full its copies are, its backlog, its damaged
 * copies and how much the scrub has read, and a line that names what needs an act. Details on demand: a row's fold with the settings
 * as the answering replica reads them, the capabilities its start check found and its storage keys by identifier; the blobs no
 * connection holds whole; the last runs of the storage's tasks. It changes no connection (the owner's decision 9); the one action is
 * **Check again** of a damaged blob for an administrator, which reads its copies again and writes no object.
 */
@Component({
  selector: 'quader-storage-page',
  imports: [DecimalPipe, RouterLink, DataTable, Disclosure, ErrorNotice, NoValue, TimeAgo],
  templateUrl: './storage-page.html',
  styleUrl: './storage-page.scss',
})
export class StoragePage {
  private readonly api = inject(StorageApi);
  private readonly jobsApi = inject(JobsApi);
  private readonly session = inject(SessionStore);
  private readonly notifier = inject(Notifier);
  private readonly pageRefresh = inject(SystemRefresh);

  protected readonly now = signal(Date.now());
  protected readonly connectionsResource = resource({ loader: () => this.api.connections() });
  protected readonly damagedResource = resource({ loader: () => this.api.damagedBlobs(DAMAGED_SHOWN) });
  protected readonly tasksResource = resource({ loader: () => this.jobsApi.tasks() });

  protected readonly formatSize = formatSize;
  protected readonly connectionState = connectionState;
  protected readonly unreachable = unreachable;
  protected readonly partlyConfigured = partlyConfigured;
  protected readonly fill = fill;
  protected readonly damagedCopies = damagedCopies;
  protected readonly scrub = scrub;
  protected readonly hostOf = hostOf;
  protected readonly jobStateText = jobStateText;

  protected readonly caption = $localize`:@@storage.connections.caption:Object-store connections in the order of the configuration`;
  protected readonly damagedCaption = $localize`:@@storage.damaged.caption:Blobs no connection holds whole`;
  protected readonly tasksCaption = $localize`:@@storage.tasks.caption:The last runs of the storage tasks`;
  protected readonly noConnection = $localize`:@@storage.connections.empty:No replica has recorded a connection yet.`;
  protected readonly columns: readonly TableColumn[] = [
    { id: 'connection', label: $localize`:@@storage.col.connection:Connection`, width: '12rem', prefer: true },
    { id: 'state', label: $localize`:@@storage.col.state:State`, width: '11rem' },
    { id: 'fill', label: $localize`:@@storage.col.fill:Fill`, width: '6.5rem' },
    { id: 'backlog', label: $localize`:@@storage.col.backlog:Backlog`, width: '7.5rem' },
    { id: 'damaged', label: $localize`:@@storage.col.damaged:Damaged copies`, width: '7rem' },
    { id: 'scrub', label: $localize`:@@storage.col.scrub:Scrub`, width: '6.5rem' },
    { id: 'encrypted', label: $localize`:@@storage.col.encrypted:Encrypted` },
  ];
  protected readonly damagedColumns = computed<readonly TableColumn[]>(() => [
    { id: 'blob', label: $localize`:@@storage.damaged.col.blob:Blob` },
    { id: 'state', label: $localize`:@@storage.damaged.col.state:State` },
    { id: 'copies', label: $localize`:@@storage.damaged.col.copies:Copies`, width: '13rem' },
    { id: 'item', label: $localize`:@@storage.damaged.col.item:Referenced by`, width: '12rem', prefer: true },
    ...(this.canRecheck() ? [{ id: 'actions', label: $localize`:@@repo.col.actions:Actions`, hideLabel: true }] : []),
  ]);
  protected readonly taskColumns: readonly TableColumn[] = [
    { id: 'task', label: $localize`:@@storage.tasks.col.task:Task` },
    { id: 'last', label: $localize`:@@storage.tasks.col.last:Last run` },
    { id: 'next', label: $localize`:@@storage.tasks.col.next:Next run` },
  ];

  protected readonly connections = computed(() => valueOf(this.connectionsResource) ?? null);
  protected readonly shown = computed(() => shownConnections(this.connections() ?? []));
  protected readonly retired = computed(() => retiredConnections(this.connections() ?? []));
  protected readonly retiredIds = computed(() => this.retired().map((c) => c.id).join(', '));
  /** One connection: the columns that matter only with several show the en dash (R-UI-196). */
  protected readonly single = computed(() => this.shown().length <= 1);
  protected readonly summaryAt = computed(() => summaryAt(this.connections() ?? []));
  protected readonly damaged = computed(() => valueOf(this.damagedResource) ?? null);
  protected readonly conditions = computed(() => {
    const list = this.connections();
    if (!list) return [];
    const d = this.damaged();
    return storageConditions(list, d ? { count: d.items.length, more: d.more } : null, this.now());
  });
  protected readonly failure = computed(() => toApiError(this.connectionsResource.error()));
  protected readonly damagedFailure = computed(() => toApiError(this.damagedResource.error()));
  protected readonly tasks = computed<readonly Task[]>(() => {
    const all = valueOf(this.tasksResource) ?? [];
    return STORAGE_TASKS.flatMap((name) => all.filter((t) => t.name === name));
  });
  protected readonly tasksFailure = computed(() => toApiError(this.tasksResource.error()));
  protected readonly canRecheck = computed(() => this.session.has('settings:write'));

  /** The connections whose fold is open. */
  protected readonly opened = signal<ReadonlySet<string>>(new Set());
  /** The copies a recheck answered, by digest, shown in place of the list's until the next refresh. */
  protected readonly rechecked = signal<ReadonlyMap<string, readonly DamagedBlobCopy[]>>(new Map());
  protected readonly checking = signal<string | null>(null);

  constructor() {
    this.pageRefresh.register(() => this.refresh(), inject(DestroyRef));
    effect((onCleanup) => {
      if (this.pageRefresh.paused()) return;
      const timer = setInterval(() => this.refresh(), REFRESH_INTERVAL_MS);
      onCleanup(() => clearInterval(timer));
    });
  }

  protected refresh(): void {
    this.now.set(Date.now());
    this.connectionsResource.reload();
    this.damagedResource.reload();
    this.tasksResource.reload();
    this.rechecked.set(new Map());
  }

  protected isOpen(c: StorageConnection): boolean {
    return this.opened().has(c.id);
  }

  protected toggle(c: StorageConnection): void {
    const next = new Set(this.opened());
    if (next.has(c.id)) next.delete(c.id);
    else next.add(c.id);
    this.opened.set(next);
  }

  protected openLabel(c: StorageConnection): string {
    return $localize`:@@storage.fold.label:Details of the connection ${c.id}:id:`;
  }

  /** The place of a connection, shown under its name: the endpoint's host and bucket/prefix. */
  protected location(c: StorageConnection): string {
    const where = `${c.bucket}/${c.prefix}`;
    const host = hostOf(c.endpoint);
    return host ? `${host} · ${where}` : where;
  }

  /** The place cut after each dot and slash, where the line may break. */
  protected locationParts(c: StorageConnection): readonly string[] {
    return this.location(c).split(/(?<=[./])/);
  }

  protected backlogOf(c: StorageConnection): ReturnType<typeof backlog> {
    return backlog(c, this.now());
  }

  protected serverSideCopies(c: StorageConnection): readonly { readonly id: string; readonly ok: boolean }[] {
    return Object.entries(c.capabilities?.serverSideCopyFrom ?? {})
      .sort(([a], [b]) => a.localeCompare(b))
      .map(([id, ok]) => ({ id, ok }));
  }

  protected periodDays(c: StorageConnection): number | null {
    return c.scrubPeriodSeconds === null ? null : Math.round(c.scrubPeriodSeconds / 86_400);
  }

  protected short(sha256: string): string {
    return sha256.slice(0, 12);
  }

  protected moreItems(b: DamagedBlob): number {
    return Math.max(0, b.itemCount - 1);
  }

  protected copiesOf(b: DamagedBlob): readonly DamagedBlobCopy[] {
    return this.rechecked().get(b.sha256) ?? b.copies;
  }

  protected copyStateText(state: string): string {
    switch (state) {
      case 'present':
        return $localize`:@@storage.copy.present:present`;
      case 'missing':
        return $localize`:@@storage.copy.missing:missing`;
      case 'corrupt':
        return $localize`:@@storage.copy.corrupt:corrupt`;
      case 'deleting':
        return $localize`:@@storage.copy.deleting:being deleted`;
    }
    return state;
  }

  protected itemLink(i: DamagedBlob['items'][number]): string[] {
    return ['/projects', i.project, 'repositories', i.repository, 'files', ...i.path.split('/')];
  }

  protected checkLabel(b: DamagedBlob): string {
    return $localize`:@@storage.damaged.check-label:Check the copies of ${this.short(b.sha256)}:digest: again`;
  }

  /** Reads the blob's copies again (R-STOR-027) and shows the states the server answered; writes no object. */
  protected async recheck(b: DamagedBlob): Promise<void> {
    this.checking.set(b.sha256);
    try {
      const out = await this.api.recheck(b.sha256);
      this.rechecked.update((m) => new Map(m).set(b.sha256, out.copies));
      this.notifier.status(
        out.contentStatus === 'ok'
          ? $localize`:@@storage.damaged.checked-ok:A connection holds ${this.short(b.sha256)}:digest: whole again.`
          : $localize`:@@storage.damaged.checked-still:No connection holds ${this.short(b.sha256)}:digest: whole yet.`,
      );
    } catch (e) {
      const err = asApiError(e);
      this.notifier.alert(describeProblem(err).text);
    } finally {
      this.checking.set(null);
    }
  }

  protected taskLink(t: Task): { kind: string } {
    return { kind: t.name };
  }
}

function toApiError(e: unknown): ApiError | null {
  return e ? asApiError(e) : null;
}
