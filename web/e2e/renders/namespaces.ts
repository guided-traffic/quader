import { Page, Route, expect } from '@playwright/test';
import { capsUrl, levelsUrl, open, packageUrl, searchUrl, tab } from './upstream';

/*
 * Finding collections as on Galaxy, the page of a name and the documentation searched across collections (W-30, A3: formats/ansible.md
 * R-ANS-047, R-ANS-057, R-ANS-058; api-and-ui.md R-UI-220). The seed holds one collection of the namespace acme and no signature, so the
 * walks answer the lists in the browser with what the server answers for an installation that holds more: a namespace of 40 collections
 * with tags and verified badges, an npm scope of one package with a long name, an image path several levels deep across projects, the
 * facts of who may publish for a reader of the configuration and none for a reader of the packages, the documentation's hits and an
 * Ansible Galaxy remote's catalogue with tags, downloads at the upstream and its signed state. Nothing is written on the server.
 */

function json(route: Route, body: unknown): Promise<void> {
  return route.fulfill({
    status: 200,
    contentType: 'application/json',
    body: JSON.stringify(body),
  });
}

/** The time `days` before now, RFC 3339. */
function ago(days: number): string {
  return new Date(Date.now() - days * 86_400_000).toISOString();
}

interface Summary {
  readonly format: string;
  readonly name: string;
  /** The key when it is not the name: an Ansible role a remote read, `role/<id>` (R-ANS-056). */
  readonly key?: string;
  readonly description?: string | null;
  readonly keywords?: readonly string[];
  readonly signature?: 'verified_internal' | 'verified_public';
  /** The deprecation of the latest version, a collection's as a whole (R-API-085, R-ANS-060). */
  readonly deprecated?: boolean;
  readonly repositories?: readonly string[];
  readonly latest?: string;
}

function summary(s: Summary, i: number): Record<string, unknown> {
  return {
    id: `pkg_${s.name.replace(/[^a-z0-9]/gi, '').toLowerCase()}`,
    format: s.format,
    name: s.name,
    ...(s.key ? { key: s.key } : {}),
    description: s.description === undefined ? null : s.description,
    latestVersion: s.latest ?? `${1 + (i % 4)}.${i % 7}.${i % 3}`,
    keywords: s.keywords ?? [],
    repositories: s.repositories ?? ['platform/collections'],
    createdAt: ago(200 + i),
    updatedAt: ago(1 + i * 2),
    versionCount: 1 + (i % 9),
    downloadCount: i * 37,
    ...(s.signature ? { signature: s.signature } : {}),
    ...(s.deprecated ? { deprecation: { scope: 'package', message: 'No longer maintained.' } } : {}),
  };
}

/** Answers the package list (not a package, its lookup or a namespace's facts) with the rows, in pages of the limit asked. */
async function answerList(page: Page, rows: readonly Summary[]): Promise<void> {
  await page.route(
    (url) => url.pathname === '/api/v1/packages',
    (route) => {
      const url = new URL(route.request().url());
      const limit = Number(url.searchParams.get('limit') ?? 50);
      const start = Number(url.searchParams.get('cursor') ?? 0);
      const items = rows.slice(start, start + limit).map((s, i) => summary(s, start + i));
      return json(route, {
        items,
        nextCursor: start + limit < rows.length ? String(start + limit) : null,
      });
    },
  );
}

/** The collections of the namespace acme: forty, with tags, a few verified, some long names and descriptions. */
const acmeCollections: readonly Summary[] = Array.from({ length: 40 }, (_, i) => {
  const names = [
    'platform',
    'network',
    'firewall',
    'kubernetes_operators_for_the_payment_and_settlement_platform',
    'monitoring',
    'storage',
    'identity',
    'backup',
  ];
  const base = names[i % names.length]!;
  return {
    format: 'ansible',
    name: `acme.${base}${i < names.length ? '' : `_${Math.floor(i / names.length)}`}`,
    description:
      i % 5 === 3
        ? 'Roles and modules that install, configure and upgrade every agent the platform team runs on the hosts of the payment network, with the checks the auditors ask for.'
        : i % 4 === 1
          ? null
          : `Modules for ${base.replace(/_/g, ' ')} of the acme hosts.`,
    keywords:
      i % 3 === 0
        ? ['linux', 'acme', 'infrastructure', 'networking', 'security', 'compliance', 'automation']
        : i % 3 === 1
          ? ['linux']
          : [],
    signature: i % 6 === 0 ? 'verified_internal' : i % 6 === 2 ? 'verified_public' : undefined,
    deprecated: i % 8 === 3,
  };
});

