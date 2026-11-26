import { TimePipe } from '../../shared/time-pipe';
import { Component, DestroyRef, computed, effect, inject, resource, signal, viewChild } from '@angular/core';
import { live } from '../../core/live-refresh';
import { newIdempotencyKey } from '../../api/client';
// >>> M1 ui-a
import { Job, JobsApi } from '../../api/jobs-api';
// <<< M1 ui-a
import { ApiError } from '../../api/problem';
import { EncryptionApi, SystemApi } from '../../api/system-api';
import { Notifier } from '../../core/notifier';
import { SessionStore } from '../../core/session-store';
import { ErrorNotice } from '../../shared/error-notice';
// >>> M1 ui-a
import { JobProgress } from '../../shared/job-progress';
import { FailureToasts } from '../../shared/failure-dialog';
import { JobTable } from './job-table';
import { TasksCard } from './tasks-card';
// <<< M1 ui-a
import {
  alarms,
  degradedChecks,
  failingChecks,
  keyStatus,
  mixedVersions,
  notes,
  PoolGroup,
  RingGap,
  ringGaps,
  RoleGroup,
  roleGroups,
  rotationStep,
  serviceStatus,
  ValkeyReasonCode,
  valkeyReasonText,
  valkeySummary,
} from './system-status-model';
import { failureOf, valueOf } from '../../shared/paged-resource';
import { DataTable, TableColumn } from '../../shared/ui/data-table';
import { ScannerReplicas } from '../../shared/scanner/scanner-replicas';
import { databaseLag, scannerGroups, waitingScanners } from '../../shared/scanner/scanner-model';
import { NoValue } from '../../shared/ui/no-value';
import { TechnicalDetails } from '../../shared/technical-details';
import { SystemRefresh } from './system-refresh';
import { Loading } from '../../shared/ui/loading';
import { Busy } from '../../shared/ui/busy';

/** Automatic refresh interval with a pause control (R-UI-038, WCAG 2.2.2). */
export const REFRESH_INTERVAL_MS = 15_000;

// >>> M1 ui-a
/** Jobs shown on the status screen; the jobs screen lists them all. */
export const RECENT_JOBS = 10;
// <<< M1 ui-a

/**
 * Replicas disagreeing about the Valkey primary for longer than this are reported. The spec
 * bounds it by `valkey.sentinel.refresh_interval` plus one heartbeat, which the UI cannot read;
 * this is a fixed upper bound of both defaults (Q-UI-12).
 */
export const PRIMARY_DISAGREEMENT_GRACE_MS = 30_000;

/**
 * The tab Status of the page System status (R-UI-038, R-UI-164): installation facts, replicas per role and the scanner replicas per
 * kind (R-OPS-048, Q-UI-897), Valkey, stored-secret keys, the scheduled tasks and the recent jobs. The page's header refreshes it and pauses its timer (Q-UI-651).
 */
@Component({
  selector: 'quader-system-status',
  imports: [Busy, Loading, DataTable, TimePipe, ErrorNotice, JobProgress, JobTable, NoValue, ScannerReplicas, TasksCard, TechnicalDetails],
  templateUrl: './system-status.html',
  styleUrl: './system-status.scss',
})
export class SystemStatus {
  /** The first twelve characters of a source revision; the full hash is its title (Q-UI-570). */
  protected shortRevision(revision: string): string {
    return /^[0-9a-f]{13,}$/.test(revision) ? revision.slice(0, 12) : revision;
  }

