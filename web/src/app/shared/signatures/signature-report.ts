import { DatePipe, formatDate } from '@angular/common';
import { ChangeDetectionStrategy, Component, LOCALE_ID, computed, inject, input, resource } from '@angular/core';
import { RepositoriesApi } from '../../api/repositories-api';
import { SignatureBadgeState, SignatureCovering, SignatureEntry, SignatureOrigin, attestationUrl } from '../../api/signatures-api';
import { Schema } from '../../api/types';
import { formatSize } from '../../features/packages/format-size';
import { valueOf } from '../paged-resource';
import { DataTable, TableColumn } from '../ui/data-table';
import { Disclosure } from '../ui/disclosure';
import { TimeAgo } from '../ui/time-ago';
import { SignatureBadge } from './signature-badge';
import {
  anchorScopeWords,
  certificateWords,
  debianIndexSentence,
  entryBadgeState,
  entryKind,
  entrySentence,
  isReceipt,
  knownKindsSentence,
  noPublisherSignature,
  originWords,
  receiptIntegrityWords,
  receiptMalwareWords,
  receiptVulnerabilityWords,
  servedReasonWords,
  servedWords,
  signatureStateSentence,
  signatureStateWord,
  signatureTime,
  timeProofWords,
} from './signature-words';

type SignatureReceiptSignature = Schema<'SignatureReceiptSignature'>;

/** What a version's repository and a stored file answer alike (R-SIG-034): the badge, the signatures, the attestations, the origin. */
export interface SignatureReportView {
  readonly state: SignatureBadgeState;
  readonly stale: boolean;
  readonly signatures: readonly SignatureEntry[];
  readonly attestations: readonly SignatureEntry[];
  readonly covering: readonly SignatureCovering[];
  readonly origin: SignatureOrigin | null;
}

/**
 * The details of an artifact's signatures (R-SIG-035, R-SIG-010, R-SIG-024, R-SIG-030): one sentence with the badge first, which is all
 * a plain user needs, then who uploaded it or where it was fetched from, then each signature with the kind in plain words, the signer,
 * the issuer, when it was signed and what proves it, the certificate's validity, the trust anchor that knows the signer and its scope,
 * a block's date and reason, a CI job's project, ref and run, and **Download original**; the identifiers, the chain, the log entries and
 * where it is attached are one fold further. An OCI image's attestations and SBOMs stand apart in a table of their own, never as tags.
 * What this installation signed itself (R-SIG-056) carries no badge and never reads as a signer no anchor knows: npm's registry
 * signature as "Origin confirmed by registry · this installation" with its key ID, and under **Attestations** the receipt with its issue
 * time, the malware state it names, where it is served and why, and **Download attestation**, the rest of its statement one fold further.
 * A Debian package says that apt checks the index and lists the releases that cover it. Every text a signature carries is text,
 * never markup. `level` is the level of the report's headings: 2 in a tab of the version page, 3 inside a section of the file page.
 */
@Component({
  selector: 'quader-signature-report',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [DataTable, DatePipe, Disclosure, SignatureBadge, TimeAgo],
  templateUrl: './signature-report.html',
  styleUrl: './signature-report.scss',
})
export class SignatureReport {
  private readonly repositories = inject(RepositoriesApi);
  private readonly locale = inject(LOCALE_ID);

  readonly report = input.required<SignatureReportView>();
  /** The format of the artifact: Debian's sentence, and the kinds a version without a signature names. */
  readonly format = input.required<string>();
  readonly level = input<2 | 3>(2);
  /** Show each signature's repository: a version held by several repositories, or read through a virtual one. */
  readonly showRepository = input(false);

  protected readonly entryLevel = computed(() => this.level() + 1);
  /** The signatures cover more than one file: each names its file. */
  protected readonly severalFiles = computed(() => new Set(this.report().signatures.map((e) => e.path ?? '')).size > 1);
  protected readonly deb = computed(() => this.format() === 'deb');
  /** Quader's receipts stand apart from the attestations of others under Attestations (R-SIG-056); the others keep their table. */
  protected readonly receipts = computed(() => this.report().attestations.filter(isReceipt));
  protected readonly otherAttestations = computed(() => this.report().attestations.filter((e) => !isReceipt(e)));
  /** No badge, yet the list below holds a registry's signature or this installation's own (Q-SIG-235). */
  private readonly listedWithoutPublisher = computed(() => {
    const r = this.report();
    return r.state === 'none' && (r.signatures.length > 0 || this.receipts().length > 0);
  });
  protected readonly sentence = computed(() =>
    this.listedWithoutPublisher() ? noPublisherSignature().sentence : signatureStateSentence(this.report().state),
  );
  /**
   * The block that decides an untrusted badge (R-SIG-020): every signature that counts for the badge and reads not trusted is a blocked
   * signer's, so the badge's sentence names the block instead of saying that no anchor knows the signer.
   */
  protected readonly blockedBy = computed(() => {
    const r = this.report();
    if (r.state !== 'untrusted') return null;
    const counting = r.signatures.filter(
      (e) => (e.role === 'publisher' || e.role === 'provenance') && (e.state === 'valid' || e.state === 'no_key'),
    );
    if (counting.length === 0 || counting.some((e) => e.trust?.state !== 'blocked')) return null;
    return counting.find((e) => e.trust?.block)?.trust?.block ?? null;
  });
  protected readonly stateWord = computed(() =>
    this.listedWithoutPublisher() ? noPublisherSignature().word : signatureStateWord(this.report().state),
  );

