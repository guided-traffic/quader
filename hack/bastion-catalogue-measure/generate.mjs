// The generated large set of the catalogue measurement (README.md beside this file): one public project with a public local
// repository per format, filled through the formats' own publish routes with many small packages, one npm repository with many
// versions per package, Ansible collections with many versions and real documentation trees, Debian packages with file lists and
// documents, and OCI images of two platforms each with a Sigstore bundle referrer. The findings: npm through the OSV archive of the
// profile's advisory mirror (the seed's records kept, ours added), OCI by SQL as a scan would have left them (the profile runs no
// image scanner and must not fetch its database). The artifact bytes are tiny; the documents are what the measurement reads.
//
// Idempotent: a version that exists answers a conflict and is skipped. Sizes: GEN_SCALE (default 1) multiplies the package counts.
import { createHash } from 'node:crypto';
import { gzipSync } from 'node:zlib';
import { Client, ar, instance, lit, psql, rng, sha256, sleep, tarEntry, tarGz, unzip, words, zip } from './lib.mjs';

const inst = instance();
const scale = Number(process.env.GEN_SCALE ?? '1');
const project = 'catalogue-measure';
const repos = {
  npm: 'npm-large',
  nuget: 'nuget-large',
  oci: 'images-large',
  deb: 'deb-large',
  ansible: 'collections-large',
  generic: 'files-large',
};

const counts = {
  npmMany: Math.round(20 * scale), // packages with 50 versions each
  npmVersions: 50,
  npmSmall: Math.round(200 * scale), // packages with 2 versions each
  nuget: Math.round(150 * scale), // 3 versions each
  deb: Math.round(100 * scale), // 2 versions each
  ansibleMany: Math.round(8 * scale), // 50 versions each
  ansibleVersions: 50,
  ansibleSmall: Math.round(30 * scale), // 2 versions each
  images: Math.round(30 * scale), // 5 tags each, an index of two platforms per tag
  generic: Math.round(300 * scale),
};

const log = (...a) => console.log(new Date().toISOString().slice(11, 19), ...a);

async function main() {
  const admin = await Client.breakGlass(inst);
  admin.retry503 = true;
  await admin.expect('POST', '/api/v1/projects', { json: { key: project, displayName: 'Catalogue measurement', visibility: 'public', description: 'The generated large set of the catalogue measurement.' } }, [201, 409]);
  for (const [format, key] of Object.entries(repos)) {
    await admin.expect('POST', `/api/v1/projects/${project}/repositories`, { json: { key, class: 'local', format, visibility: { artifacts: 'public', wiki: 'public' } } }, [201, 409]);
  }
  // A local repository's names are its own (claimed): public advisories match them only when asked (R-VULN-002), as for vendored code.
  await admin.expect('PATCH', `/api/v1/projects/${project}/repositories/${repos.npm}`, { json: { vulnerabilities: { matchPublicAdvisories: 'always' } }, headers: { 'Content-Type': 'application/merge-patch+json', 'If-Match': '*' } }, [200]);
  // The break-glass account holds no tokens: each repository gets a service account with read and deploy and a technical token of a
  // day, revoked at the end.
  const secrets = [];
  const publisher = async (format) => {
    const base = `/api/v1/projects/${project}/repositories/${repos[format]}/service-accounts`;
    await admin.expect('POST', base, { json: { name: 'measure-publisher', description: 'Publishes the generated set.', actions: ['read', 'deploy'] } }, [201, 409]);
    const created = await admin.json('POST', `${base}/measure-publisher/technical-tokens`, { json: { name: `gen-${Date.now()}`, description: '', ttlSeconds: 86400, scope: { inherit: true } } }, [201]);
    secrets.push(created.secretId);
    return Client.withToken(inst, created.token);
  };
  try {
    await npm(await publisher('npm'));
    await nuget(await publisher('nuget'));
    await deb(await publisher('deb'));
    await ansible(await publisher('ansible'));
    await generic(await publisher('generic'));
    await oci(await publisher('oci'), 'measure-publisher');
  } finally {
    for (const s of secrets) await admin.expect('DELETE', `/api/v1/tokens/${s}`, {}, [204, 404]);
  }
  await npmAdvisories(admin);
  ociFindings();
  log('done');
}

/** Runs `fn` over `items` with `n` at a time. */
async function pool(items, n, fn) {
  let next = 0;
  let done = 0;
  const workers = Array.from({ length: n }, async () => {
    while (next < items.length) {
      const item = items[next++];
      await fn(item);
      if (++done % 200 === 0) log(`  ${done}/${items.length}`);
    }
  });
  await Promise.all(workers);
}

// ---------------------------------------------------------------------------------------------------------------------------------- npm

