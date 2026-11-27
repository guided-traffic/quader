import { crc32, deflateRawSync } from 'node:zlib';

/**
 * The advisory data of the renders (R-VULN-005, R-VULN-038): the UI development profile imports npm advisories from a mirror
 * repository instead of the network (deploy/ui-dev/up.sh), and the seed uploads an archive there whose records affect the
 * vendored packages below with chosen counts, so that every grade from S to E occurs. The records are the seed's own, named
 * DEMO-…; they describe no real vulnerability. The internal packages (claimed names) and the images, which the profile cannot
 * scan, are the grade N/A.
 */

/** The generic repository the profile's OSV source reads (`vulnerabilities.sources.osv.repository` in up.sh). */
export const advisoryMirror = { project: 'platform', key: 'advisory-mirror' } as const;

type Counts = Partial<Record<'critical' | 'high' | 'medium' | 'low' | 'none' | 'unknown', number>>;

/** A vendored version, the open findings its grade needs, and how many of them name a fixed version (R-VULN-040). */
export interface VendorVersion {
  /** The project of the repository; the main project by default. */
  readonly project?: string;
  readonly repo: string;
  readonly name: string;
  readonly version: string;
  readonly counts: Counts;
  readonly grade: string;
  /** How many of the findings are fixed in `fixedIn`; the first ones of the worst severity on. */
  readonly fixable?: number;
  /** The version that fixes them; required with `fixable`. */
  readonly fixedIn?: string;
  /** The distribution tag the version is published under; `latest` by default, `next` for a pre-release. */
  readonly tag?: string;
  /** The README the tarball and the package document carry, none by default. */
  readonly readme?: string;
}

/**
 * Public packages vendored into a local repository that matches public advisories (`matchPublicAdvisories: always`), each with
 * the open findings its grade needs. The latest version of xml-stream-parser is fixed, so the list shows S and the package page
 * the C of 0.9.2; image-convert-tools carries the counts of the owner's reference card, 112 of them with a fix.
 */
export const vendorPackages: readonly VendorVersion[] = [
  { repo: 'npm-vendor', name: 'date-format-lite', version: '2.1.0', counts: {}, grade: 'S' },
  // One finding rated None (G5-55): an advisory whose CVSS vector scores 0.0, so that the badge of None is rendered.
  { repo: 'npm-vendor', name: 'tiny-colors', version: '1.2.0', counts: { low: 2, none: 1 }, grade: 'A' },
  { repo: 'npm-vendor', name: 'yaml-loader-compat', version: '3.0.0', counts: { medium: 6, low: 4 }, grade: 'B' },
  { repo: 'npm-vendor', name: 'xml-stream-parser', version: '0.9.2', counts: { high: 3, medium: 5, unknown: 2 }, grade: 'C', fixable: 8, fixedIn: '1.0.0' },
  { repo: 'npm-vendor', name: 'xml-stream-parser', version: '1.0.0', counts: {}, grade: 'S' },
  { repo: 'npm-vendor', name: 'http-proxy-legacy', version: '1.18.0', counts: { critical: 3, high: 12, medium: 30, low: 8, unknown: 2 }, grade: 'D' },
  {
    repo: 'npm-vendor',
    name: 'image-convert-tools',
    version: '4.0.1',
    counts: { critical: 13, high: 160, medium: 113, low: 16, unknown: 843 },
    grade: 'E',
    fixable: 112,
    fixedIn: '4.0.2',
  },
];

/** The package whose page the renders open with the grade E. */
export const gradedPackage = { format: 'npm', name: 'image-convert-tools', version: '4.0.1' } as const;

/**
 * A package of many versions (W-7 of the owner's walk of 2026-11-18): 30 stable versions from 1.0.0 to 3.9.0 and three pre-releases of
 * 4.0.0, each with the open findings of one grade in a cycle of S to E, some of them fixed in the next version, so that the versions
 * table of the package page shows every grade, totals, fixable counts, pre-releases and the tags `latest`, `next` and `lts` (2.9.0),
 * and so that its search, sort and paging can be judged.
 */
export const manyVersionsPackage = { format: 'npm', name: 'chart-render-kit', repo: 'npm-vendor' } as const;

