import { HttpTestingController, TestRequest, provideHttpClientTesting } from '@angular/common/http/testing';
import { TestBed } from '@angular/core/testing';
import { Router } from '@angular/router';
import { RouterTestingHarness } from '@angular/router/testing';
import { REPOSITORIES_SHOWN } from '../app/core/repositories-shown';
import { bastionConfig } from './app.config';
import { BastionTrail } from './data/trail';

// A Bastion's project view (api-and-ui.md R-UI-273, R-UI-274; W-40's D1 to D5, D14, D20) through the application's own routes and
// providers against the HTTP testing backend: a project opens on its repositories, a repository by its format, the way above each page,
// and what the switch Hide repositories leaves. Answers of tens of bytes; the file runs in about a second.

const turn = (): Promise<void> => new Promise((r) => setTimeout(r, 0));
const ID = '0192f0c4-7a7e-7b51-9b1e-2f0d6c9e1a10';
const NOT_FOUND = Symbol('404');

const PROJECTS = {
  items: [
    { key: 'platform', displayName: 'Platform team', description: 'Images and packages the platform team publishes.' },
    { key: 'upstream', displayName: 'Upstream', description: 'The public registries this installation offers to every project.' },
  ],
};
const REPOSITORIES = {
  items: [
    { name: 'platform/images', format: 'oci', class: 'local', description: '' },
    { name: 'platform/downloads', format: 'generic', class: 'local', description: '' },
    { name: 'platform/npm', format: 'npm', class: 'remote', description: 'A cache of the npm packages the teams use.' },
    { name: 'upstream/clamav', format: 'clamav', class: 'local', description: 'ClamAV definitions' },
  ],
};
const AGENT = {
  id: ID,
  name: 'tools/agent',
  format: 'oci',
  description: '',
  latestVersion: '3.0',
  versionCount: 2,
  updatedAt: '2026-11-26T08:00:00Z',
  createdAt: '2026-11-20T08:00:00Z',
};
const TAG = { prerelease: false, publishedAt: '2026-11-26T08:00:00Z', sizeBytes: 2048, sizePartial: false };

/** The state of a Bastion, with the switch Hide repositories as given. */
function state(hidden: boolean): object {
  return { confirmedAt: '2026-11-26T09:00:00Z', staleAfterSeconds: 3600, stale: false, docs: true, hideRepositories: hidden, timeFormat: 'dmy-weekday' };
}

/** The data path as a Bastion answers it: every read the pages make, by its path below the data root; anything else fails the test. */
function catalogue(hidden: boolean, projects: object = PROJECTS): Record<string, unknown> {
  const single = (name: string): unknown => {
    const r = REPOSITORIES.items.find((x) => x.name === name);
    if (!r) return NOT_FOUND;
    return hidden ? { name: r.name, format: r.format } : r;
  };
  return {
    state: state(hidden),
    legal: { imprint: null, privacy: null },
    projects,
    repositories: hidden ? NOT_FOUND : REPOSITORIES,
    'repositories/platform/images': single('platform/images'),
    'repositories/platform/downloads': single('platform/downloads'),
    'repositories/upstream/clamav': single('upstream/clamav'),
    files: { items: ['platform/downloads'] },
    'files/platform/downloads/folder': { items: [{ path: 'tools', kind: 'folder' }] },
    packages: { items: [AGENT], nextCursor: null },
    [`packages/${ID}/images`]: {
      items: [
        { ...TAG, version: '3.0', aliases: ['latest'], digest: `sha256:${'a'.repeat(64)}`, repositories: ['platform/images'] },
        { ...TAG, version: '2.1', digest: `sha256:${'b'.repeat(64)}`, repositories: ['platform/images'] },
      ],
    },
    [`packages/${ID}/vulnerabilities`]: { packageId: ID, versions: [] },
    'malware/signatures': NOT_FOUND,
    'vulnerabilities/database': NOT_FOUND,
  };
}

let backend: HttpTestingController | null = null;

function setup(): HttpTestingController {
  TestBed.configureTestingModule({ providers: [...bastionConfig.providers, provideHttpClientTesting()] });
  backend = TestBed.inject(HttpTestingController);
  return backend;
}

