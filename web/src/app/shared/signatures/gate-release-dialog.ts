import { Component, computed, inject, signal } from '@angular/core';
import { ApiError } from '../../api/problem';
import { SignatureGateRefusedReason, SignatureRelease, SignaturesApi } from '../../api/signatures-api';
import { TextField } from '../access/fields';
import { ErrorNotice } from '../error-notice';
import { fieldErrorAt } from '../field-errors';
import { asApiError } from '../paged-resource';
import { DIALOG_DATA, DialogClose, DialogRef, DialogTitle } from '../ui/dialog';
import { gateReasonSentence } from '../../views/signatures/gate-words';
import { Busy } from '../ui/busy';

/** The longest reason the API takes (R-SIG-070). */
export const GATE_RELEASE_REASON_MAX = 1000;

/** What the dialog releases: one artifact, by its digest, in one repository, and why that repository's gate refuses it now. */
export interface GateReleaseData {
  /** `<project>/<key>`. */
  readonly repository: string;
  readonly sha256: string;
  /** The artifact as a reader names it: the package and version, or the file. */
  readonly artifact: string;
  readonly reason: SignatureGateRefusedReason;
}

/**
 * **Release for this repository** (R-SIG-070, R-SIG-074): the dialog names the one repository and the reason its gate refuses the
 * artifact, says that the release serves it from that repository alone until it is withdrawn, and asks for the reason, which the API
 * requires and keeps. The same dialog stands at a refused download of the security dashboard, at a refused version's banner and in a
 * file's details. A refusal of the API stays in the dialog; `409 nothing_to_release` and `409 already_released` are said in the gate's
 * words, since the malware scan's release uses the same codes.
 */
@Component({
  selector: 'quader-gate-release-dialog',
  imports: [Busy, DialogTitle, DialogClose, ErrorNotice, TextField],
  template: `
    <h2 quaderDialogTitle>{{ title() }}</h2>
    <form (submit)="submit($event)" novalidate>
      <div class="dialog-body">
        @if (conflict(); as text) {
          <p class="banner-warning" role="alert" data-testid="release-conflict">
            <span class="material-icons inline warn" aria-hidden="true">info</span><span>{{ text }}</span>
          </p>
        } @else if (error(); as e) {
          @if (reasonError() === null) {
            <quader-error-notice [error]="e" />
          }
        }
        <p data-testid="release-refusal">
          <ng-container i18n="@@sig.release.refuses"
            ><code translate="no">{{ data.repository }}</code> serves only artifacts with a verified signature and refuses
            <span class="with-stop"><code translate="no">{{ data.artifact }}</code>.</span></ng-container
          >&ngsp;{{ sentence }}
        </p>
        <p i18n="@@sig.release.effect">
          A release serves it from this repository only, whatever its signatures say, until it is withdrawn. Every other repository keeps
          its own decision.
        </p>
        <p class="digest-line"><span class="muted" translate="no">SHA-256</span>&ngsp;<code class="digest" translate="no">{{ data.sha256 }}</code></p>
        <quader-text-field
          width="full"
          i18n-label="@@sig.release.reason"
          label="Reason"
          name="reason"
          type="textarea"
          [(value)]="reason"
          [error]="reasonError()"
          [hint]="reasonHint"
        />
      </div>
      <div class="dialog-actions">
        <button class="btn btn-tertiary" type="button" quaderDialogClose i18n="@@sig.release.cancel">Cancel</button>
        <button class="btn btn-confirm" type="submit" [disabled]="busy()" [quaderBusy]="busy()" i18n="@@sig.release.submit">Release for this repository</button>
      </div>
    </form>
  `,
  styles: `
    .digest {
      overflow-wrap: anywhere;
    }
    .digest-line {
      color: var(--quader-text-muted);
    }
    .muted {
      color: var(--quader-text-muted);
    }
    code {
      overflow-wrap: anywhere;
    }
  `,
})
export class GateReleaseDialog {
  protected readonly data = inject<GateReleaseData>(DIALOG_DATA);
  private readonly api = inject(SignaturesApi);
  private readonly ref = inject<DialogRef<SignatureRelease, GateReleaseDialog>>(DialogRef);

  protected readonly reason = signal('');
  protected readonly busy = signal(false);
  protected readonly error = signal<ApiError | null>(null);
  private readonly submitted = signal(false);

  protected readonly sentence = gateReasonSentence(this.data.reason);
  protected readonly reasonHint = $localize`:@@sig.release.reason-hint:Why this artifact may be downloaded from this repository without a verified signature, for example a vendor who signs from the next release on. At most 1,000 characters; kept with the release.`;
  protected readonly title = computed(() => $localize`:@@sig.release.title:Release ${this.data.artifact}:artifact: for this repository?`);
  protected readonly reasonError = computed(() => {
    const text = this.reason().trim();
    if (this.submitted() && text === '') return $localize`:@@sig.release.reason-required:A release needs a reason.`;
    if (text.length > GATE_RELEASE_REASON_MAX) return $localize`:@@sig.release.reason-long:The reason is longer than 1,000 characters.`;
    return fieldErrorAt(this.error(), '/reason');
  });
  /** The two conflicts in the gate's words. */
  protected readonly conflict = computed(() => {
    switch (this.error()?.code) {
      case 'nothing_to_release':
        return $localize`:@@sig.release.nothing:There is nothing to release: this repository's gate lets the artifact pass now.`;
      case 'already_released':
        return $localize`:@@sig.release.already:This artifact is released for this repository already. Load the page again to see the release.`;
      default:
        return null;
    }
  });

  protected async submit(event: Event): Promise<void> {
    event.preventDefault();
    this.submitted.set(true);
    const text = this.reason().trim();
    if (this.busy() || text === '' || text.length > GATE_RELEASE_REASON_MAX) return;
    this.busy.set(true);
    this.error.set(null);
    try {
      this.ref.close(await this.api.release(this.data.repository, this.data.sha256, text));
    } catch (e) {
      this.error.set(asApiError(e));
    } finally {
      this.busy.set(false);
    }
  }
}
