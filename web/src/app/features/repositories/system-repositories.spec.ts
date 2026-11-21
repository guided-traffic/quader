import { UpstreamApi } from '../../api/upstream-api';
import { CLAMAV_ENDPOINT, FakeUpstreamApi } from '../../api/testing/fakes-upstream';
import { TestBed } from '@angular/core/testing';
import { provideRouter, withComponentInputBinding } from '@angular/router';
import { RouterTestingHarness } from '@angular/router/testing';
import { ProjectsApi } from '../../api/projects-api';
import { RepositoriesApi } from '../../api/repositories-api';
import { FakeSessionApi, provideFakeApis } from '../../api/testing/fakes';
import { FakeRepositoriesApi, GENERIC_FORMAT, OCI_FORMAT, PACKAGE_FORMAT } from '../../api/testing/fakes-catalog';
import { FakeProjectsApi, sampleProject } from '../../api/testing/fakes-projects';
import { SessionApi } from '../../api/session-api';
import { SessionStore } from '../../core/session-store';
import { spokenText } from '../../testing/axe';
import { expectAccessible, text } from '../../testing/screen';
import { RepositoryList } from './repository-list';

const internal = { artifacts: 'internal', wiki: 'internal' } as const;
const fixed = { artifacts: 'private', wiki: 'private' } as const;

/**
 * The project upstream as an installation holds it, cut to what the assertions need: remotes of three formats, one of them on, and
 * both system repositories; and a team's repository, which the project's page never lists.
 */
function seed(fake: FakeRepositoriesApi, system = true): void {
  fake.formatsResult = [GENERIC_FORMAT, PACKAGE_FORMAT, OCI_FORMAT];
  fake.repositories = [
    { key: 'debian', project: 'upstream', class: 'remote', format: 'generic', description: 'Debian', disabled: true, visibility: internal },
    { key: 'docker-hub', project: 'upstream', class: 'remote', format: 'oci', description: 'Docker Hub', disabled: true, visibility: internal },
    { key: 'npmjs', project: 'upstream', class: 'remote', format: 'npm', description: 'npm public registry', disabled: false, visibility: internal },
    ...(system
      ? [
          { key: 'clamav', project: 'upstream', class: 'local' as const, format: 'clamav', description: 'ClamAV definitions', visibility: fixed },
          { key: 'scanner-trivy', project: 'upstream', class: 'remote' as const, format: 'oci', description: 'Image scanning database', visibility: fixed },
        ]
      : []),
    { key: 'generic-local', project: 'web', class: 'local', format: 'generic', description: 'Installers' },
  ];
}

/** The page Repositories of the project upstream as the router builds it, with its query bound to the list's inputs. */
async function open(url: string, options: { system?: boolean; capabilities?: string[]; clamavOn?: boolean } = {}) {
  TestBed.resetTestingModule();
  TestBed.configureTestingModule({
    providers: [
      provideRouter(
        [
          { path: 'projects/:project/repositories', component: RepositoryList },
          { path: 'admin/repositories', component: RepositoryList },
          { path: '**', children: [] },
        ],
        withComponentInputBinding(),
      ),
      ...provideFakeApis(),
    ],
  });
  const fake = TestBed.inject(RepositoriesApi) as FakeRepositoriesApi;
  seed(fake, options.system ?? true);
  // The description as the product stores it: the page shows its own sentence instead (Q-UI-895).
  (TestBed.inject(ProjectsApi) as FakeProjectsApi).projects = [
    sampleProject({ key: 'upstream', displayName: 'Upstream', description: 'Stored by the product', owners: [], members: 0 }),
  ];
  if (options.clamavOn) {
    (TestBed.inject(UpstreamApi) as FakeUpstreamApi).set('clamav', { clients: { enabled: true, address: CLAMAV_ENDPOINT }, visibility: 'public' });
  }
  if (options.capabilities) (TestBed.inject(SessionApi) as FakeSessionApi).meResult = { capabilities: options.capabilities };
  await TestBed.inject(SessionStore).load();
  const harness = await RouterTestingHarness.create();
  await harness.navigateByUrl(url);
  for (let i = 0; i < 6; i++) {
    await harness.fixture.whenStable();
    harness.detectChanges();
  }
  return { harness, root: harness.routeNativeElement as HTMLElement, fake };
}

