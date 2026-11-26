import { describe, it } from 'vitest';
import { PackagesApi } from '../../api/packages-api';
import { ApiError } from '../../api/problem';
import { REQUEST_ID, click, expectState, failNext, fill, openDialog } from '../../testing/states';
import { DeprecationDialog, DeprecationDialogData } from './deprecation-dialog';

/**
 * The dialog of **Deprecate version** and **Deprecate collection** (R-API-085, Q-UI-1301), as the version's head opens it
 * (package-version.ts `openDeprecation`, width 36 rem): NuGet's with its reasons, a long message, the package to use instead and two
 * repositories to choose from; its refusal by the server, whole; and an Ansible collection's, whose message Quader's pages alone show.
 */
const nuget: DeprecationDialogData = {
  format: 'nuget',
  name: 'Acme.Logging.Abstractions.For.The.Customer.Portal.Release.Engineering',
  version: '1.0.0-preview.20261118.1',
  repositories: ['platform-engineering/nuget-internal-release-candidates', 'platform/nuget-internal'],
  current: {
    scope: 'version',
    reasons: ['Legacy', 'CriticalBugs'],
    message:
      'The 1.0 line writes the scopes of a log entry twice under load and gets no fixes. Move to 1.1.0 or later; see ' +
      'https://git.example.invalid/platform/logging/-/releases for the changes.',
    alternatePackage: { name: 'Acme.Logging.Abstractions', range: '[1.1.0, )' },
  },
};

const refusal = new ApiError(
  422,
  {
    code: 'validation_failed',
    status: 422,
    detail: 'The range [2.0,1.0] of the alternate package is no NuGet version range.',
    requestId: REQUEST_ID,
  },
  REQUEST_ID,
);

describe('the dialog of a deprecation in a browser (R-API-085, R-UI-042, R-UI-173, R-UI-175)', () => {
  it("NuGet's dialog with its reasons, a long message, the package to use instead and two repositories", async () => {
    await expectState('deprecation-dialog-nuget', () => openDialog(DeprecationDialog, { width: '36rem', data: nuget }));
  });

  it('its refusal by the server, whole, under the fields', async () => {
    await expectState('deprecation-dialog-refused', async () => {
      const { panel, get } = await openDialog(DeprecationDialog, { width: '36rem', data: { ...nuget, current: null } });
      failNext(get(PackagesApi), 'setDeprecation', refusal);
      await fill(panel, 'Message', 'Moved.');
      await click(panel, 'Deprecate');
    });
  });

  it("an Ansible collection's dialog: the whole collection, a message for Quader's pages", async () => {
    await expectState('deprecation-dialog-ansible', () =>
      openDialog(DeprecationDialog, {
        width: '36rem',
        data: { format: 'ansible', name: 'acme.legacy_tools', version: '1.1.0', repositories: ['platform/collections'], current: null },
      }),
    );
  });
});
