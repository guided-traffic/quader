import { createHash } from 'node:crypto';
import { gzipSync } from 'node:zlib';
import { APIRequestContext } from '@playwright/test';
import { Api, ar, cachedSources, expectStatus, mainProject, otherProject, patient, tarEntry, tarGz } from './seed';

/**
 * A version a remote repository cached, described as a published one (npm R-NPM-050, R-NPM-051; Debian R-DEB-037): the render profile
 * reaches no registry, so each remote caches from a local repository of the other project (seed.ts `cachedSources`). The seed publishes
 * into the source, fetches through the remote as a client does, waits until the job of the format has described the cached version, and
 * deletes the source ({@link removeCachedSources}), so that the remote alone holds the version on the pictures.
 */

/** The npm package the remote caches: a README with headings, a table and a code block, dependencies and links. */
export const cachedNpm = { format: 'npm', name: 'table-format-kit', version: '2.4.0' } as const;

/** The Debian package the remote caches: Depends, Recommends, Suggests and a long description of two paragraphs. */
export const cachedDeb = { format: 'deb', name: 'acme-log-shipper', version: '3.1.0-2' } as const;

const npmReadme = `# table-format-kit

Formats tabular data for terminals, Markdown and CSV, with column widths that follow the content.

## Install

\`\`\`sh
npm install table-format-kit
\`\`\`

## Usage

\`\`\`js
import { format } from 'table-format-kit';

console.log(format([{ name: 'ui-kit', version: '1.4.0' }], { style: 'markdown' }));
\`\`\`

## Options

| Option | Default | What it does |
|---|---|---|
| \`style\` | \`"plain"\` | \`plain\`, \`markdown\` or \`csv\` |
| \`maxWidth\` | \`80\` | Wraps cells wider than this many characters |
| \`align\` | \`"left"\` | The alignment of every column, or one per column |

## Licence

MIT
`;

/** Publishes {@link cachedNpm} into the source and fetches its document and tarball through the remote. */
async function cacheNpm(ctx: APIRequestContext, edge: string, auth: Record<string, string>): Promise<void> {
  const { name, version } = cachedNpm;
  const manifest = {
    name,
    version,
    description: 'Formats tabular data for terminals, Markdown and CSV.',
    keywords: ['table', 'markdown', 'csv', 'terminal'],
    license: 'MIT',
    author: 'Web platform team <web-platform@example.invalid>',
    homepage: 'https://git.example.invalid/web/table-format-kit',
    repository: { type: 'git', url: 'https://git.example.invalid/web/table-format-kit.git' },
    bugs: { url: 'https://git.example.invalid/web/table-format-kit/issues' },
    dependencies: { 'string-width': '^7.2.0', 'wrap-ansi': '^9.0.0' },
    peerDependencies: { chalk: '>=5' },
    engines: { node: '>=20' },
  };
  const tgz = gzipSync(
    Buffer.concat([
      tarEntry('package/package.json', Buffer.from(JSON.stringify(manifest, null, 2))),
      tarEntry('package/README.md', Buffer.from(npmReadme)),
      tarEntry('package/index.js', Buffer.from("export { format } from './format.js';\n")),
      Buffer.alloc(1024),
    ]),
  );
  const file = `${name}-${version}.tgz`;
  const source = `/api/npm/${otherProject}/${cachedSources.npm}`;
  const body = {
    _id: name,
    name,
    description: manifest.description,
    'dist-tags': { latest: version },
    versions: {
      [version]: {
        ...manifest,
        _id: `${name}@${version}`,
        dist: {
          integrity: `sha512-${createHash('sha512').update(tgz).digest('base64')}`,
          shasum: createHash('sha1').update(tgz).digest('hex'),
          tarball: `${edge}${source}/${name}/-/${file}`,
        },
      },
    },
    access: null,
    _attachments: { [file]: { content_type: 'application/octet-stream', data: tgz.toString('base64'), length: tgz.length } },
  };
  await expectStatus(await patient(() => ctx.put(`${source}/${name}`, { data: body, headers: auth })), [200, 201, 409], `npm publish ${name}@${version}`);
  const cache = `/api/npm/${mainProject}/${cachedSources.npmCache}`;
  await expectStatus(await patient(() => ctx.get(`${cache}/${name}`, { headers: auth })), [200], `${name} through ${cachedSources.npmCache}`);
  await expectStatus(await patient(() => ctx.get(`${cache}/${name}/-/${file}`, { headers: auth })), [200], `${file} through ${cachedSources.npmCache}`);
}

/** A `.deb` of {@link cachedDeb}: a control paragraph with the relation fields and a long description, and one script. */
function deb(): Buffer {
  const { name, version } = cachedDeb;
  const control =
    `Package: ${name}\nVersion: ${version}\nArchitecture: amd64\nMaintainer: Platform engineering <platform@example.invalid>\n` +
    'Installed-Size: 412\nDepends: libc6 (>= 2.36), libssl3 (>= 3.0.0), adduser\nRecommends: logrotate, ca-certificates\n' +
    'Suggests: acme-log-shipper-doc\nSection: admin\nPriority: optional\nHomepage: https://git.example.invalid/platform/acme-log-shipper\n' +
    'Description: ships the logs of a host to the platform log store\n' +
    ' acme-log-shipper follows the journal and the files under /var/log, batches the\n' +
    ' lines and sends them to the log store over TLS, retrying while it is away.\n' +
    ' .\n' +
    ' It keeps its position in each file across restarts, so no line is sent twice.\n';
  return ar([
    ['debian-binary', Buffer.from('2.0\n')],
    ['control.tar.gz', tarGz([['./', Buffer.alloc(0)], ['./control', Buffer.from(control)]])],
    ['data.tar.gz', tarGz([['./', Buffer.alloc(0)], ['./usr/', Buffer.alloc(0)], ['./usr/bin/', Buffer.alloc(0)], [`./usr/bin/${name}`, Buffer.from('#!/bin/sh\n')]])],
  ]);
}

