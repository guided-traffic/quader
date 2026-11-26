import { personName } from '../../shared/person-name';
import { NgComponentOutlet } from '@angular/common';
import { ChangeDetectionStrategy, Component, computed, inject, input, output, resource, signal } from '@angular/core';
import { ApiError } from '../../api/problem';
import { SignatureReads } from '../../api/signature-reads';
import { SignatureEntry, SignatureGateRefusedReason, SignatureGateView } from '../../api/signatures-api';
import { ErrorNotice, passes } from '../../shared/error-notice';
import { asApiError, failureOf, valueOf } from '../../shared/paged-resource';
import { TimeAgo } from '../../shared/ui/time-ago';
import { GATE_ACT, GateActInputs, GateActState } from '../acts';
import { gateReasonSentence } from './gate-words';
import { anchorScopeWords } from './signature-words';

/** An artifact of the entry in the gate's repository: a stored file the gate judges, by its digest, with its own refusal when it has one. */
export interface GateArtifact {
  readonly name: string;
  readonly sha256: string;
  /** The file's own member `refusedReason` (R-SIG-073): absent while it passes. */
  readonly refusedReason?: SignatureGateRefusedReason | null;
}

/**
 * What a repository's gate decides of a version, an image or a file (R-SIG-074, R-SIG-073 `gate`), above its signatures: that the
 * repository serves only artifacts with a verified signature, the anchors it accepts (a removed one in red), and the decision in the
 * words of R-SIG-068. A refused artifact says why, and the page's place `GATE_ACT` (views/acts.ts) offers **Release for this repository**
 * to the holder of R-SIG-070's rights; a released one says so, by whom, when and why, with **Withdraw release** at the same place; one
 * that waits for its check says nothing more than **Not checked yet** (R-SIG-074). An image index that passes on its own signature says
 * of each platform that it is signed through its index (R-SIG-063); the API's answer judges the tag, so a refused index names no
 * platform's decision (Q-SIG-226). A view (R-UI-223): it reads through `SignatureReads`, and the acts are the page's.
 */
