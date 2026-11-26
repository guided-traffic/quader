import {
  SignatureAnchorKind,
  SignatureAnchorScope,
  SignatureBadgeState,
  SignatureBlockKind,
  SignatureEntry,
  SignatureOrigin,
  SignatureReceipt,
  SignatureStatus,
} from '../../api/signatures-api';
import { malwareStateWord } from '../../shared/malware/malware-words';

/**
 * The words of the signatures (R-SIG-009, R-SIG-035): one place for what each state, role, scheme and anchor is called, so that a
 * version, a file, the admin page, a project's anchors and the security dashboard say the same. A signature says who signed and
 * whether a trust anchor knows the signer; no screen calls it a guarantee that the content is safe (H-1171).
 */

/** The tone of a badge: green for the two verified states only (Q-UI-96), neutral for an untrusted signer, red for an invalid one. */
export type SignatureTone = 'ok' | 'neutral' | 'bad';

/**
 * A badge of R-SIG-035: its word, its icon (a shield of the icon font) and its tone; never told by colour alone. A state whose words
 * are too long for a pill (Q-UI-1076) puts its verdict on the badge and the rest after it as muted text (`note`), which wraps between words
 * where the badge's cell is narrow: a badge itself never breaks (R-UI-171). `words` are the state's whole words, as a fact says them.
 */
export interface SignatureBadgeLook {
  readonly word: string;
  readonly note: string | null;
  readonly words: string;
  readonly icon: string;
  readonly tone: SignatureTone;
}

/** The sentence under a page's title wherever trust is set (R-SIG-036). */
export function signaturePromise(): string {
  return $localize`:@@sig.promise:A signature tells who signed an artifact and whether a trust anchor knows the signer. It does not tell that the content is safe; the malware scan and the vulnerability scanning check that.`;
}

/** The badge of a state, or null for the three states that carry none (R-SIG-009). */
export function signatureBadge(state: SignatureBadgeState | null | undefined): SignatureBadgeLook | null {
  switch (state) {
    case 'verified_public':
      return whole($localize`:@@sig.badge.verified-public:Verified · public`, 'gpp_good', 'ok');
    case 'verified_internal':
      return whole($localize`:@@sig.badge.verified-internal:Verified · internal`, 'gpp_good', 'ok');
    case 'untrusted':
      // "Signiert · Signierer nicht vertrauenswürdig" is 269 px as one pill, wider than a file's cell at 390 px (Q-UI-1076).
      return {
        word: $localize`:@@sig.badge.untrusted-word:Signed`,
        note: $localize`:@@sig.badge.untrusted-note:signer not trusted`,
        words: $localize`:@@sig.badge.untrusted:Signed · signer not trusted`,
        icon: 'shield',
        tone: 'neutral',
      };
    case 'invalid':
      return whole($localize`:@@sig.badge.invalid:Invalid signature`, 'gpp_bad', 'bad');
    default:
      return null;
  }
}

/** A badge whose words all stand on the pill. */
function whole(word: string, icon: string, tone: SignatureTone): SignatureBadgeLook {
  return { word, note: null, words: word, icon, tone };
}

/** Whether a member `signatures` carries a badge. */
export function hasSignatureBadge(status: SignatureStatus | null | undefined): status is SignatureStatus {
  return signatureBadge(status?.state) !== null;
}

/** The word of a state without a badge, as the details and a file's fact say it (R-SIG-009 rule 5). */
export function signatureStateWord(state: SignatureBadgeState): string {
  const badge = signatureBadge(state);
  if (badge) return badge.words;
  switch (state) {
    case 'not_checked':
      return $localize`:@@sig.state.not-checked:Not checked yet`;
    case 'unsupported':
      return $localize`:@@sig.state.unsupported:Scheme not supported`;
    default:
      return $localize`:@@sig.state.none:No signature`;
  }
}

