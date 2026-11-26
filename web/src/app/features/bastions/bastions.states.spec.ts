import { describe, it } from 'vitest';
import { Bastion, BastionsApi } from '../../api/bastions-api';
import { ApiError } from '../../api/problem';
import { LimitsApi } from '../../api/limits-api';
import { sampleBastion } from '../../api/testing/fakes-bastions';
import { FakeLimitsApi, sampleBastionProfiles, sampleProfiles } from '../../api/testing/fakes-limits';
import { REQUEST_ID, expectState, failAlways, failNext, openDialog, serverFailure, until } from '../../testing/states';
import { LimitModeDialog, ModeDialogData } from '../limits/limit-mode';
import { BastionPresetDialog, PresetDialogData } from './bastion-preset-dialog';

/** A registration in Audit with a changed flood figure: the preset's dialog names a change in each of its three groups. */
function tuned(): Bastion {
  const b = sampleBastion();
  return {
    ...b,
    limits: { ...b.limits, mode: 'audit', flood: { ...b.limits.flood, anonymous: { ...b.limits.flood.anonymous, requestsPerSecond: 80 } } },
  };
}

/** The preset's dialog as bastion-limits.ts opens it, for Partner network, once it read the place's rules. */
async function preset(fail = false): Promise<HTMLElement> {
  const { panel, get } = await openDialog<BastionPresetDialog, PresetDialogData>(BastionPresetDialog, {
    maxWidth: '40rem',
    data: { bastion: tuned(), preset: 'partnerNetwork' },
    setup: (g) => void ((g(LimitsApi) as FakeLimitsApi).profileList = [...sampleProfiles(), ...sampleBastionProfiles()]),
  });
  await until(() => panel.querySelector('[data-testid="preset-kinds"]') !== null, 'the changes of the preset');
  if (fail) {
    failNext(get(BastionsApi), 'configure', serverFailure());
    panel.querySelector<HTMLButtonElement>('.dialog-actions .btn-confirm')!.click();
    await until(() => panel.querySelector('quader-error-notice') !== null, 'the refusal');
  }
  return panel;
}

describe("dialogs of a Bastion's limits in a browser (R-UI-240, R-UI-042, R-UI-216)", () => {
  it('the preset Partner network, naming each kind rule, flood figure and the mode it replaces', async () => {
    await expectState('bastion-preset', () => preset());
  });

  it("the preset's apply, refused: the error notice with its request ID", async () => {
    await expectState('bastion-preset-failed', () => preset(true));
  });

  it("the preset's dialog whose read of the rules failed: the notice with Retry", async () => {
    await expectState('bastion-preset-read-failed', async () => {
      await openDialog<BastionPresetDialog, PresetDialogData>(BastionPresetDialog, {
        maxWidth: '40rem',
        data: { bastion: tuned(), preset: 'internal' },
        setup: (g) => failAlways(g(LimitsApi), 'rules', serverFailure()),
      });
    });
  });

  it("a Bastion's switch to Audit refused for a sign-in that is not recent", async () => {
    await expectState('bastion-mode-reauth', async () => {
      const send = (): Promise<never> => Promise.reject(new ApiError(401, { code: 'reauthentication_required', status: 401, requestId: REQUEST_ID }, REQUEST_ID));
      const { panel } = await openDialog<LimitModeDialog, ModeDialogData>(LimitModeDialog, {
        maxWidth: '40rem',
        data: { to: 'audit', bastion: { name: 'dmz-frankfurt-edge', send } },
      });
      panel.querySelector<HTMLButtonElement>('.dialog-actions .btn-confirm')!.click();
      await until(() => panel.querySelector('quader-error-notice') !== null, 'the refusal');
    });
  });
});
