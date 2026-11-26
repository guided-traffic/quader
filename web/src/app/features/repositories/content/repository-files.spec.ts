import { Router } from '@angular/router';
import { FilesApi, UPLOAD_LIMIT_BYTES } from '../../../api/files-api';
import { ApiError } from '../../../api/problem';
import { RepositoriesApi } from '../../../api/repositories-api';
import { SessionApi } from '../../../api/session-api';
import { FakeSessionApi } from '../../../api/testing/fakes';
import { FakeFilesApi, FakeNode, FakeRepositoriesApi, GENERIC_FORMAT, PACKAGE_FORMAT } from '../../../api/testing/fakes-catalog';
import { PermissionCheck } from '../../../api/session-api';
import { malwareStatus } from '../../../api/testing/fakes-malware';
import { spokenText } from '../../../testing/axe';
import { button, expectAccessible, renderScreen, settle, sleep, text, type } from '../../../testing/screen';
import { RepositoryContent } from './repository-content';

/**
 * The folders and files of a repository on its page (R-UI-010 to R-UI-012, R-UI-052, ADR-203): the routes bind the project,
 * the key and the path after `files` (`filePath`); a generic repository's own address is the root of its files.
 */
async function open(
  key: string,
  filePath: string | undefined,
  setup?: (files: FakeFilesApi, catalog: FakeRepositoriesApi) => void,
  allow?: (check: PermissionCheck) => boolean,
) {
  const rendered = await renderScreen(RepositoryContent, {
    inputs: { project: 'web', key, ...(filePath === undefined ? {} : { filePath }) },
    setup: (get) => {
      const catalog = get(RepositoriesApi) as FakeRepositoriesApi;
      catalog.formatsResult = [GENERIC_FORMAT, PACKAGE_FORMAT];
      catalog.repositories = [
        ...catalog.repositories,
        { key: 'npm-local', project: 'web', class: 'local', format: 'npm', description: 'Internal packages' } as never,
      ];
      if (allow) (get(SessionApi) as FakeSessionApi).allow = allow;
      vi.spyOn(get(Router), 'navigate').mockResolvedValue(true);
      const files = get(FilesApi) as FakeFilesApi;
      files.tree.set('web/npm-local', [
        { path: '@team-a', kind: 'folder' },
        { path: '@team-a/ui', kind: 'folder' },
        { path: '@team-a/ui/-', kind: 'folder' },
        { path: '@team-a/ui/-/ui-1.4.0.tgz', kind: 'file', size: 4096 },
      ]);
      setup?.(files, catalog);
    },
  });
  return { ...rendered, files: rendered.get(FilesApi) as FakeFilesApi, router: rendered.get(Router) };
}

const rows = (root: HTMLElement) => [...root.querySelectorAll<HTMLTableRowElement>('quader-data-table tbody tr')];
const names = (root: HTMLElement) => rows(root).map((r) => text(r.querySelector('th .entry-name')!));
const confirmButton = (body: HTMLElement) =>
  [...body.querySelectorAll<HTMLButtonElement>('.dialog-actions button')].find((b) => b.textContent?.trim() === 'Delete')!;

