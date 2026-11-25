import { Provider } from '@angular/core';
import {
  ContentsQuery,
  DocsQuery,
  PackageDocEntry,
  PackageDocFile,
  PackageDocTree,
  ClientSetup,
  ClientSetupApi,
  ClientSetupQuery,
  ClientSnippet,
  PackageFilters,
  PackageReadme,
  PackageSummary,
  PackageVersion,
  PackageVersionContents,
  PackageVersionSummary,
  PackagesApi,
  VersionFilters,
} from '../packages-api';
import { DocsFixture, commonFixture, docFile, docTree } from './fakes-ansible-docs';
import { Page, PageFetcher } from '../pagination';
import { ApiError } from '../problem';
import { Repository, refOf } from '../repositories-api';

/*
 * In-memory fakes of the packages and client setup services (R-UI-047 component tier). The setup
 * fake builds the npm snippets the way R-API-030 and R-UI-017 describe them, and the curl snippets
 * of the generic repository the way R-GENERIC-016 does, so a screen is tested against realistic
 * text: placeholders, the environment reference, quoting and the scheme-less key.
 */

const BASE = 'https://quader.example.net';

function notFound(): ApiError {
  return new ApiError(404, { code: 'not_found', detail: 'not found' }, 'rid-404');
}

function pages<T>(all: readonly T[]): PageFetcher<T> {
  return async (cursor, limit): Promise<Page<T>> => {
    const start = cursor === null ? 0 : Number(cursor);
    const end = start + limit;
    return { items: all.slice(start, end), nextCursor: end < all.length ? String(end) : null };
  };
}

/** The generic repository of the fakes, the first repository of the catalog fake. */
export const GENERIC_REPOSITORY: Repository = {
  key: 'generic-local',
  project: 'web',
  class: 'local',
  format: 'generic',
  description: 'Installers and build outputs',
};

/** The npm repositories of the fakes: a local, a remote, a virtual with and one without a target. */
export const NPM_REPOSITORIES: readonly Repository[] = [
  { key: 'npm-local', project: 'web', class: 'local', format: 'npm', description: 'Internal packages' },
  { key: 'npm-remote', project: 'web', class: 'remote', format: 'npm', description: 'Public registry cache' },
  { key: 'npm-all', project: 'web', class: 'virtual', format: 'npm', description: 'Everything for npm clients' },
  { key: 'npm-readonly', project: 'web', class: 'virtual', format: 'npm', description: 'Read-only view' },
];

/** The OCI repositories of the fakes: the format registers the class local only (R-OCI-001). */
export const OCI_REPOSITORIES: readonly Repository[] = [
  { key: 'oci-local', project: 'web', class: 'local', format: 'oci', description: 'Internal images' },
];

/** The NuGet repositories of the fakes: a local, a remote and a virtual one that deploys to the local one. */
export const NUGET_REPOSITORIES: readonly Repository[] = [
  { key: 'nuget-local', project: 'web', class: 'local', format: 'nuget', description: 'Internal packages' },
  { key: 'nuget-remote', project: 'web', class: 'remote', format: 'nuget', description: 'nuget.org cache' },
  { key: 'nuget-all', project: 'web', class: 'virtual', format: 'nuget', description: 'Everything for NuGet clients' },
];

/** The Ansible repositories of the fakes: the one the package of {@link ansibleFixture} is in, and the remote of {@link roleFixture}. */
export const ANSIBLE_REPOSITORIES: readonly Repository[] = [
  { key: 'ansible-local', project: 'web', class: 'local', format: 'ansible', description: 'Internal collections' },
  { key: 'galaxy', project: 'web', class: 'remote', format: 'ansible', description: 'Ansible Galaxy cache' },
];

interface FakeVersion extends PackageVersion {
  readme: PackageReadme | null;
}

function sha(n: number): string {
  return n.toString(16).padStart(64, '0');
}

function version(
  id: string,
  name: string,
  v: string,
  over: Partial<FakeVersion> = {},
): FakeVersion {
  const file = `${name}/-/${name.split('/').pop()}-${v}.tgz`;
  return {
    packageId: id,
    format: 'npm',
    name,
    version: v,
    prerelease: v.includes('-'),
    publishedAt: '2026-10-20T09:12:44.512Z',
    licenses: ['MIT'],
    sizeBytes: 48213,
    sizePartial: false,
    downloadCount: 410,
    lastDownloadedAt: '2026-11-17T16:40:02.118Z',
    hasReadme: true,
    metadata: {
      // The tags are the package's: every version's metadata carries all of them (internal/formats/npm/view.go).
      distTags: name === '@team-a/ui' ? { latest: '1.4.0', next: '1.5.0-beta.1' } : { latest: v },
      dependencies: { lit: '^3.1.0' },
      devDependencies: { vitest: '^3.0.0' },
      authors: ['Team A <team-a@example.com>'],
      homepage: 'https://example.com/team-a/ui',
      repositoryUrl: 'git+https://example.com/team-a/ui.git',
      bugsUrl: 'https://example.com/team-a/ui/issues',
      engines: { node: '>=22' },
    },
    repositories: [
      { key: 'web/npm-local', class: 'local', publishedAt: '2026-10-20T09:12:44.512Z', available: true },
      { key: 'web/npm-all', class: 'virtual', publishedAt: '2026-10-20T09:12:44.512Z', available: true },
    ],
    files: [
      {
        path: file,
        sizeBytes: 48213,
        sha256: sha(0x5be0c1d2),
        downloadUrl: `${BASE}/api/npm/web/npm-local/${file}`,
        contentUrl: `/api/v1/projects/web/repositories/npm-local/content/${file}`,
      },
    ],
    readme: {
      contentType: 'text/markdown',
      html: `<h1>${name}</h1>\n<p>Shared UI components.</p>`,
      truncated: false,
      externalImagesBlocked: 0,
    },
    ...over,
  };
}

/**
 * A NuGet version as the server's view adapter describes it (formats/nuget.md R-NUGET-007): the ID
 * keeps its original casing, `listed` is explicit, `dependencyGroups` carry the target framework
 * verbatim, `authors` is the list the server splits at commas and `homepage` is the project URL
 * (Q-NUGET-48, internal/formats/nuget/view.go).
 */
function nugetVersion(v: string, over: Partial<FakeVersion> = {}): FakeVersion {
  const file = `contoso.logging/${v}/contoso.logging.${v}.nupkg`;
  return {
    packageId: 'pkg_01JBQ7ZZZZZZ',
    format: 'nuget',
    name: 'Contoso.Logging',
    version: v,
    prerelease: v.includes('-'),
    publishedAt: '2026-10-25T00:00:00.000Z',
    licenses: ['MIT'],
    sizeBytes: 20480,
    sizePartial: false,
    downloadCount: 12,
    lastDownloadedAt: null,
    hasReadme: true,
    metadata: {
      id: 'Contoso.Logging',
      listed: true,
      title: 'Contoso Logging',
      authors: ['Contoso Team', 'Ada Lovelace'],
      homepage: 'https://example.com/contoso/logging',
      minClientVersion: '4.3.0',
      packageTypes: ['Dependency'],
      licenseUrl: 'https://licenses.nuget.org/MIT',
      repositoryUrl: 'https://example.com/contoso/logging.git',
      tags: ['logging', 'diagnostics'],
      dependencyGroups: [
        {
          targetFramework: 'net8.0',
          dependencies: [
            { id: 'Microsoft.Extensions.Logging.Abstractions', range: '[8.0.0, )' },
            { id: 'System.Text.Json', range: '[8.0.4, )' },
          ],
        },
        { targetFramework: '.NETStandard2.0', dependencies: [{ id: 'System.Text.Json', range: '[6.0.0, )' }] },
        { targetFramework: 'net462', dependencies: [] },
      ],
    },
    repositories: [{ key: 'web/nuget-local', class: 'local', publishedAt: '2026-10-25T00:00:00.000Z', available: true }],
    files: [
      {
        path: file,
        sizeBytes: 20480,
        sha256: sha(0x7ac3),
        downloadUrl: `${BASE}/api/nuget/web/nuget-local/v3/flatcontainer/${file}`,
        contentUrl: `/api/v1/projects/web/repositories/nuget-local/content/${file}`,
      },
    ],
    readme: {
      contentType: 'text/markdown',
      html: '<h1>Contoso.Logging</h1>\n<p>Structured logging for Contoso services.</p>',
      truncated: false,
      externalImagesBlocked: 0,
    },
    ...over,
  };
}

