import { Page, expect } from '@playwright/test';

/*
 * The audit log's rows of several events (operations-and-observability.md R-OPS-055, R-OPS-056, api-and-ui.md R-UI-037), answered in
 * the browser: the render profile does not record downloads (audit.record_downloads is off by default), and the walk needs a log that
 * holds every kind of row a reader meets. Each act is written as the server writes it, and grouped as GET /api/v1/audit/rows groups it.
 */

type Json = Record<string, unknown>;

const SEC = 1000;
const at = (secondsAgo: number): string => new Date(Date.now() - secondsAgo * SEC).toISOString();
/** A hex digest made of a seed, as long as a SHA-256 is. */
const sha = (seed: number): string => (seed * 2654435761).toString(16).padStart(8, '0').slice(0, 8).repeat(8);

const priya = { type: 'user', id: '0192f0b1-2c3d-7e4f-8a5b-6c7d8e9f0a11', name: 'priya', tokenId: '0192f0b1-2c3d-7e4f-8a5b-6c7d8e9f0a12', authMethod: 'bearer' };
const sam = { type: 'user', id: '0192f0b1-2c3d-7e4f-8a5b-6c7d8e9f0a21', name: 'sam', tokenId: '0192f0b1-2c3d-7e4f-8a5b-6c7d8e9f0a22', authMethod: 'bearer' };
const ci = { type: 'ci', id: '0192f0b1-2c3d-7e4f-8a5b-6c7d8e9f0a31', name: 'ci:gitlab-internal/platform-release', tokenId: '0192f0b1-2c3d-7e4f-8a5b-6c7d8e9f0a32', authMethod: 'bearer' };
const anonymous = { type: 'anonymous', id: null, name: null, tokenId: null, authMethod: null };

let n = 0;
/** One event as its producer writes it: an item's path, digest and size, and the unit, package and version where the format names them. */
function event(
  action: string,
  secondsAgo: number,
  actor: Json,
  ip: string,
  repository: string,
  path: string,
  size: number | null,
  unit: { package?: string; version?: string; unit: string } | null,
  extra: Json = {},
  replica = 'quader-packages-7f6d9c8b5-x2k4q',
): Json {
  n++;
  const digest = sha(n);
  // A blob's or a manifest's path names its own digest.
  path = path.replace('{sha}', digest);
  const file = ['artifact.downloaded', 'artifact.deployed', 'artifact.deleted'].includes(action);
  const details: Json = file ? { path, sha256: digest, ...(size === null ? {} : { size }) } : { ...extra };
  if (unit) Object.assign(details, unit.package ? { package: unit.package } : {}, unit.version ? { version: unit.version } : {}, { unit: unit.unit });
  if (file) Object.assign(details, extra);
  return {
    id: `01a21a${String(n).padStart(2, '0')}-0000-7000-8000-${digest.slice(0, 12)}`,
    occurredAt: at(secondsAgo),
    action,
    category: action.startsWith('authz.') ? 'authz' : 'content',
    outcome: 'success',
    actor,
    source: { ip, userAgent: action === 'artifact.downloaded' && unit?.version ? 'npm/11.6.0 node/v24.11.0 linux x64' : 'docker/27.3.1 go/go1.22.7 os/linux arch/amd64' },
    traceId: digest.slice(0, 32),
    replica,
    endpoint: 'default',
    resource: { type: action === 'package.published' ? 'package' : 'repository', id: null },
    repository,
    path,
    reason: null,
    details,
  };
}

/** One artifact.downloaded as items.Store.Served writes it. */
function item(secondsAgo: number, actor: Json, ip: string, repository: string, path: string, size: number, unit: { package: string; version?: string; unit: string } | null, replica?: string): Json {
  return event('artifact.downloaded', secondsAgo, actor, ip, repository, path, unit ? size : null, unit, {}, replica);
}

/** The act of each action a row joins (`audit.ActOf`). */
const ACTS: Readonly<Record<string, string>> = {
  'artifact.downloaded': 'download',
  'bastion.data_key.handed': 'datakeys',
  'artifact.deployed': 'upload',
  'package.published': 'upload',
  'npm.publish': 'upload',
  'artifact.deleted': 'delete',
  'oci.manifest.deleted': 'delete',
  'deb.package.deleted': 'delete',
  'npm.unpublish': 'delete',
  'package.deleted': 'delete',
  'ansible.version.deleted': 'delete',
  'authz.write.allowed': 'writes',
};
const FILE_ACTIONS = ['artifact.downloaded', 'bastion.data_key.handed', 'artifact.deployed', 'artifact.deleted'];

