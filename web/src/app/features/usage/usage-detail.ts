import { formatDate } from '@angular/common';
import { ChangeDetectionStrategy, Component, LOCALE_ID, computed, effect, inject, input, signal, untracked } from '@angular/core';
import { LIVE_IDLE_MS, live } from '../../core/live-refresh';
import { Router, RouterLink } from '@angular/router';
import { LimitBlock, LimitsApi, UsageDetail, UsagePeriod, UsageScope, UsageSubject, UsageSubjectKind } from '../../api/limits-api';
import { ApiError } from '../../api/problem';
import { Notifier } from '../../core/notifier';
import { SessionStore } from '../../core/session-store';
import { Confirm } from '../../shared/confirm-dialog';
import { ErrorNotice } from '../../shared/error-notice';
import { asApiError } from '../../shared/paged-resource';
import { PageHeader } from '../../shared/ui/page-header';
import { subjectKindLabel, subjectReference, subjectTitle, subjectView } from '../limits/callers';
import { formatCount } from '../limits/figures';
import { LimitExplain } from './limit-explain';
import { UsageBudgets } from './usage-budgets';
import { UsageHistory } from './usage-history';
import { UsageByCredentialTable, UsageByRepositoryTable } from './usage-tables';
import { PERIODS, periodLabel, periodPhrase } from './usage-words';

/** The page of a technical token's service account (R-UI-181): `sa:<name>`, `sa:<project>/<name>`, `sa:<project>/<repo>/<name>`. */
export function accountLink(account: string): readonly string[] | null {
  if (!account.startsWith('sa:')) return null;
  const parts = account.slice(3).split('/');
  if (parts.length === 1) return ['/admin/service-accounts', parts[0]!];
  if (parts.length === 2) return ['/projects', parts[0]!, 'service-accounts', parts[1]!];
  if (parts.length === 3) return ['/projects', parts[0]!, 'repositories', parts[1]!, 'service-accounts', parts[2]!];
  return null;
}

/** The earliest end of a block among the budgets, or null. */
export function blockedUntil(detail: UsageDetail): string | null {
  const ends = detail.budgets.flatMap((b) => (b.blockedUntil ? [b.blockedUntil] : []));
  return ends.length ? ends.sort()[0]! : null;
}

function sameSubject(a: UsageSubject, b: UsageSubject): boolean {
  return a.kind === b.kind && a.id === b.id;
}

/**
 * A caller's usage, and which limit applies (R-UI-184): the caller as the title, under it its kind and its reference (a user's
 * `@name`, a technical token's account, Q-UI-765); a block with
 * **Lift block** for whoever may lift it; the limits of each budget key now with their meters; **Explain for a repository**; the
 * last 24 hours; and, for the period of the head, **By repository** and, for a user and a certificate subject, **By credential**. In a project only the
 * project's repositories are counted (R-LIM-029).
 */
