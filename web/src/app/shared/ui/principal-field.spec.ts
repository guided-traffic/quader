import { Component, signal } from '@angular/core';
import { TestBed } from '@angular/core/testing';
import { ProjectsApi } from '../../api/projects-api';
import { ApiError } from '../../api/problem';
import { SearchApi } from '../../api/search-api';
import { provideFakeApis } from '../../api/testing/fakes';
import { FakeProjectsApi } from '../../api/testing/fakes-projects';
import { FakeSearchApi } from '../../api/testing/fakes-search';
import { expectAccessible, settle, sleep, text, typeInto } from '../../testing/screen';
import { PrincipalChoice, PrincipalField, PrincipalKind, choiceFromRef } from './principal-field';

@Component({
  imports: [PrincipalField],
  template: `
    <form>
      <quader-principal-field
        label="Bind to"
        name="who"
        hint="Type a name."
        [kinds]="kinds()"
        [multiple]="multiple()"
        [project]="project()"
        [accounts]="accounts()"
        [exclude]="exclude()"
        [(value)]="value"
        (picked)="picked.set($event)"
      />
    </form>
  `,
})
class Host {
  readonly kinds = signal<readonly PrincipalKind[]>(['user', 'group']);
  readonly multiple = signal(false);
  readonly project = signal<string | null>(null);
  readonly accounts = signal<'instance' | 'all'>('instance');
  readonly exclude = signal<readonly string[]>([]);
  readonly value = signal<readonly string[]>([]);
  readonly picked = signal<PrincipalChoice | null>(null);
}

async function render(setup: (h: Host) => void = () => undefined) {
  TestBed.resetTestingModule();
  await TestBed.configureTestingModule({ imports: [Host], providers: provideFakeApis() }).compileComponents();
  const fixture = TestBed.createComponent(Host);
  setup(fixture.componentInstance);
  await settle(fixture);
  const root = fixture.nativeElement as HTMLElement;
  const input = (): HTMLInputElement => root.querySelector('input[role="combobox"]') as HTMLInputElement;
  const options = (): HTMLElement[] => [...root.querySelectorAll<HTMLElement>('[role="option"]')];
  const key = async (k: string, init: KeyboardEventInit = {}): Promise<KeyboardEvent> => {
    const event = new KeyboardEvent('keydown', { key: k, bubbles: true, cancelable: true, ...init });
    input().dispatchEvent(event);
    await settle(fixture);
    return event;
  };
  const search = TestBed.inject(SearchApi) as FakeSearchApi;
  const projects = TestBed.inject(ProjectsApi) as FakeProjectsApi;
  return { fixture, root, host: fixture.componentInstance, input, options, key, search, projects };
}

