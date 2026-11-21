import { Component, computed, inject, signal } from '@angular/core';
import { ApiError } from '../../api/problem';
import { SigningApi, SigningCertificateUpload, SigningIdentity, SigningKeyRequest } from '../../api/signing-api';
import { TextField } from '../../shared/access/fields';
import { DraftStore } from '../../shared/drafts';
import { ErrorNotice } from '../../shared/error-notice';
import { fieldErrorAt } from '../../shared/field-errors';
import { asApiError } from '../../shared/paged-resource';
import { signingFieldError } from '../../shared/signatures/signing-words';
import { DIALOG_DATA, DialogClose, DialogRef, DialogTitle } from '../../shared/ui/dialog';
import { Disclosure } from '../../shared/ui/disclosure';

/** The entries of the key request, kept as a draft of the route when the write needs a recent sign-in (R-API-007, R-UI-042). */
export interface SigningKeyDraft {
  readonly kind: 'signing-key';
  readonly replace: boolean;
  readonly cn: string;
  readonly o: string;
  readonly c: string;
  readonly uri: string;
  readonly ou: string;
  readonly l: string;
  readonly st: string;
  readonly email: string;
}

/** The certificate and the chain, public material, kept as a draft of the route the same way. */
export interface SigningCertificateDraft {
  readonly kind: 'signing-certificate';
  readonly chain: string;
  readonly ca: string;
}

export type SigningDraft = SigningKeyDraft | SigningCertificateDraft;

export interface SigningKeyDialogData {
  /** A key request beside a current identity: **Replace**. */
  readonly replace: boolean;
  /** The server's configured base URL (Q-UI-69), the default of the URI; null when none is configured. */
  readonly baseUrl: string | null;
  readonly draft?: SigningKeyDraft;
}

/** The default common name of a key request, the server's too (R-SIG-047, Q-SIG-147). */
export const DEFAULT_SIGNING_CN = 'Quader signing';

/**
 * **Create signing key** and **Replace** (R-SIG-047, R-SIG-056): the fields hold the defaults (common name `Quader signing`, the URI
 * of the base URL), organization and country beside them; organizational unit, locality, state and an e-mail address behind **More
 * options**. Quader makes the key and answers the certificate request; the key never leaves Quader. The form takes one URI and one
 * e-mail address, more through the API (Q-SIG-171). A refusal of a member stands under its field.
 */
@Component({
  selector: 'quader-signing-key-dialog',
  imports: [DialogClose, DialogTitle, Disclosure, ErrorNotice, TextField],
  template: `
    <h2 quaderDialogTitle>{{ heading }}</h2>
    <form (submit)="submit($event)" novalidate>
      <div class="dialog-body">
        <p class="lead" i18n="@@signing.key.lead">
          Quader makes an ECDSA P-256 key and keeps it sealed; it never leaves Quader. You get a certificate request for your company's CA.
        </p>
        @if (data.replace) {
          <p class="lead" i18n="@@signing.key.replace-lead">The current identity signs until the new one is current.</p>
        }
        @if (otherError(); as e) {
          <quader-error-notice [error]="e" />
        }
        <quader-text-field width="name" i18n-label="@@signing.key.cn" label="Common name" name="cn" [(value)]="cn" [error]="err('/subject/CN')" />
        <div class="row">
          <quader-text-field width="name" i18n-label="@@signing.key.o" label="Organization" name="o" [(value)]="o" [error]="err('/subject/O')" />
          <quader-text-field
            width="number"
            i18n-label="@@signing.key.c"
            label="Country"
            name="c"
            [(value)]="c"
            [error]="err('/subject/C')"
            i18n-hint="@@signing.key.c-hint"
            hint="Two letters, such as DE."
          />
        </div>
        <quader-text-field
          width="full"
          i18n-label="@@signing.key.uri"
          label="URI"
          name="uri"
          [(value)]="uri"
          [error]="err('/uris/0') ?? err('/uris')"
          i18n-hint="@@signing.key.uri-hint"
          hint="The identity a verifier names, such as cosign's --certificate-identity."
        />
        <quader-disclosure [inline]="true" i18n-heading="@@signing.key.more" heading="More options">
          <div class="row">
            <quader-text-field width="name" i18n-label="@@signing.key.ou" label="Organizational unit" name="ou" [(value)]="ou" [error]="err('/subject/OU')" />
            <quader-text-field width="name" i18n-label="@@signing.key.l" label="Locality" name="l" [(value)]="l" [error]="err('/subject/L')" />
            <quader-text-field width="name" i18n-label="@@signing.key.st" label="State or province" name="st" [(value)]="st" [error]="err('/subject/ST')" />
            <quader-text-field width="name" type="email" i18n-label="@@signing.key.email" label="E-mail address" name="email" [(value)]="email" [error]="err('/emails/0') ?? err('/emails')" />
          </div>
        </quader-disclosure>
      </div>
      <div class="dialog-actions">
        <button class="btn btn-tertiary" type="button" quaderDialogClose i18n="@@signing.dialog.cancel">Cancel</button>
        <button class="btn btn-confirm" type="submit" [disabled]="busy()" i18n="@@signing.key.submit">Create signing key</button>
      </div>
    </form>
  `,
  styleUrl: '../../shared/access/access.scss',
  styles: `
    .lead {
      margin-block: 0 var(--quader-space-3);
    }
    .row {
      display: grid;
      grid-template-columns: repeat(2, minmax(0, 1fr));
      column-gap: var(--quader-space-4);
    }
    @media (max-width: 40rem) {
      .row {
        grid-template-columns: minmax(0, 1fr);
      }
    }
  `,
})
export class SigningKeyDialog {
  protected readonly data = inject<SigningKeyDialogData>(DIALOG_DATA);
  private readonly api = inject(SigningApi);
  private readonly ref = inject<DialogRef<SigningIdentity, SigningKeyDialog>>(DialogRef);
  private readonly drafts = inject(DraftStore);

