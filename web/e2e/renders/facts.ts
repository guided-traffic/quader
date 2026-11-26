import { createHash } from 'node:crypto';
import { crc32, deflateSync, gzipSync } from 'node:zlib';
import { APIRequestContext } from '@playwright/test';
import { zipArchive } from './advisories';
import { expectStatus, mainProject, otherProject, patient, registryToken, tarEntry, tarGz } from './seed';

/**
 * The facts of a version as every format shows them (W-30 G3, R-UI-221): one package per format whose metadata fills every block of the
 * tab Details, so the formats can be put side by side. An npm package with every kind of dependency, twenty keywords, its runtimes, its
 * types, an install script and its publisher; a NuGet package with four framework groups and eight tags; an Ansible collection with
 * dependencies and ten tags; an image under ten tags. Each depends on a package the installation holds (a link) and on some it does not
 * (text). A few kilobytes in all; a run again publishes the same bytes, which answer a conflict.
 */
export const factsNpm = { format: 'npm', name: '@acme/portal-shell', version: '2.3.0' } as const;
/**
 * 3.2.1 adds what a NuGet version shows beyond its nuspec (R-NUGET-038, R-NUGET-039): an icon, a licence file, release notes of twenty
 * lines, build logic, an analyzer, an install script, a symbol package and its repository on a host whose commit addresses the page knows.
 * A new version, not a changed 3.1.0: an instance that holds 3.1.0 from an older seed shows the new facts on 3.2.1.
 */
export const factsNuget = { format: 'nuget', name: 'Acme.Platform.Http', version: '3.2.1' } as const;
/** A .NET tool, a template package and a package that carries nothing but its nuspec (R-NUGET-037, R-NUGET-038). */
export const factsNugetTool = { format: 'nuget', name: 'Acme.Platform.Cli', version: '1.4.0' } as const;
export const factsNugetTemplate = { format: 'nuget', name: 'Acme.Platform.Templates', version: '2.0.0' } as const;
export const factsNugetPlain = { format: 'nuget', name: 'Acme.Platform.Meta', version: '1.0.0' } as const;
export const factsCollection = { format: 'ansible', name: 'acme.network', version: '2.0.0' } as const;
export const factsImage = {
  format: 'oci',
  name: 'portal/gateway',
  tags: ['3.2.1', '3.2', '3', 'latest', 'stable', 'production', 'eu-central', '2026-11-20', 'release-3.2.1', 'sha-9c1e2f4'],
} as const;

const keywords = [
  'portal',
  'shell',
  'layout',
  'navigation',
  'react',
  'components',
  'design-system',
  'theming',
  'dark-mode',
  'accessibility',
  'routing',
  'i18n',
  'micro-frontends',
  'module-federation',
  'sidebar',
  'breadcrumbs',
  'top-bar',
  'notifications',
  'customer-portal',
  'acme',
];

/** The manifest of the npm package: in the tarball's package.json and in the publish body alike. */
function npmManifest(): Record<string, unknown> {
  return {
    name: factsNpm.name,
    version: factsNpm.version,
    description: 'The shell of the customer portal: the top bar, the navigation, the theme and the routes every page of the portal sits in.',
    license: 'Apache-2.0',
    keywords,
    author: 'Web platform <web-platform@example.invalid>',
    contributors: ['Portal team <portal@example.invalid>'],
    homepage: 'https://portal.example.invalid/docs/shell',
    repository: { type: 'git', url: 'https://git.example.invalid/web/portal-shell.git' },
    bugs: { url: 'https://git.example.invalid/web/portal-shell/issues' },
    types: 'dist/index.d.ts',
    main: 'dist/index.js',
    engines: { node: '>=20.11.0', npm: '>=10.2.0' },
    scripts: { build: 'tsc -p tsconfig.build.json', postinstall: 'node scripts/fetch-icons.js' },
    dependencies: {
      '@acme/ui-kit': '^1.1.0',
      '@acme/tokens': '^2.0.0',
      'react-router': '^7.1.0',
      tslib: '^2.8.0',
      'i18next': '^24.2.0',
    },
    devDependencies: {
      typescript: '~5.7.2',
      vitest: '^3.0.0',
      '@acme/eslint-config': '^3.2.0',
      '@types/react': '^19.0.0',
    },
    peerDependencies: { react: '^19.0.0', 'react-dom': '^19.0.0', '@acme/telemetry': '^1.0.0' },
    peerDependenciesMeta: { '@acme/telemetry': { optional: true } },
    optionalDependencies: { 'web-vitals-reporter': '^1.4.0' },
    bundleDependencies: ['@acme/tokens'],
  };
}