/**
 * Uploads {@link cachedDeb} into the source, waits for the source's index, and fetches through the remote what apt fetches: the release,
 * the index (parsed by the job deb-remote-parse, which names the package file's package and version) and the package file.
 */
async function cacheDeb(ctx: APIRequestContext, auth: Record<string, string>): Promise<void> {
  const { name, version } = cachedDeb;
  const file = `${name}_${version}_amd64.deb`;
  const source = `/api/deb/${otherProject}/${cachedSources.deb}`;
  const upload = await patient(() =>
    ctx.put(`${source}/upload/${file}?distribution=stable&component=main`, {
      data: deb(),
      headers: { ...auth, 'Content-Type': 'application/vnd.debian.binary-package' },
    }),
  );
  await expectStatus(upload, [200, 201, 409], `deb upload ${file}`);
  const index = 'dists/stable/main/binary-amd64/Packages.gz';
  await until(60, `the index of ${cachedSources.deb} lists ${name}`, async () => (await ctx.get(`${source}/${index}`, { headers: auth })).ok());
  const cache = `/api/deb/${mainProject}/${cachedSources.debCache}`;
  await expectStatus(await patient(() => ctx.get(`${cache}/dists/stable/InRelease`, { headers: auth })), [200], `the release through ${cachedSources.debCache}`);
  await expectStatus(await patient(() => ctx.get(`${cache}/${index}`, { headers: auth })), [200], `the index through ${cachedSources.debCache}`);
  // The package file is fetched once the remote's parse of the index has its row: a file no parsed index lists is stored without a
  // version (R-DEB-022, Q-REPO-330). The catalogue search answers from the parsed rows (R-DEB-036).
  const search = `/api/v1/projects/${mainProject}/repositories/${cachedSources.debCache}/upstream/search?namesOnly=true&q=${name}`;
  await until(60, `the parse of the index of ${cachedSources.debCache}`, async () => {
    const res = await ctx.get(search, { headers: auth });
    return res.ok() && (await res.text()).includes(`"${name}"`);
  });
  // The pool path of a local repository: the source's prefix and name (R-DEB-008).
  const pool = `pool/${name[0]}/${name}/${file}`;
  await expectStatus(await patient(() => ctx.get(`${cache}/${pool}`, { headers: auth })), [200], `${file} through ${cachedSources.debCache}`);
}

/** The packages the list API answers for a repository, with their descriptions. */
async function listed(ctx: APIRequestContext, auth: Record<string, string>, repo: string, format: string): Promise<{ name: string; description?: string }[]> {
  const res = await ctx.get(`/api/v1/packages?format=${format}&repository=${encodeURIComponent(repo)}`, { headers: auth });
  if (!res.ok()) return [];
  return ((await res.json()) as { items: { name: string; description?: string }[] }).items;
}

async function until(seconds: number, what: string, ok: () => Promise<boolean>): Promise<void> {
  const deadline = Date.now() + seconds * 1000;
  while (!(await ok())) {
    if (Date.now() > deadline) throw new Error(`${what}: not within ${seconds} s`);
    await new Promise((r) => setTimeout(r, 1000));
  }
}

/**
 * Deletes the two source repositories once the remotes cached from them, as the break-glass account: a version page lists every
 * repository the reader may read that holds the version, and the pictures are of a version the remote alone holds, so that every fact
 * on them is the remote's. A run again creates the sources anew, publishes into them and deletes them again; the remotes keep what they
 * cached.
 */
export async function removeCachedSources(admin: Api): Promise<void> {
  for (const key of [cachedSources.npm, cachedSources.deb]) {
    const path = `/api/v1/projects/${otherProject}/repositories/${key}`;
    await expectStatus(await admin.delete(path), [202, 204, 404], `delete the source ${key}`);
    await until(120, `the deletion of ${key}`, async () => (await admin.get(path)).status() === 404);
  }
}

/**
 * The cached versions of both formats, as the power user, and the wait for their description: the package list of each remote answers
 * the description of its package once the job described the version (R-NPM-050, R-DEB-037).
 */
export async function seedCachedVersions(ctx: APIRequestContext, edge: string, token: string): Promise<void> {
  const auth = { Authorization: `Bearer ${token}` };
  await cacheNpm(ctx, edge, auth);
  await cacheDeb(ctx, auth);
  for (const [repo, format, name] of [
    [`${mainProject}/${cachedSources.npmCache}`, 'npm', cachedNpm.name],
    [`${mainProject}/${cachedSources.debCache}`, 'deb', cachedDeb.name],
  ] as const) {
    await until(120, `the description of ${name} in ${repo}`, async () =>
      (await listed(ctx, auth, repo, format)).some((p) => p.name === name && !!p.description),
    );
  }
}
