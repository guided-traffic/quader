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
  publicGradedPackage,
  publicPackages,
  vendorPackages,
  zipArchive,
} from './advisories';
import { edge, stateDir } from './paths';

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
  /**
   * The sections a remote delivers, each an allow rule in the delivery mode onlyAllowed (remote-curation.md R-CUR-001; a remote has no
   * include patterns, R-CUR-002); absent delivers everything.
   */
  readonly allowSections?: readonly string[];
  /** The member `vulnerabilities` of the repository document (R-VULN-002). */
  readonly vulnerabilities?: { readonly matchPublicAdvisories: 'auto' | 'always' | 'never' };
  /** `remote.downloadOnProbe` of a remote (R-REPO-065); absent is the default, on. Set on an existing repository too. */
  readonly downloadOnProbe?: boolean;
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
/** The repositories of the cached versions' renders (cached.ts): a local source and a remote of it, per format. */
export const cachedSources = { npm: 'npm-docs-source', npmCache: 'npm-docs-cache', deb: 'deb-docs-source', debCache: 'deb-docs-cache' } as const;
/**
 * A remote over the same upstream whose tag arrived by a background download (W-25, R-REPO-065): the seed asks only `HEAD` of the
 * tag `3.0`, an index of three platforms, and the remote downloads in the background the two of its list (`linux/amd64`,
 * `linux/arm64`), so that the tag is listed with the size of those two, marked partial, and no client download.
 */
export const probedCache = { key: 'image-probed', tag: '3.0' } as const;
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
  // The last member is a public registry Quader offers, not switched on: the member editor shows it as a placeholder (R-UI-189).
  { project: otherProject, key: 'containers', format: 'oci', class: 'virtual', members: ['base-images', 'docker-hub-remote', 'upstream/docker-hub'] },
  // A remote that a visitor without a session pulls through (owner, 2026-11-19): public, and bound to one namespace of the
  // upstream, because a request without a credential fills its cache.
  {
    project: otherProject,
    key: 'guidedtraffic',
    format: 'oci',
    class: 'remote',
    description: 'The guidedtraffic images of Docker Hub, cached. Open to everyone, also without signing in.',
    visibility: { artifacts: 'public', wiki: 'internal' },
    allowSections: ['guidedtraffic/*'],
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
    url: `${edge}/${otherProject}/${remoteSource.key}/${remoteSource.prefix}`,
    // Its pulls give the rows of W-24 one platform and none of an index: a probe would download the rest (R-REPO-065).
    downloadOnProbe: false,
  },
  {
    project: otherProject,
    key: probedCache.key,
    format: 'oci',
    class: 'remote',
    description: 'The images of image-source; a tag a client only asks about is downloaded in the background.',
    url: `${edge}/${otherProject}/${remoteSource.key}/${remoteSource.prefix}`,
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
  // Open-source packages everyone reads, also without a session, checked against public advisories: the vulnerabilities a visitor
  // without a session reads (R-UI-224, advisories.ts publicPackages).
  {
    project: otherProject,
    key: 'npm-open-source',
    format: 'npm',
    class: 'local',
    description: 'Open-source packages of the platform team, checked against public advisories.',
    visibility: { artifacts: 'public', wiki: 'public' },
    vulnerabilities: { matchPublicAdvisories: 'always' },
  },
  { project: otherProject, key: 'galaxy', format: 'ansible', class: 'remote' },
  { project: otherProject, key: 'ansible', format: 'ansible', class: 'virtual', members: ['collections', 'galaxy'] },
  { project: longProject, key: 'signed-release-artifacts', format: 'generic', class: 'local', description: 'Signed release archives of the customer portal.' },
  // A version a remote cached, described as a published one (npm R-NPM-050, Debian R-DEB-037; cached.ts): each remote caches from a
  // local repository of the other project, which the seed deletes once the remote cached from it: the remote alone holds the version.
  { project: otherProject, key: cachedSources.npm, format: 'npm', class: 'local', description: 'The upstream of npm-docs-cache.', visibility: { artifacts: 'public', wiki: 'public' } },
  { project: mainProject, key: cachedSources.npmCache, format: 'npm', class: 'remote', description: 'The packages of npm-docs-source, cached.', url: `${edge}/api/npm/${otherProject}/${cachedSources.npm}/` },
  { project: otherProject, key: cachedSources.deb, format: 'deb', class: 'local', description: 'The upstream of deb-docs-cache.', visibility: { artifacts: 'public', wiki: 'public' } },
  { project: mainProject, key: cachedSources.debCache, format: 'deb', class: 'remote', description: 'The packages of deb-docs-source, cached.', url: `${edge}/api/deb/${otherProject}/${cachedSources.deb}/` },
];

/** Who is what: the project roles of R-AUTHZ-026 and the system role `admin` (R-AUTHZ-010); see R-UI-067. */
export const memberships: readonly { project: string; username: string; role: 'reader' | 'developer' | 'maintainer' | 'owner' }[] = [
  { project: mainProject, username: kinds.plain, role: 'reader' },
  { project: otherProject, username: kinds.plain, role: 'reader' },
  { project: mainProject, username: kinds.power, role: 'owner' },
  { project: otherProject, username: kinds.power, role: 'developer' },
];

/** An npm version the seed publishes: its repository of the main project, its manifest's dependencies, its dist-tag. */
export interface NpmSeed {
  readonly repo: string;
  readonly name: string;
  readonly version: string;
  readonly readme?: string;
  readonly dependencies?: Record<string, string>;
  readonly devDependencies?: Record<string, string>;
  readonly tag?: string;
}

/** The packages the power user publishes, through the formats' own routes. */
export const npmPackages: readonly NpmSeed[] = [
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
  // The versions of the malware scan's states at a version (malware.ts, R-MAL-024): clean, infected, unscannable, waiting.
  { repo: 'npm-internal', name: '@acme/build-helpers', version: '2.2.0' },
  { repo: 'npm-internal', name: '@acme/build-helpers', version: '2.3.0' },
  { repo: 'npm-internal', name: '@acme/build-helpers', version: '2.4.0-rc.1' },
  { repo: 'npm-internal', name: '@acme/build-helpers', version: '2.5.0-next.1' },
];

/** The deep folder of the generic files, and the number of entries of the folder with many (more than a page of 200). */
export const deepFolder =
  'deliveries/customer-portal-frontend-static-assets-for-the-november-release/build-artifacts-of-the-nightly-pipeline/2026-11-18-nightly-build-4711';
export const manyIcons = 230;

/**
 * The files of R-GENERIC-017's renders (`notes/`): one deployed without a type, which gets the type its name says; one whose
 * deployed type wins over its name; and one whose name a shell would read as an option, a quote and a command substitution, which
 * every client's command carries as one quoted argument.
 */
export const genericNotes = {
  folder: 'notes',
  guessed: 'CHANGELOG.md',
  deployed: 'release-notes.md',
  hostile: `--output=evil it's "final" $(id).txt`,
} as const;

/**
 * `type`: the `Content-Type` of the upload; absent sends `application/octet-stream`, `null` the type of a form's body, as wget sends
 * it, which the server stores as no type (Q-GENERIC-12): Playwright sends `application/octet-stream` for a body without a type.
 */