/** What decides a publish into acme, as the server answers a reader of the configuration of two repositories. */
function acmePublishing(): Record<string, unknown> {
  return {
    format: 'ansible',
    name: 'acme',
    kind: 'namespace',
    publishing: {
      repositories: [
        {
          project: 'platform',
          key: 'collections',
          packageOwnership: 'enforce',
          autoClaim: true,
          claims: [
            { pattern: 'acme.*', scope: 'members', source: 'manual', covers: 'all' },
            {
              pattern: 'acme.kubernetes_operators_for_the_payment_and_settlement_platform',
              scope: 'global',
              source: 'auto',
              covers: 'some',
            },
          ],
        },
        {
          project: 'platform',
          key: 'collections-sandbox',
          packageOwnership: 'off',
          autoClaim: true,
          claims: [],
        },
      ],
    },
  };
}

async function settledList(page: Page): Promise<void> {
  await expect(page.locator('main quader-data-table tbody tr').first()).toBeVisible();
  await page.waitForLoadState('networkidle');
}

/** The package list filtered by a namespace: its chip, the hits with their tags and badges. */
export async function* namespaceList(page: Page): AsyncGenerator<string> {
  await answerList(page, acmeCollections.slice(0, 12));
  await page.reload();
  await settledList(page);
  yield '';
}

/**
 * The package list searched by a word, best match first (R-ANS-057): collections whose name, tags or description hold it and a role a
 * remote read, whose display name `<owner>.<name>` is one link to its key and names no namespace (R-ANS-056, R-UI-220).
 */
export async function* ansibleFind(page: Page): AsyncGenerator<string> {
  await answerList(page, [
    { format: 'ansible', name: 'acme.pip', description: 'Manages pip on the acme hosts.', keywords: ['python', 'pip'], signature: 'verified_internal' },
    {
      format: 'ansible',
      name: 'geerlingguy.pip',
      key: 'role/10342',
      description: 'Pip (Python package manager) for Linux.',
      keywords: ['python', 'pip', 'development'],
      repositories: ['platform/galaxy'],
    },
    { format: 'ansible', name: 'community.general', description: 'Modules and plugins, among them pip and pipx.', keywords: ['general'] },
    { format: 'ansible', name: 'acme.python', description: 'Python runtimes with pip and venv.', keywords: ['python', 'linux'], deprecated: true },
  ]);
  await page.reload();
  await settledList(page);
  yield '';
}

/** The page of acme as a reader of the configuration: forty collections, then who may publish. */
export async function* namespaceReaderOfConfiguration(page: Page): AsyncGenerator<string> {
  await answerList(page, acmeCollections);
  await page.route(
    (url) => url.pathname === '/api/v1/packages/namespace',
    (route) => json(route, acmePublishing()),
  );
  await page.reload();
  await settledList(page);
  await expect(page.locator('main [data-testid="namespace-publishing"]')).toBeVisible();
  yield '';
}

/** The page of acme as a reader of the packages: the packages alone. */
export async function* namespaceReaderOfPackages(page: Page): AsyncGenerator<string> {
  await answerList(page, acmeCollections.slice(0, 8));
  await page.route(
    (url) => url.pathname === '/api/v1/packages/namespace',
    (route) =>
      json(route, { format: 'ansible', name: 'acme', kind: 'namespace', publishing: null }),
  );
  await page.reload();
  await settledList(page);
  yield '';
}

/** A long npm scope with one package. */
export const longScope = '@customer-portal-release-engineering-and-design-system';

export async function* namespaceOnePackage(page: Page): AsyncGenerator<string> {
  await answerList(page, [
    {
      format: 'npm',
      name: `${longScope}/merge-request-pipeline-status-reporter-for-the-frontend-monorepo`,
      description: 'Reports the state of every merge request pipeline of the frontend monorepo.',
      keywords: ['gitlab', 'ci', 'release'],
      signature: 'verified_public',
      repositories: ['web-frontend/npm-internal'],
    },
  ]);
  await page.reload();
  await settledList(page);
  yield '';
}

/** A path several levels deep across projects: images below it at every depth. */
export const deepImagePath = 'platform-team/services/payments/settlement';

