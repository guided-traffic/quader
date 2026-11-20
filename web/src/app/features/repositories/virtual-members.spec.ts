import { outputToObservable, toSignal } from '@angular/core/rxjs-interop';
import { TestBed } from '@angular/core/testing';
import { scan } from 'rxjs';
import { ApiError } from '../../api/problem';
import { Repository, RepositoriesApi } from '../../api/repositories-api';
import { FakeRepositoriesApi, FakeRepository, PACKAGE_FORMAT } from '../../api/testing/fakes-catalog';
import { UpstreamApi } from '../../api/upstream-api';
import { FakeUpstreamApi } from '../../api/testing/fakes-upstream';
import { button, expectAccessible, renderScreen, settle, text, type } from '../../testing/screen';
import { MemberChange, VirtualMembers, decisionText } from './virtual-members';

const repo = (key: string, cls: 'local' | 'remote' | 'virtual', extra: Partial<FakeRepository> = {}, project = 'web'): FakeRepository =>
  ({ key, project, class: cls, format: 'npm', description: '', disabled: false, ...extra }) as FakeRepository;

const virtualDoc = (members: string[], deployTarget: string | null = null): Repository =>
  ({ key: 'npm-all', project: 'web', class: 'virtual', format: 'npm', layout: 'npm', virtual: { members, deployTarget } }) as unknown as Repository;

async function render(options: { members?: string[]; target?: string | null; canWrite?: boolean; failure?: ApiError | null } = {}) {
  const r = await renderScreen(VirtualMembers, {
    inputs: {
      repo: virtualDoc(options.members ?? ['npm-local'], options.target ?? null),
      canWrite: options.canWrite ?? true,
      failure: options.failure ?? null,
    },
    setup: (get) => {
      const api = get(RepositoriesApi) as FakeRepositoriesApi;
      api.repositories = [
        repo('npm-all', 'virtual', { virtual: { members: options.members ?? ['npm-local'] } } as Partial<FakeRepository>),
        repo('npm-local', 'local'),
        repo('npm-team', 'local'),
        repo('npm-remote', 'remote'),
        repo('npm-other-virtual', 'virtual'),
        repo('npm-elsewhere', 'local', {}, 'other'),
        repo('files', 'local', { format: 'generic' }),
        repo('npm-gone', 'local', { lifecycle: 'deleting' } as Partial<FakeRepository>),
      ];
    },
  });
  const saves = TestBed.runInInjectionContext(() =>
    toSignal(outputToObservable(r.fixture.componentInstance.save).pipe(scan((all, c) => [...all, c], [] as MemberChange[])), {
      initialValue: [] as MemberChange[],
    }),
  );
  const reloads = TestBed.runInInjectionContext(() =>
    toSignal(outputToObservable(r.fixture.componentInstance.reload).pipe(scan((n) => n + 1, 0)), { initialValue: 0 }),
  );
  await settle(r.fixture);
  return { ...r, saves, reloads, api: r.get(RepositoriesApi) as FakeRepositoriesApi };
}

const keys = (root: ParentNode, box: 'vm-available' | 'vm-members'): string[] =>
  [...root.querySelectorAll(`[data-testid="${box}"] [role="option"]`)].map((o) => o.querySelector('[translate="no"]')!.textContent!.trim());

const option = (root: ParentNode, box: 'vm-available' | 'vm-members', key: string): HTMLElement => {
  const o = [...root.querySelectorAll<HTMLElement>(`[data-testid="${box}"] [role="option"]`)].find((e) => e.querySelector('[translate="no"]')?.textContent?.trim() === key);
  if (!o) throw new Error(`no option ${key} in ${box}`);
  return o;
};

async function click(fixture: Parameters<typeof settle>[0], el: HTMLElement): Promise<void> {
  el.click();
  await settle(fixture);
}

const live = (root: ParentNode): string => text(root.querySelector('[data-testid="vm-live"]')!);

