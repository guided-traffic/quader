import { Component, computed, inject, signal } from '@angular/core';
import { ApiError } from '../../api/problem';
import { NewSignatureAnchor, SIGNATURE_ANCHOR_KINDS, SignatureAnchor, SignatureAnchorKind, SignaturesApi } from '../../api/signatures-api';
import { Notifier } from '../../core/notifier';
import { SelectField, SelectOption, TextField } from '../access/fields';
import { ErrorNotice } from '../error-notice';
import { fieldErrorAt } from '../field-errors';
import { asApiError } from '../paged-resource';
import { Checkbox } from '../ui/checkbox';
import { DIALOG_DATA, DialogClose, DialogRef, DialogTitle } from '../ui/dialog';
import { Disclosure } from '../ui/disclosure';
import { anchorKindWords } from './signature-words';
import { Busy } from '../ui/busy';

/** Where the anchor is created: the installation's (`project` null) or a project's. */
export interface AnchorDialogData {
  readonly project: string | null;
}

/** The key an anchor's title suggests: lower-case words joined by hyphens, as the key's grammar wants (R-SIG-015). */
export function keyOf(title: string): string {
  return title
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-+|-+$/g, '')
    .slice(0, 64)
    .replace(/-+$/g, '');
}

/**
 * **Add trust anchor** (R-SIG-015, R-SIG-036, R-SIG-037): the first field is the kind, and the fields follow it. A CA certificate takes
 * its PEM certificates, and behind **More options** its use (code signing, the default, or timestamping) and a restriction of the leaf's
 * subject; an OpenPGP key its armored public keys; a public key its PEM key with npm's key ID and an expiry; a Sigstore identity the
 * OIDC issuer, the identity as an exact value or a pattern, and the trust root (the public one, or the company's `trusted_root.json`).
 * The server checks the material on save (422 at the member, a private key refused as `secret_material`), and its answer stands under
 * the field. The key follows the title until it is edited.
 */
