import { execFileSync } from 'node:child_process';
import { createHash } from 'node:crypto';
import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import { gzipSync } from 'node:zlib';
import { APIRequestContext, APIResponse } from '@playwright/test';
import {
  advisoryArchive,
  advisoryMirror,
  fetchedVersions,
  gradedPackage,
  manyVersionPackages,
  manyVersionsPackage,
  vendorPackages,
  zipArchive,
} from './advisories';
import { stateDir } from './paths';

/**
 * The data of `make ui-renders` (docs/developer/web-ui.md#renders-of-every-screen): four projects (one with a long name and key), repositories of every
 * format and class with short and long keys, a few published packages and files, the three kinds of user of R-UI-067, an
 * auditor (R-UI-158) and an empty project and repository for the empty states. Everything is created through Quader's public API and is
 * idempotent: a second run finds what the first made and adds nothing.
 */

/**
 * The kinds of user the renders sign in as (R-UI-067), and the usernames of the profile's sign-in provider (test/devidp). The
 * auditor holds the system role `auditor` and is a member of no project: the security dashboard's user (R-UI-158).
 */
export const kinds = { plain: 'sam', power: 'priya', admin: 'ada', auditor: 'noor' } as const;
export type Kind = keyof typeof kinds | 'anonymous';

export interface RepoSeed {
  readonly project: string;
  readonly key: string;
  readonly format: 'npm' | 'nuget' | 'generic' | 'oci' | 'deb' | 'ansible';
  readonly class: 'local' | 'remote' | 'virtual';
  readonly description?: string;
  /** The members of a virtual repository, the first local one its deploy target. */
  readonly members?: readonly string[];
  /** The upstream of a remote repository whose format has no well-known one. */
  readonly url?: string;
  /** The names the repository admits (R-REPO-005); absent admits every name. */
  readonly includePatterns?: readonly string[];
  /** The member `vulnerabilities` of the repository document (R-VULN-002). */
  readonly vulnerabilities?: { readonly matchPublicAdvisories: 'auto' | 'always' | 'never' };
  /** The levels of the artifacts and of the wiki (R-AUTHZ-033); absent takes the lower of the installation's default and the project's level. */
  readonly visibility?: { readonly artifacts: 'private' | 'internal' | 'public'; readonly wiki: 'private' | 'internal' | 'public' };
}

export const projects = [
  { key: 'web-frontend', visibility: 'internal', displayName: 'Web frontend', description: "The customer portal's packages, container images and static assets." },
  { key: 'platform', visibility: 'public', displayName: 'Platform engineering', description: 'Base images, Debian packages, NuGet libraries and automation for every team.' },
  // Empty: the empty states of a project.
  { key: 'sandbox', visibility: 'private', displayName: 'Sandbox', description: '' },
  // A long name and a long key: what they do to a list of projects and to a row that names its project (Q-UI-424, Q-UI-425).
  {
    key: 'customer-portal-release-delivery',
    visibility: 'internal',
    displayName: 'Customer portal release engineering and delivery',
    description: 'Release pipelines, signing keys and the delivery automation of the customer portal.',
  },
] as const;

/** The local repository a remote of the renders caches from, and the remote (W-24). */
export const remoteSource = { key: 'image-source', prefix: 'vendor' } as const;
export const remoteCache = { key: 'image-cache' } as const;
/**
 * The image of two platforms in image-source and what the seed pulls of it through image-cache: `1.9` is one platform's image
 * (its size is known from its manifest), `2.0` the index with one platform pulled (a partial size), `2.1` the index alone (a
 * size that is not known).
 */
export const remoteImage = { name: 'tools/agent', single: '1.9', partial: '2.0', unknown: '2.1' } as const;

/** The project the power user owns, and in which the screens of a project are rendered. */
export const mainProject = 'web-frontend';
export const otherProject = 'platform';
export const emptyProject = 'sandbox';
/** The project with a long name and a long key. */
export const longProject = 'customer-portal-release-delivery';
/** A key long enough to show what a long key and its address do to a list (R-UI-065). */
export const longNpmKey = 'release-candidates-for-the-customer-portal-frontend';
export const longDebKey = 'debian-internal-packages-for-bookworm-and-trixie';

export const repositories: readonly RepoSeed[] = [
  { project: mainProject, key: 'npm-internal', format: 'npm', class: 'local', description: 'Packages built by the web team.', visibility: { artifacts: 'internal', wiki: 'internal' } },
  { project: mainProject, key: 'npmjs', format: 'npm', class: 'remote', description: 'The public npm registry, cached.' },
  { project: mainProject, key: 'npm', format: 'npm', class: 'virtual', members: ['npm-internal', 'npmjs'] },
  {
    project: mainProject,
    key: longNpmKey,
    format: 'npm',
    class: 'local',
    description:
      'Release candidates of the customer portal before they are promoted; the release pipeline removes candidates older than four weeks every Monday.',
  },
  { project: mainProject, key: 'assets', format: 'generic', class: 'local', description: "Static files: fonts, icons and the design system's release archives.", visibility: { artifacts: 'internal', wiki: 'internal' } },
  { project: mainProject, key: 'images', format: 'oci', class: 'local' },
  // Vendored public packages, matched against the seed's advisories so that every grade occurs (advisories.ts, R-VULN-038).
  {
    project: mainProject,
    key: 'npm-vendor',
    format: 'npm',
    class: 'local',
    description: 'Public packages the web team vendors, checked against public advisories.',
    vulnerabilities: { matchPublicAdvisories: 'always' },
  },
  // Empty: the empty states of a repository.
  { project: mainProject, key: 'scratch', format: 'generic', class: 'local' },
  { project: otherProject, key: 'base-images', format: 'oci', class: 'local', description: 'Hardened base images.' },
  { project: otherProject, key: 'docker-hub-remote', format: 'oci', class: 'remote', description: 'Docker Hub, cached.' },
  { project: otherProject, key: 'containers', format: 'oci', class: 'virtual', members: ['base-images', 'docker-hub-remote'] },
  // A remote that a visitor without a session pulls through (owner, 2026-11-19): public, and bound to one namespace of the
  // upstream, because a request without a credential fills its cache.
  {
    project: otherProject,
    key: 'guidedtraffic',
    format: 'oci',
    class: 'remote',
    description: 'The guidedtraffic images of Docker Hub, cached. Open to everyone, also without signing in.',
    visibility: { artifacts: 'public', wiki: 'internal' },
    includePatterns: ['guidedtraffic/*'],
  },
  // A remote whose upstream is this installation (W-24): what a remote lists, without the internet. image-source holds an image of
  // two platforms; the seed pulls through image-cache one tag whole, one platform of another and only the index of a third, so that
  // its rows show a known size, a size of the platforms fetched so far and a size that is not known (R-API-080, Q-OCI-221).
  { project: otherProject, key: remoteSource.key, format: 'oci', class: 'local', description: 'The upstream of image-cache.', visibility: { artifacts: 'public', wiki: 'internal' } },
  {
    project: otherProject,
    key: remoteCache.key,
    format: 'oci',
    class: 'remote',
    description: 'The images of image-source, cached through its registry address.',
    // The upstream's namespace prefix: image-cache's tools/agent is image-source's vendor/tools/agent, so that the package page of
    // tools/agent shows the remote's versions alone.
    url: `https://localhost:4200/${otherProject}/${remoteSource.key}/${remoteSource.prefix}`,
  },
  { project: otherProject, key: longDebKey, format: 'deb', class: 'local', description: 'Packages of the platform team for Debian 12 and 13.' },
  { project: otherProject, key: 'debian', format: 'deb', class: 'remote', description: 'deb.debian.org, cached.' },
  { project: otherProject, key: 'apt', format: 'deb', class: 'virtual', members: [longDebKey] },
  { project: otherProject, key: 'nuget-internal', format: 'nuget', class: 'local', description: 'Shared .NET libraries.' },
  // The advisory data of the profile, which reads it instead of the network (R-VULN-005, deploy/ui-dev/up.sh).
  { project: advisoryMirror.project, key: advisoryMirror.key, format: 'generic', class: 'local', description: 'Advisory data for the offline sync.' },
  { project: otherProject, key: 'nuget-org', format: 'nuget', class: 'remote' },
  { project: otherProject, key: 'nuget', format: 'nuget', class: 'virtual', members: ['nuget-internal', 'nuget-org'] },
  { project: otherProject, key: 'collections', format: 'ansible', class: 'local', description: 'Ansible collections of the platform team.', visibility: { artifacts: 'public', wiki: 'internal' } },
  { project: otherProject, key: 'galaxy', format: 'ansible', class: 'remote' },
  { project: otherProject, key: 'ansible', format: 'ansible', class: 'virtual', members: ['collections', 'galaxy'] },
  { project: longProject, key: 'signed-release-artifacts', format: 'generic', class: 'local', description: 'Signed release archives of the customer portal.' },
];