export async function* namespaceDeepPath(page: Page): AsyncGenerator<string> {
  await answerList(page, [
    {
      format: 'oci',
      name: `${deepImagePath}/api`,
      repositories: ['platform/images'],
      latest: 'v2.14.3',
    },
    {
      format: 'oci',
      name: `${deepImagePath}/batch-reconciliation-worker-for-the-end-of-day-ledger`,
      repositories: ['platform/images'],
      latest: 'v2.14.3',
      signature: 'verified_internal',
    },
    {
      format: 'oci',
      name: `${deepImagePath}/workers/ledger-export`,
      repositories: ['web-frontend/docker-local'],
      latest: '2026.11.25-1',
    },
    {
      format: 'oci',
      name: `${deepImagePath}/workers/statement-renderer/pdf`,
      repositories: ['web-frontend/docker-local'],
      latest: 'sha-5ccfd1a',
    },
  ]);
  await page.reload();
  await settledList(page);
  yield '';
}

/** The documentation's hits of "firewall" across collections. */
export async function* documentationHits(page: Page): AsyncGenerator<string> {
  const hit = (
    pkg: string,
    version: string,
    type: string,
    name: string,
    shortDescription: string,
    project = 'platform',
    repository = 'collections',
  ) => ({
    format: 'ansible',
    packageId: `pkg_${pkg.replace(/[^a-z]/g, '')}`,
    package: pkg,
    version,
    project,
    repository,
    type,
    name,
    shortDescription,
    ...(pkg.startsWith('acme.kubernetes_') ? { deprecation: { scope: 'package', message: 'Replaced by acme.platform.' } } : {}),
  });
  await page.route(
    (url) => url.pathname === '/api/v1/search/documentation',
    (route) =>
      new URL(route.request().url()).searchParams.get('cursor')
        ? json(route, {
            items: [
              hit(
                'acme.network',
                '2.0.1',
                'module',
                'firewall_zone_sync',
                'Keeps the zones of the firewall in step',
              ),
            ],
            nextCursor: null,
          })
        : json(route, {
            items: [
              hit(
                'acme.firewall',
                '3.2.0',
                'module',
                'firewall',
                'Manage the rules of the host firewall',
              ),
              hit(
                'acme.firewall',
                '3.2.0',
                'module',
                'firewall_rule',
                'Add, change or remove one rule of the host firewall, with its zone, its ports and its sources',
              ),
              hit(
                'ansible.posix',
                '2.1.0',
                'module',
                'firewalld',
                'Manage arbitrary ports and services with firewalld',
                'upstream',
                'galaxy',
              ),
              hit(
                'acme.kubernetes_operators_for_the_payment_and_settlement_platform',
                '1.0.0-rc.2',
                'role',
                'firewall_hardening_for_the_settlement_nodes',
                'Hardens the firewall of a settlement node',
              ),
              hit(
                'community.general',
                '10.1.0',
                'lookup',
                'firewall_zones',
                '',
                'upstream',
                'galaxy',
              ),
              hit(
                'acme.network',
                '2.0.1',
                'filter',
                'firewall_ports',
                'The ports a rule opens, as a list',
              ),
            ],
            nextCursor: 'more',
          }),
  );
  await page.reload();
  await settledList(page);
  yield '';
}

// ---- The tab Upstream of an Ansible Galaxy remote ----

const delivered = { delivered: true, decidedBy: { mode: 'allExceptBlocked' } };

function galaxyHit(name: string, over: Record<string, unknown> = {}): Record<string, unknown> {
  return {
    name,
    description: '',
    latest: '1.0.0',
    updated: ago(3),
    flags: [],
    delivery: delivered,
    stored: 'none',
    request: null,
    level: delivered,
    inherited: delivered,
    versionRules: { allow: 0, block: 0 },
    ...over,
  };
}

