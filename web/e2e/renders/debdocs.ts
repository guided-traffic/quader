import { gzipSync } from 'node:zlib';
import { APIRequestContext } from '@playwright/test';
import { ar, debPackage, expectStatus, otherProject, patient, tarGz } from './seed';

/**
 * Debian packages that tell what Debian tells about a package (W-30 G8, R-DEB-038, R-DEB-039): every relation field with alternatives and
 * long constraints, a long description of several paragraphs with a list, a changelog of thirty entries, a copyright file in the
 * machine-readable format and 300 files in two architectures; a package of more files than Quader keeps; and one whose data archive is no
 * archive, which Quader stores and serves and does not read. The seed uploads them into the Debian repository of the other project and
 * waits until the reader has run.
 */
export const docsDeb = { format: 'deb', name: 'acme-observability-agent', version: '4.2.0-3' } as const;
export const manyFilesDeb = { format: 'deb', name: 'acme-icon-theme', version: '2026.11-1', files: 10_050 } as const;
export const brokenDeb = { format: 'deb', name: 'acme-legacy-tools', version: '0.9-1' } as const;
/** A package that uses acme-observability-agent under its own name and under a name it provides (R-API-086, used-by.ts). */
export const forwarderDeb = { format: 'deb', name: 'acme-log-forwarder', version: '1.0.0-1' } as const;

/** A tar entry of a regular file, a directory (a name ending in `/`) or a symbolic link (`link`). */
function entry(name: string, content: Buffer, link = ''): Buffer {
  const header = Buffer.alloc(512);
  const octal = (value: number, width: number): string => value.toString(8).padStart(width - 1, '0') + '\0';
  const dir = name.endsWith('/');
  header.write(name, 0, 100, 'utf8');
  header.write(octal(dir ? 0o755 : 0o644, 8), 100, 'ascii');
  header.write(octal(0, 8), 108, 'ascii');
  header.write(octal(0, 8), 116, 'ascii');
  header.write(octal(link !== '' ? 0 : content.length, 12), 124, 'ascii');
  header.write(octal(1767225600, 12), 136, 'ascii');
  header.write(' '.repeat(8), 148, 'ascii');
  header.write(dir ? '5' : link !== '' ? '2' : '0', 156, 'ascii');
  header.write(link, 157, 100, 'utf8');
  header.write('ustar\0' + '00', 257, 'ascii');
  let sum = 0;
  for (const b of header) sum += b;
  header.write(octal(sum, 7) + ' ', 148, 'ascii');
  const pad = (512 - (content.length % 512)) % 512;
  return Buffer.concat([header, link !== '' ? Buffer.alloc(0) : content, Buffer.alloc(link !== '' ? 0 : pad)]);
}

function dataTar(entries: readonly Buffer[]): Buffer {
  return gzipSync(Buffer.concat([...entries, Buffer.alloc(1024)]));
}

function changelog(name: string): string {
  const out: string[] = [];
  const changes = [
    'Ship the journal reader as a systemd unit of its own.',
    'Retry a batch with backoff while the log store answers 503.',
    'Keep the position in each file across restarts (closes: #2041).',
    'Rebuild against libssl3t64.',
    'Bound the memory of the batch queue to 64 MiB.',
  ];
  for (let i = 30; i >= 1; i--) {
    const minor = Math.floor(i / 10);
    const version = `4.${minor}.${i % 10}-${(i % 3) + 1}`;
    const day = String(((i * 7) % 28) + 1).padStart(2, '0');
    out.push(
      `${name} (${version}) unstable; urgency=medium\n\n` +
        `  * ${changes[i % changes.length]}\n` +
        (i % 4 === 0 ? `  * Update the translations of the setup prompts.\n  * Raise Standards-Version to 4.7.0.\n` : '') +
        `\n -- Platform engineering <platform@example.invalid>  Mon, ${day} Sep 2026 10:${String(i).padStart(2, '0')}:00 +0000\n`,
    );
  }
  return out.join('\n');
}

const copyright = `Format: https://www.debian.org/doc/packaging-manuals/copyright-format/1.0/
Upstream-Name: acme-observability-agent
Upstream-Contact: Platform engineering <platform@example.invalid>
Source: https://git.example.invalid/platform/acme-observability-agent

Files: *
Copyright: 2023-2026 Acme Platform Engineering
License: Apache-2.0

Files: vendor/github.com/klauspost/compress/*
Copyright: 2019 Klaus Post
License: BSD-3-Clause

Files: debian/*
Copyright: 2024-2026 Platform engineering <platform@example.invalid>
License: GPL-2+

License: Apache-2.0
 Licensed under the Apache License, Version 2.0 (the "License");
 you may not use this file except in compliance with the License.
 .
 On Debian systems, the complete text of the Apache License 2.0 can be
 found in "/usr/share/common-licenses/Apache-2.0".

License: BSD-3-Clause
 Redistribution and use in source and binary forms, with or without
 modification, are permitted provided that the conditions are met.

License: GPL-2+
 On Debian systems, the complete text of the GNU General Public License
 version 2 can be found in "/usr/share/common-licenses/GPL-2".
`;

