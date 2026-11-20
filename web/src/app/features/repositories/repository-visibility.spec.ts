import { TestBed } from '@angular/core/testing';
import { Router } from '@angular/router';
import { ApiError } from '../../api/problem';
import { ProjectsApi } from '../../api/projects-api';
import { RepositoriesApi } from '../../api/repositories-api';
import { SessionApi } from '../../api/session-api';
import { FakeSessionApi } from '../../api/testing/fakes';
import { FakeRepositoriesApi, GENERIC_FORMAT, PACKAGE_FORMAT, sampleRepositoryDefaults } from '../../api/testing/fakes-catalog';
import { FakeProjectsApi, FakeVisibilityApi, sampleProject } from '../../api/testing/fakes-projects';
import { VisibilityApi } from '../../api/visibility-api';
import { button, expectAccessible, renderScreen, settle, text, type } from '../../testing/screen';
import { RepositoryDetail } from './repository-detail';
import { RepositoryCreate } from './repository-create';

type Level = 'private' | 'internal' | 'public';

async function openAccess(options: {
  project?: Level;
  artifacts?: Level;
  wiki?: Level;
  format?: 'npm' | 'generic';
  cls?: 'local' | 'remote';
  capabilities?: readonly string[];
  member?: boolean;
  restricted?: readonly Level[];
}) {
  const format = options.format ?? 'npm';
  const r = await renderScreen(RepositoryDetail, {
    inputs: { project: 'web', key: 'repo' },
    ...(options.capabilities ? { capabilities: options.capabilities } : {}),
    setup: (get) => {
      (get(ProjectsApi) as FakeProjectsApi).projects = [sampleProject({ visibility: options.project ?? 'internal' })];
      const fake = get(RepositoriesApi) as FakeRepositoriesApi;
      fake.formatsResult = [GENERIC_FORMAT, PACKAGE_FORMAT];
      fake.repositories = [
        {
          key: 'repo',
          project: 'web',
          class: options.cls ?? 'local',
          format,
          description: 'A repository',
          visibility: { artifacts: options.artifacts ?? 'internal', wiki: options.wiki ?? 'internal' },
        },
      ];
      if (options.member) {
        (get(SessionApi) as FakeSessionApi).meResult = {
          roles: [],
          capabilities: [],
          principals: ['user:alice'],
          projects: [{ key: 'web', role: 'owner' }],
        };
      }
      const visibility = get(VisibilityApi) as FakeVisibilityApi;
      visibility.stored.settings.restrictedLevels = [...(options.restricted ?? [])];
      visibility.mayChooseRestricted = !options.member;
    },
  });
  const group = (label: string) =>
    [...r.root.querySelectorAll<HTMLFieldSetElement>('quader-repository-visibility fieldset')].find(
      (f) => f.querySelector('legend')?.textContent?.trim() === label,
    );
  const radio = (label: string, level: string) =>
    [...(group(label)?.querySelectorAll<HTMLInputElement>('input[type="radio"]') ?? [])].find((i) =>
      i.labels?.[0]?.textContent?.includes(level),
    )!;
  const fake = r.get(RepositoriesApi) as FakeRepositoriesApi;
  /** The toggle of the fold **Wiki visibility**. */
  const fold = () => r.root.querySelector<HTMLButtonElement>('quader-repository-visibility quader-disclosure .inline-toggle');
  const foldOpen = () => fold()?.getAttribute('aria-expanded') === 'true';
  const save = async () => {
    button(r.root, 'Save changes (visibility)').click();
    await settle(r.fixture);
  };
  const choose = async (label: string, level: string) => {
    radio(label, level).click();
    await settle(r.fixture);
  };
  return { ...r, group, radio, fake, fold, foldOpen, save, choose };
}

/** The one group of the main view, and the group inside the fold, by their names. */
const LEVEL = 'Visibility level';
const WIKI = 'Wiki visibility';

