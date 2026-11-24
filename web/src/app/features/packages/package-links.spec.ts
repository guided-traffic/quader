import { formatSize } from './format-size';
import { relativeTime } from './format-time';
import { Route, UrlMatchResult, UrlSegment, UrlSegmentGroup } from '@angular/router';
import { packageCommands, safeHttpUrl, splitVersion, versionCommands } from './package-links';
import { packageMatcher, versionMatcher } from './packages.routes';

describe('package links', () => {
  it('R-UI-007 only http and https values become links; every other scheme stays text', () => {
    expect(safeHttpUrl('https://example.com/team-a/ui')).toBe('https://example.com/team-a/ui');
    expect(safeHttpUrl('http://example.com')).toBe('http://example.com/');
    expect(safeHttpUrl('  https://example.com/x  ')).toBe('https://example.com/x');
    for (const bad of [
      'javascript:alert(1)',
      ' javascript:alert(1)',
      'JaVaScRiPt:alert(1)',
      'java\tscript:alert(1)',
      'vbscript:msgbox(1)',
      'data:text/html,<script>alert(1)</script>',
      'git+https://example.com/x.git',
      'ftp://example.com/x',
      'file:///etc/passwd',
      '//example.com/x',
      '/relative/path',
      'example.com',
      '',
      '   ',
    ]) {
      expect(safeHttpUrl(bad), bad).toBeNull();
    }
    expect(safeHttpUrl(undefined)).toBeNull();
    expect(safeHttpUrl({ url: 'https://example.com' })).toBeNull();
  });

  it('R-UI-003 a package page keeps the slash of a scoped name as a path separator', () => {
    expect(packageCommands('web', 'npm', '@team-a/ui')).toEqual(['/projects', 'web', 'packages', 'npm', '@team-a', 'ui']);
    expect(packageCommands('web', 'nuget', 'Contoso.Logging')).toEqual(['/projects', 'web', 'packages', 'nuget', 'Contoso.Logging']);
  });

  it('R-UI-111 a version page appends @ and the version to the last segment, and the matchers tell the two pages apart', () => {
    expect(versionCommands('web', 'npm', '@team-a/ui', '1.4.0')).toEqual(['/projects', 'web', 'packages', 'npm', '@team-a', 'ui@1.4.0']);
    expect(versionCommands('web', 'oci', 'portal/web', 'latest')).toEqual(['/projects', 'web', 'packages', 'oci', 'portal', 'web@latest']);
    expect(versionCommands('web', 'deb', 'hello', '1:2.10-3+b1')).toEqual(['/projects', 'web', 'packages', 'deb', 'hello@1:2.10-3+b1']);
    expect(splitVersion('ui@1.4.0')).toEqual({ name: 'ui', version: '1.4.0' });
    expect(splitVersion('@team-a')).toEqual({ name: '@team-a', version: null });
    expect(splitVersion('left-pad')).toEqual({ name: 'left-pad', version: null });
    expect(splitVersion('ui@')).toEqual({ name: 'ui@', version: null });

    const segments = (...paths: string[]) => paths.map((p) => new UrlSegment(p, {}));
    const params = (m: UrlMatchResult | null) =>
      m ? Object.fromEntries(Object.entries(m.posParams ?? {}).map(([k, v]) => [k, v.path])) : null;
    const npm = segments('npm', '@team-a', 'ui@1.5.0-beta.1');
    expect(params(versionMatcher(npm, new UrlSegmentGroup(npm, {}), {} as Route))).toEqual({
      format: 'npm',
      name: '@team-a/ui',
      version: '1.5.0-beta.1',
    });
    expect(packageMatcher(npm, new UrlSegmentGroup(npm, {}), {} as Route)).toBeNull();
    const page = segments('npm', '@team-a', 'ui');
    expect(params(packageMatcher(page, new UrlSegmentGroup(page, {}), {} as Route))).toEqual({ format: 'npm', name: '@team-a/ui' });
    expect(versionMatcher(page, new UrlSegmentGroup(page, {}), {} as Route)).toBeNull();
    const format = segments('npm');
    expect(packageMatcher(format, new UrlSegmentGroup(format, {}), {} as Route)).toBeNull();
  });

  it('R-UI-006 the updated time is relative in the language of the build; a time within a minute reads "now"', () => {
    const now = Date.parse('2026-10-30T12:00:00Z');
    expect(relativeTime('2026-10-27T12:00:00Z', 'en', now)).toBe('3 days ago');
    expect(relativeTime('2026-10-30T09:00:00Z', 'en', now)).toBe('3 hours ago');
    expect(relativeTime('2026-08-30T12:00:00Z', 'en', now)).toBe('2 months ago');
    expect(relativeTime('2026-10-30T11:59:40Z', 'en', now)).toBe('now');
    expect(relativeTime('2026-10-27T12:00:00Z', 'de', now)).toBe('vor 3 Tagen');
  });

  it('R-UI-007 R-UI-045 sizes are shown in binary units with the decimal mark of the locale', () => {
    expect(formatSize(0)).toBe('0 B');
    expect(formatSize(1023)).toBe('1023 B');
    expect(formatSize(48213)).toBe('47.1 KiB');
    expect(formatSize(5 * 1024 * 1024)).toBe('5.0 MiB');
    // The decimal mark is the locale's (seen in the German renders of the tab Downloads: "46.0 MiB").
    expect(formatSize(48213, 'de')).toBe('47,1 KiB');
    expect(formatSize(1023.95 * 1024 * 1024, 'de')).toBe('1024,0 MiB');
  });
});
