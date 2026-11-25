import { describe, it } from 'vitest';
import { expectState, openDialog } from '../../testing/states';
import { PolicyDiffDialog } from './policy-diff-dialog';

describe('the policy diff in a browser (R-UI-027, R-UI-173, R-UI-175)', () => {
  it('Save the policy?, with what the save changes', async () => {
    await expectState('policy-diff', () =>
      openDialog(PolicyDiffDialog, {
        maxWidth: '40rem',
        data: {
          name: 'platform-engineering-release-candidates-deployers',
          lines: [
            { change: 'changed', text: 'Description changed.' },
            { change: 'added', text: 'group:platform-engineering-release-managers gets deploy on platform-engineering/release-candidates-npm.' },
            { change: 'added', text: 'user:priya.ramanathan-subramaniam gets read, annotate and deploy on kubernetes-infrastrukturbetrieb/*.' },
            { change: 'removed', text: 'service-account:platform-engineering/ci-deploy-release-candidates loses delete on platform-engineering/release-candidates-oci.' },
            { change: 'removed', text: 'The repository pattern kubernetes-infrastrukturbetrieb/bereitstellungsverantwortlichkeitsuebertragung-* is removed.' },
          ],
        },
      }),
    );
  });
});
