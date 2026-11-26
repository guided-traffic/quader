import { personName } from '../../../shared/person-name';
import { DocumentBlock } from '../../../shared/document-block';
import { Component, computed, inject, input, output, resource, signal } from '@angular/core';
import { FilesApi, Item } from '../../../api/files-api';
import { AddressField } from '../../../shared/address-field';
import { CopyButton } from '../../../shared/copy-button';
import { ErrorNotice } from '../../../shared/error-notice';
import { failureOf, valueOf } from '../../../shared/paged-resource';
import { Disclosure } from '../../../shared/ui/disclosure';
import { TimeAgo } from '../../../shared/ui/time-ago';
import { MalwareStatusView } from '../../../shared/malware/malware-status';
import { formatSize } from '../../packages/format-size';
import { nameOf } from './file-paths';
import { mediaTypeWord } from './media-type-words';
import { systemClientSentence, upstreamKeyOf } from '../../../core/upstream-project';
import { systemClosed } from '../../../core/system-clients';
import { RouterLink } from '@angular/router';
import { SignatureBadgeState, SignatureStatus, SignaturesApi } from '../../../api/signatures-api';
import { filesCommands } from '../../../core/project-links';
import { SignatureBadge } from '../../../shared/signatures/signature-badge';
import { SignatureReport } from '../../../shared/signatures/signature-report';
import { hasSignatureBadge, signatureStateWord } from '../../../shared/signatures/signature-words';
import { SignatureGateRefusedReason } from '../../../api/signatures-api';
import { gateReasonWord } from '../../../shared/signatures/gate-words';
import { SignatureGate } from '../../../shared/signatures/signature-gate';
import { Loading } from '../../../shared/ui/loading';

/** The names of a signature beside its file in a generic repository, longest first (R-SIG-032). */
const SIBLING_SUFFIXES = ['.sigstore.json', '.jws.sig', '.cose.sig', '.asc', '.sig'];

/** The local name of a download: `./` before a name that starts with `-`, which curl would read as an option or standard output. */
const savedName = (name: string): string => (name.startsWith('-') ? `./${name}` : name);

/** A POSIX shell word for `value`: single quotes, with an embedded single quote closed and escaped. */
const shellQuote = (value: string): string => `'${value.replaceAll("'", "'\\''")}'`;

/**
 * The page of one file of a repository (R-UI-010, Q-UI-514), below its path, which names it and holds **Download** and, for a
 * holder of `delete`, **Delete** (the page's): the download address to copy, which for a generic repository's file is in the panel of
 * **Download** with every client's command (R-GENERIC-017, Q-UI-1331), and for a file of a system repository the sentence why there is
 * none (Q-UI-899); the facts a reader needs (size, the media type in a word with the type beside it, Q-UI-1333, created, last
 * changed, the malware scan's verdict of R-MAL-024, SHA-256). The other checksums, the
 * download statistics and the item as the API describes it are the closed section **Details**: they are for who checks a file,
 * not for who fetches it.
 */
