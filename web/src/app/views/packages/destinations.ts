import { InjectionToken, WritableSignal } from '@angular/core';
import { Schema } from '../../api/types';
import { bastionHosts, distributingBastions } from '../../shared/ui/distribution-mark';

/**
 * A place a client installs from besides this installation: one deployment of a Bastion that distributes the repository (bastion.md
 * R-BAS-134, R-BAS-135), named by the Bastion and the deployment's base URL. `client-setup` takes both as `bastion` and `address`.
 */
export interface Destination {
  readonly bastion: string;
  /** The deployment's base URL, one of the Bastion's listed addresses; empty for a Bastion named in a link that offers none. */
  readonly address: string;
}

/** Where the clients of one repository install from: this installation's base URL, and every deployment of a distributing Bastion. */
export interface RepositoryDestinations {
  /** This installation's public base URL without a trailing slash; null where none is configured. */
  readonly main: string | null;
  readonly bastions: readonly Destination[];
}

/**
 * The reads of the places a command installs from (R-UI-271, Q-UI-1657). The main installation binds the repository's member `bastions`
 * (`RepositoryDestinationReads`), read once per repository and page; the Bastion's application binds none, so its panels offer no
 * **From**: a Bastion is the place itself.
 */
export abstract class DestinationReads {
  /** The destinations of a repository (`<project>/<key>`); a read that fails is thrown, and the panel offers no choice. */
  abstract of(repository: string): Promise<RepositoryDestinations>;
  /** The destination chosen in a panel of this page, which every panel of the page follows while it offers it; null: this installation. */
  abstract readonly chosen: WritableSignal<Destination | null>;
}

/** The destinations the views offer; null in an application that is a destination itself (the Bastion's). */
export const DESTINATIONS = new InjectionToken<DestinationReads | null>('DESTINATIONS', {
  providedIn: 'root',
  factory: () => null,
});

/**
 * The deployments a repository's member `bastions` offers as places to install from (R-BAS-007, Q-UI-1656): every listed address of every
 * Bastion that is not disabled, in the member's order and each Bastion's (the lexically first address, which `client-setup` takes without
 * `address`, first). A Bastion without an address, or disabled now, offers none.
 */
export function bastionDestinations(member: readonly Schema<'RepositoryBastion'>[] | null | undefined): Destination[] {
  return distributingBastions(member)
    .filter((b) => !b.disabled)
    .flatMap((b) => b.addresses.map((address) => ({ bastion: b.name, address })));
}

/** Whether two destinations are the same deployment: the same Bastion, and the same base URL without regard to case and a trailing slash. */
export function sameDestination(a: Destination | null, b: Destination | null): boolean {
  if (a === null || b === null) return a === b;
  const norm = (u: string) => u.replace(/\/+$/, '').toLowerCase();
  return a.bastion === b.bastion && norm(a.address) === norm(b.address);
}

/** The option of this installation: "This installation (quader.example.com)", without the host where none is configured. */
export function mainDestinationLabel(main: string | null): string {
  const [host] = main ? bastionHosts([main]) : [];
  return host
    ? $localize`:@@setup.target.main:This installation (${host}:host:)`
    : $localize`:@@setup.target.main-no-host:This installation`;
}

/** The option of a Bastion's deployment: "Bastion artifacts · pkgs.example.com", its name alone where the link named no address. */
export function destinationLabel(d: Destination): string {
  const [host] = d.address ? bastionHosts([d.address]) : [];
  return host
    ? $localize`:@@setup.target.bastion:Bastion ${d.bastion}:name: · ${host}:host:`
    : $localize`:@@setup.target.bastion-no-host:Bastion ${d.bastion}:name:`;
}

/**
 * An address of this installation on a Bastion's deployment: a file's download address (Q-UI-1331) with the deployment's base URL in place
 * of this installation's. Null where the address is not on this installation's base URL, so the panel shows none rather than a wrong one.
 */
export function onDestination(address: string, main: string | null, d: Destination): string | null {
  if (main === null || d.address === '') return null;
  const base = main.replace(/\/+$/, '');
  if (address !== base && !address.startsWith(`${base}/`)) return null;
  return d.address.replace(/\/+$/, '') + address.slice(base.length);
}