/** The hits of "docker" on the public Galaxy: its tags and signed state; its keyword search answers no download count. */
function galaxyHits(): Record<string, unknown>[] {
  return [
    galaxyHit('community.docker', {
      description: 'Modules and plugins for working with Docker',
      latest: '5.4.1',
      keywords: ['docker'],
      signed: false,
      stored: 'partial',
    }),
    galaxyHit('nietaki.openhands', {
      description: 'Deploy OpenHands with docker compose on a host',
      latest: '0.4.0',
      keywords: ['docker', 'ai', 'agents', 'compose', 'llm', 'openhands', 'automation'],
      signed: false,
    }),
    galaxyHit('middleware_automation.keycloak', {
      description: 'Install and configure a keycloak, or Red Hat Single Sign-on, service.',
      latest: '3.1.3',
      keywords: [
        'keycloak',
        'redhat',
        'rhel',
        'sso',
        'openid',
        'application',
        'identity',
        'security',
        'infrastructure',
        'authentication',
        'java',
        'runtimes',
        'middleware',
        'a4mw',
      ],
      signed: true,
    }),
    galaxyHit('jcook3701.docker', { description: '', latest: '0.1.2', flags: ['deprecated'] }),
  ];
}

function galaxyVersions(): Record<string, unknown>[] {
  return Array.from({ length: 8 }, (_, i) => ({
    version: `5.${4 - Math.floor(i / 3)}.${2 - (i % 3)}`,
    published: ago(10 + i * 21),
    size: null,
    unpackedSize: null,
    flags: [],
    platforms: ['ansible >=2.17.0'],
    digest: null,
    delivery: delivered,
    stored: i === 0 ? 'whole' : 'none',
    queued: null,
    request: null,
    inherited: delivered,
  }));
}

async function answerGalaxy(page: Page): Promise<void> {
  await page.unrouteAll({ behavior: 'ignoreErrors' });
  await page.route(capsUrl, (r) =>
    json(r, {
      search: 'upstream',
      searchWindow: 0,
      searchHint: 'galaxy',
      levels: ['namespace'],
      levelsReason: null,
      sizes: 'perVersion',
      hosts: [],
      mode: 'allExceptBlocked',
      rules: 0,
      openRequests: 0,
      queue: { waiting: 0, running: 0, failed: 0, done: 12 },
      maySetRules: true,
    }),
  );
  await page.route(searchUrl, (r) =>
    json(r, {
      hits: galaxyHits(),
      total: 289,
      nextCursor: 'bW9yZQ',
      done: false,
      truncated: false,
      note: null,
      withheld: null,
      withheldNote: null,
    }),
  );
  await page.route(levelsUrl, (r) =>
    json(r, {
      levels: [],
      packages: [
        galaxyHit('community.docker', {
          description: 'Modules and plugins for working with Docker',
          latest: '5.4.1',
          downloads: 70_357_068,
        }),
        galaxyHit('community.general', {
          description: 'Modules and plugins supported by the Ansible community',
          latest: '12.1.0',
          downloads: 159_215_408,
        }),
        galaxyHit('community.postgresql', {
          description: 'PostgreSQL modules and plugins',
          latest: '4.1.0',
          downloads: 0,
        }),
      ],
      total: 3,
      nextCursor: null,
      done: true,
      withheld: null,
      withheldNote: null,
      unknown: null,
    }),
  );
  await page.route(packageUrl, (r) =>
    json(r, {
      name: 'community.docker',
      description: 'Modules and plugins for working with Docker',
      tags: {},
      keywords: ['docker', 'containers', 'compose', 'swarm'],
      downloads: 70_357_068,
      signed: false,
      delivery: delivered,
      stored: 'partial',
      request: null,
      level: delivered,
      inherited: delivered,
      versionRules: { allow: 0, block: 0 },
      versions: galaxyVersions(),
      total: 162,
      nextCursor: 'bW9yZQ',
      done: false,
    }),
  );
}

/** The search of an Ansible Galaxy remote with its hits' tags and signed state, a namespace's listing with the downloads at the upstream, and a collection opened. */
export async function* galaxyUpstream(page: Page): AsyncGenerator<string> {
  await answerGalaxy(page);
  await open(page, '?q=docker');
  await expect(page.locator(`${tab} tbody tr:not(.expansion)`)).toHaveCount(4);
  yield 'search';
  await page.locator(`${tab} button[aria-controls="up-community_docker"]`).click();
  await expect(page.locator(`${tab} quader-upstream-package tbody tr`).first()).toBeVisible();
  yield 'package';
  await open(page, '?level=community');
  await expect(page.locator(`${tab} tbody tr:not(.expansion)`)).toHaveCount(3);
  yield 'namespace';
  await open(page, '?name=community.docker');
  await expect(page.locator(`${tab} [data-testid="upstream-popularity"]`).first()).toBeVisible();
  yield 'lookup';
}
