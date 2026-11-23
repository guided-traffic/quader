import { GuidesApi } from '../../api/guides-api';
import { ApiError } from '../../api/problem';
import { RepositoriesApi } from '../../api/repositories-api';
import { FakeGuidesApi } from '../../api/testing/fakes-guides';
import {
  ANSIBLE_FORMAT,
  DEB_FORMAT,
  FakeRepositoriesApi,
  GENERIC_FORMAT,
  PACKAGE_FORMAT,
} from '../../api/testing/fakes-catalog';
import { AnsibleApi } from '../../api/ansible-api';
import { FakeAnsibleApi } from '../../api/testing/fakes-ansible';
import { DebApi } from '../../api/deb-api';
import { FakeDebApi } from '../../api/testing/fakes-deb';
import { ProjectsApi } from '../../api/projects-api';
import { FakeProjectsApi, sampleProject } from '../../api/testing/fakes-projects';
import { FrequentPlaces } from '../../core/frequent-places';
import {
  addressValues,
  browserStorage,
  button,
  expectAccessible,
  openSection,
  renderScreen,
  saveButton,
  sectionHeadings,
  settle,
  text,
  type,
} from '../../testing/screen';
import { RepositoryDetail } from './repository-detail';

/** Opens the section of the settings page that holds what a tab held; the standard sections are open already (R-UI-087). */
async function openTab(r: Awaited<ReturnType<typeof open>>, label: string) {
  if (['General', 'Upstream', 'Members'].includes(label)) return;
  await openSection(r.fixture, r.root, label);
}

function withNpmLocal(npm?: Record<string, unknown>) {
  return (fake: FakeRepositoriesApi) => {
    fake.formatsResult = [GENERIC_FORMAT, PACKAGE_FORMAT];
    fake.repositories.push({
      key: 'npm-local',
      project: 'web',
      class: 'local',
      format: 'npm',
      description: 'Internal packages',
      ...(npm ? { npm: npm as never } : {}),
    });
  };
}

async function open(setup?: (fake: FakeRepositoriesApi) => void, key = 'generic-local') {
  return renderScreen(RepositoryDetail, {
    inputs: { project: 'web', key },
    setup: (get) => setup?.(get(RepositoriesApi) as FakeRepositoriesApi),
  });
}

