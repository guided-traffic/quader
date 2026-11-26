// Reads every document the Bastion's public catalogue will be made of (the document paths of W-35 P5), from the main installation's
// existing reads, as the anonymous caller where the read is public and as the break-glass account where it is not yet (the version's
// vulnerabilities, which P3 opens to the anonymous caller with the open findings only). One request at a time, so that the sum of
// the times is what one builder worker spends; README.md beside this file says what the numbers stand for.
//
//   node measure.mjs <out-dir> <pass>     writes <out-dir>/docs-<pass>.jsonl, one line per document, and <out-dir>/pass-<pass>.json
//
// A line: {path, kind, format, pkg, version, raw, gzip, sha, ms}. The statements per document: after the walk, up to 200 documents of
// each kind are read again in a row and the request pool's acquisitions (the ops listener's
// quader_db_pool_acquire_duration_seconds_count) counted over them, less the idle rate of the background jobs measured first.
import { createWriteStream, mkdirSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';
import { performance } from 'node:perf_hooks';
import { gzipSync } from 'node:zlib';
import { Client, instance, sha256, sleep } from './lib.mjs';

const inst = instance();
const [outDir, pass] = process.argv.slice(2);
if (!outDir || !pass) throw new Error('usage: node measure.mjs <out-dir> <pass>');
mkdirSync(outDir, { recursive: true });
const out = createWriteStream(join(outDir, `docs-${pass}.jsonl`));

const anon = new Client(inst.base, inst.origin);
const admin = await Client.breakGlass(inst);
const enc = encodeURIComponent;

/** The request pool's acquisitions so far, from the ops listener. */
async function acquisitions() {
  const res = await fetch(new URL('/metrics', inst.opsBase));
  const text = await res.text();
  const line = text.split('\n').find((l) => l.startsWith('quader_db_pool_acquire_duration_seconds_count') && l.includes('db_pool="request"'));
  return line ? Number(line.trim().split(/\s+/).pop()) : NaN;
}

const perKind = new Map(); // kind -> {n, ms, missing}
const samples = new Map(); // kind -> [{c, url}], the first 200 documents of the kind

function count(kind, c, url, ms, ok) {
  if (!perKind.has(kind)) perKind.set(kind, { n: 0, ms: 0, missing: 0 });
  const k = perKind.get(kind);
  if (!ok) {
    k.missing++;
    return;
  }
  k.n++;
  k.ms += ms;
  if (!samples.has(kind)) samples.set(kind, []);
  if (samples.get(kind).length < 200) samples.get(kind).push({ c, url });
}

/** One document: fetched, measured, written. Answers the parsed body (JSON) or null when it is not there. */
async function doc(c, path, url, kind, format, pkg, version, { json = true } = {}) {
  const before = c.throttledMs;
  const t0 = performance.now();
  const res = await c.send('GET', url);
  const body = Buffer.from(await res.arrayBuffer());
  const ms = performance.now() - t0 - (c.throttledMs - before);
  count(kind, c, url, ms, res.status === 200);
  if (res.status !== 200) return null;
  out.write(JSON.stringify({ path, kind, format, pkg, version, raw: body.length, gzip: gzipSync(body).length, sha: sha256(body), ms: Math.round(ms * 100) / 100 }) + '\n');
  return json ? JSON.parse(body.toString()) : body;
}

/** Every page of a list, as one document (the catalogue keeps a package's versions as one): the bytes of the pages added. */
async function listDoc(c, path, url, kind, format, pkg) {
  const items = [];
  let cursor = null;
  let raw = 0;
  let gz = 0;
  let ms = 0;
  let pages = 0;
  const parts = [];
  do {
    const before = c.throttledMs;
    const t0 = performance.now();
    const res = await c.send('GET', `${url}${url.includes('?') ? '&' : '?'}limit=500${cursor ? `&cursor=${enc(cursor)}` : ''}`);
    const body = Buffer.from(await res.arrayBuffer());
    ms += performance.now() - t0 - (c.throttledMs - before);
    if (res.status !== 200) throw new Error(`${url}: ${res.status} ${body.toString().slice(0, 300)}`);
    const page = JSON.parse(body.toString());
    items.push(...page.items);
    raw += body.length;
    gz += gzipSync(body).length;
    parts.push(body);
    pages++;
    cursor = page.nextCursor ?? null;
  } while (cursor);
  count(kind, c, `${url}${url.includes('?') ? '&' : '?'}limit=500`, ms, true);
  out.write(JSON.stringify({ path, kind, format, pkg, raw, gzip: gz, sha: sha256(Buffer.concat(parts)), ms: Math.round(ms * 100) / 100, pages, items: items.length }) + '\n');
  return items;
}

async function main() {
  const started = performance.now();
  // The idle rate of the request pool's acquisitions (background jobs of the role all), to subtract.
  const idle0 = await acquisitions();
  await sleep(20_000);
  const idleRate = ((await acquisitions()) - idle0 - 1) / 20_000; // per ms; the second scrape's own acquisitions are none

  const formats = await anon.json('GET', '/api/v1/formats');
  const clientsOf = new Map(formats.formats.map((f) => [f.name, (f.clients ?? []).map((x) => x.id)]));

  // The index of public packages, in pages of 100 (the GUI's own index is built from these summaries).
  const packages = [];
  {
    let cursor = null;
    let n = 0;
    do {
      const page = await doc(anon, `index/${n}`, `/api/v1/packages?limit=100${cursor ? `&cursor=${enc(cursor)}` : ''}`, 'index', '-', null, null);
      packages.push(...page.items);
      cursor = page.nextCursor ?? null;
      n++;
    } while (cursor);
  }
  const repoSet = new Set();
  const signatureObjects = new Set();
  let i = 0;
  for (const p of packages) {
    const id = p.id;
    const format = p.format;
    const base = `/api/v1/packages/${enc(id)}`;
    await doc(anon, `packages/${id}`, base, 'package', format, id, null);
    const versions = await listDoc(anon, `packages/${id}/versions`, `${base}/versions`, 'versions', format, id);
    for (const v of versions) {
      const ver = v.version;
      const vp = `packages/${id}/versions/${ver}`;
      const vu = `${base}/versions/${enc(ver)}`;
      const vd = await doc(anon, vp, vu, 'version', format, id, ver);
      if (!vd) continue;
      await doc(anon, `${vp}/readme`, `${vu}/readme`, 'readme', format, id, ver, { json: false });
      await doc(admin, `${vp}/vulnerabilities`, `${vu}/vulnerabilities`, 'vulnerabilities', format, id, ver, { json: false });
      const sigs = await doc(anon, `${vp}/signatures`, `${vu}/signatures`, 'signatures', format, id, ver);
      for (const m of JSON.stringify(sigs ?? {}).matchAll(/\/api\/v1\/signatures\/objects\/([0-9a-f]{64})/g)) signatureObjects.add(m[1]);
      if (format === 'deb') await doc(anon, `${vp}/contents`, `${vu}/contents`, 'contents', format, id, ver, { json: false });
      if (format === 'ansible') {
        const tree = await doc(anon, `${vp}/docs`, `${vu}/docs`, 'docs-tree', format, id, ver);
        for (const e of docEntries(tree)) await doc(anon, `${vp}/docs/entries/${e.type}/${e.name}`, `${vu}/docs/entries/${enc(e.type)}/${enc(e.name)}`, 'docs-entry', format, id, ver, { json: false });
        for (const f of docFiles(tree)) await doc(anon, `${vp}/docs/files/${f}`, `${vu}/docs/files/${enc(f)}`, 'docs-file', format, id, ver, { json: false });
      }
      // The set-up snippets of every client of the format, for the version in its first repository.
      const repo = firstRepo(vd, p);
      if (repo) {
        repoSet.add(`${repo.project}/${repo.key}/${format}`);
        for (const client of clientsOf.get(format) ?? []) {
          const q = format === 'generic' ? `path=${enc(ver)}` : `package=${enc(p.name)}&version=${enc(ver)}`;
          await doc(anon, `setup/${repo.project}/${repo.key}/${client}/${p.name}/${ver}`, `/api/v1/projects/${repo.project}/repositories/${repo.key}/client-setup?client=${enc(client)}&credentials=none&${q}`, 'setup-version', format, id, ver, { json: false });
        }
      }
    }
    if (++i % 100 === 0) console.log(`${new Date().toISOString().slice(11, 19)} ${i}/${packages.length} packages`);
  }
  await genericFiles(clientsOf, repoSet);
  for (const r of repoSet) {
    const [project, key, format] = r.split('/');
    for (const client of clientsOf.get(format) ?? []) await doc(anon, `setup/${project}/${key}/${client}`, `/api/v1/projects/${project}/repositories/${key}/client-setup?client=${enc(client)}&credentials=none`, 'setup-repo', format, null, null, { json: false });
  }
  for (const o of signatureObjects) await doc(anon, `signatures/objects/${o}`, `/api/v1/signatures/objects/${o}`, 'signature-object', '-', null, null, { json: false });
  await doc(anon, 'signing/trusted-root.json', '/api/v1/signing/trusted-root.json', 'signing', '-', null, null, { json: false });
  await doc(anon, 'signing/chain.pem', '/api/v1/signing/chain.pem', 'signing', '-', null, null, { json: false });
  out.end();
  const walkMs = performance.now() - started;
  // The statements per document, kind by kind.
  for (const [kind, list] of samples) {
    const a0 = await acquisitions();
    const t0 = performance.now();
    for (const { c, url } of list) await (await c.send('GET', url)).arrayBuffer();
    const wall = performance.now() - t0;
    const a1 = await acquisitions();
    perKind.get(kind).queriesPerDoc = (a1 - a0 - idleRate * wall) / list.length;
    perKind.get(kind).sampled = list.length;
  }
  const kinds = Object.fromEntries(perKind);
  const summary = { pass, packages: packages.length, totalMs: walkMs, idleAcquiresPerSecond: idleRate * 1000, throttled: anon.throttled + admin.throttled, throttledMs: anon.throttledMs + admin.throttledMs, kinds };
  writeFileSync(join(outDir, `pass-${pass}.json`), JSON.stringify(summary, null, 2));
  console.log(JSON.stringify(summary, null, 2));
}

/**
 * The files of every public generic repository (a generic repository has no packages): each folder's listing (every page as one
 * document), each file's description and its set-up snippets per client.
 */
async function genericFiles(clientsOf, repoSet) {
  const projects = await anon.json('GET', '/api/v1/projects?limit=500');
  for (const pr of projects.items) {
    const repos = await anon.json('GET', `/api/v1/projects/${pr.key}/repositories?limit=500`);
    for (const r of repos.items.filter((x) => x.format === 'generic')) {
      const at = `/api/v1/projects/${pr.key}/repositories/${r.key}`;
      const folders = [''];
      while (folders.length) {
        const folder = folders.shift();
        const items = await listDoc(anon, `files/${pr.key}/${r.key}/${folder}`, `${at}/children?path=${enc(folder)}`, 'folder', 'generic', null);
        if (folder === '' && items.length) repoSet.add(`${pr.key}/${r.key}/generic`);
        for (const it of items) {
          const path = folder ? `${folder}/${it.path}` : it.path;
          if (it.kind === 'folder') {
            folders.push(path);
            continue;
          }
          const item = await doc(anon, `files/${pr.key}/${r.key}/${path}`, `${at}/items?path=${enc(path)}`, 'file', 'generic', `${pr.key}/${r.key}/${path}`, path);
          const sha = item?.digests?.sha256 ?? '';
          for (const client of clientsOf.get('generic') ?? []) {
            await doc(anon, `setup/${pr.key}/${r.key}/${client}/${path}`, `${at}/client-setup?client=${enc(client)}&credentials=none&path=${enc(path)}${sha ? `&sha256=${sha}` : ''}`, 'setup-version', 'generic', `${pr.key}/${r.key}/${path}`, path, { json: false });
          }
        }
      }
    }
  }
}

/** The entries of an Ansible docs tree (PackageDocTree: groups, their types, their entries). */
function docEntries(tree) {
  return (tree?.groups ?? []).flatMap((g) => g.types.flatMap((t) => t.entries.filter((e) => e.hasDocs).map((e) => ({ type: e.type, name: e.name }))));
}

/** The files of an Ansible docs tree: the README and the Markdown files of docs/, and the changelog. */
function docFiles(tree) {
  const files = (tree?.files ?? []).map((f) => f.name);
  if (tree?.changelog) files.push('changelog');
  return files;
}

/** The first repository a version lives in, from the version's document or the package's summary. */
function firstRepo(vd, p) {
  const r = vd.repositories?.[0] ?? vd.repository ?? p.repositories?.[0];
  if (!r) return null;
  const qualified = typeof r === 'string' ? r : r.key;
  if (qualified?.includes('/')) {
    const [project, key] = qualified.split('/');
    return { project, key };
  }
  return { project: r.project ?? r.projectKey, key: r.key ?? r.repoKey ?? r.repository };
}

await main();
