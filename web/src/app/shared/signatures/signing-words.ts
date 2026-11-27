import { ApiError } from '../../api/problem';
import { SigningIdentity } from '../../api/signing-api';
import { fieldMessage } from '../field-errors';

const DAY_MS = 86_400_000;
/** The validity is shown amber from 14 days before the certificate's end (R-SIG-045, R-SIG-056, R-SIG-057). */
export const SIGNING_WARN_DAYS = 14;

/** The validity of an identity's certificate against `now`: ended, ending within the warning window, or fine. */
export function signingValidity(identity: Pick<SigningIdentity, 'notAfter'>, now: number): 'expired' | 'soon' | 'ok' {
  if (!identity.notAfter) return 'ok';
  const left = Date.parse(identity.notAfter) - now;
  if (left <= 0) return 'expired';
  return left < SIGNING_WARN_DAYS * DAY_MS ? 'soon' : 'ok';
}

/** Whole days left until the certificate's end, rounded up: "expires in 12 days" (R-SIG-056). */
export function signingDaysLeft(identity: Pick<SigningIdentity, 'notAfter'>, now: number): number {
  return identity.notAfter ? Math.max(1, Math.ceil((Date.parse(identity.notAfter) - now) / DAY_MS)) : 0;
}

/** A SHA-256 in groups of four, the way an administrator compares it with the CA's record. */
export function groupedDigest(hex: string): string {
  return hex.replace(/(.{4})(?=.)/g, '$1 ');
}

/**
 * The check a refusal of R-SIG-045 names, in plain words (R-SIG-055): the server writes `<its detail> (<check>)` into a field error of
 * the upload and the import, and into the refused pair of the Helm values' files (R-SIG-046). Null for a check this UI does not know.
 */
export function signingCheckText(check: string): string | null {
  switch (check) {
    case 'malformed':
      return $localize`:@@signing.check.malformed:This is not PEM, or a certificate in it does not parse.`;
    case 'chain_bounds':
      return $localize`:@@signing.check.chain_bounds:The chain is too large: it holds at most 8 certificates of 16 KiB each.`;
    case 'key_type':
      return $localize`:@@signing.check.key_type:The key is not ECDSA on P-256, the one type npm's client accepts.`;
    case 'key_mismatch':
      return $localize`:@@signing.check.key_mismatch:The certificate is not for this key.`;
    case 'leaf_is_ca':
      return $localize`:@@signing.check.leaf_is_ca:The first certificate is a CA certificate. Put Quader's own certificate first, then its intermediates.`;
    case 'key_usage':
      return $localize`:@@signing.check.key_usage:The certificate's key usage lacks digitalSignature.`;
    case 'code_signing':
      return $localize`:@@signing.check.code_signing:The certificate is not for code signing: ask the CA for the extended key usage codeSigning.`;
    case 'alternative_name':
      return $localize`:@@signing.check.alternative_name:The certificate names no URI or e-mail address among its alternative names.`;
    case 'validity':
      return $localize`:@@signing.check.validity:The certificate is not valid now.`;
    case 'remaining_validity':
      return $localize`:@@signing.check.remaining_validity:The certificate ends in less than 24 hours.`;
    case 'chain_root':
      return $localize`:@@signing.check.chain_root:The chain does not end in a self-signed root. Add the root certificate last, or in the field for the root.`;
    case 'chain_build':
      return $localize`:@@signing.check.chain_build:The chain does not lead from the certificate to its root for code signing.`;
    default:
      return null;
  }
}

/**
 * A refusal of R-SIG-045 as one text: the check in plain words, then the server's detail with the check's name in brackets, which
 * holds what only the server knows (the dates of a certificate, the curve of a key). A text without a check is the detail alone.
 */
export function signingRefusalText(reason: string): string {
  const trimmed = reason.trim();
  const match = /\(([a-z_]+)\)$/.exec(trimmed);
  const plain = match ? signingCheckText(match[1] ?? '') : null;
  const detail = trimmed.charAt(0).toUpperCase() + trimmed.slice(1) + (trimmed.endsWith('.') ? '' : '.');
  return plain ? `${plain} ${detail}` : detail;
}

/** The error under a field of the material (`/chain`, `/ca`, `/key`): a refusal of R-SIG-045 with its check, else the field's message. */
export function signingFieldError(error: ApiError | null, pointer: string): string | null {
  const problem = error?.fieldErrors.get(pointer);
  if (!problem) return null;
  if (problem.code === 'invalid_value' && problem.detail && /\([a-z_]+\)$/.test(problem.detail.trim())) return signingRefusalText(problem.detail);
  return fieldMessage(problem);
}

/** The state of an identity in words (R-SIG-045); the colour of a badge is a second signal, never the only one. */
export function signingStateText(identity: SigningIdentity): string {
  switch (identity.state) {
    case 'pending':
      return $localize`:@@signing.state.pending:Waiting for its certificate`;
    case 'announced':
      return $localize`:@@signing.state.announced:Announced: listed, does not sign yet`;
    case 'current':
      return $localize`:@@signing.state.current:Current: signs`;
    case 'previous':
      return $localize`:@@signing.state.previous:Previous: its npm signatures stay valid`;
    default:
      return $localize`:@@signing.state.retired:Retired`;
  }
}

/** Where an identity comes from (R-SIG-046, R-SIG-047). */
export function signingSourceText(identity: Pick<SigningIdentity, 'source'>): string {
  return identity.source === 'configuration'
    ? $localize`:@@signing.source.configuration:From the Helm configuration`
    : $localize`:@@signing.source.upload:Uploaded in the admin area`;
}
