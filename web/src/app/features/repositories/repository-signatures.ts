import { Component, computed, effect, inject, input, output, resource, signal, untracked } from '@angular/core';
import { RouterLink } from '@angular/router';
import { ApiError } from '../../api/problem';
import { RepositorySignatureSettings, SignatureAnchor, SignatureGatePreview, SignaturesApi } from '../../api/signatures-api';
import { SessionStore } from '../../core/session-store';
import { Confirm } from '../../shared/confirm-dialog';
import { ErrorNotice } from '../../shared/error-notice';
import { asApiError, valueOf } from '../../shared/paged-resource';
import { gateReasonWord, previewQuestion, previewReasonLines, previewRefused, previewStateLine } from '../../shared/signatures/gate-words';
import { anchorKindWords, anchorScopeWords, shortFingerprint } from '../../shared/signatures/signature-words';
import { Checkbox } from '../../shared/ui/checkbox';
import { Disclosure } from '../../shared/ui/disclosure';
import { GateOrder, gateOrder } from './signature-gate-order';

/** One trust anchor the repository can accept, by its reference `<scope>/<key>` (R-SIG-061). */
export interface GateAnchorOption {
  readonly ref: string;
  readonly title: string;
  /** What identifies it: kind, where it comes from, the first fingerprint or subject. */
  readonly facts: string;
  /** It matches nothing now: switched off, or a project anchor while project anchors are switched off. */
  readonly off: boolean;
  /** A stored reference that names no anchor any more (R-SIG-061): shown as **Removed trust anchor**. */
  readonly removed: boolean;
}

/** The members of `signatures` a save changes; an absent member stays as stored. */
export type SignatureSettingsChange = Partial<RepositorySignatureSettings>;

/** The anchors of the repository's scope, as options; the stored references that name none are removed ones (R-SIG-061). */
export function anchorOptions(
  installation: readonly SignatureAnchor[],
  project: readonly SignatureAnchor[],
  projectAnchorsOn: boolean,
  stored: readonly string[],
): readonly GateAnchorOption[] {
  const option = (a: SignatureAnchor): GateAnchorOption => {
    const first = a.fingerprints[0];
    const id = first ? (first.subject ?? shortFingerprint(first.value)) : '';
    return {
      ref: `${a.scope}/${a.key}`,
      title: a.title,
      facts: [anchorKindWords(a.kind), anchorScopeWords(a.scope), id].filter((v) => v !== '').join(' · '),
      off: a.scope === 'project' ? !projectAnchorsOn : !a.enabled,
      removed: false,
    };
  };
  const rank = { shipped: 0, configuration: 1, installation: 2, project: 3 } as const;
  const known = [...installation.filter((a) => a.scope !== 'project'), ...project]
    .sort((a, b) => rank[a.scope] - rank[b.scope] || a.title.localeCompare(b.title))
    .map(option);
  const refs = new Set(known.map((o) => o.ref));
  const removed = stored
    .filter((r) => !refs.has(r))
    .map((ref): GateAnchorOption => ({ ref, title: ref, facts: '', off: true, removed: true }));
  return [...known, ...removed];
}

/**
 * The closed section **Signatures** of a repository's settings page (R-SIG-074, R-SIG-061, R-SIG-040), never part of the creation
 * form. First **Only artifacts with a verified signature can be downloaded from this repository**, unchecked by default; checked, the
 * **Accepted trust anchors**: any anchor of the repository (with the sentence of Q-SIG-76 that this includes the public Sigstore) or
 * these anchors, ticked from the scope's anchors with their kind, source and fingerprint, a removed one in red. Under them the stored
 * setting's state from the preview of R-SIG-072 when the section opens, with a link to the refused downloads for the readers of the
 * security dashboard. A narrowing whose preview refuses anything asks first, with the numbers per reason and up to 20 examples. A writer
 * who may not weaken the gate (an Owner) sees what would weaken it read-only, and a narrowing stays open (the order of R-SIG-061). A
 * Debian repository says why it has no such setting; while the installation's checks are switched off the section says so. A remote
 * repository adds **Fetch the upstream's signatures** (R-SIG-026). One save writes the members that changed, with the page's `ETag`.
 */
