import { Component, computed, inject, input, resource, signal } from '@angular/core';
import { FilesApi, Item } from '../../../api/files-api';
import { AddressField } from '../../../shared/address-field';
import { CopyButton } from '../../../shared/copy-button';
import { ErrorNotice } from '../../../shared/error-notice';
import { asApiError, valueOf } from '../../../shared/paged-resource';
import { Disclosure } from '../../../shared/ui/disclosure';
import { TimeAgo } from '../../../shared/ui/time-ago';
import { formatSize } from '../../packages/format-size';
import { nameOf } from './file-paths';

/** A POSIX shell word for `value`: single quotes, with an embedded single quote closed and escaped. */
const shellQuote = (value: string): string => `'${value.replaceAll("'", "'\\''")}'`;

/**
 * The page of one file of a repository (R-UI-010, Q-UI-514), below its path, which names it and holds **Download** and, for a
 * holder of `delete`, **Delete** (the page's): the download address to copy; the facts a reader needs (size, media type, created, last changed, SHA-256). The other checksums, the
 * download statistics and the item as the API describes it are the closed section **Details**: they are for who checks a file,
 * not for who fetches it.
 */
@Component({
  selector: 'quader-repository-file',
  imports: [AddressField, CopyButton, Disclosure, ErrorNotice, TimeAgo],
  template: `
    @let it = item();
    <section class="file-view" aria-labelledby="file-name">
      <!-- The name stands once on the page, as the last segment of its path above with the actions (Q-UI-514); the heading of
           the section is for assistive technology. -->
      <h2 id="file-name" class="visually-hidden" translate="no">{{ name() }}</h2>

      @if (it.downloadUrl; as url) {
        <div class="file-address">
          <span class="field-label" i18n="@@files.download-address">Download address</span>
          <quader-address-field [value]="url" [label]="addressLabel" [copyLabel]="copyAddressLabel" />
        </div>
      }

      <dl class="facts file-facts">
        <dt i18n="@@files.fact.size">Size</dt>
        <dd>{{ size() }}</dd>
        <dt i18n="@@files.fact.media">Media type</dt>
        <dd translate="no">{{ it.mediaType }}</dd>
        <dt i18n="@@files.fact.created">Created</dt>
        <dd>
          <quader-time-ago [at]="it.created.at" />
          @if (it.created.by; as by) {
            &ngsp;<span i18n="@@files.by">by</span>&ngsp;<span translate="no">{{ by }}</span>
          }
        </dd>
        <dt i18n="@@files.fact.modified">Last changed</dt>
        <dd>
          <quader-time-ago [at]="it.modified.at" />
          @if (it.modified.by; as by) {
            &ngsp;<span i18n="@@files.by">by</span>&ngsp;<span translate="no">{{ by }}</span>
          }
        </dd>
        @if (it.contentStatus && it.contentStatus !== 'ok') {
          <dt i18n="@@files.fact.content">Content</dt>
          <dd>
            <span class="material-icons inline bad" aria-hidden="true">error</span>
            @if (it.contentStatus === 'missing') {
              <ng-container i18n="@@files.content.missing">The stored content is missing.</ng-container>
            } @else {
              <ng-container i18n="@@files.content.corrupt">The stored content failed its checksum.</ng-container>
            }
          </dd>
        }
        @if (it.digests?.sha256; as sha) {
          <dt translate="no">SHA-256</dt>
          <dd class="digest">
            <code translate="no" [attr.title]="sha">{{ sha.slice(0, 16) }}…</code>
            <quader-copy-button class="on-line" [value]="sha" [label]="copyDigestLabel('SHA-256')" [quiet]="true" />
          </dd>
        }
      </dl>

      <quader-disclosure
        class="details"
        i18n-heading="@@files.details"
        heading="Details"
        i18n-hint="@@files.details-hint"
        hint="Every checksum, the downloads, and the file as the API describes it."
        (expandedChange)="detailsOpen.set($event)"
      >
        <h3 i18n="@@files.checksums">Checksums</h3>
        <dl class="facts">
          @for (d of digestNames; track d[0]) {
            @if (it.digests?.[d[0]]; as value) {
              <dt translate="no">{{ d[1] }}</dt>
              <dd class="digest">
                <code translate="no">{{ value }}</code>
                <quader-copy-button class="on-line" [value]="value" [label]="copyDigestLabel(d[1])" [quiet]="true" />
              </dd>
            }
          }
        </dl>
        <h3 i18n="@@files.stats.heading">Downloads</h3>
        @if (statsFailure(); as f) {
          <quader-error-notice [error]="f" (retry)="stats.reload()" />
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
            <dd translate="no">{{ s.lastDownloader ?? '–' }}</dd>
          </dl>
          <p class="hint" i18n="@@files.stats.hint">Counts reach the database within a minute of a download.</p>
        } @else if (detailsOpen()) {
          <p role="status" i18n="@@repo.loading">Loading.</p>
        }
        <h3 i18n="@@files.api.json">The file as the API describes it</h3>
        <pre class="code-block" translate="no" tabindex="0">{{ itemJson() }}</pre>
        <h3 i18n="@@files.api.curl">The same request with curl</h3>
        <pre class="code-block" translate="no" tabindex="0">{{ curl() }}</pre>
        @if (downloadCurl()) {
          <h3 i18n="@@files.api.download">Download with curl</h3>
          <pre class="code-block" translate="no" tabindex="0">{{ downloadCurl() }}</pre>
        }
      </quader-disclosure>
    </section>
  `,
  styleUrl: './repository-content.scss',
})
export class RepositoryFile {
  private readonly files = inject(FilesApi);
  protected readonly valueOf = valueOf;

  /** The file, as the items API describes it (R-STOR-016). */
  readonly item = input.required<Item>();

  protected readonly detailsOpen = signal(false);
  protected readonly name = computed(() => nameOf(this.item().path));
  protected readonly size = computed(() => {
    const bytes = this.item().size ?? 0;
    return bytes < 1024 ? formatSize(bytes) : `${formatSize(bytes)} (${bytes.toLocaleString()} B)`;
  });
  /** The download statistics, read once Details is open. */
  protected readonly stats = resource({
    params: () => (this.detailsOpen() ? { repo: this.item().repository, path: this.item().path } : undefined),
    loader: ({ params }) => this.files.stats(params.repo, params.path),
  });
  protected readonly statsFailure = computed(() => {
    const e = this.stats.error();
    return e ? asApiError(e) : null;
  });
  protected readonly itemJson = computed(() => JSON.stringify(this.item(), null, 2));
  protected readonly curl = computed(() => `curl -H "Authorization: Bearer $QUADER_TOKEN" ${shellQuote(this.item().itemUrl)}`);
  protected readonly downloadCurl = computed(() => {
    const url = this.item().downloadUrl;
    return url ? `curl -L -O -H "Authorization: Bearer $QUADER_TOKEN" ${shellQuote(url)}` : '';
  });

  protected readonly addressLabel = $localize`:@@files.download-address:Download address`;
  protected readonly copyAddressLabel = $localize`:@@files.copy-download-address:Copy download address`;
  protected copyDigestLabel(name: string): string {
    return $localize`:@@files.copy-digest:Copy ${name}:checksum:`;
  }
  protected readonly digestNames: readonly (readonly ['sha256' | 'sha512' | 'sha1' | 'md5', string])[] = [
    ['sha256', 'SHA-256'],
    ['sha512', 'SHA-512'],
    ['sha1', 'SHA-1'],
    ['md5', 'MD5'],
  ];
}
