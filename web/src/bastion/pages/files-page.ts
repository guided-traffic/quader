import { Component, computed, inject, input, resource } from '@angular/core';
import { FileReads, Item } from '../../app/api/file-reads';
import { filesAddress } from '../../app/api/refs';
import { contentShapeOf } from '../../app/views/repositories/content-shape';
import { ErrorNotice } from '../../app/shared/error-notice';
import { failureOf, valueOf } from '../../app/shared/paged-resource';
import { Loading } from '../../app/shared/ui/loading';
import { FileView } from '../../app/views/files/file-view';
import { FilesHead } from '../../app/views/files/files-head';
import { FolderView } from '../../app/views/files/folder-view';
import { BastionRepositories } from '../data/repositories';
import { BastionRepositoryHead } from './repository-head';

/** What the address names: a folder, or a file with its description. */
type At = { readonly path: string; readonly kind: 'folder' } | { readonly path: string; readonly kind: 'file'; readonly item: Item };

/**
 * A public generic repository's folders and files at the Bastion (R-UI-229, Q-UI-1455), at the main installation's addresses: the
 * repository's own address is the root of its files, `…/files/<path…>` a folder or a file. The pages are the main installation's views
 * (views/files/, R-UI-223) without an act: the path with a file's **Download** through the Bastion's own package route and its
 * clients' commands, a folder's entries, a file's facts with its malware verdict and its signature as the catalogue keeps them, and its
 * checksums; no upload, no delete, no download statistics, no description by the management API (Q-UI-1457). A path its parent folder
 * does not list is read as a file, whose answer (not found) the notice shows whole. Its head is the repository page's (R-UI-273): the
 * key, the description or what the files are, the format and the class, **Set up client**; while the repositories are hidden its key
 * stays as the folder's name every download address of the files has, without class and description (R-UI-274).
 */
@Component({
  selector: 'quader-bastion-files',
  imports: [BastionRepositoryHead, ErrorNotice, FileView, FilesHead, FolderView, Loading],
  template: `
    <quader-bastion-repository-head
      [project]="project()"
      [key]="key()"
      [repository]="repository()"
      [here]="path() === ''"
      [fallback]="sentence"
    />
    @if (path() !== '') {
      <quader-files-head [repository]="ref()" [path]="path()" [root]="root()" [file]="file()" [generic]="true" [verb]="shape.commandVerb" />
    }
    @if (failure(); as f) {
      <quader-error-notice [error]="f" (retry)="failure.retry()" />
    } @else {
      @switch (kind()) {
        @case ('folder') {
          <quader-folder-view [repository]="ref()" [filesAddress]="filesAddress()" [path]="path()" fill="none" [shape]="shape" [generic]="true" />
        }
        @case ('file') {
          @if (file(); as it) {
            <quader-file-view [item]="it" format="generic" [signatures]="false" />
          }
        }
        @default {
          <quader-loading size="page" i18n="@@repo.item.loading">Loading the item.</quader-loading>
        }
      }
    }
  `,
})
export class FilesPage {
  private readonly files = inject(FileReads);
  private readonly repositories = inject(BastionRepositories);

  /** Route parameters, bound by the router's component input binding: the project, the repository, the path after `files`. */
  readonly project = input.required<string>();
  readonly key = input.required<string>();
  readonly filePath = input<string | undefined>();

  protected readonly ref = computed(() => `${this.project()}/${this.key()}`);
  /** The repository's facts for the head; a failed read leaves the head with its key and the files' sentence. */
  private readonly facts = resource({
    params: () => ({ project: this.project(), key: this.key() }),
    loader: ({ params }) => this.repositories.one(params.project, params.key).catch(() => null),
  });
  protected readonly repository = computed(() => valueOf(this.facts) ?? null);
  /** The folder or file of the page; '' is the root of the repository. */
  protected readonly path = computed(() => (this.filePath() ?? '').split('/').filter((s) => s !== '').join('/'));
  /** The root of the files: the repository's own address, as a generic repository's at the main installation. */
  protected readonly root = computed(() => ['/projects', this.project(), 'repositories', this.key()]);
  protected readonly shape = contentShapeOf('generic');
  protected readonly sentence = $localize`:@@bastion.files.sentence:The files published in this repository.`;
  /** The repository's address for downloads by path at this Bastion: its own origin, never another (R-BAS-120). */
  protected readonly filesAddress = computed(() => filesAddress(location.origin, this.ref()));

  /**
   * Whether the path names a file (its description) or a folder (its entries), as its parent folder's entry says: the parent's document
   * is read, then a file's description; no read answers 404 for what the page shows (a probe of the file's document for a folder did).
   */
  private readonly at = resource({
    params: () => ({ repo: this.ref(), path: this.path() }),
    loader: async ({ params }): Promise<At> => {
      if (params.path === '') return { path: '', kind: 'folder' };
      const cut = params.path.lastIndexOf('/');
      const parent = cut < 0 ? '' : params.path.slice(0, cut);
      const name = params.path.slice(cut + 1);
      const entries = await this.files.children(params.repo, parent, Number.MAX_SAFE_INTEGER)(null, Number.MAX_SAFE_INTEGER);
      const entry = entries.items.find((c) => c.path === name || c.path === params.path);
      // A path the parent does not list is read as a file: the Bastion's answer, a 404, is what the notice shows whole (R-UI-216).
      if (entry?.kind === 'folder') return { path: params.path, kind: 'folder' };
      return { path: params.path, kind: 'file', item: await this.files.item(params.repo, params.path) };
    },
  });
  protected readonly failure = failureOf(this.at);
  private readonly current = computed(() => {
    const at = valueOf(this.at);
    return at && at.path === this.path() ? at : null;
  });
  protected readonly kind = computed(() => this.current()?.kind ?? 'loading');
  protected readonly file = computed(() => {
    const at = this.current();
    return at?.kind === 'file' ? at.item : null;
  });
}
