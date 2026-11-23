import { TestBed } from '@angular/core/testing';
import { Router } from '@angular/router';
import { vi } from 'vitest';
import { ApiError } from '../../api/problem';
import { FormatDescriptor, RepositoriesApi, Repository } from '../../api/repositories-api';
import {
  DEB_FORMAT,
  FakeRepositoriesApi,
  GENERIC_FORMAT,
  NUGET_FORMAT,
  OCI_FORMAT,
  PACKAGE_FORMAT,
  sampleRepositoryDefaults,
} from '../../api/testing/fakes-catalog';
import { button, expectAccessible, renderScreen, settle, sleep, storedText, text, type } from '../../testing/screen';
import { DraftStore } from '../../shared/drafts';
import { RepositoryCreate } from './repository-create';

const NO_VIRTUAL: FormatDescriptor = { ...NUGET_FORMAT, classes: ['local', 'remote'] };
/** A format with a remote class and no presets: its URL is typed. */
const NO_PRESETS: FormatDescriptor = { ...NUGET_FORMAT, upstreamPresets: [] };

type Form = Awaited<ReturnType<typeof renderScreen<RepositoryCreate>>>;

async function openForm(
  format: string,
  options: {
    formats?: readonly FormatDescriptor[];
    setup?: (fake: FakeRepositoriesApi) => void;
    capabilities?: readonly string[];
  } = {},
): Promise<Form & { fake: FakeRepositoriesApi }> {
  const r = await renderScreen(RepositoryCreate, {
    inputs: { project: 'web', format },
    ...(options.capabilities ? { capabilities: options.capabilities } : {}),
    setup: (get) => {
      const fake = get(RepositoriesApi) as FakeRepositoriesApi;
      fake.formatsResult = options.formats ?? [GENERIC_FORMAT, PACKAGE_FORMAT, NUGET_FORMAT, OCI_FORMAT, DEB_FORMAT];
      options.setup?.(fake);
    },
  });
  return { ...r, fake: r.get(RepositoriesApi) as FakeRepositoriesApi };
}

const $ = <T extends Element = HTMLInputElement>(r: Form, selector: string) => r.root.querySelector<T & Element>(selector)!;

async function chooseClass(r: Form, cls: string) {
  $(r, `input[name="class"][value="${cls}"]`).click();
  await settle(r.fixture);
}

async function openAdvanced(r: Form) {
  button(r.root, /Advanced settings/).click();
  await settle(r.fixture);
}

async function create(r: Form) {
  button(r.root, 'Create repository').click();
  await settle(r.fixture);
}

function stayOnPage(): ReturnType<typeof vi.spyOn> {
  return vi.spyOn(TestBed.inject(Router), 'navigate').mockResolvedValue(true);
}

const created = (fake: FakeRepositoriesApi, key: string) => fake.repositories.find((x) => x.key === key) as Repository;

