import { Component, computed, inject, input, resource, signal } from '@angular/core';
import { firstValueFrom } from 'rxjs';
import { ApiError } from '../../api/problem';
import { SignatureAnchor, SignaturesApi } from '../../api/signatures-api';
import { Notifier } from '../../core/notifier';
import { SessionStore } from '../../core/session-store';
import { GateActions } from '../../shared/signatures/gate-releases';
import { ErrorNotice } from '../../shared/error-notice';
import { asApiError, valueOf } from '../../shared/paged-resource';
import { AnchorDialog, AnchorDialogData } from '../../shared/signatures/anchor-dialog';
import { AnchorRowControls, AnchorTable } from '../../shared/signatures/anchor-table';
import { QuaderDialog } from '../../shared/ui/dialog';
import { Disclosure } from '../../shared/ui/disclosure';

/**
 * The closed section **Trust anchors** of a project's settings (R-SIG-037, R-SIG-019): while the administrator's switch is on, the
 * project's own anchors, **Add trust anchor** and **Remove** for its Owner, the list for every member; below them the installation's
 * anchors read-only ("Also trusted in this project"), so an Owner sees the whole scope. While the switch is off the section is not
 * shown, except to the Owner of a project that holds anchors, who reads them with "Not applied: project anchors are switched off by an
 * administrator." A holder of `security-settings:write` removes a project's anchor whatever the switch says.
 */
@Component({
  selector: 'quader-project-signature-anchors',
  imports: [AnchorTable, Disclosure, ErrorNotice],
  template: `
    @if (shown()) {
      @let l = list()!;
      <quader-disclosure i18n-heading="@@sig.project.heading" heading="Trust anchors" [hint]="hint()" [count]="items().length" data-testid="project-anchors">
        @if (error(); as e) {
          <quader-error-notice [error]="e" />
        }
        @if (!l.projectAnchors) {
          <p class="banner-warning" role="note" data-testid="project-anchors-off">
            <span class="material-icons inline warn" aria-hidden="true">info</span>
            <span i18n="@@sig.anchor.not-applied">Not applied: project anchors are switched off by an administrator.</span>
          </p>
        } @else if (canAdd()) {
          <div class="row-actions">
            <button class="btn btn-default" type="button" (click)="add()" i18n="@@sig.form.heading">Add trust anchor</button>
          </div>
        }
        <quader-anchor-table
          [anchors]="items()"
          [caption]="ownCaption"
          [emptyText]="ownEmpty"
          [controls]="controls"
          [busy]="busy()"
          (remove)="remove($event)"
        />
        <h3 class="also" i18n="@@sig.project.also">Also trusted in this project</h3>
        <p class="muted" i18n="@@sig.project.also-note">The anchors of the installation apply in every project; a project cannot switch them off.</p>
        <quader-anchor-table [anchors]="l.installation" [caption]="installationCaption" [emptyText]="installationEmpty" [showState]="false" />
      </quader-disclosure>
    }
  `,
  styles: `
    .row-actions {
      margin-block: 0 var(--quader-space-3);
    }
    .also {
      margin: var(--quader-space-5) 0 var(--quader-space-1);
      font-size: var(--quader-text-size);
      font-weight: 600;
    }
    .muted {
      margin-block: 0 var(--quader-space-2);
      color: var(--quader-text-muted);
    }
  `,
})
export class ProjectSignatureAnchors {
  private readonly api = inject(SignaturesApi);
  private readonly session = inject(SessionStore);
  private readonly dialog = inject(QuaderDialog);
  private readonly gate = inject(GateActions);
  private readonly notifier = inject(Notifier);

  /** The project's key. */
  readonly project = input.required<string>();

  /** A refusal (a member of another project, the signatures off) leaves the section out without an error. */
  protected readonly answer = resource({
    params: () => ({ project: this.project() }),
    loader: async ({ params }) => {
      try {
        return await this.api.projectAnchors(params.project);
      } catch {
        return null;
      }
    },
  });
  protected readonly list = computed(() => valueOf(this.answer) ?? null);
  /** The project's own anchors, kept after a change without a reload. */
  private readonly changed = signal<readonly SignatureAnchor[] | null>(null);
  protected readonly items = computed(() => this.changed() ?? this.list()?.items ?? []);
  protected readonly busy = signal<ReadonlySet<number>>(new Set());
  protected readonly error = signal<ApiError | null>(null);

  private readonly isOwner = computed(() => this.session.isOwnerOf(this.project()));
  private readonly isSecurityAdmin = computed(() => this.session.has('security-settings:write'));
  /** Shown while the switch is on; while it is off only to whoever may act on the anchors a project still holds. */
  protected readonly shown = computed(() => {
    const l = this.list();
    if (!l) return false;
    if (l.projectAnchors) return true;
    return this.items().length > 0 && (this.isOwner() || this.isSecurityAdmin());
  });
  protected readonly canAdd = computed(() => (this.isOwner() || this.isSecurityAdmin()) && this.list()?.projectAnchors === true);
  protected readonly controls = (): AnchorRowControls => ({
    toggle: false,
    remove: this.isSecurityAdmin() || (this.isOwner() && this.list()?.projectAnchors === true),
  });
  protected readonly hint = computed(() =>
    this.list()?.projectAnchors
      ? $localize`:@@sig.project.hint:Certificates and keys whose signatures read as verified in this project's repositories.`
      : $localize`:@@sig.project.hint-off:This project's anchors do not apply while project anchors are switched off.`,
  );
  protected readonly ownCaption = $localize`:@@sig.project.caption:Trust anchors of this project`;
  protected readonly ownEmpty = $localize`:@@sig.project.empty:This project has no trust anchor of its own.`;
  protected readonly installationCaption = $localize`:@@sig.project.installation-caption:Trust anchors of the installation`;
  protected readonly installationEmpty = $localize`:@@sig.project.installation-empty:The installation has no trust anchor that applies.`;

  protected async add(): Promise<void> {
    const ref = this.dialog.open<AnchorDialog, AnchorDialogData, SignatureAnchor>(AnchorDialog, { data: { project: this.project() }, maxWidth: '44rem' });
    const created = await firstValueFrom(ref.closed);
    if (created) this.changed.set([...this.items(), created]);
  }

  protected async remove(anchor: SignatureAnchor): Promise<void> {
    const title = anchor.title;
    // The removal's question, with what it makes the project's gated repositories refuse (R-SIG-071, R-SIG-074).
    const go = await this.gate.askImpact(
      { anchor: anchor.id, change: 'remove' },
      {
        title: $localize`:@@sig.anchor.remove-title:Remove the trust anchor ${title}:title:?`,
        lines: [$localize`:@@sig.anchor.remove-text:The signatures it knows read "Signed · signer not trusted" unless another anchor knows their signer.`],
        confirmLabel: $localize`:@@sig.anchor.remove-confirm:Remove trust anchor`,
      },
      { title: '', confirmLabel: '' },
    );
    if (!go) return;
    this.busy.set(new Set([anchor.id]));
    this.error.set(null);
    try {
      if (this.isOwner() && !this.isSecurityAdmin()) await this.api.deleteProjectAnchor(this.project(), anchor.id);
      else await this.api.deleteAnchor(anchor.id);
      this.changed.set(this.items().filter((a) => a.id !== anchor.id));
      this.notifier.status($localize`:@@sig.anchor.removed:Trust anchor removed.`);
    } catch (e) {
      this.error.set(asApiError(e));
    } finally {
      this.busy.set(new Set());
    }
  }
}
