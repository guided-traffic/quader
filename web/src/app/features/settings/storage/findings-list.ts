import { CdkMenu, CdkMenuItem, CdkMenuTrigger } from '@angular/cdk/menu';
import { Component, computed, effect, inject, input, output, signal, untracked } from '@angular/core';
import { RouterLink } from '@angular/router';
import { firstValueFrom } from 'rxjs';
import { FindingFilter, StorageApi, StorageFinding, StoragePeriod } from '../../../api/storage-api';
import { Notifier } from '../../../core/notifier';
import { FailureToasts } from '../../../shared/failure-dialog';
import { PagedResource, asApiError } from '../../../shared/paged-resource';
import { TimePipe } from '../../../shared/time-pipe';
import { Busy } from '../../../shared/ui/busy';
import { DataTable, TableColumn } from '../../../shared/ui/data-table';
import { QuaderDialog } from '../../../shared/ui/dialog';
import { formatSize } from '../../packages/format-size';
import { AcknowledgeDialog } from './acknowledge-dialog';
import { FindingState, findingState, findingStateWord, findingTone, reasonWord, sourceWord } from './storage-history';

/** The findings one page shows; **Load more** reads the next. */
export const FINDINGS_PAGE = 10;

/** The filter's states (Q-UI-1603): the open ones, or every finding of the period. */
export type FindingsShown = 'open' | 'all';

/**
 * The findings of the period in the section Integrity (api-and-ui.md R-UI-265, Q-UI-1604; storage.md R-STOR-108, R-STOR-111): each
 * copy found damaged with when, the connection, the blob by its short digest and the first item behind it, its bytes, what found it and
 * why, and its state. **Open** is the default while open findings exist, otherwise **All** (Q-UI-1603). A holder of `settings:write`
 * has **Check again** (reads every copy whole now, writes no object) and **Acknowledge** (records that a person knows of it, repairs
 * nothing, Q-STOR-530) on an unresolved finding (Q-UI-1606). The list grows the page (R-UI-215) and reads again in place (R-UI-213).
 */
