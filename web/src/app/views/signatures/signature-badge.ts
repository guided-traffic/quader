import { ChangeDetectionStrategy, Component, computed, input } from '@angular/core';
import { SignatureBadgeState } from '../../api/signatures-api';
import { signatureBadge } from './signature-words';

/**
 * The badge of an artifact's signatures (R-SIG-009, R-SIG-035, Q-SIG-1): a shield of the icon font and the words **Verified · public**,
 * **Verified · internal**, **Signed** (with "signer not trusted" after it) or **Invalid signature**, a pill as every status of the design language.
 * Green for the two verified states only (Q-UI-96), neutral for a signer no anchor knows, red for an invalid signature; the state is
 * always its words, never its colour alone. The three states without a badge (not checked yet, scheme not supported, no signature)
 * draw nothing: the details say which. `stale` adds that the trust is being recomputed after a change of anchors (R-SIG-012), as
 * the badge's title and for assistive technology. The untrusted state's badge says **Signed** and the words "signer not trusted" follow
 * it as muted text (Q-UI-1076): as one pill, its German words were wider than a file's cell at 390 px, and a badge does not break
 * (R-UI-171). In a list's cell the words are the cell's second line (`.secondary`, as a refused file's reason under **Refused here**;
 * in a version's cell of the list of versions, which shows the host as `display: contents`, the line under the version),
 * elsewhere (a header's facts, a fact, the details) they follow the badge on its line; either way they wrap between their words.
 * `data-state` names the state whatever the language (the renders' walks find it by it).
 */
@Component({
  selector: 'quader-signature-badge',
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    @if (look(); as b) {
      <span
        [class]="b.tone === 'neutral' ? 'badge' : 'badge ' + b.tone"
        [attr.title]="title()"
        [attr.data-state]="state()"
        data-testid="signature-badge"
        ><span class="material-icons" aria-hidden="true">{{ b.icon }}</span>{{ b.word }}@if (stale()) {<span class="visually-hidden">{{ staleText }}</span>}</span
      >@if (b.note) {&ngsp;<span class="note secondary" data-testid="signature-note">{{ b.note }}@if (sentence()) {.}</span>}
    }
  `,
  styles: `
    :host {
      display: inline;
    }
    :host:empty {
      display: none;
    }
    /* The rest of a state's words after its badge, muted as a refused file's reason, also inside a link (the version's header). */
    .note {
      color: var(--quader-text-muted);
      font-weight: 400;
    }
    /* Where the host hands its boxes to a wrapping row (a version's cell in the list of versions), the words take a line of their own. */
    .note.secondary {
      flex-basis: 100%;
    }
  `,
})
export class SignatureBadge {
  /** The badge's state of the member `signatures`; null or a state without a badge draws nothing. */
  readonly state = input<SignatureBadgeState | null | undefined>(null);
  /** A trust row that decides the badge was computed before the last change of anchors, blocks or the switch. */
  readonly stale = input(false);
  /** The badge opens a sentence (the details' state line): the words after it end with a full stop before the sentence goes on. */
  readonly sentence = input(false);

  protected readonly look = computed(() => signatureBadge(this.state()));
  protected readonly staleText = $localize`:@@sig.badge.stale:Trust is being recomputed after a change of trust anchors.`;
  protected readonly title = computed(() => (this.stale() ? this.staleText : null));
}