/** Who is what: the project roles of R-AUTHZ-026 and the system role `admin` (R-AUTHZ-010); see R-UI-067. */
export const memberships: readonly { project: string; username: string; role: 'reader' | 'developer' | 'maintainer' | 'owner' }[] = [
  { project: mainProject, username: kinds.plain, role: 'reader' },
  { project: otherProject, username: kinds.plain, role: 'reader' },
  { project: mainProject, username: kinds.power, role: 'owner' },
  { project: otherProject, username: kinds.power, role: 'developer' },
];

/** The packages the power user publishes, through the formats' own routes. */
export const npmPackages: readonly { repo: string; name: string; version: string; readme?: string; dependencies?: Record<string, string>; tag?: string }[] = [
  {
    repo: 'npm-internal',
    name: '@acme/ui-kit',
    version: '1.0.0',
    readme: '# @acme/ui-kit\n\nThe components of the customer portal.\n',
  },
  {
    repo: 'npm-internal',
    name: '@acme/ui-kit',
    version: '1.1.0',
    readme: '# @acme/ui-kit\n\nThe components of the customer portal.\n\n## Install\n\n```shell\nnpm install @acme/ui-kit\n```\n\n## Use\n\nImport a component and give it the theme of the page.\n',
    dependencies: { '@acme/tokens': '^2.0.0', tslib: '^2.8.0' },
  },
  { repo: 'npm-internal', name: '@acme/eslint-config', version: '3.2.1' },
  { repo: 'npm-internal', name: 'customer-portal-api-client-generated-from-the-openapi-document', version: '0.3.0' },
  { repo: 'npm-internal', name: 'customer-portal-api-client-generated-from-the-openapi-document', version: '0.4.0' },
  { repo: 'npm-internal', name: 'customer-portal-api-client-generated-from-the-openapi-document', version: '0.5.0-next.20261118.1' },
  { repo: longNpmKey, name: '@acme/portal', version: '5.0.0-rc.3' },
];

/** The deep folder of the generic files, and the number of entries of the folder with many (more than a page of 200). */
export const deepFolder =
  'deliveries/customer-portal-frontend-static-assets-for-the-november-release/build-artifacts-of-the-nightly-pipeline/2026-11-18-nightly-build-4711';
export const manyIcons = 230;

export const genericFiles: readonly { repo: string; path: string; content: string }[] = [
  { repo: 'assets', path: 'README.md', content: '# Assets\n\nStatic files of the customer portal.\n' },
  { repo: 'assets', path: 'fonts/inter/Inter-Regular.woff2', content: 'font bytes' },
  { repo: 'assets', path: 'icons/sprite.svg', content: '<svg xmlns="http://www.w3.org/2000/svg"></svg>\n' },
  { repo: 'assets', path: 'releases/design-system/2.4.0/design-system-2.4.0.tar.gz', content: 'archive 2.4.0' },
  { repo: 'assets', path: 'releases/design-system/2.5.0/design-system-2.5.0.tar.gz', content: 'archive 2.5.0' },
  {
    repo: 'assets',
    path: 'releases/design-system/2.5.0/design-system-2.5.0-with-a-file-name-long-enough-to-wrap-a-narrow-column.tar.gz',
    content: 'archive 2.5.0 long',
  },
  // A folder several levels deep whose names are long: a path that has to wrap in the trail and the breadcrumb (R-UI-010).
  { repo: 'assets', path: `${deepFolder}/sprite-with-every-icon-of-the-design-system-for-the-customer-portal.svg`, content: '<svg/>\n' },
  // A folder with more entries than one page of the list holds (200), so that it shows Load more (Q-UI-513).
  ...Array.from({ length: manyIcons }, (_, i) => ({
    repo: 'assets',
    path: `icons/material/${String(i + 1).padStart(3, '0')}-icon.svg`,
    content: `<svg id="${i + 1}"/>\n`,
  })),
];

/** Folders created empty: an empty folder's state (R-UI-010). */
export const genericFolders: readonly { repo: string; path: string }[] = [{ repo: 'assets', path: 'drafts' }];

export const images: readonly { project: string; repo: string; name: string; tags: readonly string[] }[] = [
  // Several tags of one image: the page of an image lists its tags, the tag leading (W-7).
  { project: mainProject, repo: 'images', name: 'portal/web', tags: ['1.2.0', '1.3.0', '1.3.1', '1.4.0', 'latest', 'stable'] },
  { project: mainProject, repo: 'images', name: 'portal/api', tags: ['1.4.0'] },
];

/** The first package of the main project: the package page the renders open, with every tab. */
export const npmPackage = { format: 'npm', name: '@acme/ui-kit' } as const;
export const ociPackage = { format: 'oci', name: 'portal/web' } as const;
/** The npm package with the longest name, three versions. */
export const longNamePackage = { format: 'npm', name: 'customer-portal-api-client-generated-from-the-openapi-document' } as const;

/** A NuGet package of the platform project (W-7): three versions, a pre-release among them, with dependency groups. */
export const nugetPackage = {
  format: 'nuget',
  repo: 'nuget-internal',
  name: 'Acme.Logging.Abstractions',
  versions: ['1.0.0', '1.1.0', '2.0.0-preview.1'],
} as const;
/** A Debian package of the platform project (W-7): two versions in the suite stable. */
export const debPackage = { format: 'deb', repo: longDebKey, name: 'acme-agent', versions: ['1.2.0-1', '1.3.0-1'] } as const;
/** An Ansible collection of the platform project (W-7): two versions. */
export const ansiblePackage = { format: 'ansible', repo: 'collections', namespace: 'acme', name: 'platform', versions: ['1.0.0', '1.1.0'] } as const;

/** A thin client of the API with the cookie session of the caller, its CSRF token and the UI's origin (R-AUTH-012). */
export class Api {
  constructor(
    readonly ctx: APIRequestContext,
    private readonly csrf: string,
    private readonly origin: string,
  ) {}

  get csrfToken(): string {
    return this.csrf;
  }
  get originUrl(): string {
    return this.origin;
  }

  static async of(ctx: APIRequestContext, origin: string): Promise<Api> {
    const csrf = (await ctx.storageState()).cookies.find((c) => c.name === '__Host-qdr_csrf')?.value ?? '';
    return new Api(ctx, csrf, origin);
  }

  private headers(extra: Record<string, string> = {}): Record<string, string> {
    return { 'X-CSRF-Token': this.csrf, Origin: this.origin, ...extra };
  }

  get(url: string): Promise<APIResponse> {
    return this.ctx.get(url);
  }
  post(url: string, data: unknown, extra?: Record<string, string>): Promise<APIResponse> {
    return this.ctx.post(url, { data, headers: this.headers(extra) });
  }
  put(url: string, data: unknown, extra?: Record<string, string>): Promise<APIResponse> {
    return this.ctx.put(url, { data, headers: this.headers(extra) });
  }
  patch(url: string, data: unknown, extra?: Record<string, string>): Promise<APIResponse> {
    return this.ctx.patch(url, { data, headers: this.headers({ 'Content-Type': 'application/merge-patch+json', ...extra }) });
  }
  delete(url: string): Promise<APIResponse> {
    return this.ctx.delete(url, { headers: this.headers() });
  }
}

/**
 * Sends a request again after the `Retry-After` of a 429, up to ten times: the seed publishes some sixty versions and uploads a
 * folder of many entries in a row, more than the rate limit of one principal lets through at once (R-API-016, R-API-052).
 */
export async function patient(send: () => Promise<APIResponse>): Promise<APIResponse> {
  for (let i = 0; ; i++) {
    const res = await send();
    if (res.status() !== 429 || i === 10) return res;
    const wait = Number(res.headers()['retry-after'] ?? '1');
    await new Promise((r) => setTimeout(r, (Number.isFinite(wait) && wait > 0 ? Math.min(wait, 30) : 1) * 1000));
  }
}

/** Fails with the request and the answer unless the status is one of `ok`. */
export async function expectStatus(res: APIResponse, ok: readonly number[], what: string): Promise<void> {
  if (!ok.includes(res.status())) throw new Error(`${what}: ${res.status()} ${(await res.text()).slice(0, 600)}`);
}

/** The projects and their repositories, created by the break-glass account; an existing one is left as it is. */
export async function seedProjectsAndRepositories(admin: Api): Promise<void> {
  for (const p of projects) {
    const res = await admin.post('/api/v1/projects', { key: p.key, displayName: p.displayName, visibility: p.visibility, ...(p.description ? { description: p.description } : {}) });
    await expectStatus(res, [201, 409], `create project ${p.key}`);
  }
  // Locals first: a virtual repository names its members.
  const order = [...repositories].sort((a, b) => rank(a.class) - rank(b.class));
  for (const r of order) {
    const body: Record<string, unknown> = { key: r.key, class: r.class, format: r.format };
    if (r.description) body['description'] = r.description;
    if (r.visibility) body['visibility'] = r.visibility;
    if (r.includePatterns) body['includePatterns'] = r.includePatterns;
    if (r.class === 'remote' && r.url) body['remote'] = { url: r.url };
    if (r.vulnerabilities) body['vulnerabilities'] = r.vulnerabilities;
    if (r.class === 'virtual') {
      const target = r.members?.find((m) => repositories.find((x) => x.key === m)?.class === 'local');
      body['virtual'] = { members: r.members ?? [], deployTarget: target ?? null };
    }
    const res = await admin.post(`/api/v1/projects/${r.project}/repositories`, body);
    await expectStatus(res, [201, 409], `create repository ${r.project}/${r.key}`);
  }
}