describe('Repository edit form', () => {
  it('R-UI-021 key, class and format are read-only; the settings are editable; the page passes the accessibility check', async () => {
    const r = await open();
    const t = text(r.root);
    expect(t).toContain('generic-local');
    expect(r.root.querySelector<HTMLInputElement>('input#f-key')?.readOnly).toBe(true);
    expect(r.root.querySelector<HTMLTextAreaElement>('#f-description')?.value).toBe(
      'Installers and build outputs',
    );
    await expectAccessible(r.root);
  });

  it('R-UI-020 deleting the repository is a danger button last on its settings, behind Advanced, and asks for the key first', async () => {
    const { root, fixture, get } = await open();
    const fake = get(RepositoriesApi) as FakeRepositoriesApi;
    const save = saveButton(root);
    expect(save.parentElement!.textContent).not.toContain('Delete');
    const advanced = [...root.querySelectorAll('quader-disclosure')].at(-1)!;
    expect(text(advanced.querySelector('.heading span:not(.material-icons)')!)).toBe('Advanced');
    expect(save.compareDocumentPosition(advanced) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
    const remove = button(advanced, 'Delete repository');
    expect(remove.classList).toContain('btn-danger');
    remove.click();
    await settle(fixture);
    const dialog = root.ownerDocument.body.querySelector<HTMLElement>('[role="dialog"]')!;
    type(dialog.querySelector<HTMLInputElement>('#confirm-name')!, 'generic-local');
    await settle(fixture);
    button(dialog, 'Delete repository').click();
    await settle(fixture);
    expect(fake.deleteCalls.map((c) => c.key)).toEqual(['web/generic-local']);
  });

  it('R-UI-021 saving sends a merge patch of the changed members only, with the loaded ETag', async () => {
    const r = await open();
    const fake = r.get(RepositoriesApi) as FakeRepositoriesApi;
    expect(saveButton(r.root).disabled).toBe(true);

    type(r.root.querySelector<HTMLTextAreaElement>('#f-description')!, 'Changed');
    await settle(r.fixture);
    expect(r.fixture.componentInstance.hasUnsavedChanges()).toBe(true);
    saveButton(r.root).click();
    await settle(r.fixture);

    expect(fake.patches).toHaveLength(1);
    expect(fake.patches[0]).toMatchObject({
      key: 'web/generic-local',
      patch: { description: 'Changed' },
      etag: '"1"',
    });
    expect(r.fixture.componentInstance.hasUnsavedChanges()).toBe(false);
    expect(fake.repositories[0]?.description).toBe('Changed');
  });

  it('R-UI-021 and R-UI-042 saving after another administrator saved first opens the conflict dialog; Overwrite stores the draft with the new ETag', async () => {
    const r = await open((fake) => {
      fake.beforePatch = () => {
        // The other administrator saved first.
        fake.repositories[0] = { ...fake.repositories[0]!, description: 'Theirs' };
        fake.versions.set('web/generic-local', 2);
        fake.beforePatch = null;
      };
    });
    const fake = r.get(RepositoriesApi) as FakeRepositoriesApi;
    type(r.root.querySelector<HTMLTextAreaElement>('#f-description')!, 'Mine');
    await settle(r.fixture);
    saveButton(r.root).click();
    await settle(r.fixture);

    const body = r.root.ownerDocument.body;
    expect(text(body)).toContain('Changed by someone else since you opened it');
    expect(text(body)).toContain('Theirs');
    expect(text(body)).toContain('Mine');
    await expectAccessible(body.querySelector('[role="dialog"]')!);

    button(body, 'Overwrite').click();
    await settle(r.fixture);
    expect(fake.repositories[0]?.description).toBe('Mine');
    expect(fake.patches.at(-1)?.etag).toBe('"2"');
  });

  it('R-UI-042 a 412 on a retried PATCH whose first attempt committed is a success, not a conflict', async () => {
    const r = await open();
    const fake = r.get(RepositoriesApi) as FakeRepositoriesApi;
    const original = fake.patch.bind(fake);
    fake.patch = async (key, patch, etag) => {
      await original(key, patch, etag);
      const error = new ApiError(412, { code: 'precondition_failed' }, null);
      error.afterRetry = true;
      throw error;
    };
    type(r.root.querySelector<HTMLTextAreaElement>('#f-description')!, 'Once');
    await settle(r.fixture);
    saveButton(r.root).click();
    await settle(r.fixture);
    expect(r.root.ownerDocument.body.querySelector('[role="dialog"]')).toBeNull();
    expect(r.fixture.componentInstance.hasUnsavedChanges()).toBe(false);
  });

  it('R-UI-021 a server field error appears at its field and in the summary', async () => {
    const r = await open((fake) => {
      fake.patch = async () => {
        throw new ApiError(
          422,
          {
            code: 'invalid_pattern',
            errors: [{ pointer: '/description', code: 'invalid_value', detail: 'too long' }],
          },
          null,
        );
      };
    });
    type(r.root.querySelector<HTMLTextAreaElement>('#f-description')!, 'x');
    await settle(r.fixture);
    saveButton(r.root).click();
    await settle(r.fixture);
    expect(r.root.querySelector('[role="alert"]')?.textContent).toContain(
      'Some entries are not valid',
    );
    expect(r.root.querySelector('#f-description')?.getAttribute('aria-invalid')).toBe('true');
    await expectAccessible(r.root);
  });

  it('R-UI-021 R-ARCH-004 the npm settings tab is rendered from the schema for the class; turning the guard off patches the npm member only', async () => {
    const r = await open(withNpmLocal(), 'npm-local');
    const fake = r.get(RepositoriesApi) as FakeRepositoriesApi;
    await openTab(r, 'npm settings');
    const guard = r.root.querySelector<HTMLInputElement>('#f-npm-latestGuard')!;
    expect(guard.checked).toBe(true);
    expect(r.root.querySelector('#f-npm-latestStrategy')).toBeNull();
    expect(saveButton(r.root).disabled).toBe(true);
    await expectAccessible(r.root);

    guard.checked = false;
    guard.dispatchEvent(new Event('change', { bubbles: true }));
    await settle(r.fixture);
    saveButton(r.root).click();
    await settle(r.fixture);
    expect(fake.patches.at(-1)?.patch).toEqual({ npm: { latestGuard: false } });
    expect(fake.repositories.find((x) => x.key === 'npm-local')?.npm).toEqual({
      latestGuard: false,
    });
    expect(r.fixture.componentInstance.hasUnsavedChanges()).toBe(false);
  });

  it('R-UI-021 Q-UI-80 setting a value back to its default removes it from the document, and a stored default is no change', async () => {
    const r = await open(withNpmLocal({ latestGuard: false }), 'npm-local');
    const fake = r.get(RepositoriesApi) as FakeRepositoriesApi;
    await openTab(r, 'npm settings');
    const guard = r.root.querySelector<HTMLInputElement>('#f-npm-latestGuard')!;
    expect(guard.checked).toBe(false);
    guard.checked = true;
    guard.dispatchEvent(new Event('change', { bubbles: true }));
    await settle(r.fixture);
    saveButton(r.root).click();
    await settle(r.fixture);
    expect(fake.patches.at(-1)?.patch).toEqual({ npm: null });

    const stored = await open(withNpmLocal({ latestGuard: true }), 'npm-local');
    expect(stored.fixture.componentInstance.hasUnsavedChanges()).toBe(false);
  });

  it('R-UI-021 a repository of a format without settings has no section of format settings', async () => {
    const r = await open((fake) => (fake.formatsResult = [GENERIC_FORMAT, PACKAGE_FORMAT]));
    expect(sectionHeadings(r.root)).toEqual(['Access details', 'Malware scan', 'Signatures', 'Download links', 'Administrator notes', 'Service accounts', 'Details', 'API', 'Advanced']);
  });

  it('R-UI-021 R-UI-169 a caller who may use but not configure the repository sees the consumer view without a form and without an address field', async () => {
    const r = await open((fake) => (fake.consumerOnly = true));
    expect(text(r.root)).toContain(
      'You may use this repository but not read or change its configuration.',
    );
    expect(r.root.querySelector('textarea')).toBeNull();
    expect(r.root.querySelector('quader-address-field')).toBeNull();
    await expectAccessible(r.root);
  });

  it('R-UI-120 the settings have no section about the client: the guide is on Set up client of the repository\'s header', async () => {
    const r = await open(withNpmLocal(), 'npm-local');
    expect(sectionHeadings(r.root)).toEqual(['npm settings', 'Access details', 'Malware scan', 'Signatures', 'Download links', 'Administrator notes', 'Service accounts', 'Details', 'API', 'Advanced']);
    const guides = r.get(GuidesApi) as FakeGuidesApi;
    expect(guides.calls).toEqual([]);
    const consumer = await open((fake) => {
      withNpmLocal()(fake);
      fake.consumerOnly = true;
    }, 'npm-local');
    expect(text(consumer.root)).not.toContain('How to use');
    expect(consumer.root.querySelector('a[href$="/setup"]')).toBeNull();
    await expectAccessible(consumer.root);
  });

  it('R-UI-021 an unknown repository shows the not-found error and passes the accessibility check', async () => {
    const r = await open(undefined, 'no-such-repo');
    expect(text(r.root)).toContain('does not exist');
    await expectAccessible(r.root);
  });

  it('R-REPO-002 R-UI-021 R-UI-169 the curl command carries the configured base URL even though the page is served from another origin; the client address is Set up client\'s', async () => {
    const r = await open((fake) => (fake.formatsResult = [GENERIC_FORMAT, PACKAGE_FORMAT]));
    expect(location.origin).not.toBe('https://quader.example.net');
    expect(addressValues(r.root)).toEqual([]);
    type(r.root.querySelector<HTMLTextAreaElement>('#f-description')!, 'Api');
    await settle(r.fixture);
    await openSection(r.fixture, r.root, 'API');
    await settle(r.fixture);
    expect(text(r.root)).toContain(
      "curl -X PATCH 'https://quader.example.net/api/v1/projects/web/repositories/generic-local'",
    );
    expect(text(r.root)).not.toContain(location.origin);
  });

  it('R-REPO-002 R-UI-021 R-UI-066 a server without a configured base URL leaves the client address out instead of showing the page origin', async () => {
    const r = await renderScreen(RepositoryDetail, {
      inputs: { project: 'web', key: 'generic-local' },
      capabilities: ['repositories:read-config', 'repositories:write-config'],
      setup: (get) => {
        const fake = get(RepositoriesApi) as FakeRepositoriesApi;
        fake.baseUrlResult = null;
      },
    });
    expect(text(r.root)).not.toContain(location.origin);
    expect(r.root.querySelector('quader-address-field')).toBeNull();
  });

  it('R-UI-021 the section API shows the stored document and the request that saves the change', async () => {
    const r = await open();
    type(r.root.querySelector<HTMLTextAreaElement>('#f-description')!, 'Api');
    await settle(r.fixture);
    await openSection(r.fixture, r.root, 'API');
    await settle(r.fixture);
    expect(text(r.root)).toContain('curl -X PATCH');
    expect(text(r.root)).toContain('If-Match: "1"');
  });

  describe('Debian', () => {
    const withDeb = (cls: 'local' | 'remote' | 'virtual', deb?: Record<string, unknown>) => (fake: FakeRepositoriesApi) => {
      fake.formatsResult = [GENERIC_FORMAT, DEB_FORMAT];
      fake.repositories.push({
        key: `deb-${cls}`,
        project: 'web',
        class: cls,
        format: 'deb',
        description: 'APT',
        ...(deb ? { deb: deb as never } : {}),
      });
    };
    const openDeb = (cls: 'local' | 'remote' | 'virtual', deb?: Record<string, unknown>, capabilities?: string[]) =>
      renderScreen(RepositoryDetail, {
        inputs: { project: 'web', key: `deb-${cls}` },
        ...(capabilities ? { capabilities } : {}),
        setup: (get) => withDeb(cls, deb)(get(RepositoriesApi) as FakeRepositoriesApi),
      });
    const tabNames = (r: Awaited<ReturnType<typeof openDeb>>) =>
      sectionHeadings(r.root);

    it('R-DEB-030 R-UI-021 a local repository has the sections of its format settings, signing keys, indexes and the APT guide', async () => {
      const r = await openDeb('local');
      expect(tabNames(r)).toEqual(['Debian (APT) settings', 'Access details', 'Malware scan', 'Signatures', 'Download links', 'Administrator notes', 'Signing keys', 'Indexes', 'Service accounts', 'Details', 'API', 'Advanced']);
      await openTab(r, 'Signing keys');
      expect(text(r.root)).toContain('3F2A 0000');
      const api = r.get(DebApi) as FakeDebApi;
      expect(api.calls).toContain('signingKeys');
      expect(api.calls).not.toContain('indexState');
      await openTab(r, 'Index');
      expect(api.calls).toContain('indexState');
      expect(text(r.root)).toContain('Waiting for a build');
      await expectAccessible(r.root);
    });

    it('R-DEB-015 someone who may write repositories can promote; a reader of the configuration sees the keys without the buttons', async () => {
      const writer = await openDeb('virtual');
      await openTab(writer, 'Signing keys');
      expect(text(writer.root)).toContain('Generate a next key');
      const reader = await openDeb('virtual', undefined, ['repositories:read-config']);
      await openTab(reader, 'Signing keys');
      expect(text(reader.root)).toContain('3F2A 0000');
      expect(text(reader.root)).not.toContain('Generate a next key');
      await openTab(reader, 'Index');
      expect(text(reader.root)).not.toContain('Rebuild all indexes');
    });

    it('R-DEB-020 a remote repository has no signing keys and no index state of its own and shows the fingerprints of its upstream keys read-only', async () => {
      const r = await openDeb('remote', {
        upstreamKeys: 'BLOCK',
        upstreamKeyFingerprints: ['AAAA000000000000000000000000000000001111', 'BBBB000000000000000000000000000000002222'],
      });
      const names = tabNames(r);
      expect(names).not.toContain('Signing keys');
      expect(names).not.toContain('Index');
      await openTab(r, 'Debian (APT)');
      const section = r.root.querySelector('[data-testid="deb-upstream-fingerprints"]')!;
      expect([...section.querySelectorAll('code')].map((c) => text(c))).toEqual([
        'AAAA 0000 0000 0000 0000 0000 0000 0000 0000 1111',
        'BBBB 0000 0000 0000 0000 0000 0000 0000 0000 2222',
      ]);
      expect(section.querySelector('input, textarea')).toBeNull();
      expect(r.root.querySelector<HTMLTextAreaElement>('#f-deb-upstreamKeys')?.value).toBe('BLOCK');
    });

    it('R-DEB-020 the read-only fingerprints are never written back: changing another setting patches that setting only', async () => {
      const r = await openDeb('remote', {
        upstreamKeys: 'BLOCK',
        upstreamKeyFingerprints: ['AAAA000000000000000000000000000000001111'],
      });
      await openTab(r, 'Mirror');
      const fake = r.get(RepositoriesApi) as FakeRepositoriesApi;
      const mirror = r.root.querySelector<HTMLInputElement>('input[name="remote-mode"][value="mirror"]')!;
      mirror.checked = true;
      mirror.dispatchEvent(new Event('change', { bubbles: true }));
      await settle(r.fixture);
      type(r.root.querySelector<HTMLTextAreaElement>('#f-deb-mirror-suites')!, 'stable');
      await settle(r.fixture);
      saveButton(r.root, 'Mirror').click();
      await settle(r.fixture);
      expect(fake.patches[0]?.patch).toEqual({ remote: { mode: 'mirror' }, deb: { mirror: { suites: ['stable'] } } });
    });
  });

  describe('Ansible Galaxy', () => {
    const withAnsible = (cls: 'local' | 'remote' | 'virtual', ansible?: Record<string, unknown>) => (fake: FakeRepositoriesApi) => {
      fake.formatsResult = [GENERIC_FORMAT, ANSIBLE_FORMAT];
      fake.repositories.push({
        key: `ansible-${cls}`,
        project: 'web',
        class: cls,
        format: 'ansible',
        description: 'Galaxy',
        ...(ansible ? { ansible: ansible as never } : {}),
      });
    };
    const openAnsible = (cls: 'local' | 'remote' | 'virtual', ansible?: Record<string, unknown>, capabilities?: string[]) =>
      renderScreen(RepositoryDetail, {
        inputs: { project: 'web', key: `ansible-${cls}` },
        ...(capabilities ? { capabilities } : {}),
        setup: (get) => withAnsible(cls, ansible)(get(RepositoriesApi) as FakeRepositoriesApi),
      });
    const tabNames = (r: Awaited<ReturnType<typeof openAnsible>>) =>
      sectionHeadings(r.root);

    it('R-ANS-026 a local repository has the tabs for its import tasks, its tombstones and the ansible-galaxy guide, loaded when opened', async () => {
      const r = await openAnsible('local');
      expect(tabNames(r)).toEqual(expect.arrayContaining(['Import tasks', 'Tombstones']));
      const api = r.get(AnsibleApi) as FakeAnsibleApi;
      expect(api.calls).toEqual([]);
      await openTab(r, 'Import tasks');
      expect(api.calls).toEqual(['importTasks']);
      expect(text(r.root)).toContain('acme.tools');
      await openTab(r, 'Tombstones');
      expect(api.calls).toContain('tombstones');
      await expectAccessible(r.root);
    });

    it('R-ANS-026 a virtual repository shows the import tasks of its deployment target and has no tombstones', async () => {
      const r = await openAnsible('virtual');
      expect(tabNames(r)).toContain('Import tasks');
      expect(tabNames(r)).not.toContain('Tombstones');
    });

    it('R-ANS-026 a remote repository has neither tab, and its hub settings are the format settings', async () => {
      const r = await openAnsible('remote', { authUrl: 'https://sso.example.com/token', clientId: 'cloud-services' });
      expect(tabNames(r)).not.toContain('Import tasks');
      expect(tabNames(r)).not.toContain('Tombstones');
      const settings = tabNames(r).find((n) => n.includes('Ansible Galaxy'))!;
      await openTab(r, settings);
      expect(r.root.querySelector<HTMLInputElement>('#f-ansible-authUrl')?.value).toBe('https://sso.example.com/token');
      expect(r.root.querySelector<HTMLInputElement>('#f-ansible-clientId')?.value).toBe('cloud-services');
    });

    it('R-ANS-012 someone who cannot write the configuration sees the tombstones without the clearing', async () => {
      const r = await openAnsible('local', undefined, ['repositories:read-config']);
      await openTab(r, 'Tombstones');
      expect(text(r.root)).toContain('acme.tools');
      expect([...r.root.querySelectorAll('button')].map((b) => text(b))).not.toContain('Clear');
    });
  });
});

describe('A repository that is gone leaves the remembered places (R-UI-167)', () => {
  /** The store of the fakes' signed-in principal, as the search and the side bar read it. */
  const STORE = 'quader.frequent.00000000-0000-0000-0000-000000000001';
  const remember = (...refs: string[]) =>
    browserStorage.set('local', STORE, JSON.stringify({ repositories: refs.map((id, i) => ({ id, visits: 1, last: 1000 - i })), projects: [] }));
  const remembered = (r: Awaited<ReturnType<typeof open>>) => r.get(FrequentPlaces).recentRepositories().map((v) => v.id);

  beforeEach(() => browserStorage.clear('local'));
  afterEach(() => browserStorage.clear('local'));

  it('R-UI-167 the deletion the person confirmed forgets the repository at once, also while its job runs', async () => {
    const r = await open((fake) => {
      remember('web/releases');
      fake.deleteResult = { done: false, job: { id: 'job-1', kind: 'repository-delete', state: 'queued', href: '/api/v1/system/jobs/job-1' } };
    });
    // Opening the page recorded the visit.
    expect(remembered(r)).toEqual(['web/generic-local', 'web/releases']);
    button(r.root, 'Delete repository').click();
    await settle(r.fixture);
    const dialog = r.root.ownerDocument.body.querySelector<HTMLElement>('[role="dialog"]')!;
    type(dialog.querySelector<HTMLInputElement>('#confirm-name')!, 'generic-local');
    await settle(r.fixture);
    button(dialog, 'Delete repository').click();
    await settle(r.fixture);
    expect((r.get(RepositoriesApi) as FakeRepositoriesApi).deleteCalls).toHaveLength(1);
    expect(remembered(r)).toEqual(['web/releases']);
  });

  it('R-UI-167 a move forgets the name the repository had', async () => {
    const r = await renderScreen(RepositoryDetail, {
      inputs: { project: 'web', key: 'generic-local' },
      setup: (get) => {
        (get(ProjectsApi) as FakeProjectsApi).projects = [sampleProject(), sampleProject({ key: 'platform', displayName: 'Platform' })];
      },
    });
    expect(remembered(r)).toEqual(['web/generic-local']);
    const target = r.root.querySelector<HTMLSelectElement>('select[name="moveTarget"]')!;
    target.value = 'platform';
    target.dispatchEvent(new Event('change'));
    await settle(r.fixture);
    button(r.root, 'Move repository').click();
    await settle(r.fixture);
    button(r.root.ownerDocument.body.querySelector<HTMLElement>('[role="dialog"]')!, 'Move repository').click();
    await settle(r.fixture);
    expect((r.get(RepositoriesApi) as FakeRepositoriesApi).moves).toEqual([{ ref: 'web/generic-local', project: 'platform' }]);
    expect(remembered(r)).toEqual([]);
  });

  it('R-UI-167 a repository its settings page does not find is forgotten, and one that is being deleted is not recorded', async () => {
    const missing = await open(() => remember('web/missing', 'web/releases'), 'missing');
    expect(text(missing.root)).not.toContain('Save changes');
    expect(remembered(missing)).toEqual(['web/releases']);

    browserStorage.clear('local');
    const deleting = await open((fake) => {
      fake.repositories[0] = { ...fake.repositories[0]!, lifecycle: 'deleting' };
    });
    expect(text(deleting.root)).toContain('This repository is being deleted.');
    expect(remembered(deleting)).toEqual([]);
  });
});
