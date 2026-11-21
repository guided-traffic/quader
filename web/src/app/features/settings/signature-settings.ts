import { DatePipe, DecimalPipe } from '@angular/common';
import { Component, computed, inject, signal } from '@angular/core';
import { RouterLink } from '@angular/router';
import { firstValueFrom } from 'rxjs';
import { ApiError } from '../../api/problem';
import { LoadedSignatureSettings, SignatureAnchor, SignatureBlock, SignatureSettings, SignaturesApi } from '../../api/signatures-api';
import { Notifier } from '../../core/notifier';
import { SessionStore } from '../../core/session-store';
import { Needs } from '../../shared/access/needs';
import { Confirm } from '../../shared/confirm-dialog';
import { ConflictResolver } from '../../shared/conflict-dialog';
import { ErrorNotice } from '../../shared/error-notice';
import { principalName } from '../../shared/malware/malware-words';
import { asApiError } from '../../shared/paged-resource';
import { AnchorDialog, AnchorDialogData } from '../../shared/signatures/anchor-dialog';
import { AnchorRowControls, AnchorTable } from '../../shared/signatures/anchor-table';
import { BlockDialog } from '../../shared/signatures/block-dialog';
import { GateActions } from '../../shared/signatures/gate-releases';
import { blockKindWords, signaturePromise } from '../../shared/signatures/signature-words';
import { Checkbox } from '../../shared/ui/checkbox';
import { DataTable, TableColumn } from '../../shared/ui/data-table';
import { QuaderDialog } from '../../shared/ui/dialog';
import { Disclosure } from '../../shared/ui/disclosure';
import { PageHeader } from '../../shared/ui/page-header';
import { TimeAgo } from '../../shared/ui/time-ago';
import { SigningIdentitySection } from './signing-identity';

/**
 * **Admin area › Settings › Signatures** (R-SIG-036, Q-SIG-44): for the holder of `settings:read`, its controls for the holder of
 * `security-settings:write`. Under the title the sentence that a signature says who signed, not that the content is safe. Then, in one
 * column: **Trust anchors** (open), every anchor of the installation by kind and where it comes from, a shipped one with only its
 * switch, one of the Helm values with nothing, an administrator's with its switch and **Remove**, and **Add trust anchor**, with the
 * backlog of checks under it; **Project trust anchors** (open), the switch for project Owners with what it means and every project's
 * anchors with **Remove**; **Blocked signers** (closed), the blocks with their reasons, **Block signer** and **Lift block**; **Public
 * Sigstore root** (closed), where the root and npm's keys in use come from, when they were fetched and the last failure. Before the
 * four stands stage 2's section **Signing identity** (R-SIG-056, `SigningIdentitySection`).
 */
@Component({
  selector: 'quader-signature-settings',
  imports: [AnchorTable, Checkbox, DataTable, DatePipe, DecimalPipe, Disclosure, ErrorNotice, Needs, PageHeader, RouterLink, SigningIdentitySection, TimeAgo],
  templateUrl: './signature-settings.html',
  styleUrl: '../../shared/access/access.scss',
  styles: `
    /* The lists take the page's width, so that their columns stand side by side; the text keeps the measure of a settings page. */
    .settings p,
    .settings form,
    .settings dl.facts {
      max-inline-size: var(--quader-settings-measure);
    }
    .promise {
      margin-block: 0 var(--quader-space-4);
    }
    .open-section {
      padding-block: var(--quader-space-4) var(--quader-space-5);
      border-block-start: 1px solid var(--quader-border);
    }
    .open-section:first-of-type {
      border-block-start: 0;
      padding-block-start: 0;
    }
    .open-section h2 {
      margin: 0 0 var(--quader-space-2);
      font: var(--quader-type-title-small);
    }
    .open-section h3 {
      margin: var(--quader-space-4) 0 var(--quader-space-2);
      font-size: var(--quader-text-size);
      font-weight: 600;
    }
    .row-actions {
      display: flex;
      flex-wrap: wrap;
      gap: var(--quader-space-2);
      margin-block: var(--quader-space-3);
    }
    .block-value {
      overflow-wrap: anywhere;
    }
    .block-reason {
      white-space: pre-line;
      overflow-wrap: anywhere;
    }
  `,
})
export class SignatureSettingsPage {
  private readonly api = inject(SignaturesApi);
  private readonly notifier = inject(Notifier);
  private readonly confirm = inject(Confirm);
  private readonly conflicts = inject(ConflictResolver);
  private readonly dialog = inject(QuaderDialog);
  private readonly gate = inject(GateActions);
  protected readonly session = inject(SessionStore);

