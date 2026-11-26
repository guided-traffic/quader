// The tables of the catalogue measurement from the files measure.mjs wrote: counts and bytes per document kind and format, the
// distribution, the dedupe of content-addressed documents, the builder's time (both passes), the incremental case, the sync and the
// scaled sizes. Every figure printed under "derived" is computed from the measured ones by the rule printed beside it.
//
//   node report.mjs <out-dir>        reads docs-1.jsonl, pass-1.json and pass-2.json (docs-2.jsonl for the second pass's times)
import { existsSync, readFileSync } from 'node:fs';
import { join } from 'node:path';

const dir = process.argv[2];
const read = (pass) => readFileSync(join(dir, `docs-${pass}.jsonl`), 'utf8').trim().split('\n').map((l) => JSON.parse(l));
const docs = read(1);
const second = existsSync(join(dir, 'docs-2.jsonl')) ? read(2) : [];
const passes = [1, 2].filter((p) => existsSync(join(dir, `pass-${p}.json`))).map((p) => JSON.parse(readFileSync(join(dir, `pass-${p}.json`), 'utf8')));

const KiB = 1024;
const MiB = 1024 * KiB;
const GiB = 1024 * MiB;
const fmt = (b) => (b >= GiB ? `${(b / GiB).toFixed(2)} GiB` : b >= MiB ? `${(b / MiB).toFixed(1)} MiB` : b >= KiB ? `${(b / KiB).toFixed(1)} KiB` : `${Math.round(b)} B`);
const q = (xs, p) => {
  if (!xs.length) return 0;
  const s = [...xs].sort((a, b) => a - b);
  return s[Math.min(s.length - 1, Math.ceil(p * s.length) - 1)];
};
const sum = (xs) => xs.reduce((a, b) => a + b, 0);
const group = (xs, key) => Map.groupBy(xs, key);
const line = (cells) => `| ${cells.join(' | ')} |`;

// ------------------------------------------------------------------------------------------------------------------------------ counts
const versions = docs.filter((d) => d.kind === 'version');
const pkgs = docs.filter((d) => d.kind === 'package');
console.log('## Counts (measured)\n');
console.log(line(['Format', 'Public packages', 'Public versions', 'Documents', 'Bytes raw', 'Bytes gzip']));
console.log(line(['---', '---:', '---:', '---:', '---:', '---:']));
for (const [format, ds] of [...group(docs, (d) => d.format)].sort()) {
  console.log(line([format, pkgs.filter((d) => d.format === format).length, versions.filter((d) => d.format === format).length, ds.length, fmt(sum(ds.map((d) => d.raw))), fmt(sum(ds.map((d) => d.gzip)))]));
}
const distinct = [...new Map(docs.map((d) => [d.sha, d])).values()];
console.log(line(['all', pkgs.length, versions.length, docs.length, fmt(sum(docs.map((d) => d.raw))), fmt(sum(docs.map((d) => d.gzip)))]));
console.log(line(['all, distinct contents', '', '', distinct.length, fmt(sum(distinct.map((d) => d.raw))), fmt(sum(distinct.map((d) => d.gzip)))]));

// -------------------------------------------------------------------------------------------------------------------- bytes per kind
console.log('\n## Bytes per document kind and format (measured)\n');
console.log(line(['Kind', 'Format', 'n', 'raw median', 'raw p95', 'raw max', 'gzip median', 'gzip p95', 'gzip max', 'raw total', 'gzip total']));
console.log(line(['---', '---', ...Array(9).fill('---:')]));
for (const [key, ds] of [...group(docs, (d) => `${d.kind}\t${d.format}`)].sort((a, b) => a[0].localeCompare(b[0]))) {
  const [kind, format] = key.split('\t');
  const raw = ds.map((d) => d.raw);
  const gz = ds.map((d) => d.gzip);
  console.log(line([kind, format, ds.length, fmt(q(raw, 0.5)), fmt(q(raw, 0.95)), fmt(Math.max(...raw)), fmt(q(gz, 0.5)), fmt(q(gz, 0.95)), fmt(Math.max(...gz)), fmt(sum(raw)), fmt(sum(gz))]));
}

