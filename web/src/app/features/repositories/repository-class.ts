import { RepositoryClass } from '../../api/repositories-api';

/** The classes of the repository document (`Repository.class` of the API), in the order a form offers them. */
export const CLASSES: readonly RepositoryClass[] = ['local', 'remote', 'virtual'];

export function classLabel(cls: string): string {
  switch (cls) {
    case 'local':
      return $localize`:@@repo.class.local:Local`;
    case 'remote':
      return $localize`:@@repo.class.remote:Remote`;
    case 'virtual':
      return $localize`:@@repo.class.virtual:Virtual`;
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
    default:
      return '';
  }
}

/** The icon of a class (a ligature of the icon font) wherever a class is written with its icon: the lists and a repository's header. */
export function classIcon(cls: string): string {
  return cls === 'local' ? 'storage' : cls === 'remote' ? 'cloud_download' : 'layers';
}
