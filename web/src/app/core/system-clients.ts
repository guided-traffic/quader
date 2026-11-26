import { DestroyRef, Signal, computed, effect, inject, resource, untracked } from '@angular/core';
import { valueOf } from '../shared/paged-resource';
import { ReadAgain } from './read-again';
import { isSystemRepositoryName, mayServeClients, upstreamKeyOf } from './upstream-project';

/**
 * Whether a system repository of the project `upstream` answers its clients now (R-REPO-075, R-UI-190, the development agent's
 * Q-UI-908): `upstream/clamav` while **ClamAV definition updates** is on, and no other. While it is on, the screens treat it as any
 * repository the caller reads: **Set up client**, its address, a download and the client setup in the install panel; while it is off,
 * R-UI-193's rules hold (no address, no download, no command).
 *
 * A caller with `repositories:read-config` (an administrator, an auditor) reads the switch from the admin area's item. Anyone else
 * reads the repository only while the switch is on: it is `private` and fixed while off (R-REPO-075), so a screen that shows it to
 * such a caller shows it because it is on. A principal a global policy names reads a private repository too; for it the screens
 * offer a setup that answers 404 while the switch is off (named in Q-UI-908).
 *
 * A read of the views (R-UI-223): the main installation binds `HttpSystemClients` (system-clients-http.ts, `provideViewReads`), the
 * Bastion's application its own, for which a system repository its catalogue shows is public and so serves its clients (R-UI-228).
 */
export abstract class SystemClients {
  /** Whether `<project>/<key>` answers its clients: always for any repository but a system one, for `upstream/clamav` while on. */
  abstract served(name: string): Promise<boolean>;
  /** A write of the switch: the next question reads it again. */
  abstract forget(): void;
}

/**
 * Whether the repository `name` is a system repository that answers no client now (R-UI-193 while closed, Q-UI-908): true for
 * `upstream/scanner-trivy` always and for `upstream/clamav` while its switch is off or not read yet; false for every other repository.
 * Called in an injection context; the switch is asked only for a repository that may serve clients.
 */
export function systemClosed(name: () => string | null | undefined): Signal<boolean> {
  const clients = inject(SystemClients);
  const served = resource({
    params: () => {
      const n = name();
      return n && isSystemRepositoryName(n) && mayServeClients(upstreamKeyOf(n)) ? n : undefined;
    },
    loader: ({ params }) => clients.served(params),
  });
  // Closed while the switch cannot be read, and read again with the notice's back-off (R-UI-216): a failure used to read as "off"
  // until the page was opened again.
  const again = new ReadAgain(() => served.reload());
  effect(() => {
    const e = served.error();
    untracked(() => (e ? again.failed(e) : served.hasValue() && again.answered()));
  });
  inject(DestroyRef).onDestroy(() => again.answered());
  return computed(() => isSystemRepositoryName(name()) && valueOf(served) !== true);
}
