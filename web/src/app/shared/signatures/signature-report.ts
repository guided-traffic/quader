import { DatePipe, formatDate } from '@angular/common';
import { ChangeDetectionStrategy, Component, LOCALE_ID, computed, inject, input, resource } from '@angular/core';
import { RepositoriesApi } from '../../api/repositories-api';
import { SignatureBadgeState, SignatureCovering, SignatureEntry, SignatureOrigin } from '../../api/signatures-api';
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
  knownKindsSentence,
  originWords,
  signatureStateSentence,
  signatureStateWord,
  signatureTime,
  timeProofWords,
} from './signature-words';

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
  protected readonly sentence = computed(() => signatureStateSentence(this.report().state));
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
  protected readonly stateWord = computed(() => signatureStateWord(this.report().state));

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

  protected entryId(e: SignatureEntry, i: number): string {
    return `sig-entry-${e.id}-${i}`;
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
