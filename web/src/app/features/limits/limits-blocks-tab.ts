import { ChangeDetectionStrategy, Component, LOCALE_ID, computed, inject, signal } from '@angular/core';
import { LimitBlock, LimitsApi } from '../../api/limits-api';
import { ApiError } from '../../api/problem';
import { Notifier } from '../../core/notifier';
import { SessionStore } from '../../core/session-store';
import { Confirm } from '../../shared/confirm-dialog';
import { asApiError } from '../../shared/paged-resource';
import { Checkbox } from '../../shared/ui/checkbox';
import { DataTable, TableColumn } from '../../shared/ui/data-table';
import { ListHead } from '../../shared/ui/list-head';
import { TimeAgo } from '../../shared/ui/time-ago';
import { CallerCell } from './caller-cell';
import { placeLabel, subjectView } from './callers';
import { formatCount } from './figures';

/** "61 of 60 per hour" (R-UI-177). */
export function refusedOfLimit(b: LimitBlock, locale: string): string {
  return $localize`:@@limits.blocks.refused-of:${formatCount(b.refused, locale)}:refused: of ${formatCount(b.limit, locale)}:limit: per hour`;
}

/** Whether a block still holds: not lifted, and its end is ahead. */
export function blockActive(b: LimitBlock, now = Date.now()): boolean {
  return b.liftedAt === null && Date.parse(b.until) > now;
}

/**
 * The tab **Blocks** of **Download limits** (R-UI-177): the active blocks, newest first, each with its caller, its place, when it
 * started and ends, and the refused requests against the limit; **Lift** for a holder of `limits:write`, after a confirmation that
 * names the caller. **Show ended blocks** adds those of the last 30 days with their end or who lifted them.
 */
@Component({
  selector: 'quader-limits-blocks-tab',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [CallerCell, Checkbox, DataTable, ListHead, TimeAgo],
  template: `
    <quader-list-head [filters]="false">
      <quader-checkbox headEnd [checked]="showEnded()" (checkedChange)="toggleEnded($event)">
        <ng-container i18n="@@limits.blocks.show-ended">Show ended blocks</ng-container>
      </quader-checkbox>
    </quader-list-head>
    <quader-data-table
      caption="Blocks"
      i18n-caption="@@limits.blocks.caption"
      [columns]="columns()"
      [loaded]="blocks() !== null"
      [loading]="blocks() === null && !failure()"
      [error]="failure()"
      (retry)="load()"
      [empty]="(blocks() ?? []).length === 0"
      emptyText="Nothing is blocked."
      i18n-emptyText="@@limits.blocks.empty"
    >
      @for (b of blocks(); track b.id) {
        <tr>
          <th scope="row"><quader-caller-cell [view]="caller(b)" /></th>
          <td translate="no">{{ place(b) }}</td>
          <td><quader-time-ago [at]="b.startedAt" absolute="title" /></td>
          <td>
            @if (b.liftedAt; as at) {
              <span class="ended">
                <ng-container i18n="@@limits.blocks.lifted-by">Lifted by {{ b.liftedBy ?? '–' }}</ng-container>
                <quader-time-ago [at]="at" absolute="title" />
              </span>
            } @else {
              <quader-time-ago [at]="b.until" absolute="title" />
            }
          </td>
          <td>{{ refused(b) }}</td>
          @if (canWrite()) {
            <td class="actions">
              @if (active(b)) {
                <button class="btn btn-tertiary" type="button" (click)="lift(b)">
                  <ng-container i18n="@@limits.blocks.lift">Lift</ng-container>
                  <span class="visually-hidden" translate="no"> {{ caller(b).primary }}</span>
                </button>
              }
            </td>
          }
        </tr>
      }
    </quader-data-table>
  `,
  styles: `
    .ended {
      display: flex;
      flex-direction: column;
    }
  `,
})
export class LimitsBlocksTab {
  private readonly api = inject(LimitsApi);
  private readonly session = inject(SessionStore);
  private readonly confirmer = inject(Confirm);
  private readonly notifier = inject(Notifier);
  private readonly locale = inject(LOCALE_ID);

  protected readonly canWrite = computed(() => this.session.has('limits:write'));
  protected readonly showEnded = signal(false);
  protected readonly blocks = signal<readonly LimitBlock[] | null>(null);
  protected readonly failure = signal<ApiError | null>(null);
  protected readonly columns = computed<readonly TableColumn[]>(() => [
    { id: 'caller', label: $localize`:@@limits.blocks.col-caller:Caller`, width: '12rem', prefer: true },
    { id: 'place', label: $localize`:@@limits.blocks.col-place:Place`, width: '8rem' },
    { id: 'started', label: $localize`:@@limits.blocks.col-started:Started` },
    { id: 'ends', label: $localize`:@@limits.blocks.col-ends:Ends` },
    { id: 'refused', label: $localize`:@@limits.blocks.col-refused:Refused requests` },
    ...(this.canWrite() ? [{ id: 'actions', label: $localize`:@@limits.rules.col-actions:Actions`, hideLabel: true }] : []),
  ]);

  constructor() {
    void this.load();
  }

  protected async load(): Promise<void> {
    this.failure.set(null);
    try {
      const list = await this.api.blocks({ active: this.showEnded() ? false : true });
      this.blocks.set([...list].sort((a, b) => b.startedAt.localeCompare(a.startedAt)));
    } catch (e) {
      this.failure.set(asApiError(e));
    }
  }

  protected toggleEnded(on: boolean): void {
    this.showEnded.set(on);
    this.blocks.set(null);
    void this.load();
  }

  protected caller(b: LimitBlock) {
    return subjectView(b.subject);
  }

  protected place(b: LimitBlock): string {
    return placeLabel(b.place);
  }

  protected refused(b: LimitBlock): string {
    return refusedOfLimit(b, this.locale);
  }

  protected active(b: LimitBlock): boolean {
    return blockActive(b);
  }

  protected async lift(b: LimitBlock): Promise<void> {
    const name = this.caller(b).primary;
    const answer = await this.confirmer.ask({
      title: $localize`:@@limits.blocks.lift-title:Lift the block of ${name}:name:?`,
      lines: [$localize`:@@limits.blocks.lift-text:The caller's requests are admitted again at once, and its refused requests count from zero.`],
      confirmLabel: $localize`:@@limits.blocks.lift-confirm:Lift block`,
    });
    if (answer === null) return;
    this.failure.set(null);
    try {
      await this.api.liftBlock(b.id);
      this.notifier.status($localize`:@@limits.blocks.lifted:Block lifted.`);
      await this.load();
    } catch (e) {
      this.failure.set(asApiError(e));
    }
  }
}