/** The sentence of an artifact's state in its details (R-SIG-035). */
export function signatureStateSentence(state: SignatureBadgeState): string {
  switch (state) {
    case 'verified_internal':
      return $localize`:@@sig.sentence.verified-internal:The signature matches the content, and a trust anchor of this installation or of this project knows the signer.`;
    case 'verified_public':
      return $localize`:@@sig.sentence.verified-public:The signature matches the content, and a public trust anchor shipped with Quader knows the signer.`;
    case 'untrusted':
      return $localize`:@@sig.sentence.untrusted:The signature matches the content or cannot be matched without a key. No trust anchor knows this signer.`;
    case 'invalid':
      return $localize`:@@sig.sentence.invalid:The signature does not match the content.`;
    case 'not_checked':
      return $localize`:@@sig.sentence.not-checked:A signature was found and is not checked yet.`;
    case 'unsupported':
      return $localize`:@@sig.sentence.unsupported:A signature was found in a scheme Quader does not read, or it could not be read.`;
    default:
      return $localize`:@@sig.sentence.none:No signature found.`;
  }
}

/**
 * The words of an artifact without a badge whose details still list a signature or a receipt (Q-SIG-235): a registry's signature, this
 * installation's own, its receipt. "No signature found" above them would contradict the list; they confirm where the artifact was
 * published or received, never who built it, which is what the badge stands for (Q-SIG-1).
 */
export function noPublisherSignature(): { readonly word: string; readonly sentence: string } {
  return {
    word: $localize`:@@sig.state.no-publisher:No signature of the publisher`,
    sentence: $localize`:@@sig.sentence.no-publisher:What is listed below confirms where the artifact was published or received, not who built it.`,
  };
}

/** What the format knows, after "No signature found." (R-SIG-009): the kinds that count for the badge, or that it has none. */
export function knownKindsSentence(kinds: readonly string[] | null): string {
  if (kinds === null) return '';
  if (kinds.length === 0) return $localize`:@@sig.kinds.none:This format has no signatures.`;
  const list = kinds.join(', ');
  return $localize`:@@sig.kinds.known:Signatures this format knows: ${list}:kinds:.`;
}

/** Debian signs the index, not the package (R-SIG-030): the sentence of a package's details. */
export function debianIndexSentence(): string {
  return $localize`:@@sig.deb.index:apt checks the signature of this repository's index, not of the package.`;
}

/** The scheme in plain words; Sigstore says whether it was keyless (R-SIG-035: "Publisher signature · Sigstore, keyless"). */
export function schemeWords(e: Pick<SignatureEntry, 'scheme' | 'identity' | 'oidcIssuer' | 'schemeName'>): string {
  switch (e.scheme) {
    case 'sigstore-bundle':
      return e.identity || e.oidcIssuer ? $localize`:@@sig.scheme.sigstore-keyless:Sigstore, keyless` : $localize`:@@sig.scheme.sigstore-key:Sigstore, with a key`;
    case 'cosign-legacy':
      return $localize`:@@sig.scheme.cosign:cosign, older layout`;
    case 'notation-jws':
    case 'notation-cose':
      return 'Notation';
    case 'nuget-cms':
      return $localize`:@@sig.scheme.nuget:NuGet package signature`;
    case 'openpgp':
      return 'OpenPGP';
    case 'npm-registry':
      return $localize`:@@sig.scheme.npm:npm registry signature`;
    case 'public-key':
      return $localize`:@@sig.scheme.public-key:a public key`;
    default:
      return e.schemeName ?? $localize`:@@sig.scheme.unknown:an unknown scheme`;
  }
}