// ------------------------------------------------------------------------------------------------------------------------ per version
console.log('\n## Bytes per public version, every document of the version (measured)\n');
const perVersion = group(docs.filter((d) => d.version != null), (d) => `${d.pkg}\t${d.version}`);
const versionSets = [...perVersion.values()].map((ds) => ({ format: ds[0].format, raw: sum(ds.map((d) => d.raw)), gzip: sum(ds.map((d) => d.gzip)), n: ds.length, ms: sum(ds.map((d) => d.ms)) }));
console.log(line(['Format', 'versions', 'docs per version median', 'raw median', 'raw p95', 'raw max', 'gzip median', 'gzip p95', 'gzip mean']));
console.log(line(['---', ...Array(8).fill('---:')]));
const meanGzipByFormat = {};
for (const [format, vs] of [...group(versionSets, (v) => v.format)].sort()) {
  meanGzipByFormat[format] = sum(vs.map((v) => v.gzip)) / vs.length;
  console.log(line([format, vs.length, q(vs.map((v) => v.n), 0.5), fmt(q(vs.map((v) => v.raw), 0.5)), fmt(q(vs.map((v) => v.raw), 0.95)), fmt(Math.max(...vs.map((v) => v.raw))), fmt(q(vs.map((v) => v.gzip), 0.5)), fmt(q(vs.map((v) => v.gzip), 0.95)), fmt(meanGzipByFormat[format])]));
}

// ------------------------------------------------------------------------------------------------------------------------------ dedupe
console.log('\n## Dedupe across the versions of one package (measured)\n');
console.log(line(['Kind', 'Package', 'versions', 'documents', 'distinct', 'ratio', 'raw', 'raw distinct']));
console.log(line(['---', '---', ...Array(6).fill('---:')]));
for (const kind of ['docs-entry', 'readme', 'docs-file', 'vulnerabilities', 'setup-version', 'signatures', 'version']) {
  const byPkg = group(docs.filter((d) => d.kind === kind), (d) => d.pkg);
  const rows = [...byPkg].map(([pkg, ds]) => ({ pkg, ds, vs: new Set(ds.map((d) => d.version)).size, distinct: new Map(ds.map((d) => [d.sha, d])) })).filter((r) => r.vs >= 40);
  if (!rows.length) continue;
  const all = rows.flatMap((r) => r.ds);
  const dist = rows.flatMap((r) => [...r.distinct.values()]);
  const pick = rows[0];
  console.log(line([kind, `${pick.ds[0].format} ${pick.pkg.slice(0, 8)}…`, pick.vs, pick.ds.length, pick.distinct.size, (pick.ds.length / pick.distinct.size).toFixed(1), fmt(sum(pick.ds.map((d) => d.raw))), fmt(sum([...pick.distinct.values()].map((d) => d.raw)))]));
  console.log(line([kind, `all ${rows.length} packages of 40+ versions`, sum(rows.map((r) => r.vs)), all.length, dist.length, (all.length / dist.length).toFixed(1), fmt(sum(all.map((d) => d.raw))), fmt(sum(dist.map((d) => d.raw)))]));
}