@Component({
  selector: 'quader-repository-signatures',
  imports: [Checkbox, Disclosure, ErrorNotice, RouterLink],
  template: `
    <quader-disclosure
      i18n-heading="@@repo.signatures.heading"
      heading="Signatures"
      [hint]="hint()"
      [note]="dirty() ? unsavedNote : ''"
      (expandedChange)="opened.set(opened() || $event)"
    >
      <form (submit)="submit($event)" novalidate data-testid="repo-signatures">
        @if (failure(); as e) {
          <quader-error-notice [error]="e" />
        }

        @if (deb()) {
          <!-- apt checks the index, not a package (Q-SIG-80): nothing is offered. -->
          <p class="hint" data-testid="gate-deb" i18n="@@repo.gate.deb">
            apt checks the signature of this repository's index, not of a package. This setting is not available for Debian repositories.
          </p>
        } @else {
          @if (checksOff()) {
            <p class="banner-warning" role="note" data-testid="gate-checks-off">
              <span class="material-icons inline warn" aria-hidden="true">info</span>
              <span i18n="@@repo.gate.checks-off">Signature checks are switched off in this installation. This setting does not apply.</span>
            </p>
          }
          <quader-checkbox [checked]="onlyVerified()" [disabled]="readOnly() || (storedOn() && !canWeaken())" [describedBy]="'repo-gate-note'" (checkedChange)="setOn($event)">
            <ng-container i18n="@@repo.gate.only-verified">Only artifacts with a verified signature can be downloaded from this repository</ng-container>
          </quader-checkbox>
          <div id="repo-gate-note" class="notes">
            <p class="hint" i18n="@@repo.gate.what">
              Artifacts without a signature that a trust anchor of this repository knows are refused. A signature says who signed an
              artifact, not that it is safe.
            </p>
            @if (virtual()) {
              <p class="hint" data-testid="gate-virtual" i18n="@@repo.gate.virtual">Applies to everything this repository serves from its members.</p>
            }
            @if (lockedOn()) {
              <p class="hint" data-testid="gate-locked" i18n="@@repo.gate.locked">Only an administrator or someone who triages security findings can loosen this.</p>
            }
          </div>

          @if (onlyVerified()) {
            <fieldset class="anchors" data-testid="gate-anchors">
              <legend i18n="@@repo.gate.accepted">Accepted trust anchors</legend>
              <label class="check-row">
                <input type="radio" name="gate-anchors" [checked]="!chosen()" [disabled]="readOnly() || (lockedOn() && storedAnchors().length > 0)" (change)="choose(false)" />
                <span class="choice-text">
                  <span i18n="@@repo.gate.any">Any trust anchor of this repository</span>
                  @if (!chosen()) {
                    <span class="warn-line" data-testid="gate-any-warning">
                      <span class="material-icons inline warn" aria-hidden="true">warning</span
                      ><span i18n="@@repo.gate.any-warning">This includes the public Sigstore, where anyone can sign. Choose anchors to accept only your own signers.</span>
                    </span>
                  }
                </span>
              </label>
              <label class="check-row">
                <input type="radio" name="gate-anchors" [checked]="chosen()" [disabled]="readOnly()" (change)="choose(true)" />
                <span i18n="@@repo.gate.these">These trust anchors</span>
              </label>
              @if (chosen()) {
                @if (options().length === 0) {
                  <p class="hint anchor-list" i18n="@@repo.gate.no-anchors">No trust anchor applies to this repository yet.</p>
                } @else {
                  <ul class="anchor-list" data-testid="gate-anchor-list">
                    @for (o of options(); track o.ref) {
                      <li>
                        <label class="check-row">
                          <input
                            type="checkbox"
                            [checked]="refs().includes(o.ref)"
                            [disabled]="readOnly() || !mayToggle(o.ref)"
                            (change)="tick(o.ref, $any($event.target).checked)"
                          />
                          <span class="choice-text">
                            @if (o.removed) {
                              <span class="removed" data-testid="gate-option-removed"
                                ><span i18n="@@sig.gate.removed">Removed trust anchor</span>&ngsp;<code translate="no">{{ o.ref }}</code></span
                              >
                            } @else {
                              <span translate="no">{{ o.title }}</span>
                              <span class="secondary detail">{{ o.facts }}@if (o.off) {&ngsp;· <ng-container i18n="@@repo.gate.off">switched off, matches nothing</ng-container>}</span>
                            }
                          </span>
                        </label>
                      </li>
                    }
                  </ul>
                }
                @if (chosen() && refs().length === 0) {
                  <p class="field-error" role="alert" data-testid="gate-choose" i18n="@@repo.gate.choose">Choose at least one trust anchor.</p>
                }
              }
            </fieldset>
          }

          @if (storedPreview(); as p) {
            <p class="state-line" data-testid="gate-state">
              @if (storedOn()) {
                {{ stateLine(p) }}
              } @else {
                <span class="muted" i18n="@@repo.gate.if-on">Switched on with the stored anchors:</span>&ngsp;{{ stateLine(p) }}
              }
              @if (refusalsLink()) {
                &ngsp;<a [routerLink]="['/security/signatures']" [queryParams]="{ repository: repository() }" i18n="@@repo.gate.refusals-link">Refused downloads</a>
              }
            </p>
          } @else if (previewLoading()) {
            <p class="state-line muted" role="status" i18n="@@repo.gate.counting">Counting what the setting lets pass.</p>
          }
        }

        @if (remote()) {
          <div class="fetch">
            <quader-checkbox [checked]="fetch()" [disabled]="readOnly()" [describedBy]="'repo-signatures-note'" (checkedChange)="fetch.set($event)">
              <ng-container i18n="@@repo.signatures.fetch">Fetch the upstream's signatures</ng-container>
            </quader-checkbox>
            <div id="repo-signatures-note" class="notes">
              <p class="hint" i18n="@@repo.signatures.what">
                Quader asks the upstream in the background for the signatures of what this repository stores, at most 600 requests an hour,
                so that the badge is right before any client asks. A client's own request for signatures passes through either way.
              </p>
              <p class="hint" i18n="@@repo.signatures.off">
                Switch it off for an upstream with tight rate limits: on Docker Hub without an account, fetching a signature counts as a pull.
              </p>
              @if (onlyVerified() && !fetch() && format() === 'oci') {
                <p class="hint warn-line" data-testid="gate-fetch-off">
                  <span class="material-icons inline warn" aria-hidden="true">warning</span
                  ><span i18n="@@repo.gate.fetch-off">Signatures of images are not fetched: an image whose signature no client pulled is refused.</span>
                </p>
              }
            </div>
          </div>
        }

        @if (canChange() && (!deb() || remote())) {
          <div class="actions">
            <button class="btn btn-confirm" type="submit" [disabled]="!dirty() || saving() || asking() || (chosen() && onlyVerified() && refs().length === 0)">
              <ng-container i18n="@@repo.settings.save">Save changes</ng-container>
              <span class="visually-hidden" i18n="@@repo.signatures.save-for"> (signatures)</span>
            </button>
            @if (dirty()) {
              <button class="btn btn-tertiary" type="button" (click)="reset()" i18n="@@repo.edit.reset">Discard changes</button>
            }
          </div>
        }
      </form>
    </quader-disclosure>
  `,
  styles: `
    .notes p {
      margin-block: var(--quader-space-1) 0;
    }
    .anchors {
      margin: var(--quader-space-4) 0 0;
      padding: 0;
      border: 0;
      min-inline-size: 0;
    }
    .anchors legend {
      margin-block-end: var(--quader-space-2);
      padding: 0;
      font-weight: 600;
    }
    .anchors > .check-row + .check-row {
      margin-block-start: var(--quader-space-2);
    }
    .choice-text {
      display: flex;
      flex-direction: column;
      min-inline-size: 0;
    }
    .warn-line {
      display: block;
      margin-block-start: var(--quader-space-1);
      color: var(--quader-text);
    }
    .anchor-list {
      margin: var(--quader-space-2) 0 0;
      padding-inline-start: var(--quader-space-6);
      list-style: none;
    }
    .anchor-list li + li {
      margin-block-start: var(--quader-space-2);
    }
    .detail,
    .removed {
      overflow-wrap: anywhere;
    }
    .removed {
      color: var(--quader-status-bad);
    }
    .secondary {
      color: var(--quader-text-muted);
      font-size: var(--quader-text-muted-size);
    }
    .muted {
      color: var(--quader-text-muted);
    }
    .state-line {
      margin-block: var(--quader-space-4) 0;
    }
    .field-error {
      margin-block: var(--quader-space-2) 0;
      color: var(--quader-status-bad);
    }
    .fetch {
      margin-block-start: var(--quader-space-5);
    }
    .actions {
      display: flex;
      flex-wrap: wrap;
      gap: var(--quader-space-2);
      margin-block-start: var(--quader-space-4);
    }
  `,
})
export class RepositorySignaturesSection {
  private readonly api = inject(SignaturesApi);
  private readonly confirm = inject(Confirm);
  private readonly session = inject(SessionStore);