export const genericFiles: readonly { repo: string; path: string; content: string; type?: string | null }[] = [
  { repo: 'assets', path: `${genericNotes.folder}/${genericNotes.guessed}`, content: '# Changelog\n\n## 2.5.0\n', type: null },
  { repo: 'assets', path: `${genericNotes.folder}/${genericNotes.deployed}`, content: 'Release notes\n', type: 'text/plain; charset=utf-8' },
  { repo: 'assets', path: `${genericNotes.folder}/${genericNotes.hostile}`, content: 'a name to quote\n', type: null },
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

/**
 * The images the seed pushes, each a list of images (manifests) in the order they are pushed, each image named by its tags. A
 * pipeline tags each version twice, its version and its commit (`sha-…`), as the owner's image of 2026-11-25 was (Q-UI-1130): a list
 * shows one row per image, led by its version. The images of a name share their layer and differ by a label of their config.
 */
export const images: readonly { project: string; repo: string; name: string; images: readonly (readonly string[])[] }[] = [
  // Six images: a version and its commit tag each; 1.4.0 also as 1.4, 1, latest and stable; one with only main and its commit; one
  // with a branch's long tag (the longest a tag may be is 128 characters), whose row wraps between its tags and inside the long one.
  {
    project: mainProject,
    repo: 'images',
    name: 'portal/web',
    images: [
      ['1.2.0', 'sha-3f9a2c1'],
      ['1.3.0', 'sha-8b14e07'],
      ['1.3.1', 'sha-c52d9e4'],
      ['1.4.0', '1.4', '1', 'latest', 'stable', 'sha-71e0b3a'],
      ['main', 'sha-e4a1f60'],
      ['feature-customer-portal-checkout-redesign-with-the-new-payment-provider-and-address-validation', 'sha-0d8c5b2'],
    ],
  },
  { project: mainProject, repo: 'images', name: 'portal/api', images: [['1.4.0']] },
  // Twelve images, 26 tags: more images than an opened item shows (ten) and more tags than fit, so the bound, the order of the versions
  // (1.16.6 above 1.10.1 above 1.9.0, the pre-release 2.0.0-rc.1 first) and the link to all tags are in one picture.
  {
    project: mainProject,
    repo: 'images',
    name: 'portal/worker',
    images: [
      ['1.2.8', 'sha-1a2b3c4'],
      ['1.3.0', 'sha-2b3c4d5'],
      ['1.4.0', 'sha-3c4d5e6'],
      ['1.5.2', 'sha-4d5e6f7'],
      ['1.6.0', 'sha-5e6f708'],
      ['1.7.3', 'sha-6f70819'],
      ['1.8.1', 'sha-708192a'],
      ['1.9.0', 'sha-8192a3b'],
      ['1.10.0', 'sha-92a3b4c'],
      ['1.10.1', 'sha-a3b4c5d'],
      ['1.16.6', '1.16', 'latest', 'sha-b4c5d6e'],
      ['2.0.0-rc.1', 'sha-c5d6e7f'],
    ],
  },
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
// The second version is 1.2.0 since the collection holds a fragment, a role's argument specs and a changelog (R-ANS-051 to R-ANS-053): an
// instance that holds an older 1.1.0 keeps it, and the screens read the version that has them.
export const ansiblePackage = { format: 'ansible', repo: 'collections', namespace: 'acme', name: 'platform', versions: ['1.0.0', '1.2.0'] } as const;
/** A second collection of the repository whose changelog is a CHANGELOG.md (R-ANS-053). */
export const ansibleNotesPackage = { format: 'ansible', repo: 'collections', namespace: 'acme', name: 'notes', versions: ['1.0.0'] } as const;

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
    if (r.class === 'remote' && r.url) body['remote'] = { url: r.url };
    if (r.class === 'remote' && r.allowSections) body['remote'] = { ...(body['remote'] as object), delivery: { mode: 'onlyAllowed' } };
    if (r.class === 'remote' && r.downloadOnProbe !== undefined) body['remote'] = { ...(body['remote'] as object), downloadOnProbe: r.downloadOnProbe };
    if (publicRemotes.includes(r)) body['remote'] = { ...(body['remote'] as object | undefined), availability: { mode: 'offline' } };
    if (r.vulnerabilities) body['vulnerabilities'] = r.vulnerabilities;
    if (r.class === 'virtual') {
      const target = r.members?.find((m) => repositories.find((x) => x.key === m)?.class === 'local');
      body['virtual'] = { members: r.members ?? [], deployTarget: target ?? null };
    }
    const res = await admin.post(`/api/v1/projects/${r.project}/repositories`, body);
    await expectStatus(res, [201, 409], `create repository ${r.project}/${r.key}`);
    if (res.status() === 409 && r.downloadOnProbe !== undefined) {
      // A database of an earlier run holds the repository without the setting: it is set, as the seed's pictures need it.
      const set = await admin.patch(`/api/v1/projects/${r.project}/repositories/${r.key}`, { remote: { downloadOnProbe: r.downloadOnProbe } }, { 'If-Match': '*' });
      await expectStatus(set, [200], `downloadOnProbe of ${r.project}/${r.key}`);
    }
    if (res.status() === 409 && publicRemotes.includes(r)) await setAvailability(admin, r, 'offline');
    for (const pattern of r.allowSections ?? []) {
      // A second run finds the rule: the write of an existing subject answers 200 (R-CUR-006).
      const rule = await admin.post(`/api/v1/projects/${r.project}/repositories/${r.key}/delivery-rules`, { kind: 'section', pattern, effect: 'allow' });
      await expectStatus(rule, [200, 201], `the rule ${pattern} of ${r.project}/${r.key}`);
    }
  }
}

/**
 * The remotes whose upstream is a public registry: no `url`, so the format's well-known upstream. The seed never reaches the
 * internet through them: they are offline from their creation until `publicRemotesOnline` at the end of the seed. A publish into a
 * local repository that a virtual repository shares with one of them asks it whether it already serves the name (R-REPO-048); an
 * offline remote answers "not served" without an upstream request, where an online one asked the public registry, and a registry
 * that did not answer in time made the publish a 503 `shadow_check_unavailable` and failed the seed (layout job of 2026-11-23, 18:19).
 */
export const publicRemotes: readonly RepoSeed[] = repositories.filter((r) => r.class === 'remote' && !r.url);

async function setAvailability(admin: Api, r: RepoSeed, mode: 'online' | 'offline'): Promise<void> {
  const res = await admin.patch(`/api/v1/projects/${r.project}/repositories/${r.key}`, { remote: { availability: { mode } } }, { 'If-Match': '*' });
  await expectStatus(res, [200], `${r.project}/${r.key} ${mode}`);
}

/** Puts the public registries' remotes online once nothing of the seed publishes any more: the screens show them as configured. */
export async function publicRemotesOnline(admin: Api): Promise<void> {
  for (const r of publicRemotes) await setAvailability(admin, r, 'online');
}

function rank(c: RepoSeed['class']): number {
  return c === 'local' ? 0 : c === 'remote' ? 1 : 2;
}

/**
 * The public registries of the project upstream that the seed switches on (R-UI-192): the project's page as an installation shows
 * it, some registries on and some off beside its two system repositories. None is a format's default preset, so the form of a new
 * remote opens without the line of R-UI-191, and `docker-hub` stays off for the placeholder of `containers` (R-UI-189). Switching on
 * contacts no upstream (R-REPO-068); the plain user, who is no external user, reads them at their level Internal.
 */
export const switchedOnRegistries = ['ghcr', 'quay', 'ubuntu', 'debian-security'] as const;

/** Switches the registries of `switchedOnRegistries` on; a registry that is on already answers 200 and records nothing. */
export async function seedUpstreamRegistries(admin: Api): Promise<void> {
  for (const key of switchedOnRegistries) {
    const res = await admin.put(`/api/v1/upstream-repositories/${key}/state`, { state: 'on' });
    await expectStatus(res, [200], `switch on upstream/${key}`);
  }
  // ClamAV definition updates on at Public (R-REPO-075): the client setup of upstream/clamav is the server's for that level, and the
  // plain user reads the repository in the section of the project's page (R-UI-192). The off states are the walks' (clamav.ts).
  const clamav = await admin.put('/api/v1/upstream-repositories/clamav/clients', { enabled: true, level: 'public' });
  await expectStatus(clamav, [200], 'switch on ClamAV definition updates');
}

/**
 * The registries of this installation (R-REPO-076, R-UI-195): two of OCI, one with a long name and a token service that receives the
 * credential, and one of npm, so that the admin page shows two groups and the forms of a new OCI and npm remote show their buttons
 * after the well-known ones. The list is replaced whole; `*` makes the seed a blind write on a database the render reuses.
 */
export const registryPresets = [
  {
    format: 'oci',
    displayName: 'Harbor',
    url: 'https://harbor.example-company.com/',
    extraHosts: [{ pattern: 'harbor.example-company.com', sendCredentials: true }],
  },
  {
    format: 'oci',
    displayName: 'Registry of the build farm in Frankfurt',
    url: 'https://registry.build-farm.eu-central.example-company.com/',
    extraHosts: [
      { pattern: 'auth.build-farm.eu-central.example-company.com', sendCredentials: true },
      { pattern: 'cdn.example-company.com', sendCredentials: false },
    ],
  },
  { format: 'npm', displayName: 'Verdaccio', url: 'https://npm.example-company.com/', extraHosts: [] },
] as const;

