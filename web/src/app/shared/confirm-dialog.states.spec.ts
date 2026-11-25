import { TestBed } from '@angular/core/testing';
import { describe, it } from 'vitest';
import { app, dialogPanel, expectState } from '../testing/states';
import { Confirm, ConfirmData } from './confirm-dialog';

/** Asks through the application's own service, as every call site does. */
async function ask(data: ConfirmData): Promise<void> {
  await app();
  void TestBed.inject(Confirm).ask(data);
  await dialogPanel();
}

describe('confirm dialog shapes in a browser (R-UI-011, R-UI-173, R-UI-175)', () => {
  it('asks for the name before a delete: type to confirm', async () => {
    await expectState('confirm-type-to-confirm', () =>
      ask({
        title: 'Delete the repository platform-engineering-release-candidates?',
        lines: [
          'The repository platform-engineering-release-candidates and its 1,284 packages are deleted for good, with every version, file and download count. Clients that install from it fail from now on.',
        ],
        confirmLabel: 'Delete repository',
        typeToConfirm: 'platform-engineering-release-candidates',
      }),
    );
  });

  it('offers a choice beside the confirmation', async () => {
    await expectState('confirm-choice', () =>
      ask({
        title: 'Delete the repository platform-engineering-release-candidates?',
        lines: ['Its 1,284 packages are deleted for good.'],
        confirmLabel: 'Delete repository',
        typeToConfirm: 'platform-engineering-release-candidates',
        choice: 'Also detach it from the virtual repositories that include it (platform-engineering-all, platform-engineering-virtual)',
      }),
    );
  });
});
