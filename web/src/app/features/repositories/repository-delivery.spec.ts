import { Router } from '@angular/router';
import { vi } from 'vitest';
import { expectAccessible, renderScreen, settle, text } from '../../testing/screen';
import { DeliverySettings, RepositoryDeliverySection, deliveryOf } from './repository-delivery';

async function open(inputs: Record<string, unknown>, url = '/projects/web/repositories/npmjs/settings') {
  const r = await renderScreen(RepositoryDeliverySection, {
    inputs: { project: 'web', repositoryKey: 'npmjs', ...inputs },
    setup: async (get) => {
      await get(Router).navigateByUrl(url);
    },
  });
  await settle(r.fixture);
  return r;
}

describe('The settings section Delivery of a remote repository', () => {
  it('R-CUR-018 R-CUR-020 a closed section with the mode as two choices, the examination switch and its days, saved on its own', async () => {
    const { root, fixture } = await open({ stored: undefined, canChange: true }, '/projects/web/repositories/npmjs/settings#delivery');
    // The tab Upstream's Change leads here with #delivery: the section is open.
    expect(root.querySelector('.toggle')?.getAttribute('aria-expanded')).toBe('true');
    const radios = [...root.querySelectorAll<HTMLInputElement>('input[type="radio"]')];
    expect(radios.map((r) => r.checked)).toEqual([true, false]);
    expect(text(root)).toContain('Everything except what is blocked');
    expect(text(root)).toContain('Only what is allowed');
    expect(text(root)).toContain('Fetch for examination when someone asks');
    expect(root.querySelector<HTMLInputElement>('#delivery-days')!.value).toBe('30');
    expect(root.querySelector('a')?.getAttribute('href')).toBe('/projects/web/repositories/npmjs/upstream');
    const save = vi.spyOn(fixture.componentInstance.save, 'emit');
    radios[1]!.click();
    const days = root.querySelector<HTMLInputElement>('#delivery-days')!;
    days.value = '7';
    days.dispatchEvent(new Event('input'));
    await settle(fixture);
    expect(text(root)).toContain('Unsaved changes');
    root.querySelector<HTMLButtonElement>('button[type="submit"]')!.click();
    await settle(fixture);
    expect(save.mock.calls).toEqual([[{ mode: 'onlyAllowed', examineOnRequest: true, examinedRetentionDays: 7 } satisfies DeliverySettings]]);
    await expectAccessible(root);
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
});
