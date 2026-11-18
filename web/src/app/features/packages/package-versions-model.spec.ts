import { PackageVersionSummary } from '../../api/packages-api';
import { sampleGrade } from '../../api/testing/fakes-vulnerabilities';
import { VulnerabilityGrade } from '../../api/vulnerabilities-api';
import {
  DEFAULT_VERSION_SORT,
  distTagsOf,
  filterVersions,
  parseVersionSort,
  repositoriesDiffer,
  repositoryLabel,
  sortVersions,
  versionRows,
  versionSortParam,
} from './package-versions-model';
import { gradeSummaryText } from '../../shared/vulnerability/grade-words';

const summary = (version: string, over: Partial<PackageVersionSummary> = {}): PackageVersionSummary => ({
  version,
  prerelease: version.includes('-'),
  publishedAt: '2026-11-01T00:00:00Z',
  sizeBytes: 100,
  repositories: ['web/npm-local'],
  downloadCount: 0,
  lastDownloadedAt: null,
  ...over,
});

/** In the server's version order, the highest first. */
const listed = [
  summary('3.0.0-rc.1', { publishedAt: '2026-11-10T00:00:00Z', sizeBytes: 300 }),
  summary('2.1.0', { publishedAt: '2026-11-05T00:00:00Z', sizeBytes: 50, lastDownloadedAt: '2026-11-17T00:00:00Z' }),
  summary('2.0.0', { publishedAt: '2026-11-07T00:00:00Z', sizeBytes: 200, lastDownloadedAt: '2026-11-12T00:00:00Z' }),
  summary('1.0.0', { publishedAt: '2026-10-01T00:00:00Z', sizeBytes: 200 }),
];
const grades = new Map<string, VulnerabilityGrade>([
  ['2.1.0', sampleGrade('A', { low: 2 })],
  ['2.0.0', sampleGrade('E', { critical: 7 })],
  ['1.0.0', sampleGrade('D', { critical: 2 })],
]);
const rows = versionRows(listed, grades, [
  ['latest', '2.1.0'],
  ['next', '3.0.0-rc.1'],
  ['lts', '2.1.0'],
]);
const order = (rs: readonly { version: string }[]) => rs.map((r) => r.version);

describe('The versions table of a package (R-UI-110)', () => {
  it('R-UI-110 Q-UI-453 a row carries the tags that point at it, its grade, and its place in the version order', () => {
    expect(rows.map((r) => [r.version, r.tags, r.grade?.grade ?? null, r.order])).toEqual([
      ['3.0.0-rc.1', ['next'], null, 0],
      ['2.1.0', ['latest', 'lts'], 'A', 1],
      ['2.0.0', [], 'E', 2],
      ['1.0.0', [], 'D', 3],
    ]);
  });

  it('R-UI-110 Q-UI-452 the search finds a version by a part of it or by a tag, case-insensitive; an empty search keeps every row', () => {
    expect(order(filterVersions(rows, '2.'))).toEqual(['2.1.0', '2.0.0']);
    expect(order(filterVersions(rows, 'RC'))).toEqual(['3.0.0-rc.1']);
    expect(order(filterVersions(rows, 'latest'))).toEqual(['2.1.0']);
    expect(order(filterVersions(rows, '  '))).toEqual(order(rows));
    expect(filterVersions(rows, '9.9')).toEqual([]);
  });

  it('R-UI-110 Q-UI-452 every column sorts both ways; equal keys keep the highest version first', () => {
    expect(order(sortVersions(rows, DEFAULT_VERSION_SORT))).toEqual(['3.0.0-rc.1', '2.1.0', '2.0.0', '1.0.0']);
    expect(order(sortVersions(rows, { column: 'version', direction: 'asc' }))).toEqual(['1.0.0', '2.0.0', '2.1.0', '3.0.0-rc.1']);
    expect(order(sortVersions(rows, { column: 'published', direction: 'desc' }))).toEqual(['3.0.0-rc.1', '2.0.0', '2.1.0', '1.0.0']);
    // Equal sizes: 2.0.0 before 1.0.0 in both directions.
    expect(order(sortVersions(rows, { column: 'size', direction: 'asc' }))).toEqual(['2.1.0', '2.0.0', '1.0.0', '3.0.0-rc.1']);
    expect(order(sortVersions(rows, { column: 'size', direction: 'desc' }))).toEqual(['3.0.0-rc.1', '2.0.0', '1.0.0', '2.1.0']);
    // Worst first; a version without a grade ranks lowest.
    expect(order(sortVersions(rows, { column: 'vulnerabilities', direction: 'desc' }))).toEqual(['2.0.0', '1.0.0', '2.1.0', '3.0.0-rc.1']);
    // Never fetched is the oldest time.
    expect(order(sortVersions(rows, { column: 'fetched', direction: 'desc' }))).toEqual(['2.1.0', '2.0.0', '3.0.0-rc.1', '1.0.0']);
  });

  it('R-UI-110 R-UI-003 the sort of the address reads back; the default and anything unknown are the version, highest first', () => {
    for (const raw of ['version', '-size', 'published', '-fetched', 'vulnerabilities']) {
      expect(versionSortParam(parseVersionSort(raw))).toBe(raw);
    }
    expect(parseVersionSort(undefined)).toEqual(DEFAULT_VERSION_SORT);
    expect(parseVersionSort('-name')).toEqual(DEFAULT_VERSION_SORT);
    expect(versionSortParam(DEFAULT_VERSION_SORT)).toBeNull();
  });

  it('R-UI-110 Q-UI-457 the column Repositories is there only when the rows differ, and names a repository of the project by its key', () => {
    expect(repositoriesDiffer(rows)).toBe(false);
    const mixed = versionRows([summary('1.0.0'), summary('2.0.0', { repositories: ['web/npm-local', 'web/npm-remote'] })], new Map(), []);
    expect(repositoriesDiffer(mixed)).toBe(true);
    expect(repositoryLabel('web/npm-local', 'web')).toBe('npm-local');
    expect(repositoryLabel('platform/npm-shared', 'web')).toBe('platform/npm-shared');
  });

  it('R-UI-110 the distribution tags are read from the metadata; another shape is no tag', () => {
    expect(distTagsOf({ distTags: { latest: '1.0.0', beta: 2 } })).toEqual([['latest', '1.0.0']]);
    expect(distTagsOf({ distTags: ['latest'] })).toEqual([]);
    expect(distTagsOf(undefined)).toEqual([]);
  });

  it('R-UI-112 Q-UI-454 the grade reads as its total and its fixable findings, in the language of the build', () => {
    expect(gradeSummaryText(sampleGrade('E', { critical: 13, high: 160, medium: 113, low: 14, none: 2, unknown: 843 }, { fixable: 112 }), 'en')).toBe(
      '1,145 findings · 112 fixable',
    );
    expect(gradeSummaryText(sampleGrade('A', { low: 1 }), 'en')).toBe('1 finding · 0 fixable');
    expect(gradeSummaryText(sampleGrade('S'), 'en')).toBe('no findings');
    expect(gradeSummaryText(sampleGrade('N/A'), 'en')).toBe('not scanned');
    expect(gradeSummaryText(sampleGrade('C', { high: 1234 }, { fixable: 1000 }), 'de')).toBe('1.234 findings · 1.000 fixable');
  });
});