const CYCLE: readonly { readonly grade: string; readonly counts: Counts }[] = [
  { grade: 'S', counts: {} },
  { grade: 'A', counts: { low: 3 } },
  { grade: 'B', counts: { medium: 4, low: 2 } },
  { grade: 'C', counts: { high: 2, medium: 5, low: 1 } },
  { grade: 'D', counts: { critical: 2, high: 6, medium: 3, unknown: 4 } },
  { grade: 'E', counts: { critical: 7, high: 11, medium: 9, low: 5, unknown: 12 } },
];

function manyVersions(): VendorVersion[] {
  const stable: string[] = [];
  for (let major = 1; major <= 3; major++) for (let minor = 0; minor <= 9; minor++) stable.push(`${major}.${minor}.0`);
  const all = [...stable, '4.0.0-beta.1', '4.0.0-rc.1', '4.0.0-rc.2'];
  return all.map((version, i): VendorVersion => {
    // The newest version is S, the next newer ones walk through the grades: the first rows of the table show all of them.
    const { grade, counts } = CYCLE[(all.length - 1 - i) % CYCLE.length]!;
    const next = stable[i + 1];
    const total = Object.values(counts).reduce((n, c) => n + c, 0);
    // A stable version with a later stable one has about half of its findings fixed there; a pre-release has none fixed.
    const fixable = next && total > 0 ? Math.ceil(total / 2) : 0;
    const tag = version.includes('-') ? 'next' : version === '2.9.0' ? 'lts' : 'latest';
    return { repo: manyVersionsPackage.repo, name: manyVersionsPackage.name, version, counts, grade, tag, ...(fixable > 0 ? { fixable, fixedIn: next } : {}) };
  });
}

export const manyVersionPackages: readonly VendorVersion[] = manyVersions();

/**
 * Open-source packages in the public repository `npm-open-source` of the project platform (R-UI-224, the owner's decision of 2026-11-26):
 * what a visitor without a session reads of the vulnerabilities of a public repository. config-merge 2.3.0 grades C with five of its nine
 * findings fixed in 2.4.0, which grades S; the seed dismisses one of its Medium findings, which the visitor does not see, neither as a
 * row nor in a count (publicDismissed). tar-stream-lite 1.0.4 grades D. Their records follow the others', so that no identifier moves.
 */
export const publicPackages: readonly VendorVersion[] = [
  { project: 'platform', repo: 'npm-open-source', name: 'config-merge', version: '2.3.0', counts: { high: 2, medium: 3, low: 4 }, grade: 'C', fixable: 5, fixedIn: '2.4.0' },
  { project: 'platform', repo: 'npm-open-source', name: 'config-merge', version: '2.4.0', counts: {}, grade: 'S' },
  { project: 'platform', repo: 'npm-open-source', name: 'tar-stream-lite', version: '1.0.4', counts: { critical: 2, high: 1, unknown: 1 }, grade: 'D' },
  // A public package with a README, which a Bastion's page of it opens on (bastion.md R-BAS-126, R-UI-232).
  {
    project: 'platform',
    repo: 'npm-open-source',
    name: 'yaml-lines',
    version: '1.2.0',
    counts: {},
    grade: 'S',
    readme:
      '# yaml-lines\n\nReads a YAML stream line by line and keeps the line numbers of every value.\n\n## Install\n\n```shell\nnpm install yaml-lines\n```\n\n## Use\n\n| Option | Default | What it does |\n|---|---|---|\n| `strict` | `true` | Refuses duplicate keys. |\n| `maxDepth` | `64` | Stops at nested values deeper than this. |\n',
  },
  // A public package whose versions hold every malware state a Bastion's visitor meets (R-UI-258; malware.ts writes the verdicts).
  ...['1.0.0', '1.1.0', '1.2.0', '1.3.0', '1.4.0'].map((version) => ({
    project: 'platform',
    repo: 'npm-open-source',
    name: 'checksum-stream',
    version,
    counts: {},
    grade: 'S',
  })),
];

/** The public package whose versions carry the malware states of the Bastion's renders (R-UI-258). */
export const publicMalwarePackage = { format: 'npm', name: 'checksum-stream', versions: ['1.0.0', '1.1.0', '1.2.0', '1.3.0', '1.4.0'] } as const;

/** The public package with a README (R-UI-232). */
export const publicReadmePackage = { format: 'npm', name: 'yaml-lines', version: '1.2.0' } as const;