async function publishNpm(ctx: APIRequestContext, edge: string, auth: Record<string, string>): Promise<void> {
  const manifest = npmManifest();
  const readme = '# @acme/portal-shell\n\nThe shell of the customer portal.\n';
  const tgz = gzipSync(
    Buffer.concat([
      tarEntry('package/package.json', Buffer.from(JSON.stringify(manifest, null, 2))),
      tarEntry('package/README.md', Buffer.from(readme)),
      tarEntry('package/dist/index.js', Buffer.from('export const shell = {};\n')),
      tarEntry('package/dist/index.d.ts', Buffer.from('export declare const shell: object;\n')),
      tarEntry('package/scripts/fetch-icons.js', Buffer.from("console.log('icons');\n")),
      Buffer.alloc(1024),
    ]),
  );
  const file = `portal-shell-${factsNpm.version}.tgz`;
  const body = {
    _id: factsNpm.name,
    name: factsNpm.name,
    description: manifest['description'],
    'dist-tags': { latest: factsNpm.version },
    readme,
    versions: {
      [factsNpm.version]: {
        ...manifest,
        readme,
        _id: `${factsNpm.name}@${factsNpm.version}`,
        dist: {
          integrity: `sha512-${createHash('sha512').update(tgz).digest('base64')}`,
          shasum: createHash('sha1').update(tgz).digest('hex'),
          tarball: `${edge}/api/npm/${mainProject}/npm-internal/${factsNpm.name}/-/${file}`,
        },
      },
    },
    access: null,
    _attachments: { [file]: { content_type: 'application/octet-stream', data: tgz.toString('base64'), length: tgz.length } },
  };
  const res = await patient(() => ctx.put(`/api/npm/${mainProject}/npm-internal/${factsNpm.name.replace('/', '%2f')}`, { data: body, headers: auth }));
  await expectStatus(res, [200, 201, 409], `npm publish ${factsNpm.name}@${factsNpm.version}`);
}

/** A PNG of 64 x 64 pixels, four squares of two colours: an icon a package embeds. */
function iconPng(): Buffer {
  const size = 64;
  const chunk = (type: string, data: Buffer): Buffer => {
    const len = Buffer.alloc(4);
    len.writeUInt32BE(data.length);
    const td = Buffer.concat([Buffer.from(type, 'ascii'), data]);
    const crc = Buffer.alloc(4);
    crc.writeUInt32BE(crc32(td) >>> 0);
    return Buffer.concat([len, td, crc]);
  };
  const ihdr = Buffer.alloc(13);
  ihdr.writeUInt32BE(size, 0);
  ihdr.writeUInt32BE(size, 4);
  ihdr[8] = 8; // bit depth
  ihdr[9] = 2; // RGB
  const rows: Buffer[] = [];
  for (let y = 0; y < size; y++) {
    const row = Buffer.alloc(1 + size * 3);
    for (let x = 0; x < size; x++) {
      const dark = (x < size / 2) !== (y < size / 2);
      row.set(dark ? [0x1f, 0x4f, 0x99] : [0x63, 0xa4, 0xe8], 1 + x * 3);
    }
    rows.push(row);
  }
  return Buffer.concat([
    Buffer.from([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a]),
    chunk('IHDR', ihdr),
    chunk('IDAT', deflateSync(Buffer.concat(rows))),
    chunk('IEND', Buffer.alloc(0)),
  ]);
}

/** Twenty lines of release notes, as a changelog of a release writes them. */
const RELEASE_NOTES = [
  'Changes in 3.2.1:',
  ...['Retries', 'Timeouts', 'Telemetry', 'Handlers', 'Logging'].flatMap((area) => [
    `- ${area}: the client honours the server's Retry-After header.`,
    `- ${area}: a cancelled request no longer counts as a failure.`,
    `- ${area}: the defaults are read from the configuration section of the platform.`,
    `- ${area}: fixed a race when two requests share one handler.`,
  ]).slice(0, 19),
].join('\n');

