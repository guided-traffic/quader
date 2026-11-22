import { DatePipe, DecimalPipe, NgTemplateOutlet } from '@angular/common';
import { Component, computed, inject, signal } from '@angular/core';
import { firstValueFrom } from 'rxjs';
import { ApiError } from '../../api/problem';
import { SIGNING_CHAIN_URL, SIGNING_TRUSTED_ROOT_URL, SigningApi, SigningIdentity, SigningOverview } from '../../api/signing-api';
import { Notifier } from '../../core/notifier';
import { ServerAddress } from '../../core/server-address';
import { SessionStore } from '../../core/session-store';
import { CodeWindow } from '../../shared/code/code-window';
import { Confirm } from '../../shared/confirm-dialog';
import { CopyButton } from '../../shared/copy-button';
import { DraftStore } from '../../shared/drafts';
import { ErrorNotice } from '../../shared/error-notice';
import { principalName } from '../../shared/malware/malware-words';
import { asApiError } from '../../shared/paged-resource';
import {
  groupedDigest,
  signingDaysLeft,
  signingRefusalText,
  signingSourceText,
  signingStateText,
  signingValidity,
} from '../../shared/signatures/signing-words';
import { DataTable, TableColumn } from '../../shared/ui/data-table';
import { QuaderDialog } from '../../shared/ui/dialog';
import { Disclosure } from '../../shared/ui/disclosure';
import { TimeAgo } from '../../shared/ui/time-ago';
import {
  SigningCertificateDialog,
  SigningCertificateDialogData,
  SigningCertificateDraft,
  SigningDraft,
  SigningImportDialog,
  SigningKeyDialog,
  SigningKeyDialogData,
  SigningKeyDraft,
} from './signing-dialogs';

/**
 * The section **Signing identity** of **Admin area › Settings › Signatures** (R-SIG-056, Q-SIG-70), first on the page and open, in
 * every state of R-SIG-045: none (the sentence; for the holder of `security-settings:write` **Create signing key**, and **Import key and
 * certificate** behind the section's **More options**), a pending key request (its request with **Copy** and **Download**, the sentence
 * about the company's CA, **Upload certificate** and **Discard**; shown again after a reload), from the Helm values (the facts and no
 * controls; a refused pair of the files as a red line with its reason), uploaded (the facts, **Replace**, **Remove**). The facts are the
 * subject, the issuer, the alternative names, the validity with "expires in 12 days" amber from 14 days and "expired" red, the
 * fingerprint, npm's key ID and the state, with **Download chain** and **Download trusted root**; an announced identity stands below with
 * its switch; one quiet line says what the job `signing` has left to do. **Previous identities**, closed, each with its dates and
 * **Retire**. An administrator without the right reads the same
 * without a control. The writes need a recent sign-in: the app's re-authentication dialog asks for it, and the key request and the
 * certificate wait as a draft of the route and open again after it (Q-SIG-174).
 */
@Component({
  selector: 'quader-signing-identity',
  imports: [CodeWindow, CopyButton, DataTable, DatePipe, DecimalPipe, Disclosure, ErrorNotice, NgTemplateOutlet, TimeAgo],
  templateUrl: './signing-identity.html',
  styleUrl: '../../shared/access/access.scss',
  styles: `
    :host {
      display: block;
    }
    p,
    dl.facts {
      max-inline-size: var(--quader-settings-measure);
    }
    dl.facts {
      margin-block-end: var(--quader-space-3);
    }
    /* On a phone the facts stand under their names: beside a name as long as "Fingerprint (SHA-256)" a subject broke every few letters. */
    @media (max-width: 40rem) {
      dl.facts {
        grid-template-columns: minmax(0, 1fr);
        row-gap: 0;
      }
      dl.facts dd {
        margin-block-end: var(--quader-space-2);
      }
    }
    p {
      margin-block: 0 var(--quader-space-3);
    }
    h3 {
      margin: var(--quader-space-4) 0 var(--quader-space-2);
      font-size: var(--quader-text-size);
      font-weight: 600;
    }
    .row-actions {
      margin-block: var(--quader-space-3);
    }
    .refused {
      display: flex;
      align-items: flex-start;
      color: var(--quader-status-bad);
    }
    .refused .material-icons {
      flex: none;
    }
    dd.digest {
      display: flex;
      align-items: flex-start;
      gap: var(--quader-space-1);
    }
    dd.digest code {
      min-inline-size: 0;
    }
    .reason {
      overflow-wrap: anywhere;
    }
    dl.facts code {
      overflow-wrap: anywhere;
    }
    .names {
      display: flex;
      flex-direction: column;
    }
    .request {
      max-inline-size: var(--quader-settings-measure);
      margin-block: 0 var(--quader-space-3);
    }
    .unused {
      display: block;
    }
  `,
})
export class SigningIdentitySection {
  private readonly api = inject(SigningApi);
  private readonly notifier = inject(Notifier);
  private readonly confirm = inject(Confirm);
  private readonly dialog = inject(QuaderDialog);
  private readonly drafts = inject(DraftStore);
  private readonly address = inject(ServerAddress);
  private readonly session = inject(SessionStore);