const digestOf = (n: number): string => 'sha256:' + n.toString(16).padEnd(64, 'f');
const OCI_SINGLE_DIGEST = digestOf(0xa4d64);
const OCI_INDEX_DIGEST = digestOf(0x1d3a);

/** The package of the OCI fakes. It is not in the default list of {@link FakePackagesApi}; a test adds it with {@link ociFixture}. */
export const OCI_PACKAGE: PackageSummary = {
  id: 'pkg_01JBQ8OCIOCI',
  format: 'oci',
  name: 'team/app',
  description: null,
  latestVersion: '1.2.0',
  keywords: [],
  repositories: ['web/oci-local'],
  createdAt: '2026-11-01T00:00:00.000Z',
  updatedAt: '2026-11-05T00:00:00.000Z',
  versionCount: 2,
  downloadCount: 0,
};

/**
 * An OCI tag as the view adapter describes it (formats/oci.md R-OCI-031, internal/formats/oci/view.go). A tag has no README and no
 * license. Its one file is the lead item `<name>/_tags/<tag>`, the bytes of the manifest the tag points at, so its SHA-256 is the
 * manifest's digest; the manifest item is not linked to the version (InsertVersion links the lead item only, OCI never calls LinkItem).
 * The version's `sizeBytes` is the sum of its linked items, the manifest document's bytes, not the content size of the metadata.
 */
function ociVersion(tag: string, metadata: Record<string, unknown>, manifestSize: number, over: Partial<FakeVersion> = {}): FakeVersion {
  const hex = String(metadata['digest']).replace('sha256:', '');
  const file = (path: string): PackageVersion['files'][number] => ({
    path,
    sizeBytes: manifestSize,
    sha256: hex,
    downloadUrl: `${BASE}/api/v1/projects/web/repositories/oci-local/content/${path}`,
    contentUrl: `/api/v1/projects/web/repositories/oci-local/content/${path}`,
  });
  return {
    packageId: OCI_PACKAGE.id,
    format: 'oci',
    name: OCI_PACKAGE.name,
    version: tag,
    prerelease: false,
    publishedAt: '2026-11-05T00:00:00.000Z',
    licenses: [],
    sizeBytes: manifestSize,
    sizePartial: false,
    downloadCount: 0,
    lastDownloadedAt: null,
    hasReadme: false,
    metadata,
    repositories: [{ key: 'web/oci-local', class: 'local', publishedAt: '2026-11-05T00:00:00.000Z', available: true }],
    files: [file(`team/app/_tags/${tag}`)],
    readme: null,
    ...over,
  };
}

const MANIFEST = 'application/vnd.oci.image.manifest.v1+json';

/**
 * The OCI package with a single-platform tag (`1.1.0`, which has an artifact type: the media type of its config) and a multi-platform
 * index (`1.2.0`, which has none): two images, two provenance attestations that are both `unknown/unknown`, and a child the index
 * names without a platform (`""` on the wire, the COALESCE of the rows).
 */
export function ociFixture(fake: FakePackagesApi): void {
  fake.packages.push(OCI_PACKAGE);
  const children = [
    { platform: 'linux/amd64', digest: digestOf(0xa4d64), mediaType: MANIFEST, size: 31_457_280 },
    { platform: 'linux/arm64', digest: digestOf(0xa4c64), mediaType: MANIFEST, size: 31_457_000 },
    { platform: 'unknown/unknown', digest: digestOf(0xa77e1), mediaType: MANIFEST, size: 280 },
    { platform: 'unknown/unknown', digest: digestOf(0xa77e2), mediaType: MANIFEST, size: 281 },
    { platform: '', digest: digestOf(0xb1a4c), mediaType: MANIFEST, size: 99 },
  ];
  fake.versionList.push(
    ociVersion(
      '1.1.0',
      { digest: OCI_SINGLE_DIGEST, mediaType: MANIFEST, size: 31_457_280, artifactType: 'application/vnd.oci.image.config.v1+json' },
      482,
      { publishedAt: '2026-11-01T00:00:00.000Z' },
    ),
    ociVersion(
      '1.2.0',
      {
        digest: OCI_INDEX_DIGEST,
        mediaType: 'application/vnd.oci.image.index.v1+json',
        size: children.reduce((n, c) => n + c.size, 0),
        platforms: children,
      },
      1024,
    ),
  );
}

/**
 * The OCI package `team/app` as a pipeline pushes it (the owner's image of 2026-11-25, Q-UI-1130): each image a manifest of its own,
 * named by its tags (a version, its commit tag `sha-…`, `latest` on the newest). The first image is the newest push; `versionCount`
 * counts the tags, as the list's row does. `signed` gives the image of that index a verified signature.
 */
export function ociImagesFixture(fake: FakePackagesApi, images: readonly (readonly string[])[], signed: readonly number[] = []): void {
  const tags = images.reduce((n, i) => n + i.length, 0);
  fake.packages.push({ ...OCI_PACKAGE, latestVersion: images[0]?.[0] ?? '', versionCount: tags });
  images.forEach((image, i) => {
    const pushed = new Date(Date.UTC(2026, 10, 20, 12) - i * 3_600_000).toISOString();
    for (const tag of image) {
      fake.versionList.push(
        ociVersion(tag, { digest: digestOf(0x1000 + i), mediaType: MANIFEST, size: 1024 }, 512, {
          publishedAt: pushed,
          prerelease: /^\d.*-/.test(tag),
          ...(signed.includes(i) ? { signatures: { state: 'verified_internal', stale: false } } : {}),
        }),
      );
    }
  });
}

/** The Debian repositories of the fakes: a local, a remote and a virtual one. */
export const DEB_REPOSITORIES: readonly Repository[] = [
  { key: 'deb-local', project: 'web', class: 'local', format: 'deb', description: 'Internal packages' },
  { key: 'deb-remote', project: 'web', class: 'remote', format: 'deb', description: 'Debian archive cache' },
  { key: 'deb-all', project: 'web', class: 'virtual', format: 'deb', description: 'Everything for APT clients' },
];

/** The package of the Debian fakes. It is not in the default list of {@link FakePackagesApi}; a test adds it with {@link debFixture}. */
export const DEB_PACKAGE: PackageSummary = {
  id: 'pkg_01JBQ9DEBDEB',
  format: 'deb',
  name: 'hello',
  description: 'example package based on GNU hello',
  latestVersion: '2.10-3',
  keywords: [],
  repositories: ['web/deb-local'],
  createdAt: '2026-11-01T00:00:00.000Z',
  updatedAt: '2026-11-08T00:00:00.000Z',
  versionCount: 2,
  downloadCount: 0,
};

/**
 * A Debian version as the view adapter describes it (formats/debian.md R-DEB-030, internal/formats/deb/view.go): the architectures of the
 * files, the coordinates as `suite/component` and the control fields it sends, those the version has. There is no README.
 */
function debVersion(v: string, metadata: Record<string, unknown>, over: Partial<FakeVersion> = {}): FakeVersion {
  const file = (arch: string): PackageVersion['files'][number] => {
    const path = `pool/main/h/hello/hello_${v}_${arch}.deb`;
    return {
      path,
      sizeBytes: 56_132,
      sha256: sha(arch === 'amd64' ? 0x3d1 : 0x3d2),
      downloadUrl: `${BASE}/api/deb/web/deb-local/${path}`,
      contentUrl: `/api/v1/projects/web/repositories/deb-local/content/${path}`,
    };
  };
  return {
    packageId: DEB_PACKAGE.id,
    format: 'deb',
    name: 'hello',
    version: v,
    prerelease: false,
    publishedAt: '2026-11-08T00:00:00.000Z',
    licenses: [],
    sizeBytes: 112_264,
    sizePartial: false,
    downloadCount: 0,
    lastDownloadedAt: null,
    hasReadme: false,
    metadata,
    repositories: [{ key: 'web/deb-local', class: 'local', publishedAt: '2026-11-08T00:00:00.000Z', available: true }],
    files: [file('amd64'), file('arm64')],
    readme: null,
    ...over,
  };
}

