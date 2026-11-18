import { Component, signal } from '@angular/core';
import { TestBed } from '@angular/core/testing';
import { seriousViolations } from '../../testing/axe';
import { settle } from '../../testing/screen';
import {
  VisibilityIcon,
  VisibilityLevel,
  VisibilityLevelControl,
  aboveProjectReasons,
  firstReasons,
  installationReasons,
  isMoreOpen,
  lowerLevel,
  restrictedNotes,
} from './visibility-level';

@Component({
  imports: [VisibilityLevelControl, VisibilityIcon],
  template: `
    <quader-visibility-level
      label="Artifacts"
      subject="artifacts"
      [(value)]="level"
      [reasons]="reasons()"
      [disabled]="locked()"
    />
    <quader-visibility-icon level="internal" />
  `,
})
class Host {
  readonly level = signal<VisibilityLevel>('private');
  readonly reasons = signal<Partial<Record<VisibilityLevel, string>>>({});
  readonly locked = signal(false);
}

describe('The visibility level control (R-UI-053, R-UI-055, Q-UI-228)', () => {
  async function render() {
    await TestBed.configureTestingModule({ imports: [Host] }).compileComponents();
    const fixture = TestBed.createComponent(Host);
    document.body.appendChild(fixture.nativeElement);
    await settle(fixture);
    const root = fixture.nativeElement as HTMLElement;
    const radios = () => [...root.querySelectorAll<HTMLInputElement>('input[type="radio"]')];
    return { fixture, root, radios, host: fixture.componentInstance };
  }

  it('R-UI-055 is a group of three native radios named Private, Internal and Public, each with its icon and a sentence, and passes axe', async () => {
    const { root, radios } = await render();
    expect(root.querySelector('legend')?.textContent?.trim()).toBe('Artifacts');
    expect(radios().map((r) => r.labels?.[0]?.querySelector('.name')?.textContent?.trim())).toEqual([
      'lockPrivate',
      'shieldInternal',
      'publicPublic',
    ]);
    expect(radios().map((r) => r.checked)).toEqual([true, false, false]);
    const sentence = document.getElementById(radios()[2]!.getAttribute('aria-describedby')!.split(' ')[0]!);
    expect(sentence?.textContent).toContain('Anyone, also without signing in');
    expect(await seriousViolations(root)).toEqual([]);
  });

  it('Q-UI-228 choosing an option reports it and the group shares one name, so the arrow keys move within it', async () => {
    const { fixture, radios, host } = await render();
    const names = new Set(radios().map((r) => r.name));
    expect(names.size).toBe(1);
    radios()[1]!.click();
    await settle(fixture);
    expect(host.level()).toBe('internal');
    radios()[2]!.click();
    await settle(fixture);
    expect(host.level()).toBe('public');
  });

  it('R-UI-055 an option with a reason is disabled, stays visible and names the reason in its description', async () => {
    const { fixture, root, radios, host } = await render();
    host.reasons.set({ public: 'The project is internal, so this repository cannot be public.' });
    await settle(fixture);
    expect(radios().map((r) => r.disabled)).toEqual([false, false, true]);
    const ids = radios()[2]!.getAttribute('aria-describedby')!.split(' ');
    expect(ids).toHaveLength(2);
    expect(document.getElementById(ids[1]!)?.textContent).toContain('cannot be public');
    radios()[2]!.click();
    await settle(fixture);
    expect(host.level()).toBe('private');
    expect(await seriousViolations(root)).toEqual([]);
  });

  it('R-UI-053 a locked group disables every option', async () => {
    const { fixture, radios, host } = await render();
    host.locked.set(true);
    await settle(fixture);
    expect(radios().every((r) => r.closest('fieldset')!.disabled)).toBe(true);
  });

  it('Q-UI-230 the icon has the level name as its accessible label', async () => {
    const { root } = await render();
    const icon = root.querySelector('quader-visibility-icon [role="img"]')!;
    expect(icon.getAttribute('aria-label')).toBe('Internal');
    expect(icon.textContent?.trim()).toBe('shield');
  });

  it('R-UI-055 the order of the levels and the reasons above a project', () => {
    expect(lowerLevel('public', 'internal')).toBe('internal');
    expect(lowerLevel('private', 'internal')).toBe('private');
    expect(isMoreOpen('public', 'internal')).toBe(true);
    expect(isMoreOpen('internal', 'internal')).toBe(false);
    expect(aboveProjectReasons('internal')).toEqual({
      public: 'The project is internal, so this repository cannot be public.',
    });
    expect(Object.keys(aboveProjectReasons('private'))).toEqual(['internal', 'public']);
    expect(aboveProjectReasons('public')).toEqual({});
    expect(aboveProjectReasons(null)).toEqual({});
  });
});

describe('The reasons of the installation (R-UI-141, R-AUTHZ-038)', () => {
  const choices = (over: Partial<{ restrictedLevels: VisibilityLevel[]; mayChooseRestricted: boolean; publicAvailable: boolean }> = {}) => ({
    defaultLevel: 'private' as VisibilityLevel,
    restrictedLevels: [] as VisibilityLevel[],
    mayChooseRestricted: false,
    publicAvailable: true,
    ...over,
  });

  it('R-UI-141 a restricted level is off for whom may not choose it, except the level the object keeps', () => {
    expect(Object.keys(installationReasons(choices({ restrictedLevels: ['internal', 'public'] })))).toEqual(['internal', 'public']);
    expect(Object.keys(installationReasons(choices({ restrictedLevels: ['internal', 'public'] }), 'public'))).toEqual(['internal']);
    expect(installationReasons(choices({ restrictedLevels: ['public'], mayChooseRestricted: true }))).toEqual({});
    expect(installationReasons(null)).toEqual({});
  });

  it('R-AUTHZ-038 Public unavailable is named before the restriction, and the project before both', () => {
    const r = installationReasons(choices({ restrictedLevels: ['public'], publicAvailable: false }));
    expect(r.public).toContain('endpoints.default.auth.anonymous');
    expect(firstReasons(aboveProjectReasons('internal'), r).public).toContain('The project is internal');
  });

  it('R-UI-141 an administrator gets a note under every restricted level, anyone else under the level it keeps', () => {
    expect(Object.keys(restrictedNotes(choices({ restrictedLevels: ['internal', 'public'], mayChooseRestricted: true })))).toEqual([
      'internal',
      'public',
    ]);
    expect(Object.keys(restrictedNotes(choices({ restrictedLevels: ['internal', 'public'] }), 'public'))).toEqual(['public']);
  });
});
