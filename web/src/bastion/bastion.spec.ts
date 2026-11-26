import { provideHttpClient, withInterceptors } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { Type } from '@angular/core';
import { TestBed } from '@angular/core/testing';
import { provideRouter } from '@angular/router';
import { ClientSetupReads, PackageReads } from '../app/api/package-reads';
import { ApiError } from '../app/api/problem';
import { SignatureReads } from '../app/api/signature-reads';
import { VulnerabilityReads } from '../app/api/vulnerability-reads';
import {
  ANSIBLE_FORMAT,
  CLAMAV_DEFINITIONS_FORMAT,
  DEB_FORMAT,
  GENERIC_FORMAT,
  NUGET_FORMAT,
  OCI_FORMAT,
  PACKAGE_FORMAT,
} from '../app/api/testing/fakes-catalog';
import { NamespaceOf } from '../app/views/packages/namespace-of';
import { PackageName } from '../app/views/packages/package-name';
import { BastionApp } from './app';
import { BASTION_FORMATS } from './data/formats';
import { bastionDataInterceptor, seg } from './data/http';
import { provideBastionReads } from './data/reads';

// The Bastion's application (bastion.md R-BAS-116 to R-BAS-118, api-and-ui.md R-UI-227, R-UI-228) against the HTTP testing backend: the
// reads ask the data path with the catalogue's spelling, and the shell shows the state's age, the sentence of a Bastion that is not
// ready and the legal links. Answers of tens of bytes; the file runs in well under a second.

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
const ID = '0192f0c4-7a7e-7b51-9b1e-2f0d6c9e1a10';