/** Adds the Debian package `hello` with `2.10-3` (full control data, two architectures, one coordinate) and `2.9-1` (a bare version). */
export function debFixture(fake: FakePackagesApi): void {
  fake.packages.push(DEB_PACKAGE);
  fake.versionList.push(
    debVersion('2.9-1', { architectures: ['amd64'], coordinates: ['stable/main'] }, { publishedAt: '2026-10-01T00:00:00.000Z', files: [] }),
    debVersion('2.10-3', {
      architectures: ['amd64', 'arm64'],
      coordinates: ['stable/main'],
      maintainer: 'Jane Maintainer <jane@example.com>',
      section: 'devel',
      priority: 'optional',
      installedSize: '280',
      homepage: 'https://www.gnu.org/software/hello/',
      source: 'hello (2.10-3)',
      multiArch: 'foreign',
      relations: {
        depends: [[{ name: 'libc6', op: '>=', version: '2.34' }], [{ name: 'python3', arch: 'any' }, { name: 'python3-minimal' }]],
        recommends: [[{ name: 'hello-doc' }]],
        provides: [[{ name: 'greeter', op: '=', version: '2.10' }]],
      },
      relationsText: { breaks: 'hello-old [amd64' },
      longDescription: [
        { text: 'The GNU hello program produces a familiar, friendly greeting.' },
        { text: 'It also lists:' },
        { text: ' * a greeting\n * a farewell', verbatim: true },
      ],
    }),
  );
  fake.contentsFixtures.set(`${DEB_PACKAGE.id}@2.10-3`, {
    state: 'available',
    repository: 'web/deb-local',
    architecture: 'amd64',
    architectures: ['amd64', 'arm64'],
    readAt: '2026-11-25T21:14:02.118Z',
    changelog: {
      file: 'changelog.Debian.gz',
      text: 'hello (2.10-3) unstable; urgency=medium\n\n  * Rebuild.\n\n -- Jane Maintainer <jane@example.com>  Mon, 03 Mar 2025 10:00:00 +0000\n',
      truncated: true,
    },
    copyright: { text: 'Format: https://www.debian.org/doc/packaging-manuals/copyright-format/1.0/\n', truncated: false, licenses: ['GPL-3+'] },
    files: {
      total: 3,
      kept: 3,
      matched: 3,
      offset: 0,
      items: [
        { path: '/usr/bin/hello', size: 26936, kind: 'file' },
        { path: '/usr/bin/hi', kind: 'link', target: 'hello' },
        { path: '/usr/share/doc/hello/changelog.Debian.gz', size: 512, kind: 'file' },
      ],
    },
  });
}

/** The package of the Ansible fakes. It is not in the default list of {@link FakePackagesApi}; a test adds it with {@link ansibleFixture}. */
export const ANSIBLE_PACKAGE: PackageSummary = {
  id: 'pkg_01JBQ9ANSANS',
  format: 'ansible',
  name: 'acme.tools',
  description: 'Tools of the Acme playbooks',
  latestVersion: '1.2.0',
  keywords: ['tools', 'linux'],
  repositories: ['web/ansible-local'],
  createdAt: '2026-11-01T00:00:00.000Z',
  updatedAt: '2026-11-08T00:00:00.000Z',
  versionCount: 2,
  downloadCount: 0,
};

/**
 * An Ansible collection version as the view adapter describes it (formats/ansible.md R-ANS-026): `requiresAnsible`, the
 * dependencies on other collections, the authors and the links; the licences and the tags are the generic fields.
 */
function ansibleVersion(v: string, metadata: Record<string, unknown>, over: Partial<FakeVersion> = {}): FakeVersion {
  const path = `collections/acme/tools/acme-tools-${v}.tar.gz`;
  return {
    packageId: ANSIBLE_PACKAGE.id,
    format: 'ansible',
    name: 'acme.tools',
    version: v,
    prerelease: false,
    publishedAt: '2026-11-08T00:00:00.000Z',
    licenses: [],
    sizeBytes: 20_480,
    sizePartial: false,
    downloadCount: 0,
    lastDownloadedAt: null,
    hasReadme: false,
    metadata,
    repositories: [{ key: 'web/ansible-local', class: 'local', publishedAt: '2026-11-08T00:00:00.000Z', available: true }],
    files: [
      {
        path,
        sizeBytes: 20_480,
        sha256: sha(0x4a1),
        downloadUrl: `${BASE}/api/ansible/web/ansible-local/v3/artifacts/collections/acme-tools-${v}.tar.gz`,
        contentUrl: `/api/v1/projects/web/repositories/ansible-local/content/${path}`,
      },
    ],
    readme: null,
    ...over,
  };
}

/** Adds the collection `acme.tools` with `1.2.0` (full manifest data) and `1.0.0` (a bare version). */
export function ansibleFixture(fake: FakePackagesApi): void {
  fake.packages.push(ANSIBLE_PACKAGE);
  fake.versionList.push(
    ansibleVersion('1.0.0', {}, { publishedAt: '2026-10-01T00:00:00.000Z' }),
    ansibleVersion('1.2.0', {
      requiresAnsible: '>=2.15.0',
      dependencies: { 'community.general': '>=8.0.0' },
      authors: ['Jane Maintainer'],
      documentationUrl: 'https://example.com/acme/tools/docs',
      homepage: 'https://example.com/acme/tools',
      repositoryUrl: 'https://example.com/acme/tools',
      bugsUrl: 'https://example.com/acme/tools/issues',
    }, { licenses: ['Apache-2.0', 'MIT'] }),
  );
}

/** The role `geerlingguy.pip` a remote of a Galaxy read (formats/ansible.md R-ANS-056): the package `role/16`, shown by its owner and name. */
export const ROLE_PACKAGE: PackageSummary = {
  id: 'pkg_01JBQ9ROLE16',
  format: 'ansible',
  name: 'geerlingguy.pip',
  key: 'role/16',
  description: 'Pip (Python package manager) for Linux.',
  latestVersion: '2.0.0',
  keywords: ['python', 'pip', 'development'],
  repositories: ['web/galaxy'],
  createdAt: '2026-11-20T00:00:00.000Z',
  updatedAt: '2026-11-24T00:00:00.000Z',
  versionCount: 1,
  downloadCount: 2,
};

/**
 * Adds the role {@link ROLE_PACKAGE} with `2.0.0`: the role's facts in `metadata.role` and the GitHub repository as its link, its README
 * the upstream's HTML (`readme` the state of the server's README; `available` gives it one). `over` changes the version.
 */
export function roleFixture(fake: FakePackagesApi, readme: 'available' | 'queued' | 'absent' | 'unavailable' | 'none' = 'available', over: Partial<FakeVersion> = {}): void {
  fake.packages.push(ROLE_PACKAGE);
  const path = 'roles/16/2.0.0.tar.gz';
  fake.versionList.push({
    packageId: ROLE_PACKAGE.id,
    format: 'ansible',
    name: 'geerlingguy.pip',
    version: '2.0.0',
    prerelease: false,
    publishedAt: '2026-11-24T00:00:00.000Z',
    licenses: [],
    sizeBytes: 6_144,
    sizePartial: false,
    downloadCount: 2,
    lastDownloadedAt: '2026-11-24T10:00:00.000Z',
    hasReadme: readme === 'available',
    metadata: {
      role: {
        id: 16, owner: 'geerlingguy', name: 'pip', packageName: 'role/16', readme, upstreamDownloads: 30_832_123, branch: 'master',
        dependencies: ['geerlingguy.repo-epel'], ...(readme === 'unavailable' ? { readmeReason: 'too_large' } : {}),
      },
      repositoryUrl: 'https://github.com/geerlingguy/ansible-role-pip',
    },
    repositories: [{ key: 'web/galaxy', class: 'remote', publishedAt: '2026-11-24T00:00:00.000Z', available: true }],
    files: [
      {
        path,
        sizeBytes: 6_144,
        sha256: sha(0x16),
        downloadUrl: `${BASE}/api/ansible/web/galaxy/v1/roles/archives/16/2.0.0.tar.gz`,
        contentUrl: `/api/v1/projects/web/repositories/galaxy/content/${path}`,
      },
    ],
    readme:
      readme === 'available'
        ? { contentType: 'text/html', html: '<h1>Ansible Role: Pip</h1><p>An Ansible Role that installs Pip on Linux.</p>', truncated: false, externalImagesBlocked: 0 }
        : null,
    ...over,
  });
}

