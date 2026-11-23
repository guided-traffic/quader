/**
 * A principal's reference `<kind>:<name>` as a page shows it to a person (Q-UI-1006, the one rule of every page): a user by the
 * username, without the kind ("by priya"); a service account, a group, a Bastion and a CI job with their kind as a word ("service
 * account ci-build", "Servicekonto ci-build"). No page prints a reference's syntax (`user:priya`, `sa:ci-build`). A reference of a kind
 * this function does not know keeps its name and loses nothing; `null` is "someone".
 */
export function personName(ref: string | null | undefined): string {
  if (!ref) return $localize`:@@person.someone:someone`;
  const at = ref.indexOf(':');
  if (at <= 0) return ref;
  const kind = ref.slice(0, at);
  const name = ref.slice(at + 1);
  switch (kind) {
    case 'user':
      return name;
    case 'sa':
    case 'service_account':
      return $localize`:@@person.service-account:service account ${name}:name:`;
    case 'group':
      return $localize`:@@person.group:group ${name}:name:`;
    case 'bastion':
      return $localize`:@@person.bastion:Bastion ${name}:name:`;
    case 'ci':
      return $localize`:@@person.ci:CI job of ${name}:name:`;
    default:
      return ref;
  }
}
