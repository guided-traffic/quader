import { formatDate } from '@angular/common';
import { ChangeDetectionStrategy, Component, LOCALE_ID, computed, effect, inject, input, signal, untracked } from '@angular/core';
import { LIVE_IDLE_MS, live } from '../../core/live-refresh';
import { Router } from '@angular/router';
import { LimitsApi, UsageDetail, UsagePeriod } from '../../api/limits-api';
import { ApiError } from '../../api/problem';
import { ErrorNotice } from '../../shared/error-notice';
import { asApiError } from '../../shared/paged-resource';
import { PageHeader } from '../../shared/ui/page-header';
import { ProfileNav } from '../profile/profile-nav';
import { LimitExplain } from './limit-explain';
import { UsageBudgets } from './usage-budgets';
import { blockedUntil } from './usage-detail';
import { UsageHistory } from './usage-history';
import { UsageByCredentialTable, UsageByRepositoryTable } from './usage-tables';
import { PERIODS, periodLabel, periodPhrase } from './usage-words';

/** True where the caller used nothing in the 24 hours of the history. */
export function nothingInDay(d: UsageDetail): boolean {
  return d.history.every((p) => p.requests === 0 && p.cachedBytes === 0 && p.uncachedBytes === 0);
}

/**
 * The profile's tab **Usage** (R-UI-185, Q-UI-735): every signed-in user's own limits per budget key ("Your limits come from …"),
 * which limit applies in a repository it can read, the last 24 hours, by repository and by credential. A block is shown without
 * an action; in Audit a line says that limits are counted, not applied yet.
 */
@Component({
  selector: 'quader-profile-usage',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [ErrorNotice, LimitExplain, PageHeader, ProfileNav, UsageBudgets, UsageByCredentialTable, UsageByRepositoryTable, UsageHistory],
  template: `
    <quader-page-header heading="Usage" i18n-heading="@@profile.usage.heading" description="What you downloaded, and the limits that apply to you." i18n-description="@@profile.usage.description">
      <div actions class="period" role="group" i18n-aria-label="@@usage.period" aria-label="Period">
        @for (p of periods; track p) {
          <button class="btn btn-default" type="button" [attr.aria-pressed]="activePeriod() === p" (click)="setPeriod(p)">{{ periodText(p) }}</button>
        }
      </div>
    </quader-page-header>
    <quader-profile-nav />

    @if (failure(); as e) {
      <quader-error-notice [error]="e" (retry)="load()" />
    } @else if (detail(); as d) {
      @if (blocked(); as until) {
        <p class="notice-block" role="status">
          <span class="material-icons bad" aria-hidden="true">block</span>
          <span>{{ blockText(until) }}</span>
        </p>
      }
      @if (d.mode === 'audit') {
        <p class="mode-note" i18n="@@profile.usage.audit">Limits are counted, not applied yet.</p>
      }
      @if (empty()) {
        <p class="hint" i18n="@@profile.usage.empty">You have not downloaded anything in the last 24 hours.</p>
      }

      <section class="section" aria-labelledby="own-limits">
        <h2 id="own-limits" i18n="@@profile.usage.limits">Your limits</h2>
        <quader-usage-budgets [budgets]="d.budgets" [mode]="d.mode" [own]="true" />
        <quader-limit-explain [subjectName]="you" />
      </section>

      @if (!empty()) {
        <section class="section">
          <quader-usage-history [points]="d.history" />
        </section>
      }

      <section class="section" aria-labelledby="own-repos">
        <h2 id="own-repos" i18n="@@usage.by-repository">By repository</h2>
        <quader-usage-by-repository [rows]="d.repositories" [emptyText]="emptyText()" />
      </section>

      <section class="section" aria-labelledby="own-credentials">
        <h2 id="own-credentials" i18n="@@usage.by-credential">By credential</h2>
        <quader-usage-by-credential [rows]="d.credentials" [emptyText]="emptyText()" />
      </section>
    } @else {
      <p role="status" i18n="@@limits.loading">Loading.</p>
    }
  `,
  styleUrl: './usage.scss',
  styles: `
    /* The icon stands on the middle of the notice's first line (R-UI-175): a 24 px glyph on the line of the text. */
    .notice-block > .material-icons {
      margin-block: calc((var(--quader-line) - 24px) / 2);
    }
    .notice-block {
      display: flex;
      align-items: flex-start;
      gap: var(--quader-space-2);
      margin: 0 0 var(--quader-space-3);
      padding: var(--quader-space-2) var(--quader-space-3);
      border: 1px solid var(--quader-border);
      border-radius: var(--quader-radius-m);
    }
    .bad {
      color: var(--quader-status-bad);
    }
  `,
})
export class ProfileUsage {
  private readonly api = inject(LimitsApi);
  private readonly router = inject(Router);
  private readonly locale = inject(LOCALE_ID);

  readonly period = input<string | undefined>(undefined);

  protected readonly you = $localize`:@@profile.usage.you:you`;
  protected readonly periods = PERIODS;
  protected readonly activePeriod = computed<UsagePeriod>(() => (PERIODS.includes(this.period() as UsagePeriod) ? (this.period() as UsagePeriod) : 'day'));
  protected readonly detail = signal<UsageDetail | null>(null);
  protected readonly failure = signal<ApiError | null>(null);
  protected readonly blocked = computed(() => {
    const d = this.detail();
    return d ? blockedUntil(d) : null;
  });
  protected readonly empty = computed(() => {
    const d = this.detail();
    return d ? nothingInDay(d) : false;
  });
  protected readonly emptyText = computed(() => $localize`:@@usage.empty:No downloads ${periodPhrase(this.activePeriod())}:period:.`);

  constructor() {
    effect(() => {
      this.activePeriod();
      untracked(() => void this.load());
    });
    // The meters move with every download: read again every minute in place while the page is seen (R-UI-213).
    live({
      reload: async () => {
        if (this.detail() === null) return;
        const period = this.activePeriod();
        const detail = await this.api.myUsage(period);
        if (period === this.activePeriod()) this.detail.set(detail);
      },
      every: () => LIVE_IDLE_MS,
    });
  }

  protected async load(): Promise<void> {
    this.failure.set(null);
    try {
      this.detail.set(await this.api.myUsage(this.activePeriod()));
    } catch (e) {
      this.detail.set(null);
      this.failure.set(asApiError(e));
    }
  }

  protected periodText(p: UsagePeriod): string {
    return periodLabel(p);
  }

  protected setPeriod(p: UsagePeriod): void {
    void this.router.navigate([], { queryParams: { period: p === 'day' ? null : p }, queryParamsHandling: 'merge' });
  }

  /** "Downloads are paused for you until 10:55 after too many refused requests." (R-UI-186's words). */
  protected blockText(until: string): string {
    return $localize`:@@limits.notice.blocked:Downloads are paused for you until ${formatDate(until, 'shortTime', this.locale)}:time: after too many refused requests.`;
  }
}