describe('The visibility block of a repository (R-UI-055)', () => {
  it('R-UI-055 the main view shows one group, Visibility level, in place of "Require authentication"; the wiki\'s level is the closed fold Wiki visibility, and the opened block passes axe', async () => {
    const r = await openAccess({});
    expect(text(r.root)).not.toContain('Require authentication');
    const block = r.root.querySelector('quader-repository-visibility')!;
    // One visible name of a group: the wiki's legend is for assistive technology, its fold's toggle names it on the screen.
    expect([...block.querySelectorAll('legend:not(.visually-hidden)')].map((l) => text(l))).toEqual([LEVEL]);
    expect(r.radio(LEVEL, 'Internal').checked).toBe(true);
    expect(text(r.fold()!)).toContain(WIKI);
    // The wiki has the repository's level: the fold is closed.
    expect(r.foldOpen()).toBe(false);
    expect(block.querySelector(`#${r.fold()!.getAttribute('aria-controls')}`)!.classList.contains('open')).toBe(false);

    r.fold()!.click();
    await settle(r.fixture);
    expect(r.foldOpen()).toBe(true);
    expect(r.group(WIKI)!.querySelector('legend')!.classList.contains('visually-hidden')).toBe(true);
    expect(r.radio(WIKI, 'Internal').checked).toBe(true);
    expect(text(block)).toContain('The wiki is the README and the documentation of the packages.');
    await expectAccessible(r.root);
  });

  it('R-UI-055 a format without wiki content shows the one group and no fold', async () => {
    const r = await openAccess({ format: 'generic' });
    expect(r.group(LEVEL)).toBeTruthy();
    expect(r.group(WIKI)).toBeUndefined();
    expect(r.fold()).toBeNull();
  });

  it('R-UI-055 the fold is open by itself while the wiki\'s level differs from the repository\'s, also for a reader, who sees the levels read-only with the reason', async () => {
    const r = await openAccess({ project: 'public', artifacts: 'public', wiki: 'internal', capabilities: ['repositories:read-config'] });
    expect(r.foldOpen()).toBe(true);
    expect(r.radio(WIKI, 'Internal').checked).toBe(true);
    expect(r.root.querySelector('quader-repository-visibility button[type="submit"]')).toBeNull();
    expect(r.group(LEVEL)!.disabled).toBe(true);
    expect(r.group(WIKI)!.disabled).toBe(true);
    expect(text(r.root.querySelector('quader-repository-visibility')!)).toContain('Only the project\'s Owner');
    // The person may close it; it is their choice then.
    r.fold()!.click();
    await settle(r.fixture);
    expect(r.foldOpen()).toBe(false);
  });

  it('R-UI-055 an option above the project is disabled with the reason; the wiki is bounded by the repository\'s level', async () => {
    const r = await openAccess({});
    expect(r.radio(LEVEL, 'Public').disabled).toBe(true);
    expect(r.radio(WIKI, 'Public').disabled).toBe(true);
    expect(text(r.group(LEVEL)!)).toContain('The project is internal, so this repository cannot be public.');
    expect(r.radio(LEVEL, 'Private').disabled).toBe(false);

    await r.choose(LEVEL, 'Private');
    // The wiki goes down with the level, and nothing above the level can be chosen for it.
    expect(r.radio(WIKI, 'Private').checked).toBe(true);
    expect(r.radio(WIKI, 'Internal').disabled).toBe(true);
    expect(text(r.group(WIKI)!)).toContain('The wiki cannot be more open than the repository.');
  });

  it('R-UI-055 a wiki without a level of its own follows the visibility level: an Owner raises the level, both parts are sent, and the fold stays closed', async () => {
    const r = await openAccess({ project: 'public', member: true, capabilities: [] });
    await r.choose(LEVEL, 'Public');
    expect(r.radio(WIKI, 'Public').checked).toBe(true);
    expect(r.foldOpen()).toBe(false);
    expect(button(r.root, 'Save changes (visibility)').disabled).toBe(false);
    await r.save();
    expect(r.fake.patches).toHaveLength(1);
    expect(r.fake.patches[0]).toMatchObject({ key: 'web/repo', patch: { visibility: { artifacts: 'public', wiki: 'public' } }, etag: '"1"' });
    expect(r.fake.repositories[0]?.visibility).toEqual({ artifacts: 'public', wiki: 'public' });
    expect(button(r.root, 'Save changes (visibility)').disabled).toBe(true);
    expect(r.foldOpen()).toBe(false);
  });

  it('R-UI-055 a level chosen in the fold is the wiki\'s own: the visibility level no longer moves it, a closed fold says "Unsaved changes", and only the changed parts are sent', async () => {
    const r = await openAccess({ project: 'public' });
    r.fold()!.click();
    await settle(r.fixture);
    await r.choose(WIKI, 'Private');
    await r.choose(LEVEL, 'Public');
    expect(r.radio(WIKI, 'Private').checked).toBe(true);
    // Closed by the person with the choice inside: the toggle says so.
    r.fold()!.click();
    await settle(r.fixture);
    expect(text(r.fold()!)).toContain('Unsaved changes');
    await r.save();
    expect(r.fake.patches[0]?.patch).toEqual({ visibility: { artifacts: 'public', wiki: 'private' } });
    expect(text(r.fold()!)).not.toContain('Unsaved changes');

    // The wiki alone: one part.
    await r.choose(WIKI, 'Internal');
    await r.save();
    expect(r.fake.patches[1]?.patch).toEqual({ visibility: { wiki: 'internal' } });
  });

  it('R-UI-055 a wiki with its own level is lowered where the visibility level falls below it, gets it back when the level rises again, and both parts are sent', async () => {
    const r = await openAccess({ project: 'public', artifacts: 'public', wiki: 'internal' });
    await r.choose(LEVEL, 'Private');
    expect(r.radio(WIKI, 'Private').checked).toBe(true);
    await r.choose(LEVEL, 'Public');
    expect(r.radio(WIKI, 'Internal').checked).toBe(true);
    expect(button(r.root, 'Save changes (visibility)').disabled).toBe(true);

    await r.choose(LEVEL, 'Private');
    await r.save();
    expect(r.fake.patches[0]?.patch).toEqual({ visibility: { artifacts: 'private', wiki: 'private' } });
  });

  it('R-UI-055 what the server answers for a level stands under that level\'s group, the wiki\'s opens its fold, and an answer that names no level is the block\'s notice', async () => {
    const r = await openAccess({ project: 'public' });
    const block = r.root.querySelector('quader-repository-visibility')!;
    const refuse = (status: number, code: string, pointer?: string) => {
      r.fake.beforePatch = () => {
        const top = pointer && status === 422 ? 'validation_failed' : code;
        throw new ApiError(status, { code: top, ...(pointer ? { errors: [{ pointer, code }] } : {}) }, 'req-1');
      };
    };
    const errorOf = (label: string) => r.group(label)!.querySelector('.field-error');

    // The wiki follows the level up behind the closed fold; the server refuses the wiki's part.
    await r.choose(LEVEL, 'Public');
    expect(r.foldOpen()).toBe(false);
    refuse(422, 'visibility_wiki_above_artifacts', '/visibility/wiki');
    await r.save();
    expect(r.foldOpen()).toBe(true);
    expect(text(errorOf(WIKI)!)).toBe('The wiki cannot be more open than the repository.');
    expect(r.group(WIKI)!.getAttribute('aria-describedby')).toBe(errorOf(WIKI)!.id);
    expect(errorOf(LEVEL)).toBeNull();
    expect(block.querySelector('quader-error-notice')).toBeNull();

    refuse(403, 'visibility_level_restricted', '/visibility/artifacts');
    await r.save();
    expect(text(errorOf(LEVEL)!)).toContain('Only an administrator of Quader can choose this visibility level.');
    expect(errorOf(WIKI)).toBeNull();
    expect(block.querySelector('quader-error-notice')).toBeNull();

    refuse(409, 'public_not_allowed');
    await r.save();
    expect(errorOf(LEVEL)).toBeNull();
    expect(text(block.querySelector('quader-error-notice')!)).toContain('nothing can be public');
    await expectAccessible(r.root);
  });

  it('R-UI-055 the warning of the answer is shown at the block', async () => {
    const r = await openAccess({ project: 'public', cls: 'remote', format: 'npm' });
    await r.choose(LEVEL, 'Public');
    await r.save();
    expect(text(r.root.querySelector('quader-repository-visibility')!)).toContain(
      'Anyone can fill the cache of this remote repository from its upstream.',
    );
  });

  it('R-UI-141 an Owner sees a restricted level disabled with the reason, and keeps the level the repository has', async () => {
    const r = await openAccess({ project: 'public', artifacts: 'public', wiki: 'internal', member: true, capabilities: [], restricted: ['public'] });
    // The repository is public: the level stays selectable and says that it cannot be chosen again once it is left.
    expect(r.radio(LEVEL, 'Public').disabled).toBe(false);
    expect(text(r.group(LEVEL)!)).toContain('Restricted: kept while it is set; only an administrator can choose it again.');
    // The wiki is internal: public is a new choice, which only an administrator makes.
    expect(r.radio(WIKI, 'Public').disabled).toBe(true);
    expect(text(r.group(WIKI)!)).toContain('Only an administrator of Quader can choose this level.');
    await expectAccessible(r.root);
  });

  it('R-UI-141 the reason of the project comes first: an option above the project names the project, not the restriction', async () => {
    const r = await openAccess({ project: 'internal', member: true, capabilities: [], restricted: ['public'] });
    expect(text(r.group(LEVEL)!)).toContain('The project is internal, so this repository cannot be public.');
    expect(text(r.group(LEVEL)!)).not.toContain('Only an administrator');
  });

  it('R-UI-141 an administrator sees a restricted level enabled, with a note', async () => {
    const r = await openAccess({ project: 'public', restricted: ['public'] });
    expect(r.radio(LEVEL, 'Public').disabled).toBe(false);
    expect(text(r.group(LEVEL)!)).toContain('Restricted: only administrators can choose this level.');
  });

  it('R-UI-055 the repository header shows the artifacts level as an icon with its name', async () => {
    const r = await openAccess({ artifacts: 'private', wiki: 'private' });
    const icon = r.root.querySelector('.subject quader-visibility-icon [role="img"]')!;
    expect(icon.getAttribute('aria-label')).toBe('Private');
  });
});

