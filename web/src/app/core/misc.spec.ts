import { TestBed } from '@angular/core/testing';
import { Title } from '@angular/platform-browser';
import { Router, TitleStrategy, provideRouter } from '@angular/router';
import { Notifier } from './notifier';
import { BrowserStorage } from './storage';
import { QuaderTitleStrategy } from './title-strategy';
import { diffFields } from '../shared/conflict-dialog';
import { filesMatcher } from '../features/repositories/repositories.routes';
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
  it('R-UI-042 lists only the fields that differ, theirs against yours', () => {
    const rows = diffFields({
      mine: { description: 'mine', enabled: true, extra: 1 },
      theirs: { description: 'theirs', enabled: true },
    });
    expect(rows).toEqual([
      { field: 'description', mine: '"mine"', theirs: '"theirs"' },
      { field: 'extra', mine: '1', theirs: '' },
    ]);
  });
});