function rank(c: RepoSeed['class']): number {
  return c === 'local' ? 0 : c === 'remote' ? 1 : 2;
}

/** The memberships and the administrator's role; the users exist from their first sign-in (R-AUTH-007). */
export async function seedGrants(admin: Api): Promise<void> {
  for (const m of memberships) {
    const res = await admin.put(`/api/v1/projects/${m.project}/members/${encodeURIComponent(`user:${m.username}`)}`, { role: m.role });
    await expectStatus(res, [200, 201], `member ${m.username} of ${m.project}`);
  }
  const bound = await admin.put(`/api/v1/role-bindings/admin/${encodeURIComponent(`user:${kinds.admin}`)}`, {});
  await expectStatus(bound, [200, 201], `role admin of ${kinds.admin}`);
  const auditor = await admin.put(`/api/v1/role-bindings/auditor/${encodeURIComponent(`user:${kinds.auditor}`)}`, {});
  await expectStatus(auditor, [200, 201], `role auditor of ${kinds.auditor}`);

  // A group and service accounts, so that the lists and pages of identities have content.
  await expectStatus(await admin.post('/api/v1/groups', { name: group, description: 'Everyone who builds the customer portal.' }), [201, 409], `group ${group}`);
  const members = await admin.ctx.patch(`/api/v1/groups/${group}/members`, {
    data: { add: [kinds.plain, kinds.power] },
    headers: { 'X-CSRF-Token': admin.csrfToken, Origin: admin.originUrl },
  });
  await expectStatus(members, [200], `members of ${group}`);
  await expectStatus(
    await admin.post('/api/v1/service-accounts', { name: serviceAccount, description: 'Reads the backups for the restore drill.' }),
    [201, 409],
    `service account ${serviceAccount}`,
  );
  await expectStatus(
    await admin.post(`/api/v1/projects/${mainProject}/service-accounts`, { name: projectServiceAccount, description: 'Publishes from the release pipeline.' }),
    [201, 409],
    `service account ${mainProject}/${projectServiceAccount}`,
  );
  await seedTechnicalTokens(admin);
  // A paused exposure rule, so that the rule editor renders a rule that exists, with its Advanced section; paused, it exposes and
  // hides nothing on the default endpoint.
  await expectStatus(
    await admin.post(
      '/api/v1/endpoints/default/exposure-rules',
      { name: exposureRule, description: 'Released files for customers.', repository: `${mainProject}/assets`, enabled: false },
      { 'Idempotency-Key': `renders-${exposureRule}` },
    ),
    [201, 409],
    `exposure rule ${exposureRule}`,
  );
}

/**
 * The technical tokens of the accounts on the three levels and a repository's account with a long name (R-AUTH-045, R-AUTH-046,
 * R-UI-181): an active token, one with a long name, one whose secret lives a minute (expired by the time the account's page is
 * rendered; a later run finds it expired) and one revoked. A second run finds them (409) and leaves them.
 */
async function seedTechnicalTokens(admin: Api): Promise<void> {
  const repoAccounts = `/api/v1/projects/${mainProject}/repositories/npm-internal/service-accounts`;
  await expectStatus(
    await patient(() =>
      admin.post(repoAccounts, {
        name: repositoryServiceAccount,
        description: 'Publishes the frontend packages from the release pipeline of the customer portal.',
        actions: ['read', 'deploy'],
      }),
    ),
    [201, 409],
    `service account ${mainProject}/npm-internal/${repositoryServiceAccount}`,
  );
  const accounts = [
    `/api/v1/service-accounts/${serviceAccount}`,
    `/api/v1/projects/${mainProject}/service-accounts/${projectServiceAccount}`,
    `${repoAccounts}/${repositoryServiceAccount}`,
  ];
  for (const base of accounts) {
    // Only what is missing is written: the seed's writes share the administrator's write budget (R-API-016).
    const listed = await patient(() => admin.get(`${base}/technical-tokens`));
    await expectStatus(listed, [200], `technical tokens of ${base}`);
    const have = new Set(((await listed.json()) as { items: { name: string }[] }).items.map((t) => t.name));
    for (const t of [
      { name: 'release', description: 'The release job.', ttlSeconds: 90 * 86_400 },
      { name: longTechnicalToken, description: 'The nightly mirror of the disaster-recovery site.', ttlSeconds: 30 * 86_400 },
      { name: 'expiry-check', description: '', ttlSeconds: 60 },
      { name: 'retired-scanner', description: 'The scanner that was replaced.', ttlSeconds: 30 * 86_400 },
    ]) {
      if (have.has(t.name)) continue;
      const res = await patient(() => admin.post(`${base}/technical-tokens`, { ...t, scope: { inherit: true } }));
      await expectStatus(res, [201, 409], `technical token ${t.name} of ${base}`);
      if (res.status() === 201 && t.name === 'retired-scanner') {
        const secret = ((await res.json()) as { secretId: string }).secretId;
        await expectStatus(await patient(() => admin.delete(`/api/v1/tokens/${secret}`)), [204], `revoke ${t.name} of ${base}`);
      }
    }
  }
}

export const group = 'web-team';
export const serviceAccount = 'backup-reader';
export const projectServiceAccount = 'release-pipeline';
/** A repository's service account with a long name (R-AUTH-046): the rows and the heading that hold it are looked at. */
export const repositoryServiceAccount = 'frontend-release-pipeline-of-the-customer-portal-team';
/** A technical token with a long name (R-AUTH-045, at most 63 characters). */
export const longTechnicalToken = 'nightly-mirror-of-the-customer-portal-artifacts-for-the-dr-site';
export const exposureRule = 'customer-releases';

/** One ustar entry: a 512-byte header and the content padded to the block size; a name ending in `/` is a directory. */
function tarEntry(name: string, content: Buffer): Buffer {
  const header = Buffer.alloc(512);
  const octal = (value: number, width: number): string => value.toString(8).padStart(width - 1, '0') + '\0';
  const dir = name.endsWith('/');
  header.write(name, 0, 100, 'utf8');
  header.write(octal(dir ? 0o755 : 0o644, 8), 100, 'ascii');
  header.write(octal(0, 8), 108, 'ascii');
  header.write(octal(0, 8), 116, 'ascii');
  header.write(octal(content.length, 12), 124, 'ascii');
  header.write(octal(0, 12), 136, 'ascii');
  header.write(' '.repeat(8), 148, 'ascii');
  header.write(dir ? '5' : '0', 156, 'ascii');
  header.write('ustar\0' + '00', 257, 'ascii');
  const sum = header.reduce((a, b) => a + b, 0);
  header.write(sum.toString(8).padStart(6, '0') + '\0 ', 148, 'ascii');
  return Buffer.concat([header, content, Buffer.alloc((512 - (content.length % 512)) % 512)]);
}

/** A gzip-compressed tar of the entries, with the two empty blocks of its end. */
function tarGz(entries: readonly (readonly [string, Buffer])[]): Buffer {
  return gzipSync(Buffer.concat([...entries.map(([name, content]) => tarEntry(name, content)), Buffer.alloc(1024)]));
}

/** The body of an npm publish, as the npm client sends it: the manifest and the tarball as an attachment. */
function npmPublishBody(edge: string, project: string, p: (typeof npmPackages)[number]): object {
  const manifest = {
    name: p.name,
    version: p.version,
    description: `${p.name}, published by the seed of the renders`,
    license: 'MIT',
    ...(p.dependencies ? { dependencies: p.dependencies } : {}),
    repository: { type: 'git', url: 'https://git.example.invalid/web/ui-kit.git' },
  };
  const entries = [tarEntry('package/package.json', Buffer.from(JSON.stringify(manifest, null, 2)))];
  if (p.readme) entries.push(tarEntry('package/README.md', Buffer.from(p.readme)));
  const tgz = gzipSync(Buffer.concat([...entries, Buffer.alloc(1024)]));
  const file = `${p.name.split('/').pop()}-${p.version}.tgz`;
  return {
    _id: p.name,
    name: p.name,
    description: manifest.description,
    'dist-tags': { [p.tag ?? (p.version.includes('-') ? 'next' : 'latest')]: p.version },
    ...(p.readme ? { readme: p.readme } : {}),
    versions: {
      [p.version]: {
        ...manifest,
        ...(p.readme ? { readme: p.readme } : {}),
        _id: `${p.name}@${p.version}`,
        dist: {
          integrity: `sha512-${createHash('sha512').update(tgz).digest('base64')}`,
          shasum: createHash('sha1').update(tgz).digest('hex'),
          tarball: `${edge}/api/npm/${project}/${p.repo}/${p.name}/-/${file}`,
        },
      },
    },
    access: null,
    _attachments: { [file]: { content_type: 'application/octet-stream', data: tgz.toString('base64'), length: tgz.length } },
  };
}