describe('The visibility of a repository in the creation form (R-UI-055, R-UI-081, R-REPO-063)', () => {
  /** The form of a local npm repository whose defaults (R-REPO-063) answer `artifacts` as the visibility, or fail. */
  async function openForm(project: Level, artifacts: Level | 'unavailable', capabilities?: readonly string[], restricted: readonly Level[] = []) {
    const r = await renderScreen(RepositoryCreate, {
      inputs: { project: 'web', format: 'npm' },
      ...(capabilities ? { capabilities } : {}),
      setup: (get) => {
        const visibility = get(VisibilityApi) as FakeVisibilityApi;
        visibility.stored.settings.restrictedLevels = [...restricted];
        visibility.mayChooseRestricted = capabilities?.length !== 0;
        (get(ProjectsApi) as FakeProjectsApi).projects = [sampleProject({ visibility: project })];
        const fake = get(RepositoriesApi) as FakeRepositoriesApi;
        fake.formatsResult = [GENERIC_FORMAT, PACKAGE_FORMAT];
        if (artifacts === 'unavailable') {
          fake.defaultsError = new ApiError(503, { code: 'unavailable' }, 'req-1');
        } else {
          fake.defaultsFor = (_p, format, cls) => {
            const d = sampleRepositoryDefaults(PACKAGE_FORMAT, cls);
            return { ...d, repository: { ...d.repository, format, visibility: { artifacts, wiki: artifacts } } };
          };
        }
        if (capabilities?.length === 0) {
          (get(SessionApi) as FakeSessionApi).meResult = {
            roles: [],
            capabilities: [],
            principals: ['user:alice'],
            projects: [{ key: 'web', role: 'owner' }],
          };
        }
      },
    });
    const radios = () => [...r.root.querySelectorAll<HTMLInputElement>('quader-visibility-level input[type="radio"]')];
    const create = async () => {
      button(r.root, 'Create repository').click();
      await settle(r.fixture);
    };
    return { ...r, radios, create };
  }

  it('R-UI-081 R-REPO-063 the artifacts group is preset to the visibility of the defaults, and a level above the project is disabled', async () => {
    const a = await openForm('public', 'internal');
    expect(a.radios().map((x) => x.checked)).toEqual([false, true, false]);
    const b = await openForm('private', 'private');
    expect(b.radios().map((x) => x.checked)).toEqual([true, false, false]);
    expect(b.radios().map((x) => x.disabled)).toEqual([false, true, true]);
    await expectAccessible(b.root);
  });

  it('R-UI-055 R-UI-084 a create that keeps the preset leaves visibility out; another level is sent for artifacts and wiki', async () => {
    navigateSpy();
    const kept = await openForm('public', 'internal');
    await kept.create();
    const fake = kept.get(RepositoriesApi) as FakeRepositoriesApi;
    expect(fake.repositories.find((x) => x.key === 'npm-local')?.visibility).toBeUndefined();

    const r = await openForm('public', 'internal');
    r.radios()[2]!.click();
    await settle(r.fixture);
    await r.create();
    const other = r.get(RepositoriesApi) as FakeRepositoriesApi;
    expect(other.repositories.find((x) => x.key === 'npm-local')?.visibility).toEqual({ artifacts: 'public', wiki: 'public' });
  });

  it('R-REPO-063 an Owner without settings:read gets the visibility of the defaults, not Private', async () => {
    const r = await openForm('internal', 'internal', []);
    expect(r.radios().map((x) => x.checked)).toEqual([false, true, false]);
  });

  it('R-UI-141 an Owner sees a restricted level disabled in the creation form, with no step and no field more', async () => {
    const r = await openForm('public', 'internal', [], ['public']);
    expect(r.radios().map((x) => x.disabled)).toEqual([false, false, true]);
    expect(text(r.root.querySelector('quader-visibility-level')!)).toContain('Only an administrator of Quader can choose this level.');
    expect(r.root.querySelectorAll('quader-visibility-level')).toHaveLength(1);
  });

  it('R-UI-141 an administrator chooses the restricted level, marked with a note', async () => {
    const r = await openForm('public', 'internal', undefined, ['public']);
    expect(r.radios().map((x) => x.disabled)).toEqual([false, false, false]);
    expect(text(r.root.querySelector('quader-visibility-level')!)).toContain('Restricted: only administrators can choose this level.');
  });

  it('R-UI-081 without the defaults the field is left out and the server applies its default', async () => {
    navigateSpy();
    const r = await openForm('public', 'unavailable');
    expect(r.root.querySelector('quader-visibility-level')).toBeNull();
    expect(r.root.querySelector<HTMLInputElement>('#f-key')!.value).toBe('');
    type(r.root.querySelector<HTMLInputElement>('#f-key')!, 'owner-repo');
    await settle(r.fixture);
    await r.create();
    const fake = r.get(RepositoriesApi) as FakeRepositoriesApi;
    expect(fake.repositories.find((x) => x.key === 'owner-repo')?.visibility).toBeUndefined();
  });
});

function navigateSpy(): void {
  vi.spyOn(TestBed.inject(Router), 'navigate').mockResolvedValue(true);
}
