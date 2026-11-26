import { AuditDownload, AuditEvent } from '../../api/audit-api';
import { formatSize } from '../packages/format-size';
import { elapsedText } from '../../shared/duration-text';
import { versionText } from '../../shared/upstream-downloads/download-words';

/*
 * The words of a download's row in the audit log (api-and-ui.md R-UI-037, operations-and-observability.md R-OPS-055): the unit a person
 * asked for, named as the top bar's downloads name it (R-UI-211), its items and their bytes, and how long it took.
 */

/**
 * What a download's row names: the package and its version or tag, muted beside it; an image pulled by its digest, which names no
 * tag, by the digest of its first item, the manifest its client asked for first (R-OPS-055); the unit alone where the events name no
 * package.
 */
export function downloadSubject(d: AuditDownload): { readonly name: string; readonly reference: string; readonly title: string } {
  const name = d.package ?? d.unit;
  if (d.version) return { name, reference: versionText(d.version), title: `${name} ${d.version}` };
  const first = d.first[0]?.sha256;
  if (first) return { name, reference: versionText(`sha256:${first}`), title: `${name} sha256:${first}` };
  return { name, reference: '', title: name };
}

/** The seconds from a download's first item to its last; 0 for a download of one item. */
export function downloadSeconds(e: Pick<AuditEvent, 'occurredAt'>, d: Pick<AuditDownload, 'lastAt'>): number {
  return Math.max(0, (Date.parse(d.lastAt) - Date.parse(e.occurredAt)) / 1000);
}

/** The time a download took from its first item to its last (`12 s`, `< 1 s`), or null for a download of one item. */
export function downloadTook(e: Pick<AuditEvent, 'occurredAt'>, d: AuditDownload): string | null {
  return d.items > 1 ? elapsedText(downloadSeconds(e, d)) : null;
}

/** The bytes of a download's items (`48.1 MiB`), null where an item carries no size. */
export function downloadSize(d: Pick<AuditDownload, 'size'>): string | null {
  return d.size === null ? null : formatSize(d.size);
}