@Component({
  selector: 'quader-anchor-dialog',
  imports: [Busy, Checkbox, DialogClose, DialogTitle, Disclosure, ErrorNotice, SelectField, TextField],
  template: `
    <h2 quaderDialogTitle>{{ heading }}</h2>
    <form (submit)="submit($event)" novalidate>
      <div class="dialog-body">
        @if (otherError(); as e) {
          <quader-error-notice [error]="e" />
        }
        <quader-select-field i18n-label="@@sig.form.kind" label="Kind" name="kind" [options]="kindOptions" [value]="kind()" (valueChange)="setKind($event)" />
        <quader-text-field width="name" i18n-label="@@sig.form.title" label="Title" name="title" [value]="title()" (valueChange)="setTitle($event)" [required]="true" [error]="err('/title')"
          i18n-hint="@@sig.form.title-hint" hint="The name shown at the signatures this anchor knows." />
        <quader-text-field width="name" i18n-label="@@sig.form.key" label="Key" name="key" [value]="key()" (valueChange)="setKey($event)" [required]="true" [error]="err('/key')"
          i18n-hint="@@sig.form.key-hint" hint="Lower-case letters, digits and hyphens; unique among the anchors." />

        @switch (kind()) {
          @case ('ca') {
            <quader-text-field width="full" type="textarea" i18n-label="@@sig.form.certificates" label="Certificates (PEM)" name="certificates" [(value)]="certificates" [required]="true"
              [error]="err('/ca/certificates')" i18n-hint="@@sig.form.certificates-hint" hint="1 to 16 CA certificates, or the signer's own certificate to pin it." />
            <quader-disclosure [inline]="true" i18n-heading="@@sig.form.more" heading="More options">
              <quader-select-field i18n-label="@@sig.form.use" label="Use" name="use" [options]="useOptions" [(value)]="use" />
              <p class="hint" i18n="@@sig.form.subject-hint">Only signers whose certificate subject has these values (all optional):</p>
              <div class="row">
                <quader-text-field width="name" label="O" name="subjectO" [(value)]="subjectO" [error]="err('/ca/subject/O')" />
                <quader-text-field width="name" label="OU" name="subjectOU" [(value)]="subjectOU" [error]="err('/ca/subject/OU')" />
                <quader-text-field width="name" label="CN" name="subjectCN" [(value)]="subjectCN" [error]="err('/ca/subject/CN')" />
                <quader-text-field width="name" label="C" name="subjectC" [(value)]="subjectC" [error]="err('/ca/subject/C')" />
              </div>
              <quader-text-field width="full" type="textarea" i18n-label="@@sig.form.sans" label="Alternative names (patterns, one per line)" name="sans" [(value)]="sans"
                [error]="err('/ca/subjectAltNames')" />
            </quader-disclosure>
          }
          @case ('openpgp') {
            <quader-text-field width="full" type="textarea" i18n-label="@@sig.form.openpgp" label="Public keys (armored)" name="keys" [(value)]="openpgpKeys" [required]="true"
              [error]="err('/openpgp/keys')" i18n-hint="@@sig.form.openpgp-hint" hint="1 to 32 public keys, as gpg --armor --export writes them." />
          }
          @case ('public-key') {
            <quader-text-field width="full" type="textarea" i18n-label="@@sig.form.pem" label="Public key (PEM)" name="pem" [(value)]="pem" [required]="true"
              [error]="err('/publicKey/keys/0/pem')" />
            <quader-text-field width="full" i18n-label="@@sig.form.keyid" label="Key ID (optional)" name="keyid" [(value)]="keyid" [error]="err('/publicKey/keys/0/keyid')"
              i18n-hint="@@sig.form.keyid-hint" hint="npm's key ID, SHA256: and the key's digest in base64." />
            <quader-text-field width="bytes" type="date" i18n-label="@@sig.form.expires" label="Expires (optional)" name="expires" [(value)]="expires"
              [error]="err('/publicKey/keys/0/expires')" />
          }
          @case ('sigstore') {
            <quader-text-field width="full" i18n-label="@@sig.form.issuer" label="OIDC issuer" name="issuer" [(value)]="issuer" [required]="true" [error]="err('/sigstore/issuer')"
              i18n-hint="@@sig.form.issuer-hint" hint="The issuer of the CI's identity tokens, such as your GitLab's address." />
            <quader-text-field width="full" i18n-label="@@sig.form.identity" label="Identity" name="identity" [(value)]="identity" [required]="true"
              [error]="err(identityPattern() ? '/sigstore/identityPattern' : '/sigstore/identity')" />
            <quader-checkbox [checked]="identityPattern()" (checkedChange)="identityPattern.set($event)">
              <ng-container i18n="@@sig.form.pattern">The identity is a pattern (a regular expression of the whole identity)</ng-container>
            </quader-checkbox>
            <quader-select-field i18n-label="@@sig.form.root" label="Trust root" name="trustRoot" [options]="rootOptions" [(value)]="trustRoot" />
            @if (trustRoot() === 'own') {
              <quader-text-field width="full" type="textarea" i18n-label="@@sig.form.trusted-root" label="Trusted root (trusted_root.json)" name="trustedRoot" [(value)]="trustedRoot"
                [required]="true" [error]="trustedRootError() ?? err('/sigstore/trustedRoot')" />
            }
          }
        }
        <quader-text-field width="full" type="textarea" i18n-label="@@sig.form.note" label="Note (optional)" name="note" [(value)]="note" [error]="err('/note')" />
      </div>
      <div class="dialog-actions">
        <button class="btn btn-tertiary" type="button" quaderDialogClose i18n="@@sig.form.cancel">Cancel</button>
        <button class="btn btn-confirm" type="submit" [disabled]="busy()" [quaderBusy]="busy()" i18n="@@sig.form.submit">Add trust anchor</button>
      </div>
    </form>
  `,
  styleUrl: '../access/access.scss',
  styles: `
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
    .hint {
      margin-block: 0 var(--quader-space-2);
    }
  `,
})
export class AnchorDialog {
  private readonly api = inject(SignaturesApi);
  private readonly ref = inject<DialogRef<SignatureAnchor, AnchorDialog>>(DialogRef);
  private readonly notifier = inject(Notifier);
  private readonly data = inject<AnchorDialogData | null>(DIALOG_DATA, { optional: true });

  protected readonly heading = this.data?.project
    ? $localize`:@@sig.form.heading-project:Add a trust anchor to this project`
    : $localize`:@@sig.form.heading:Add trust anchor`;
  protected readonly kindOptions: readonly SelectOption[] = SIGNATURE_ANCHOR_KINDS.map((k) => ({ value: k, label: anchorKindWords(k) }));
  protected readonly useOptions: readonly SelectOption[] = [
    { value: 'codeSigning', label: $localize`:@@sig.form.use-code:Code signing` },
    { value: 'timestamping', label: $localize`:@@sig.form.use-timestamping:Timestamping` },
  ];
  protected readonly rootOptions: readonly SelectOption[] = [
    { value: 'public', label: $localize`:@@sig.form.root-public:The public Sigstore` },
    { value: 'own', label: $localize`:@@sig.form.root-own:Our own Sigstore` },
  ];

