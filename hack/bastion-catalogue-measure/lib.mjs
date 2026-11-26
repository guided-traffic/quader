// Helpers of the measurement of the Bastion's public catalogue (README.md beside this file): the instance of the UI development profile
// the scripts run against, an HTTP client with the break-glass session or a token, the archive builders the generator needs, and SQL
// through the profile's PostgreSQL container. Standard library only; run with NODE_EXTRA_CA_CERTS set to the instance's CA (run.sh does).
import { execFileSync } from 'node:child_process';
import { createHash } from 'node:crypto';
import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import { crc32, deflateRawSync, gzipSync, inflateRawSync } from 'node:zlib';

/** The instance: UI_STATE (the state directory), UI_BACKEND_PORT, UI_PROJECT, as deploy/ui-dev/instance.sh exports them. */
export function instance() {
  const state = process.env.UI_STATE;
  const port = process.env.UI_BACKEND_PORT;
  const project = process.env.UI_PROJECT;
  if (!state || !port || !project) throw new Error('UI_STATE, UI_BACKEND_PORT and UI_PROJECT are unset: start the scripts through run.sh');
  const yaml = readFileSync(join(state, 'quader.yaml'), 'utf8');
  const origin = /^\s*base_url:\s*(\S+)/m.exec(yaml)?.[1];
  if (!origin) throw new Error(`no server.base_url in ${state}/quader.yaml`);
  return { state, base: `https://127.0.0.1:${port}`, opsBase: `https://127.0.0.1:${Number(port) + 1}`, origin, postgres: `${project}-postgres` };
}

export const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
export const sha256 = (b) => createHash('sha256').update(b).digest('hex');

/**
 * A client of one caller: anonymous, the break-glass session (cookies and CSRF token) or a bearer token. A 429 is waited out with its
 * Retry-After and the wait is counted in `throttledMs`, so that a timing can leave it out.
 */
export class Client {
  constructor(base, origin) {
    this.base = base;
    this.origin = origin;
    this.cookies = new Map();
    this.bearer = null;
    this.throttledMs = 0;
    this.throttled = 0;
    this.retry503 = false;
  }

  static async breakGlass(inst) {
    const c = new Client(inst.base, inst.origin);
    const password = readFileSync(join(inst.state, 'secrets', 'break-glass-password'), 'utf8').trim();
    const res = await c.send('POST', '/auth/local/login', { json: { username: 'admin', password } });
    if (res.status !== 204) throw new Error(`break-glass sign-in: ${res.status} ${await res.text()}`);
    return c;
  }

  static withToken(inst, token) {
    const c = new Client(inst.base, inst.origin);
    c.bearer = token;
    c.retry503 = true;
    return c;
  }

  headers(extra = {}) {
    const h = { Origin: this.origin, ...extra };
    if (this.cookies.size) {
      h.Cookie = [...this.cookies].map(([k, v]) => `${k}=${v}`).join('; ');
      const csrf = this.cookies.get('__Host-qdr_csrf');
      if (csrf) h['X-CSRF-Token'] = csrf;
    }
    if (this.bearer && !h.Authorization) h.Authorization = `Bearer ${this.bearer}`;
    return h;
  }

  /** One request; `json` sends a JSON body, `body` raw bytes. Answers the Response with its body unread. */
  async send(method, path, { json, body, headers = {} } = {}) {
    const extra = { ...headers };
    let payload = body;
    if (json !== undefined) {
      payload = JSON.stringify(json);
      extra['Content-Type'] ??= 'application/json';
    }
    for (let i = 0; ; i++) {
      const res = await fetch(new URL(path, this.base), { method, headers: this.headers(extra), body: payload, redirect: 'manual' });
      for (const c of res.headers.getSetCookie?.() ?? []) {
        const [pair] = c.split(';');
        const at = pair.indexOf('=');
        this.cookies.set(pair.slice(0, at), pair.slice(at + 1));
      }
      if (res.status === 503 && this.retry503 && i < 5) {
        // The shared machine's load made a database call time out; the generator tries again (a measured read never does).
        await res.arrayBuffer();
        this.retried = (this.retried ?? 0) + 1;
        await sleep(2000 * (i + 1));
        continue;
      }
      if (res.status !== 429 || i === 20) return res;
      await res.arrayBuffer();
      const wait = Math.min(Math.max(Number(res.headers.get('retry-after') ?? '1'), 0.2), 30) * 1000;
      this.throttled++;
      this.throttledMs += wait;
      await sleep(wait);
    }
  }