describe('The creation form of a repository', () => {
  it('R-UI-080 R-UI-088 is one page titled after the format, with Change format, no step list and no Next button', async () => {
    const r = await openForm('nuget');
    expect(text($(r, 'h1'))).toBe('New NuGet repository');
    expect(r.root.querySelector('nav ol, [aria-current="step"]')).toBeNull();
    expect([...r.root.querySelectorAll('button')].some((b) => /^\s*Next\s*$/.test(b.textContent ?? ''))).toBe(false);
    const change = [...r.root.querySelectorAll('a')].find((a) => text(a) === 'Change format')!;
    expect(change.getAttribute('href')).toBe('/projects/web/repositories/new');
    const oci = await openForm('oci');
    expect(text($(oci, 'h1'))).toBe('New OCI (Docker) repository');
    for (const h of oci.root.querySelectorAll('h1, h2, h3')) expect(text(h), text(h)).not.toMatch(/ and /);
    await expectAccessible(oci.root);
  });

  it('R-UI-080 the class is a radio group preset to local; a class the format lacks is disabled with its reason', async () => {
    const r = await openForm('nuget', { formats: [NO_VIRTUAL] });
    const radio = (c: string) => $(r, `input[name="class"][value="${c}"]`);
    expect(radio('local').checked).toBe(true);
    expect(radio('virtual').disabled).toBe(true);
    expect(text(r.root.querySelector('fieldset.choices')!)).toContain('NuGet does not implement the class Virtual.');
    await expectAccessible(r.root);
  });

  it('R-UI-080 an unknown format of the address shows the chooser with the notice', async () => {
    const r = await openForm('rpm');
    expect(text($(r, 'h1'))).toBe('New repository');
    expect(text(r.root)).toContain('This installation has no format rpm.');
    expect(r.root.querySelectorAll('a.tile').length).toBe(5);
  });

  it('R-UI-081 R-UI-084 R-UI-086 the key starts with the free suggestion and the opened form shows no error', async () => {
    const r = await openForm('npm', {
      setup: (fake) => fake.repositories.push({ key: 'npm-local', project: 'web', class: 'local', format: 'npm' }),
    });
    expect($(r, '#f-key').value).toBe('npm-local-2');
    expect(r.root.querySelector('.field-error, [aria-invalid="true"], [role="alert"]')).toBeNull();
    await chooseClass(r, 'remote');
    expect($(r, '#f-key').value).toBe('npm-remote');
    expect(r.root.querySelector('.field-error, [aria-invalid="true"]')).toBeNull();
  });

  it('R-UI-086 the key shows its error after it was left with a value, and an empty key stops a create with the summary', async () => {
    const r = await openForm('npm');
    const key = $(r, '#f-key');
    type(key, 'A');
    await settle(r.fixture);
    expect(r.root.querySelector('#e-key')).toBeNull();
    key.dispatchEvent(new Event('blur'));
    await settle(r.fixture);
    expect(text($(r, '#e-key'))).toBe('Use 2 to 64 lower-case letters and digits, with ., _, __ or - between them.');
    expect(key.getAttribute('aria-invalid')).toBe('true');

    type(key, '');
    await settle(r.fixture);
    await create(r);
    expect(r.fake.createKeys).toEqual([]);
    const summary = $(r, '[role="alert"]');
    expect(text(summary)).toContain('Key');
    await sleep(r.fixture, 30);
    expect(document.activeElement).toBe(summary);
    await expectAccessible(r.root);
  });

  it('R-UI-086 the availability of a typed key is asked after a pause and a taken key is named', async () => {
    const r = await openForm('generic', {
      setup: (fake) => fake.repositories.push({ key: 'taken', project: 'web', class: 'local', format: 'generic' }),
    });
    type($(r, '#f-key'), 'taken');
    await settle(r.fixture);
    expect(r.root.querySelector('#e-key')).toBeNull();
    await sleep(r.fixture, 500);
    expect(text($(r, '#e-key'))).toBe('A repository with this key already exists in the project.');
  });

  it("R-UI-086 an untouched key is never asked, also while the next class's suggestion is slow to come", async () => {
    const r = await openForm('npm');
    const asked = vi.spyOn(r.fake, 'get');
    let release!: () => void;
    r.fake.defaultsHold = new Promise((go) => (release = go));
    // Not chooseClass nor sleep: their settle waits for the held answer.
    $(r, 'input[name="class"][value="remote"]').click();
    r.fixture.detectChanges();
    await new Promise((go) => setTimeout(go, 500));
    r.fixture.detectChanges();
    expect(asked).not.toHaveBeenCalled();
    r.fake.defaultsHold = null;
    release();
    await sleep(r.fixture, 20);
    expect($(r, '#f-key').value).toBe('npm-remote');
    await sleep(r.fixture, 500);
    expect(asked).not.toHaveBeenCalled();
  });

  it('R-UI-081 R-UI-042 a create of a local generic repository without typing sends the identity only, once on a double click', async () => {
    const r = await openForm('generic');
    const navigate = stayOnPage();
    button(r.root, 'Create repository').click();
    button(r.root, 'Create repository').click();
    await settle(r.fixture);
    expect(r.fake.createKeys.length).toBe(1);
    expect(created(r.fake, 'generic-local-2')).toEqual({ key: 'generic-local-2', class: 'local', format: 'generic', project: 'web' });
    expect(navigate).toHaveBeenCalledWith(['/projects', 'web', 'repositories', 'generic-local-2']);
  });

  it('R-UI-081 R-UI-083 the main form holds no notes, patterns or switches; Advanced settings is closed and holds them, without Disabled', async () => {
    const r = await openForm('npm');
    const advanced = $(r, '.advanced-settings');
    const outside = [...r.root.querySelectorAll('form > *')].filter((el) => el !== advanced);
    for (const id of ['f-adminNotes', 'f-includePatterns', 'f-hideExistence', 'f-disabled']) {
      expect(outside.some((el) => el.querySelector(`#${id}`)), id).toBe(false);
    }
    expect(button(r.root, /Advanced settings/).getAttribute('aria-expanded')).toBe('false');
    expect(text(advanced)).toContain('Notes, paths and the settings of the format. The defaults suit most repositories.');
    await openAdvanced(r);
    expect([...advanced.querySelectorAll('h3')].map((h) => text(h))).toEqual([
      'Administrator notes',
      'Paths',
      'Access details',
      'npm settings',
    ]);
    expect(advanced.querySelector('#f-npm-latestGuard')).not.toBeNull();
    expect(r.root.querySelector('#f-disabled')).toBeNull();
    await expectAccessible(r.root);
  });

  it('R-UI-084 a format setting changed in Advanced settings is written; untouched ones are not', async () => {
    const r = await openForm('npm');
    stayOnPage();
    await openAdvanced(r);
    const guard = $(r, '#f-npm-latestGuard');
    expect(guard.checked).toBe(true);
    guard.click();
    type($(r, '#f-description'), 'Our packages');
    await settle(r.fixture);
    await create(r);
    expect(created(r.fake, 'npm-local')).toEqual({
      key: 'npm-local',
      class: 'local',
      format: 'npm',
      project: 'web',
      description: 'Our packages',
      npm: { latestGuard: false },
    });
  });

  it('R-UI-084 a local Debian repository shows the schema defaults as values and a create that keeps them writes none', async () => {
    const r = await openForm('deb');
    stayOnPage();
    await openAdvanced(r);
    expect($(r, '#f-deb-origin').value).toBe('quader');
    expect($<HTMLTextAreaElement>(r, '#f-deb-architectures').value).toBe('amd64\narm64');
    expect(text(r.root)).not.toContain('Default:');
    const origin = $(r, '#f-deb-origin');
    type(origin, '');
    await settle(r.fixture);
    origin.dispatchEvent(new Event('blur'));
    await settle(r.fixture);
    expect(origin.value).toBe('quader');
    await create(r);
    expect(created(r.fake, 'deb-local').deb).toBeUndefined();
  });

  it('R-UI-083 R-UI-044 a server error at /includePatterns/0 opens Advanced settings, marks the field, and the summary link focuses it', async () => {
    const r = await openForm('generic', {
      setup: (fake) => {
        fake.create = async () => {
          throw new ApiError(
            422,
            { code: 'validation_failed', errors: [{ pointer: '/includePatterns/0', code: 'invalid_pattern', detail: 'bad pattern' }] },
            null,
          );
        };
      },
    });
    await create(r);
    expect(button(r.root, /Advanced settings/).getAttribute('aria-expanded')).toBe('true');
    expect($(r, '#f-includePatterns').getAttribute('aria-invalid')).toBe('true');
    const link = r.root.querySelector<HTMLButtonElement>('[role="alert"] button')!;
    expect(text(link)).toBe('Include patterns');
    button(r.root, /Advanced settings/).click();
    await settle(r.fixture);
    link.click();
    await settle(r.fixture);
    expect(button(r.root, /Advanced settings/).getAttribute('aria-expanded')).toBe('true');
    expect(document.activeElement).toBe($(r, '#f-includePatterns'));
  });

  it('R-UI-082 an OCI remote offers the four registries; the pressed one follows the URL and the create sends the URL of another', async () => {
    const r = await openForm('oci');
    stayOnPage();
    await chooseClass(r, 'remote');
    const presets = [...r.root.querySelectorAll<HTMLButtonElement>('[aria-pressed]')];
    expect(presets.map((b) => text(b))).toEqual(['Docker Hub', 'Quay', 'GitHub (ghcr.io)', 'GitLab']);
    expect(presets.map((b) => b.getAttribute('aria-pressed'))).toEqual(['true', 'false', 'false', 'false']);
    expect($(r, '#f-remote-url').value).toBe('https://registry-1.docker.io/');
    expect($(r, '#f-remote-url').classList.contains('field-w-full')).toBe(true);

    presets[3]!.click();
    await settle(r.fixture);
    expect($(r, '#f-remote-url').value).toBe('https://registry.gitlab.com/');
    expect(presets.map((b) => b.getAttribute('aria-pressed'))).toEqual(['false', 'false', 'false', 'true']);
    type($<HTMLSelectElement>(r, '#f-remote-authMode'), 'basic');
    await settle(r.fixture);
    expect(text($(r, '#d-remote-credential-hosts'))).toBe('The credential is sent to registry.gitlab.com and gitlab.com.');

    type($(r, '#f-remote-url'), 'https://registry.example.com/');
    await settle(r.fixture);
    expect(presets.every((b) => b.getAttribute('aria-pressed') === 'false')).toBe(true);
    presets[3]!.click();
    type($<HTMLSelectElement>(r, '#f-remote-authMode'), 'none');
    await settle(r.fixture);
    await create(r);
    expect(created(r.fake, 'oci-remote').remote).toEqual({ url: 'https://registry.gitlab.com/' });
    await expectAccessible(r.root);
  });

  it('R-UI-082 an npm remote has one pressed button, and a create that changes nothing sends no remote member', async () => {
    const r = await openForm('npm');
    stayOnPage();
    await chooseClass(r, 'remote');
    const presets = [...r.root.querySelectorAll<HTMLButtonElement>('[aria-pressed]')];
    expect(presets.map((b) => [text(b), b.getAttribute('aria-pressed')])).toEqual([['npm public registry', 'true']]);
    await create(r);
    expect(created(r.fake, 'npm-remote').remote).toBeUndefined();
  });

  it('R-UI-082 Test connection under the credentials posts the unsaved section and says the result in words; a change clears it', async () => {
    const r = await openForm('npm');
    await chooseClass(r, 'remote');
    type($<HTMLSelectElement>(r, '#f-remote-authMode'), 'basic');
    await settle(r.fixture);
    type($(r, '#f-remote-username'), 'ci');
    type($(r, '#f-remote-password'), 's3cret');
    await settle(r.fixture);
    const order = [...r.root.querySelectorAll('#f-remote-password, button')].map((el) => el.id || text(el));
    expect(order.indexOf('Test connection')).toBeGreaterThan(order.indexOf('f-remote-password'));
    button(r.root, 'Test connection').click();
    await settle(r.fixture);
    expect(r.fake.upstreamTests).toEqual([
      {
        format: 'npm',
        remote: { url: 'https://registry.npmjs.org/', extraHosts: [], authMode: 'basic', username: 'ci', password: { value: 's3cret' } },
      },
    ]);
    expect(r.fake.upstreamTestProjects).toEqual(['web']);
    expect(text($(r, '[data-testid="upstream-test-result"]'))).toContain('The upstream answered and accepted the credentials.');
    type($(r, '#f-remote-username'), 'other');
    await settle(r.fixture);
    expect(r.root.querySelector('[data-testid="upstream-test-result"]')).toBeNull();
  });

  it('R-UI-082 R-REPO-076 an OCI remote offers the registries of this installation after the well-known ones; one sets the URL, its hosts follow it, and the create names them', async () => {
    const harbor = { displayName: 'Harbor', url: 'https://harbor.example.com/', extraHosts: [{ pattern: 'harbor.example.com', sendCredentials: true }] };
    const r = await openForm('oci', {
      setup: (fake) => {
        fake.defaultsFor = (project, format, cls) => ({
          ...sampleRepositoryDefaults(OCI_FORMAT, cls),
          registryPresets: cls === 'remote' ? [harbor] : [],
        });
      },
    });
    stayOnPage();
    await chooseClass(r, 'remote');
    const groups = [...r.root.querySelectorAll('quader-upstream-settings [role="group"] > .field-label')].map((l) => text(l));
    expect(groups.slice(0, 2)).toEqual(['Well-known registries', 'Registries of this installation']);
    // The hint under the field names both kinds of button it offers (R-UI-082).
    expect(text($(r, '#d-remote-url'))).toBe('Choose a well-known registry or one of this installation, or enter the address of another.');
    const presets = [...r.root.querySelectorAll<HTMLButtonElement>('[aria-pressed]')];
    expect(presets.map((b) => text(b))).toEqual(['Docker Hub', 'Quay', 'GitHub (ghcr.io)', 'GitLab', 'Harbor']);
    presets[4]!.click();
    await settle(r.fixture);
    expect($(r, '#f-remote-url').value).toBe('https://harbor.example.com/');
    expect(presets.map((b) => b.getAttribute('aria-pressed'))).toEqual(['false', 'false', 'false', 'false', 'true']);
    expect($(r, '#f-remote-extraHosts-0').value).toBe('harbor.example.com');
    type($<HTMLSelectElement>(r, '#f-remote-authMode'), 'basic');
    await settle(r.fixture);
    expect($(r, '#f-remote-password').value).toBe('');
    expect(text($(r, '#d-remote-credential-hosts'))).toBe('The credential is sent to harbor.example.com.');
    type($<HTMLSelectElement>(r, '#f-remote-authMode'), 'none');
    await settle(r.fixture);
    await create(r);
    expect(created(r.fake, 'oci-remote').remote).toEqual({
      url: 'https://harbor.example.com/',
      extraHosts: [{ pattern: 'harbor.example.com', sendCredentials: true }],
    });
  });

  it('R-UI-082 R-REPO-076 the form of a format without well-known registries offers the registries of this installation alone', async () => {
    const r = await openForm('nuget', {
      formats: [NO_PRESETS],
      setup: (fake) => {
        fake.defaultsFor = (project, format, cls) => ({
          ...sampleRepositoryDefaults(NO_PRESETS, cls),
          registryPresets: [{ displayName: 'Feed', url: 'https://nuget.example.com/v3/index.json', extraHosts: [] }],
        });
      },
    });
    await chooseClass(r, 'remote');
    const presets = [...r.root.querySelectorAll<HTMLButtonElement>('[aria-pressed]')];
    expect(presets.map((b) => [text(b), b.getAttribute('aria-pressed')])).toEqual([['Feed', 'false']]);
    expect($(r, '#f-remote-url').value).toBe('');
    expect(text($(r, '#d-remote-url'))).toBe('Choose a registry of this installation or enter the address of another.');
    await expectAccessible(r.root);
  });

  it('R-UI-082 R-UI-086 a format without presets starts with an empty URL that a create asks for', async () => {
    const r = await openForm('nuget', { formats: [NO_PRESETS] });
    await chooseClass(r, 'remote');
    expect(r.root.querySelector('[aria-pressed]')).toBeNull();
    expect($(r, '#f-remote-url').value).toBe('');
    expect(r.root.querySelector('quader-upstream-settings .field-error')).toBeNull();
    await create(r);
    expect(r.fake.createKeys).toEqual([]);
    expect(text($(r, 'quader-upstream-settings .field-error'))).toBe('Enter the address of the registry, such as https://registry.example.com/.');
    expect(text($(r, '[role="alert"]'))).toContain('Upstream URL');
  });

  it('R-UI-081 a virtual npm repository is created with its members in order and the first local member as deployment target', async () => {
    const r = await openForm('npm', {
      setup: (fake) =>
        fake.repositories.push(
          { key: 'npm-remote', project: 'web', class: 'remote', format: 'npm' },
          { key: 'npm-local', project: 'web', class: 'local', format: 'npm' },
          { key: 'npm-other', project: 'data', class: 'local', format: 'npm' },
          { key: 'generic-x', project: 'web', class: 'local', format: 'generic' },
        ),
    });
    stayOnPage();
    await chooseClass(r, 'virtual');
    const available = () => [...r.root.querySelectorAll('[data-testid="vm-available"] [role="option"]')].map((o) => o.getAttribute('aria-label') ?? text(o));
    expect(available().map((t) => t.split(' ')[0])).toEqual(['npm-local', 'npm-remote']);
    const option = (key: string) =>
      [...r.root.querySelectorAll<HTMLElement>('[data-testid="vm-available"] [role="option"]')].find((o) => text(o).startsWith(key))!;
    option('npm-local').click();
    await settle(r.fixture);
    button(r.root, 'Add').click();
    await settle(r.fixture);
    option('npm-remote').click();
    await settle(r.fixture);
    button(r.root, 'Add').click();
    await settle(r.fixture);
    expect($<HTMLSelectElement>(r, '#f-deployTarget').value).toBe('npm-local');
    await expectAccessible(r.root);
    await create(r);
    expect(created(r.fake, 'npm-virtual').virtual).toEqual({ members: ['npm-local', 'npm-remote'], deployTarget: 'npm-local' });
  });

  it('R-UI-080 R-UI-043 Q-UI-552 Show as API request shows the body with a typed password masked and the curl on the base URL', async () => {
    const r = await openForm('npm');
    await chooseClass(r, 'remote');
    type($<HTMLSelectElement>(r, '#f-remote-authMode'), 'bearer');
    await settle(r.fixture);
    type($(r, '#f-remote-password'), 'tok-123');
    await settle(r.fixture);
    // Q-UI-552: a closed section of the form with the app's disclosure, as Advanced settings is; no bare <details>.
    expect(r.root.querySelector('details')).toBeNull();
    const toggle = $<HTMLButtonElement>(r, 'quader-disclosure.api-request h2 button');
    expect(text(toggle)).toContain('Show as API request');
    expect(toggle.getAttribute('aria-expanded')).toBe('false');
    toggle.click();
    await settle(r.fixture);
    expect(toggle.getAttribute('aria-expanded')).toBe('true');
    const request = text($(r, '.api-request'));
    expect(request).toContain('"value": "<the password>"');
    expect(request).not.toContain('tok-123');
    expect(request).toContain("curl -X POST 'https://quader.example.net/api/v1/projects/web/repositories'");
  });

  it('R-UI-080 another class keeps an edited key and the description and starts the Advanced settings afresh', async () => {
    const r = await openForm('npm');
    type($(r, '#f-key'), 'team-npm');
    type($(r, '#f-description'), 'Team packages');
    await openAdvanced(r);
    type($(r, '#f-adminNotes'), 'note');
    await settle(r.fixture);
    await chooseClass(r, 'remote');
    expect($(r, '#f-key').value).toBe('team-npm');
    expect($<HTMLTextAreaElement>(r, '#f-description').value).toBe('Team packages');
    expect($<HTMLTextAreaElement>(r, '#f-adminNotes').value).toBe('');
  });

  it('R-UI-085 Q-UI-551 every field takes the width of its kind of input; the description is a sentence, as wide as the key above it', async () => {
    const r = await openForm('npm');
    expect($(r, '#f-key').classList.contains('field-w-name')).toBe(true);
    expect($(r, '#f-description').classList.contains('field-w-name')).toBe(true);
    await openAdvanced(r);
    expect($(r, '#f-adminNotes').classList.contains('field-w-full')).toBe(true);
    expect($(r, '#f-includePatterns').classList.contains('field-w-full')).toBe(true);
    expect($(r, '#f-hideExistence').classList.contains('field-w-name')).toBe(true);
  });

  it('R-UI-080 leaving with something typed counts as unsaved; the opened form and a created repository do not', async () => {
    const r = await openForm('npm');
    expect(r.fixture.componentInstance.hasUnsavedChanges()).toBe(false);
    type($(r, '#f-description'), 'x');
    await settle(r.fixture);
    expect(r.fixture.componentInstance.hasUnsavedChanges()).toBe(true);
    stayOnPage();
    await create(r);
    expect(r.fixture.componentInstance.hasUnsavedChanges()).toBe(false);
  });

  it('R-UI-080 R-UI-043 a draft kept for a recent sign-in has no password', async () => {
    const r = await openForm('npm', {
      setup: (fake) => (fake.defaultsFor = (_p, _f, cls) => sampleRepositoryDefaults(PACKAGE_FORMAT, cls)),
    });
    await chooseClass(r, 'remote');
    type($<HTMLSelectElement>(r, '#f-remote-authMode'), 'basic');
    await settle(r.fixture);
    type($(r, '#f-remote-password'), 'hunter2');
    await settle(r.fixture);
    (r.fixture.componentInstance as unknown as { keepDraft(): void }).keepDraft();
    expect(storedText()).not.toContain('hunter2');
    expect(r.get(DraftStore).take<{ cls: string }>()?.cls).toBe('remote');
  });

  it('R-UI-021 R-REPO-001 Q-UI-1040 the project is the address\'s, named under the title and never chosen', async () => {
    const r = await openForm('npm', { formats: [PACKAGE_FORMAT] });
    expect(r.root.querySelector('#f-project')).toBeNull();
    expect(text(r.root.querySelector('.page-header-description')!)).toBe('In the project web.');
    stayOnPage();
    type($(r, '#f-description'), 'x');
    await settle(r.fixture);
    await create(r);
    expect(created(r.fake, 'npm-local')?.project).toBe('web');
  });

  describe('a default that arrives while the person is in its field (Q-UI-702)', () => {
    /**
     * Renders and lets every answer but the held one arrive. `settle` waits for the app to be stable, which a held answer of
     * `getRepositoryDefaults` (a resource) keeps it from being.
     */
    async function step(r: Form): Promise<void> {
      for (let i = 0; i < 4; i++) {
        await new Promise((go) => setTimeout(go, 0));
        r.fixture.detectChanges();
      }
    }

    /** Holds the next answers of the class's defaults until the returned function is called. */
    function holdDefaults(fake: FakeRepositoriesApi): () => void {
      let release!: () => void;
      fake.defaultsHold = new Promise((go) => (release = go));
      return () => {
        fake.defaultsHold = null;
        release();
      };
    }

    const focus = (el: HTMLElement): boolean => el.dispatchEvent(new Event('focus'));
    const leave = (el: HTMLElement): boolean => el.dispatchEvent(new Event('blur'));

    it('R-UI-084 Q-UI-342 Q-UI-702 a person who clicks straight into the empty key gets no suggestion under their hands; it is written when they leave the key untouched', async () => {
      const r = await renderScreen(RepositoryCreate, {
        inputs: { format: 'npm' },
        setup: (get) => ((get(RepositoriesApi) as FakeRepositoriesApi).formatsResult = [PACKAGE_FORMAT]),
      });
      const fake = r.get(RepositoriesApi) as FakeRepositoriesApi;
      const release = holdDefaults(fake);
      // The router binds the project after the page opened: the defaults are asked then, and held.
      r.fixture.componentRef.setInput('project', 'web');
      await step(r);
      const key = $(r, '#f-key');
      expect(key.value).toBe('');
      focus(key);
      await step(r);
      release();
      await settle(r.fixture);
      expect(key.value).toBe('');
      leave(key);
      await settle(r.fixture);
      expect(key.value).toBe('npm-local');
      expect(r.fixture.componentInstance.hasUnsavedChanges()).toBe(false);
    });

    it('R-UI-084 Q-UI-342 Q-UI-702 a suggestion that arrives between the focus and the first character never joins the typed key', async () => {
      const r = await openForm('npm', { formats: [PACKAGE_FORMAT] });
      const key = $(r, '#f-key');
      expect(key.value).toBe('npm-local');
      const release = holdDefaults(r.fake);
      $(r, 'input[name="class"][value="remote"]').click();
      await step(r);
      focus(key);
      await step(r);
      release();
      await settle(r.fixture);
      // The field the person is in keeps what it showed; before Q-UI-702 it became npm-remote here, and the typing went after it.
      expect(key.value).toBe('npm-local');
      type(key, 'npm-mine');
      await settle(r.fixture);
      leave(key);
      await settle(r.fixture);
      expect(key.value).toBe('npm-mine');
      stayOnPage();
      await create(r);
      expect(created(r.fake, 'npm-mine')).toBeDefined();
    });

    it('R-UI-084 Q-UI-342 Q-UI-702 a key left untouched takes the class’s suggestion when it is left', async () => {
      const r = await openForm('npm', { formats: [PACKAGE_FORMAT] });
      const key = $(r, '#f-key');
      const release = holdDefaults(r.fake);
      $(r, 'input[name="class"][value="remote"]').click();
      await step(r);
      focus(key);
      await step(r);
      release();
      await settle(r.fixture);
      expect(key.value).toBe('npm-local');
      leave(key);
      await settle(r.fixture);
      expect(key.value).toBe('npm-remote');
      expect(r.fixture.componentInstance.hasUnsavedChanges()).toBe(false);
    });

    it('R-UI-084 Q-UI-702 a cache period that arrives while its field has the focus is shown once the field is left', async () => {
      const r = await openForm('npm', {
        formats: [PACKAGE_FORMAT],
        setup: (fake) =>
          (fake.defaultsFor = (_p, _f, cls) => {
            const d = sampleRepositoryDefaults(PACKAGE_FORMAT, cls);
            const remote = (d.repository as Record<string, unknown>)['remote'] as { cache: { metadataTtlSeconds: number } } | undefined;
            if (remote) remote.cache.metadataTtlSeconds = 600;
            return d;
          }),
      });
      const release = holdDefaults(r.fake);
      $(r, 'input[name="class"][value="remote"]').click();
      await step(r);
      const period = $(r, '#f-remote-cache-metadataTtlSeconds');
      expect(period.value).toBe('');
      focus(period);
      await step(r);
      release();
      await settle(r.fixture);
      expect(period.value).toBe('');
      leave(period);
      await settle(r.fixture);
      expect(period.value).toBe('600');
    });

    it('R-UI-084 Q-UI-702 a mirror interval that arrives while its field has the focus is shown once the field is left', async () => {
      const r = await openForm('npm', { formats: [PACKAGE_FORMAT] });
      await openAdvanced(r);
      const release = holdDefaults(r.fake);
      $(r, 'input[name="class"][value="remote"]').click();
      await step(r);
      const mirror = $(r, 'input[name="remote-mode"][value="mirror"]');
      mirror.checked = true;
      mirror.dispatchEvent(new Event('change', { bubbles: true }));
      await step(r);
      const interval = $(r, '#f-remote-mirror-intervalSeconds');
      expect(interval.value).toBe('');
      focus(interval);
      await step(r);
      release();
      await settle(r.fixture);
      expect(interval.value).toBe('');
      leave(interval);
      await settle(r.fixture);
      expect(interval.value).toBe('3600');
    });
  });
});