describe('The files of a repository', () => {
  it('R-UI-010 R-UI-052 R-GENERIC-017 the root of a generic repository lists folders and files; each name links to its own address and a file\'s row has Download with its address and every client\'s command', async () => {
    const { root, fixture } = await open('generic-local', undefined);
    // In the order of the server's answer.
    expect(names(root)).toEqual(['releases', 'readme.txt']);
    const links = rows(root).map((r) => r.querySelector('th a')?.getAttribute('href'));
    expect(links).toEqual([
      '/projects/web/repositories/generic-local/files/releases',
      '/projects/web/repositories/generic-local/files/readme.txt',
    ]);
    expect(rows(root)[0]!.querySelector('quader-client-command-menu')).toBeNull();
    // The row's act, where a package's row has its verb (R-GENERIC-017): the panel of the file's address and commands.
    const trigger = rows(root)[1]!.querySelector<HTMLButtonElement>('quader-client-command-menu button')!;
    expect(spokenText(trigger)).toBe('readme.txt: Download');
    // The root of a generic repository is the repository's page: no trail repeats its name.
    expect(root.querySelector('nav[aria-label="Folder path"]')).toBeNull();
    await expectAccessible(root);
    document.body.appendChild(root);
    trigger.click();
    await settle(fixture);
    const panel = document.getElementById(trigger.getAttribute('aria-controls')!)!;
    expect(panel.getAttribute('aria-label')).toBe('Download readme.txt');
    expect(panel.querySelector<HTMLInputElement>('.command-address input')?.value).toBe('https://quader.example.net/files/web/generic-local/readme.txt');
    expect([...panel.querySelectorAll('[role="tab"]')].map((t) => text(t))).toEqual(['curl', 'wget', 'PowerShell']);
    const shown = [...panel.querySelectorAll<HTMLInputElement>('[role="tabpanel"]:not([inert]) quader-address-field input')].map((i) => i.value);
    expect(shown).toEqual([
      'quader_curl --fail --location --output readme.txt "https://quader.example.net/files/web/generic-local/readme.txt"',
      `printf '%s  %s\\n' ${'a'.repeat(64)} readme.txt | sha256sum -c`,
    ]);
    expect(panel.querySelector('a.setup-link')?.getAttribute('href')).toBe('/projects/web/repositories/generic-local/setup?client=curl');
    await expectAccessible(panel);
    trigger.click();
    await settle(fixture);
    root.remove();
  });

  it('R-GENERIC-017 R-UI-011 Upload files keeps its dialog as the main act, and its chevron offers the upload into the folder with each client', async () => {
    const { root, fixture } = await open('generic-local', 'releases', undefined, () => true);
    document.body.appendChild(root);
    const chevron = root.querySelector<HTMLButtonElement>('.files-actions quader-client-command-menu button[aria-haspopup="dialog"]')!;
    expect(chevron.getAttribute('aria-label')).toBe('Upload with a client');
    expect(text(root.querySelector('.files-actions quader-client-command-menu')!)).toContain('Upload files');
    chevron.click();
    await settle(fixture);
    const panel = document.getElementById(chevron.getAttribute('aria-controls')!)!;
    expect(panel.getAttribute('aria-label')).toBe('Upload releases');
    const tabs = [...panel.querySelectorAll<HTMLElement>('[role="tab"]')];
    tabs[2]!.click();
    await settle(fixture);
    const shown = panel.querySelector<HTMLInputElement>('[role="tabpanel"]:not([inert]) quader-address-field input')!;
    expect(shown.value).toBe('Invoke-WebRequest -Method Put -InFile <file> -Uri "https://quader.example.net/files/web/generic-local/releases/<file>" -Headers $QuaderHeaders');
    // An upload has no check line: there is nothing downloaded to check.
    expect(panel.querySelectorAll('[role="tabpanel"]:not([inert]) quader-client-command')).toHaveLength(1);
    chevron.click();
    await settle(fixture);
    root.remove();
  });

  it('R-UI-010 a folder deep in the path shows its path once: Files, each folder a link, the last one the page with the copy button after it', async () => {
    const { root } = await open('generic-local', 'releases/1.0');
    const trail = root.querySelector('nav[aria-label="Folder path"]')!;
    // The collapsed control of the folders in between is in the DOM; a container query shows it or the folders by the width.
    expect([...trail.querySelectorAll('li.seg:not(.collapsed)')].map((li) => text(li.querySelector('a, .here-name')!))).toEqual(['Files', 'releases', '1.0']);
    expect(trail.querySelector('li.collapsed button')?.getAttribute('aria-label')).toBe('Show the folders in between');
    expect(trail.querySelector('li.here quader-copy-button')).not.toBeNull();
    expect([...trail.querySelectorAll('a')].map((a) => a.getAttribute('href'))).toEqual([
      '/projects/web/repositories/generic-local',
      '/projects/web/repositories/generic-local/files/releases',
    ]);
    expect(trail.querySelector('[aria-current="page"]')?.textContent?.trim()).toBe('1.0');
    expect(names(root)).toEqual(['app.zip', 'app.zip.sha256']);
    await expectAccessible(root);
  });

  it('R-UI-010 a list writes when a file changed relative to now, with the absolute time as its title', async () => {
    const { root } = await open('generic-local', undefined);
    const time = rows(root)[1]!.querySelector('time')!;
    expect(time.getAttribute('datetime')).toBe('2026-10-28T09:14:03Z');
    expect(time.textContent).toMatch(/ago|last|yesterday/);
    expect(time.textContent).not.toMatch(/\d:\d\d:\d\d/);
    expect(time.getAttribute('title')).toMatch(/2026/);
  });

  it('R-UI-010 a folder with 450 entries shows the first 200, Load more loads the next, and the filter says it searches only what is loaded', async () => {
    const { root, fixture, files } = await open('big', undefined, (f, catalog) => {
      catalog.repositories = [...catalog.repositories, { key: 'big', project: 'web', class: 'local', format: 'generic' } as never];
      const nodes: FakeNode[] = Array.from({ length: 450 }, (_, i) => ({ path: `f${String(i).padStart(3, '0')}.bin`, kind: 'file', size: 1 }));
      f.tree.set('web/big', nodes);
    });
    expect(rows(root)).toHaveLength(200);
    button(root, 'Load more').click();
    await settle(fixture);
    expect(rows(root)).toHaveLength(400);
    expect(files.childCalls.at(-1)).toMatchObject({ cursor: '200', limit: 200 });
    type(root.querySelector<HTMLInputElement>('input[type="search"]')!, 'f44');
    await settle(fixture);
    expect(names(root)).toEqual([]);
    expect(text(root)).toContain('The filter searches the 400 entries loaded so far. Load more to search the rest.');
  });

  it('R-UI-011 R-UI-005 the reader sees the list and nothing else; an Owner is offered upload, new folder and the delete of the folder', async () => {
    const reader = await open('generic-local', 'releases', undefined, (c) => c.action === 'read');
    expect(text(reader.root.querySelector('.files-toolbar')!)).not.toMatch(/Upload|New folder|Delete/);
    const owner = await open('generic-local', 'releases');
    const actions = text(owner.root.querySelector('.files-actions')!);
    expect(actions).toContain('Upload files');
    expect(actions).toContain('New folder');
    // Deleting a folder is rare: it is in the folder's menu, not a button beside the upload (Q-UI-512).
    expect(actions).not.toContain('Delete folder');
    owner.root.querySelector<HTMLButtonElement>('button[aria-label="More actions for this folder"]')!.click();
    await settle(owner.fixture);
    expect(text(owner.root.ownerDocument.body.querySelector('.menu')!)).toContain('Delete folder');
    await expectAccessible(owner.root);
  });

  it('R-UI-011 the root of a repository cannot be deleted, and a disabled repository offers no operation', async () => {
    const rootView = await open('generic-local', undefined);
    expect(rootView.root.querySelector('button[aria-label="More actions for this folder"]')).toBeNull();
    const disabled = await open('generic-local', undefined, (_, catalog) => {
      catalog.repositories = catalog.repositories.map((r) => (r.key === 'generic-local' ? { ...r, disabled: true } : r));
    });
    expect(disabled.root.querySelector('.files-actions')).toBeNull();
  });

  it('R-UI-012 uploads into the folder of the page with the SHA-256 in Repr-Digest, create-only unless Replace is ticked; a file above the limit is refused first; the list shows the new file', async () => {
    const { root, fixture, files } = await open('generic-local', 'releases');
    button(root, /Upload files/).click();
    await settle(fixture);
    const body = root.ownerDocument.body;
    expect(body.querySelector<HTMLInputElement>('#upload-folder')?.value).toBe('releases');
    const small = new File(['hello'], 'hello.txt', { type: 'text/plain' });
    const huge = new File(['x'], 'huge.bin');
    Object.defineProperty(huge, 'size', { value: UPLOAD_LIMIT_BYTES + 1 });
    const drop = new Event('drop', { bubbles: true, cancelable: true });
    Object.defineProperty(drop, 'dataTransfer', { value: { files: [small, huge] } });
    body.querySelector('.drop')!.dispatchEvent(drop);
    await settle(fixture);
    expect(text(body)).toContain('Larger than 100 MiB');
    await expectAccessible(body.querySelector('[role="dialog"]')!);
    button(body, 'Upload').click();
    await sleep(fixture);
    await sleep(fixture);
    expect(files.uploads).toHaveLength(1);
    expect(files.uploads[0]).toMatchObject({ repository: 'web/generic-local', path: 'releases/hello.txt', overwrite: false });
    expect(files.uploads[0]?.reprDigest).toMatch(/^sha-256=:[A-Za-z0-9+/=]+:$/);
    button(body, 'Close').click();
    await settle(fixture);
    expect(names(root)).toContain('hello.txt');
  });

  it('R-UI-012 a refused upload names the reason and the file stays in the dialog as failed', async () => {
    const { root, fixture } = await open('generic-local', 'releases', (f) => {
      f.uploadError = new ApiError(403, { code: 'not_admitted', detail: 'no' }, null);
    });
    button(root, /Upload files/).click();
    await settle(fixture);
    const body = root.ownerDocument.body;
    const drop = new Event('drop', { bubbles: true, cancelable: true });
    Object.defineProperty(drop, 'dataTransfer', { value: { files: [new File(['a'], 'a.bin')] } });
    body.querySelector('.drop')!.dispatchEvent(drop);
    await settle(fixture);
    button(body, 'Upload').click();
    await sleep(fixture);
    await sleep(fixture);
    expect(text(body)).toContain('do not admit this path');
  });

  it('R-UI-011 New folder creates a folder below the one of the page and the list shows it', async () => {
    const { root, fixture, files } = await open('generic-local', 'releases');
    button(root, /New folder/).click();
    await settle(fixture);
    const body = root.ownerDocument.body;
    type(body.querySelector<HTMLInputElement>('#folder-name')!, '2.0');
    await settle(fixture);
    button(body, 'Create folder').click();
    await sleep(fixture);
    expect(files.folders).toEqual(['web/generic-local/releases/2.0']);
    expect(names(root)).toContain('2.0');
  });

  it('R-UI-011 deleting a folder of more than 100 items asks for its name, runs as a job whose progress the folder above shows', async () => {
    const { root, fixture, files, router } = await open('generic-local', 'releases', (f) => {
      f.countResult = { count: 101, capped: true };
      f.deleteResult = { done: false, job: { id: 'job-del', kind: 'items-delete', state: 'queued', href: '/api/v1/system/jobs/job-del' } };
    });
    root.querySelector<HTMLButtonElement>('button[aria-label="More actions for this folder"]')!.click();
    await settle(fixture);
    const body = root.ownerDocument.body;
    button(body, /Delete folder/).click();
    await settle(fixture);
    expect(text(body)).toContain('more than 100 items');
    expect(confirmButton(body).disabled).toBe(true);
    type(body.querySelector<HTMLInputElement>('#confirm-name')!, 'releases');
    await settle(fixture);
    confirmButton(body).click();
    await settle(fixture);
    expect(files.deleted).toEqual(['web/generic-local/releases']);
    expect(router.navigate).toHaveBeenCalledWith(['/projects', 'web', 'repositories', 'generic-local']);
    expect(text(root)).toContain('Deleting');
  });

  it('R-UI-011 deleting a file asks naming its path and leads to its folder', async () => {
    const { root, fixture, files, router } = await open('generic-local', 'releases/1.0/app.zip');
    button(root, /^\s*delete\s*Delete\s*$/).click();
    await settle(fixture);
    const body = root.ownerDocument.body;
    expect(text(body)).toContain('Delete releases/1.0/app.zip?');
    expect(body.querySelector('#confirm-name')).toBeNull();
    confirmButton(body).click();
    await settle(fixture);
    expect(files.deleted).toEqual(['web/generic-local/releases/1.0/app.zip']);
    expect(router.navigate).toHaveBeenCalledWith(['/projects', 'web', 'repositories', 'generic-local', 'files', 'releases', '1.0']);
  });

  it('R-UI-010 a file shows its name once, as the last segment of its path with its icon, Download through the content route, its download address and its facts; the checksums, the statistics and the API are in Details', async () => {
    const { root, fixture } = await open('generic-local', 'releases/1.0/app.zip');
    const here = root.querySelector('nav[aria-label="Folder path"] .here-name')!;
    expect(here.querySelector('.entry-icon')?.textContent).toBe('description');
    expect(here.lastElementChild?.textContent).toBe('app.zip');
    // The section's heading is for assistive technology only: the name is not shown twice.
    expect(root.querySelector('#file-name')?.classList.contains('visually-hidden')).toBe(true);
    const download = [...root.querySelectorAll('a')].find((a) => a.textContent?.includes('Download'))!;
    expect(download.getAttribute('href')).toBe('/api/v1/projects/web/repositories/generic-local/content/releases/1.0/app.zip');
    // Download in the browser is the main act; its chevron holds the address and the commands (R-GENERIC-017, Q-UI-1330, Q-UI-1331).
    expect(download.closest('quader-client-command-menu')).not.toBeNull();
    expect(root.querySelector('.file-address')).toBeNull();
    document.body.appendChild(root);
    const chevron = root.querySelector<HTMLButtonElement>('.file-actions button[aria-haspopup="dialog"]')!;
    expect(chevron.getAttribute('aria-label')).toBe('Download with a client');
    chevron.click();
    await settle(fixture);
    const panel = document.getElementById(chevron.getAttribute('aria-controls')!)!;
    expect(panel.querySelector<HTMLInputElement>('.command-address input')?.value).toBe(
      'https://quader.example.net/files/web/generic-local/releases/1.0/app.zip',
    );
    expect(text(panel)).toContain('Check the download');
    chevron.click();
    await settle(fixture);
    const facts = text(root.querySelector('.file-facts')!);
    expect(facts).toContain('2.0 KiB');
    expect(facts).toContain('SHA-256');
    // Q-UI-1004: who created and changed it is named as a person, not in a principal's syntax.
    expect(text(root.querySelector('.file-view dl.facts')!)).toContain('by alice');
    expect(text(root.querySelector('.file-view dl.facts')!)).not.toContain('user:alice');
    const details = root.querySelector<HTMLElement>('quader-disclosure.details')!;
    details.querySelector<HTMLButtonElement>('button[aria-expanded]')!.click();
    await settle(fixture);
    expect(text(details)).toContain('SHA-1');
    expect(text(details)).toContain('CI job of acme/web');
    // The request of the item's description stays; the download's commands are the panel's.
    const commands = [...details.querySelectorAll('pre.code-block')].map((e) => e.textContent ?? '').filter((b) => b.startsWith('quader_curl'));
    expect(commands).toHaveLength(1);
    // The function hands curl the token on standard input: no command names it (Q-UI-1336).
    for (const c of commands) expect(c).not.toContain('QUADER_TOKEN');
    // The media type in a word a person knows, the type beside it (Q-UI-1333).
    expect(text(root.querySelector('.file-facts .media-type')!)).toBe('ZIP archive application/zip');
    await expectAccessible(root);
    root.remove();
  });

  it('R-UI-010 R-UI-193 a file of a disabled repository offers no Download, no download address and no download with curl, and says why once; so does a reader\'s view', async () => {
    for (const consumer of [false, true]) {
      const { root, fixture } = await open('generic-local', 'releases/1.0/app.zip', (_, catalog) => {
        catalog.repositories = catalog.repositories.map((r) => (r.key === 'generic-local' ? { ...r, disabled: true } : r));
        catalog.consumerOnly = consumer;
      });
      expect([...root.querySelectorAll('a')].some((a) => a.textContent?.includes('Download'))).toBe(false);
      expect(root.querySelector('.file-address')).toBeNull();
      expect(text(root.querySelector('[data-testid="file-disabled"]')!)).toBe(
        'The repository is disabled: nobody can download this file until it is enabled again.',
      );
      // The header offers no Set up client either, to the reader too (R-UI-052).
      expect([...root.querySelectorAll('a')].some((a) => a.textContent?.trim() === 'Set up client')).toBe(false);
      const details = root.querySelector<HTMLElement>('quader-disclosure.details')!;
      details.querySelector<HTMLButtonElement>('button[aria-expanded]')!.click();
      await settle(fixture);
      const commands = [...details.querySelectorAll('pre.code-block')].map((e) => e.textContent ?? '').filter((b) => b.startsWith('quader_curl'));
      // The item's own request stays: the management API answers it (Q-UI-898).
      expect(commands).toHaveLength(1);
      expect(commands[0]).not.toContain('-O');
    }
  });

  it('R-UI-010 R-UI-193 a folder of a disabled repository offers no file address to copy, and has no column for it', async () => {
    const { root } = await open('generic-local', undefined, (_, catalog) => {
      catalog.repositories = catalog.repositories.map((r) => (r.key === 'generic-local' ? { ...r, disabled: true } : r));
    });
    expect(root.querySelector('quader-data-table tbody quader-copy-button')).toBeNull();
    expect(root.querySelector('quader-data-table td.actions')).toBeNull();
  });

  it('R-UI-011 a reader of a file may download and copy, and is offered no Delete', async () => {
    const { root } = await open('generic-local', 'readme.txt', undefined, (c) => c.action === 'read');
    expect(text(root.querySelector('.file-actions')!)).toContain('Download');
    expect(text(root.querySelector('.file-actions')!)).not.toContain('Delete');
  });

  it('R-MAL-024 a file the malware scan refuses offers no Download, and says why; one that waits keeps it', async () => {
    const refused = await open('generic-local', 'readme.txt', (files) => {
      files.tree.set('web/generic-local', [
        { path: 'readme.txt', kind: 'file', size: 120, malware: malwareStatus({ state: 'infected', signature: 'Eicar-Test-Signature', refused: true }) },
      ]);
    });
    expect(text(refused.root.querySelector('.file-actions')!)).not.toContain('Download');
    expect(text(refused.root.querySelector('[data-testid="malware-fact"]')!)).toContain('Downloads are refused.');
    const pending = await open('generic-local', 'readme.txt', (files) => {
      files.tree.set('web/generic-local', [{ path: 'readme.txt', kind: 'file', size: 120, malware: malwareStatus({ state: 'pending' }) }]);
    });
    expect(text(pending.root.querySelector('.file-actions')!)).toContain('Download');
  });

  it('R-UI-052 a package format keeps its packages as the page and offers its raw files under Files, with a way back; it takes no upload', async () => {
    const page = await open('npm-local', undefined);
    page.root.querySelector<HTMLButtonElement>('button[aria-label="More actions"]')!.click();
    await settle(page.fixture);
    const files = [...page.root.ownerDocument.body.querySelectorAll('.menu a')].find((a) => a.textContent?.trim() === 'Files');
    expect(files?.getAttribute('href')).toBe('/projects/web/repositories/npm-local/files');

    const raw = await open('npm-local', '');
    expect(text(raw.root)).toContain('The raw files of the repository, as Quader stores them.');
    expect(raw.root.querySelector('.back-link')?.getAttribute('href')).toBe('/projects/web/repositories/npm-local');
    expect(names(raw.root)).toEqual(['@team-a']);
    expect(raw.root.querySelector('.files-actions')).toBeNull();
    await expectAccessible(raw.root);

    const tarball = await open('npm-local', '@team-a/ui/-/ui-1.4.0.tgz');
    expect(tarball.root.querySelector('#file-name')?.textContent).toContain('ui-1.4.0.tgz');
    expect(text(tarball.root.querySelector('.file-actions')!)).toContain('Delete');
  });

  it('R-UI-010 an unknown path shows the not-found error', async () => {
    const { root } = await open('generic-local', 'no/such/file.txt');
    expect(root.querySelector('quader-error-notice')).not.toBeNull();
    await expectAccessible(root);
  });

  it('R-UI-052 R-UI-144 an empty folder says so; an empty repository points who may upload at the one Upload files, and tells anyone else who fills it', async () => {
    const emptyFolder = await open('generic-local', 'releases', (f) => {
      f.tree.set('web/generic-local', [{ path: 'releases', kind: 'folder' }]);
    });
    expect(text(emptyFolder.root.querySelector('quader-empty-state')!)).toContain('This folder is empty.');
    const owner = await open('generic-local', undefined, (f) => f.tree.set('web/generic-local', []));
    expect(text(owner.root.querySelector('quader-empty-state')!)).toContain('Upload files here or with your client.');
    expect(owner.root.querySelectorAll('button')).toSatisfy((all: NodeListOf<HTMLButtonElement>) =>
      [...all].filter((b) => b.textContent?.includes('Upload files')).length === 1,
    );
    const reader = await open('generic-local', undefined, (f) => f.tree.set('web/generic-local', []), (c) => c.action === 'read');
    // Set up client is the header's, once (Q-UI-555): the empty state offers no second one.
    expect(reader.root.querySelector('quader-empty-state :is(a, button)')).toBeNull();
    expect(text(reader.root.querySelector('quader-empty-state')!)).toBe('This repository holds no files yet. Members who may upload to it fill it with their client.');
    expect([...reader.root.querySelectorAll('a, button')].filter((b) => text(b) === 'Set up client')).toHaveLength(1);
  });
});