@Component({
  selector: 'quader-storage-findings',
  imports: [Busy, CdkMenu, CdkMenuItem, CdkMenuTrigger, DataTable, RouterLink, TimePipe],
  template: `
    <div class="findings-head">
      <h3 class="sub-heading" id="storage-findings-heading" i18n="@@storage.findings.heading">Findings</h3>
      <div class="filter" role="group" i18n-aria-label="@@storage.findings.filter" aria-label="Findings shown">
        <button class="btn btn-default btn-sm" type="button" [attr.aria-pressed]="shown() === 'open'" (click)="show('open')" i18n="@@storage.findings.open">Open</button>
        <button class="btn btn-default btn-sm" type="button" [attr.aria-pressed]="shown() === 'all'" (click)="show('all')" i18n="@@storage.findings.all">All</button>
      </div>
    </div>
    <quader-data-table
      [caption]="caption"
      [columns]="columns()"
      [loaded]="list.loaded()"
      [loading]="list.loading()"
      [loadingText]="loadingText"
      [error]="list.error()"
      [empty]="list.items().length === 0"
      [emptyText]="emptyText()"
      [hasMore]="list.hasMore()"
      (more)="list.more()"
      (retry)="list.reload()"
    >
      @for (f of list.items(); track f.id) {
        @let st = state(f);
        <tr>
          <th scope="row" class="when">{{ f.detectedAt | time }}</th>
          <td translate="no">{{ f.connection }}</td>
          <!-- The digest on its line, the first item that holds the blob under it; a path breaks anywhere (R-UI-174). -->
          <td class="item-cell lines-cell">
            <div class="lines">
              <code class="line" translate="no" [attr.title]="f.sha256">{{ f.sha256.slice(0, 12) }}</code>
              @if (f.items[0]; as i) {
                <span class="line">
                  @if (i.trashed) {
                    <span class="secondary" translate="no">{{ i.project }}/{{ i.repository }}: {{ i.path }}</span>
                  } @else {
                    <a [routerLink]="itemLink(i)" translate="no">{{ i.project }}/{{ i.repository }}: {{ i.path }}</a>
                  }
                  @if (f.itemCount > 1) {
                    &ngsp;<span class="secondary" i18n="@@storage.findings.more-items">+ {{ f.itemCount - 1 }} more</span>
                  }
                </span>
              }
            </div>
          </td>
          <td class="figure">{{ size(f.storedSize) }}</td>
          <td>{{ source(f.source) }} · {{ reason(f.reason) }}</td>
          <td class="lines-cell">
            <div class="lines">
              <span class="line" [class.warn]="tone(f, st) === 'warn'" [class.bad]="tone(f, st) === 'bad'">
                @if (tone(f, st) !== 'neutral') {
                  <span class="material-icons" aria-hidden="true">{{ tone(f, st) === 'bad' ? 'error' : 'warning' }}</span>
                }{{ stateWord(st) }}</span
              >
              @if (f.unavailable && st === 'open') {
                <span class="line state-words" i18n="@@storage.findings.no-other-copy">No other copy</span>
              }
              @if (f.acknowledged; as a) {
                <span class="line state-words">
                  @if (a.by; as by) {
                    <ng-container i18n="@@storage.findings.acknowledged-by">by <span translate="no">{{ by.name }}</span>, {{ a.at | time }}</ng-container>
                  } @else {
                    {{ a.at | time }}
                  }
                </span>
                @if (a.note) {
                  <span class="line note-text">{{ a.note }}</span>
                }
              }
            </div>
          </td>
          @if (canWrite()) {
            <td class="actions">
              <!-- An unresolved finding's two acts are the row's menu (R-UI-163, Q-UI-1606); a resolved one has none. -->
              @if (!f.resolution) {
                <button
                  class="btn btn-tertiary btn-icon"
                  type="button"
                  [quaderBusy]="checking() === f.id"
                  [attr.aria-label]="actionsLabel(f)"
                  [cdkMenuTriggerFor]="rowMenu"
                >
                  <span class="material-icons" aria-hidden="true">more_vert</span>
                </button>
                <ng-template #rowMenu>
                  <div class="menu" cdkMenu>
                    <button class="menu-item" cdkMenuItem type="button" (cdkMenuItemTriggered)="recheck(f)" i18n="@@storage.findings.check">Check again</button>
                    <button class="menu-item" cdkMenuItem type="button" (cdkMenuItemTriggered)="acknowledge(f)" i18n="@@storage.findings.acknowledge">
                      Acknowledge
                    </button>
                  </div>
                </ng-template>
              }
            </td>
          }
        </tr>
      }
    </quader-data-table>
  `,
  styleUrl: './storage-sections.scss',
  styles: `
    .findings-head {
      display: flex;
      flex-wrap: wrap;
      align-items: center;
      justify-content: space-between;
      gap: var(--quader-space-2) var(--quader-space-4);
      margin-block: var(--quader-space-4) var(--quader-space-2);

      .sub-heading {
        margin: 0;
      }
    }
    .filter {
      display: inline-flex;
      gap: var(--quader-space-1);

      button[aria-pressed='true'] {
        background: var(--quader-accent-container);
        color: var(--quader-on-accent-container);
      }
    }
    .note-text {
      overflow-wrap: anywhere;
    }
  `,
})
export class FindingsList {
  private readonly api = inject(StorageApi);
  private readonly dialog = inject(QuaderDialog);
  private readonly notifier = inject(Notifier);
  private readonly failureToasts = inject(FailureToasts);

  readonly period = input.required<StoragePeriod>();
  /** Whether open findings exist in the period (the overview's): the filter starts at Open then, at All otherwise (Q-UI-1603). */
  readonly anyOpen = input(false);
  readonly canWrite = input(false);
  /** A recheck or an acknowledgement changed a finding: the page reads its figures again. */
  readonly changed = output<void>();

  protected readonly list = new PagedResource<StorageFinding>(FINDINGS_PAGE);
  /** The filter the person chose; null while the default applies. */
  private readonly chosen = signal<FindingsShown | null>(null);
  protected readonly shown = computed<FindingsShown>(() => this.chosen() ?? (this.anyOpen() ? 'open' : 'all'));
  protected readonly checking = signal<number | null>(null);

