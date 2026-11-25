import { Component, DestroyRef, computed, effect, inject, input, signal, untracked } from '@angular/core';
import { LIVE_SLOW_MS, live } from '../../core/live-refresh';
import { Router } from '@angular/router';
import { Job, JobState, JobsApi, jobActive } from '../../api/jobs-api';
import { Notifier } from '../../core/notifier';
import { ErrorNotice } from '../../shared/error-notice';
import { PagedResource, asApiError } from '../../shared/paged-resource';
import { EmptyState } from '../../shared/ui/empty-state';
import { FilterCategory, FilterPanel } from '../../shared/ui/filter-panel';
import { ListChip, ListHead } from '../../shared/ui/list-head';
import { JobTable, jobStatusText } from './job-table';
import { SystemRefresh } from './system-refresh';
import { FailureToasts } from '../../shared/failure-dialog';

const STATES: readonly JobState[] = [
  'queued',
  'running',
  'cancelling',
  'succeeded',
  'failed',
  'cancelled',
];

/** Refresh interval of the job list while it shows an active job (WCAG 2.2.2: with a pause control). */
export const JOBS_REFRESH_MS = 5_000;

/** The kinds of a comma-separated text, trimmed, without empty entries. */
function kindsOf(text: string | undefined): string[] {
  return (text ?? '')
    .split(',')
    .map((k) => k.trim())
    .filter((k) => k !== '');
}

/**
 * The tab Jobs of the page System status (R-UI-038, R-UI-164, R-API-015): every job the caller may see, newest first, filterable by kind and
 * state (query parameters `kind` and repeated `state`), with "Cancel" for queued and running jobs.
 * The filters are the head of every list (Q-UI-420, Q-UI-566): **Filters** opens the drawer, the
 * filters that are set stand beside it as chips. A cancelled running job shows as `cancelling` until
 * it stops at its next checkpoint, then as `cancelled`; the list refreshes itself while an active job
 * is shown, and the page's header pauses and refreshes it (Q-UI-651).
 */
@Component({
  selector: 'quader-jobs-page',
  imports: [EmptyState, ErrorNotice, FilterCategory, FilterPanel, JobTable, ListHead],
  template: `
    <!-- The jobs have no search by name; the head is Filters and the filters that are set (Q-UI-420, Q-UI-566). -->
    <quader-list-head [chips]="chips()" (removeChip)="removeChip($event)" [(filtersOpen)]="panelOpen" [controls]="panel.panelId" />
    <quader-filter-panel
      #panel
      [(open)]="panelOpen"
      [activeCount]="chips().length"
      (apply)="apply()"
      (clearAll)="clear()"
      (dismissed)="restage()"
    >
      <div class="field">
        <label class="field-label" for="j-kind" i18n="@@jobs.filter.kind">Kind</label>
        <input
          id="j-kind"
          class="text-input"
          autocomplete="off"
          spellcheck="false"
          aria-describedby="j-kind-hint"
          [value]="kindDraft()"
          (input)="kindDraft.set($any($event.target).value)"
        />
        <p id="j-kind-hint" class="hint" i18n="@@jobs.filter.kind-hint">Several kinds separated by commas: gc, gc-full</p>
      </div>
      <quader-filter-category i18n-label="@@jobs.filter.state" label="State">
        @for (s of states; track s) {
          <label class="check-row">
            <input
              type="checkbox"
              [checked]="stateDraft().includes(s)"
              (change)="toggleState(s, $any($event.target).checked)"
            />
            <span>{{ stateText(s) }}</span>
          </label>
        }
      </quader-filter-category>
    </quader-filter-panel>

    @if (list.error(); as f) {
      <quader-error-notice [error]="f" (retry)="load()" />
    }
    @if (list.loaded()) {
      @if (list.items().length === 0) {
        <quader-empty-state [text]="chips().length > 0 ? noMatch : noJob" />
      } @else {
        <quader-job-table
          [jobs]="list.items()"
          [caption]="caption"
          (cancelRequested)="cancel($event)"
        />
        @if (list.hasMore()) {
          <p>
            <button class="btn btn-default"
              type="button"
              [disabled]="list.loading()"
              (click)="list.more()"
              i18n="@@list.load-more"
            >
              Load more
            </button>
          </p>
        }
      }
    } @else if (list.loading()) {
      <p role="status" i18n="@@system.loading">Loading.</p>
    }
  `,
})
export class JobsPage {
  private readonly api = inject(JobsApi);
  private readonly router = inject(Router);
  private readonly notifier = inject(Notifier);
  private readonly failureToasts = inject(FailureToasts);
  private readonly refresh = inject(SystemRefresh);

