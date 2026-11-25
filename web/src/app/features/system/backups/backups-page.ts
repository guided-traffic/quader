import { DocumentBlock } from '../../../shared/document-block';
import { DatePipe } from '@angular/common';
import { Component, DestroyRef, computed, effect, inject, input, resource, signal, untracked } from '@angular/core';
import { live } from '../../../core/live-refresh';
import { Router, RouterLink } from '@angular/router';
import {
  BACKUP_OPERATIONS,
  BACKUP_STATES,
  BackupRun,
  BackupsApi,
} from '../../../api/backups-api';
import { describeBackupProblem } from '../../../core/http/problem-messages-backup';
import { SelectField, SelectOption } from '../../../shared/access/fields';
import { ErrorNotice } from '../../../shared/error-notice';
import { EmptyState } from '../../../shared/ui/empty-state';
import { FilterPanel } from '../../../shared/ui/filter-panel';
import { ListChip, ListHead } from '../../../shared/ui/list-head';
import { NoValue } from '../../../shared/ui/no-value';
import { PagedResource, failureOf, valueOf } from '../../../shared/paged-resource';
import { formatSize } from '../../packages/format-size';
import { REFRESH_INTERVAL_MS } from '../system-status';
import { BackupState } from './backup-state';
import { durationText } from '../../../shared/duration-text';
import {
  errorCode,
  operationText,
  phaseText,
  resultNumber,
  resultWarnings,
  runSeconds,
  stateView,
} from './backups-model';
import { ManualRunPanel } from './manual-run-panel';
import { SystemRefresh } from '../system-refresh';

/** Runs per page of the list; the automatic refresh keeps the first page current and leaves a longer list alone. */
export const RUNS_PAGE_SIZE = 25;

/**
 * The tab Backups of the page System status (R-UI-048, R-UI-164, R-API-050): the status of the backup job, the run in progress,
 * the runs, the stored backups the newest listing run found, the command for a manual run and, after a restore, where this
 * installation came from. The screen only reads: every request it sends is a GET. It refreshes with the status screen's
 * interval; the page's header pauses and refreshes it (Q-UI-651). The runs' filters are the head of every list: **Filters**
 * opens the drawer, the filters that are set stand beside it as chips (Q-UI-566).
 */
@Component({
  selector: 'quader-backups-page',
  imports: [DocumentBlock, DatePipe, RouterLink, EmptyState, ErrorNotice, FilterPanel, ListHead, NoValue, SelectField, BackupState, ManualRunPanel],
  templateUrl: './backups-page.html',
  styleUrl: './backups-page.scss',
})
export class BackupsPage {
  private readonly api = inject(BackupsApi);
  private readonly router = inject(Router);
  private readonly pageRefresh = inject(SystemRefresh);

  /** Query parameters of the run filters. */
  readonly operation = input<string | undefined>();
  readonly state = input<string | undefined>();

  protected readonly operations = BACKUP_OPERATIONS;
  protected readonly states = BACKUP_STATES;
  protected readonly operationText = operationText;
  protected readonly phaseText = phaseText;
  protected readonly durationText = durationText;
  protected readonly formatSize = formatSize;
  protected readonly describeBackupProblem = describeBackupProblem;
  protected readonly errorCode = errorCode;
  protected readonly resultNumber = resultNumber;
  protected readonly resultWarnings = resultWarnings;
  protected readonly caption = $localize`:@@backups.runs.caption:Backup runs, newest first`;
  protected readonly blobsLabel = $localize`:@@backups.active.blobs-label:Blobs copied`;
  protected readonly storedCaption = $localize`:@@backups.stored.caption:Complete backups found in the target`;

  protected readonly now = signal(Date.now());
  /** The drawer's values; they reach the address with **Apply filters** and go back to it when the drawer closes without. */
  protected readonly operationDraft = signal('');
  protected readonly stateDraft = signal('');
  protected readonly panelOpen = signal(false);
  protected readonly noRun = $localize`:@@backups.runs.empty-none:There is no backup run yet.`;
  protected readonly noMatch = $localize`:@@backups.runs.empty:No backup run matches.`;
  protected readonly operationOptions: readonly SelectOption[] = [
    { value: '', label: $localize`:@@backups.filter.any:Any` },
    ...BACKUP_OPERATIONS.map((o) => ({ value: o, label: operationText(o) })),
  ];
  protected readonly stateOptions: readonly SelectOption[] = [
    { value: '', label: $localize`:@@backups.filter.any:Any` },
    ...BACKUP_STATES.map((st) => ({ value: st, label: stateView(st, false).text })),
  ];
  protected readonly selectedId = signal<string | null>(null);

  protected readonly runs = new PagedResource<BackupRun>(RUNS_PAGE_SIZE);

  /** The summary and, while a run is active, that run with its progress: one load, so the two never disagree. */
  protected readonly status = resource({
    loader: async () => {
      const summary = await this.api.summary();
      const active = summary.active ? await this.api.run(summary.active.id).catch(() => null) : null;
      return { summary, active };
    },
  });
  protected readonly detail = resource({
    params: () => this.selectedId() ?? undefined,
    loader: ({ params }) => this.api.run(params),
  });