const section = (root: HTMLElement) => root.querySelector<HTMLElement>('quader-disclosure.system-repositories');
const toggle = (root: HTMLElement) => section(root)!.querySelector<HTMLButtonElement>('button[aria-expanded]')!;
const keys = (table: Element | null) => [...(table?.querySelectorAll('tbody th[scope="row"] a') ?? [])].map((a) => text(a));
const mainTable = (root: HTMLElement) => root.querySelector(':scope > quader-data-table');
const sectionTable = (root: HTMLElement) => section(root)!.querySelector('quader-data-table');

describe('The system repositories of the project upstream (R-UI-192)', () => {
  it('R-UI-192 Q-UI-891 Q-UI-892 the list holds no system repository; they stand below it in the closed section System repositories, counted, said in one sentence, without Set up client', async () => {
    const { root, harness } = await open('/projects/upstream/repositories');
    expect(keys(mainTable(root))).toEqual(['debian', 'docker-hub', 'npmjs']);
    // Every registry keeps Set up client in the list.
    expect(mainTable(root)!.querySelectorAll('td.actions.with-setup a.row-setup')).toHaveLength(3);

    const s = section(root)!;
    expect(s.compareDocumentPosition(mainTable(root)!) & Node.DOCUMENT_POSITION_PRECEDING).toBeTruthy();
    // Heard as the heading and its number, the chevron left out.
    expect(spokenText(toggle(root))).toBe('System repositories 2');
    expect(s.querySelector('h2 .heading-count')?.textContent?.trim()).toBe('2');
    expect(text(s.querySelector('.hint')!)).toBe(
      'Quader writes these repositories itself. Its scanners read them, and an administrator can offer the ClamAV definitions to your ClamAV installations.',
    );
    // Closed at the visit, so nothing in it can be focused (the style guide's collapse region).
    expect(toggle(root).getAttribute('aria-expanded')).toBe('false');
    expect(s.querySelector('.collapse-region')!.classList.contains('open')).toBe(false);

    // The same columns as the list above, and the rows' facts; no Set up client, no Delete.
    const heads = (t: Element) => [...t.querySelectorAll('thead th')].map((th) => text(th).replace('help_outline', '').trim());
    expect(heads(sectionTable(root)!)).toEqual(heads(mainTable(root)!));
    expect(keys(sectionTable(root))).toEqual(['clamav', 'scanner-trivy']);
    const trivy = [...sectionTable(root)!.querySelectorAll('tbody tr')].find((tr) => text(tr).includes('scanner-trivy'))!;
    expect(text(trivy.querySelector('td.format-cell')!)).toContain('OCI (Docker)');
    expect(text(trivy.querySelector('td.class-cell')!)).toContain('Remote');
    expect(sectionTable(root)!.querySelector('a.row-setup, td.with-setup')).toBeNull();
    expect([...s.querySelectorAll('a')].some((a) => (a.getAttribute('href') ?? '').endsWith('/setup'))).toBe(false);

    // The project's own sentence under the title, not the stored one (Q-UI-895).
    expect(text(root.querySelector('.page-header-description')!)).toBe('The public registries this installation offers to every project.');

    toggle(root).click();
    harness.detectChanges();
    expect(toggle(root).getAttribute('aria-expanded')).toBe('true');
    expect(s.querySelector('.collapse-region')!.classList.contains('open')).toBe(true);
    await expectAccessible(root);
  });

  it('R-UI-192 the row menu of a system repository offers what the API lets a caller do with it: Settings, Files and, for an image repository, Vulnerabilities', async () => {
    const { root, harness } = await open('/projects/upstream/repositories');
    const menuOf = async (key: string): Promise<string[]> => {
      sectionTable(root)!.querySelector<HTMLButtonElement>(`button[aria-label="Actions for ${key}"]`)!.click();
      harness.detectChanges();
      await harness.fixture.whenStable();
      const menu = root.ownerDocument.body.querySelector('.menu')!;
      const items = [...menu.querySelectorAll('[role="menuitem"]')].map((i) => text(i));
      root.ownerDocument.body.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }));
      menu.closest('.cdk-overlay-pane')?.remove();
      return items;
    };
    expect(await menuOf('scanner-trivy')).toEqual(['Settings', 'Files', 'Vulnerabilities']);
    expect(await menuOf('clamav')).toEqual(['Settings', 'Files', 'Versions']);
  });

  it('R-UI-192 Q-UI-894 the filter covers both tables: a match there opens the section and is counted; no match there leaves it closed at 0', async () => {
    const trivy = await open('/projects/upstream/repositories?q=TRIVY');
    expect(text(mainTable(trivy.root)!)).toContain('No repository matches.');
    expect(toggle(trivy.root).getAttribute('aria-expanded')).toBe('true');
    expect(section(trivy.root)!.querySelector('.heading-count')?.textContent?.trim()).toBe('1');
    expect(keys(sectionTable(trivy.root))).toEqual(['scanner-trivy']);

    // A filter of the drawer is a filter too: the image repositories, one of them a system repository.
    const oci = await open('/projects/upstream/repositories?format=oci');
    expect(keys(mainTable(oci.root))).toEqual(['docker-hub']);
    expect(toggle(oci.root).getAttribute('aria-expanded')).toBe('true');
    expect(keys(sectionTable(oci.root))).toEqual(['scanner-trivy']);

    const docker = await open('/projects/upstream/repositories?q=docker');
    expect(keys(mainTable(docker.root))).toEqual(['docker-hub']);
    expect(toggle(docker.root).getAttribute('aria-expanded')).toBe('false');
    expect(section(docker.root)!.querySelector('.heading-count')?.textContent?.trim()).toBe('0');
    expect(text(sectionTable(docker.root)!)).toContain('No system repository matches.');
    // The section's rows are read once per visit, by name, and filtered here.
    expect(docker.fake.listFilters.filter((f) => f.name?.length)).toEqual([
      { project: 'upstream', name: ['upstream/scanner-trivy', 'upstream/clamav'] },
    ]);
  });

  it('R-UI-193 Q-UI-898 Q-UI-902 the admin area lists the system repositories with the badge System, without the copy button and without Copy client address', async () => {
    const { root, harness } = await open('/admin/repositories?project=upstream');
    const row = (key: string) => [...root.querySelectorAll('tbody tr')].find((tr) => text(tr.querySelector('th a')!) === key)!;
    const badges = (key: string) => [...row(key).querySelectorAll('th .badge')].map((b) => text(b));
    expect(badges('scanner-trivy')).toEqual(['System']);
    expect(badges('clamav')).toEqual(['System']);
    expect(badges('npmjs')).toEqual([]);
    expect(row('scanner-trivy').querySelector('quader-copy-button')).toBeNull();
    expect(row('npmjs').querySelector('quader-copy-button button')?.getAttribute('aria-label')).toBe(
      'Copy client address https://quader.example.net/files/upstream/npmjs/',
    );
    const menuOf = async (key: string): Promise<string[]> => {
      row(key).querySelector<HTMLButtonElement>(`button[aria-label="Actions for ${key}"]`)!.click();
      harness.detectChanges();
      await harness.fixture.whenStable();
      const menu = root.ownerDocument.body.querySelector('.menu')!;
      const items = [...menu.querySelectorAll('[role="menuitem"]')].map((i) => text(i));
      root.ownerDocument.body.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }));
      menu.closest('.cdk-overlay-pane')?.remove();
      return items;
    };
    expect(await menuOf('scanner-trivy')).toEqual(['Settings', 'Files', 'Vulnerabilities']);
    expect(await menuOf('npmjs')).toContain('Copy client address');
    // No section in the admin area: the badge sets them apart there (Q-UI-902).
    expect(section(root)).toBeNull();
    await expectAccessible(root);
  });

  it('R-UI-192 a caller who reads no system repository sees no section', async () => {
    const { root } = await open('/projects/upstream/repositories', { system: false, capabilities: [] });
    expect(keys(mainTable(root))).toEqual(['debian', 'docker-hub', 'npmjs']);
    expect(section(root)).toBeNull();
  });

  it('R-UI-192 Q-UI-908 with ClamAV definition updates on, the row of clamav has Set up client, and scanner-trivy still none', async () => {
    const { root } = await open('/projects/upstream/repositories', { clamavOn: true });
    const setups = [...sectionTable(root)!.querySelectorAll<HTMLAnchorElement>('a.row-setup')].map((a) => a.getAttribute('href'));
    expect(setups).toEqual(['/projects/upstream/repositories/clamav/setup']);
  });
});
