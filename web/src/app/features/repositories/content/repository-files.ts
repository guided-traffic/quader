import { CdkMenu, CdkMenuItem, CdkMenuTrigger } from '@angular/cdk/menu';
import { Component, computed, effect, inject, input, output, signal, untracked } from '@angular/core';
import { RouterLink } from '@angular/router';
import { firstValueFrom } from 'rxjs';
import { Child, FilesApi } from '../../../api/files-api';
import { Notifier } from '../../../core/notifier';
import { filesCommands } from '../../../core/project-links';
import { CopyButton } from '../../../shared/copy-button';
import { PagedResource } from '../../../shared/paged-resource';
import { DataTable, TableColumn } from '../../../shared/ui/data-table';
import { QuaderDialog } from '../../../shared/ui/dialog';
import { formatSize } from '../../packages/format-size';
import { TimeAgo } from '../../../shared/ui/time-ago';
import { ContentShape, contentShapeOf, emptyContentText } from './content-shape';
import { FilePermissions, NO_FILE_PERMISSIONS, iconOf } from './file-paths';
import { FolderDialog, FolderDialogData } from './folder-dialog';
import { UploadDialog, UploadDialogData } from './upload-dialog';

/** `ui.browser.page_size` (R-UI-010, default 200): how many entries of a folder load at a time. The UI cannot read the setting. */
export const FILES_PAGE = 200;

/**
 * A folder of a repository's files (R-UI-010, R-UI-052): its entries as the shared data table, folders and files by name in the
 * order of their paths, in pages of 200 with **Load more** (R-API-025). A folder's name leads to the folder, a file's name to its
 * page; a file's row copies its download address. The filter narrows the entries loaded so far, because the server has no name
 * filter on children (Q-UI-513). Above the list, the operations the caller may use here (R-UI-011, Q-UI-512): **Upload files** and
 * **New folder** for a holder of `deploy` in a local repository that takes raw uploads, and **Delete folder**, which the page runs.
 */
@Component({
  selector: 'quader-repository-files',
  imports: [CdkMenu, CdkMenuItem, CdkMenuTrigger, TimeAgo, RouterLink, CopyButton, DataTable],
  template: `
    <div class="toolbar files-toolbar">
      <div class="filter">
        <input
          class="text-input"
          type="search"
          autocomplete="off"
          spellcheck="false"
          i18n-aria-label="@@repo.content.filter-files"
          aria-label="Filter files"
          i18n-placeholder="@@repo.content.filter-files"
          placeholder="Filter files"
          [value]="filter()"
          (input)="filter.set($any($event.target).value)"
        />
        <span class="material-icons" aria-hidden="true">search</span>
      </div>
      @if (permissions().deploy || (permissions().delete && path() !== '')) {
        <div class="files-actions">
          @if (permissions().deploy) {
            <button class="btn btn-default" type="button" (click)="upload()">
              <span class="material-icons" aria-hidden="true">upload</span>
              <ng-container i18n="@@files.upload">Upload files</ng-container>
            </button>
            <button class="btn btn-default" type="button" (click)="newFolder()">
              <span class="material-icons" aria-hidden="true">create_new_folder</span>
              <ng-container i18n="@@files.new-folder">New folder</ng-container>
            </button>
          }
          @if (permissions().delete && path() !== '') {
            <!-- Rare and destructive: behind the folder's menu, not beside the upload (Q-UI-512). -->
            <button
              class="btn btn-default btn-icon"
              type="button"
              [attr.aria-label]="folderActionsLabel"
              [cdkMenuTriggerFor]="folderMenu"
            >
              <span class="material-icons" aria-hidden="true">more_vert</span>
            </button>
            <ng-template #folderMenu>
              <div class="menu" cdkMenu>
                <button class="menu-item" cdkMenuItem type="button" (cdkMenuItemTriggered)="remove.emit()" i18n="@@files.delete-folder"
                  >Delete folder</button>
              </div>
            </ng-template>
          }
        </div>
      }
    </div>
    @if (filter() && list.hasMore()) {
      <p class="content-note filter-note" role="note" i18n="@@files.filter-partial">
        The filter searches the {{ list.items().length }} entries loaded so far. Load more to search the rest.
      </p>
    }
    <quader-data-table
      [caption]="caption"
      [columns]="columns"
      [loaded]="list.loaded()"
      [loading]="list.loading()"
      [error]="list.error()"
      [empty]="shown().length === 0"
      [emptyText]="emptyText()"
      [hasMore]="list.hasMore()"
      (more)="list.more()"
      (retry)="list.more()"
    >
      @for (c of shown(); track c.path) {
        <tr>
          <th scope="row" class="wrap-name">
            <a class="entry" [routerLink]="entryLink(c)">
              <span class="material-icons entry-icon" aria-hidden="true">{{ icon(c) }}</span>
              <span class="entry-name" translate="no">{{ c.path }}</span>
            </a>
          </th>
          <td class="num">
            @if (c.kind === 'file') {
              {{ size(c.size ?? 0) }}
            } @else {
              –
            }
          </td>
          <td>
            @if (c.modifiedAt; as at) {
              <quader-time-ago [at]="at" absolute="title" />
            } @else {
              –
            }
          </td>
          <td class="actions">
            @if (c.kind === 'file' && fileUrl(c); as url) {
              <quader-copy-button [value]="url" [label]="copyUrlLabel(c)" [quiet]="true" />
            }
          </td>
        </tr>
      }
    </quader-data-table>
  `,
  styleUrl: './repository-content.scss',
})
export class RepositoryFiles {
  private readonly files = inject(FilesApi);
  private readonly dialog = inject(QuaderDialog);
  private readonly notifier = inject(Notifier);

