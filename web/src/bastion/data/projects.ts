import { Injectable, inject } from '@angular/core';
import { BastionData } from './http';

/** A project as `/ui/data/v1/projects` names it (bastion.md R-BAS-128): its key, display name and description ("" for none). */
export interface BastionProject {
  readonly key: string;
  readonly displayName: string;
  readonly description: string;
}

/** How long one read of the projects serves the pages and titles that ask together (a navigation's title and its page). */
const SHARED_MS = 5_000;

/**
 * The projects the Bastion offers, with their names (bastion.md R-BAS-128, Q-UI-1470): the start page lists them, a project's packages
 * page and its title name it as the main installation does, never by its key alone. One read serves the reads of a moment; a failed one
 * is not kept.
 */
@Injectable({ providedIn: 'root' })
export class BastionProjects {
  private readonly data = inject(BastionData);
  private kept: { readonly at: number; readonly list: Promise<readonly BastionProject[]> } | null = null;

  /** The projects in the order of their keys. */
  list(): Promise<readonly BastionProject[]> {
    if (this.kept && Date.now() - this.kept.at < SHARED_MS) return this.kept.list;
    const list = this.data.get<{ readonly items: readonly BastionProject[] }>('projects').then((a) => a.items);
    list.catch(() => {
      if (this.kept?.list === list) this.kept = null;
    });
    this.kept = { at: Date.now(), list };
    return list;
  }

  /** The project of a key; its key as its name where the Bastion does not offer it (or the read fails). */
  async named(key: string): Promise<BastionProject> {
    try {
      return (await this.list()).find((p) => p.key === key) ?? { key, displayName: key, description: '' };
    } catch {
      return { key, displayName: key, description: '' };
    }
  }
}