/** The public package whose version page the renders of a visitor without a session open (R-UI-224). */
export const publicGradedPackage = { format: 'npm', name: 'config-merge', version: '2.3.0' } as const;

/** The versions of the many-version package that the seed fetches once, so that the column Last download has times (R-API-065). */
export const fetchedVersions: readonly string[] = ['3.9.0', '3.7.0', '2.9.0'];

const LEVEL: Record<keyof Counts, string | null> = { critical: 'CRITICAL', high: 'HIGH', medium: 'MODERATE', low: 'LOW', none: null, unknown: null };
const SEVERITY_ORDER: readonly (keyof Counts)[] = ['critical', 'high', 'medium', 'low', 'none', 'unknown'];
/** A CVSS 3.1 vector without any impact: its score is 0.0, the level None (R-VULN-007); a record's qualitative level cannot say None. */
const NO_IMPACT = 'CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:N/I:N/A:N';

/**
 * One OSV record per finding a vendored version needs; the severity is the qualitative one of R-VULN-007 rule 4. A fixable finding
 * affects the range from the version to its fix (a `fixed` event, R-VULN-012), the others the version alone. The records of the
 * vendored packages come first and keep their identifiers; those of the many-version package follow.
 */
function records(): { id: string; body: string }[] {
  const out: { id: string; body: string }[] = [];
  let n = 0;
  for (const p of [...vendorPackages, ...manyVersionPackages, ...publicPackages]) {
    let fixed = 0;
    for (const level of SEVERITY_ORDER) {
      const count = p.counts[level] ?? 0;
      for (let i = 0; i < count; i++) {
        const id = `DEMO-2026-${String(++n).padStart(5, '0')}`;
        const severity = LEVEL[level];
        const withFix = p.fixedIn !== undefined && fixed < (p.fixable ?? 0);
        if (withFix) fixed++;
        const affected = withFix
          ? { package: { ecosystem: 'npm', name: p.name }, ranges: [{ type: 'SEMVER', events: [{ introduced: p.version }, { fixed: p.fixedIn }] }] }
          : { package: { ecosystem: 'npm', name: p.name }, versions: [p.version] };
        out.push({
          id,
          body: JSON.stringify({
            schema_version: '1.6.0',
            id,
            modified: '2026-11-01T00:00:00Z',
            published: '2026-10-01T00:00:00Z',
            summary: `Demonstration advisory ${n} of the renders for ${p.name}`,
            details: 'Made up by the seed of make ui-renders, so that the screens show every grade. It describes no real vulnerability.',
            affected: [affected],
            ...(severity ? { database_specific: { severity } } : {}),
            ...(level === 'none' ? { severity: [{ type: 'CVSS_V3', score: NO_IMPACT }] } : {}),
          }),
        });
      }
    }
  }
  return out;
}

/** A zip archive of deflated entries with a fixed time, so that equal entries give equal bytes. */
export function zipArchive(entries: readonly { readonly name: string; readonly data: Buffer }[]): Buffer {
  const locals: Buffer[] = [];
  const centrals: Buffer[] = [];
  let offset = 0;
  for (const e of entries) {
    const name = Buffer.from(e.name);
    const packed = deflateRawSync(e.data);
    const crc = crc32(e.data);
    const local = Buffer.alloc(30);
    local.writeUInt32LE(0x04034b50, 0);
    local.writeUInt16LE(20, 4);
    local.writeUInt16LE(0, 6);
    local.writeUInt16LE(8, 8);
    local.writeUInt16LE(0, 10);
    local.writeUInt16LE(0x5b61, 12); // 2025-11-01
    local.writeUInt32LE(crc, 14);
    local.writeUInt32LE(packed.length, 18);
    local.writeUInt32LE(e.data.length, 22);
    local.writeUInt16LE(name.length, 26);
    local.writeUInt16LE(0, 28);
    const central = Buffer.alloc(46);
    central.writeUInt32LE(0x02014b50, 0);
    central.writeUInt16LE(20, 4);
    central.writeUInt16LE(20, 6);
    central.writeUInt16LE(0, 8);
    central.writeUInt16LE(8, 10);
    central.writeUInt16LE(0, 12);
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

/** The shape of the OSV dump `npm/all.zip`: one `<id>.json` per record. */
export function advisoryArchive(): Buffer {
  return zipArchive(records().map((r) => ({ name: `${r.id}.json`, data: Buffer.from(r.body) })));
}
