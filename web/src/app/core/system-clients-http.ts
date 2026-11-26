import { Injectable, inject } from '@angular/core';
import { UpstreamApi } from '../api/upstream-api';
import { SessionStore } from './session-store';
import { SystemClients } from './system-clients';
import { isSystemRepositoryName, mayServeClients, upstreamKeyOf } from './upstream-project';

/** How long one answer about a switch is reused: the screens of one visit ask it once, not once per component. */
const KEEP_MS = 5_000;

/**
 * The main installation's {@link SystemClients}: a caller with `repositories:read-config` reads the switch from the admin area's item,
 * anyone else reads the repository only while it is on (R-REPO-075, Q-UI-908).
 */
@Injectable()
export class HttpSystemClients extends SystemClients {
  private readonly upstream = inject(UpstreamApi);
  private readonly session = inject(SessionStore);
  private readonly kept = new Map<string, { readonly at: number; readonly answer: Promise<boolean> }>();

  override async served(name: string): Promise<boolean> {
    if (!isSystemRepositoryName(name)) return true;
    const key = upstreamKeyOf(name);
    if (!mayServeClients(key)) return false;
    if (!this.session.has('repositories:read-config')) return true;
    const now = Date.now();
    const hit = this.kept.get(name);
    if (hit && now - hit.at < KEEP_MS) return hit.answer;
    // A failed read is not kept: it says nothing of the switch, and the next question reads again (R-UI-216).
    const answer = this.upstream.get(key!).then(
      (r) => r.value.clients?.enabled === true,
      (e: unknown) => {
        this.kept.delete(name);
        throw e;
      },
    );
    this.kept.set(name, { at: now, answer });
    return answer;
  }

  override forget(): void {
    this.kept.clear();
  }
}
