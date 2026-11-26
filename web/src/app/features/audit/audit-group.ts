import { AuditAct, AuditEvent, AuditGroup } from '../../api/audit-api';
import { formatSize } from '../packages/format-size';
import { elapsedText } from '../../shared/duration-text';
import { versionText } from '../../shared/upstream-downloads/download-words';

/*
 * The words of a row of several events in the audit log (api-and-ui.md R-UI-037, operations-and-observability.md R-OPS-055, R-OPS-056):
 * what a person did (a download, a push, a publish, a delete), the unit it did it to, named as the top bar's downloads name it
 * (R-UI-211), its items and their bytes, and how long it took.
 */

/** The act a row names, in the words of the row's action (R-OPS-056): one word of what the person did, the unit beside it. */
export function actLabel(act: AuditAct): string {
  switch (act) {
    case 'download':
      return $localize`:@@audit.act.download:Downloaded`;
    case 'datakeys':
      return $localize`:@@audit.act.datakeys:Data keys handed`;
    case 'push':
      return $localize`:@@audit.act.push:Pushed`;
    case 'publish':
      return $localize`:@@audit.act.publish:Published`;
    case 'upload':
      return $localize`:@@audit.act.upload:Uploaded`;
    case 'delete':
      return $localize`:@@audit.act.delete:Deleted`;
    case 'writes':
      return $localize`:@@audit.act.writes:Writes allowed`;
  }
}

/**
 * What a group's row names: the package and its version or tag, muted beside it; several versions by their number (a delete of a
 * package of five versions); an image pulled by its digest, which names no tag, by the digest of its first item, the manifest its
 * client asked for first (R-OPS-055); the unit alone where the events name no package (a folder of files).
 */
export function groupSubject(g: AuditGroup): { readonly name: string; readonly reference: string; readonly title: string } {
  const name = g.package ?? g.unit;
  if (g.versions > 1) {
    const reference = $localize`:@@audit.group.versions:${g.versions}:count: versions`;
    return { name, reference, title: `${name} ${reference}` };
  }
  if (g.version) return { name, reference: versionText(g.version), title: `${name} ${g.version}` };
  const first = g.first[0]?.sha256;
  if (first && g.package) return { name, reference: versionText(`sha256:${first}`), title: `${name} sha256:${first}` };
  return { name, reference: '', title: name };
}

/** The seconds from a group's first item to its last; 0 for a group of one item. */
export function groupSeconds(e: Pick<AuditEvent, 'occurredAt'>, g: Pick<AuditGroup, 'lastAt'>): number {
  return Math.max(0, (Date.parse(g.lastAt) - Date.parse(e.occurredAt)) / 1000);
}

/** The time a group took from its first item to its last (`12 s`, `< 1 s`), or null for a group of one item. */
export function groupTook(e: Pick<AuditEvent, 'occurredAt'>, g: AuditGroup): string | null {
  return g.items > 1 ? elapsedText(groupSeconds(e, g)) : null;
}

/** The bytes of a group's files (`48.1 MiB`), null where a file carries no size or the group holds none. */
export function groupSize(g: Pick<AuditGroup, 'size'>): string | null {
  return g.size === null ? null : formatSize(g.size);
}

/** The actions whose event is one file with its bytes; the other items of an act name a version, a manifest or a decision. */
const FILE_ACTIONS: readonly string[] = ['artifact.downloaded', 'bastion.data_key.handed', 'artifact.deployed', 'artifact.deleted'];

/** Whether an item of a group is a file (its path and digest are the file's) rather than the event of a version or a decision. */
export function isFileItem(action: string): boolean {
  return FILE_ACTIONS.includes(action);
}