  /** The replicas of a role or pool in the shared data table, which stacks where its columns do not fit (no sideways scroll at 390 px). */
  protected readonly replicaColumns: readonly TableColumn[] = [
    { id: 'name', label: $localize`:@@system.col.name:Name`, width: '10rem' },
    { id: 'node', label: $localize`:@@system.col.node:Node`, width: '6rem' },
    { id: 'version', label: $localize`:@@system.col.version:Version`, width: '5rem' },
    { id: 'state', label: $localize`:@@system.col.state:State` },
    { id: 'heartbeat', label: $localize`:@@system.col.heartbeat:Heartbeat`, width: '6rem' },
    { id: 'valkey', label: $localize`:@@system.col.valkey:Valkey`, width: '7rem' },
  ];
  /** The key tables in the shared data table too: the canary's header names a replica, which pushed the page at 390 px. */
  protected readonly keysCaption = $localize`:@@system.keys.caption:Key IDs and sealed values`;
  protected readonly keyColumns = computed<readonly TableColumn[]>(() => {
    const by = valueOf(this.encryption)?.answeredBy ?? '';
    return [
      { id: 'id', label: $localize`:@@system.keys.col.id:Key ID`, width: '5rem' },
      { id: 'sealed', label: $localize`:@@system.keys.col.sealed:Sealed values`, width: '6rem' },
      { id: 'canary', label: $localize`:@@system.keys.col.canary-by:Canary (reported by ${by}:replica:)`, width: '8rem' },
      { id: 'status', label: $localize`:@@system.keys.col.status:Status` },
    ];
  });
  protected readonly keyRingsCaption = $localize`:@@system.keys.replicas.caption:Key rings of the live replicas`;
  protected readonly keyRingColumns: readonly TableColumn[] = [
    { id: 'name', label: $localize`:@@system.col.name:Name`, width: '10rem' },
    { id: 'role', label: $localize`:@@system.keys.replicas.role:Role`, width: '4rem' },
    { id: 'active', label: $localize`:@@system.keys.active:Active key ID`, width: '6rem' },
    { id: 'ring', label: $localize`:@@system.keys.replicas.ring:Key IDs held`, width: '6rem' },
    { id: 'state', label: $localize`:@@system.col.state:State` },
  ];
  protected replicasCaption(role: string, pool: string | null): string {
    return pool === null
      ? $localize`:@@system.replicas.caption:Replicas of the role ${role}:role:`
      : $localize`:@@system.replicas.caption-pool:Replicas of the pool ${pool}:pool: of the role ${role}:role:`;
  }
  protected readonly valueOf = valueOf;
  protected readonly valkeyReason = valkeyReasonText;
  protected valkeyReasons(reasons: readonly NonNullable<ValkeyReasonCode>[]): string {
    return reasons.map(valkeyReasonText).join(', ');
  }
  private readonly system = inject(SystemApi);
  private readonly encryptionApi = inject(EncryptionApi);
  private readonly notifier = inject(Notifier);
  private readonly failureToasts = inject(FailureToasts);
  private readonly session = inject(SessionStore);
  private readonly refresh = inject(SystemRefresh);
  // >>> M1 ui-a
  private readonly jobsApi = inject(JobsApi);
  private readonly tasksCard = viewChild(TasksCard);
  // <<< M1 ui-a

  protected readonly info = resource({ loader: () => this.system.info() });
  protected readonly replicas = resource({ loader: () => this.system.replicas() });
  protected readonly encryption = resource({ loader: () => this.encryptionApi.status() });
  /** The kinds of scanner and the scanner replicas (R-OPS-048, Q-UI-897): one more kind of row of the replicas, after the roles. */
  protected readonly scanners = resource({ loader: () => this.system.scanners() });
  protected readonly scannerGroups = computed(() => {
    const s = valueOf(this.scanners);
    return s ? scannerGroups(s) : [];
  });
  /** The scanner replicas live and ready, the counts of the group's heading. */
  protected readonly scannerCounts = computed(() => ({
    live: this.scannerGroups().reduce((n, g) => n + g.live, 0),
    ready: this.scannerGroups().reduce((n, g) => n + g.ready, 0),
  }));
  /** The kinds whose replicas report themselves and none of which is ready: the header warns that their scans wait. */
  protected readonly waitingScanners = computed(() => waitingScanners(this.scannerGroups()));
  /** The ready image scanners and how many of them hold an older database or none (Q-UI-915). */
  protected readonly databaseLag = databaseLag;
  protected readonly scannersFailure = failureOf(this.scanners);

  // >>> M1 ui-a
  protected readonly recentJobs = resource({
    loader: async () => (await this.jobsApi.jobs({})(null, RECENT_JOBS)).items,
  });
  protected readonly recentFailure = failureOf(this.recentJobs);
  /** The job of a re-encryption started from the key card; followed until it ends. */
  protected readonly reencryptJob = signal<string | null>(null);
  protected readonly gaps = computed(() => {
    const e = valueOf(this.encryption);
    return e ? ringGaps(e) : [];
  });
  protected readonly step = computed(() => {
    const e = valueOf(this.encryption);
    return e ? rotationStep(e) : null;
  });
  // <<< M1 ui-a
  protected readonly reencrypting = signal(false);
  private disagreeSince: number | null = null;

  /** The page's failure, kept while it reads again (R-UI-216); its notice's Retry reads the whole page again (`reload`). */
  protected readonly failure = failureOf([this.info, this.replicas]);
  protected readonly encryptionFailure = failureOf(this.encryption);

  protected readonly groups = computed(() => {
    const i = valueOf(this.info);
    const r = valueOf(this.replicas);
    return i && r ? roleGroups(i, r) : [];
  });
  /** The header's statements (R-UI-038): what is wrong first, then what only informs (Q-UI-600 to Q-UI-603). */
  protected readonly status = computed(() => {
    const r = valueOf(this.replicas);
    return r && valueOf(this.info) ? serviceStatus(this.groups(), r) : null;
  });
  protected readonly alarms = alarms;
  protected readonly notes = notes;
  protected readonly versions = computed(() => {
    const i = valueOf(this.info);
    return i ? mixedVersions(i) : [];
  });
  protected readonly valkey = computed(() => {
    const i = valueOf(this.info);
    const r = valueOf(this.replicas);
    return i && r ? valkeySummary(i, r) : null;
  });
  /** Disagreement that outlasted the grace period. */
  protected readonly primariesDisagree = signal(false);