// ------------------------------------------------------------------------------------------------------------------------- volatile
if (second.length) {
  console.log('\n## Documents whose bytes changed between the two passes, with no publish between (measured)\n');
  const firstSha = new Map(docs.map((d) => [d.path, d.sha]));
  console.log(line(['Kind', 'Format', 'documents', 'changed', 'share']));
  console.log(line(['---', '---', '---:', '---:', '---:']));
  for (const [key, ds] of [...group(second.filter((d) => firstSha.has(d.path)), (d) => `${d.kind}\t${d.format}`)].sort((a, b) => a[0].localeCompare(b[0]))) {
    const changed = ds.filter((d) => firstSha.get(d.path) !== d.sha).length;
    if (changed) console.log(line([...key.split('\t'), ds.length, changed, `${((changed / ds.length) * 100).toFixed(0)} %`]));
  }
}
const stripped = [1, 2].filter((r) => existsSync(join(dir, `stripped-${r}.jsonl`))).map((r) => readFileSync(join(dir, `stripped-${r}.jsonl`), 'utf8').trim().split('\n').map((l) => JSON.parse(l)));
if (stripped.length) {
  const [s1, s2] = stripped;
  console.log('\n## Without the time members builtAt, checkedAt, syncedAt and scannedAt (measured, volatile.mjs)\n');
  if (s2) {
    const a = new Map(s1.map((d) => [d.path, d]));
    console.log(line(['Kind', 'Format', 'documents', 'served bytes changed between the runs', 'stripped bytes changed']));
    console.log(line(['---', '---', '---:', '---:', '---:']));
    for (const [key, ds] of [...group(s2.filter((d) => a.has(d.path)), (d) => `${d.kind}\t${d.format}`)].sort((x, y) => x[0].localeCompare(y[0]))) {
      console.log(line([...key.split('\t'), ds.length, ds.filter((d) => a.get(d.path).served !== d.served).length, ds.filter((d) => a.get(d.path).sha !== d.sha).length]));
    }
  }
  const entries = group(s1.filter((d) => d.kind === 'docs-entry'), (d) => d.pkg);
  const rows = [...entries.values()].filter((ds) => new Set(ds.map((d) => d.version)).size >= 40);
  const bytes = new Map(docs.map((d) => [d.path, d.raw]));
  console.log('\n' + line(['Ansible entries, packages of 40+ versions', 'versions', 'entries', 'distinct served', 'distinct stripped', 'ratio stripped', 'raw', 'raw distinct stripped']));
  console.log(line(['---', ...Array(7).fill('---:')]));
  for (const ds of rows) {
    const d = new Map(ds.map((x) => [x.sha, x]));
    console.log(line([`${ds[0].pkg.slice(0, 12)}…`, new Set(ds.map((x) => x.version)).size, ds.length, new Set(ds.map((x) => x.served)).size, d.size, (ds.length / d.size).toFixed(1), fmt(sum(ds.map((x) => bytes.get(x.path) ?? 0))), fmt(sum([...d.values()].map((x) => bytes.get(x.path) ?? 0)))]));
  }
}

// ---------------------------------------------------------------------------------------------------------------------- builder time
console.log('\n## Builder time: every document, one request at a time (measured)\n');
console.log(line(['Pass', 'documents', 'sum of request times', 'mean per document', 'throttled waits (left out)', 'wall time of the pass']));
console.log(line(['---', ...Array(5).fill('---:')]));
for (const [i, ds] of [docs, second].entries()) {
  if (!ds.length) continue;
  const p = passes[i];
  const ms = sum(ds.map((d) => d.ms));
  console.log(line([i + 1, ds.length, `${(ms / 1000).toFixed(0)} s`, `${(ms / ds.length).toFixed(1)} ms`, p ? `${p.throttled} (${(p.throttledMs / 1000).toFixed(0)} s)` : '', p ? `${(p.totalMs / 1000).toFixed(0)} s` : '']));
}
console.log('\n' + line(['Kind', 'mean ms pass 1', 'mean ms pass 2', 'pool acquisitions per document pass 1', 'pass 2']));
console.log(line(['---', '---:', '---:', '---:', '---:']));
const kinds = [...new Set(docs.map((d) => d.kind))].sort();
for (const kind of kinds) {
  const m = (ds) => {
    const k = ds.filter((d) => d.kind === kind);
    return k.length ? (sum(k.map((d) => d.ms)) / k.length).toFixed(1) : '';
  };
  const qp = (p) => (p?.kinds?.[kind]?.queriesPerDoc != null ? p.kinds[kind].queriesPerDoc.toFixed(1) : '');
  console.log(line([kind, m(docs), m(second), qp(passes[0]), qp(passes[1])]));
}