function control(fields: string): Buffer {
  return tarGz([['./', Buffer.alloc(0)], ['./control', Buffer.from(fields)]]);
}

/** The package of every relation field: 300 files, a link, the changelog and the copyright, for one architecture. */
function docsPackage(arch: string): Buffer {
  const { name, version } = docsDeb;
  const fields =
    `Package: ${name}\nSource: acme-observability (4.2.0-3)\nVersion: ${version}\nArchitecture: ${arch}\n` +
    'Maintainer: Platform engineering <platform@example.invalid>\nInstalled-Size: 18432\n' +
    'Pre-Depends: init-system-helpers (>= 1.54~)\n' +
    'Depends: libc6 (>= 2.36), libssl3t64 (>= 3.0.0) | libssl3 (>= 3.0.0), libsystemd0 (>= 252), adduser, ' +
    'python3:any (>= 3.11~), default-logrotate | logrotate (>= 3.21.0-1~deb12u1)\n' +
    'Recommends: ca-certificates, acme-observability-agent-doc (= 4.2.0-3)\n' +
    'Suggests: acme-observability-dashboards, prometheus-node-exporter [amd64 arm64]\n' +
    'Enhances: rsyslog, systemd-journal-remote\n' +
    'Breaks: acme-log-shipper (<< 3.0~)\nConflicts: acme-agent-legacy\nReplaces: acme-log-shipper (<< 3.0~)\n' +
    'Provides: acme-telemetry-agent (= 4.2.0), log-shipper\n' +
    'Section: admin\nPriority: optional\nMulti-Arch: foreign\nHomepage: https://git.example.invalid/platform/acme-observability-agent\n' +
    'Description: collects the logs, metrics and traces of a host for the platform\n' +
    ' acme-observability-agent follows the journal and the files under /var/log, reads the\n' +
    ' metrics of the host and of its services, and sends both to the platform\'s stores over\n' +
    ' TLS, retrying while a store is away.\n' +
    ' .\n' +
    ' It ships the following components:\n' +
    '  * the log shipper, which keeps its position in each file across restarts;\n' +
    '  * the metrics reader, with collectors for the kernel, systemd and the network;\n' +
    '  * the trace relay, which forwards OTLP from local services.\n' +
    ' .\n' +
    ' This package holds the agent and its systemd units; the dashboards are in\n' +
    ' acme-observability-dashboards.\n';
  const entries: Buffer[] = [entry('./', Buffer.alloc(0)), entry('./usr/', Buffer.alloc(0)), entry('./usr/bin/', Buffer.alloc(0))];
  entries.push(entry(`./usr/bin/${name}`, Buffer.from('#!/bin/sh\necho agent\n')));
  entries.push(entry('./usr/bin/acme-agent', Buffer.alloc(0), name));
  entries.push(entry(`./usr/share/doc/${name}/`, Buffer.alloc(0)));
  entries.push(entry(`./usr/share/doc/${name}/changelog.Debian.gz`, gzipSync(Buffer.from(changelog(name)))));
  entries.push(entry(`./usr/share/doc/${name}/copyright`, Buffer.from(copyright)));
  const libdir = arch === 'arm64' ? 'aarch64-linux-gnu' : 'x86_64-linux-gnu';
  for (let i = 0; i < 296; i++) {
    const group = ['collectors', 'shippers', 'relays', 'templates'][i % 4];
    entries.push(entry(`./usr/lib/${libdir}/${name}/${group}/module-${String(i).padStart(3, '0')}.so`, Buffer.alloc((i % 7) * 100)));
  }
  return ar([
    ['debian-binary', Buffer.from('2.0\n')],
    ['control.tar.gz', control(fields)],
    ['data.tar.gz', dataTar(entries)],
  ]);
}

