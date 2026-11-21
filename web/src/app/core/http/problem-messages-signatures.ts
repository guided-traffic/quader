/**
 * The codes of the signatures' operations (R-SIG-015 to R-SIG-020, R-SIG-034): what the admin page and a project's anchors meet when a
 * change is refused.
 */
export function describeSignatureProblem(code: string | null): string | null {
  switch (code) {
    case 'secret_material':
      return $localize`:@@problem.secret_material:This holds a private key. A trust anchor takes public material only: a certificate or a public key.`;
    case 'anchor_exists':
      return $localize`:@@problem.anchor_exists:A trust anchor with this key exists already. Choose another key.`;
    case 'anchor_managed':
      return $localize`:@@problem.anchor_managed:This trust anchor is shipped with Quader or declared in the Helm values; it cannot be changed here.`;
    case 'project_anchors_disabled':
      return $localize`:@@problem.project_anchors_disabled:An administrator has switched project trust anchors off, so a project's anchors cannot be changed now.`;
    case 'block_exists':
      return $localize`:@@problem.block_exists:This signer is blocked already.`;
    default:
      return null;
  }
}