// ---------------------------------------------------------------------------------------------------------------------- incremental
console.log('\n## One changed version (measured: the sum of its documents\' request times; derived: plus its package, versions list and one index page)\n');
const pkgDoc = new Map(pkgs.map((d) => [d.pkg, d]));
const listDoc = new Map(docs.filter((d) => d.kind === 'versions').map((d) => [d.pkg, d]));
const indexDoc = docs.find((d) => d.kind === 'index');
console.log(line(['Format', 'ms median', 'ms p95', 'ms max', 'gzip bytes median', 'gzip bytes p95']));
console.log(line(['---', ...Array(5).fill('---:')]));
for (const [format, sets] of [...group([...perVersion.values()], (ds) => ds[0].format)].sort()) {
  const ms = sets.map((ds) => sum(ds.map((d) => d.ms)) + (pkgDoc.get(ds[0].pkg)?.ms ?? 0) + (listDoc.get(ds[0].pkg)?.ms ?? 0) + (indexDoc?.ms ?? 0));
  const gz = sets.map((ds) => sum(ds.map((d) => d.gzip)) + (pkgDoc.get(ds[0].pkg)?.gzip ?? 0) + (listDoc.get(ds[0].pkg)?.gzip ?? 0) + (indexDoc?.gzip ?? 0));
  console.log(line([format, q(ms, 0.5).toFixed(0), q(ms, 0.95).toFixed(0), Math.max(...ms).toFixed(0), fmt(q(gz, 0.5)), fmt(q(gz, 0.95))]));
}

// ------------------------------------------------------------------------------------------------------------------------------ sync
console.log('\n## Initial sync (derived from the measured bytes: bytes / rate; no request overhead)\n');
const rawAll = sum(distinct.map((d) => d.raw));
const gzAll = sum(distinct.map((d) => d.gzip));
const MB = 1e6;
console.log(line(['Transfer', 'bytes', 'at 10 MB/s', 'at 100 MB/s']));
console.log(line(['---', '---:', '---:', '---:']));
console.log(line(['distinct documents, raw', fmt(rawAll), `${(rawAll / (10 * MB)).toFixed(1)} s`, `${(rawAll / (100 * MB)).toFixed(2)} s`]));
console.log(line(['distinct documents, gzip each', fmt(gzAll), `${(gzAll / (10 * MB)).toFixed(1)} s`, `${(gzAll / (100 * MB)).toFixed(2)} s`]));
console.log(`\nDocuments to fetch: ${distinct.length} distinct of ${docs.length}.`);

