import { Component, computed, input } from '@angular/core';
import { Schema } from '../../api/types';
import { Tooltip } from './tooltip';

/** An entry of a repository's member `bastions` as a read gives it (bastion.md R-BAS-134): its name, addresses and state. */
type RepositoryBastion = Schema<'RepositoryBastion'>;

/** A Bastion that distributes a repository, as the screens show it. */
export interface DistributingBastion {
  readonly name: string;
  /** The base URLs of its deployments the installation lists; empty before its first contact. */
  readonly addresses: readonly string[];
  readonly disabled: boolean;
}

/** The member `bastions` of a read as the screens use it: a written name counts as a Bastion without an address. */
export function distributingBastions(member: readonly RepositoryBastion[] | null | undefined): DistributingBastion[] {
  return (member ?? []).map((b) =>
    typeof b === 'string' ? { name: b, addresses: [], disabled: false } : { name: b.name, addresses: b.addresses ?? [], disabled: b.disabled ?? false },
  );
}

/** The hosts of a Bastion's base URLs, in their order, without duplicates; an address that is no URL is shown as written. */
export function bastionHosts(addresses: readonly string[]): string[] {
  const hosts = addresses.map((a) => {
    try {
      return new URL(a).host;
    } catch {
      return a;
    }
  });
  return [...new Set(hosts)];
}

/** One Bastion in the sentence of the mark: its name, its hosts in brackets, and "(disabled now)" while it is disabled. */
function namedBastion(b: DistributingBastion): string {
  const hosts = bastionHosts(b.addresses).join(', ');
  const named = hosts ? `${b.name} (${hosts})` : b.name;
  return b.disabled ? `${named} ${$localize`:@@distribution.disabled-now:(disabled now)`}` : named;
}

/**
 * The sentence of the mark (R-UI-271): the Bastion that also distributes the repository with its host, or every one of several; empty
 * while none does.
 */
export function distributionText(bastions: readonly DistributingBastion[]): string {
  const [b] = bastions;
  if (b === undefined) return '';
  if (bastions.length === 1) {
    const hosts = bastionHosts(b.addresses).join(', ');
    const one = hosts
      ? $localize`:@@distribution.mark:Also distributed through the Bastion ${b.name}:name: (${hosts}:host:)`
      : $localize`:@@distribution.mark-no-host:Also distributed through the Bastion ${b.name}:name:`;
    return b.disabled ? `${one} ${$localize`:@@distribution.disabled-now:(disabled now)`}` : one;
  }
  const names = bastions.map(namedBastion).join(', ');
  return $localize`:@@distribution.mark-many:Also distributed through the Bastions ${names}:names:`;
}

/**
 * The distribution mark (R-UI-271, Q-UI-1644): the Bastion's tower beside a repository's visibility, saying that the repository also
 * leaves the network through a Bastion, and through which, in its tooltip. The tooltip shows on hover and on focus, and a tap pins it
 * (R-UI-267), so it reads on a touch screen as well. Nothing while the repository is distributed through no Bastion.
 *
 * In a list and a heading it is the icon alone, after the key (the key keeps its column, Q-UI-425); in a repository's head (`badge`)
 * it is the badge "Bastion" right after the visibility's badge. The sentence is its accessible name, as the visibility icon's level is.
 *
 *   <quader-distribution-mark [bastions]="row.bastions" />
 *   <quader-distribution-mark [bastions]="repository.bastions" [badge]="true" />
 */
@Component({
  selector: 'quader-distribution-mark',
  imports: [Tooltip],
  template: `
    @if (text(); as t) {
      @if (badge()) {
        <span class="badge" tabindex="0" data-testid="distribution-mark" [quaderTooltip]="t" [quaderTooltipPin]="true"
          ><span class="material-icons" aria-hidden="true">fort</span>{{ badgeWord }}</span
        >
      } @else {
        <!-- The space between the key and the mark belongs to the line that holds them; a flex row (a heading's) drops it and keeps its gap. -->
        &ngsp;<span
          class="material-icons inline mark"
          role="img"
          tabindex="0"
          data-testid="distribution-mark"
          [attr.aria-label]="t"
          [quaderTooltip]="t"
          [quaderTooltipPin]="true"
          >fort</span
        >
      }
    }
  `,
  styles: `
    :host {
      display: contents;
    }
    /* The visibility icon's colour: the two marks read as one pair. */
    .mark {
      cursor: default;
    }
  `,
})
export class DistributionMark {
  /** The repository's member `bastions` as read. */
  readonly bastions = input<readonly RepositoryBastion[] | null | undefined>([]);
  /** The badge of a repository's head instead of the icon of a list or a heading. */
  readonly badge = input(false);

  protected readonly badgeWord = $localize`:@@distribution.badge:Bastion`;
  protected readonly text = computed(() => distributionText(distributingBastions(this.bastions())));
}
