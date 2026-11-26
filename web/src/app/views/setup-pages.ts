import { InjectionToken } from '@angular/core';

/**
 * Whether the application has the page Set up client of a repository (R-UI-126) that a panel of commands leads to
 * (views/packages/client-command-menu.ts): the main installation's does; the Bastion's application has none and provides false, so a
 * panel offers no link that ends at its page not found (Q-UI-1456). A module of its own, so that an application's providers name it
 * without loading the panel.
 */
export const SETUP_PAGES = new InjectionToken<boolean>('SETUP_PAGES', { providedIn: 'root', factory: () => true });
