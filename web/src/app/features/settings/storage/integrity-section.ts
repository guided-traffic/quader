import { DecimalPipe } from '@angular/common';
import { Component, computed, input, output, viewChild } from '@angular/core';
import { StorageConnection, StorageHistory, StorageHistoryConnection, StorageOverview, StoragePeriod } from '../../../api/storage-api';
import { TimePipe } from '../../../shared/time-pipe';
import { Busy } from '../../../shared/ui/busy';
import { DataTable, TableColumn } from '../../../shared/ui/data-table';
import { Loading } from '../../../shared/ui/loading';
import { NoValue } from '../../../shared/ui/no-value';
import { formatSize } from '../../packages/format-size';
import { FindingsList } from './findings-list';
import { StorageMeter } from './storage-meter';
import { holdingConnections, inFormat, percentOf } from './storage-model';
import { periodWord } from './storage-history';

/** One mark of a connection's checks (B.5): on with a check, or off with a dash; never a paragraph. */
export interface CheckMark {
  readonly kind: 'upload' | 'download' | 'every-read' | 'digest-only' | 'writes';
  readonly on: boolean;
}

/**
 * The checks that protect a connection (B.5, Q-UI-1611): the digests of every upload, the check at download (or its key off), every read
 * authenticated by Quader's format on an encrypted connection, "Digest checks only" on a plain one, and the writes the store confirmed by
 * their checksum where its start check found it so.
 */
export function checkMarks(c: StorageConnection, downloadVerify: boolean): readonly CheckMark[] {
  const marks: CheckMark[] = [
    { kind: 'upload', on: true },
    { kind: 'download', on: downloadVerify },
    c.encrypted ? { kind: 'every-read', on: true } : { kind: 'digest-only', on: false },
  ];
  if (c.capabilities) marks.push({ kind: 'writes', on: c.capabilities.payloadChecksumEnforced });
  return marks;
}

/**
 * The section **Integrity** of Settings › Storage (api-and-ui.md R-UI-265; storage.md R-STOR-002, R-STOR-013, R-STOR-027, R-STOR-082,
 * R-STOR-108; the owner's second message: the state of the format's integrity check): per connection that holds content the checks as
 * marks, the scrub's coverage within its period with its last and next run and **Scrub now**, and the copies found damaged in the period
 * with their bytes and outcome; then the findings of the period. Nothing here deletes, drains or touches a key (B.11).
 */
@Component({
  selector: 'quader-storage-integrity',
  imports: [Busy, DataTable, DecimalPipe, FindingsList, Loading, NoValue, StorageMeter, TimePipe],
  templateUrl: './integrity-section.html',
  styleUrl: './storage-sections.scss',
})
export class IntegritySection {
  readonly overview = input.required<StorageOverview>();
  readonly connections = input.required<readonly StorageConnection[]>();
  /** The period's cells and totals; null while they are read. */
  readonly history = input<StorageHistory | null>(null);
  readonly period = input.required<StoragePeriod>();
  readonly canWrite = input(false);
  /** The connections whose scrub, started here, runs. */
  readonly scrubbing = input<ReadonlySet<string>>(new Set());
  readonly scrub = output<string>();
  /** A finding changed by an act here: the page reads its figures again. */
  readonly changed = output<void>();

  private readonly findings = viewChild(FindingsList);

  protected readonly size = formatSize;
  protected readonly percent = percentOf;
  protected readonly inFormat = inFormat;

  protected readonly caption = $localize`:@@storage.integrity.caption:Integrity per connection`;
  protected readonly columns = computed<readonly TableColumn[]>(() => [
    { id: 'connection', label: $localize`:@@storage.col.connection:Connection`, width: '9rem' },
    {
      id: 'checks',
      label: $localize`:@@storage.col.checks:Checks`,
      width: '15rem',
      prefer: true,
      help: $localize`:@@storage.col.checks.help:Every upload's digests are kept. A whole download is checked against them (storage.download.verify). On an encrypted connection Quader's format authenticates every 64 KiB of every read.`,
    },
    {
      id: 'scrub',
      label: $localize`:@@storage.col.scrub:Scrub`,
      width: '14rem',
      help: $localize`:@@storage.col.scrub.help:The scrub reads every copy whole once per scrub_period and replaces a damaged one from another copy in the same run.`,
    },
    { id: 'damaged', label: $localize`:@@storage.col.damaged-period:Damaged · ${periodWord(this.period())}:period:`, width: '11rem' },
  ]);

  protected readonly rows = computed(() => holdingConnections(this.connections()));
  protected readonly anyOpen = computed(() => this.overview().problems.open > 0);

  /** A connection's history of the period; null while it is read or when the period names no such connection. */
  protected historyOf(c: StorageConnection): StorageHistoryConnection | null {
    return this.history()?.connections.find((h) => h.id === c.id) ?? null;
  }

  /** The reads of the period that the format refused because the copy names a key the replica does not know: they mark no copy. */
  protected unknownKeyReads(h: StorageHistoryConnection): number {
    return h.buckets.reduce((n, b) => n + b.unknownKeyReads, 0);
  }

  protected marks(c: StorageConnection): readonly CheckMark[] {
    return checkMarks(c, this.overview().integrity.downloadVerify);
  }

  protected markText(m: CheckMark): string {
    switch (m.kind) {
      case 'upload':
        return $localize`:@@storage.check.upload:Digest at upload`;
      case 'download':
        return m.on ? $localize`:@@storage.check.download:Download check` : $localize`:@@storage.check.download-off:Download check off`;
      case 'every-read':
        return $localize`:@@storage.check.every-read:Every read checked`;
      case 'digest-only':
        return $localize`:@@storage.check.digest-only:Digest checks only`;
    }
    return m.on ? $localize`:@@storage.check.writes:Writes confirmed` : $localize`:@@storage.check.writes-off:Writes not confirmed`;
  }

  /** The scrub's coverage of a connection: its present copies read whole within its period over all; null before the first count. */
  protected coverage(c: StorageConnection): number | null {
    const s = c.summary;
    if (!s) return null;
    return s.presentCopies === 0 ? 1 : s.verifiedWithinPeriod / s.presentCopies;
  }

  protected periodDays(c: StorageConnection): number | null {
    return c.scrubPeriodSeconds === null ? null : Math.round(c.scrubPeriodSeconds / 86_400);
  }

  protected coverageLabel(c: StorageConnection): string {
    return $localize`:@@storage.scrub.meter:Copies of ${c.id}:id: read whole within the period`;
  }

  protected scrubLabel(c: StorageConnection): string {
    return $localize`:@@storage.scrub.now-label:Scrub ${c.id}:id: now`;
  }

  /** Reads the findings shown again in place, with the page's live read (R-UI-213). */
  refresh(): Promise<void> {
    return this.findings()?.refresh() ?? Promise.resolve();
  }
}
