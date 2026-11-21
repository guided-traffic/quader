import { ChangeDetectionStrategy, Component, computed, input } from '@angular/core';
import { SignatureBadgeState } from '../../api/signatures-api';
import { signatureBadge } from './signature-words';

/**
 * The badge of an artifact's signatures (R-SIG-009, R-SIG-035, Q-SIG-1): a shield of the icon font and the words **Verified · public**,
 * **Verified · internal**, **Signed · signer not trusted** or **Invalid signature**, a pill as every status of the design language.
 * Green for the two verified states only (Q-UI-96), neutral for a signer no anchor knows, red for an invalid signature; the state is
 * always its words, never its colour alone. The three states without a badge (not checked yet, scheme not supported, no signature)
 * draw nothing: the details say which. `stale` adds that the trust is being recomputed after a change of anchors (R-SIG-012), as
 * the badge's title and for assistive technology.
 */
@Component({
  selector: 'quader-signature-badge',
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    @if (look(); as b) {
      <span [class]="b.tone === 'neutral' ? 'badge' : 'badge ' + b.tone" [attr.title]="title()" data-testid="signature-badge"
        ><span class="material-icons" aria-hidden="true">{{ b.icon }}</span>{{ b.word }}@if (stale()) {<span class="visually-hidden">{{ staleText }}</span>}</span
      >
    }
  `,
  styles: `
    :host {
      display: inline;
    }
    :host:empty {
      display: none;
    }
  `,
})
export class SignatureBadge {
  /** The badge's state of the member `signatures`; null or a state without a badge draws nothing. */
  readonly state = input<SignatureBadgeState | null | undefined>(null);
  /** A trust row that decides the badge was computed before the last change of anchors, blocks or the switch. */
  readonly stale = input(false);

  protected readonly look = computed(() => signatureBadge(this.state()));
  protected readonly staleText = $localize`:@@sig.badge.stale:Trust is being recomputed after a change of trust anchors.`;
  protected readonly title = computed(() => (this.stale() ? this.staleText : null));
}