/** The NuGet package: four framework groups, one of them empty, a dependency the installation holds (Acme.Logging.Abstractions). */
function nupkg(version: string = factsNuget.version): Buffer {
  const { name: id } = factsNuget;
  const rich = version !== '3.1.0';
  const nuspec = `<?xml version="1.0" encoding="utf-8"?>
<package xmlns="http://schemas.microsoft.com/packaging/2013/05/nuspec.xsd">
  <metadata>
    <id>${id}</id>
    <version>${version}</version>
    <authors>Platform engineering</authors>
    <description>Typed HTTP clients of the platform's services, with retries, timeouts and the platform's telemetry.</description>
    ${rich ? '<license type="file">LICENSE.txt</license>\n    <icon>images/icon.png</icon>' : '<license type="expression">Apache-2.0</license>'}
    <projectUrl>https://git.example.invalid/platform/http</projectUrl>
    ${
      rich
        ? '<repository type="git" url="https://github.com/acme-platform/http.git" branch="refs/heads/release/3.2" commit="4b7e2a9c1d3f5e6a8b0c2d4e6f8a0b1c3d5e7f9a" />'
        : '<repository type="git" url="https://git.example.invalid/platform/http.git" branch="main" commit="9c1e2f4a7b3d5e6f8a9b0c1d2e3f4a5b6c7d8e9f" />'
    }
    <copyright>Copyright 2026 Acme platform engineering</copyright>
    <releaseNotes>${rich ? RELEASE_NOTES : 'Retries honour Retry-After.\nTimeouts are per attempt, not per call.'}</releaseNotes>
    <tags>http client retry polly resilience telemetry platform acme</tags>
    <dependencies>
      <group targetFramework="net9.0">
        <dependency id="Acme.Logging.Abstractions" version="1.1.0" />
        <dependency id="Microsoft.Extensions.Http" version="9.0.0" />
        <dependency id="Microsoft.Extensions.Http.Resilience" version="9.0.0" />
      </group>
      <group targetFramework="net8.0">
        <dependency id="Acme.Logging.Abstractions" version="1.1.0" />
        <dependency id="Microsoft.Extensions.Http" version="8.0.1" />
        <dependency id="Polly.Core" version="8.5.0" />
      </group>
      <group targetFramework=".NETStandard2.0">
        <dependency id="Acme.Logging.Abstractions" version="1.0.0" />
        <dependency id="Microsoft.Extensions.Http" version="6.0.0" />
        <dependency id="Polly" version="7.2.4" />
        <dependency id="System.Text.Json" version="8.0.5" />
      </group>
      <group targetFramework=".NETFramework4.7.2" />
    </dependencies>
  </metadata>
</package>
`;
  return zipArchive([
    { name: `${id}.nuspec`, data: Buffer.from(nuspec) },
    { name: `lib/net9.0/${id}.dll`, data: Buffer.from(`MZ ${id} ${version} net9.0`) },
    { name: `lib/net8.0/${id}.dll`, data: Buffer.from(`MZ ${id} ${version} net8.0`) },
    { name: `lib/netstandard2.0/${id}.dll`, data: Buffer.from(`MZ ${id} ${version} netstandard2.0`) },
    { name: `lib/net472/${id}.dll`, data: Buffer.from(`MZ ${id} ${version} net472`) },
    ...(rich
      ? [
          { name: 'images/icon.png', data: iconPng() },
          { name: 'LICENSE.txt', data: Buffer.from('MIT License\n\nCopyright 2026 Acme platform engineering\n\nPermission is hereby granted, free of charge, to any person obtaining a copy of this software.\n') },
          { name: `build/${id}.targets`, data: Buffer.from('<Project><Target Name="AcmeHttpCheck" BeforeTargets="Build" /></Project>') },
          { name: `buildTransitive/net8.0/${id}.props`, data: Buffer.from('<Project />') },
          { name: `analyzers/dotnet/cs/${id}.Generators.dll`, data: Buffer.from(`MZ ${id} generators`) },
          { name: 'tools/install.ps1', data: Buffer.from('param($installPath, $toolsPath, $package, $project)') },
        ]
      : []),
  ]);
}

