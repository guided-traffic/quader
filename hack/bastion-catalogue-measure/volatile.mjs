// The documents whose bytes change without a change of their content: reads every Ansible documentation entry and every vulnerabilities
// document that measure.mjs recorded again, removes the time members that a build or a sync rewrites (`builtAt`, `checkedAt`,
// `syncedAt`, `scannedAt`), and records the SHA-256 of what is left. Run it twice with an advisory sync between (the default schedule
// runs one every 15 minutes) and report.mjs compares: equal stripped hashes with different served ones are documents that only the
// time members change; the distinct stripped entries per package are the dedupe a builder gets once it leaves those members out.
//
//   node volatile.mjs <out-dir> <run>      reads docs-1.jsonl, writes stripped-<run>.jsonl ({path, kind, format, pkg, version, sha, served})
import { createWriteStream, readFileSync } from 'node:fs';
import { join } from 'node:path';
import { Client, instance, sha256 } from './lib.mjs';

const inst = instance();
const [outDir, run] = process.argv.slice(2);
if (!outDir || !run) throw new Error('usage: node volatile.mjs <out-dir> <run>');
const anon = new Client(inst.base, inst.origin);
const admin = await Client.breakGlass(inst);
const VOLATILE = new Set(['builtAt', 'checkedAt', 'syncedAt', 'scannedAt']);
const strip = (v) => (Array.isArray(v) ? v.map(strip) : v && typeof v === 'object' ? Object.fromEntries(Object.entries(v).filter(([k]) => !VOLATILE.has(k)).map(([k, x]) => [k, strip(x)])) : v);

const docs = readFileSync(join(outDir, 'docs-1.jsonl'), 'utf8').trim().split('\n').map((l) => JSON.parse(l)).filter((d) => d.kind === 'docs-entry' || d.kind === 'vulnerabilities');
const out = createWriteStream(join(outDir, `stripped-${run}.jsonl`));
const enc = encodeURIComponent;
let n = 0;
for (const d of docs) {
  // The catalogue path is the API path below /api/v1/ with the parts percent-encoded.
  const rest = d.path.slice(`packages/${d.pkg}/versions/${d.version}`.length);
  const url = `/api/v1/packages/${enc(d.pkg)}/versions/${enc(d.version)}${rest.split('/').map(enc).join('/')}`;
  const res = await (d.kind === 'vulnerabilities' ? admin : anon).send('GET', url);
  const body = Buffer.from(await res.arrayBuffer());
  if (res.status !== 200) continue;
  out.write(JSON.stringify({ path: d.path, kind: d.kind, format: d.format, pkg: d.pkg, version: d.version, served: sha256(body), sha: sha256(Buffer.from(JSON.stringify(strip(JSON.parse(body.toString()))))) }) + '\n');
  if (++n % 2000 === 0) console.log(`${new Date().toISOString().slice(11, 19)} ${n}/${docs.length}`);
}
out.end();
console.log(`${n} documents`);