  /** The qualified name of the repository. */
  readonly repository = input.required<string>();
  /**
   * The repository's address for downloads by path (its raw files address, R-REPO-002), as the page has
   * it from the server's addresses or the format catalog; empty while it is not known.
   */
  readonly filesAddress = input<string>('');
  /** The folder shown; '' is the root. */
  readonly path = input<string>('');
  /** What the caller may do here, as the page decided it (Q-UI-512). */
  readonly permissions = input<FilePermissions>(NO_FILE_PERMISSIONS);
  /**
   * What the empty root says fills the repository for a caller the toolbar offers no upload (R-UI-144, Q-UI-555): `publisher`,
   * `other` or `none` (a disabled repository). It offers no button: **Set up client** is the header's, **Upload files** the
   * toolbar's.
   */
  readonly fill = input<'publisher' | 'other' | 'none'>('other');
  /** Changes when the folder changed by something the list did not do itself (a delete job that ended): the list reloads. */
  readonly revision = input(0);
  /** Delete this folder: the page asks and deletes (R-UI-011). */
  readonly remove = output<void>();

  protected readonly list = new PagedResource<Child>(FILES_PAGE);
  protected readonly filter = signal('');

  protected readonly shown = computed(() => {
    const f = this.filter().trim().toLowerCase();
    return f ? this.list.items().filter((c) => c.path.toLowerCase().includes(f)) : this.list.items();
  });

  /** The words of the repository's files: a generic repository's, or a package format's raw files, by its class. */
  readonly shape = input<ContentShape>(contentShapeOf('generic'));
  protected readonly caption = contentShapeOf('generic').itemsLabel;
  protected readonly emptyText = computed(() => {
    if (this.filter()) return $localize`:@@repo.content.files-empty-filtered:No file matches the filter.`;
    if (this.path() !== '') return $localize`:@@files.folder-empty:This folder is empty.`;
    if (this.permissions().deploy) return $localize`:@@files.empty-upload:This repository holds no files yet. Upload files here or with your client.`;
    return emptyContentText(this.shape(), this.fill());
  });
  protected readonly columns: readonly TableColumn[] = [
    { id: 'name', label: $localize`:@@repo.content.col.name:Name`, width: '12rem' },
    { id: 'size', label: $localize`:@@repo.content.col.size:Size`, numeric: true, width: '6rem' },
    { id: 'modified', label: $localize`:@@repo.content.col.modified:Last change`, width: '9rem' },
    { id: 'actions', label: $localize`:@@repo.col.actions:Actions`, hideLabel: true, width: '3rem' },
  ];

  constructor() {
    effect(() => {
      const repository = this.repository();
      const path = this.path();
      this.revision();
      untracked(() => {
        this.filter.set('');
        void this.list.start(this.files.children(repository, path, FILES_PAGE), FILES_PAGE);
      });
    });
  }

  private fullPath(c: Child): string {
    const dir = this.path();
    return dir ? `${dir}/${c.path}` : c.path;
  }

  protected entryLink(c: Child): string[] {
    return filesCommands(this.repository(), this.fullPath(c).split('/'));
  }

  protected icon(c: Child): string {
    return iconOf(c.kind);
  }

  /** The download address of a file: the repository's address for downloads by path and the file's path (R-REPO-002). */
  protected fileUrl(c: Child): string {
    const root = this.filesAddress();
    return root ? `${root.replace(/\/*$/, '/')}${this.fullPath(c).split('/').map(encodeURIComponent).join('/')}` : '';
  }

  protected readonly folderActionsLabel = $localize`:@@files.folder-actions:More actions for this folder`;

  protected copyUrlLabel(c: Child): string {
    return $localize`:@@files.copy-url-of:Copy the download address of ${c.path}:name:`;
  }

  protected size(bytes: number): string {
    return formatSize(bytes);
  }

  protected async upload(): Promise<void> {
    const data: UploadDialogData = {
      repository: this.repository(),
      folder: this.path(),
      canOverwrite: this.permissions().overwrite,
    };
    const ref = this.dialog.open<UploadDialog, UploadDialogData, number>(UploadDialog, { data, width: '40rem', maxWidth: '95vw' });
    const uploaded = (await firstValueFrom(ref.closed)) ?? 0;
    if (uploaded > 0) this.reload();
  }

  protected async newFolder(): Promise<void> {
    const data: FolderDialogData = { repository: this.repository(), parent: this.path() };
    const ref = this.dialog.open<FolderDialog, FolderDialogData, string>(FolderDialog, { data, width: '32rem', maxWidth: '95vw' });
    const created = await firstValueFrom(ref.closed);
    if (created) {
      this.notifier.status($localize`:@@files.folder-created:Folder ${created}:path: created.`);
      this.reload();
    }
  }

  private reload(): void {
    void this.list.start(this.files.children(this.repository(), this.path(), FILES_PAGE), FILES_PAGE);
  }
}