  /** Query parameters; `state` repeats and arrives as a string or an array. */
  readonly kind = input<string | undefined>();
  readonly state = input<string | readonly string[] | undefined>();

  protected readonly list = new PagedResource<Job>();
  protected readonly panelOpen = signal(false);
  protected readonly states = STATES;
  /** The drawer's values; they reach the address with **Apply filters** and go back to it when the drawer closes without. */
  protected readonly kindDraft = signal('');
  protected readonly stateDraft = signal<readonly JobState[]>([]);
  protected readonly caption = $localize`:@@jobs.caption:Jobs, newest first`;
  protected readonly noJob = $localize`:@@jobs.empty-none:There is no job yet.`;
  protected readonly noMatch = $localize`:@@jobs.empty:No job matches.`;

  private readonly kinds = computed(() => kindsOf(this.kind()));
  private readonly stateFilter = computed<readonly JobState[]>(() => {
    const raw = this.state();
    const values = raw === undefined ? [] : typeof raw === 'string' ? [raw] : raw;
    return STATES.filter((s) => values.includes(s));
  });
  private readonly hasActive = computed(() => this.list.items().some((j) => jobActive(j.state)));

  /** The filters in the address as the head's chips (Q-UI-422): the kinds as one chip, the states as one. */
  protected readonly chips = computed<readonly ListChip[]>(() => {
    const out: ListChip[] = [];
    const kinds = this.kinds();
    if (kinds.length > 0) out.push({ key: 'kind', label: $localize`:@@jobs.filter.kind:Kind`, value: kinds.join(', ') });
    const states = this.stateFilter();
    if (states.length > 0) {
      out.push({ key: 'state', label: $localize`:@@jobs.filter.state:State`, value: states.map((s) => jobStatusText(s)).join(', ') });
    }
    return out;
  });

  constructor() {
    this.refresh.register(() => this.load(), inject(DestroyRef));
    effect(() => {
      this.kinds();
      this.stateFilter();
      untracked(() => {
        this.restage();
        this.load();
      });
    });
    // The jobs shown are read again in place (R-UI-213): every 5 s while one of them runs, every half minute while none does (a new
    // job appears), while the page is seen and not paused.
    live({
      reload: () => this.list.refresh(),
      every: () => (this.hasActive() ? JOBS_REFRESH_MS : LIVE_SLOW_MS),
      when: () => !this.refresh.paused(),
    });
  }

  protected stateText = jobStatusText;

  protected load(): void {
    void this.list.start(this.api.jobs({ kind: this.kinds(), state: this.stateFilter() }));
  }

  /** The drawer's fields back to the filters the list shows. */
  protected restage(): void {
    this.kindDraft.set(this.kinds().join(', '));
    this.stateDraft.set(this.stateFilter());
  }

  protected toggleState(state: JobState, checked: boolean): void {
    this.stateDraft.update((list) =>
      checked ? [...list, state] : list.filter((s) => s !== state),
    );
  }

  protected apply(): void {
    const kinds = kindsOf(this.kindDraft());
    this.navigate({
      kind: kinds.length > 0 ? kinds.join(',') : null,
      state: this.stateDraft().length > 0 ? [...this.stateDraft()] : null,
    });
  }

  protected removeChip(chip: ListChip): void {
    this.navigate({ [chip.key]: null });
  }

  protected clear(): void {
    this.navigate({ kind: null, state: null });
  }

  private navigate(queryParams: Record<string, string | string[] | null>): void {
    void this.router.navigate([], { queryParams, queryParamsHandling: 'merge' });
  }

  protected async cancel(job: Job): Promise<void> {
    try {
      const updated = await this.api.cancel(job.id);
      this.list.replace((j) => j.id === job.id, updated);
      this.notifier.status($localize`:@@jobs.cancelling:Job ${job.kind}:kind: is being cancelled.`);
    } catch (e) {
      const error = asApiError(e);
      this.failureToasts.alert(error);
      this.load();
    }
  }
}