/** The rows of a list of events, newest first by each row's first event, as the server forms them (one minute's pause at most). */
function rowsOf(events: Json[]): Json[] {
  const d = (e: Json): Json => e['details'] as Json;
  const key = (e: Json): string | null => {
    const act = ACTS[String(e['action'])];
    if (!act || typeof d(e)['unit'] !== 'string') return null;
    const a = e['actor'] as Json;
    const s = e['source'] as Json;
    return [act, e['repository'], d(e)['unit'], a['type'], a['id'], a['name'], a['tokenId'], s['ip']].join('|');
  };
  const oldest = [...events].sort((a, b) => String(a['occurredAt']).localeCompare(String(b['occurredAt'])));
  const groups: Json[][] = [];
  const open = new Map<string, Json[]>();
  for (const e of oldest) {
    const k = key(e);
    const g = k === null ? undefined : open.get(k);
    const last = g?.at(-1);
    if (g && last && Date.parse(String(e['occurredAt'])) - Date.parse(String(last['occurredAt'])) <= 60 * SEC) {
      g.push(e);
      continue;
    }
    const fresh = [e];
    groups.push(fresh);
    if (k !== null) open.set(k, fresh);
  }
  return groups
    .map((g) => {
      const head = g[0]!;
      if (key(head) === null) return { event: head, group: null };
      const files = g.filter((e) => FILE_ACTIONS.includes(String(e['action'])));
      const versions = [...new Set(g.map((e) => d(e)['version']).filter((v): v is string => typeof v === 'string'))];
      const published = g.find((e) => e['action'] === 'package.published' || e['action'] === 'npm.publish');
      let act = ACTS[String(head['action'])]!;
      if (act === 'upload' && published) act = ['oci', 'nuget'].includes(String(d(published)['format'])) ? 'push' : 'publish';
      const digest = (e: Json): string =>
        String(d(e)['sha256'] ?? String(d(e)['digest'] ?? '').replace(/^sha256:/, '') ?? '') || String((d(e)['files'] as Json[] | undefined)?.[0]?.['sha256'] ?? '');
      return {
        event: head,
        group: {
          act,
          unit: d(head)['unit'],
          package: d(head)['package'] ?? d(head)['name'] ?? null,
          version: versions[0] ?? null,
          versions: versions.length,
          items: g.length,
          more: false,
          size: files.length && files.every((e) => typeof d(e)['size'] === 'number') ? files.reduce((s, e) => s + Number(d(e)['size']), 0) : null,
          lastAt: g.at(-1)!['occurredAt'],
          first: g.slice(0, 10).map((e) => ({
            id: e['id'],
            occurredAt: e['occurredAt'],
            action: e['action'],
            path: e['path'],
            sha256: digest(e),
            version: d(e)['version'] ?? null,
            size: d(e)['size'] ?? null,
            replica: e['replica'],
            delivery: null,
          })),
        },
      };
    })
    .sort((a, b) => String(b.event['occurredAt']).localeCompare(String(a.event['occurredAt'])));
}

/**
 * A morning of acts: priya's push of an image of 24 tiny layers with its allowed writes, a CI job's NuGet push, sam's delete of an npm
 * version, priya's delete of a NuGet package of three versions, three files uploaded into a folder; priya's pull of an image of 30 tiny
 * layers over two replicas, named by its tag; an anonymous pull by digest; sam's `npm install` of three packages; a CI job's pull that is
 * still going on; a refusal of the malware gate beside them; and two downloads written before the events named their unit.
 */