  /** The kinds the format declares that count for the badge (R-SIG-009), read only for an artifact without a signature. */
  private readonly formats = resource({
    params: () => (this.report().state === 'none' && !this.deb() ? { format: this.format() } : undefined),
    loader: async ({ params }) => {
      try {
        const f = (await this.repositories.formats()).find((d) => d.name === params.format);
        return f ? f.signatureKinds.filter((k) => k.role === 'publisher' || k.role === 'provenance').map((k) => k.name) : null;
      } catch {
        return null;
      }
    },
  });
  protected readonly kindsSentence = computed(() => knownKindsSentence(valueOf(this.formats) ?? null));

  protected readonly kind = entryKind;
  protected readonly entrySentence = entrySentence;
  protected readonly badgeOf = entryBadgeState;
  protected readonly time = signatureTime;
  protected readonly proof = timeProofWords;
  protected readonly scopeWords = anchorScopeWords;
  protected readonly originText = originWords;
  protected readonly debSentence = debianIndexSentence();
  protected readonly size = formatSize;

  protected certificate(e: SignatureEntry): string {
    return certificateWords(e, Date.now(), (iso) => formatDate(iso, 'mediumDate', this.locale));
  }

  /** The claims of a CI job a certificate carries, as lines (R-SIG-005): its source and ref, its run, its build configuration. */
  protected ciLines(e: SignatureEntry): string[] {
    const ci = e.ci;
    if (!ci) return [];
    const source = [ci.sourceRepositoryUri, ci.sourceRepositoryRef].filter((v): v is string => !!v).join(' · ');
    return [source, ci.runInvocationUri, ci.buildConfigUri].filter((v): v is string => !!v);
  }

  protected algorithms(e: SignatureEntry): string {
    return [e.algorithm, e.hashAlgorithm].filter((v): v is string => !!v).join(' · ');
  }

  protected readonly wordOf = signatureStateWord;
  protected readonly malwareWords = receiptMalwareWords;
  protected readonly vulnerabilityWords = receiptVulnerabilityWords;
  protected readonly served = servedWords;
  protected readonly servedReason = servedReasonWords;
  protected readonly integrityWords = receiptIntegrityWords;

  protected entryId(e: SignatureEntry, i: number): string {
    return `sig-entry-${e.id}-${i}`;
  }

  /** When the receipt was issued: the statement's own time, else when its signature was stored. */
  protected issuedAt(e: SignatureEntry): string {
    return e.attestation?.issuedAt ?? e.claimedAt ?? e.receivedAt;
  }

  /** The receipt's download through the API (R-SIG-054); none for a retired identity's, which the route does not serve (Q-SIG-191). */
  protected receiptDownload(e: SignatureEntry): string | null {
    if (e.issuedBy?.state === 'retired' || !e.repository || !e.location) return null;
    return attestationUrl(e.repository, e.location);
  }

  /** One signature a receipt names, in a line: its kind and what its check said when the receipt was issued. */
  protected receiptSignatureLine(s: SignatureReceiptSignature): string {
    const kind = entryKind({ role: s.role as SignatureEntry['role'], scheme: s.scheme as SignatureEntry['scheme'], identity: s.identity });
    return `${kind} · ${receiptIntegrityWords(s.integrity)}`;
  }

  protected readonly attestationColumns: readonly TableColumn[] = [
    { id: 'type', label: $localize`:@@sig.att.type:Type`, width: '14rem', prefer: true },
    { id: 'platform', label: $localize`:@@sig.att.platform:Platform` },
    { id: 'size', label: $localize`:@@sig.att.size:Size`, numeric: true },
    { id: 'received', label: $localize`:@@sig.att.received:Received` },
    { id: 'download', label: $localize`:@@sig.att.download:Download`, hideLabel: true },
  ];
  protected readonly attestationCaption = $localize`:@@sig.att.caption:Attestations and SBOMs of this image`;
  protected readonly technicalHeading = $localize`:@@sig.technical:Technical details`;
  protected readonly imageWord = $localize`:@@sig.att.image:Image`;
}