/**
 * The packages and files, published by the power user with a personal access token through the formats' routes, as a
 * client does. A version that exists already answers a conflict, which is fine.
 */
export async function seedContent(ctx: APIRequestContext, edge: string, token: string): Promise<void> {
  const auth = { Authorization: `Bearer ${token}` };
  for (const p of [...npmPackages, ...vendorPackages, ...manyVersionPackages]) {
    const url = `/api/npm/${mainProject}/${p.repo}/${p.name.replace('/', '%2f')}`;
    const res = await patient(() => ctx.put(url, { data: npmPublishBody(edge, mainProject, p), headers: auth }));
    await expectStatus(res, [200, 201, 409], `npm publish ${p.name}@${p.version}`);
  }
  await fetchOnce(ctx, auth);
  await publishNuget(ctx, auth);
  await publishDeb(ctx, auth);
  await publishCollections(ctx, auth);
  for (const f of genericFiles) {
    const url = `/files/${mainProject}/${f.repo}/${f.path.split('/').map(encodeURIComponent).join('/')}`;
    const head = await patient(() => ctx.head(url, { headers: auth }));
    if (head.status() === 200) continue;
    const res = await patient(() =>
      ctx.put(url, { data: Buffer.from(f.content), headers: { ...auth, 'Content-Type': 'application/octet-stream' } }),
    );
    await expectStatus(res, [200, 201], `upload ${f.repo}/${f.path}`);
  }
  for (const f of genericFolders) {
    // An empty body with a trailing slash creates the folder; an existing one answers 200 (R-API-025).
    const res = await ctx.put(`/files/${mainProject}/${f.repo}/${f.path}/`, { data: Buffer.alloc(0), headers: auth });
    await expectStatus(res, [200, 201], `folder ${f.repo}/${f.path}`);
  }
  for (const image of images) await pushImage(ctx, edge, token, image);
  await seedRemoteImages(ctx, edge, token);
}

/**
 * Runs the sync of the OSV source as the break-glass account and waits until the evaluation has graded the package of grade E with
 * its fixable findings and the many-version package, so that the screens show this archive's grades (R-VULN-038, R-VULN-040). An
 * archive that is already imported is not imported again, and the grades are there already. The database is shared by the
 * worktrees that render: another seed's archive may have been imported since, and the wait holds until this one's is.
 */
export async function syncAdvisories(admin: Api): Promise<void> {
  // The archive replaces the one before it: the power user holds deploy on the mirror repository but not overwrite, the break-glass
  // account both (R-AUTHZ-005). Through the content route the session authenticates (R-API-025).
  const archive = `/api/v1/projects/${advisoryMirror.project}/repositories/${advisoryMirror.key}/content/osv/npm/all.zip`;
  const upload = await admin.put(archive, advisoryArchive(), { 'Content-Type': 'application/zip' });
  await expectStatus(upload, [200, 201], `upload the advisory archive to ${advisoryMirror.project}/${advisoryMirror.key}`);
  const run = await admin.post('/api/v1/system/tasks/vuln-sync-osv/run', {});
  await expectStatus(run, [202, 409], 'run the advisory sync');
  const graded = (await idOf(admin, gradedPackage.name)) as string;
  const many = await idOf(admin, manyVersionsPackage.name);
  const want = manyVersionPackages.find((v) => v.grade === 'E' && (v.fixable ?? 0) > 0)!;
  const isGraded = async (id: string, version: string, grade: string, fixable: number): Promise<boolean> => {
    const res = await admin.get(`/api/v1/packages/${id}/versions/${version}/vulnerabilities`);
    if (!res.ok()) return false;
    const report = (await res.json()) as { repositories: { grade?: { grade: string; fixable?: number } }[] };
    return report.repositories.some((r) => r.grade?.grade === grade && r.grade.fixable === fixable);
  };
  for (let i = 0; i < 180; i++) {
    if ((await isGraded(graded, gradedPackage.version, 'E', 112)) && (await isGraded(many, want.version, 'E', want.fixable ?? 0))) return;
    if (i % 30 === 29) await expectStatus(await admin.post('/api/v1/system/tasks/vuln-sync-osv/run', {}), [202, 409], 'run the advisory sync again');
    await new Promise((r) => setTimeout(r, 1000));
  }
  throw new Error(
    `the advisory archive was not imported and evaluated within three minutes: ${gradedPackage.name} is not E with 112 fixable, or ${manyVersionsPackage.name}@${want.version} not E`,
  );
}

async function idOf(admin: Api, name: string): Promise<string> {
  const list = await admin.get(`/api/v1/packages?repository=${mainProject}/npm-vendor&format=npm&q=${name}`);
  await expectStatus(list, [200], `find ${name}`);
  const item = ((await list.json()) as { items: { id: string; name: string }[] }).items.find((p) => p.name === name);
  if (!item) throw new Error(`${name} was not published`);
  return item.id;
}

/**
 * Fetches the tarballs of {@link fetchedVersions} once, so that the column **Last download** of the versions table has times (R-API-065)
 * and the other versions read "Never". A version that was fetched before is not fetched again: a second run adds nothing.
 */
async function fetchOnce(ctx: APIRequestContext, auth: Record<string, string>): Promise<void> {
  const lookup = await patient(() => ctx.get(`/api/v1/packages/lookup?format=npm&name=${manyVersionsPackage.name}`, { headers: auth }));
  await expectStatus(lookup, [200], `look up ${manyVersionsPackage.name}`);
  const id = ((await lookup.json()) as { id: string }).id;
  for (const version of fetchedVersions) {
    const detail = await patient(() => ctx.get(`/api/v1/packages/${id}/versions/${version}`, { headers: auth }));
    await expectStatus(detail, [200], `${manyVersionsPackage.name}@${version}`);
    if (((await detail.json()) as { lastDownloadedAt: string | null }).lastDownloadedAt) continue;
    const tarball = `/api/npm/${mainProject}/${manyVersionsPackage.repo}/${manyVersionsPackage.name}/-/${manyVersionsPackage.name}-${version}.tgz`;
    await expectStatus(await patient(() => ctx.get(tarball, { headers: auth })), [200], `fetch ${manyVersionsPackage.name}@${version}`);
  }
}

/** A `.nupkg`: the nuspec and one library, as `dotnet pack` builds it; the same version gives the same bytes, so a second push replays. */
function nupkg(id: string, version: string): Buffer {
  const nuspec = `<?xml version="1.0" encoding="utf-8"?>
<package xmlns="http://schemas.microsoft.com/packaging/2013/05/nuspec.xsd">
  <metadata>
    <id>${id}</id>
    <version>${version}</version>
    <authors>Platform engineering</authors>
    <description>Logging abstractions shared by the platform's .NET services.</description>
    <license type="expression">MIT</license>
    <projectUrl>https://git.example.invalid/platform/logging</projectUrl>
    <repository type="git" url="https://git.example.invalid/platform/logging.git" />
    <tags>logging diagnostics</tags>
    <dependencies>
      <group targetFramework="net8.0">
        <dependency id="Microsoft.Extensions.Logging.Abstractions" version="8.0.0" />
      </group>
      <group targetFramework=".NETStandard2.0" />
    </dependencies>
  </metadata>
</package>
`;
  return zipArchive([
    { name: `${id}.nuspec`, data: Buffer.from(nuspec) },
    { name: `lib/net8.0/${id}.dll`, data: Buffer.from(`MZ ${id} ${version}`) },
  ]);
}

/** The NuGet package of {@link nugetPackage}, pushed as `curl` would: the bare package as the body (R-NUGET-006). */
async function publishNuget(ctx: APIRequestContext, auth: Record<string, string>): Promise<void> {
  for (const version of nugetPackage.versions) {
    const res = await patient(() =>
      ctx.put(`/api/nuget/${otherProject}/${nugetPackage.repo}/v3/package`, {
      data: nupkg(nugetPackage.name, version),
      headers: { ...auth, 'Content-Type': 'application/octet-stream' },
    }),
    );
    await expectStatus(res, [200, 201, 202, 409], `nuget push ${nugetPackage.name} ${version}`);
  }
}

/** An `ar` archive of dpkg: the global header, then a 60-byte header per member and its data padded to an even length. */
function ar(members: readonly (readonly [string, Buffer])[]): Buffer {
  const parts: Buffer[] = [Buffer.from('!<arch>\n')];
  for (const [name, data] of members) {
    const header = `${name.padEnd(16)}${'1767225600'.padEnd(12)}${'0'.padEnd(6)}${'0'.padEnd(6)}${'100644'.padEnd(8)}${String(data.length).padEnd(10)}\u0060\n`;
    parts.push(Buffer.from(header, 'ascii'), data);
    if (data.length % 2 === 1) parts.push(Buffer.from('\n'));
  }
  return Buffer.concat(parts);
}