/** The README of an npm package: the same text for every patch release of a minor (a README changes when the package does). */
function npmReadme(name, minor) {
  const r = rng(parseInt(sha256(`${name}:${minor}`).slice(0, 8), 16));
  const section = (title, n) => `## ${title}\n\n${Array.from({ length: n }, () => words(r, 25 + Math.floor(r() * 30)) + '.').join('\n\n')}\n`;
  const code = (n) => '```js\n' + Array.from({ length: n }, (_, i) => `const ${words(r, 1)}${i} = await client.${words(r, 1)}({ ${words(r, 1)}: '${words(r, 2)}', retries: ${i} });`).join('\n') + '\n```\n';
  const table = '| Option | Type | Default | Description |\n|---|---|---|---|\n' +
    Array.from({ length: 8 + Math.floor(r() * 10) }, () => `| \`${words(r, 1)}\` | \`${['string', 'number', 'boolean'][Math.floor(r() * 3)]}\` | \`${words(r, 1)}\` | ${words(r, 12)}. |`).join('\n') + '\n';
  return [
    `# ${name}\n`,
    `[![build](https://ci.example.invalid/${name}/badge.svg)](https://ci.example.invalid/${name}) [![coverage](https://cov.example.invalid/${name}.svg)](https://cov.example.invalid/${name})\n`,
    words(r, 40) + '.\n',
    '## Install\n\n```sh\nnpm install ' + name + '\n```\n',
    section('Usage', 2),
    code(6 + Math.floor(r() * 8)),
    section('Configuration', 2),
    table,
    section('How it works', 3),
    code(4),
    section('Compatibility', 1),
    `## Licence\n\nMIT, see [LICENSE](LICENSE). Release line ${minor}.x.\n`,
  ].join('\n');
}

function npmBody(name, version, readme, deps) {
  const manifest = { name, version, description: `${name}: ${words(rng(name.length), 10)}`, license: 'MIT', keywords: ['catalogue', 'measure', name.split('-')[1] ?? 'x'], dependencies: deps, repository: { type: 'git', url: `https://git.example.invalid/${name}.git` }, homepage: `https://docs.example.invalid/${name}` };
  const tgz = gzipSync(Buffer.concat([tarEntry('package/package.json', Buffer.from(JSON.stringify(manifest, null, 2))), tarEntry('package/README.md', Buffer.from(readme)), tarEntry('package/index.js', Buffer.from(`module.exports = '${name}@${version}';\n`)), Buffer.alloc(1024)]));
  const file = `${name}-${version}.tgz`;
  return {
    _id: name,
    name,
    description: manifest.description,
    'dist-tags': { latest: version },
    readme,
    versions: {
      [version]: {
        ...manifest,
        readme,
        _id: `${name}@${version}`,
        dist: {
          integrity: `sha512-${createHash('sha512').update(tgz).digest('base64')}`,
          shasum: createHash('sha1').update(tgz).digest('hex'),
          tarball: `${inst.origin}/api/npm/${project}/${repos.npm}/${name}/-/${file}`,
        },
      },
    },
    access: null,
    _attachments: { [file]: { content_type: 'application/octet-stream', data: tgz.toString('base64'), length: tgz.length } },
  };
}

function npmDeps(r) {
  const deps = {};
  for (let i = 0; i < 3 + Math.floor(r() * 8); i++) deps[`dep-${words(r, 1)}-${i}`] = `^${1 + Math.floor(r() * 5)}.${Math.floor(r() * 10)}.0`;
  return deps;
}

const npmManyName = (i) => `measure-many-${String(i).padStart(3, '0')}`;
const npmManyVersion = (v) => `1.${Math.floor(v / 5)}.${v % 5}`;

async function npm(c) {
  const jobs = [];
  for (let i = 0; i < counts.npmMany; i++) {
    const name = npmManyName(i);
    for (let v = 0; v < counts.npmVersions; v++) jobs.push({ name, version: npmManyVersion(v), minor: Math.floor(v / 5) });
  }
  for (let i = 0; i < counts.npmSmall; i++) {
    const name = `measure-small-${String(i).padStart(4, '0')}`;
    jobs.push({ name, version: '0.1.0', minor: 0 }, { name, version: '0.2.0', minor: 1 });
  }
  log(`npm: ${jobs.length} versions`);
  // Versions of one package in order (dist-tags), packages in parallel.
  const byName = Map.groupBy(jobs, (j) => j.name);
  await pool([...byName.values()], 6, async (versions) => {
    for (const j of versions) {
      const r = rng(parseInt(sha256(j.name).slice(0, 8), 16));
      const body = npmBody(j.name, j.version, npmReadme(j.name, j.minor), npmDeps(r));
      await c.expect('PUT', `/api/npm/${project}/${repos.npm}/${j.name}`, { json: body }, [200, 201, 409]);
    }
  });
}

