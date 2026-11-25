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
import { shownTags } from './image-tags';

const summary = (version: string, over: Partial<PackageVersionSummary> = {}): PackageVersionSummary => ({
  version,
  prerelease: version.includes('-'),
  publishedAt: '2026-11-01T00:00:00Z',
  sizeBytes: 100,
  sizePartial: false,
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
  it('R-UI-110 a row carries the tags that point at it, its grade, and its place in the version order', () => {
    expect(rows.map((r) => [r.version, r.tags, r.grade?.grade ?? null, r.order])).toEqual([
      ['3.0.0-rc.1', ['next'], null, 0],
      ['2.1.0', ['latest', 'lts'], 'A', 1],
      ['2.0.0', [], 'E', 2],
      ['1.0.0', [], 'D', 3],
    ]);
  });

  it('R-UI-110 the search finds a version by a part of it or by a tag, case-insensitive; an empty search keeps every row', () => {
    expect(order(filterVersions(rows, '2.'))).toEqual(['2.1.0', '2.0.0']);
    expect(order(filterVersions(rows, 'RC'))).toEqual(['3.0.0-rc.1']);
    expect(order(filterVersions(rows, 'latest'))).toEqual(['2.1.0']);
    expect(order(filterVersions(rows, '  '))).toEqual(order(rows));
    expect(filterVersions(rows, '9.9')).toEqual([]);
  });

  it('R-UI-110 an image of a list by content is found by its leading tag or any other tag; a list of versions has no aliases', () => {
    const images = versionRows(
      [
        summary('5.4.7', { aliases: ['5.4', 'latest', 'sha-f00cf6c'], aliasesMore: false }),
        summary('5.4.6', { aliases: ['sha-6b24a87'], aliasesMore: true }),
      ],
      new Map(),
      [],
    );
    expect(images.map((r) => [r.aliases, r.aliasesMore])).toEqual([
      [['5.4', 'latest', 'sha-f00cf6c'], false],
      [['sha-6b24a87'], true],
    ]);
    expect(order(filterVersions(images, 'SHA-6B'))).toEqual(['5.4.6']);
    expect(order(filterVersions(images, 'latest'))).toEqual(['5.4.7']);
    expect(order(filterVersions(images, '5.4'))).toEqual(['5.4.7', '5.4.6']);
    expect(rows.every((r) => r.aliases === null && !r.aliasesMore)).toBe(true);
  });

  it('R-UI-052 R-UI-110 a row names its image\'s other tags, all or the first four, and counts what it leaves out', () => {
    const tags = ['5.4', '5', 'latest', 'stable', 'sha-f00cf6c'];
    expect(shownTags(tags, false, 4)).toEqual({ shown: ['5.4', '5', 'latest', 'stable'], hidden: 1, more: false });
    expect(shownTags(tags, false, null)).toEqual({ shown: tags, hidden: 0, more: false });
    expect(shownTags(['sha-1'], true, 4)).toEqual({ shown: ['sha-1'], hidden: 0, more: true });
    expect(shownTags([], false, 4)).toEqual({ shown: [], hidden: 0, more: false });
  });

  it('R-UI-110 every column sorts both ways; equal keys keep the highest version first', () => {
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

  it('R-API-080 a size that is not known stays null and sorts below every known one; a partial size keeps its mark', () => {
    const sized = versionRows(
      [summary('1.0.0', { sizeBytes: null }), summary('2.0.0', { sizeBytes: 0 }), summary('3.0.0', { sizeBytes: 9, sizePartial: true })],
      new Map(),
      [],
    );
    expect(sized.map((r) => [r.version, r.sizeBytes, r.sizePartial])).toEqual([
      ['1.0.0', null, false],
      ['2.0.0', 0, false],
      ['3.0.0', 9, true],
    ]);
    expect(order(sortVersions(sized, { column: 'size', direction: 'asc' }))).toEqual(['1.0.0', '2.0.0', '3.0.0']);
  });

  it('R-UI-110 R-UI-003 the sort of the address reads back; the default and anything unknown are the version, highest first', () => {
    for (const raw of ['version', '-size', 'published', '-fetched', 'vulnerabilities']) {
      expect(versionSortParam(parseVersionSort(raw))).toBe(raw);
    }
    expect(parseVersionSort(undefined)).toEqual(DEFAULT_VERSION_SORT);
    expect(parseVersionSort('-name')).toEqual(DEFAULT_VERSION_SORT);
    expect(versionSortParam(DEFAULT_VERSION_SORT)).toBeNull();
  });

  it('R-UI-110 the column Repositories is there only when the rows differ, and names a repository of the project by its key', () => {
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

  it('R-UI-112 the grade reads as its total and its fixable findings, in the language of the build', () => {
    expect(gradeSummaryText(sampleGrade('E', { critical: 13, high: 160, medium: 113, low: 14, none: 2, unknown: 843 }, { fixable: 112 }), 'en')).toBe(
      '1,145 findings · 112 fixable',
    );
    expect(gradeSummaryText(sampleGrade('A', { low: 1 }), 'en')).toBe('1 finding · 0 fixable');
    expect(gradeSummaryText(sampleGrade('S'), 'en')).toBe('no findings');
    expect(gradeSummaryText(sampleGrade('N/A', {}, { state: 'no_scanner' }), 'en')).toBe('No scanner');
    expect(gradeSummaryText(sampleGrade('C', { high: 1234 }, { fixable: 1000 }), 'de')).toBe('1.234 findings · 1.000 fixable');
  });
});
