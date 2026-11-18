/** What the caller may do at a folder or a file of a repository's files (R-UI-011, Q-UI-512). */
export interface FilePermissions {
  /** Upload files and create folders here. */
  readonly deploy: boolean;
  /** Replace a file that exists, in the upload dialog. */
  readonly overwrite: boolean;
  /** Delete the folder or the file of the page. */
  readonly delete: boolean;
}

export const NO_FILE_PERMISSIONS: FilePermissions = { deploy: false, overwrite: false, delete: false };

/** The last segment of a path: the name of a folder or a file. */
export function nameOf(path: string): string {
  return path.slice(path.lastIndexOf('/') + 1);
}

/** The folder a path lies in; '' is the root of the repository. */
export function parentOf(path: string): string {
  const i = path.lastIndexOf('/');
  return i < 0 ? '' : path.slice(0, i);
}

/**
 * The Material icon of an entry: a folder or a file, one icon each, so that a list of both reads at a glance (Q-UI-520). An
 * archive is a file: `folder_zip` has a folder's outline and was read as a folder.
 */
export function iconOf(kind: 'file' | 'folder'): string {
  return kind === 'folder' ? 'folder' : 'description';
}