// --------------------------------------------------------------------------------------------------------------------------------- NuGet

function nupkg(id, version, withReadme) {
  const r = rng(parseInt(sha256(id).slice(0, 8), 16));
  const nuspec = `<?xml version="1.0" encoding="utf-8"?>
<package xmlns="http://schemas.microsoft.com/packaging/2013/05/nuspec.xsd">
  <metadata>
    <id>${id}</id>
    <version>${version}</version>
    <authors>Catalogue measurement</authors>
    <description>${words(r, 30)}</description>
    <license type="expression">MIT</license>
    <projectUrl>https://git.example.invalid/${id}</projectUrl>
    <repository type="git" url="https://git.example.invalid/${id}.git" />
    <tags>${words(r, 4)}</tags>
    ${withReadme ? '<readme>docs/README.md</readme>' : ''}
    <dependencies>
      <group targetFramework="net8.0">
        <dependency id="Microsoft.Extensions.Logging.Abstractions" version="8.0.0" />
        <dependency id="System.Text.Json" version="8.0.4" />
      </group>
      <group targetFramework=".NETStandard2.0" />
    </dependencies>
  </metadata>
</package>
`;
  const entries = [
    { name: `${id}.nuspec`, data: Buffer.from(nuspec) },
    { name: `lib/net8.0/${id}.dll`, data: Buffer.from(`MZ ${id} ${version}`) },
  ];
  if (withReadme) entries.push({ name: 'docs/README.md', data: Buffer.from(npmReadme(id, version.split('.')[1])) });
  return zip(entries);
}

async function nuget(c) {
  const jobs = [];
  for (let i = 0; i < counts.nuget; i++) for (const v of ['1.0.0', '1.1.0', '2.0.0']) jobs.push({ id: `Measure.Library${String(i).padStart(4, '0')}`, version: v, readme: i % 2 === 0 });
  log(`nuget: ${jobs.length} versions`);
  await pool(jobs, 6, async (j) => {
    await c.expect('PUT', `/api/nuget/${project}/${repos.nuget}/v3/package`, { body: nupkg(j.id, j.version, j.readme), headers: { 'Content-Type': 'application/octet-stream' } }, [200, 201, 202, 409]);
  });
}

// -------------------------------------------------------------------------------------------------------------------------------- Debian

function debFile(name, version, files) {
  const r = rng(parseInt(sha256(name).slice(0, 8), 16));
  const control =
    `Package: ${name}\nVersion: ${version}\nArchitecture: amd64\nMaintainer: Catalogue measurement <measure@example.invalid>\n` +
    `Installed-Size: ${files * 4}\nDepends: libc6 (>= 2.36), libssl3 (>= 3.0.0), ca-certificates\nSection: admin\nPriority: optional\n` +
    `Homepage: https://git.example.invalid/${name}\nDescription: ${words(r, 6)}\n ${words(r, 30)}\n .\n ${words(r, 30)}\n`;
  const changelog = Array.from({ length: 20 }, (_, i) => `${name} (1.${20 - i}-1) unstable; urgency=medium\n\n  * ${words(r, 14)}.\n  * ${words(r, 10)}.\n\n -- Catalogue measurement <measure@example.invalid>  Mon, 0${(i % 9) + 1} Jun 2026 10:00:00 +0000\n`).join('\n');
  const copyright = `Format: https://www.debian.org/doc/packaging-manuals/copyright-format/1.0/\nUpstream-Name: ${name}\n\nFiles: *\nCopyright: 2026 Catalogue measurement\nLicense: MIT\n ${words(r, 120)}\n`;
  const data = [['./', Buffer.alloc(0)], ['./usr/', Buffer.alloc(0)], ['./usr/lib/', Buffer.alloc(0)], [`./usr/lib/${name}/`, Buffer.alloc(0)], ['./usr/share/', Buffer.alloc(0)], ['./usr/share/doc/', Buffer.alloc(0)], [`./usr/share/doc/${name}/`, Buffer.alloc(0)]];
  data.push([`./usr/share/doc/${name}/changelog.Debian.gz`, gzipSync(Buffer.from(changelog))], [`./usr/share/doc/${name}/copyright`, Buffer.from(copyright)]);
  for (let i = 0; i < files; i++) data.push([`./usr/lib/${name}/${words(r, 1)}-${i}.so`, Buffer.from(`${i}`)]);
  return ar([
    ['debian-binary', Buffer.from('2.0\n')],
    ['control.tar.gz', tarGz([['./', Buffer.alloc(0)], ['./control', Buffer.from(control)]])],
    ['data.tar.gz', tarGz(data)],
  ]);
}

