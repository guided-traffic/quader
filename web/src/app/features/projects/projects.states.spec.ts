import { describe, it } from 'vitest';
import { ProjectsApi } from '../../api/projects-api';
import { FakeProjectsApi } from '../../api/testing/fakes-projects';
import { expectState, failNext, fill, openDialog, serverFailure, submit, until } from '../../testing/states';
import { ProjectDialog } from './project-dialog';
import { ProjectMemberDialog } from './project-member-dialog';

/** New project, as projects-home.ts opens it. */
const newProject = () => openDialog(ProjectDialog, { maxWidth: '40rem' });

/** Add member, as project-members.ts opens it, over a project whose lookup knows people with long names. */
async function addMember() {
  const state = await openDialog(ProjectMemberDialog, {
    width: '32rem',
    data: { project: 'web', exclude: [] },
    setup: (get) => {
      const projects = get(ProjectsApi) as FakeProjectsApi;
      projects.principals.push(
        { kind: 'user', name: 'priya.ramanathan-subramaniam', principal: 'user:priya.ramanathan-subramaniam', displayName: 'Priya Ramanathan-Subramaniam' },
        { kind: 'group', name: 'platform-engineering-release-managers', principal: 'group:platform-engineering-release-managers', displayName: 'Platform engineering release managers' },
        { kind: 'user', name: 'pr.bereitstellungsverantwortliche', principal: 'user:pr.bereitstellungsverantwortliche', displayName: 'Bereitstellungsverantwortliche Plattform' },
      );
    },
  });
  await fill(state.panel, 'User or group', 'pr');
  await until(() => document.querySelector('quader-principal-field [role="listbox"][aria-busy="true"]') === null, 'the lookup');
  return state;
}

describe('project dialogs in a browser (R-UI-041, R-UI-042, R-UI-150, R-UI-173, R-UI-175)', () => {
  it('New project, as it opens', async () => {
    await expectState('project-new', newProject);
  });

  it('New project, submitted empty: the key and the display name say what is wrong under their fields', async () => {
    await expectState('project-new-invalid', async () => {
      const { panel } = await newProject();
      await submit(panel);
    });
  });

  it('New project, refused by the server: the error notice with its request ID', async () => {
    await expectState('project-new-failed', async () => {
      const { panel, get } = await newProject();
      failNext(get(ProjectsApi), 'create', serverFailure());
      await fill(panel, 'Key', 'platform-engineering-rc');
      await fill(panel, 'Display name', 'Platform engineering release candidates');
      await submit(panel);
    });
  });

  it('Add member, with the matches of the person search open', async () => {
    await expectState('project-member-search', addMember);
  });

  it('Add member, with a member chosen and the role and its hint shown', async () => {
    await expectState('project-member-chosen', async () => {
      await addMember();
      const option = document.querySelector<HTMLElement>('quader-principal-field [role="option"]');
      if (!option) throw new Error('no match');
      option.click();
      await until(() => document.querySelector('quader-select-field') !== null, 'the role field');
    });
  });
});