/** The kind of a signature in plain words (R-SIG-035): the role, then the scheme for a publisher's signature. */
export function entryKind(
  e: Pick<SignatureEntry, 'role' | 'scheme' | 'identity' | 'oidcIssuer' | 'schemeName'> & { readonly type?: string; readonly issuedBy?: SignatureEntry['issuedBy'] },
): string {
  switch (e.role) {
    case 'publisher': {
      const scheme = schemeWords(e);
      if (e.scheme === 'nuget-cms') return $localize`:@@sig.kind.author:Author signature · ${scheme}:scheme:`;
      return $localize`:@@sig.kind.publisher:Publisher signature · ${scheme}:scheme:`;
    }
    case 'provenance':
      return $localize`:@@sig.kind.provenance:Provenance attestation`;
    case 'registry':
      // Quader's own npm signature of a local version (R-SIG-056): the same role as npmjs's, named as this installation's.
      return e.issuedBy
        ? $localize`:@@sig.kind.registry-own:Origin confirmed by registry · this installation`
        : $localize`:@@sig.kind.registry:Origin confirmed by registry`;
    default:
      if (e.issuedBy) return $localize`:@@sig.kind.receipt:Receipt attestation by this installation`;
      return e.type ?? schemeWords(e);
  }
}

/** Quader's receipt attestation (R-SIG-051): an attestation this installation issued with its signing identity. */
export function isReceipt(e: Pick<SignatureEntry, 'role' | 'issuedBy'>): boolean {
  return !!e.issuedBy && e.role === 'attestation';
}

/**
 * The sentence of a signature this installation made (R-SIG-056): what it confirms, never that a trust anchor knows its signer, since
 * Quader checks no signature it made (R-SIG-054); a retired identity's, which is no longer served, says so and what follows.
 */
export function issuedSentence(e: Pick<SignatureEntry, 'role' | 'issuedBy'>): string {
  const retired = e.issuedBy?.state === 'retired';
  if (e.role === 'attestation') {
    return retired
      ? $localize`:@@sig.issued.receipt-retired:Made with a signing identity an administrator retired, so it is no longer served. Quader makes it again with the current identity.`
      : $localize`:@@sig.issued.receipt:This installation's signed record of when it received the artifact, from where, and what its checks said then. It is not a signature of the publisher.`;
  }
  return retired
    ? $localize`:@@sig.issued.registry-retired:Signed with a signing identity an administrator retired, so npm no longer gets it. Quader signs the version again with the current identity.`
    : $localize`:@@sig.issued.registry:This installation signed the version with its signing identity, and npm audit signatures checks it against this repository's keys. It confirms where the version was published, not who built it.`;
}

/** The malware state a receipt names (R-SIG-051): the scan's words for a verdict, and what "not scanned" and "pending" meant then. */
export function receiptMalwareWords(state: SignatureReceipt['malware']['state']): string {
  switch (state) {
    case 'not_scanned':
      return $localize`:@@sig.receipt.malware-off:Not scanned: the malware scan is off`;
    case 'pending':
      return $localize`:@@sig.receipt.malware-pending:No verdict yet when it was issued`;
    default:
      return malwareStateWord(state);
  }
}

/** The vulnerability state a receipt names (R-SIG-051), as of the date of its advisory data. */
export function receiptVulnerabilityWords(v: SignatureReceipt['vulnerabilities']): string {
  switch (v.state) {
    case 'evaluated': {
      const grade = v.grade ?? 'N/A';
      const c = v.counts;
      const open = c ? c.critical + c.high + c.medium + c.low + c.unknown : 0;
      return open === 0
        ? $localize`:@@sig.receipt.vuln-none:Grade ${grade}:grade:, no open finding`
        : $localize`:@@sig.receipt.vuln-open:Grade ${grade}:grade:, ${open}:count: open findings`;
    }
    case 'pending_scan':
    case 'pending_data':
      return $localize`:@@sig.receipt.vuln-pending:Not evaluated yet when it was issued`;
    case 'internal':
      return $localize`:@@sig.receipt.vuln-internal:An internal package: public advisories do not apply`;
    case 'scan_failed':
      return $localize`:@@sig.receipt.vuln-failed:The image's scan had failed`;
    default:
      return $localize`:@@sig.receipt.vuln-not-covered:Not covered by the vulnerability scanning`;
  }
}