async function deb(c) {
  const jobs = [];
  // File counts of a library package to a large one: 10 to 640.
  for (let i = 0; i < counts.deb; i++) for (const v of ['1.0-1', '1.1-1']) jobs.push({ name: `measure-deb-${String(i).padStart(4, '0')}`, version: v, files: 10 * 2 ** (i % 7) });
  log(`deb: ${jobs.length} versions`);
  await pool(jobs, 4, async (j) => {
    const file = `${j.name}_${j.version}_amd64.deb`;
    await c.expect('PUT', `/api/deb/${project}/${repos.deb}/upload/${file}?distribution=stable&component=main`, { body: debFile(j.name, j.version, j.files), headers: { 'Content-Type': 'application/vnd.debian.binary-package' } }, [200, 201, 409]);
  });
}

// ------------------------------------------------------------------------------------------------------------------------------- Ansible

/**
 * A module's documentation as a real collection writes it: a description, 8 to 20 options, some with suboptions and choices, examples
 * and return values. `rev` is the module's revision: each revision adds one option with its `version_added`.
 */
function moduleDoc(ns, coll, mod, rev, revVersions) {
  const r = rng(parseInt(sha256(`${ns}.${coll}.${mod}`).slice(0, 8), 16));
  const n = 8 + Math.floor(r() * 12);
  const opts = [];
  for (let i = 0; i < n; i++) {
    const type = ['str', 'int', 'bool', 'list', 'dict', 'path'][Math.floor(r() * 6)];
    let o = `  ${words(r, 1)}_${i}:\n    description:\n      - ${words(r, 18)}.\n      - ${words(r, 12)} O(state=present).\n    type: ${type}\n`;
    if (type === 'list') o += '    elements: str\n';
    if (r() < 0.3) o += `    choices: [${words(r, 3).split(' ').join(', ')}]\n`;
    if (r() < 0.4) o += `    default: ${type === 'bool' ? 'false' : type === 'int' ? 10 : 'auto'}\n`;
    if (type === 'dict') o += `    suboptions:\n      ${words(r, 1)}:\n        description: ${words(r, 10)}.\n        type: str\n      ${words(r, 1)}_b:\n        description: ${words(r, 8)}.\n        type: int\n`;
    opts.push(o);
  }
  for (let k = 0; k < rev; k++) opts.push(`  added_${k}:\n    description: ${words(r, 14)}.\n    type: str\n    version_added: "${revVersions[k]}"\n`);
  const examples = Array.from({ length: 3 }, (_, i) => `- name: ${words(r, 5)}\n  ${ns}.${coll}.${mod}:\n    state: present\n    ${words(r, 1)}_${i}: ${words(r, 1)}\n    retries: ${i + 2}\n  register: result${i}\n`).join('\n');
  const ret = Array.from({ length: 4 }, () => `${words(r, 1)}:\n  description: ${words(r, 12)}.\n  returned: success\n  type: str\n  sample: ${words(r, 1)}\n`).join('');
  return `#!/usr/bin/python
DOCUMENTATION = r'''
module: ${mod}
short_description: ${words(r, 7)}
version_added: "1.0.0"
description:
  - ${words(r, 30)}.
  - ${words(r, 25)}.
author:
  - Catalogue measurement (@measure)
options:
${opts.join('')}'''

EXAMPLES = r'''
${examples}'''

RETURN = r'''
${ret}'''
`;
}

function roleSpecs(ns, coll, role) {
  const r = rng(parseInt(sha256(`${ns}.${coll}.${role}`).slice(0, 8), 16));
  const opts = Array.from({ length: 6 + Math.floor(r() * 10) }, (_, i) => `      ${role}_${words(r, 1)}_${i}:\n        description: ${words(r, 14)}.\n        type: ${['str', 'int', 'bool'][i % 3]}\n${i % 3 === 1 ? '        default: 5\n' : ''}`).join('');
  return `---\nargument_specs:\n  main:\n    short_description: ${words(r, 6)}\n    description:\n      - ${words(r, 20)}.\n    options:\n${opts}`;
}

/**
 * The collection at version index `v` of `total`: 25 modules at the start, one more every fifth version, two modules revised per version
 * (rotating), five roles, one lookup and one filter; the README changes with the minor.
 */