/** A `.deb` of one script, as test/debstub/debgen builds it. */
function deb(name: string, version: string): Buffer {
  const script = Buffer.from(`#!/bin/sh\necho '${name} ${version}'\n`);
  const control =
    `Package: ${name}\nVersion: ${version}\nArchitecture: amd64\nMaintainer: Platform engineering <platform@example.invalid>\n` +
    'Installed-Size: 1\nDepends: libc6 (>= 2.36), ca-certificates\nSection: admin\nPriority: optional\n' +
    'Homepage: https://git.example.invalid/platform/acme-agent\n' +
    'Description: the agent of the platform team\n Collects the host facts the platform dashboards show.\n';
  return ar([
    ['debian-binary', Buffer.from('2.0\n')],
    ['control.tar.gz', tarGz([['./', Buffer.alloc(0)], ['./control', Buffer.from(control)]])],
    [
      'data.tar.gz',
      tarGz([
        ['./', Buffer.alloc(0)],
        ['./usr/', Buffer.alloc(0)],
        ['./usr/bin/', Buffer.alloc(0)],
        [`./usr/bin/${name}`, script],
      ]),
    ],
  ]);
}

/** The Debian package of {@link debPackage}, uploaded to the suite stable (R-DEB-006). */
async function publishDeb(ctx: APIRequestContext, auth: Record<string, string>): Promise<void> {
  for (const version of debPackage.versions) {
    const file = `${debPackage.name}_${version}_amd64.deb`;
    const res = await patient(() =>
      ctx.put(`/api/deb/${otherProject}/${debPackage.repo}/upload/${file}?distribution=stable&component=main`, {
      data: deb(debPackage.name, version),
      headers: { ...auth, 'Content-Type': 'application/vnd.debian.binary-package' },
    }),
    );
    await expectStatus(res, [200, 201, 409], `deb upload ${file}`);
  }
}

/** A collection archive with the MANIFEST.json and FILES.json ansible-galaxy builds (test/ansiblestub/ansiblegen). */
function collection(namespace: string, name: string, version: string): Buffer {
  const sha = (b: Buffer): string => createHash('sha256').update(b).digest('hex');
  const files: [string, Buffer][] = [
    ['README.md', Buffer.from(`# ${namespace}.${name}\n\nRoles and modules of the platform team, version ${version}.\n`)],
    ['meta/runtime.yml', Buffer.from("requires_ansible: '>=2.15.0'\n")],
    ['roles/agent/tasks/main.yml', Buffer.from('---\n- name: Install the agent\n  ansible.builtin.package:\n    name: acme-agent\n')],
    ['roles/agent/meta/main.yml', Buffer.from("---\ngalaxy_info:\n  author: platform\n  description: the agent\n  license: MIT\n  min_ansible_version: '2.15'\n  platforms: []\ndependencies: []\n")],
  ];
  const dirs = ['meta', 'roles', 'roles/agent', 'roles/agent/meta', 'roles/agent/tasks'];
  const list = [
    { name: '.', ftype: 'dir', chksum_type: null, chksum_sha256: null, format: 1 },
    ...[...dirs.map((d) => [d, null] as const), ...files]
      .sort((a, b) => a[0].localeCompare(b[0]))
      .map(([n, data]) =>
        data === null
          ? { name: n, ftype: 'dir', chksum_type: null, chksum_sha256: null, format: 1 }
          : { name: n, ftype: 'file', chksum_type: 'sha256', chksum_sha256: sha(data), format: 1 },
      ),
  ];
  const filesJson = Buffer.from(JSON.stringify({ files: list, format: 1 }, null, 4));
  const manifest = Buffer.from(
    JSON.stringify(
      {
        collection_info: {
          namespace,
          name,
          version,
          authors: ['Platform engineering <platform@example.invalid>'],
          readme: 'README.md',
          tags: ['platform', 'agent'],
          description: 'Roles and modules of the platform team',
          license: ['MIT'],
          license_file: null,
          dependencies: {},
          repository: `https://git.example.invalid/${namespace}/${name}`,
          documentation: `https://git.example.invalid/${namespace}/${name}/docs`,
          homepage: `https://git.example.invalid/${namespace}/${name}`,
          issues: `https://git.example.invalid/${namespace}/${name}/issues`,
        },
        file_manifest_file: { name: 'FILES.json', ftype: 'file', chksum_type: 'sha256', chksum_sha256: sha(filesJson), format: 1 },
        format: 1,
      },
      null,
      4,
    ),
  );
  return tarGz([
    ['FILES.json', filesJson],
    ['MANIFEST.json', manifest],
    ...dirs.map((d): [string, Buffer] => [`${d}/`, Buffer.alloc(0)]),
    ...files,
  ]);
}

/** The collection of {@link ansiblePackage}, published as ansible-galaxy does (R-ANS-008); the import runs as a task. */
async function publishCollections(ctx: APIRequestContext, auth: Record<string, string>): Promise<void> {
  for (const version of ansiblePackage.versions) {
    const archive = collection(ansiblePackage.namespace, ansiblePackage.name, version);
    const res = await patient(() =>
      ctx.post(`/api/ansible/${otherProject}/${ansiblePackage.repo}/v3/artifacts/collections/`, {
      headers: auth,
      multipart: {
        sha256: createHash('sha256').update(archive).digest('hex'),
        file: { name: `${ansiblePackage.namespace}-${ansiblePackage.name}-${version}.tar.gz`, mimeType: 'application/gzip', buffer: archive },
      },
    }),
    );
    await expectStatus(res, [200, 201, 202, 409], `ansible publish ${ansiblePackage.namespace}.${ansiblePackage.name} ${version}`);
  }
}

/** Pushes a small image (one layer, a config, a manifest per tag) through `/v2/` with a registry token (R-OCI-005). */
async function pushImage(ctx: APIRequestContext, edge: string, pat: string, image: (typeof images)[number]): Promise<void> {
  const repo = `${image.project}/${image.repo}/${image.name}`;
  const bearer = await registryToken(ctx, edge, pat, repo);
  const auth = { Authorization: `Bearer ${bearer}` };
  const layer = gzipSync(tarEntry('etc/acme-release', Buffer.from(`${image.name}\n`)));
  const config = Buffer.from(
    JSON.stringify({ architecture: 'amd64', os: 'linux', config: { Labels: { 'org.opencontainers.image.title': image.name } }, rootfs: { type: 'layers', diff_ids: [] } }),
  );
  const digestOf = (b: Buffer): string => `sha256:${createHash('sha256').update(b).digest('hex')}`;
  for (const blob of [layer, config]) {
    const digest = digestOf(blob);
    if ((await ctx.head(`/v2/${repo}/blobs/${digest}`, { headers: auth })).status() === 200) continue;
    const start = await ctx.post(`/v2/${repo}/blobs/uploads/`, { headers: auth });
    await expectStatus(start, [202], `start upload ${repo}`);
    const location = new URL(start.headers()['location'] ?? '', edge);
    location.searchParams.set('digest', digest);
    const put = await ctx.put(location.pathname + location.search, {
      data: blob,
      headers: { ...auth, 'Content-Type': 'application/octet-stream' },
    });
    await expectStatus(put, [201], `upload blob ${repo}`);
  }
  const manifest = Buffer.from(
    JSON.stringify({
      schemaVersion: 2,
      mediaType: 'application/vnd.oci.image.manifest.v1+json',
      config: { mediaType: 'application/vnd.oci.image.config.v1+json', digest: digestOf(config), size: config.length },
      layers: [{ mediaType: 'application/vnd.oci.image.layer.v1.tar+gzip', digest: digestOf(layer), size: layer.length }],
      annotations: { 'org.opencontainers.image.description': `The ${image.name} image of the renders` },
    }),
  );
  for (const tag of image.tags) {
    const res = await patient(() =>
      ctx.put(`/v2/${repo}/manifests/${tag}`, {
      data: manifest,
      headers: { ...auth, 'Content-Type': 'application/vnd.oci.image.manifest.v1+json' },
      }),
    );
    await expectStatus(res, [201], `push ${repo}:${tag}`);
  }
}

/**
 * The image of two platforms in image-source, and the pulls through the remote image-cache that give its rows the three kinds of
 * size (W-24, R-API-080, Q-OCI-221): the single-platform tag whole, one platform of the index `2.0`, and only the index `2.1`. Every
 * GET of a tag is a client's download of it (R-API-079), so the remote lists the three tags. A run again pushes nothing and pulls
 * again, which only counts more downloads.
 */