@Component({
  selector: 'quader-signature-gate',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [ErrorNotice, NgComponentOutlet, TimeAgo],
  template: `
    @let g = gate();
    <section class="sig-gate" [class.refused]="g.decision === 'refused'" data-testid="signature-gate" [attr.aria-label]="label">
      <p class="gate-head">
        <span class="material-icons inline gate-icon" aria-hidden="true">policy</span>
        <span i18n="@@sig.gate.head">This repository serves only artifacts with a verified signature.</span>
      </p>
      <p class="gate-anchors">
        <span class="muted" i18n="@@sig.gate.accepted">Accepted trust anchors:</span>&ngsp;
        @if (g.anchors.length === 0) {
          <span i18n="@@sig.gate.any">Any trust anchor of this repository</span>
        } @else {
          @for (a of g.anchors; track a.ref; let last = $last) {
            @if (a.removed) {
              <span class="removed" data-testid="gate-anchor-removed"
                ><span class="material-icons inline" aria-hidden="true">error</span><span i18n="@@sig.gate.removed">Removed trust anchor</span
                >&ngsp;<code translate="no">{{ a.ref }}</code></span
              >
            } @else {
              <span translate="no">{{ a.title }}</span>&ngsp;<span class="muted">({{ scopeWords(a.scope) }})</span>
            }
            @if (!last) {
              <span class="muted">, </span>
            }
          }
        }
      </p>
      @if (actionError(); as e) {
        <quader-error-notice [error]="e" />
      }
      @if (releasesFailure(); as f) {
        <quader-error-notice [error]="f" (retry)="releasesFailure.retry()" />
      }
      @switch (g.decision) {
        @case ('refused') {
          <div class="gate-decision" data-testid="gate-refused">
            @for (a of refusedArtifacts(); track a.artifact.sha256) {
              <p class="gate-line">
                <span class="fstatus bad"
                  ><span class="fstatus-main"
                    ><span class="material-icons" aria-hidden="true">block</span><span i18n="@@sig.gate.refused-here">Refused here</span></span
                  ></span
                >
                <span
                  >@if (refusedArtifacts().length > 1) {<code translate="no">{{ a.artifact.name }}</code>:&ngsp;}{{ sentence(a.artifact.refusedReason ?? g.reason ?? 'missing') }}</span
                >
              </p>
              @if (gateAct; as slot) {
                @if (slot.offered()) {
                  <ng-container *ngComponentOutlet="slot.component; inputs: a.release" />
                }
              }
            }
          </div>
        }
        @case ('pending') {
          <p class="gate-decision muted" data-testid="gate-pending" i18n="@@sig.state.not-checked">Not checked yet</p>
        }
        @default {
          @if (g.passedBy?.kind === 'release') {
            <div class="gate-decision" data-testid="gate-released">
              <p class="gate-line">
                <span class="fstatus"
                  ><span class="fstatus-main"
                    ><span class="material-icons" aria-hidden="true">lock_open</span><span i18n="@@sig.gate.released">Released for this repository</span></span
                  ></span
                >
                @if (releaseOf(); as rel) {
                  &ngsp;<span class="muted"
                    ><ng-container i18n="@@sig.gate.released-by">by {{ principal(rel.releasedBy) }}</ng-container>&ngsp;<quader-time-ago
                      [at]="rel.releasedAt"
                      absolute="title"
                  /></span>
                }
              </p>
              @if (releaseOf(); as rel) {
                <p class="release-reason"><span class="muted" i18n="@@sig.gate.release-reason">Reason:</span>&ngsp;<span>{{ rel.reason }}</span></p>
                @if (gateAct; as slot) {
                  @if (slot.offered() && withdrawal(); as act) {
                    <ng-container *ngComponentOutlet="slot.component; inputs: act" />
                  }
                }
              }
            </div>
          } @else {
            <p class="gate-decision gate-line" data-testid="gate-pass">
              <span class="fstatus ok"
                ><span class="fstatus-main"
                  ><span class="material-icons" aria-hidden="true">check_circle</span><span i18n="@@sig.gate.passes">Passes</span></span
                ></span
              >&ngsp;<span i18n="@@sig.gate.passes-why">A signature this repository accepts verifies it.</span>
            </p>
            @if (throughIndex() && platforms().length > 0) {
              <ul class="gate-platforms" data-testid="gate-platforms">
                @for (p of platforms(); track p) {
                  <li><span translate="no">{{ p }}</span>&ngsp;<span class="muted">·</span>&ngsp;<span i18n="@@sig.gate.through-index">Signed through its image index</span></li>
                }
              </ul>
            }
          }
        }
      }
    </section>
  `,
  styles: `
    :host {
      display: block;
    }
    .sig-gate {
      max-inline-size: var(--quader-settings-measure);
      margin-block: 0 var(--quader-space-4);
      padding: var(--quader-space-3) var(--quader-space-4);
      border: 1px solid var(--quader-border);
      border-radius: var(--quader-radius-m);
    }
    .sig-gate p {
      margin: 0 0 var(--quader-space-2);
    }
    .sig-gate > :last-child,
    .gate-decision > :last-child {
      margin-block-end: 0;
    }
    .gate-head {
      font-weight: 600;
    }
    .gate-icon {
      color: var(--quader-text-muted);
    }
    .gate-anchors,
    .release-reason {
      overflow-wrap: anywhere;
    }
    .release-reason {
      white-space: pre-line;
    }
    .removed {
      color: var(--quader-status-bad);
    }
    .muted {
      color: var(--quader-text-muted);
    }
    .gate-platforms {
      margin: var(--quader-space-1) 0 0;
      padding-inline-start: var(--quader-space-5);
    }
    /* A decision's status and its sentence or who released it: one row centred on each other, the sentence under it where it does not fit. */
    .gate-line {
      display: flex;
      flex-wrap: wrap;
      align-items: center;
      column-gap: var(--quader-space-2);
      row-gap: var(--quader-space-1);
    }
  `,
})
export class SignatureGate {
  private readonly api = inject(SignatureReads);
  /** The release and the withdrawal the page provides (R-SIG-070, views/acts.ts); none in an application without its acts. */
  protected readonly gateAct = inject(GATE_ACT, { optional: true });