function morning(): { events: Json[]; pull: Json[] } {
  n = 0;
  const images = 'web-frontend/images';
  const push: Json[] = [];
  const pushUnit = { package: 'platform/checkout-service', unit: 'platform/checkout-service' };
  for (let i = 0; i < 25; i++) {
    push.push(event('artifact.deployed', 7200 - i * 1.3, priya, '10.12.4.7', images, 'platform/checkout-service/_blobs/sha256:{sha}', 1_024 + i * 7_919, pushUnit, { overwritten: false }));
    push.push(event('authz.write.allowed', 7200 - i * 1.3 - 0.1, priya, '10.12.4.7', images, 'platform/checkout-service/_blobs/sha256:{sha}', null, pushUnit, { reason: 'allow_grant', actions: ['deploy'] }));
  }
  push.push(event('artifact.deployed', 7166, priya, '10.12.4.7', images, 'platform/checkout-service/_manifests/sha256:{sha}', 1_093, pushUnit, { overwritten: false }));
  push.push(event('artifact.deployed', 7165.9, priya, '10.12.4.7', images, 'platform/checkout-service/_tags/2.4.0', 1_093, { ...pushUnit, version: '2.4.0' }, { overwritten: false }));
  push.push(event('package.published', 7165.8, priya, '10.12.4.7', images, 'platform/checkout-service', null, { ...pushUnit, version: '2.4.0' },
    { format: 'oci', files: [{ path: 'platform/checkout-service/_tags/2.4.0', sha256: sha(999), size: 1_093 }] }));

  const nuget = 'web-frontend/nuget-local';
  const widget = { package: 'acme.widgets', version: '3.1.0', unit: 'acme.widgets@3.1.0' };
  const nugetPush = [
    event('artifact.deployed', 6600, ci, '10.40.0.12', nuget, 'acme.widgets/3.1.0/acme.widgets.3.1.0.nupkg', 184_311, widget, { overwritten: false }),
    event('artifact.deployed', 6599.9, ci, '10.40.0.12', nuget, 'acme.widgets/3.1.0/acme.widgets.nuspec', 1_204, widget, { overwritten: false }),
    event('package.published', 6599.8, ci, '10.40.0.12', nuget, 'acme.widgets', null, widget, { format: 'nuget', files: [{ path: 'acme.widgets/3.1.0/acme.widgets.3.1.0.nupkg', sha256: sha(998), size: 184_311 }] }),
  ];
  const npmLocal = 'web-frontend/npm-local';
  const leftPad = { package: 'left-pad', version: '1.3.0', unit: 'left-pad' };
  const versionDelete = [
    event('artifact.deleted', 5400, sam, '10.12.9.31', npmLocal, 'left-pad/-/left-pad-1.3.0.tgz', 2_181, leftPad),
    event('npm.unpublish', 5399.9, sam, '10.12.9.31', npmLocal, 'left-pad', null, leftPad, { integrity: 'sha512-x' }),
  ];
  const packageDelete = ['1.0.0', '1.1.0', '2.0.0'].flatMap((v, i) => {
    const legacy = { package: 'acme.legacy', version: v, unit: 'acme.legacy' };
    return [
      event('artifact.deleted', 5000 - i, priya, '10.12.4.7', nuget, `acme.legacy/${v}/acme.legacy.${v}.nupkg`, 96_000 + i * 1_000, legacy),
      event('artifact.deleted', 4999.9 - i, priya, '10.12.4.7', nuget, `acme.legacy/${v}/acme.legacy.nuspec`, 980, legacy),
      event('package.deleted', 4999.8 - i, priya, '10.12.4.7', nuget, 'acme.legacy', null, legacy),
    ];
  });
  const folder = { unit: 'releases/2026/11/' };
  const upload = ['checkout-service-2.4.0.tar.gz', 'checkout-service-2.4.0.tar.gz.sha256', 'NOTES.md'].map((f, i) =>
    event('artifact.deployed', 4200 - i * 3, sam, '10.12.9.31', 'web-frontend/generic-local', `releases/2026/11/${f}`, [48_213_377, 97, 2_311][i]!, folder, { overwritten: false }),
  );

  const image = { package: 'platform/checkout-service', unit: 'platform/checkout-service' };
  const pull: Json[] = [item(3600, priya, '10.12.4.7', images, 'platform/checkout-service/_tags/2.3.1', 1_574, { ...image, version: '2.3.1' })];
  pull.push(item(3599.6, priya, '10.12.4.7', images, 'platform/checkout-service/_manifests/sha256:{sha}', 1_093, image));
  for (let i = 0; i < 30; i++) {
    const replica = i % 2 ? 'quader-packages-7f6d9c8b5-m8v2j' : 'quader-packages-7f6d9c8b5-x2k4q';
    pull.push(item(3599 - i * 0.7, priya, '10.12.4.7', images, 'platform/checkout-service/_blobs/sha256:{sha}', 2_048 + i * 913, image, replica));
  }
  const byDigest = { package: 'library/redis', unit: 'library/redis' };
  const anon = [0, 1, 2, 3].map((i) =>
    item(2700 - i, anonymous, '2001:db8:85a3::8a2e:370:7334', 'upstream/docker-hub', i === 0 ? 'library/redis/_manifests/sha256:{sha}' : 'library/redis/_blobs/sha256:{sha}', 812 + i * 4_096, byDigest),
  );
  const npm = [
    item(1800, sam, '10.12.9.31', 'web-frontend/npm-all', '@acme/ui/-/ui-4.12.0.tgz', 48_213, { package: '@acme/ui', version: '4.12.0', unit: '@acme/ui@4.12.0' }),
    item(1799.8, sam, '10.12.9.31', 'web-frontend/npm-all', 'left-pad/-/left-pad-1.3.0.tgz', 2_181, { package: 'left-pad', version: '1.3.0', unit: 'left-pad@1.3.0' }),
    item(1799.5, sam, '10.12.9.31', 'web-frontend/npm-all', 'react-dom/-/react-dom-19.1.0.tgz', 1_212_455, { package: 'react-dom', version: '19.1.0', unit: 'react-dom@19.1.0' }),
  ];
  const blocked = item(1700, sam, '10.12.9.31', 'web-frontend/npm-all', 'evil-pkg/-/evil-pkg-1.0.0.tgz', 0, null);
  const refused: Json = { ...blocked, action: 'malware.download_blocked', outcome: 'denied', reason: 'infected', details: { ...(blocked['details'] as Json), reason: 'infected' } };
  const release = { package: 'platform/release-tools', unit: 'platform/release-tools' };
  const running = [0, 1, 2, 3, 4, 5].map((i) =>
    item(14 - i * 2, ci, '10.40.0.12', images, i === 0 ? 'platform/release-tools/_tags/2026.11.26' : 'platform/release-tools/_blobs/sha256:{sha}', 4_096 * (i + 1), i === 0 ? { ...release, version: '2026.11.26' } : release),
  );
  const old = [
    item(86_000, priya, '10.12.4.7', 'web-frontend/generic-local', 'releases/2026/11/checkout-service-2.3.0.tar.gz', 0, null),
    event('artifact.deployed', 86_100, sam, '10.12.9.31', images, 'platform/checkout-service/_blobs/sha256:{sha}', 4_096, null, { overwritten: false }),
  ];
  return {
    events: [...push, ...nugetPush, ...versionDelete, ...packageDelete, ...upload, ...pull, ...anon, ...npm, refused, ...running, ...old],
    pull,
  };
}

