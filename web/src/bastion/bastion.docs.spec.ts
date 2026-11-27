import { provideHttpClient, withInterceptors } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { Injectable, computed, signal } from '@angular/core';
import { TestBed } from '@angular/core/testing';
import { provideRouter } from '@angular/router';
import { PackageReads } from '../app/api/package-reads';
import { PackagesApi } from '../app/api/packages-api';
import { ApiError } from '../app/api/problem';
import { DOCS_SEARCH_FIXTURE } from '../app/api/testing/docs-search-fixture';
import { FakePackagesApi, ansibleDocsFixture, ansibleFixture } from '../app/api/testing/fakes-packages';
import { FormatCatalog } from '../app/core/format-names';
import { SIGNED_IN } from '../app/core/signed-in';
import { SystemClients } from '../app/core/system-clients';
import { renderScreen, settle, text } from '../app/testing/screen';
import { bastionDataInterceptor } from './data/http';
import { BastionFormatCatalog, BastionSystemClients, provideBastionReads } from './data/reads';
import { BastionState, BastionStateStore } from './data/state';
import { DocumentationHits } from './pages/documentation-hits';
import { VersionPage } from './pages/package-pages';

// The documentation at a Bastion (bastion.md R-BAS-124 to R-BAS-126, api-and-ui.md R-UI-232): a version's search narrows the catalogue's
// tree in the browser, a project's search finds the documentation's entries below its packages, and the switch docs off takes the
// documentation's tabs away. The HTTP testing backend and the main installation's in-memory fakes; answers of bytes to a few
// kilobytes; the file runs in about a second.

const ID = '0192f0c4-7a7e-7b51-9b1e-2f0d6c9e1a10';
const turn = (): Promise<void> => new Promise((r) => setTimeout(r, 0));
const STATE: BastionState = { confirmedAt: '2026-11-26T09:00:00Z', staleAfterSeconds: 3600, stale: false, docs: true, timeFormat: 'dmy-weekday' };

/** The state store held at one answer. */
@Injectable()
class HeldState {
  readonly answer = signal<BastionState | null>(STATE);
  readonly state = this.answer.asReadonly();
  readonly failure = signal<ApiError | null>(null).asReadonly();
  readonly notReady = computed(() => false);
  readonly off = computed(() => false);
  readonly settled = computed(() => true);
  refresh(): Promise<void> {
    return Promise.resolve();
  }
}

const notFound = { status: 404, statusText: 'Not Found', headers: { 'Content-Type': 'application/problem+json' } };

