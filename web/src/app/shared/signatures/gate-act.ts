import { ChangeDetectionStrategy, Component, computed, inject, input, signal } from '@angular/core';
import { asApiError } from '../paged-resource';
import { Busy } from '../ui/busy';
import { ActSlot, GateActArtifact, GateActState } from '../../views/acts';
import { GateActions } from './gate-releases';

/**
 * The acts of a repository's signature gate at a place of a view (R-SIG-070, R-SIG-074, `GATE_ACT` of views/acts.ts): **Release for this
 * repository** under a refused artifact, in the gate's box (`place: 'gate'`) or in the banner above a version's tabs (`place: 'banner'`),
 * and **Withdraw release** under a released one. The gate shows the act's refusal above its decision (`state`).
 */
@Component({
  selector: 'quader-gate-act',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [Busy],
  template: `
    @if (place() === 'banner') {
      <p class="gate-banner-actions">
        <button class="btn btn-default" type="button" (click)="release()">
          <ng-container i18n="@@sig.release.action">Release for this repository</ng-container
          ><span class="visually-hidden" translate="no">&ngsp;{{ repository() }}</span>
        </button>
      </p>
    } @else if (act() === 'release') {
      <p class="gate-actions">
        <button class="btn btn-default" type="button" [disabled]="busy()" [quaderBusy]="busy()" (click)="release()">
          <ng-container i18n="@@sig.release.action">Release for this repository</ng-container
          ><span class="visually-hidden" translate="no">&ngsp;{{ artifact().name }}</span>
        </button>
      </p>
    } @else {
      <p class="gate-actions">
        <button class="btn btn-default" type="button" [disabled]="busy()" [quaderBusy]="busy()" (click)="withdraw()" i18n="@@sig.withdraw">Withdraw release</button>
      </p>
    }
  `,
  styles: `
    /* The act stands where the view put its place, as a line of the gate's box or of the banner. */
    :host {
      display: contents;
    }
    /* The gate's paragraphs (signature-gate.ts): no room above, a step below, none after the decision's last line. */
    .gate-actions {
      margin: 0 0 var(--quader-space-2);
    }
    :host(:last-child) .gate-actions {
      margin-block-end: 0;
    }
    /* Under the banner's sentence (package-version-view.scss). */
    .gate-banner-actions {
      margin-block-start: var(--quader-space-2);
    }
  `,
})
export class GateAct {
  private readonly actions = inject(GateActions);

  readonly act = input.required<'release' | 'withdraw'>();
  readonly place = input.required<'gate' | 'banner'>();
  /** `<project>/<key>` of the gate's repository. */
  readonly repository = input.required<string>();
  readonly artifact = input.required<GateActArtifact>();
  /** Whether an act of the gate runs and its refusal, which the gate shows; null in a banner. */
  readonly state = input<GateActState | null>(null);
  /** Called after a release was granted or withdrawn: the view reads its answers again. */
  readonly changed = input<() => void>(() => undefined);

  private readonly idle = signal(false);
  protected readonly busy = computed(() => (this.state()?.busy ?? this.idle)());

  protected async release(): Promise<void> {
    this.state()?.error.set(null);
    const a = this.artifact();
    const done = await this.actions.release({ repository: this.repository(), sha256: a.sha256, artifact: a.name, reason: a.reason });
    if (done) this.changed()();
  }

  protected async withdraw(): Promise<void> {
    const state = this.state();
    const a = this.artifact();
    state?.busy.set(true);
    state?.error.set(null);
    try {
      if (await this.actions.withdraw(this.repository(), a.sha256, a.name)) this.changed()();
    } catch (e) {
      state?.error.set(asApiError(e));
    } finally {
      state?.busy.set(false);
    }
  }
}

/**
 * The gate's acts as the main installation's pages provide them (`{ provide: GATE_ACT, useFactory: gateActSlot }`): offered to the holder
 * of `vulnerabilities:triage` (R-SIG-070).
 */
export function gateActSlot(): ActSlot {
  const actions = inject(GateActions);
  return { component: GateAct, offered: computed(() => actions.canRelease()) };
}
