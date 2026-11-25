import { TestBed } from '@angular/core/testing';
import { describe, it } from 'vitest';
import { LimitsApi } from '../../api/limits-api';
import { QuaderDialog } from '../../shared/ui/dialog';
import { app, dialogPanel, expectState, failAlways, failNext, fill, openDialog, serverFailure, submit } from '../../testing/states';
import { holdCalls } from '../../testing/screen';
import { LimitModeDialog } from './limit-mode';
import { LimitMaximumDialog, MaximumDialogData } from './maximum-dialog';
import { LimitRuleDialog } from './rule-dialog';

/** The projects **Add maximum** offers, with the long names an installation has. */
const projects = [
  { key: 'platform-engineering-release-candidates', displayName: 'Platform engineering release candidates' },
  { key: 'kubernetes-infrastrukturbetrieb', displayName: 'Kubernetes-Infrastrukturbetriebsteam Frankfurt' },
  { key: 'web', displayName: 'Web platform' },
];

/** The maximum dialog as limits-projects-tab.ts opens it; `change` reads the stored maximum of `web` first, as the tab does. */
async function maximum(change: boolean, fail = false): Promise<HTMLElement> {
  const get = await app();
  const api = get(LimitsApi);
  const data: MaximumDialogData = change ? { project: 'web', maximum: await api.maximum('web') } : { maximum: null, projects };
  if (fail) failNext(api, 'putMaximum', serverFailure());
  TestBed.inject(QuaderDialog).open(LimitMaximumDialog, { data, maxWidth: '44rem', width: '44rem' });
  return dialogPanel();
}

/** Add rule of the instance's rules, as limit-rules.ts opens it. */
const addRule = () =>
  openDialog(LimitRuleDialog, { maxWidth: '44rem', width: '44rem', data: { place: { kind: 'instance' }, capped: false } });

describe('dialogs of the download limits in a browser (R-UI-177, R-UI-181, R-UI-182, R-UI-042, R-UI-173, R-UI-175)', () => {
  it("Add maximum of a project, as it opens", async () => {
    await expectState('limits-maximum-add', () => maximum(false));
  });

  it("Add maximum, submitted without a project: the sentence under the field", async () => {
    await expectState('limits-maximum-add-invalid', async () => {
      const panel = await maximum(false);
      await fill(panel, 'Profile', 'technical');
      await submit(panel);
    });
  });

  it("a project's maximum, changed", async () => {
    await expectState('limits-maximum-change', () => maximum(true));
  });

  it("a project's maximum, whose save failed: the error notice with its request ID", async () => {
    await expectState('limits-maximum-failed', async () => submit(await maximum(true, true)));
  });

  it('Add rule while its profiles load: the select held, the spinner inside it (R-UI-217)', async () => {
    await expectState('limits-rule-profiles-loading', () =>
      openDialog(LimitRuleDialog, {
        maxWidth: '44rem',
        width: '44rem',
        data: { place: { kind: 'instance' }, capped: false },
        setup: (get) => void holdCalls(get(LimitsApi), 'profiles'),
      }),
    );
  });

  it('Add rule, submitted without the user it is for: the sentence under the field', async () => {
    await expectState('limits-rule-invalid', async () => {
      const { panel } = await addRule();
      await fill(panel, 'Profile', 'technical');
      await submit(panel);
    });
  });

  it("a rule's change, whose save failed: the error notice with its request ID", async () => {
    await expectState('limits-rule-failed', async () => {
      const get = await app();
      const api = get(LimitsApi);
      const rule = (await api.rules({ kind: 'instance' }))[0];
      if (!rule) throw new Error('the fake has no rule of the instance');
      failNext(api, 'rule', serverFailure());
      TestBed.inject(QuaderDialog).open(LimitRuleDialog, {
        maxWidth: '44rem',
        width: '44rem',
        data: { place: { kind: 'instance' }, rule, capped: false },
      });
      await submit(await dialogPanel());
    });
  });

  it('Switch to Enforce, whose preview failed: the error notice with its request ID and Retry', async () => {
    await expectState('limits-mode-failed', async () => {
      const get = await app({ setup: (g) => failAlways(g(LimitsApi), 'modePreview', serverFailure()) });
      const settings = await get(LimitsApi).settings();
      TestBed.inject(QuaderDialog).open(LimitModeDialog, { data: { settings, to: 'enforce' }, maxWidth: '40rem' });
      await dialogPanel();
    });
  });
});