@Component({
  selector: 'quader-repository-file',
  imports: [Loading, DocumentBlock, AddressField, CopyButton, Disclosure, ErrorNotice, MalwareStatusView, RouterLink, SignatureBadge, SignatureGate, SignatureReport, TimeAgo],
  template: `
    @let it = item();
    <section class="file-view" aria-labelledby="file-name">
      <!-- The name stands once on the page, as the last segment of its path above with the actions (Q-UI-514); the heading of
           the section is for assistive technology. -->
      <h2 id="file-name" class="visually-hidden" translate="no">{{ name() }}</h2>

      <!-- A file of a system repository has no address to copy: its downloads by path answer 404 to every caller. One sentence
           says why where the address would stand (Q-UI-898, Q-UI-899). -->
      @if (system()) {
        <p class="content-note system-note" data-testid="file-system">{{ systemNote() }}</p>
      } @else if (disabled()) {
        <!-- A disabled repository answers every download 404 (R-REPO-005): no address, no Download, one sentence (Q-UI-965). -->
        <p class="content-note system-note" data-testid="file-disabled" i18n="@@files.disabled-note">The repository is disabled: nobody can download this file until it is enabled again.</p>
      } @else if ((format() !== 'generic' || refused()) && it.downloadUrl; as url) {
        <!-- A generic file's address stands in the panel of Download, beside its clients' commands (R-GENERIC-017, Q-UI-1331); a
             package format's raw file has no clients of its own for a path, and a refused file no Download, so its address stays
             here as a fact (Q-UI-882). -->
        <div class="file-address">
          <span class="field-label" i18n="@@files.download-address">Download address</span>
          <quader-address-field [value]="url" [label]="addressLabel" [copyLabel]="copyAddressLabel" />
        </div>
      }

      <dl class="facts file-facts">
        <dt i18n="@@files.fact.size">Size</dt>
        <dd>{{ size() }}</dd>
        <dt i18n="@@files.fact.media">Media type</dt>
        <!-- A word a person knows where the product has one, the media type beside it (R-GENERIC-017, Q-UI-1333). -->
        <dd class="media-type">
          @if (mediaWord(); as w) {
            <span>{{ w }}</span>&ngsp;
          }
          <code translate="no">{{ it.mediaType }}</code>
        </dd>
        <dt i18n="@@files.fact.created">Created</dt>
        <dd>
          <quader-time-ago [at]="it.created.at" />
          @if (it.created.by; as by) {
            &ngsp;<span i18n="@@files.by">by</span>&ngsp;<span translate="no" [attr.title]="by">{{ person(by) }}</span>
          }
        </dd>
        <dt i18n="@@files.fact.modified">Last changed</dt>
        <dd>
          <quader-time-ago [at]="it.modified.at" />
          @if (it.modified.by; as by) {
            &ngsp;<span i18n="@@files.by">by</span>&ngsp;<span translate="no" [attr.title]="by">{{ person(by) }}</span>
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
        <!-- The malware scan's verdict of the file's bytes (R-MAL-024, Q-UI-872); absent for a generated file and while the scanning is off. -->
        @if (it.malware; as m) {
          <dt i18n="@@files.fact.malware">Malware scan</dt>
          <dd data-testid="malware-fact"><quader-malware-status [status]="m" /></dd>
        }
        <!-- The badge of the file's signatures, or the word of a state without one (R-SIG-035); absent while the signatures are off. -->
        @if (it.signatures; as sig) {
          <dt i18n="@@files.fact.signature">Signature</dt>
          <dd data-testid="signature-fact">
            <!-- The repository's gate refuses the file now (R-SIG-074): "Refused here" with the reason, as the malware scan's fact says
                 that downloads are refused (Q-UI-882); its section Signatures says why in full and offers the release. -->
            @if (sig.refused === true) {
              <span class="gate-fact"
                ><span class="fstatus bad" data-testid="signature-refused"
                  ><span class="fstatus-main"
                    ><span class="material-icons" aria-hidden="true">block</span><span i18n="@@sig.gate.refused-here">Refused here</span></span
                  ></span
                ><span class="muted">{{ reasonWord(sig.refusedReason) }}</span></span
              >
            } @else if (badged(sig)) {
              <quader-signature-badge [state]="sig.state" [stale]="sig.stale" />
            } @else {
              <span class="muted">{{ stateWord(sig.state) }}</span>
            }
          </dd>
        }
        @if (signatureOf(); as subject) {
          <dt i18n="@@files.fact.signature-of">Signature of</dt>
          <dd><a [routerLink]="subject.link" translate="no">{{ subject.name }}</a></dd>
        }
        @if (it.digests?.sha256; as sha) {
          <dt translate="no">SHA-256</dt>
          <dd class="digest">
            <code translate="no" [attr.title]="sha">{{ sha.slice(0, 16) }}…</code>
            <quader-copy-button class="on-line" [value]="sha" [label]="copyDigestLabel('SHA-256')" [quiet]="true" />
          </dd>
        }
      </dl>

      <!-- The details of the signatures and the origin of the upload, one click away (R-SIG-035, R-SIG-010). -->
      @if (it.signatures; as sig) {
        <quader-disclosure
          class="details"
          i18n-heading="@@files.signatures"
          heading="Signatures"
          [hint]="signaturesHint(sig.state)"
          (expandedChange)="signaturesOpen.set($event)"
        >
          @if (signatureFailure(); as f) {
            <quader-error-notice [error]="f" (retry)="signatureFailure.retry()" />
          } @else if (valueOf(signatureReport); as report) {
            @if (report.gate; as g) {
              <quader-signature-gate
                [gate]="g"
                [repository]="report.repository"
                [artifacts]="[{ name: name(), sha256: report.sha256, refusedReason: sig.refusedReason ?? null }]"
                [signatures]="report.signatures"
                (changed)="signatureReport.reload(); changed.emit()"
              />
            }
            <quader-signature-report [report]="report" [format]="format()" [level]="3" />
          } @else if (signaturesOpen()) {
            <quader-loading i18n="@@files.signatures.loading">Loading the signatures.</quader-loading>
          }
        </quader-disclosure>
      }

      <quader-disclosure
        class="details"
        remember="file.details"
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
      </quader-disclosure>
    </section>
  `,
  styleUrl: './repository-file.scss',
  // "Refused here" and its reason on one row, centred on each other (R-SIG-074).
  styles: `
    .gate-fact {
      display: inline-flex;
      flex-wrap: wrap;
      align-items: center;
      column-gap: var(--quader-space-2);
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

  // ---- The signatures (R-SIG-035) ----
  private readonly signaturesApi = inject(SignaturesApi);
  protected readonly signaturesOpen = signal(false);
  /** The details, read once the section is open. */
  protected readonly signatureReport = resource({
    params: () => (this.signaturesOpen() && this.item().signatures ? { repo: this.item().repository, path: this.item().path } : undefined),
    loader: ({ params }) => this.signaturesApi.file(params.repo, params.path),
  });
  protected readonly signatureFailure = failureOf(this.signatureReport);
  protected readonly badged = (s: SignatureStatus): boolean => hasSignatureBadge(s);
  protected reasonWord(reason: SignatureGateRefusedReason | null | undefined): string {
    return gateReasonWord(reason ?? 'missing');
  }
  /** A release was granted or withdrawn in the section Signatures: the page reads the file again for its fact and its Download. */
  readonly changed = output<void>();
  protected readonly stateWord = signatureStateWord;
  protected signaturesHint(state: SignatureBadgeState): string {
    return state === 'none'
      ? $localize`:@@files.signatures-hint-none:No signature found, and who uploaded the file or where it was fetched from.`
      : $localize`:@@files.signatures-hint:Who signed the file, whether a trust anchor knows the signer, and who uploaded it.`;
  }
  /**
   * A generic file whose name is a signature's (R-SIG-032, Q-SIG-8): "Signature of" the file beside it, by the longest suffix. The
   * file stays a file to download; the signed one carries the badge.
   */
  protected readonly signatureOf = computed<{ readonly name: string; readonly link: string[] } | null>(() => {
    if (this.format() !== 'generic') return null;
    const path = this.item().path;
    const suffix = SIBLING_SUFFIXES.find((s) => path.endsWith(s) && path.length > s.length && !path.endsWith(`/${s}`));
    if (!suffix) return null;
    const subject = path.slice(0, -suffix.length);
    return { name: nameOf(subject), link: filesCommands(this.item().repository, subject.split('/')) };
  });

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
  protected readonly statsFailure = failureOf(this.stats);
  protected readonly copyItem = $localize`:@@files.api.copy-json:Copy the description`;
  /**
   * Who created, changed or last downloaded the file, named as the other pages name a person (`priya`), never in a principal's syntax
   * (`user:priya`); the reference is the title (Q-UI-1004).
   */
  protected readonly person = personName;
  /** The item's request with the function `quader_curl`, which hands curl the token on standard input (Q-GENERIC-9, Q-UI-1336). */
  protected readonly curl = computed(() => `quader_curl --fail ${shellQuote(this.item().itemUrl)}`);
  /** The download with curl; none for a file of a system repository, which answers it 404 (Q-UI-898). */
  protected readonly downloadCurl = computed(() => {
    const url = this.item().downloadUrl;
    // A generic file's download commands are the panel of Download, the server's for every client (R-GENERIC-017).
    if (this.format() === 'generic') return '';
    return url && !this.system() && !this.disabled()
      ? `quader_curl --fail --location --output ${shellQuote(savedName(this.name()))} ${shellQuote(url)}`
      : '';
  });
  /** The malware scan's or the signature gate refuses the file here: the page offers no Download, and its address stays a fact. */
  protected readonly refused = computed(() => this.item().malware?.refused === true || this.item().signatures?.refused === true);
  /** The media type in a word a person knows, or '' for a type the product has no word for. */
  protected readonly mediaWord = computed(() => mediaTypeWord(this.item().mediaType ?? ''));
  /**
   * A file of a system repository of the project `upstream` (R-UI-193): Quader's own work reads it, and every download, by its
   * address for downloads by path and through the content route of the UI alike, answers 404 (R-REPO-070, R-REPO-075). The item's
   * request with curl stays: the management API reads it (Q-UI-898).
   */
  protected readonly system = systemClosed(() => this.item().repository);
  protected readonly systemNote = computed(() => systemClientSentence(upstreamKeyOf(this.item().repository), 'file'));

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