// --------------------------------------------------------------------------------------------------------------------------- scaled
console.log('\n## Scaled (derived: measured gzip bytes per version of each format times the versions; the mix of this set)\n');
const mix = Object.fromEntries([...group(versionSets, (v) => v.format)].map(([f, vs]) => [f, vs.length / versionSets.length]));
const perVersionMix = sum(Object.entries(mix).map(([f, share]) => share * meanGzipByFormat[f]));
const perVersionRawMix = sum(versionSets.map((v) => v.raw)) / versionSets.length;
const distinctShare = gzAll / sum(docs.map((d) => d.gzip));
console.log(`Mix: ${Object.entries(mix).map(([f, s]) => `${f} ${(s * 100).toFixed(0)} %`).join(', ')}; mean per version ${fmt(perVersionMix)} gzip, ${fmt(perVersionRawMix)} raw; distinct share of the bytes ${(distinctShare * 100).toFixed(0)} %.\n`);
console.log(line(['Public versions', 'gzip, every document', 'gzip, distinct', 'raw, distinct']));
console.log(line(['---', '---:', '---:', '---:']));
const rawDistinctShare = rawAll / sum(docs.map((d) => d.raw));
for (const n of [1000, 10_000, 100_000]) console.log(line([n.toLocaleString('en'), fmt(n * perVersionMix), fmt(n * perVersionMix * distinctShare), fmt(n * perVersionRawMix * rawDistinctShare)]));
console.log('\n' + line(['Format alone', 'gzip per version (mean)', 'versions to 1 GiB', 'versions to 10 GiB']));
console.log(line(['---', '---:', '---:', '---:']));
for (const [f, b] of Object.entries(meanGzipByFormat).sort()) console.log(line([f, fmt(b), Math.round(GiB / b).toLocaleString('en'), Math.round((10 * GiB) / b).toLocaleString('en')]));
console.log(`\nMix: 1 GiB at ${Math.round(GiB / (perVersionMix * distinctShare)).toLocaleString('en')} versions, 10 GiB at ${Math.round((10 * GiB) / (perVersionMix * distinctShare)).toLocaleString('en')} versions (gzip, distinct).`);
if (stripped.length) {
  // The same with the Ansible entries content-addressed without their time members (volatile.mjs): every other document by its served
  // bytes, an entry by its stripped ones.
  const strippedSha = new Map(stripped[0].map((d) => [d.path, d.sha]));
  const seen = new Map();
  for (const d of docs) {
    const key = d.kind === 'docs-entry' ? `e:${strippedSha.get(d.path) ?? d.sha}` : d.sha;
    if (!seen.has(key)) seen.set(key, d);
  }
  const kept = [...seen.values()];
  const g = sum(kept.map((d) => d.gzip));
  const r = sum(kept.map((d) => d.raw));
  const nv = versionSets.length;
  console.log(`\n## Scaled with the Ansible entries deduplicated without builtAt (derived: the distinct bytes of this set per version, times the versions)\n`);
  console.log(`Distinct documents ${kept.length}, ${fmt(g)} gzip, ${fmt(r)} raw; per version ${fmt(g / nv)} gzip, ${fmt(r / nv)} raw; entries ${((sum(kept.filter((d) => d.kind === 'docs-entry').map((d) => d.gzip)) / g) * 100).toFixed(1)} % of the gzip bytes.\n`);
  console.log(line(['Public versions', 'gzip, distinct', 'raw, distinct']));
  console.log(line(['---', '---:', '---:']));
  for (const n of [1000, 10_000, 100_000]) console.log(line([n.toLocaleString('en'), fmt((n * g) / nv), fmt((n * r) / nv)]));
  console.log(`\n1 GiB gzip at ${Math.round(GiB / (g / nv)).toLocaleString('en')} versions, 4 GiB at ${Math.round((4 * GiB) / (g / nv)).toLocaleString('en')}, 10 GiB at ${Math.round((10 * GiB) / (g / nv)).toLocaleString('en')}; raw 1 GiB at ${Math.round(GiB / (r / nv)).toLocaleString('en')}.`);
  const ans = docs.filter((d) => d.format === 'ansible');
  const ansVersions = versionSets.filter((v) => v.format === 'ansible').length;
  const ansKept = kept.filter((d) => d.format === 'ansible');
  const per = sum(ansKept.map((d) => d.gzip)) / ansVersions;
  console.log(`Ansible alone: ${fmt(sum(ans.map((d) => d.gzip)) / ansVersions)} gzip per version as served, ${fmt(per)} deduplicated; 1 GiB at ${Math.round(GiB / per).toLocaleString('en')} versions, 4 GiB at ${Math.round((4 * GiB) / per).toLocaleString('en')}, 10 GiB at ${Math.round((10 * GiB) / per).toLocaleString('en')}.`);
}
console.log('\nShare of the bytes by kind (gzip, every document):');
const total = sum(docs.map((d) => d.gzip));
for (const [k, ds] of [...group(docs, (d) => d.kind)].sort((a, b) => sum(b[1].map((d) => d.gzip)) - sum(a[1].map((d) => d.gzip)))) console.log(`- ${k}: ${((sum(ds.map((d) => d.gzip)) / total) * 100).toFixed(1)} %`);
