import { DecimalPipe } from '@angular/common';
import { Component, computed, input, output, signal } from '@angular/core';
import { StorageConnection, StorageOverview } from '../../../api/storage-api';
import { TimePipe } from '../../../shared/time-pipe';
import { Busy } from '../../../shared/ui/busy';
import { DataTable, TableColumn } from '../../../shared/ui/data-table';
import { NoValue } from '../../../shared/ui/no-value';
import { Tooltip } from '../../../shared/ui/tooltip';
import { formatSize } from '../../packages/format-size';
import { StorageMeter } from './storage-meter';
import { backlog, holdingConnections, holds, percentOf } from './storage-model';

/** One part of the bar of copies per blob. */
interface CopiesPart {
  readonly copies: number;
  readonly blobs: number;
  readonly bytes: number;
  readonly percent: number;
  readonly tone: 'neutral' | 'warn' | 'bad';
}

/**
 * The section **Availability** of Settings › Storage (api-and-ui.md R-UI-263; storage.md R-STOR-106; the owner's second message):
 * whether the installation survives the loss of one connection. The target against what is configured, the copies per blob as one bar,
 * the one line of the loss, and per connection how much of every blob it holds, what only it holds, its backlog and the reconciliation's
 * pass. A holder of `settings:write` runs the reconciliation now; the overview's figures are the server's (Q-STOR-505).
 */
@Component({
  selector: 'quader-storage-availability',
  imports: [Busy, DataTable, DecimalPipe, NoValue, StorageMeter, TimePipe, Tooltip],
  templateUrl: './availability-section.html',
  styleUrl: './storage-sections.scss',
})
export class AvailabilitySection {
  readonly overview = input.required<StorageOverview>();
  readonly connections = input.required<readonly StorageConnection[]>();
  /** The time the backlog's age is counted to. */
  readonly now = input.required<number>();
  /** Whether the reader may run the reconciliation (`settings:write`). */
  readonly canRun = input(false);
  /** Whether a run of the reconciliation started here runs. */
  readonly running = input(false);
  readonly run = output<void>();

  protected readonly formatSize = formatSize;
  protected readonly holds = holds;

  protected readonly caption = $localize`:@@storage.availability.caption:Copies per connection`;
  protected readonly columns: readonly TableColumn[] = [
    { id: 'connection', label: $localize`:@@storage.col.connection:Connection`, width: '9rem', prefer: true },
    {
      id: 'holds',
      label: $localize`:@@storage.col.holds:Holds`,
      width: '9rem',
      help: $localize`:@@storage.col.holds.help:The share of every blob with a whole copy on the connection, from the hourly count.`,
    },
    {
      id: 'only',
      label: $localize`:@@storage.col.only-here:Only here`,
      width: '9rem',
      help: $localize`:@@storage.col.only-here.help:Blobs whose only copy lies on this connection: unavailable if it is lost.`,
    },
    { id: 'backlog', label: $localize`:@@storage.col.backlog:Backlog`, width: '13rem' },
    {
      id: 'reconciliation',
      label: $localize`:@@storage.col.reconciliation:Reconciliation`,
      width: '12rem',
      help: $localize`:@@storage.col.reconciliation.help:The task storage-reconcile copies every missing copy, one full pass after another.`,
    },
  ];

  protected readonly rows = computed(() => holdingConnections(this.connections()));
  protected readonly targetHelp = $localize`:@@storage.availability.target.help:Target: every connection without drain in storage.connections. Written before answering: storage.replication.min_copies.`;

  /** The bar of copies per blob, the most copies first; a part below the target warns, a blob without a copy is an error. */
  protected readonly parts = computed<readonly CopiesPart[]>(() => {
    const o = this.overview();
    const all = o.availability.perBlob.reduce((a, p) => a + p.blobs, 0);
    return [...o.availability.perBlob]
      .filter((p) => p.blobs > 0)
      .sort((a, b) => b.copies - a.copies)
      .map((p) => ({
        ...p,
        percent: all > 0 ? Math.max(0.5, (p.blobs / all) * 100) : 0,
        tone: p.copies === 0 ? 'bad' : p.copies < o.availability.target ? 'warn' : 'neutral',
      }));
  });

  /** The loss line (B.6): the worst connection, the others on demand. */
  protected readonly worst = computed(() => this.overview().availability.loss[0] ?? null);
  protected readonly others = computed(() => this.overview().availability.loss.slice(1));
  protected readonly othersOpen = signal(false);

  protected backlogOf(c: StorageConnection): ReturnType<typeof backlog> {
    return backlog(c, this.now());
  }

  protected holdsLabel(c: StorageConnection): string {
    return $localize`:@@storage.holds.meter:Share of every blob on ${c.id}:id:`;
  }

  protected holdsRatio(c: StorageConnection): number {
    return c.summary?.fillRatio ?? 0;
  }

  protected percent(ratio: number): number {
    return percentOf(ratio);
  }
}