/**
 * **Admin area › Audit log** with the rows of several events (R-OPS-055, R-OPS-056): the log closed; priya's push opened in place, its
 * first ten items with **All 28 items**; her pull opened; then that link's log of the pull's 32 events, oldest first.
 */
export async function* auditRows(page: Page): AsyncGenerator<string> {
  const { events, pull } = morning();
  const rows = rowsOf(events);
  const isRows = (url: URL): boolean => url.pathname === '/api/v1/audit/rows';
  const isItems = (url: URL): boolean => /^\/api\/v1\/audit\/rows\/[^/]+\/events$/.test(url.pathname);
  await page.route(isRows, (route) =>
    route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ items: rows, nextCursor: null }) }),
  );
  await page.route(isItems, (route) =>
    route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ items: pull, nextCursor: null }) }),
  );
  await page.reload();
  await page.waitForLoadState('networkidle');
  // The controls by their place, not their words: the German pass walks the log too (UI_RENDERS_LOCALE=de).
  const pushToggle = page.locator('main td.target', { hasText: '2.4.0' }).filter({ hasText: 'checkout-service' }).first().locator('button[aria-expanded]');
  const pullToggle = page.locator('main td.target', { hasText: '2.3.1' }).locator('button[aria-expanded]');
  await expect(pullToggle).toBeVisible();
  yield 'rows';
  await pushToggle.click();
  await expect(page.locator('main .download-items a.all-items')).toBeVisible();
  yield 'pushed';
  await pushToggle.click();
  await pullToggle.click();
  await expect(page.locator('main .download-items a.all-items')).toBeVisible();
  yield 'opened';
  await page.locator('main .download-items a.all-items').click();
  await expect(page.locator('main quader-audit-log table.data > tbody > tr:not(.expansion)')).toHaveCount(pull.length);
  await page.waitForLoadState('networkidle');
  yield 'download';
  await page.unroute(isRows);
  await page.unroute(isItems);
}