  async json(method, path, opts, ok = [200, 201]) {
    const res = await this.send(method, path, opts);
    const text = await res.text();
    if (!ok.includes(res.status)) throw new Error(`${method} ${path}: ${res.status} ${text.slice(0, 500)}`);
    return text ? JSON.parse(text) : null;
  }

  async expect(method, path, opts, ok) {
    const res = await this.send(method, path, opts);
    const text = await res.text();
    if (!ok.includes(res.status)) throw new Error(`${method} ${path}: ${res.status} ${text.slice(0, 500)}`);
    return res.status;
  }
}

/** One ustar entry; a name ending in `/` is a directory. */
export function tarEntry(name, content = Buffer.alloc(0)) {
  const header = Buffer.alloc(512);
  const octal = (value, width) => value.toString(8).padStart(width - 1, '0') + '\0';
  const dir = name.endsWith('/');
  header.write(name, 0, 100, 'utf8');
  header.write(octal(dir ? 0o755 : 0o644, 8), 100, 'ascii');
  header.write(octal(0, 8), 108, 'ascii');
  header.write(octal(0, 8), 116, 'ascii');
  header.write(octal(content.length, 12), 124, 'ascii');
  header.write(octal(1767225600, 12), 136, 'ascii');
  header.write(' '.repeat(8), 148, 'ascii');
  header.write(dir ? '5' : '0', 156, 'ascii');
  header.write('ustar\0' + '00', 257, 'ascii');
  let sum = 0;
  for (const b of header) sum += b;
  header.write(sum.toString(8).padStart(6, '0') + '\0 ', 148, 'ascii');
  return Buffer.concat([header, content, Buffer.alloc((512 - (content.length % 512)) % 512)]);
}

export const tarGz = (entries) => gzipSync(Buffer.concat([...entries.map(([n, c]) => tarEntry(n, c)), Buffer.alloc(1024)]));

/** A zip of deflated entries with a fixed time. */
export function zip(entries) {
  const locals = [];
  const centrals = [];
  let offset = 0;
  for (const e of entries) {
    const name = Buffer.from(e.name);
    const packed = deflateRawSync(e.data);
    const crc = crc32(e.data);
    const local = Buffer.alloc(30);
    local.writeUInt32LE(0x04034b50, 0);
    local.writeUInt16LE(20, 4);
    local.writeUInt16LE(8, 8);
    local.writeUInt16LE(0x5b61, 12);
    local.writeUInt32LE(crc, 14);
    local.writeUInt32LE(packed.length, 18);
    local.writeUInt32LE(e.data.length, 22);
    local.writeUInt16LE(name.length, 26);
    const central = Buffer.alloc(46);
    central.writeUInt32LE(0x02014b50, 0);
    central.writeUInt16LE(20, 4);
    central.writeUInt16LE(20, 6);
    central.writeUInt16LE(8, 10);
    central.writeUInt16LE(0x5b61, 14);
    central.writeUInt32LE(crc, 16);
    central.writeUInt32LE(packed.length, 20);
    central.writeUInt32LE(e.data.length, 24);
    central.writeUInt16LE(name.length, 28);
    central.writeUInt32LE(offset, 42);
    locals.push(local, name, packed);
    centrals.push(central, name);
    offset += local.length + name.length + packed.length;
  }
  const directory = Buffer.concat(centrals);
  const end = Buffer.alloc(22);
  end.writeUInt32LE(0x06054b50, 0);
  end.writeUInt16LE(entries.length, 8);
  end.writeUInt16LE(entries.length, 10);
  end.writeUInt32LE(directory.length, 12);
  end.writeUInt32LE(offset, 16);
  return Buffer.concat([...locals, directory, end]);
}