  protected readonly overview = signal<SigningOverview | null>(null);
  protected readonly loadError = signal<ApiError | null>(null);
  protected readonly actionError = signal<ApiError | null>(null);
  protected readonly busy = signal(false);
  protected readonly now = signal(Date.now());

  protected readonly canWrite = computed(() => this.session.has('security-settings:write'));
  /** Whether the holder of the right may change the identity here: not while the Helm values name it (R-SIG-046). */
  protected readonly canChange = computed(() => this.canWrite() && this.overview()?.managed === false);
  /** **Remove** stands at an uploaded current identity; an identity of the Helm values is removed by taking it out of the values. */
  protected readonly canRemove = computed(() => this.canChange() && this.overview()?.current?.source === 'upload');
  /** **Replace** besides, while no key is pending (its request stands below) and none is announced (it waits for its switch). */
  protected readonly canReplace = computed(() => {
    const ov = this.overview();
    return this.canRemove() && !ov?.pending && !ov?.announced;
  });
  /** **Import key and certificate** behind **More options**: while no key is pending and none is announced (Q-SIG-172). */
  protected readonly canImport = computed(() => {
    const ov = this.overview();
    return this.canChange() && !ov?.pending && !ov?.announced;
  });
  /**
   * The job's backlog in one quiet line (R-SIG-055 `backlog`, Q-SIG-236): npm versions and receipts that wait, the oldest of them, and
   * those that reached their last attempt; shown while an identity signs or work is left, nothing beside an installation that never signed.
   */
  protected readonly backlog = computed(() => {
    const ov = this.overview();
    if (!ov) return null;
    const of = (kind: string) => ov.backlog.find((b) => b.kind === kind);
    const npm = of('npm')?.due ?? 0;
    const receipts = of('attestation')?.due ?? 0;
    const failed = ov.backlog.reduce((n, b) => n + b.failed, 0);
    const oldest = ov.backlog.map((b) => b.oldestDueAt).filter((t): t is string => !!t).sort()[0] ?? null;
    if (!ov.current && npm + receipts + failed === 0) return null;
    return { npm, receipts, failed, oldest, idle: npm + receipts + failed === 0 };
  });
  protected readonly previousColumns = computed<readonly TableColumn[]>(() => [
    { id: 'identity', label: $localize`:@@signing.prev.col.identity:Identity`, width: '12rem', prefer: true },
    { id: 'source', label: $localize`:@@signing.prev.col.source:Source`, width: '12rem' },
    { id: 'until', label: $localize`:@@signing.prev.col.until:Valid until`, width: '7rem' },
    { id: 'signed', label: $localize`:@@signing.prev.col.signed:Signed`, width: '14rem' },
    ...(this.canWrite() ? [{ id: 'actions', label: $localize`:@@signing.prev.col.actions:Actions`, hideLabel: true }] : []),
  ]);

  protected readonly chainUrl = SIGNING_CHAIN_URL;
  protected readonly rootUrl = SIGNING_TRUSTED_ROOT_URL;
  protected readonly principal = principalName;
  protected readonly grouped = groupedDigest;
  protected readonly stateText = signingStateText;
  protected readonly sourceText = signingSourceText;
  protected readonly refusalText = signingRefusalText;
  protected readonly previousCaption = $localize`:@@signing.prev.caption:Previous signing identities`;
  protected readonly previousHint = $localize`:@@signing.prev.hint:Identities that signed before. Their npm signatures stay valid until you retire them.`;
  protected readonly copyFingerprint = $localize`:@@signing.copy-fingerprint:Copy fingerprint`;
  protected readonly copyKeyId = $localize`:@@signing.copy-keyid:Copy npm key ID`;
  protected readonly copyRequest = $localize`:@@signing.copy-request:Copy request`;

  constructor() {
    void this.load(true);
  }

  protected async load(first = false): Promise<void> {
    this.loadError.set(null);
    try {
      const ov = await this.api.overview();
      this.now.set(Date.now());
      this.overview.set(ov);
      if (first) await this.restoreDraft(ov);
    } catch (e) {
      this.loadError.set(asApiError(e));
    }
  }

