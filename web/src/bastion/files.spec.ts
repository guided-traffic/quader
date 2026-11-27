import { provideHttpClient, withInterceptors } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { provideRouter } from '@angular/router';
import { Item } from '../app/api/file-reads';
import { FileReads } from '../app/api/file-reads';
import { ClientSetupReads } from '../app/api/package-reads';
import { ApiError } from '../app/api/problem';
import { bastionDataInterceptor } from './data/http';
import { provideBastionReads } from './data/reads';
import { FilesPage } from './pages/files-page';
import { PackageListPage } from './pages/package-list-page';

// A public generic repository's folders and files at the Bastion (api-and-ui.md R-UI-229, Q-UI-1454 to Q-UI-1457; bastion.md R-BAS-118,
// Q-BAS-400) against the HTTP testing backend: the reads ask the catalogue's documents of the repository, the page decides a file from a
// folder by them and composes the shared views without an act. Answers of tens of bytes; the file runs in well under a second.

let backend: HttpTestingController | null = null;

function setup(): HttpTestingController {
  TestBed.configureTestingModule({
    providers: [
      provideRouter([]),
      provideHttpClient(withInterceptors([bastionDataInterceptor])),
      provideHttpClientTesting(),
      ...provideBastionReads(),
    ],
  });
  backend = TestBed.inject(HttpTestingController);
  return backend;
}

const turn = (): Promise<void> => new Promise((r) => setTimeout(r, 0));

/** The Bastion's package route of the file, built so (R-ARCH-007 reads every literal of an address). */
const route = ['', 'files', 'platform', 'downloads', 'tools', 'agent%201.9.tar.gz'].join('/');

const notFound = { type: 'about:blank', title: 'Not Found', status: 404, code: 'not_found', detail: 'Nothing is offered at this address.' };
const notFoundAnswer = { status: 404, statusText: 'Not Found', headers: { 'Content-Type': 'application/problem+json' } };

/** A file as the catalogue keeps it: no person, no API address, its download the Bastion's package route (R-BAS-106, R-BAS-107). */
const agent: Item = {
  repository: 'platform/downloads',
  path: 'tools/agent 1.9.tar.gz',
  kind: 'file',
  size: 2048,
  mediaType: 'application/gzip',
  digests: { sha256: 'a'.repeat(64), sha512: 'c'.repeat(128) },
  contentStatus: 'ok',
  downloadUrl: 'https://bastion.example.org/files/platform/downloads/tools/agent%201.9.tar.gz',
  contentUrl: route,
  created: { at: '2026-11-26T08:00:00Z', by: null },
  modified: { at: '2026-11-26T08:00:00Z', by: null },
  updatedAt: '2026-11-26T08:00:00Z',
  package: null,
  upstreamUrl: null,
  signatures: { state: 'none', stale: false },
  itemUrl: null,
} as unknown as Item;

async function settled<T>(fixture: ComponentFixture<T>): Promise<void> {
  fixture.detectChanges();
  await turn();
  await fixture.whenStable();
  fixture.detectChanges();
}

