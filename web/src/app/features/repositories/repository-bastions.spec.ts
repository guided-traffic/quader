import { ApiError } from '../../api/problem';
import { ProjectBastion, ProjectsApi } from '../../api/projects-api';
import { Repository, RepositoriesApi } from '../../api/repositories-api';
import { SessionApi } from '../../api/session-api';
import { FakeSessionApi } from '../../api/testing/fakes';
import { FakeRepositoriesApi, GENERIC_FORMAT, PACKAGE_FORMAT } from '../../api/testing/fakes-catalog';
import { FakeProjectsApi, sampleProject } from '../../api/testing/fakes-projects';
import { button, expectAccessible, renderScreen, settle, text } from '../../testing/screen';
import { RepositoryDetail } from './repository-detail';

const artifacts: ProjectBastion = { name: 'artifacts', disabled: false, webInterface: true, addresses: ['https://pkgs.example.com'] };
const partners: ProjectBastion = { name: 'partners', disabled: false, webInterface: false, addresses: [] };

type Level = 'private' | 'internal' | 'public';

/** The settings page of web/repo with the Bastions that allow the project and the repositories of the project. */
async function open(options: {
  offered?: ProjectBastion[];
  cls?: 'local' | 'remote' | 'virtual';
  level?: Level;
  bastions?: Repository['bastions'];
  members?: { key: string; bastions: string[] }[];
  /** A reader of the configuration who is no Owner (capabilities without repositories:write-config). */
  reader?: boolean;
}) {
  const r = await renderScreen(RepositoryDetail, {
    inputs: { project: 'web', key: 'repo' },
    ...(options.reader ? { capabilities: ['repositories:read-config'] } : {}),
    setup: (get) => {
      const projects = get(ProjectsApi) as FakeProjectsApi;
      projects.projects = [sampleProject({ visibility: 'public' })];
      projects.bastionOffers = { web: options.offered ?? [] };
      const fake = get(RepositoriesApi) as FakeRepositoriesApi;
      fake.formatsResult = [GENERIC_FORMAT, PACKAGE_FORMAT];
      const level = options.level ?? 'internal';
      fake.repositories = [
        {
          key: 'repo',
          project: 'web',
          class: options.cls ?? 'local',
          format: 'npm',
          description: 'A repository',
          visibility: { artifacts: level, wiki: level },
          bastions: options.bastions ?? [],
          ...(options.cls === 'virtual' ? { virtual: { members: (options.members ?? []).map((m) => m.key) } } : {}),
        },
        ...(options.members ?? []).map((m) => ({
          key: m.key,
          project: 'web',
          class: 'local' as const,
          format: 'npm',
          visibility: { artifacts: level, wiki: level },
          bastions: m.bastions,
        })),
      ];
      if (options.reader) {
        (get(SessionApi) as FakeSessionApi).meResult = { capabilities: ['repositories:read-config'] };
      }
    },
  });
  const group = () => r.root.querySelector<HTMLElement>('quader-repository-visibility [data-testid="repository-bastions"]');
  const box = (name: string) =>
    [...(group()?.querySelectorAll<HTMLInputElement>('input[type="checkbox"]') ?? [])].find((i) =>
      i.closest('quader-checkbox')?.textContent?.includes(name),
    )!;
  const tick = async (name: string) => {
    box(name).click();
    await settle(r.fixture);
  };
  const save = async () => {
    button(r.root, 'Save changes (visibility)').click();
    await settle(r.fixture);
  };
  return { ...r, fake: r.get(RepositoriesApi) as FakeRepositoriesApi, group, box, tick, save };
}

describe('the group Bastions of a repository\'s Visibility (R-UI-271, R-UI-055)', () => {
  it('R-UI-271 is absent while no Bastion allows the project', async () => {
    const r = await open({});
    expect(r.group()).toBeNull();
    expect(r.root.querySelector('quader-repository-visibility')).not.toBeNull();
  });

  it('R-UI-271 offers each Bastion with its host in grey and saves the choice as the member bastions alone, and passes axe', async () => {
    const r = await open({ offered: [artifacts, partners] });
    expect([...r.group()!.querySelectorAll('.host')].map((h) => h.textContent)).toEqual(['pkgs.example.com']);
    expect(r.group()!.querySelector('legend')?.textContent).toContain('Bastions');
    expect(r.group()!.querySelector('quader-help')).not.toBeNull();
    expect(button(r.root, 'Save changes (visibility)').disabled).toBe(true);
    await r.tick('artifacts');
    await r.save();
    expect(r.fake.patches.at(-1)?.patch).toEqual({ bastions: ['artifacts'] });
    expect(r.box('artifacts').checked).toBe(true);
    await expectAccessible(r.root.querySelector('quader-repository-visibility')!);
  });

  it('R-UI-271 says under a remote that is Public what the Bastion\'s web interface lists, and not while it is Internal', async () => {
    const pub = await open({ offered: [artifacts, partners], cls: 'remote', level: 'public', bastions: ['artifacts', 'partners'] });
    expect(text(pub.group()!)).toContain('Its web interface also lists every package your builds fetched through this remote.');
    // partners' web interface is off: one line, under artifacts only.
    expect(pub.group()!.querySelectorAll('.note').length).toBe(1);
    const internal = await open({ offered: [artifacts], cls: 'remote', level: 'internal', bastions: ['artifacts'] });
    expect(internal.group()!.querySelector('.note')).toBeNull();
  });

  it('R-UI-271 warns under a virtual which members the checked Bastion does not distribute', async () => {
    const r = await open({
      offered: [artifacts],
      cls: 'virtual',
      bastions: ['artifacts'],
      members: [
        { key: 'npm-internal', bastions: ['artifacts'] },
        { key: 'npm-cache', bastions: [] },
      ],
    });
    expect(text(r.group()!)).toContain('Through artifacts it serves only members distributed there too; not: npm-cache.');
  });

  it('R-UI-271 shows a reader who does not choose the names as a sentence, and the head\'s mark', async () => {
    const r = await open({ reader: true, offered: [artifacts], bastions: [{ name: 'artifacts', addresses: ['https://pkgs.example.com'], disabled: false }] });
    expect(r.group()!.querySelector('input')).toBeNull();
    expect(text(r.group()!)).toBe('Distributed through artifacts.');
    expect(r.root.querySelector('quader-page-header [data-testid="distribution-mark"]')?.getAttribute('aria-label')).toBe(
      'Also distributed through the Bastion artifacts (pkgs.example.com)',
    );
  });

  it('R-UI-271 shows the server\'s bastion_not_allowed under the group', async () => {
    const r = await open({ offered: [artifacts] });
    r.fake.beforePatch = () => {
      r.fake.beforePatch = null;
      throw new ApiError(
        422,
        { code: 'validation_failed', errors: [{ pointer: '/bastions/0', code: 'bastion_not_allowed', detail: 'the Bastion does not allow' }] },
        null,
      );
    };
    await r.tick('artifacts');
    await r.save();
    expect(r.group()!.querySelector('.field-error')?.textContent).toBe(
      "The Bastion does not allow this project; an administrator allows it on the Bastion's card.",
    );
    expect(r.root.querySelector('quader-repository-visibility quader-error-notice')).toBeNull();
  });
});