  protected readonly heading = this.data.replace
    ? $localize`:@@signing.key.heading-replace:Replace signing identity`
    : $localize`:@@signing.key.heading:Create signing key`;
  protected readonly cn = signal(this.data.draft?.cn ?? DEFAULT_SIGNING_CN);
  protected readonly o = signal(this.data.draft?.o ?? '');
  protected readonly c = signal(this.data.draft?.c ?? '');
  protected readonly uri = signal(this.data.draft?.uri ?? this.data.baseUrl ?? '');
  protected readonly ou = signal(this.data.draft?.ou ?? '');
  protected readonly l = signal(this.data.draft?.l ?? '');
  protected readonly st = signal(this.data.draft?.st ?? '');
  protected readonly email = signal(this.data.draft?.email ?? '');
  protected readonly busy = signal(false);
  protected readonly error = signal<ApiError | null>(null);
  /** A refusal that names no field of the form stands at the top. */
  protected readonly otherError = computed(() => {
    const e = this.error();
    return e && e.fieldErrors.size === 0 ? e : null;
  });

  protected err(pointer: string): string | null {
    return fieldErrorAt(this.error(), pointer);
  }

  /** The request the form describes: only the members that hold a value, so that the server fills the rest (Q-SIG-147). */
  protected request(): SigningKeyRequest {
    const subject = Object.fromEntries(
      (
        [
          ['CN', this.cn()],
          ['O', this.o()],
          ['OU', this.ou()],
          ['L', this.l()],
          ['ST', this.st()],
          ['C', this.c().toUpperCase()],
        ] as const
      )
        .map(([k, v]) => [k, v.trim()] as const)
        .filter(([, v]) => v !== ''),
    );
    const uri = this.uri().trim();
    const email = this.email().trim();
    return { subject, uris: uri ? [uri] : [], emails: email ? [email] : [] };
  }

  private draft(): SigningKeyDraft {
    return { kind: 'signing-key', replace: this.data.replace, cn: this.cn(), o: this.o(), c: this.c(), uri: this.uri(), ou: this.ou(), l: this.l(), st: this.st(), email: this.email() };
  }

  protected async submit(event: Event): Promise<void> {
    event.preventDefault();
    if (this.busy()) return;
    this.busy.set(true);
    this.error.set(null);
    try {
      this.ref.close(await this.api.createKey(this.request()));
    } catch (e) {
      const error = asApiError(e);
      this.error.set(error);
      if (error.code === 'reauthentication_required') this.drafts.keep(this.draft());
    } finally {
      this.busy.set(false);
    }
  }
}

export interface SigningCertificateDialogData {
  readonly draft?: SigningCertificateDraft;
}