  protected readonly loaded = signal<LoadedSignatureSettings | null>(null);
  protected readonly anchors = signal<readonly SignatureAnchor[]>([]);
  protected readonly projectAnchorList = signal<readonly SignatureAnchor[]>([]);
  protected readonly blocks = signal<readonly SignatureBlock[]>([]);
  protected readonly loadError = signal<ApiError | null>(null);
  protected readonly actionError = signal<ApiError | null>(null);
  protected readonly switchError = signal<ApiError | null>(null);
  protected readonly busy = signal<ReadonlySet<number>>(new Set());
  protected readonly saving = signal(false);
  protected readonly projectAnchors = signal(false);

  protected readonly settings = computed<SignatureSettings | null>(() => this.loaded()?.settings ?? null);
  protected readonly canWrite = computed(() => this.session.has('security-settings:write'));
  /** The findings are the security dashboard's (R-SIG-038): the link for whoever may open it. */
  protected readonly canSeeFindings = computed(() => this.session.has('content:metadata-read'));
  protected readonly dirty = computed(() => {
    const s = this.settings();
    return s !== null && s.projectAnchors !== this.projectAnchors();
  });
  /** The anchors of the installation in the order the page lists them: shipped, from the Helm values, the administrator's. */
  protected readonly installationAnchors = computed(() => {
    const rank = { shipped: 0, configuration: 1, installation: 2, project: 3 } as const;
    return [...this.anchors()].sort((a, b) => rank[a.scope] - rank[b.scope] || a.title.localeCompare(b.title));
  });

  /** A shipped anchor only its switch, a Helm one nothing, an administrator's its switch and Remove (R-SIG-036). */
  protected readonly installationControls = (a: SignatureAnchor): AnchorRowControls => ({
    toggle: this.canWrite() && (a.scope === 'shipped' || a.scope === 'installation'),
    remove: this.canWrite() && a.scope === 'installation',
  });
  /** An administrator removes any project's anchor, whatever the switch says (R-SIG-019). */
  protected readonly projectControls = (): AnchorRowControls => ({ toggle: false, remove: this.canWrite() });

  protected readonly promise = signaturePromise();
  protected readonly principal = principalName;
  protected readonly blockKind = blockKindWords;
  protected readonly anchorsCaption = $localize`:@@sig.settings.anchors-caption:Trust anchors of the installation`;
  protected readonly projectCaption = $localize`:@@sig.settings.project-caption:Trust anchors of the projects`;
  protected readonly projectEmpty = $localize`:@@sig.settings.project-empty:No project has a trust anchor of its own.`;
  protected readonly blocksCaption = $localize`:@@sig.settings.blocks-caption:Blocked signers`;
  protected readonly blocksEmpty = $localize`:@@sig.settings.blocks-empty:No signer is blocked.`;
  protected readonly blocksHint = $localize`:@@sig.settings.blocks-hint:Signers whose signatures read as not trusted, whatever anchor knows them.`;
  protected readonly rootHeading = $localize`:@@sig.settings.root:Public Sigstore root`;
  protected readonly rootHint = $localize`:@@sig.settings.root-hint:Where the public Sigstore root and npm's registry keys in use come from.`;
  protected readonly blockColumns = computed<readonly TableColumn[]>(() => [
    { id: 'value', label: $localize`:@@sig.block.col.signer:Signer`, width: '16rem', prefer: true },
    { id: 'reason', label: $localize`:@@sig.block.col.reason:Reason`, width: '14rem' },
    { id: 'signatures', label: $localize`:@@sig.anchor.col.signatures:Signatures`, numeric: true },
    { id: 'blocked', label: $localize`:@@sig.block.col.blocked:Blocked` },
    ...(this.canWrite() ? [{ id: 'actions', label: $localize`:@@sig.anchor.col.actions:Actions`, hideLabel: true }] : []),
  ]);

