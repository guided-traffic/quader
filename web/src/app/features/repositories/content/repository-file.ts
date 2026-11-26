import { Component, computed, inject, input, output, resource, signal } from '@angular/core';
import { FilesApi, Item } from '../../../api/files-api';
import { systemClosed } from '../../../core/system-clients';
import { DocumentBlock } from '../../../shared/document-block';
import { ErrorNotice } from '../../../shared/error-notice';
import { failureOf, valueOf } from '../../../shared/paged-resource';
import { personName } from '../../../shared/person-name';
import { gateActSlot } from '../../../shared/signatures/gate-act';
import { Loading } from '../../../shared/ui/loading';
import { TimeAgo } from '../../../shared/ui/time-ago';
import { GATE_ACT } from '../../../views/acts';
import { FileView } from '../../../views/files/file-view';
import { nameOf } from './file-paths';

/** The local name of a download: `./` before a name that starts with `-`, which curl would read as an option or standard output. */
const savedName = (name: string): string => (name.startsWith('-') ? `./${name}` : name);

/** A POSIX shell word for `value`: single quotes, with an embedded single quote closed and escaped. */
const shellQuote = (value: string): string => `'${value.replaceAll("'", "'\\''")}'`;

/**
 * The page of one file of a repository at the main installation (R-UI-010, Q-UI-514), below its path, which names it and holds
 * **Download** and, for a holder of `delete`, **Delete** (the page's): the shared view of the file (views/files/file-view.ts, R-UI-223)
 * with the gate's release where its decision is shown (R-SIG-070), and in its closed section **Details**, after every checksum, what only
 * the management API answers: the download statistics and the item as the API describes it, with its requests by curl. They are for
 * who checks a file, not for who fetches it.
 */
@Component({
  selector: 'quader-repository-file',
  imports: [DocumentBlock, ErrorNotice, FileView, Loading, TimeAgo],
  // The gate's release and withdrawal at the place the view of its decision offers them (R-SIG-070, R-UI-223).
  providers: [{ provide: GATE_ACT, useFactory: gateActSlot }],
  template: `
    <quader-file-view
      [item]="item()"
      [format]="format()"
      [disabled]="disabled()"
      [detailsHint]="detailsHint"
      (detailsChange)="detailsOpen.set($event)"
      (changed)="changed.emit()"
    >
      <ng-container ngProjectAs="[fileDetails]">
        <h3 i18n="@@files.stats.heading">Downloads</h3>
        @if (statsFailure(); as f) {
          <quader-error-notice [error]="f" (retry)="statsFailure.retry()" />
        } @else if (valueOf(stats); as s) {
          <dl class="facts">
            <dt i18n="@@files.stats.downloads">Downloads</dt>
            <dd>{{ s.downloads }}</dd>
            <dt i18n="@@files.stats.last">Last download</dt>
            <dd>
              @if (s.lastDownloadAt; as at) {
                <quader-time-ago [at]="at" />
              } @else {
                –
              }
            </dd>
            <dt i18n="@@files.stats.by">Last downloader</dt>
            <dd translate="no" [attr.title]="s.lastDownloader">{{ s.lastDownloader ? person(s.lastDownloader) : '–' }}</dd>
          </dl>
          <p class="hint" i18n="@@files.stats.hint">Counts reach the database within a minute of a download.</p>
        } @else if (detailsOpen()) {
          <quader-loading i18n="@@files.details.loading">Loading the file's details.</quader-loading>
        }
        <h3 i18n="@@files.api.json">The file as the API describes it</h3>
        <quader-document-block [document]="item()" [copyLabel]="copyItem" />
        <h3 i18n="@@files.api.curl">The same request with curl</h3>
        <pre class="code-block" translate="no" tabindex="0">{{ curl() }}</pre>
        @if (downloadCurl()) {
          <h3 i18n="@@files.api.download">Download with curl</h3>
          <pre class="code-block" translate="no" tabindex="0">{{ downloadCurl() }}</pre>
        }
        <!-- The function keeps the token off curl's command line, as every command with a credential does (Q-GENERIC-9, Q-UI-1336). -->
        <p class="hint" i18n="@@files.api.function-hint">
          <code>quader_curl</code> is the function of Set up client of a generic repository and of the manual's Personal access tokens: it
          hands curl the token from <code>QUADER_TOKEN</code> on standard input, so no process list shows it.
        </p>
      </ng-container>
    </quader-file-view>
  `,
  // The headings of Details as the view's own, and the API's view of a file and its curl requests wrap instead of scrolling sideways:
  // a long address is one word.
  styles: `
    h3 {
      margin-block: var(--quader-space-4) var(--quader-space-2);
    }
    pre.code-block {
      white-space: pre-wrap;
      overflow-wrap: anywhere;
    }
  `,
})
export class RepositoryFile {
  private readonly files = inject(FilesApi);
  protected readonly valueOf = valueOf;

  /** The file, as the items API describes it (R-STOR-016). */
  readonly item = input.required<Item>();
  /** The repository's format: Debian's sentence and the kinds a file without a signature names (R-SIG-009), a generic sibling. */
  readonly format = input('generic');
  /** The repository is disabled: the file has no address, no download and no download with curl (Q-UI-965). */
  readonly disabled = input(false);
  /** A release was granted or withdrawn in the section Signatures: the page reads the file again for its fact and its Download. */
  readonly changed = output<void>();

  protected readonly detailsHint = $localize`:@@files.details-hint:Every checksum, the downloads, and the file as the API describes it.`;
  protected readonly detailsOpen = signal(false);
  protected readonly name = computed(() => nameOf(this.item().path));
  /** The download statistics, read once Details is open. */
  protected readonly stats = resource({
    params: () => (this.detailsOpen() ? { repo: this.item().repository, path: this.item().path } : undefined),
    loader: ({ params }) => this.files.stats(params.repo, params.path),
  });
  protected readonly statsFailure = failureOf(this.stats);
  protected readonly copyItem = $localize`:@@files.api.copy-json:Copy the description`;
  /**
   * Who last downloaded the file, named as the other pages name a person (`priya`), never in a principal's syntax (`user:priya`); the
   * reference is the title (Q-UI-1004).
   */
  protected readonly person = personName;
  /** The item's request with the function `quader_curl`, which hands curl the token on standard input (Q-GENERIC-9, Q-UI-1336). */
  protected readonly curl = computed(() => `quader_curl --fail ${shellQuote(this.item().itemUrl)}`);
  /** A file of a system repository: its downloads answer 404, so there is no download with curl (Q-UI-898). */
  private readonly system = systemClosed(() => this.item().repository);
  /** The download with curl; none for a file of a system repository, which answers it 404 (Q-UI-898). */
  protected readonly downloadCurl = computed(() => {
    const url = this.item().downloadUrl;
    // A generic file's download commands are the panel of Download, the server's for every client (R-GENERIC-017).
    if (this.format() === 'generic') return '';
    return url && !this.system() && !this.disabled()
      ? `quader_curl --fail --location --output ${shellQuote(savedName(this.name()))} ${shellQuote(url)}`
      : '';
  });
}