/**
 * **Upload certificate** (R-SIG-047): what the company's CA issued for the pending key, with its chain (PEM, the certificate first,
 * then its intermediates, the root last); the root alone behind **More options** for a CA that sends it apart. A refusal of R-SIG-045
 * stands under the field with the check it names in plain words (`signingFieldError`); a certificate for another key is the notice at
 * the top (`422 signing_key_mismatch`).
 */
@Component({
  selector: 'quader-signing-certificate-dialog',
  imports: [DialogClose, DialogTitle, Disclosure, ErrorNotice, TextField],
  template: `
    <h2 quaderDialogTitle i18n="@@signing.cert.heading">Upload certificate</h2>
    <form (submit)="submit($event)" novalidate>
      <div class="dialog-body">
        <p class="lead" i18n="@@signing.cert.lead">
          The certificate your company's CA issued for the request, with its chain: the certificate first, then its intermediates, the root last.
        </p>
        @if (otherError(); as e) {
          <quader-error-notice [error]="e" />
        }
        <quader-text-field width="full" type="textarea" i18n-label="@@signing.cert.chain" label="Certificate and chain (PEM)" name="chain" [(value)]="chain" [required]="true" [error]="err('/chain')" />
        <quader-disclosure [inline]="true" i18n-heading="@@signing.key.more" heading="More options">
          <quader-text-field
            width="full"
            type="textarea"
            i18n-label="@@signing.cert.ca"
            label="Root certificate (PEM)"
            name="ca"
            [(value)]="ca"
            [error]="err('/ca')"
            i18n-hint="@@signing.cert.ca-hint"
            hint="Only if the chain does not end in the root."
          />
        </quader-disclosure>
      </div>
      <div class="dialog-actions">
        <button class="btn btn-tertiary" type="button" quaderDialogClose i18n="@@signing.dialog.cancel">Cancel</button>
        <button class="btn btn-confirm" type="submit" [disabled]="busy() || chain().trim() === ''" i18n="@@signing.cert.submit">Upload certificate</button>
      </div>
    </form>
  `,
  styleUrl: '../../shared/access/access.scss',
  styles: `
    .lead {
      margin-block: 0 var(--quader-space-3);
    }
  `,
})
export class SigningCertificateDialog {
  protected readonly data = inject<SigningCertificateDialogData | null>(DIALOG_DATA, { optional: true });
  private readonly api = inject(SigningApi);
  private readonly ref = inject<DialogRef<SigningIdentity, SigningCertificateDialog>>(DialogRef);
  private readonly drafts = inject(DraftStore);

  protected readonly chain = signal(this.data?.draft?.chain ?? '');
  protected readonly ca = signal(this.data?.draft?.ca ?? '');
  protected readonly busy = signal(false);
  protected readonly error = signal<ApiError | null>(null);
  protected readonly otherError = computed(() => {
    const e = this.error();
    return e && e.fieldErrors.size === 0 ? e : null;
  });

  protected err(pointer: string): string | null {
    return signingFieldError(this.error(), pointer);
  }

  protected async submit(event: Event): Promise<void> {
    event.preventDefault();
    if (this.busy()) return;
    const body: SigningCertificateUpload = this.ca().trim() ? { chain: this.chain(), ca: this.ca() } : { chain: this.chain() };
    this.busy.set(true);
    this.error.set(null);
    try {
      this.ref.close(await this.api.uploadCertificate(body));
    } catch (e) {
      const error = asApiError(e);
      this.error.set(error);
      if (error.code === 'reauthentication_required') {
        this.drafts.keep({ kind: 'signing-certificate', chain: this.chain(), ca: this.ca() } satisfies SigningCertificateDraft);
      }
    } finally {
      this.busy.set(false);
    }
  }
}

/**
 * **Import key and certificate** (R-SIG-047), behind the section's **More options**: for a CA that hands out the key and the certificate
 * together, or a key moved from another installation. The key (PKCS#8, SEC 1 or encrypted PKCS#8) and its passphrase are sent once and
 * kept nowhere in the browser: they leave the form whatever the answer was, and no draft keeps them (R-UI-043), also when the import needs
 * a recent sign-in.
 */