describe('Virtual repository member editor (R-UI-022)', () => {
  it('R-UI-022 offers only the local and remote repositories of the own project and format that are not members, in two listboxes', async () => {
    const { root } = await render({ members: ['npm-local'] });
    expect(keys(root, 'vm-available')).toEqual(['npm-remote', 'npm-team']);
    expect(keys(root, 'vm-members')).toEqual(['npm-local']);
    const boxes = [...root.querySelectorAll('[role="listbox"]')];
    expect(boxes).toHaveLength(2);
    expect(boxes.every((b) => b.hasAttribute('aria-labelledby'))).toBe(true);
    await expectAccessible(root);
  });

  it('R-UI-022 Add appends the picked repositories, Move up, Move down and Move to top reorder, and the live region says where it went', async () => {
    const { fixture, root } = await render({ members: ['npm-local'] });
    await click(fixture, option(root, 'vm-available', 'npm-team'));
    await click(fixture, button(root, 'Add'));
    expect(keys(root, 'vm-members')).toEqual(['npm-local', 'npm-team']);
    expect(live(root)).toBe('npm-team added at position 2 of 2');

    await click(fixture, option(root, 'vm-available', 'npm-remote'));
    await click(fixture, button(root, 'Add'));
    expect(keys(root, 'vm-members')).toEqual(['npm-local', 'npm-team', 'npm-remote']);

    await click(fixture, button(root, 'Move to top'));
    expect(keys(root, 'vm-members')).toEqual(['npm-remote', 'npm-local', 'npm-team']);
    expect(live(root)).toBe('npm-remote moved to position 1 of 3');
    expect(button(root, 'Move up').disabled).toBe(true);
    expect(button(root, 'Move to top').disabled).toBe(true);

    await click(fixture, button(root, 'Move down'));
    expect(keys(root, 'vm-members')).toEqual(['npm-local', 'npm-remote', 'npm-team']);
    expect(live(root)).toBe('npm-remote moved to position 2 of 3');
    await click(fixture, button(root, 'Move up'));
    expect(keys(root, 'vm-members')).toEqual(['npm-remote', 'npm-local', 'npm-team']);
  });

  it('R-UI-022 the move buttons are off without a selected member, and Move down is off at the end', async () => {
    const { fixture, root } = await render({ members: ['npm-local', 'npm-team'] });
    for (const label of ['Remove', 'Move up', 'Move down', 'Move to top']) expect(button(root, label).disabled, label).toBe(true);
    await click(fixture, option(root, 'vm-members', 'npm-team'));
    expect(button(root, 'Move down').disabled).toBe(true);
    expect(button(root, 'Move up').disabled).toBe(false);
  });

  it('R-UI-022 Alt with the arrow keys or Home moves the selected member', async () => {
    const { fixture, root } = await render({ members: ['npm-local', 'npm-team', 'npm-remote'] });
    await click(fixture, option(root, 'vm-members', 'npm-team'));
    const box = root.querySelector<HTMLElement>('[data-testid="vm-members"]')!;
    const press = async (key: string) => {
      box.dispatchEvent(new KeyboardEvent('keydown', { key, altKey: true, bubbles: true, cancelable: true }));
      await settle(fixture);
    };
    await press('ArrowDown');
    expect(keys(root, 'vm-members')).toEqual(['npm-local', 'npm-remote', 'npm-team']);
    await press('Home');
    expect(keys(root, 'vm-members')).toEqual(['npm-team', 'npm-local', 'npm-remote']);
    await press('ArrowUp');
    expect(keys(root, 'vm-members')).toEqual(['npm-team', 'npm-local', 'npm-remote']);
  });

  it('R-UI-022 Remove takes the member out and clears the default deployment target when it was the target', async () => {
    const { fixture, root } = await render({ members: ['npm-local', 'npm-team'], target: 'npm-local' });
    await click(fixture, option(root, 'vm-members', 'npm-local'));
    await click(fixture, button(root, 'Remove'));
    expect(keys(root, 'vm-members')).toEqual(['npm-team']);
    expect(keys(root, 'vm-available')).toContain('npm-local');
    expect(live(root)).toContain('npm-local removed');
    expect(live(root)).toContain('default deployment target is now none');
    expect(root.querySelector<HTMLSelectElement>('select[name="deployTarget"]')!.value).toBe('');
  });

  it('R-UI-022 the default deployment target is chosen among the local members only', async () => {
    const { fixture, root, saves } = await render({ members: ['npm-local', 'npm-remote'] });
    const select = root.querySelector<HTMLSelectElement>('select[name="deployTarget"]')!;
    expect([...select.options].map((o) => o.value)).toEqual(['', 'npm-local']);
    type(select, 'npm-local');
    await settle(fixture);
    button(root, /Save changes/).click();
    await settle(fixture);
    expect(saves()).toEqual([{ members: ['npm-local', 'npm-remote'], deployTarget: 'npm-local' }]);
  });

  it('R-UI-022 a user adds two members, reorders them and saves: the change carries that order', async () => {
    const { fixture, root, saves } = await render({ members: [] });
    expect(button(root, /Save changes/).disabled).toBe(true);
    await click(fixture, option(root, 'vm-available', 'npm-remote'));
    await click(fixture, button(root, 'Add'));
    await click(fixture, option(root, 'vm-available', 'npm-team'));
    await click(fixture, button(root, 'Add'));
    await click(fixture, button(root, 'Move to top'));
    button(root, /Save changes/).click();
    await settle(fixture);
    expect(saves()).toEqual([{ members: ['npm-team', 'npm-remote'], deployTarget: null }]);
  });

  it('R-UI-022 Discard changes restores the stored members', async () => {
    const { fixture, root } = await render({ members: ['npm-local'] });
    await click(fixture, option(root, 'vm-available', 'npm-team'));
    await click(fixture, button(root, 'Add'));
    await click(fixture, button(root, 'Discard changes'));
    expect(keys(root, 'vm-members')).toEqual(['npm-local']);
    expect(button(root, /Save changes/).disabled).toBe(true);
  });

  it('R-UI-022 a cycle or any refusal the server points at a member is shown on that member', async () => {
    const failure = new ApiError(
      422,
      { code: 'validation_failed', errors: [{ pointer: '/virtual/members/1', code: 'invalid_member', detail: 'npm-team would create a cycle.' }] },
      null,
    );
    const { root } = await render({ members: ['npm-local', 'npm-team'], failure });
    const errors = [...root.querySelectorAll('[data-testid="vm-member-error"]')];
    expect(errors).toHaveLength(1);
    expect(option(root, 'vm-members', 'npm-team').contains(errors[0]!)).toBe(true);
    expect(option(root, 'vm-members', 'npm-team').getAttribute('aria-invalid')).toBe('true');
    expect(root.querySelector('quader-error-notice')).toBeNull();
  });

  it('R-UI-022 a 412 tells the stored repository changed and offers to load it', async () => {
    const failure = new ApiError(412, { code: 'precondition_failed' }, null);
    const { root, reloads } = await render({ failure });
    button(root, 'Load the stored repository').click();
    expect(reloads()).toBe(1);
  });

  it('R-UI-022 without the right to change the configuration the lists are shown and nothing can be changed or saved', async () => {
    const { root } = await render({ canWrite: false });
    expect([...root.querySelectorAll('button')].map((b) => text(b))).not.toContain('Save changes (members)');
    expect(button(root, 'Add').disabled).toBe(true);
  });

  it('R-UI-022 the effective resolution order is the saved list with the deployment target marked, and a path is explained member by member', async () => {
    const { fixture, root, api } = await render({ members: ['npm-local', 'npm-remote'], target: 'npm-local' });
    const order = [...root.querySelectorAll('[data-testid="vm-effective"] li')].map((li) => text(li));
    expect(order).toEqual(['npm-local Local Default deployment target', 'npm-remote Remote']);

    api.resolutionFor = (ref, path) => ({
      repository: ref,
      descriptor: { path, kind: 'metadata', package: path },
      decidedBy: { claim: 'lodash', claimedBy: ['npm-local'] },
      members: [
        { key: 'npm-local', class: 'local', decision: 'hit' },
        { key: 'npm-remote', class: 'remote', decision: 'claimed-elsewhere' },
      ],
      unreadableMembers: 2,
    });
    type(root.querySelector<HTMLInputElement>('input[name="samplePath"]')!, 'lodash');
    await settle(fixture);
    button(root, 'Explain').click();
    await settle(fixture);
    expect(api.resolutionCalls).toEqual([{ ref: 'web/npm-all', path: 'lodash' }]);
    const x = root.querySelector('[data-testid="vm-explanation"]')!;
    expect(text(x)).toContain('matches the claim lodash.');
    const rows = [...x.querySelectorAll('tbody tr')].map((tr) => [...tr.children].map((c) => text(c)));
    expect(rows).toEqual([
      ['npm-local', 'Holds it and would serve it'],
      ['npm-remote', 'Not asked: another repository claims this name'],
    ]);
    expect(text(x)).toContain('2 members are not shown');
    await expectAccessible(root);
  });

  it('R-UI-022 every decision of the explanation has words, and an unknown one is shown as sent', () => {
    for (const d of ['hit', 'miss', 'negative', 'disabled', 'not_admitted', 'pinned-out', 'claimed-elsewhere', 'refused-claimed', 'no-read', 'no-cache-fill', 'offline', 'circuit-open']) {
      expect(decisionText(d), d).not.toBe(d);
    }
    expect(decisionText('something-new')).toBe('something-new');
  });
});

