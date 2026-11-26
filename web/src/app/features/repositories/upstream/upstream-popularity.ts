import { DecimalPipe } from '@angular/common';
import { ChangeDetectionStrategy, Component, computed, input } from '@angular/core';
import { PackageTags } from '../../../views/packages/package-tags';

/**
 * What the upstream tells beside a package on the tab Upstream (R-ANS-047, Q-ANS-129): its own word that the package is signed, the
 * downloads it counts, named as the upstream's ("downloads at the upstream", apart from Quader's own count, which only a stored version
 * has), and its tags as text. Each only where the upstream's answer carries it; nothing at all for a format that tells none.
 * "Signed at the upstream" is the upstream's claim, not Quader's verification: a neutral badge, never the green of a verified one.
 */
@Component({
  selector: 'quader-upstream-popularity',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [DecimalPipe, PackageTags],
  template: `
    @if (signed() || count() !== null) {
      <span class="secondary popularity-line" data-testid="upstream-popularity">
        @if (signed()) {
          <span class="badge" data-testid="upstream-signed"
            ><span class="material-icons" aria-hidden="true">verified_user</span
            ><ng-container i18n="@@upstream.signed">Signed at the upstream</ng-container></span
          >
        }
        @if (count(); as n) {
          <span data-testid="upstream-downloads" i18n="@@upstream.downloads"
            >{n, plural, =1 {1 download at the upstream} other {{{ n | number }} downloads at the upstream}}</span
          >
        } @else if (count() === 0) {
          <span data-testid="upstream-downloads" i18n="@@upstream.downloads-none">No download at the upstream</span>
        }
      </span>
    }
    <quader-package-tags [tags]="tags()" [project]="false" />
  `,
  styles: `
    :host {
      display: block;
    }
    .popularity-line {
      display: flex;
      flex-wrap: wrap;
      align-items: baseline;
      gap: var(--quader-space-1) var(--quader-space-3);
    }
  `,
})
export class UpstreamPopularity {
  readonly keywords = input<readonly string[] | undefined>(undefined);
  readonly downloads = input<number | null | undefined>(undefined);
  readonly signed = input<boolean | undefined>(undefined);

  protected readonly tags = computed(() => this.keywords() ?? []);
  protected readonly count = computed(() => this.downloads() ?? null);
}
