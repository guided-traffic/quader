import { Component } from '@angular/core';
import { TestBed } from '@angular/core/testing';
import { Router, provideRouter, withNavigationErrorHandler } from '@angular/router';
import { BrowserNavigation } from './browser-navigation';
import { isGoneScript, loadGoneScreen } from './gone-script';
import { Notifier } from './notifier';

@Component({ template: 'here' })
class Here {}

const CHROMIUM = 'Failed to fetch dynamically imported module: https://quader.example/ui/chunk-VXCV2FAN.js';

function setup(): { router: Router; assigned: string[]; notifier: Notifier } {
  const assigned: string[] = [];
  TestBed.configureTestingModule({
    providers: [
      provideRouter(
        [
          { path: 'here', component: Here },
          { path: 'gone', loadComponent: () => Promise.reject(new TypeError(CHROMIUM)) },
          { path: 'broken', loadComponent: () => Promise.reject(new Error('the screen threw')) },
        ],
        withNavigationErrorHandler(loadGoneScreen),
      ),
      { provide: BrowserNavigation, useValue: { assign: (url: string) => assigned.push(url) } },
    ],
  });
  return { router: TestBed.inject(Router), assigned, notifier: TestBed.inject(Notifier) };
}

describe('R-UI-213: a link to a screen whose script is gone, and a navigation that fails', () => {
  it('is loaded from the server at the address the link named, with its query and fragment', async () => {
    const { router, assigned, notifier } = setup();
    await router.navigateByUrl('/here');
    await router.navigateByUrl('/gone?tab=tags#row-3').catch(() => undefined);
    expect(assigned).toEqual(['/gone?tab=tags#row-3']);
    expect(notifier.notices()).toEqual([]);
  });

  it('is not loaded a second time by a window that was just loaded: the reader gets the sentence with what the engine said', async () => {
    const { router, assigned, notifier } = setup();
    await router.navigateByUrl('/gone').catch(() => undefined);
    expect(assigned).toEqual([]);
    expect(notifier.notices().map((n) => [n.kind, n.text])).toEqual([
      ['alert', `This screen could not be loaded, its script is missing on the server: ${CHROMIUM}`],
    ]);
  });

  it('says every other failure of a navigation, and loads nothing', async () => {
    const { router, assigned, notifier } = setup();
    await router.navigateByUrl('/here');
    await router.navigateByUrl('/broken').catch(() => undefined);
    expect(assigned).toEqual([]);
    expect(notifier.notices().map((n) => [n.kind, n.text])).toEqual([['alert', 'This screen could not be opened: the screen threw']]);
  });

  it('is told by what each engine says', () => {
    expect(isGoneScript(new TypeError(CHROMIUM))).toBe(true);
    expect(isGoneScript(new TypeError('error loading dynamically imported module: https://quader.example/ui/chunk-A.js'))).toBe(true);
    expect(isGoneScript(new TypeError('Importing a module script failed.'))).toBe(true);
    expect(isGoneScript(new Error('Cannot read properties of undefined'))).toBe(false);
  });
});