/** Where Quader serves its receipt (R-SIG-054): as a referrer of the image, or through the API. */
export function servedWords(e: Pick<SignatureEntry, 'servedAs'>): string {
  return e.servedAs === 'referrer'
    ? $localize`:@@sig.receipt.served-referrer:As a referrer of the image`
    : $localize`:@@sig.receipt.served-api:Through the API`;
}

/** Why an image's receipt is served through the API and not as a referrer (R-SIG-054, Q-SIG-66, Q-SIG-183); empty for other formats. */
export function servedReasonWords(e: Pick<SignatureEntry, 'servedAs' | 'servedReason'>): string {
  if (e.servedAs === 'referrer') return $localize`:@@sig.receipt.reason-referrer:cosign and a cluster's policy engine find it beside the image.`;
  switch (e.servedReason) {
    case 'hides_signature':
      return $localize`:@@sig.receipt.reason-hides:The image carries an older cosign signature, which cosign stops reading once a referrer stands beside it.`;
    case 'no_referrers_api':
      return $localize`:@@sig.receipt.reason-remote:A remote or virtual repository holds the image, and Quader adds no referrer of its own there.`;
    default:
      return '';
  }
}

/** The integrity state a receipt names of one signature, in a few words (R-SIG-006): what the check said when it was issued. */
export function receiptIntegrityWords(integrity: string): string {
  switch (integrity) {
    case 'valid':
      return $localize`:@@sig.receipt.integrity-valid:matched the content`;
    case 'invalid':
      return $localize`:@@sig.receipt.integrity-invalid:did not match the content`;
    case 'no_key':
      return $localize`:@@sig.receipt.integrity-no-key:no key known`;
    case 'pending':
      return $localize`:@@sig.receipt.integrity-pending:not checked yet`;
    case 'unsupported':
      return $localize`:@@sig.receipt.integrity-unsupported:scheme not supported`;
    case 'malformed':
      return $localize`:@@sig.receipt.integrity-malformed:could not be read`;
    default:
      return integrity;
  }
}

/** Why a check reached its state (the closed reasons of R-SIG-006), in plain words; an unknown code is shown as it is. */
export function signatureReason(reason: string | null): string {
  switch (reason) {
    case null:
      return '';
    case 'digest_mismatch':
      return $localize`:@@sig.reason.digest:the content's digest differs from the signed one`;
    case 'subject_mismatch':
      return $localize`:@@sig.reason.subject:the signature names other content`;
    case 'signature_mismatch':
      return $localize`:@@sig.reason.signature:the signature value does not verify`;
    case 'chain_broken':
      return $localize`:@@sig.reason.chain:the certificate chain does not sign it`;
    case 'log_entry_invalid':
      return $localize`:@@sig.reason.log:its transparency-log entry does not verify`;
    case 'timestamp_invalid':
      return $localize`:@@sig.reason.timestamp:its timestamp does not verify`;
    case 'attribute_missing':
      return $localize`:@@sig.reason.attribute:a signed attribute the scheme requires is missing`;
    case 'weak_algorithm':
      return $localize`:@@sig.reason.weak:a weak algorithm (SHA-1, or RSA below 2048 bits)`;
    case 'scheme_unknown':
      return $localize`:@@sig.reason.scheme:a scheme Quader does not read`;
    case 'version_unsupported':
      return $localize`:@@sig.reason.version:a version of the scheme Quader does not read`;
    case 'limits_exceeded':
      return $localize`:@@sig.reason.limits:larger than the bounds Quader reads`;
    case 'subject_missing':
      return $localize`:@@sig.reason.subject-missing:the signed content is not stored yet`;
    case 'content_unreadable':
      return $localize`:@@sig.reason.content:the signed content could not be read`;
    default:
      return reason;
  }
}

