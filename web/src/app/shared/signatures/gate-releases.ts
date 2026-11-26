import { Injectable, inject } from '@angular/core';
import { firstValueFrom } from 'rxjs';
import { SignatureImpactRequest, SignatureRelease, SignaturesApi } from '../../api/signatures-api';
import { Notifier } from '../../core/notifier';
import { SessionStore } from '../../core/session-store';
import { Confirm, ConfirmData } from '../confirm-dialog';
import { QuaderDialog } from '../ui/dialog';
import { GateReleaseData, GateReleaseDialog } from './gate-release-dialog';
import { impactItems, impactQuestion } from '../../views/signatures/gate-words';

/**
 * The actions of a repository's gate that more than one screen offers (R-SIG-070, R-SIG-071, R-SIG-074): **Release for this repository**
 * with its dialog, **Withdraw release** with its question, and the impact asked before a change of anchors, blocks or the switch.
 */
@Injectable({ providedIn: 'root' })
export class GateActions {
  private readonly api = inject(SignaturesApi);
  private readonly dialog = inject(QuaderDialog);
  private readonly confirm = inject(Confirm);
  private readonly notifier = inject(Notifier);
  private readonly session = inject(SessionStore);

  /**
   * Who may release and withdraw (R-SIG-070): the holder of `vulnerabilities:triage`. The second half of the right, `read` on the
   * repository or `content:metadata-read`, is held by whoever sees the artifact on a screen; the API answers 404 otherwise.
   */
  canRelease(): boolean {
    return this.session.has('vulnerabilities:triage');
  }

  /** Opens the dialog; resolves with the release, or null when it was cancelled. */
  async release(data: GateReleaseData): Promise<SignatureRelease | null> {
    const ref = this.dialog.open<GateReleaseDialog, GateReleaseData, SignatureRelease>(GateReleaseDialog, { data, maxWidth: '40rem' });
    const released = await firstValueFrom(ref.closed);
    if (!released) return null;
    const { artifact, repository } = data;
    this.notifier.status($localize`:@@sig.release.done:${artifact}:artifact: is released for ${repository}:repository:.`);
    return released;
  }

  /** Asks, then withdraws; resolves true when the release is gone, false when the question was cancelled. A refusal rejects. */
  async withdraw(repository: string, sha256: string, artifact: string): Promise<boolean> {
    const answer = await this.confirm.ask({
      title: $localize`:@@sig.withdraw.title:Withdraw the release of ${artifact}:artifact:?`,
      lines: [
        $localize`:@@sig.withdraw.text:${repository}:repository: refuses it again from the next download, unless a signature it accepts arrives.`,
      ],
      confirmLabel: $localize`:@@sig.withdraw:Withdraw release`,
    });
    if (answer === null) return false;
    await this.api.withdraw(repository, sha256);
    this.notifier.status($localize`:@@sig.withdraw.done:The release of ${artifact}:artifact: is withdrawn.`);
    return true;
  }

  /**
   * Asks with the impact of a change before it is saved (R-SIG-071): nothing more when it refuses nothing. `base` is the question the
   * screen asks anyway (a removal's), or null where it asks only for an impact. Resolves true to go on. An impact that cannot be read
   * (the checks switched off, a failure) leaves the screen's own question, or none.
   */
  async askImpact(change: SignatureImpactRequest, base: ConfirmData | null, fallback: Omit<ConfirmData, 'lines'> & { readonly lines?: readonly string[] }): Promise<boolean> {
    let lines: string[] = [];
    let items: readonly string[] | undefined;
    try {
      const impact = await this.api.impact(change);
      if (impact.artifacts > 0) {
        lines = [impactQuestion(impact)];
        items = impactItems(impact);
      }
    } catch {
      // The count is a help, not a condition: the change is the administrator's either way.
    }
    if (lines.length === 0 && base === null) return true;
    const data: ConfirmData = base
      ? { ...base, lines: [...base.lines, ...lines], ...(items ? { items } : {}) }
      : { ...fallback, lines: [...(fallback.lines ?? []), ...lines], ...(items ? { items } : {}) };
    return (await this.confirm.ask(data)) !== null;
  }
}
