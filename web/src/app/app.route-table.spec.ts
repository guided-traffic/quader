import { Route, Routes, UrlMatcher } from '@angular/router';
import specPage from '../../../docs/spec/api-and-ui.md' with { loader: 'text' };
import { routes } from './app.routes';
import { packageMatcher, versionMatcher, versionsMatcher } from './features/packages/packages.routes';
import { filesMatcher } from './features/repositories/repositories.routes';
import { advisoryMatcher } from './features/vulnerabilities/vulnerabilities.routes';

/** The route matchers of the router as the table of R-UI-003 writes the segments they capture. */
const MATCHERS = new Map<UrlMatcher, string>([
  [filesMatcher, 'files/{path…}'],
  [packageMatcher, '{format}/{name…}'],
  [versionMatcher, '{format}/{name…}@{version}'],
  [versionsMatcher, '{format}/{name…}/-/versions'],
  [advisoryMatcher, 'advisories/{ecosystem}/{id}'],
]);

/**
 * Every route of the router that opens a page, written as the table writes it (`:name` is `{name}`): a route with a component, a
 * redirect, or a guard that decides where a full path leads (`/`, `/admin`). The lazy areas are loaded; the not-found route is left
 * out, the table's "anything else".
 */
async function routerRoutes(rs: Routes, prefix: string, out: Set<string>): Promise<void> {
  for (const r of rs) {
    let segment: string;
    if (r.matcher) {
      const written = MATCHERS.get(r.matcher);
      if (written === undefined) throw new Error(`a route matcher below ${prefix || '/'} that this test does not know`);
      segment = written;
    } else {
      segment = (r.path ?? '')
        .split('/')
        .map((s) => (s.startsWith(':') ? `{${s.slice(1)}}` : s))
        .join('/');
    }
    if (segment === '**') continue;
    const path = segment === '' ? prefix : `${prefix}/${segment}`;
    const page =
      r.loadComponent !== undefined ||
      r.component !== undefined ||
      r.redirectTo !== undefined ||
      (r.pathMatch === 'full' && (r.canActivate?.length ?? 0) > 0);
    if (page) out.add(path || '/');
    if (r.children) await routerRoutes(r.children, path, out);
    if (r.loadChildren) await routerRoutes((await (r as Required<Pick<Route, 'loadChildren'>>).loadChildren()) as Routes, path, out);
  }
}

/**
 * The routes of the table of R-UI-003: every backquoted value of the first column that starts with `/` (a query such as `?q=` and a
 * word such as `general` are not routes), split by whether their row says "not built".
 */
function tableRoutes(): { built: Set<string>; notBuilt: Set<string> } {
  const start = specPage.indexOf('### R-UI-003 ');
  const table = specPage.indexOf('| Route | Screen | Requirement |', start);
  if (start < 0 || table < 0) throw new Error('the route table of R-UI-003 moved; update the test');
  const built = new Set<string>();
  const notBuilt = new Set<string>();
  for (const line of specPage.slice(table).split('\n').slice(2)) {
    if (!line.startsWith('|')) break;
    const first = line.split('|')[1] ?? '';
    const into = line.includes('not built') ? notBuilt : built;
    for (const m of first.matchAll(/`([^`]+)`/g)) {
      const route = m[1] ?? '';
      if (route.startsWith('/')) into.add(route);
    }
  }
  return { built, notBuilt };
}

describe('the route table of R-UI-003', () => {
  it('R-UI-003 every route of the router is a row of the route table, every row a route, and a row that says "not built" no route', async () => {
    const router = new Set<string>();
    await routerRoutes(routes, '', router);
    const { built, notBuilt } = tableRoutes();
    expect(router.size).toBeGreaterThan(80);
    expect([...router].filter((r) => !built.has(r)).sort(), 'routes of the router without a row').toEqual([]);
    expect([...built].filter((r) => !router.has(r)).sort(), 'rows that are no route of the router').toEqual([]);
    expect([...notBuilt].filter((r) => router.has(r)).sort(), 'built routes whose row says "not built"').toEqual([]);
  });
});