/** A package of more files than Quader keeps of a list (10,000). */
function manyFilesPackage(): Buffer {
  const { name, version, files } = manyFilesDeb;
  const fields =
    `Package: ${name}\nVersion: ${version}\nArchitecture: all\nMaintainer: Platform engineering <platform@example.invalid>\n` +
    'Installed-Size: 40960\nSection: graphics\nPriority: optional\nDescription: the icons of the platform\'s desktop images\n';
  const entries: Buffer[] = [];
  for (let i = 0; i < files; i++) {
    entries.push(entry(`./usr/share/icons/acme/${48 + (i % 4) * 16}x${48 + (i % 4) * 16}/apps/icon-${String(i).padStart(5, '0')}.png`, Buffer.alloc(0)));
  }
  return ar([
    ['debian-binary', Buffer.from('2.0\n')],
    ['control.tar.gz', control(fields)],
    ['data.tar.gz', dataTar(entries)],
  ]);
}

/** A package whose data archive is gzip but no tar: stored, indexed and served, and not read. */
function brokenPackage(): Buffer {
  const { name, version } = brokenDeb;
  const fields =
    `Package: ${name}\nVersion: ${version}\nArchitecture: amd64\nMaintainer: Platform engineering <platform@example.invalid>\n` +
    'Section: admin\nPriority: optional\nDescription: the tools of the platform\'s first hosts\n';
  return ar([
    ['debian-binary', Buffer.from('2.0\n')],
    ['control.tar.gz', control(fields)],
    ['data.tar.gz', gzipSync(Buffer.from('this is no tar archive\n'.repeat(40)))],
  ]);
}

/** The package of "Used by": it depends on the agent and on a name the agent provides, and suggests a name the agent provides. */
function forwarderPackage(): Buffer {
  const { name, version } = forwarderDeb;
  const fields =
    `Package: ${name}\nVersion: ${version}\nArchitecture: all\nMaintainer: Platform engineering <platform@example.invalid>\n` +
    'Depends: acme-observability-agent (>= 4.2.0), log-shipper\nSuggests: acme-telemetry-agent\n' +
    'Section: admin\nPriority: optional\nDescription: forwards the agent\'s logs to a second store\n';
  return ar([
    ['debian-binary', Buffer.from('2.0\n')],
    ['control.tar.gz', control(fields)],
    ['data.tar.gz', dataTar([entry('./', Buffer.alloc(0))])],
  ]);
}

async function until(seconds: number, what: string, ok: () => Promise<boolean>): Promise<void> {
  const deadline = Date.now() + seconds * 1000;
  while (!(await ok())) {
    if (Date.now() > deadline) throw new Error(`${what}: not within ${seconds} s`);
    await new Promise((r) => setTimeout(r, 1000));
  }
}

/** Uploads the three packages and waits until Quader read (or refused) each one's data archive. */
export async function seedDebDocs(ctx: APIRequestContext, token: string): Promise<void> {
  const auth = { Authorization: `Bearer ${token}` };
  const uploads: [string, Buffer][] = [
    [`${docsDeb.name}_${docsDeb.version}_amd64.deb`, docsPackage('amd64')],
    [`${docsDeb.name}_${docsDeb.version}_arm64.deb`, docsPackage('arm64')],
    [`${manyFilesDeb.name}_${manyFilesDeb.version}_all.deb`, manyFilesPackage()],
    [`${brokenDeb.name}_${brokenDeb.version}_amd64.deb`, brokenPackage()],
    [`${forwarderDeb.name}_${forwarderDeb.version}_all.deb`, forwarderPackage()],
  ];
  for (const [file, data] of uploads) {
    const res = await patient(() =>
      ctx.put(`/api/deb/${otherProject}/${debPackage.repo}/upload/${file}?distribution=stable&component=main`, {
        data,
        headers: { ...auth, 'Content-Type': 'application/vnd.debian.binary-package' },
      }),
    );
    await expectStatus(res, [200, 201, 409], `deb upload ${file}`);
  }
  for (const p of [docsDeb, manyFilesDeb, brokenDeb]) {
    await until(120, `the contents of ${p.name}`, async () => {
      const found = await ctx.get(`/api/v1/packages/lookup?format=deb&name=${p.name}`, { headers: auth });
      if (!found.ok()) return false;
      const id = ((await found.json()) as { id: string }).id;
      const res = await ctx.get(`/api/v1/packages/${id}/versions/${encodeURIComponent(p.version)}/contents?limit=1`, { headers: auth });
      return res.ok() && ((await res.json()) as { state: string }).state !== 'pending';
    });
  }
}

/**
 * The answers of a Debian version's page that are its normal state (Q-UI-1270): the page looks every relation's name up, and a name no
 * repository of the installation holds (libc6, adduser) answers 404, which leaves it text.
 */
export const debRelationLookups = [
  {
    match: /^GET \/api\/v1\/packages\/lookup\?format=deb&name=[a-z0-9][a-z0-9.+-]* answered 404$/,
    why: 'a relation names a package the installation does not hold: its lookup answers 404 and the name stays text',
  },
];
