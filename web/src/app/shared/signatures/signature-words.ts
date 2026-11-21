import {
  SignatureAnchorKind,
  SignatureAnchorScope,
  SignatureBadgeState,
  SignatureBlockKind,
  SignatureEntry,
  SignatureOrigin,
  SignatureStatus,
} from '../../api/signatures-api';

/**
 * The words of the signatures (R-SIG-009, R-SIG-035): one place for what each state, role, scheme and anchor is called, so that a
 * version, a file, the admin page, a project's anchors and the security dashboard say the same. A signature says who signed and
 * whether a trust anchor knows the signer; no screen calls it a guarantee that the content is safe (H-1171).
 */

/** The tone of a badge: green for the two verified states only (Q-UI-96), neutral for an untrusted signer, red for an invalid one. */
export type SignatureTone = 'ok' | 'neutral' | 'bad';

/** A badge of R-SIG-035: its word, its icon (a shield of the icon font) and its tone; never told by colour alone. */
export interface SignatureBadgeLook {
  readonly word: string;
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
      return { word: $localize`:@@sig.badge.verified-public:Verified · public`, icon: 'gpp_good', tone: 'ok' };
    case 'verified_internal':
      return { word: $localize`:@@sig.badge.verified-internal:Verified · internal`, icon: 'gpp_good', tone: 'ok' };
    case 'untrusted':
      return { word: $localize`:@@sig.badge.untrusted:Signed · signer not trusted`, icon: 'shield', tone: 'neutral' };
    case 'invalid':
      return { word: $localize`:@@sig.badge.invalid:Invalid signature`, icon: 'gpp_bad', tone: 'bad' };
    default:
      return null;
  }
}

/** Whether a member `signatures` carries a badge. */
export function hasSignatureBadge(status: SignatureStatus | null | undefined): status is SignatureStatus {
  return signatureBadge(status?.state) !== null;
}

/** The word of a state without a badge, as the details and a file's fact say it (R-SIG-009 rule 5). */
export function signatureStateWord(state: SignatureBadgeState): string {
  const badge = signatureBadge(state);
  if (badge) return badge.word;
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
export function entryKind(e: Pick<SignatureEntry, 'role' | 'scheme' | 'identity' | 'oidcIssuer' | 'schemeName'> & { readonly type?: string }): string {
  switch (e.role) {
    case 'publisher': {
      const scheme = schemeWords(e);
      if (e.scheme === 'nuget-cms') return $localize`:@@sig.kind.author:Author signature · ${scheme}:scheme:`;
      return $localize`:@@sig.kind.publisher:Publisher signature · ${scheme}:scheme:`;
    }
    case 'provenance':
      return $localize`:@@sig.kind.provenance:Provenance attestation`;
    case 'registry':
      return $localize`:@@sig.kind.registry:Origin confirmed by registry`;
    default:
      return e.type ?? schemeWords(e);
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

/** The badge of one signature's own verdict in the details (R-SIG-008, R-SIG-009), or null for a state that carries none. */
export function entryBadgeState(e: Pick<SignatureEntry, 'state' | 'trust'>): SignatureBadgeState | null {
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

/** What proves the time of a signature (R-SIG-007, R-SIG-035). */
export function timeProofWords(e: Pick<SignatureEntry, 'timeProof'>): string {
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

/** The line of who uploaded or where it came from (R-SIG-010, R-SIG-035), without the time, which the screen adds. */
export function originWords(o: SignatureOrigin): string {
  if (o.kind === 'fetched') {
    const from = o.upstreamUrl ?? '';
    return from ? $localize`:@@sig.origin.fetched:Fetched from ${from}:upstream:` : $localize`:@@sig.origin.fetched-remote:Fetched from the remote's upstream`;
  }
  const name = o.principalName ?? '';
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