export async function seedRegistryPresets(admin: Api): Promise<void> {
  const res = await admin.put('/api/v1/settings/registry-presets', { presets: registryPresets }, { 'If-Match': '*' });
  await expectStatus(res, [200], 'the registries of this installation');
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
export function tarEntry(name: string, content: Buffer): Buffer {
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
export function tarGz(entries: readonly (readonly [string, Buffer])[]): Buffer {
  return gzipSync(Buffer.concat([...entries.map(([name, content]) => tarEntry(name, content)), Buffer.alloc(1024)]));
}

/** The body of an npm publish, as the npm client sends it: the manifest and the tarball as an attachment. */
export function npmPublishBody(edge: string, project: string, p: NpmSeed): object {
  const manifest = {
    name: p.name,
    version: p.version,
    description: `${p.name}, published by the seed of the renders`,
    license: 'MIT',
    ...(p.dependencies ? { dependencies: p.dependencies } : {}),
    ...(p.devDependencies ? { devDependencies: p.devDependencies } : {}),
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
  for (const p of [...npmPackages, ...vendorPackages, ...manyVersionPackages, ...publicPackages]) {
    const project = 'project' in p && p.project ? p.project : mainProject;
    const url = `/api/npm/${project}/${p.repo}/${p.name.replace('/', '%2f')}`;
    const res = await patient(() => ctx.put(url, { data: npmPublishBody(edge, project, p), headers: auth }));
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
      ctx.put(url, {
        data: Buffer.from(f.content),
        headers: { ...auth, 'Content-Type': f.type === null ? 'application/x-www-form-urlencoded' : (f.type ?? 'application/octet-stream') },
      }),
    );
    await expectStatus(res, [200, 201], `upload ${f.repo}/${f.path}`);
  }
  for (const f of genericFolders) {
    // An empty body with a trailing slash creates the folder; an existing one answers 200 (R-API-025).
    const res = await ctx.put(`/files/${mainProject}/${f.repo}/${f.path}/`, { data: Buffer.alloc(0), headers: auth });
    await expectStatus(res, [200, 201], `folder ${f.repo}/${f.path}`);
  }
  for (const image of images) await pushImage(ctx, edge, token, image);
  await seedImageFacts(ctx, edge, token);
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
  const open = await idOf(admin, publicGradedPackage.name, `${otherProject}/npm-open-source`);
  const want = manyVersionPackages.find((v) => v.grade === 'E' && (v.fixable ?? 0) > 0)!;
  const isGraded = async (id: string, version: string, grade: string, fixable: number | null): Promise<boolean> => {
    const res = await admin.get(`/api/v1/packages/${id}/versions/${version}/vulnerabilities`);
    if (!res.ok()) return false;
    const report = (await res.json()) as { repositories: { grade?: { grade: string; fixable?: number } }[] };
    return report.repositories.some((r) => r.grade?.grade === grade && (fixable === null || r.grade.fixable === fixable));
  };
  for (let i = 0; i < 180; i++) {
    if (
      (await isGraded(graded, gradedPackage.version, 'E', 112)) &&
      (await isGraded(many, want.version, 'E', want.fixable ?? 0)) &&
      (await isGraded(open, publicGradedPackage.version, 'C', null))
    ) {
      spreadDetections();
      publicDismissed();
      return;
    }
    if (i % 30 === 29) await expectStatus(await admin.post('/api/v1/system/tasks/vuln-sync-osv/run', {}), [202, 409], 'run the advisory sync again');
    await new Promise((r) => setTimeout(r, 1000));
  }
  throw new Error(
    `the advisory archive was not imported and evaluated within three minutes: ${gradedPackage.name} is not E with 112 fixable, or ${manyVersionsPackage.name}@${want.version} not E`,
  );
}

/**
 * The trend over 30 days (G5-54): the seed's findings are all detected within its own minutes, so the chart showed one day. The
 * findings of the many-version package are spread by SQL over the 30 days before, each on a day of its own from its id, so the bands
 * rise over the month. None is resolved by SQL: a status is the triage's, and the grades of the versions table stay as the cycle made
 * them. Only findings detected in the last day are moved, so a second seed moves nothing again.
 */
function spreadDetections(): void {
  psql(`UPDATE vuln_finding SET detected_at = now() - make_interval(days => 1 + abs(hashtext(id::text)) % 30)
         WHERE package_name = '${manyVersionsPackage.name}' AND detected_at > now() - interval '1 day';`);
}

/**
 * One Medium finding of the public package's graded version dismissed as a triager would (R-UI-224): the version page of a visitor without
 * a session shows neither its row nor a count of it, a signed-in reader's report still lists it. Once: a second seed finds it dismissed.
 */
function publicDismissed(): void {
  psql(`UPDATE vuln_finding SET status = 'dismissed', dismissal_reason = 'not_exploitable'
         WHERE id = (SELECT f.id FROM vuln_finding f JOIN repositories r ON r.id = f.repo_id
                      WHERE r.key = 'npm-open-source' AND f.package_name = '${publicGradedPackage.name}' AND f.version = '${publicGradedPackage.version}'
                        AND f.severity = 'medium' AND f.status = 'needs_triage'
                        AND NOT EXISTS (SELECT 1 FROM vuln_finding d WHERE d.repo_id = f.repo_id AND d.package_name = f.package_name AND d.status = 'dismissed')
                      ORDER BY f.advisory_id LIMIT 1);`);
}

async function idOf(admin: Api, name: string, repository = `${mainProject}/npm-vendor`): Promise<string> {
  const list = await admin.get(`/api/v1/packages?repository=${repository}&format=npm&q=${name}`);
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
export function ar(members: readonly (readonly [string, Buffer])[]): Buffer {
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

/** One example of the module {@link mountModule}: a play of about forty lines. */
function mountExample(n: number): string {
  const lines = [
    `- name: Prepare data volume ${n}`,
    '  hosts: storage',
    '  become: true',
    '  vars:',
    `    volume_label: data${n}`,
    '    mount_root: /srv/volumes',
    '  tasks:',
    `    - name: Mount volume ${n} by its label (example ${n})`,
    '      acme.platform.mount_volume:',
    '        path: "{{ mount_root }}/{{ volume_label }}"',
    "        src: 'LABEL={{ volume_label }}'",
    '        fstype: ext4',
    '        state: mounted',
    '        opts:',
    '          - noatime',
    '          - nodev',
    '        backup: true  # keep the previous fstab',
    '        retry:',
    `          count: ${n + 2}`,
    '          backoff:',
    '            seconds: 1.5',
    '            jitter: false',
    '      register: mounted',
    '',
    '    - name: Show what changed',
    '      ansible.builtin.debug:',
    '        msg: "{{ mounted.dev.name }} at {{ mounted.dev.uuid }}"',
    '      when: mounted is changed',
    '',
    '    - name: Write a marker',
    '      ansible.builtin.shell: |',
    '        set -eu',
    '        echo "volume: {{ volume_label }}" > {{ mount_root }}/.marker',
    '        sync',
    '      args:',
    '        creates: "{{ mount_root }}/.marker"',
    '',
    '    - name: Check the mount',
    '      ansible.builtin.command: findmnt --target {{ mount_root }}',
    '      changed_when: false',
  ];
  return lines.join('\n');
}

/**
 * A module as the documentation of a real collection writes it (R-ANS-048 to R-ANS-050): twenty options, suboptions three levels deep,
 * choices, defaults, aliases, a long description with the markup's macros, attributes, see-also, three examples of forty lines and
 * nested return values.
 */
function mountModule(): string {
  const more = Array.from(
    { length: 9 },
    (_, i) => `  option_${String(i + 1).padStart(2, '0')}:\n    description: An extra setting number ${i + 1} that O(state=mounted) reads.\n    type: ${['str', 'int', 'bool'][i % 3]}\n${i % 3 === 1 ? '    default: 10\n' : ''}`,
  ).join('');
  return `#!/usr/bin/python
DOCUMENTATION = r'''
module: mount_volume
short_description: Control active and configured mount points of the platform's volumes
version_added: "1.0.0"
extends_documentation_fragment:
  - acme.platform.connection
  - ansible.builtin.files
description:
  - Controls active and configured mount points in C(/etc/fstab) for the volumes of the platform, with retries for slow devices.
  - Use O(state=mounted) to mount a device and write it to the fstab, or O(state=present) to write the entry only. The options in
    O(opts) are passed to the mount command; B(never) put credentials there, use O(credentials.file) instead.
  - The module returns the device in RV(dev) and its RV(dev.name). See M(acme.platform.mount_volume) for the plain case, the lookup
    P(acme.platform.env_vars#lookup) to read the settings from the environment and the callback P(acme.platform.timer#callback) to time
    the mounts. Set E(PLATFORM_MOUNT_DEBUG) for a trace. HORIZONTALLINE
    Since version 1.1.0 a value of V(auto) for O(fstype) asks the kernel.
author:
  - Platform engineering (@platform)
  - Jane Maintainer (@jane)
requirements:
  - util-linux >= 2.36
  - python >= 3.9
notes:
  - Remounting needs O(state=remounted) and works on Linux only.
  - Refer to U(https://docs.ansible.com/ansible/latest/collections/ansible/posix/mount_module.html) for the module this follows.
seealso:
  - module: ansible.posix.mount
    description: The general module for mount points.
  - plugin: acme.platform.env_vars
    plugin_type: lookup
    description: Reads the settings of a volume from the environment.
  - name: Filesystem table
    description: The format of the fstab.
    link: https://man7.org/linux/man-pages/man5/fstab.5.html
attributes:
  check_mode:
    support: full
    description: Can run in check mode and return changed status prediction without modifying target.
  diff_mode:
    support: partial
    description: Will return details on what has changed (or possibly needs changing in check_mode), when in diff mode.
    details: Only the fstab entry is compared, not the mounted state.
  platform:
    support: full
    platforms: posix
    description: Target OS/families that can be operated against.
options:
  path:
    description: Path to the mount point (for example C(/mnt/files)).
    type: path
    required: true
    aliases: [name]
  src:
    description: Device (or NFS volume, or something else) to be mounted on O(path). Required when O(state) is V(mounted) or V(present).
    type: path
    aliases: [source, device]
  fstype:
    description: Filesystem type, or V(auto) to let the kernel decide.
    type: str
    default: auto
    version_added: "1.1.0"
  state:
    description: Whether the mount point is mounted, only in the fstab, unmounted or absent.
    type: str
    required: true
    choices: [absent, mounted, present, unmounted, remounted]
  opts:
    description: Mount options (see fstab(5), or vfstab(4) on Solaris).
    type: list
    elements: str
    default: [defaults]
  backup:
    description: Create a backup file including the timestamp information so you can get the original file back.
    type: bool
    default: false
  boot:
    description: Determines if the filesystem should be mounted on boot.
    type: bool
    default: true
  retry:
    description: How a slow device is waited for.
    type: dict
    suboptions:
      count:
        description: How often to try again.
        type: int
        default: 3
      backoff:
        description: The waiting between two tries.
        type: dict
        suboptions:
          seconds:
            description: Seconds between two tries.
            type: float
            default: 1.5
          jitter:
            description: Spread the tries over a random share of O(retry.backoff.seconds).
            type: bool
            default: false
  credentials:
    description: Where the credentials of a network volume come from.
    type: dict
    suboptions:
      file:
        description: A file with the credentials, readable by root only.
        type: path
      user:
        description: The user, when no O(credentials.file) is given.
        type: str
${more}'''

EXAMPLES = r'''
${[1, 2, 3].map(mountExample).join('\n\n')}
'''

RETURN = r'''
dev:
  description: The device that was mounted.
  returned: success
  type: dict
  contains:
    name:
      description: The name of the device.
      type: str
      sample: /dev/sdb1
    uuid:
      description: The UUID of its filesystem.
      type: str
      sample: 4f3c1a2e-9b0d-4f6a-8c55-1d2e3f4a5b6c
    usage:
      description: The space used, after the mount.
      type: dict
      version_added: "1.1.0"
      sample: {"used": 1024, "free": 4096, "ok": true, "labels": ["data", "fast"]}
backup_file:
  description: The name of the backup file of the fstab.
  returned: changed and O(backup) is V(true)
  type: str
  sample: /etc/fstab.2026-11-25@21:38:06~
'''
`;
}

/** A lookup plugin with its terms and options configured by ini, env and vars (R-ANS-049, R-ANS-050). */
const envVarsLookup = `DOCUMENTATION = r'''
name: env_vars
short_description: Read the settings of a volume from the environment
version_added: "1.1.0"
description:
  - Returns the values of the environment variables named in the terms, with O(prefix) in front.
options:
  _terms:
    description: The names of the variables.
    type: list
    elements: str
    required: true
  prefix:
    description: The prefix of every name.
    type: str
    default: PLATFORM_
    ini:
      - section: platform_lookup
        key: prefix
    env:
      - name: PLATFORM_LOOKUP_PREFIX
    vars:
      - name: platform_lookup_prefix
  default:
    description: The value of a variable that is not set.
    type: raw
    version_added: "1.1.0"
    env:
      - name: PLATFORM_LOOKUP_DEFAULT
'''

EXAMPLES = r'''
- name: Read two settings
  ansible.builtin.debug:
    msg: "{{ lookup('acme.platform.env_vars', 'VOLUME', 'ROOT', prefix='APP_') }}"
'''

RETURN = r'''
_list:
  description: The values, in the order of the terms.
  type: list
  elements: str
'''
`;

/** A callback plugin configured by ini, env and a play keyword (R-ANS-049). */
const timerCallback = `DOCUMENTATION = r'''
name: timer
type: aggregate
short_description: Adds the time of every task to the summary
version_added: "1.0.0"
description:
  - Prints the time of each task at the end of the play, sorted by O(sort_order).
requirements:
  - enable in the configuration
options:
  sort_order:
    description: The order of the summary.
    type: str
    default: descending
    choices: [descending, ascending, none]
    ini:
      - section: callback_timer
        key: sort_order
    env:
      - name: PLATFORM_TIMER_SORT_ORDER
  summary_only:
    description: Only the summary, not each task.
    type: bool
    default: false
    version_added: "1.1.0"
    ini:
      - section: callback_timer
        key: summary_only
    env:
      - name: PLATFORM_TIMER_SUMMARY_ONLY
    keyword:
      - name: timer_summary_only
'''
`;

/** The fragment the module extends (R-ANS-051): the connection's options and a note, merged into its page. */
const connectionFragment = `# -*- coding: utf-8 -*-


class ModuleDocFragment(object):
    # The options every module of the platform takes to reach its API.
    DOCUMENTATION = r'''
options:
  api_url:
    description:
      - The address of the platform's API, for example V(https://platform.example.invalid/api).
    type: str
    required: true
  api_token:
    description: A token of the platform's API. Set it from a vault, never in the play.
    type: str
    aliases: [token]
  validate_certs:
    description: Whether the certificate of the API is checked.
    type: bool
    default: true
notes:
  - The API answers within 30 seconds; a slower answer is an error.
'''
`;

/** The argument specs of the role agent (R-ANS-052): two entry points, one with nested options. */
const agentArgumentSpecs = `---
argument_specs:
  main:
    short_description: Install and start the platform's agent
    description:
      - Installs the agent from the platform's package repository and starts its service.
      - Set O(agent_tls.cert) and O(agent_tls.key) to talk to the platform over TLS.
    author:
      - Platform engineering (@platform)
    version_added: 1.2.0
    options:
      agent_version:
        description: The version of the agent's package; the newest when empty.
        type: str
      agent_port:
        description: The port the agent listens on.
        type: int
        default: 9100
      agent_tls:
        description: The agent's certificate and key.
        type: dict
        options:
          cert:
            description: The path of the certificate on the host.
            type: path
            required: true
          key:
            description: The path of the private key on the host.
            type: path
            required: true
          protocols:
            description: The TLS versions the agent accepts.
            type: list
            elements: str
            choices: [TLSv1.2, TLSv1.3]
            default: [TLSv1.3]
  upgrade:
    short_description: Upgrade the agent in place
    description:
      - Upgrades the agent and restarts it only when O(agent_restart) is true.
    options:
      agent_restart:
        description: Whether the service is restarted after the upgrade.
        type: bool
        default: true
`;

/** A changelog of antsibull-changelog (R-ANS-053): five versions, every category, a new module, plugins and a role. */
const platformChangelog = `ancestor: null
releases:
  1.0.0:
    changes:
      release_summary: First release of the platform's collection.
    modules:
    - description: Control active and configured mount points of the platform's volumes
      name: mount_volume
      namespace: ''
    release_date: '2026-01-12'
  1.0.1:
    changes:
      bugfixes:
      - mount_volume - a device name with a space is quoted in the fstab (https://git.example.invalid/acme/platform/pull/4).
      release_summary: Bugfix release.
    release_date: '2026-02-03'
  1.1.0:
    changes:
      deprecated_features:
      - mount_volume - the option \`\`opts\`\` as a string is deprecated, give a list.
      minor_changes:
      - mount_volume - the option \`\`fstype\`\` takes \`\`auto\`\`.
      - The lookup reads variables with a prefix, see \`the guide <https://git.example.invalid/acme/platform/docs>\`_.
      release_summary: Feature release.
      security_fixes:
      - mount_volume - the credentials file is written with mode 0600.
    plugins:
      callback:
      - description: Times every task and the play
        name: timer
        namespace: null
      lookup:
      - description: Read settings from the environment
        name: env_vars
        namespace: null
    release_date: '2026-03-20'
  1.1.1:
    changes:
      known_issues:
      - mount_volume - a remount of a busy NFS share can take a minute.
      release_summary: Maintenance release.
    release_date: '2026-04-02'
  1.2.0:
    changes:
      breaking_changes:
      - The collection needs ansible-core 2.15 or later.
      major_changes:
      - Every module reads the connection to the platform from one shared fragment.
      removed_features:
      - mount_volume - the option \`\`legacy_mode\`\` is removed.
      release_summary: Feature release with a role.
    objects:
      role:
      - description: Install and start the platform's agent
        name: agent
        namespace: null
    release_date: '2026-05-15'
`;

/** A collection archive with the MANIFEST.json and FILES.json ansible-galaxy builds (test/ansiblestub/ansiblegen). */
export function collection(namespace: string, name: string, version: string): Buffer {
  const files: [string, Buffer][] = [
    ['README.md', Buffer.from(`# ${namespace}.${name}\n\nRoles and modules of the platform team, version ${version}.\n`)],
    ['changelogs/changelog.yaml', Buffer.from(platformChangelog)],
    ['meta/runtime.yml', Buffer.from("requires_ansible: '>=2.15.0'\n")],
    ['plugins/callback/timer.py', Buffer.from(timerCallback)],
    ['plugins/doc_fragments/connection.py', Buffer.from(connectionFragment)],
    ['plugins/lookup/env_vars.py', Buffer.from(envVarsLookup)],
    ['plugins/modules/mount_volume.py', Buffer.from(mountModule())],
    ['roles/agent/tasks/main.yml', Buffer.from('---\n- name: Install the agent\n  ansible.builtin.package:\n    name: acme-agent\n')],
    ['roles/agent/meta/argument_specs.yml', Buffer.from(agentArgumentSpecs)],
    ['roles/agent/meta/main.yml', Buffer.from("---\ngalaxy_info:\n  author: platform\n  description: the agent\n  license: MIT\n  min_ansible_version: '2.15'\n  platforms: []\ndependencies: []\n")],
  ];
  const dirs = [
    'changelogs',
    'meta',
    'plugins',
    'plugins/callback',
    'plugins/doc_fragments',
    'plugins/lookup',
    'plugins/modules',
    'roles',
    'roles/agent',
    'roles/agent/meta',
    'roles/agent/tasks',
  ];
  return archive(namespace, name, version, files, dirs);
}

/** The archive of {@link ansibleNotesPackage}: a README and a CHANGELOG.md (R-ANS-053). */
function notesCollection(namespace: string, name: string, version: string): Buffer {
  const changelog = [
    '# Changelog',
    '',
    '## 1.0.0 (2026-05-20)',
    '',
    '### Added',
    '',
    '- The role `notes` writes release notes into the play\'s log.',
    '- A filter `notes_format` that renders a note as Markdown.',
    '',
    '### Fixed',
    '',
    '- A note with a `<tag>` is written as text.',
    '',
    '## 0.9.0 (2026-04-30)',
    '',
    '- First preview.',
    '',
  ].join('\n');
  const files: [string, Buffer][] = [
    ['CHANGELOG.md', Buffer.from(changelog)],
    ['README.md', Buffer.from(`# ${namespace}.${name}\n\nRelease notes in the play, version ${version}.\n`)],
    ['meta/runtime.yml', Buffer.from("requires_ansible: '>=2.15.0'\n")],
  ];
  return archive(namespace, name, version, files, ['meta']);
}

/** The tar.gz of a collection: its files and directories, with the FILES.json and MANIFEST.json ansible-galaxy writes. */
function archive(namespace: string, name: string, version: string, files: [string, Buffer][], dirs: string[]): Buffer {
  const sha = (b: Buffer): string => createHash('sha256').update(b).digest('hex');
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

/** The collections of {@link ansiblePackage} and {@link ansibleNotesPackage}, published as ansible-galaxy does (R-ANS-008). */
async function publishCollections(ctx: APIRequestContext, auth: Record<string, string>): Promise<void> {
  const all = [
    ...ansiblePackage.versions.map((v) => ({ pkg: ansiblePackage, version: v, build: collection })),
    ...ansibleNotesPackage.versions.map((v) => ({ pkg: ansibleNotesPackage, version: v, build: notesCollection })),
  ];
  for (const { pkg, version, build } of all) {
    const tgz = build(pkg.namespace, pkg.name, version);
    const res = await patient(() =>
      ctx.post(`/api/ansible/${otherProject}/${pkg.repo}/v3/artifacts/collections/`, {
        headers: auth,
        multipart: {
          sha256: createHash('sha256').update(tgz).digest('hex'),
          file: { name: `${pkg.namespace}-${pkg.name}-${version}.tar.gz`, mimeType: 'application/gzip', buffer: tgz },
        },
      }),
    );
    await expectStatus(res, [200, 201, 202, 409], `ansible publish ${pkg.namespace}.${pkg.name} ${version}`);
  }
}

/**
 * Pushes the images of a name through `/v2/` with a registry token (R-OCI-005): each a small image (one layer, a config, a manifest)
 * under each of its tags. Every image of the name has the same layer (`bastions.ts` names its blob's digest), and the first keeps the
 * config and the manifest it always had; the others' configs carry the commit as a label, so each is a manifest of its own. A few
 * hundred bytes per image.
 */
async function pushImage(ctx: APIRequestContext, edge: string, pat: string, image: (typeof images)[number]): Promise<void> {
  const repo = `${image.project}/${image.repo}/${image.name}`;
  const bearer = await registryToken(ctx, edge, pat, repo);
  const auth = { Authorization: `Bearer ${bearer}` };
  const layer = gzipSync(tarEntry('etc/acme-release', Buffer.from(`${image.name}\n`)));
  const digestOf = (b: Buffer): string => `sha256:${createHash('sha256').update(b).digest('hex')}`;
  const upload = async (blob: Buffer): Promise<void> => {
    const digest = digestOf(blob);
    if ((await ctx.head(`/v2/${repo}/blobs/${digest}`, { headers: auth })).status() === 200) return;
    const start = await ctx.post(`/v2/${repo}/blobs/uploads/`, { headers: auth });
    await expectStatus(start, [202], `start upload ${repo}`);
    const location = new URL(start.headers()['location'] ?? '', edge);
    location.searchParams.set('digest', digest);
    const put = await ctx.put(location.pathname + location.search, {
      data: blob,
      headers: { ...auth, 'Content-Type': 'application/octet-stream' },
    });
    await expectStatus(put, [201], `upload blob ${repo}`);
  };
  await upload(layer);
  for (const [i, tags] of image.images.entries()) {
    const labels: Record<string, string> = { 'org.opencontainers.image.title': image.name };
    const commit = tags.find((t) => t.startsWith('sha-'));
    if (i > 0 && commit) labels['org.opencontainers.image.revision'] = commit.slice(4);
    const config = Buffer.from(JSON.stringify({ architecture: 'amd64', os: 'linux', config: { Labels: labels }, rootfs: { type: 'layers', diff_ids: [] } }));
    await upload(config);
    const manifest = Buffer.from(
      JSON.stringify({
        schemaVersion: 2,
        mediaType: 'application/vnd.oci.image.manifest.v1+json',
        config: { mediaType: 'application/vnd.oci.image.config.v1+json', digest: digestOf(config), size: config.length },
        layers: [{ mediaType: 'application/vnd.oci.image.layer.v1.tar+gzip', digest: digestOf(layer), size: layer.length }],
        annotations: { 'org.opencontainers.image.description': `The ${image.name} image of the renders` },
      }),
    );
    for (const tag of tags) {
      const res = await patient(() =>
        ctx.put(`/v2/${repo}/manifests/${tag}`, {
          data: manifest,
          headers: { ...auth, 'Content-Type': 'application/vnd.oci.image.manifest.v1+json' },
        }),
      );
      await expectStatus(res, [201], `push ${repo}:${tag}`);
    }
  }
}

/**
 * The images of an image's facts (formats/oci.md R-OCI-039 to R-OCI-041) in `images`: `facts/web-api`, an index of six platforms and
 * a BuildKit attestation, whose platforms have twenty layers each and a configuration with thirty environment entries of long values,
 * labels with addresses and a history of forty steps with long `RUN` commands, under three tags; `facts/bare`, an image without any
 * annotation or label; and two Helm charts, one with its README and one deprecated without (R-OCI-042). Every layer is a tar of a few bytes; a run again
 * pushes the same bytes.
 */
export const factsImages = { index: 'facts/web-api', tags: ['2.4.1', '2.4', 'latest'], bare: 'facts/bare', chart: 'charts/web-api', oldChart: 'charts/web-api-legacy' } as const;

export async function seedImageFacts(ctx: APIRequestContext, edge: string, pat: string): Promise<void> {
  const digestOf = (b: Buffer): string => `sha256:${createHash('sha256').update(b).digest('hex')}`;
  const push = async (name: string): Promise<{ blob: (b: Buffer) => Promise<string>; manifest: (ref: string, body: Buffer, type: string) => Promise<void> }> => {
    const repo = `${mainProject}/images/${name}`;
    const auth = { Authorization: `Bearer ${await registryToken(ctx, edge, pat, repo)}` };
    return {
      blob: async (b: Buffer): Promise<string> => {
        const digest = digestOf(b);
        if ((await ctx.head(`/v2/${repo}/blobs/${digest}`, { headers: auth })).status() === 200) return digest;
        const start = await ctx.post(`/v2/${repo}/blobs/uploads/`, { headers: auth });
        await expectStatus(start, [202], `start upload ${repo}`);
        const location = new URL(start.headers()['location'] ?? '', edge);
        location.searchParams.set('digest', digest);
        const put = await ctx.put(location.pathname + location.search, { data: b, headers: { ...auth, 'Content-Type': 'application/octet-stream' } });
        await expectStatus(put, [201], `upload blob ${repo}`);
        return digest;
      },
      manifest: async (ref: string, body: Buffer, type: string): Promise<void> => {
        const res = await patient(() => ctx.put(`/v2/${repo}/manifests/${ref}`, { data: body, headers: { ...auth, 'Content-Type': type } }));
        await expectStatus(res, [201], `push ${repo}:${ref}`);
      },
    };
  };
  const json = (v: unknown): Buffer => Buffer.from(JSON.stringify(v));
  const ociManifest = 'application/vnd.oci.image.manifest.v1+json';
  const layerType = 'application/vnd.oci.image.layer.v1.tar+gzip';

  // facts/web-api: six platforms and an attestation.
  const api = await push(factsImages.index);
  const platforms: Record<string, string>[] = [
    { os: 'linux', architecture: 'amd64' },
    { os: 'linux', architecture: 'arm64', variant: 'v8' },
    { os: 'linux', architecture: 'arm', variant: 'v7' },
    { os: 'linux', architecture: 'ppc64le' },
    { os: 'linux', architecture: 's390x' },
    { os: 'linux', architecture: '386' },
  ];
  const created = '2026-11-20T09:14:07.512Z';
  const env = [
    'PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin:/opt/web-api/bin:/opt/web-api/node_modules/.bin',
    'NODE_VERSION=24.11.1',
    'NODE_ENV=production',
    'APP_HOME=/opt/web-api',
    `JAVA_TOOL_OPTIONS=${['-XX:+UseContainerSupport', '-XX:MaxRAMPercentage=75.0', '-XX:+ExitOnOutOfMemoryError', '-Dfile.encoding=UTF-8', '-Duser.timezone=UTC', '-Djava.security.egd=file:/dev/./urandom'].join(' ')}`,
    `NODE_OPTIONS=--max-old-space-size=1536 --enable-source-maps --unhandled-rejections=strict --dns-result-order=ipv4first --trace-warnings`,
    'TZ=UTC',
    'LANG=C.UTF-8',
    ...Array.from({ length: 22 }, (_, i) => `WEB_API_FEATURE_${String(i + 1).padStart(2, '0')}=${i % 3 === 0 ? 'enabled' : `https://config.example-company.com/web-api/feature-flags/${i + 1}?environment=production&region=eu-central-1&fallback=disabled`}`),
  ];
  const labels = {
    'org.opencontainers.image.title': 'web-api',
    'org.opencontainers.image.description': 'The web API of the portal: REST and GraphQL behind the edge, with the session store and the job queue.',
    'org.opencontainers.image.source': 'https://git.example-company.com/portal/web-api',
    'org.opencontainers.image.url': 'https://portal.example-company.com/developers/web-api',
    'org.opencontainers.image.documentation': 'https://docs.example-company.com/portal/web-api/2.4/',
    'org.opencontainers.image.licenses': 'Apache-2.0',
    'org.opencontainers.image.vendor': 'Example Company',
    'org.opencontainers.image.revision': '71e0b3a5c2d94f18a6b0e3c7d9f2a4b6c8e0f1a3',
    'org.opencontainers.image.version': '2.4.1',
    'org.opencontainers.image.base.name': 'docker.io/library/node:24-bookworm-slim',
    'com.example-company.team': 'portal-platform',
    'com.example-company.oncall': 'https://oncall.example-company.com/schedules/portal-platform',
  };
  const runs = [
    'apt-get update && apt-get install -y --no-install-recommends ca-certificates curl tini dumb-init libssl3 libstdc++6 && rm -rf /var/lib/apt/lists/* /var/cache/apt/archives/*.deb && update-ca-certificates --fresh',
    'groupadd --system --gid 10001 web-api && useradd --system --uid 10001 --gid web-api --home-dir /opt/web-api --shell /usr/sbin/nologin web-api',
    'npm ci --omit=dev --no-audit --no-fund --ignore-scripts && npm cache clean --force && find /opt/web-api/node_modules -name "*.md" -delete -o -name "*.ts" -not -name "*.d.ts" -delete',
    'node scripts/build.mjs --target=node24 --minify --sourcemap=external --define:process.env.NODE_ENV=\\"production\\" --outdir=/opt/web-api/dist',
  ];
  const history: { created: string; created_by: string; comment?: string; empty_layer?: boolean }[] = [];
  for (let i = 0; i < 40; i++) {
    const empty = i % 2 === 1;
    history.push({
      created,
      created_by: empty
        ? `ENV WEB_API_FEATURE_${String(i).padStart(2, '0')}=enabled`
        : `RUN /bin/sh -c ${runs[(i / 2) % runs.length]} # step ${i / 2 + 1}`,
      comment: 'buildkit.dockerfile.v0',
      ...(empty ? { empty_layer: true } : {}),
    });
  }
  const children: { mediaType: string; digest: string; size: number; platform: Record<string, string>; annotations?: Record<string, string> }[] = [];
  for (const p of platforms) {
    const arch = [p.architecture, p.variant].filter(Boolean).join('');
    const layers = [];
    for (let i = 0; i < 20; i++) {
      const layer = gzipSync(tarEntry(`opt/web-api/layer-${i}-${arch}`, Buffer.from(`${arch} ${i}\n`)));
      layers.push({ mediaType: layerType, digest: await api.blob(layer), size: layer.length });
    }
    const config = json({
      ...p,
      created,
      config: {
        User: 'web-api:web-api',
        ExposedPorts: { '8080/tcp': {}, '9464/tcp': {} },
        Env: env,
        Entrypoint: ['/usr/bin/tini', '--', 'node'],
        Cmd: ['/opt/web-api/dist/server.js', '--listen=0.0.0.0:8080', '--metrics=0.0.0.0:9464'],
        WorkingDir: '/opt/web-api',
        Labels: labels,
        Volumes: { '/var/lib/web-api/uploads': {} },
        StopSignal: 'SIGTERM',
        Healthcheck: { Test: ['CMD', 'node', '/opt/web-api/dist/health.js'], Interval: 30_000_000_000, Timeout: 5_000_000_000, Retries: 3 },
      },
      rootfs: { type: 'layers', diff_ids: [] },
      history,
    });
    const manifest = json({
      schemaVersion: 2,
      mediaType: ociManifest,
      config: { mediaType: 'application/vnd.oci.image.config.v1+json', digest: await api.blob(config), size: config.length },
      layers,
    });
    const digest = digestOf(manifest);
    await api.manifest(digest, manifest, ociManifest);
    children.push({ mediaType: ociManifest, digest, size: manifest.length, platform: p });
  }
  // BuildKit's attestation of the first platform: an image manifest of the platform unknown/unknown whose layer is an in-toto statement.
  const statement = json({ _type: 'https://in-toto.io/Statement/v0.1', predicateType: 'https://slsa.dev/provenance/v0.2', subject: [{ name: factsImages.index, digest: { sha256: children[0]!.digest.slice(7) } }] });
  const empty = json({});
  const attestation = json({
    schemaVersion: 2,
    mediaType: ociManifest,
    config: { mediaType: 'application/vnd.oci.image.config.v1+json', digest: await api.blob(empty), size: empty.length },
    layers: [{ mediaType: 'application/vnd.in-toto+json', digest: await api.blob(statement), size: statement.length, annotations: { 'in-toto.io/predicate-type': 'https://slsa.dev/provenance/v0.2' } }],
  });
  const attDigest = digestOf(attestation);
  await api.manifest(attDigest, attestation, ociManifest);
  children.push({
    mediaType: ociManifest,
    digest: attDigest,
    size: attestation.length,
    platform: { os: 'unknown', architecture: 'unknown' },
    annotations: { 'vnd.docker.reference.digest': children[0]!.digest, 'vnd.docker.reference.type': 'attestation-manifest' },
  });
  const index = json({
    schemaVersion: 2,
    mediaType: 'application/vnd.oci.image.index.v1+json',
    manifests: children,
    annotations: {
      'org.opencontainers.image.description': 'The web API of the portal, for six platforms.',
      'org.opencontainers.image.created': created,
    },
  });
  for (const tag of factsImages.tags) await api.manifest(tag, index, 'application/vnd.oci.image.index.v1+json');

  // facts/bare: no annotation, no label, one layer.
  const bare = await push(factsImages.bare);
  const bareLayer = gzipSync(tarEntry('bin/hello', Buffer.from('hello\n')));
  const bareConfig = json({ architecture: 'amd64', os: 'linux', config: { Cmd: ['/bin/hello'] }, rootfs: { type: 'layers', diff_ids: [] } });
  const bareManifest = json({
    schemaVersion: 2,
    mediaType: ociManifest,
    config: { mediaType: 'application/vnd.oci.image.config.v1+json', digest: await bare.blob(bareConfig), size: bareConfig.length },
    layers: [{ mediaType: layerType, digest: await bare.blob(bareLayer), size: bareLayer.length }],
  });
  await bare.manifest('1.0', bareManifest, ociManifest);

  // facts/chart: a Helm chart (R-OCI-042): its Chart.yaml as the config, a package with a README of a table and code and values of 200
  // lines, two dependencies; facts/chart-old: a deprecated chart without a README.
  const readme = [
    '# web-api',
    '',
    'The portal\'s web API for Kubernetes: a Deployment, its Service, an Ingress and the job queue\'s worker.',
    '',
    '## Installing',
    '',
    '```console',
    'helm install web-api oci://registry.example-company.com/charts/web-api --version 0.3.0 --set ingress.enabled=true',
    '```',
    '',
    '## Parameters',
    '',
    '| Key | Type | Default | Description |',
    '|---|---|---|---|',
    '| `replicaCount` | int | `2` | How many pods serve the API. |',
    '| `image.repository` | string | `registry.example-company.com/portal/web-api` | The image. |',
    '| `ingress.enabled` | bool | `false` | Whether an Ingress routes the host to the Service. |',
    '| `resources.limits.memory` | string | `1536Mi` | The memory limit of a pod. |',
    '',
  ].join('\n');
  const values = ['# Default values for web-api.', 'replicaCount: 2', 'image:', '  repository: registry.example-company.com/portal/web-api', '  tag: ""', '  pullPolicy: IfNotPresent'];
  for (let i = 1; values.length < 200; i++) {
    values.push(`# Feature ${i}: switched by the operator of the installation, documented at https://docs.example-company.com/portal/web-api/features#feature-${i}`);
    values.push(`feature${i}:`, `  enabled: ${i % 2 === 0}`, `  timeoutSeconds: ${i * 5}`);
  }
  const chartConfig = (name: string, deprecated: boolean): Buffer =>
    json({
      apiVersion: 'v2',
      name,
      version: '0.3.0',
      appVersion: '2.4.1',
      description: deprecated ? 'The web API of the portal, as a chart of the old layout.' : "The portal's web API for Kubernetes.",
      type: 'application',
      kubeVersion: '>=1.29.0-0',
      keywords: ['portal', 'api', 'graphql'],
      home: 'https://portal.example-company.com/developers/web-api',
      sources: ['https://git.example-company.com/portal/web-api'],
      maintainers: [{ name: 'Portal platform team', email: 'portal-platform@example-company.com' }],
      dependencies: [
        { name: 'redis', version: '^19.6.0', repository: 'oci://registry.example-company.com/charts', condition: 'redis.enabled' },
        { name: 'postgresql', version: '~15.5.0', repository: 'oci://registry.example-company.com/charts', alias: 'db' },
      ],
      annotations: { 'artifacthub.io/license': 'Apache-2.0' },
      ...(deprecated ? { deprecated: true } : {}),
    });
  for (const [name, deprecated] of [[factsImages.chart, false], [factsImages.oldChart, true]] as const) {
    const chart = await push(name);
    const dir = name.split('/').pop()!;
    const files: [string, Buffer][] = [[`${dir}/Chart.yaml`, Buffer.from(`apiVersion: v2\nname: ${dir}\nversion: 0.3.0\n`)]];
    if (!deprecated) files.push([`${dir}/README.md`, Buffer.from(readme)]);
    files.push([`${dir}/values.yaml`, Buffer.from(values.join('\n') + '\n')]);
    const content = tarGz(files);
    const config = chartConfig(dir, deprecated);
    const manifest = json({
      schemaVersion: 2,
      mediaType: ociManifest,
      config: { mediaType: 'application/vnd.cncf.helm.config.v1+json', digest: await chart.blob(config), size: config.length },
      layers: [{ mediaType: 'application/vnd.cncf.helm.chart.content.v1.tar+gzip', digest: await chart.blob(content), size: content.length }],
      annotations: { 'org.opencontainers.image.title': dir, 'org.opencontainers.image.version': '0.3.0', 'org.opencontainers.image.created': created },
    });
    await chart.manifest('0.3.0', manifest, ociManifest);
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
  const index = async (tag: string, arches: readonly string[] = ['amd64', 'arm64']): Promise<string[]> => {
    const children = [];
    for (const arch of arches) children.push(await image(arch, tag));
    const body = Buffer.from(
      JSON.stringify({
        schemaVersion: 2,
        mediaType: 'application/vnd.oci.image.index.v1+json',
        manifests: children.map((c, i) => ({
          mediaType: 'application/vnd.oci.image.manifest.v1+json',
          digest: c.digest,
          size: c.size,
          platform: { os: 'linux', architecture: arches[i] },
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
  await index(probedCache.tag, ['amd64', 'arm64', 's390x']);

  // The pulls of a client through the remote, as the power user (a developer of the project reads it).
  const cached = `${otherProject}/${remoteCache.key}/${remoteImage.name}`;
  const pull = { Authorization: `Bearer ${await registryToken(ctx, edge, pat, cached, 'pull')}`, Accept: 'application/vnd.oci.image.index.v1+json, application/vnd.oci.image.manifest.v1+json' };
  for (const ref of [remoteImage.single, remoteImage.partial, partial[0]!, remoteImage.unknown]) {
    const res = await patient(() => ctx.get(`/v2/${cached}/manifests/${ref}`, { headers: pull }));
    await expectStatus(res, [200], `pull ${cached}:${ref} through the remote`);
  }

  // A client that only asks about a tag (W-25): the HEAD of the manifest of 3.0 through image-probed starts its background download,
  // which a worker of the instance runs; the seed waits until the remote lists the tag, which it does once the download stored the
  // content (R-API-081). A database of an earlier run lists it already.
  const probed = `${otherProject}/${probedCache.key}/${remoteImage.name}`;
  const ask = { Authorization: `Bearer ${await registryToken(ctx, edge, pat, probed, 'pull')}`, Accept: pull.Accept };
  const head = await patient(() => ctx.head(`/v2/${probed}/manifests/${probedCache.tag}`, { headers: ask }));
  await expectStatus(head, [200], `HEAD ${probed}:${probedCache.tag} through the remote`);
  const auth = { Authorization: `Bearer ${pat}` };
  const deadline = Date.now() + 120_000;
  // On a new database the package exists only once a pull through a remote has recorded it, which is a moment after the pull
  // was answered: the lookup is asked until it finds the package, within the same deadline as the tag.
  let lookup = await patient(() => ctx.get(`/api/v1/packages/lookup?format=oci&name=${encodeURIComponent(remoteImage.name)}`, { headers: auth }));
  while (lookup.status() === 404 && Date.now() < deadline) {
    await new Promise((r) => setTimeout(r, 1000));
    lookup = await patient(() => ctx.get(`/api/v1/packages/lookup?format=oci&name=${encodeURIComponent(remoteImage.name)}`, { headers: auth }));
  }
  await expectStatus(lookup, [200], `look up ${remoteImage.name}`);
  const id = ((await lookup.json()) as { id: string }).id;
  for (;;) {
    const res = await ctx.get(`/api/v1/packages/${id}/versions/${probedCache.tag}`, { headers: auth });
    if (res.status() === 200) break;
    if (Date.now() > deadline) throw new Error(`${probed}:${probedCache.tag} is not listed 120 s after its probe: ${res.status()} ${(await res.text()).slice(0, 300)}`);
    await new Promise((r) => setTimeout(r, 1000));
  }
}

/** The handshake of a registry client: the challenge of `/v2/`, then the token service with the PAT as the password. */
export async function registryToken(ctx: APIRequestContext, edge: string, pat: string, repo: string, actions = 'pull,push'): Promise<string> {
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
/**
 * The policies and the CI mappings whose own pages the renders show (R-AUTHZ-004, R-CI-009): a policy of the main project and one of
 * the installation, a mapping of the CI issuer's system tier that grants the installation's policy and one of the main project that
 * grants the project's.
 */
export const projectPolicy = 'release-pipeline-readers-of-the-customer-portal';
export const instancePolicy = 'read-everything-for-the-yearly-audit';
export const systemMapping = 'protected-release-branches-of-every-project';
export const projectMapping = 'web-frontend-release-tags';
/** The anonymous callers of the renders: an IPv6 prefix with a rule of its own, a blocked address, a small one (R-LIM-002). */
export const anonymousV6 = '2001:db8:1:2::/64';
export const blockedAddress = '198.51.100.23';
export const quietAddress = '203.0.113.9';
/** The certificate subject of a ClamAV installation that fetches the definitions with its client certificate (R-CLAMAV-010). */
export const certificateSubject = 'CN=clamav-scan-01.example.com,O=Example GmbH';

/**
 * The UI development profile's PostgreSQL container (deploy/ui-dev/compose.yaml). UI_RENDERS_POSTGRES names another one: a
 * worktree that renders against a stack of its own beside the running profile, whose database the seed must not write
 * (docs/developer/web-ui.md#local-ui-development).
 */
const postgresContainer = process.env['UI_RENDERS_POSTGRES'] ?? 'quader-ui-dev-postgres';

/**
 * Runs SQL in the UI development profile's database through its container (deploy/ui-dev/compose.yaml), as the profile's
 * database owner. It is the renders' seam for what no request makes in the seed's time: 24 hours and 30 days of usage, the CI
 * issuer of a CI project, a block. Quader has no path that writes any of it.
 */
export function psql(sql: string): void {
  const password = readFileSync(join(stateDir, 'secrets', 'db-pass'), 'utf8').trim();
  execFileSync(
    'docker',
    ['exec', '-i', '-e', `PGPASSWORD=${password}`, postgresContainer, 'psql', '-q', '-v', 'ON_ERROR_STOP=1', 'host=localhost dbname=quader user=quader sslmode=require'],
    { input: sql, stdio: ['pipe', 'inherit', 'inherit'] },
  );
}

/** A text as an SQL literal. */
export const lit = (s: string): string => `'${s.replace(/'/g, "''")}'`;

/** The ID of a technical token by its account's stored name and its name, as SQL (R-AUTH-045). */
const tokenRef = (account: string, name: string): string =>
  `(SELECT t.id::text FROM technical_token t JOIN principal p ON p.id = t.service_account_id WHERE p.name = ${lit(account)} AND t.name = ${lit(name)})`;
const userRef = (name: string): string => `(SELECT id::text FROM principal WHERE kind = 'user' AND name = ${lit(name)})`;
export const repoId = (project: string, key: string): string =>
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
  readonly kind: 'user' | 'technical_token' | 'ci' | 'certificate' | 'anonymous' | 'system';
  /** The subject's reference, as SQL. */
  readonly ref: string;
  /**
   * The credential column (R-LIM-027): a user's session or personal token, a certificate subject's certificate serial, empty for the
   * other kinds; as SQL.
   */
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
    // A ClamAV installation by its certificate's subject, its certificate renewed this month: two serials (Q-CLAMAV-12).
    { kind: 'certificate', ref: lit(certificateSubject), credential: `'5c2f9e0b71a4d3e8'`, place: `'instance'`, repo: repoId('upstream', 'clamav'),
      day: [96, 2_300 * MB, 0, 0, 0], hour: [4, 120 * MB, 0, 0, 0], profile: 'technical' },
    { kind: 'certificate', ref: lit(certificateSubject), credential: `'0a7d33c19f6e2b45'`, place: `'instance'`, repo: repoId('upstream', 'clamav'),
      day: [12, 110 * MB, 0, 0, 0], hour: [0, 0, 0, 0, 0], profile: 'technical' },
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
 * The policies and the CI mappings of the renders (`projectPolicy` to `projectMapping`), created through the API after the CI issuer
 * (`seedLimits`); a second run finds them (409) and leaves them. Each has a long name and a description, conditions of more than one
 * leaf and several principals, so that their editors have something to lay out.
 */
export async function seedPoliciesAndMappings(admin: Api): Promise<void> {
  await expectStatus(
    await admin.post(`/api/v1/projects/${mainProject}/policies`, {
      name: projectPolicy,
      description: 'Lets the release pipeline and the web team read the internal packages and the released files of the customer portal.',
      resources: { repositories: ['npm-internal', 'assets', 'npm-*'] },
      entries: [
        { principal: `group:${group}`, actions: ['read'] },
        { principal: `user:${kinds.plain}`, actions: ['read'] },
        { principal: `sa:${mainProject}/${projectServiceAccount}`, actions: ['write'] },
      ],
    }),
    [201, 409],
    `policy ${projectPolicy}`,
  );
  await expectStatus(
    await admin.post('/api/v1/permission-policies', {
      name: instancePolicy,
      description: 'Read access to every repository of every project for the external auditors, for the four weeks of the yearly audit.',
      resources: { repositories: ['@any'] },
      entries: [
        { principal: `user:${kinds.auditor}`, actions: ['read'] },
        { principal: `sa:${serviceAccount}`, actions: ['read'] },
      ],
    }),
    [201, 409],
    `policy ${instancePolicy}`,
  );
  // The issuer is public: a condition pins one of its project IDs at the top of its `all` (R-CI-010).
  const match = {
    all: [
      { claim: 'project_id', in: [ciProject.id, '4712', '4713'] },
      { claim: 'ref_protected', equals: true },
      { claim: 'ref_type', in: ['branch', 'tag'] },
      { claim: 'namespace_path', glob: 'acme/customer-portal/*' },
    ],
  };
  await expectStatus(
    await admin.post(`/api/v1/ci/issuers/${ciIssuer}/mappings`, {
      name: systemMapping,
      description: 'Every protected branch and tag of the customer portal reads every repository for its builds.',
      priority: 10,
      match,
      grants: { policies: [instancePolicy], groups: [group] },
    }),
    [201, 409],
    `CI mapping ${systemMapping}`,
  );
  await expectStatus(
    await admin.post(`/api/v1/projects/${mainProject}/ci/mappings`, {
      issuer: ciIssuer,
      name: projectMapping,
      description: 'The tag pipelines of the web frontend publish its packages.',
      priority: 20,
      match: { all: [{ claim: 'project_id', equals: ciProject.id }, { claim: 'ref_type', equals: 'tag' }, { claim: 'ref_protected', equals: true }] },
      grants: { policies: [projectPolicy] },
    }),
    [201, 409],
    `CI mapping ${mainProject}/${projectMapping}`,
  );
}

/**
 * The renders run in Audit (R-LIM-026): an instance starts in Enforce, which would refuse the seed's own heavy requests and every
 * builder's after them; in Audit everything is counted and shown, and Enforce is pictured through the page's routes.
 */
export async function seedAuditMode(admin: Api): Promise<void> {
  await expectStatus(await admin.patch('/api/v1/limits/settings', { mode: 'audit' }, { 'If-Match': '*' }), [200], 'the limits mode Audit');
}

/**
 * The download limits of the renders (R-UI-177 to R-UI-186): an own profile with a long name, rules at the three places (an IPv6
 * prefix, a group and a CI project at the instance, the power user and the project's heavy mirror token at the main project, every
 * anonymous caller at a remote), a maximum of the project with the long name, and the ceilings of a public remote. The power user's
 * hourly request figure is low on purpose: the seed's own requests pass it, so the overview shows a caller at the limit in Audit.
 * The mode is Audit (seedAuditMode): the database is shared, and Enforce is rendered through the page's routes. A second run adds
 * nothing.
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
        figures: { requestsPerSecond: 50, requestsBurst: 200, requestsPerHour: 5000, cachedBytesPerHour: 50_000_000_000, parallelTransfers: 6 },
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