export async function seedRemoteImages(ctx: APIRequestContext, edge: string, pat: string): Promise<void> {
  const source = `${otherProject}/${remoteSource.key}/${remoteSource.prefix}/${remoteImage.name}`;
  const push = { Authorization: `Bearer ${await registryToken(ctx, edge, pat, source)}` };
  const digestOf = (b: Buffer): string => `sha256:${createHash('sha256').update(b).digest('hex')}`;
  const putBlob = async (blob: Buffer): Promise<string> => {
    const digest = digestOf(blob);
    if ((await ctx.head(`/v2/${source}/blobs/${digest}`, { headers: push })).status() === 200) return digest;
    const start = await ctx.post(`/v2/${source}/blobs/uploads/`, { headers: push });
    await expectStatus(start, [202], `start upload ${source}`);
    const location = new URL(start.headers()['location'] ?? '', edge);
    location.searchParams.set('digest', digest);
    const put = await ctx.put(location.pathname + location.search, { data: blob, headers: { ...push, 'Content-Type': 'application/octet-stream' } });
    await expectStatus(put, [201], `upload blob ${source}`);
    return digest;
  };
  const putManifest = async (ref: string, body: Buffer, type: string): Promise<void> => {
    const res = await patient(() => ctx.put(`/v2/${source}/manifests/${ref}`, { data: body, headers: { ...push, 'Content-Type': type } }));
    await expectStatus(res, [201], `push ${source}:${ref}`);
  };
  const image = async (arch: string, release: string): Promise<{ digest: string; size: number; body: Buffer }> => {
    // A layer of some MiB, so that the sizes read as an image's do. Its bytes are pseudo-random from a seed of the platform and the
    // release, so that they hardly compress and a second run of the seed pushes the same image again.
    const content = Buffer.alloc((arch === 'amd64' ? 3 : 2) * 1024 * 1024);
    let x = 2463534242 ^ (arch.charCodeAt(1) * 65_537 + release.charCodeAt(release.length - 1));
    for (let i = 0; i < content.length; i++) {
      x ^= x << 13;
      x ^= x >>> 17;
      x ^= x << 5;
      content[i] = x & 0xff;
    }
    const layer = gzipSync(tarEntry(`opt/agent/agent-${arch}`, content), { level: 1 });
    const config = Buffer.from(JSON.stringify({ architecture: arch, os: 'linux', config: { Labels: { release } }, rootfs: { type: 'layers', diff_ids: [] } }));
    const manifest = Buffer.from(
      JSON.stringify({
        schemaVersion: 2,
        mediaType: 'application/vnd.oci.image.manifest.v1+json',
        config: { mediaType: 'application/vnd.oci.image.config.v1+json', digest: await putBlob(config), size: config.length },
        layers: [{ mediaType: 'application/vnd.oci.image.layer.v1.tar+gzip', digest: await putBlob(layer), size: layer.length }],
      }),
    );
    const digest = digestOf(manifest);
    await putManifest(digest, manifest, 'application/vnd.oci.image.manifest.v1+json');
    return { digest, size: manifest.length, body: manifest };
  };
  const index = async (tag: string): Promise<string[]> => {
    const children = [await image('amd64', tag), await image('arm64', tag)];
    const body = Buffer.from(
      JSON.stringify({
        schemaVersion: 2,
        mediaType: 'application/vnd.oci.image.index.v1+json',
        manifests: children.map((c, i) => ({
          mediaType: 'application/vnd.oci.image.manifest.v1+json',
          digest: c.digest,
          size: c.size,
          platform: { os: 'linux', architecture: i === 0 ? 'amd64' : 'arm64' },
        })),
      }),
    );
    await putManifest(tag, body, 'application/vnd.oci.image.index.v1+json');
    return children.map((c) => c.digest);
  };
  const single = await image('amd64', remoteImage.single);
  await putManifest(remoteImage.single, single.body, 'application/vnd.oci.image.manifest.v1+json');
  const partial = await index(remoteImage.partial);
  await index(remoteImage.unknown);

  // The pulls of a client through the remote, as the power user (a developer of the project reads it).
  const cached = `${otherProject}/${remoteCache.key}/${remoteImage.name}`;
  const pull = { Authorization: `Bearer ${await registryToken(ctx, edge, pat, cached, 'pull')}`, Accept: 'application/vnd.oci.image.index.v1+json, application/vnd.oci.image.manifest.v1+json' };
  for (const ref of [remoteImage.single, remoteImage.partial, partial[0]!, remoteImage.unknown]) {
    const res = await patient(() => ctx.get(`/v2/${cached}/manifests/${ref}`, { headers: pull }));
    await expectStatus(res, [200], `pull ${cached}:${ref} through the remote`);
  }
}

/** The handshake of a registry client: the challenge of `/v2/`, then the token service with the PAT as the password. */
async function registryToken(ctx: APIRequestContext, edge: string, pat: string, repo: string, actions = 'pull,push'): Promise<string> {
  const challenge = (await ctx.get('/v2/')).headers()['www-authenticate'] ?? '';
  const realm = /realm="([^"]+)"/.exec(challenge)?.[1];
  const service = /service="([^"]+)"/.exec(challenge)?.[1];
  if (!realm || !service) throw new Error(`/v2/ sent no Bearer challenge: ${challenge}`);
  const url = new URL(realm, edge);
  url.searchParams.set('service', service);
  url.searchParams.set('scope', `repository:${repo}:${actions}`);
  const res = await ctx.get(url.pathname + url.search, {
    headers: { Authorization: `Basic ${Buffer.from(`${kinds.power}:${pat}`).toString('base64')}` },
  });
  await expectStatus(res, [200], `registry token for ${repo}`);
  const body = (await res.json()) as { token?: string; access_token?: string };
  const token = body.token ?? body.access_token;
  if (!token) throw new Error('the token service sent no token');
  return token;
}


/** The own profile of the renders. */
export const limitProfile = 'partners';
/** The CI issuer and the CI project of the renders' usage (R-LIM-001: a CI project is charged by issuer and project ID). */
export const ciIssuer = 'gitlab-internal';
export const ciProject = { id: '4711', path: 'acme/customer-portal/web-frontend' } as const;
/** The anonymous callers of the renders: an IPv6 prefix with a rule of its own, a blocked address, a small one (R-LIM-002). */
export const anonymousV6 = '2001:db8:1:2::/64';
export const blockedAddress = '198.51.100.23';
export const quietAddress = '203.0.113.9';

/** The UI development profile's PostgreSQL container (deploy/ui-dev/compose.yaml). */
const postgresContainer = 'quader-ui-dev-postgres';

/**
 * Runs SQL in the UI development profile's database through its container (deploy/ui-dev/compose.yaml), as the profile's
 * database owner. It is the renders' seam for what no request makes in the seed's time: 24 hours and 30 days of usage, the CI
 * issuer of a CI project, a block. Quader has no path that writes any of it.
 */
function psql(sql: string): void {
  const password = readFileSync(join(stateDir, 'secrets', 'db-pass'), 'utf8').trim();
  execFileSync(
    'docker',
    ['exec', '-i', '-e', `PGPASSWORD=${password}`, postgresContainer, 'psql', '-q', '-v', 'ON_ERROR_STOP=1', 'host=localhost dbname=quader user=quader sslmode=require'],
    { input: sql, stdio: ['pipe', 'inherit', 'inherit'] },
  );
}

/** A text as an SQL literal. */
const lit = (s: string): string => `'${s.replace(/'/g, "''")}'`;

/** The ID of a technical token by its account's stored name and its name, as SQL (R-AUTH-045). */
const tokenRef = (account: string, name: string): string =>
  `(SELECT t.id::text FROM technical_token t JOIN principal p ON p.id = t.service_account_id WHERE p.name = ${lit(account)} AND t.name = ${lit(name)})`;
const userRef = (name: string): string => `(SELECT id::text FROM principal WHERE kind = 'user' AND name = ${lit(name)})`;
const repoId = (project: string, key: string): string =>
  `(SELECT r.id FROM repositories r JOIN project p ON p.id = r.project_id WHERE p.key = ${lit(project)} AND r.key = ${lit(key)})`;
const projectPlace = (project: string): string => `(SELECT 'project:' || id::text FROM project WHERE key = ${lit(project)})`;
const repoPlace = (project: string, key: string): string => `('repository:' || ${repoId(project, key)}::text)`;

/** The CI issuer and one issuance of the renders' CI project, so that the project is named by its path (R-LIM-034). */
function ciIssuerSql(): string {
  return `
INSERT INTO ci_issuers (id, name, kind, issuer, allowed_audiences, required_claims, pin_claims, single_use_subject_tokens, enabled, description)
SELECT gen_random_uuid(), ${lit(ciIssuer)}, 'gitlab', 'https://gitlab.renders.invalid', '{quader}', '{project_id}', '{project_id}', false, false,
       'The renders'' CI issuer: disabled, its key set is never fetched.'
 WHERE NOT EXISTS (SELECT 1 FROM ci_issuers WHERE name = ${lit(ciIssuer)});
INSERT INTO ci_token_issuances (token_id, issuer_id, mapping_ids, mapping_names, subject_kind, project_id, project_path, job_id, claims,
                                grants, granted_scope, issued_at, expires_at)
SELECT gen_random_uuid(), i.id, '{}', '{}', 'id_token', ${lit(ciProject.id)}, ${lit(ciProject.path)}, '1', '{}', '[]', '', now(), now() + interval '1 hour'
  FROM ci_issuers i
 WHERE i.name = ${lit(ciIssuer)}
   AND NOT EXISTS (SELECT 1 FROM ci_token_issuances x WHERE x.issuer_id = i.id AND x.project_id = ${lit(ciProject.id)});
`;
}