/**
 * Gives `acme.tools@1.2.0` the documentation of {@link commonFixture} (or `fx`): the version announces it in `metadata.docs` and has a
 * README, as a version that the generator has read does (R-ANS-036). Call it after {@link ansibleFixture}.
 */
export function ansibleDocsFixture(fake: FakePackagesApi, fx: DocsFixture = commonFixture(), version = '1.2.0'): DocsFixture {
  const v = fake.versionList.find((x) => x.packageId === ANSIBLE_PACKAGE.id && x.version === version);
  if (!v) throw new Error(`no version ${version}`);
  fake.docFixtures.set(`${ANSIBLE_PACKAGE.id}@${version}`, fx);
  v.hasReadme = fx.files.some((f) => f.name === 'README.md');
  v.metadata = {
    ...v.metadata,
    docs: {
      state: fx.state,
      ...(fx.state === 'available' ? { source: fx.source, entries: fx.entries.length } : {}),
    },
  };
  return fx;
}

export class FakePackagesApi extends PackagesApi {
  packages: PackageSummary[] = [
    {
      id: 'pkg_01JBQ6V7S3K9',
      format: 'npm',
      name: '@team-a/ui',
      description: 'Shared UI components',
      latestVersion: '1.4.0',
      keywords: ['ui', 'components'],
      repositories: ['web/npm-local', 'web/npm-all'],
      createdAt: '2026-09-02T08:00:12.201Z',
      updatedAt: '2026-10-20T09:12:44.512Z',
      versionCount: 3,
      downloadCount: 1832,
    },
    {
      id: 'pkg_01JBQ7A1B2C3',
      format: 'npm',
      name: 'left-pad',
      description: 'Pads a string on the left',
      latestVersion: '1.3.0',
      keywords: ['string'],
      repositories: ['web/npm-remote', 'web/npm-all'],
      createdAt: '2026-08-11T12:00:00.000Z',
      updatedAt: '2026-08-11T12:00:00.000Z',
      versionCount: 1,
      downloadCount: 77,
    },
    {
      id: 'pkg_01JBQ7ZZZZZZ',
      format: 'nuget',
      name: 'Contoso.Logging',
      description: null,
      latestVersion: '2.3.1',
      keywords: [],
      repositories: ['web/nuget-local'],
      createdAt: '2026-10-01T00:00:00.000Z',
      updatedAt: '2026-10-25T00:00:00.000Z',
      versionCount: 2,
      downloadCount: 0,
    },
  ];
  versionList: FakeVersion[] = [
    version('pkg_01JBQ6V7S3K9', '@team-a/ui', '1.3.0', { publishedAt: '2026-09-02T08:00:12.201Z', downloadCount: 900, lastDownloadedAt: null }),
    version('pkg_01JBQ6V7S3K9', '@team-a/ui', '1.4.0'),
    version('pkg_01JBQ6V7S3K9', '@team-a/ui', '1.5.0-beta.1', {
      publishedAt: '2026-10-24T10:00:00.000Z',
      downloadCount: 3,
      lastDownloadedAt: '2026-11-02T07:30:00.000Z',
      hasReadme: false,
      readme: null,
    }),
    version('pkg_01JBQ7A1B2C3', 'left-pad', '1.3.0', { hasReadme: false, readme: null }),
    nugetVersion('2.3.1'),
    nugetVersion('2.2.0', {
      publishedAt: '2026-10-01T00:00:00.000Z',
      hasReadme: false,
      readme: null,
      metadata: { listed: false, authors: 'Contoso Team', dependencyGroups: [] },
    }),
  ];
  listFilters: PackageFilters[] = [];
  /** Rejects the next `list` page once. */
  listError: ApiError | null = null;
  readmeCalls: string[] = [];
  /** The documentation by `<packageId>@<version>`; a version without an entry answers 404 (R-ANS-036). */
  docFixtures = new Map<string, DocsFixture>();
  /** What the reader of a Debian data archive kept, by `<packageId>@<version>`; a version without one is `pending` (R-DEB-039). */
  contentsFixtures = new Map<string, PackageVersionContents>();
  contentsCalls: string[] = [];
  /** Each call of the three documentation operations: `tree`, `tree?q=…`, `entry:<type>/<name>`, `file:<name>`. */
  docCalls: string[] = [];
  /** The refusal the next call of a documentation operation throws once. */
  docFailure: ApiError | null = null;
  /** The repository each documentation call asked for, in call order (`''`: none). */
  docRepositories: string[] = [];

  override list(filters: PackageFilters): PageFetcher<PackageSummary> {
    this.listFilters.push(filters);
    const q = filters.q?.toLowerCase();
    // Each page reads the packages as they are then, as the server does: a list read again in place (R-UI-213) sees a push.
    const read = (): PackageSummary[] => {
      const items = this.packages
        .filter((p) => !filters.format?.length || filters.format.includes(p.format))
        .filter((p) => !filters.repository?.length || p.repositories.some((r) => filters.repository?.includes(r)))
        .filter((p) => !q || p.name.toLowerCase().includes(q) || (p.description ?? '').toLowerCase().includes(q))
        .filter((p) => !filters.namePrefix || p.name.startsWith(filters.namePrefix))
        .filter((p) => !filters.keyword || p.keywords.includes(filters.keyword))
        .filter((p) => !filters.createdFrom || p.createdAt >= filters.createdFrom)
        .filter((p) => !filters.createdTo || p.createdAt < filters.createdTo);
      const sort = filters.sort ?? 'name';
      const desc = sort.startsWith('-');
      const field = sort.replace('-', '') as 'name' | 'createdAt' | 'updatedAt';
      return [...items].sort((a, b) => (desc ? -1 : 1) * a[field].localeCompare(b[field]));
    };
    return async (cursor, limit) => {
      if (this.listError) {
        const e = this.listError;
        this.listError = null;
        throw e;
      }
      return pages(read())(cursor, limit);
    };
  }

  override async lookup(format: string, name: string): Promise<PackageSummary> {
    // The address names a package by its key where it has one (an Ansible role's role/<id>, R-ANS-056).
    const found = this.packages.find((p) => p.format === format && (p.key ?? p.name) === name) ?? this.packages.find((p) => p.format === format && p.name === name);
    if (!found) throw notFound();
    return found;
  }

  /** The filters of every read of a version list, in order. */
  readonly versionFilters: VersionFilters[] = [];
  /** Every page asked of a version list, in order: its cursor and its limit. */
  readonly versionPages: { readonly cursor: string | null; readonly limit: number }[] = [];
  /**
   * How many entries each next page of a version list answers, in order, as a server whose read budget ended (R-API-010, Q-API-432):
   * a page may be short or empty while its cursor is set. Once they are used up, a page is whole again.
   */
  versionPageSizes: number[] = [];