  protected readonly kind = signal<SignatureAnchorKind>('ca');
  protected readonly title = signal('');
  protected readonly key = signal('');
  private readonly keyEdited = signal(false);
  protected readonly certificates = signal('');
  protected readonly use = signal('codeSigning');
  protected readonly subjectO = signal('');
  protected readonly subjectOU = signal('');
  protected readonly subjectCN = signal('');
  protected readonly subjectC = signal('');
  protected readonly sans = signal('');
  protected readonly openpgpKeys = signal('');
  protected readonly pem = signal('');
  protected readonly keyid = signal('');
  protected readonly expires = signal('');
  protected readonly issuer = signal('');
  protected readonly identity = signal('');
  protected readonly identityPattern = signal(false);
  protected readonly trustRoot = signal('public');
  protected readonly trustedRoot = signal('');
  protected readonly note = signal('');
  protected readonly busy = signal(false);
  protected readonly error = signal<ApiError | null>(null);
  protected readonly trustedRootError = signal<string | null>(null);
  /** A refusal that names no field of the form stands at the top. */
  protected readonly otherError = computed(() => {
    const e = this.error();
    return e && e.fieldErrors.size === 0 ? e : null;
  });

  protected err(pointer: string): string | null {
    return fieldErrorAt(this.error(), pointer);
  }

  protected setKind(value: string): void {
    this.kind.set(value as SignatureAnchorKind);
    this.error.set(null);
  }

  protected setTitle(value: string): void {
    this.title.set(value);
    if (!this.keyEdited()) this.key.set(keyOf(value));
  }

  protected setKey(value: string): void {
    this.key.set(value);
    this.keyEdited.set(true);
  }

  /** The anchor the form describes, or null when the company's trusted root is no JSON. */
  protected anchor(): NewSignatureAnchor | null {
    const base = { key: this.key().trim(), title: this.title().trim(), kind: this.kind(), enabled: true, ...(this.note().trim() ? { note: this.note().trim() } : {}) };
    const lines = (text: string): string[] => text.split('\n').map((l) => l.trim()).filter((l) => l !== '');
    switch (this.kind()) {
      case 'ca': {
        const subject = Object.fromEntries(
          (
            [
              ['O', this.subjectO()],
              ['OU', this.subjectOU()],
              ['CN', this.subjectCN()],
              ['C', this.subjectC()],
            ] as const
          )
            .map(([k, v]) => [k, v.trim()] as const)
            .filter(([, v]) => v !== ''),
        );
        const patterns = lines(this.sans());
        return {
          ...base,
          ca: {
            certificates: this.certificates(),
            use: this.use() === 'timestamping' ? 'timestamping' : 'codeSigning',
            ...(Object.keys(subject).length > 0 ? { subject } : {}),
            ...(patterns.length > 0 ? { subjectAltNames: patterns } : {}),
          },
        };
      }
      case 'openpgp':
        return { ...base, openpgp: { keys: this.openpgpKeys() } };
      case 'public-key': {
        const keyid = this.keyid().trim();
        const expires = this.expires().trim();
        return { ...base, publicKey: { keys: [{ pem: this.pem(), ...(keyid ? { keyid } : {}), ...(expires ? { expires: `${expires}T00:00:00Z` } : {}) }] } };
      }
      default: {
        const who = this.identity().trim();
        const own = this.trustRoot() === 'own';
        let trustedRoot: Record<string, unknown> | undefined;
        this.trustedRootError.set(null);
        if (own) {
          try {
            const parsed: unknown = JSON.parse(this.trustedRoot());
            if (typeof parsed !== 'object' || parsed === null || Array.isArray(parsed)) throw new Error('not an object');
            trustedRoot = parsed as Record<string, unknown>;
          } catch {
            this.trustedRootError.set($localize`:@@sig.form.trusted-root-json:This is not a JSON document. Paste the whole trusted_root.json of your Sigstore.`);
            return null;
          }
        }
        return {
          ...base,
          sigstore: {
            issuer: this.issuer().trim(),
            ...(this.identityPattern() ? { identityPattern: who } : { identity: who }),
            trustRoot: own ? 'own' : 'public',
            ...(trustedRoot ? { trustedRoot } : {}),
          },
        };
      }
    }
  }

  protected async submit(event: Event): Promise<void> {
    event.preventDefault();
    if (this.busy()) return;
    const anchor = this.anchor();
    if (!anchor) return;
    this.busy.set(true);
    this.error.set(null);
    try {
      const project = this.data?.project ?? null;
      const created = project ? await this.api.createProjectAnchor(project, anchor) : await this.api.createAnchor(anchor);
      this.notifier.status($localize`:@@sig.form.created:Trust anchor added. Signatures it knows are judged again in the background.`);
      this.ref.close(created);
    } catch (e) {
      this.error.set(asApiError(e));
    } finally {
      this.busy.set(false);
    }
  }
}
