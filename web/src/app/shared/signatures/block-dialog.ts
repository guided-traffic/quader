import { Component, computed, inject, signal } from '@angular/core';
import { ApiError } from '../../api/problem';
import { SIGNATURE_BLOCK_KINDS, SignatureBlock, SignatureBlockKind, SignatureImpact, SignaturesApi } from '../../api/signatures-api';
import { impactItems, impactQuestion } from '../../views/signatures/gate-words';
import { Notifier } from '../../core/notifier';
import { SelectField, SelectOption, TextField } from '../access/fields';
import { ErrorNotice } from '../error-notice';
import { fieldErrorAt } from '../field-errors';
import { asApiError } from '../paged-resource';
import { DialogClose, DialogRef, DialogTitle } from '../ui/dialog';
import { blockKindWords } from '../../views/signatures/signature-words';
import { Busy } from '../ui/busy';

/**
 * **Block signer** (R-SIG-020): the kind (a certificate's or an OpenPGP key's fingerprint, an issuer and a serial, npm's or a raw key's
 * ID, a Sigstore identity with its issuer), the value and a reason. A block acts at once on every signature of the signer in every
 * scope: their badge falls to **Signed · signer not trusted** and the details say who blocked it, when and why.
 */
@Component({
  selector: 'quader-block-dialog',
  imports: [Busy, DialogClose, DialogTitle, ErrorNotice, SelectField, TextField],
  template: `
    <h2 quaderDialogTitle i18n="@@sig.block.heading">Block signer</h2>
    <form (submit)="submit($event)" novalidate>
      <div class="dialog-body">
        @if (otherError(); as e) {
          <quader-error-notice [error]="e" />
        }
        <quader-select-field i18n-label="@@sig.block.kind" label="Kind" name="kind" [options]="kindOptions" [value]="kind()" (valueChange)="kind.set($any($event))" />
        @if (kind() === 'identity') {
          <quader-text-field width="full" i18n-label="@@sig.block.issuer" label="OIDC issuer" name="issuer" [(value)]="issuer" [required]="true" [error]="err('/issuer')" />
        }
        <quader-text-field width="full" i18n-label="@@sig.block.value" label="Value" name="value" [(value)]="value" [required]="true" [error]="err('/value')" [hint]="valueHint()" />
        <quader-text-field width="full" type="textarea" i18n-label="@@sig.block.reason" label="Reason" name="reason" [(value)]="reason" [required]="true" [error]="err('/reason')"
          i18n-hint="@@sig.block.reason-hint" hint="Shown at every signature of this signer." />
        <p class="hint" i18n="@@sig.block.effect">The block acts at once on every signature of this signer, in every project.</p>
        <!-- What the block refuses in the repositories that serve only verified artifacts (R-SIG-071, R-SIG-074): asked before it is
             saved, and nothing is asked when it refuses nothing. -->
        @if (impact(); as i) {
          <div class="banner-warning" role="alert" data-testid="block-impact">
            <span class="material-icons inline warn" aria-hidden="true">warning</span>
            <div>
              <p>{{ question(i) }}</p>
              <ul class="impact-items">
                @for (item of items(i); track item) {
                  <li translate="no">{{ item }}</li>
                }
              </ul>
            </div>
          </div>
        }
      </div>
      <div class="dialog-actions">
        <button class="btn btn-tertiary" type="button" quaderDialogClose i18n="@@sig.form.cancel">Cancel</button>
        <button class="btn btn-danger" type="submit" [disabled]="busy()" [quaderBusy]="busy()">
          @if (impact()) {
            <ng-container i18n="@@sig.block.submit-anyway">Block signer and refuse them</ng-container>
          } @else {
            <ng-container i18n="@@sig.block.submit">Block signer</ng-container>
          }
        </button>
      </div>
    </form>
  `,
  styleUrl: '../access/access.scss',
  styles: `
    .impact-items {
      margin: var(--quader-space-1) 0 0;
      padding-inline-start: var(--quader-space-5);
    }
  `,
})
export class BlockDialog {
  private readonly api = inject(SignaturesApi);
  private readonly ref = inject<DialogRef<SignatureBlock, BlockDialog>>(DialogRef);
  private readonly notifier = inject(Notifier);

  protected readonly kindOptions: readonly SelectOption[] = SIGNATURE_BLOCK_KINDS.map((k) => ({ value: k, label: blockKindWords(k) }));
  protected readonly kind = signal<SignatureBlockKind>('fingerprint');
  protected readonly value = signal('');
  protected readonly issuer = signal('');
  protected readonly reason = signal('');
  protected readonly busy = signal(false);
  protected readonly error = signal<ApiError | null>(null);
  /** The impact shown for the block as it was asked; a change of the kind, the value or the issuer asks again. */
  protected readonly impact = signal<SignatureImpact | null>(null);
  private askedFor = '';
  protected readonly question = impactQuestion;
  protected readonly items = impactItems;
  protected readonly otherError = computed(() => {
    const e = this.error();
    return e && e.fieldErrors.size === 0 ? e : null;
  });
  protected readonly valueHint = computed(() => {
    switch (this.kind()) {
      case 'fingerprint':
        return $localize`:@@sig.block.hint-fingerprint:The SHA-256 fingerprint of a certificate, or an OpenPGP key's fingerprint.`;
      case 'serial':
        return $localize`:@@sig.block.hint-serial:The issuer's name and the certificate's serial, separated by a vertical bar.`;
      case 'keyid':
        return $localize`:@@sig.block.hint-keyid:npm's key ID, or the SHA-256 of a raw public key.`;
      default:
        return $localize`:@@sig.block.hint-identity:The identity, exact or as a pattern of the whole identity.`;
    }
  });

  protected err(pointer: string): string | null {
    return fieldErrorAt(this.error(), pointer);
  }

  protected async submit(event: Event): Promise<void> {
    event.preventDefault();
    if (this.busy()) return;
    this.busy.set(true);
    this.error.set(null);
    try {
      const kind = this.kind();
      const block = { kind, value: this.value().trim(), ...(kind === 'identity' ? { issuer: this.issuer().trim() } : {}) };
      const key = JSON.stringify(block);
      if (key !== this.askedFor && block.value !== '') {
        this.askedFor = key;
        this.impact.set(null);
        const impact = await this.api.impact({ block }).catch(() => null);
        if (impact && impact.artifacts > 0) {
          this.impact.set(impact);
          return;
        }
      }
      const created = await this.api.createBlock({
        kind,
        value: this.value().trim(),
        reason: this.reason().trim(),
        ...(kind === 'identity' ? { issuer: this.issuer().trim() } : {}),
      });
      this.notifier.status($localize`:@@sig.block.created:Signer blocked. Its signatures read "Signed · signer not trusted" once the trust is recomputed.`);
      this.ref.close(created);
    } catch (e) {
      this.error.set(asApiError(e));
    } finally {
      this.busy.set(false);
    }
  }
}
