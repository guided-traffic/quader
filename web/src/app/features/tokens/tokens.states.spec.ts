import { describe, it } from 'vitest';
import { BastionsApi } from '../../api/bastions-api';
import { CiApi } from '../../api/ci-api';
import { sampleIssuance } from '../../api/testing/fakes-ci';
import { TokenSecret } from '../../shared/access/token-secret';
import { BastionCreateDialog, BastionTokenDialog } from '../bastions/bastion-dialogs';
import { IssuanceDetailDialog } from '../ci/issuance-list';
import { LONG_TEXT, expectState, failAlways, failNext, fill, openDialog, openInPage, serverFailure, submit } from '../../testing/states';
import { RotateDialog } from './token-manager';

// The dialogs and the shown-once secret of tokens: a person's (token-manager.ts), a Bastion's (bastion-list.ts, bastion-card.ts)
// and a CI job's issued token (issuance-list.ts), opened as those screens open them.
const newBastion = () => openDialog(BastionCreateDialog, { maxWidth: '36rem' });
const bastionToken = () =>
  openDialog(BastionTokenDialog, { maxWidth: '36rem', data: { name: 'eu-central-airgap-mirror' } });

/** An issued CI token with the long values a real one has: a nested group's path, a branch, a user agent. */
const issuance = sampleIssuance({
  issuer: 'gitlab-platform-engineering-eu-central',
  mappingNames: ['platform-engineering-release-candidates', 'platform-engineering-nightly-deploy'],
  projectPath: 'platform-engineering/release-tooling/kubernetes-infrastructure-operators',
  ref: 'feature/bereitstellungsverantwortlichkeitsuebertragung-for-the-release-pipeline',
  refType: 'branch',
  userLogin: 'priya.ramanathan-subramaniam',
  userAgent: 'quader-ci-exchange/0.48.2 (linux; amd64) gitlab-runner/18.6.0',
  grantedScope: 'read deploy:platform-engineering/release-candidates-npm deploy:platform-engineering/release-candidates-oci',
});

describe('token dialogs and the shown-once secret in a browser (R-UI-029, R-UI-035, R-UI-042, R-UI-043, R-UI-173, R-UI-175)', () => {
  it('Rotate this token?, as it opens', async () => {
    await expectState('token-rotate', () => openDialog(RotateDialog, { maxWidth: '32rem' }));
  });

  it("a new token's secret, shown once in the page", async () => {
    await expectState('token-secret', () =>
      openInPage(TokenSecret, {
        value: 'qdr_pat_7Hq2mV9xKc4LpW8sZr1TnB6yFd3GjE5aUo0iXwQeR2tY8uP4sDf1Gh',
        description: LONG_TEXT,
      }),
    );
  });

  it('New Bastion, as it opens', async () => {
    await expectState('bastion-new', newBastion);
  });

  it('New Bastion, with a name it refuses: the sentence under the field', async () => {
    await expectState('bastion-new-invalid', async () => {
      const { panel } = await newBastion();
      await fill(panel, 'Name', 'EU-Central_Airgap-Mirror-Frankfurt');
      await submit(panel);
    });
  });

  it('New Bastion, failed: the error notice with its request ID', async () => {
    await expectState('bastion-new-failed', async () => {
      const { panel, get } = await newBastion();
      failNext(get(BastionsApi), 'create', serverFailure());
      await fill(panel, 'Name', 'eu-central-airgap');
      await fill(panel, 'Description', LONG_TEXT);
      await submit(panel);
    });
  });

  it('Issue a token for the Bastion, as it opens', async () => {
    await expectState('bastion-token', bastionToken);
  });

  it('Issue a token for the Bastion, with a lifetime it refuses: the sentence under the field', async () => {
    await expectState('bastion-token-invalid', async () => {
      const { panel } = await bastionToken();
      await fill(panel, 'Lifetime in days', '0');
      await submit(panel);
    });
  });

  it('Issue a token for the Bastion, failed: the error notice with its request ID', async () => {
    await expectState('bastion-token-failed', async () => {
      const { panel, get } = await bastionToken();
      failNext(get(BastionsApi), 'createToken', serverFailure());
      await fill(panel, 'Description', LONG_TEXT);
      await submit(panel);
    });
  });

  it("an issued CI token's details, with its usage", async () => {
    await expectState('ci-issuance', () =>
      openDialog(IssuanceDetailDialog, { maxWidth: '44rem', width: '44rem', data: { issuance, withUsage: true } }),
    );
  });

  it("an issued CI token's details, whose usage failed to load: the error notice with its request ID", async () => {
    await expectState('ci-issuance-failed', () =>
      openDialog(IssuanceDetailDialog, {
        maxWidth: '44rem',
        width: '44rem',
        data: { issuance, withUsage: true },
        setup: (get) => failAlways(get(CiApi), 'getIssuance', serverFailure()),
      }),
    );
  });
});
