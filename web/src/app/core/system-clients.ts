import { Injectable, Signal, computed, inject, resource } from '@angular/core';
import { UpstreamApi } from '../api/upstream-api';
import { valueOf } from '../shared/paged-resource';
import { SessionStore } from './session-store';
import { isSystemRepositoryName, mayServeClients, upstreamKeyOf } from './upstream-project';

/** How long one answer about a switch is reused: the screens of one visit ask it once, not once per component. */
const KEEP_MS = 5_000;

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
 */
@Injectable({ providedIn: 'root' })
export class SystemClients {
  private readonly upstream = inject(UpstreamApi);
  private readonly session = inject(SessionStore);
  private readonly kept = new Map<string, { readonly at: number; readonly answer: Promise<boolean> }>();

  /** Whether `<project>/<key>` answers its clients: always for any repository but a system one, for `upstream/clamav` while on. */
  async served(name: string): Promise<boolean> {
    if (!isSystemRepositoryName(name)) return true;
    const key = upstreamKeyOf(name);
    if (!mayServeClients(key)) return false;
    if (!this.session.has('repositories:read-config')) return true;
    const now = Date.now();
    const hit = this.kept.get(name);
    if (hit && now - hit.at < KEEP_MS) return hit.answer;
    const answer = this.upstream.get(key!).then(
      (r) => r.value.clients?.enabled === true,
      () => false,
    );
    this.kept.set(name, { at: now, answer });
    return answer;
  }

  /** A write of the switch: the next question reads it again. */
  forget(): void {
    this.kept.clear();
  }
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
  return computed(() => isSystemRepositoryName(name()) && valueOf(served) !== true);
}