function collectionArchive(ns, coll, v, version, versionOf, small) {
  const modules = small ? 8 : 25 + Math.floor(v / 5);
  const files = [];
  const dirs = ['meta', 'plugins', 'plugins/modules', 'plugins/lookup', 'roles', 'docs'];
  for (let m = 0; m < modules; m++) {
    // Module m is revised at the versions where (version index) % modules hits m or m+1: two per version.
    const revs = [];
    for (let k = 1; k <= v; k++) if (k % modules === m || (k + 1) % modules === m) revs.push(versionOf(k));
    files.push([`plugins/modules/mod_${String(m).padStart(2, '0')}.py`, Buffer.from(moduleDoc(ns, coll, `mod_${String(m).padStart(2, '0')}`, revs.length, revs))]);
  }
  files.push(['plugins/lookup/settings.py', Buffer.from(moduleDoc(ns, coll, 'settings', 0, []).replace('module: settings', 'name: settings'))]);
  const roles = small ? 2 : 5;
  for (let k = 0; k < roles; k++) {
    const role = `role_${k}`;
    dirs.push(`roles/${role}`, `roles/${role}/meta`, `roles/${role}/tasks`);
    files.push([`roles/${role}/meta/argument_specs.yml`, Buffer.from(roleSpecs(ns, coll, role))]);
    files.push([`roles/${role}/meta/main.yml`, Buffer.from(`---\ngalaxy_info:\n  author: measure\n  description: ${role}\n  license: MIT\n  min_ansible_version: '2.15'\n  platforms: []\ndependencies: []\n`)]);
    files.push([`roles/${role}/tasks/main.yml`, Buffer.from(`---\n- name: ${role}\n  ansible.builtin.debug:\n    msg: ${role}\n`)]);
  }
  files.push(['README.md', Buffer.from(npmReadme(`${ns}.${coll}`, version.split('.').slice(0, 2).join('.')))]);
  files.push(['docs/guide.md', Buffer.from(`# Guide\n\n${words(rng(coll.length), 300)}\n`)]);
  files.push(['meta/runtime.yml', Buffer.from("requires_ansible: '>=2.15.0'\n")]);
  const list = [{ name: '.', ftype: 'dir', chksum_type: null, chksum_sha256: null, format: 1 }, ...[...dirs.map((d) => [d, null]), ...files].sort((a, b) => a[0].localeCompare(b[0])).map(([n, data]) => (data === null ? { name: n, ftype: 'dir', chksum_type: null, chksum_sha256: null, format: 1 } : { name: n, ftype: 'file', chksum_type: 'sha256', chksum_sha256: sha256(data), format: 1 }))];
  const filesJson = Buffer.from(JSON.stringify({ files: list, format: 1 }, null, 4));
  const manifest = Buffer.from(JSON.stringify({
    collection_info: { namespace: ns, name: coll, version, authors: ['Catalogue measurement <measure@example.invalid>'], readme: 'README.md', tags: ['measure', 'catalogue'], description: `The collection ${coll} of the measurement`, license: ['MIT'], license_file: null, dependencies: {}, repository: `https://git.example.invalid/${ns}/${coll}`, documentation: null, homepage: null, issues: null },
    file_manifest_file: { name: 'FILES.json', ftype: 'file', chksum_type: 'sha256', chksum_sha256: sha256(filesJson), format: 1 },
    format: 1,
  }, null, 4));
  return tarGz([['FILES.json', filesJson], ['MANIFEST.json', manifest], ...dirs.map((d) => [`${d}/`, Buffer.alloc(0)]), ...files]);
}

const ansibleVersion = (v) => `${1 + Math.floor(v / 10)}.${v % 10}.0`;

async function ansible(c) {
  const jobs = [];
  for (let i = 0; i < counts.ansibleMany; i++) for (let v = 0; v < counts.ansibleVersions; v++) jobs.push({ ns: 'measure', coll: `big${String(i).padStart(2, '0')}`, v, small: false });
  for (let i = 0; i < counts.ansibleSmall; i++) for (let v = 0; v < 2; v++) jobs.push({ ns: 'measure', coll: `small${String(i).padStart(3, '0')}`, v, small: true });
  log(`ansible: ${jobs.length} versions`);
  const byName = Map.groupBy(jobs, (j) => j.coll);
  await pool([...byName.values()], 4, async (versions) => {
    for (const j of versions) {
      const version = ansibleVersion(j.v);
      const tgz = collectionArchive(j.ns, j.coll, j.v, version, ansibleVersion, j.small);
      const form = new FormData();
      form.set('sha256', sha256(tgz));
      form.set('file', new Blob([tgz], { type: 'application/gzip' }), `${j.ns}-${j.coll}-${version}.tar.gz`);
      const res = await fetch(new URL(`/api/ansible/${project}/${repos.ansible}/v3/artifacts/collections/`, inst.base), { method: 'POST', headers: { Authorization: `Bearer ${c.bearer}` }, body: form });
      const text = await res.text();
      if (res.status === 429 || res.status === 503) {
        await sleep(1000);
        versions.push(j);
        continue;
      }
      if (![200, 201, 202, 409].includes(res.status)) throw new Error(`ansible publish ${j.coll} ${version}: ${res.status} ${text.slice(0, 300)}`);
    }
  });
}

