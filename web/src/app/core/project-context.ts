import { Injectable, computed, effect, inject, signal, untracked } from '@angular/core';
import { toSignal } from '@angular/core/rxjs-interop';
import { NavigationCancel, NavigationEnd, NavigationError, Router, RoutesRecognized } from '@angular/router';
import { filter, map } from 'rxjs';
import { collectAll } from '../api/pagination';
import { ApiError } from '../api/problem';
import { asFailure } from './http/failure';
import { Project, ProjectsApi } from '../api/projects-api';
import { FrequentPlaces } from './frequent-places';
import { upstreamFirst } from './upstream-project';
import { SessionStore } from './session-store';

/** How many recently visited projects the switcher shows. */
export const RECENT_PROJECTS = 5;

/** The project key of a URL below `/projects/<key>`, or null outside a project (Q-UI-129). */
export function projectKeyOf(url: string): string | null {
  const match = /^\/projects\/([^/?#]+)/.exec(url);
  if (!match?.[1]) return null;
  try {
    return decodeURIComponent(match[1]);
  } catch {
    return null;
  }
}

/**
 * The project the screens are in (Q-UI-129): the key is the URL (`/projects/<key>/…`), the projects are the ones the
 * caller may see (`GET /api/v1/projects`: the memberships, every project for `projects:read`), and the recently visited
 * ones are the search's store of frequently visited places, kept per signed-in principal in this browser (R-UI-096,
 * `FrequentPlaces`). A key in the URL that names no
 * visible project is still the context: the screen under it reports what the API answers.
 */
@Injectable({ providedIn: 'root' })
export class ProjectContext {
  private readonly api = inject(ProjectsApi);
  private readonly session = inject(SessionStore);
  private readonly frequent = inject(FrequentPlaces);
  private readonly router = inject(Router);

  private readonly _projects = signal<readonly Project[]>([]);
  private readonly _loaded = signal(false);
  private readonly _failure = signal<ApiError | null>(null);
  private pending: Promise<void> | null = null;

  /**
   * The URL the screens are in. It moves as soon as the router has recognised the target, before the screens of the target
   * are created, so a screen that asks for the project in its constructor gets the new one; a navigation that is cancelled
   * or fails puts the old URL back.
   */
  private readonly url = toSignal(
    this.router.events.pipe(
      filter(
        (e) =>
          e instanceof RoutesRecognized ||
          e instanceof NavigationEnd ||
          e instanceof NavigationCancel ||
          e instanceof NavigationError,
      ),
      map((e) => (e instanceof RoutesRecognized || e instanceof NavigationEnd ? e.urlAfterRedirects : this.router.url)),
    ),
    { initialValue: this.router.url },
  );

  /** The key of the project the current URL is in, or null (the start page, the admin area, the profile …). */
  readonly key = computed(() => projectKeyOf(this.url()));
  /** The projects the caller may see: the built-in project `upstream` first (Q-UI-837), then the others by key. */
  readonly projects = this._projects.asReadonly();
  /** The list has been read (successfully or not); the start page waits for it. */
  readonly loaded = this._loaded.asReadonly();
  /**
   * The failure of the last read of the list, kept until a read answers (R-UI-216): the project list shows it instead of "You are
   * not a member of a project yet", and the store reads again on its next use (`ready`) and by the list's notice.
   */
  readonly failure = this._failure.asReadonly();
  /** The record of the current project, when the caller sees it in the list. */
  readonly current = computed<Project | null>(() => {
    const key = this.key();
    return key === null ? null : (this._projects().find((p) => p.key === key) ?? null);
  });
  /** The keys of the recently visited projects, newest first. */
  readonly recent = computed(() =>
    this.frequent
      .recentProjects()
      .slice(0, RECENT_PROJECTS)
      .map((v) => v.id),
  );

  constructor() {
    effect(() => {
      // The anonymous subject reads the public projects while the installation has public content (R-UI-057).
      const ready = this.session.authenticated() || this.session.state() === 'anonymous';
      untracked(() => {
        if (ready) void this.refresh();
        else {
          this._projects.set([]);
          this._loaded.set(false);
          this._failure.set(null);
        }
      });
    });
    effect(() => {
      const key = this.key();
      // A visit of the signed-in principal: recorded again once the session has loaded, whose principal keys the store.
      if (!this.session.authenticated()) return;
      this.session.whoami();
      if (key !== null) untracked(() => this.frequent.visitProject(key));
    });
  }

  /** Reads the projects again; the screens call it after they create, change or delete one. */
  refresh(): Promise<void> {
    this.pending ??= this.read().finally(() => {
      this.pending = null;
    });
    return this.pending;
  }

  /** The projects, read once, and again after a read that failed: the start guard waits for them. */
  async ready(): Promise<readonly Project[]> {
    if (!this._loaded() || this._failure()) await this.refresh();
    return this._projects();
  }

  private async read(): Promise<void> {
    try {
      this._projects.set(upstreamFirst(await collectAll(this.api.list({})), (p) => p.key));
      this._failure.set(null);
    } catch (e) {
      // A convenience for the switcher, which then offers the recent keys only; the list of projects shows the failure. The list
      // read before stays: it is truer than none.
      this._failure.set(asFailure(e));
    } finally {
      this._loaded.set(true);
    }
  }

  /** Forgets a project that no longer exists. */
  forget(key: string): void {
    this.frequent.forgetProject(key);
  }
}