  readonly gate = input.required<SignatureGateView>();
  /** `<project>/<key>` of the gate's repository. */
  readonly repository = input.required<string>();
  /** The artifacts of the entry in this repository: the file, or the version's files. */
  readonly artifacts = input.required<readonly GateArtifact[]>();
  /** The entry's signatures: whether the one that passed is the index's own. */
  readonly signatures = input<readonly SignatureEntry[]>([]);
  /** An image index's platforms, each signed through the index when the index passes on its own signature. */
  readonly platforms = input<readonly string[]>([]);
  /** A release was granted or withdrawn: the page reads the answers again. */
  readonly changed = output<void>();

  /** Whether an act of the page runs here, and its refusal, shown above the decision (R-UI-216). */
  private readonly busy = signal(false);
  protected readonly actionError = signal<ApiError | null>(null);
  private readonly actState: GateActState = { busy: this.busy, error: this.actionError };
  private readonly emitChanged = (): void => this.changed.emit();
  protected readonly principal = personName;
  protected readonly scopeWords = anchorScopeWords;
  protected readonly sentence = gateReasonSentence;
  protected readonly label = $localize`:@@sig.gate.label:Signature check of this repository`;

  /** The artifacts the gate refuses: those whose own member says so, else all of them (a file's answer has only its gate). */
  protected readonly refusedArtifacts = computed(() => {
    const all = this.artifacts();
    const own = all.filter((a) => a.refusedReason && a.refusedReason !== 'pending');
    const gate = this.gate();
    return (own.length > 0 ? own : all).map((artifact) => {
      const release: GateActInputs = {
        act: 'release',
        place: 'gate',
        repository: this.repository(),
        artifact: { name: artifact.name, sha256: artifact.sha256, reason: artifact.refusedReason ?? gate.reason ?? 'missing' },
        state: this.actState,
        changed: this.emitChanged,
      };
      return { artifact, release };
    });
  });

  /** The passing signature is the index's own (it names no platform): every platform passes through it. */
  protected readonly throughIndex = computed(() => {
    const by = this.gate().passedBy;
    if (by?.kind !== 'signature' || by.signature === null) return false;
    const entry = this.signatures().find((e) => e.id === by.signature);
    return entry !== undefined && !entry.platform;
  });

  /** The repository's releases, read while the entry passes by one: who granted it, when and why. */
  private readonly releases = resource({
    params: () => (this.gate().passedBy?.kind === 'release' ? { repository: this.repository() } : undefined),
    loader: async ({ params }) => {
      try {
        return await this.api.releases(params.repository);
      } catch (e) {
        // A refusal leaves the release without its grantor; a failure that may pass is shown and read again by its notice
        // (R-UI-216): taken for none, the reason and **Withdraw release** stayed away until the page was opened again.
        if (passes(asApiError(e))) throw e;
        return [];
      }
    },
  });
  protected readonly releasesFailure = failureOf(this.releases);
  protected readonly releaseOf = computed(() => {
    const digests = new Set(this.artifacts().map((a) => a.sha256));
    return (valueOf(this.releases) ?? []).find((r) => digests.has(r.sha256)) ?? null;
  });

  /** The withdrawal of the release the entry passes by, at the page's place. */
  protected readonly withdrawal = computed<GateActInputs | null>(() => {
    const rel = this.releaseOf();
    if (!rel) return null;
    const name = this.artifacts().find((a) => a.sha256 === rel.sha256)?.name ?? rel.sha256;
    return {
      act: 'withdraw',
      place: 'gate',
      repository: this.repository(),
      artifact: { name, sha256: rel.sha256, reason: this.gate().reason ?? 'missing' },
      state: this.actState,
      changed: this.emitChanged,
    };
  });
}