  protected readonly size = formatSize;
  protected readonly source = sourceWord;
  protected readonly reason = reasonWord;
  protected readonly state = findingState;
  protected readonly stateWord = findingStateWord;
  protected readonly caption = $localize`:@@storage.findings.caption:Damaged copies of the period`;
  protected readonly loadingText = $localize`:@@storage.findings.loading:Loading the findings.`;
  protected readonly columns = computed<readonly TableColumn[]>(() => [
    { id: 'when', label: $localize`:@@storage.col.when:When`, width: '12rem' },
    { id: 'connection', label: $localize`:@@storage.col.connection:Connection`, width: '7rem' },
    { id: 'blob', label: $localize`:@@storage.col.blob:Blob`, width: '12rem', prefer: true },
    { id: 'bytes', label: $localize`:@@storage.col.bytes:Bytes`, width: '6rem' },
    { id: 'found', label: $localize`:@@storage.col.found-by:Found by`, width: '9rem' },
    { id: 'state', label: $localize`:@@storage.col.finding-state:State`, width: '10rem' },
    ...(this.canWrite() ? [{ id: 'actions', label: $localize`:@@repo.col.actions:Actions`, hideLabel: true }] : []),
  ]);
  protected readonly emptyText = computed(() =>
    this.shown() === 'open'
      ? $localize`:@@storage.findings.empty-open:No open damaged copy in this period.`
      : $localize`:@@storage.findings.empty:No damaged copy in this period.`,
  );

  constructor() {
    // A new period or filter is a new question: its load shows, never the rows of the one before (R-UI-217).
    effect(() => {
      const fetch = this.api.findings(this.period(), this.filter());
      untracked(() => void this.list.start(fetch));
    });
  }

  private filter(): FindingFilter {
    return { state: this.shown() === 'open' ? 'open' : 'all' };
  }

  /** Reads the rows shown again in place (R-UI-213). */
  refresh(): Promise<void> {
    return this.list.refresh(this.api.findings(untracked(() => this.period()), untracked(() => this.filter())));
  }

  protected show(s: FindingsShown): void {
    this.chosen.set(s);
  }

  protected tone(f: StorageFinding, st: FindingState): 'bad' | 'warn' | 'neutral' {
    return findingTone(st, f.unavailable);
  }

  protected itemLink(i: StorageFinding['items'][number]): string[] {
    return ['/projects', i.project, 'repositories', i.repository, 'files', ...i.path.split('/')];
  }

  protected actionsLabel(f: StorageFinding): string {
    return $localize`:@@storage.findings.actions-label:Actions for the damaged copy of ${f.sha256.slice(0, 12)}:digest: on ${f.connection}:connection:`;
  }

  /** **Check again** (R-STOR-027): reads every copy of the blob whole now; the finding follows what the copy is then. */
  protected async recheck(f: StorageFinding): Promise<void> {
    this.checking.set(f.id);
    try {
      const out = await this.api.recheck(f.sha256);
      this.notifier.status(
        out.contentStatus === 'ok'
          ? $localize`:@@storage.findings.checked-ok:A connection holds ${f.sha256.slice(0, 12)}:digest: whole.`
          : $localize`:@@storage.findings.checked-still:No connection holds ${f.sha256.slice(0, 12)}:digest: whole yet.`,
      );
      await this.refresh();
      this.changed.emit();
    } catch (e) {
      this.failureToasts.alert(asApiError(e));
    } finally {
      this.checking.set(null);
    }
  }

  /** **Acknowledge** (R-STOR-111, Q-UI-1607): the dialog, then the row as the server answered it. */
  protected async acknowledge(f: StorageFinding): Promise<void> {
    const ref = this.dialog.open<AcknowledgeDialog, StorageFinding, StorageFinding>(AcknowledgeDialog, { data: f, width: '32rem' });
    const done = await firstValueFrom(ref.closed);
    if (!done) return;
    this.list.replace((x) => x.id === done.id, done);
    this.notifier.status($localize`:@@storage.findings.acknowledged:The damaged copy is acknowledged. It stays open until its copy is repaired.`);
    this.changed.emit();
  }
}
