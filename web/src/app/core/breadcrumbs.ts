import { Injectable, computed, inject } from '@angular/core';
import { toSignal } from '@angular/core/rxjs-interop';
import { ActivatedRouteSnapshot, NavigationEnd, Router } from '@angular/router';
import { filter, map, startWith } from 'rxjs';

/** One step of the trail above the page title. */
export interface Crumb {
  readonly label: string;
  /** Router commands of the target; null for a step that is only a name (an area without a page). */
  readonly commands: readonly string[] | null;
  /** The label is data (a repository key, a user name): not translated, not hyphenated by the browser. */
  readonly literal: boolean;
}

/** A crumb declared on a route's `data.crumbs` (the area names and wrapper routes that have no title). */
export interface CrumbSpec {
  readonly label: string;
  readonly link: boolean;
}

/**
 * The breadcrumb trail of the current route (R-UI-002, R-UI-003), derived from the route
 * configuration and never maintained by screens:
 *
 * - `data.crumbs` of a route adds the named steps of an area ("Administration"), with or without a link;
 * - a route with a string `title` adds a step with that title, and a route whose title a resolver gives one step with the
 *   resolved title (the creation form of a format), unless it repeats the step before it (the chooser for an unknown format);
 * - a parameter segment (`:key`) adds a step with the parameter's value, so a detail screen reads
 *   "Repositories > my-repo";
 * - a matcher route without `data.crumbParam` (the folders and files of a repository) adds no step: the trail ends at the
 *   repository, and the page's own path shows the folders (Q-UI-510);
 * - a matcher route with `data.crumbParam` adds one step with that parameter's value (a package
 *   name such as `@team-a/ui`, whose slash belongs to the name); so does a path route with
 *   `data.crumbParam` (the project of `/projects/:project`), whose step links to that route itself; a resolved
 *   `crumbLabel` of that route replaces the parameter's value (the format of `/guides/:format`, Q-UI-553);
 * - a matcher route with `data.crumbTail` (the table of a package's versions, `…/{name}/-/versions`, Q-UI-1242) adds the package,
 *   linked to its address without the last `crumbTail` segments, and then the route's title.
 *
 * A trail of one step repeats the page title and is not shown.
 */
@Injectable({ providedIn: 'root' })
export class Breadcrumbs {
  private readonly router = inject(Router);

  // A fresh wrapper per navigation, so that the signal notices even a navigation that keeps the
  // same route objects.
  private readonly snapshot = toSignal(
    this.router.events.pipe(
      filter((e): e is NavigationEnd => e instanceof NavigationEnd),
      map(() => ({ root: this.router.routerState.snapshot.root })),
      startWith({ root: this.router.routerState.snapshot.root }),
    ),
    { requireSync: true },
  );

  /** The trail; empty when it would only repeat the page title. */
  readonly trail = computed<readonly Crumb[]>(() => {
    const crumbs = collect(this.snapshot().root);
    return crumbs.length > 1 ? crumbs : [];
  });

  /**
   * The page lies below the trail's last step (`data.belowTrail` of the deepest route: a folder or a file of a repository,
   * whose path the page shows itself, Q-UI-510): then the last step is a link like the others, not the current page.
   */
  readonly belowTrail = computed(() => {
    let route = this.snapshot().root;
    while (route.firstChild) route = route.firstChild;
    return route.data['belowTrail'] === true;
  });
}

/** Builds the trail from a snapshot tree (exported for the unit tests). */
export function collect(root: ActivatedRouteSnapshot): Crumb[] {
  const out: Crumb[] = [];
  const add = (crumb: Crumb): void => {
    const last = out[out.length - 1];
    const same =
      last?.label === crumb.label && (last.commands?.join('/') ?? null) === (crumb.commands?.join('/') ?? null);
    if (!same) out.push(crumb);
  };

  let route = root.firstChild;
  let commands: string[] = ['/'];
  while (route) {
    const config = route.routeConfig;
    const own = route.url.map((s) => s.path);
    const before = commands;
    commands = [...commands, ...own];

    const specs = config?.data?.['crumbs'] as readonly CrumbSpec[] | undefined;
    for (const spec of specs ?? []) {
      add({ label: spec.label, commands: spec.link ? commands : null, literal: false });
    }

    const title = typeof config?.title === 'string' ? config.title : undefined;
    const crumbParam = config?.data?.['crumbParam'] as string | undefined;
    if (config && config.path !== undefined && !config.matcher && crumbParam) {
      // A path route whose parameter names the thing (the project of `/projects/:project`): one step whose target is
      // that route itself, so the route has to answer there (it redirects to the project's first screen). A route that
      // resolves `crumbLabel` (a format's name, `OCI (Docker)` for `oci`) shows that instead of the parameter.
      const resolved = route.data['crumbLabel'] as string | undefined;
      add({ label: resolved || (route.params[crumbParam] ?? crumbParam), commands: [...before, ...own], literal: true });
    } else if (config && config.path === undefined && config.matcher && crumbParam) {
      // A matcher route whose parameter names the thing (a package name with its slash): one step. A version's page
      // (`data.crumbVersion`, `ui@1.4.0`) adds the package, linked to its page without the version, and then the version.
      const versionParam = config.data?.['crumbVersion'] as string | undefined;
      const version = versionParam ? route.params[versionParam] : undefined;
      // The table of a package's versions where its own address is its newest version (`…/-/versions`, `data.crumbTail` segments
      // after the name, Q-UI-1242): the package, linked to its address without them, then the page's title.
      const tail = (config.data?.['crumbTail'] as number | undefined) ?? 0;
      if (tail > 0 && own.length > tail) {
        add({ label: route.params[crumbParam] ?? crumbParam, commands: [...before, ...own.slice(0, -tail)], literal: true });
        add({ label: title ?? '', commands: [...before, ...own], literal: false });
      } else if (version) {
        const last = own.length - 1;
        const unversioned = own.map((s, i) => (i === last ? s.slice(0, s.length - version.length - 1) : s));
        add({ label: route.params[crumbParam] ?? crumbParam, commands: [...before, ...unversioned], literal: true });
        add({ label: version, commands: [...before, ...own], literal: true });
      } else {
        add({ label: route.params[crumbParam] ?? crumbParam, commands: [...before, ...own], literal: true });
      }
    } else if (config && typeof config.title === 'function' && config.path !== undefined && route.title) {
      // A title a resolver gives (the creation form's "New OCI (Docker) repository"): one step that names the page. A resolver that
      // answers its parent's title (an unknown format shows the chooser, titled "New repository") adds no step: the trail read
      // "New repository / New repository" (Q-UI-1215).
      if (out[out.length - 1]?.label !== route.title) add({ label: route.title, commands: [...before, ...own], literal: false });
    } else if (config && title && config.path !== undefined) {
      const pieces = config.path.split('/');
      pieces.forEach((piece, i) => {
        const here = [...before, ...own.slice(0, i + 1)];
        if (piece.startsWith(':')) {
          add({ label: route!.params[piece.slice(1)] ?? piece, commands: here, literal: true });
        } else if (i === pieces.length - 1) {
          add({ label: title, commands: here, literal: false });
        }
      });
    }
    route = route.firstChild;
  }
  return out;
}
