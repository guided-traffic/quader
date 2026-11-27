import { InjectionToken, Signal, signal } from '@angular/core';

/**
 * Whether the views name the repositories that hold a package (R-UI-223, R-UI-269, Q-UI-1632): true by default, which is the main
 * installation's, which never binds it. A Bastion whose switch **Hide repositories** is on binds it to false (bastion.md, the W-40 plan's
 * D14): the views then leave out the column and the fact **Repositories** of a package's page, the table **Repositories** of a version's
 * Details, the select **Repository** of a command's panel, the heading of each card of a version's findings, the line "Repository: …" of
 * an Ansible collection's documentation and the keys of a delivery note. A command, a download address and Set up client still carry a
 * repository's key: a client needs it.
 */
export const REPOSITORIES_SHOWN = new InjectionToken<Signal<boolean>>('REPOSITORIES_SHOWN', {
  providedIn: 'root',
  factory: () => signal(true).asReadonly(),
});