  override versions(packageId: string, filters: VersionFilters = {}): PageFetcher<PackageVersionSummary> {
    this.versionFilters.push(filters);
    // The server's order (Q-API-431): the versions the format ranks, highest first, then the strings it does not rank (an image's
    // `latest`, `main`, `sha-…`) newest first by their time. A version here is what the fixtures use: digits and dots, a suffix.
    const ranked = (v: string): boolean => /^v?\d+(\.\d+)*([-+].*)?$/.test(v);
    const byVersion = (a: PackageVersionSummary, b: PackageVersionSummary): number => {
      if (ranked(a.version) !== ranked(b.version)) return ranked(a.version) ? -1 : 1;
      if (!ranked(a.version)) return b.publishedAt.localeCompare(a.publishedAt) || a.version.localeCompare(b.version);
      return b.version.localeCompare(a.version, undefined, { numeric: true });
    };
    const order = (a: PackageVersionSummary, b: PackageVersionSummary): number => {
      switch (filters.sort ?? '-version') {
        case 'publishedAt':
          return a.publishedAt.localeCompare(b.publishedAt);
        case '-publishedAt':
          return b.publishedAt.localeCompare(a.publishedAt);
        case 'version':
          return -byVersion(a, b);
        default:
          return byVersion(a, b);
      }
    };
    const digestOf = (v: FakeVersion): string | undefined => (typeof v.metadata['digest'] === 'string' ? v.metadata['digest'] : undefined);
    const withDigest = filters.include?.includes('digest') || filters.group === 'content';
    // Each page reads the versions as they are then, as the server does: a list read again in place (R-UI-213) sees a push.
    const read = (): PackageVersionSummary[] => {
      const rows = this.versionList
        .filter((v) => v.packageId === packageId)
        .filter((v) => filters.prerelease === undefined || v.prerelease === filters.prerelease)
        .map((v) => {
          const digest = digestOf(v);
          const summary: PackageVersionSummary = {
            version: v.version,
            prerelease: v.prerelease,
            publishedAt: v.publishedAt,
            sizeBytes: v.sizeBytes,
            sizePartial: v.sizePartial,
            repositories: v.repositories.map((r) => r.key),
            downloadCount: v.downloadCount,
            lastDownloadedAt: v.lastDownloadedAt,
            ...(v.signatures ? { signatures: v.signatures } : {}),
            ...(withDigest && digest ? { digest } : {}),
            ...(filters.include?.includes('state') && typeof v.metadata['state'] === 'string' ? { state: v.metadata['state'] } : {}),
            ...(filters.include?.includes('kind') && typeof v.metadata['kind'] === 'string' ? { kind: v.metadata['kind'] } : {}),
          };
          return { digest, summary };
        })
        .sort((a, b) => order(a.summary, b.summary));
      if (filters.group !== 'content') return rows.map((r) => r.summary);
      // The list by content (Q-API-432): an entry per digest at its first version in the order, the others its aliases (at most 100);
      // a version that names no content is an entry of its own.
      const groups = new Map<string, string[]>();
      const entries: { lead: PackageVersionSummary; digest: string | undefined }[] = [];
      for (const { digest, summary } of rows) {
        const group = digest === undefined ? undefined : groups.get(digest);
        if (group) {
          group.push(summary.version);
          continue;
        }
        if (digest !== undefined) groups.set(digest, []);
        entries.push({ lead: summary, digest });
      }
      return entries.map(({ lead, digest }) => {
        const others = digest === undefined ? [] : groups.get(digest)!;
        return { ...lead, aliases: others.slice(0, 100), aliasesMore: others.length > 100 };
      });
    };
    return async (cursor, limit) => {
      this.versionPages.push({ cursor, limit });
      const all = read();
      const start = cursor === null ? 0 : Number.parseInt(cursor, 10);
      const size = this.versionPageSizes.shift();
      const end = start + (size === undefined ? limit : Math.min(size, limit));
      // A short page of a budget that ended keeps a cursor while the list goes on, an empty one too; the server's cursor is the walk's
      // position, which moved by what it read, so even an empty page's cursor is a new one (here: the position and the request).
      return { items: all.slice(start, end), nextCursor: end < all.length ? `${end}.${this.versionPages.length}` : null };
    };
  }

  override async version(packageId: string, v: string): Promise<PackageVersion> {
    const found = this.versionList.find((x) => x.packageId === packageId && x.version === v);
    if (!found) throw notFound();
    const { readme: _readme, ...rest } = found;
    void _readme;
    return rest;
  }

  override async readme(packageId: string, v: string): Promise<PackageReadme | null> {
    this.readmeCalls.push(`${packageId}@${v}`);
    return this.versionList.find((x) => x.packageId === packageId && x.version === v)?.readme ?? null;
  }

  private doc(call: string, packageId: string, v: string, repository: string | undefined): DocsFixture | null {
    this.docCalls.push(call);
    this.docRepositories.push(repository ?? '');
    if (this.docFailure) {
      const e = this.docFailure;
      this.docFailure = null;
      throw e;
    }
    return this.docFixtures.get(`${packageId}@${v}`) ?? null;
  }

  override async docs(packageId: string, v: string, query: DocsQuery = {}): Promise<PackageDocTree | null> {
    const fx = this.doc(query.q ? `tree?q=${query.q}` : 'tree', packageId, v, query.repository);
    return fx ? docTree(fx, query.q) : null;
  }

  override async contents(packageId: string, v: string, query: ContentsQuery = {}): Promise<PackageVersionContents> {
    this.contentsCalls.push(`${packageId}@${v}${query.q ? `?q=${query.q}` : ''}`);
    const fx = this.contentsFixtures.get(`${packageId}@${v}`);
    if (!fx) return { state: 'pending', repository: 'web/deb-local', architecture: 'amd64', architectures: ['amd64'] };
    if (!fx.files) return fx;
    const q = (query.q ?? '').toLowerCase();
    const matching = fx.files.items.filter((f) => q === '' || f.path.toLowerCase().includes(q));
    const offset = query.offset ?? 0;
    const items = matching.slice(offset, offset + (query.limit ?? 200));
    return { ...fx, files: { ...fx.files, matched: q === '' ? fx.files.kept : matching.length, offset, items } };
  }

  override async docEntry(packageId: string, v: string, type: string, name: string, repository?: string): Promise<PackageDocEntry> {
    const fx = this.doc(`entry:${type}/${name}`, packageId, v, repository);
    const found = fx?.entries.find((e) => e.type === type && e.name === name);
    if (!found) throw notFound();
    return found;
  }

  override async docFile(packageId: string, v: string, file: string, repository?: string): Promise<PackageDocFile> {
    const fx = this.doc(`file:${file}`, packageId, v, repository);
    const found = fx ? docFile(fx, file) : null;
    if (!found) throw notFound();
    return found;
  }
}

/** The examples each template declares (`Examples`, `PackageExample` of the Go templates, ADR-208), by snippet id. */
const SNIPPET_EXAMPLES: Readonly<Record<string, readonly string[]>> = {
  'docker.install': ['myapp:1.0'],
  'podman.install': ['myapp:1.0'],
  'docker.publish': ['myapp:1.0'],
  'docker.publish.buildx': ['myapp:1.0'],
  'podman.publish': ['myapp:1.0'],
  'dotnet.publish.shell': ['bin/Release/Acme.Widgets.1.2.3.nupkg'],
  'dotnet.publish.powershell': ['bin/Release/Acme.Widgets.1.2.3.nupkg'],
  'nuget.publish.cmd': ['Acme.Widgets.1.2.3.nupkg'],
  'nuget.publish.powershell': ['Acme.Widgets.1.2.3.nupkg'],
  'ansible-galaxy.publish.collection': ['./acme-tools-1.0.0.tar.gz'],
  'curl.install': ['<path>'],
  'curl.publish': ['<file>', '<path>'],
};

/** The languages of the templates that are no POSIX shell (Q-UI-801), by snippet id. */
const SNIPPET_LANGUAGES: Readonly<Record<string, ClientSnippet['language']>> = {
  'dotnet.configure.powershell': 'powershell',
  'dotnet.publish.powershell': 'powershell',
  'nuget.configure.cmd': 'cmd',
  'nuget.configure.powershell': 'powershell',
  'nuget.install': 'cmd',
  'nuget.publish.cmd': 'cmd',
  'nuget.publish.powershell': 'powershell',
  'ansible-galaxy.configure.token-env-powershell': 'powershell',
  'ansible-galaxy.configure.token-env-cmd': 'cmd',
};

/**
 * The marks of a snippet as `snippetMarks` of internal/api/v1/setup_snippets.go names them (R-API-030): the token reference the
 * credential mode wrote, the placeholders of the core, the template's examples; each once, in the order it first stands.
 */
export function marksOf(id: string, content: string): ClientSnippet['marks'] {
  const candidates: ClientSnippet['marks'] = [
    ...['${QUADER_TOKEN}', '%QUADER_TOKEN%', '$env:QUADER_TOKEN', '{{token}}'].map((text) => ({ text, kind: 'token' as const })),
    ...['<package>', '<version>', '<project>', '<repository>', ...(SNIPPET_EXAMPLES[id] ?? [])].map((text) => ({ text, kind: 'value' as const })),
  ];
  return candidates
    .filter((m, i) => content.includes(m.text) && candidates.findIndex((o) => o.text === m.text) === i)
    .sort((a, b) => content.indexOf(a.text) - content.indexOf(b.text));
}

/** Every snippet of a fake answer gets the language and the marks the server sends. */
export function finishSetup(answer: RawSetup): ClientSetup {
  return {
    ...answer,
    snippets: answer.snippets.map((sn) => ({ ...sn, language: SNIPPET_LANGUAGES[sn.id] ?? sn.language, marks: marksOf(sn.id, sn.content) })),
  };
}