  constructor() {
    void this.load();
  }

  protected async load(): Promise<void> {
    this.loadError.set(null);
    try {
      const [settings, anchors, project, blocks] = await Promise.all([
        this.api.settings(),
        this.api.anchors(),
        this.api.anchors('project'),
        this.api.blocks(),
      ]);
      this.adopt(settings);
      this.anchors.set(anchors.filter((a) => a.scope !== 'project'));
      this.projectAnchorList.set(project);
      this.blocks.set(blocks);
    } catch (e) {
      this.loadError.set(asApiError(e));
    }
  }

  private adopt(loaded: LoadedSignatureSettings): void {
    this.loaded.set(loaded);
    this.projectAnchors.set(loaded.settings.projectAnchors);
  }

  private mark(id: number, on: boolean): void {
    this.busy.update((s) => {
      const next = new Set(s);
      if (on) next.add(id);
      else next.delete(id);
      return next;
    });
  }

  protected async addAnchor(): Promise<void> {
    const ref = this.dialog.open<AnchorDialog, AnchorDialogData, SignatureAnchor>(AnchorDialog, { data: { project: null }, maxWidth: '44rem' });
    const created = await firstValueFrom(ref.closed);
    if (created) this.anchors.update((list) => [...list, created]);
  }

  /** Switches a shipped or an administrator's anchor on or off (`PATCH {enabled}` with its version). */
  protected async toggle(change: { readonly anchor: SignatureAnchor; readonly on: boolean }): Promise<void> {
    const { anchor, on } = change;
    // Switching an anchor off narrows every gate that relied on it (R-SIG-071): asked with its impact when that is not 0.
    if (!on) {
      const title = anchor.title;
      const go = await this.gate.askImpact({ anchor: anchor.id, change: 'disable' }, null, {
        title: $localize`:@@sig.anchor.off-title:Switch off the trust anchor ${title}:title:?`,
        confirmLabel: $localize`:@@sig.anchor.off-confirm:Switch off`,
      });
      if (!go) return;
    }
    this.mark(anchor.id, true);
    this.actionError.set(null);
    try {
      const saved = await this.api.updateAnchor(anchor, { enabled: on });
      this.anchors.update((list) => list.map((a) => (a.id === saved.id ? saved : a)));
      const title = anchor.title;
      this.notifier.status(
        on
          ? $localize`:@@sig.anchor.switched-on:${title}:title: applies again. Its signatures are judged again in the background.`
          : $localize`:@@sig.anchor.switched-off:${title}:title: no longer applies. Its signatures are judged again in the background.`,
      );
    } catch (e) {
      this.actionError.set(asApiError(e));
    } finally {
      this.mark(anchor.id, false);
    }
  }

  protected async removeAnchor(anchor: SignatureAnchor): Promise<void> {
    const title = anchor.title;
    // The removal's question, with the impact on the gates of R-SIG-071 when it refuses anything.
    const go = await this.gate.askImpact(
      { anchor: anchor.id, change: 'remove' },
      {
        title: $localize`:@@sig.anchor.remove-title:Remove the trust anchor ${title}:title:?`,
        lines: [
          $localize`:@@sig.anchor.remove-text:The signatures it knows read "Signed · signer not trusted" unless another anchor knows their signer.`,
        ],
        confirmLabel: $localize`:@@sig.anchor.remove-confirm:Remove trust anchor`,
      },
      { title: '', confirmLabel: '' },
    );
    if (!go) return;
    this.mark(anchor.id, true);
    this.actionError.set(null);
    try {
      await this.api.deleteAnchor(anchor.id);
      this.anchors.update((list) => list.filter((a) => a.id !== anchor.id));
      this.projectAnchorList.update((list) => list.filter((a) => a.id !== anchor.id));
      this.notifier.status($localize`:@@sig.anchor.removed:Trust anchor removed.`);
    } catch (e) {
      this.actionError.set(asApiError(e));
    } finally {
      this.mark(anchor.id, false);
    }
  }