  protected readonly summary = computed(() => valueOf(this.status)?.summary ?? null);
  protected readonly activeRun = computed(() => valueOf(this.status)?.active ?? null);
  /** The status's failure, kept while it reads again (R-UI-216); its notice's Retry reads the page again (`refresh`). */
  protected readonly statusFailure = failureOf(this.status);
  protected readonly detailFailure = failureOf(this.detail);
  protected readonly detailRun = computed(() => valueOf(this.detail) ?? null);

  /** The age of the last success, against the clock that ticks with the refresh. */
  protected readonly lastSuccessAge = computed(() => {
    const at = this.summary()?.lastSuccessAt;
    return at ? durationText((this.now() - Date.parse(at)) / 1000) : null;
  });
  protected readonly lastFailureText = computed(() => {
    const failure = this.summary()?.lastFailure;
    if (!failure) return null;
    return failure.code ? (describeBackupProblem(failure.code) ?? null) : null;
  });
  protected readonly storedAsOf = computed(() => this.summary()?.storedBackups ?? null);

  private readonly appliedOperation = computed(() => (this.operations as readonly string[]).includes(this.operation() ?? '') ? this.operation()! : '');
  private readonly appliedState = computed(() => (this.states as readonly string[]).includes(this.state() ?? '') ? this.state()! : '');

  /** The filters in the address as the head's chips (Q-UI-422). */
  protected readonly chips = computed<readonly ListChip[]>(() => {
    const out: ListChip[] = [];
    const operation = this.appliedOperation();
    if (operation) out.push({ key: 'operation', label: $localize`:@@backups.filter.operation:Operation`, value: operationText(operation) });
    const state = this.appliedState();
    if (state) out.push({ key: 'state', label: $localize`:@@backups.filter.state:State`, value: this.stateName(state) });
    return out;
  });

  constructor() {
    this.pageRefresh.register(() => this.refresh(), inject(DestroyRef));
    effect(() => {
      this.appliedOperation();
      this.appliedState();
      untracked(() => {
        this.restage();
        this.loadRuns();
      });
    });
    // Read again while the page is seen and not paused, at once when it is seen again (R-UI-213).
    live({ reload: () => this.refresh(), every: () => REFRESH_INTERVAL_MS, when: () => !this.pageRefresh.paused() });
  }

  private loadRuns(): void {
    void this.runs.start(
      this.api.runs({ operation: this.appliedOperation(), state: this.appliedState() }),
    );
  }

  /** Reloads the status and the open run, and the first page of the list unless the user has loaded further pages. */
  protected refresh(): void {
    this.now.set(Date.now());
    this.status.reload();
    if (this.selectedId() !== null) this.detail.reload();
    // The runs shown are read again in place: the rows, the open run and what **Load more** added stay (R-UI-213).
    void this.runs.refresh().catch(() => undefined);
  }

  /**
   * The runs' notice's Retry (R-UI-216): a first page that failed is read again from the start, a **Load more** that failed is
   * loaded again. `refresh` reads only a list that has its first page.
   */
  protected retryRuns(): void {
    void (this.runs.loaded() ? this.runs.more() : this.runs.reload());
  }

  /** The drawer's fields back to the filters the list shows. */
  protected restage(): void {
    this.operationDraft.set(this.appliedOperation());
    this.stateDraft.set(this.appliedState());
  }

  protected apply(): void {
    this.navigate({ operation: this.operationDraft() || null, state: this.stateDraft() || null });
  }

  protected removeChip(chip: ListChip): void {
    this.navigate({ [chip.key]: null });
  }

  protected clear(): void {
    this.navigate({ operation: null, state: null });
  }

  private navigate(queryParams: Record<string, string | null>): void {
    void this.router.navigate([], { queryParams, queryParamsHandling: 'merge' });
  }

  protected open(run: BackupRun): void {
    this.selectedId.set(run.id);
  }

  protected close(): void {
    this.selectedId.set(null);
  }

  protected duration(run: BackupRun): string {
    const seconds = runSeconds(run, this.now());
    return seconds === null ? '' : durationText(seconds);
  }

  protected databaseBytes(run: BackupRun): string {
    const bytes = resultNumber(run, 'databaseBytes');
    return bytes === null ? '' : formatSize(bytes);
  }

  protected blobsCopied(run: BackupRun): string {
    const copied = run.progress.blobsCopied;
    return copied === undefined ? '' : String(copied);
  }

  /** The pretty-printed result of an open run. */
  protected readonly copyResult = $localize`:@@backups.copy-result:Copy the result`;

  protected stateName(state: string): string {
    return stateView(state, false).text;
  }

  protected detailLabel(run: BackupRun): string {
    return $localize`:@@backups.details.label:Details of the run of pod ${run.pod}:pod:`;
  }
}
