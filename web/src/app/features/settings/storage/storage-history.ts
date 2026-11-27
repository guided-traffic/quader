import { StorageFinding, StorageHistoryBucket, StorageHistoryConnection, StoragePeriod, StorageProblem } from '../../../api/storage-api';
import { formatTime } from '../../../shared/time-format';
import { formatSize } from '../../packages/format-size';

/**
 * Pure helpers of the history on Settings › Storage (api-and-ui.md R-UI-265; storage.md R-STOR-111): the words of a period, a problem's
 * kind, a finding's source, reason and state, a duration, and a cell's text summary. Every tone and state is the server's (Q-STOR-528);
 * what stands here picks the words a value of the API is shown with.
 */

/** The period's word in the group of the header and the tile's label (E.2). */
export function periodWord(p: StoragePeriod): string {
  switch (p) {
    case '24h':
      return $localize`:@@storage.period.24h:24 h`;
    case '7d':
      return $localize`:@@storage.period.7d:7 d`;
    case '30d':
      return $localize`:@@storage.period.30d:30 d`;
    case '90d':
      return $localize`:@@storage.period.90d:90 d`;
  }
  return $localize`:@@storage.period.1y:1 year`;
}

/** The period's name for a screen reader and a tooltip: "the last 30 days". */
export function periodLong(p: StoragePeriod): string {
  switch (p) {
    case '24h':
      return $localize`:@@storage.period.24h.long:the last 24 hours`;
    case '7d':
      return $localize`:@@storage.period.7d.long:the last 7 days`;
    case '30d':
      return $localize`:@@storage.period.30d.long:the last 30 days`;
    case '90d':
      return $localize`:@@storage.period.90d.long:the last 90 days`;
  }
  return $localize`:@@storage.period.1y.long:the last year`;
}

/** A length of time in at most its two largest units: "1 h 12 min", "3 d 4 h", "4 min", "less than a minute". */
export function durationText(seconds: number): string {
  const s = Math.max(0, Math.floor(seconds));
  const d = Math.floor(s / 86_400);
  const h = Math.floor((s % 86_400) / 3_600);
  const m = Math.floor((s % 3_600) / 60);
  if (d > 0) return h > 0 ? $localize`:@@storage.duration.dh:${d}:days: d ${h}:hours: h` : $localize`:@@storage.duration.d:${d}:days: d`;
  if (h > 0) return m > 0 ? $localize`:@@storage.duration.hm:${h}:hours: h ${m}:minutes: min` : $localize`:@@storage.duration.h:${h}:hours: h`;
  if (m > 0) return $localize`:@@storage.duration.m:${m}:minutes: min`;
  return $localize`:@@storage.duration.short:less than a minute`;
}

/** A problem's kind in words (E.2). */
export function problemKindWord(kind: StorageProblem['kind']): string {
  switch (kind) {
    case 'unreachable':
      return $localize`:@@storage.problem.unreachable:Unreachable`;
    case 'start_check_failed':
      return $localize`:@@storage.problem.start-check-failed:Start check failed`;
    case 'copies_damaged':
      return $localize`:@@storage.problem.copies-damaged:Copies damaged`;
    case 'uploads_refused':
      return $localize`:@@storage.problem.uploads-refused:Uploads refused`;
  }
  return $localize`:@@storage.problem.reads-unavailable:Reads not served`;
}

/** Where a finding came from (E.2). */
export function sourceWord(source: string): string {
  switch (source) {
    case 'download':
      return $localize`:@@storage.source.download:Download`;
    case 'read':
      return $localize`:@@storage.source.read:Read`;
    case 'scrub':
      return $localize`:@@storage.source.scrub:Scrub`;
    case 'recheck':
      return $localize`:@@storage.source.recheck:Recheck`;
    case 'reconcile':
      return $localize`:@@storage.source.reconcile:Copy`;
    case 'drain':
      return $localize`:@@storage.source.drain:Drain`;
    case 'sweep':
      return $localize`:@@storage.source.sweep:Sweep`;
  }
  return $localize`:@@storage.source.other:Other`;
}

/** Why a copy was found damaged (E.2); a step of Quader's format by its name. */
export function reasonWord(reason: string): string {
  switch (reason) {
    case 'not_found':
    case 'missing':
      return $localize`:@@storage.reason.not-found:Not found`;
    case 'size':
      return $localize`:@@storage.reason.size:Size`;
    case 'digest':
    case 'corrupt':
      return $localize`:@@storage.reason.digest:Digest`;
    case 'record':
      return $localize`:@@storage.reason.record:Record`;
    case 'commitment':
      return $localize`:@@storage.reason.commitment:Commitment`;
    case 'segment':
      return $localize`:@@storage.reason.segment:Segment`;
    case 'final':
      return $localize`:@@storage.reason.final:Final segment`;
    case 'format':
      return $localize`:@@storage.reason.format:Format`;
    case 'destroyed_key':
      return $localize`:@@storage.reason.destroyed-key:Destroyed key`;
    case 'unknown_key':
      return $localize`:@@storage.reason.unknown-key:Unknown key`;
  }
  return reason;
}

/** A finding's state for the list: open, acknowledged (never resolved by it, Q-STOR-530) or its resolution. */
export type FindingState = 'open' | 'acknowledged' | 'rewritten' | 'found_again' | 'superseded' | 'deleted' | 'reconciled';

export function findingState(f: StorageFinding): FindingState {
  if (f.resolution) return f.resolution;
  return f.acknowledged ? 'acknowledged' : 'open';
}