  /** `<project>/<key>`. */
  readonly repository = input.required<string>();
  readonly project = input.required<string>();
  readonly format = input.required<string>();
  readonly repoClass = input.required<'local' | 'remote' | 'virtual'>();
  /** The member `signatures` as stored; absent members are their defaults. */
  readonly stored = input<RepositorySignatureSettings | null | undefined>(null);
  /** The caller may write the repository's settings, and the repository is not being deleted. */
  readonly canChange = input(false);
  /** The caller may weaken the gate: `repositories:write-config` or `vulnerabilities:triage` (R-SIG-061). */
  readonly canWeaken = input(false);
  readonly saving = input(false);
  readonly failure = input<ApiError | null>(null);
  readonly save = output<SignatureSettingsChange>();

  protected readonly opened = signal(false);
  protected readonly asking = signal(false);
  protected readonly onlyVerified = signal(false);
  protected readonly chosen = signal(false);
  protected readonly refs = signal<readonly string[]>([]);
  protected readonly fetch = signal(true);

  protected readonly deb = computed(() => this.format() === 'deb');
  protected readonly remote = computed(() => this.repoClass() === 'remote');
  protected readonly virtual = computed(() => this.repoClass() === 'virtual');
  protected readonly storedOn = computed(() => this.stored()?.onlyVerified ?? false);
  protected readonly storedAnchors = computed<readonly string[]>(() => this.stored()?.anchors ?? []);
  private readonly storedFetch = computed(() => this.stored()?.fetch ?? true);
  protected readonly readOnly = computed(() => !this.canChange() || this.saving() || this.deb());
  /** On, and the caller may not loosen it (R-SIG-061): a narrowing stays open. */
  protected readonly lockedOn = computed(() => this.storedOn() && this.canChange() && !this.canWeaken());
  protected readonly unsavedNote = $localize`:@@repo.settings.unsaved:Unsaved changes`;
  protected readonly stateLine = previewStateLine;
  protected readonly hint = computed(() =>
    this.remote()
      ? $localize`:@@repo.signatures.hint-gate-remote:Whether only artifacts with a verified signature can be downloaded, and whether Quader fetches the upstream's signatures.`
      : $localize`:@@repo.signatures.hint-gate:Whether only artifacts with a verified signature can be downloaded from this repository.`,
  );