/** A snippet and an answer as the builders below write them; `finishSetup` adds what the server derives. */
type RawSnippet = Omit<ClientSnippet, 'marks'>;
type RawSetup = Omit<ClientSetup, 'snippets'> & { snippets: RawSnippet[] };

/** Single-quotes a value for a shell unless it is plain (R-API-030). */
export function shellQuote(value: string): string {
  return /^[A-Za-z0-9@/._+:=-]+$/.test(value) ? value : `'${value.replace(/'/g, `'\\''`)}'`;
}

export class FakeClientSetupApi extends ClientSetupApi {
  calls: { repository: string; query: ClientSetupQuery }[] = [];
  failure: ApiError | null = null;
  /** The virtual repository without a default deployment target has no publish snippets. */
  noTarget = new Set<string>(['web/npm-readonly']);
  /** Repositories the caller may read but not publish to. */
  readOnly = new Set<string>(['web/npm-remote']);
  ttl = 2_592_000;
  /**
   * The base URL each Bastion registration reported and the installation lists (R-BAS-007); `null` is a
   * registration whose address is not listed (409), a name that is not here is no registration (404).
   */
  bastionBases: Record<string, string | null> = { partners: 'https://packages.example.com', unlisted: null };
  /**
   * The URI of the installation's current signing identity; set, every answer but a Bastion's carries the tab verify with the cosign
   * command of signatures.md R-SIG-052 (R-SIG-055, Q-SIG-71). `null`: no identity is current, and the tab is not answered.
   */
  signingIdentity: string | null = null;

  override async get(repository: string, query: ClientSetupQuery): Promise<ClientSetup> {
    this.calls.push({ repository, query });
    if (this.failure) {
      const e = this.failure;
      this.failure = null;
      throw e;
    }
    const raw = this.answer(repository, query);
    if (this.signingIdentity !== null && !query.bastion) {
      // npm's client checks the registry signatures with its own command first (Q-SIG-163).
      if (query.client === 'npm') raw.snippets.push({ id: 'npm.verify.signatures', tab: 'verify', fileName: null, language: 'shell', content: 'npm audit signatures\n' });
      raw.snippets.push(verifySnippet(repository, query, this.signingIdentity));
    }
    return finishSetup(raw);
  }

  private answer(repository: string, query: ClientSetupQuery): RawSetup {
    if (repository === refOf(GENERIC_REPOSITORY)) {
      return curlSetup(repository, query, !this.readOnly.has(repository), this.ttl);
    }
    const nuget = NUGET_REPOSITORIES.find((r) => refOf(r) === repository);
    if (nuget) {
      const mayPublish = !this.readOnly.has(repository) && nuget.class !== 'remote' && !this.noTarget.has(repository);
      return nugetSetup(nuget, query, mayPublish, this.ttl);
    }
    const oci = OCI_REPOSITORIES.find((r) => refOf(r) === repository);
    if (oci) {
      const mayPublish = !this.readOnly.has(repository) && !this.noTarget.has(repository);
      return ociSetup(oci, query, mayPublish, this.ttl);
    }
    const ansible = ANSIBLE_REPOSITORIES.find((r) => refOf(r) === repository);
    if (ansible) {
      const mayPublish = !this.readOnly.has(repository) && !this.noTarget.has(repository);
      return ansibleSetup(ansible, query, mayPublish, this.ttl);
    }
    const repo = NPM_REPOSITORIES.find((r) => refOf(r) === repository);
    if (!repo) throw notFound();
    const credentials = query.credentials ?? 'placeholder';
    let base = BASE;
    if (query.bastion) {
      const reported = this.bastionBases[query.bastion];
      if (reported === undefined) throw notFound();
      if (reported === null) throw new ApiError(409, { code: 'bastion_base_url_unknown' }, null);
      base = reported;
    }
    const registryUrl = `${base}/api/npm/${repository}/`;
    const endpointPath = registryUrl.replace(/^https?:/, '');
    const target =
      query.bastion || this.readOnly.has(repository) || this.noTarget.has(repository)
        ? null
        : repo.class === 'virtual'
          ? 'web/npm-local'
          : repository;
    const token = credentials === 'placeholder' ? '{{token}}' : credentials === 'env' ? '${QUADER_TOKEN}' : null;
    const auth = (line: string): string => (token === null ? '' : line.replace('TOKEN', token));
    const snippets: RawSnippet[] = [];
    const yarnBerry = query.client === 'yarn';
    const client = query.client;
    if (!['npm', 'pnpm', 'yarn', 'yarn-classic'].includes(client)) {
      throw new ApiError(400, { code: 'malformed_request', detail: 'unknown client' }, null);
    }
    if (yarnBerry) {
      snippets.push({
        id: 'yarn.configure.yarnrc',
        tab: 'configure',
        fileName: '.yarnrc.yml',
        language: 'yaml',
        content:
          `npmRegistryServer: "${registryUrl}"\n` +
          (token === null ? '' : `npmAlwaysAuth: true\nnpmAuthToken: "${token}"\n`),
      });
    } else {
      snippets.push({
        id: `${client}.configure.npmrc`,
        tab: 'configure',
        fileName: '.npmrc',
        language: 'ini',
        content:
          `registry=${registryUrl}\n` +
          auth(`${endpointPath}:_authToken=TOKEN\n`) +
          (query.bastion && client === 'npm' ? 'replace-registry-host=always\n' : ''),
      });
      if (query.scope) {
        snippets.push({
          id: `${client}.configure.npmrc-scoped`,
          tab: 'configure',
          fileName: '.npmrc',
          language: 'ini',
          content: `${query.scope}:registry=${registryUrl}\n` + auth(`${endpointPath}:_authToken=TOKEN\n`),
        });
      }
    }
    if (client === 'npm') {
      snippets.push({
        id: 'npm.configure.cli',
        tab: 'configure',
        fileName: null,
        language: 'shell',
        content: `npm config set registry=${registryUrl}\n` + auth(`npm config set "${endpointPath}:_authToken=TOKEN"\n`),
      });
    }
    const verb = client === 'npm' ? 'npm install' : client === 'pnpm' ? 'pnpm add' : 'yarn add';
    const spec = query.package
      ? shellQuote(query.version ? `${query.package}@${query.version}` : query.package)
      : '<package>';
    snippets.push({ id: `${client}.install`, tab: 'install', fileName: null, language: 'shell', content: `${verb} ${spec}` });
    if (target !== null) {
      // As the server renders them (npm format templates, R-API-030, Q-API-180): the registry of the
      // repository the caller chose, a virtual one forwards the publish to its deployment target.
      const publish: Record<string, [string, ClientSnippet['language'], string | null]> = {
        npm: [`npm publish --registry ${registryUrl}\n`, 'shell', null],
        pnpm: [`pnpm publish --registry ${registryUrl}\n`, 'shell', null],
        'yarn-classic': [`yarn publish --registry ${registryUrl}\n`, 'shell', null],
        yarn: [`npmPublishRegistry: "${registryUrl}"\n`, 'yaml', '.yarnrc.yml'],
      };
      const [content, language, fileName] = publish[client] ?? ['', 'text', null];
      snippets.push({ id: `${client}.publish`, tab: 'publish', fileName, language, content });
    }
    return {
      repository,
      format: 'npm',
      client,
      credentials,
      registryUrl,
      endpointPath,
      username: credentials === 'none' ? null : 'alice',
      publishTarget: target,
      tokenTtlSeconds: this.ttl,
      snippets,
    };
  }
}

/**
 * The NuGet snippets as the server renders them (internal/formats/nuget/setup.go, Q-NUGET-49): the
 * registry URL ends in a slash and the source is its `v3/index.json`; the ids are
 * `<client>.<section>[.<name>]`; the token is one spelling per shell (`${QUADER_TOKEN}`,
 * `%QUADER_TOKEN%`, `$env:QUADER_TOKEN`), replaced by the placeholder or by the variable of the
 * credential mode; with `none` the lines that hold a token go. The user name is the fixed `quader`.
 * A virtual repository publishes through itself, so the push names the repository the caller chose.
 */
function nugetSetup(repo: Repository, query: ClientSetupQuery, mayPublish: boolean, ttl: number): RawSetup {
  const clients = ['dotnet', 'nuget', 'visual-studio'];
  if (!clients.includes(query.client)) {
    throw new ApiError(400, { code: 'malformed_request', detail: 'unknown client' }, null);
  }
  const credentials = query.credentials ?? 'placeholder';
  const registryUrl = `${BASE}/api/nuget/${refOf(repo)}/`;
  const source = `${registryUrl}v3/index.json`;
  const target = mayPublish ? (repo.class === 'virtual' ? 'web/nuget-local' : refOf(repo)) : null;
  const refs = {
    sh: credentials === 'placeholder' ? '{{token}}' : credentials === 'env' ? '${QUADER_TOKEN}' : null,
    cmd: credentials === 'placeholder' ? '{{token}}' : credentials === 'env' ? '%QUADER_TOKEN%' : null,
    ps: credentials === 'placeholder' ? '{{token}}' : credentials === 'env' ? '$env:QUADER_TOKEN' : null,
  };
  const auth = credentials !== 'none';
  const pkg = query.package ?? '<package>';
  const withVersion = query.package && query.version ? query.version : null;
  const snippets: RawSnippet[] = [];
  const add = (id: string, tab: ClientSnippet['tab'], language: ClientSnippet['language'], content: string, fileName: string | null = null): void => {
    snippets.push({ id, tab, fileName, language, content });
  };

  const config =
    '<?xml version="1.0" encoding="utf-8"?>\n<configuration>\n  <packageSources>\n    <clear />\n' +
    `    <add key="quader" value="${source}" protocolVersion="3" />\n  </packageSources>\n` +
    (auth
      ? '  <packageSourceCredentials>\n    <quader>\n      <add key="Username" value="quader" />\n' +
        `      <add key="ClearTextPassword" value="${refs.cmd}" />\n    </quader>\n  </packageSourceCredentials>\n`
      : '') +
    '</configuration>\n';

  if (query.client === 'dotnet') {
    add(
      'dotnet.configure.shell',
      'configure',
      'shell',
      `dotnet nuget add source ${source} --name quader` + (auth ? ` \\\n  --username quader --password "${refs.sh}" --store-password-in-clear-text` : '') + '\n',
    );
    add(
      'dotnet.configure.powershell',
      'configure',
      'shell',
      `dotnet nuget add source ${source} --name quader` + (auth ? ` \`\n  --username quader --password "${refs.ps}" --store-password-in-clear-text` : '') + '\n',
    );
  }
  if (query.client === 'nuget') {
    add('nuget.configure.cmd', 'configure', 'shell', `nuget sources add -Name quader -Source ${source}` + (auth ? ` ^\n  -Username quader -Password ${refs.cmd}` : '') + '\n');
    add(
      'nuget.configure.powershell',
      'configure',
      'shell',
      `nuget sources add -Name quader -Source ${source}` + (auth ? ` \`\n  -Username quader -Password "${refs.ps}"` : '') + '\n',
    );
  }
  add(`${query.client}.configure.nugetconfig`, 'configure', 'xml', config, 'nuget.config');

  if (query.client === 'dotnet') {
    add('dotnet.install', 'install', 'shell', `dotnet add package ${pkg}${withVersion ? ` --version ${withVersion}` : ''}\n`);
  } else if (query.client === 'nuget') {
    add('nuget.install', 'install', 'shell', `nuget install ${pkg} -Source quader -OutputDirectory packages${withVersion ? ` -Version ${withVersion}` : ''}\n`);
  } else {
    add('visual-studio.install.packagereference', 'install', 'xml', `<PackageReference Include="${pkg}"${withVersion ? ` Version="${withVersion}"` : ''} />\n`);
  }

  if (target !== null && query.client !== 'visual-studio') {
    const feed = source;
    const key = (r: string | null, quote: boolean): string => (r === null ? '' : quote ? ` --api-key "${r}"` : '');
    if (query.client === 'dotnet') {
      const push = `dotnet nuget push bin/Release/Acme.Widgets.1.2.3.nupkg --source ${feed}`;
      add('dotnet.publish.shell', 'publish', 'shell', push + key(refs.sh, true) + '\n');
      add('dotnet.publish.powershell', 'publish', 'shell', push + key(refs.ps, true) + '\n');
    } else {
      const push = `nuget push Acme.Widgets.1.2.3.nupkg -Source ${feed}`;
      add('nuget.publish.cmd', 'publish', 'shell', push + (refs.cmd === null ? '' : ` -ApiKey ${refs.cmd}`) + '\n');
      add('nuget.publish.powershell', 'publish', 'shell', push + (refs.ps === null ? '' : ` -ApiKey "${refs.ps}"`) + '\n');
    }
  }
  return {
    repository: refOf(repo),
    format: 'nuget',
    client: query.client,
    credentials,
    registryUrl,
    endpointPath: source.replace(/^https?:/, ''),
    username: credentials === 'none' ? null : 'alice',
    publishTarget: target,
    tokenTtlSeconds: ttl,
    snippets,
  };
}