describe("A generic repository's files at the Bastion (R-UI-229)", () => {
  afterEach(() => {
    backend?.verify();
    backend = null;
  });

  it("R-UI-229 the file reads ask the repository's documents in the catalogue spelling, a folder in pages of the view's size", async () => {
    const http = setup();
    const files = TestBed.inject(FileReads);
    const item = files.item('platform/downloads', "tools/agent 1.9.tar.gz");
    http.expectOne('/ui/data/v1/files/platform/downloads/file/tools/agent%201.9.tar.gz').flush(agent);
    expect((await item).path).toBe('tools/agent 1.9.tar.gz');

    // The folder's document holds every entry; the view's pages of two are cut from it, the last without a cursor.
    const entries = { items: [{ path: 'a', kind: 'folder' }, { path: 'b.txt', kind: 'file' }, { path: 'c.txt', kind: 'file' }] };
    const fetcher = files.children('platform/downloads', '', 2);
    const first = fetcher(null, 2);
    http.expectOne('/ui/data/v1/files/platform/downloads/folder').flush(entries);
    expect(await first).toEqual({ items: entries.items.slice(0, 2), nextCursor: '2' });
    const second = fetcher('2', 2);
    http.expectOne('/ui/data/v1/files/platform/downloads/folder').flush(entries);
    expect(await second).toEqual({ items: entries.items.slice(2), nextCursor: null });
    const nested = files.children('platform/downloads', "tools/it's", 200)(null, 200);
    http.expectOne('/ui/data/v1/files/platform/downloads/folder/tools/it%27s').flush({ items: [] });
    expect((await nested).items).toEqual([]);

    // A file's commands are the set-up of its path (Q-UI-1454); a folder's upload is no read of a visitor and asks nothing.
    const setupReads = TestBed.inject(ClientSetupReads);
    const commands = setupReads.get('platform/downloads', { client: 'curl', credentials: 'none', path: 'tools/agent 1.9.tar.gz', sha256: 'a'.repeat(64) });
    http.expectOne('/ui/data/v1/files/platform/downloads/setup/curl/tools/agent%201.9.tar.gz').flush({ client: 'curl', snippets: [] });
    expect(await commands).toEqual({ client: 'curl', snippets: [] });
    await expect(setupReads.get('platform/downloads', { client: 'curl', path: 'tools/' })).rejects.toBeInstanceOf(ApiError);
  });

  it("R-UI-229 a file's page shows its path with Download through the Bastion, its facts and checksums, and no act, no statistics and no Signatures", async () => {
    const http = setup();
    const fixture = TestBed.createComponent(FilesPage);
    fixture.componentRef.setInput('project', 'platform');
    fixture.componentRef.setInput('key', 'downloads');
    fixture.componentRef.setInput('filePath', 'tools/agent 1.9.tar.gz');
    fixture.detectChanges();
    // The parent's entry says it is a file, then its description is read.
    http.expectOne('/ui/data/v1/files/platform/downloads/folder/tools').flush({ items: [{ path: 'agent 1.9.tar.gz', kind: 'file' }] });
    await turn();
    http.expectOne('/ui/data/v1/files/platform/downloads/file/tools/agent%201.9.tar.gz').flush(agent);
    await settled(fixture);
    const root = fixture.nativeElement as HTMLElement;
    const segs = [...root.querySelectorAll('.files-trail .seg:not(.collapsed)')].map((s) => s.querySelector('a, .here-name span:last-child')?.textContent);
    expect(segs).toEqual(['Files', 'tools', 'agent 1.9.tar.gz']);
    expect(root.querySelector('.files-trail a')?.getAttribute('href')).toBe('/projects/platform/repositories/downloads');
    expect(root.querySelector('.file-actions a[download]')?.getAttribute('href')).toBe(route);
    expect([...root.querySelectorAll('.file-actions button')].map((b) => b.textContent?.trim()).filter((t) => t === 'Delete')).toEqual([]);
    const facts = [...root.querySelectorAll('dl.file-facts dt')].map((dt) => dt.textContent?.trim());
    expect(facts).toEqual(['Size', 'Media type', 'Created', 'Last changed', 'Signature', 'SHA-256']);
    // Who uploaded it is no fact of the catalogue (R-BAS-106).
    expect(root.querySelector('dl.file-facts')?.textContent).not.toContain(' by ');
    const headings = [...root.querySelectorAll('quader-disclosure')].map((d) => d.textContent ?? '');
    expect(headings).toHaveLength(1);
    expect(headings[0]).toContain('Every checksum of the file.');
    expect(headings[0]).toContain('SHA-512');
    expect(headings[0]).not.toContain('Downloads');
    expect(headings[0]).not.toContain('quader_curl');
  });

  it("R-UI-229 a folder is a folder by its parent's entry, without a read that answers 404: its entries, a row download on the Bastion, no upload and no delete", async () => {
    const http = setup();
    const fixture = TestBed.createComponent(FilesPage);
    fixture.componentRef.setInput('project', 'platform');
    fixture.componentRef.setInput('key', 'downloads');
    fixture.componentRef.setInput('filePath', 'tools');
    fixture.detectChanges();
    http.expectOne('/ui/data/v1/files/platform/downloads/folder').flush({ items: [{ path: 'tools', kind: 'folder' }] });
    await turn();
    http.expectNone('/ui/data/v1/files/platform/downloads/file/tools');
    const entries = { items: [{ path: 'agent.tar.gz', kind: 'file', size: 2048, sha256: 'a'.repeat(64), modifiedAt: '2026-11-26T08:00:00Z' }] };
    await settled(fixture);
    http.expectOne('/ui/data/v1/files/platform/downloads/folder/tools').flush(entries);
    await settled(fixture);
    const root = fixture.nativeElement as HTMLElement;
    const rows = [...root.querySelectorAll('quader-data-table tbody tr')];
    expect(rows.map((r) => r.querySelector('.entry-name')?.textContent)).toEqual(['agent.tar.gz']);
    expect(rows[0]?.querySelector('th a')?.getAttribute('href')).toBe('/projects/platform/repositories/downloads/files/tools/agent.tar.gz');
    expect(rows[0]?.querySelector('quader-client-command-menu')).not.toBeNull();
    expect(root.querySelector('.files-actions')).toBeNull();
    expect(root.textContent).not.toContain('Upload files');
  });

  it('R-UI-229 a path its parent does not list is read as a file, and the notice shows its answer whole', async () => {
    const http = setup();
    const fixture = TestBed.createComponent(FilesPage);
    fixture.componentRef.setInput('project', 'platform');
    fixture.componentRef.setInput('key', 'downloads');
    fixture.componentRef.setInput('filePath', 'gone.txt');
    fixture.detectChanges();
    http.expectOne('/ui/data/v1/files/platform/downloads/folder').flush({ items: [] });
    await turn();
    http.expectOne('/ui/data/v1/files/platform/downloads/file/gone.txt').flush(notFound, notFoundAnswer);
    await settled(fixture);
    expect((fixture.nativeElement as HTMLElement).querySelector('quader-error-notice')?.textContent).toContain('Nothing is offered at this address.');
  });

  it("R-UI-229 a project's packages lead to its repositories with files, which no search of packages finds", async () => {
    const http = setup();
    const fixture = TestBed.createComponent(PackageListPage);
    fixture.componentRef.setInput('project', 'platform');
    fixture.detectChanges();
    http.expectOne('/ui/data/v1/files').flush({ items: ['platform/downloads', 'other/files'] });
    http.expectOne((r) => r.url === '/ui/data/v1/packages').flush({ items: [], nextCursor: null });
    http.expectOne('/ui/data/v1/projects').flush({ items: [{ key: 'platform', displayName: 'Platform', description: '' }] });
    await settled(fixture);
    const root = fixture.nativeElement as HTMLElement;
    const links = [...root.querySelectorAll('nav.file-repositories a')];
    expect(links.map((a) => a.textContent?.trim())).toEqual(['folderdownloads']);
    expect(links[0]?.getAttribute('href')).toBe('/projects/platform/repositories/downloads');
  });
});