// ------------------------------------------------------------------------------------------------------------------------------- generic

async function generic(c) {
  const jobs = Array.from({ length: counts.generic }, (_, i) => ({ path: `releases/tool-${String(Math.floor(i / 10)).padStart(3, '0')}/${i % 10}.0.0/tool-${i}.tar.gz` }));
  log(`generic: ${jobs.length} files`);
  await pool(jobs, 6, async (j) => {
    const head = await c.send('HEAD', `/files/${project}/${repos.generic}/${j.path}`);
    await head.arrayBuffer();
    if (head.status === 200) return;
    await c.expect('PUT', `/files/${project}/${repos.generic}/${j.path}`, { body: Buffer.from(`${j.path}\n`), headers: { 'Content-Type': 'application/octet-stream' } }, [200, 201]);
  });
}

// ----------------------------------------------------------------------------------------------------------------------------------- OCI

const ociDigests = []; // { name, digest } of every platform manifest, for the findings

async function registryToken(c, user, repo) {
  const ch = await fetch(new URL('/v2/', inst.base));
  await ch.arrayBuffer();
  const challenge = ch.headers.get('www-authenticate') ?? '';
  const realm = /realm="([^"]+)"/.exec(challenge)?.[1];
  const service = /service="([^"]+)"/.exec(challenge)?.[1];
  const url = new URL(realm, inst.base);
  url.searchParams.set('service', service);
  url.searchParams.set('scope', `repository:${repo}:pull,push`);
  const res = await fetch(new URL(url.pathname + url.search, inst.base), { headers: { Authorization: `Basic ${Buffer.from(`${user}:${c.bearer}`).toString('base64')}` } });
  const body = await res.json();
  if (!res.ok) throw new Error(`registry token: ${res.status} ${JSON.stringify(body)}`);
  return body.token ?? body.access_token;
}

async function oci(c, user) {
  log(`oci: ${counts.images} images x 5 tags`);
  const digestOf = (b) => `sha256:${sha256(b)}`;
  await pool(Array.from({ length: counts.images }, (_, i) => i), 4, async (i) => {
    const name = `measure/app-${String(i).padStart(3, '0')}`;
    const repo = `${project}/${repos.oci}/${name}`;
    const reg = Client.withToken(inst, await registryToken(c, user, repo));
    const upload = async (blob) => {
      const digest = digestOf(blob);
      const head = await reg.send('HEAD', `/v2/${repo}/blobs/${digest}`);
      await head.arrayBuffer();
      if (head.status === 200) return digest;
      const start = await reg.send('POST', `/v2/${repo}/blobs/uploads/`);
      await start.arrayBuffer();
      if (start.status !== 202) throw new Error(`start upload ${repo}: ${start.status}`);
      const location = new URL(start.headers.get('location'), inst.base);
      location.searchParams.set('digest', digest);
      await reg.expect('PUT', location.pathname + location.search, { body: blob, headers: { 'Content-Type': 'application/octet-stream' } }, [201]);
      return digest;
    };
    const putManifest = async (ref, body, type) => {
      await reg.expect('PUT', `/v2/${repo}/manifests/${ref}`, { body, headers: { 'Content-Type': type } }, [201]);
    };
    const layers = [];
    for (let l = 0; l < 3; l++) {
      const layer = gzipSync(tarEntry(`etc/layer-${l}`, Buffer.from(`${name} ${l}\n`)));
      layers.push({ mediaType: 'application/vnd.oci.image.layer.v1.tar+gzip', digest: await upload(layer), size: layer.length });
    }
    const tags = ['1.0.0', '1.1.0', '1.2.0', '2.0.0', 'latest'];
    for (const [t, tag] of tags.entries()) {
      const platforms = [];
      for (const arch of ['amd64', 'arm64']) {
        const config = Buffer.from(JSON.stringify({
          architecture: arch, os: 'linux',
          config: { Env: ['PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin', `APP_VERSION=${tag}`], Labels: { 'org.opencontainers.image.title': name, 'org.opencontainers.image.version': tag, 'org.opencontainers.image.source': `https://git.example.invalid/${name}` } },
          history: Array.from({ length: 12 }, (_, h) => ({ created: '2026-11-01T00:00:00Z', created_by: `/bin/sh -c ${words(rng(h + 1), 12)}` })),
          rootfs: { type: 'layers', diff_ids: layers.map((x) => x.digest) },
        }));
        const manifest = Buffer.from(JSON.stringify({ schemaVersion: 2, mediaType: 'application/vnd.oci.image.manifest.v1+json', config: { mediaType: 'application/vnd.oci.image.config.v1+json', digest: await upload(config), size: config.length }, layers }));
        const digest = digestOf(manifest);
        await putManifest(digest, manifest, 'application/vnd.oci.image.manifest.v1+json');
        platforms.push({ mediaType: 'application/vnd.oci.image.manifest.v1+json', digest, size: manifest.length, platform: { architecture: arch, os: 'linux' } });
        ociDigests.push({ name, digest, tagIndex: t, arch });
      }
      const index = Buffer.from(JSON.stringify({ schemaVersion: 2, mediaType: 'application/vnd.oci.image.index.v1+json', manifests: platforms, annotations: { 'org.opencontainers.image.description': `The image ${name} of the measurement` } }));
      const indexDigest = digestOf(index);
      if (tag === 'latest') {
        // latest names the same index as 2.0.0, as a release pipeline tags it.
        continue;
      }
      await putManifest(tag, index, 'application/vnd.oci.image.index.v1+json');
      if (t === 3) await putManifest('latest', index, 'application/vnd.oci.image.index.v1+json');
      // A Sigstore bundle referrer of the index, about the size cosign 3 writes (a certificate, a log entry, the signature).
      const bundle = Buffer.from(JSON.stringify({ mediaType: 'application/vnd.dev.sigstore.bundle.v0.3+json', verificationMaterial: { certificate: { rawBytes: Buffer.from(words(rng(t + i + 7), 260)).toString('base64') }, tlogEntries: [{ logIndex: String(1e8 + i * 10 + t), logId: { keyId: sha256(name) }, kindVersion: { kind: 'hashedrekord', version: '0.0.1' }, integratedTime: '1767225600', inclusionProof: { hashes: Array.from({ length: 20 }, (_, k) => sha256(`${k}${name}`)) }, canonicalizedBody: Buffer.from(words(rng(i), 80)).toString('base64') }] }, messageSignature: { messageDigest: { algorithm: 'SHA2_256', digest: Buffer.from(indexDigest).toString('base64') }, signature: Buffer.from(sha256(index)).toString('base64') } }));
      const empty = Buffer.from('{}');
      const referrer = Buffer.from(JSON.stringify({ schemaVersion: 2, mediaType: 'application/vnd.oci.image.manifest.v1+json', artifactType: 'application/vnd.dev.sigstore.bundle.v0.3+json', config: { mediaType: 'application/vnd.oci.empty.v1+json', digest: await upload(empty), size: 2 }, layers: [{ mediaType: 'application/vnd.dev.sigstore.bundle.v0.3+json', digest: await upload(bundle), size: bundle.length, annotations: { 'dev.sigstore.bundle.content': 'message-signature', 'dev.sigstore.bundle.predicateType': 'https://sigstore.dev/cosign/sign/v1' } }], subject: { mediaType: 'application/vnd.oci.image.index.v1+json', digest: indexDigest, size: index.length } }));
      await putManifest(digestOf(referrer), referrer, 'application/vnd.oci.image.manifest.v1+json');
    }
  });
}

