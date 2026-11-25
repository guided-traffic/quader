import { createHash } from 'node:crypto';
import { Page, expect } from '@playwright/test';
import { longDebKey, otherProject, remoteCache } from './seed';

/**
 * A remote's list after a mirror pass (section 12 C of docs/projektplan/promt.md, W-24; R-API-081, R-API-080, R-API-065): no pass runs
 * on the render stack, so the answers of the list are changed in the browser, each row and each version shaped as the server answers
 * one of the same format's seeded repository (`template`), so that a member the server adds stands in these rows too. Nothing is saved.
 */

/** A row a mirror pass stored, and the first page of the versions it opens to, newest first. */
interface PassRow {
  readonly name: string;
  /** How many versions the pass stored; an opened row lists its newest and links to the package's page for all of them. */
  readonly count: number;
  readonly versions: readonly { readonly version: string; readonly size: number | null; readonly partial?: boolean }[];
  /** The downloads of clients through the remote since the pass, on its latest version; most rows have none. */
  readonly downloads?: number;
}

/** The tags of `library/nginx` on the first page of its 214, after the named ones: the upstream's variants of older releases. */
const olderNginx = ['bookworm', 'alpine', 'alpine-slim', 'alpine3.20-otel', 'bookworm-otel', 'perl'].flatMap((variant) =>
  ['1.27.2', '1.27.1'].map((release) => ({ version: `${release}-${variant}`, size: variant.startsWith('alpine') ? 49_618_340 : 139_826_118, partial: true })),
);

/**
 * What a bounded pass stored in the OCI remote `docker-hub-remote` and in the Debian remote `debian` of the render project, in the
 * scope the walk `storedMirror` of screens.ts shows (OCI: library/nginx, library/postgres and bitnami/*, the two default platforms of an
 * index, Q-OCI-223; Debian: nginx* and postgresql-15* of bookworm, amd64 and arm64). An index of more platforms upstream than the two is
 * held in part, so its size is partial (R-API-080), and a tag whose platforms the pass did not reach yet has none; a Debian version's size
 * is the sum of its package files of every architecture. The long tags and names are the upstream's own.
 */
export const passRows: Record<'oci' | 'deb', { readonly template: string; readonly rows: readonly PassRow[] }> = {
  oci: {
    template: remoteCache.key,
    rows: [
      {
        name: 'bitnami/postgresql-repmgr',
        count: 3,
        versions: [
          { version: '16.6.0-debian-12-r2', size: 301_352_817, partial: true },
          { version: '16.6.0-debian-12-r1', size: 301_352_817, partial: true },
          { version: '16.4.0-debian-12-r19', size: 295_018_455, partial: true },
        ],
      },
      { name: 'bitnami/redis', count: 1, downloads: 37, versions: [{ version: '7.4.1-debian-12-r3', size: 124_118_203, partial: true }] },
      {
        name: 'library/nginx',
        count: 214,
        downloads: 1_284,
        versions: [
          { version: '1.27.3-alpine3.20-perl', size: 48_734_906, partial: true },
          { version: '1.27.3-alpine-slim', size: 12_904_771, partial: true },
          { version: '1.27.3-bookworm-perl', size: 141_337_288, partial: true },
          { version: '1.27.3', size: 143_002_614, partial: true },
          { version: 'stable', size: 140_266_031, partial: true },
          { version: 'latest', size: 143_002_614, partial: true },
          { version: '1.27.3-alpine3.20-otel', size: null },
          ...olderNginx.slice(0, 5),
        ],
      },
      { name: 'library/postgres', count: 2, versions: [{ version: '17.2-bookworm', size: 303_915_662, partial: true }, { version: '17.2', size: 303_915_662, partial: true }] },
    ],
  },
  deb: {
    template: longDebKey,
    rows: [
      { name: 'nginx', count: 2, downloads: 12, versions: [{ version: '1.22.1-9+deb12u1', size: 186_368 }, { version: '1.22.1-9', size: 185_344 }] },
      { name: 'nginx-common', count: 2, versions: [{ version: '1.22.1-9+deb12u1', size: 114_688 }, { version: '1.22.1-9', size: 114_688 }] },
      { name: 'nginx-extras', count: 2, versions: [{ version: '1.22.1-9+deb12u1', size: 1_312_768 }, { version: '1.22.1-9', size: 1_309_696 }] },
      { name: 'postgresql-15', count: 2, versions: [{ version: '15.10-0+deb12u1', size: 35_120_847 }, { version: '15.8-0+deb12u1', size: 35_120_847 }] },
      { name: 'postgresql-15-postgis-3-scripts', count: 1, versions: [{ version: '3.3.2+dfsg-1', size: 3_239_936 }] },
    ],
  },
};

/** The page of versions a list asks for where its request names no `limit`. */
const VERSIONS_PAGE = 20;

/**
 * An OCI or a Debian remote's list after a mirror pass: the rows as it opens, then the row of the most versions opened, with its long
 * tags, its sizes (partial, and one not known) and the link to all its tags. Every row a pass stored that no client fetched says 0 downloads; the
 * rows a client pulled since count them (R-API-065).
 */