/** A symbol package of the rich version: one portable PDB, as `dotnet pack -p:SymbolPackageFormat=snupkg` builds it. */
function snupkg(): Buffer {
  const { name: id, version } = factsNuget;
  const nuspec = `<?xml version="1.0" encoding="utf-8"?>
<package xmlns="http://schemas.microsoft.com/packaging/2013/05/nuspec.xsd"><metadata><id>${id}</id><version>${version}</version><authors>Platform engineering</authors><description>Symbols</description><packageTypes><packageType name="SymbolsPackage" /></packageTypes></metadata></package>`;
  return zipArchive([
    { name: `${id}.nuspec`, data: Buffer.from(nuspec) },
    { name: `lib/net9.0/${id}.pdb`, data: Buffer.concat([Buffer.from('BSJB'), Buffer.alloc(60)]) },
  ]);
}

/** A package of one nuspec and one folder: a .NET tool, a template package, or a package that carries nothing (no type, no folder). */
function smallNupkg(p: { readonly name: string; readonly version: string }, type: string | null, folder: string | null, description: string): Buffer {
  const nuspec = `<?xml version="1.0" encoding="utf-8"?>
<package xmlns="http://schemas.microsoft.com/packaging/2013/05/nuspec.xsd"><metadata><id>${p.name}</id><version>${p.version}</version><authors>Platform engineering</authors><description>${description}</description>${type ? `<packageTypes><packageType name="${type}" /></packageTypes>` : ''}</metadata></package>`;
  return zipArchive([
    { name: `${p.name}.nuspec`, data: Buffer.from(nuspec) },
    ...(folder ? [{ name: folder, data: Buffer.from(`MZ ${p.name}`) }] : []),
  ]);
}

