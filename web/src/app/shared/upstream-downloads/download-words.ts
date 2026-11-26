import { UpstreamDownload, UpstreamDownloadFinished, UpstreamDownloadOutcome } from '../../api/upstream-downloads-api';
import { failureClassText, queueOriginText } from '../../features/repositories/downloads/download-words';
import { formatSize } from '../../features/packages/format-size';
import { elapsedText } from '../duration-text';

/*
 * The words of the downloads panel of the top bar (api-and-ui.md R-UI-210 to R-UI-212). A row is one download: the unit a person asked
 * for, an image with every file that belongs to it, a version's package, a queue row's run (operations-and-observability.md R-OPS-049);
 * its files are never listed. The queue's origins and failure classes are the queue's own words (R-CUR-018), so a row reads the same
 * here and on a repository's tab Downloads.
 */

/** The last segment of a path; a digest in it cut to its algorithm and 12 hex digits (`sha256:1a2b3c4d5e6f`). */
export function fileOf(path: string): string {
  const last = path.split('/').filter((p) => p !== '').pop() ?? path;
  const digest = /^([a-z0-9]+):([0-9a-f]{12})[0-9a-f]+$/.exec(last);
  return digest ? `${digest[1]}:${digest[2]}` : last;
}

/** A version that is an image's digest (an image asked for by its digest) cut as a file's digest is; any other version as it is. */
export function versionText(version: string): string {
  const digest = /^([a-z0-9]+):([0-9a-f]{12})[0-9a-f]+$/.exec(version);
  return digest ? `${digest[1]}:${digest[2]}` : version;
}

/**
 * What a row names (R-UI-211): the name and the version, else the name alone (an image whose tag is not known yet), else the first
 * file of a download of no package. `name` is what stands as the row's name, `detail` what stands muted beside it.
 */
export function subjectOf(d: Pick<UpstreamDownload, 'name' | 'version' | 'path'>): { readonly name: string; readonly detail: string } {
  if (d.name === '') return { name: fileOf(d.path), detail: '' };
  return { name: d.name, detail: versionText(d.version) };
}

/** The title of a row's subject: the name with the whole version (a digest uncut), or the first file's path of a download of no package. */
export function subjectTitle(d: Pick<UpstreamDownload, 'name' | 'version' | 'path'>): string {
  if (d.name === '') return d.path;
  return d.version === '' ? d.name : `${d.name} ${d.version}`;
}

/** The speed in the unit of the sizes per second (`12.3 MiB/s`); the en dash while the API has none yet (the first second). */
export function speedText(bytesPerSecond: number | null): string {
  return bytesPerSecond === null ? '–' : `${formatSize(bytesPerSecond)}/s`;
}

/** A running download's figures: "128.4 MiB of 286.9 MiB · 12.3 MiB/s · 42 s", without a total "128.4 MiB · 12.3 MiB/s · 42 s". */
export function runningFigures(d: { readonly bytes: number; readonly total: number | null; readonly bytesPerSecond: number | null; readonly elapsedSeconds: number }): readonly string[] {
  const size =
    d.total === null
      ? formatSize(d.bytes)
      : $localize`:@@upstream-downloads.of:${formatSize(d.bytes)}:bytes: of ${formatSize(d.total)}:total:`;
  return [size, speedText(d.bytesPerSecond), elapsedText(d.elapsedSeconds)];
}

/** A finished download's figures: "286.9 MiB · 1 min 12 s · 4.0 MiB/s"; no average below one second ("2.2 MiB · < 1 s"). */
export function finishedFigures(d: Pick<UpstreamDownloadFinished, 'bytes' | 'durationSeconds' | 'bytesPerSecond'>): readonly string[] {
  const parts = [formatSize(d.bytes), elapsedText(d.durationSeconds)];
  if (d.durationSeconds >= 1 && d.bytesPerSecond !== null) parts.push(speedText(d.bytesPerSecond));
  return parts;
}

/**
 * What caused a download, in words (R-UI-211): **Client · sam** or **Client**; for the queue the person who started the row, else
 * the queue's word for its origin; **Mirror pass**; **Vulnerability database**.
 */
export function originText(d: Pick<UpstreamDownload, 'origin' | 'queueOrigin' | 'by'>): string {
  switch (d.origin) {
    case 'client': {
      const client = queueOriginText('client');
      return d.by ? `${client} · ${d.by.name}` : client;
    }
    case 'queue':
      if (d.queueOrigin === 'person' || d.queueOrigin === null) return d.by?.name ?? queueOriginText('person');
      return queueOriginText(d.queueOrigin);
    case 'mirror':
      return $localize`:@@upstream-downloads.origin.mirror:Mirror pass`;
    case 'scanner_database':
      return $localize`:@@upstream-downloads.origin.scanner-database:Vulnerability database`;
  }
}

/** The badge of a finished download that did not complete (R-UI-212); null for one that completed. */
export interface OutcomeBadge {
  readonly tone: 'bad' | 'warn' | 'neutral';
  readonly icon: string | null;
  readonly text: string;
  readonly title: string | null;
}

/** The title of a failed download's badge: the failure's code and the file it failed at (R-UI-212), the code alone without one. */
function failureTitle(code: string, path: string | null | undefined): string {
  if (!path) return code;
  return $localize`:@@upstream-downloads.failed-file:${code}:code: at the file ${path}:path:`;
}

export function outcomeBadge(d: Pick<UpstreamDownloadFinished, 'outcome' | 'failure'>): OutcomeBadge | null {
  const outcome: UpstreamDownloadOutcome = d.outcome;
  switch (outcome) {
    case 'completed':
      return null;
    case 'failed': {
      const failed = $localize`:@@upstream-downloads.outcome.failed:Failed`;
      return {
        tone: 'bad',
        icon: 'error',
        text: d.failure ? `${failed}: ${failureClassText(d.failure.class)}` : failed,
        title: d.failure ? failureTitle(d.failure.code, d.failure.path) : null,
      };
    }
    case 'cancelled':
      return { tone: 'neutral', icon: null, text: $localize`:@@upstream-downloads.outcome.cancelled:Cancelled`, title: null };
    case 'interrupted':
      return {
        tone: 'warn',
        icon: 'warning',
        text: $localize`:@@upstream-downloads.outcome.interrupted:Interrupted`,
        title: $localize`:@@upstream-downloads.outcome.interrupted-title:A replica stopped while this download ran.`,
      };
  }
}

/** The count of the badge on the button: the number, "99+" above 99. */
export function countText(n: number): string {
  return n > 99 ? '99+' : String(n);
}
