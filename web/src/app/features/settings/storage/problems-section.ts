import { DecimalPipe } from '@angular/common';
import { Component, effect, inject, input, untracked } from '@angular/core';
import { StorageApi, StorageHistory, StoragePeriod, StorageProblem } from '../../../api/storage-api';
import { PagedResource } from '../../../shared/paged-resource';
import { TimePipe } from '../../../shared/time-pipe';
import { DataTable, TableColumn } from '../../../shared/ui/data-table';
import { Loading } from '../../../shared/ui/loading';
import { formatSize } from '../../packages/format-size';
import { durationText, problemKindWord, reasonWord, sourceWord } from './storage-history';
import { Timeline } from './timeline';

/** The problems one page shows; **Load more** reads the next. */
export const PROBLEMS_PAGE = 10;

/**
 * The section **Problems** of Settings › Storage (api-and-ui.md R-UI-265; storage.md R-STOR-111): per connection the strip of the period's
 * cells (Q-UI-1600), then the problems of the period, newest first, each with when it began, the connection, what it was with its codes
 * on demand, its length or amount, and whether it lasts (Q-UI-1605). Fall-overs are no problems of their own (Q-STOR-526): they colour
 * the cells and stand in Availability. The list grows the page (R-UI-215) and reads again in place (R-UI-213).
 */
@Component({
  selector: 'quader-storage-problems',
  imports: [DataTable, DecimalPipe, Loading, TimePipe, Timeline],
  templateUrl: './problems-section.html',
  styleUrls: ['./storage-sections.scss', './storage-problems.scss'],
})
export class ProblemsSection {
  private readonly api = inject(StorageApi);

  readonly period = input.required<StoragePeriod>();
  /** The period's cells; null while they are read. */
  readonly history = input<StorageHistory | null>(null);

  protected readonly list = new PagedResource<StorageProblem>(PROBLEMS_PAGE);
  protected readonly size = formatSize;
  protected readonly duration = durationText;
  protected readonly kindWord = problemKindWord;

  protected readonly caption = $localize`:@@storage.problems.caption:Problems of the period`;
  protected readonly loadingText = $localize`:@@storage.problems.loading:Loading the problems.`;
  protected readonly emptyText = $localize`:@@storage.problems.empty:No problem in this period.`;
  protected readonly columns: readonly TableColumn[] = [
    { id: 'when', label: $localize`:@@storage.col.when:When`, width: '12rem' },
    { id: 'connection', label: $localize`:@@storage.col.connection:Connection`, width: '7rem' },
    { id: 'what', label: $localize`:@@storage.col.what:What`, width: '12rem', prefer: true },
    { id: 'amount', label: $localize`:@@storage.col.duration-amount:Duration / amount`, width: '10rem' },
    { id: 'state', label: $localize`:@@storage.col.problem-state:State`, width: '7rem' },
  ];

  constructor() {
    // A new period is a new question: its load shows, never the rows of the one before (R-UI-217).
    effect(() => {
      const fetch = this.api.problems(this.period());
      untracked(() => void this.list.start(fetch));
    });
  }

  /** Reads the rows shown again in place (R-UI-213). */
  refresh(): Promise<void> {
    return this.list.refresh(this.api.problems(untracked(() => this.period())));
  }

  protected stripLabel(id: string): string {
    return $localize`:@@storage.problems.strip:Cells of ${id}:id:`;
  }

  /** The codes, sources or reasons behind a problem, in words where Quader has them; a failure code as the record keeps it. */
  protected details(p: StorageProblem): string {
    const parts: string[] = [];
    if (p.kind === 'copies_damaged') {
      parts.push(...p.sources.map(sourceWord), ...p.codes.map(reasonWord));
    } else {
      parts.push(...p.steps, ...p.codes.map(reasonWord));
    }
    return parts.join(', ');
  }
}
