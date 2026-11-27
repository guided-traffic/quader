import { DestroyRef, Injectable, computed, effect, inject, signal, untracked } from '@angular/core';

/** One step of the way from the start to a page: its words, the router commands of its page, and whether its words are data. */
export interface TrailStep {
  readonly label: string;
  /** Null for the step of the page itself, the last one. */
  readonly commands: readonly string[] | null;
  /** The words are data (a key, a name): not translated. */
  readonly literal: boolean;
}

/**
 * The way from the start to the page at the Bastion (R-UI-273, W-40's D20): the main installation's breadcrumb, drawn by the shell
 * above the page, its steps the ones the crawler's text writes (`Crumbs` of internal/bastion/gui/seo.go). The Bastion has no route
 * table of crumbs (the main installation's `Breadcrumbs` asks the session), so each page binds its steps here; a page that binds none
 * (the start, a legal page, not found) has no trail. A page's steps leave with it, so the next page never shows the last one's.
 */
@Injectable({ providedIn: 'root' })
export class BastionTrail {
  private readonly held = signal<{ readonly owner: object; readonly steps: readonly TrailStep[] } | null>(null);

  /** The steps of the page shown; empty where it has none. */
  readonly steps = computed(() => this.held()?.steps ?? []);

  /** Binds the page's steps for as long as the page lives; call it in the page's constructor. */
  bind(steps: () => readonly TrailStep[]): void {
    const owner = {};
    effect(() => {
      const s = steps();
      untracked(() => this.held.set({ owner, steps: s }));
    });
    inject(DestroyRef).onDestroy(() => {
      if (this.held()?.owner === owner) this.held.set(null);
    });
  }
}

/** The start's step. */
function projectsStep(): TrailStep {
  return { label: $localize`:@@bastion.start.title:Projects`, commands: ['/'], literal: false };
}

/**
 * The way to a page of a project: Projects › the project by its display name, which leads to the project's default view. `here` makes
 * the project's step the page's own (its two views, Repositories and Packages, are its page).
 */
export function projectTrail(project: string, displayName: string, here = false): TrailStep[] {
  return [projectsStep(), { label: displayName, commands: here ? null : ['/projects', project], literal: true }];
}

/**
 * The way to a page of a repository: the project's, its step Repositories while the repositories are shown, and the repository's key,
 * which leads to its page; hidden, the key follows the project (a generic repository's files keep it as a folder's name, W-40's D14).
 * `here` makes the key the page's own step.
 */
export function repositoryTrail(project: string, displayName: string, key: string, shown: boolean, here = false): TrailStep[] {
  const steps = projectTrail(project, displayName);
  if (shown) {
    steps.push({ label: $localize`:@@route.repositories.title:Repositories`, commands: ['/projects', project, 'repositories'], literal: false });
  }
  steps.push({ label: key, commands: here ? null : ['/projects', project, 'repositories', key], literal: true });
  return steps;
}

/** The way to a page of a package: the project's, its step Packages, then the package where the page is below it. */
export function packageTrail(project: string, displayName: string): TrailStep[] {
  return [
    ...projectTrail(project, displayName),
    { label: $localize`:@@nav.packages:Packages`, commands: ['/projects', project, 'packages'], literal: false },
  ];
}