/** A finding's state in words (E.2): a resolution says what happened to the copy. */
export function findingStateWord(state: FindingState): string {
  switch (state) {
    case 'open':
      return $localize`:@@storage.finding.open:Open`;
    case 'acknowledged':
      return $localize`:@@storage.finding.acknowledged:Acknowledged`;
    case 'rewritten':
      return $localize`:@@storage.finding.rewritten:Repaired`;
    case 'found_again':
      return $localize`:@@storage.finding.found-again:Found again`;
    case 'superseded':
      return $localize`:@@storage.finding.superseded:Superseded`;
    case 'deleted':
      return $localize`:@@storage.finding.deleted:Deleted`;
  }
  return $localize`:@@storage.finding.reconciled:Resolved`;
}

/** A finding state's tone: warning while open, none once acknowledged or resolved (green only for a positive state: none here). */
export function findingTone(state: FindingState, unavailable: boolean): 'bad' | 'warn' | 'neutral' {
  if (state === 'open') return unavailable ? 'bad' : 'warn';
  return 'neutral';
}

/** The sums of a connection's cells over the period. */
export interface PeriodSums {
  readonly readFallovers: number;
  readonly writeFallovers: number;
  readonly copiesFailed: number;
  readonly readsRefused: number;
  readonly unknownKeyReads: number;
  readonly damagedCopies: number;
  readonly damagedBytes: number;
  readonly unreachableSeconds: number;
}

export function periodSums(c: StorageHistoryConnection | null | undefined): PeriodSums {
  const sums = {
    readFallovers: 0,
    writeFallovers: 0,
    copiesFailed: 0,
    readsRefused: 0,
    unknownKeyReads: 0,
    damagedCopies: 0,
    damagedBytes: 0,
    unreachableSeconds: 0,
  };
  for (const b of c?.buckets ?? []) {
    sums.readFallovers += b.readFallovers;
    sums.writeFallovers += b.writeFallovers;
    sums.copiesFailed += b.copiesFailed;
    sums.readsRefused += b.readsRefused;
    sums.unknownKeyReads += b.unknownKeyReads;
    sums.damagedCopies += b.damagedCopies;
    sums.damagedBytes += b.damagedBytes;
    sums.unreachableSeconds += b.unreachableSeconds;
  }
  return sums;
}

/** The time a cell starts, as its summary names it: the minutes for cells shorter than a day, the date otherwise (R-UI-250). */
export function cellStart(b: StorageHistoryBucket, bucketSeconds: number): string {
  return formatTime(b.start, { part: bucketSeconds < 86_400 ? 'minutes' : 'date' });
}

/**
 * What happened in a cell, in words (Q-UI-1600): the outage with its length and replicas, the failing start checks, the copies found
 * damaged and the blobs no other copy held, then what the reads and writes absorbed or refused. "Nothing" for a quiet cell.
 */
export function cellParts(b: StorageHistoryBucket): readonly string[] {
  const out: string[] = [];
  if (b.unreachableSeconds > 0) {
    out.push($localize`:@@storage.cell.unreachable:unreachable ${durationText(b.unreachableSeconds)}:duration: on ${b.replicas}:replicas: replicas`);
  }
  if (b.startChecksFailed > 0) out.push($localize`:@@storage.cell.start-checks:${b.startChecksFailed}:count: failed start checks`);
  if (b.damagedCopies > 0) {
    out.push($localize`:@@storage.cell.damaged:${b.damagedCopies}:count: copies damaged (${formatSize(b.damagedBytes)}:size:)`);
  }
  if (b.unavailableBlobs > 0) out.push($localize`:@@storage.cell.unavailable:${b.unavailableBlobs}:count: blobs without another copy`);
  if (b.readsUnavailable > 0) out.push($localize`:@@storage.cell.reads-unavailable:${b.readsUnavailable}:count: reads not served`);
  if (b.uploadsRefused > 0) out.push($localize`:@@storage.cell.uploads-refused:${b.uploadsRefused}:count: uploads refused`);
  if (b.readFallovers > 0) out.push($localize`:@@storage.cell.read-fallovers:${b.readFallovers}:count: reads from another copy`);
  if (b.writeFallovers > 0) out.push($localize`:@@storage.cell.write-fallovers:${b.writeFallovers}:count: writes to the next connection`);
  if (b.copiesFailed > 0) out.push($localize`:@@storage.cell.copies-failed:${b.copiesFailed}:count: copies failed`);
  if (b.readsRefused > 0) out.push($localize`:@@storage.cell.reads-refused:${b.readsRefused}:count: reads refused by the format`);
  return out;
}

/** A cell's whole summary: its start, then what happened, or that nothing did. */
export function cellSummary(b: StorageHistoryBucket, bucketSeconds: number): string {
  const parts = cellParts(b);
  const what = parts.length > 0 ? parts.join(', ') : $localize`:@@storage.cell.nothing:nothing`;
  return `${cellStart(b, bucketSeconds)}: ${what}`;
}

/** A strip's summary for assistive technology: how many cells had a warning and an error, or that the period was quiet. */
export function stripSummary(buckets: readonly StorageHistoryBucket[]): string {
  const warn = buckets.filter((b) => b.tone === 'warning').length;
  const bad = buckets.filter((b) => b.tone === 'error').length;
  if (warn === 0 && bad === 0) return $localize`:@@storage.strip.quiet:Nothing happened in this period.`;
  return $localize`:@@storage.strip.summary:${buckets.length}:cells: cells: ${bad}:errors: with clients refused, ${warn}:warnings: absorbed by Quader.`;
}