export async function* mirrorFilled(page: Page): AsyncGenerator<string> {
  const key = new URL(page.url()).pathname.split('/').at(-1)!;
  const format = key === 'debian' ? 'deb' : 'oci';
  const ref = `${otherProject}/${key}`;
  const pass = passRows[format];
  const source = `${otherProject}/${pass.template}`;
  const read = async (url: string): Promise<Record<string, unknown>> => {
    const res = await page.request.get(url);
    expect(res.ok(), `${url}: ${res.status()}`).toBe(true);
    return (await res.json()) as Record<string, unknown>;
  };
  // The server's own row and version of the format, with the grade it answers for them where the installation scans.
  const sourceRow = ((await read(`/api/v1/packages?repository=${encodeURIComponent(source)}&format=${format}&include=vulnerabilities`))['items'] as Record<string, unknown>[])[0]!;
  const sourceVersion = (
    (await read(`/api/v1/packages/${encodeURIComponent(sourceRow['id'] as string)}/versions?repository=${encodeURIComponent(source)}&include=digest`))[
      'items'
    ] as Record<string, unknown>[]
  )[0]!;
  const sourceGrade = sourceRow['vulnerabilityGrade'] as Record<string, unknown> | undefined;
  const grade = sourceGrade === undefined ? undefined : { ...sourceGrade, repository: ref };
  // The pass ended two hours ago; it stored the versions of a row a few seconds apart.
  const passEnd = Date.now() - 2 * 3_600_000;
  const at = (secondsBefore: number): string => new Date(passEnd - secondsBefore * 1000).toISOString();
  const idOf = (row: PassRow): string => `mirror-${format}-${row.name.replace(/[^a-z0-9]+/g, '-')}`;
  const digestOf = (version: string): string => `sha256:${createHash('sha256').update(`${ref}:${version}`).digest('hex')}`;
  const { digest: _digest, ...versionShape } = sourceVersion;
  const versionsOf = (row: PassRow, r: number, limit: number) =>
    row.versions.slice(0, limit).map((v, i) => ({
      ...versionShape,
      version: v.version,
      prerelease: false,
      publishedAt: at(r * 300 + i * 7),
      sizeBytes: v.size,
      sizePartial: v.partial ?? false,
      ...(format === 'oci' ? { digest: digestOf(v.version) } : {}),
      repositories: [ref],
      downloadCount: i === 0 ? (row.downloads ?? 0) : 0,
      lastDownloadedAt: i === 0 && row.downloads ? new Date(Date.now() - 1_800_000).toISOString() : null,
    }));
  const { vulnerabilityGrade: _grade, ...rowShape } = sourceRow;
  const rowOf = (row: PassRow, r: number, withGrade: boolean): Record<string, unknown> => ({
    ...rowShape,
    id: idOf(row),
    format,
    name: row.name,
    description: null,
    latestVersion: row.versions[0]!.version,
    keywords: [],
    repositories: [ref],
    createdAt: at(r * 300 + row.versions.length * 7),
    updatedAt: at(r * 300),
    versionCount: row.count,
    downloadCount: row.downloads ?? 0,
    ...(withGrade && grade ? { vulnerabilityGrade: grade } : {}),
  });
  // Matched by the path: a query's value holds a slash (the repository's `platform/…`), which a glob's `*` does not cross.
  await page.route((url) => url.pathname === '/api/v1/packages', async (route) => {
    const url = new URL(route.request().url());
    if (!url.searchParams.getAll('repository').includes(ref)) return route.fallback();
    const withGrade = url.searchParams.getAll('include').includes('vulnerabilities');
    const items = pass.rows.map((row, r) => rowOf(row, r, withGrade));
    return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ items, nextCursor: null }) });
  });
  for (const [r, row] of pass.rows.entries()) {
    const id = idOf(row);
    // The page the opened row asks for (its `limit`, R-API-010), and a cursor where the pass stored more.
    await page.route((url) => url.pathname === `/api/v1/packages/${id}/versions`, (route) => {
      const limit = Number(new URL(route.request().url()).searchParams.get('limit') ?? VERSIONS_PAGE);
      return route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ items: versionsOf(row, r, limit), nextCursor: row.count > limit ? 'next' : null }),
      });
    });
    await page.route((url) => url.pathname === `/api/v1/packages/${id}/vulnerabilities`, (route) =>
      route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ packageId: idOf(row), versions: grade ? row.versions.map((v) => ({ version: v.version, grade })) : [] }),
      }),
    );
  }
  await page.reload();
  await expect(page.locator('main quader-repository-items .name-cell')).toHaveCount(pass.rows.length);
  await page.waitForLoadState('networkidle');
  yield '';
  // The row of the most versions: its newest tags or versions, a size not known, and the link to all of them.
  const most = pass.rows.reduce((a, b) => (b.versions.length > a.versions.length ? b : a));
  await page.locator(`main .name-cell button[aria-controls="versions-${idOf(most)}"]`).click();
  const opened = page.locator('main .collapse-region.open quader-repository-item-versions');
  await expect(opened.locator('table.data tbody tr').first()).toBeVisible();
  if (most.count > most.versions.length) await expect(opened.locator('a').last()).toBeVisible();
  await page.evaluate(() => Promise.all(document.getAnimations().map((a) => a.finished.catch(() => undefined))));
  yield 'opened';
}
