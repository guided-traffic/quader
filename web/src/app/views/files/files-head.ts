import { CdkMenu, CdkMenuItem, CdkMenuTrigger } from '@angular/cdk/menu';
import { Component, computed, input } from '@angular/core';
import { RouterLink } from '@angular/router';
import type { Item } from '../../api/file-reads';
import { filesCommands } from '../../core/project-links';
import { CopyButton } from '../../shared/copy-button';
import { DownloadLink } from '../../shared/download-link';
import { ClientCommandMenu } from '../packages/client-command-menu';

/**
 * The path of a folder's or a file's page (R-UI-010, Q-UI-510, Q-UI-521, R-UI-223), the view both applications compose: **Files**, the
 * folders, the folder or the file of the page, a separator kept with the segment before it; below 64 rem of its own width the folders
 * between **Files** and the last segment collapse into one menu. On a file's page the last segment is the file with its icon, and at
 * the row's end its **Download**, in a generic repository with the chevron of its address and every client's command (R-GENERIC-017,
 * Q-UI-1330); the page projects its own acts on the file after it as `[fileActs]` (the main installation's **Delete**, Q-UI-1458).
 */
@Component({
  selector: 'quader-files-head',
  imports: [CdkMenu, CdkMenuItem, CdkMenuTrigger, ClientCommandMenu, CopyButton, DownloadLink, RouterLink],
  template: `
    <div class="files-head">
      <nav class="files-trail" i18n-aria-label="@@repo.content.folder-path" aria-label="Folder path">
        <ol>
          <li class="seg">
            @if (path() === '') {
              <span aria-current="page" i18n="@@files.root">Files</span>
            } @else {
              <a [routerLink]="root()" i18n="@@files.root">Files</a>
            }
          </li>
          @if (between().length > 0) {
            <li class="seg collapsed">
              <button
                class="btn btn-tertiary btn-icon trail-more on-line"
                type="button"
                [attr.aria-label]="betweenLabel"
                [cdkMenuTriggerFor]="betweenMenu"
              >
                <span class="material-icons" aria-hidden="true">more_horiz</span>
              </button>
              <ng-template #betweenMenu>
                <div class="menu" cdkMenu>
                  @for (c of between(); track $index) {
                    <a class="menu-item" cdkMenuItem [routerLink]="c.commands" translate="no">{{ c.name }}</a>
                  }
                </div>
              </ng-template>
            </li>
            @for (c of between(); track $index) {
              <li class="seg between" translate="no"><a [routerLink]="c.commands">{{ c.name }}</a></li>
            }
          }
          @if (here(); as c) {
            <li class="seg here" translate="no">
              <span class="here-name" aria-current="page">
                @if (file()) {
                  <span class="material-icons entry-icon" aria-hidden="true">description</span>
                }
                <span>{{ c.name }}</span>
              </span>
              <quader-copy-button class="on-line" [value]="path()" [label]="copyPathLabel" [quiet]="true" />
            </li>
          }
        </ol>
      </nav>
      @if (file(); as it) {
        <div class="file-actions">
          <!-- A file the malware scan's gate or the repository's signature gate refuses here offers no download: the fact Malware
               scan or Signature says why (Q-UI-882, R-SIG-074). Nor does a file of a system repository, whose downloads by path answer
               404 to every caller, nor a file of a disabled repository; the file's view says why (Q-UI-898, Q-UI-899, Q-UI-965). -->
          @if (it.contentUrl && it.malware?.refused !== true && it.signatures?.refused !== true && !closed()) {
            @if (generic()) {
              <!-- Download in the browser is the main act; the chevron opens the address and the clients' commands, each with the
                   check of the download as its second line (R-GENERIC-017, Q-UI-1330). A refused file has neither: the commands
                   would answer 403, and its address stays a fact of the file's view (Q-UI-882). -->
              <quader-client-command-menu
                class="on-line"
                size="page"
                format="generic"
                [name]="here()?.name ?? ''"
                version=""
                [repository]="repository()"
                [path]="it.path"
                [sha256]="it.digests?.sha256 ?? null"
                [address]="it.downloadUrl ?? null"
                [verb]="verb()"
                [split]="true"
              >
                <a mainAct class="btn btn-default" [href]="it.contentUrl" download quaderDownload>
                  <span class="material-icons" aria-hidden="true">download</span>
                  <ng-container i18n="@@files.download">Download</ng-container>
                </a>
              </quader-client-command-menu>
            } @else {
              <a class="btn btn-default on-line" [href]="it.contentUrl" download quaderDownload>
                <span class="material-icons" aria-hidden="true">download</span>
                <ng-container i18n="@@files.download">Download</ng-container>
              </a>
            }
          }
          <ng-content select="[fileActs]" />
        </div>
      }
    </div>
  `,
  styleUrl: './files-head.scss',
})
export class FilesHead {
  /** The qualified name of the repository. */
  readonly repository = input.required<string>();
  /** The folder or the file of the page; '' is the root of the repository's files. */
  readonly path = input.required<string>();
  /** The router commands of the root of the files: the repository's own address for a generic repository, `files` for a package format. */
  readonly root = input.required<readonly string[]>();
  /** The file of the page, for its Download; null on a folder's page. */
  readonly file = input<Item | null>(null);
  /** A generic repository: the file's Download has the chevron of its clients' commands (R-GENERIC-017). */
  readonly generic = input(false);
  /** The verb of a file's commands (Download). */
  readonly verb = input.required<string>();
  /** No client reaches the file: a system repository that answers no client, or a disabled one (Q-UI-898, Q-UI-965). */
  readonly closed = input(false);

  /** The folders of the path as links, the last one the page itself (Q-UI-510). */
  protected readonly trail = computed(() => {
    const parts = this.path().split('/').filter((s) => s !== '');
    return parts.map((name, i) => ({ name, commands: filesCommands(this.repository(), parts.slice(0, i + 1)) }));
  });
  /** The folders between Files and the page's own segment: they collapse into one menu where the path is narrow (Q-UI-521). */
  protected readonly between = computed(() => this.trail().slice(0, -1));
  /** The page's own segment: the folder or the file it shows. */
  protected readonly here = computed(() => this.trail().at(-1) ?? null);
  protected readonly betweenLabel = $localize`:@@files.trail.between:Show the folders in between`;
  protected readonly copyPathLabel = $localize`:@@files.copy-path:Copy path`;
}