/**
 * The findings of every platform manifest, as a scan would have left them (R-VULN-041): a scan row, the advisories, the findings with
 * the image's packages. A Debian-based image of a service holds tens to a few hundred findings; the counts below cycle through
 * 0, 12, 40, 90, 160 and 320 by image, the same for both platforms of a tag, and fall with a newer tag (fixes land).
 */
function ociFindings() {
  const advisories = 600;
  const severities = [['critical', 5], ['high', 4], ['medium', 3], ['low', 2]];
  let sql = 'BEGIN;\n';
  for (let a = 0; a < advisories; a++) {
    const r = rng(a + 11);
    const [sev] = severities[a % 4];
    const id = `CVE-2026-${String(10000 + a)}`;
    const refs = JSON.stringify(Array.from({ length: 4 }, (_, k) => ({ type: k === 0 ? 'ADVISORY' : 'WEB', url: `https://security.example.invalid/${id}/${k}` })));
    sql += `INSERT INTO vuln_advisory (ecosystem, id, canonical_id, modified, published, kind, summary, details, severity, severity_source, cvss_version, cvss_vector, cvss_score, "references", cwe_ids, record_sha256)
VALUES ('OCI', ${lit(id)}, ${lit(id)}, now(), now() - interval '30 days', 'vulnerability', ${lit(words(r, 12))}, ${lit(words(r, 90))}, '${sev}', 'cvss', '3.1', 'CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:H/A:H', ${(4 + (a % 6)).toFixed(1)}, ${lit(refs)}::jsonb, ARRAY['CWE-${100 + (a % 50)}'], decode('${sha256(id)}', 'hex'))
ON CONFLICT DO NOTHING;\n`;
  }
  const cycle = [0, 12, 40, 90, 160, 320];
  for (const m of ociDigests) {
    const img = Number(m.name.slice(-3));
    const n = Math.max(0, cycle[img % cycle.length] - m.tagIndex * Math.floor(cycle[img % cycle.length] / 6));
    const repo = `(SELECT r.id FROM repositories r JOIN project p ON p.id = r.project_id WHERE p.key = ${lit(project)} AND r.key = ${lit(repos.oci)})`;
    const hex = m.digest.slice(7);
    sql += `INSERT INTO vuln_image_scan (repo_id, name, digest, state, db_digest, scanned_at, os_family, os_name, packages) VALUES (${repo}, ${lit(m.name)}, decode('${hex}', 'hex'), 'scanned', 'sha256:measure', now(), 'debian', '12.8', 180)
ON CONFLICT (repo_id, name, digest) DO UPDATE SET state = 'scanned', db_digest = 'sha256:measure', scanned_at = now(), os_family = 'debian', os_name = '12.8', packages = 180;\n`;
    for (let k = 0; k < n; k++) {
      const a = (img * 37 + k) % advisories;
      const [sev, rank] = severities[a % 4];
      const r = rng(a + 101);
      const comps = JSON.stringify(Array.from({ length: 1 + (a % 3) }, (_, q) => ({ type: 'debian', class: 'os-pkgs', name: `lib${words(r, 1)}${q}`, version: `1.${a % 9}.${q}-1`, ...(a % 2 === 0 ? { fixedVersion: `1.${a % 9}.${q + 1}-1` } : {}) })));
      sql += `INSERT INTO vuln_finding (id, repo_id, ecosystem, package_name, version, advisory_id, kind, severity, severity_rank, cvss_score, fix_available, fixed_versions, advisory_modified, components)
VALUES (gen_random_uuid(), ${repo}, 'OCI', ${lit(m.name)}, ${lit(m.digest)}, 'CVE-2026-${10000 + a}', 'vulnerability', '${sev}', ${rank}, ${(4 + (a % 6)).toFixed(1)}, ${a % 2 === 0}, ${a % 2 === 0 ? `ARRAY['1.${a % 9}.1-1']` : "'{}'"}, now(), ${lit(comps)}::jsonb)
ON CONFLICT DO NOTHING;\n`;
    }
  }
  sql += 'COMMIT;\n';
  log(`oci findings: ${ociDigests.length} manifests`);
  psql(inst, sql);
}

