import { FileReads } from '../../api/file-reads';
import { FilesApi, Item } from '../../api/files-api';
import { FakeFilesApi } from '../../api/testing/fakes-catalog';
import { renderScreen, text } from '../../testing/screen';
import { FileView } from './file-view';
import { FilesHead } from './files-head';
import { FolderView } from './folder-view';

// The views of a repository's folders and files (R-UI-223), composed without a page's acts, with the main installation's binding of
// their reads (FileReads to FilesApi, provideViewReads) and its fakes. Fixtures of a few entries; the file runs in about a second.

const file = (over: Partial<Item> = {}): Item => ({
  repository: 'web/generic-local',
  path: 'releases/1.0/app.zip',
  kind: 'file',
  size: 2048,
  mediaType: 'application/zip',
  digests: { sha256: 'a'.repeat(64), md5: 'd'.repeat(32) },
  contentStatus: 'ok',
  downloadUrl: 'https://quader.example.net/files/web/generic-local/releases/1.0/app.zip',
  contentUrl: '/api/v1/projects/web/repositories/generic-local/content/releases/1.0/app.zip',
  created: { at: '2026-11-21T08:00:00Z', by: 'user:priya' },
  modified: { at: '2026-11-21T08:00:00Z', by: 'user:priya' },
  updatedAt: '2026-11-21T08:00:00Z',
  package: null,
  upstreamUrl: null,
  signatures: { state: 'none', stale: false },
  itemUrl: 'https://quader.example.net/api/v1/projects/web/repositories/generic-local/items?path=releases/1.0/app.zip',
  ...over,
});

describe('The views of a repository\'s files (R-UI-223)', () => {
  it("R-UI-223 the folder's view reads through FileReads, which the main installation binds to its FilesApi, and draws no act of its own", async () => {
    const { root, get } = await renderScreen(FolderView, {
      inputs: { repository: 'web/generic-local', path: 'releases/1.0', generic: true, filesAddress: 'https://quader.example.net/files/web/generic-local' },
    });
    expect(get(FileReads)).toBe(get(FilesApi));
    expect((get(FilesApi) as FakeFilesApi).childCalls.map((c) => c.path)).toEqual(['releases/1.0']);
    expect([...root.querySelectorAll('tbody .entry-name')].map((e) => text(e))).toEqual(['app.zip', 'app.zip.sha256']);
    // A row's Download is a read: the panel of the address and the clients' commands stays; the folder's operations are the page's.
    expect(root.querySelectorAll('tbody quader-client-command-menu')).toHaveLength(2);
    expect(root.querySelector('.files-actions')).toBeNull();
    expect(root.textContent).not.toContain('Upload files');
  });

  it("R-UI-223 the file's view has its facts and its checksums, the section Signatures only where the application reads a file's report", async () => {
    const { root } = await renderScreen(FileView, { inputs: { item: file(), signatures: false } });
    const facts = [...root.querySelectorAll('dl.file-facts dt')].map((dt) => text(dt));
    expect(facts).toEqual(['Size', 'Media type', 'Created', 'Last changed', 'Signature', 'SHA-256']);
    const sections = [...root.querySelectorAll('quader-disclosure')];
    expect(sections).toHaveLength(1);
    expect(text(sections[0]!)).toContain('Every checksum of the file.');
    expect(text(sections[0]!)).toContain('MD5');
    expect(text(sections[0]!)).not.toContain('The file as the API describes it');
    const { root: main } = await renderScreen(FileView, { inputs: { item: file() } });
    expect([...main.querySelectorAll('quader-disclosure')].map((d) => text(d.querySelector('.heading, button')!).replace('chevron_right', ''))).toEqual(['Signatures', 'Details']);
  });

  it("R-UI-223 the path names each folder, and a file's Download stands at its end without the page's Delete", async () => {
    const { root } = await renderScreen(FilesHead, {
      inputs: {
        repository: 'web/generic-local',
        path: 'releases/1.0/app.zip',
        root: ['/projects', 'web', 'repositories', 'generic-local'],
        file: file(),
        generic: true,
        verb: 'Download',
      },
    });
    const links = [...root.querySelectorAll('.files-trail .seg:not(.collapsed) a')].map((a) => a.getAttribute('href'));
    expect(links).toEqual([
      '/projects/web/repositories/generic-local',
      '/projects/web/repositories/generic-local/files/releases',
      '/projects/web/repositories/generic-local/files/releases/1.0',
    ]);
    expect(root.querySelector('.file-actions a[download]')?.getAttribute('href')).toBe(file().contentUrl);
    expect([...root.querySelectorAll('.file-actions button')].some((b) => text(b) === 'Delete')).toBe(false);
    // A refused file offers no Download (Q-UI-882).
    const { root: refused } = await renderScreen(FilesHead, {
      inputs: {
        repository: 'web/generic-local',
        path: 'releases/1.0/app.zip',
        root: ['/projects', 'web', 'repositories', 'generic-local'],
        file: file({ signatures: { state: 'none', stale: false, refused: true, refusedReason: 'missing' } as Item['signatures'] }),
        verb: 'Download',
      },
    });
    expect(refused.querySelector('.file-actions a[download]')).toBeNull();
  });
});