describe('The Bastion application (R-UI-228)', () => {
  afterEach(() => {
    backend?.verify();
    backend = null;
  });

  it('R-UI-228 the reads ask the data path in the catalogue spelling and answer in the main installation shapes', async () => {
    const http = setup();
    const reads = TestBed.inject(PackageReads);

    // The times the builder took out of the version's documents come back from its state, by pointer (R-BAS-103, Q-UI-1451): one read of
    // the state serves the version's documents read with it.
    // The pointer of the first file's scan, a JSON pointer and no address (built so, as R-ARCH-007's search reads every literal).
    const scanned = ['', 'files', '0', 'malware', 'scannedAt'].join('/');
    const version = reads.version(ID, '1.0.0+build.1');
    http.expectOne(`/ui/data/v1/packages/${ID}/versions/1.0.0%2Bbuild.1`).flush({ version: '1.0.0+build.1', files: [{ malware: { state: 'clean' } }] });
    await turn();
    http.expectOne(`/ui/data/v1/packages/${ID}/versions/1.0.0%2Bbuild.1/state`).flush({
      times: { version: { [scanned]: '2026-11-26T10:00:00Z', '/gone/at': 'x' }, docs: { '/builtAt': '2026-11-26T09:00:00Z' } },
    });
    const v = await version;
    expect(v.version).toBe('1.0.0+build.1');
    expect(v.files[0]?.malware).toEqual({ state: 'clean', scannedAt: '2026-11-26T10:00:00Z' });
    const tree = reads.docs(ID, '1.0.0+build.1');
    http.expectOne(`/ui/data/v1/packages/${ID}/versions/1.0.0%2Bbuild.1/docs`).flush({ groups: [] });
    expect(await tree).toEqual({ groups: [], builtAt: '2026-11-26T09:00:00Z' });

    // A file of the documentation is one segment per part of its path, as the grammar writes it; a version without a state keeps its
    // documents as they are.
    const file = reads.docFile(ID, '2.0.0', "docs/it's(1).md");
    http.expectOne(`/ui/data/v1/packages/${ID}/versions/2.0.0/docs/files/docs/it%27s%281%29.md`).flush({ html: '<p>x</p>' });
    await turn();
    http.expectOne(`/ui/data/v1/packages/${ID}/versions/2.0.0/state`).flush({ type: 'about:blank', status: 404, code: 'not_found' }, {
      status: 404,
      statusText: 'Not Found',
      headers: { 'Content-Type': 'application/problem+json' },
    });
    expect(await file).toEqual({ html: '<p>x</p>' });

    const lookup = reads.lookup('npm', '@acme/ui');
    const req = http.expectOne((r) => r.url === '/ui/data/v1/packages/lookup');
    expect(req.request.params.get('format')).toBe('npm');
    expect(req.request.params.get('name')).toBe('@acme/ui');
    expect(req.request.method).toBe('GET');
    req.flush({ id: ID, name: '@acme/ui' });
    await lookup;

    const page = reads.versions(ID, { sort: '-version', group: 'content' })(null, 100);
    const vreq = http.expectOne((r) => r.url === `/ui/data/v1/packages/${ID}/versions`);
    expect(vreq.request.params.keys().sort()).toEqual(['limit']);
    vreq.flush({ items: [{ version: '1.0.0' }], nextCursor: null });
    expect((await page).items).toHaveLength(1);

    // A walk of every page with the main installation's 500 asks the Bastion's largest page (Q-UI-1445): its 400 stood in the renders.
    const all = reads.versions(ID)(null, 500);
    const allReq = http.expectOne((r) => r.url === `/ui/data/v1/packages/${ID}/versions`);
    expect(allReq.request.params.get('limit')).toBe('100');
    allReq.flush({ items: [], nextCursor: null });
    await all;

    // A README the catalogue does not hold is none, as the main installation's 404.
    const readme = reads.readme(ID, '1.0.0');
    http.expectOne(`/ui/data/v1/packages/${ID}/versions/1.0.0/readme`).flush({ type: 'about:blank', status: 404, code: 'not_found' }, {
      status: 404,
      statusText: 'Not Found',
      headers: { 'Content-Type': 'application/problem+json' },
    });
    expect(await readme).toBeNull();

    // What the catalogue holds no document for answers as a read the caller may not make, without a request.
    await expect(reads.dependents(ID)).rejects.toBeInstanceOf(ApiError);
    await expect(TestBed.inject(VulnerabilityReads).packageReport(ID)).rejects.toMatchObject({ status: 404 });
    expect(await TestBed.inject(SignatureReads).releases('acme/npm')).toEqual([]);
    expect(seg("a+b!'()*~")).toBe('a%2Bb%21%27%28%29%2A~');
  });

  it("R-UI-228 a version's set-up is found by the package's catalogue ID, which a search of its repository answers", async () => {
    const http = setup();
    const setupReads = TestBed.inject(ClientSetupReads);
    const answer = setupReads.get('acme/npm-local', { client: 'npm', package: '@acme/ui', version: '1.0.0', credentials: 'none' });
    const search = http.expectOne((r) => r.url === '/ui/data/v1/packages');
    expect(search.request.params.get('repository')).toBe('acme/npm-local');
    search.flush({ items: [{ id: 'other', name: '@acme/ui-kit' }, { id: ID, name: '@acme/ui' }], nextCursor: null });
    await turn();
    http.expectOne(`/ui/data/v1/setup/acme/npm-local/npm/${ID}/1.0.0`).flush({ client: 'npm', snippets: [] });
    expect(await answer).toEqual({ client: 'npm', snippets: [] });
  });

  it('R-UI-228 a failed read is the error the notice shows whole: its status, its code and its call', async () => {
    const http = setup();
    const version = TestBed.inject(PackageReads).version(ID, '9.9.9');
    http.expectOne(`/ui/data/v1/packages/${ID}/versions/9.9.9`).flush(
      { type: 'about:blank', title: 'Not Found', status: 404, code: 'not_found', detail: 'Nothing is offered at this address.' },
      { status: 404, statusText: 'Not Found', headers: { 'Content-Type': 'application/problem+json' } },
    );
    const e = await version.catch((x: unknown) => x);
    expect(e).toBeInstanceOf(ApiError);
    expect((e as ApiError).status).toBe(404);
    expect((e as ApiError).problem?.code).toBe('not_found');
    expect((e as ApiError).answer?.call).toEqual({ method: 'GET', path: `/ui/data/v1/packages/${ID}/versions/9.9.9` });
  });

  it('R-UI-228 the format table names every format as the main installation does', () => {
    for (const f of [PACKAGE_FORMAT, NUGET_FORMAT, OCI_FORMAT, DEB_FORMAT, ANSIBLE_FORMAT, GENERIC_FORMAT, CLAMAV_DEFINITIONS_FORMAT]) {
      const ours = BASTION_FORMATS.find((b) => b.name === f.name);
      expect(ours?.displayName, f.name).toBe(f.displayName);
      expect(ours?.systemOnly, f.name).toBe(f.systemOnly);
      // The fakes give NuGet and Debian npm's rule by their spread; the server's formats name a namespace for npm, OCI and Ansible alone.
      if (['npm', 'oci', 'ansible'].includes(f.name)) expect(ours?.namespaces, f.name).toEqual(f.namespaces);
      else expect(ours?.namespaces, f.name).toBeNull();
      expect(ours?.signatureKinds, f.name).toEqual(f.signatureKinds);
    }
  });
});

