import german from '../locale/messages.de.xlf' with { loader: 'text' };
import { clearTranslations, loadTranslations } from '@angular/localize';
import { provideHttpClient, withInterceptors } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { provideRouter } from '@angular/router';
import { ClientSnippet } from '../app/api/packages-api';
import { VulnerabilityReads } from '../app/api/vulnerability-reads';
import { FACTORY_TIME_FORMAT, timeFormat } from '../app/shared/time-format';
import { bastionDataInterceptor } from './data/http';
import { provideBastionReads } from './data/reads';
import { BastionStateStore } from './data/state';
import { PackageListPage } from './pages/package-list-page';
import { SetupPage } from './pages/setup-page';
import { StartPage } from './pages/start-page';

// What a visitor of the Bastion met without (bastion.md R-BAS-128, R-BAS-129; api-and-ui.md Q-UI-1467 to Q-UI-1470) against the HTTP
// testing backend: a package's grade per version from its catalogue document, the projects by their names, the installation's form of
// a time from the state, and the page Set up client from the catalogue's set-up documents. Answers of tens of bytes; well under a second.

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

async function settled<T>(fixture: ComponentFixture<T>): Promise<void> {
  fixture.detectChanges();
  await turn();
  await fixture.whenStable();
  fixture.detectChanges();
}

const PROJECTS = {
  items: [
    { key: 'platform', displayName: 'Platform team', description: 'Images and packages the platform team publishes.' },
    { key: 'tools', displayName: 'Tools', description: '' },
  ],
};

/** The German targets of the translation file by unit id, for the units without a placeholder. */
function germanUnits(): Record<string, string> {
  const unit = /<unit id="([^"]+)">[\s\S]*?<target>([^<]*)<\/target>/g;
  return Object.fromEntries([...german.matchAll(unit)].map((m) => [m[1]!, m[2]!.replace(/&apos;/g, "'").replace(/&amp;/g, '&')]));
}

/** A snippet as the catalogue's set-up document carries it (R-API-030). */
function snippet(id: string, tab: ClientSnippet['tab'], content: string): ClientSnippet {
  return { id, tab, language: 'shell', fileName: null, content, marks: [] } as unknown as ClientSnippet;
}

