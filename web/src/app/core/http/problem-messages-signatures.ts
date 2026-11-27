/**
 * The codes of the signatures' operations (R-SIG-015 to R-SIG-020, R-SIG-034), of the gate per repository (R-SIG-061, R-SIG-068,
 * R-SIG-073) and of the signing identity (R-SIG-055): what the admin page, a project's anchors and a repository's settings meet when a
 * change is refused.
 */
export function describeSignatureProblem(code: string | null): string | null {
  switch (code) {
    case 'secret_material':
      return $localize`:@@problem.secret_material:This holds a private key. A trust anchor takes public material only: a certificate or a public key.`;
    case 'anchor_exists':
      return $localize`:@@problem.anchor_exists:A trust anchor with this key exists already. Choose another key.`;
    case 'anchor_managed':
      return $localize`:@@problem.anchor_managed:This trust anchor is shipped with Quader or declared in the Helm configuration; it cannot be changed here.`;
    case 'project_anchors_disabled':
      return $localize`:@@problem.project_anchors_disabled:An administrator has switched project trust anchors off, so a project's anchors cannot be changed now.`;
    case 'block_exists':
      return $localize`:@@problem.block_exists:This signer is blocked already.`;
    // The gate per repository (R-SIG-061, R-SIG-068, R-SIG-073).
    case 'signature_weakening_refused':
      return $localize`:@@problem.signature_weakening_refused:Only an administrator or someone who triages security findings can loosen this setting. Ask one of them.`;
    case 'signature_not_verified':
      return $localize`:@@problem.signature_not_verified:This repository serves only artifacts with a verified signature, and this one has none it accepts. Someone who triages security findings can release it for this repository.`;
    case 'signature_check_pending':
      return $localize`:@@problem.signature_check_pending:Quader is checking the signatures of this artifact. Try again in a minute.`;
    case 'signatures_not_enabled':
      return $localize`:@@problem.signatures_not_enabled:Signature checks are switched off in this installation, so no repository refuses anything for its signatures.`;
    // The signing identity (R-SIG-055, Q-SIG-146): each refusal says what to do instead.
    case 'signing_identity_managed':
      return $localize`:@@problem.signing_identity_managed:The Helm configuration names the signing identity. Change it there; uploads and key requests are refused while it names one.`;
    case 'signing_identity_pending':
      return $localize`:@@problem.signing_identity_pending:A signing key is waiting for its certificate. Upload the certificate the CA issued for it, or discard it first.`;
    case 'signing_key_mismatch':
      return $localize`:@@problem.signing_key_mismatch:This certificate is not for the pending key: the CA certified another key. Upload the certificate issued for this request.`;
    case 'signing_no_pending_key':
      return $localize`:@@problem.signing_no_pending_key:No signing key is waiting for a certificate. Create a signing key first, then upload the certificate the CA issued for its request.`;
    case 'signing_identity_announced':
      return $localize`:@@problem.signing_identity_announced:A new signing identity is announced and becomes current within minutes. Wait for the switch, then try again.`;
    case 'signing_identity_exists':
      return $localize`:@@problem.signing_identity_exists:This certificate belongs to a signing identity Quader holds already.`;
    case 'signing_identity_not_previous':
      return $localize`:@@problem.signing_identity_not_previous:Only a previous signing identity can be retired. Reload the page to see its state.`;
    case 'signing_key_in_use':
      return $localize`:@@problem.signing_key_in_use:The current or the announced identity has the same key, so retiring this certificate would withdraw nothing. Replace the key first.`;
    default:
      return null;
  }
}
