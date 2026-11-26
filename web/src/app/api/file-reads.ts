import { PageFetcher } from './pagination';
import { Schema } from './types';

export type Item = Schema<'Item'>;
export type Child = Schema<'Child'>;

/**
 * What the views of a repository's folders and files read (R-UI-010, R-UI-223): a folder's entries a page at a time and one item. The
 * main installation's `FilesApi` extends it with the uploads, the deletes, the count of a folder and the download statistics; the
 * Bastion's web interface reads the catalogue's documents of a public generic repository (R-UI-229), and the same views read from
 * wherever their application reads. A file's clients' commands are the client set-up's (`ClientSetupReads`), which the command view
 * reads for a package's version too (Q-UI-1454).
 */
export abstract class FileReads {
  /** The file or the folder at the path; rejects with 404 for a path that holds neither. */
  abstract item(repository: string, path: string): Promise<Item>;
  /** The direct children of a folder; `path` '' is the root of the repository. */
  abstract children(repository: string, path: string, limit: number): PageFetcher<Child>;
}