describe('The documentation at a Bastion (R-UI-232)', () => {
  let backend: HttpTestingController | null = null;
  beforeEach(() => TestBed.resetTestingModule());
  afterEach(() => {
    backend?.verify();
    backend = null;
  });

  it("R-UI-232 a version's search reads the whole tree once and narrows it in the browser by the main installation's rule", async () => {
    TestBed.configureTestingModule({
      providers: [
        provideRouter([]),
        provideHttpClient(withInterceptors([bastionDataInterceptor])),
        provideHttpClientTesting(),
        ...provideBastionReads(),
      ],
    });
    backend = TestBed.inject(HttpTestingController);
    const reads = TestBed.inject(PackageReads);

    const searched = reads.docs(ID, '1.0.0', { q: 'HOST answers', repository: 'it/ansible-local' });
    // The data path holds no search of a version: the tree is asked without the text.
    backend.expectOne(`/ui/data/v1/packages/${ID}/versions/1.0.0/docs`).flush(DOCS_SEARCH_FIXTURE.tree);
    await turn();
    backend.expectOne(`/ui/data/v1/packages/${ID}/versions/1.0.0/state`).flush({ code: 'not_found' }, notFound);
    const tree = await searched;
    expect(tree?.matchCount).toBe(1);
    expect(tree?.groups?.flatMap((g) => g.types.flatMap((t) => t.entries.map((e) => e.name)))).toEqual(['host_check']);
    expect(tree?.groups?.map((g) => g.count)).toEqual([3, 1, 0]);

    // A text the main installation refuses is refused the same way, after the same one read of the tree.
    const short = reads.docs(ID, '1.0.0', { q: 'h' });
    backend.expectOne(`/ui/data/v1/packages/${ID}/versions/1.0.0/docs`).flush(DOCS_SEARCH_FIXTURE.tree);
    await turn();
    for (const r of backend.match(`/ui/data/v1/packages/${ID}/versions/1.0.0/state`)) r.flush({ code: 'not_found' }, notFound);
    await expect(short).rejects.toMatchObject({ status: 422 });
  });

  it("R-UI-232 a project's search finds the documentation's entries below its packages, each opening its page, and only while docs is on", async () => {
    TestBed.configureTestingModule({
      providers: [
        provideRouter([{ path: '**', children: [] }]),
        provideHttpClient(withInterceptors([bastionDataInterceptor])),
        provideHttpClientTesting(),
        { provide: FormatCatalog, useClass: BastionFormatCatalog },
        { provide: BastionStateStore, useClass: HeldState },
      ],
    });
    backend = TestBed.inject(HttpTestingController);
    const fixture = TestBed.createComponent(DocumentationHits);
    fixture.componentRef.setInput('project', 'acme');
    fixture.componentRef.setInput('text', ' host ');
    await settle(fixture);
    const req = backend.expectOne((r) => r.url === '/ui/data/v1/search/documentation');
    expect(req.request.params.get('q')).toBe('host');
    expect(req.request.params.get('project')).toBe('acme');
    req.flush({
      items: [
        {
          format: 'ansible', packageId: 'p1', package: 'acme.tools', version: '2.0.0', project: 'acme', repository: 'galaxy',
          type: 'module', name: 'host_check', shortDescription: 'Checks whether a host answers',
        },
      ],
      nextCursor: null,
    });
    await settle(fixture);
    const root = fixture.nativeElement as HTMLElement;
    expect(text(root.querySelector('h2')!)).toBe('Documentation');
    const link = root.querySelector<HTMLAnchorElement>('th a')!;
    expect(text(link)).toBe('host_check');
    expect(link.getAttribute('href')).toBe('/projects/acme/packages/ansible/acme.tools?doc=module%2Fhost_check');
    expect(text(root.querySelector('th .secondary')!)).toBe('module · Checks whether a host answers');

    // A text that finds no entry shows no section; one character asks nothing.
    fixture.componentRef.setInput('text', 'zz');
    await settle(fixture);
    backend.expectOne((r) => r.url === '/ui/data/v1/search/documentation').flush({ items: [], nextCursor: null });
    await settle(fixture);
    expect(root.querySelector('[data-testid="documentation-hits"]')).toBeNull();
    fixture.componentRef.setInput('text', 'h');
    await settle(fixture);
    backend.expectNone((r) => r.url === '/ui/data/v1/search/documentation');

    // docs off: nothing is asked and nothing is shown.
    (TestBed.inject(BastionStateStore) as unknown as HeldState).answer.set({ ...STATE, docs: false });
    fixture.componentRef.setInput('text', 'host');
    await settle(fixture);
    backend.expectNone((r) => r.url === '/ui/data/v1/search/documentation');
    expect(root.querySelector('[data-testid="documentation-hits"]')).toBeNull();
  });

  describe("a version's page by the switch docs (R-BAS-126)", () => {
    const providers = [
      { provide: FormatCatalog, useClass: BastionFormatCatalog },
      { provide: SystemClients, useClass: BastionSystemClients },
      { provide: SIGNED_IN, useValue: signal(false).asReadonly() },
      { provide: BastionStateStore, useClass: HeldState },
    ];
    const tabs = (root: HTMLElement) => [...root.querySelectorAll('[role="tab"]')].map((t) => text(t));

    async function open(inputs: Record<string, unknown>, docs: boolean, ansible = false) {
      const r = await renderScreen(VersionPage, {
        providers,
        inputs: { project: 'web', ...inputs },
        setup: (get) => {
          (get(BastionStateStore) as unknown as HeldState).answer.set({ ...STATE, docs });
          if (ansible) {
            const fake = get(PackagesApi) as FakePackagesApi;
            ansibleFixture(fake);
            ansibleDocsFixture(fake);
          }
        },
      });
      return r;
    }

    it('R-BAS-126 an npm version opens on its README while docs is on, and has no README tab while it is off', async () => {
      const on = await open({ format: 'npm', name: '@team-a/ui', version: '1.4.0' }, true);
      expect(tabs(on.root)[0]).toBe('README');
      expect(text(on.root.querySelector('[role="tab"][aria-selected="true"]')!)).toBe('README');
      const off = await open({ format: 'npm', name: '@team-a/ui', version: '1.4.0' }, false);
      expect(tabs(off.root)).not.toContain('README');
      expect((off.get(PackagesApi) as FakePackagesApi).readmeCalls).toEqual([]);
    });

    it("R-BAS-126 an Ansible collection's address opens on its documentation while docs is on, and on its facts while it is off", async () => {
      const on = await open({ format: 'ansible', name: 'acme.tools' }, true, true);
      expect(tabs(on.root)[0]).toBe('Documentation');
      expect(tabs(on.root)[1]).toMatch(/^Contents/);
      expect(text(on.root.querySelector('[role="tab"][aria-selected="true"]')!)).toBe('Documentation');
      const off = await open({ format: 'ansible', name: 'acme.tools', doc: 'module/check_host_reachable' }, false, true);
      expect(tabs(off.root).filter((t) => t.startsWith('Documentation') || t.startsWith('Contents'))).toEqual([]);
    });
  });
});
