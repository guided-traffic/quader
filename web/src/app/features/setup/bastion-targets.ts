import { Bastion, BastionsApi } from '../../api/bastions-api';
import { failureMayPass } from '../../shared/none-on-refusal';
import { SessionStore } from '../../core/session-store';

/**
 * The Bastions the client setup and the guides offer as a place to install from (R-BAS-007, R-UI-049):
 * the registrations that have reported a base URL. Reading them needs `endpoints:read` (R-BAS-004); a
 * caller without it, or a refusal of the list, gets no choice and no error, because the choice is
 * an offer: a Bastion named in the query of the page still works, the setup operation needs only `read`
 * on the repository (Q-BAS-96). A failure that may pass is thrown, so the page reads the list again with
 * its own reads (R-UI-216) instead of offering no choice until a reload.
 */
export async function reportedBastions(api: BastionsApi, session: SessionStore): Promise<readonly Bastion[]> {
  if (!session.has('endpoints:read')) return [];
  try {
    return (await api.list()).filter((b) => b.contacts.some((c) => c.baseUrl !== ''));
  } catch (e) {
    if (failureMayPass(e)) throw e;
    return [];
  }
}