/**
 * The Ansible snippets as the server renders them (internal/formats/ansible/setup.go, R-ANS-025; the ids are
 * `<client>.<section>[.<name>]` and `snippet-ids.fixture.ts` pins them against the Go side). The server is named `quader` in an
 * `ansible.cfg` that holds no token, the token is the environment variable `ANSIBLE_GALAXY_SERVER_QUADER_TOKEN` in the spelling of each
 * shell, and a `~/.netrc` line is a command that names the host alone. The credential reference is rewritten by the mode of the request
 * the way the API does (`applyCredentials` of internal/api/v1/setup_snippets.go): the placeholder, the variable, or the line dropped.
 * `PowerShell` and `cmd` are reported as `shell`, the one language the document knows for them (Q-API-191).
 */
function ansibleSetup(repo: Repository, query: ClientSetupQuery, mayPublish: boolean, ttl: number): RawSetup {
  if (query.client !== 'ansible-galaxy') {
    throw new ApiError(400, { code: 'malformed_request', detail: 'unknown client' }, null);
  }
  const credentials = query.credentials ?? 'placeholder';
  const registryUrl = `${BASE}/api/ansible/${refOf(repo)}/`;
  const host = new URL(BASE).hostname;
  const target = mayPublish ? refOf(repo) : null;
  const auth = credentials !== 'none';
  const sh = '${QUADER_TOKEN}';
  const cmd = '%QUADER_TOKEN%';
  const ps = '$env:QUADER_TOKEN';
  const variable = 'ANSIBLE_GALAXY_SERVER_QUADER_TOKEN';
  const anonymous = 'This repository is open to anonymous readers: no credential is needed to install from it.';
  const credential = (content: string): string => {
    if (credentials === 'env') return content;
    if (credentials === 'none') {
      return content
        .split(/(?<=\n)/)
        .filter((line) => ![sh, cmd, ps].some((ref) => line.includes(ref)))
        .join('');
    }
    return content.split(sh).join('{{token}}').split(cmd).join('{{token}}').split(ps).join('{{token}}');
  };
  const pkg = query.package ? shellQuote(query.package) : null;
  const snippets: RawSnippet[] = [];
  const add = (id: string, tab: ClientSnippet['tab'], language: ClientSnippet['language'], content: string, fileName: string | null = null): void => {
    snippets.push({ id, tab, fileName, language, content: credential(content) });
  };
  add('ansible-galaxy.configure.ansible-cfg', 'configure', 'ini', `[galaxy]\nserver_list = quader\n\n[galaxy_server.quader]\nurl=${registryUrl}\n`, 'ansible.cfg');
  add(
    'ansible-galaxy.configure.token-env',
    'configure',
    'shell',
    auth ? `export ${variable}="${sh}"\n` : `# ${anonymous}\n`,
  );
  add(
    'ansible-galaxy.configure.token-env-powershell',
    'configure',
    'shell',
    auth ? `$env:${variable} = "${ps}"\n` : `# ${anonymous}\n`,
  );
  add('ansible-galaxy.configure.token-env-cmd', 'configure', 'shell', auth ? `set ${variable}=${cmd}\n` : `REM ${anonymous}\n`);
  add(
    'ansible-galaxy.configure.netrc',
    'configure',
    'shell',
    auth
      ? `touch ~/.netrc && chmod 600 ~/.netrc\nprintf 'machine %s login quader password %s\\n' ${host} "${sh}" >>~/.netrc\n`
      : '# This repository is open to anonymous readers: no ~/.netrc entry is needed.\n',
  );
  const version = query.package && query.version ? query.version : null;
  // The server writes a role's own install snippets with its display name (R-ANS-056, Q-ANS-114); the fake knows the one role it serves.
  if (query.package === 'role/16') {
    add('ansible-galaxy.install.role', 'install', 'shell', `ansible-galaxy role install geerlingguy.pip${version ? `,${shellQuote(version)}` : ''}\n`);
  }
  add('ansible-galaxy.install.collection', 'install', 'shell', `ansible-galaxy collection install ${pkg === null ? '<package>' : version ? `${pkg}:${shellQuote(version)}` : pkg}\n`);
  add(
    'ansible-galaxy.install.requirements',
    'install',
    'yaml',
    '---\ncollections:\n' +
      `  - name: ${query.package ?? '<package>'}${version ? `\n    version: "${version}"` : ''}\n` +
      '    source: quader\n' +
      '# Roles are served by remote and virtual repositories of a Galaxy only. Install them with\n' +
      '#   ansible-galaxy role install -r requirements.yml\n' +
      '# roles:\n' +
      '#   - name: <owner>.<role>\n',
    'requirements.yml',
  );
  if (target !== null) {
    add('ansible-galaxy.publish.collection', 'publish', 'shell', `ansible-galaxy collection publish ./acme-tools-1.0.0.tar.gz --server ${shellQuote(registryUrl)} --token=${sh}\n`);
  }
  return {
    repository: refOf(repo),
    format: 'ansible',
    client: query.client,
    credentials,
    registryUrl,
    endpointPath: registryUrl.replace(/^https?:/, ''),
    username: credentials === 'none' ? null : 'alice',
    publishTarget: target,
    tokenTtlSeconds: ttl,
    snippets,
  };
}

