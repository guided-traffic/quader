import { ApiError } from '../../api/problem';
import { VisibilityApi } from '../../api/visibility-api';
import { FakeVisibilityApi } from '../../api/testing/fakes-projects';
import { button, expectAccessible, renderScreen, settle, text } from '../../testing/screen';
import { GeneralSettings } from './general-settings';

async function render(options: { capabilities?: readonly string[]; setup?: (api: FakeVisibilityApi) => void } = {}) {
  const r = await renderScreen(GeneralSettings, {
    capabilities: options.capabilities,
    setup: (get) => options.setup?.(get(VisibilityApi) as FakeVisibilityApi),
  });
  const api = r.get(VisibilityApi) as FakeVisibilityApi;
  const radios = () => [...r.root.querySelectorAll<HTMLInputElement>('quader-visibility-level input[type="radio"]')];
  const boxes = () => [...r.root.querySelectorAll<HTMLInputElement>('quader-restricted-levels input[type="checkbox"]')];
  const save = () => button(r.root, /^\s*Save changes\s+\(visibility\)\s*$/);
  return { ...r, api, radios, boxes, save };
}

describe('General settings: the installation default visibility (R-UI-053)', () => {
  it('R-UI-140 the section Visibility is open when the page opens, names the default project visibility, and passes axe', async () => {
    const { root, radios } = await render();
    const toggle = [...root.querySelectorAll<HTMLButtonElement>('button[aria-expanded]')].find((b) => text(b).endsWith('Visibility'))!;
    expect(toggle.getAttribute('aria-expanded')).toBe('true');
    expect(text(root)).toContain('New projects are private.');
    expect(text(root.querySelector('quader-visibility-level legend')!)).toBe('Default project visibility');
    expect(radios().map((x) => x.checked)).toEqual([true, false, false]);
    expect(text(root)).toContain('It also bounds new repositories');
    await expectAccessible(root);
  });

  it('R-UI-053 choosing Internal and saving patches the default with the loaded ETag', async () => {
    const { fixture, api, radios, save } = await render();
    expect(save().disabled).toBe(true);
    radios()[1]!.click();
    await settle(fixture);
    save().click();
    await settle(fixture);
    expect(api.patches).toEqual([{ patch: { defaultLevel: 'internal' }, etag: '"1"' }]);
    expect(api.stored.settings.defaultLevel).toBe('internal');
    expect(save().disabled).toBe(true);
  });

  it('R-UI-053 while the installation admits no request without a credential, Public is disabled and says why', async () => {
    const { root, radios } = await render({ setup: (api) => (api.stored.settings.publicAvailable = false) });
    expect(radios()[2]!.disabled).toBe(true);
    expect(text(root)).toContain('endpoints.default.auth.anonymous');
  });

  it('R-UI-053 lists what is public now, each linked to its settings, or says nothing is', async () => {
    const empty = await render();
    expect(text(empty.root.querySelector('[data-testid="public-objects"]')!)).toBe('Nothing is public.');

    const { root } = await render({
      setup: (api) => (api.stored.settings.publicObjects = { projects: ['web'], repositories: ['web/npm-public'], truncated: false }),
    });
    const links = [...root.querySelectorAll<HTMLAnchorElement>('[data-testid="public-objects"] a')];
    expect(links.map((a) => [text(a), a.getAttribute('href')])).toEqual([
      ['web', '/projects/web/settings'],
      ['web/npm-public', '/projects/web/repositories/npm-public/settings'],
    ]);
  });

  it('R-UI-053 a holder of settings:read alone sees the section read-only with the reason', async () => {
    const { root, radios } = await render({ capabilities: ['settings:read'] });
    expect(radios().every((x) => x.closest('fieldset')!.disabled)).toBe(true);
    expect(text(root)).toContain('security-settings:write');
    expect(() => button(root, /Save changes\s+\(visibility\)/)).toThrow();
  });

  it('R-UI-053 a 409 public_not_allowed is shown at the group', async () => {
    const { fixture, root, api, radios, save } = await render();
    api.failure = new ApiError(409, { code: 'public_not_allowed' }, 'req-1');
    radios()[2]!.click();
    await settle(fixture);
    save().click();
    await settle(fixture);
    expect(text(root.querySelector('quader-error-notice')!)).toContain('admits no request without a credential');
  });
});

describe('General settings: the restricted visibility levels (R-UI-140)', () => {
  it('R-UI-140 one checkbox per level with its icon and sentence; the default is disabled with the reason, and passes axe', async () => {
    const { root, boxes } = await render();
    const group = root.querySelector('quader-restricted-levels')!;
    expect(text(group.querySelector('legend')!)).toBe('Restricted visibility levels');
    expect(boxes().map((b) => b.labels?.[0]?.querySelector('.name')?.textContent?.trim())).toEqual([
      'lockPrivate',
      'shieldInternal',
      'publicPublic',
    ]);
    expect(boxes().map((b) => b.disabled)).toEqual([true, false, false]);
    const reason = document.getElementById(boxes()[0]!.getAttribute('aria-describedby')!.split(' ')[1]!);
    expect(text(reason!)).toBe('This is the default project visibility, which cannot be restricted.');
    expect(text(group)).toContain('Only an administrator can make a project or a repository public.');
    await expectAccessible(root);
  });

  it('R-UI-140 restricting Public disables it as the default, and one Save changes sends the set', async () => {
    const { fixture, api, radios, boxes, save } = await render();
    boxes()[2]!.click();
    await settle(fixture);
    expect(radios()[2]!.disabled).toBe(true);
    expect(save().disabled).toBe(false);
    save().click();
    await settle(fixture);
    expect(api.patches).toEqual([{ patch: { restrictedLevels: ['public'] }, etag: '"1"' }]);
    expect(api.stored.settings.restrictedLevels).toEqual(['public']);
    expect(save().disabled).toBe(true);
  });

  it('R-UI-140 choosing another default frees the old default and disables the new one; both changes travel in one patch', async () => {
    const { fixture, api, radios, boxes, save } = await render();
    radios()[1]!.click();
    await settle(fixture);
    expect(boxes().map((b) => b.disabled)).toEqual([false, true, false]);
    boxes()[0]!.click();
    await settle(fixture);
    save().click();
    await settle(fixture);
    expect(api.patches).toEqual([{ patch: { defaultLevel: 'internal', restrictedLevels: ['private'] }, etag: '"1"' }]);
  });

  it('R-UI-140 the summary of the section names the restricted levels', async () => {
    const { fixture, root, boxes, save } = await render();
    boxes()[2]!.click();
    await settle(fixture);
    save().click();
    await settle(fixture);
    expect(text(root)).toContain('New projects are private. Restricted to administrators: Public.');
  });

  it('R-UI-140 a holder of settings:read alone sees the checkboxes read-only', async () => {
    const { boxes } = await render({ capabilities: ['settings:read'], setup: (api) => (api.stored.settings.restrictedLevels = ['public']) });
    expect(boxes().map((b) => b.checked)).toEqual([false, false, true]);
    expect(boxes().every((b) => b.closest('fieldset')!.disabled)).toBe(true);
  });
});
