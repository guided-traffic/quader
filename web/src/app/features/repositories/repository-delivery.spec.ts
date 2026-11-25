import { Router } from '@angular/router';
import { vi } from 'vitest';
import { DeliveryRulesApi, DeliveryRulesSummary } from '../../api/delivery-rules-api';
import { RepositoriesApi, Repository } from '../../api/repositories-api';
import { SessionApi } from '../../api/session-api';
import { FakeRepositoriesApi, GENERIC_FORMAT, PACKAGE_FORMAT } from '../../api/testing/fakes-catalog';
import { FakeSessionApi } from '../../api/testing/fakes';
import { FakeDeliveryRulesApi } from '../../api/testing/fakes-upstream-catalogue';
import { expectAccessible, renderScreen, settle, text, waitUntil } from '../../testing/screen';
import { DeliverySave, DeliverySettings, RepositoryDeliverySection, deliveryOf } from './repository-delivery';
import { RepositoryDetail } from './repository-detail';

interface Options {
  readonly summary?: Partial<DeliveryRulesSummary>;
  readonly manage?: boolean;
}

async function open(inputs: Record<string, unknown>, url = '/projects/web/repositories/npmjs/settings', options: Options = {}) {
  const r = await renderScreen(RepositoryDeliverySection, {
    inputs: { project: 'web', repositoryKey: 'npmjs', ...inputs },
    setup: async (get) => {
      const rules = get(DeliveryRulesApi) as FakeDeliveryRulesApi;
      rules.summaryAnswer = { ...rules.summaryAnswer, ...options.summary };
      if (options.manage === false) {
        const session = get(SessionApi) as FakeSessionApi;
        session.meResult = { capabilities: ['repositories:write-config'], roles: [] };
        session.allow = (c) => c.action !== 'manage';
      }
      await get(Router).navigateByUrl(url);
    },
  });
  await settle(r.fixture);
  return { ...r, rules: r.get(DeliveryRulesApi) as FakeDeliveryRulesApi };
}

/** The dialog over the page: the CDK renders it outside the component. */
function dialog(): HTMLElement {
  return document.querySelector<HTMLElement>('.quader-dialog')!;
}

function button(root: Element, label: string): HTMLButtonElement {
  return [...root.querySelectorAll<HTMLButtonElement>('button')].find((b) => text(b).startsWith(label))!;
}

/** Picks the mode and submits the section. */
async function changeMode(r: Awaited<ReturnType<typeof open>>, index: number): Promise<void> {
  r.root.querySelectorAll<HTMLInputElement>('input[type="radio"]')[index]!.click();
  await settle(r.fixture);
  r.root.querySelector<HTMLButtonElement>('button[type="submit"]')!.click();
  await waitUntil(r.fixture, () => !!document.querySelector('[data-testid="delivery-mode-dialog"]') && !text(dialog()).includes('Reading'), 'the dialog to read the summary');
  await settle(r.fixture);
}

const UNRULED: Partial<DeliveryRulesSummary> = {
  rules: { allow: 2, block: 1, sections: 1 },
  unruled: { count: 3, names: ['express', 'left-pad', 'lodash'], counted: true },
};

