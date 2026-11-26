/**
 * The codes a publish of an Ansible Galaxy collection is refused with before its archive was read (formats/ansible.md R-ANS-007,
 * R-ANS-008, R-ANS-061): what the dialog Upload collection says in the reader's language; the notice keeps the server's own sentence,
 * which names the collection and the version, among its facts (R-UI-216).
 */
export function describeAnsibleProblem(code: string | null): string | null {
  switch (code) {
    case 'ansible_version_exists':
      return $localize`:@@problem.ansible_version_exists:This version of the collection is published already, with other content. A version cannot change: build the collection with a new version.`;
    case 'ansible_version_tombstoned':
      return $localize`:@@problem.ansible_version_tombstoned:This version of the collection was deleted, and its number is not taken again until someone who manages the repository clears its tombstone.`;
    case 'ansible_sha256_mismatch':
      return $localize`:@@problem.ansible_sha256_mismatch:The archive that arrived is not the one that was sent. Nothing was stored; upload it again.`;
    case 'ansible_invalid_request':
      return $localize`:@@problem.ansible_invalid_request:The server could not read the upload as a collection archive.`;
    case 'shadows_remote_package':
      return $localize`:@@problem.shadows_remote_package:A remote repository serves a package of this name to the same clients. Its first version here needs someone who manages the repository.`;
    default:
      return null;
  }
}