@Component({
  selector: 'quader-usage-detail',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [ErrorNotice, LimitExplain, PageHeader, RouterLink, UsageBudgets, UsageByCredentialTable, UsageByRepositoryTable, UsageHistory],
  template: `
    <quader-page-header [heading]="title()" [name]="true">
      <span meta class="meta">
        <span class="badge">{{ kindLabel() }}</span>
        @if (reference(); as ref) {
          <span class="reference" translate="no">{{ ref }}</span>
        }
        @if (account(); as a) {
          @if (accountRoute(a); as route) {
            <a [routerLink]="route" translate="no">{{ a }}</a>
          } @else {
            <span translate="no">{{ a }}</span>
          }
        }
      </span>
      <div actions class="period" role="group" i18n-aria-label="@@usage.period" aria-label="Period">
        @for (p of periods; track p) {
          <button class="btn btn-default" type="button" [attr.aria-pressed]="activePeriod() === p" (click)="setPeriod(p)">{{ periodText(p) }}</button>
        }
      </div>
    </quader-page-header>

    @if (failure(); as e) {
      <quader-error-notice [error]="e" (retry)="load()" />
    } @else if (detail(); as d) {
      @if (blockedEnd(); as until) {
        <div class="notice-block" role="status">
          <span class="material-icons bad" aria-hidden="true">block</span>
          <span class="text">{{ blockText(until) }}</span>
          @if (block() && canLift()) {
            <button class="btn btn-default on-line" type="button" (click)="lift()" i18n="@@usage.block.lift">Lift block</button>
          }
        </div>
      }
      @if (d.mode === 'audit') {
        <p class="mode-note" i18n="@@usage.audit-line">Mode: <strong>Audit</strong>. Limits are counted, not applied.</p>
      }

      <section class="section" aria-labelledby="limits-title">
        <h2 id="limits-title" i18n="@@usage.limits">Limits</h2>
        <quader-usage-budgets [budgets]="d.budgets" [mode]="d.mode" />
        <quader-limit-explain [subject]="{ kind: d.subject.kind, id: d.subject.id }" [subjectName]="caller(d.subject).primary" />
      </section>

      <section class="section">
        <quader-usage-history [points]="d.history" />
      </section>

      <section class="section" aria-labelledby="repos-title">
        <h2 id="repos-title" i18n="@@usage.by-repository">By repository</h2>
        <quader-usage-by-repository [rows]="d.repositories" [emptyText]="emptyText()" />
      </section>

      <!-- A user's session and tokens, a certificate subject's certificates (R-UI-184, R-LIM-027). -->
      @if (d.subject.kind === 'user' || d.subject.kind === 'certificate') {
        <section class="section" aria-labelledby="credentials-title">
          <h2 id="credentials-title" i18n="@@usage.by-credential">By credential</h2>
          <quader-usage-by-credential [rows]="d.credentials" [emptyText]="emptyText()" />
        </section>
      }
    } @else {
      <p role="status" i18n="@@limits.loading">Loading.</p>
    }
  `,
  styleUrl: './usage.scss',
  styles: `
    .meta {
      display: flex;
      flex-wrap: wrap;
      align-items: flex-start;
      gap: var(--quader-space-2);
    }
    .reference {
      color: var(--quader-text-muted);
    }
    /* The icon stands on the middle of the notice's first line (R-UI-175): a 24 px glyph on the line of the text. */
    .notice-block > .material-icons {
      margin-block: calc((var(--quader-line) - 24px) / 2);
    }
    .notice-block {
      display: flex;
      flex-wrap: wrap;
      align-items: flex-start;
      gap: var(--quader-space-2);
      margin-block-end: var(--quader-space-3);
      padding: var(--quader-space-2) var(--quader-space-3);
      border: 1px solid var(--quader-border);
      border-radius: var(--quader-radius-m);
    }
    .notice-block .text {
      flex: 1 1 16rem;
    }
    .bad {
      color: var(--quader-status-bad);
    }
  `,
})
export class UsageDetailPage {
  private readonly api = inject(LimitsApi);
  private readonly session = inject(SessionStore);
  private readonly router = inject(Router);
  private readonly confirmer = inject(Confirm);
  private readonly notifier = inject(Notifier);
  private readonly locale = inject(LOCALE_ID);

  readonly scope = input<'instance' | 'project'>('instance');
  readonly project = input<string | undefined>(undefined);
  readonly kind = input.required<string>();
  readonly id = input.required<string>();
  readonly period = input<string | undefined>(undefined);

  protected readonly periods = PERIODS;
  protected readonly activePeriod = computed<UsagePeriod>(() => (PERIODS.includes(this.period() as UsagePeriod) ? (this.period() as UsagePeriod) : 'day'));
  protected readonly detail = signal<UsageDetail | null>(null);
  protected readonly failure = signal<ApiError | null>(null);
  protected readonly block = signal<LimitBlock | null>(null);

