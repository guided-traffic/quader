import { TestBed } from '@angular/core/testing';
import { Title } from '@angular/platform-browser';
import { Router, TitleStrategy, provideRouter } from '@angular/router';
import { Notifier } from './notifier';
import { BrowserStorage } from './storage';
import { QuaderTitleStrategy } from './title-strategy';
import { diffFields } from '../shared/conflict-dialog';
import { filesMatcher } from '../features/repositories/files-matchers';
import { UrlSegment } from '@angular/router';

describe('BrowserStorage', () => {
  it('R-UI-006 stores and reads through the wrapper', () => {
    const s = TestBed.inject(BrowserStorage);
    expect(s.available('local')).toBe(true);
    expect(s.set('local', 'quader.k', 'v')).toBe(true);
    expect(s.get('local', 'quader.k')).toBe('v');
    s.remove('local', 'quader.k');
    expect(s.get('local', 'quader.k')).toBeNull();
  });

  it('R-UI-006 blocked storage only makes the features disappear: reads answer null, writes false', () => {
    const s = TestBed.inject(BrowserStorage);
    const spy = vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {
      throw new DOMException('blocked', 'SecurityError');
    });
    expect(s.available('session')).toBe(false);
    expect(s.set('session', 'k', 'v')).toBe(false);
    spy.mockRestore();
    const get = vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => {
      throw new DOMException('blocked', 'SecurityError');
    });
    expect(s.get('local', 'k')).toBeNull();
    get.mockRestore();
  });
});

describe('Notifier', () => {
  it('R-UI-042 keeps errors and notices with an action until dismissed, and lets a plain confirmation expire', () => {
    vi.useFakeTimers();
    const n = TestBed.inject(Notifier);
    n.status('Saved');
    n.status('Deleted', { label: 'Undo', run: () => undefined });
    n.alert('Failed');
    expect(n.notices().map((x) => x.kind)).toEqual(['status', 'status', 'alert']);
    vi.advanceTimersByTime(9000);
    expect(n.notices().map((x) => x.text)).toEqual(['Deleted', 'Failed']);
    vi.useRealTimers();
  });
});

describe('QuaderTitleStrategy', () => {
  it('R-UI-003 sets "Title – Quader" from the route title', async () => {
    TestBed.configureTestingModule({
      providers: [
        provideRouter([{ path: 'x', title: 'Packages', children: [] }]),
        { provide: TitleStrategy, useExisting: QuaderTitleStrategy },
      ],
    });
    await TestBed.inject(Router).navigateByUrl('/x');
    expect(TestBed.inject(Title).getTitle()).toBe('Packages – Quader');
  });
});

describe('filesMatcher', () => {
  const seg = (...p: string[]) => p.map((x) => new UrlSegment(x, {}));

  it('R-UI-003 takes the segments after `files` as the path, slashes kept (the repository is the parent route)', () => {
    const m = filesMatcher(seg('files', '@team-a', 'ui'), null as never, null as never);
    expect(m?.consumed.length).toBe(3);
    expect(m?.posParams?.['filePath']?.path).toBe('@team-a/ui');
  });

  it('R-UI-003 matches `files` alone as the root and nothing else', () => {
    expect(filesMatcher(seg('files'), null as never, null as never)?.posParams?.['filePath']?.path).toBe('');
    expect(filesMatcher(seg('settings'), null as never, null as never)).toBeNull();
    expect(filesMatcher([], null as never, null as never)).toBeNull();
  });
});

describe('conflict diff', () => {
  it('R-UI-042 lists only the fields that differ, theirs against yours, by their API names where the form names none', () => {
    const rows = diffFields({
      mine: { description: 'mine', enabled: true, extra: 1 },
      theirs: { description: 'theirs', enabled: true },
    });
    expect(rows).toEqual([
      { pointer: '/description', field: 'description', apiName: true, mine: { entries: ['mine'] }, theirs: { entries: ['theirs'] } },
      { pointer: '/extra', field: 'extra', apiName: true, mine: { entries: ['1'] }, theirs: { entries: [] } },
    ]);
  });

  it('R-UI-042 names a field by the form\'s label and shows a value as the form does: a list as its entries, a switch as on or off, none, never a secret', () => {
    const rows = diffFields({
      mine: { includePatterns: ['a', 'b'], hideExistence: 'true', remote: { url: 'https://x', password: { value: 's3cret' } }, on: true },
      theirs: { includePatterns: [], hideExistence: 'inherit', remote: { url: 'https://y' }, on: false },
      fields: {
        '/includePatterns': 'Include patterns',
        '/hideExistence': { label: 'Hide existence', show: (v) => (v === 'true' ? 'Hide' : 'Use the installation setting') },
        '/remote/url': 'Upstream URL',
        '/on': 'Enabled',
      },
    });
    expect(rows.map((r) => [r.field, r.apiName, r.theirs.entries, r.mine.entries])).toEqual([
      ['Include patterns', false, [], ['a', 'b']],
      ['Hide existence', false, ['Use the installation setting'], ['Hide']],
      ['Upstream URL', false, ['https://y'], ['https://x']],
      // A member the form does not name keeps its API name; a password's value never stands, whatever the form says.
      ['remote/password/value', true, [], ['Hidden']],
      ['Enabled', false, ['Off'], ['On']],
    ]);
    expect(JSON.stringify(rows)).not.toContain('s3cret');
  });

  it('R-UI-042 two empty values do not differ, and an empty value the form names shows the form\'s choice', () => {
    expect(diffFields({ mine: { a: '', b: [] }, theirs: { a: null } })).toEqual([]);
    const [row] = diffFields({
      mine: { locale: 'de' },
      theirs: { locale: null },
      fields: { '/locale': { label: 'Language', show: () => 'Deutsch', empty: 'Follow the browser' } },
    });
    expect([row!.theirs.entries, row!.mine.entries]).toEqual([['Follow the browser'], ['Deutsch']]);
  });
});