/** The sentence of one signature's state in the details (R-SIG-035); a blocked signer's is the block's own line. */
export function entrySentence(e: SignatureEntry): string {
  if (e.issuedBy) return issuedSentence(e);
  switch (e.state) {
    case 'valid':
      if (e.trust?.state === 'public' || e.trust?.state === 'internal') {
        return $localize`:@@sig.entry.trusted:The signature matches the content. A trust anchor knows the signer.`;
      }
      return $localize`:@@sig.entry.untrusted:The signature matches the content. No trust anchor knows this signer.`;
    case 'no_key':
      return $localize`:@@sig.entry.no-key:No key of this signer is known, so the signature cannot be matched with the content yet.`;
    case 'invalid': {
      const why = signatureReason(e.reason);
      return why
        ? $localize`:@@sig.entry.invalid-why:The signature does not match the content: ${why}:reason:.`
        : $localize`:@@sig.entry.invalid:The signature does not match the content.`;
    }
    case 'unsupported':
      return $localize`:@@sig.entry.unsupported:Scheme not supported.`;
    case 'malformed':
      return $localize`:@@sig.entry.malformed:The signature could not be read.`;
    default:
      return $localize`:@@sig.entry.pending:Not checked yet.`;
  }
}

/**
 * The badge of one signature's own verdict in the details (R-SIG-008, R-SIG-009), or null for a state that carries none. A signature
 * this installation made itself (`issuedBy`, npm's registry signature of a local version) carries none: Quader's signature is never the
 * badge (Q-SIG-1), and its trust is not computed (R-SIG-054).
 */
export function entryBadgeState(e: Pick<SignatureEntry, 'state' | 'trust' | 'issuedBy'>): SignatureBadgeState | null {
  if (e.issuedBy) return null;
  if (e.state === 'invalid') return 'invalid';
  if (e.state !== 'valid' && e.state !== 'no_key') return null;
  switch (e.trust?.state) {
    case 'public':
      return 'verified_public';
    case 'internal':
      return 'verified_internal';
    default:
      return 'untrusted';
  }
}

/**
 * The certificate's validity in the words of R-SIG-035: a short-lived certificate "valid for 10 minutes at signing" and what covers it,
 * one that expired since "expired on …" (information, not a verdict, R-SIG-007), otherwise "valid until …". `date` formats a time.
 */
export function certificateWords(
  e: Pick<SignatureEntry, 'certificateNotBefore' | 'certificateNotAfter' | 'timeProof'>,
  now: number,
  date: (iso: string) => string,
): string {
  const after = e.certificateNotAfter;
  if (!after) return '';
  const end = Date.parse(after);
  if (e.certificateNotBefore) {
    const minutes = Math.round((end - Date.parse(e.certificateNotBefore)) / 60000);
    if (minutes > 0 && minutes <= 60) {
      if (e.timeProof === 'log') return $localize`:@@sig.cert.short-log:valid for ${minutes}:minutes: minutes at signing; covered by the log entry`;
      if (e.timeProof === 'timestamp') return $localize`:@@sig.cert.short-timestamp:valid for ${minutes}:minutes: minutes at signing; covered by the timestamp`;
      return $localize`:@@sig.cert.short:valid for ${minutes}:minutes: minutes at signing`;
    }
  }
  const when = date(after);
  return end < now ? $localize`:@@sig.cert.expired:expired on ${when}:date:` : $localize`:@@sig.cert.valid-until:valid until ${when}:date:`;
}

/** What proves the time of a signature (R-SIG-007, R-SIG-035); a signature this installation made carries its own time. */
export function timeProofWords(e: Pick<SignatureEntry, 'timeProof'> & { readonly issuedBy?: SignatureEntry['issuedBy'] }): string {
  if (e.issuedBy) return $localize`:@@sig.time.issued:by this installation's signing identity`;
  switch (e.timeProof) {
    case 'log':
      return $localize`:@@sig.time.log:proven by the transparency log`;
    case 'timestamp':
      return $localize`:@@sig.time.timestamp:proven by a timestamp`;
    default:
      return $localize`:@@sig.time.received:received by Quader`;
  }
}