  /** The draft as the members it would store. */
  private readonly draftAnchors = computed<readonly string[]>(() => (this.chosen() ? this.refs() : []));
  protected readonly order = computed<GateOrder>(() =>
    gateOrder({ on: this.storedOn(), anchors: this.storedAnchors() }, { on: this.onlyVerified(), anchors: this.draftAnchors() }),
  );
  protected readonly dirty = computed(() => this.order() !== 'none' || (this.remote() && this.fetch() !== this.storedFetch()));

  // ---- what the section reads when it is first opened ----

  /** Read again after a save: the state line follows what is stored. */
  private readonly previewStamp = signal(0);

  /** The stored setting judged by the preview (R-SIG-072); `409 signatures_not_enabled` says that the checks are off. */
  private readonly preview = resource({
    params: () => (this.opened() && !this.deb() ? { repository: this.repository(), stamp: this.previewStamp() } : undefined),
    loader: async ({ params }): Promise<{ readonly preview: SignatureGatePreview | null; readonly off: boolean }> => {
      try {
        return { preview: await this.api.preview(params.repository), off: false };
      } catch (e) {
        return { preview: null, off: asApiError(e).code === 'signatures_not_enabled' };
      }
    },
  });
  protected readonly storedPreview = computed(() => valueOf(this.preview)?.preview ?? null);
  protected readonly previewLoading = computed(() => this.preview.isLoading());
  protected readonly checksOff = computed(() => valueOf(this.preview)?.off === true);
  /** The refused downloads are the security dashboard's (R-SIG-073): a link for whoever opens it. */
  protected readonly refusalsLink = computed(() => this.storedOn() && this.session.has('content:metadata-read'));