/** Answers every open read from `answers` until none is left; the reads the pages ask are recorded. */
async function serve(http: HttpTestingController, answers: Record<string, unknown>, asked: string[] = []): Promise<string[]> {
  for (let round = 0; round < 12; round++) {
    await turn();
    const open: TestRequest[] = http.match(() => true);
    for (const req of open) {
      const path = req.request.url.replace('/ui/data/v1/', '');
      if (!(path in answers)) throw new Error(`the test does not answer ${req.request.urlWithParams}`);
      asked.push(req.request.urlWithParams.replace('/ui/data/v1/', ''));
      const answer = answers[path];
      if (answer === NOT_FOUND) {
        req.flush(
          { type: 'about:blank', title: 'Not Found', status: 404, code: 'not_found' },
          { status: 404, statusText: 'Not Found', headers: { 'Content-Type': 'application/problem+json' } },
        );
      } else {
        req.flush(answer as object);
      }
    }
  }
  return asked;
}

/** Opens an address of the application and answers its reads; the harness and the reads it made. */
async function open(harness: RouterTestingHarness, http: HttpTestingController, url: string, answers: Record<string, unknown>) {
  const asked: string[] = [];
  const navigation = harness.navigateByUrl(url);
  await serve(http, answers, asked);
  await navigation;
  harness.detectChanges();
  await serve(http, answers, asked);
  harness.detectChanges();
  return { root: harness.routeNativeElement as HTMLElement, asked };
}

/** The words of each element the selector finds, without the icons' ligatures. */
const texts = (root: HTMLElement, selector: string): string[] =>
  [...root.querySelectorAll(selector)].map((e) => {
    const copy = e.cloneNode(true) as HTMLElement;
    copy.querySelectorAll('.material-icons, quader-format-icon').forEach((i) => i.remove());
    return (copy.textContent ?? '').replace(/\s+/g, ' ').trim();
  });
const trail = (): string[] => TestBed.inject(BastionTrail).steps().map((s) => s.label);

