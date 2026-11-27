/**
 * Translated messages of the problem codes of the Bastion registrations and the client setup for a
 * Bastion (R-UI-042, R-UI-045, R-BAS-004, R-BAS-007), and of a repository's distribution through one (R-BAS-134, R-UI-271).
 * `describeProblem` consults this table after the access and CI tables. The codes of the token limit and of the lifetime are in
 * the access table.
 * Returns null for a code this table does not know.
 */
export function describeBastionProblem(code: string | null): string | null {
  switch (code) {
    case 'bastion_base_url_unknown':
      return $localize`:@@problem.bastion.base-url-unknown:This Bastion has not reported a base URL yet, or the one it reported is not an entry of server.additional_base_urls. Add it to that setting on the main installation, and the instructions appear.`;
    case 'repository_not_distributed':
      return $localize`:@@problem.repository_not_distributed:This Bastion does not distribute the repository.`;
    case 'bastion_not_allowed':
      return $localize`:@@problem.bastion_not_allowed:The Bastion does not allow this project; an administrator allows it on the Bastion's card.`;
    default:
      return null;
  }
}