// ----------------------------------------------------------------------------------------------------------------- npm advisories (OSV)

/**
 * The OSV archive of the profile's mirror with our records added: each many-version package has six advisories over version ranges
 * (an advisory affects a range of releases until its fix), so that a version carries 0 to 6 open findings. The seed's records are kept.
 */
async function npmAdvisories(admin) {
  const path = '/api/v1/projects/platform/repositories/advisory-mirror/content/osv/npm/all.zip';
  const res = await admin.send('GET', path);
  const current = res.status === 200 ? unzip(Buffer.from(await res.arrayBuffer())) : (await res.arrayBuffer(), []);
  const kept = current.filter((e) => !e.name.startsWith('MEASURE-'));
  const ours = [];
  const sevs = ['CRITICAL', 'HIGH', 'MODERATE', 'LOW'];
  for (let i = 0; i < counts.npmMany; i++) {
    for (let a = 0; a < 6; a++) {
      const id = `MEASURE-2026-${String(i * 10 + a).padStart(5, '0')}`;
      const from = (a * 7 + i) % counts.npmVersions;
      const to = Math.min(counts.npmVersions, from + 10 + a * 4);
      const affected = { package: { ecosystem: 'npm', name: npmManyName(i) }, ranges: [{ type: 'SEMVER', events: [{ introduced: npmManyVersion(from) }, ...(to < counts.npmVersions ? [{ fixed: npmManyVersion(to) }] : [])] }] };
      const body = { schema_version: '1.6.0', id, modified: '2026-11-01T00:00:00Z', published: '2026-10-01T00:00:00Z', summary: `Measurement advisory ${a} of ${npmManyName(i)}: ${words(rng(i * 10 + a + 1), 8)}`, details: words(rng(i * 10 + a + 3), 120), affected: [affected], references: Array.from({ length: 3 }, (_, k) => ({ type: 'WEB', url: `https://security.example.invalid/${id}/${k}` })), database_specific: { severity: sevs[a % 4], cwe_ids: [`CWE-${200 + a}`] } };
      ours.push({ name: `${id}.json`, data: Buffer.from(JSON.stringify(body)) });
    }
  }
  await admin.expect('PUT', path, { body: zip([...kept, ...ours]), headers: { 'Content-Type': 'application/zip' } }, [200, 201]);
  await admin.expect('POST', '/api/v1/system/tasks/vuln-sync-osv/run', { json: {} }, [202, 409]);
  log(`npm advisories: ${kept.length} kept, ${ours.length} added; sync started`);
}

await main();
