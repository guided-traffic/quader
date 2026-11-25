import { FormatDescriptor, RepositoryClass } from '../../api/repositories-api';

/** The classes of the repository document (`Repository.class` of the API), in the order a form offers them. */
export const CLASSES: readonly RepositoryClass[] = ['local', 'remote', 'virtual'];

/**
 * What a repository is shown as wherever its class is written (R-UI-033, Q-UI-1203): its class, or `system` for a repository of a
 * format that is system only (`systemOnly` of `GET /api/v1/formats`, R-CLAMAV-001). Such a repository is of the class `local` in the
 * API, but nobody publishes to it: the installation fills it. The catalog's fact decides, never a format's name.
 */
export type ShownClass = RepositoryClass | 'system';

/** The formats of the catalog as far as the shown class needs them. */
type CatalogFacts = readonly Pick<FormatDescriptor, 'name' | 'systemOnly'>[];

/** Whether the catalog marks the format system only; false while the catalog is not known. */
export function isSystemOnly(format: string | null | undefined, formats: CatalogFacts | null | undefined): boolean {
  return !!format && !!formats?.some((f) => f.name === format && f.systemOnly);
}

/** The shown class of a repository (its `class` and `format`), from the catalog's formats; its class while they are not known. */
export function shownClass(r: { readonly class: string; readonly format: string }, formats: CatalogFacts | null | undefined): string {
  return isSystemOnly(r.format, formats) ? 'system' : r.class;
}

/** The formats a person creates repositories of, in the catalog's order: every one that is not system only (Q-UI-1200). */
export function creatableFormats<T extends Pick<FormatDescriptor, 'systemOnly'>>(formats: readonly T[]): readonly T[] {
  return formats.filter((f) => !f.systemOnly);
}

export function classLabel(cls: string): string {
  switch (cls) {
    case 'local':
      return $localize`:@@repo.class.local:Local`;
    case 'remote':
      return $localize`:@@repo.class.remote:Remote`;
    case 'virtual':
      return $localize`:@@repo.class.virtual:Virtual`;
    case 'system':
      return $localize`:@@repo.class.system:System`;
    default:
      return cls;
  }
}

export function classHelp(cls: string): string {
  switch (cls) {
    case 'local':
      return $localize`:@@repo.class.local.help:Holds content that is deployed to Quader.`;
    case 'remote':
      return $localize`:@@repo.class.remote.help:Caches content from an upstream registry.`;
    case 'virtual':
      return $localize`:@@repo.class.virtual.help:Presents several repositories under one address.`;
    case 'system':
      return $localize`:@@repo.class.system.help:Filled by this installation itself, on a schedule; nobody publishes to it.`;
    default:
      return '';
  }
}

/**
 * The tooltip of a shown class where it stands as a badge or a cell (Q-UI-1203): what a system repository is; nothing for the classes
 * a person creates, which the column's help explains.
 */
export function classTip(cls: string): string {
  return cls === 'system' ? classHelp(cls) : '';
}

/** The icon of a class (a ligature of the icon font) wherever a class is written with its icon: the lists and a repository's header. */
export function classIcon(cls: string): string {
  switch (cls) {
    case 'local':
      return 'storage';
    case 'remote':
      return 'cloud_download';
    case 'system':
      // The icon of the caller System on the usage pages, the installation's own background work (callers.ts, Q-UI-1203); the admin
      // area's `dns` read like Local's `storage` at 20 px.
      return 'settings';
    default:
      return 'layers';
  }
}

/** The values of a list's filter by class: the three classes, and System where the catalog has a system-only format (Q-UI-1203). */
export function classFilterValues(formats: CatalogFacts | null | undefined): readonly ShownClass[] {
  return formats?.some((f) => f.systemOnly) ? [...CLASSES, 'system'] : CLASSES;
}

/**
 * The list request's `class` and `format` for a filter by shown class (Q-UI-1203): `system` is the class `local` of the system-only
 * formats, `local` the class `local` of every other format, so that a reader who filters for Local to find where to publish meets no
 * repository nobody publishes to. A chosen format narrows either. `null` when no repository can match (System with a format that is
 * not system only); the parameters as they are while the catalog is not known.
 */
export function classFilterQuery(
  cls: string | undefined,
  format: string | undefined,
  formats: CatalogFacts | null | undefined,
): { readonly class?: string; readonly format?: string } | null {
  if (cls !== 'local' && cls !== 'system') return { class: cls, format };
  const system = (formats ?? []).filter((f) => f.systemOnly).map((f) => f.name);
  if (cls === 'local' && system.length === 0) return { class: cls, format };
  const pool = (formats ?? []).filter((f) => !!f.systemOnly === (cls === 'system')).map((f) => f.name);
  const chosen = format ? pool.filter((f) => f === format) : pool;
  return chosen.length === 0 ? null : { class: 'local', format: chosen.join(',') };
}