describe('The settings section Delivery of a remote repository', () => {
  it('R-CUR-018 R-CUR-020 a closed section with the mode as two choices, the examination switch and its days, saved on its own', async () => {
    const r = await open({ stored: undefined, canChange: true }, '/projects/web/repositories/npmjs/settings#delivery');
    const { root, fixture } = r;
    // The tab Upstream's Change leads here with #delivery: the section is open.
    expect(root.querySelector('.toggle')?.getAttribute('aria-expanded')).toBe('true');
    const radios = [...root.querySelectorAll<HTMLInputElement>('input[type="radio"]')];
    expect(radios.map((x) => x.checked)).toEqual([true, false]);
    expect(text(root)).toContain('Everything except what is blocked');
    expect(text(root)).toContain('Only what is allowed');
    expect(text(root)).toContain('Fetch for examination when someone asks');
    expect(root.querySelector<HTMLInputElement>('#delivery-days')!.value).toBe('30');
    expect(root.querySelector('a')?.getAttribute('href')).toBe('/projects/web/repositories/npmjs/upstream');
    const save = vi.spyOn(fixture.componentInstance.save, 'emit');
    const days = root.querySelector<HTMLInputElement>('#delivery-days')!;
    days.value = '7';
    days.dispatchEvent(new Event('input'));
    await settle(fixture);
    expect(text(root)).toContain('Unsaved changes');
    // The days alone open no dialog.
    root.querySelector<HTMLButtonElement>('button[type="submit"]')!.click();
    await settle(fixture);
    expect(document.querySelector('[data-testid="delivery-mode-dialog"]')).toBeNull();
    expect(save.mock.calls).toEqual([[{ settings: { mode: 'allExceptBlocked', examineOnRequest: true, examinedRetentionDays: 7 } satisfies DeliverySettings, allowStored: false } satisfies DeliverySave]]);
    await expectAccessible(root);
  });

  it('R-CUR-025 a change to onlyAllowed names the stored packages the mode decides and offers, ticked, to allow them first', async () => {
    const r = await open({ stored: undefined, canChange: true }, undefined, { summary: UNRULED });
    const save = vi.spyOn(r.fixture.componentInstance.save, 'emit');
    await changeMode(r, 1);
    const d = dialog();
    expect(r.rules.calls).toContain('summary');
    expect(text(d)).toContain('Deliver only what is allowed?');
    expect(text(d.querySelector('[data-testid="delivery-mode-names"]')!)).toBe(
      'The repository stores 3 packages that no rule allows or blocks: express, left-pad, lodash. They are delivered today because of the mode and refused after the change.',
    );
    const offer = d.querySelector<HTMLInputElement>('input[type="checkbox"]')!;
    expect(offer.checked).toBe(true);
    expect(text(d)).toContain('Allow these 3 packages with every version, so that they stay delivered');
    await expectAccessible(d);
    button(d, 'Change the mode').click();
    await settle(r.fixture);
    expect(save.mock.calls.at(-1)).toEqual([{ settings: { mode: 'onlyAllowed', examineOnRequest: true, examinedRetentionDays: 30 }, allowStored: true }]);
  });

  it('R-CUR-025 the offer unticked saves the mode alone; cancelled, nothing is saved', async () => {
    const r = await open({ stored: undefined, canChange: true }, undefined, { summary: UNRULED });
    const save = vi.spyOn(r.fixture.componentInstance.save, 'emit');
    await changeMode(r, 1);
    dialog().querySelector<HTMLInputElement>('input[type="checkbox"]')!.click();
    button(dialog(), 'Change the mode').click();
    await settle(r.fixture);
    expect(save.mock.calls.at(-1)?.[0].allowStored).toBe(false);
    r.root.querySelector<HTMLButtonElement>('button[type="submit"]')!.click();
    await waitUntil(r.fixture, () => !!document.querySelector('[data-testid="delivery-mode-dialog"]'), 'the dialog');
    button(dialog(), 'Cancel').click();
    await settle(r.fixture);
    expect(save.mock.calls).toHaveLength(1);
  });

  it('R-CUR-025 R-CUR-005 a writer of the configuration without manage reads the list without the offer; beyond the room no offer either', async () => {
    const r = await open({ stored: undefined, canChange: true }, undefined, { summary: UNRULED, manage: false });
    await changeMode(r, 1);
    expect(dialog().querySelector('input[type="checkbox"]')).toBeNull();
    expect(text(dialog().querySelector('[data-testid="delivery-mode-no-offer"]')!)).toContain('Only those who manage the repository may allow them.');
    button(dialog(), 'Cancel').click();
    await settle(r.fixture);
    const full = await open({ stored: undefined, canChange: true }, undefined, {
      summary: { maxRules: 100, rules: { allow: 90, block: 3, sections: 1 }, unruled: { count: 8, names: ['a', 'b', 'c', 'd', 'e', 'f', 'g', 'h'], counted: true } },
    });
    await changeMode(full, 1);
    expect(dialog().querySelector('input[type="checkbox"]')).toBeNull();
    expect(text(dialog().querySelector('[data-testid="delivery-mode-beyond"]')!)).toContain('The repository may hold 6 more rules');
  });

  it('R-CUR-025 a change to allExceptBlocked is a confirmation that the allow rules stay', async () => {
    const r = await open({ stored: { mode: 'onlyAllowed' }, canChange: true }, undefined, { summary: { mode: 'onlyAllowed', rules: { allow: 12, block: 0, sections: 0 } } });
    const save = vi.spyOn(r.fixture.componentInstance.save, 'emit');
    await changeMode(r, 0);
    const d = dialog();
    expect(text(d)).toContain('Deliver everything except what is blocked?');
    expect(text(d.querySelector('[data-testid="delivery-mode-allows"]')!)).toBe(
      'The 12 allow rules stay; they decide again when the repository delivers only what is allowed once more.',
    );
    button(d, 'Change the mode').click();
    await settle(r.fixture);
    expect(save.mock.calls.at(-1)?.[0]).toEqual({ settings: { mode: 'allExceptBlocked', examineOnRequest: true, examinedRetentionDays: 30 }, allowStored: false });
  });

  it('R-CUR-020 a day count outside 1 to 365 says so and does not save; a reader without the right reads the values', async () => {
    const { root, fixture } = await open({ stored: { mode: 'onlyAllowed', examineOnRequest: false, examinedRetentionDays: 10 }, canChange: true });
    const days = root.querySelector<HTMLInputElement>('#delivery-days')!;
    days.value = '400';
    days.dispatchEvent(new Event('input'));
    await settle(fixture);
    expect(text(root)).toContain('Enter a whole number of days from 1 to 365.');
    expect(root.querySelector<HTMLButtonElement>('button[type="submit"]')!.disabled).toBe(true);
    const reader = await open({ stored: { mode: 'onlyAllowed' }, canChange: false });
    expect(reader.root.querySelector('button[type="submit"]')).toBeNull();
    expect(reader.root.querySelector<HTMLFieldSetElement>('fieldset.mode')!.disabled).toBe(true);
    expect(deliveryOf({ mode: 'onlyAllowed' })).toEqual({ mode: 'onlyAllowed', examineOnRequest: true, examinedRetentionDays: 30 });
  });

  it('R-CUR-025 the settings page allows the stored packages first and saves the mode after, so no client is refused between them', async () => {
    const repo = { key: 'npmjs', project: 'web', class: 'remote', format: 'npm', remote: { url: 'https://registry.npmjs.org/', mode: 'pullThrough', mirror: {} } } as unknown as Repository;
    const r = await renderScreen(RepositoryDetail, {
      inputs: { project: 'web', key: 'npmjs' },
      setup: async (get) => {
        const fake = get(RepositoriesApi) as FakeRepositoriesApi;
        fake.formatsResult = [GENERIC_FORMAT, PACKAGE_FORMAT];
        fake.repositories.push(repo);
        (get(DeliveryRulesApi) as FakeDeliveryRulesApi).summaryAnswer = { mode: 'allExceptBlocked', maxRules: 50000, ...UNRULED } as DeliveryRulesSummary;
        await get(Router).navigateByUrl('/projects/web/repositories/npmjs/settings#delivery');
      },
    });
    await settle(r.fixture);
    const repos = r.get(RepositoriesApi) as FakeRepositoriesApi;
    const rules = r.get(DeliveryRulesApi) as FakeDeliveryRulesApi;
    // The patches the fake had received when allow-stored was called: none.
    const patchesAtAllow: number[] = [];
    const allowStored = rules.allowStored.bind(rules);
    rules.allowStored = () => {
      patchesAtAllow.push(repos.patches.length);
      return allowStored();
    };
    const section = r.root.querySelector<HTMLElement>('quader-repository-delivery')!;
    section.querySelectorAll<HTMLInputElement>('input[type="radio"]')[1]!.click();
    await settle(r.fixture);
    section.querySelector<HTMLButtonElement>('button[type="submit"]')!.click();
    await waitUntil(r.fixture, () => !!document.querySelector('[data-testid="delivery-mode-names"]'), 'the dialog to name the packages');
    button(dialog(), 'Change the mode').click();
    await waitUntil(r.fixture, () => repos.patches.length === 1, 'the mode to be saved');
    expect(patchesAtAllow).toEqual([0]);
    expect(repos.patches[0]!.patch).toEqual({ remote: { delivery: { mode: 'onlyAllowed', examineOnRequest: true, examinedRetentionDays: 30 } } });
  });
});