/**
 * One caller's usage at one place (R-LIM-027): its requests, bytes from the cache and from the upstream, upstream requests and
 * refused requests in the last 24 hours and, of those, in the last hour, and the profile whose limits the rows record.
 */
interface UsageSeed {
  readonly kind: 'user' | 'technical_token' | 'ci' | 'anonymous' | 'system';
  /** The subject's reference, as SQL. */
  readonly ref: string;
  /** The credential column (R-LIM-027): a user's session or personal token, empty for the other kinds; as SQL. */
  readonly credential: string;
  readonly place: string;
  readonly repo: string;
  readonly day: readonly [number, number, number, number, number];
  readonly hour: readonly [number, number, number, number, number];
  /** The profile whose figures the rows record as the budget key's limits; null for an unlimited caller. */
  readonly profile: string | null;
  /** A figure the rows record lower than the profile's: an override. */
  readonly requestsPerHour?: number;
}

const GB = 1_000_000_000;
const MB = 1_000_000;

/**
 * The usage of the renders: every kind of caller, apart by orders of magnitude, with a day's history in five-minute points and a
 * month in hours; the tightest figure of the project's heavy mirror token at 82 % (close to the limit), the IPv6 prefix above its
 * hour (would have been limited), the administrator unlimited.
 */
function usageSeeds(): readonly UsageSeed[] {
  const instanceToken = tokenRef(serviceAccount, 'release');
  const projectToken = tokenRef(`${mainProject}/${projectServiceAccount}`, longTechnicalToken);
  const repoToken = tokenRef(`${mainProject}/npm-internal/${repositoryServiceAccount}`, 'release');
  const ci = `((SELECT id::text FROM ci_issuers WHERE name = ${lit(ciIssuer)}) || ':${ciProject.id}')`;
  // The plain user's personal token "laptop" (setup.ts), so that its usage splits into the session and the token (R-UI-184).
  const laptop = `COALESCE((SELECT t.id::text FROM api_token t WHERE t.principal_id::text = ${userRef(kinds.plain)} AND t.description = 'laptop' ORDER BY t.created_at LIMIT 1), 'session')`;
  return [
    { kind: 'technical_token', ref: projectToken, credential: `''`, place: projectPlace(mainProject), repo: repoId(mainProject, 'npmjs'),
      day: [2_400_000, 4_100 * GB, 210 * GB, 18_000, 40], hour: [164_000, 190 * GB, 9 * GB, 900, 2], profile: 'technical' },
    { kind: 'ci', ref: ci, credential: `''`, place: `'instance'`, repo: repoId(mainProject, 'npm-internal'),
      day: [380_000, 1_400 * GB, 0, 0, 120], hour: [21_000, 60 * GB, 0, 0, 4], profile: 'technical' },
    { kind: 'technical_token', ref: instanceToken, credential: `''`, place: `'instance'`, repo: repoId(otherProject, 'base-images'),
      day: [120_400, 640 * GB, 12 * GB, 800, 0], hour: [5_200, 31 * GB, 0, 0, 0], profile: 'technical' },
    { kind: 'user', ref: userRef(kinds.power), credential: `'session'`, place: `'instance'`, repo: repoId(otherProject, 'base-images'),
      day: [48_210, 1_203 * MB, 50 * MB, 120, 3], hour: [2_300, 70 * MB, 0, 0, 0], profile: 'standard' },
    { kind: 'technical_token', ref: repoToken, credential: `''`, place: `'instance'`, repo: repoId(mainProject, 'npm-internal'),
      day: [3_400, 12 * GB, 0, 0, 0], hour: [150, 500 * MB, 0, 0, 0], profile: 'technical' },
    { kind: 'user', ref: userRef(kinds.plain), credential: `'session'`, place: `'instance'`, repo: repoId(mainProject, 'assets'),
      day: [1_204, 3_200 * MB, 0, 0, 0], hour: [40, 90 * MB, 0, 0, 0], profile: 'standard' },
    { kind: 'user', ref: userRef(kinds.plain), credential: laptop, place: `'instance'`, repo: repoId(mainProject, 'npm-internal'),
      day: [2_900, 1_100 * MB, 0, 0, 0], hour: [95, 30 * MB, 0, 0, 0], profile: 'standard' },
    { kind: 'user', ref: userRef(kinds.admin), credential: `'session'`, place: `'instance'`, repo: repoId(mainProject, 'assets'),
      day: [310, 45 * MB, 0, 0, 0], hour: [12, 2 * MB, 0, 0, 0], profile: null },
    { kind: 'anonymous', ref: lit(anonymousV6), credential: `''`, place: `'instance'`, repo: repoId(otherProject, 'docker-hub-remote'),
      day: [24_000, 9_800 * MB, 1_900 * MB, 140, 12], hour: [5_600, 2_100 * MB, 0, 0, 0], profile: 'restricted' },
    { kind: 'anonymous', ref: lit(blockedAddress), credential: `''`, place: `'instance'`,
      repo: repoId(otherProject, 'docker-hub-remote'), day: [61, 0, 0, 0, 61], hour: [61, 0, 0, 0, 61], profile: 'anonymous' },
    { kind: 'anonymous', ref: lit(quietAddress), credential: `''`, place: repoPlace(mainProject, 'npmjs'), repo: repoId(mainProject, 'npmjs'),
      day: [1_400, 30 * MB, 2 * MB, 3, 0], hour: [12, 0, 0, 0, 0], profile: 'anonymous' },
    { kind: 'system', ref: `'system'`, credential: `''`, place: `'instance'`, repo: repoId(otherProject, 'docker-hub-remote'),
      day: [0, 0, 40 * GB, 2_000, 0], hour: [0, 0, 1_600 * MB, 80, 0], profile: null },
  ];
}

/** The rows of one caller: 288 five-minute points of the last 24 hours and 720 hourly ones of the last 30 days. */
function usageSql(s: UsageSeed): string {
  const limit = (column: string): string => (s.profile ? `(SELECT ${column} FROM limit_profile WHERE key = ${lit(s.profile)})` : 'NULL');
  const limits = [
    s.requestsPerHour !== undefined ? String(s.requestsPerHour) : limit('requests_per_hour'),
    limit('cached_bytes_per_hour'),
    limit('uncached_bytes_per_hour'),
    limit('upstream_requests_per_hour'),
    limit('bytes_per_day'),
  ].join(', ');
  // A day's shape: quiet at night, busy from the morning to the evening, never the same twice.
  const shape = (at: string, i: string): string =>
    `(0.3 + 1.4 * greatest(0, sin(pi() * (extract(hour FROM ${at}) + extract(minute FROM ${at}) / 60 - 6) / 14)))` +
    ` * (0.6 + 0.8 * mod((abs(sin(${i} * 12.9898 + 78.233)) * 43758.5453)::numeric, 1))`;
  const share = (n: number, h: number, i: string, at: string): string =>
    `CASE WHEN ${i} < 12 THEN ${Math.round(h / 12)} ELSE round(${n - h} / 276.0 * ${shape(at, i)})::bigint END`;
  const [rq, cb, ub, up, rf] = s.day;
  const [hrq, hcb, hub, hup, hrf] = s.hour;
  const at5 = `(n.t - make_interval(mins => 5 * i))`;
  const at1 = `(date_trunc('hour', now()) - make_interval(hours => i))`;
  const hourly = (n: number): string => `round(${n} / 24.0 * ${shape(at1, 'i')} * (0.7 + 0.3 * cos(i / 24.0)))::bigint`;
  return `
INSERT INTO usage_bucket (granularity, bucket_start, subject_kind, subject_ref, credential_ref, place, repository_id, requests, refused,
                          cached_bytes, uncached_bytes, upstream_requests, limit_requests_hour, limit_cached_hour, limit_uncached_hour,
                          limit_upstream_hour, limit_bytes_day)
SELECT '5m', ${at5}, ${lit(s.kind)}, ${s.ref}, ${s.credential}, ${s.place}, ${s.repo},
       ${share(rq, hrq, 'i', at5)}, ${share(rf, hrf, 'i', at5)}, ${share(cb, hcb, 'i', at5)}, ${share(ub, hub, 'i', at5)},
       ${share(up, hup, 'i', at5)}, ${limits}
  FROM (SELECT to_timestamp(floor(extract(epoch FROM now()) / 300) * 300) AS t) n, generate_series(0, 287) i
 WHERE ${s.ref} IS NOT NULL
ON CONFLICT DO NOTHING;
INSERT INTO usage_bucket (granularity, bucket_start, subject_kind, subject_ref, credential_ref, place, repository_id, requests, refused,
                          cached_bytes, uncached_bytes, upstream_requests, limit_requests_hour, limit_cached_hour, limit_uncached_hour,
                          limit_upstream_hour, limit_bytes_day)
SELECT '1h', ${at1}, ${lit(s.kind)}, ${s.ref}, ${s.credential}, ${s.place}, ${s.repo},
       ${hourly(rq)}, ${hourly(rf)}, ${hourly(cb)}, ${hourly(ub)}, ${hourly(up)}, ${limits}
  FROM generate_series(1, 720) i
 WHERE ${s.ref} IS NOT NULL
ON CONFLICT DO NOTHING;
`;
}

