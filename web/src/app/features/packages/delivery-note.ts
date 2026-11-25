import { Component, computed, input } from '@angular/core';
import { RouterLink } from '@angular/router';
import { PackageVersion } from '../../api/packages-api';
import { repositoryCommands } from '../../core/project-links';

type Holder = PackageVersion['repositories'][number];

/** Why a remote does not deliver a version it holds, in the words of the tab Upstream (R-CUR-014, R-CUR-026). */
function reasonText(h: Holder): string {
  switch (h.unavailableReason) {
    case 'blocked':
      return $localize`:@@pkg.delivery.blocked:a rule of the repository blocks it`;
    case 'outsideRange':
      return $localize`:@@pkg.delivery.outside-range:it lies outside the range of versions the repository allows`;
    default:
      return $localize`:@@pkg.delivery.not-allowed:the repository delivers only what a rule allows, and no rule allows it`;
  }
}

/**
 * The remote repositories that hold a version and whose delivery rules do not deliver it (R-CUR-014, Q-UI-1260): a note at the top of the
 * version's page for each, with its reason and the way to the tab Upstream that decides it, for every format. Nothing for a version every
 * holder delivers. `rulesName` is the name the rules see, which a role's page gives (`role/<id>`, R-ANS-056) and the package's name otherwise.
 */
@Component({
  selector: 'quader-delivery-note',
  imports: [RouterLink],
  template: `
    @for (h of refused(); track h.key) {
      <p class="banner-warning" role="note" data-testid="delivery-note">
        <span class="material-icons inline warn" aria-hidden="true">block</span>
        <span
          ><strong i18n="@@pkg.delivery.heading">Not delivered by</strong>{{ ' ' }}<span translate="no">{{ h.key }}</span>: {{ reason(h) }}.
          <span i18n="@@pkg.delivery.consequence">A client cannot install this version through it.</span>&ngsp;<a
            [routerLink]="upstream(h)"
            [queryParams]="{ name: rulesName() }"
            i18n="@@pkg.delivery.link"
            >Delivery in the tab Upstream</a
          ></span
        >
      </p>
    }
  `,
})
export class DeliveryNote {
  readonly version = input.required<PackageVersion>();
  readonly rulesName = input.required<string>();
  protected readonly refused = computed(() => this.version().repositories.filter((r) => r.class === 'remote' && r.unavailableReason));
  protected reason(h: Holder): string {
    return reasonText(h);
  }
  protected upstream(h: Holder): string[] {
    return [...repositoryCommands(h.key), 'upstream'];
  }
}