describe('A well-known repository as a member (R-UI-189)', () => {
  async function renderNpm(
    members: string[],
    options: {
      capabilities?: readonly string[];
      canWrite?: boolean;
      upstream?: (a: FakeUpstreamApi) => void;
      repositories?: FakeRepository[];
    } = {},
  ) {
    const r = await renderScreen(VirtualMembers, {
      inputs: { repo: virtualDoc(members), canWrite: options.canWrite ?? true },
      ...(options.capabilities ? { capabilities: options.capabilities } : {}),
      setup: (get) => {
        const api = get(RepositoriesApi) as FakeRepositoriesApi;
        api.formatsResult = [PACKAGE_FORMAT];
        api.repositories = options.repositories ?? [repo('npm-all', 'virtual'), repo('npm-local', 'local')];
        options.upstream?.(get(UpstreamApi) as FakeUpstreamApi);
      },
    });
    const saves = TestBed.runInInjectionContext(() =>
      toSignal(outputToObservable(r.fixture.componentInstance.save).pipe(scan((all, c) => [...all, c], [] as MemberChange[])), {
        initialValue: [] as MemberChange[],
      }),
    );
    await settle(r.fixture);
    return { ...r, saves };
  }

  it('R-UI-189 the picker of an npm virtual repository offers npm-local and, under Upstream registries, upstream/npmjs with its state; adding it saves both', async () => {
    const { fixture, root, saves } = await renderNpm([]);
    expect(keys(root, 'vm-available')).toEqual(['npm-local']);
    const group = root.querySelector('[data-testid="vm-upstream"]')!;
    expect(text(root.querySelector(`#${group.getAttribute('aria-labelledby')}`)!)).toBe('Upstream registries');
    const npmjs = group.querySelector<HTMLElement>('[role="option"]')!;
    expect(text(npmjs)).toContain('npm public registry');
    expect(text(npmjs)).toContain('upstream/npmjs');
    expect(text(npmjs)).toContain('Not switched on');

    await click(fixture, option(root, 'vm-available', 'npm-local'));
    await click(fixture, npmjs);
    await click(fixture, button(root, 'Add'));
    expect(keys(root, 'vm-members')).toEqual(['npm-local', 'upstream/npmjs']);
    // The member keeps its place and says why it takes no part (Q-UI-833); the section's head counts it.
    const placeholder = option(root, 'vm-members', 'upstream/npmjs');
    expect(text(placeholder)).toContain('Not switched on');
    expect(text(placeholder)).toContain('Skipped until an administrator of Quader switches it on.');
    expect(text(root.querySelector('[data-testid="vm-skipped"] span:last-child')!)).toBe('1 member takes no part: upstream/npmjs.');
    button(root, /Save changes/).click();
    await settle(fixture);
    expect(saves()).toEqual([{ members: ['npm-local', 'upstream/npmjs'], deployTarget: null }]);
    await expectAccessible(root);
  });

  it('R-UI-189 a switched-off and a retired member keep their places with their words; an active private one says who reads through it', async () => {
    const { root } = await renderNpm(['upstream/npmjs', 'npm-local', 'upstream/old-registry'], {
      upstream: (a) => a.set('npmjs', { state: 'switched-off' }),
    });
    expect(keys(root, 'vm-members')).toEqual(['upstream/npmjs', 'npm-local', 'upstream/old-registry']);
    expect(text(option(root, 'vm-members', 'upstream/npmjs'))).toContain('Skipped: an administrator of Quader switched it off.');
    expect(text(option(root, 'vm-members', 'upstream/old-registry'))).toContain('No longer offered');
    expect(text(root.querySelector('[data-testid="vm-effective"]')!)).toContain('Skipped');

    const active = await renderNpm(['upstream/npmjs'], { upstream: (a) => a.set('npmjs', { state: 'active', visibility: 'private' }) });
    expect(text(option(active.root, 'vm-members', 'upstream/npmjs'))).toContain(
      'Private: the readers of this virtual repository do not read through it unless a global policy names them.',
    );
    expect(active.root.querySelector('[data-testid="vm-skipped"]')).toBeNull();
  });

  it("R-UI-189 a project's Owner without the admin read learns each registry's exact state and level from the format's read", async () => {
    const { root, get } = await renderNpm(['upstream/npmjs'], {
      capabilities: [],
      upstream: (a) => a.set('npmjs', { state: 'switched-off' }),
    });
    const api = get(UpstreamApi) as FakeUpstreamApi;
    expect(api.calls).toContainEqual({ op: 'format', body: { format: 'npm' } });
    expect(text(option(root, 'vm-members', 'upstream/npmjs'))).toContain('Switched off');
    expect(text(option(root, 'vm-members', 'upstream/npmjs'))).toContain('Skipped: an administrator of Quader switched it off.');
  });

  it('R-UI-189 a reader who may not change the members, or whom the format\'s read refuses, learns a registry it reads that is off as Off', async () => {
    const readable = [repo('npm-all', 'virtual'), repo('npmjs', 'remote', { disabled: true }, 'upstream')];
    const reader = await renderNpm(['upstream/npmjs'], { capabilities: [], canWrite: false, repositories: readable });
    expect((reader.get(UpstreamApi) as FakeUpstreamApi).calls.some((c) => c.op === 'format')).toBe(false);
    expect(text(option(reader.root, 'vm-members', 'upstream/npmjs'))).toContain('Off');
    expect(text(option(reader.root, 'vm-members', 'upstream/npmjs'))).toContain('Skipped until an administrator of Quader switches it on.');

    const refused = await renderNpm(['upstream/npmjs'], {
      capabilities: [],
      repositories: readable,
      upstream: (a) => (a.formatFailure = new ApiError(403, { code: 'forbidden', detail: 'The operation is not permitted.' }, 'rid')),
    });
    expect(text(option(refused.root, 'vm-members', 'upstream/npmjs'))).toContain('Off');
  });

  it('R-UI-189 Explain a path names a placeholder by its state, in the words of its badge', async () => {
    const { fixture, root, get } = await renderNpm(['npm-local', 'upstream/npmjs']);
    (get(RepositoriesApi) as FakeRepositoriesApi).resolutionFor = (ref, path) => ({
      repository: ref,
      descriptor: { path, kind: 'metadata', package: path },
      members: [
        { key: 'npm-local', class: 'local', decision: 'miss' },
        { key: 'upstream/npmjs', class: 'remote', decision: 'disabled', upstreamState: 'not-switched-on' },
      ],
      unreadableMembers: 0,
    });
    type(root.querySelector<HTMLInputElement>('input[name="samplePath"]')!, 'left-pad');
    await settle(fixture);
    button(root, 'Explain').click();
    await settle(fixture);
    const rows = [...root.querySelectorAll('[data-testid="vm-explanation"] tbody tr')].map((tr) => [...tr.children].map((c) => text(c)));
    expect(rows).toEqual([
      ['npm-local', 'Does not hold it'],
      ['upstream/npmjs', 'Not switched on, so not asked'],
    ]);
  });
});
