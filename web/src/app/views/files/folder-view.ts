import { Component, computed, effect, inject, input, signal, untracked } from '@angular/core';
import { MalwareMark, malwareColumn, malwareShown } from '../../shared/malware/malware-mark';
import { MalwareSignaturesStore } from '../../core/malware-signatures';
import { RouterLink } from '@angular/router';
import { Child, FileReads } from '../../api/file-reads';
import { filesCommands } from '../../core/project-links';
import { systemClosed } from '../../core/system-clients';
import { LIVE_SLOW_MS, live } from '../../core/live-refresh';
import { CopyButton } from '../../shared/copy-button';
import { PagedResource } from '../../shared/paged-resource';
import { DataTable, TableColumn } from '../../shared/ui/data-table';
import { TimeAgo } from '../../shared/ui/time-ago';
import { formatSize } from '../../features/packages/format-size';
import { ContentShape, contentShapeOf, emptyContentText } from '../../features/repositories/content/content-shape';
import { iconOf } from '../../features/repositories/content/file-paths';
import { ClientCommandMenu } from '../packages/client-command-menu';

/** `ui.browser.page_size` (R-UI-010, default 200): how many entries of a folder load at a time. The UI cannot read the setting. */
export const FILES_PAGE = 200;

/**
 * A folder of a repository's files (R-UI-010, R-UI-052, R-UI-223), the view both applications compose: its entries as the shared data
 * table, folders and files by name in the order of their paths, in pages of 200 with **Load more** (R-API-025). A folder's name leads
 * to the folder, a file's name to its page; a file's row has **Download** with the panel of its address and every client's command in a
 * generic repository (R-GENERIC-017) and copies its download address in a package format's raw files, except in a system repository,
 * which answers no client (Q-UI-898). The filter narrows the entries loaded so far, because the server has no name filter on children
 * (Q-UI-513). The operations on the folder are no part of the view: the page that may act projects them as `[folderActs]` at the end of
 * the toolbar (R-UI-011, Q-UI-512, Q-UI-1458); the Bastion's page projects none.
 */