  /** Saves the switch for project Owners with the loaded `ETag`; switching it off asks first while projects hold anchors. */
  protected async saveSwitch(event: Event): Promise<void> {
    event.preventDefault();
    const current = this.loaded();
    if (!current || !this.dirty() || this.saving()) return;
    const on = this.projectAnchors();
    if (!on) {
      // Switching project anchors off narrows the gates of their projects (R-SIG-071): the impact joins the question.
      const n = this.projectAnchorList().length;
      const off = {
        title: $localize`:@@sig.switch.off-title:Switch project trust anchors off?`,
        confirmLabel: $localize`:@@sig.switch.off-confirm:Switch off`,
      };
      const base =
        n > 0
          ? {
              ...off,
              lines: [
                $localize`:@@sig.switch.off-text:${n}:count: project anchors stay stored and stop applying. The signatures only they know read "Signed · signer not trusted".`,
              ],
            }
          : null;
      if (!(await this.gate.askImpact({ projectAnchors: false }, base, off))) return;
    }
    this.saving.set(true);
    this.switchError.set(null);
    try {
      this.adopt(await this.api.updateSettings(on, current.etag));
      this.projectAnchorList.update((list) => list.map((a) => ({ ...a, applied: on })));
      this.notifier.status(
        on
          ? $localize`:@@sig.switch.saved-on:Project owners may add trust anchors for their project now.`
          : $localize`:@@sig.switch.saved-off:Project trust anchors are switched off.`,
      );
    } catch (e) {
      const error = asApiError(e);
      if (error.status === 412) await this.resolveConflict(on);
      else this.switchError.set(error);
    } finally {
      this.saving.set(false);
    }
  }

  private async resolveConflict(on: boolean): Promise<void> {
    let theirs: LoadedSignatureSettings;
    try {
      theirs = await this.api.settings();
    } catch (e) {
      this.switchError.set(asApiError(e));
      return;
    }
    if (theirs.settings.projectAnchors === on) {
      this.adopt(theirs);
      return;
    }
    const decision = await this.conflicts.resolve({ mine: { projectAnchors: on }, theirs: { projectAnchors: theirs.settings.projectAnchors } });
    if (decision === 'overwrite') {
      try {
        this.adopt(await this.api.updateSettings(on, theirs.etag));
      } catch (e) {
        this.switchError.set(asApiError(e));
      }
    } else {
      this.adopt(theirs);
    }
  }

  protected async blockSigner(): Promise<void> {
    const ref = this.dialog.open<BlockDialog, unknown, SignatureBlock>(BlockDialog, { maxWidth: '36rem' });
    const created = await firstValueFrom(ref.closed);
    if (created) this.blocks.update((list) => [...list, created]);
  }

  protected async lift(block: SignatureBlock): Promise<void> {
    const answer = await this.confirm.ask({
      title: $localize`:@@sig.block.lift-title:Lift this block?`,
      lines: [$localize`:@@sig.block.lift-text:The signer's signatures are judged by the trust anchors again.`],
      confirmLabel: $localize`:@@sig.block.lift-confirm:Lift block`,
    });
    if (answer === null) return;
    this.mark(-block.id, true);
    this.actionError.set(null);
    try {
      await this.api.deleteBlock(block.id);
      this.blocks.update((list) => list.filter((b) => b.id !== block.id));
      this.notifier.status($localize`:@@sig.block.lifted:Block lifted.`);
    } catch (e) {
      this.actionError.set(asApiError(e));
    } finally {
      this.mark(-block.id, false);
    }
  }

  protected liftBusy(block: SignatureBlock): boolean {
    return this.busy().has(-block.id);
  }
}
