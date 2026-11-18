import { DatePipe, DecimalPipe, NgTemplateOutlet } from '@angular/common';
import { Component, computed, effect, inject, resource, signal } from '@angular/core';
import { RouterLink } from '@angular/router';
import { BackupsApi } from '../../api/backups-api';
import { EncryptionApi, SystemApi } from '../../api/system-api';
import { ApiError } from '../../api/problem';
import { SessionStore } from '../../core/session-store';
import { ErrorNotice } from '../../shared/error-notice';
import { valueOf } from '../../shared/paged-resource';
import { formatSize } from '../packages/format-size';
import { CheckSummary, checkSummary, lastBackup, roleLoad, share } from './dashboard-model';
import { REFRESH_INTERVAL_MS } from './system-status';
import { alarms, roleGroups, serviceStatus } from './system-status-model';
import { PageHeader } from '../../shared/ui/page-header';
import { TimeAgo } from '../../shared/ui/time-ago';

/**
 * The admin dashboard (Q-OPS-175): the first screen of the admin area, an overview of the installation in cards.
 * The instance bar, the components (each role's replicas and their load from the heartbeat measurements), the
 * dependencies (PostgreSQL, Valkey, the object store with the content it holds, each with its readiness check), and
 * the counts of content and access with the way to their screens, and the state of the backup (R-UI-165). It refreshes like the
 * status screen, with a pause.
 */
@Component({
  selector: 'quader-dashboard',
  imports: [PageHeader, DatePipe, DecimalPipe, NgTemplateOutlet, RouterLink, ErrorNotice, TimeAgo],
  templateUrl: './dashboard.html',
  styleUrl: './dashboard.scss',
})
export class Dashboard {
  private readonly system = inject(SystemApi);
  private readonly encryptionApi = inject(EncryptionApi);
  private readonly backups = inject(BackupsApi);
  protected readonly session = inject(SessionStore);
  protected readonly size = formatSize;
  /** A resource's value without the throw of its error state, which would stop the whole template. */
  protected readonly valueOf = valueOf;

  protected readonly info = resource({ loader: () => this.system.info() });
  protected readonly replicas = resource({ loader: () => this.system.replicas() });
  protected readonly overview = resource({ loader: () => this.system.overview() });
  protected readonly encryption = resource({ loader: () => this.encryptionApi.status() });
  /** The Backups tab's own summary; refused or failing, the card leaves the backup out (R-UI-165). */
  protected readonly backup = resource({ loader: () => this.backups.summary() });
  protected readonly lastBackup = computed(() => {
    const summary = valueOf(this.backup);
    return summary ? lastBackup(summary) : null;
  });
  protected readonly paused = signal(false);
  /** The repository classes in the order the repository screens name them. */
  protected readonly classes = [
    { key: 'local', label: $localize`:@@dashboard.class.local:Local` },
    { key: 'remote', label: $localize`:@@dashboard.class.remote:Remote` },
    { key: 'virtual', label: $localize`:@@dashboard.class.virtual:Virtual` },
  ] as const;

  private readonly groups = computed(() => {
    const info = valueOf(this.info);
    const replicas = valueOf(this.replicas);
    return info && replicas ? roleGroups(info, replicas) : [];
  });
  /**
   * The instance bar's state, from the same rule as the header of the status screen (R-UI-038): the worst condition
   * that is an error or a warning, else "all roles ready"; and whether replicas drain or start, which only informs
   * (Q-UI-600 to Q-UI-602).
   */
  protected readonly status = computed(() => {
    const replicas = valueOf(this.replicas);
    if (!replicas || !valueOf(this.info)) return null;
    const st = serviceStatus(this.groups(), replicas);
    return { first: alarms(st)[0] ?? null, replacing: st.conditions.some((c) => c.kind === 'replacing') };
  });
  /**
   * One card per role. A live replica of the development role `all` serves every role, so the main roles it covers
   * are not shown as missing next to it.
   */
  protected readonly loads = computed(() => {
    const loads = this.groups().map(roleLoad);
    const all = loads.some((l) => l.role === 'all' && l.live > 0);
    return all ? loads.filter((l) => l.role === 'all' || l.live > 0) : loads;
  });
  protected readonly checks = computed(() => {
    const replicas = valueOf(this.replicas) ?? [];
    return {
      database: checkSummary(replicas, 'database'),
      valkey: checkSummary(replicas, 'valkey'),
      objectStore: checkSummary(replicas, 'objectStore'),
    };
  });
  protected readonly pool = computed(() => {
    const pools = valueOf(this.info)?.database?.pools ?? {};
    const all = Object.values(pools);
    const inUse = all.reduce((a, p) => a + p.inUse, 0);
    const max = all.reduce((a, p) => a + p.max, 0);
    return { inUse, max, share: share(inUse, max) };
  });
  protected readonly failure = computed(() => {
    const e = this.info.error() ?? this.replicas.error();
    return e ? (e instanceof ApiError ? e : new ApiError(0, null, null)) : null;
  });
  protected readonly overviewFailure = computed(() => {
    const e = this.overview.error();
    return e ? (e instanceof ApiError ? e : new ApiError(0, null, null)) : null;
  });

  constructor() {
    effect((onCleanup) => {
      if (this.paused()) return;
      const timer = setInterval(() => this.reload(), REFRESH_INTERVAL_MS);
      onCleanup(() => clearInterval(timer));
    });
  }

  protected reload(): void {
    this.info.reload();
    this.replicas.reload();
    this.overview.reload();
    this.encryption.reload();
    this.backup.reload();
  }

  protected togglePause(): void {
    this.paused.update((p) => !p);
  }

  /** A bar's width in percent, or null without a known whole. */
  protected percent(part: number, whole: number | null): number | null {
    const s = share(part, whole);
    return s === null ? null : Math.round(s * 100);
  }

  protected checkState(c: CheckSummary): 'ok' | 'bad' | 'none' {
    return c.reporting === 0 ? 'none' : c.failing > 0 ? 'bad' : 'ok';
  }
}