/**
 * The OCI snippets as the server renders them (internal/formats/oci/setup.go, R-OCI-030; the ids are `<client>.<section>[.<name>]`,
 * Q-API-160, and `snippet-ids.fixture.ts` pins them against the Go side): the image prefix is `<host>/<project>/<repository>`, not
 * the registry URL. With a credential the login reads the token from standard input; without one it is the interactive login and the
 * pull secret is a comment that asks for the snippet with a credential (the API drops the lines that hold the token reference).
 */
function ociSetup(repo: Repository, query: ClientSetupQuery, mayPublish: boolean, ttl: number): RawSetup {
  if (!['docker', 'podman'].includes(query.client)) {
    throw new ApiError(400, { code: 'malformed_request', detail: 'unknown client' }, null);
  }
  const credentials = query.credentials ?? 'placeholder';
  const registryUrl = `${BASE}/v2/${refOf(repo)}/`;
  const prefix = `${BASE.replace(/^https?:\/\//, '')}/${refOf(repo)}`;
  const token = credentials === 'placeholder' ? '{{token}}' : credentials === 'env' ? '${QUADER_TOKEN}' : null;
  const target = mayPublish ? refOf(repo) : null;
  const tool = query.client;
  // Without a package asked for, the pull names the image the push names (PackageExample, Q-UI-802).
  const pkg = query.package ? shellQuote(query.package) : 'myapp:1.0';
  const spec = query.package && query.version ? `${pkg}:${shellQuote(query.version)}` : pkg;
  const snippets: RawSnippet[] = [];
  const add = (id: string, tab: ClientSnippet['tab'], content: string): void => {
    snippets.push({ id, tab, fileName: null, language: 'shell', content });
  };
  add(
    `${tool}.configure.shell`,
    'configure',
    token === null
      ? `${tool} login ${prefix} --username quader\n`
      : `printf '%s' "${token}" | ${tool} login ${prefix} --username quader --password-stdin\n`,
  );
  if (tool === 'docker') {
    add(
      'docker.configure.kubernetes',
      'configure',
      token === null
        ? '# An image pull secret holds a credential: ask for the snippet with one.\n'
        : `printf '{"auths":{"%s":{"username":"quader","password":"%s"}}}' ${prefix} "${token}" | \\\n` +
            '  kubectl create secret generic quader-registry --type=kubernetes.io/dockerconfigjson --from-file=.dockerconfigjson=/dev/stdin\n',
    );
  }
  add(`${tool}.install`, 'install', `${tool} pull ${prefix}/${spec}\n`);
  if (target !== null) {
    add(`${tool}.publish`, 'publish', `${tool} tag myapp:1.0 ${prefix}/myapp:1.0\n${tool} push ${prefix}/myapp:1.0\n`);
    if (tool === 'docker') {
      add('docker.publish.buildx', 'publish', `docker buildx build --platform linux/amd64,linux/arm64 --tag ${prefix}/myapp:1.0 --push .\n`);
    }
  }
  return {
    repository: refOf(repo),
    format: 'oci',
    client: tool,
    credentials,
    registryUrl,
    endpointPath: registryUrl.replace(/^https?:/, ''),
    username: credentials === 'none' ? null : 'alice',
    publishTarget: target,
    tokenTtlSeconds: ttl,
    snippets,
  };
}

/** The curl snippets of a generic repository: the function of the configure tab and its calls. */
/** The snippet of the tab verify as the server writes it (internal/api/v1/setup_verify.go): an image's or a file's cosign command. */
function verifySnippet(repository: string, query: ClientSetupQuery, identity: string): RawSnippet {
  const trust = [
    'curl -fsSo quader-trusted-root.json ' + BASE + '/api/v1/signing/trusted-root.json',
  ];
  const flags = [
    '  --trusted-root quader-trusted-root.json \\',
    `  --certificate-identity ${identity} \\`,
    "  --certificate-oidc-issuer-regexp '.*' \\",
    '  --insecure-ignore-tlog --insecure-ignore-sct \\',
    '  --type https://github.com/guided-traffic/quader/blob/main/docs/spec/signatures.md#receipt-attestation-v1 \\',
  ];
  const image = OCI_REPOSITORIES.some((r) => refOf(r) === repository);
  const lines = image
    ? [...trust, 'cosign verify-attestation \\', ...flags, `  ${BASE.replace(/^https?:\/\//, '')}/${repository}/${query.package ?? 'myapp'}@sha256:<digest>`]
    : [
        ...trust,
        `curl -fsS -o <file>.quader.sigstore.json '${BASE}/api/v1/projects/${repository.replace('/', '/repositories/')}/attestation?path=<path>'`,
        'cosign verify-blob-attestation \\',
        '  --bundle <file>.quader.sigstore.json \\',
        ...flags,
        '  <file>',
      ];
  return { id: `${query.client}.verify.attestation`, tab: 'verify', fileName: null, language: 'shell', content: lines.join('\n') + '\n' };
}

function curlSetup(repository: string, query: ClientSetupQuery, mayPublish: boolean, ttl: number): RawSetup {
  if (query.client !== 'curl') {
    throw new ApiError(400, { code: 'malformed_request', detail: 'unknown client' }, null);
  }
  const credentials = query.credentials ?? 'placeholder';
  const registryUrl = `${BASE}/files/${repository}/`;
  const token = credentials === 'placeholder' ? '{{token}}' : credentials === 'env' ? '${QUADER_TOKEN}' : null;
  const header = token === null ? '' : `header = "Authorization: Bearer ${token}"\n`;
  const snippets: RawSnippet[] = [
    {
      id: 'curl.configure.function',
      tab: 'configure',
      fileName: null,
      language: 'shell',
      content: `quader_curl() {\n  curl --silent --show-error --config - "$@" <<EOF\n${header}EOF\n}\n`,
    },
    {
      id: 'curl.install',
      tab: 'install',
      fileName: null,
      language: 'shell',
      content: `quader_curl --fail --remote-name "${registryUrl}<path>"\n`,
    },
  ];
  if (mayPublish) {
    snippets.push({
      id: 'curl.publish',
      tab: 'publish',
      fileName: null,
      language: 'shell',
      content: `quader_curl --fail --upload-file <file> "${registryUrl}<path>"\n`,
    });
  }
  return {
    repository,
    format: 'generic',
    client: 'curl',
    credentials,
    registryUrl,
    endpointPath: registryUrl.replace(/^https?:/, ''),
    username: credentials === 'none' ? null : 'alice',
    publishTarget: mayPublish ? repository : null,
    tokenTtlSeconds: ttl,
    snippets,
  };
}

export function providePackageFakes(): Provider[] {
  return [
    { provide: PackagesApi, useClass: FakePackagesApi },
    { provide: ClientSetupApi, useClass: FakeClientSetupApi },
  ];
}
