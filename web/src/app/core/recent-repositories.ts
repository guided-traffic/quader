import { Injectable, computed, inject, signal } from '@angular/core';
import { RepositoriesApi, refOf, splitRef } from '../api/repositories-api';
import { AuthzEpoch } from './authz-epoch';
import { FrequentPlaces } from './frequent-places';
import { ProjectContext } from './project-context';
import { SessionStore } from './session-store';

/** How many repositories the side bar's **Recently opened** lists (R-UI-167). */
export const RECENT_SHOWN = 5;

/** One entry of **Recently opened**: a repository the caller opened last and may still read. */
export interface RecentRepository {
  /** The qualified name `<project>/<key>`. */
  readonly ref: string;
  readonly project: string;
  readonly key: string;
  /** The project's display name, else its key. */
  readonly projectName: string;
  /** The entry's title and accessible name: the key first, as it is shown, then the project and the qualified name. */
  readonly name: string;
}

interface Confirmed {
  /** The principal and the authorization epoch the answers were given for. */
  readonly scope: string;
  /** Per qualified name, whether the server listed it for the caller. */
  readonly readable: ReadonlyMap<string, boolean>;
}

/**
 * The side bar's **Recently opened** (R-UI-167, Q-UI-662): the newest five repositories of the search's store (`FrequentPlaces`,
 * by the last visit), each shown only after one `GET /api/v1/repositories?name=…` returned it (Q-UI-406). An answer is kept for
 * the session, per principal and authorization epoch, so the request runs once for the names the session has not asked yet: when
 * the home side bar is first shown, and again when a repository the session has not seen becomes one of the five, or the epoch
 * changes. Navigating does not ask again. A name the server did not return is left out and stays stored.
 */
@Injectable({ providedIn: 'root' })
export class RecentRepositories {
  private readonly frequent = inject(FrequentPlaces);
  private readonly repositories = inject(RepositoriesApi);
  private readonly context = inject(ProjectContext);
  private readonly session = inject(SessionStore);
  private readonly epoch = inject(AuthzEpoch);

  private readonly scope = computed(() => `${this.session.whoami()?.principal?.id ?? ''}|${this.epoch.value() ?? ''}`);
  private readonly confirmed = signal<Confirmed>({ scope: '', readable: new Map() });
  private asking: string | null = null;

  /** The newest five stored repositories; none for an anonymous caller or without browser storage. */
  private readonly newest = computed<readonly string[]>(() =>
    this.session.authenticated() && this.frequent.available()
      ? this.frequent
          .recentRepositories()
          .slice(0, RECENT_SHOWN)
          .map((v) => v.id)
      : [],
  );

  /** The answers for the current principal and epoch; an older scope's are forgotten. */
  private readonly readable = computed<ReadonlyMap<string, boolean>>(() => {
    const c = this.confirmed();
    return c.scope === this.scope() ? c.readable : new Map();
  });

  /** The newest five the server has not answered for in this scope yet: what {@link confirm} asks for. */
  readonly unconfirmed = computed<readonly string[]>(() => {
    const readable = this.readable();
    return this.newest().filter((ref) => !readable.has(ref));
  });

  /** The entries of the group, newest first; empty while none is confirmed, and then the group is not shown. */
  readonly entries = computed<readonly RecentRepository[]>(() => {
    const readable = this.readable();
    const names = new Map(this.context.projects().map((p) => [p.key, p.displayName || p.key]));
    return this.newest()
      .filter((ref) => readable.get(ref) === true)
      .map((ref) => {
        const { project, key } = splitRef(ref);
        const projectName = names.get(project) ?? project;
        return {
          ref,
          project,
          key,
          projectName,
          name: $localize`:@@nav.recent.entry:${key}:key: in ${projectName}:project: (${ref}:ref:)`,
        };
      });
  });

  /** Asks the server for the names of {@link unconfirmed}, once; a failed request leaves them out until the next change. */
  async confirm(names: readonly string[]): Promise<void> {
    if (names.length === 0) return;
    const scope = this.scope();
    const asking = `${scope}|${names.join('\n')}`;
    if (this.asking === asking) return;
    this.asking = asking;
    try {
      const page = await this.repositories.list({ name: [...names] })(null, names.length);
      const listed = new Set(page.items.map(refOf));
      if (this.scope() !== scope) return;
      const readable = new Map(this.readable());
      for (const ref of names) readable.set(ref, listed.has(ref));
      this.confirmed.set({ scope, readable });
    } catch {
      // A convenience: without the answer the names stay out of the group, and stay stored.
    } finally {
      if (this.asking === asking) this.asking = null;
    }
  }
}
