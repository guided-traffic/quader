import { PackageSummary, PackageVersion } from '../packages-api';
import { FakePackagesApi } from './fakes-packages';

/*
 * The ClamAV definitions of the fakes (R-UI-194, R-CLAMAV-003): the three databases of `upstream/clamav` with the metadata the format's
 * view adapter sends (`internal/formats/clamav/view.go` `Metadata`). `daily` has the version in use, the one before it and a history
 * row; `main` was built 8 days before `NOW` and `bytecode` 6 days before, neither of them outdated (Q-UI-912). `dailyDays` sets the age of
 * the `daily` in use, for a case that is outdated.
 */

/** The clock of the fixture: the ages count from it. */
export const CLAMAV_NOW = Date.parse('2026-11-21T12:00:00Z');
const DAY = 86_400_000;
const REPO = 'upstream/clamav';
const CONTENT = '/api/v1/projects/upstream/repositories/clamav/content';

interface FixtureVersion {
  readonly db: string;
  readonly version: string;
  readonly days: number;
  readonly state: 'in_use' | 'previous' | 'superseded' | 'files_removed';
}

const VERSIONS: readonly FixtureVersion[] = [
  { db: 'daily', version: '28188', days: 3, state: 'files_removed' },
  { db: 'daily', version: '28189', days: 1, state: 'previous' },
  { db: 'daily', version: '28190', days: 0.2, state: 'in_use' },
  { db: 'main', version: '63', days: 8, state: 'in_use' },
  { db: 'bytecode', version: '339', days: 6, state: 'in_use' },
];

const iso = (days: number): string => new Date(CLAMAV_NOW - days * DAY).toISOString();

/** The package id of a database in the fakes. */
export const clamavPackageId = (db: string): string => `pkg_clamav${db}`;

/** The metadata of one version, as the view adapter sends it. */
export function clamavMetadata(v: FixtureVersion): Record<string, unknown> {
  const removed = v.state === 'files_removed';
  return {
    database: v.db,
    version: Number(v.version),
    state: v.state,
    storedAt: iso(v.days),
    file: `${v.db}.cvd`,
    ...(removed ? { filesRemovedAt: iso(v.days - 1) } : {}),
    builtAt: iso(v.days),
    buildTimeText: '21 Nov 2026 07-00 +0000',
    signatures: 2_076_903,
    functionalityLevel: 210,
    functionalityLevelText: 'ClamAV 1.5 or later',
    builder: 'raynman',
    verification: { outcome: 'signature_verified', headerMd5: 'a1'.repeat(16), computedMd5: 'a1'.repeat(16), source: '' },
    membersRead: !removed,
    members: removed
      ? []
      : [
          { name: `${v.db}.ndb`, size: 8_712_330, sha256: 'b'.repeat(64), lines: 70_115 },
          { name: `${v.db}.info`, size: 1_066, sha256: 'c'.repeat(64), lines: 0 },
        ],
    detachedSignature: removed ? { present: false } : { present: true, name: `${v.db}-${v.version}.cvd.sign`, size: 1_284, signatures: 2 },
  };
}

function versionOf(v: FixtureVersion): PackageVersion & { readme: null } {
  const removed = v.state === 'files_removed';
  const path = `${v.db}/${v.version}/${v.db}.cvd`;
  return {
    packageId: clamavPackageId(v.db),
    format: 'clamav',
    name: v.db,
    version: v.version,
    prerelease: false,
    publishedAt: iso(v.days),
    licenses: [],
    keywords: [],
    sizeBytes: removed ? 0 : 63_018_446,
    sizePartial: false,
    downloadCount: 3,
    lastDownloadedAt: null,
    hasReadme: false,
    metadata: clamavMetadata(v),
    repositories: [{ key: REPO, class: 'local', publishedAt: iso(v.days), available: true }],
    files: removed
      ? []
      : [{ path, sizeBytes: 63_018_446, sha256: 'd'.repeat(64), downloadUrl: `https://quader.example.com/files/${REPO}/${path}`, contentUrl: `${CONTENT}/${path}` }],
    readme: null,
  };
}

function summaryOf(db: string): PackageSummary {
  const vs = VERSIONS.filter((v) => v.db === db);
  const inUse = vs.find((v) => v.state === 'in_use')!;
  return {
    id: clamavPackageId(db),
    format: 'clamav',
    name: db,
    description: `ClamAV ${db} signatures`,
    latestVersion: inUse.version,
    keywords: [],
    repositories: [REPO],
    createdAt: iso(vs[0]!.days),
    updatedAt: iso(inUse.days),
    versionCount: vs.length,
    downloadCount: 3,
  };
}

/** Adds the three databases of `upstream/clamav` to the fake packages. */
export function clamavFixture(fake: FakePackagesApi, options: { readonly dailyDays?: number } = {}): void {
  const versions = VERSIONS.map((v) => (v.db === 'daily' && v.state === 'in_use' && options.dailyDays !== undefined ? { ...v, days: options.dailyDays } : v));
  fake.packages.push(...['bytecode', 'daily', 'main'].map(summaryOf));
  fake.versionList.push(...versions.map(versionOf));
}