/** The collection: it depends on acme.platform, which the installation holds, and on two collections it does not. */
function collectionArchive(): Buffer {
  const [namespace, name] = factsCollection.name.split('.') as [string, string];
  const version = factsCollection.version;
  const sha = (b: Buffer): string => createHash('sha256').update(b).digest('hex');
  const files: [string, Buffer][] = [
    ['README.md', Buffer.from(`# ${namespace}.${name}\n\nThe network roles of the platform team: VLANs, routes and the host firewall.\n`)],
    ['meta/runtime.yml', Buffer.from("requires_ansible: '>=2.16.0'\n")],
    ['roles/firewall/tasks/main.yml', Buffer.from('---\n- name: Apply the host firewall\n  ansible.builtin.debug:\n    msg: firewall\n')],
  ];
  const dirs = ['meta', 'roles', 'roles/firewall', 'roles/firewall/tasks'];
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
          authors: ['Platform engineering <platform@example.invalid>', 'Network team <network@example.invalid>'],
          readme: 'README.md',
          tags: ['networking', 'firewall', 'routing', 'vlan', 'nftables', 'linux', 'infrastructure', 'platform', 'acme', 'security'],
          description: 'The network roles of the platform team',
          license: ['GPL-3.0-or-later'],
          license_file: null,
          dependencies: { 'acme.platform': '>=1.0.0', 'community.general': '>=8.0.0', 'ansible.netcommon': '>=6.0.0,<8.0.0' },
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

/** The image: one manifest with the annotations of its description, licence and links, under ten tags. */
async function pushImage(ctx: APIRequestContext, edge: string, pat: string): Promise<void> {
  const repo = `${mainProject}/images/${factsImage.name}`;
  const auth = { Authorization: `Bearer ${await registryToken(ctx, edge, pat, repo)}` };
  const digestOf = (b: Buffer): string => `sha256:${createHash('sha256').update(b).digest('hex')}`;
  const upload = async (blob: Buffer): Promise<void> => {
    const digest = digestOf(blob);
    if ((await ctx.head(`/v2/${repo}/blobs/${digest}`, { headers: auth })).status() === 200) return;
    const start = await ctx.post(`/v2/${repo}/blobs/uploads/`, { headers: auth });
    await expectStatus(start, [202], `start upload ${repo}`);
    const location = new URL(start.headers()['location'] ?? '', edge);
    location.searchParams.set('digest', digest);
    const put = await ctx.put(location.pathname + location.search, { data: blob, headers: { ...auth, 'Content-Type': 'application/octet-stream' } });
    await expectStatus(put, [201], `upload blob ${repo}`);
  };
  const layer = gzipSync(tarEntry('etc/acme-gateway', Buffer.from('gateway\n')));
  const config = Buffer.from(
    JSON.stringify({
      architecture: 'amd64',
      os: 'linux',
      config: { Entrypoint: ['/usr/bin/gateway'], ExposedPorts: { '8443/tcp': {} }, Labels: { 'org.opencontainers.image.title': 'gateway' } },
      rootfs: { type: 'layers', diff_ids: [] },
    }),
  );
  await upload(layer);
  await upload(config);
  const manifest = Buffer.from(
    JSON.stringify({
      schemaVersion: 2,
      mediaType: 'application/vnd.oci.image.manifest.v1+json',
      config: { mediaType: 'application/vnd.oci.image.config.v1+json', digest: digestOf(config), size: config.length },
      layers: [{ mediaType: 'application/vnd.oci.image.layer.v1.tar+gzip', digest: digestOf(layer), size: layer.length }],
      annotations: {
        'org.opencontainers.image.description': 'The edge gateway of the customer portal: TLS, routing and rate limits in front of every service.',
        'org.opencontainers.image.licenses': 'Apache-2.0',
        'org.opencontainers.image.source': 'https://git.example.invalid/platform/gateway',
        'org.opencontainers.image.url': 'https://portal.example.invalid/docs/gateway',
        'org.opencontainers.image.documentation': 'https://portal.example.invalid/docs/gateway/operations',
        'org.opencontainers.image.version': '3.2.1',
        'org.opencontainers.image.revision': '9c1e2f4a7b3d5e6f8a9b0c1d2e3f4a5b6c7d8e9f',
        'org.opencontainers.image.vendor': 'Acme platform engineering',
        'org.opencontainers.image.created': '2026-11-20T08:12:44Z',
      },
    }),
  );
  for (const tag of factsImage.tags) {
    const res = await patient(() =>
      ctx.put(`/v2/${repo}/manifests/${tag}`, { data: manifest, headers: { ...auth, 'Content-Type': 'application/vnd.oci.image.manifest.v1+json' } }),
    );
    await expectStatus(res, [201], `push ${repo}:${tag}`);
  }
}

export async function seedFacts(ctx: APIRequestContext, edge: string, token: string): Promise<void> {
  const auth = { Authorization: `Bearer ${token}` };
  await publishNpm(ctx, edge, auth);
  const push = async (resource: 'package' | 'symbolpackage', data: Buffer, what: string): Promise<void> => {
    const res = await patient(() =>
      ctx.put(`/api/nuget/${otherProject}/nuget-internal/v3/${resource}`, { data, headers: { ...auth, 'Content-Type': 'application/octet-stream' } }),
    );
    await expectStatus(res, [200, 201, 202, 409], what);
  };
  await push('package', nupkg('3.1.0'), `nuget push ${factsNuget.name} 3.1.0`);
  await push('package', nupkg(), `nuget push ${factsNuget.name} ${factsNuget.version}`);
  await push('symbolpackage', snupkg(), `nuget push ${factsNuget.name} ${factsNuget.version}.snupkg`);
  await push('package', smallNupkg(factsNugetTool, 'DotnetTool', `tools/net8.0/any/${factsNugetTool.name}.dll`, 'The command line of the platform.'), `nuget push ${factsNugetTool.name}`);
  await push('package', smallNupkg(factsNugetTemplate, 'Template', 'content/service/.template.config/template.json', 'Project templates of the platform.'), `nuget push ${factsNugetTemplate.name}`);
  await push('package', smallNupkg(factsNugetPlain, null, null, 'A package that brings nothing but its description.'), `nuget push ${factsNugetPlain.name}`);
  const archive = collectionArchive();
  const [namespace, name] = factsCollection.name.split('.') as [string, string];
  const ansible = await patient(() =>
    ctx.post(`/api/ansible/${otherProject}/collections/v3/artifacts/collections/`, {
      headers: auth,
      multipart: {
        sha256: createHash('sha256').update(archive).digest('hex'),
        file: { name: `${namespace}-${name}-${factsCollection.version}.tar.gz`, mimeType: 'application/gzip', buffer: archive },
      },
    }),
  );
  await expectStatus(ansible, [200, 201, 202, 409], `ansible publish ${factsCollection.name} ${factsCollection.version}`);
  await pushImage(ctx, edge, token);
}