  /** The anchors of the scope: the project's list for every member, the installation's own list (switched-off ones too) for `settings:read`. */
  private readonly anchors = resource({
    params: () => (this.opened() && !this.deb() ? { project: this.project(), full: this.session.has('settings:read') } : undefined),
    loader: async ({ params }) => {
      try {
        const list = await this.api.projectAnchors(params.project);
        const installation = params.full ? await this.api.anchors().catch(() => list.installation) : list.installation;
        return { installation, project: list.items, projectAnchorsOn: list.projectAnchors };
      } catch {
        return { installation: [], project: [], projectAnchorsOn: false };
      }
    },
  });
  protected readonly options = computed(() => {
    const a = valueOf(this.anchors);
    return anchorOptions(a?.installation ?? [], a?.project ?? [], a?.projectAnchorsOn ?? false, this.storedAnchors());
  });

  constructor() {
    // The draft follows what is stored, after the load and after a save.
    effect(() => {
      const stored = this.stored();
      untracked(() => this.adopt(stored));
    });
    effect(() => {
      if (!this.saving()) untracked(() => this.previewStamp.update((n) => n + 1));
    });
  }

  private adopt(stored: RepositorySignatureSettings | null | undefined): void {
    this.onlyVerified.set(stored?.onlyVerified ?? false);
    this.chosen.set((stored?.anchors ?? []).length > 0);
    this.refs.set(stored?.anchors ?? []);
    this.fetch.set(stored?.fetch ?? true);
  }

  protected reset(): void {
    this.adopt(this.stored());
  }

  protected setOn(on: boolean): void {
    this.onlyVerified.set(on);
  }

  protected choose(chosen: boolean): void {
    this.chosen.set(chosen);
  }

  /**
   * Whether the caller may tick or untick a reference (R-SIG-061): every change is open to whoever may weaken, and to anyone while the
   * stored gate is off or accepts any anchor (a list narrows that); with a stored list an Owner may only take references away, keeping one.
   */
  protected mayToggle(ref: string): boolean {
    if (this.canWeaken() || !this.storedOn() || this.storedAnchors().length === 0) return true;
    const ticked = this.refs().includes(ref);
    if (!ticked) return false;
    return this.refs().length > 1;
  }

  protected tick(ref: string, on: boolean): void {
    this.refs.update((list) => (on ? [...list.filter((r) => r !== ref), ref] : list.filter((r) => r !== ref)));
  }

  protected async submit(event: Event): Promise<void> {
    event.preventDefault();
    if (!this.dirty() || this.saving() || this.asking()) return;
    const change: SignatureSettingsChange = {};
    const order = this.order();
    if (order !== 'none') {
      if (!this.onlyVerified()) change.onlyVerified = false;
      else {
        change.onlyVerified = true;
        change.anchors = [...this.draftAnchors()];
      }
    }
    if (this.remote() && this.fetch() !== this.storedFetch()) change.fetch = this.fetch();
    if (order === 'narrowing' && !(await this.askNarrowing())) return;
    this.save.emit(change);
  }

  /**
   * A narrowing whose preview refuses anything asks first (R-SIG-074): the numbers, per reason, and up to 20 examples. A preview that
   * cannot be read lets the save go on; the gate decides at the download either way.
   */
  private async askNarrowing(): Promise<boolean> {
    this.asking.set(true);
    try {
      let p: SignatureGatePreview;
      try {
        p = await this.api.preview(this.repository(), this.draftAnchors());
      } catch {
        return true;
      }
      if (previewRefused(p) === 0) return true;
      const answer = await this.confirm.ask({
        title: $localize`:@@repo.gate.ask-title:Refuse downloads of artifacts without an accepted signature?`,
        lines: [previewQuestion(p), ...previewReasonLines(p), ...(p.examples.length > 0 ? [examplesLine(p.examples.length)] : [])],
        items: p.examples.map((x) => exampleLine(x.package, x.version, x.path, gateReasonWord(x.reason))),
        confirmLabel: $localize`:@@repo.gate.ask-confirm:Save and refuse them`,
      });
      return answer !== null;
    } finally {
      this.asking.set(false);
    }
  }
}

/** The line before the preview's examples. */
function examplesLine(count: number): string {
  return $localize`:@@repo.gate.examples:The newest ${count}:count: of them:`;
}

/** An example of the preview: "portal/api 1.4.0 · No publisher's signature", or the path where no package names it. */
export function exampleLine(pkg: string | null, version: string | null, path: string, reason: string): string {
  const name = pkg ? (version ? `${pkg} ${version}` : pkg) : path;
  return `${name} · ${reason}`;
}