/** The time of a signature: the proven one, else Quader's receipt (R-SIG-007). */
export function signatureTime(e: Pick<SignatureEntry, 'provenAt' | 'receivedAt'>): string {
  return e.provenAt ?? e.receivedAt;
}

/** The words of an anchor's scope (R-SIG-036). */
export function anchorScopeWords(scope: SignatureAnchorScope): string {
  switch (scope) {
    case 'shipped':
      return $localize`:@@sig.scope.shipped:Shipped with Quader`;
    case 'configuration':
      return $localize`:@@sig.scope.configuration:From the Helm values`;
    case 'installation':
      return $localize`:@@sig.scope.installation:Added by an administrator`;
    default:
      return $localize`:@@sig.scope.project:Added by the project's Owner`;
  }
}

/** The words of an anchor's kind (R-SIG-015). */
export function anchorKindWords(kind: SignatureAnchorKind): string {
  switch (kind) {
    case 'ca':
      return $localize`:@@sig.anchor-kind.ca:CA certificate`;
    case 'openpgp':
      return $localize`:@@sig.anchor-kind.openpgp:OpenPGP key`;
    case 'public-key':
      return $localize`:@@sig.anchor-kind.public-key:Public key`;
    default:
      return $localize`:@@sig.anchor-kind.sigstore:Sigstore identity`;
  }
}

/** The words of a block's kind (R-SIG-020). */
export function blockKindWords(kind: SignatureBlockKind): string {
  switch (kind) {
    case 'fingerprint':
      return $localize`:@@sig.block-kind.fingerprint:Fingerprint`;
    case 'serial':
      return $localize`:@@sig.block-kind.serial:Issuer and serial`;
    case 'keyid':
      return $localize`:@@sig.block-kind.keyid:Key ID`;
    default:
      return $localize`:@@sig.block-kind.identity:Sigstore identity`;
  }
}

/**
 * The line of who uploaded or where it came from (R-SIG-010, R-SIG-035), without the time, which the screen adds. An upload that names no
 * one (a Bastion's catalogue strips who uploaded, bastion.md R-BAS-106) is "Uploaded" alone (Q-UI-1452).
 */
export function originWords(o: SignatureOrigin): string {
  if (o.kind === 'fetched') {
    const from = o.upstreamUrl ?? '';
    return from ? $localize`:@@sig.origin.fetched:Fetched from ${from}:upstream:` : $localize`:@@sig.origin.fetched-remote:Fetched from the remote's upstream`;
  }
  const name = o.principalName ?? '';
  if (!o.ci && !o.principalKind && !name) return $localize`:@@sig.origin.uploaded:Uploaded`;
  if (o.ci) {
    const parts = [o.ci.projectPath, o.ci.ref, o.ci.pipelineId ? $localize`:@@sig.origin.pipeline:pipeline ${o.ci.pipelineId}:id:` : undefined]
      .filter((p): p is string => !!p)
      .join(' · ');
    return $localize`:@@sig.origin.ci:Uploaded by CI job ${parts}:job:`;
  }
  switch (o.principalKind) {
    case 'service_account':
      return $localize`:@@sig.origin.service-account:Uploaded by the service account ${name}:name:`;
    case 'anonymous':
      return $localize`:@@sig.origin.anonymous:Uploaded without signing in`;
    default:
      return $localize`:@@sig.origin.user:Uploaded by ${name}:name:`;
  }
}

/** A shortened fingerprint or digest for a list: its first and last characters. */
export function shortFingerprint(value: string): string {
  const v = value.replace(/^sha256:/i, '');
  return v.length > 20 ? `${v.slice(0, 8)}…${v.slice(-8)}` : v;
}
