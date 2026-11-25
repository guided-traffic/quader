import { describe, it } from 'vitest';
import { IdentityApi } from '../../api/identity-api';
import { TechnicalTokensApi } from '../../api/technical-tokens-api';
import { sampleTechnicalToken } from '../../api/testing/access-fakes';
import {
  LONG_TEXT,
  expectState,
  failAlways,
  failNext,
  fieldRefusal,
  fill,
  openDialog,
  serverFailure,
  submit,
} from '../../testing/states';
import { GroupCreateDialog } from '../groups/group-create-dialog';
import { ServiceAccountCreateDialog } from '../service-accounts/service-account-list';
import { TechnicalTokenDescriptionDialog } from '../service-accounts/technical-token-description-dialog';
import { UserCreateDialog } from './user-create-dialog';

// The dialogs of the administration of people and accounts, opened as user-list.ts, group-list.ts, service-account-list.ts,
// repository-service-accounts.ts and technical-tokens.ts open them.
const newUser = () => openDialog(UserCreateDialog, { maxWidth: '36rem' });
const newGroup = () => openDialog(GroupCreateDialog, { maxWidth: '36rem' });
const newRepositoryAccount = () =>
  openDialog(ServiceAccountCreateDialog, { maxWidth: '36rem', data: { project: 'platform-engineering', repository: 'release-candidates-npm' } });

describe('dialogs of users, groups and service accounts in a browser (R-UI-025, R-UI-041, R-UI-042, R-UI-173, R-UI-175)', () => {
  it('New user, as it opens', async () => {
    await expectState('user-new', newUser);
  });

  it('New user, its fields refused by the server: each sentence under its field', async () => {
    await expectState('user-new-invalid', async () => {
      const { panel, get } = await newUser();
      failNext(
        get(IdentityApi),
        'createUser',
        fieldRefusal({
          '/username': 'The user name priya.ramanathan-subramaniam is taken by another user.',
          '/email': 'The address is not a valid email address: priya.ramanathan-subramaniam@platform-engineering',
        }),
      );
      await fill(panel, 'User name', 'priya.ramanathan-subramaniam');
      await fill(panel, 'Email', 'priya.ramanathan-subramaniam@platform-engineering');
      await submit(panel);
    });
  });

  it('New user, failed: the error notice with its request ID', async () => {
    await expectState('user-new-failed', async () => {
      const { panel, get } = await newUser();
      failNext(get(IdentityApi), 'createUser', serverFailure());
      await fill(panel, 'User name', 'priya.ramanathan-subramaniam');
      await submit(panel);
    });
  });

  it('New group, as it opens', async () => {
    await expectState('group-new', newGroup);
  });

  it('New group, its name refused by the server: the sentence under the field', async () => {
    await expectState('group-new-invalid', async () => {
      const { panel, get } = await newGroup();
      failNext(get(IdentityApi), 'createGroup', fieldRefusal({ '/name': 'The name holds characters a group name may not hold: upper-case letters and spaces.' }));
      await fill(panel, 'Name', 'Platform Engineering Release Managers');
      await submit(panel);
    });
  });

  it('New group, failed: the error notice with its request ID', async () => {
    await expectState('group-new-failed', async () => {
      const { panel, get } = await newGroup();
      failNext(get(IdentityApi), 'createGroup', serverFailure());
      await fill(panel, 'Name', 'platform-engineering-release-managers');
      await fill(panel, 'Description', LONG_TEXT);
      await submit(panel);
    });
  });

  it("New service account of a repository, its fields refused: the sentences under the name and the rights", async () => {
    await expectState('service-account-new-invalid', async () => {
      const { panel, get } = await newRepositoryAccount();
      failNext(
        get(IdentityApi),
        'createServiceAccount',
        fieldRefusal({
          '/name': 'The name ci-deploy-platform-engineering-release-candidates is longer than 63 characters.',
          '/actions': 'A service account of a repository needs at least one right.',
        }),
      );
      await fill(panel, 'Name', 'ci-deploy-platform-engineering-release-candidates');
      await submit(panel);
    });
  });

  it('New service account, failed: the error notice with its request ID', async () => {
    await expectState('service-account-new-failed', async () => {
      const { panel, get } = await openDialog(ServiceAccountCreateDialog, { maxWidth: '36rem' });
      failNext(get(IdentityApi), 'createServiceAccount', serverFailure());
      await fill(panel, 'Name', 'ci-deploy-platform-engineering');
      await submit(panel);
    });
  });

  it("Edit description of a technical token, whose read failed: the error notice with its request ID", async () => {
    await expectState('technical-token-description-failed', () =>
      openDialog(TechnicalTokenDescriptionDialog, {
        maxWidth: '32rem',
        data: {
          account: { name: 'ci-deploy-platform-engineering', project: 'platform-engineering', repository: null },
          token: sampleTechnicalToken({ name: 'release-candidates-publisher-2026', description: LONG_TEXT }),
        },
        setup: (get) => failAlways(get(TechnicalTokensApi), 'get', serverFailure()),
      }),
    );
  });
});