  protected readonly schemaSupported = computed(() => {
    const db = valueOf(this.info)?.database;
    return db ? db.supportedSchemaVersions.includes(db.schemaVersion) : true;
  });
  /** Re-encryption is an administrator's task (R-API-049); `content:bypass` is the admin role's. */
  protected readonly canReencrypt = computed(() => this.session.has('content:bypass'));

  constructor() {
    this.refresh.register(() => this.reload(), inject(DestroyRef));
    // Read again while the page is seen and not paused, at once when it is seen again (R-UI-213).
    live({ reload: () => this.reload(), every: () => REFRESH_INTERVAL_MS, when: () => !this.refresh.paused() });
    effect(() => {
      const v = this.valkey();
      const now = Date.now();
      if (!v?.primariesDisagree) {
        this.disagreeSince = null;
        this.primariesDisagree.set(false);
        return;
      }
      this.disagreeSince ??= now;
      this.primariesDisagree.set(now - this.disagreeSince >= PRIMARY_DISAGREEMENT_GRACE_MS);
    });
  }

  protected reload(): void {
    this.info.reload();
    this.replicas.reload();
    this.scanners.reload();
    this.encryption.reload();
    // >>> M1 ui-a
    this.recentJobs.reload();
    this.tasksCard()?.reload();
    // <<< M1 ui-a
  }

  /** The id of the heading of a sub-group: the role's main Deployment, or one of its pools. */
  protected headingId(g: RoleGroup, p: PoolGroup): string {
    return p.pool === null ? 'role-' + g.role + '-main' : 'role-' + g.role + '-pool-' + p.pool;
  }
  protected failing = failingChecks;
  protected degraded = degradedChecks;
  protected keyStatus = keyStatus;

  protected async reencrypt(): Promise<void> {
    this.reencrypting.set(true);
    try {
      // >>> M1 ui-a
      this.reencryptJob.set(await this.encryptionApi.reencrypt(newIdempotencyKey()));
      // <<< M1 ui-a
      this.notifier.status($localize`:@@system.reencrypt.started:Re-encryption started.`);
      this.encryption.reload();
    } catch (e) {
      this.failureToasts.alert(e, e instanceof ApiError ? undefined : $localize`:@@system.reencrypt.failed:Re-encryption could not be started.`);
    } finally {
      this.reencrypting.set(false);
    }
  }

  // >>> M1 ui-a
  protected gapOf(replica: string): RingGap | undefined {
    return this.gaps().find((g) => g.replica === replica);
  }

  protected reencryptEnded(): void {
    this.reencryptJob.set(null);
    this.encryption.reload();
    this.recentJobs.reload();
  }

  protected async cancelJob(job: Job): Promise<void> {
    try {
      await this.jobsApi.cancel(job.id);
      this.notifier.status($localize`:@@jobs.cancelling:Job ${job.kind}:kind: is being cancelled.`);
    } catch (e) {
      this.failureToasts.alert(e, e instanceof ApiError ? undefined : $localize`:@@jobs.cancel.failed:The job could not be cancelled.`);
    } finally {
      this.recentJobs.reload();
    }
  }

  protected readonly recentCaption = $localize`:@@system.jobs.caption:Recent jobs`;
  protected readonly reencryptLabel = $localize`:@@system.reencrypt.progress:Re-encryption`;
  // <<< M1 ui-a

  protected checkText(name: string): string {
    switch (name) {
      case 'objectStore':
        return $localize`:@@system.check.objectStore:Object store: blob requests on this replica fail with a retryable 503; everything else keeps working.`;
      case 'valkey':
        return $localize`:@@system.check.valkey:Valkey: no request fails, but remote fetches are collapsed per replica only, the negative cache is per replica, rate limits fall back to per-replica shares, and changes arrive through the change-log poll.`;
      case 'vulnScannerTrivy':
        return $localize`:@@system.check.vulnScannerTrivy:Image scanning: the Trivy scanner of this replica cannot analyse images (its analyser is missing or does not run, its certificate did not load, or its working directory is not writable), so new images get no vulnerability results until it works again.`;
      case 'malwareScannerClamav':
        return $localize`:@@system.check.malwareScannerClamav:Malware scan: this worker has no ClamAV scanner to call (malware.scanner.url is empty), so new artifacts stay unchecked.`;
      default:
        return name;
    }
  }
}