describe("A Bastion's project view (R-UI-273, R-UI-274)", () => {
  afterEach(() => {
    backend?.verify();
    backend = null;
  });

  it('R-UI-273 a project opens on its repositories and its packages are the second tab', async () => {
    const http = setup();
    const harness = await RouterTestingHarness.create();
    const answers = catalogue(false);
    const { root } = await open(harness, http, '/projects/platform', answers);
    expect(TestBed.inject(Router).url).toBe('/projects/platform/repositories');
    expect(texts(root, 'nav.project-tabs a')).toEqual(['Repositories', 'Packages']);
    expect(root.querySelector('nav.project-tabs a[aria-current="page"]')?.textContent?.trim()).toBe('Repositories');
    expect(texts(root, 'h1')).toEqual(['Platform team']);
    // The project's repositories, each with its format, its class and its Set up client; another project's are not listed.
    expect(texts(root, 'tbody th a')).toEqual(['images', 'downloads', 'npm']);
    expect(texts(root, 'tbody .format-cell')).toEqual(['OCI (Docker)', 'Generic', 'npm']);
    expect(texts(root, 'tbody .class-cell')).toEqual(['Local', 'Local', 'Remote']);
    expect([...root.querySelectorAll('tbody a.row-setup')].map((a) => a.getAttribute('href'))).toEqual([
      '/projects/platform/repositories/images/setup',
      '/projects/platform/repositories/downloads/setup',
      '/projects/platform/repositories/npm/setup',
    ]);
    expect(texts(root, 'tbody .secondary')).toEqual(['A cache of the npm packages the teams use.']);
    expect(trail()).toEqual(['Projects', 'Platform team']);

    // The filter reads the key and the description, in the browser: no read of the server.
    const filtered = await open(harness, http, '/projects/platform/repositories?q=cache', answers);
    expect(texts(filtered.root, 'tbody th a')).toEqual(['npm']);
    expect(filtered.asked.filter((a) => a.startsWith('repositories'))).toEqual([]);
  });

  it('R-UI-273 a repository lists its images with their tags and a Pull per row, read from its index and the image list by content', async () => {
    const http = setup();
    const harness = await RouterTestingHarness.create();
    const { root, asked } = await open(harness, http, '/projects/platform/repositories/images', catalogue(false));
    expect(texts(root, 'h1')).toEqual(['images']);
    expect(texts(root, 'quader-page-header .badge')).toEqual(['OCI (Docker)', 'Local']);
    expect(root.querySelector('quader-page-header a.btn')?.getAttribute('href')).toBe('/projects/platform/repositories/images/setup');
    // The repository's own index (bastion.md R-BAS-130): the search names the repository.
    expect(asked.some((a) => a.startsWith('packages?') && a.includes('repository=platform/images'))).toBe(true);
    expect(texts(root, 'quader-repository-items tbody th a')).toContain('tools/agent');
    expect(root.querySelector('quader-repository-items tbody quader-client-command-menu')).not.toBeNull();
    expect(texts(root, 'quader-repository-items thead th')).not.toContain('Downloads');
    expect(trail()).toEqual(['Projects', 'Platform team', 'Repositories', 'images']);

    // The row opens the image's newest tags by content, each with its own Pull.
    (root.querySelector('quader-repository-items tbody button[aria-expanded]') as HTMLButtonElement).click();
    harness.detectChanges();
    const opened = await serve(http, catalogue(false));
    harness.detectChanges();
    expect(opened).toContain(`packages/${ID}/images`);
    const tags = root.querySelectorAll('quader-repository-item-versions tbody tr');
    expect(tags).toHaveLength(2);
    expect(root.querySelectorAll('quader-repository-item-versions tbody quader-client-command-menu')).toHaveLength(2);
  });

  it("R-UI-273 a generic repository's address is its files and ClamAV's its databases", async () => {
    const http = setup();
    const harness = await RouterTestingHarness.create();
    const files = await open(harness, http, '/projects/platform/repositories/downloads', catalogue(false));
    expect(files.root.querySelector('quader-bastion-files quader-folder-view')).not.toBeNull();
    expect(texts(files.root, 'h1')).toEqual(['downloads']);
    expect(trail()).toEqual(['Projects', 'Platform team', 'Repositories', 'downloads']);

    const answers = { ...catalogue(false), packages: { items: [], nextCursor: null } };
    const clamav = await open(harness, http, '/projects/upstream/repositories/clamav', answers);
    expect(clamav.root.querySelector('quader-clamav-databases')).not.toBeNull();
    expect(texts(clamav.root, 'quader-page-header .badge')).toEqual(['ClamAV definitions', 'System']);
    expect(clamav.asked.some((a) => a.startsWith('packages?') && a.includes('repository=upstream/clamav'))).toBe(true);
  });

  it('R-UI-274 hidden: the project opens on its packages, a repository address is not found but a generic one, the trail has no Repositories', async () => {
    const http = setup();
    const harness = await RouterTestingHarness.create();
    const answers = { ...catalogue(true), packages: { items: [], nextCursor: null } };
    const { root } = await open(harness, http, '/projects/platform', answers);
    expect(TestBed.inject(Router).url).toBe('/projects/platform/packages');
    expect(root.querySelector('nav.project-tabs')).toBeNull();
    expect(TestBed.inject(REPOSITORIES_SHOWN)()).toBe(false);

    const list = await open(harness, http, '/projects/platform/repositories', answers);
    expect(texts(list.root, 'h1')).toEqual(['Page not found']);
    const images = await open(harness, http, '/projects/platform/repositories/images', answers);
    expect(texts(images.root, 'h1')).toEqual(['Page not found']);

    // A generic repository's files keep their root, whose folder is the key every download address has; no class, no step Repositories.
    const files = await open(harness, http, '/projects/platform/repositories/downloads', answers);
    expect(files.root.querySelector('quader-folder-view')).not.toBeNull();
    expect(texts(files.root, 'quader-page-header .badge')).toEqual(['Generic']);
    expect(trail()).toEqual(['Projects', 'Platform team', 'downloads']);
    const setup$ = await open(harness, http, '/projects/platform/repositories/downloads/setup?client=curl', {
      ...answers,
      'setup/platform/downloads/curl': { client: 'curl', snippets: [] },
    });
    expect(setup$.root.querySelector('.repo')).toBeNull();
    expect(trail()).toEqual(['Projects', 'Platform team', 'Set up client']);
  });

  it('R-UI-273 the start lists Upstream first and counts the repositories; a Bastion that shows nothing says so', async () => {
    const http = setup();
    const harness = await RouterTestingHarness.create();
    const { root } = await open(harness, http, '/', catalogue(false));
    expect(texts(root, 'tbody th a')).toEqual(['Upstream', 'Platform team']);
    expect(texts(root, 'thead th')).toEqual(['Project', 'Description', 'Repositories']);
    expect(texts(root, 'tbody td.num')).toEqual(['1', '3']);
  });

  it('R-UI-273 a Bastion that shows no project yet says so on its start', async () => {
    const http = setup();
    const harness = await RouterTestingHarness.create();
    const { root } = await open(harness, http, '/', catalogue(false, { items: [] }));
    expect(root.textContent).toContain('Nothing is shown here yet.');
  });
});