  /** After a sign-in that a write asked for, the entries come back and the dialog opens again (R-UI-042). */
  private async restoreDraft(ov: SigningOverview): Promise<void> {
    if (!this.canChange()) return;
    const draft = this.drafts.take<SigningDraft>();
    if (draft?.kind === 'signing-key') {
      this.notifier.status($localize`:@@signing.draft-restored:Your entries were restored. Send them again.`);
      await this.createKey(draft.replace, draft);
    } else if (draft?.kind === 'signing-certificate' && ov.pending) {
      this.notifier.status($localize`:@@signing.draft-restored:Your entries were restored. Send them again.`);
      await this.uploadCertificate(draft);
    }
  }

  protected validity(i: SigningIdentity): 'expired' | 'soon' | 'ok' {
    return signingValidity(i, this.now());
  }

  protected daysLeft(i: SigningIdentity): number {
    return signingDaysLeft(i, this.now());
  }

  /** The common name of a subject, the identity's name in a list; the whole subject when it has none. */
  protected commonName(subject: string): string {
    return /(?:^|,)CN=([^,]+)/.exec(subject)?.[1] ?? subject;
  }

  protected async createKey(replace: boolean, draft?: SigningKeyDraft): Promise<void> {
    const baseUrl = await this.address.baseUrl();
    const ref = this.dialog.open<SigningKeyDialog, SigningKeyDialogData, SigningIdentity>(SigningKeyDialog, {
      data: { replace, baseUrl, draft },
      maxWidth: '40rem',
    });
    const made = await firstValueFrom(ref.closed);
    if (!made) return;
    this.notifier.status($localize`:@@signing.key.done:The signing key is made. Have your company's CA issue a certificate for its request.`);
    await this.load();
  }

  protected async uploadCertificate(draft?: SigningCertificateDraft): Promise<void> {
    const ref = this.dialog.open<SigningCertificateDialog, SigningCertificateDialogData, SigningIdentity>(SigningCertificateDialog, {
      data: { draft },
      maxWidth: '44rem',
    });
    const installed = await firstValueFrom(ref.closed);
    if (!installed) return;
    this.installed(installed);
    await this.load();
  }

  protected async importIdentity(): Promise<void> {
    const ref = this.dialog.open<SigningImportDialog, unknown, SigningIdentity>(SigningImportDialog, { maxWidth: '44rem' });
    const installed = await firstValueFrom(ref.closed);
    if (!installed) return;
    this.installed(installed);
    await this.load();
  }

  private installed(i: SigningIdentity): void {
    this.notifier.status(
      i.state === 'announced'
        ? $localize`:@@signing.installed-announced:The new signing identity is announced. It becomes current after the announce period; until then the current one signs.`
        : $localize`:@@signing.installed-current:The signing identity is current. Quader signs with it from now on.`,
    );
  }

  /** Runs one write, then reads the identity again: the server's state is the truth after any answer, a refusal too. */
  private async act(op: () => Promise<unknown>, done: string): Promise<void> {
    if (this.busy()) return;
    this.busy.set(true);
    this.actionError.set(null);
    try {
      await op();
      this.notifier.status(done);
    } catch (e) {
      this.actionError.set(asApiError(e));
    } finally {
      this.busy.set(false);
      await this.load();
    }
  }

  protected async discard(): Promise<void> {
    const answer = await this.confirm.ask({
      title: $localize`:@@signing.discard.title:Discard the signing key?`,
      lines: [$localize`:@@signing.discard.text:The key and its request are deleted. A certificate the CA issues for this request can no longer be uploaded.`],
      confirmLabel: $localize`:@@signing.discard.confirm:Discard`,
    });
    if (answer === null) return;
    await this.act(() => this.api.discardPending(), $localize`:@@signing.discard.done:The signing key is discarded.`);
  }

  protected async remove(): Promise<void> {
    const answer = await this.confirm.ask({
      title: $localize`:@@signing.remove.title:Remove the signing identity?`,
      lines: [
        $localize`:@@signing.remove.text:It becomes a previous identity and Quader signs nothing until another identity is current. Its npm signatures stay valid, and the attestations it made stay served until its certificate expires.`,
      ],
      confirmLabel: $localize`:@@signing.remove.confirm:Remove`,
    });
    if (answer === null) return;
    await this.act(() => this.api.remove(), $localize`:@@signing.remove.done:The signing identity is removed. Quader signs nothing now.`);
  }

  protected async retire(i: SigningIdentity): Promise<void> {
    const answer = await this.confirm.ask({
      title: $localize`:@@signing.retire.title:Retire this identity?`,
      lines: [
        $localize`:@@signing.retire.text:Its key leaves npm's keys at once; Quader signs its packages and attestations again with the current identity.`,
      ],
      confirmLabel: $localize`:@@signing.retire.confirm:Retire`,
    });
    if (answer === null) return;
    await this.act(() => this.api.retire(i.id), $localize`:@@signing.retire.done:The identity is retired.`);
  }
}