describe("What a Bastion's visitor reads (R-BAS-128, R-BAS-129)", () => {
  afterEach(() => {
    backend?.verify();
    backend = null;
    timeFormat.set(FACTORY_TIME_FORMAT);
  });

  it("R-BAS-128 a package's grade per version is its catalogue document, so the versions' table has its column", async () => {
    const http = setup();
    const report = TestBed.inject(VulnerabilityReads).packageReport(ID);
    const grades = { packageId: ID, versions: [{ version: '1.0.0', grade: { grade: 'C' } }, { version: '0.9.0', grade: { grade: 'S' } }] };
    http.expectOne(`/ui/data/v1/packages/${ID}/vulnerabilities`).flush(grades);
    expect(await report).toEqual(grades);
  });

  it("R-BAS-128 the installation's form of a time from the state is the form every time is written in; one this build does not know keeps it", async () => {
    const http = setup();
    const store = TestBed.inject(BastionStateStore);
    const state = { confirmedAt: '2026-11-26T09:00:00Z', staleAfterSeconds: 3600, stale: false, docs: true };
    let read = store.refresh();
    http.expectOne((r) => r.url === '/ui/data/v1/legal').flush({ imprint: null, privacy: null });
    http.expectOne('/ui/data/v1/state').flush({ ...state, timeFormat: 'iso' });
    await read;
    expect(timeFormat()).toBe('iso');
    read = store.refresh();
    http.expectOne((r) => r.url === '/ui/data/v1/legal').flush({ imprint: null, privacy: null });
    http.expectOne('/ui/data/v1/state').flush({ ...state, timeFormat: 'yyyy' });
    await read;
    expect(timeFormat()).toBe('iso');
  });

  it('R-BAS-128 the start names every project by its display name with its description, its address by its key', async () => {
    const http = setup();
    const fixture = TestBed.createComponent(StartPage);
    fixture.detectChanges();
    http.expectOne('/ui/data/v1/projects').flush(PROJECTS);
    await settled(fixture);
    const root = fixture.nativeElement as HTMLElement;
    const links = [...root.querySelectorAll('.project-list a')];
    expect(links.map((a) => a.textContent?.trim())).toEqual(['Platform team', 'Tools']);
    expect(links[0]?.getAttribute('href')).toBe('/projects/platform/packages');
    expect([...root.querySelectorAll('.project-list .description')].map((p) => p.textContent?.trim())).toEqual([
      'Images and packages the platform team publishes.',
    ]);
  });

  it("R-UI-272 in German, the start says the project Upstream's description in German and a team's own as written", async () => {
    loadTranslations(germanUnits());
    try {
      const http = setup();
      const fixture = TestBed.createComponent(StartPage);
      fixture.detectChanges();
      const upstream = { key: 'upstream', displayName: 'Upstream', description: 'The public registries this installation offers to every project.' };
      http.expectOne('/ui/data/v1/projects').flush({ items: [upstream, ...PROJECTS.items] });
      await settled(fixture);
      const descriptions = [...(fixture.nativeElement as HTMLElement).querySelectorAll('.project-list .description')];
      expect(descriptions.map((p) => p.textContent?.trim())).toEqual([
        'Die öffentlichen Registrys, die diese Installation jedem Projekt anbietet.',
        'Images and packages the platform team publishes.',
      ]);
      // The product's sentence is the reader's language; a team's text is left to the browser as written.
      expect(descriptions.map((p) => p.getAttribute('translate'))).toEqual([null, 'no']);
    } finally {
      clearTranslations();
    }
  });

  it("R-BAS-128 a project's packages are headed by its name and description", async () => {
    const http = setup();
    const fixture = TestBed.createComponent(PackageListPage);
    fixture.componentRef.setInput('project', 'platform');
    fixture.detectChanges();
    http.expectOne('/ui/data/v1/files').flush({ items: [] });
    http.expectOne((r) => r.url === '/ui/data/v1/packages').flush({ items: [], nextCursor: null });
    http.expectOne('/ui/data/v1/projects').flush(PROJECTS);
    await settled(fixture);
    const root = fixture.nativeElement as HTMLElement;
    expect(root.querySelector('h1')?.textContent?.trim()).toBe('Platform team');
    expect(root.querySelector('quader-page-header')?.textContent).toContain('Images and packages the platform team publishes.');
  });

  it("R-BAS-129 Set up client of a generic repository: its clients, the chosen one's set-up from the catalogue with the function its command calls, no credential and no publish", async () => {
    const http = setup();
    const fixture = TestBed.createComponent(SetupPage);
    fixture.componentRef.setInput('project', 'platform');
    fixture.componentRef.setInput('key', 'downloads');
    fixture.componentRef.setInput('client', 'wget');
    fixture.detectChanges();
    http.expectOne('/ui/data/v1/repositories').flush({
      items: [{ name: 'platform/downloads', format: 'generic', class: 'local' }, { name: 'platform/npm', format: 'npm', class: 'local' }],
    });
    // The formats are the Bastion's own table (a promise), then the set-up of the chosen client is asked.
    for (let i = 0; i < 3; i++) {
      await turn();
      fixture.detectChanges();
    }
    http.expectOne('/ui/data/v1/setup/platform/downloads/wget').flush({
      repository: 'platform/downloads',
      format: 'generic',
      client: 'wget',
      credentials: 'none',
      registryUrl: 'https://bastion.example.org/files/platform/downloads/',
      snippets: [
        snippet(
          'wget.configure.function',
          'configure',
          'quader_wget() {\n  wget --no-verbose --no-config "$@"\n}\n',
        ),
        snippet('wget.install', 'install', 'quader_wget --output-document agent.tar.gz "https://bastion.example.org/files/platform/downloads/agent.tar.gz"\n'),
      ],
    });
    await settled(fixture);
    const root = fixture.nativeElement as HTMLElement;
    expect(root.querySelector('h1')?.textContent?.trim()).toBe('Set up client');
    expect([...root.querySelectorAll<HTMLOptionElement>('#setup-client option')].map((o) => o.value)).toEqual(['curl', 'wget', 'powershell']);
    expect([...root.querySelectorAll('.step h2')].map((h) => h.textContent?.replace(/\d/g, '').trim())).toEqual(['Configure', 'Install']);
    const text = root.textContent ?? '';
    expect(text).toContain('wget --no-verbose --no-config');
    expect(text).toContain('Everything here is public: the snippets need no credentials.');
    // The Bastion's visitor is anonymous: the caption says the function sends no credential (R-UI-123).
    expect(text).toContain('The function sends no credential: this repository can be read without signing in.');
    expect(text).not.toContain("The token stays off wget's command line.");
    expect(text).not.toContain('Create a token');
    expect(text.toLowerCase()).not.toContain('main installation');
    expect(root.querySelector('.repo a')?.getAttribute('href')).toBe('/projects/platform/repositories/downloads');
  });

  it('R-BAS-129 R-OCI-030 Set up client of a public image repository starts at Install: a pull needs no login, and a Bastion takes no push', async () => {
    const http = setup();
    const fixture = TestBed.createComponent(SetupPage);
    fixture.componentRef.setInput('project', 'platform');
    fixture.componentRef.setInput('key', 'images');
    fixture.componentRef.setInput('client', 'docker');
    fixture.detectChanges();
    http.expectOne('/ui/data/v1/repositories').flush({ items: [{ name: 'platform/images', format: 'oci', class: 'local' }] });
    for (let i = 0; i < 3; i++) {
      await turn();
      fixture.detectChanges();
    }
    // The catalogue's set-up document as the server renders it now (formats/oci.md Q-OCI-277): no configure snippet.
    http.expectOne('/ui/data/v1/setup/platform/images/docker').flush({
      repository: 'platform/images',
      format: 'oci',
      client: 'docker',
      credentials: 'none',
      registryUrl: 'https://bastion.example.org/v2/platform/images/',
      snippets: [snippet('docker.install', 'install', 'docker pull bastion.example.org/platform/images/myapp:1.0\n')],
    });
    await settled(fixture);
    const root = fixture.nativeElement as HTMLElement;
    expect([...root.querySelectorAll('.step h2')].map((h) => h.textContent?.trim())).toEqual(['1Install']);
    expect(root.querySelector('section[aria-labelledby="step-configure"]')).toBeNull();
    expect(root.textContent).not.toContain('login');
  });

  it("R-UI-272 Set up client names the clients by the application's table, as a version's commands do, not by the catalogue's names", async () => {
    const http = setup();
    const fixture = TestBed.createComponent(SetupPage);
    fixture.componentRef.setInput('project', 'platform');
    fixture.componentRef.setInput('key', 'nuget');
    fixture.componentRef.setInput('client', 'nuget');
    fixture.detectChanges();
    http.expectOne('/ui/data/v1/repositories').flush({ items: [{ name: 'platform/nuget', format: 'nuget', class: 'local' }] });
    for (let i = 0; i < 3; i++) {
      await turn();
      fixture.detectChanges();
    }
    http.expectOne('/ui/data/v1/setup/platform/nuget/nuget').flush({
      repository: 'platform/nuget',
      format: 'nuget',
      client: 'nuget',
      credentials: 'none',
      registryUrl: 'https://bastion.example.org/api/nuget/platform/nuget/v3/index.json',
      snippets: [snippet('nuget.install', 'install', 'nuget install Acme.Core -Source quader\n')],
    });
    await settled(fixture);
    const options = [...(fixture.nativeElement as HTMLElement).querySelectorAll<HTMLOptionElement>('#setup-client option')];
    expect(options.map((o) => o.textContent?.trim())).toEqual(['.NET CLI (dotnet)', 'NuGet CLI (nuget.exe)', 'Visual Studio']);
  });

  it('R-BAS-129 Set up client of a repository the Bastion does not offer says so and asks for no set-up', async () => {
    const http = setup();
    const fixture = TestBed.createComponent(SetupPage);
    fixture.componentRef.setInput('project', 'platform');
    fixture.componentRef.setInput('key', 'internal');
    fixture.detectChanges();
    http.expectOne('/ui/data/v1/repositories').flush({ items: [{ name: 'platform/npm', format: 'npm', class: 'local' }] });
    await settled(fixture);
    expect((fixture.nativeElement as HTMLElement).querySelector('[data-testid="setup-unknown"]')?.textContent).toContain(
      'This address offers no repository of that name.',
    );
  });
});