@Component({
  selector: 'quader-signing-import-dialog',
  imports: [DialogClose, DialogTitle, Disclosure, ErrorNotice, TextField],
  template: `
    <h2 quaderDialogTitle i18n="@@signing.import.heading">Import key and certificate</h2>
    <form (submit)="submit($event)" novalidate>
      <div class="dialog-body">
        <p class="lead" i18n="@@signing.import.lead">
          For a CA that hands out the key and the certificate together, or a key moved from another installation. The key is sent once and
          sealed on the server; it is never shown again.
        </p>
        @if (otherError(); as e) {
          <quader-error-notice [error]="e" />
        }
        <div class="field">
          <label class="field-label" for="signing-import-key" i18n="@@signing.import.key">Private key (PEM)</label>
          <textarea
            id="signing-import-key"
            class="area-input field-w-full"
            rows="5"
            spellcheck="false"
            autocomplete="off"
            aria-describedby="signing-import-key-hint"
            [attr.aria-invalid]="err('/key') ? 'true' : null"
            [value]="key()"
            (input)="key.set($any($event.target).value)"
          ></textarea>
          <p id="signing-import-key-hint" class="hint" i18n="@@signing.import.key-hint">
            PKCS#8 or SEC 1, encrypted or not. PKCS#12 is not taken: convert it with openssl pkcs12 -in &lt;file&gt; -nodes.
          </p>
        </div>
        @if (err('/key'); as text) {
          <p class="field-error" role="alert">{{ text }}</p>
        }
        <quader-text-field
          width="name"
          type="password"
          i18n-label="@@signing.import.passphrase"
          label="Passphrase"
          name="passphrase"
          [(value)]="passphrase"
          [secret]="true"
          i18n-hint="@@signing.import.passphrase-hint"
          hint="Only if the key is encrypted. It is used once and never stored."
        />
        <quader-text-field width="full" type="textarea" i18n-label="@@signing.cert.chain" label="Certificate and chain (PEM)" name="chain" [(value)]="chain" [required]="true" [error]="err('/chain')" />
        <quader-disclosure [inline]="true" i18n-heading="@@signing.key.more" heading="More options">
          <quader-text-field
            width="full"
            type="textarea"
            i18n-label="@@signing.cert.ca"
            label="Root certificate (PEM)"
            name="ca"
            [(value)]="ca"
            [error]="err('/ca')"
            i18n-hint="@@signing.cert.ca-hint"
            hint="Only if the chain does not end in the root."
          />
        </quader-disclosure>
      </div>
      <div class="dialog-actions">
        <button class="btn btn-tertiary" type="button" quaderDialogClose i18n="@@signing.dialog.cancel">Cancel</button>
        <button class="btn btn-confirm" type="submit" [disabled]="busy() || key().trim() === '' || chain().trim() === ''" i18n="@@signing.import.submit">Import</button>
      </div>
    </form>
  `,
  styleUrl: '../../shared/access/access.scss',
  styles: `
    .lead {
      margin-block: 0 var(--quader-space-3);
    }
    .field {
      display: flex;
      flex-direction: column;
      gap: var(--quader-space-1);
      margin-block-end: var(--quader-space-3);
    }
    .field:has(+ .field-error) {
      margin-block-end: var(--quader-space-1);
    }
    .hint {
      margin: 0;
    }
  `,
})
export class SigningImportDialog {
  private readonly api = inject(SigningApi);
  private readonly ref = inject<DialogRef<SigningIdentity, SigningImportDialog>>(DialogRef);

  protected readonly key = signal('');
  protected readonly passphrase = signal('');
  protected readonly chain = signal('');
  protected readonly ca = signal('');
  protected readonly busy = signal(false);
  protected readonly error = signal<ApiError | null>(null);
  protected readonly otherError = computed(() => {
    const e = this.error();
    return e && e.fieldErrors.size === 0 ? e : null;
  });

  protected err(pointer: string): string | null {
    return signingFieldError(this.error(), pointer);
  }

  protected async submit(event: Event): Promise<void> {
    event.preventDefault();
    if (this.busy()) return;
    const body = {
      key: this.key(),
      chain: this.chain(),
      ...(this.passphrase() !== '' ? { passphrase: this.passphrase() } : {}),
      ...(this.ca().trim() ? { ca: this.ca() } : {}),
    };
    this.busy.set(true);
    this.error.set(null);
    try {
      this.ref.close(await this.api.importIdentity(body));
    } catch (e) {
      this.error.set(asApiError(e));
    } finally {
      // The secret leaves the form whatever the answer was; a refused key is pasted again.
      this.key.set('');
      this.passphrase.set('');
      this.busy.set(false);
    }
  }
}