/** The entries of a zip (stored or deflated), read from its central directory. */
export function unzip(buf) {
  let end = buf.length - 22;
  while (end >= 0 && buf.readUInt32LE(end) !== 0x06054b50) end--;
  if (end < 0) throw new Error('not a zip archive');
  const count = buf.readUInt16LE(end + 10);
  let at = buf.readUInt32LE(end + 16);
  const out = [];
  for (let i = 0; i < count; i++) {
    const method = buf.readUInt16LE(at + 10);
    const size = buf.readUInt32LE(at + 20);
    const nameLen = buf.readUInt16LE(at + 28);
    const extraLen = buf.readUInt16LE(at + 30);
    const commentLen = buf.readUInt16LE(at + 32);
    const local = buf.readUInt32LE(at + 42);
    const name = buf.subarray(at + 46, at + 46 + nameLen).toString();
    const dataAt = local + 30 + buf.readUInt16LE(local + 26) + buf.readUInt16LE(local + 28);
    const raw = buf.subarray(dataAt, dataAt + size);
    out.push({ name, data: method === 8 ? inflateRawSync(raw) : Buffer.from(raw) });
    at += 46 + nameLen + extraLen + commentLen;
  }
  return out;
}

/** An `ar` archive as dpkg writes it. */
export function ar(members) {
  const parts = [Buffer.from('!<arch>\n')];
  for (const [name, data] of members) {
    const header = `${name.padEnd(16)}${'1767225600'.padEnd(12)}${'0'.padEnd(6)}${'0'.padEnd(6)}${'100644'.padEnd(8)}${String(data.length).padEnd(10)}${String.fromCharCode(0x60)}\n`;
    parts.push(Buffer.from(header, 'ascii'), data);
    if (data.length % 2 === 1) parts.push(Buffer.from('\n'));
  }
  return Buffer.concat(parts);
}

/** Runs SQL in the instance's database through its container, as the database owner; answers psql's output (unaligned, tuples only). */
export function psql(inst, sql, { tuples = false } = {}) {
  const password = readFileSync(join(inst.state, 'secrets', 'db-pass'), 'utf8').trim();
  const args = ['exec', '-i', '-e', `PGPASSWORD=${password}`, inst.postgres, 'psql', '-q', '-v', 'ON_ERROR_STOP=1'];
  if (tuples) args.push('-At', '-F', '\t');
  args.push('host=localhost dbname=quader user=quader sslmode=require');
  return execFileSync('docker', args, { input: sql, maxBuffer: 1 << 28 }).toString();
}

export const lit = (s) => `'${String(s).replace(/'/g, "''")}'`;

/** A small deterministic generator, so that a second run builds the same bytes. */
export function rng(seed) {
  let s = seed >>> 0 || 1;
  return () => {
    s ^= s << 13;
    s ^= s >>> 17;
    s ^= s << 5;
    return (s >>> 0) / 0x100000000;
  };
}

const WORDS = (
  'agent alpha api archive async backoff batch bridge buffer build cache channel check client cluster codec config connect ' +
  'context cursor daemon data debug decode default deploy device digest driver encode endpoint engine event export fetch ' +
  'filter format frame gateway graph handler header health index input journal kernel key label layer lease limit listen ' +
  'loader log manifest mapper metric middleware mirror module mount network node option output packet parse patch path ' +
  'pipeline plugin policy pool port probe profile proxy queue quota reader record registry release replica request resolve ' +
  'retry route rule runtime schema scope secret service session shard signal socket source stream subject sync table task ' +
  'template tenant timer token trace transport tree upload value vault version volume watch worker writer zone'
).split(' ');

/** Plain sentences of `n` words from `r`. */
export function words(r, n) {
  const out = [];
  for (let i = 0; i < n; i++) out.push(WORDS[Math.floor(r() * WORDS.length)]);
  return out.join(' ');
}
