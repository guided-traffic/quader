import { Injectable, inject } from '@angular/core';
import { ApiError } from '../../app/api/problem';
import { BastionData, seg } from './http';

/**
 * A public repository as `/ui/data/v1/repositories` and `/ui/data/v1/repositories/<project>/<key>` name it (bastion.md R-BAS-129,
 * R-BAS-132): its qualified name and format always; its class and description while the repositories are shown, which the single
 * answer leaves out while they are hidden (R-BAS-131).
 */
export interface PublicRepository {
  readonly name: string;
  readonly format: string;
  readonly class?: string;
  readonly description?: string;
}

/** How long one read serves the pages and titles that ask together (a navigation's guard, its title and its page). */
const SHARED_MS = 5_000;

interface Kept<T> {
  readonly at: number;
  readonly value: Promise<T>;
}

/**
 * The public repositories the Bastion shows (R-UI-273): the list of every one, which a project's repositories and the start's counts
 * read and which answers 404 while the repositories are hidden, and one repository's facts, which its page, its files and its Set up
 * client read in both modes. One read serves the reads of a moment; a failed one is not kept.
 */
@Injectable({ providedIn: 'root' })
export class BastionRepositories {
  private readonly data = inject(BastionData);
  private list$: Kept<readonly PublicRepository[]> | null = null;
  private readonly single = new Map<string, Kept<PublicRepository | null>>();

  /** Every public repository in the order of the public set. */
  list(): Promise<readonly PublicRepository[]> {
    if (this.list$ && Date.now() - this.list$.at < SHARED_MS) return this.list$.value;
    const value = this.data.get<{ readonly items: readonly PublicRepository[] }>('repositories').then((a) => a.items);
    const kept = { at: Date.now(), value };
    value.catch(() => {
      if (this.list$ === kept) this.list$ = null;
    });
    this.list$ = kept;
    return value;
  }

  /** One repository of the public set; null for one the Bastion does not show (its answer, 404). */
  one(project: string, key: string): Promise<PublicRepository | null> {
    const name = `${project}/${key}`;
    const held = this.single.get(name);
    if (held && Date.now() - held.at < SHARED_MS) return held.value;
    const value = this.data.get<PublicRepository>(`repositories/${seg(project)}/${seg(key)}`).catch((e: unknown) => {
      if (e instanceof ApiError && e.status === 404) return null;
      throw e;
    });
    const kept = { at: Date.now(), value };
    value.catch(() => {
      if (this.single.get(name) === kept) this.single.delete(name);
    });
    this.single.set(name, kept);
    return value;
  }
}
