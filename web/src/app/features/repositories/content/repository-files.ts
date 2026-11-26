import { CdkMenu, CdkMenuItem, CdkMenuTrigger } from '@angular/cdk/menu';
import { Component, computed, inject, input, output, viewChild } from '@angular/core';
import { firstValueFrom } from 'rxjs';
import { Notifier } from '../../../core/notifier';
import { QuaderDialog } from '../../../shared/ui/dialog';
import { ContentShape, contentShapeOf } from './content-shape';
import { FilePermissions, NO_FILE_PERMISSIONS, nameOf } from './file-paths';
import { ClientCommandMenu } from '../../../views/packages/client-command-menu';
import { FolderView } from '../../../views/files/folder-view';
import { FolderDialog, FolderDialogData } from './folder-dialog';
import { UploadDialog, UploadDialogData } from './upload-dialog';

/**
 * A folder of a repository's files at the main installation (R-UI-010, R-UI-052): the shared view of its entries
 * (views/files/folder-view.ts, R-UI-223) with the operations the caller may use here at the end of its toolbar (R-UI-011, Q-UI-512):
 * **Upload files** and **New folder** for a holder of `deploy` in a local repository that takes raw uploads, **Upload files** of a
 * generic repository with the chevron of the upload's commands for the folder (Q-UI-1334), and **Delete folder**, which the page runs.
 */
@Component({
  selector: 'quader-repository-files',
  imports: [CdkMenu, CdkMenuItem, CdkMenuTrigger, ClientCommandMenu, FolderView],
  template: `
    <quader-folder-view
      [repository]="repository()"
      [filesAddress]="filesAddress()"
      [path]="path()"
      [uploads]="permissions().deploy"
      [fill]="fill()"
      [revision]="revision()"
      [shape]="shape()"
      [generic]="generic()"
      [disabled]="disabled()"
    >
      @if (permissions().deploy || (permissions().delete && path() !== '')) {
        <div folderActs class="files-actions">
          @if (permissions().deploy && generic()) {
            <!-- The dialog is the main act; the chevron opens the upload's command for this folder in each client (R-GENERIC-017,
                 Q-UI-1334). -->
            <quader-client-command-menu
              size="page"
              format="generic"
              [name]="folderName()"
              version=""
              [repository]="repository()"
              [path]="path() + '/'"
              section="publish"
              [verb]="uploadVerb"
              [split]="true"
            >
              <button mainAct class="btn btn-default" type="button" (click)="upload()">
                <span class="material-icons" aria-hidden="true">upload</span>
                <ng-container i18n="@@files.upload">Upload files</ng-container>
              </button>
            </quader-client-command-menu>
          } @else if (permissions().deploy) {
            <button class="btn btn-default" type="button" (click)="upload()">
              <span class="material-icons" aria-hidden="true">upload</span>
              <ng-container i18n="@@files.upload">Upload files</ng-container>
            </button>
          }
          @if (permissions().deploy) {
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
    </quader-folder-view>
  `,
  styleUrl: './repository-lists.scss',
})
export class RepositoryFiles {
  private readonly dialog = inject(QuaderDialog);
  private readonly notifier = inject(Notifier);
  private readonly view = viewChild.required(FolderView);

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
   * `other` or `none` (a disabled repository).
   */
  readonly fill = input<'publisher' | 'other' | 'none'>('other');
  /** Changes when the folder changed by something the list did not do itself (a delete job that ended): the list reloads. */
  readonly revision = input(0);
  /** Delete this folder: the page asks and deletes (R-UI-011). */
  readonly remove = output<void>();
  /** The words of the repository's files: a generic repository's, or a package format's raw files, by its class. */
  readonly shape = input<ContentShape>(contentShapeOf('generic'));
  /** The repository is disabled: it answers every download by path 404, so no file's row offers its address (Q-UI-965). */
  readonly disabled = input(false);
  /** A generic repository: a file's row and the folder's upload offer the clients' commands (R-GENERIC-017). */
  readonly generic = input(false);

  /** The verb of the upload's commands. */
  protected readonly uploadVerb = $localize`:@@files.upload-verb:Upload`;
  /** The folder's name for the panel of its upload: its last segment, or the repository at its root. */
  protected readonly folderName = computed(() => (this.path() === '' ? this.repository() : nameOf(this.path())));
  protected readonly folderActionsLabel = $localize`:@@files.folder-actions:More actions for this folder`;

  protected async upload(): Promise<void> {
    const data: UploadDialogData = {
      repository: this.repository(),
      folder: this.path(),
      canOverwrite: this.permissions().overwrite,
    };
    const ref = this.dialog.open<UploadDialog, UploadDialogData, number>(UploadDialog, { data, width: '40rem', maxWidth: '95vw' });
    const uploaded = (await firstValueFrom(ref.closed)) ?? 0;
    if (uploaded > 0) this.view().reload();
  }

  protected async newFolder(): Promise<void> {
    const data: FolderDialogData = { repository: this.repository(), parent: this.path() };
    const ref = this.dialog.open<FolderDialog, FolderDialogData, string>(FolderDialog, { data, width: '32rem', maxWidth: '95vw' });
    const created = await firstValueFrom(ref.closed);
    if (created) {
      this.notifier.status($localize`:@@files.folder-created:Folder ${created}:path: created.`);
      this.view().reload();
    }
  }
}
