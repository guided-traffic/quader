import { DatePipe } from '@angular/common';
import { Component, computed, inject, input, output } from '@angular/core';
import { Job, JobState, jobActive, jobCancellable } from '../../api/jobs-api';
import { SessionStore } from '../../core/session-store';
import { DataTable, TableColumn } from '../../shared/ui/data-table';
import { NoValue } from '../../shared/ui/no-value';

/**
 * A job's state as a status, the word of its badge (Q-UI-560, Q-UI-564): with a capital, as every status of the app. A sentence
 * that names the state ("Deleting succeeded") takes `jobStateText` of job-progress.ts, in lower case.
 */
export function jobStatusText(state: JobState): string {
  switch (state) {
    case 'queued':
      return $localize`:@@job.status.queued:Queued`;
    case 'running':
      return $localize`:@@job.status.running:Running`;
    case 'cancelling':
      return $localize`:@@job.status.cancelling:Cancelling`;
    case 'succeeded':
      return $localize`:@@job.status.succeeded:Succeeded`;
    case 'failed':
      return $localize`:@@job.status.failed:Failed`;
    case 'cancelled':
      return $localize`:@@job.status.cancelled:Cancelled`;
  }
}

/**
 * A table of jobs (R-UI-038): kind, state as a badge (its icon and its word, never broken, Q-UI-560), start, progress, replica and
 * result. The kind and the replica are names that stay on one line; the result is read and wraps.
 * "Cancel" is offered only for `queued` and `running` jobs, and only to a holder of
 * `settings:write` or the principal that started the job (R-API-015).
 */
@Component({
  selector: 'quader-job-table',
  imports: [DataTable, DatePipe, NoValue],
  styles: `
    /* A result is read in full: it wraps in its column, at any character where it must. */
    .long {
      overflow-wrap: anywhere;
    }
  `,
  template: `
    <!-- The shared data table: a column budget and stacked rows where the columns do not fit, so the screen never scrolls sideways at 390 px. -->
    <quader-data-table [caption]="caption()" [columns]="columns">
      @for (j of jobs(); track j.id) {
        <tr>
          <th scope="row" translate="no">{{ j.kind }}</th>
          <td>
            @switch (j.state) {
              @case ('succeeded') {
                <span class="ok"><span class="material-icons" aria-hidden="true">check_circle</span>{{ stateText(j) }}</span>
              }
              @case ('failed') {
                <span class="bad"><span class="material-icons" aria-hidden="true">error</span>{{ stateText(j) }}</span>
              }
              @case ('cancelled') {
                <span class="badge"><span class="material-icons" aria-hidden="true">cancel</span>{{ stateText(j) }}</span>
              }
              @default {
                <span class="badge"><span class="material-icons" aria-hidden="true">hourglass_empty</span>{{ stateText(j) }}</span>
              }
            }
          </td>
          <td>
            <time [attr.datetime]="j.enqueuedAt">{{ j.enqueuedAt | date: 'medium' }}</time>
          </td>
          <td translate="no">
            @if (progressText(j); as p) {
              {{ p }}
            } @else {
              <quader-no-value />
            }
          </td>
          <td translate="no">
            @if (j.replica) {
              {{ j.replica }}
            } @else {
              <quader-no-value />
            }
          </td>
          <td class="long">
            @if (j.error; as err) {
              <span translate="no">{{ err.code }}</span>
              @if (err.detail) {
                <span class="error-detail" translate="no">{{ err.detail }}</span>
                <span class="error-detail" i18n="@@jobs.attempt">Attempt {{ attemptOf(j) }} of {{ j.maxAttempts }}</span>
              }
            } @else if (summary(j); as text) {
              <span translate="no">{{ text }}</span>
            } @else if (!active(j)) {
              <quader-no-value />
            }
          </td>
          <td>
            @if (canCancel(j)) {
              <button class="btn btn-tertiary"
                type="button"
                [attr.aria-label]="cancelLabel(j)"
                (click)="cancelRequested.emit(j)"
                i18n="@@jobs.cancel"
              >
                Cancel
              </button>
            }
          </td>
        </tr>
      }
    </quader-data-table>
  `,
})
export class JobTable {
  private readonly session = inject(SessionStore);

  readonly jobs = input.required<readonly Job[]>();
  readonly caption = input.required<string>();
  readonly cancelRequested = output<Job>();

  /**
   * The kind, the state, the time, the progress, the replica and the action never break and are as wide as their content
   * (Q-UI-563); the result is the one column that wraps and takes what they leave. A replica of 7 rem broke "ms2022.local-39645"
   * at its hyphen beside a result column of half the page (2026-11-18).
   */
  protected readonly columns: readonly TableColumn[] = [
    { id: 'kind', label: $localize`:@@jobs.col.kind:Kind` },
    { id: 'state', label: $localize`:@@jobs.col.state:State` },
    { id: 'enqueued', label: $localize`:@@jobs.col.enqueued:Enqueued` },
    { id: 'progress', label: $localize`:@@jobs.col.progress:Progress` },
    { id: 'replica', label: $localize`:@@jobs.col.replica:Replica` },
    { id: 'result', label: $localize`:@@jobs.col.result:Result`, width: '10rem', prefer: true },
    { id: 'actions', label: $localize`:@@repo.col.actions:Actions`, hideLabel: true },
  ];

  private readonly self = computed(() => this.session.whoami()?.principal.id ?? null);

  protected stateText(job: Job): string {
    return jobStatusText(job.state);
  }

  protected active(job: Job): boolean {
    return jobActive(job.state);
  }

  /** The progress as "3/10 items", or empty for a job that reports none. */
  protected progressText(job: Job): string {
    const p = job.progress;
    if (!p) return '';
    return `${p.done}${p.total === null ? '' : `/${p.total}`} ${p.unit ?? ''}`.trim();
  }

  protected canCancel(job: Job): boolean {
    if (!jobCancellable(job.state)) return false;
    return (
      this.session.has('settings:write') ||
      (job.triggeredBy !== null && job.triggeredBy === this.self())
    );
  }

  protected cancelLabel(job: Job): string {
    return $localize`:@@jobs.cancel.label:Cancel job ${job.kind}:kind:`;
  }

  /** The attempt a job's error belongs to: the error's own, else the job's (Q-UI-172). */
  protected attemptOf(job: Job): number {
    return job.error?.attempt ?? job.attempt;
  }

  /** A compact summary of a result, or empty for none; the full object is in the API. A list shows its length, an object its JSON. */
  protected summary(job: Job): string {
    if (!job.result) return '';
    return Object.entries(job.result)
      .map(([k, v]) => `${k}: ${resultValue(v)}`)
      .join(', ');
  }
}

function resultValue(v: unknown): string {
  if (Array.isArray(v)) return String(v.length);
  return v !== null && typeof v === 'object' ? JSON.stringify(v) : String(v);
}