  /** The thing (Q-UI-765): a user's display name, a token's name, a CI project's path, an address; its reference under it. */
  protected readonly title = computed(() => {
    const d = this.detail();
    return d ? subjectTitle(d.subject) : this.id();
  });
  protected readonly reference = computed(() => {
    const d = this.detail();
    return d ? subjectReference(d.subject) : null;
  });
  protected readonly kindLabel = computed(() => subjectKindLabel((this.detail()?.subject.kind ?? this.kind()) as UsageSubjectKind));
  protected readonly account = computed(() => {
    const s = this.detail()?.subject;
    return s?.kind === 'technicalToken' ? s.account : null;
  });
  protected readonly blockedEnd = computed(() => {
    const d = this.detail();
    return d ? blockedUntil(d) : null;
  });
  /** `limits:write` lifts every block; an Owner the blocks of its project (R-LIM-016). */
  protected readonly canLift = computed(() => this.session.has('limits:write') || (this.scope() === 'project' && this.session.isOwnerOf(this.project())));
  protected readonly emptyText = computed(() => $localize`:@@usage.empty:No downloads ${periodPhrase(this.activePeriod())}:period:.`);

  constructor() {
    effect(() => {
      this.kind();
      this.id();
      this.activePeriod();
      this.project();
      untracked(() => void this.load());
    });
    // The meters move with every download: read again every minute in place while the page is seen (R-UI-213).
    live({
      reload: async () => {
        if (this.detail() === null) return;
        const key = `${this.kind()} ${this.id()} ${this.activePeriod()} ${this.project()}`;
        const detail = await this.api.subjectUsage(this.usageScope(), this.kind() as UsageSubjectKind, this.id(), this.activePeriod());
        if (key === `${this.kind()} ${this.id()} ${this.activePeriod()} ${this.project()}`) this.detail.set(detail);
      },
      every: () => LIVE_IDLE_MS,
    });
  }

  private usageScope(): UsageScope {
    return this.scope() === 'project' ? { project: this.project() ?? null } : {};
  }

  protected async load(): Promise<void> {
    this.failure.set(null);
    try {
      const d = await this.api.subjectUsage(this.usageScope(), this.kind() as UsageSubjectKind, this.id(), this.activePeriod());
      this.detail.set(d);
      if (blockedUntil(d) && this.canLift()) void this.findBlock(d.subject);
    } catch (e) {
      this.detail.set(null);
      this.failure.set(asApiError(e));
    }
  }

  /** The active block of the subject, whose ID **Lift block** needs. */
  private async findBlock(subject: UsageSubject): Promise<void> {
    try {
      const place = this.scope() === 'project' ? `project:${this.project()}` : undefined;
      const blocks = await this.api.blocks({ active: true, place });
      this.block.set(blocks.find((b) => sameSubject(b.subject, subject)) ?? null);
    } catch {
      this.block.set(null);
    }
  }

  protected caller(s: UsageSubject) {
    return subjectView(s);
  }

  protected accountRoute(account: string): readonly string[] | null {
    return accountLink(account);
  }

  protected periodText(p: UsagePeriod): string {
    return periodLabel(p);
  }

  protected setPeriod(p: UsagePeriod): void {
    void this.router.navigate([], { queryParams: { period: p === 'day' ? null : p }, queryParamsHandling: 'merge' });
  }

  /** "Blocked until 10:55 after 61 refused requests in an hour" (R-UI-184). */
  protected blockText(until: string): string {
    const time = formatDate(until, 'shortTime', this.locale);
    const b = this.block();
    return b
      ? $localize`:@@usage.block.text:Blocked until ${time}:time: after ${formatCount(b.refused, this.locale)}:count: refused requests in an hour`
      : $localize`:@@usage.block.text-short:Blocked until ${time}:time: after too many refused requests`;
  }

  protected async lift(): Promise<void> {
    const b = this.block();
    const d = this.detail();
    if (!b || !d) return;
    const name = subjectView(d.subject).primary;
    const answer = await this.confirmer.ask({
      title: $localize`:@@limits.blocks.lift-title:Lift the block of ${name}:name:?`,
      lines: [$localize`:@@limits.blocks.lift-text:The caller's requests are admitted again at once, and its refused requests count from zero.`],
      confirmLabel: $localize`:@@limits.blocks.lift-confirm:Lift block`,
    });
    if (answer === null) return;
    try {
      await this.api.liftBlock(b.id);
      this.notifier.status($localize`:@@limits.blocks.lifted:Block lifted.`);
      this.block.set(null);
      await this.load();
    } catch (e) {
      this.failure.set(asApiError(e));
    }
  }
}