describe("The Bastion's pages (R-UI-229)", () => {
  afterEach(() => {
    backend?.verify();
    backend = null;
  });

  /** A view's namespace as the Bastion's pages draw it, once the format table is read. */
  async function drawn<T>(component: Type<T>, inputs: Record<string, unknown>): Promise<HTMLElement> {
    const fixture = TestBed.createComponent(component);
    for (const [k, v] of Object.entries(inputs)) fixture.componentRef.setInput(k, v);
    fixture.detectChanges();
    await turn();
    await fixture.whenStable();
    fixture.detectChanges();
    return fixture.nativeElement as HTMLElement;
  }

  it('R-UI-229 a namespace is text at the Bastion, which has no namespace page, and the package keeps its link', async () => {
    setup();
    const head = await drawn(NamespaceOf, { format: 'npm', name: '@team-a/widgets', project: 'platform', links: false });
    expect(head.textContent).toContain('Scope');
    expect(head.textContent).toContain('@team-a');
    expect(head.querySelector('a')).toBeNull();
    const row = await drawn(PackageName, {
      format: 'npm',
      name: '@team-a/widgets',
      project: 'platform',
      link: ['/projects', 'platform', 'packages', 'npm', '@team-a/widgets'],
      namespaceLinks: false,
    });
    expect(row.querySelector('.ns-text')?.textContent).toBe('@team-a');
    expect(row.querySelector('[data-testid=namespace-link]')).toBeNull();
    expect(row.querySelectorAll('a')).toHaveLength(1);
    const main = await drawn(NamespaceOf, { format: 'npm', name: '@team-a/widgets', project: 'platform' });
    expect(main.querySelector('a')?.textContent).toBe('@team-a');
  });
});

describe('The Bastion shell (R-UI-227)', () => {
  afterEach(() => {
    backend?.verify();
    backend = null;
  });

  async function shell(stateAnswer: (req: ReturnType<HttpTestingController['expectOne']>) => void) {
    const http = setup();
    const fixture = TestBed.createComponent(BastionApp);
    fixture.detectChanges();
    http.expectOne((r) => r.url === '/ui/data/v1/legal').flush({ imprint: { html: '<p>Example GmbH</p>', lang: 'de' }, privacy: null });
    stateAnswer(http.expectOne('/ui/data/v1/state'));
    await turn();
    await fixture.whenStable();
    fixture.detectChanges();
    return fixture.nativeElement as HTMLElement;
  }

  it('R-UI-227 an old state shows the band with its time, the footer the imprint and As of, and nothing names another party', async () => {
    const root = await shell((req) =>
      req.flush({ confirmedAt: '2026-11-26T08:00:00Z', staleAfterSeconds: 3600, stale: true, docs: true }),
    );
    expect(root.querySelector('.stale-band')?.textContent).toContain('Newer information is not available right now.');
    const footer = root.querySelector('.bastion-footer')?.textContent ?? '';
    expect(footer).toContain('Imprint');
    expect(footer).not.toContain('Privacy notice');
    expect(footer).toContain('As of');
    expect(root.textContent?.toLowerCase()).not.toContain('main installation');
    expect(root.querySelector('.language-link')?.getAttribute('href')).toBe('/ui/de/');
  });

  it('R-UI-227 a Bastion without a catalogue to show says so in place of the page, and a fresh state shows no band', async () => {
    const notReady = await shell((req) =>
      req.flush(
        { type: 'about:blank', status: 503, code: 'not_ready', detail: 'This page is not available right now. Try again in a few minutes.' },
        { status: 503, statusText: 'Service Unavailable', headers: { 'Content-Type': 'application/problem+json' } },
      ),
    );
    expect(notReady.querySelector('.not-ready')?.textContent).toContain('This page is not available right now. Try again in a few minutes.');
    expect(notReady.querySelector('router-outlet')).toBeNull();
  });

  it('R-UI-227 a not_ready with its Retry-After is said at once, without the retry of a read that would hold the spinner 30 s', async () => {
    const root = await shell((req) =>
      req.flush(
        { type: 'about:blank', status: 503, code: 'not_ready', detail: 'This page is not available right now. Try again in a few minutes.' },
        { status: 503, statusText: 'Service Unavailable', headers: { 'Content-Type': 'application/problem+json', 'Retry-After': '30' } },
      ),
    );
    expect(root.querySelector('.not-ready')).not.toBeNull();
    backend?.expectNone('/ui/data/v1/state');
  });

  it('R-UI-227 a fresh state shows the page and no band', async () => {
    const root = await shell((req) =>
      req.flush({ confirmedAt: new Date().toISOString(), staleAfterSeconds: 3600, stale: false, docs: false }),
    );
    expect(root.querySelector('.stale-band')).toBeNull();
    expect(root.querySelector('router-outlet')).not.toBeNull();
  });
});
