import { SignatureFileReport, SignatureRelease, SignatureVersionReport } from './signatures-api';

/**
 * What the views read of the signatures of a version or a file (R-UI-223, R-SIG-034): the report per repository, the releases a
 * repository's gate passes by, and the address of Quader's receipt attestation. The main installation's `SignaturesApi` extends it with
 * the settings, the anchors, the blocks and the releases' writes; an application without the management API provides its own.
 */
export abstract class SignatureReads {
  /** `GET /api/v1/packages/{packageId}/versions/{version}/signatures`: per readable repository that holds the version. */
  abstract version(packageId: string, version: string): Promise<SignatureVersionReport>;
  /** `GET …/repositories/{repoKey}/signatures?path=`: one stored file. */
  abstract file(repository: string, path: string): Promise<SignatureFileReport>;
  /** `GET …/repositories/{repoKey}/signatures/releases`: the repository's releases, newest first. */
  abstract releases(repository: string): Promise<readonly SignatureRelease[]>;
  /**
   * The download of Quader's receipt attestation of a stored file or an image's manifest (R-SIG-054), from the repository and the
   * location the receipt is attached at.
   */
  abstract attestationUrl(repository: string, path: string): string;
}
