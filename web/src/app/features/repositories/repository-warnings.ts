import { RepositoryWarning } from '../../api/repositories-api';
import { clientsLoweredWarning } from '../upstream/upstream-words';
import { memberInactiveText } from './well-known-members';

/**
 * The sentence of a warning a repository's write answered (R-UI-214): the screen says it per code in its own language and never
 * shows the API's English `detail`, which is written for a reader of the API. `members` names a virtual repository's members, so
 * that `member_inactive` can name the member it is about.
 */
export function repositoryWarningText(w: RepositoryWarning, members: readonly string[] = []): string {
  switch (w.code) {
    case 'member_inactive':
      return memberInactiveText(w.pointer, members);
    case 'no_members':
      return $localize`:@@repo.warning.no-members:The virtual repository has no members, so it resolves nothing.`;
    case 'dependency_confusion_unprotected':
      return $localize`:@@repo.warning.unprotected:The virtual repository has local and remote members, no pin rule, and its local members claim no names: a public package can take the place of an internal name that is not published yet.`;
    case 'pattern_unscoped_only':
      return $localize`:@@repo.warning.unscoped-only:The pattern * matches names without a scope only. Add @*/* for every scoped name, or use ** for every name.`;
    case 'claim_overlaps':
      return $localize`:@@repo.warning.claim-overlaps:A pattern overlaps a claim of another repository; the more specific claim decides where a name is resolved from.`;
    case 'deb_upstream_unverified':
      return $localize`:@@repo.warning.deb-unverified:The remote has no upstream keys: it stores what its upstream sends, and its clients verify it with their own keyrings.`;
    case 'plain_http_upstream':
      return $localize`:@@repo.warning.plain-http:The upstream is reached over plain http: credentials and content cross the network unprotected.`;
    case 'source_network_single_address':
      return $localize`:@@repo.warning.single-address:The source network holds a single address, which fits one replica at most. Enter the network the replicas' addresses come from.`;
    case 'mirror_unbounded':
      return $localize`:@@repo.warning.mirror-unbounded:The mirror has neither a scope with a fixed prefix nor a storage limit: it stores everything the upstream offers and the rules deliver.`;
    case 'public_populates_remote':
      return $localize`:@@repo.warning.public-fills:A request without a credential at this installation fills the cache of this repository, because filling it needs only the right to read; one through a Bastion never does. To let only a caller who may deploy fill it, set its cache fill action to deploy through the API.`;
    case 'public_not_allowed_lowered':
      return clientsLoweredWarning();
  }
}