/** The limits the callers met (R-LIM-027): would-limit events in Audit for the IPv6 prefix and the heavy mirror, the block's refusals. */
function eventsSql(): string {
  const events = (kind: string, ref: string, place: string, repo: string, figure: string, perBucket: number, from: number, to: number): string => `
INSERT INTO usage_limit_event (granularity, bucket_start, subject_kind, subject_ref, place, repository_id, figure, mode, count)
SELECT '5m', n.t - make_interval(mins => 5 * i), ${lit(kind)}, ${ref}, ${place}, ${repo}, ${lit(figure)}, 'audit', ${perBucket}
  FROM (SELECT to_timestamp(floor(extract(epoch FROM now()) / 300) * 300) AS t) n, generate_series(${from}, ${to}) i
 WHERE ${ref} IS NOT NULL
ON CONFLICT DO NOTHING;`;
  const projectToken = tokenRef(`${mainProject}/${projectServiceAccount}`, longTechnicalToken);
  return [
    events('anonymous', lit(anonymousV6), `'instance'`, repoId(otherProject, 'docker-hub-remote'), 'requestsPerHour', 50, 0, 11),
    events('technical_token', projectToken, projectPlace(mainProject), repoId(mainProject, 'npmjs'), 'cachedBytesPerSecond', 3, 20, 59),
    events('anonymous', lit(blockedAddress), `'instance'`, repoId(otherProject, 'docker-hub-remote'), 'refusedPerHour', 1, 0, 0),
  ].join('\n');
}

/** An active block of the blocked address, which probed the public remote, and a block of yesterday that ended (R-LIM-025). */
function blocksSql(): string {
  const place = `'instance'`;
  return `
INSERT INTO limit_block (id, subject_kind, subject_ref, place, started_at, until, refused_count, refused_limit)
SELECT gen_random_uuid(), 'anonymous', ${lit(blockedAddress)}, ${place}, now() - interval '3 minutes', now() + interval '12 minutes', 61, 60
 WHERE NOT EXISTS (SELECT 1 FROM limit_block WHERE subject_ref = ${lit(blockedAddress)} AND lifted_at IS NULL AND until > now());
INSERT INTO limit_block (id, subject_kind, subject_ref, place, started_at, until, refused_count, refused_limit)
SELECT gen_random_uuid(), 'anonymous', ${lit(quietAddress)}, 'instance', now() - interval '20 hours', now() - interval '19 hours 45 minutes', 64, 60
 WHERE NOT EXISTS (SELECT 1 FROM limit_block WHERE subject_ref = ${lit(quietAddress)} AND started_at > now() - interval '1 day');
`;
}

/**
 * The usage of the renders (R-UI-181, R-UI-183 to R-UI-185): written into the usage store through the profile's database (`psql`),
 * because requests cannot make a day's history, a CI project and a block in the seed's time. The callers no request of the renders
 * charges (the technical tokens, the CI project, the documentation addresses) are written anew on every run, so their figures stay
 * the ones below; the users and the system, whom the renders' own requests charge too, get the buckets they miss and keep the rest.
 */
export function seedUsage(): void {
  const seeds = usageSeeds();
  const own = seeds.filter((u) => u.kind !== 'user' && u.kind !== 'system');
  const forget = own.map(
    (u) => `DELETE FROM usage_bucket WHERE subject_kind = ${lit(u.kind)} AND subject_ref = ${u.ref};
DELETE FROM usage_limit_event WHERE subject_kind = ${lit(u.kind)} AND subject_ref = ${u.ref};`,
  );
  // The tokens that downloaded were used a moment ago: their rows say so beside their last 24 hours (R-UI-181).
  const used = seeds
    .filter((u) => u.kind === 'technical_token')
    .map((u) => `UPDATE api_token SET last_used_at = now() - interval '3 minutes' WHERE technical_token_id::text = ${u.ref} AND revoked_at IS NULL;`);
  psql(['BEGIN;', ...forget, ...seeds.map(usageSql), eventsSql(), blocksSql(), ...used, 'COMMIT;'].join('\n'));
}

/**
 * The download limits of the renders (R-UI-177 to R-UI-186): an own profile with a long name, rules at the three places (an IPv6
 * prefix, a group and a CI project at the instance, the power user and the project's heavy mirror token at the main project, every
 * anonymous caller at a remote), a maximum of the project with the long name, and the ceilings of a public remote. The power user's
 * hourly request figure is low on purpose: the seed's own requests pass it, so the overview shows a caller at the limit in Audit.
 * The mode stays Audit: the database is shared, and Enforce is rendered through the page's routes. A second run adds nothing.
 */
export async function seedLimits(admin: Api): Promise<void> {
  const settings = await admin.get('/api/v1/limits/settings');
  await expectStatus(settings, [200], 'the limits settings');
  psql(ciIssuerSql());
  await expectStatus(
    await admin.post(
      '/api/v1/limits/profiles',
      {
        key: limitProfile,
        name: 'Partners with a contract for the nightly mirrors',
        description: 'External partners with a contract for nightly mirrors of the release artifacts',
        figures: { requestsPerSecond: 50, requestsBurst: 200, requestsPerHour: 5000, cachedBytesPerHour: 50_000_000_000, parallelDownloads: 6 },
      },
      { 'Idempotency-Key': `renders-profile-${limitProfile}` },
    ),
    [201, 409],
    `limit profile ${limitProfile}`,
  );
  const rule = async (base: string, body: object, what: string): Promise<void> =>
    expectStatus(await admin.post(base, body, { 'Idempotency-Key': `renders-rule-${what}` }), [201, 409], `limit rule ${what}`);
  await rule(
    '/api/v1/limits/assignments',
    { who: { kind: 'address', prefix: anonymousV6 }, profile: 'restricted', overrides: {}, note: 'A partner office behind one IPv6 prefix' },
    'v6',
  );
  await rule(
    '/api/v1/limits/assignments',
    { who: { kind: 'group', group }, profile: limitProfile, overrides: { uncachedBytesPerHour: 20_000_000_000 }, note: '' },
    'group',
  );
  await rule(
    '/api/v1/limits/assignments',
    { who: { kind: 'ciProject', issuer: ciIssuer, projectId: ciProject.id }, profile: 'technical', overrides: { bytesPerDay: 2_000_000_000_000 }, note: 'The release pipeline of the customer portal' },
    'ci',
  );
  await rule(
    `/api/v1/projects/${mainProject}/limits/assignments`,
    {
      who: { kind: 'user', user: kinds.power },
      profile: 'restricted',
      overrides: { requestsPerHour: 20 },
      note: 'Low on purpose: the renders show a caller at the limit',
    },
    'power',
  );
  // The project's heavy mirror token, set where its account lives (R-LIM-038): its row on the account's page names the rule.
  const tokens = await admin.get(`/api/v1/projects/${mainProject}/service-accounts/${projectServiceAccount}/technical-tokens`);
  await expectStatus(tokens, [200], `technical tokens of ${projectServiceAccount}`);
  const mirror = ((await tokens.json()) as { items: { id: string; name: string }[] }).items.find((t) => t.name === longTechnicalToken);
  if (mirror) {
    await rule(
      `/api/v1/projects/${mainProject}/limits/assignments`,
      { who: { kind: 'technicalToken', technicalToken: mirror.id }, profile: 'technical', overrides: { cachedBytesPerSecond: 100_000_000 }, note: '' },
      'mirror-token',
    );
  }
  await rule(
    `/api/v1/projects/${mainProject}/repositories/npmjs/limits/assignments`,
    { who: { kind: 'every', callerKind: 'anonymous' }, profile: 'anonymous', overrides: { upstreamRequestsPerHour: 5 }, note: '' },
    'npmjs-anonymous',
  );
  // A project's first maximum takes no If-Match (Q-API-354).
  const maximum = await admin.get(`/api/v1/projects/${longProject}/limits/maximum`);
  await expectStatus(maximum, [200], `maximum of ${longProject}`);
  if ((await maximum.json()).maximum === null) {
    await expectStatus(
      await admin.put(`/api/v1/projects/${longProject}/limits/maximum`, { profile: 'technical', overrides: { bytesPerDay: 2_000_000_000_000 } }),
      [201],
      `maximum of ${longProject}`,
    );
  }
  const ceilingsUrl = `/api/v1/projects/${otherProject}/repositories/docker-hub-remote/limits/ceilings`;
  const ceilings = await admin.get(ceilingsUrl);
  await expectStatus(ceilings, [200], 'ceilings of docker-hub-remote');
  await expectStatus(
    await admin.put(
      ceilingsUrl,
      {
        anonymous: { uncachedBytesPerHour: 10_000_000_000, upstreamRequestsPerHour: 100 },
        signedIn: { uncachedBytesPerHour: 50_000_000_000, upstreamRequestsPerHour: 2000 },
      },
      { 'If-Match': ceilings.headers()['etag'] ?? '*' },
    ),
    [200],
    'ceilings of docker-hub-remote',
  );
}
