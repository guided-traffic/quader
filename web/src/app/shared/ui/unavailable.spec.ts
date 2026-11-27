import { Component, signal } from '@angular/core';
import { TestBed } from '@angular/core/testing';
import { Router, RouterLink, provideRouter } from '@angular/router';
import { seriousViolations } from '../../testing/axe';
import { settle, sleep } from '../../testing/screen';
import { Unavailable } from './unavailable';

const REASON = 'Declared in config.authz.role_bindings of the Helm configuration: remove it there.';

@Component({
  imports: [Unavailable, RouterLink],
  template: `
    <form (submit)="submitted = submitted + 1; $event.preventDefault()">
      <button type="button" class="btn btn-tertiary act" [quaderUnavailable]="reason()" (click)="clicks = clicks + 1">Unbind</button>
      <button type="submit" class="btn btn-confirm send" [quaderUnavailable]="reason()">Save</button>
      <input class="text-input field" aria-label="Address" value="198.51.100.21" [quaderUnavailable]="reason()" />
      <a class="btn btn-default go" routerLink="/elsewhere" [quaderUnavailable]="reason()">Go</a>
    </form>
  `,
})
class Host {
  readonly reason = signal(REASON);
  clicks = 0;
  submitted = 0;
}

describe('An unavailable control (R-UI-270)', () => {
  async function render() {
    await TestBed.configureTestingModule({ imports: [Host], providers: [provideRouter([])] }).compileComponents();
    const fixture = TestBed.createComponent(Host);
    document.body.appendChild(fixture.nativeElement);
    await settle(fixture);
    const root = fixture.nativeElement as HTMLElement;
    const act = root.querySelector<HTMLButtonElement>('.act')!;
    return { fixture, root, act, host: fixture.componentInstance };
  }
  const bubble = () => document.querySelector('quader-tooltip');

  it('R-UI-270 is aria-disabled, never disabled, and described by its reason; axe finds nothing', async () => {
    const { root, act } = await render();
    expect(act.disabled).toBe(false);
    expect(act.getAttribute('aria-disabled')).toBe('true');
    const describedBy = act.getAttribute('aria-describedby');
    expect(document.getElementById(describedBy!)?.textContent).toBe(REASON);
    const field = root.querySelector<HTMLInputElement>('.field')!;
    expect(field.readOnly).toBe(true);
    expect(field.getAttribute('aria-disabled')).toBe('true');
    expect(await seriousViolations(root)).toEqual([]);
  });

  it('R-UI-270 a click, Enter and Space act on nothing: no handler, no submit, no navigation', async () => {
    const { fixture, root, act, host } = await render();
    const navigate = vi.spyOn(TestBed.inject(Router), 'navigateByUrl');
    act.click();
    root.querySelector<HTMLButtonElement>('.send')!.click();
    root.querySelector<HTMLAnchorElement>('.go')!.click();
    for (const key of ['Enter', ' ']) {
      const press = new KeyboardEvent('keydown', { key, bubbles: true, cancelable: true });
      act.dispatchEvent(press);
      expect(press.defaultPrevented).toBe(true);
    }
    await settle(fixture);
    expect(host.clicks).toBe(0);
    expect(host.submitted).toBe(0);
    expect(navigate).not.toHaveBeenCalled();
  });

  it('R-UI-270 hover and focus show the reason; a tap pins it until the next tap', async () => {
    const { fixture, act } = await render();
    act.dispatchEvent(new Event('mouseenter'));
    await settle(fixture);
    expect(bubble()?.textContent).toBe(REASON);
    act.dispatchEvent(new Event('mouseleave'));
    await sleep(fixture, 150);
    expect(bubble()).toBeNull();

    act.dispatchEvent(new Event('focus'));
    await settle(fixture);
    expect(bubble()?.textContent).toBe(REASON);
    act.dispatchEvent(new Event('blur'));
    await settle(fixture);
    expect(bubble()).toBeNull();

    act.click();
    await settle(fixture);
    act.dispatchEvent(new Event('mouseleave'));
    await sleep(fixture, 150);
    expect(bubble()?.textContent).toBe(REASON);
    act.click();
    await settle(fixture);
    expect(bubble()).toBeNull();
  });

  it('R-UI-270 an empty reason leaves the control as it is: it acts, without a tooltip', async () => {
    const { fixture, root, act, host } = await render();
    host.reason.set('');
    await settle(fixture);
    expect(act.hasAttribute('aria-disabled')).toBe(false);
    expect(act.hasAttribute('aria-describedby')).toBe(false);
    expect(root.querySelector<HTMLInputElement>('.field')!.readOnly).toBe(false);
    act.click();
    root.querySelector<HTMLButtonElement>('.send')!.click();
    await settle(fixture);
    expect(host.clicks).toBe(1);
    expect(host.submitted).toBe(1);
    act.dispatchEvent(new Event('mouseenter'));
    await settle(fixture);
    expect(bubble()).toBeNull();
  });
});