@Component({
  selector: 'quader-folder-view',
  imports: [ClientCommandMenu, TimeAgo, RouterLink, CopyButton, DataTable, MalwareMark],
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
      <ng-content select="[folderActs]" />
    </div>
    @if (filter() && list.hasMore()) {
      <p class="content-note filter-note" role="note" i18n="@@files.filter-partial">
        The filter searches the {{ list.items().length }} entries loaded so far. Load more to search the rest.
      </p>
    }
    <quader-data-table
      [caption]="caption"
      [columns]="columns()"
      [loaded]="list.loaded()"
      [loading]="list.loading()"
      loadingText="Loading the files."
      i18n-loadingText="@@files.loading"
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
          <!-- A file's malware scan in one word, its details on demand (R-UI-255); a folder has none. -->
          @if (malware()) {
            <td>
              @if (c.kind === 'file' && c.malware; as m) {
                <quader-malware-mark [malware]="m" [digest]="c.sha256 ?? null" />
              }
            </td>
          }
          <td>
            @if (c.modifiedAt; as at) {
              <quader-time-ago [at]="at" absolute="title" />
            } @else {
              –
            }
          </td>
          @if (!closed()) {
            <td class="actions">
              @if (c.kind === 'file' && fileUrl(c); as url) {
                @if (generic()) {
                  <!-- The row's act, where a package's row has its verb: the address and every client's command (R-GENERIC-017). -->
                  <quader-client-command-menu
                    format="generic"
                    [name]="c.path"
                    version=""
                    [repository]="repository()"
                    [path]="fullPath(c)"
                    [sha256]="c.sha256 ?? null"
                    [address]="url"
                    [verb]="shape().commandVerb"
                  />
                } @else {
                  <quader-copy-button [value]="url" [label]="copyUrlLabel(c)" [quiet]="true" />
                }
              }
            </td>
          }
        </tr>
      }
    </quader-data-table>
  `,
  styleUrl: '../../features/repositories/content/repository-lists.scss',
})
export class FolderView {
  private readonly files = inject(FileReads);

  /** The qualified name of the repository. */
  readonly repository = input.required<string>();
  /**
   * The repository's address for downloads by path (its raw files address, R-REPO-002), as the page has it from the server's addresses,
   * the format catalog or the Bastion's own origin; empty while it is not known.
   */
  readonly filesAddress = input<string>('');
  /** The folder shown; '' is the root. */
  readonly path = input<string>('');
  /** The caller may upload here: the empty root says so (the page decides it, Q-UI-512). */
  readonly uploads = input(false);
  /**
   * What the empty root says fills the repository for a caller the toolbar offers no upload (R-UI-144, Q-UI-555): `publisher`,
   * `other` or `none` (a disabled repository, a Bastion's visitor). It offers no button: **Set up client** is the header's, **Upload
   * files** the toolbar's.
   */
  readonly fill = input<'publisher' | 'other' | 'none'>('other');
  /** Changes when the folder changed by something the list did not do itself (a delete job that ended): the list reloads. */
  readonly revision = input(0);
  /** The words of the repository's files: a generic repository's, or a package format's raw files, by its class. */
  readonly shape = input<ContentShape>(contentShapeOf('generic'));
  /** A generic repository: a file's row offers the clients' commands (R-GENERIC-017). */
  readonly generic = input(false);
  /** The repository is disabled: it answers every download by path 404, so no file's row offers its address (Q-UI-965). */
  readonly disabled = input(false);

  protected readonly list = new PagedResource<Child>(FILES_PAGE);
  protected readonly filter = signal('');

  protected readonly shown = computed(() => {
    const f = this.filter().trim().toLowerCase();
    return f ? this.list.items().filter((c) => c.path.toLowerCase().includes(f)) : this.list.items();
  });

  protected readonly caption = contentShapeOf('generic').itemsLabel;
  protected readonly emptyText = computed(() => {
    if (this.filter()) return $localize`:@@repo.content.files-empty-filtered:No file matches the filter.`;
    if (this.path() !== '') return $localize`:@@files.folder-empty:This folder is empty.`;
    if (this.uploads()) return $localize`:@@files.empty-upload:This repository holds no files yet. Upload files here or with your client.`;
    return emptyContentText(this.shape(), this.fill());
  });
  /**
   * The name wraps beside its icon and declares its least (Q-UI-308); the size, the time and the copy button never break and are
   * as wide as their values (Q-UI-563). Their guessed widths kept 120 px empty beside them while a long file name stood in three
   * lines at 1,024 px (2026-11-18).
   */
  private readonly malwareMarks = inject(MalwareSignaturesStore);
  /** The files carry their malware state (R-MAL-039): the column **Malware scan** (R-UI-255); a folder's cell stays empty. */
  protected readonly malware = computed(() => malwareShown(this.malwareMarks, this.list.items()));
  protected readonly columns = computed<readonly TableColumn[]>(() => [
    { id: 'name', label: $localize`:@@repo.content.col.name:Name`, width: '12rem' },
    { id: 'size', label: $localize`:@@repo.content.col.size:Size`, numeric: true },
    ...(this.malware() ? [malwareColumn('file')] : []),
    { id: 'modified', label: $localize`:@@repo.content.col.modified:Last change` },
    // A system repository's files and a disabled repository's have no address to copy, so the column would stay empty (Q-UI-898,
    // Q-UI-965).
    ...(this.closed() ? [] : [{ id: 'actions', label: $localize`:@@repo.col.actions:Actions`, hideLabel: true }]),
  ]);
  /**
   * A system repository of the project `upstream` (R-UI-193): every download by path answers 404 to every caller (R-REPO-070,
   * R-REPO-075), so no file's row offers its download address (Q-UI-898).
   */
  protected readonly system = systemClosed(() => this.repository());
  /** No client reaches the files: a system repository that answers no client, or a disabled one. */
  protected readonly closed = computed(() => this.system() || this.disabled());

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
    // A client's upload or delete changes the folder: its entries shown are read again in place, the filter stays (R-UI-213).
    live({ reload: () => this.list.refresh(), every: () => LIVE_SLOW_MS });
  }

  /** Reads the folder again from its first page, the filter kept: the page's own act changed it (an upload, a new folder). */
  reload(): void {
    void this.list.start(this.files.children(this.repository(), this.path(), FILES_PAGE), FILES_PAGE);
  }

  protected fullPath(c: Child): string {
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

  protected copyUrlLabel(c: Child): string {
    return $localize`:@@files.copy-url-of:Copy the download address of ${c.path}:name:`;
  }

  protected size(bytes: number): string {
    return formatSize(bytes);
  }
}
