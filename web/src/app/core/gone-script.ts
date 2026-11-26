import { Location } from '@angular/common';
import { inject } from '@angular/core';
import { NavigationError, Router } from '@angular/router';
import { BrowserNavigation } from './browser-navigation';
import { Notifier } from './notifier';

/**
 * What the three engines say when the script of a screen cannot be loaded: Chromium "Failed to fetch dynamically imported module",
 * Firefox "error loading dynamically imported module", WebKit "Importing a module script failed.".
 */
const GONE_SCRIPT = /dynamically imported module|importing a module script failed/i;

/** Whether a navigation failed because the script of its screen could not be loaded. */
export function isGoneScript(error: unknown): boolean {
  return GONE_SCRIPT.test(error instanceof Error ? error.message : String(error));
}

/**
 * The router's handler of a failed navigation (R-UI-213, Q-UI-1370). Without one a navigation that fails leaves the window where it
 * was and says nothing: the reader clicked a link and nothing happened.
 *
 * A screen's script is loaded when the screen is first opened, under a name that changes with every build; an open window still asks
 * for the names of the build it was loaded from, so a link to a screen the window had not opened yet finds no script once the server
 * holds another build. The handler loads the link's address from the server instead: the window gets the build that is served, and
 * the screen the link named. A window that has not ended a navigation yet was just loaded from the server: a script missing there is
 * missing on the server, loading again would not find it, and that is also what stops a second load.
 *
 * Every failure it does not mend is said, with what the engine or the server said of it.
 */
export function loadGoneScreen(error: NavigationError): void {
  const said = error.error instanceof Error ? error.error.message : String(error.error);
  if (!isGoneScript(error.error)) {
    inject(Notifier).alert($localize`:@@nav.failed:This screen could not be opened: ${said}:message:`);
    return;
  }
  if (!inject(Router).navigated) {
    inject(Notifier).alert($localize`:@@nav.script-missing:This screen could not be loaded, its script is missing on the server: ${said}:message:`);
    return;
  }
  inject(BrowserNavigation).assign(inject(Location).prepareExternalUrl(error.url));
}