describe('The principal field (R-UI-150)', () => {
  it('R-UI-150 searches from the second character after a pause and lists each match with its avatar, its name and its username', async () => {
    const { fixture, root, input, options, search } = await render();
    typeInto(root, 'Bind to', 's');
    await sleep(fixture, 320);
    expect(search.requests).toHaveLength(0);
    expect(input().getAttribute('aria-expanded')).toBe('false');

    typeInto(root, 'Bind to', 'sa');
    await sleep(fixture, 320);
    expect(search.requests).toHaveLength(1);
    expect(search.requests[0]).toMatchObject({ q: 'sa', kinds: ['user', 'group'], limit: 10 });
    expect(input().getAttribute('aria-expanded')).toBe('true');
    expect(input().getAttribute('aria-controls')).toBe(root.querySelector('[role="listbox"]')!.id);
    const [sam] = options();
    expect(text(sam!)).toContain('Sam Reader');
    expect(text(sam!)).toContain('@sam');
    expect(sam!.querySelector('quader-avatar')).not.toBeNull();
    await expectAccessible(root);
  });

  it('R-UI-150 a group shows its icon and the word Group; a disabled user is not offered', async () => {
    const { fixture, root, options, search } = await render();
    search.users = [
      { id: '0192f0c4-7a7e-7b51-9b1e-2f0d6c9e1a20', username: 'web-gone', displayName: null, disabled: true, avatarUrl: null },
    ];
    typeInto(root, 'Bind to', 'web');
    await sleep(fixture, 320);
    expect(options()).toHaveLength(1);
    expect(options()[0]!.querySelector('.material-icons')?.textContent).toBe('groups');
    expect(text(options()[0]!)).toContain('web-leads');
    expect(text(options()[0]!)).toContain('Group');
  });

  it('R-UI-150 the arrows move the active match, Enter takes it as the value, Escape closes the list and then empties the text', async () => {
    const { fixture, root, host, input, options, key } = await render();
    typeInto(root, 'Bind to', 'a');
    typeInto(root, 'Bind to', 'ada');
    await sleep(fixture, 320);
    expect(options()).toHaveLength(1);
    await key('ArrowDown');
    expect(input().getAttribute('aria-activedescendant')).toBe(options()[0]!.id);
    expect(options()[0]!.getAttribute('aria-selected')).toBe('true');
    const enter = await key('Enter');
    expect(enter.defaultPrevented).toBe(true);
    expect(host.value()).toEqual(['user:ada']);
    expect(host.picked()?.name).toBe('ada');
    // One choice: the chip takes the input's place, with a button that removes it.
    expect(input()).toBeNull();
    const remove = root.querySelector<HTMLButtonElement>('.chip-remove')!;
    expect(remove.getAttribute('aria-label')).toBe('Remove ada');
    remove.click();
    await settle(fixture);
    expect(host.value()).toEqual([]);

    typeInto(root, 'Bind to', 'sam');
    await sleep(fixture, 320);
    await key('Escape');
    expect(input().getAttribute('aria-expanded')).toBe('false');
    expect(input().value).toBe('sam');
    await key('Escape');
    expect(input().value).toBe('');
  });

  it('R-UI-150 many choices are chips; a chosen one is not offered again; Backspace on an empty text removes the last', async () => {
    const { fixture, root, host, options, key } = await render((h) => {
      h.multiple.set(true);
      h.value.set(['group:web-leads']);
    });
    expect(text(root.querySelector('.chips')!)).toContain('web-leads');
    typeInto(root, 'Bind to', 'sa');
    await sleep(fixture, 320);
    options()[0]!.click();
    await settle(fixture);
    expect(host.value()).toEqual(['group:web-leads', 'user:sam']);
    typeInto(root, 'Bind to', 'we');
    await sleep(fixture, 320);
    expect(options()).toHaveLength(0);
    expect(text(root)).toContain('Nobody matches “we”');
    typeInto(root, 'Bind to', '');
    await settle(fixture);
    await key('Backspace');
    expect(host.value()).toEqual(['group:web-leads']);
  });

  it('R-UI-150 says when nothing matches, and that a person is found after the first sign-in', async () => {
    const { fixture, root } = await render();
    typeInto(root, 'Bind to', 'zz');
    await sleep(fixture, 320);
    expect(text(root)).toContain('Nobody matches “zz”. A person can be found after their first sign-in.');
  });

  it('R-UI-150 says when the search failed, and keeps no stale match', async () => {
    const { fixture, root, options, search } = await render();
    search.failure = new ApiError(403, { status: 403, code: 'forbidden' }, null);
    typeInto(root, 'Bind to', 'sam');
    await sleep(fixture, 320);
    expect(options()).toHaveLength(0);
    expect(text(root)).toContain('The search failed.');
  });

  it('R-UI-150 inside a project the project lookup answers (Q-AUTHZ-182), with the kinds of the field', async () => {
    const { fixture, root, options, search, projects } = await render((h) => {
      h.project.set('web');
      h.kinds.set(['user', 'service_account']);
    });
    typeInto(root, 'Bind to', 'de');
    await sleep(fixture, 320);
    expect(search.requests).toHaveLength(0);
    expect(projects.lookups.at(-1)).toEqual({ key: 'web', q: 'de', kinds: ['user', 'service_account'] });
    expect(options()).toHaveLength(1);
    expect(text(options()[0]!)).toContain('web/deploy');
    expect(text(options()[0]!)).toContain('Service account of web');
  });

  it('R-UI-150 the admin area offers the instance-wide service accounts, and every account where a filter asks for all', async () => {
    const instance = await render((h) => h.kinds.set(['service_account']));
    typeInto(instance.root, 'Bind to', 'bot');
    await sleep(instance.fixture, 320);
    expect(instance.options().map((o) => text(o))).toEqual([expect.stringContaining('ci-bot')]);
    typeInto(instance.root, 'Bind to', 'deploy');
    await sleep(instance.fixture, 320);
    expect(instance.options()).toHaveLength(0);

    const all = await render((h) => {
      h.kinds.set(['service_account']);
      h.accounts.set('all');
    });
    typeInto(all.root, 'Bind to', 'deploy');
    await sleep(all.fixture, 320);
    expect(all.options()).toHaveLength(1);
  });

  it('R-UI-150 a member that is taken already is not offered (exclude)', async () => {
    const { fixture, root, options } = await render((h) => h.exclude.set(['user:sam']));
    typeInto(root, 'Bind to', 'sa');
    await sleep(fixture, 320);
    expect(options()).toHaveLength(0);
  });

  describe('the active match when a newer answer replaces the list (Q-UI-700, Q-UI-701)', () => {
    const PEOPLE = [
      { id: '0192f0c4-7a7e-7b51-9b1e-2f0d6c9e1a30', username: 'sam', displayName: 'Sam Reader', disabled: false, avatarUrl: null },
      { id: '0192f0c4-7a7e-7b51-9b1e-2f0d6c9e1a31', username: 'sara', displayName: null, disabled: false, avatarUrl: null },
      { id: '0192f0c4-7a7e-7b51-9b1e-2f0d6c9e1a32', username: 'sasha', displayName: null, disabled: false, avatarUrl: null },
    ];

    /** The matches of `sa`; then `sas` is typed and its answer held, so the person walks the list of `sa` meanwhile. */
    async function walking() {
      const r = await render();
      r.search.users = PEOPLE;
      typeInto(r.root, 'Bind to', 'sa');
      await sleep(r.fixture, 320);
      expect(r.options().map((o) => text(o))).toEqual([
        expect.stringContaining('@sam'),
        expect.stringContaining('@sara'),
        expect.stringContaining('@sasha'),
      ]);
      r.search.hold = true;
      typeInto(r.root, 'Bind to', 'sas');
      await sleep(r.fixture, 320);
      const answer = async (): Promise<void> => {
        r.search.hold = false;
        r.search.release();
        await settle(r.fixture);
      };
      const active = (): string | null => {
        const id = r.input().getAttribute('aria-activedescendant');
        return id === null ? null : text(r.root.querySelector(`[id="${id}"]`)!);
      };
      return { ...r, answer, active };
    }

    it('R-UI-150 Q-UI-700 Q-UI-701 the match walked to stays active under the same id; the pointer resting over the moved list takes nothing', async () => {
      const r = await walking();
      r.options()[2]!.dispatchEvent(new MouseEvent('mousemove', { bubbles: true, screenX: 20, screenY: 90 }));
      await settle(r.fixture);
      expect(r.active()).toContain('@sasha');
      const before = r.input().getAttribute('aria-activedescendant');
      await r.answer();
      expect(r.options()).toHaveLength(1);
      // The browser reports the list moving under the resting pointer as a move at the same place.
      r.options()[0]!.dispatchEvent(new MouseEvent('mousemove', { bubbles: true, screenX: 20, screenY: 90 }));
      await settle(r.fixture);
      expect(r.input().getAttribute('aria-activedescendant')).toBe(before);
      expect(r.active()).toContain('@sasha');
      // A held Enter acts once: its repeat takes nothing and submits nothing; the press takes the active match.
      const repeat = await r.key('Enter', { repeat: true });
      expect(repeat.defaultPrevented).toBe(true);
      expect(r.host.value()).toEqual([]);
      await r.key('Enter');
      expect(r.host.value()).toEqual(['user:sasha']);
    });

    it('R-UI-150 Q-UI-700 a new list without the match leaves none active; Enter then does what it does without one', async () => {
      const r = await walking();
      await r.key('ArrowDown');
      await r.key('ArrowDown');
      expect(r.active()).toContain('@sara');
      await r.answer();
      expect(r.options().map((o) => text(o))).toEqual([expect.stringContaining('@sasha')]);
      expect(r.input().getAttribute('aria-activedescendant')).toBeNull();
      expect(r.options()[0]!.getAttribute('aria-selected')).toBe('false');
      // Without an active match, Enter takes the only match the list shows (R-UI-150).
      await r.key('Enter');
      expect(r.host.value()).toEqual(['user:sasha']);
    });
  });

  it('R-UI-150 a stored reference reads as its kind and name', () => {
    expect(choiceFromRef('sa:web/deploy')).toMatchObject({ kind: 'service_account', name: 'web/deploy', project: 'web' });
    expect(choiceFromRef('group:web-leads')).toMatchObject({ kind: 'group', name: 'web-leads' });
    expect(choiceFromRef('user:alice')).toMatchObject({ kind: 'user', name: 'alice' });
    expect(choiceFromRef('ci:gitlab/release')).toMatchObject({ kind: 'other', name: 'ci:gitlab/release' });
  });
});
